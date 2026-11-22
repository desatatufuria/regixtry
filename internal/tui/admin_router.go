package tui

import tea "charm.land/bubbletea/v2"

// routeAdminKey sends a key to the active slot screen; the parent REPLACES its
// held sub-model value (m.adminScreens[slot] = next), it never writes into the
// sub-model's fields directly. The login form is the one screen that lives on
// Model itself rather than in a slot.
func routeAdminKey(m Model, msg keyMsg) (tea.Model, tea.Cmd) {
	if slot, ok := slotFor(m.screen); ok {
		s := m.adminScreens[slot]
		if s == nil {
			return m, nil
		}
		next, cmd, _ := s.Update(m.screenEnv(), msg)
		m.adminScreens[slot] = next
		return m, cmd
	}
	if m.screen == screenAdminLogin {
		return m.updateAdminLoginKey(msg)
	}
	return m, nil
}

// routeAdminMsg broadcasts a non-key tea.Msg to every occupied slot in
// adminScreens (design.md Decision H): unlike a key message, which goes only to
// the active screen, every other message goes to every migrated screen so
// none of them can miss a result it needs while the operator has navigated
// away — each screen's Update ignores what it does not own via its own type
// switch. Returns the updated screen set and the first non-nil cmd
// (at most one sub-model is mounted at a time, so there is nothing to batch).
func routeAdminMsg(env screenEnv, screens adminScreenSet, msg tea.Msg) (adminScreenSet, tea.Cmd) {
	var firstCmd tea.Cmd
	for slot, s := range screens {
		if s == nil {
			continue
		}
		next, cmd, _ := s.Update(env, msg)
		screens[slot] = next
		if firstCmd == nil {
			firstCmd = cmd
		}
	}
	return screens, firstCmd
}
