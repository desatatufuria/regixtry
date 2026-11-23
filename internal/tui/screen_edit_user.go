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

// editUserScreen is screenAdminEditUser (tui-v2-redesign S3, the "Edit user"
// board): one user's profile, password entry, a summary of their repository
// grants and tokens, and the danger area. It is addressed by user id, so it is
// reachable from the Users list and from the Tokens screen alike.
type editUserScreen struct {
	gen      uint64
	userID   string
	username string

	user   ports.AdminUser
	found  bool
	loaded bool
	// refreshing is set while a reload is in flight; r is ignored meanwhile.
	refreshing bool
	userErr    string
	grants     []ports.AdminRepoGrant
	grantsErr  string
	// projectGrants are the project roles the user holds: access inherited by
	// every repository of the project, listed next to the exact grants.
	projectGrants    []ports.AdminProjectGrant
	projectGrantsErr string
	tokens           []ports.AdminToken
	tokensErr        string

	// grantsFocused hands up/down, j/k and x to the access list (Tab toggles
	// it); grantCursor is the selected row of accessRows: project roles first,
	// then the exact repository grants.
	grantsFocused bool
	grantCursor   int
	// grantsFloor is the load sequence below which a grants read is stale: a
	// revoke sets it, so a reload that started before the revoke cannot bring
	// the row back.
	grantsFloor uint64
	// revokeOpen marks the open prompt as a revoke; revoking is set from the
	// moment its command is dispatched until the result arrives, even if Esc
	// closed the prompt meanwhile.
	revokeOpen bool
	revoking   bool

	confirm confirmPrompt

	notice     string
	noticeTone userNoticeTone
}

// editUserLoadSeq numbers every read of the screen, across instances.
var editUserLoadSeq atomic.Uint64

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
			key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y / enter", "confirm")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}}
	}
	if s.grantsFocused && len(s.accessRows()) > 0 {
		return screenKeys{short: []key.Binding{
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
			key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "revoke")),
			key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "grant access")),
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "done")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			quitKey(),
		}}
	}
	keys := []key.Binding{
		key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "enable/disable")),
		key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "password")),
		key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "grant access")),
	}
	if len(s.accessRows()) > 0 {
		keys = append(keys, key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "grants")))
	}
	return screenKeys{short: append(keys,
		key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tokens")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "disable")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		quitKey(),
	)}
}

// editUserLoadedMsg carries the reads behind the screen. The user comes from
// the user list (there is no read-one-user call).
type editUserLoadedMsg struct {
	gen uint64
	// seq numbers the read (editUserLoadSeq), taken when it was issued.
	seq       uint64
	userID    string
	users     []ports.AdminUser
	usersErr  error
	grants    []ports.AdminRepoGrant
	grantsErr error
	// projectGrants are the user's project roles (global administrators read
	// them from GET /admin/v1/users/{id}/project-grants).
	projectGrants    []ports.AdminProjectGrant
	projectGrantsErr error
	tokens           []ports.AdminToken
	tokensErr        error
}

func (m editUserLoadedMsg) screenGen() uint64 { return m.gen }

func (m editUserLoadedMsg) expiredReason() (string, bool) {
	return expiredReasonOf(m.usersErr, m.grantsErr, m.projectGrantsErr, m.tokensErr)
}

func editUserLoadCmd(env screenEnv, gen uint64, userID string) tea.Cmd {
	client, session := env.Client, env.Session
	seq := editUserLoadSeq.Add(1)
	return func() tea.Msg {
		if client == nil {
			unavailable := errors.New("admin API is unavailable for this session")
			return editUserLoadedMsg{gen: gen, seq: seq, userID: userID, usersErr: unavailable, grantsErr: unavailable, projectGrantsErr: unavailable, tokensErr: unavailable}
		}
		users, usersErr := client.ListUsers(context.Background(), session)
		grants, grantsErr := client.ListUserGrants(context.Background(), session, userID)
		projectGrants, projectGrantsErr := client.ListUserProjectGrants(context.Background(), session, userID)
		tokens, tokensErr := client.ListUserAdminTokens(context.Background(), session, userID)
		return editUserLoadedMsg{gen: gen, seq: seq, userID: userID, users: users, usersErr: usersErr, grants: grants, grantsErr: grantsErr,
			projectGrants: projectGrants, projectGrantsErr: projectGrantsErr, tokens: tokens, tokensErr: tokensErr}
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
	case editUserGrantRevokedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		next, cmd := s.applyRevoked(env, typed)
		return next, cmd, false
	case editUserProjectRevokedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		next, cmd := s.applyProjectRevoked(env, typed)
		return next, cmd, false
	case userEnabledMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		s.confirm, s.revokeOpen = confirmPrompt{}, false
		s.noticeTone, s.notice = userEnabledNotice(typed)
		if typed.err != nil {
			return s, nil, false
		}
		s.refreshing = true
		return s, editUserLoadCmd(env, s.gen, s.userID), false
	}
	return s, nil, false
}

func (s editUserScreen) applyLoaded(msg editUserLoadedMsg) editUserScreen {
	s.loaded, s.refreshing = true, false
	s.userErr, s.grantsErr, s.tokensErr = errText(msg.usersErr), errText(msg.grantsErr), errText(msg.tokensErr)
	s.projectGrantsErr = errText(msg.projectGrantsErr)
	if msg.usersErr == nil {
		s.found = false
		for _, user := range msg.users {
			if user.ID == s.userID {
				s.user, s.found, s.username = user, true, user.Username
				break
			}
		}
	}
	if msg.seq >= s.grantsFloor {
		grants, projectGrants := s.grants, s.projectGrants
		if msg.grantsErr == nil {
			grants = msg.grants
		}
		if msg.projectGrantsErr == nil {
			projectGrants = msg.projectGrants
		}
		s = s.withAccess(grants, projectGrants)
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
		if s.revokeOpen {
			s.revoking = s.revoking || cmd != nil
			s.revokeOpen = next.Active()
		}
		return s, cmd, consumed
	}
	if s.grantsFocused {
		switch {
		case isMoveUpKey(msg):
			s.grantCursor = boundedIndex(s.grantCursor-1, len(s.accessRows()))
			return s, nil, true
		case isMoveDownKey(msg):
			s.grantCursor = boundedIndex(s.grantCursor+1, len(s.accessRows()))
			return s, nil, true
		case isCommandRune(msg, 'x'):
			return s.askRevoke(), nil, true
		}
	}
	name := adminFirstNonEmpty(s.username, s.userID)
	switch {
	case isEscKey(msg):
		return s, openUserScreen(openUserScreenMsg{to: screenAdminUsers, userID: s.userID}), true
	case isTabKey(msg):
		s.grantsFocused = !s.grantsFocused && len(s.accessRows()) > 0
		return s, nil, true
	case isRuneKey(msg, 'g'):
		return s, openGrantAccess(s.userID, screenAdminEditUser), true
	case isRuneKey(msg, 't'):
		return s, openUserScreen(openUserScreenMsg{to: screenAdminEditUserTokens, userID: s.userID, username: name}), true
	case isRuneKey(msg, 'p'):
		return s, openUserScreen(openUserScreenMsg{to: screenAdminChangePassword, userID: s.userID, username: name}), true
	case isRuneKey(msg, 'r'):
		if !s.loaded || s.refreshing {
			return s, nil, true
		}
		s.refreshing, s.notice = true, ""
		return s, editUserLoadCmd(env, s.gen, s.userID), true
	case isRuneKey(msg, 'd'), isSpaceKey(msg):
		if !s.found {
			return s.withNotice(userNoticeInfo, "The user is not loaded yet."), nil, true
		}
		if blocked, reason := ownAccountGuard(env, s.user); blocked {
			return s.withNotice(userNoticeError, reason), nil, true
		}
		s.confirm, s.notice = userToggleConfirm(s.gen, s.user), ""
	}
	return s, nil, true
}

// accessSummary words the counts under the access list: project roles and
// exact repository grants.
func accessSummary(projects, repositories int) string {
	return fmt.Sprintf("%s, %s.", plural(projects, "project role"), plural(repositories, "repository grant"))
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

// editUserAccessRow is one row of the access list: a project role (shown as
// "odabank/*") or an exact repository grant.
type editUserAccessRow struct {
	project bool
	// name is the project for a project role, the repository otherwise.
	name string
	role domainauth.RepoRole
	// since is when the grant was made.
	since time.Time
}

func (r editUserAccessRow) key() string {
	if r.project {
		return "project:" + r.name
	}
	return "repo:" + r.name
}

// label is the first column: "odabank/*" for a project role.
func (r editUserAccessRow) label() string {
	if r.project {
		return r.name + "/*"
	}
	return r.name
}

// accessRows lists the project roles (by project) and then the exact
// repository grants, the order the cursor walks.
func (s editUserScreen) accessRows() []editUserAccessRow {
	rows := make([]editUserAccessRow, 0, len(s.projectGrants)+len(s.grants))
	for _, grant := range s.projectGrants {
		rows = append(rows, editUserAccessRow{project: true, name: grant.Project, role: grant.Role, since: grant.CreatedAt})
	}
	for _, grant := range s.grants {
		rows = append(rows, editUserAccessRow{name: grant.Repository.String(), role: grant.Role, since: grant.CreatedAt})
	}
	return rows
}

func (s editUserScreen) selectedRow() (editUserAccessRow, bool) {
	rows := s.accessRows()
	if len(rows) == 0 {
		return editUserAccessRow{}, false
	}
	return rows[boundedIndex(s.grantCursor, len(rows))], true
}

// withGrants replaces the exact repository grants, keeping the selection.
func (s editUserScreen) withGrants(grants []ports.AdminRepoGrant) editUserScreen {
	return s.withAccess(grants, s.projectGrants)
}

// withAccess replaces both lists, keeping the selection on the same row when
// it is still there.
func (s editUserScreen) withAccess(grants []ports.AdminRepoGrant, projectGrants []ports.AdminProjectGrant) editUserScreen {
	keep := ""
	if row, ok := s.selectedRow(); ok {
		keep = row.key()
	}
	s.grants, s.projectGrants = grants, projectGrants
	rows := s.accessRows()
	s.grantCursor = boundedIndex(s.grantCursor, len(rows))
	for i, row := range rows {
		if keep != "" && row.key() == keep {
			s.grantCursor = i
		}
	}
	if len(rows) == 0 {
		s.grantsFocused = false
	}
	return s
}

// selectedGrant is the selected row when it is an exact repository grant.
func (s editUserScreen) selectedGrant() (ports.AdminRepoGrant, bool) {
	row, ok := s.selectedRow()
	if !ok || row.project {
		return ports.AdminRepoGrant{}, false
	}
	for _, grant := range s.grants {
		if grant.Repository.String() == row.name {
			return grant, true
		}
	}
	return ports.AdminRepoGrant{}, false
}

// editUserGrantRevokedMsg is the result of revoking one of the user's grants.
type editUserGrantRevokedMsg struct {
	gen      uint64
	repo     string
	username string
	err      error
}

func (m editUserGrantRevokedMsg) screenGen() uint64 { return m.gen }

func (m editUserGrantRevokedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

func editUserRevokeCmd(env screenEnv, gen uint64, repo, username string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return editUserGrantRevokedMsg{gen: gen, repo: repo, username: username, err: errors.New("admin API is unavailable for this session")}
		}
		err := client.DeleteRepositoryGrant(context.Background(), session, repo, username)
		return editUserGrantRevokedMsg{gen: gen, repo: repo, username: username, err: err}
	}
}

// askRevoke opens the revoke dialog for the selected grant. The target is
// captured now: a reload while the dialog is open cannot change what Enter
// revokes.
func (s editUserScreen) askRevoke() editUserScreen {
	row, ok := s.selectedRow()
	if !ok {
		return s
	}
	if row.project {
		return s.askRevokeProjectRole(row)
	}
	grant, ok := s.selectedGrant()
	if !ok {
		return s
	}
	if s.revoking {
		return s.withNotice(userNoticeInfo, "Revoking, please wait.")
	}
	if s.username == "" {
		return s.withNotice(userNoticeError, "Cannot revoke: this user has no username.")
	}
	gen, repo, username := s.gen, grant.Repository.String(), s.username
	s.notice = ""
	s.confirm = revokeGrantConfirm(repo, username, grant.Role, func(env screenEnv) tea.Cmd { return editUserRevokeCmd(env, gen, repo, username) })
	s.revokeOpen = true
	return s
}

// applyRevoked applies a revoke result. On success the row leaves the list at
// once, reads started before it can no longer bring it back, and the grants
// are read again; on failure the list stays as it was.
func (s editUserScreen) applyRevoked(env screenEnv, msg editUserGrantRevokedMsg) (editUserScreen, tea.Cmd) {
	s.revoking = false
	if s.revokeOpen {
		s.confirm, s.revokeOpen = confirmPrompt{}, false
	}
	if msg.err != nil {
		s = s.withNotice(userNoticeError, fmt.Sprintf("Could not revoke %s on %s: %s", msg.username, msg.repo, msg.err.Error()))
		if IsAdminNotFound(msg.err) {
			// Already gone elsewhere: read the grants again so the row leaves.
			s.refreshing = true
			return s, editUserLoadCmd(env, s.gen, s.userID)
		}
		return s, nil
	}
	kept := make([]ports.AdminRepoGrant, 0, len(s.grants))
	for _, grant := range s.grants {
		if grant.Repository.String() != msg.repo {
			kept = append(kept, grant)
		}
	}
	s = s.withAccess(kept, s.projectGrants)
	s.grantsFloor = editUserLoadSeq.Add(1)
	s = s.withNotice(userNoticeSuccess, fmt.Sprintf("Revoked %s on %s.", msg.username, msg.repo))
	s.refreshing = true
	return s, editUserLoadCmd(env, s.gen, s.userID)
}

// editUserProjectRevokedMsg is the result of revoking one of the user's
// project roles.
type editUserProjectRevokedMsg struct {
	gen      uint64
	project  string
	username string
	err      error
}

func (m editUserProjectRevokedMsg) screenGen() uint64 { return m.gen }

func (m editUserProjectRevokedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

func editUserProjectRevokeCmd(env screenEnv, gen uint64, project, username string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return editUserProjectRevokedMsg{gen: gen, project: project, username: username, err: errors.New("admin API is unavailable for this session")}
		}
		err := client.DeleteProjectMember(context.Background(), session, project, username)
		return editUserProjectRevokedMsg{gen: gen, project: project, username: username, err: err}
	}
}

// askRevokeProjectRole opens the dialog that removes the selected project
// role. It is the same dialog the project's Members screen shows. The target
// is captured now, like the repository revoke.
func (s editUserScreen) askRevokeProjectRole(row editUserAccessRow) editUserScreen {
	if s.revoking {
		return s.withNotice(userNoticeInfo, "Revoking, please wait.")
	}
	if s.username == "" {
		return s.withNotice(userNoticeError, "Cannot revoke: this user has no username.")
	}
	gen, project, username := s.gen, row.name, s.username
	member := ports.AdminProjectMember{Username: username, Role: row.role, CreatedAt: row.since}
	s.notice = ""
	s.confirm = revokeProjectRoleConfirm(project, member, func(env screenEnv) tea.Cmd { return editUserProjectRevokeCmd(env, gen, project, username) })
	s.revokeOpen = true
	return s
}

// applyProjectRevoked applies a project revoke result like applyRevoked does
// for a repository: the row leaves at once, reads started before it cannot
// bring it back, and the lists are read again; on failure the list stays.
func (s editUserScreen) applyProjectRevoked(env screenEnv, msg editUserProjectRevokedMsg) (editUserScreen, tea.Cmd) {
	s.revoking = false
	if s.revokeOpen {
		s.confirm, s.revokeOpen = confirmPrompt{}, false
	}
	if msg.err != nil {
		s = s.withNotice(userNoticeError, fmt.Sprintf("Could not revoke %s on project %s: %s", msg.username, msg.project, strings.TrimRight(adminErrorDetail(msg.err), ".")+"."))
		if IsAdminNotFound(msg.err) {
			// Already gone elsewhere: read again so the row leaves.
			s.refreshing = true
			return s, editUserLoadCmd(env, s.gen, s.userID)
		}
		return s, nil
	}
	kept := make([]ports.AdminProjectGrant, 0, len(s.projectGrants))
	for _, grant := range s.projectGrants {
		if grant.Project != msg.project {
			kept = append(kept, grant)
		}
	}
	s = s.withAccess(s.grants, kept)
	s.grantsFloor = editUserLoadSeq.Add(1)
	s = s.withNotice(userNoticeSuccess, fmt.Sprintf("Revoked %s on project %s.", msg.username, msg.project))
	s.refreshing = true
	return s, editUserLoadCmd(env, s.gen, s.userID)
}
