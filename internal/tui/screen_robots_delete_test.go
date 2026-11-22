package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

var updateRobotsDeleteGoldens = flag.Bool("update-robots-delete", false, "rewrite the Robots delete golden files")

func TestDeletingARobotShowsTheBoardsDialog(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)

	asked, _ := accessPress(t, s, env, testKeyRunes("x"))
	if !asked.(robotsScreen).confirm.Active() || client.deleteRobotCalls != 0 {
		t.Fatalf("x must open the confirm and call nothing: calls=%d", client.deleteRobotCalls)
	}
	overlay := strings.Join(strings.Fields(strings.ReplaceAll(accessStrip(asked.View(newAdminTheme(), env).Overlay), "│", " ")), " ")
	mustContain(t, overlay,
		"Delete robot ci-publisher",
		"The robot account is removed together with its repository access and all of its tokens.",
		"repository team/api, role Writer",
		"Anything using its tokens stops working immediately. This cannot be undone.",
		"Delete y / enter", "Cancel esc",
	)
}

func TestDeleteDialogOfARobotWithoutAccessSaysSo(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)
	asked, _ := accessPress(t, s, env, testKeyDown(), testKeyRunes("x"))
	mustContain(t, accessStrip(asked.View(newAdminTheme(), env).Overlay), "Delete robot gh-actions", "no repository access")
}

func TestDeletingARobotSendsItOnceAndSelectsTheNextRow(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)
	asked, _ := accessPress(t, s, env, testKeyRunes("x"))

	first, cmd, _ := asked.Update(env, testKeyEnter())
	if cmd == nil || !first.(robotsScreen).confirm.inFlight {
		t.Fatal("Enter did not dispatch")
	}
	again, second, _ := first.Update(env, testKeyEnter())
	if second != nil {
		t.Fatal("second Enter dispatched again while in flight")
	}
	if _, third, _ := again.Update(env, testKeyRunes("y")); third != nil {
		t.Fatal("y dispatched again while in flight")
	}
	msg := cmd()
	done, _ := accessSettle(again, env, func() tea.Msg { return msg })
	got := done.(robotsScreen)

	if client.deleteRobotCalls != 1 || client.lastDeleteRobotUserID != "r-ci" {
		t.Fatalf("DeleteRobot calls = %d for %q", client.deleteRobotCalls, client.lastDeleteRobotUserID)
	}
	if got.confirm.Active() {
		t.Fatal("result must close the prompt")
	}
	if got.notice != "Robot ci-publisher deleted." || got.noticeFail {
		t.Fatalf("notice = %q fail=%v", got.notice, got.noticeFail)
	}
	if len(got.robots) != 3 {
		t.Fatalf("robots = %d, want 3", len(got.robots))
	}
	if robot, _ := got.selectedRobot(); robot.ID != "r-gh" {
		t.Fatalf("selected %q, want the next row r-gh", robot.ID)
	}
	if _, ok := got.tokens["r-ci"]; ok {
		t.Fatal("the deleted robot's tokens are still cached")
	}
	view := accessView(done, env)
	mustContain(t, view, "Robot ci-publisher deleted.", "3 robots")
	mustNotContain(t, view, "release-pipeline")
}

func TestDeletingTheLastRowSelectsThePreviousOne(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)
	done, _ := accessPress(t, s, env, testKeyDown(), testKeyDown(), testKeyDown(), testKeyRunes("x"), testKeyRunes("y"))
	got := done.(robotsScreen)
	if robot, _ := got.selectedRobot(); robot.ID != "r-stg" || len(got.robots) != 3 {
		t.Fatalf("selected %q of %d robots", robot.ID, len(got.robots))
	}
}

func TestDeletingTheOnlyRobotShowsTheEmptyList(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	client.robots = client.robots[:1]
	s := openRobotsScreen(t, client, env)
	done, _ := accessPress(t, s, env, testKeyRunes("x"), testKeyEnter())
	mustContain(t, accessView(done, env), "No robots yet")
	if _, cmd, _ := done.Update(env, testKeyRunes("x")); cmd != nil {
		t.Fatal("x with no robot dispatched something")
	}
}

func TestDeletingARobotWithTokensUsesItsIdOnly(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)
	if got := s; len(got.tokens["r-ci"]) != 2 {
		t.Fatalf("fixture robot has %d tokens, want 2", len(got.tokens["r-ci"]))
	}
	accessPress(t, s, env, testKeyRunes("x"), testKeyEnter())
	if client.deleteRobotCalls != 1 || len(client.revoked) != 0 {
		t.Fatalf("delete calls=%d revoked=%v: tokens go with the robot, never one by one", client.deleteRobotCalls, client.revoked)
	}
}

func TestFailedDeleteClosesThePromptShowsTheErrorAndKeepsTheRobot(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	client.deleteRobotErr = errBoom
	s := openRobotsScreen(t, client, env)
	next, _ := accessPress(t, s, env, testKeyRunes("x"), testKeyEnter())
	got := next.(robotsScreen)
	if got.confirm.Active() || got.notice != "boom" || !got.noticeFail || len(got.robots) != 4 {
		t.Fatalf("state after failure: confirm=%v notice=%q robots=%d", got.confirm.Active(), got.notice, len(got.robots))
	}
	if robot, _ := got.selectedRobot(); robot.ID != "r-ci" {
		t.Fatalf("selection moved to %q", robot.ID)
	}
	mustContain(t, accessView(next, env), "boom")
}

func TestEscCancelsTheDeleteAndNDoesNotConfirm(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)
	closed, _ := accessPress(t, s, env, testKeyRunes("x"), testKeyEsc())
	if closed.(robotsScreen).confirm.Active() || client.deleteRobotCalls != 0 {
		t.Fatal("esc must close the prompt without calling the API")
	}
	// The board's only cancel key is esc: any other letter is swallowed.
	kept, _ := accessPress(t, s, env, testKeyRunes("x"), testKeyRunes("n"))
	if !kept.(robotsScreen).confirm.Active() || client.deleteRobotCalls != 0 {
		t.Fatal("n must neither delete nor start another action")
	}
}

func TestNothingElseStartsWhileADeleteIsInFlightEvenAfterEsc(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)
	asked, _ := accessPress(t, s, env, testKeyRunes("x"))
	sent, cmd, _ := asked.Update(env, testKeyEnter())
	if cmd == nil {
		t.Fatal("Enter did not dispatch")
	}
	closed, _, _ := sent.Update(env, testKeyEsc())
	for _, k := range []keyMsg{testKeyRunes("d"), testKeyRunes("x")} {
		next, again, _ := closed.Update(env, k)
		if again != nil || next.(robotsScreen).confirm.Active() {
			t.Fatalf("key %q opened an action while the delete is in flight", k.Text)
		}
	}
	// The result lifts the guard.
	done, _ := accessSettle(closed, env, func() tea.Msg { return cmd() })
	if next, _, _ := done.Update(env, testKeyRunes("d")); !next.(robotsScreen).confirm.Active() {
		t.Fatal("d still blocked after the delete finished")
	}
}

func TestNoDeleteWhileADisableIsInFlight(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)
	asked, _ := accessPress(t, s, env, testKeyRunes("d"))
	sent, cmd, _ := asked.Update(env, testKeyEnter())
	if cmd == nil {
		t.Fatal("Enter did not dispatch")
	}
	closed, _, _ := sent.Update(env, testKeyEsc())
	if next, _, _ := closed.Update(env, testKeyRunes("x")); next.(robotsScreen).confirm.Active() {
		t.Fatal("x opened a delete while a disable is in flight")
	}
}

func TestDeleteResultOfAnotherInstanceIsDropped(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)
	next, _, _ := s.Update(env, robotDeletedMsg{gen: s.gen + 1, robotID: "r-ci", name: "ci-publisher"})
	if got := next.(robotsScreen); len(got.robots) != 4 || got.notice != "" {
		t.Fatalf("a foreign result changed the screen: robots=%d notice=%q", len(got.robots), got.notice)
	}
}

func TestDeleteExpiredSessionIsReportedAndGoesToLogin(t *testing.T) {
	t.Parallel()
	expired := NewAdminSessionExpiredError("session expired")
	if reason, ok := (robotDeletedMsg{err: expired}).expiredReason(); !ok || reason != "session expired" {
		t.Fatalf("expiredReason = %q, %v", reason, ok)
	}
	m, _ := accessModel(t, func(c *accessClient) { c.deleteRobotErr = expired })
	m = overviewPress(t, openRobotsOf(t, m), testKeyRunes("x"), testKeyEnter())
	if m.screen != screenAdminLogin || m.adminAuth != adminAuthStateExpired {
		t.Fatalf("screen=%q auth=%q, want the expired login", m.screen, m.adminAuth)
	}
}

func TestRobotsFooterOffersDelete(t *testing.T) {
	t.Parallel()
	client, env := robotsFixture()
	s := openRobotsScreen(t, client, env)
	for _, size := range accessSizes {
		sized := accessEnvSized(client, size.w, size.h)
		mustContain(t, accessView(s, sized), "x delete")
	}
	mustContain(t, accessView(s, env), "Delete robot", "x")
}

func deleteDialogState(w, h int, keys ...keyMsg) func(*testing.T) (adminScreen, screenEnv) {
	return func(t *testing.T) (adminScreen, screenEnv) {
		client, _ := robotsFixture()
		env := accessEnvSized(client, w, h)
		var s adminScreen = openRobotsScreen(t, client, env)
		s, _ = accessPress(t, s, env, keys...)
		return s, env
	}
}

func composedDeleteView(s adminScreen, env screenEnv) string {
	frame := s.View(newAdminTheme(), env)
	if frame.Overlay == "" {
		return accessStrip(frame.Body)
	}
	return accessStrip(compositeOverlay(frame.Body, frame.Overlay, env.Layout.Width, env.Layout.Height))
}

func TestDeleteDialogAndStatesFitEverySize(t *testing.T) {
	t.Parallel()
	states := map[string][]keyMsg{
		"dialog":          {testKeyRunes("x")},
		"dialog_noaccess": {testKeyDown(), testKeyRunes("x")},
		"deleted":         {testKeyRunes("x"), testKeyRunes("y")},
	}
	for name, keys := range states {
		for _, size := range accessSizes {
			t.Run(name+"_"+sizeLabel(size.w, size.h), func(t *testing.T) {
				t.Parallel()
				s, env := deleteDialogState(size.w, size.h, keys...)(t)
				assertFitsTerminal(t, composedDeleteView(s, env), size.w, size.h)
			})
		}
	}
}

func TestDeleteDialogGoldens(t *testing.T) {
	for name, keys := range map[string][]keyMsg{
		"delete_confirm": {testKeyRunes("x")},
		"delete_done":    {testKeyRunes("x"), testKeyRunes("y")},
	} {
		for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
			file := name + "_" + sizeLabel(size.w, size.h)
			t.Run(file, func(t *testing.T) {
				s, env := deleteDialogState(size.w, size.h, keys...)(t)
				got := composedDeleteView(s, env)
				path := filepath.Join("testdata", "robots_tokens", file+".golden")
				if *updateRobotsDeleteGoldens {
					if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read golden (run with -update-robots-delete): %v", err)
				}
				if string(want) != got {
					t.Fatalf("view differs from %s\n--- want\n%s\n--- got\n%s", path, want, got)
				}
			})
		}
	}
}

func TestDeletingARobotThroughTheModelRemovesItFromTheList(t *testing.T) {
	t.Parallel()
	m, client := accessModel(t, nil)
	m = overviewPress(t, openRobotsOf(t, m), testKeyRunes("x"), testKeyEnter())
	if m.screen != screenAdminRobots || client.deleteRobotCalls != 1 {
		t.Fatalf("screen=%q delete calls=%d", m.screen, client.deleteRobotCalls)
	}
	view := overviewRender(m)
	mustContain(t, view, "Robot ci-publisher deleted.", "old-deploy")
	mustNotContain(t, view, "team/api")
}
