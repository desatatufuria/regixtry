package tui

import (
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// mouseBlocked: a dialog or the search box is in front, so the mouse does
// nothing (the keys there are the only way to answer a dialog).
func (s usersScreen) mouseBlocked() bool { return s.confirm.Active() || s.searching }

// Mouse: the wheel moves the selection, a click selects a row and a second
// click on the selected row opens the profile, like Enter.
func (s usersScreen) Mouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	visible := s.visible()
	return listMouse(s, env, msg, mouseList{Rows: s.mouseRows(env, visible), Selected: s.selectedIndex(visible), Open: true})
}

// mouseRows is where the user rows were drawn, from the same arithmetic View
// uses.
func (s usersScreen) mouseRows(env screenEnv, visible []ports.AdminUser) mouseRows {
	if !s.loaded || s.loadErr != "" || len(visible) == 0 {
		return mouseRows{}
	}
	theme := newAdminTheme()
	bodyW, bodyH := shellContentSize(env.Layout.Width, env.Layout.Height)
	head := len(s.headLines(theme, bodyW))
	columnsHeight := max(bodyH-head, 3)
	detailW := min(usersDetailWidth, max(bodyW/3, 20))
	tableW := max(bodyW-detailW-usersColumnGap, 10)

	capacity := usersTableCapacity(s.tableHeight(theme, tableW, columnsHeight))
	top := usersTableTop(s.selectedIndex(visible), len(visible), capacity)
	x, y := shellContentOrigin()
	above := head + len(s.renderSearchLines(theme, tableW))
	if s.notice != "" {
		above++
	}
	return mouseRows{X: x, Y: y + above + usersTableChromeRows, Width: tableW, Count: min(capacity, len(visible)-top), First: top}
}
