package tui

import (
	"charm.land/lipgloss/v2"
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// overviewScanSummaryLimit is how many repositories' latest scan the
// Overview asks for: the same default the Scan runs screen uses, and the
// server returns them severity-ordered, so a repository with critical
// findings is never crowded out by cleaner ones.
const overviewScanSummaryLimit = 25

const (
	// overviewExpiringWithin and overviewExpiringLimit are the window and
	// size of the one expiring-tokens read: tokens that stop working within a
	// week, at most 100 of them (more shows as "100+").
	overviewExpiringWithin = 7 * lifetimeDay
	overviewExpiringLimit  = 100
	// overviewEventsLimit is the one page of the access history behind the
	// Recent changes panel; the panel shows as many rows as fit.
	overviewEventsLimit = 20
)

// overviewGenerations hands out the unique id of every Overview instance, so
// a load that finishes after the screen was replaced (a new login) is dropped.
var overviewGenerations atomic.Uint64

// overviewScreen is screenAdminOverview: the post-login landing screen
// (tui-v2-redesign S3, the "Admin workspace" board). It renders inside the
// application shell; up/down move the sidebar selection, enter opens the
// selected item, and g, r and t are the three quick actions.
type overviewScreen struct {
	gen uint64
	// selected indexes shellNavItems(): the sidebar row up/down moved to.
	selected int
	loaded   bool
	// attentionFocus is 0 while the arrows drive the sidebar and i+1 while Tab
	// has put the selection on attention row i.
	attentionFocus int

	// robotsWithoutAccess holds the usernames of robots with no repository
	// grant (AdminRobot.Repository is empty).
	robotsWithoutAccess []string
	robotsErr           string
	// criticalImages counts the repositories whose latest scan found at
	// least one critical vulnerability.
	criticalImages int
	// imagesCapped is set when every repository the read returned (a full
	// overviewScanSummaryLimit) has critical findings: the true count may be
	// larger than criticalImages.
	imagesCapped bool
	imagesErr    string

	// robotsWithoutAccessIDs is parallel to robotsWithoutAccess: the id the
	// Grant access screen preselects.
	robotsWithoutAccessIDs []string

	// expiringCount is how many tokens expire within overviewExpiringWithin;
	// soonest is the one that expires first, valid when expiringCount > 0.
	expiringCount  int
	expiringCapped bool
	soonest        ports.AdminExpiringToken
	expiringErr    string

	events    []ports.AdminAccessEvent
	eventsErr string
}

func newOverviewScreen() overviewScreen {
	return overviewScreen{gen: overviewGenerations.Add(1)}
}

func (s overviewScreen) ID() screen { return screenAdminOverview }

func (s overviewScreen) Keys() screenKeys { return overviewKeys }

// Init loads what the "Needs attention" panel is built from.
func (s overviewScreen) Init(env screenEnv) tea.Cmd { return overviewLoadCmd(env, s.gen) }

func (s overviewScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case overviewLoadedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		return s.applyLoaded(typed), nil, false
	}
	return s, nil, false
}

func (s overviewScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	items := shellNavItems()
	switch {
	case isEscKey(msg):
		return s, returnToInspectionCmd(), true
	// Only the arrows move: j and k are not special here.
	case isArrowUpKey(msg):
		s.attentionFocus = 0
		s.selected = boundedIndex(s.selected-1, len(items))
	case isArrowDownKey(msg):
		s.attentionFocus = 0
		s.selected = boundedIndex(s.selected+1, len(items))
	case isTabKey(msg):
		s.attentionFocus = cycleIndex(s.attentionFocus+1, len(s.attentionItems(env.now()))+1)
	case isShiftTabKey(msg):
		s.attentionFocus = cycleIndex(s.attentionFocus-1, len(s.attentionItems(env.now()))+1)
	case isEnterKey(msg):
		if row := s.attentionFocus - 1; row >= 0 {
			if attention := s.attentionItems(env.now()); row < len(attention) {
				return s, attention[row].open, true
			}
		}
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
		return openRepoAccess()
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

// overviewLoadedMsg carries the four reads behind the Overview. Each fails on
// its own: one error never blanks the others.
type overviewLoadedMsg struct {
	gen          uint64
	robots       []ports.AdminRobot
	robotsErr    error
	summaries    []ports.RepositoryScanSummary
	summariesErr error
	expiring     []ports.AdminExpiringToken
	expiringErr  error
	events       []ports.AdminAccessEvent
	eventsErr    error
}

// expiredReason lets Model.Update turn an expired admin session found by
// any read into the usual re-login.
func (m overviewLoadedMsg) expiredReason() (string, bool) {
	return expiredReasonOf(m.robotsErr, m.summariesErr, m.expiringErr, m.eventsErr)
}

func (s overviewScreen) applyLoaded(msg overviewLoadedMsg) overviewScreen {
	s.loaded = true
	s.robotsWithoutAccess, s.robotsWithoutAccessIDs, s.robotsErr = nil, nil, errText(msg.robotsErr)
	s.criticalImages, s.imagesCapped, s.imagesErr = 0, false, errText(msg.summariesErr)
	s.expiringCount, s.expiringCapped, s.soonest, s.expiringErr = 0, false, ports.AdminExpiringToken{}, errText(msg.expiringErr)
	s.events, s.eventsErr = msg.events, errText(msg.eventsErr)
	for _, robot := range msg.robots {
		if robot.Enabled && robot.Repository == "" {
			s.robotsWithoutAccess = append(s.robotsWithoutAccess, robot.Username)
			s.robotsWithoutAccessIDs = append(s.robotsWithoutAccessIDs, robot.ID)
		}
	}
	for _, summary := range msg.summaries {
		if summary.Run.Critical > 0 {
			s.criticalImages++
		}
	}
	s.imagesCapped = len(msg.summaries) == overviewScanSummaryLimit && s.criticalImages == overviewScanSummaryLimit
	for i, token := range msg.expiring {
		if i == 0 || token.ExpiresAt.Before(s.soonest.ExpiresAt) {
			s.soonest = token
		}
	}
	s.expiringCount = len(msg.expiring)
	s.expiringCapped = s.expiringCount >= overviewExpiringLimit
	if s.attentionFocus > len(s.attentionItems(time.Time{})) {
		s.attentionFocus = 0
	}
	return s
}

func overviewLoadCmd(env screenEnv, gen uint64) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			unavailable := errors.New("admin API is unavailable for this session")
			return overviewLoadedMsg{gen: gen, robotsErr: unavailable, summariesErr: unavailable, expiringErr: unavailable, eventsErr: unavailable}
		}
		ctx := context.Background()
		msg := overviewLoadedMsg{gen: gen}
		msg.robots, msg.robotsErr = client.ListRobots(ctx, session)
		msg.summaries, msg.summariesErr = client.ListRepositoryScanSummaries(ctx, session, overviewScanSummaryLimit)
		msg.expiring, msg.expiringErr = client.ListExpiringTokens(ctx, session, overviewExpiringWithin, overviewExpiringLimit)
		msg.events, msg.eventsErr = client.ListAccessEvents(ctx, session, overviewEventsLimit)
		return msg
	}
}

// attentionTone picks the color of a "Needs attention" row.
type attentionTone int

const (
	attentionWarning attentionTone = iota
	attentionCritical
)

// attentionItem is one "Needs attention" row: what is wrong and, when a key
// deals with it, that key.
type attentionItem struct {
	tone attentionTone
	text string
	hint string
	// fit, when set, words the row for a width, shortening what matters
	// least (a name) before what matters most (a time left).
	fit func(width int) string
	// open is what Enter does on the row once Tab selected it.
	open tea.Cmd
}

// attentionItems lists everything that needs the operator, most urgent
// source first, in the board's order.
func (s overviewScreen) attentionItems(now time.Time) []attentionItem {
	var items []attentionItem
	if s.expiringCount > 0 {
		items = append(items, attentionItem{tone: attentionWarning, text: s.expiringText(now), fit: func(w int) string { return s.expiringTextFit(now, w) }, open: overviewOpenSoonestToken(s.soonest)})
	}
	for i, name := range s.robotsWithoutAccess {
		items = append(items, attentionItem{tone: attentionWarning, text: fmt.Sprintf("Robot %s has no repository access", name), hint: "g grant", open: openGrantAccess(s.robotsWithoutAccessIDs[i], screenAdminOverview)})
	}
	images := navigate(screenSecurityTrivy)
	switch {
	case s.imagesCapped:
		items = append(items, attentionItem{tone: attentionCritical, text: fmt.Sprintf("%d+ images have critical findings", s.criticalImages), open: images})
	case s.criticalImages == 1:
		items = append(items, attentionItem{tone: attentionCritical, text: "1 image has critical findings", open: images})
	case s.criticalImages > 1:
		items = append(items, attentionItem{tone: attentionCritical, text: fmt.Sprintf("%d images have critical findings", s.criticalImages), open: images})
	}
	return items
}

// expiringText is the "Tokens expiring soon" row: how many, and whose token
// goes first. The time left is hours or days, never seconds.
func (s overviewScreen) expiringText(now time.Time) string {
	return s.expiringTextFit(now, 0)
}

// expiringTextFit is expiringText in width columns (0 is unlimited): the
// owner's name is shortened with an ellipsis before the time left is touched.
func (s overviewScreen) expiringTextFit(now time.Time, width int) string {
	count := fmt.Sprint(s.expiringCount)
	if s.expiringCapped {
		count += "+"
	}
	owner := sanitizeFieldText(s.soonest.Username, true)
	left := remainingText(s.soonest.ExpiresAt.Sub(now))
	text := fmt.Sprintf("Tokens expiring soon: %s. Soonest: %s in %s", count, owner, left)
	if width <= 0 || lipgloss.Width(text) <= width {
		return text
	}
	if room := width - lipgloss.Width(text) + lipgloss.Width(owner); room >= 4 {
		return fmt.Sprintf("Tokens expiring soon: %s. Soonest: %s in %s", count, grantTruncate(owner, room), left)
	}
	return grantTruncate(text, width)
}

// overviewOpenSoonestToken opens the Tokens screen of the principal whose
// token expires first: a user from the Users path, a robot from Robots.
func overviewOpenSoonestToken(token ports.AdminExpiringToken) tea.Cmd {
	if token.IsRobot {
		return openTokens(token.UserID, token.Username, true)
	}
	return openUserScreen(openUserScreenMsg{to: screenAdminEditUserTokens, userID: token.UserID, username: token.Username})
}

var overviewKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "attention")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
	key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "grant access")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "robot")),
	key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "token")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
}}
