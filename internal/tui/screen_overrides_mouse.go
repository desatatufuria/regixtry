package tui

import tea "charm.land/bubbletea/v2"

// The override screens share overridesList: the wheel moves the cursor and a
// click selects a repository. Only Trivy's list opens something on a second
// click (the scan history, like Enter); the other two have no Enter, and the
// override editor is only ever opened with o.

func (s featureOverridesScreen) mouseBlocked() bool { return s.editor.Active() }

func (s featureOverridesScreen) Mouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	rows := s.mouseRows(newAdminTheme(), env, s.viewSpec())
	return listMouse(s, env, msg, mouseList{Rows: rows, Selected: boundedIndex(s.cursor, len(s.rows))})
}

func (s trivyReposScreen) mouseBlocked() bool { return s.editor.Active() }

func (s trivyReposScreen) Mouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	theme := newAdminTheme()
	rows := s.mouseRows(theme, env, s.viewSpec(theme))
	return listMouse(s, env, msg, mouseList{Rows: rows, Selected: boundedIndex(s.cursor, len(s.rows)), Open: true})
}
