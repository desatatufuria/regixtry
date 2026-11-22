package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// trivyReposScreen is the Repositories tab of "Image scanning" (the approved
// "Image scanning" board): one row per repository with its latest scan counts
// and whether it overrides the Trivy settings, and the editor of the override
// of the selected repository. Enter opens the scan history of the selected
// repository; Esc there comes back here.
//
// The field name editor (inside overridesList) is read by Model
// (migratedScreenCapturesTextInput): do not rename it.
type trivyReposScreen struct {
	overridesList
}

func newTrivyReposScreen() trivyReposScreen {
	list := newOverridesList(trivyFeatureName)
	list.reading = true
	return trivyReposScreen{list}
}

func (s trivyReposScreen) ID() screen { return screenSecurityTrivyRepos }

func (s trivyReposScreen) Keys() screenKeys { return trivyReposKeys }

// Init reads the latest scan of each repository; the overrides are read once
// they arrive, so the list is built from both.
func (s trivyReposScreen) Init(env screenEnv) tea.Cmd {
	return loadOverrideSummariesCmd(env, s.gen, overridesScanLimit)
}

func (s trivyReposScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	if key, ok := msg.(keyMsg); ok {
		if s.editor.Active() {
			list, cmd := s.updateEditorKey(env, key)
			s.overridesList = list
			return s, cmd, true
		}
		return s.updateKey(env, key)
	}
	if owned, ok := msg.(securityMsg); ok && owned.securityGen() != s.gen {
		return s, nil, false
	}
	switch typed := msg.(type) {
	case overrideSummariesMsg:
		s.overridesList = s.applySummaries(env, typed)
		return s, s.loadCmd(env), false
	case overrideListMsg:
		list, cmd := s.applyOverrides(env, typed)
		s.overridesList = list
		return s, cmd, false
	}
	list, cmd, _ := s.applyEditorMsg(env, msg)
	s.overridesList = list
	return s, cmd, false
}

func (s trivyReposScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if isPasteKey(msg) {
		// Pasted text is never a command: its first character must not open
		// an editor, a history or reload the list.
		return s, nil, true
	}
	if list, handled := s.moveKey(msg); handled {
		s.overridesList = list
		return s, nil, true
	}
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminOverview), true
	case isTabKey(msg):
		return s, navigate(screenSecurityTrivy), true
	case isRuneKey(msg, 'o'):
		list, cmd := s.openEditor(env)
		s.overridesList = list
		return s, cmd, true
	case isEnterKey(msg):
		row, ok := s.selectedRow()
		if !ok {
			return s, nil, true
		}
		return s, openScanHistory(row.Repository, screenSecurityTrivyRepos, scanHistoryTabIndexVulnerabilities), true
	case isRuneKey(msg, 'r'):
		if s.reading {
			return s, nil, true
		}
		s.reading, s.note = true, ""
		return s, loadOverrideSummariesCmd(env, s.gen, overridesScanLimit), true
	}
	return s, nil, true
}

func (s trivyReposScreen) View(theme adminTheme, env screenEnv) screenFrame {
	other := lipgloss.NewStyle().Padding(0, 1).Foreground(theme.muted.GetForeground())
	return s.frame(theme, env, overridesView{
		title:    "Image scanning",
		subtitle: "Trivy, vulnerability scanner",
		crumb:    []string{featureNavGroup, "Image scanning", "Repositories"},
		nav:      shellNavImageScanning,
		tabs:     other.Render("Config") + " " + theme.selected.Render("Repositories"),
		foot:     "One row per repository, severity first. enter opens its scan history, o edits its override.",
		keys:     s.Keys(),
		history:  true,
	})
}

var trivyReposKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch tabs")),
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "override")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "scan history")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}}
