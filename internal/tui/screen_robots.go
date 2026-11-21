package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// accessGenerations hands out the unique id of every Robots, Create robot and
// Tokens screen instance. Every async message a screen produces carries it,
// and a message with another id is dropped, so a late result can never land on
// a newer screen that reuses the slot.
var accessGenerations atomic.Uint64

// accessMsg is an async result of one of the Access screens above.
type accessMsg interface {
	accessSlot() screenSlot
	accessGen() uint64
	// expiredReason reports an expired admin session found in the result.
	expiredReason() (string, bool)
}

// robotsScreen is "Admin > Access > Robots" (tui-v2-redesign S3, the Robots
// board): the robot accounts as a table with the selected one's detail beside
// it. A robot is a user with one repository grant (ports.AdminRobot), so the
// ACCESS block shows at most one repository.
type robotsScreen struct {
	gen uint64

	robots   []ports.AdminRobot
	loaded   bool
	err      string
	selected int
	// selectID is the robot to highlight once the list first arrives.
	selectID string

	// tokens holds each robot's tokens by robot id, read one robot at a time
	// after the list. It is replaced, never edited, because Model is copied by
	// value on every Update.
	tokens       map[string][]ports.AdminToken
	tokensLoaded bool
	tokensErr    string

	// confirm is the enable/disable confirmation; its in-flight guard keeps a
	// second Enter from sending the change twice.
	confirm confirmPrompt
	// notice is the line under the table: the last action's outcome.
	notice     string
	noticeFail bool
}

func newRobotsScreen() robotsScreen {
	return robotsScreen{gen: accessGenerations.Add(1)}
}

func (s robotsScreen) ID() screen { return screenAdminRobots }

func (s robotsScreen) generation() uint64 { return s.gen }

func (s robotsScreen) Keys() screenKeys { return robotsKeys }

func (s robotsScreen) Init(env screenEnv) tea.Cmd { return robotsLoadCmd(env, s.gen) }

// selectedRobot is the robot under the cursor.
func (s robotsScreen) selectedRobot() (ports.AdminRobot, bool) {
	if len(s.robots) == 0 {
		return ports.AdminRobot{}, false
	}
	return s.robots[boundedIndex(s.selected, len(s.robots))], true
}

// withSelection moves the cursor to the robot with this id, if it is listed.
func (s robotsScreen) withSelection(id string) robotsScreen {
	if i := slices.IndexFunc(s.robots, func(r ports.AdminRobot) bool { return r.ID == id }); i >= 0 {
		s.selected = i
	}
	return s
}

// ---- messages ----

type robotsLoadedMsg struct {
	gen    uint64
	robots []ports.AdminRobot
	err    error
}

type robotsTokensLoadedMsg struct {
	gen    uint64
	tokens map[string][]ports.AdminToken
	err    error
}

type robotEnabledMsg struct {
	gen     uint64
	robotID string
	name    string
	enabled bool
	err     error
}

func (m robotsLoadedMsg) accessSlot() screenSlot        { return slotRobots }
func (m robotsLoadedMsg) accessGen() uint64             { return m.gen }
func (m robotsLoadedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

func (m robotsTokensLoadedMsg) accessSlot() screenSlot        { return slotRobots }
func (m robotsTokensLoadedMsg) accessGen() uint64             { return m.gen }
func (m robotsTokensLoadedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

func (m robotEnabledMsg) accessSlot() screenSlot        { return slotRobots }
func (m robotEnabledMsg) accessGen() uint64             { return m.gen }
func (m robotEnabledMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

// openRobotsMsg asks the parent to mount a FRESH Robots screen (reloaded, so a
// robot just created or changed shows) with the robot of selectID highlighted.
type openRobotsMsg struct{ selectID string }

func openRobots(selectID string) tea.Cmd {
	return func() tea.Msg { return openRobotsMsg{selectID: selectID} }
}

// openCreateRobotMsg asks the parent to mount a FRESH Create robot form.
type openCreateRobotMsg struct{}

func openCreateRobot() tea.Cmd {
	return func() tea.Msg { return openCreateRobotMsg{} }
}

func (s robotsScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case robotsLoadedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		return s.applyRobots(env, typed)
	case robotsTokensLoadedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		s.tokensLoaded = true
		s.tokens, s.tokensErr = typed.tokens, errText(typed.err)
		return s, nil, false
	case robotEnabledMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		s.confirm = confirmPrompt{}
		if typed.err != nil {
			s.notice, s.noticeFail = typed.err.Error(), true
			return s, nil, false
		}
		verb := "disabled"
		if typed.enabled {
			verb = "enabled"
		}
		s.notice, s.noticeFail = fmt.Sprintf("Robot %s %s.", typed.name, verb), false
		return s, robotsLoadCmd(env, s.gen), false
	}
	return s, nil, false
}

func (s robotsScreen) applyRobots(env screenEnv, msg robotsLoadedMsg) (adminScreen, tea.Cmd, bool) {
	s.loaded = true
	s.err = errText(msg.err)
	if msg.err != nil {
		return s, nil, false
	}
	keep := s.selectID
	if robot, ok := s.selectedRobot(); ok {
		keep = robot.ID
	}
	s.selectID = ""
	s.robots = slices.Clone(msg.robots)
	s.selected = boundedIndex(s.selected, len(s.robots))
	s = s.withSelection(keep)
	s.tokensLoaded = false
	return s, robotsTokensCmd(env, s.gen, s.robots), false
}

func (s robotsScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if s.confirm.Active() {
		next, cmd, _ := s.confirm.update(env, msg)
		s.confirm = next
		return s, cmd, true
	}
	robot, hasRobot := s.selectedRobot()
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminOverview), true
	case isMoveUpKey(msg):
		s.selected = boundedIndex(s.selected-1, len(s.robots))
	case isMoveDownKey(msg):
		s.selected = boundedIndex(s.selected+1, len(s.robots))
	case isCommandRune(msg, 'n'):
		return s, openCreateRobot(), true
	case isCommandRune(msg, 'r'):
		s.loaded, s.err, s.notice = false, "", ""
		return s, robotsLoadCmd(env, s.gen), true
	case !hasRobot:
		// Every remaining key acts on the selected robot.
	case isCommandRune(msg, 't') || isEnterKey(msg):
		return s, openTokens(robot.ID, robot.Username, true), true
	case isCommandRune(msg, 'g'):
		return s, openGrantAccess(robot.ID, screenAdminRobots), true
	case isCommandRune(msg, 'd') && robot.Enabled:
		s.confirm = s.enableConfirm(robot, false)
		s.notice = ""
	case isCommandRune(msg, 'e') && !robot.Enabled:
		s.confirm = s.enableConfirm(robot, true)
		s.notice = ""
	}
	return s, nil, true
}

// enableConfirm builds the confirmation for disabling or enabling a robot. The
// command is built from the env at confirm time, never at open time.
func (s robotsScreen) enableConfirm(robot ports.AdminRobot, enable bool) confirmPrompt {
	verb := "disable"
	if enable {
		verb = "enable"
	}
	gen, id, name := s.gen, robot.ID, robot.Username
	prompt := newConfirmPrompt(
		"Confirm "+verb,
		fmt.Sprintf("Confirm %s robot %q?", verb, name),
		verb,
		fmt.Sprintf("Submitting %s for %s...", verb, name),
		func(env screenEnv) tea.Cmd { return robotEnableCmd(env, gen, id, name, enable) },
	).withTarget(name, robotDetail(robot))
	if !enable {
		prompt = prompt.withConsequence(name + " can no longer authenticate.")
	}
	return prompt
}

// ---- commands ----

var errAdminUnavailable = errors.New("admin API is unavailable for this session")

func robotsLoadCmd(env screenEnv, gen uint64) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return robotsLoadedMsg{gen: gen, err: errAdminUnavailable}
		}
		robots, err := client.ListRobots(context.Background(), session)
		return robotsLoadedMsg{gen: gen, robots: robots, err: err}
	}
}

// robotsTokensCmd reads every robot's tokens, one robot at a time inside ONE
// command (this codebase avoids tea.Batch). A failed read leaves that robot
// without token data and is reported once; an expired session stops the rest.
func robotsTokensCmd(env screenEnv, gen uint64, robots []ports.AdminRobot) tea.Cmd {
	if len(robots) == 0 {
		return nil
	}
	client, session := env.Client, env.Session
	ids := make([]string, len(robots))
	for i, robot := range robots {
		ids[i] = robot.ID
	}
	return func() tea.Msg {
		tokens := make(map[string][]ports.AdminToken, len(ids))
		var firstErr error
		for _, id := range ids {
			if client == nil {
				return robotsTokensLoadedMsg{gen: gen, err: errAdminUnavailable}
			}
			list, err := client.ListUserAdminTokens(context.Background(), session, id)
			if err != nil {
				if IsAdminSessionExpired(err) {
					return robotsTokensLoadedMsg{gen: gen, err: err}
				}
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			tokens[id] = list
		}
		return robotsTokensLoadedMsg{gen: gen, tokens: tokens, err: firstErr}
	}
}

func robotEnableCmd(env screenEnv, gen uint64, robotID, name string, enable bool) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return robotEnabledMsg{gen: gen, robotID: robotID, name: name, enabled: enable, err: errAdminUnavailable}
		}
		var err error
		if enable {
			_, err = client.EnableUser(context.Background(), session, robotID)
		} else {
			_, err = client.DisableUser(context.Background(), session, robotID)
		}
		return robotEnabledMsg{gen: gen, robotID: robotID, name: name, enabled: enable, err: err}
	}
}

// ---- token facts about a robot ----

// liveTokens are the robot's tokens that were not revoked.
func liveTokens(tokens []ports.AdminToken) []ports.AdminToken {
	var live []ports.AdminToken
	for _, token := range tokens {
		if token.RevokedAt == nil {
			live = append(live, token)
		}
	}
	return live
}

// firstExpiry is the token that ends first: the soonest expiry among the
// tokens still valid at now; when every live token already expired, the
// latest one. ok is false when the robot has no live token.
func firstExpiry(tokens []ports.AdminToken, now time.Time) (time.Time, bool) {
	var soonest, latest time.Time
	found := false
	for _, token := range liveTokens(tokens) {
		found = true
		if token.ExpiresAt.After(latest) {
			latest = token.ExpiresAt
		}
		if token.ExpiresAt.After(now) && (soonest.IsZero() || token.ExpiresAt.Before(soonest)) {
			soonest = token.ExpiresAt
		}
	}
	if !found {
		return time.Time{}, false
	}
	if soonest.IsZero() {
		return latest, true
	}
	return soonest, true
}

// withoutAccess reports an enabled robot that has no repository grant: it can
// authenticate but cannot pull or push.
func withoutAccess(robot ports.AdminRobot) bool {
	return robot.Enabled && robot.Repository == ""
}

var robotsKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
	key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new robot")),
	key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tokens")),
	key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "grant access")),
	key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "disable")),
	key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "enable")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
}}

// robotDetail is the line under a robot's name in a dialog: its access.
func robotDetail(robot ports.AdminRobot) string {
	if robot.Repository == "" {
		return "no repository access"
	}
	return grantRoleLabel(robot.Role) + " on " + robot.Repository
}
