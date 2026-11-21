package tui

import (
	"context"
	"errors"
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

type resetPasswordField int

const (
	resetPasswordFieldNew resetPasswordField = iota
	resetPasswordFieldConfirm
	resetPasswordFieldCount
)

// userPasswordMismatch is the error under the confirmation field.
const userPasswordMismatch = "The two passwords do not match. Retype the confirmation."

// resetPasswordScreen is screenAdminChangePassword (the "Reset password"
// board): a new password typed twice, masked. The passwords live only in this
// value and in the one command that sends the first of them; they are never
// rendered in clear and never put in a status message.
type resetPasswordScreen struct {
	gen      uint64
	userID   string
	username string

	password string
	confirm  string
	focus    resetPasswordField

	// saving is set from the moment the command is sent until its result
	// arrives: Enter is ignored meanwhile.
	saving bool
	err    string
	// confirmErr is shown under the confirmation field only.
	confirmErr string
}

func newResetPasswordScreen(userID, username string) resetPasswordScreen {
	return resetPasswordScreen{gen: userGenerations.Add(1), userID: userID, username: username}
}

func (s resetPasswordScreen) ID() screen { return screenAdminChangePassword }

func (s resetPasswordScreen) Init(screenEnv) tea.Cmd { return nil }

func (s resetPasswordScreen) Keys() screenKeys {
	return screenKeys{short: []key.Binding{
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "set password")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}}
}

// userPasswordResetMsg is the result of the reset command. It never carries a
// password.
type userPasswordResetMsg struct {
	gen uint64
	err error
}

func (m userPasswordResetMsg) screenGen() uint64 { return m.gen }

func (m userPasswordResetMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

func resetPasswordCmd(env screenEnv, gen uint64, input ports.AdminResetPasswordInput) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return userPasswordResetMsg{gen: gen, err: errors.New("admin API is unavailable for this session")}
		}
		return userPasswordResetMsg{gen: gen, err: client.ResetPassword(context.Background(), session, input)}
	}
}

func (s resetPasswordScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case userPasswordResetMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		s.saving = false
		if typed.err != nil {
			s.err = typed.err.Error()
			return s, nil, false
		}
		s.password, s.confirm = "", ""
		return s, openUserScreen(openUserScreenMsg{
			to:         screenAdminEditUser,
			userID:     s.userID,
			username:   s.username,
			notice:     fmt.Sprintf("Password reset for %q.", s.username),
			noticeTone: userNoticeSuccess,
		}), false
	}
	return s, nil, false
}

func (s resetPasswordScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return s, openUserScreen(openUserScreenMsg{to: screenAdminEditUser, userID: s.userID, username: s.username}), true
	case isTabKey(msg), isShiftTabKey(msg), isArrowDownKey(msg), isArrowUpKey(msg):
		s.focus = (s.focus + 1) % resetPasswordFieldCount
	case isEnterKey(msg):
		return s.submit(env)
	case isBackspaceKey(msg):
		s = s.edit(dropLastRune)
	case isTextKey(msg):
		typed := printableText(keyText(msg))
		s = s.edit(func(value string) string { return value + typed })
	}
	return s, nil, true
}

func (s resetPasswordScreen) edit(change func(string) string) resetPasswordScreen {
	if s.focus == resetPasswordFieldNew {
		s.password = change(s.password)
	} else {
		s.confirm = change(s.confirm)
	}
	s.err, s.confirmErr = "", ""
	return s
}

func (s resetPasswordScreen) mismatch() bool { return s.confirm != "" && s.confirm != s.password }

// submit checks both fields on the client (the server stays the authority)
// and sends the reset command.
func (s resetPasswordScreen) submit(env screenEnv) (adminScreen, tea.Cmd, bool) {
	if s.saving {
		return s, nil, true
	}
	if reason := validateNewPassword(s.password); reason != "" {
		s.err, s.focus = reason, resetPasswordFieldNew
		return s, nil, true
	}
	if s.confirm != s.password {
		s.confirmErr, s.focus = userPasswordMismatch, resetPasswordFieldConfirm
		return s, nil, true
	}
	s.saving, s.err, s.confirmErr = true, "", ""
	input := ports.AdminResetPasswordInput{UserID: s.userID, NewPassword: s.password}
	return s, resetPasswordCmd(env, s.gen, input), true
}
