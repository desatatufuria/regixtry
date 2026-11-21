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
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// repoAccessScreen is "Repository access" (tui-v2-redesign, the "Repository
// access" board): every grant, grouped by repository, for a global admin.
//
// The grants come from ONE endpoint, GET /admin/v1/repository-grants, read a
// page (200 grants) at a time by following the opaque cursor it returns. The
// first page is read on open; the next one only when the cursor nears the end
// of what is loaded or a filter has too few matches, until the cursor is
// empty. It also lists grants on repositories that were never pushed.
type repoAccessScreen struct {
	// gen identifies this instance: every async message carries it and a
	// message with another gen is dropped, so a late result can never land on
	// a newer screen that reuses the slot.
	gen uint64

	rows []repoAccessRow
	// next is the cursor of the page to read next ("" for the first one). It
	// only advances when a page arrives, so after a failure it is the cursor
	// to retry from.
	next string
	// done is set once a page came back with no next cursor.
	done    bool
	loading bool
	// loadErr is the text of the last failed page read, "" when none.
	loadErr string

	filter    string
	filtering bool
	cursor    int

	confirm  confirmPrompt
	revoking bool

	status     string
	statusTone grantTone
}

// repoAccessRow is one grant: a principal's role on a repository.
type repoAccessRow struct {
	repo     string
	userID   string
	username string
	isRobot  bool
	role     domainauth.RepoRole
	since    time.Time
}

// principal is the user or robot behind the grant.
func (r repoAccessRow) principal() grantPrincipal {
	kind := grantKindUser
	if r.isRobot {
		kind = grantKindRobot
	}
	return grantPrincipal{ID: r.userID, Name: r.username, Kind: kind}
}

// repoAccessGenerations hands out the unique id of each repoAccessScreen.
var repoAccessGenerations atomic.Uint64

// repoAccessDefaultPage is the page size assumed before the terminal size is
// known.
const repoAccessDefaultPage = 20

// repoAccessPageLimit is how many grants one read asks for: the most the
// endpoint serves.
const repoAccessPageLimit = 200

// newRepoAccessScreen starts loading: Init issues the first read.
func newRepoAccessScreen() repoAccessScreen {
	return repoAccessScreen{gen: repoAccessGenerations.Add(1), loading: true}
}

func (s repoAccessScreen) ID() screen { return screenAdminRepositoryAccess }

func (s repoAccessScreen) Keys() screenKeys {
	switch {
	case s.filtering:
		return repoAccessFilterKeys
	case s.loadErr != "":
		return repoAccessRetryKeys
	}
	return repoAccessKeys
}

var repoAccessKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
	key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "grant access")),
	key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "revoke")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}}

// repoAccessRetryKeys adds "r retry" while a page read has failed.
var repoAccessRetryKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
	key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "retry")),
	key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "grant access")),
	key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "revoke")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}}

var repoAccessFilterKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "done")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "done")),
}}

// ---- messages ----

// repoAccessGrantsLoadedMsg is the result of reading the page that starts at
// the cursor after.
type repoAccessGrantsLoadedMsg struct {
	gen   uint64
	after string
	page  ports.AdminGrantPage
	err   error
}

type repoAccessRevokedMsg struct {
	gen      uint64
	repo     string
	username string
	err      error
}

// repoAccessOpenPrincipalMsg asks the parent to open the user or robot behind
// a grant: the Model owns the screens that show them.
type repoAccessOpenPrincipalMsg struct {
	principal grantPrincipal
}

func (m repoAccessGrantsLoadedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

func (m repoAccessRevokedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

// repoAccessMsgGen returns the screen generation a repository-access async
// message belongs to.
func repoAccessMsgGen(msg tea.Msg) (uint64, bool) {
	switch typed := msg.(type) {
	case repoAccessGrantsLoadedMsg:
		return typed.gen, true
	case repoAccessRevokedMsg:
		return typed.gen, true
	}
	return 0, false
}

// openRepoAccess asks the parent to mount a FRESH repository-access screen.
func openRepoAccess() tea.Cmd { return navigate(screenAdminRepositoryAccess) }

// ---- Init and Update ----

func (s repoAccessScreen) Init(env screenEnv) tea.Cmd {
	return repoAccessLoadPageCmd(env, s.gen, "")
}

func (s repoAccessScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	if gen, ok := repoAccessMsgGen(msg); ok && gen != s.gen {
		return s, nil, false
	}
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case repoAccessGrantsLoadedMsg:
		s.loading = false
		if typed.err != nil {
			s.loadErr = typed.err.Error()
			return s, nil, false
		}
		s.loadErr = ""
		for _, grant := range typed.page.Grants {
			s.rows = append(s.rows, repoAccessRow{
				repo: grant.Repository, userID: grant.UserID, username: grant.Username,
				isRobot: grant.IsRobot, role: grant.Role, since: grant.CreatedAt,
			})
		}
		s.next, s.done = typed.page.Next, typed.page.Next == ""
		next, cmd := s.fill(env)
		return next, cmd, false
	case repoAccessRevokedMsg:
		return s.revoked(env, typed), nil, false
	}
	return s, nil, false
}

// fill reads the next page when the matching rows do not yet cover the cursor
// plus one screen of rows and there is a page left. A failed read is not
// retried on its own: the operator presses r.
func (s repoAccessScreen) fill(env screenEnv) (repoAccessScreen, tea.Cmd) {
	if s.loading || s.done || s.loadErr != "" {
		return s, nil
	}
	if len(s.visibleRows()) >= s.cursor+repoAccessPageRows(env.Layout.Height) {
		return s, nil
	}
	s.loading = true
	return s, repoAccessLoadPageCmd(env, s.gen, s.next)
}

// retry reads again from the cursor that failed.
func (s repoAccessScreen) retry(env screenEnv) (repoAccessScreen, tea.Cmd) {
	if s.loadErr == "" || s.loading {
		return s, nil
	}
	s.loadErr = ""
	s.loading = true
	return s, repoAccessLoadPageCmd(env, s.gen, s.next)
}

// repoAccessLoadPageCmd reads the page that starts at the cursor after.
func repoAccessLoadPageCmd(env screenEnv, gen uint64, after string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return repoAccessGrantsLoadedMsg{gen: gen, after: after, err: errors.New("admin API is unavailable for this session")}
		}
		page, err := client.ListAllRepositoryGrants(context.Background(), session, repoAccessPageLimit, after)
		return repoAccessGrantsLoadedMsg{gen: gen, after: after, page: page, err: err}
	}
}

func repoAccessRevokeCmd(env screenEnv, gen uint64, repo, username string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return repoAccessRevokedMsg{gen: gen, repo: repo, username: username, err: errors.New("admin API is unavailable for this session")}
		}
		err := client.DeleteRepositoryGrant(context.Background(), session, repo, username)
		return repoAccessRevokedMsg{gen: gen, repo: repo, username: username, err: err}
	}
}

// ---- keys ----

func (s repoAccessScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if s.confirm.Active() {
		// The prompt ignores a second Enter while the delete is in flight.
		confirm, cmd, consumed := s.confirm.update(env, msg)
		s.confirm = confirm
		s.revoking = s.revoking || confirm.inFlight
		return s, cmd, consumed
	}
	if s.filtering {
		return s.updateFilterKey(env, msg)
	}
	rows := s.visibleRows()
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminOverview), true
	case isArrowUpKey(msg):
		s.cursor = boundedIndex(s.cursor-1, len(rows))
	case isArrowDownKey(msg):
		s.cursor = boundedIndex(s.cursor+1, len(rows))
		next, cmd := s.fill(env)
		return next, cmd, true
	case isRuneKey(msg, '/'):
		s.filtering = true
	case isRuneKey(msg, 'r'):
		return s.retryKey(env)
	case isRuneKey(msg, 'g'):
		return s, openGrantAccess("", screenAdminRepositoryAccess), true
	case isEnterKey(msg):
		return s.openPrincipal(rows)
	case isRuneKey(msg, 'x'):
		return s.askRevoke(env, rows)
	}
	return s, nil, true
}

func (s repoAccessScreen) retryKey(env screenEnv) (adminScreen, tea.Cmd, bool) {
	next, cmd := s.retry(env)
	return next, cmd, true
}

func (s repoAccessScreen) updateFilterKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg), isEnterKey(msg):
		s.filtering = false
		return s, nil, true
	case isArrowUpKey(msg):
		s.cursor = boundedIndex(s.cursor-1, len(s.visibleRows()))
		return s, nil, true
	case isArrowDownKey(msg):
		s.cursor = boundedIndex(s.cursor+1, len(s.visibleRows()))
		next, cmd := s.fill(env)
		return next, cmd, true
	case isBackspaceKey(msg):
		return s.withFilter(env, dropLastRune(s.filter))
	case isTextKey(msg):
		if text := grantSanitizeFilterText(keyText(msg)); text != "" {
			return s.withFilter(env, s.filter+text)
		}
	}
	return s, nil, true
}

func (s repoAccessScreen) withFilter(env screenEnv, filter string) (adminScreen, tea.Cmd, bool) {
	s.filter = filter
	s.cursor = 0
	next, cmd := s.fill(env)
	return next, cmd, true
}

func (s repoAccessScreen) openPrincipal(rows []repoAccessRow) (adminScreen, tea.Cmd, bool) {
	row, ok := s.selected(rows)
	if !ok {
		return s, nil, true
	}
	principal := row.principal()
	return s, func() tea.Msg { return repoAccessOpenPrincipalMsg{principal: principal} }, true
}

func (s repoAccessScreen) askRevoke(env screenEnv, rows []repoAccessRow) (adminScreen, tea.Cmd, bool) {
	row, ok := s.selected(rows)
	if !ok {
		return s, nil, true
	}
	if s.revoking {
		s.status, s.statusTone = "Revoking, please wait.", grantToneInfo
		return s, nil, true
	}
	gen, repo, username := s.gen, row.repo, row.username
	s.status, s.statusTone = "", grantToneNone
	s.confirm = newConfirmPrompt(
		"Revoke access",
		fmt.Sprintf("Revoke %s's %s access to %s?", username, grantRoleLabel(row.role), repo),
		"revoke",
		fmt.Sprintf("Revoking %s on %s...", username, repo),
		func(env screenEnv) tea.Cmd { return repoAccessRevokeCmd(env, gen, repo, username) },
	).withTarget(fmt.Sprintf("%s on %s", username, repo), grantRoleLabel(row.role)).
		withConsequence(fmt.Sprintf("%s loses %s access to %s.", username, grantRoleLabel(row.role), repo))
	return s, nil, true
}

// revoked applies a delete result: the row leaves the list on success; either
// way the prompt closes and the outcome is on the status line.
func (s repoAccessScreen) revoked(env screenEnv, msg repoAccessRevokedMsg) repoAccessScreen {
	s.revoking = false
	s.confirm = confirmPrompt{}
	if msg.err != nil {
		s.status, s.statusTone = fmt.Sprintf("Could not revoke %s on %s: %s", msg.username, msg.repo, msg.err.Error()), grantToneError
		return s
	}
	kept := make([]repoAccessRow, 0, len(s.rows))
	for _, row := range s.rows {
		if row.repo != msg.repo || row.username != msg.username {
			kept = append(kept, row)
		}
	}
	s.rows = kept
	s.cursor = boundedIndex(s.cursor, len(s.visibleRows()))
	s.status, s.statusTone = fmt.Sprintf("Revoked %s on %s.", msg.username, msg.repo), grantToneSuccess
	return s
}

// ---- derived data ----

// visibleRows is the grants matching the filter, which matches the
// repository or the principal, case-insensitively.
func (s repoAccessScreen) visibleRows() []repoAccessRow {
	needle := strings.ToLower(strings.TrimSpace(s.filter))
	if needle == "" {
		return s.rows
	}
	var out []repoAccessRow
	for _, row := range s.rows {
		if strings.Contains(strings.ToLower(row.repo), needle) || strings.Contains(strings.ToLower(row.username), needle) {
			out = append(out, row)
		}
	}
	return out
}

func (s repoAccessScreen) selected(rows []repoAccessRow) (repoAccessRow, bool) {
	if len(rows) == 0 {
		return repoAccessRow{}, false
	}
	return rows[boundedIndex(s.cursor, len(rows))], true
}

// repoAccessPageRows is how many table rows fit at this terminal height; the
// next page is read when fewer than that many rows lie below the cursor.
func repoAccessPageRows(height int) int {
	if height <= 0 {
		return repoAccessDefaultPage
	}
	_, bodyH := shellContentSize(0, height)
	return max(bodyH-repoAccessTableChromeRows, 1)
}
