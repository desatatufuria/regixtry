package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	bubbletable "github.com/evertras/bubble-table/table"
	"regixtry/internal/ports"
)

// trivyReposScreen is Trivy's Repository Alerts tab, promoted to its own
// top-level screen (Phase 11, design.md Decision I: "Tab becomes a screen
// switch"). It owns TrivySummaries/TrivyScanRuns/TrivySelectedAlert/
// TrivyAlertsLoaded/TrivyOverrides and the ScanSummary table (design.md's
// State Migration table), plus its own embedded overrideEditor -- mirroring
// featureOverridesScreen's own pattern, which is why the former
// slotTrivyOverride overlay slot no longer exists (screen.go).
//
// policy/policyLoaded are a read-only copy of the scan policy, loaded
// independently on Init purely to compose the persistent policy badge
// (design.md Decision 6) onto this screen's own tab-bar header -- mirroring
// how trivyConfigScreen owns the editable copy, per Decision D4 (no shared
// cross-screen state): duplicated on read, never written here.
//
// Enter opens the still-legacy, Slice-3-gated scan history modal via
// openAdminScanHistory (model.go) -- a migrated screen cannot write to
// AdminViewState.ScanHistoryModal directly (design.md Decision B), so this
// mirrors navigateMsg's own "ask the parent" pattern for that one piece of
// state Phase 11 does not migrate.
type trivyReposScreen struct {
	summaries []repositorySummary
	scanRuns  []ports.ScanRun
	selected  int
	loaded    bool
	overrides []ports.RepositoryOverrideDetails
	table     bubbletable.Model
	editor    overrideEditor
	policy    ports.ScanPolicySettings
}

func newTrivyReposScreen() trivyReposScreen { return trivyReposScreen{} }

func (s trivyReposScreen) ID() screen { return screenSecurityTrivyRepos }

func (s trivyReposScreen) Keys() screenKeys { return trivyReposKeys }

func (s trivyReposScreen) Init(env screenEnv) tea.Cmd {
	return loadTrivyRepositoryScanSummariesCmd(env, 25)
}

func (s trivyReposScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	if s.editor.Active() {
		switch typed := msg.(type) {
		case tea.KeyMsg:
			next, cmd, consumed := s.editor.update(env, typed)
			s.editor = next
			return s, cmd, consumed
		case adminRepositoryOverrideLoadedMsg:
			s.editor = s.editor.applyLoaded(typed)
			return s, nil, false
		case adminRepositoryOverrideSavedMsg:
			s.editor = s.editor.applySaved(typed)
			return s, nil, false
		}
		return s, nil, false
	}
	switch typed := msg.(type) {
	case tea.KeyMsg:
		return s.updateKey(env, typed)
	case tea.WindowSizeMsg:
		s.rebuildTable(env)
		return s, nil, false
	case adminRepositoryScanSummariesLoadedMsg:
		if typed.err != nil {
			return s, nil, false
		}
		// The backend already collapses to one row per repository, ordered
		// severity-first, before applying limit (ports.RepositoryScanSummary)
		// -- mirrors the pre-change central handler exactly.
		s.summaries = repositorySummariesFromScanSummaries(typed.summaries)
		s.scanRuns = scanRunsFromScanSummaries(typed.summaries)
		s.selected = boundedIndex(0, len(s.summaries))
		s.loaded = true
		s.rebuildTable(env)
		// The disabled-row annotation needs the override list loaded before
		// it can render; chained as a follow-up Cmd, mirroring the
		// pre-change central handler's own single-Cmd-return style.
		return s, loadTrivyRepositoryOverridesListCmd(env), false
	case adminRepositoryOverridesListLoadedMsg:
		if typed.err != nil || typed.feature != trivyFeatureName {
			return s, nil, false
		}
		s.overrides = typed.overrides
		s.rebuildTable(env)
		// The policy badge (renderTrivyTabs) needs ScanPolicy loaded before
		// it can render a real state; chained as a follow-up Cmd (not
		// tea.Batch, this codebase's own convention -- see
		// adminScanRunDetailLoadedMsg's doc comment in model.go), matching
		// Init's own single-Cmd-return chain.
		return s, loadScanPolicyCmd(env), false
	case adminScanPolicyLoadedMsg:
		if typed.err != nil {
			return s, nil, false
		}
		s.policy = typed.settings
		return s, nil, false
	case adminScanPolicyUpdatedMsg:
		if typed.err != nil {
			return s, nil, false
		}
		s.policy = typed.settings
		return s, nil, false
	}
	return s, nil, false
}

func (s *trivyReposScreen) rebuildTable(env screenEnv) {
	if len(s.summaries) == 0 {
		return
	}
	primary, _ := tableRoles(env.Layout)
	s.table = buildAdminScanSummaryTable(newAdminTheme(), annotateDisabledSummaries(s.summaries, s.overrides), s.selected, primary)
}

func (s trivyReposScreen) updateKey(env screenEnv, msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminFeatures), true
	case isTabKey(msg):
		return s, navigate(screenSecurityTrivy), true
	case isMoveUpKey(msg):
		if len(s.summaries) == 0 {
			return s, nil, true
		}
		s.selected = boundedIndex(s.selected-1, len(s.summaries))
		s.rebuildTable(env)
		return s, nil, true
	case isMoveDownKey(msg):
		if len(s.summaries) == 0 {
			return s, nil, true
		}
		s.selected = boundedIndex(s.selected+1, len(s.summaries))
		s.rebuildTable(env)
		return s, nil, true
	case isRuneKey(msg, 'o'):
		summary, ok := s.selectedSummary()
		if !ok {
			return s, nil, true
		}
		editor := newOverrideEditor(trivyFeatureName, summary.Repository)
		s.editor = editor
		return s, editor.Init(env), true
	case isEnterKey(msg):
		summary, ok := s.selectedSummary()
		if !ok {
			return s, nil, true
		}
		return s, openScanHistory(summary.Repository, screenSecurityTrivyRepos, scanHistoryTabIndexVulnerabilities), true
	case isRuneKey(msg, 'r'):
		return s, loadTrivyRepositoryScanSummariesCmd(env, 25), true
	}
	return s, nil, true
}

func (s trivyReposScreen) selectedSummary() (repositorySummary, bool) {
	if len(s.summaries) == 0 {
		return repositorySummary{}, false
	}
	return s.summaries[boundedIndex(s.selected, len(s.summaries))], true
}

func (s trivyReposScreen) View(theme adminTheme, env screenEnv) screenFrame {
	lines := []string{renderTrivyTabs(theme, screenSecurityTrivyRepos, s.policy)}
	lines = append(lines, renderAdminScanSummary(theme, s.summaries, s.loaded, s.table)...)
	lines = append(lines, renderAdminOperatorFooter(theme, env.Session, env.now())...)
	frame := screenFrame{
		Context: "Security & Compliance / Trivy / Repository Alerts",
		Body:    renderSection(theme, strings.Join(lines, "\n"), env.Layout),
	}
	if s.editor.Active() {
		frame.Overlay = renderOverrideEditor(theme, s.editor)
	}
	return frame
}

// loadTrivyRepositoryScanSummariesCmd/loadTrivyRepositoryOverridesListCmd
// are the screenEnv-scoped equivalents of Model's own loadAdmin* methods
// (mirror screen_gitleaks_config.go's configureFeatureCmd wrapper).
func loadTrivyRepositoryScanSummariesCmd(env screenEnv, limit int) tea.Cmd {
	return env.asModel().loadAdminRepositoryScanSummariesCmd(limit)
}

func loadTrivyRepositoryOverridesListCmd(env screenEnv) tea.Cmd {
	return env.asModel().loadRepositoryOverridesListCmd(trivyFeatureName)
}

var trivyReposKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up"), key.WithHelp("Up", "select alert")),
	key.NewBinding(key.WithKeys("down"), key.WithHelp("Down", "select alert")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "details")),
	key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "override")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "switch tabs")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "back")),
}}
