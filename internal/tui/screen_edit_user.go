package tui

import (
	"context"
	"errors"
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// editUserScreen is screenAdminEditUser (tui-v2-redesign S3, the "Edit user"
// board): one user's profile, password entry, a summary of their repository
// grants and tokens, and the danger area. It is addressed by user id, so it is
// reachable from the Users list and from the Tokens screen alike.
type editUserScreen struct {
	gen      uint64
	userID   string
	username string

	user      ports.AdminUser
	found     bool
	loaded    bool
	userErr   string
	grants    []ports.AdminRepoGrant
	grantsErr string
	tokens    []ports.AdminToken
	tokensErr string

	confirm confirmPrompt

	notice     string
	noticeTone userNoticeTone
}

func newEditUserScreen(userID, username string) editUserScreen {
	return editUserScreen{gen: userGenerations.Add(1), userID: userID, username: username}
}

func (s editUserScreen) withNotice(tone userNoticeTone, text string) editUserScreen {
	s.notice, s.noticeTone = text, tone
	return s
}

func (s editUserScreen) ID() screen { return screenAdminEditUser }

func (s editUserScreen) Init(env screenEnv) tea.Cmd { return editUserLoadCmd(env, s.gen, s.userID) }

func (s editUserScreen) Keys() screenKeys {
	if s.confirm.Active() {
		return screenKeys{short: []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}}
	}
	return screenKeys{short: []key.Binding{
		key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "enable/disable")),
		key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "password")),
		key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "grant access")),
		key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tokens")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "disable")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}}
}

// editUserLoadedMsg carries the reads behind the screen. The user comes from
// the user list (there is no read-one-user call).
type editUserLoadedMsg struct {
	gen       uint64
	userID    string
	users     []ports.AdminUser
	usersErr  error
	grants    []ports.AdminRepoGrant
	grantsErr error
	tokens    []ports.AdminToken
	tokensErr error
}

func (m editUserLoadedMsg) screenGen() uint64 { return m.gen }

func (m editUserLoadedMsg) expiredReason() (string, bool) {
	return expiredReasonOf(m.usersErr, m.grantsErr, m.tokensErr)
}

func editUserLoadCmd(env screenEnv, gen uint64, userID string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			unavailable := errors.New("admin API is unavailable for this session")
			return editUserLoadedMsg{gen: gen, userID: userID, usersErr: unavailable, grantsErr: unavailable, tokensErr: unavailable}
		}
		users, usersErr := client.ListUsers(context.Background(), session)
		grants, grantsErr := client.ListUserGrants(context.Background(), session, userID)
		tokens, tokensErr := client.ListUserAdminTokens(context.Background(), session, userID)
		return editUserLoadedMsg{gen: gen, userID: userID, users: users, usersErr: usersErr, grants: grants, grantsErr: grantsErr, tokens: tokens, tokensErr: tokensErr}
	}
}

func (s editUserScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case editUserLoadedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		return s.applyLoaded(typed), nil, false
	case userEnabledMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		s.confirm = confirmPrompt{}
		s.noticeTone, s.notice = userEnabledNotice(typed)
		if typed.err != nil {
			return s, nil, false
		}
		return s, editUserLoadCmd(env, s.gen, s.userID), false
	}
	return s, nil, false
}

func (s editUserScreen) applyLoaded(msg editUserLoadedMsg) editUserScreen {
	s.loaded = true
	s.userErr, s.grantsErr, s.tokensErr = errText(msg.usersErr), errText(msg.grantsErr), errText(msg.tokensErr)
	if msg.usersErr == nil {
		s.found = false
		for _, user := range msg.users {
			if user.ID == s.userID {
				s.user, s.found, s.username = user, true, user.Username
				break
			}
		}
	}
	if msg.grantsErr == nil {
		s.grants = msg.grants
	}
	if msg.tokensErr == nil {
		s.tokens = msg.tokens
	}
	return s
}

func (s editUserScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if s.confirm.Active() {
		next, cmd, consumed := s.confirm.update(env, msg)
		s.confirm = next
		return s, cmd, consumed
	}
	name := adminFirstNonEmpty(s.username, s.userID)
	switch {
	case isEscKey(msg):
		return s, openUserScreen(openUserScreenMsg{to: screenAdminUsers, userID: s.userID}), true
	case isRuneKey(msg, 'g'):
		return s, openGrantAccess(s.userID, screenAdminEditUser), true
	case isRuneKey(msg, 't'):
		return s, openUserScreen(openUserScreenMsg{to: screenAdminEditUserTokens, userID: s.userID, username: name}), true
	case isRuneKey(msg, 'p'):
		return s, openUserScreen(openUserScreenMsg{to: screenAdminChangePassword, userID: s.userID, username: name}), true
	case isRuneKey(msg, 'd'), isSpaceKey(msg):
		if !s.found {
			return s.withNotice(userNoticeInfo, "The user is not loaded yet."), nil, true
		}
		s.confirm, s.notice = userToggleConfirm(s.gen, s.user), ""
	}
	return s, nil, true
}

// grantsSummary words the grants count.
func grantsSummary(count int) string {
	if count == 0 {
		return "No repository grants."
	}
	if count == 1 {
		return "1 repository."
	}
	return fmt.Sprintf("%d repositories.", count)
}

// breadcrumb is the header trail: Users, then the user's name.
func (s editUserScreen) breadcrumb() []string {
	return shellCrumb("Access", "Users", adminFirstNonEmpty(s.username, s.userID))
}

// tokenLeftText is the right-hand text of a token row and its tone.
func tokenLeftText(token ports.AdminToken, env screenEnv) (string, attentionTone, bool) {
	now := env.now()
	switch {
	case token.RevokedAt != nil:
		return "revoked", attentionCritical, true
	case !token.ExpiresAt.After(now):
		return "expired", attentionCritical, true
	}
	left := token.ExpiresAt.Sub(now)
	return remainingText(left) + " left", attentionWarning, left <= 7*lifetimeDay
}

func (s editUserScreen) tokenRowName(token ports.AdminToken) string {
	return adminFirstNonEmpty(token.Name, token.Accessor, token.ID)
}
