package tui

import tea "charm.land/bubbletea/v2"

// The console browser's three screens take the wheel and clicks like the other
// lists. Enter on a row only opens the next level (tags, repositories, a
// manifest), so a second click may do the same; d, the delete, stays a key and
// a dialog in front blocks the mouse.

func (s consoleRepositoriesScreen) mouseBlocked() bool { return s.confirm.Active() }

func (s consoleRepositoriesScreen) Mouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	if s.filtered {
		return s.treeMouse(env, msg)
	}
	total := len(s.visible())
	rows := consoleMouseRows(env.Layout.Width, env.Layout.Height, s.selected, total)
	return listMouse(s, env, msg, mouseList{Rows: rows, Selected: boundedIndex(s.selected, total), Open: true})
}

func (s consoleProjectsScreen) mouseBlocked() bool { return s.confirm.Active() }

func (s consoleProjectsScreen) Mouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	total := len(s.projects)
	rows := consoleMouseRows(env.Layout.Width, env.Layout.Height, s.selected, total)
	return listMouse(s, env, msg, mouseList{Rows: rows, Selected: boundedIndex(s.selected, total), Open: true})
}

func (s consoleTagsScreen) mouseBlocked() bool { return s.confirm.Active() }

func (s consoleTagsScreen) Mouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	total := len(s.items)
	if s.err != "" || !s.loaded {
		total = 0 // the table shows its message instead of rows
	}
	rows := consoleMouseRows(env.Layout.Width, env.Layout.Height, s.selected, total)
	return listMouse(s, env, msg, mouseList{Rows: rows, Selected: boundedIndex(s.selected, total), Open: true})
}
