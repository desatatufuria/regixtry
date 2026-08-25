package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// adminMenuMenuRow is one row of the post-login domain menu (design.md
// Decision I / D8): Browse leaves the admin panel entirely (unchanged
// drill-down), Security & Compliance/Identity & Access/Operations each
// navigate into an existing top-level screen. Two levels -- domain rows to
// existing screens -- per the user's confirmed reading of Decision I (not a
// single flat grouped list, design.md's own Open Questions section).
type adminMenuRow struct {
	Label string
	Help  string
}

var adminMenuRows = []adminMenuRow{
	{Label: "Browse", Help: "Repositories, Tags, Manifests, Blobs & Uploads"},
	{Label: "Security & Compliance", Help: "Trivy, Gitleaks, Signing"},
	{Label: "Identity & Access", Help: "Users, Robots, Repository Grants, Tokens"},
	{Label: "Operations", Help: "Scan Runs, Secret Scan Findings"},
}

// adminMenuScreen is screenAdminMenu: the post-login landing screen (Phase
// 18, design.md Decision I). It replaces screenAdminUsers as the admin
// panel's own root -- Esc here leaves the panel back to Console inspection
// (returnToInspectionMsg), the role screenAdminUsers' Esc used to carry.
type adminMenuScreen struct {
	selected int
}

func newAdminMenuScreen() adminMenuScreen { return adminMenuScreen{} }

func (s adminMenuScreen) ID() screen { return screenAdminMenu }

func (s adminMenuScreen) Keys() screenKeys { return adminMenuKeys }

// Init has nothing to load: every row navigates to an already-loadable
// screen, which loads its own data once entered (mirrors securityMenuScreen
// composing peers, but this menu itself carries no backend-loaded content).
func (s adminMenuScreen) Init(env screenEnv) tea.Cmd { return nil }

func (s adminMenuScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		return s.updateKey(typed)
	}
	return s, nil, false
}

func (s adminMenuScreen) updateKey(msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return s, returnToInspectionCmd(), true
	case isMoveUpKey(msg):
		s.selected = boundedIndex(s.selected-1, len(adminMenuRows))
		return s, nil, true
	case isMoveDownKey(msg):
		s.selected = boundedIndex(s.selected+1, len(adminMenuRows))
		return s, nil, true
	case isEnterKey(msg):
		return s, s.navigateToSelected(), true
	}
	return s, nil, true
}

func (s adminMenuScreen) navigateToSelected() tea.Cmd {
	if len(adminMenuRows) == 0 {
		return nil
	}
	switch adminMenuRows[boundedIndex(s.selected, len(adminMenuRows))].Label {
	case "Browse":
		return navigate(screenRepositories)
	case "Security & Compliance":
		return navigate(screenAdminFeatures)
	case "Identity & Access":
		return navigate(screenAdminUsers)
	case "Operations":
		return navigate(screenAdminOperations)
	}
	return nil
}

func (s adminMenuScreen) View(theme adminTheme, env screenEnv) screenFrame {
	lines := []string{theme.subheading.Render("Admin")}
	highlighted := boundedIndex(s.selected, len(adminMenuRows))
	for index, row := range adminMenuRows {
		label := row.Label + "  " + theme.muted.Render(row.Help)
		if index == highlighted {
			label = theme.selected.Render(row.Label) + "  " + theme.muted.Render(row.Help)
		}
		lines = append(lines, label)
	}
	lines = append(lines, renderAdminOperatorFooter(theme, env.Session, env.now())...)
	return screenFrame{
		Context: "Admin",
		Body:    renderSection(theme, strings.Join(lines, "\n"), env.Layout),
	}
}

var adminMenuKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up"), key.WithHelp("Up", "previous domain")),
	key.NewBinding(key.WithKeys("down"), key.WithHelp("Down", "next domain")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "open domain")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "back")),
	key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
}}
