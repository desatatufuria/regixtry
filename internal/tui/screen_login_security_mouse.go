package tui

import tea "charm.land/bubbletea/v2"

// The mouse on Login security only selects and focuses: a click puts the focus
// on the panel under it and selects the row it lands on, the wheel moves the
// selection of the panel under the pointer. It never opens the reset dialog and
// never confirms anything: x and Enter stay keys. A dialog, the filter input or
// a reset in flight blocks the mouse (the sidebar included).

func (s loginSecurityScreen) mouseBlocked() bool { return s.filtering || s.confirm.Active() || s.busy }

func (s loginSecurityScreen) Mouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	if !s.ready() {
		return s, nil
	}
	pos := msg.Mouse()
	panel, row, onRow := s.panelAt(env, pos.X, pos.Y)
	if panel < 0 {
		return s, nil
	}
	switch step := wheelStep(msg); step {
	case -1, 1:
		s.focus = panel
		if step < 0 {
			return pressKeys(s, env, mouseKeyUp)
		}
		return pressKeys(s, env, mouseKeyDown)
	}
	if _, _, click := leftClick(msg); !click {
		return s, nil
	}
	s.focus = panel
	if onRow {
		switch panel {
		case loginPanelWaiting:
			s.selWaiting = row
		case loginPanelFailures:
			s.selFail = row
		}
	}
	return s, nil
}

// panelAt is the panel drawn under the terminal cell (x, y), and when that cell
// is on one of its data rows, the index of the row (in the list the panel shows).
func (s loginSecurityScreen) panelAt(env screenEnv, x, y int) (panel loginPanel, row int, onRow bool) {
	l := s.layout(env)
	ox, oy := shellContentOrigin()
	inside := func(r loginRect) bool { return x >= ox+r.x && x < ox+r.x+r.w && y >= oy+r.y && y < oy+r.y+r.h }
	switch {
	case inside(l.waiting):
		rows := s.backoff.Accounts
		start, end := grantWindow(boundedIndex(s.selWaiting, len(rows)), len(rows), s.waitingDataRows(l))
		index := y - (oy + l.waiting.y + 2) + start // border and column header first
		return loginPanelWaiting, index, y >= oy+l.waiting.y+2 && index >= start && index < end
	case inside(l.failures):
		rows := s.visibleFailures()
		start, end := grantWindow(boundedIndex(s.selFail, len(rows)), len(rows), s.failureDataRows(l))
		first := oy + l.failures.y + 2 // border and column header
		index := y - first + start
		return loginPanelFailures, index, y >= first && index >= start && index < end
	}
	return -1, 0, false
}
