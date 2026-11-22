package tui

import tea "charm.land/bubbletea/v2"

// The configuration screens and the Update channel have no list to scroll:
// the mouse reaches only the shell's sidebar, handled by the router. Their
// buttons stay keys (c, p, e, d, Enter save the setting), so Mouse does
// nothing here, and a modal, a dialog or a save in flight blocks the sidebar
// too.

func (s trivyConfigScreen) mouseBlocked() bool {
	return s.confirm.Active() || s.cfg.Active() || s.policyModal.Active()
}

func (s trivyConfigScreen) Mouse(_ screenEnv, _ tea.MouseMsg) (adminScreen, tea.Cmd) { return s, nil }

func (s gitleaksConfigScreen) mouseBlocked() bool { return s.confirm.Active() || s.cfg.Active() }

func (s gitleaksConfigScreen) Mouse(_ screenEnv, _ tea.MouseMsg) (adminScreen, tea.Cmd) {
	return s, nil
}

func (s signingConfigScreen) mouseBlocked() bool { return s.confirm.Active() || s.cfg.Active() }

func (s signingConfigScreen) Mouse(_ screenEnv, _ tea.MouseMsg) (adminScreen, tea.Cmd) { return s, nil }

func (s updateChannelScreen) mouseBlocked() bool { return s.saving }

func (s updateChannelScreen) Mouse(_ screenEnv, _ tea.MouseMsg) (adminScreen, tea.Cmd) { return s, nil }
