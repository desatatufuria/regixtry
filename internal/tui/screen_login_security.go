package tui

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

const (
	// loginFailuresPageSize is how many aggregated rows one read of the recent
	// failures asks for.
	loginFailuresPageSize = 50
	// loginFailuresWindow is how far back the recent failures reach: the
	// screen's empty state says "in the last 24 hours".
	loginFailuresWindow = 24 * time.Hour
)

// loginSecurityLoadSeq numbers every read of the screen, across instances.
var loginSecurityLoadSeq atomic.Uint64

// loginPanel names the two panels Tab moves between.
type loginPanel int

const (
	loginPanelWaiting loginPanel = iota
	loginPanelFailures
	loginPanelCount
)

// loginSecurityScreen is Admin > Security > Login security: the live backoff
// state, the accounts waiting out a delay and the recent aggregated failures.
type loginSecurityScreen struct {
	gen uint64

	loaded    bool
	err       string
	forbidden bool

	summary ports.AdminLoginSummary
	backoff ports.AdminLoginBackoff

	failures []ports.LoginFailureRecord
	// next is the cursor of the following page of failures, "" on the last.
	next string
	// from is the start of the window the first page was read with; every
	// further page uses it, so the cursor stays valid.
	from time.Time
	// appliedSeq is the read that produced failures: a page of an older read
	// is dropped.
	appliedSeq  uint64
	loadingMore bool
	moreErr     string
	filtering   bool
	filterDraft string
	filter      string
	focus       loginPanel
	selWaiting  int
	selFail     int
	floor       uint64
	notice      string
	noticeTone  grantTone

	// confirm is the reset confirmation; its in-flight guard keeps a second
	// Enter from sending the reset twice.
	confirm confirmPrompt
	// busy is set from the moment a confirmed reset is sent until its result
	// arrives, even if Esc closed the dialog meanwhile: no other change starts
	// while one is on its way.
	busy bool
}

func newLoginSecurityScreen() loginSecurityScreen {
	return loginSecurityScreen{gen: accessGenerations.Add(1)}
}

func (s loginSecurityScreen) ID() screen { return screenAdminLoginSecurity }

func (s loginSecurityScreen) generation() uint64 { return s.gen }

func (s loginSecurityScreen) Init(env screenEnv) tea.Cmd {
	return loginSecurityLoadCmd(env, s.gen, true, time.Time{})
}

// capturesInput reports an input or dialog in front: typed letters belong to it.
func (s loginSecurityScreen) capturesInput() bool { return s.filtering || s.confirm.Active() }

// ---- messages ----

// loginSecurityMsg marks the async results of this screen: the Model delivers
// them like the other Access results (generation first, expiry next).
type loginSecurityMsg interface {
	accessMsg
	loginSecurityMsg()
}

// loginSecurityLoadedMsg is one read of the screen. A full read also brings the
// first page of failures; a partial one (after a reset) only the summary and the
// backoff state.
type loginSecurityLoadedMsg struct {
	gen  uint64
	seq  uint64
	full bool
	from time.Time

	summary      ports.AdminLoginSummary
	backoff      ports.AdminLoginBackoff
	page         ports.AdminLoginFailurePage
	summaryErr   error
	backoffErr   error
	failuresErr  error
	failuresRead bool
}

// loginSecurityPageMsg is a further page of failures.
type loginSecurityPageMsg struct {
	gen    uint64
	base   uint64
	cursor string
	page   ports.AdminLoginFailurePage
	err    error
}

func (loginSecurityLoadedMsg) loginSecurityMsg()             {}
func (loginSecurityPageMsg) loginSecurityMsg()               {}
func (loginSecurityLoadedMsg) accessSlot() screenSlot        { return slotLoginSecurity }
func (loginSecurityPageMsg) accessSlot() screenSlot          { return slotLoginSecurity }
func (m loginSecurityLoadedMsg) accessGen() uint64           { return m.gen }
func (m loginSecurityPageMsg) accessGen() uint64             { return m.gen }
func (m loginSecurityPageMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }
func (m loginSecurityLoadedMsg) expiredReason() (string, bool) {
	return expiredReasonOf(m.summaryErr, m.backoffErr, m.failuresErr)
}

// ---- commands ----

// loginSecurityLoadCmd reads the summary, the backoff state and, for a full
// read, the first page of failures. from is the window start a full read keeps
// (zero means "now minus loginFailuresWindow").
func loginSecurityLoadCmd(env screenEnv, gen uint64, full bool, from time.Time) tea.Cmd {
	client, session := env.Client, env.Session
	seq := loginSecurityLoadSeq.Add(1)
	if from.IsZero() {
		from = env.now().Add(-loginFailuresWindow)
	}
	return func() tea.Msg {
		msg := loginSecurityLoadedMsg{gen: gen, seq: seq, full: full, from: from}
		if client == nil {
			msg.summaryErr = errAdminUnavailable
			return msg
		}
		ctx := context.Background()
		msg.summary, msg.summaryErr = client.GetLoginSummary(ctx, session)
		if msg.summaryErr == nil {
			msg.backoff, msg.backoffErr = client.GetLoginBackoff(ctx, session)
		}
		if full && msg.summaryErr == nil && msg.backoffErr == nil {
			msg.failuresRead = true
			msg.page, msg.failuresErr = client.ListLoginFailures(ctx, session, AdminLoginFailuresQuery{From: from, Limit: loginFailuresPageSize})
		}
		return msg
	}
}

func loginSecurityPageCmd(env screenEnv, gen, base uint64, from time.Time, cursor string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return loginSecurityPageMsg{gen: gen, base: base, cursor: cursor, err: errAdminUnavailable}
		}
		page, err := client.ListLoginFailures(context.Background(), session, AdminLoginFailuresQuery{From: from, Limit: loginFailuresPageSize, Cursor: cursor})
		return loginSecurityPageMsg{gen: gen, base: base, cursor: cursor, page: page, err: err}
	}
}

// ---- Update ----

func (s loginSecurityScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case loginSecurityLoadedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		return s.applyLoaded(typed), nil, false
	case loginSecurityPageMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		return s.applyPage(typed), nil, false
	case loginSecurityResetMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		next, cmd := s.applyReset(env, typed)
		return next, cmd, false
	}
	return s, nil, false
}

// loadErrorText is the reason of a failed read, without a closing period.
func loadErrorText(err error) string { return strings.TrimRight(adminErrorDetail(err), ".") }

func (s loginSecurityScreen) applyLoaded(msg loginSecurityLoadedMsg) loginSecurityScreen {
	if msg.seq < s.floor {
		return s
	}
	s.floor = msg.seq
	for _, err := range []error{msg.summaryErr, msg.backoffErr, msg.failuresErr} {
		if err == nil {
			continue
		}
		if IsAdminForbidden(err) {
			s.forbidden, s.loaded, s.err = true, true, ""
			return s
		}
		text := "Could not load login security: " + loadErrorText(err) + ". r retry"
		if s.loaded {
			// What is on screen stays; the failure is a line above it.
			s.notice, s.noticeTone = text, grantToneError
			return s
		}
		s.err = text
		return s
	}
	keep, hadSelection := s.selectedWaiting()
	s.loaded, s.err, s.forbidden = true, "", false
	s.summary, s.backoff = msg.summary, msg.backoff
	s.selWaiting = boundedIndex(s.selWaiting, len(s.backoff.Accounts))
	if hadSelection {
		for i, account := range s.backoff.Accounts {
			if account.Account == keep.Account {
				s.selWaiting = i
			}
		}
	}
	if msg.full {
		s.failures, s.next, s.from, s.appliedSeq = msg.page.Failures, msg.page.Next, msg.from, msg.seq
		s.loadingMore, s.moreErr = false, ""
		s.selFail = boundedIndex(s.selFail, len(s.visibleFailures()))
		s.notice, s.noticeTone = "", grantToneNone
	}
	return s
}

func (s loginSecurityScreen) applyPage(msg loginSecurityPageMsg) loginSecurityScreen {
	if msg.base != s.appliedSeq || msg.cursor != s.next || !s.loadingMore {
		// A page of an older read, or one already applied.
		return s
	}
	s.loadingMore = false
	if msg.err != nil {
		s.moreErr = "Could not load more failures: " + loadErrorText(msg.err) + ". n retry"
		return s
	}
	s.moreErr = ""
	s.failures = append(append([]ports.LoginFailureRecord(nil), s.failures...), msg.page.Failures...)
	s.next = msg.page.Next
	return s
}

// ---- selection helpers ----

func (s loginSecurityScreen) selectedWaiting() (ports.AdminBackoffAccount, bool) {
	if len(s.backoff.Accounts) == 0 {
		return ports.AdminBackoffAccount{}, false
	}
	return s.backoff.Accounts[boundedIndex(s.selWaiting, len(s.backoff.Accounts))], true
}

// visibleFailures is the loaded failures that match the applied filter: a
// case-insensitive substring of the account (name or chip), the IP or the
// reason, as the board words it.
func (s loginSecurityScreen) visibleFailures() []ports.LoginFailureRecord {
	needle := strings.ToLower(strings.TrimSpace(s.filter))
	if needle == "" {
		return s.failures
	}
	var rows []ports.LoginFailureRecord
	for _, row := range s.failures {
		for _, haystack := range []string{row.Account, loginAccountName(row.Account), row.IP, row.Reason, loginReasonLabel(row.Reason)} {
			if strings.Contains(strings.ToLower(haystack), needle) {
				rows = append(rows, row)
				break
			}
		}
	}
	return rows
}

// ---- keys ----

func (s loginSecurityScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if s.confirm.Active() {
		next, cmd, _ := s.confirm.update(env, msg)
		s.confirm = next
		s.busy = s.busy || cmd != nil
		return s, cmd, true
	}
	if s.filtering {
		return s.updateFilterKey(msg)
	}
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminOverview), true
	case s.forbidden || !s.loaded:
		// Nothing to act on yet; r retries a failed read.
		if s.err != "" && isCommandRune(msg, 'r') {
			s.err = ""
			return s, loginSecurityLoadCmd(env, s.gen, true, time.Time{}), true
		}
		return s, nil, true
	case isTabKey(msg):
		s.focus = (s.focus + 1) % loginPanelCount
	case isShiftTabKey(msg):
		s.focus = (s.focus + loginPanelCount - 1) % loginPanelCount
	case isMoveUpKey(msg):
		s = s.move(-1)
	case isMoveDownKey(msg):
		var cmd tea.Cmd
		s, cmd = s.moveDown(env, 1)
		return s, cmd, true
	case isPgUpKey(msg):
		s = s.move(-s.pageRows(env))
	case isPgDnKey(msg):
		var cmd tea.Cmd
		s, cmd = s.moveDown(env, s.pageRows(env))
		return s, cmd, true
	case s.busy && isCommandRune(msg, 'r', 'x'):
		// A reset is on its way: its result decides what the list shows.
	case isCommandRune(msg, 'x') && s.focus == loginPanelWaiting:
		if account, ok := s.selectedWaiting(); ok {
			s.confirm, s.notice, s.noticeTone = s.resetConfirm(account), "", grantToneNone
		}
	case isCommandRune(msg, 'r'):
		s.notice, s.noticeTone, s.moreErr = "", grantToneNone, ""
		return s, loginSecurityLoadCmd(env, s.gen, true, time.Time{}), true
	case isCommandRune(msg, 'n') && s.focus == loginPanelFailures:
		next, cmd := s.loadMore(env)
		return next, cmd, true
	case isCommandRune(msg, '/'):
		s.filtering, s.filterDraft, s.focus = true, s.filter, loginPanelFailures
	}
	return s, nil, true
}

// updateFilterKey edits the filter: enter applies it, esc clears it.
func (s loginSecurityScreen) updateFilterKey(msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		s.filtering, s.filterDraft, s.filter, s.selFail = false, "", "", 0
	case isEnterKey(msg):
		s.filtering, s.filter, s.selFail = false, strings.TrimSpace(s.filterDraft), 0
	case isBackspaceKey(msg):
		s.filterDraft = dropLastRune(s.filterDraft)
	case isTextKey(msg):
		s.filterDraft += printableText(keyText(msg))
	}
	return s, nil, true
}

func (s loginSecurityScreen) move(delta int) loginSecurityScreen {
	switch s.focus {
	case loginPanelWaiting:
		s.selWaiting = boundedIndex(s.selWaiting+delta, len(s.backoff.Accounts))
	case loginPanelFailures:
		s.selFail = boundedIndex(s.selFail+delta, len(s.visibleFailures()))
	}
	return s
}

// moveDown moves the cursor down; past the last loaded failure it asks for the
// next page.
func (s loginSecurityScreen) moveDown(env screenEnv, delta int) (loginSecurityScreen, tea.Cmd) {
	s = s.move(delta)
	if s.focus == loginPanelFailures && s.selFail >= len(s.visibleFailures())-1 {
		return s.loadMore(env)
	}
	return s, nil
}

// loadMore reads the next page of failures, once at a time.
func (s loginSecurityScreen) loadMore(env screenEnv) (loginSecurityScreen, tea.Cmd) {
	if s.next == "" || s.loadingMore {
		return s, nil
	}
	s.loadingMore, s.moreErr = true, ""
	return s, loginSecurityPageCmd(env, s.gen, s.appliedSeq, s.from, s.next)
}

// pageRows is how many rows a page key moves: the rows of the focused panel.
func (s loginSecurityScreen) pageRows(env screenEnv) int {
	l := s.layout(env)
	if s.focus == loginPanelWaiting {
		return max(l.waiting.h-3, 1)
	}
	return max(s.failureDataRows(l), 1)
}

// ---- footer ----

// Keys is the footer. A hint shows only while its key does something.
func (s loginSecurityScreen) Keys() screenKeys {
	esc := key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	switch {
	case s.confirm.Active():
		return screenKeys{short: []key.Binding{
			key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y / enter", "confirm")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}}
	case s.filtering:
		return screenKeys{short: []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear")),
		}}
	case s.forbidden || !s.loaded:
		keys := []key.Binding{}
		if s.err != "" {
			keys = append(keys, key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "retry")))
		}
		return screenKeys{short: append(keys, esc, quitKey())}
	}
	keys := []key.Binding{key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move"))}
	if s.focus == loginPanelFailures {
		keys = append(keys, key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("pgup/pgdn", "page")))
		if s.next != "" {
			keys = append(keys, key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "load more")))
		}
	}
	keys = append(keys, key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next panel")))
	if s.focus == loginPanelWaiting && len(s.backoff.Accounts) > 0 {
		// x acts on the selected waiting account only.
		keys = append(keys, key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "reset")))
	}
	keys = append(keys,
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		esc, quitKey())
	return screenKeys{short: keys}
}

// loginSecurityResetMsg is the result of resetting one account's backoff.
type loginSecurityResetMsg struct {
	gen uint64
	// account is the label that was sent (a name or "hash:xxxxxxxx") and
	// display how the dialog named it.
	account string
	display string
	err     error
}

func (loginSecurityResetMsg) loginSecurityMsg()               {}
func (loginSecurityResetMsg) accessSlot() screenSlot          { return slotLoginSecurity }
func (m loginSecurityResetMsg) accessGen() uint64             { return m.gen }
func (m loginSecurityResetMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

// ---- reset ----

// resetConfirm builds the confirmation for resetting one account. Everything it
// needs is captured NOW, from the row under the cursor: a reload or a reorder
// while the dialog is open can never change what Enter resets. The backend only
// clears the failure counter of that one account; it revokes no token.
func (s loginSecurityScreen) resetConfirm(account ports.AdminBackoffAccount) confirmPrompt {
	gen, label := s.gen, account.Account
	display := loginAccountName(label)
	counts := pluralUnit(int64(account.Failures), "failed login") + ", waiting " + loginDelayText(account.RemainingSeconds)
	body := "The login delay for this account ends now."
	detail := "user, " + counts
	consequence := "Clears the failure counter for " + display + ". " + display + " can try again immediately. Active access tokens are not affected."
	if loginIsHashLabel(label) {
		body = "The login delay for this name ends now."
		detail = "no such account, " + counts
		consequence = "Clears the failure counter for " + display + ". Anyone using that name can try again immediately. Active access tokens are not affected."
	}
	return newConfirmPrompt(
		"Reset backoff?", body, "reset", "Resetting backoff…",
		func(env screenEnv) tea.Cmd { return loginSecurityResetCmd(env, gen, label, display) },
	).withTarget(display, detail).withButtonLabel("Reset backoff").withConsequence(consequence)
}

func loginSecurityResetCmd(env screenEnv, gen uint64, account, display string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return loginSecurityResetMsg{gen: gen, account: account, display: display, err: errAdminUnavailable}
		}
		_, err := client.ResetLoginBackoff(context.Background(), session, account)
		return loginSecurityResetMsg{gen: gen, account: account, display: display, err: err}
	}
}

// applyReset closes the dialog and shows the outcome. After a reset, or when the
// account was already gone, the row is dropped at once and the waiting list and
// the summary are read again; the floor makes every read that started before
// this result stale, so none of them can bring the row back.
func (s loginSecurityScreen) applyReset(env screenEnv, msg loginSecurityResetMsg) (loginSecurityScreen, tea.Cmd) {
	s.confirm, s.busy = confirmPrompt{}, false
	refresh := func(s loginSecurityScreen) (loginSecurityScreen, tea.Cmd) {
		kept := make([]ports.AdminBackoffAccount, 0, len(s.backoff.Accounts))
		for _, account := range s.backoff.Accounts {
			if account.Account != msg.account {
				kept = append(kept, account)
			}
		}
		if len(kept) < len(s.backoff.Accounts) && s.summary.AccountsInBackoff > 0 {
			s.summary.AccountsInBackoff--
		}
		s.backoff.Accounts = kept
		s.selWaiting = boundedIndex(s.selWaiting, len(kept))
		s.floor = loginSecurityLoadSeq.Add(1)
		return s, loginSecurityLoadCmd(env, s.gen, false, time.Time{})
	}
	switch {
	case msg.err == nil:
		if loginIsHashLabel(msg.account) {
			s.notice = "Backoff reset for " + msg.display + ". Anyone using that name can try again now."
		} else {
			s.notice = "Backoff reset for " + msg.display + ". " + msg.display + " can try again now."
		}
		s.noticeTone = grantToneSuccess
		return refresh(s)
	case IsAdminNotFound(msg.err):
		s.notice, s.noticeTone = msg.display+" is no longer waiting. The list was refreshed.", grantToneWarning
		return refresh(s)
	}
	s.notice, s.noticeTone = "Could not reset backoff: "+loadErrorText(msg.err)+". The list stays as it was.", grantToneError
	return s, nil
}
