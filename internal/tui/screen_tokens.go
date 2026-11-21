package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// tokensScreen is "Admin > Access > Users > alice > Tokens" (or Robots > ci >
// Tokens): the auth tokens of one principal, each with a bar of the time it
// has left, and the "Issue a new token" panel beside them (tui-v2-redesign
// S3/S5, the Tokens board). Users and robots are both users in the backend, so
// the same calls serve either.
type tokensScreen struct {
	gen uint64

	principalID   string
	principalName string
	// robot picks the breadcrumb (Robots instead of Users) and where Esc goes.
	robot bool

	tokens   []ports.AdminToken
	loaded   bool
	err      string
	selected int
	// loading is true while a read is in flight; r does not start another.
	loading bool

	// focus is the list until the operator starts issuing a token with n or
	// Tab; the panel's name and lifetime then take every key, letters included.
	focus    tokensFocus
	name     string
	lifetime tokenLifetime
	// creating is true from the Enter that issued the create command until its
	// result arrives; Enter is ignored meanwhile. Esc still leaves: the Model
	// then shows the secret anyway when the token was created after all.
	creating bool
	formErr  string

	done    bool
	created createdToken

	confirm    confirmPrompt
	notice     string
	noticeFail bool
}

type tokensFocus int

const (
	tokensFocusList tokensFocus = iota
	tokensFocusName
	tokensFocusLifetime
)

// createdToken is what the panel shows once the token exists.
type createdToken struct {
	name     string
	accessor string
	secret   string
	expires  time.Time
}

func newTokensScreen(principalID, principalName string, robot bool) tokensScreen {
	return tokensScreen{
		gen:           accessGenerations.Add(1),
		principalID:   principalID,
		principalName: principalName,
		robot:         robot,
	}
}

func (s tokensScreen) ID() screen { return screenAdminEditUserTokens }

func (s tokensScreen) generation() uint64 { return s.gen }

func (s tokensScreen) Keys() screenKeys {
	switch {
	case s.done:
		return tokensDoneKeys
	case s.focus != tokensFocusList:
		return tokensPanelKeys
	}
	return tokensListKeys
}

func (s tokensScreen) Init(env screenEnv) tea.Cmd { return tokensLoadCmd(env, s.gen, s.principalID) }

// reload starts a read of the tokens unless one is already in flight.
func (s tokensScreen) reload(env screenEnv) (tokensScreen, tea.Cmd) {
	if s.loading || !s.loaded {
		return s, nil
	}
	return s.startLoad(env)
}

// startLoad reads the tokens now, whatever is in flight.
func (s tokensScreen) startLoad(env screenEnv) (tokensScreen, tea.Cmd) {
	s.loading = true
	return s, tokensLoadCmd(env, s.gen, s.principalID)
}

// capturesInput reports a state in which letters are typed or swallowed, so
// the global q (quit) and l (logout) keys must not act.
func (s tokensScreen) capturesInput() bool {
	return s.confirm.Active() || s.focus != tokensFocusList || s.done
}

// ---- messages ----

// openTokensMsg asks the parent to mount a FRESH Tokens screen for one
// principal (nothing from an earlier visit, a secret above all, survives).
type openTokensMsg struct {
	principalID   string
	principalName string
	robot         bool
}

func openTokens(principalID, principalName string, robot bool) tea.Cmd {
	return func() tea.Msg {
		return openTokensMsg{principalID: principalID, principalName: principalName, robot: robot}
	}
}

type tokensLoadedMsg struct {
	gen    uint64
	tokens []ports.AdminToken
	err    error
}

type tokenCreatedMsg struct {
	gen uint64
	// principal is who the token was issued for, so a result that outlived its
	// screen can still rebuild the secret state.
	principalID, principalName string
	robot                      bool
	input                      ports.AdminCreateTokenInput
	created                    ports.AdminCreatedToken
	err                        error
}

type tokenRevokedMsg struct {
	gen      uint64
	accessor string
	err      error
}

func (m tokensLoadedMsg) accessSlot() screenSlot        { return slotTokens }
func (m tokensLoadedMsg) accessGen() uint64             { return m.gen }
func (m tokensLoadedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

func (m tokenCreatedMsg) accessSlot() screenSlot        { return slotTokens }
func (m tokenCreatedMsg) accessGen() uint64             { return m.gen }
func (m tokenCreatedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

func (m tokenRevokedMsg) accessSlot() screenSlot        { return slotTokens }
func (m tokenRevokedMsg) accessGen() uint64             { return m.gen }
func (m tokenRevokedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

func (s tokensScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case tokensLoadedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		s.loaded, s.loading, s.err = true, false, errText(typed.err)
		if typed.err == nil {
			var keep string
			if token, ok := s.selectedToken(); ok {
				keep = token.Accessor
			}
			s.tokens = orderTokens(typed.tokens, env.now())
			s.selected = boundedIndex(s.selected, len(s.tokens))
			if i := slices.IndexFunc(s.tokens, func(t ports.AdminToken) bool { return t.Accessor == keep }); i >= 0 {
				s.selected = i
			}
		}
		return s, nil, false
	case tokenCreatedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		s.creating = false
		if typed.err != nil {
			s.formErr = typed.err.Error()
			return s, nil, false
		}
		s.done, s.formErr = true, ""
		s.created = createdTokenFrom(typed)
		next, cmd := s.startLoad(env)
		return next, cmd, false
	case tokenRevokedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		s.confirm = confirmPrompt{}
		if typed.err != nil {
			s.notice, s.noticeFail = typed.err.Error(), true
			return s, nil, false
		}
		s.notice, s.noticeFail = "Token revoked.", false
		next, cmd := s.startLoad(env)
		return next, cmd, false
	}
	return s, nil, false
}

func (s tokensScreen) selectedToken() (ports.AdminToken, bool) {
	if len(s.tokens) == 0 {
		return ports.AdminToken{}, false
	}
	return s.tokens[boundedIndex(s.selected, len(s.tokens))], true
}

// orderTokens puts the tokens still working first, the soonest to end on top,
// then the expired ones, then the revoked ones.
func orderTokens(tokens []ports.AdminToken, now time.Time) []ports.AdminToken {
	ordered := slices.Clone(tokens)
	rank := func(t ports.AdminToken) int {
		switch {
		case t.RevokedAt != nil:
			return 2
		case !t.ExpiresAt.After(now):
			return 1
		}
		return 0
	}
	slices.SortStableFunc(ordered, func(a, b ports.AdminToken) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		return a.ExpiresAt.Compare(b.ExpiresAt)
	})
	return ordered
}

func (s tokensScreen) back() tea.Cmd {
	if s.robot {
		return openRobots(s.principalID)
	}
	return openUserScreen(openUserScreenMsg{to: screenAdminEditUser, userID: s.principalID, username: s.principalName})
}

func (s tokensScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case s.confirm.Active():
		next, cmd, _ := s.confirm.update(env, msg)
		s.confirm = next
		return s, cmd, true
	case s.done:
		// Enter and Esc both mean Done: the secret is dropped, the list stays.
		if isEnterKey(msg) || isEscKey(msg) {
			s.done, s.created, s.focus = false, createdToken{}, tokensFocusList
			s.name, s.lifetime = "", tokenLifetime{}
			next, cmd := s.startLoad(env)
			return next, cmd, true
		}
		return s, nil, true
	case s.creating:
		if isEscKey(msg) {
			return s, s.back(), true
		}
		return s, nil, true
	case s.focus != tokensFocusList:
		return s.panelKey(env, msg)
	}
	return s.listKey(env, msg)
}

func (s tokensScreen) listKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return s, s.back(), true
	case isMoveUpKey(msg):
		s.selected = boundedIndex(s.selected-1, len(s.tokens))
	case isMoveDownKey(msg):
		s.selected = boundedIndex(s.selected+1, len(s.tokens))
	case isCommandRune(msg, 'r'):
		next, cmd := s.reload(env)
		return next, cmd, true
	case isCommandRune(msg, 'g'):
		return s, openGrantAccess(s.principalID, screenAdminEditUserTokens), true
	case isCommandRune(msg, 'n') || isTabKey(msg):
		s.focus, s.formErr, s.notice = tokensFocusName, "", ""
	case isCommandRune(msg, 'x'):
		token, ok := s.selectedToken()
		switch {
		case !ok:
			s.notice, s.noticeFail = "No token selected to revoke.", true
		case token.RevokedAt != nil:
			s.notice, s.noticeFail = "That token is already revoked.", true
		default:
			s.confirm = s.revokeConfirm(token)
			s.notice = ""
		}
	}
	return s, nil, true
}

func (s tokensScreen) revokeConfirm(token ports.AdminToken) confirmPrompt {
	gen, userID, accessor := s.gen, s.principalID, token.Accessor
	return newConfirmPrompt(
		"Confirm Token Revocation",
		fmt.Sprintf("Revoke token %q (%s) for %q?", tokenDisplayName(token), shortAccessor(accessor), s.principalName),
		"revoke",
		fmt.Sprintf("Revoking token %s for %s...", shortAccessor(accessor), s.principalName),
		func(env screenEnv) tea.Cmd { return tokenRevokeCmd(env, gen, userID, accessor) },
	).withTarget(tokenDisplayName(token), s.principalName).
		withConsequence("Anything using this token stops working immediately.")
}

func tokenDisplayName(token ports.AdminToken) string {
	if token.Name != "" {
		return token.Name
	}
	return "unnamed"
}

func (s tokensScreen) panelKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		// Cancel: back to the list with a clean form.
		s.focus, s.formErr = tokensFocusList, ""
		s.name, s.lifetime = "", tokenLifetime{}
		return s, nil, true
	case isTabKey(msg) || isShiftTabKey(msg):
		if s.focus == tokensFocusName {
			s.focus = tokensFocusLifetime
		} else {
			s.focus = tokensFocusName
		}
		return s, nil, true
	case isEnterKey(msg):
		ttl, ok := s.lifetime.duration(lifetimeCeiling())
		if !ok {
			// The panel stays as it is; the validation message replaces the preview.
			return s, nil, true
		}
		s.creating, s.formErr = true, ""
		input := ports.AdminCreateTokenInput{UserID: s.principalID, Name: strings.TrimSpace(s.name), TTL: ttl}
		return s, tokenCreateCmd(env, s, input), true
	}
	switch s.focus {
	case tokensFocusName:
		s.name = typeInto(s.name, msg, true)
	case tokensFocusLifetime:
		s.lifetime, _ = s.lifetime.handleKey(msg, lifetimeCeiling())
	}
	return s, nil, true
}

// ---- commands ----

func tokensLoadCmd(env screenEnv, gen uint64, principalID string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return tokensLoadedMsg{gen: gen, err: errAdminUnavailable}
		}
		tokens, err := client.ListUserAdminTokens(context.Background(), session, principalID)
		return tokensLoadedMsg{gen: gen, tokens: tokens, err: err}
	}
}

func tokenCreateCmd(env screenEnv, s tokensScreen, input ports.AdminCreateTokenInput) tea.Cmd {
	gen := s.gen
	who := tokenCreatedMsg{gen: gen, principalID: s.principalID, principalName: s.principalName, robot: s.robot, input: input}
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			who.err = errAdminUnavailable
			return who
		}
		who.created, who.err = client.CreateUserAdminToken(context.Background(), session, input)
		return who
	}
}

func tokenRevokeCmd(env screenEnv, gen uint64, userID, accessor string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return tokenRevokedMsg{gen: gen, accessor: accessor, err: errAdminUnavailable}
		}
		err := client.RevokeUserAdminToken(context.Background(), session, userID, accessor)
		return tokenRevokedMsg{gen: gen, accessor: accessor, err: err}
	}
}

var tokensListKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new token")),
	key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "revoke")),
	key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "grant access")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
}}

var tokensPanelKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
	key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "lifetime")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "create token")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}}

var tokensDoneKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("enter", "esc"), key.WithHelp("enter", "done")),
}}

// createdTokenFrom is what the secret state shows for a created token.
func createdTokenFrom(msg tokenCreatedMsg) createdToken {
	return createdToken{
		name:     firstText(msg.created.Token.Name, msg.input.Name),
		accessor: firstText(msg.created.Accessor, msg.created.Token.Accessor),
		secret:   msg.created.Secret,
		expires:  msg.created.ExpiresAt,
	}
}

// newClaimedTokensScreen is the secret state of a token created by a screen
// that is gone: the credential cannot be displayed again.
func newClaimedTokensScreen(msg tokenCreatedMsg) tokensScreen {
	s := newTokensScreen(msg.principalID, msg.principalName, msg.robot)
	s.done = true
	s.created = createdTokenFrom(msg)
	return s
}
