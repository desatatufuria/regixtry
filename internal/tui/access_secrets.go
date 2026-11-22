package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// A credential the backend created is shown once and cannot be read again, so
// a result that carries one is never dropped, whichever screen is current:
// routeCreatedCredential claims the secret state for it. A failed creation
// whose screen the operator already left is reported in one line instead of
// vanishing.

// shellFooterRows is the rule and the key-hint row under a shell screen.
const shellFooterRows = 2

// createdCredentialError is the error of a create result, with the sentence
// that names what failed. It never carries a secret.
func createdCredentialError(msg accessMsg) (string, error) {
	switch typed := msg.(type) {
	case robotCreatedMsg:
		return "Creating the robot failed: ", typed.err
	case tokenCreatedMsg:
		return "Issuing the token failed: ", typed.err
	}
	return "", nil
}

// createdCredentialScreen is the screen a create result belongs to.
func createdCredentialScreen(msg accessMsg) screen {
	if _, ok := msg.(robotCreatedMsg); ok {
		return screenAdminCreateRobot
	}
	return screenAdminEditUserTokens
}

func (m Model) routeCreatedCredential(msg accessMsg) (tea.Model, tea.Cmd) {
	prefix, err := createdCredentialError(msg)
	current, ok := m.adminScreens[msg.accessSlot()].(interface{ generation() uint64 })
	visible := ok && current.generation() == msg.accessGen() && m.screen == createdCredentialScreen(msg)
	if err != nil {
		if reason, expired := msg.expiredReason(); expired {
			return m.expireAdminSession(reason), nil
		}
		if visible {
			var cmd tea.Cmd
			m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
			return m, cmd
		}
		// The screen that asked is gone: say so, in one line.
		return m.withLateNotice(prefix + err.Error()), nil
	}
	if visible {
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	}
	// The screen that asked was left or replaced, but the credential exists
	// and cannot be read again: show it now, or right after the secret that is
	// on screen.
	if m.secretOnScreen() {
		m.queuedSecrets = append(m.queuedSecrets, msg)
		return m, nil
	}
	return m.showClaimed(msg)
}

// showClaimed brings the secret state of a created credential to the front. It
// first drops everything that could take the operator's next key: the scan
// history overlay, so the secret screen owns the keys.
func (m Model) showClaimed(msg accessMsg) (tea.Model, tea.Cmd) {
	m.adminScreens[slotScanHistory] = nil
	m.lateNotice = ""
	switch typed := msg.(type) {
	case robotCreatedMsg:
		m.adminScreens[slotCreateRobot] = newClaimedRobotScreen(typed)
		m.screen = screenAdminCreateRobot
		return m, nil
	case tokenCreatedMsg:
		next := newClaimedTokensScreen(typed)
		m.adminScreens[slotTokens] = next
		m.screen = screenAdminEditUserTokens
		return m, next.Init(m.screenEnv())
	}
	return m, nil
}

// secretOnScreen reports a one-time secret currently shown.
func (m Model) secretOnScreen() bool {
	switch s := m.adminScreens[slotCreateRobot].(type) {
	case createRobotScreen:
		if s.done && m.screen == screenAdminCreateRobot {
			return true
		}
	}
	if s, ok := m.adminScreens[slotTokens].(tokensScreen); ok && s.done && m.screen == screenAdminEditUserTokens {
		return true
	}
	return false
}

// handleKey is updateKey plus the two things a secret screen needs: a notice
// lasts one key, and when the operator is done with a secret that another
// created credential was waiting behind, the next one is shown at once.
func (m Model) handleKey(msg keyMsg) (tea.Model, tea.Cmd) {
	m.lateNotice = ""
	showing := m.secretOnScreen()
	next, cmd := m.updateKey(msg)
	if !showing {
		return next, cmd
	}
	updated, ok := next.(Model)
	if !ok || updated.secretOnScreen() || len(updated.queuedSecrets) == 0 {
		return next, cmd
	}
	queued := updated.queuedSecrets[0]
	updated.queuedSecrets = updated.queuedSecrets[1:]
	return updated.showClaimed(queued)
}

// withLateNotice reports a result that arrived after its screen was left: in
// the status line, and, on the shell screens that draw no status line, on the
// last row of the content area.
func (m Model) withLateNotice(text string) Model {
	m.status, m.lateNotice = text, text
	return m
}

// overlayLateNotice draws the late notice on a shell screen's content area.
func (m Model) overlayLateNotice(content string, layout consoleLayout) string {
	if m.lateNotice == "" {
		return content
	}
	slot, ok := slotFor(m.screen)
	if !ok || m.adminScreens[slot] == nil {
		return content
	}
	theme := newAdminTheme()
	env := screenEnv{Session: m.adminSession, Layout: layout, KnownRepositories: m.repositories.Names(), Now: m.now}
	if !m.adminScreens[slot].View(theme, env).Full {
		return content
	}
	lines := strings.Split(content, "\n")
	row := len(lines) - 1 - shellFooterRows
	if row < 1 {
		return content
	}
	room := max(layout.Width-shellSidebarWidth-shellContentPadX*2, 8)
	lines[row] = ansi.Truncate(lines[row], shellSidebarWidth, "") + strings.Repeat(" ", shellContentPadX) +
		theme.error.Render(grantTruncate(m.lateNotice, room))
	return strings.Join(lines, "\n")
}
