package tui

import tea "charm.land/bubbletea/v2"

// mouseBlocked: the two scan lists have no dialog or text input.
func (s scanRunsScreen) mouseBlocked() bool { return false }

// Mouse: the wheel moves the selection, a click selects a row and a second
// click on the selected row opens the repository's scan history, like Enter.
func (s scanRunsScreen) Mouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	total := s.rowCount()
	return listMouse(s, env, msg, mouseList{Rows: s.mouseRows(env, total), Selected: boundedIndex(s.cursor, total), Open: true})
}

func (s scanRunsScreen) mouseRows(env screenEnv, total int) mouseRows {
	if total == 0 {
		return mouseRows{}
	}
	_, bodyH, listW, _ := scanBoardSize(env)
	visible := s.visibleRows(s.footnoteLines(newAdminTheme(), listW), bodyH)
	start, end := grantWindow(boundedIndex(s.cursor, total), total, visible)
	x, y := shellContentOrigin()
	// The box's top border and its column header sit above the first row.
	return mouseRows{X: x, Y: y + scanTitleRows + scanTableChromeRows - 1, Width: listW, Count: end - start, First: start}
}
