package tui

import tea "github.com/charmbracelet/bubbletea"

// legacyHandler is a Go method expression on an existing, unmodified
// AdminViewState-backed key handler (design.md Decision C). legacyHandler
// itself holds no state — the adapter is the map below, not this type — so
// AdminViewState stays exactly where it is on Model and adding a screen
// here adds one line, never a new field.
type legacyHandler func(Model, tea.KeyMsg) (tea.Model, tea.Cmd)

// legacyScreenHandlers is THE D5 adapter for every admin screen Slice 1
// does not migrate to a sub-model. It holds no data of its own: every value
// here is a method expression on a handler that already existed before this
// change. The router resolves a screen id in exactly one place: slotFor
// first, then this table — TestEveryScreenRoutesExactlyOnce/
// TestLegacyAdapterHoldsNoState keep that boundary honest.
var legacyScreenHandlers = map[screen]legacyHandler{
	screenAdminLogin: Model.updateAdminLoginKey,
	screenAdminAuthenticating: func(m Model, _ tea.KeyMsg) (tea.Model, tea.Cmd) {
		return m, nil
	},
	screenAdminUsers:          Model.updateAdminUsersKey,
	screenAdminCreateUser:     Model.updateCreateUserFormKey,
	screenAdminEditUser:       Model.updateAdminEditUserKey,
	screenAdminChangePassword: Model.updateResetPasswordFormKey,
	screenAdminEditUserGrants: Model.updateAdminGrantsKey,
	screenAdminAddGrant:       Model.updateGrantFormKey,
	screenAdminEditUserTokens: Model.updateAdminTokensKey,
	screenAdminCreateToken:    Model.updateTokenFormKey,
	screenRepoAdminGrants:     Model.updateRepoAdminGrantsKey,
	screenRepoAdminAddGrant:   Model.updateRepoAdminAddGrantKey,
	screenAdminRobots:         Model.updateAdminRobotsKey,
	screenAdminCreateRobot:    Model.updateCreateRobotFormKey,
}

// routeAdminKey resolves m.screen through the migrated slot first, then
// legacyScreenHandlers (design.md Data Flow), preserving updateAdminKey's
// prior switch-on-m.screen dispatch exactly. Slice 2 populates the migrated
// arm: the parent REPLACES its held sub-model value (m.adminScreens[slot] =
// next), it never writes into the sub-model's fields directly (Decision B).
func routeAdminKey(m Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if slot, ok := slotFor(m.screen); ok {
		s := m.adminScreens[slot]
		if s == nil {
			return m, nil
		}
		next, cmd, _ := s.Update(m.screenEnv(), msg)
		m.adminScreens[slot] = next
		return m, cmd
	}
	if handler, ok := legacyScreenHandlers[m.screen]; ok {
		return handler(m, msg)
	}
	return m, nil
}

// routeAdminMsg broadcasts a non-key tea.Msg to every occupied slot in
// adminScreens (design.md Decision H): unlike tea.KeyMsg, which goes only to
// the active screen, every other message goes to every migrated screen so
// none of them can miss a result it needs while the operator has navigated
// away — each screen's Update ignores what it does not own via its own type
// switch. Returns the updated screen set and the first non-nil cmd
// (Slice 1 mounts at most one sub-model at a time, so batching is not yet
// needed; a future slice with concurrent occupied slots would batch these).
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

// renderRoutedFrame renders a migrated slot's screenFrame, or the zero value
// when the slot is unoccupied. Slice 1 has exactly one caller of this shape
// today (the gitleaks overlay check inside updateAdminKey/renderAdminWorkspace
// reads adminScreens[slotGitleaksConfig] directly, since it is an overlay
// rather than a slotFor-addressed top-level screen) — this helper exists for
// Slice 2's top-level migrated screens, which DO resolve through slotFor.
func renderRoutedFrame(theme adminTheme, env screenEnv, screens adminScreenSet, slot screenSlot) screenFrame {
	s := screens[slot]
	if s == nil {
		return screenFrame{}
	}
	return s.View(theme, env)
}
