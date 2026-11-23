package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// adminProjectMembersScreen is "Project detail > Members" (board 28): the
// users and robots holding a project role, and the project's repositories
// beside them. The members come from GET /admin/v1/projects/{p}/members (the
// whole list, page by page); the repositories are the catalog's names whose
// first path segment is the project, read-only.
//
// A global administrator and an administrator of the project may open it; the
// API decides what each may do, and its refusals are shown as they come.
type adminProjectMembersScreen struct {
	gen     uint64
	project string

	// info is the project record (global administrators only; its absence is
	// not an error: the subtitle just has less to say).
	info     ports.ProjectSummary
	hasInfo  bool
	loaded   bool
	loadErr  string
	notFound bool
	members  []ports.AdminProjectMember
	// truncated is set when the list was longer than the page budget.
	truncated bool

	// focusRepos hands up and down to the repositories list (Tab toggles it).
	focusRepos   bool
	selectedUser string
	repoCursor   int
	// repoCount is how many repositories the last Update saw in the project:
	// the footer offers the arrows only when there is something to move over.
	repoCount int
	// floor is the load sequence below which a read is stale: a revoke sets
	// it, so a read that started before the revoke cannot bring the row back.
	floor uint64

	confirm confirmPrompt
	// revokeOpen marks the open prompt as a revoke; revoking is set from the
	// moment its command is dispatched until the result arrives.
	revokeOpen bool
	revoking   bool

	status     string
	statusTone grantTone
}

// projectMembersLoadSeq numbers every read of a members list, across instances.
var projectMembersLoadSeq atomic.Uint64

// projectMembersPageBudget bounds how many pages one read follows (200 members
// each): a project with more members shows the first ones and says so.
const projectMembersPageBudget = 50

func newAdminProjectMembersScreen(project, toast string) adminProjectMembersScreen {
	s := adminProjectMembersScreen{gen: accessGenerations.Add(1), project: project}
	if toast != "" {
		s.status, s.statusTone = toast, grantToneSuccess
	}
	return s
}

func (s adminProjectMembersScreen) ID() screen { return screenAdminProjectMembers }

func (s adminProjectMembersScreen) generation() uint64 { return s.gen }

func (s adminProjectMembersScreen) Init(env screenEnv) tea.Cmd {
	return projectMembersLoadCmd(env, s.gen, s.project)
}

// capturesInput reports a dialog in front.
func (s adminProjectMembersScreen) capturesInput() bool { return s.confirm.Active() }

func (s adminProjectMembersScreen) Keys() screenKeys {
	if s.confirm.Active() {
		return screenKeys{short: []key.Binding{
			key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y / enter", "confirm")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}}
	}
	back := []key.Binding{key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")), quitKey()}
	if s.notFound {
		return screenKeys{short: back}
	}
	if s.focusRepos {
		keys := []key.Binding{key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "members"))}
		if s.repoCount > 0 {
			keys = append(keys, key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")))
		}
		return screenKeys{short: append(keys, back...)}
	}
	keys := []key.Binding{key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "repositories"))}
	if len(s.members) > 0 {
		keys = append(keys, key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")))
	}
	if s.loadErr != "" {
		keys = append(keys, key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "retry")))
	}
	if s.loaded {
		keys = append(keys, key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "add member")))
	}
	if len(s.members) > 0 {
		keys = append(keys, key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "revoke")))
	}
	return screenKeys{short: append(keys, back...)}
}

// ---- messages ----

type adminProjectMembersLoadedMsg struct {
	gen       uint64
	seq       uint64
	members   []ports.AdminProjectMember
	truncated bool
	err       error
	info      ports.ProjectSummary
	hasInfo   bool
}

type adminProjectMemberRevokedMsg struct {
	gen      uint64
	username string
	wasAdmin bool
	err      error
}

func (adminProjectMembersLoadedMsg) projectScreenMsg()               {}
func (adminProjectMemberRevokedMsg) projectScreenMsg()               {}
func (adminProjectMembersLoadedMsg) accessSlot() screenSlot          { return slotProjectMembers }
func (adminProjectMemberRevokedMsg) accessSlot() screenSlot          { return slotProjectMembers }
func (m adminProjectMembersLoadedMsg) accessGen() uint64             { return m.gen }
func (m adminProjectMemberRevokedMsg) accessGen() uint64             { return m.gen }
func (m adminProjectMembersLoadedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }
func (m adminProjectMemberRevokedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

// openGrantProjectRoleMsg asks the parent to mount a FRESH Grant project role
// form; username (with isRobot and role) preselects an existing member whose
// role is being changed.
type openGrantProjectRoleMsg struct {
	project  string
	username string
	isRobot  bool
	role     domainauth.RepoRole
}

func projectMembersLoadCmd(env screenEnv, gen uint64, project string) tea.Cmd {
	client, session := env.Client, env.Session
	seq := projectMembersLoadSeq.Add(1)
	return func() tea.Msg {
		if client == nil {
			return adminProjectMembersLoadedMsg{gen: gen, seq: seq, err: errAdminUnavailable}
		}
		ctx := context.Background()
		out := adminProjectMembersLoadedMsg{gen: gen, seq: seq}
		if info, err := client.GetProject(ctx, session, project); err == nil {
			out.info, out.hasInfo = info, true
		} else if IsAdminSessionExpired(err) {
			out.err = err
			return out
		}
		after := ""
		for page := range projectMembersPageBudget {
			result, err := client.ListProjectMembers(ctx, session, project, adminProjectMembersMaxLimit, after)
			if err != nil {
				out.err, out.members = err, nil
				return out
			}
			out.members = append(out.members, result.Members...)
			if result.Next == "" {
				return out
			}
			after = result.Next
			out.truncated = page == projectMembersPageBudget-1
		}
		return out
	}
}

func projectMemberRevokeCmd(env screenEnv, gen uint64, project, username string, wasAdmin bool) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return adminProjectMemberRevokedMsg{gen: gen, username: username, wasAdmin: wasAdmin, err: errAdminUnavailable}
		}
		err := client.DeleteProjectMember(context.Background(), session, project, username)
		return adminProjectMemberRevokedMsg{gen: gen, username: username, wasAdmin: wasAdmin, err: err}
	}
}

// ---- Update ----

func (s adminProjectMembersScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	s.repoCount = len(s.repositories(env))
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case adminProjectMembersLoadedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		return s.applyLoaded(typed), nil, false
	case adminProjectMemberRevokedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		next, cmd := s.applyRevoked(env, typed)
		return next, cmd, false
	}
	return s, nil, false
}

func (s adminProjectMembersScreen) applyLoaded(msg adminProjectMembersLoadedMsg) adminProjectMembersScreen {
	if msg.seq < s.floor {
		return s
	}
	s.loaded = true
	if msg.err != nil {
		if IsAdminNotFound(msg.err) {
			s.notFound, s.members, s.loadErr = true, nil, ""
			return s
		}
		s.loadErr = strings.TrimRight(adminErrorDetail(msg.err), ".")
		return s
	}
	s.loadErr, s.notFound = "", false
	// The API pages by user id, which means nothing to an operator: list the
	// members by name, case-insensitively.
	s.members, s.truncated = slices.Clone(msg.members), msg.truncated
	slices.SortStableFunc(s.members, func(a, b ports.AdminProjectMember) int {
		return strings.Compare(strings.ToLower(a.Username), strings.ToLower(b.Username))
	})
	s.info, s.hasInfo = msg.info, msg.hasInfo
	s.selectedUser = s.keptSelection()
	return s
}

func (s adminProjectMembersScreen) keptSelection() string {
	if len(s.members) == 0 {
		return ""
	}
	for _, member := range s.members {
		if member.Username == s.selectedUser {
			return s.selectedUser
		}
	}
	return s.members[0].Username
}

func (s adminProjectMembersScreen) applyRevoked(env screenEnv, msg adminProjectMemberRevokedMsg) (adminProjectMembersScreen, tea.Cmd) {
	s.revoking = false
	if s.revokeOpen {
		s.confirm, s.revokeOpen = confirmPrompt{}, false
	}
	reload := func(s adminProjectMembersScreen) (adminProjectMembersScreen, tea.Cmd) {
		s.floor = projectMembersLoadSeq.Add(1)
		return s, projectMembersLoadCmd(env, s.gen, s.project)
	}
	switch {
	case msg.err == nil:
		s.members = slices.DeleteFunc(slices.Clone(s.members), func(m ports.AdminProjectMember) bool { return m.Username == msg.username })
		s.selectedUser = s.keptSelection()
		s.status, s.statusTone = fmt.Sprintf("Revoked %s on project %s.", msg.username, s.project), grantToneSuccess
		return reload(s)
	case IsAdminNotFound(msg.err):
		s.status, s.statusTone = fmt.Sprintf("%s no longer has a role on project %s. The list was refreshed.", msg.username, s.project), grantToneWarning
		return reload(s)
	case IsAdminForbidden(msg.err) && msg.wasAdmin:
		s.status, s.statusTone = "Only a global administrator can revoke a project admin.", grantToneError
		return s, nil
	}
	s.status, s.statusTone = fmt.Sprintf("Could not revoke %s: %s.", msg.username, strings.TrimRight(adminErrorDetail(msg.err), ".")), grantToneError
	return s, nil
}

func (s adminProjectMembersScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if s.confirm.Active() {
		next, cmd, consumed := s.confirm.update(env, msg)
		s.confirm = next
		if s.revokeOpen {
			s.revoking = s.revoking || cmd != nil
			s.revokeOpen = next.Active()
		}
		return s, cmd, consumed
	}
	repos := s.repositories(env)
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminProjects), true
	case s.notFound:
		return s, nil, true
	case isTabKey(msg):
		s.focusRepos = !s.focusRepos
		return s, nil, true
	case s.focusRepos:
		switch {
		case isMoveUpKey(msg):
			s.repoCursor = boundedIndex(s.repoCursor-1, len(repos))
		case isMoveDownKey(msg):
			s.repoCursor = boundedIndex(s.repoCursor+1, len(repos))
		}
		return s, nil, true
	case isMoveUpKey(msg):
		return s.moveBy(-1), nil, true
	case isMoveDownKey(msg):
		return s.moveBy(1), nil, true
	case isCommandRune(msg, 'r'):
		if s.loadErr != "" {
			s.loadErr = ""
			return s, projectMembersLoadCmd(env, s.gen, s.project), true
		}
	case isCommandRune(msg, 'g'):
		if !s.loaded {
			return s, nil, true
		}
		open := openGrantProjectRoleMsg{project: s.project}
		if member, ok := s.selected(); ok && s.selectedUser != "" {
			open.username, open.isRobot, open.role = member.Username, member.IsRobot, member.Role
		}
		return s, func() tea.Msg { return open }, true
	case isCommandRune(msg, 'x'):
		return s.askRevoke(), nil, true
	}
	return s, nil, true
}

func (s adminProjectMembersScreen) moveBy(delta int) adminProjectMembersScreen {
	if len(s.members) == 0 {
		return s
	}
	s.selectedUser = s.members[boundedIndex(s.index()+delta, len(s.members))].Username
	return s
}

func (s adminProjectMembersScreen) index() int {
	for i, member := range s.members {
		if member.Username == s.selectedUser {
			return i
		}
	}
	return 0
}

func (s adminProjectMembersScreen) selected() (ports.AdminProjectMember, bool) {
	if len(s.members) == 0 {
		return ports.AdminProjectMember{}, false
	}
	return s.members[s.index()], true
}

// repositories are the catalog's repositories of this project: names whose
// FIRST path segment is the project (odabank/api, never odabank2/api).
func (s adminProjectMembersScreen) repositories(env screenEnv) []string {
	var names []string
	for _, name := range env.KnownRepositories {
		if strings.HasPrefix(name, s.project+"/") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// askRevoke opens the revoke dialog for the selected member. The target is
// captured now: a reload while the dialog is open cannot change what Enter
// revokes.
func (s adminProjectMembersScreen) askRevoke() adminProjectMembersScreen {
	member, ok := s.selected()
	if !ok {
		return s
	}
	if s.revoking {
		s.status, s.statusTone = "Revoking, please wait.", grantToneInfo
		return s
	}
	gen, project, username, wasAdmin := s.gen, s.project, member.Username, member.Role == domainauth.RepoRoleAdmin
	s.status, s.statusTone = "", grantToneNone
	s.confirm = revokeProjectRoleConfirm(project, member, func(env screenEnv) tea.Cmd {
		return projectMemberRevokeCmd(env, gen, project, username, wasAdmin)
	})
	s.revokeOpen = true
	return s
}

// projectRoleLabel words a project role: "Project reader", "Project writer",
// "Project admin".
func projectRoleLabel(role domainauth.RepoRole) string {
	return "Project " + strings.ToLower(grantRoleLabel(role))
}
