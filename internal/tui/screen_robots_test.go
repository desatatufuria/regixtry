package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

func openRobotsScreen(t *testing.T, client *accessClient, env screenEnv) robotsScreen {
	t.Helper()
	s := newRobotsScreen()
	settled, _ := accessSettle(s, env, s.Init(env))
	return settled.(robotsScreen)
}

func robotsFixture() (*accessClient, screenEnv) {
	client := newAccessClient()
	client.robots = sampleRobots()
	client.tokens = sampleRobotTokens()
	return client, accessEnv(client)
}

func TestRobotsScreenSatisfiesAdminScreenInterface(t *testing.T) {
	t.Parallel()
	var _ adminScreen = robotsScreen{}
	var _ adminScreen = createRobotScreen{}
	var _ adminScreen = tokensScreen{}
}

func TestRobotsScreenLoadsTheListThenEachRobotsTokens(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)
	if len(s.robots) != 4 || !s.loaded || !s.tokensLoaded {
		t.Fatalf("robots=%d loaded=%v tokensLoaded=%v", len(s.robots), s.loaded, s.tokensLoaded)
	}
	if got := strings.Join(client.listedTokens, ","); got != "r-ci,r-gh,r-stg,r-old" {
		t.Fatalf("tokens read for %q, want every robot in list order", got)
	}
}

func TestRobotsTableShowsTheBoardsColumnsAndStates(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	view := accessView(openRobotsScreen(t, client, env), env)
	mustContain(t, view,
		"Robots", "4 robots, 1 without repository access",
		"NAME", "REPOSITORY", "ROLE", "STATE", "EXPIRES",
		"▸ ci-publisher", "team/api", "Writer", "enabled", "6 days",
		"gh-actions", "none", "89 days",
		"staging-deploy", "18 days",
		"old-deploy", "disabled", "expired",
		"EXPIRES is the token that ends first.",
	)
	mustNotContain(t, view, "Sample data")
}

func TestRobotsDetailPanelShowsAccessActionsAndWhatNeedsAttention(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	view := accessView(openRobotsScreen(t, client, env), env)
	mustContain(t, view,
		"robot, enabled, created 12 Jun 2026",
		"ACCESS", "team/api", "[Writer]", "1 repository, 2 tokens",
		"ACTIONS", "New robot", "Tokens", "Grant access", "Disable robot",
		"NEEDS ATTENTION", "Token release-pipeline expires in", "6 days", "t renew",
	)
}

func TestRobotWithoutRepositoryAccessIsFlaggedAndExplained(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)
	next, _ := accessPress(t, s, env, testKeyDown())
	view := accessView(next, env)
	mustContain(t, view,
		"gh-actions can authenticate but cannot pull or push.", "Grant it a repository with g.",
		"No repository access", "g grant", "0 repositories, 1 token",
	)
}

func TestRobotsSelectionMovesAndStaysInBounds(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)
	keys := []keyMsg{testKeyUp(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyUp(), testKeyRunes("k"), testKeyRunes("j")}
	want := []int{0, 1, 2, 3, 3, 3, 2, 1, 2}
	var cur adminScreen = s
	for i, k := range keys {
		cur, _ = accessPress(t, cur, env, k)
		if got := cur.(robotsScreen).selected; got != want[i] {
			t.Fatalf("after key %d selected = %d, want %d", i, got, want[i])
		}
	}
}

func TestRobotsKeysOpenTheScreensOfTheBoard(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)

	cases := []struct {
		name string
		key  keyMsg
		want func(tea.Msg) error
	}{
		{"n opens the Create robot form", testKeyRunes("n"), func(m tea.Msg) error {
			if _, ok := m.(openCreateRobotMsg); !ok {
				return fmt.Errorf("got %#v", m)
			}
			return nil
		}},
		{"t opens the robot's tokens", testKeyRunes("t"), wantTokensMsg},
		{"enter opens the robot's tokens", testKeyEnter(), wantTokensMsg},
		{"g grants access with the robot preselected", testKeyRunes("g"), func(m tea.Msg) error {
			got, ok := m.(openGrantAccessMsg)
			if !ok || got.preselectID != "r-ci" || got.returnTo != screenAdminRobots {
				return fmt.Errorf("got %#v", m)
			}
			return nil
		}},
		{"esc goes back to the Overview", testKeyEsc(), func(m tea.Msg) error {
			if got, ok := m.(navigateMsg); !ok || got.To != screenAdminOverview {
				return fmt.Errorf("got %#v", m)
			}
			return nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, cmd, consumed := s.Update(env, tc.key)
			if cmd == nil || !consumed {
				t.Fatalf("cmd=%v consumed=%v", cmd, consumed)
			}
			if err := tc.want(cmd()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func wantTokensMsg(m tea.Msg) error {
	got, ok := m.(openTokensMsg)
	if !ok || got.principalID != "r-ci" || got.principalName != "ci-publisher" || !got.robot || got.returnTo != screenAdminRobots {
		return fmt.Errorf("got %#v", m)
	}
	return nil
}

func TestRobotsPastedLetterIsNotACommand(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)
	_, cmd, _ := s.Update(env, keyMsg{Text: "n"})
	if cmd != nil {
		t.Fatal("a pasted n opened a form")
	}
}

func TestDisablingARobotAsksFirstAndSendsItOnce(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)

	asked, _ := accessPress(t, s, env, testKeyRunes("d"))
	if !asked.(robotsScreen).confirm.Active() || len(client.disabled) != 0 {
		t.Fatalf("d must open the confirm and call nothing: disabled=%v", client.disabled)
	}
	if frame := asked.View(newAdminTheme(), env); !strings.Contains(accessStrip(frame.Overlay), `Confirm disable robot "ci-publisher"?`) {
		t.Fatalf("overlay = %q", frame.Overlay)
	}

	// Enter dispatches; a second Enter before the result is ignored.
	first, cmd, _ := asked.Update(env, testKeyEnter())
	if cmd == nil || !first.(robotsScreen).acting {
		t.Fatal("Enter did not dispatch")
	}
	again, second, _ := first.Update(env, testKeyEnter())
	if second != nil {
		t.Fatal("second Enter dispatched again while in flight")
	}
	msg := cmd()
	reloaded, seen := accessSettle(again, env, func() tea.Msg { return msg })
	got := reloaded.(robotsScreen)
	if len(client.disabled) != 1 || client.disabled[0] != "r-ci" {
		t.Fatalf("DisableUser calls = %v", client.disabled)
	}
	if got.confirm.Active() || got.acting {
		t.Fatal("result must clear the confirm and the in-flight flag")
	}
	if got.notice != "Robot ci-publisher disabled." {
		t.Fatalf("notice = %q", got.notice)
	}
	if client.listRobotsCalls < 2 {
		t.Fatalf("the list was not reloaded after the change (%d reads, %d msgs)", client.listRobotsCalls, len(seen))
	}
}

func TestEscClosesTheConfirmWithoutDisabling(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)
	asked, _ := accessPress(t, s, env, testKeyRunes("d"), testKeyEsc())
	if asked.(robotsScreen).confirm.Active() || len(client.disabled) != 0 {
		t.Fatal("Esc must cancel without calling the API")
	}
}

func TestEnableOnlyAppliesToADisabledRobotAndDisableToAnEnabledOne(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)

	if next, _ := accessPress(t, s, env, testKeyRunes("e")); next.(robotsScreen).confirm.Active() {
		t.Fatal("e opened a confirm on an enabled robot")
	}
	old, _ := accessPress(t, s, env, testKeyDown(), testKeyDown(), testKeyDown())
	if next, _ := accessPress(t, old, env, testKeyRunes("d")); next.(robotsScreen).confirm.Active() {
		t.Fatal("d opened a confirm on a disabled robot")
	}
	enabled, _ := accessPress(t, old, env, testKeyRunes("e"), testKeyEnter())
	if len(client.enabled) != 1 || client.enabled[0] != "r-old" {
		t.Fatalf("EnableUser calls = %v", client.enabled)
	}
	if got := enabled.(robotsScreen).notice; got != "Robot old-deploy enabled." {
		t.Fatalf("notice = %q", got)
	}
	mustContain(t, accessView(old, env), "Enable robot", "e")
}

func TestFailedDisableShowsTheErrorAndKeepsTheRobot(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	client.disableErr = fmt.Errorf("cannot disable")
	s := openRobotsScreen(t, client, env)
	next, _ := accessPress(t, s, env, testKeyRunes("d"), testKeyEnter())
	got := next.(robotsScreen)
	if got.notice != "cannot disable" || !got.noticeFail || got.confirm.Active() || got.acting {
		t.Fatalf("state after failure: %#v", got)
	}
	mustContain(t, accessView(next, env), "cannot disable")
}

func TestRobotsExpiredSessionIsReportedToTheParent(t *testing.T) {
	t.Parallel()
	expired := NewAdminSessionExpiredError("session expired")
	for name, msg := range map[string]accessMsg{
		"list":    robotsLoadedMsg{err: expired},
		"tokens":  robotsTokensLoadedMsg{err: expired},
		"disable": robotEnabledMsg{err: expired},
	} {
		if reason, ok := msg.expiredReason(); !ok || reason != "session expired" {
			t.Fatalf("%s: expiredReason = %q, %v", name, reason, ok)
		}
	}
}

func TestRobotsDropsMessagesOfAnotherInstance(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)
	next, cmd, _ := s.Update(env, robotsLoadedMsg{gen: s.gen + 1, robots: nil})
	if cmd != nil || len(next.(robotsScreen).robots) != 4 {
		t.Fatal("a foreign list result replaced the robots")
	}
	next, _, _ = s.Update(env, robotEnabledMsg{gen: s.gen + 1, name: "x", err: errBoom})
	if next.(robotsScreen).notice != "" {
		t.Fatal("a foreign result set the notice")
	}
}

func TestRobotsReloadKeepsTheSelectedRobot(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)
	cur, _ := accessPress(t, s, env, testKeyDown(), testKeyDown())
	// A new robot sorts first; the cursor must stay on staging-deploy.
	client.robots = append([]ports.AdminRobot{{ID: "r-new", Username: "aaa", Enabled: true}}, client.robots...)
	cur, _ = accessPress(t, cur, env, testKeyRunes("r"))
	if robot, _ := cur.(robotsScreen).selectedRobot(); robot.ID != "r-stg" {
		t.Fatalf("selected %q after reload, want r-stg", robot.ID)
	}
}

func TestRobotsLoadFailureAndEmptyAndLoadingStates(t *testing.T) {
	t.Parallel()
	client := newAccessClient()
	client.listRobotsErr = fmt.Errorf("registry unreachable")
	env := accessEnv(client)
	failed := openRobotsScreen(t, client, env)
	mustContain(t, accessView(failed, env), "Could not load robots: registry unreachable", "Press r to try again.")

	client.listRobotsErr = nil
	retried, _ := accessPress(t, failed, env, testKeyRunes("r"))
	mustContain(t, accessView(retried, env), "No robots yet. Press n to create one.")

	loading := newRobotsScreen()
	mustContain(t, accessView(loading, env), "Loading robots...")
}

func TestRobotsTokenReadFailureStillListsTheRobots(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	client.tokensErr = fmt.Errorf("tokens down")
	s := openRobotsScreen(t, client, env)
	view := accessView(s, env)
	mustContain(t, view, "ci-publisher", "Some token expiries could not be read: tokens down")
	mustNotContain(t, view, "6 days")
}

func TestFirstExpiryIsTheTokenThatEndsFirst(t *testing.T) {
	t.Parallel()
	day := 24 * time.Hour
	revoked := accessNow
	cases := []struct {
		name   string
		tokens []ports.AdminToken
		want   time.Time
		ok     bool
	}{
		{"none", nil, time.Time{}, false},
		{"soonest of two", []ports.AdminToken{{ExpiresAt: accessNow.Add(90 * day)}, {ExpiresAt: accessNow.Add(6 * day)}}, accessNow.Add(6 * day), true},
		{"an expired token does not hide a live one", []ports.AdminToken{{ExpiresAt: accessNow.Add(-day)}, {ExpiresAt: accessNow.Add(20 * day)}}, accessNow.Add(20 * day), true},
		{"all expired shows the latest", []ports.AdminToken{{ExpiresAt: accessNow.Add(-5 * day)}, {ExpiresAt: accessNow.Add(-2 * day)}}, accessNow.Add(-2 * day), true},
		{"revoked ones do not count", []ports.AdminToken{{ExpiresAt: accessNow.Add(day), RevokedAt: &revoked}}, time.Time{}, false},
	}
	for _, tc := range cases {
		got, ok := firstExpiry(tc.tokens, accessNow)
		if ok != tc.ok || !got.Equal(tc.want) {
			t.Fatalf("%s: got %v %v, want %v %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestRobotsLongAndMultibyteNamesAndManyRowsKeepTheSelectionVisible(t *testing.T) {
	t.Parallel()
	client := newAccessClient()
	for i := range 40 {
		client.robots = append(client.robots, ports.AdminRobot{
			ID: fmt.Sprintf("r-%02d", i), Username: fmt.Sprintf("robot-%02d-%s", i, strings.Repeat("ñ", 30)),
			Repository: "team/" + strings.Repeat("repo", 10), Role: domainauth.RepoRoleReader, Enabled: true,
		})
	}
	env := accessEnv(client)
	var cur adminScreen = openRobotsScreen(t, client, env)
	for range 25 {
		cur, _ = accessPress(t, cur, env, testKeyDown())
	}
	view := accessView(cur, env)
	mustContain(t, view, "robot-25", "26 of 40")
	for _, line := range accessLines(view) {
		if w := lipglossWidth(line); w > 150 {
			t.Fatalf("line %d cells wide: %q", w, line)
		}
	}
}

func TestRobotsKeysAreDerivedFromTheBoardAndQuit(t *testing.T) {
	t.Parallel()
	var helps []string
	for _, b := range robotsKeys.short {
		helps = append(helps, b.Help().Key+" "+b.Help().Desc)
	}
	got := strings.Join(helps, "|")
	for _, want := range []string{"↑↓ move", "enter open", "n new robot", "t tokens", "g grant access", "d disable", "esc back", "q quit"} {
		if !strings.Contains(got, want) {
			t.Fatalf("keys %q lack %q", got, want)
		}
	}
}
