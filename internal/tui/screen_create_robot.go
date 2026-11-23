package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// createRobotScreen is "Admin > Access > Robots > New robot" (tui-v2-redesign
// S3, the Create robot board): a name, a repository picked from a suggestion
// list (a not-yet-pushed name is allowed), a role and the token's lifetime
// with its exact expiry. After creation the same screen shows the one-time
// secret; leaving it drops the secret for good.
type createRobotScreen struct {
	gen uint64

	name       string
	repository string
	// scope is where the robot's one grant lies: on a repository (the default)
	// or on a whole project. The two are mutually exclusive: the robot is
	// created with one or the other.
	scope robotScope
	// project is the typed or picked project of scope Project; projects are
	// read when the scope first becomes Project.
	project      string
	projects     []ports.ProjectSummary
	projectsRead projectsRead
	projectsErr  string
	// suggestion indexes the repository (or project) suggestion rows; only the
	// arrows move it, because j, k and q are typed text in the fields.
	suggestion int
	role       domainauth.RepoRole
	lifetime   tokenLifetime
	focus      robotField

	// creating is true from the Enter that issued the create command until its
	// result arrives; Enter is ignored meanwhile. Esc still leaves: the Model
	// then shows the secret anyway when the robot was created after all.
	creating bool
	// err is the validation or server message shown above the buttons.
	err string

	done    bool
	created createdRobot
	// reveal shows the bare screen with the secret alone on its row; copied
	// says the c key was pressed (the note it shows holds no secret).
	reveal, copied bool
}

// robotScope is the scope of a new robot's grant.
type robotScope int

const (
	robotScopeRepository robotScope = iota
	robotScopeProject
)

// projectsRead is how far the project list read has got.
type projectsRead int

const (
	projectsNotRead projectsRead = iota
	projectsReading
	projectsRead_
)

type robotField int

const (
	robotFieldName robotField = iota
	robotFieldScope
	robotFieldRepository
	robotFieldRole
	robotFieldLifetime
	robotFieldCount
)

// createdRobot is what the screen shows once the robot exists.
type createdRobot struct {
	id         string
	name       string
	repository string
	// project is set instead of repository for a robot on a whole project.
	project  string
	role     domainauth.RepoRole
	accessor string
	secret   string
	expires  time.Time
}

func newCreateRobotScreen() createRobotScreen {
	return createRobotScreen{gen: accessGenerations.Add(1), role: domainauth.RepoRoleReader}
}

func (s createRobotScreen) ID() screen { return screenAdminCreateRobot }

func (s createRobotScreen) generation() uint64 { return s.gen }

func (s createRobotScreen) Keys() screenKeys {
	if s.done {
		return secretFooterKeys
	}
	pick := "pick repository"
	if s.scope == robotScopeProject {
		pick = "pick project"
	}
	return screenKeys{short: []key.Binding{
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", pick)),
		key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "scope, role, lifetime")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "create")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}}
}

func (s createRobotScreen) Init(screenEnv) tea.Cmd { return nil }

type robotCreatedMsg struct {
	gen     uint64
	input   ports.AdminCreateRobotInput
	created ports.AdminCreatedRobot
	err     error
}

func (m robotCreatedMsg) accessSlot() screenSlot        { return slotCreateRobot }
func (m robotCreatedMsg) accessGen() uint64             { return m.gen }
func (m robotCreatedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

func (s createRobotScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case robotCreatedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		next, cmd := s.applyCreated(typed)
		return next, cmd, false
	case createRobotProjectsMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		s.projectsRead = projectsRead_
		s.projects, s.projectsErr = typed.projects, ""
		if typed.err != nil {
			s.projects, s.projectsErr = nil, strings.TrimRight(adminErrorDetail(typed.err), ".")
		}
		return s, nil, false
	}
	return s, nil, false
}

// createRobotProjectsMsg is the project list the Project scope picks from.
type createRobotProjectsMsg struct {
	gen      uint64
	projects []ports.ProjectSummary
	err      error
}

func (createRobotProjectsMsg) projectScreenMsg()               {}
func (createRobotProjectsMsg) accessSlot() screenSlot          { return slotCreateRobot }
func (m createRobotProjectsMsg) accessGen() uint64             { return m.gen }
func (m createRobotProjectsMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

func createRobotProjectsCmd(env screenEnv, gen uint64) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return createRobotProjectsMsg{gen: gen, err: errAdminUnavailable}
		}
		projects, err := client.ListProjects(context.Background(), session)
		return createRobotProjectsMsg{gen: gen, projects: projects, err: err}
	}
}

// projectRows are the projects the Project scope suggests: those whose name
// contains what was typed.
func (s createRobotScreen) projectRows() []ports.ProjectSummary {
	needle := strings.ToLower(strings.TrimSpace(s.project))
	var rows []ports.ProjectSummary
	for _, project := range s.projects {
		if needle == "" || strings.Contains(strings.ToLower(project.Name), needle) {
			rows = append(rows, project)
		}
	}
	return rows
}

// rowCount is how many suggestion rows the current scope shows.
func (s createRobotScreen) rowCount(repositories []grantRepoRow) int {
	if s.scope == robotScopeProject {
		return len(s.projectRows())
	}
	return len(repositories)
}

// projectAdminRefused is the one scope and role the form refuses itself.
func (s createRobotScreen) projectAdminRefused() bool {
	return s.scope == robotScopeProject && s.role == domainauth.RepoRoleAdmin
}

func (s createRobotScreen) applyCreated(msg robotCreatedMsg) (createRobotScreen, tea.Cmd) {
	s.creating = false
	if msg.err != nil {
		s.err = msg.err.Error()
		if msg.input.Project != "" {
			if IsAdminNotFound(msg.err) {
				s.err = unknownProjectText(msg.input.Project)
			} else {
				s.err = fmt.Sprintf("Could not create robot: %s.", strings.TrimRight(adminErrorDetail(msg.err), "."))
			}
		}
		return s, nil
	}
	s.done, s.err = true, ""
	s.created = createdRobotFrom(msg)
	return s, nil
}

func firstText(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (s createRobotScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if s.done {
		// Enter and Esc both mean Done: back to the list, secret dropped.
		switch {
		case isEnterKey(msg) || isEscKey(msg):
			return newCreateRobotScreen(), openRobots(s.created.id), true
		case isCommandRune(msg, 'c'):
			s.copied = true
			return s, copySecretCmd(s.created.secret), true
		case isCommandRune(msg, 'v'):
			s.reveal = !s.reveal
		}
		return s, nil, true
	}
	if s.creating {
		if isEscKey(msg) {
			return s, openRobots(""), true
		}
		return s, nil, true
	}
	if isEscKey(msg) {
		return s, openRobots(""), true
	}

	rows := grantRepoRows(env.KnownRepositories, s.repository)
	switch {
	case isTabKey(msg):
		s.focus = (s.focus + 1) % robotFieldCount
		return s, nil, true
	case isShiftTabKey(msg):
		s.focus = (s.focus + robotFieldCount - 1) % robotFieldCount
		return s, nil, true
	case isEnterKey(msg):
		return s.enter(env, rows)
	}

	switch s.focus {
	case robotFieldName:
		s.name = typeInto(s.name, msg, true)
	case robotFieldScope:
		switch {
		case isMoveLeftKey(msg):
			return s.withScope(env, robotScopeRepository)
		case isMoveRightKey(msg):
			return s.withScope(env, robotScopeProject)
		}
	case robotFieldRepository:
		if s.scope == robotScopeProject {
			count := len(s.projectRows())
			switch {
			case isArrowUpKey(msg):
				s.suggestion = boundedIndex(boundedIndex(s.suggestion, count)-1, count)
			case isArrowDownKey(msg):
				s.suggestion = boundedIndex(boundedIndex(s.suggestion, count)+1, count)
			default:
				if typed := typeInto(s.project, msg, false); typed != s.project {
					s.project, s.suggestion, s.err = typed, 0, ""
				}
			}
			break
		}
		switch {
		case isArrowUpKey(msg):
			s.suggestion = boundedIndex(boundedIndex(s.suggestion, len(rows))-1, len(rows))
		case isArrowDownKey(msg):
			s.suggestion = boundedIndex(boundedIndex(s.suggestion, len(rows))+1, len(rows))
		default:
			if typed := typeInto(s.repository, msg, false); typed != s.repository {
				s.repository = typed
				s.suggestion = preferredSuggestion(env.KnownRepositories, typed)
			}
		}
	case robotFieldRole:
		index := grantRoleIndex(s.role)
		switch {
		case isMoveLeftKey(msg):
			index = boundedIndex(index-1, len(grantRoleOptions))
		case isMoveRightKey(msg):
			index = boundedIndex(index+1, len(grantRoleOptions))
		}
		s.role = grantRoleOptions[index].Role
	case robotFieldLifetime:
		s.lifetime, _ = s.lifetime.handleKey(msg, lifetimeCeiling())
	}
	return s, nil, true
}

// withScope switches the robot between a repository and a project. The
// projects are read the first time the scope becomes Project.
func (s createRobotScreen) withScope(env screenEnv, scope robotScope) (adminScreen, tea.Cmd, bool) {
	if s.scope == scope {
		return s, nil, true
	}
	s.scope, s.suggestion, s.err = scope, 0, ""
	if scope == robotScopeProject && s.projectsRead == projectsNotRead {
		s.projectsRead = projectsReading
		return s, createRobotProjectsCmd(env, s.gen), true
	}
	return s, nil, true
}

// typeInto applies a typed or pasted character, or Backspace, to a field.
func typeInto(value string, msg keyMsg, spaces bool) string {
	switch {
	case isBackspaceKey(msg):
		return trimLastRune(value)
	case isTextKey(msg):
		return value + sanitizeFieldText(keyText(msg), spaces)
	}
	return value
}

// preferredSuggestion is the suggestion row to highlight after typing: the one
// named exactly what was typed, else the first.
func preferredSuggestion(known []string, typed string) int {
	for i, row := range grantRepoRows(known, typed) {
		if !row.New && row.Name == strings.TrimSpace(typed) {
			return i
		}
	}
	return 0
}

// enter accepts the highlighted suggestion while the Repository (or Project)
// field has focus; anywhere else it creates the robot.
func (s createRobotScreen) enter(env screenEnv, rows []grantRepoRow) (adminScreen, tea.Cmd, bool) {
	if s.scope == robotScopeProject {
		return s.enterProject(env)
	}
	if s.focus == robotFieldRepository {
		if len(rows) > 0 {
			s.repository = rows[boundedIndex(s.suggestion, len(rows))].Name
		}
		if strings.TrimSpace(s.repository) == "" {
			s.err = "Repository is required."
			return s, nil, true
		}
		s.focus, s.suggestion, s.err = robotFieldRole, 0, ""
		return s, nil, true
	}
	name, repository := strings.TrimSpace(s.name), strings.TrimSpace(s.repository)
	if name == "" || repository == "" {
		s.err = "Name and repository are required."
		return s, nil, true
	}
	ttl, ok := s.lifetime.duration(lifetimeCeiling())
	if !ok {
		// The form stays open; the validation message replaces the preview.
		return s, nil, true
	}
	s.creating, s.err = true, ""
	input := ports.AdminCreateRobotInput{Name: name, Repository: repository, Role: s.role, TTL: ttl}
	return s, createRobotCmd(env, s.gen, input), true
}

// enterProject is enter for scope Project. Admin is refused here (robots on a
// project are readers or writers) and nothing is sent; a project that is not
// in the list read is refused with the board's text before any request.
func (s createRobotScreen) enterProject(env screenEnv) (adminScreen, tea.Cmd, bool) {
	if s.focus == robotFieldRepository {
		if rows := s.projectRows(); len(rows) > 0 {
			s.project = rows[boundedIndex(s.suggestion, len(rows))].Name
		}
		if strings.TrimSpace(s.project) == "" {
			s.err = "Project is required."
			return s, nil, true
		}
		s.focus, s.suggestion, s.err = robotFieldRole, 0, ""
		return s, nil, true
	}
	name, project := strings.TrimSpace(s.name), strings.TrimSpace(s.project)
	switch {
	case name == "" || project == "":
		s.err = "Name and project are required."
		return s, nil, true
	case s.projectAdminRefused():
		return s, nil, true
	case s.projectsRead == projectsRead_ && s.projectsErr == "" && !s.hasProject(project):
		s.err = unknownProjectText(project)
		return s, nil, true
	}
	ttl, ok := s.lifetime.duration(lifetimeCeiling())
	if !ok {
		return s, nil, true
	}
	s.creating, s.err = true, ""
	input := ports.AdminCreateRobotInput{Name: name, Project: project, Role: s.role, TTL: ttl}
	return s, createRobotCmd(env, s.gen, input), true
}

func (s createRobotScreen) hasProject(name string) bool {
	for _, project := range s.projects {
		if project.Name == name {
			return true
		}
	}
	return false
}

func unknownProjectText(project string) string {
	return fmt.Sprintf("Project %s does not exist. Pick one from the list.", project)
}

func createRobotCmd(env screenEnv, gen uint64, input ports.AdminCreateRobotInput) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return robotCreatedMsg{gen: gen, input: input, err: errAdminUnavailable}
		}
		created, err := client.CreateRobot(context.Background(), session, input)
		return robotCreatedMsg{gen: gen, input: input, created: created, err: err}
	}
}

// createdRobotFrom is what the secret state shows for a created robot.
func createdRobotFrom(msg robotCreatedMsg) createdRobot {
	robot := msg.created.Robot
	created := createdRobot{
		id:         robot.ID,
		name:       firstText(robot.Username, msg.input.Name),
		repository: firstText(robot.Repository, msg.input.Repository),
		project:    firstText(robot.Project, msg.input.Project),
		role:       robot.Role,
		accessor:   msg.created.Accessor,
		secret:     msg.created.Secret,
		expires:    msg.created.ExpiresAt,
	}
	if created.role == "" {
		created.role = msg.input.Role
	}
	return created
}

// newClaimedRobotScreen is the secret state of a robot created by a screen
// that is gone: the credential cannot be displayed again, so it is shown
// whatever the operator was doing.
func newClaimedRobotScreen(msg robotCreatedMsg) createRobotScreen {
	s := newCreateRobotScreen()
	s.done = true
	s.created = createdRobotFrom(msg)
	return s
}
