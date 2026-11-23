package tui

import tea "charm.land/bubbletea/v2"

// mouseBlocked: a dialog is in front, or a delete is on its way.
func (s adminProjectsScreen) mouseBlocked() bool { return s.capturesInput() || s.saving || s.deleting }

// Mouse: the wheel moves the selection and a click selects a project; a
// second click on the selected one opens its members, like Enter. Creating and
// deleting stay keys, and the mouse never confirms a dialog.
func (s adminProjectsScreen) Mouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	rows := s.sorted()
	return listMouse(s, env, msg, mouseList{Rows: s.mouseRows(env, len(rows)), Selected: s.index(rows), Open: true})
}

func (s adminProjectsScreen) mouseRows(env screenEnv, total int) mouseRows {
	if total == 0 {
		return mouseRows{}
	}
	bodyW, bodyH := shellContentSize(env.Layout.Width, env.Layout.Height)
	helpLines := len(grantWrap(projectsHelp, bodyW))
	visible := max(bodyH-projectsRowsAbove-projectsRowsBelow-helpLines, 1)
	start, end := grantWindow(s.index(s.sorted()), total, visible)
	x, y := shellContentOrigin()
	return mouseRows{X: x, Y: y + projectsRowsAbove, Width: bodyW, Count: end - start, First: start}
}
