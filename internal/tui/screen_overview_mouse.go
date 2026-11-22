package tui

import tea "charm.land/bubbletea/v2"

// mouseBlocked: the Overview has no dialog or text input.
func (s overviewScreen) mouseBlocked() bool { return false }

// Mouse: the wheel moves the sidebar cursor like the arrows, a click on a
// NEEDS ATTENTION row focuses it (the Tab key) and a click on the focused row
// opens what it points at, like Enter. A click on a sidebar item is handled
// by the router: it opens that screen.
func (s overviewScreen) Mouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	if wheelStep(msg) != 0 {
		return listMouse(s, env, msg, mouseList{})
	}
	x, y, ok := leftClick(msg)
	if !ok {
		return s, nil
	}
	item, ok := s.attentionAt(env, x, y)
	if !ok {
		return s, nil
	}
	if s.attentionFocus == item+1 {
		return pressKeys(s, env, mouseKeyEnter)
	}
	// Tab cycles over the sidebar slot and every item: press it until the item
	// is focused.
	cycle := len(s.attentionItems(env.now())) + 1
	presses := ((item+1-s.attentionFocus)%cycle + cycle) % cycle
	keys := make([]keyMsg, presses)
	for i := range keys {
		keys[i] = keyMsg{Code: tea.KeyTab}
	}
	return pressKeys(s, env, keys...)
}

// attentionAt is the NEEDS ATTENTION item drawn under the cell (x, y).
func (s overviewScreen) attentionAt(env screenEnv, x, y int) (int, bool) {
	if !s.loaded {
		return 0, false
	}
	items := s.attentionItems(env.now())
	if len(items) == 0 {
		return 0, false
	}
	width, headRows, room := s.attentionLayout(newAdminTheme(), env)
	shown := attentionShown(items, room, s.attentionFocus-1)
	originX, originY := shellContentOrigin()
	rows := mouseRows{X: originX, Y: originY + headRows + attentionPanelChromeRows, Width: width, Count: len(shown)}
	position, ok := rows.rowAt(x, y)
	if !ok {
		return 0, false
	}
	return shown[position], true
}
