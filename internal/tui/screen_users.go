package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// userGenerations hands out the unique id of every user-management screen
// instance (Users, Edit user, New user, Reset password). Each screen's async
// messages carry it and the screen drops a message with another id, so a late
// result from a screen the operator already left never lands on a new one.
var userGenerations atomic.Uint64

const (
	// usersExpiringWithin is the window of the EXPIRING column: tokens that
	// expire within 7 days, as the board's footnote says.
	usersExpiringWithin = 7 * lifetimeDay
	// usersExpiringLimit is the most rows the one expiring-tokens call may
	// return; a full page means some tokens may be missing from the counts.
	usersExpiringLimit = adminExpiringMaxLimit
)

// usersScreen is screenAdminUsers (tui-v2-redesign S3, the "Users" board): the
// searchable user table with a detail panel and the actions for the selected
// user. It owns its list; nothing is shared with other screens.
type usersScreen struct {
	gen uint64

	users   []ports.AdminUser
	loaded  bool
	loadErr string
	// refreshing is set from the moment a reload is requested (r, or after a
	// disable or enable) until its result arrives; r is ignored meanwhile.
	refreshing bool

	// expiring maps a user id to that user's tokens expiring within
	// usersExpiringWithin, from ONE call for all users.
	expiring       map[string][]ports.AdminExpiringToken
	expiringCapped bool
	expiringErr    string

	query     string
	searching bool
	// selectedID is the highlighted user. It is an id, not an index, so a
	// reload or a narrower filter keeps the cursor on the same person.
	selectedID string

	// confirm is the disable/enable prompt; its own in-flight guard keeps a
	// second Enter from sending the command twice.
	confirm confirmPrompt

	notice     string
	noticeTone userNoticeTone
}

func newUsersScreen(selectID string) usersScreen {
	return usersScreen{gen: userGenerations.Add(1), selectedID: selectID}
}

func (s usersScreen) withNotice(tone userNoticeTone, text string) usersScreen {
	s.notice, s.noticeTone = text, tone
	return s
}

func (s usersScreen) ID() screen { return screenAdminUsers }

func (s usersScreen) Init(env screenEnv) tea.Cmd { return usersLoadCmd(env, s.gen) }

func (s usersScreen) Keys() screenKeys {
	switch {
	case s.confirm.Active():
		return screenKeys{short: []key.Binding{
			key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y / enter", "confirm")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}}
	case s.searching:
		return screenKeys{short: []key.Binding{
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "keep filter")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "done")),
		}}
	}
	return usersKeys
}

var usersKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
	key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new user")),
	key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "grant access")),
	key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tokens")),
	key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "password")),
	key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "disable")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	quitKey(),
}}

// usersLoadedMsg carries the two reads behind the screen: the user list and
// the one expiring-tokens report.
type usersLoadedMsg struct {
	gen         uint64
	users       []ports.AdminUser
	usersErr    error
	expiring    []ports.AdminExpiringToken
	expiringErr error
}

func (m usersLoadedMsg) screenGen() uint64 { return m.gen }

func (m usersLoadedMsg) expiredReason() (string, bool) {
	return expiredReasonOf(m.usersErr, m.expiringErr)
}

// userEnabledMsg is the result of a disable or enable.
type userEnabledMsg struct {
	gen      uint64
	userID   string
	username string
	enabled  bool
	err      error
}

func (m userEnabledMsg) screenGen() uint64 { return m.gen }

func (m userEnabledMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

// userScreenMsg is any async result of a user-management screen: the Model
// routes it only to the screen instance whose generation it carries, and turns
// an expired session into the usual re-login.
type userScreenMsg interface {
	screenGen() uint64
	expiredReason() (string, bool)
}

func usersLoadCmd(env screenEnv, gen uint64) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			unavailable := errors.New("admin API is unavailable for this session")
			return usersLoadedMsg{gen: gen, usersErr: unavailable, expiringErr: unavailable}
		}
		users, usersErr := client.ListUsers(context.Background(), session)
		expiring, expiringErr := client.ListExpiringTokens(context.Background(), session, usersExpiringWithin, usersExpiringLimit)
		return usersLoadedMsg{gen: gen, users: users, usersErr: usersErr, expiring: expiring, expiringErr: expiringErr}
	}
}

// userSetEnabledCmd disables or enables one user.
func userSetEnabledCmd(env screenEnv, gen uint64, userID, username string, enable bool) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return userEnabledMsg{gen: gen, userID: userID, username: username, enabled: enable, err: errors.New("admin API is unavailable for this session")}
		}
		var err error
		if enable {
			_, err = client.EnableUser(context.Background(), session, userID)
		} else {
			_, err = client.DisableUser(context.Background(), session, userID)
		}
		return userEnabledMsg{gen: gen, userID: userID, username: username, enabled: enable, err: err}
	}
}

// userToggleConfirm builds the disable/enable confirmation shared by the Users
// list and the Edit user screen.
func userToggleConfirm(gen uint64, user ports.AdminUser) confirmPrompt {
	enable := !user.Enabled
	verb, title := "disable", "Disable user?"
	message := "The user cannot sign in and their tokens stop working."
	consequence := fmt.Sprintf("Grants are kept. Enable %s again to restore access.", user.Username)
	if enable {
		// Enabling only switches the account on (SetUserEnabled): grants and
		// tokens are left exactly as they are.
		verb, title = "enable", "Enable user?"
		message = "The user can sign in again."
		consequence = fmt.Sprintf("%s can sign in again. Their grants and tokens are left as they are.", user.Username)
	}
	userID, username := user.ID, user.Username
	return newConfirmPrompt(
		title,
		message,
		verb,
		fmt.Sprintf("Submitting %s for %s...", verb, username),
		func(env screenEnv) tea.Cmd { return userSetEnabledCmd(env, gen, userID, username, enable) },
	).withTarget(username, userSubtitle(user, true)).withConsequence(consequence)
}

// userOwnAccountReason is the notice shown when the operator tries to disable
// the account they are signed in with.
const userOwnAccountReason = "You cannot disable your own account"

// ownAccountGuard reports whether user is the signed-in operator and the
// action would disable it. Enabling is never blocked. The session carries only
// the operator's username, which is what the comparison uses.
func ownAccountGuard(env screenEnv, user ports.AdminUser) (bool, string) {
	if user.Enabled && env.Session.Username != "" && strings.EqualFold(env.Session.Username, user.Username) {
		return true, userOwnAccountReason
	}
	return false, ""
}

// userEnabledNotice is the line shown after a disable or enable.
func userEnabledNotice(msg userEnabledMsg) (userNoticeTone, string) {
	if msg.err != nil {
		return userNoticeError, msg.err.Error()
	}
	verb := "disabled"
	if msg.enabled {
		verb = "enabled"
	}
	return userNoticeSuccess, fmt.Sprintf("User %q %s.", msg.username, verb)
}

// openUserScreenMsg asks the Model to mount a FRESH user-management screen
// (state from an earlier visit, a typed password above all, is discarded) or
// the Tokens screen for the user.
type openUserScreenMsg struct {
	to       screen
	userID   string
	username string
	// notice is shown on the opened Users or Edit user screen.
	notice     string
	noticeTone userNoticeTone
}

func openUserScreen(msg openUserScreenMsg) tea.Cmd {
	return func() tea.Msg { return msg }
}

func (s usersScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case usersLoadedMsg:
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
		s.refreshing = true
		return s, usersLoadCmd(env, s.gen), false
	}
	return s, nil, false
}

func (s usersScreen) applyLoaded(msg usersLoadedMsg) usersScreen {
	s.loaded, s.refreshing = true, false
	s.loadErr, s.expiringErr = errText(msg.usersErr), errText(msg.expiringErr)
	if msg.usersErr == nil {
		s.users = msg.users
	}
	s.expiring, s.expiringCapped = nil, false
	if msg.expiringErr == nil {
		s.expiring = make(map[string][]ports.AdminExpiringToken)
		for _, token := range msg.expiring {
			s.expiring[token.UserID] = append(s.expiring[token.UserID], token)
		}
		s.expiringCapped = len(msg.expiring) >= usersExpiringLimit
	}
	return s
}

// visible is the users the search lets through, in list order.
func (s usersScreen) visible() []ports.AdminUser {
	query := strings.ToLower(strings.TrimSpace(s.query))
	if query == "" {
		return s.users
	}
	var matches []ports.AdminUser
	for _, user := range s.users {
		if strings.Contains(strings.ToLower(user.Username), query) {
			matches = append(matches, user)
		}
	}
	return matches
}

// selectedIndex is the cursor's row in visible: the selected user's row, or
// the first row when that user is not listed.
func (s usersScreen) selectedIndex(visible []ports.AdminUser) int {
	for i, user := range visible {
		if user.ID == s.selectedID {
			return i
		}
	}
	return 0
}

// current is the highlighted user.
func (s usersScreen) current() (ports.AdminUser, bool) {
	visible := s.visible()
	if len(visible) == 0 {
		return ports.AdminUser{}, false
	}
	return visible[s.selectedIndex(visible)], true
}

func (s usersScreen) move(delta int) usersScreen {
	visible := s.visible()
	if len(visible) == 0 {
		return s
	}
	s.selectedID = visible[boundedIndex(s.selectedIndex(visible)+delta, len(visible))].ID
	return s
}

func (s usersScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if s.confirm.Active() {
		next, cmd, consumed := s.confirm.update(env, msg)
		s.confirm = next
		return s, cmd, consumed
	}
	if s.searching {
		return s.updateSearchKey(msg)
	}

	user, hasUser := s.current()
	switch {
	case isEscKey(msg):
		// The Overview is the admin panel's root: Esc goes back up to it.
		return s, navigate(screenAdminOverview), true
	case isRuneKey(msg, '/'):
		s.searching = true
		s.notice = ""
	case isMoveUpKey(msg):
		s = s.move(-1)
	case isMoveDownKey(msg):
		s = s.move(1)
	case isRuneKey(msg, 'n'):
		return s, openUserScreen(openUserScreenMsg{to: screenAdminCreateUser}), true
	case isRuneKey(msg, 'r'):
		// Reload the list and the expiring report; the cursor stays on the
		// same user (it is kept by id). Ignored while a load is in flight.
		if !s.loaded || s.refreshing {
			return s, nil, true
		}
		s.refreshing, s.notice = true, ""
		return s, usersLoadCmd(env, s.gen), true
	case !hasUser:
		if isEnterKey(msg) || isRuneKey(msg, 'g', 't', 'p', 'd') {
			s = s.withNotice(userNoticeInfo, "Select a user first.")
		}
	case isEnterKey(msg):
		return s, openUserScreen(openUserScreenMsg{to: screenAdminEditUser, userID: user.ID, username: user.Username}), true
	case isRuneKey(msg, 'g'):
		return s, openGrantAccess(user.ID, screenAdminUsers), true
	case isRuneKey(msg, 't'):
		return s, openUserScreen(openUserScreenMsg{to: screenAdminEditUserTokens, userID: user.ID, username: user.Username}), true
	case isRuneKey(msg, 'p'):
		return s, openUserScreen(openUserScreenMsg{to: screenAdminChangePassword, userID: user.ID, username: user.Username}), true
	case isRuneKey(msg, 'd'):
		if blocked, reason := ownAccountGuard(env, user); blocked {
			return s.withNotice(userNoticeError, reason), nil, true
		}
		s.confirm, s.notice = userToggleConfirm(s.gen, user), ""
	}
	return s, nil, true
}

// updateSearchKey handles the search box: every letter is typed (j, k and q
// included), only the arrows move, Enter and Esc end the search.
func (s usersScreen) updateSearchKey(msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg), isEnterKey(msg):
		s.searching = false
	case isBackspaceKey(msg):
		s.query = dropLastRune(s.query)
	case isArrowUpKey(msg):
		s = s.move(-1)
	case isArrowDownKey(msg):
		s = s.move(1)
	case isTextKey(msg):
		s.query += printableText(keyText(msg))
	}
	return s, nil, true
}

// expiringFor is the highlighted user's tokens that expire within a week, and
// whether the report loaded at all.
func (s usersScreen) expiringFor(userID string) ([]ports.AdminExpiringToken, bool) {
	if s.expiring == nil {
		return nil, false
	}
	return s.expiring[userID], true
}

// attentionItems is the "Needs attention" list of one user, only from data the
// API returns: tokens about to expire and a disabled account.
func (s usersScreen) attentionItems(user ports.AdminUser, now time.Time) []attentionItem {
	var items []attentionItem
	tokens, _ := s.expiringFor(user.ID)
	for _, token := range tokens {
		name := adminFirstNonEmpty(token.Name, token.Accessor)
		// The expiring report only lists tokens that have not expired yet, so
		// there is no "has expired" row: nothing says which tokens did.
		text := fmt.Sprintf("Token %s expires in %s", name, remainingText(max(token.ExpiresAt.Sub(now), 0)))
		items = append(items, attentionItem{tone: attentionWarning, text: text, hint: "t tokens"})
	}
	if !user.Enabled {
		items = append(items, attentionItem{tone: attentionCritical, text: "User is disabled", hint: "d enable"})
	}
	return items
}
