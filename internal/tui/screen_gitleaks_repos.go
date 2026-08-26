package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	bubbletable "github.com/evertras/bubble-table/table"
	"regixtry/internal/ports"
)

// featureOverrideRow is one repository row on a featureOverridesScreen:
// catalog ∪ stored overrides (design.md Decision J).
type featureOverrideRow struct {
	Repository  string
	HasOverride bool
	Detail      ports.RepositoryOverrideDetails
}

// featureOverridesScreen backs BOTH screenSecurityGitleaksRepos and
// screenSecuritySigningRepos (design.md "State Migration" section): two
// DEDICATED screens (confirmed D3), not one parameterized picker — feature
// is fixed at construction, exactly as in overrideEditor, so neither screen
// can be navigated into the other's feature. This is the actual fix for the
// originally reported defect: Gitleaks and Signing each get their own
// discoverable, independently reachable repository override entry point.
type featureOverridesScreen struct {
	id       screen
	feature  string
	rows     []featureOverrideRow
	selected int
	loaded   bool
	table    bubbletable.Model
	editor   overrideEditor
	err      string
}

// newFeatureOverridesScreen constructs an unloaded screen; Init returns the
// load Cmd (ListRepositoryOverrides merged against the Console catalog).
func newFeatureOverridesScreen(id screen, feature string) featureOverridesScreen {
	return featureOverridesScreen{id: id, feature: feature}
}

func (s featureOverridesScreen) ID() screen { return s.id }

func (s featureOverridesScreen) Keys() screenKeys { return featureOverridesKeys }

func (s featureOverridesScreen) Init(env screenEnv) tea.Cmd {
	return loadFeatureOverridesCmd(env, s.feature)
}

// Update routes keys to the embedded overrideEditor while it is open,
// otherwise to the row list; non-key messages are filtered by feature so a
// stale/foreign response is a no-op (design.md Decision H's broadcast
// model).
func (s featureOverridesScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	if s.editor.Active() {
		switch typed := msg.(type) {
		case tea.KeyMsg:
			next, cmd, consumed := s.editor.update(env, typed)
			s.editor = next
			return s, cmd, consumed
		case adminRepositoryOverrideLoadedMsg:
			next, cmd := s.editor.applyLoaded(env, typed)
			s.editor = next
			return s, cmd, false
		case adminRepositoryOverrideSavedMsg:
			s.editor = s.editor.applySaved(typed)
			s.applySavedToRow(typed)
			s.rebuildTable(env)
			return s, nil, false
		case adminSigningPolicyLoadedMsg:
			s.editor = s.editor.applyGlobalPolicyLoaded(typed)
			return s, nil, false
		case adminSigningKeyUsageLoadedMsg:
			s.editor.keys = s.editor.keys.applyUsageLoaded(typed)
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
	case featureOverridesLoadedMsg:
		if typed.feature != s.feature {
			return s, nil, false
		}
		if typed.err != nil {
			s.err = typed.err.Error()
			return s, nil, false
		}
		s.rows = mergeFeatureOverrideRows(env.KnownRepositories, typed.overrides)
		s.selected = boundedIndex(s.selected, len(s.rows))
		s.loaded = true
		s.err = ""
		s.rebuildTable(env)
		return s, nil, false
	}
	return s, nil, false
}

func (s *featureOverridesScreen) rebuildTable(env screenEnv) {
	if len(s.rows) == 0 {
		return
	}
	primary, _ := tableRoles(env.Layout)
	s.table = buildFeatureOverridesTable(newAdminTheme(), s.feature, s.rows, s.selected, primary)
}

func (s featureOverridesScreen) updateKey(env screenEnv, msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminFeatures), true
	case isMoveUpKey(msg):
		if len(s.rows) == 0 {
			return s, nil, true
		}
		s.selected = boundedIndex(s.selected-1, len(s.rows))
		s.rebuildTable(env)
		return s, nil, true
	case isMoveDownKey(msg):
		if len(s.rows) == 0 {
			return s, nil, true
		}
		s.selected = boundedIndex(s.selected+1, len(s.rows))
		s.rebuildTable(env)
		return s, nil, true
	case isRuneKey(msg, 'o'):
		// spec.md "The override key is scoped to the opening screen's row
		// only": inert with no row highlighted (empty catalog and no
		// stored overrides).
		if len(s.rows) == 0 {
			return s, nil, true
		}
		row := s.rows[boundedIndex(s.selected, len(s.rows))]
		s.editor = newOverrideEditor(s.feature, row.Repository)
		return s, s.editor.Init(env), true
	case isRuneKey(msg, 'r'):
		return s, loadFeatureOverridesCmd(env, s.feature), true
	}
	return s, nil, true
}

// applySavedToRow reflects a successful set/clear back onto the matching
// row (spec.md "...be reflected back in the modal and the repository row").
func (s *featureOverridesScreen) applySavedToRow(msg adminRepositoryOverrideSavedMsg) {
	if msg.err != nil {
		return
	}
	for i := range s.rows {
		if s.rows[i].Repository != msg.repository {
			continue
		}
		s.rows[i].HasOverride = msg.exists
		if msg.exists {
			s.rows[i].Detail = msg.override
		} else {
			s.rows[i].Detail = ports.RepositoryOverrideDetails{}
		}
		return
	}
}

func (s featureOverridesScreen) View(theme adminTheme, env screenEnv) screenFrame {
	var lines []string
	if s.err != "" {
		lines = []string{theme.subheading.Render(featureDisplayName(s.feature) + " — Repository Overrides"), theme.error.Render(s.err)}
	} else {
		lines = renderFeatureOverridesTable(theme, s.feature, s.rows, s.loaded, s.table)
	}
	frame := screenFrame{
		Context: "Security & Compliance / " + featureDisplayName(s.feature) + " / Repositories",
		Body:    renderSection(theme, strings.Join(lines, "\n"), env.Layout),
	}
	if s.editor.Active() {
		frame.Overlay = renderOverrideEditor(theme, s.editor)
	}
	return frame
}

func featureOverrideRowStatus(row featureOverrideRow) string {
	if row.HasOverride {
		return "override active"
	}
	return "inheriting global settings"
}

// featureDisplayName maps a feature identity to its display label. A
// switch over exactly the three built-in features (feature_registry.go's
// builtInFeatures), not strings.Title (deprecated).
func featureDisplayName(feature string) string {
	switch feature {
	case trivyFeatureName:
		return "Trivy"
	case gitleaksFeatureName:
		return "Gitleaks"
	case signingFeatureName:
		return "Signing"
	default:
		return feature
	}
}

// mergeFeatureOverrideRows is the row set = catalog ∪ stored overrides
// (design.md Decision J): Gitleaks/Signing have no scan-summary surface, and
// ListRepositoryOverrides alone would make "add an override to a repository
// that has none" impossible. A stored override for a repository no longer
// in the catalog still shows. An empty catalog with no stored overrides
// yields an explicit empty state (rendered by View, not fabricated here).
func mergeFeatureOverrideRows(knownRepositories []string, overrides []ports.RepositoryOverrideDetails) []featureOverrideRow {
	byRepository := make(map[string]ports.RepositoryOverrideDetails, len(overrides))
	for _, override := range overrides {
		byRepository[override.Repository] = override
	}
	seen := make(map[string]bool, len(knownRepositories))
	rows := make([]featureOverrideRow, 0, len(knownRepositories)+len(overrides))
	for _, repository := range knownRepositories {
		seen[repository] = true
		if detail, ok := byRepository[repository]; ok {
			rows = append(rows, featureOverrideRow{Repository: repository, HasOverride: true, Detail: detail})
			continue
		}
		rows = append(rows, featureOverrideRow{Repository: repository})
	}
	for _, override := range overrides {
		if seen[override.Repository] {
			continue
		}
		rows = append(rows, featureOverrideRow{Repository: override.Repository, HasOverride: true, Detail: override})
	}
	return rows
}

// featureOverridesLoadedMsg is loadFeatureOverridesCmd's result.
type featureOverridesLoadedMsg struct {
	feature   string
	overrides []ports.RepositoryOverrideDetails
	err       error
}

// loadFeatureOverridesCmd fetches every stored override row for one
// feature via the already-generic ListRepositoryOverrides (design.md
// Decision J: zero new AdminClient methods, zero backend change).
func loadFeatureOverridesCmd(env screenEnv, feature string) tea.Cmd {
	return func() tea.Msg {
		if env.Client == nil {
			return featureOverridesLoadedMsg{feature: feature, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		overrides, err := env.Client.ListRepositoryOverrides(context.Background(), env.Session, feature)
		return featureOverridesLoadedMsg{feature: feature, overrides: overrides, err: err}
	}
}

var featureOverridesKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up"), key.WithHelp("Up", "previous repository")),
	key.NewBinding(key.WithKeys("down"), key.WithHelp("Down", "next repository")),
	key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "override")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "back")),
}}
