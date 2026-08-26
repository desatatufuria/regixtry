package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// adminOperationsRow is one row of the Operations domain (design.md
// Decision I/D8/D9): Scan Runs and Secret Scan Findings are results
// screens; Update Channel (tui-update-check feature) is Operations' first
// config-like row, since it governs the registry's own runtime behavior
// (which released tags the background update-check considers) rather than
// a Trivy/Gitleaks/Signing security policy, which is why it lives here
// instead of Security & Compliance.
type adminOperationsRow struct {
	Label string
	Help  string
}

var adminOperationsRows = []adminOperationsRow{
	{Label: "Scan Runs", Help: "Repository scan summaries and history"},
	{Label: "Secret Scan Findings", Help: "Leaked-secret findings across repositories"},
	{Label: "Update Channel", Help: "Which released tags the background update-check considers"},
}

// adminOperationsScreen is screenAdminOperations (Phase 18, design.md
// Decision I): a bare peer-row list, mirroring securityMenuScreen's own
// "no page/action state of its own" shape. Scan Runs/Secret Scan Findings
// navigate into a scanRunsScreen-backed repository picker (Phase 19); Update
// Channel (tui-update-check feature) navigates into its own settings
// screen.
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
	case "Update Channel":
		return navigate(screenAdminUpdateChannel)
	}
	return nil
}

func (s adminOperationsScreen) View(theme adminTheme, env screenEnv) screenFrame {
	lines := []string{theme.subheading.Render("Operations")}
	highlighted := boundedIndex(s.selected, len(adminOperationsRows))
	peerRows := make([]peerMenuRow, len(adminOperationsRows))
	for index, row := range adminOperationsRows {
		peerRows[index] = peerMenuRow{Label: row.Label, Help: row.Help}
	}
	lines = append(lines, renderPeerMenuRows(theme, peerRows, highlighted)...)
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
