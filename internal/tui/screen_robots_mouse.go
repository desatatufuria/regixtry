package tui

import tea "charm.land/bubbletea/v2"

// mouseBlocked: a dialog is in front, or a change is on its way.
func (s robotsScreen) mouseBlocked() bool { return s.confirm.Active() || s.busy }

// Mouse: the wheel moves the selection, a click selects a robot and a second
// click on the selected one opens its tokens, like Enter.
func (s robotsScreen) Mouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	return listMouse(s, env, msg, mouseList{Rows: s.mouseRows(env), Selected: boundedIndex(s.selected, len(s.robots)), Open: true})
}

func (s robotsScreen) mouseRows(env screenEnv) mouseRows {
	if !s.loaded || s.err != "" || len(s.robots) == 0 {
		return mouseRows{}
	}
	bodyW, bodyH := shellContentSize(env.Layout.Width, env.Layout.Height)
	leftW, _ := robotsWidths(bodyW)
	_, start, end, _ := s.listWindow(newAdminTheme(), leftW, bodyH)
	x, y := shellContentOrigin()
	return mouseRows{X: x, Y: y + robotsTableChromeRows, Width: leftW, Count: end - start, First: start}
}
