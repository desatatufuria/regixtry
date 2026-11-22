package tui

import tea "charm.land/bubbletea/v2"

// mouseBlocked: the history is a full shell screen with no dialog.
func (s scanHistoryScreen) mouseBlocked() bool { return false }

// findingCount is the rows of the active tab's list.
func (s scanHistoryScreen) findingCount() int {
	if s.activeKind() == adminScanHistoryTabLeaks {
		return len(s.modal.Secrets)
	}
	return len(s.modal.Detail.Findings)
}

// Mouse: the wheel and a click move the finding cursor. A click on the
// selected finding does nothing: Enter would open a browser, which a mouse
// click never does.
func (s scanHistoryScreen) Mouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	total := s.findingCount()
	return listMouse(s, env, msg, mouseList{Rows: s.mouseRows(env, total), Selected: boundedIndex(s.modal.FindingCursor, total)})
}

func (s scanHistoryScreen) mouseRows(env screenEnv, total int) mouseRows {
	if total == 0 {
		return mouseRows{}
	}
	_, bodyH, listW, _ := scanBoardSize(env)
	start, end := grantWindow(boundedIndex(s.modal.FindingCursor, total), total, scanHistoryVisibleRows(bodyH))
	x, y := shellContentOrigin()
	return mouseRows{X: x, Y: y + scanHistoryHeadRows + scanTableChromeRows - 1, Width: listW, Count: end - start, First: start}
}
