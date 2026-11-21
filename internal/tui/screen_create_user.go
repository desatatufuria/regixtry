package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

type createUserField int

const (
	createUserFieldUsername createUserField = iota
	createUserFieldPassword
	createUserFieldRole
	createUserFieldEnabled
	createUserFieldCount
)

// createUserRoles are the three role buttons of the board, in order. Admin and
// read-only are independent flags (as the old form had them) and "user" is
// what is left when neither is set.
var createUserRoles = []string{"admin", "read-only", "user"}

// createUserScreen is screenAdminCreateUser (the "New user" board): username,
// masked password, the role buttons and the enabled flag. Enter creates it.
type createUserScreen struct {
	gen uint64

	username string
	password string
	// roleCursor is the role button the arrows are on; admin and readOnly are
	// the flags the space bar toggles on admin and read-only.
	roleCursor int
	admin      bool
	readOnly   bool
	enabled    bool
	focus      createUserField

	// creating is set from the moment the create command is sent until its
	// result arrives: Enter is ignored meanwhile, so one press never makes two
	// users.
	creating bool
	err      string
}

func newCreateUserScreen() createUserScreen {
	return createUserScreen{gen: userGenerations.Add(1), roleCursor: len(createUserRoles) - 1, enabled: true}
}

func (s createUserScreen) ID() screen { return screenAdminCreateUser }

func (s createUserScreen) Init(screenEnv) tea.Cmd { return nil }

func (s createUserScreen) Keys() screenKeys {
	return screenKeys{short: []key.Binding{
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
		key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "role")),
		key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "toggle")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "create")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}}
}

// userCreatedMsg is the result of the create command. It never carries the
// password.
type userCreatedMsg struct {
	gen  uint64
	user ports.AdminUser
	err  error
}

func (m userCreatedMsg) screenGen() uint64 { return m.gen }

func (m userCreatedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

func createUserCmd(env screenEnv, gen uint64, input ports.AdminCreateUserInput) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return userCreatedMsg{gen: gen, err: errors.New("admin API is unavailable for this session")}
		}
		user, err := client.CreateUser(context.Background(), session, input)
		return userCreatedMsg{gen: gen, user: user, err: err}
	}
}

func (s createUserScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case userCreatedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		s.creating = false
		if typed.err != nil {
			s.err = typed.err.Error()
			return s, nil, false
		}
		// Done: leave the form (and the password in it) behind.
		s.password = ""
		return s, openUserScreen(openUserScreenMsg{
			to:         screenAdminUsers,
			userID:     typed.user.ID,
			notice:     fmt.Sprintf("User %q created.", typed.user.Username),
			noticeTone: userNoticeSuccess,
		}), false
	}
	return s, nil, false
}

func (s createUserScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		// Leave nothing typed behind: the returned value replaces the slot's.
		cancelled := newCreateUserScreen()
		return cancelled, openUserScreen(openUserScreenMsg{to: screenAdminUsers}), true
	case isTabKey(msg), isArrowDownKey(msg):
		s.focus = (s.focus + 1) % createUserFieldCount
	case isShiftTabKey(msg), isArrowUpKey(msg):
		s.focus = (s.focus + createUserFieldCount - 1) % createUserFieldCount
	case isEnterKey(msg):
		return s.submit(env)
	case isMoveLeftKey(msg) && s.focus == createUserFieldRole:
		s.roleCursor = boundedIndex(s.roleCursor-1, len(createUserRoles))
	case isMoveRightKey(msg) && s.focus == createUserFieldRole:
		s.roleCursor = boundedIndex(s.roleCursor+1, len(createUserRoles))
	case isBackspaceKey(msg):
		s = s.edit(func(value string) string { return dropLastRune(value) })
	case isSpaceKey(msg) && s.focus == createUserFieldRole:
		s = s.toggleRole()
	case isSpaceKey(msg) && s.focus == createUserFieldEnabled:
		s.enabled = !s.enabled
	case isTextKey(msg):
		typed := printableText(keyText(msg))
		s = s.edit(func(value string) string { return value + typed })
	}
	return s, nil, true
}

// toggleRole is Space on a role button: admin and read-only flip their own
// flag, "user" clears both.
func (s createUserScreen) toggleRole() createUserScreen {
	switch createUserRoles[s.roleCursor] {
	case "admin":
		s.admin = !s.admin
	case "read-only":
		s.readOnly = !s.readOnly
	default:
		s.admin, s.readOnly = false, false
	}
	return s
}

// roleLit reports whether role button i shows as chosen.
func (s createUserScreen) roleLit(i int) bool {
	switch createUserRoles[i] {
	case "admin":
		return s.admin
	case "read-only":
		return s.readOnly
	}
	return !s.admin && !s.readOnly
}

// edit changes the text of the focused field; the other fields take no text.
func (s createUserScreen) edit(change func(string) string) createUserScreen {
	switch s.focus {
	case createUserFieldUsername:
		s.username = change(s.username)
		s.err = ""
	case createUserFieldPassword:
		s.password = change(s.password)
		s.err = ""
	}
	return s
}

// submit checks the form on the client (the server stays the authority) and
// sends the create command.
func (s createUserScreen) submit(env screenEnv) (adminScreen, tea.Cmd, bool) {
	if s.creating {
		return s, nil, true
	}
	username := strings.TrimSpace(s.username)
	switch {
	case username == "":
		s.err, s.focus = "username is required", createUserFieldUsername
		return s, nil, true
	case s.password == "":
		s.err, s.focus = "password is required", createUserFieldPassword
		return s, nil, true
	}
	if reason := validateNewPassword(s.password); reason != "" {
		s.err, s.focus = reason, createUserFieldPassword
		return s, nil, true
	}
	s.creating, s.err = true, ""
	input := ports.AdminCreateUserInput{
		Username:   username,
		Password:   s.password,
		IsAdmin:    s.admin,
		IsReadOnly: s.readOnly,
		Enabled:    s.enabled,
	}
	return s, createUserCmd(env, s.gen, input), true
}
