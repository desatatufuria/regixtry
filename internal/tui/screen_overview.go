package tui

import (
	"context"
	"errors"
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// overviewScanSummaryLimit is how many repositories' latest scan the
// Overview asks for: the same default the Scan runs screen uses, and the
// server returns them severity-ordered, so a repository with critical
// findings is never crowded out by cleaner ones.
const overviewScanSummaryLimit = 25

// overviewScreen is screenAdminOverview: the post-login landing screen
// (tui-v2-redesign S3, the "Admin workspace" board). It renders inside the
// application shell; up/down move the sidebar selection, enter opens the
// selected item, and g, r and t are the three quick actions.
type overviewScreen struct {
	// selected indexes shellNavItems(): the sidebar row up/down moved to.
	selected int
	loaded   bool

	// robotsWithoutAccess holds the usernames of robots with no repository
	// grant (AdminRobot.Repository is empty).
	robotsWithoutAccess []string
	robotsErr           string
	// criticalImages counts the repositories whose latest scan found at
	// least one critical vulnerability.
	criticalImages int
	imagesErr      string
}

func newOverviewScreen() overviewScreen { return overviewScreen{} }

func (s overviewScreen) ID() screen { return screenAdminOverview }

func (s overviewScreen) Keys() screenKeys { return overviewKeys }

// Init loads what the "Needs attention" panel is built from.
func (s overviewScreen) Init(env screenEnv) tea.Cmd { return overviewLoadCmd(env) }

func (s overviewScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(typed)
	case overviewLoadedMsg:
		return s.applyLoaded(typed), nil, false
	}
	return s, nil, false
}

func (s overviewScreen) updateKey(msg keyMsg) (adminScreen, tea.Cmd, bool) {
	items := shellNavItems()
	switch {
	case isEscKey(msg):
		return s, returnToInspectionCmd(), true
	// Only the arrows move: j and k are not special here.
	case isArrowUpKey(msg):
		s.selected = boundedIndex(s.selected-1, len(items))
	case isArrowDownKey(msg):
		s.selected = boundedIndex(s.selected+1, len(items))
	case isEnterKey(msg):
		return s, overviewOpenItem(items[boundedIndex(s.selected, len(items))].ID), true
	case isRuneKey(msg, 'g'):
		return s, openGrantAccess("", screenAdminOverview), true
	case isRuneKey(msg, 'r'):
		return s, overviewOpen(overviewOpenCreateRobot), true
	case isRuneKey(msg, 't'):
		return s, overviewOpen(overviewOpenUsersForToken), true
	}
	return s, nil, true
}

// selectedID is the sidebar item the cursor is on.
func (s overviewScreen) selectedID() shellNavID {
	items := shellNavItems()
	return items[boundedIndex(s.selected, len(items))].ID
}

// overviewOpenItem is what Enter does on a sidebar item: it opens the
// existing screen the item stands for.
func overviewOpenItem(id shellNavID) tea.Cmd {
	switch id {
	case shellNavUsers:
		return navigate(screenAdminUsers)
	case shellNavRobots:
		return overviewOpen(overviewOpenRobots)
	case shellNavRepositoryAccess:
		return openGrantAccess("", screenAdminOverview)
	case shellNavImageScanning:
		return navigate(screenSecurityTrivy)
	case shellNavSecretScanning:
		return navigate(screenSecurityGitleaksConfig)
	case shellNavSigningPolicy:
		return navigate(screenSecuritySigningConfig)
	case shellNavScanRuns:
		return navigate(screenAdminScanRuns)
	case shellNavSecretFindings:
		return navigate(screenAdminSecretFindings)
	case shellNavUpdateChannel:
		return navigate(screenAdminUpdateChannel)
	}
	return nil
}

// overviewOpenAction names the destinations that need Model state to open
// (a load, a form reset, a status line): the screen asks, the Model does.
type overviewOpenAction int

const (
	overviewOpenRobots overviewOpenAction = iota
	overviewOpenCreateRobot
	overviewOpenUsersForToken
)

type overviewOpenMsg struct{ action overviewOpenAction }

func overviewOpen(action overviewOpenAction) tea.Cmd {
	return func() tea.Msg { return overviewOpenMsg{action: action} }
}

// overviewUsersTokenStatus is the one line `t` leaves on the Users list:
// tokens are per principal, so the operator picks one first.
const overviewUsersTokenStatus = "Pick a user, then press t to issue a token"

// overviewLoadedMsg carries the two reads behind "Needs attention".
type overviewLoadedMsg struct {
	robots       []ports.AdminRobot
	robotsErr    error
	summaries    []ports.RepositoryScanSummary
	summariesErr error
}

// expiredReason lets Model.Update turn an expired admin session found by
// either read into the usual re-login.
func (m overviewLoadedMsg) expiredReason() (string, bool) {
	return expiredReasonOf(m.robotsErr, m.summariesErr)
}

func (s overviewScreen) applyLoaded(msg overviewLoadedMsg) overviewScreen {
	s.loaded = true
	s.robotsWithoutAccess, s.robotsErr = nil, ""
	s.criticalImages, s.imagesErr = 0, ""
	if msg.robotsErr != nil {
		s.robotsErr = msg.robotsErr.Error()
	}
	for _, robot := range msg.robots {
		if robot.Repository == "" {
			s.robotsWithoutAccess = append(s.robotsWithoutAccess, robot.Username)
		}
	}
	if msg.summariesErr != nil {
		s.imagesErr = msg.summariesErr.Error()
	}
	for _, summary := range msg.summaries {
		if summary.Run.Critical > 0 {
			s.criticalImages++
		}
	}
	return s
}

func overviewLoadCmd(env screenEnv) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			unavailable := errors.New("admin API is unavailable for this session")
			return overviewLoadedMsg{robotsErr: unavailable, summariesErr: unavailable}
		}
		robots, robotsErr := client.ListRobots(context.Background(), session)
		summaries, summariesErr := client.ListRepositoryScanSummaries(context.Background(), session, overviewScanSummaryLimit)
		return overviewLoadedMsg{robots: robots, robotsErr: robotsErr, summaries: summaries, summariesErr: summariesErr}
	}
}

// attentionTone picks the color of a "Needs attention" row.
type attentionTone int

const (
	attentionWarning attentionTone = iota
	attentionCritical
)

// attentionItem is one "Needs attention" row: what is wrong and the key that
// deals with it.
type attentionItem struct {
	tone attentionTone
	text string
	hint string
}

// attentionItems lists everything that needs the operator, most urgent
// source first, in the board's order.
func (s overviewScreen) attentionItems() []attentionItem {
	items := s.expiringTokenItems()
	for _, name := range s.robotsWithoutAccess {
		items = append(items, attentionItem{tone: attentionWarning, text: fmt.Sprintf("Robot %s has no repository access", name), hint: "g grant"})
	}
	switch {
	case s.criticalImages == 1:
		items = append(items, attentionItem{tone: attentionCritical, text: "1 image has critical findings", hint: "enter open"})
	case s.criticalImages > 1:
		items = append(items, attentionItem{tone: attentionCritical, text: fmt.Sprintf("%d images have critical findings", s.criticalImages), hint: "enter open"})
	}
	return items
}

// expiringTokenItems is the seam for "Token <name> expires in N days": the
// next wave fills it from GET /admin/v1/admin-tokens/expiring. Until that
// endpoint exists the Overview reports no expiring tokens.
func (s overviewScreen) expiringTokenItems() []attentionItem { return nil }

var overviewKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
	key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "grant access")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "robot")),
	key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "token")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
}}
