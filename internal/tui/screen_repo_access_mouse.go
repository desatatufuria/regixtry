package tui

import tea "charm.land/bubbletea/v2"

// mouseBlocked: a revoke dialog or the filter box is in front.
func (s repoAccessScreen) mouseBlocked() bool { return s.confirm.Active() || s.filtering }

// Mouse: the wheel moves the selection (reading the next page of grants like
// the arrow key does), a click selects a grant and a second click on the
// selected one opens its principal, like Enter. Revoking stays a key.
func (s repoAccessScreen) Mouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	rows := s.visibleRows()
	return listMouse(s, env, msg, mouseList{Rows: s.mouseRows(env, len(rows)), Selected: boundedIndex(s.cursor, len(rows)), Open: true})
}

func (s repoAccessScreen) mouseRows(env screenEnv, total int) mouseRows {
	if total == 0 {
		return mouseRows{}
	}
	bodyW, bodyH := shellContentSize(env.Layout.Width, env.Layout.Height)
	panelW := min(repoAccessPanelWidth, max(bodyW/3, 1))
	leftW := max(bodyW-panelW-2, 1)
	visible := max(bodyH-repoAccessTableChromeRows, 1)
	start, end := grantWindow(boundedIndex(s.cursor, total), total, visible)
	x, y := shellContentOrigin()
	return mouseRows{X: x, Y: y + repoAccessTableChromeRows - repoAccessTableBelowRows, Width: leftW, Count: end - start, First: start}
}
