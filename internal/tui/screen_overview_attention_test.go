package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"regixtry/internal/ports"
)

// overviewRichClient adds to overviewClient two expiring tokens (the robot's
// listed first on purpose: the soonest is alice's) and an access history with
// every kind of row.
func overviewRichClient() *fakeAdminClient {
	client := overviewClient()
	client.robots[1].ID = "r-2"
	client.expiringTokens = []ports.AdminExpiringToken{
		{ID: "t-1", Name: "ci", UserID: "r-1", Username: "ci-publisher", IsRobot: true, ExpiresAt: charFixedTime.Add(6*lifetimeDay + 3*lifetimeHour)},
		{ID: "t-2", Name: "laptop", UserID: "u-1", Username: "alice", ExpiresAt: charFixedTime.Add(5*lifetimeHour + 20*time.Minute)},
	}
	client.accessEvents = []ports.AdminAccessEvent{
		{ID: "e1", At: charFixedTime.Add(-3 * lifetimeHour), Action: "grant.put", Actor: "admin", Subject: "alice", Repository: "team/api", Role: "writer"},
		{ID: "e2", At: charFixedTime.Add(-2 * lifetimeDay), Action: "token.issued", Actor: "admin", Subject: "ci-publisher", TokenName: "ci", Detail: "30 days"},
		{ID: "e3", At: charFixedTime.Add(-30 * time.Minute), Action: "quota.changed", Actor: "admin", Subject: "bob"},
	}
	return client
}

func overviewRowsAndWidths(t *testing.T, m Model, w, h int, label string) {
	t.Helper()
	lines := strings.Split(overviewRender(m), "\n")
	if len(lines) > h {
		t.Errorf("%s %dx%d: %d rows", label, w, h, len(lines))
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > w {
			t.Errorf("%s %dx%d row %d is %d columns wide", label, w, h, i, got)
		}
	}
}

func TestOverviewLoadsExpiringTokensAndEventsInOneCallEach(t *testing.T) {
	t.Parallel()
	client := overviewRichClient()
	overviewModelAt(t, client, 150, 24)
	if client.listExpiringCalls != 1 || client.lastExpiringWithin != 7*lifetimeDay || client.lastExpiringLimit != 100 {
		t.Errorf("expiring: calls=%d within=%v limit=%d", client.listExpiringCalls, client.lastExpiringWithin, client.lastExpiringLimit)
	}
	if client.listEventsCalls != 1 || client.lastEventsLimit != 20 {
		t.Errorf("events: calls=%d limit=%d", client.listEventsCalls, client.lastEventsLimit)
	}
}

func TestOverviewNeedsAttentionShowsTheExpiringTokens(t *testing.T) {
	t.Parallel()
	out := overviewRender(overviewModelAt(t, overviewRichClient(), 200, 40))
	if !strings.Contains(out, "Tokens expiring soon: 2. Soonest: alice in 5 hours") {
		t.Errorf("expiring row missing or wrong (soonest must be alice, in hours):\n%s", out)
	}
	if strings.Contains(out, "seconds") || strings.Contains(out, "minutes") {
		t.Errorf("time left is not in hours or days:\n%s", out)
	}
	client := overviewRichClient()
	client.expiringTokens = client.expiringTokens[:1]
	if out := overviewRender(overviewModelAt(t, client, 200, 40)); !strings.Contains(out, "Tokens expiring soon: 1. Soonest: ci-publisher in 6 days") {
		t.Errorf("one token in days:\n%s", out)
	}
	client.expiringTokens = nil
	if out := overviewRender(overviewModelAt(t, client, 200, 40)); strings.Contains(out, "Tokens expiring soon") {
		t.Errorf("row shown without tokens:\n%s", out)
	}
	for i := range 100 {
		client.expiringTokens = append(client.expiringTokens, ports.AdminExpiringToken{ID: fmt.Sprint(i), UserID: "u-1", Username: "alice", ExpiresAt: charFixedTime.Add(lifetimeDay)})
	}
	if out := overviewRender(overviewModelAt(t, client, 200, 40)); !strings.Contains(out, "Tokens expiring soon: 100+.") {
		t.Errorf("a full page is not shown as 100+:\n%s", out)
	}
}

func TestOverviewTabSelectsAttentionRowsAndEnterOpensThem(t *testing.T) {
	t.Parallel()
	m := overviewModelAt(t, overviewRichClient(), 150, 24)
	if got := overviewScreenOf(t, m).attentionFocus; got != 0 {
		t.Fatalf("focus starts on %d", got)
	}
	// Row 1: the expiring tokens, of alice (a user).
	m = overviewPress(t, m, testKeyTab(), testKeyEnter())
	tokens, _ := m.adminScreens[slotTokens].(tokensScreen)
	if m.screen != screenAdminEditUserTokens || tokens.principalID != "u-1" || tokens.principalName != "alice" || tokens.robot {
		t.Fatalf("expiring row opened screen=%q tokens=%#v, want alice's tokens", m.screen, tokens)
	}
}

func TestOverviewEnterOnARobotsExpiringTokenOpensRobotTokens(t *testing.T) {
	t.Parallel()
	client := overviewRichClient()
	client.expiringTokens = client.expiringTokens[:1]
	m := overviewPress(t, overviewModelAt(t, client, 150, 24), testKeyTab(), testKeyEnter())
	tokens, _ := m.adminScreens[slotTokens].(tokensScreen)
	if m.screen != screenAdminEditUserTokens || tokens.principalID != "r-1" || tokens.principalName != "ci-publisher" || !tokens.robot {
		t.Fatalf("screen=%q tokens=%#v, want the robot's tokens", m.screen, tokens)
	}
}

func TestOverviewTabWalksTheRowsAndBackToTheShortcuts(t *testing.T) {
	t.Parallel()
	m := overviewModelAt(t, overviewRichClient(), 150, 24) // rows: expiring, robot, images
	want := []int{1, 2, 3, 0, 1}
	for i, focus := range want {
		m = overviewPress(t, m, testKeyTab())
		if got := overviewScreenOf(t, m).attentionFocus; got != focus {
			t.Fatalf("Tab #%d: focus %d, want %d", i+1, got, focus)
		}
	}
	m = overviewPress(t, m, testKeyShiftTab(), testKeyShiftTab())
	if got := overviewScreenOf(t, m).attentionFocus; got != 3 {
		t.Fatalf("Shift+Tab from the shortcuts: focus %d, want the last row", got)
	}
}

func TestOverviewEnterOnEachAttentionRow(t *testing.T) {
	t.Parallel()
	m := overviewPress(t, overviewModelAt(t, overviewRichClient(), 150, 24), testKeyTab(), testKeyTab(), testKeyEnter())
	if m.screen != screenAdminGrantAccess {
		t.Fatalf("robot row opened %q, want Grant access", m.screen)
	}
	grant, _ := m.adminScreens[slotGrantAccess].(grantAccessScreen)
	if grant.chosenID != "r-2" || grant.returnTo != screenAdminOverview {
		t.Fatalf("grant chosen=%q returnTo=%q, want the robot r-2 and the Overview", grant.chosenID, grant.returnTo)
	}
	m = overviewPress(t, overviewModelAt(t, overviewRichClient(), 150, 24), testKeyShiftTab(), testKeyEnter())
	if m.screen != screenSecurityTrivy {
		t.Fatalf("images row opened %q, want Image scanning", m.screen)
	}
}

func TestOverviewArrowsReturnTheSelectionToTheSidebar(t *testing.T) {
	t.Parallel()
	m := overviewPress(t, overviewModelAt(t, overviewRichClient(), 150, 24), testKeyTab(), testKeyDown())
	if s := overviewScreenOf(t, m); s.attentionFocus != 0 || s.selected != 1 {
		t.Fatalf("focus=%d selected=%d", s.attentionFocus, s.selected)
	}
	m = overviewPress(t, m, testKeyEnter())
	if m.screen != screenAdminUsers {
		t.Fatalf("Enter after the arrows opened %q, want the sidebar item (Users)", m.screen)
	}
}

func TestOverviewTabWithNothingToSelectDoesNothing(t *testing.T) {
	t.Parallel()
	client := &fakeAdminClient{repositoryScanSummaries: []ports.RepositoryScanSummary{}}
	m := overviewPress(t, overviewModelAt(t, client, 150, 24), testKeyTab(), testKeyShiftTab(), testKeyEnter())
	if s := overviewScreenOf(t, m); s.attentionFocus != 0 || m.screen != screenAdminOverview {
		t.Fatalf("focus=%d screen=%q", s.attentionFocus, m.screen)
	}
}

func TestOverviewKeepsItsOtherKeysWhileARowIsSelected(t *testing.T) {
	t.Parallel()
	for key, want := range map[string]screen{"g": screenAdminGrantAccess, "r": screenAdminCreateRobot, "t": screenAdminUsers} {
		m := overviewPress(t, overviewModelAt(t, overviewRichClient(), 150, 24), testKeyTab(), testKeyRunes(key))
		if m.screen != want {
			t.Errorf("%s with a row selected opened %q, want %q", key, m.screen, want)
		}
	}
}

func TestOverviewSelectedRowIsMarkedAtEverySize(t *testing.T) {
	t.Parallel()
	sizes := []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}}
	for _, size := range sizes {
		m := overviewPress(t, overviewModelAt(t, overviewRichClient(), size.w, size.h), testKeyTab(), testKeyTab())
		marked := 0
		for _, line := range strings.Split(overviewRender(m), "\n") {
			if strings.Contains(line, "▸ Robot gh-actions") && strings.Contains(line, "enter open") {
				marked++
			}
		}
		if marked != 1 {
			t.Errorf("%dx%d: selected row marked %d times:\n%s", size.w, size.h, marked, overviewRender(m))
		}
	}
}

func TestOverviewSelectedRowStaysVisibleWhenThePanelOverflows(t *testing.T) {
	t.Parallel()
	client := overviewRichClient()
	client.robots = nil
	for i := range 12 {
		client.robots = append(client.robots, ports.AdminRobot{ID: fmt.Sprintf("r-%d", i), Username: fmt.Sprintf("bot-%02d", i), Enabled: true})
	}
	for _, size := range []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}} {
		m := overviewModelAt(t, client, size.w, size.h)
		for press := 1; press <= 14; press++ {
			m = overviewPress(t, m, testKeyTab())
			out := overviewRender(m)
			if !strings.Contains(out, "▸ ") {
				t.Fatalf("%dx%d Tab #%d: no row is marked:\n%s", size.w, size.h, press, out)
			}
			overviewRowsAndWidths(t, m, size.w, size.h, "overflow")
		}
	}
}

func TestOverviewRecentChangesWordsEveryEvent(t *testing.T) {
	t.Parallel()
	out := overviewRender(overviewModelAt(t, overviewRichClient(), 200, 40))
	for _, want := range []string{
		"3 h ago", "admin gave alice Writer on team/api",
		"2 d ago", "admin issued token ci for ci-publisher (30 days)",
		"<1 h ago", "admin quota.changed bob",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Recent changes lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, " s ago") || strings.Contains(out, "second") {
		t.Errorf("a time in seconds:\n%s", out)
	}
}

func TestEventSentenceCoversEveryServerAction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		event ports.AdminAccessEvent
		want  string
	}{
		{ports.AdminAccessEvent{Action: "grant.put", Subject: "alice", Repository: "team/api", Role: "reader"}, "gave alice Reader on team/api"},
		{ports.AdminAccessEvent{Action: "grant.delete", Subject: "alice", Repository: "team/api"}, "revoked alice on team/api"},
		{ports.AdminAccessEvent{Action: "token.issued", Subject: "bot", TokenName: "ci", Detail: "30 days"}, "issued token ci for bot (30 days)"},
		{ports.AdminAccessEvent{Action: "token.revoked", Subject: "bot", TokenName: "ci"}, "revoked token ci of bot"},
		{ports.AdminAccessEvent{Action: "robot.created", Subject: "bot"}, "created robot bot"},
		{ports.AdminAccessEvent{Action: "robot.deleted", Subject: "bot"}, "deleted robot bot"},
		{ports.AdminAccessEvent{Action: "user.created", Subject: "bob", Detail: "administrator"}, "created user bob (administrator)"},
		{ports.AdminAccessEvent{Action: "user.enabled", Subject: "bob"}, "enabled user bob"},
		{ports.AdminAccessEvent{Action: "user.disabled", Subject: "bob"}, "disabled user bob"},
		{ports.AdminAccessEvent{Action: "user.updated", Subject: "bob", Detail: "read-only on"}, "updated user bob (read-only on)"},
		{ports.AdminAccessEvent{Action: "user.password_reset", Subject: "bob"}, "reset the password of bob"},
		{ports.AdminAccessEvent{Action: "user.deleted", Subject: "bob"}, "deleted user bob"},
		{ports.AdminAccessEvent{Action: "future.thing", Subject: "bob", Detail: "x"}, "future.thing bob x"},
		{ports.AdminAccessEvent{Action: "grant.put"}, "gave on"},
		{ports.AdminAccessEvent{Action: "user.created", Subject: "a\x1b[31mb\nc"}, "created user a[31mbc"},
	}
	for _, tt := range tests {
		if got := eventSentence(tt.event); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.event.Action, got, tt.want)
		}
	}
}

func TestAgoTextIsHoursDaysOrYearsNeverSeconds(t *testing.T) {
	t.Parallel()
	tests := map[time.Duration]string{
		-time.Hour:         "<1 h ago",
		5 * time.Second:    "<1 h ago",
		59 * time.Minute:   "<1 h ago",
		time.Hour:          "1 h ago",
		47 * lifetimeHour:  "47 h ago",
		2 * lifetimeDay:    "2 d ago",
		400 * lifetimeDay:  "400 d ago",
		2 * lifetimeYear:   "2 y ago",
		3*lifetimeYear + 1: "3 y ago",
	}
	for elapsed, want := range tests {
		if got := agoText(elapsed); got != want {
			t.Errorf("agoText(%v) = %q, want %q", elapsed, got, want)
		}
	}
}

func TestOverviewRecentChangesStates(t *testing.T) {
	t.Parallel()
	client := overviewRichClient()
	client.accessEvents = nil
	if out := overviewRender(overviewModelAt(t, client, 150, 24)); !strings.Contains(out, "No changes recorded yet.") {
		t.Errorf("no empty state:\n%s", out)
	}
	client = overviewRichClient()
	client.accessEventsErr = errors.New("history exploded")
	out := overviewRender(overviewModelAt(t, client, 200, 40))
	if !strings.Contains(out, "Could not load recent changes: history exploded") {
		t.Errorf("a failed history read is hidden:\n%s", out)
	}
	if strings.Contains(out, "No changes recorded yet.") {
		t.Errorf("a failed read shown as empty:\n%s", out)
	}
	if !strings.Contains(out, "Tokens expiring soon") || !strings.Contains(out, "2 images have critical findings") {
		t.Errorf("the attention panel went down with the history:\n%s", out)
	}
	loading := newOverviewScreen()
	view := ansi.Strip(loading.View(newAdminTheme(), screenEnv{Layout: consoleLayout{Width: 150, Height: 24}, Now: func() time.Time { return charFixedTime }}).Body)
	if strings.Count(view, "Loading...") != 2 {
		t.Errorf("both panels should say Loading...:\n%s", view)
	}
}

func TestOverviewRecentChangesShowsAsManyRowsAsFit(t *testing.T) {
	t.Parallel()
	client := overviewRichClient()
	client.accessEvents = nil
	for i := range 20 {
		client.accessEvents = append(client.accessEvents, ports.AdminAccessEvent{ID: fmt.Sprint(i), At: charFixedTime.Add(-time.Duration(i+1) * lifetimeHour), Action: "user.enabled", Actor: "admin", Subject: fmt.Sprintf("user-%02d", i)})
	}
	small := overviewRender(overviewModelAt(t, client, 150, 24))
	large := overviewRender(overviewModelAt(t, client, 200, 60))
	if strings.Count(small, "enabled user") >= strings.Count(large, "enabled user") || strings.Count(large, "enabled user") != 20 {
		t.Errorf("rows at 150x24: %d, at 200x60: %d (want 20)", strings.Count(small, "enabled user"), strings.Count(large, "enabled user"))
	}
	if !strings.Contains(small, "user-00") {
		t.Errorf("the newest event is not first:\n%s", small)
	}
}

func TestOverviewEachReadFailsOnItsOwn(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		set  func(*fakeAdminClient)
		note string
		kept []string
	}{
		"robots":   {func(c *fakeAdminClient) { c.listRobotsErr = errors.New("boom") }, "Could not check robots: boom", []string{"Tokens expiring soon", "images have critical", "admin gave alice"}},
		"images":   {func(c *fakeAdminClient) { c.featureErr = errors.New("boom") }, "Could not check image scans: boom", []string{"Tokens expiring soon", "gh-actions has no repository access", "admin gave alice"}},
		"expiring": {func(c *fakeAdminClient) { c.expiringErr = errors.New("boom") }, "Could not check expiring tokens: boom", []string{"gh-actions has no repository access", "images have critical", "admin gave alice"}},
		"events":   {func(c *fakeAdminClient) { c.accessEventsErr = errors.New("boom") }, "Could not load recent changes: boom", []string{"Tokens expiring soon", "gh-actions has no repository access", "images have critical"}},
	}
	for name, tc := range cases {
		client := overviewRichClient()
		tc.set(client)
		out := overviewRender(overviewModelAt(t, client, 200, 40))
		if !strings.Contains(out, tc.note) {
			t.Errorf("%s: note %q missing:\n%s", name, tc.note, out)
		}
		for _, kept := range tc.kept {
			if !strings.Contains(out, kept) {
				t.Errorf("%s: %q was dropped:\n%s", name, kept, out)
			}
		}
	}
	every := overviewRichClient()
	every.listRobotsErr, every.featureErr = errors.New("a"), errors.New("b")
	every.expiringErr, every.accessEventsErr = errors.New("c"), errors.New("d")
	if out := overviewRender(overviewModelAt(t, every, 200, 40)); strings.Contains(out, "Nothing needs attention") || strings.Contains(out, "No changes recorded") {
		t.Errorf("all reads failed and the screen says all good:\n%s", out)
	}
}

func TestOverviewExpiredSessionInAnyReadReturnsToLogin(t *testing.T) {
	t.Parallel()
	expired := AdminSessionExpiredError{Reason: AdminSessionExpiredReasonExpired}
	for name, set := range map[string]func(*fakeAdminClient){
		"expiring": func(c *fakeAdminClient) { c.expiringErr = expired },
		"events":   func(c *fakeAdminClient) { c.accessEventsErr = expired },
		"images":   func(c *fakeAdminClient) { c.featureErr = expired },
	} {
		client := overviewRichClient()
		set(client)
		if m := overviewModelAt(t, client, 150, 24); m.screen != screenAdminLogin {
			t.Errorf("%s: screen = %q, want the login", name, m.screen)
		}
	}
}

func TestOverviewDropsALoadOfAReplacedInstance(t *testing.T) {
	t.Parallel()
	m := overviewModelAt(t, overviewRichClient(), 150, 24)
	stale := overviewLoadedMsg{gen: overviewScreenOf(t, m).gen + 1000, expiringErr: AdminSessionExpiredError{Reason: AdminSessionExpiredReasonExpired}}
	updated, _ := m.Update(stale)
	if got := updated.(Model); got.screen != screenAdminOverview {
		t.Fatalf("a stale expiry logged the operator out: screen = %q", got.screen)
	}
	s := overviewScreenOf(t, m)
	next, _, _ := s.Update(screenEnv{}, overviewLoadedMsg{gen: s.gen + 1, events: nil, eventsErr: errors.New("late")})
	if next.(overviewScreen).eventsErr != "" {
		t.Fatalf("a foreign load was applied")
	}
}

func TestOverviewFitsEveryStateAtEverySize(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("très-long-ñame-日本語-", 8)
	many := overviewRichClient()
	many.robots = nil
	for i := range 30 {
		many.robots = append(many.robots, ports.AdminRobot{ID: fmt.Sprint(i), Username: fmt.Sprintf("%s%d", long, i), Enabled: true})
	}
	many.accessEvents = nil
	for i := range 20 {
		many.accessEvents = append(many.accessEvents, ports.AdminAccessEvent{ID: fmt.Sprint(i), At: charFixedTime.Add(-lifetimeDay * time.Duration(i)), Action: "grant.put", Actor: long, Subject: long, Repository: long, Role: "writer"})
	}
	many.expiringTokens[0].Username = long
	failing := overviewRichClient()
	failing.listRobotsErr, failing.featureErr = errors.New(long), errors.New(long)
	failing.expiringErr, failing.accessEventsErr = errors.New(long), errors.New(long)
	states := map[string]*fakeAdminClient{
		"full": overviewRichClient(), "many and long": many, "failing": failing,
		"empty": {repositoryScanSummaries: []ports.RepositoryScanSummary{}},
	}
	for name, client := range states {
		for _, size := range []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}} {
			m := overviewModelAt(t, client, size.w, size.h)
			overviewRowsAndWidths(t, m, size.w, size.h, name)
			m = overviewPress(t, m, testKeyShiftTab())
			overviewRowsAndWidths(t, m, size.w, size.h, name+" selected")
		}
	}
}
