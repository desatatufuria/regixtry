package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
// No backend call returns all grants at once, so the list is built from the
// calls that exist: the catalog's repository names (no call, the screen
// reads screenEnv.KnownRepositories), one ListRepositoryGrants per
// repository, and one ListUsers plus one ListRobots to tell users from
// robots and to open a principal. Grants are read lazily, a batch of
// repositories (about one page) at a time, only as far as the visible page
// and the cursor need, so a large catalog costs a page of calls, not one per
// repository.
type repoAccessScreen struct {
	// gen identifies this instance: every async message carries it and a
	// message with another gen is dropped, so a late result can never land on
	// a newer screen that reuses the slot.
	gen uint64

	repos []string
	// next indexes repos: everything before it has been requested.
	next    int
	loading bool
	rows    []repoAccessRow
	// repoErrs lists the repositories whose grants could not be read.
	repoErrs []string

	// principals maps a username to the user or robot behind it. It is
	// replaced as a whole, never mutated, so copies of the screen share it
	// safely.
	principals       map[string]grantPrincipal
	principalsLoaded bool
	principalsErr    string

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
	username string
	role     domainauth.RepoRole
	since    time.Time
}

// repoAccessGenerations hands out the unique id of each repoAccessScreen.
var repoAccessGenerations atomic.Uint64

// repoAccessDefaultPage is the page size assumed before the terminal size is
// known.
const repoAccessDefaultPage = 20

// repoAccessMinBatch is the fewest repositories one read asks for.
const repoAccessMinBatch = 5

func newRepoAccessScreen() repoAccessScreen {
	return repoAccessScreen{gen: repoAccessGenerations.Add(1)}
}

func (s repoAccessScreen) ID() screen { return screenAdminRepositoryAccess }

func (s repoAccessScreen) Keys() screenKeys {
	if s.filtering {
		return repoAccessFilterKeys
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

var repoAccessFilterKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "done")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "done")),
}}

// ---- messages ----

type repoAccessReposMsg struct {
	gen   uint64
	repos []string
}

type repoAccessPrincipalsLoadedMsg struct {
	gen       uint64
	users     []ports.AdminUser
	usersErr  error
	robots    []ports.AdminRobot
	robotsErr error
}

// repoAccessRepoGrants is one repository's read inside a batch.
type repoAccessRepoGrants struct {
	repo   string
	grants []ports.AdminRepositoryGrant
	err    error
}

type repoAccessGrantsLoadedMsg struct {
	gen     uint64
	results []repoAccessRepoGrants
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

func (m repoAccessPrincipalsLoadedMsg) expiredReason() (string, bool) {
	return expiredReasonOf(m.usersErr, m.robotsErr)
}

func (m repoAccessGrantsLoadedMsg) expiredReason() (string, bool) {
	for _, result := range m.results {
		if reason, ok := expiredReasonOf(result.err); ok {
			return reason, true
		}
	}
	return "", false
}

func (m repoAccessRevokedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

// repoAccessMsgGen returns the screen generation a repository-access async
// message belongs to.
func repoAccessMsgGen(msg tea.Msg) (uint64, bool) {
	switch typed := msg.(type) {
	case repoAccessReposMsg:
		return typed.gen, true
	case repoAccessPrincipalsLoadedMsg:
		return typed.gen, true
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
	gen := s.gen
	repos := sortedUniqueRepositories(env.KnownRepositories)
	return tea.Batch(
		func() tea.Msg { return repoAccessReposMsg{gen: gen, repos: repos} },
		repoAccessLoadPrincipalsCmd(env, gen),
	)
}

func sortedUniqueRepositories(names []string) []string {
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func (s repoAccessScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	if gen, ok := repoAccessMsgGen(msg); ok && gen != s.gen {
		return s, nil, false
	}
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case repoAccessReposMsg:
		s.repos = typed.repos
		next, cmd := s.fill(env)
		return next, cmd, false
	case repoAccessPrincipalsLoadedMsg:
		s.principalsLoaded = true
		s.principalsErr = ""
		if typed.usersErr != nil {
			s.principalsErr = "Users: " + typed.usersErr.Error()
		}
		if typed.robotsErr != nil {
			s.principalsErr = strings.TrimSpace(s.principalsErr + " Robots: " + typed.robotsErr.Error())
		}
		s.principals = make(map[string]grantPrincipal)
		for _, p := range mergeGrantPrincipals(typed.users, typed.robots) {
			s.principals[strings.ToLower(p.Name)] = p
		}
		return s, nil, false
	case repoAccessGrantsLoadedMsg:
		s.loading = false
		rows := append([]repoAccessRow(nil), s.rows...)
		errs := append([]string(nil), s.repoErrs...)
		for _, result := range typed.results {
			if result.err != nil {
				errs = append(errs, result.repo+": "+result.err.Error())
				continue
			}
			for _, grant := range result.grants {
				rows = append(rows, repoAccessRow{repo: result.repo, username: grant.Username, role: grant.Role, since: grant.CreatedAt})
			}
		}
		sortRepoAccessRows(rows)
		s.rows, s.repoErrs = rows, errs
		next, cmd := s.fill(env)
		return next, cmd, false
	case repoAccessRevokedMsg:
		return s.revoked(env, typed), nil, false
	}
	return s, nil, false
}

func sortRepoAccessRows(rows []repoAccessRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].repo != rows[j].repo {
			return rows[i].repo < rows[j].repo
		}
		return strings.ToLower(rows[i].username) < strings.ToLower(rows[j].username)
	})
}

// fill reads the next batch of repositories when the visible rows do not yet
// cover the cursor plus one page, and there is something left to read.
func (s repoAccessScreen) fill(env screenEnv) (repoAccessScreen, tea.Cmd) {
	if s.loading || s.next >= len(s.repos) {
		return s, nil
	}
	page := repoAccessPageRows(env.Layout.Height)
	if len(s.visibleRows()) >= s.cursor+page {
		return s, nil
	}
	count := min(max(page, repoAccessMinBatch), len(s.repos)-s.next)
	batch := s.repos[s.next : s.next+count]
	s.next += count
	s.loading = true
	return s, repoAccessLoadGrantsCmd(env, s.gen, batch)
}

func repoAccessLoadPrincipalsCmd(env screenEnv, gen uint64) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			unavailable := errors.New("admin API is unavailable for this session")
			return repoAccessPrincipalsLoadedMsg{gen: gen, usersErr: unavailable, robotsErr: unavailable}
		}
		users, usersErr := client.ListUsers(context.Background(), session)
		robots, robotsErr := client.ListRobots(context.Background(), session)
		return repoAccessPrincipalsLoadedMsg{gen: gen, users: users, usersErr: usersErr, robots: robots, robotsErr: robotsErr}
	}
}

// repoAccessLoadGrantsCmd reads the grants of each repository in batch, one
// call each, and stops at the first expired session: the rest would only
// repeat the same error.
func repoAccessLoadGrantsCmd(env screenEnv, gen uint64, batch []string) tea.Cmd {
	client, session := env.Client, env.Session
	repos := append([]string(nil), batch...)
	return func() tea.Msg {
		results := make([]repoAccessRepoGrants, 0, len(repos))
		for _, repo := range repos {
			if client == nil {
				results = append(results, repoAccessRepoGrants{repo: repo, err: errors.New("admin API is unavailable for this session")})
				continue
			}
			grants, err := client.ListRepositoryGrants(context.Background(), session, repo)
			results = append(results, repoAccessRepoGrants{repo: repo, grants: grants, err: err})
			if err != nil && IsAdminSessionExpired(err) {
				break
			}
		}
		return repoAccessGrantsLoadedMsg{gen: gen, results: results}
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
		// A second Enter while the revoke is in flight must not send a second
		// delete.
		if isEnterKey(msg) && s.revoking {
			return s, nil, true
		}
		if isEnterKey(msg) {
			s.revoking = true
		}
		confirm, cmd, consumed := s.confirm.update(env, msg)
		s.confirm = confirm
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
	case isRuneKey(msg, 'g'):
		return s, openGrantAccess("", screenAdminRepositoryAccess), true
	case isEnterKey(msg):
		return s.openPrincipal(rows)
	case isRuneKey(msg, 'x'):
		return s.askRevoke(env, rows)
	}
	return s, nil, true
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
	principal, found := s.principals[strings.ToLower(row.username)]
	if !found {
		s.status, s.statusTone = fmt.Sprintf("Cannot open %s: it is not in the users or robots list.", row.username), grantToneWarning
		return s, nil, true
	}
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
	)
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

// repoAccessPageRows is how many table rows fit at this terminal height; it
// sizes the batches of repositories read.
func repoAccessPageRows(height int) int {
	if height <= 0 {
		return repoAccessDefaultPage
	}
	_, bodyH := shellContentSize(0, height)
	return max(bodyH-repoAccessTableChromeRows, 1)
}

// complete reports whether every repository has been read.
func (s repoAccessScreen) complete() bool { return s.next >= len(s.repos) && !s.loading }
