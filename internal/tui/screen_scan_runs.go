package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	bubbletable "github.com/evertras/bubble-table/table"
)

// scanRunsScreen lists per-repository scan summaries (design.md D9,
// ListRepositoryScanSummaries -- zero new AdminClient methods). It backs
// BOTH of screenAdminOperations' rows (design.md's Slice 3 File Changes
// table lists only screen_scan_runs.go for this repository-list side,
// mirroring screen_signing_repos.go's "one shared type, two named
// constructors" precedent, Phase 12.4): "Scan Runs" opens scanHistoryScreen
// on its default Vulnerabilities tab; "Secret Scan Findings" opens the SAME
// underlying repository list, but Enter defaults the Leaks tab instead --
// distinguished by id/defaultTab, set once at construction, never mutated.
type scanRunsScreen struct {
	id         screen
	defaultTab int
	summaries  []repositorySummary
	selected   int
	loaded     bool
	table      bubbletable.Model
	err        string
}

func newScanRunsScreen() scanRunsScreen {
	return scanRunsScreen{id: screenAdminScanRuns, defaultTab: scanHistoryTabIndexVulnerabilities}
}

func newSecretFindingsRunsScreen() scanRunsScreen {
	return scanRunsScreen{id: screenAdminSecretFindings, defaultTab: scanHistoryTabIndexLeaks}
}

func (s scanRunsScreen) ID() screen { return s.id }

func (s scanRunsScreen) Keys() screenKeys { return scanRunsKeys }

func (s scanRunsScreen) Init(env screenEnv) tea.Cmd {
	return loadScanRunsScreenSummariesCmd(env, 25)
}

func (s scanRunsScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
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
		s.summaries = repositorySummariesFromScanSummaries(typed.summaries)
		s.selected = boundedIndex(0, len(s.summaries))
		s.loaded = true
		s.rebuildTable(env)
		return s, nil, false
	}
	return s, nil, false
}

func (s *scanRunsScreen) rebuildTable(env screenEnv) {
	if len(s.summaries) == 0 {
		return
	}
	primary, _ := tableRoles(env.Layout)
	s.table = buildAdminScanSummaryTable(newAdminTheme(), s.summaries, s.selected, primary)
}

func (s scanRunsScreen) updateKey(env screenEnv, msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminOperations), true
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
	case isEnterKey(msg):
		summary, ok := s.selectedSummary()
		if !ok {
			return s, nil, true
		}
		return s, openScanHistory(summary.Repository, s.id, s.defaultTab), true
	case isRuneKey(msg, 'r'):
		return s, loadScanRunsScreenSummariesCmd(env, 25), true
	}
	return s, nil, true
}

func (s scanRunsScreen) selectedSummary() (repositorySummary, bool) {
	if len(s.summaries) == 0 {
		return repositorySummary{}, false
	}
	return s.summaries[boundedIndex(s.selected, len(s.summaries))], true
}

func (s scanRunsScreen) View(theme adminTheme, env screenEnv) screenFrame {
	heading := "Scan Runs"
	context := "Operations / Scan Runs"
	if s.id == screenAdminSecretFindings {
		heading = "Secret Scan Findings"
		context = "Operations / Secret Scan Findings"
	}
	lines := []string{theme.subheading.Render(heading)}
	switch {
	case s.err != "":
		lines = append(lines, theme.error.Render(s.err))
	case !s.loaded:
		lines = append(lines, theme.muted.Render("Loading repository scan history..."))
	case len(s.summaries) == 0:
		lines = append(lines, theme.muted.Render("No repository scan history found."))
	default:
		lines = append(lines, s.table.View())
	}
	lines = append(lines, renderAdminOperatorFooter(theme, env.Session, env.now())...)
	return screenFrame{
		Context: context,
		Body:    renderSection(theme, strings.Join(lines, "\n"), env.Layout),
	}
}

// loadScanRunsScreenSummariesCmd is the screenEnv-scoped equivalent of
// Model's own loadAdminRepositoryScanSummariesCmd (mirrors
// screen_trivy_repos.go's loadTrivyRepositoryScanSummariesCmd wrapper).
func loadScanRunsScreenSummariesCmd(env screenEnv, limit int) tea.Cmd {
	return env.asModel().loadAdminRepositoryScanSummariesCmd(limit)
}

var scanRunsKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up"), key.WithHelp("Up", "previous repository")),
	key.NewBinding(key.WithKeys("down"), key.WithHelp("Down", "next repository")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "scan history")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "back")),
}}
