package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// grantProjectRoleScreen is "Grant project role" (board 29): pick a user or a
// robot and a project role (Project reader, writer or admin) for ONE project.
// A project role covers every repository of the project, now and later, and
// applies from the next request. Robots are readers or writers only: the form
// refuses admin for a robot before anything is sent, and the API repeats the
// rule. Everything else the API refuses (a project admin granting admin, a
// read-only user given write access, an unknown member) is shown as it comes.
type grantProjectRoleScreen struct {
	gen     uint64
	project string

	kind   string
	member string
	// suggestion indexes the member rows; only the arrows move it, because j,
	// k and q are typed text in the member field.
	suggestion int
	role       domainauth.RepoRole
	focus      grantProjectField

	// principals are the users and robots the member field suggests; they are
	// absent for a session that may not list them (a project administrator),
	// who types the exact username.
	principals       []grantPrincipal
	principalsLoaded bool
	principalsErr    string

	// applying is set from the Enter that dispatched the grant until its
	// result arrives; keys other than the typing ones are ignored meanwhile.
	applying bool
	err      string
}

type grantProjectField int

const (
	grantProjectFieldKind grantProjectField = iota
	grantProjectFieldMember
	grantProjectFieldRole
	grantProjectFieldCount
)

func newGrantProjectRoleScreen(open openGrantProjectRoleMsg) grantProjectRoleScreen {
	s := grantProjectRoleScreen{gen: accessGenerations.Add(1), project: open.project, kind: grantKindUser, role: domainauth.RepoRoleReader}
	if open.username != "" {
		s.member, s.role, s.focus = open.username, open.role, grantProjectFieldRole
		if open.isRobot {
			s.kind = grantKindRobot
		}
	}
	return s
}

func (s grantProjectRoleScreen) ID() screen { return screenAdminGrantProjectRole }

func (s grantProjectRoleScreen) generation() uint64 { return s.gen }

func (s grantProjectRoleScreen) Init(env screenEnv) tea.Cmd {
	return grantProjectRolePrincipalsCmd(env, s.gen)
}

func (s grantProjectRoleScreen) Keys() screenKeys {
	if s.applying {
		return screenKeys{}
	}
	return screenKeys{short: []key.Binding{
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "pick member")),
		key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "type, role")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "grant")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}}
}

// ---- messages ----

type grantProjectRolePrincipalsMsg struct {
	gen       uint64
	users     []ports.AdminUser
	usersErr  error
	robots    []ports.AdminRobot
	robotsErr error
}

type grantProjectRoleDoneMsg struct {
	gen      uint64
	project  string
	username string
	isRobot  bool
	role     domainauth.RepoRole
	err      error
}

func (grantProjectRolePrincipalsMsg) projectScreenMsg()      {}
func (grantProjectRoleDoneMsg) projectScreenMsg()            {}
func (grantProjectRolePrincipalsMsg) accessSlot() screenSlot { return slotGrantProjectRole }
func (grantProjectRoleDoneMsg) accessSlot() screenSlot       { return slotGrantProjectRole }
func (m grantProjectRolePrincipalsMsg) accessGen() uint64    { return m.gen }
func (m grantProjectRoleDoneMsg) accessGen() uint64          { return m.gen }
func (m grantProjectRolePrincipalsMsg) expiredReason() (string, bool) {
	return expiredReasonOf(m.usersErr, m.robotsErr)
}
func (m grantProjectRoleDoneMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

func grantProjectRolePrincipalsCmd(env screenEnv, gen uint64) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return grantProjectRolePrincipalsMsg{gen: gen, usersErr: errAdminUnavailable, robotsErr: errAdminUnavailable}
		}
		users, usersErr := client.ListUsers(context.Background(), session)
		robots, robotsErr := client.ListRobots(context.Background(), session)
		return grantProjectRolePrincipalsMsg{gen: gen, users: users, usersErr: usersErr, robots: robots, robotsErr: robotsErr}
	}
}

func grantProjectRoleApplyCmd(env screenEnv, gen uint64, input ports.AdminPutProjectGrantInput, isRobot bool) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		done := grantProjectRoleDoneMsg{gen: gen, project: input.Project, username: input.Username, isRobot: isRobot, role: input.Role}
		if client == nil {
			done.err = errAdminUnavailable
			return done
		}
		_, done.err = client.PutProjectMember(context.Background(), session, input)
		return done
	}
}

// ---- Update ----

func (s grantProjectRoleScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case grantProjectRolePrincipalsMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		s.principalsLoaded = true
		s.principalsErr = errText(typed.usersErr)
		if s.principalsErr == "" {
			s.principalsErr = errText(typed.robotsErr)
		}
		s.principals = mergeGrantPrincipals(typed.users, typed.robots)
		return s, nil, false
	case grantProjectRoleDoneMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		next, cmd := s.applyDone(typed)
		return next, cmd, false
	}
	return s, nil, false
}

func (s grantProjectRoleScreen) applyDone(msg grantProjectRoleDoneMsg) (grantProjectRoleScreen, tea.Cmd) {
	s.applying = false
	if msg.err == nil {
		toast := fmt.Sprintf("%s is now %s of %s.", msg.username, projectRoleLabel(msg.role), msg.project)
		project := msg.project
		return s, func() tea.Msg { return openProjectMembersMsg{project: project, toast: toast} }
	}
	switch {
	case IsAdminConflict(msg.err) && msg.isRobot:
		s.err = fmt.Sprintf("%s already has a grant. A robot holds one grant: remove it first.", msg.username)
	default:
		s.err = fmt.Sprintf("Could not grant role: %s.", strings.TrimRight(adminErrorDetail(msg.err), "."))
	}
	return s, nil
}

// rows are the member suggestions: the principals of the chosen type whose
// name contains what was typed.
func (s grantProjectRoleScreen) rows() []grantPrincipal {
	var out []grantPrincipal
	for _, principal := range filterGrantPrincipals(s.principals, s.member) {
		if principal.Kind == s.kind {
			out = append(out, principal)
		}
	}
	return out
}

// robotAdmin is the one combination the form refuses itself.
func (s grantProjectRoleScreen) robotAdmin() bool {
	return s.kind == grantKindRobot && s.role == domainauth.RepoRoleAdmin
}

func (s grantProjectRoleScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if s.applying {
		return s, nil, true
	}
	if isEscKey(msg) {
		project := s.project
		return s, func() tea.Msg { return openProjectMembersMsg{project: project} }, true
	}
	rows := s.rows()
	switch {
	case isTabKey(msg):
		s.focus = (s.focus + 1) % grantProjectFieldCount
		return s, nil, true
	case isShiftTabKey(msg):
		s.focus = (s.focus + grantProjectFieldCount - 1) % grantProjectFieldCount
		return s, nil, true
	case isEnterKey(msg):
		return s.enter(env, rows)
	}
	switch s.focus {
	case grantProjectFieldKind:
		switch {
		case isMoveLeftKey(msg):
			s = s.withKind(grantKindUser)
		case isMoveRightKey(msg):
			s = s.withKind(grantKindRobot)
		}
	case grantProjectFieldMember:
		switch {
		case isArrowUpKey(msg):
			s.suggestion = boundedIndex(boundedIndex(s.suggestion, len(rows))-1, len(rows))
		case isArrowDownKey(msg):
			s.suggestion = boundedIndex(boundedIndex(s.suggestion, len(rows))+1, len(rows))
		default:
			if typed := typeInto(s.member, msg, false); typed != s.member {
				s.member, s.suggestion, s.err = typed, 0, ""
			}
		}
	case grantProjectFieldRole:
		index := grantRoleIndex(s.role)
		switch {
		case isMoveLeftKey(msg):
			index = boundedIndex(index-1, len(grantRoleOptions))
		case isMoveRightKey(msg):
			index = boundedIndex(index+1, len(grantRoleOptions))
		}
		s.role, s.err = grantRoleOptions[index].Role, ""
	}
	return s, nil, true
}

// withKind switches between User and Robot, dropping a member typed for the
// other kind only when it no longer names anyone of the new one.
func (s grantProjectRoleScreen) withKind(kind string) grantProjectRoleScreen {
	if s.kind != kind {
		s.kind, s.suggestion, s.err = kind, 0, ""
	}
	return s
}

// enter accepts the highlighted suggestion while the Member field has focus;
// anywhere else it grants the role.
func (s grantProjectRoleScreen) enter(env screenEnv, rows []grantPrincipal) (adminScreen, tea.Cmd, bool) {
	if s.focus == grantProjectFieldMember {
		if len(rows) > 0 {
			s.member = rows[boundedIndex(s.suggestion, len(rows))].Name
		}
		if strings.TrimSpace(s.member) == "" {
			s.err = "Pick who gets the role first."
			return s, nil, true
		}
		s.focus, s.suggestion, s.err = grantProjectFieldRole, 0, ""
		return s, nil, true
	}
	name := strings.TrimSpace(s.member)
	switch {
	case name == "":
		s.err = "Pick who gets the role first."
		return s, nil, true
	case s.robotAdmin():
		// Blocked here and refused by the API too: nothing is sent.
		return s, nil, true
	}
	s.applying, s.err = true, ""
	input := ports.AdminPutProjectGrantInput{Project: s.project, Username: name, Role: s.role}
	return s, grantProjectRoleApplyCmd(env, s.gen, input, s.kind == grantKindRobot), true
}
