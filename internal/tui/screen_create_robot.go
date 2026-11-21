package tui

import (
	"context"
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
	// suggestion indexes the repository suggestion rows; only the arrows move
	// it, because j, k and q are typed text in the fields.
	suggestion int
	role       domainauth.RepoRole
	lifetime   tokenLifetime
	focus      robotField

	// creating is true from the Enter that issued the create command until its
	// result arrives; Enter is ignored meanwhile and Esc does not leave, so the
	// one-time secret can never be lost.
	creating bool
	// err is the validation or server message shown above the buttons.
	err string

	done    bool
	created createdRobot
}

type robotField int

const (
	robotFieldName robotField = iota
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
	role       domainauth.RepoRole
	accessor   string
	secret     string
	expires    time.Time
}

func newCreateRobotScreen() createRobotScreen {
	return createRobotScreen{gen: accessGenerations.Add(1), role: domainauth.RepoRoleReader}
}

func (s createRobotScreen) ID() screen { return screenAdminCreateRobot }

func (s createRobotScreen) generation() uint64 { return s.gen }

func (s createRobotScreen) Keys() screenKeys {
	if s.done {
		return createRobotDoneKeys
	}
	return createRobotKeys
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
		return s.applyCreated(typed), nil, false
	}
	return s, nil, false
}

func (s createRobotScreen) applyCreated(msg robotCreatedMsg) createRobotScreen {
	s.creating = false
	if msg.err != nil {
		s.err = msg.err.Error()
		return s
	}
	robot := msg.created.Robot
	s.done, s.err = true, ""
	s.created = createdRobot{
		id:         robot.ID,
		name:       firstText(robot.Username, msg.input.Name),
		repository: firstText(robot.Repository, msg.input.Repository),
		role:       msg.created.Robot.Role,
		accessor:   msg.created.Accessor,
		secret:     msg.created.Secret,
		expires:    msg.created.ExpiresAt,
	}
	if s.created.role == "" {
		s.created.role = msg.input.Role
	}
	return s
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
		if isEnterKey(msg) || isEscKey(msg) {
			return newCreateRobotScreen(), openRobots(s.created.id), true
		}
		return s, nil, true
	}
	if s.creating {
		if isEscKey(msg) {
			s.err = "Creating the robot, please wait: its secret is shown once it exists."
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
	case robotFieldRepository:
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

// enter accepts the highlighted suggestion while the Repository field has
// focus; anywhere else it creates the robot.
func (s createRobotScreen) enter(env screenEnv, rows []grantRepoRow) (adminScreen, tea.Cmd, bool) {
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

var createRobotKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "pick repository")),
	key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "role, lifetime")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "create")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}}

var createRobotDoneKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("enter", "esc"), key.WithHelp("enter", "done")),
}}
