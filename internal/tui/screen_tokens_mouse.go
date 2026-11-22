package tui

import tea "charm.land/bubbletea/v2"

// mouseBlocked: a revoke dialog, the issue form or a secret on show is in
// front.
func (s tokensScreen) mouseBlocked() bool { return s.capturesInput() }

// Mouse: the wheel and a click move the selection over the token cards. Enter
// has no action on the list, and revoking (x) stays a key.
func (s tokensScreen) Mouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	total := len(s.tokens)
	return listMouse(s, env, msg, mouseList{Rows: s.mouseRows(env, total), Selected: boundedIndex(s.selected, total)})
}

func (s tokensScreen) mouseRows(env screenEnv, total int) mouseRows {
	if !s.loaded || s.err != "" || total == 0 {
		return mouseRows{}
	}
	bodyW, bodyH := shellContentSize(env.Layout.Width, env.Layout.Height)
	leftW, _ := tokensWidths(bodyW)
	start, end := grantWindow(boundedIndex(s.selected, total), total, tokensVisible(bodyH))
	x, y := shellContentOrigin()
	return mouseRows{X: x, Y: y + tokensHeadRows, Width: leftW, Count: end - start, Step: tokensCardRows + 1, Gap: 1, First: start}
}
