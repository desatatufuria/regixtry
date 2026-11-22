package tui

import tea "charm.land/bubbletea/v2"

// mouseBlocked: a revoke dialog or the grant form is in front.
func (s delegateScreen) mouseBlocked() bool { return s.confirm.Active() || s.form.active }

// Mouse: the wheel and a click move the selection. Enter has no action on this
// list (the keys n, e and x act on the selected grant), so a second click does
// nothing.
func (s delegateScreen) Mouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	total := len(s.rows)
	return listMouse(s, env, msg, mouseList{Rows: s.mouseRows(env, total), Selected: boundedIndex(s.cursor, total)})
}

func (s delegateScreen) mouseRows(env screenEnv, total int) mouseRows {
	if total == 0 {
		return mouseRows{}
	}
	bodyW, bodyH := shellContentSize(env.Layout.Width, env.Layout.Height)
	panelW := min(delegatePanelWidth, max(bodyW/3, 1))
	leftW := max(bodyW-panelW-2, 1)
	visible := max(bodyH-delegateTableChromeRows, 1)
	start, end := grantWindow(boundedIndex(s.cursor, total), total, visible)
	x, y := shellContentOrigin()
	return mouseRows{X: x, Y: y + delegateTableChromeRows - delegateTableBelowRows, Width: leftW, Count: end - start, First: start}
}
