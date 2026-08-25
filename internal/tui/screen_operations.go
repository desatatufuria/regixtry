package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// adminOperationsRow is one row of the Operations domain (design.md
// Decision I/D8/D9): Scan Runs and Secret Scan Findings, both results
// screens (config lives in Security & Compliance instead).
type adminOperationsRow struct {
	Label string
	Help  string
}

var adminOperationsRows = []adminOperationsRow{
	{Label: "Scan Runs", Help: "Repository scan summaries and history"},
	{Label: "Secret Scan Findings", Help: "Leaked-secret findings across repositories"},
}

// adminOperationsScreen is screenAdminOperations (Phase 18, design.md
// Decision I): a bare 2-row peer list, mirroring securityMenuScreen's own
// "no page/action state of its own" shape. Each row navigates into its own
// scanRunsScreen-backed repository picker (Phase 19).
type adminOperationsScreen struct {
	selected int
}

func newAdminOperationsScreen() adminOperationsScreen { return adminOperationsScreen{} }

func (s adminOperationsScreen) ID() screen { return screenAdminOperations }

func (s adminOperationsScreen) Keys() screenKeys { return adminOperationsKeys }

func (s adminOperationsScreen) Init(env screenEnv) tea.Cmd { return nil }

func (s adminOperationsScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		return s.updateKey(typed)
	}
	return s, nil, false
}

func (s adminOperationsScreen) updateKey(msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminMenu), true
	case isMoveUpKey(msg):
		s.selected = boundedIndex(s.selected-1, len(adminOperationsRows))
		return s, nil, true
	case isMoveDownKey(msg):
		s.selected = boundedIndex(s.selected+1, len(adminOperationsRows))
		return s, nil, true
	case isEnterKey(msg):
		return s, s.navigateToSelected(), true
	}
	return s, nil, true
}

func (s adminOperationsScreen) navigateToSelected() tea.Cmd {
	if len(adminOperationsRows) == 0 {
		return nil
	}
	switch adminOperationsRows[boundedIndex(s.selected, len(adminOperationsRows))].Label {
	case "Scan Runs":
		return navigate(screenAdminScanRuns)
	case "Secret Scan Findings":
		return navigate(screenAdminSecretFindings)
	}
	return nil
}

func (s adminOperationsScreen) View(theme adminTheme, env screenEnv) screenFrame {
	lines := []string{theme.subheading.Render("Operations")}
	highlighted := boundedIndex(s.selected, len(adminOperationsRows))
	for index, row := range adminOperationsRows {
		label := row.Label + "  " + theme.muted.Render(row.Help)
		if index == highlighted {
			label = theme.selected.Render(row.Label) + "  " + theme.muted.Render(row.Help)
		}
		lines = append(lines, label)
	}
	lines = append(lines, renderAdminOperatorFooter(theme, env.Session, env.now())...)
	return screenFrame{
		Context: "Operations",
		Body:    renderSection(theme, strings.Join(lines, "\n"), env.Layout),
	}
}

var adminOperationsKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up"), key.WithHelp("Up", "previous item")),
	key.NewBinding(key.WithKeys("down"), key.WithHelp("Down", "next item")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "open")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "back")),
	key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
}}
