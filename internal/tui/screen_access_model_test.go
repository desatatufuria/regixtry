package tui

import (
	"testing"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// accessModel is a signed-in model on the Overview whose client serves the
// Robots, Create robot and Tokens screens.
func accessModel(t *testing.T, mutate func(*accessClient)) (Model, *accessClient) {
	t.Helper()
	client := newAccessClient()
	client.users = []ports.AdminUser{{ID: "u-1", Username: "alice", Enabled: true}}
	client.robots = []ports.AdminRobot{
		{ID: "r-ci", Username: "ci-publisher", Repository: "team/api", Role: domainauth.RepoRoleWriter, Enabled: true},
		{ID: "r-old", Username: "old-deploy", Repository: "team/infra", Role: domainauth.RepoRoleReader},
	}
	client.createRobot = ports.AdminCreatedRobot{
		Robot:  ports.AdminRobot{ID: "r-new", Username: "release-bot", Repository: "team/api", Role: domainauth.RepoRoleReader, Enabled: true},
		Secret: "robot-secret-value", Accessor: "act_robot", ExpiresAt: fixedTestTime.Add(30 * day),
	}
	client.createToken = ports.AdminCreatedToken{Secret: "token-secret-value", Accessor: "act_tok", ExpiresAt: fixedTestTime.Add(30 * day)}
	if mutate != nil {
		mutate(client)
	}
	return overviewModelAt(t, client, 150, 24), client
}

func accessModelText(m Model) string { return accessStrip(m.View().Content) }

func openRobotsOf(t *testing.T, m Model) Model {
	t.Helper()
	return overviewPress(t, m, testKeyDown(), testKeyDown(), testKeyEnter())
}

func TestRobotsTokensKeyOpensTheRobotsTokensAndEscComesBackToThatRobot(t *testing.T) {
	t.Parallel()
	m, client := accessModel(t, nil)
	client.tokens["r-old"] = []ports.AdminToken{accessToken("legacy", "act_legacy", fixedTestTime.Add(-day), fixedTestTime.Add(day))}
	m = openRobotsOf(t, m)
	m = overviewPress(t, m, testKeyDown(), testKeyRunes("t"))
	if m.screen != screenAdminEditUserTokens {
		t.Fatalf("t opened %q", m.screen)
	}
	view := accessModelText(m)
	mustContain(t, view, "Admin › Access › Robots › old-deploy › Tokens", "Tokens for old-deploy", "legacy")
	m = overviewPress(t, m, testKeyEsc())
	if m.screen != screenAdminRobots {
		t.Fatalf("Esc reached %q, want the Robots list", m.screen)
	}
	if robot, _ := m.adminScreens[slotRobots].(robotsScreen).selectedRobot(); robot.ID != "r-old" {
		t.Fatalf("selected %q after coming back, want old-deploy", robot.ID)
	}
}

func TestOverviewROpensCreateRobotAndEscGoesToRobotsThenOverview(t *testing.T) {
	t.Parallel()
	m, _ := accessModel(t, nil)
	m = overviewPress(t, m, testKeyRunes("r"))
	if m.screen != screenAdminCreateRobot {
		t.Fatalf("r opened %q", m.screen)
	}
	mustContain(t, accessModelText(m), "Admin › Access › Robots › New robot", "Lifetime")
	m = overviewPress(t, m, testKeyEsc())
	if m.screen != screenAdminRobots {
		t.Fatalf("Esc reached %q", m.screen)
	}
	m = overviewPress(t, m, testKeyEsc())
	if m.screen != screenAdminOverview {
		t.Fatalf("second Esc reached %q", m.screen)
	}
}

func TestCreatingARobotEndToEndShowsTheSecretOnceAndTheListReloads(t *testing.T) {
	t.Parallel()
	m, client := accessModel(t, nil)
	m = overviewPress(t, m, testKeyRunes("r"))
	m = overviewPress(t, m, testKeyRunes("release-bot"), testKeyTab(), testKeyRunes("team/api"), testKeyEnter(), testKeyEnter())
	if client.createRobotCalls != 1 {
		t.Fatalf("CreateRobot calls = %d", client.createRobotCalls)
	}
	in := client.lastCreateRobotInput
	if in.Name != "release-bot" || in.Repository != "team/api" || in.Role != domainauth.RepoRoleReader || in.TTL != 30*day {
		t.Fatalf("input = %#v", in)
	}
	mustContain(t, accessModelText(m), "robot-secret-value", "act_robot", oneTimeSecretWarning)

	listed := client.listRobotsCalls
	client.robots = append(client.robots, client.createRobot.Robot)
	m = overviewPress(t, m, testKeyEnter())
	if m.screen != screenAdminRobots || client.listRobotsCalls != listed+1 {
		t.Fatalf("Done: screen=%q robot reads=%d (was %d)", m.screen, client.listRobotsCalls, listed)
	}
	view := accessModelText(m)
	mustContain(t, view, "release-bot")
	mustNotContain(t, view, "robot-secret-value")
	if robot, _ := m.adminScreens[slotRobots].(robotsScreen).selectedRobot(); robot.ID != "r-new" {
		t.Fatalf("selected %q, want the new robot", robot.ID)
	}
	// Opening the form again never shows the earlier secret.
	again := overviewPress(t, m, testKeyRunes("n"))
	mustNotContain(t, accessModelText(again), "robot-secret-value")
}

func TestLettersAreTypedNotCommandsInTheFormsAtModelLevel(t *testing.T) {
	t.Parallel()
	m, _ := accessModel(t, nil)
	create := overviewPress(t, m, testKeyRunes("r"))
	create, cmd := sendKeyCmd(create, testKeyRunes("q"))
	if didQuit(cmd) {
		t.Fatal("q quit while the robot name had focus")
	}
	create, cmd = sendKeyCmd(create, testKeyRunes("l"))
	if create.screen != screenAdminCreateRobot || createRobotNameOf(create) != "ql" || cmd != nil && didQuit(cmd) {
		t.Fatalf("screen=%q name=%q: l logged out or q/l were not typed", create.screen, createRobotNameOf(create))
	}

	robots := openRobotsOf(t, m)
	tokens := overviewPress(t, robots, testKeyRunes("t"), testKeyRunes("n"))
	tokens, cmd = sendKeyCmd(tokens, testKeyRunes("q"))
	if didQuit(cmd) {
		t.Fatal("q quit while the token name had focus")
	}
	tokens, _ = sendKeyCmd(tokens, testKeyRunes("l"))
	if tokens.screen != screenAdminEditUserTokens || tokenNameOf(tokens) != "ql" {
		t.Fatalf("screen=%q name=%q: l logged out or q/l were not typed", tokens.screen, tokenNameOf(tokens))
	}
}

func TestQQuitsAndLLogsOutFromTheListsOnly(t *testing.T) {
	t.Parallel()
	m, _ := accessModel(t, func(c *accessClient) {
		c.tokens["r-ci"] = []ports.AdminToken{accessToken("ci", "act_ci", fixedTestTime.Add(-day), fixedTestTime.Add(day))}
	})
	robots := openRobotsOf(t, m)
	if _, cmd := sendKeyCmd(robots, testKeyRunes("q")); !didQuit(cmd) {
		t.Fatal("q on the Robots list did not quit")
	}
	if out, _ := sendKeyCmd(robots, testKeyRunes("l")); out.screen != screenAdminLogin {
		t.Fatalf("l on the Robots list reached %q, want the login", out.screen)
	}
	tokens := overviewPress(t, robots, testKeyRunes("t"))
	if _, cmd := sendKeyCmd(tokens, testKeyRunes("q")); !didQuit(cmd) {
		t.Fatal("q on the token list did not quit")
	}
	confirming := overviewPress(t, tokens, testKeyRunes("x"))
	if _, cmd := sendKeyCmd(confirming, testKeyRunes("q")); didQuit(cmd) {
		t.Fatal("q quit while the revoke confirm was open")
	}
}

func TestRobotsConfirmSwallowsLettersAtModelLevel(t *testing.T) {
	t.Parallel()
	m, client := accessModel(t, nil)
	robots := overviewPress(t, openRobotsOf(t, m), testKeyRunes("d"))
	if _, cmd := sendKeyCmd(robots, testKeyRunes("q")); didQuit(cmd) {
		t.Fatal("q quit while the disable confirm was open")
	}
	if out, _ := sendKeyCmd(robots, testKeyRunes("l")); out.screen != screenAdminRobots {
		t.Fatalf("l reached %q while the confirm was open", out.screen)
	}
	robots = overviewPress(t, robots, testKeyEnter())
	if len(client.disabled) != 1 {
		t.Fatalf("DisableUser calls = %v", client.disabled)
	}
}

func TestExpiredSessionDuringCreationGoesToLoginAndDropsTheScreens(t *testing.T) {
	t.Parallel()
	m, _ := accessModel(t, func(c *accessClient) { c.createRobotErr = NewAdminSessionExpiredError("session expired") })
	m = overviewPress(t, m, testKeyRunes("r"))
	m = overviewPress(t, m, testKeyRunes("bot"), testKeyTab(), testKeyRunes("team/api"), testKeyEnter(), testKeyEnter())
	if m.screen != screenAdminLogin || m.adminAuth != adminAuthStateExpired {
		t.Fatalf("screen=%q auth=%q, want the expired login", m.screen, m.adminAuth)
	}
	for _, slot := range []screenSlot{slotRobots, slotCreateRobot, slotTokens} {
		if m.adminScreens[slot] != nil {
			t.Fatalf("slot %d still mounted after the session expired", slot)
		}
	}
}

func TestAForeignGenerationNeverReachesTheMountedScreen(t *testing.T) {
	t.Parallel()
	m, _ := accessModel(t, nil)
	m = overviewPress(t, m, testKeyRunes("r"))
	current := m.adminScreens[slotCreateRobot].(createRobotScreen)
	// A stale list result is dropped, and a stale expiry never logs out.
	updated, _ := m.Update(robotsLoadedMsg{gen: current.gen + 99, err: NewAdminSessionExpiredError("old")})
	if updated.(Model).screen != screenAdminCreateRobot {
		t.Fatalf("a stale expiry changed the screen to %q", updated.(Model).screen)
	}
}

func TestEscMidCreationBringsTheSecretBackWhenTheRobotIsCreated(t *testing.T) {
	t.Parallel()
	m, client := accessModel(t, nil)
	m = overviewPress(t, m, testKeyRunes("r"))
	m = overviewPress(t, m, testKeyRunes("late-bot"), testKeyTab(), testKeyRunes("team/api"), testKeyEnter())
	updated, create := m.Update(testKeyEnter())
	m = updated.(Model)
	if create == nil {
		t.Fatal("Enter did not start the creation")
	}
	m = overviewPress(t, m, testKeyEsc())
	if m.screen != screenAdminRobots {
		t.Fatalf("Esc mid-creation reached %q", m.screen)
	}
	m = runCmd(t, m, create)
	if m.screen != screenAdminCreateRobot {
		t.Fatalf("the late result left the operator on %q", m.screen)
	}
	mustContain(t, accessModelText(m), "robot-secret-value")
	if client.createRobotCalls != 1 {
		t.Fatalf("CreateRobot calls = %d", client.createRobotCalls)
	}
}

func TestLeavingTheAdminPanelDropsEverySecret(t *testing.T) {
	t.Parallel()
	m, _ := accessModel(t, nil)
	m = overviewPress(t, m, testKeyRunes("r"))
	m = overviewPress(t, m, testKeyRunes("bot"), testKeyTab(), testKeyRunes("team/api"), testKeyEnter(), testKeyEnter())
	if !m.adminScreens[slotCreateRobot].(createRobotScreen).done {
		t.Fatal("setup: no secret on screen")
	}
	if out := m.logoutAdmin(); out.adminScreens[slotCreateRobot] != nil || out.adminScreens[slotTokens] != nil {
		t.Fatal("logout left a screen mounted")
	}
	if out := m.returnToInspection(); out.adminScreens[slotCreateRobot] != nil {
		t.Fatal("leaving the admin panel left the secret mounted")
	}
}

func TestTokensScreenWorksAtEverySizeThroughTheModel(t *testing.T) {
	t.Parallel()
	for _, size := range accessSizes {
		m, _ := accessModel(t, nil)
		m = resizeModel(t, m, size.w, size.h)
		m = overviewPress(t, openRobotsOf(t, m), testKeyRunes("t"), testKeyRunes("n"))
		lines := accessLines(accessModelText(m))
		if len(lines) > size.h {
			t.Fatalf("%dx%d: %d rows", size.w, size.h, len(lines))
		}
		for _, line := range lines {
			if lipglossWidth(line) > size.w {
				t.Fatalf("%dx%d: a row is %d cells wide", size.w, size.h, lipglossWidth(line))
			}
		}
	}
}

// The revoke and disable prompts use the shared confirm: its target box and
// consequence, its y or Enter only, and its in-flight guard.
func TestRevokeAndDisablePromptsUseTheSharedConfirm(t *testing.T) {
	t.Parallel()
	_, env, tokens := tokensFixture(true)
	revoke, _ := accessPress(t, tokens, env, testKeyRunes("x"))
	overlay := accessStrip(revoke.View(newAdminTheme(), env).Overlay)
	mustContain(t, overlay, "Confirm Token Revocation", "release-pipeline", "ci-publisher", "Anything using this token stops working immediately.", "y / enter")

	_, renv := robotsFixture()
	robots := openRobotsScreen(t, nil, renv)
	disable, _ := accessPress(t, robots, renv, testKeyRunes("d"))
	overlay = accessStrip(disable.View(newAdminTheme(), renv).Overlay)
	mustContain(t, overlay, `Confirm disable robot "ci-publisher"?`, "Writer on team/api", "ci-publisher can no longer authenticate.", "y / enter")

	for name, open := range map[string]struct {
		s   adminScreen
		env screenEnv
	}{"revoke": {revoke, env}, "disable": {disable, renv}} {
		for _, other := range []keyMsg{testKeyRunes("n"), testKeyRunes("Y"), testKeyRunes("x"), testKeyRunes("q"), testKeySpace(), testKeyTab()} {
			if _, cmd, _ := open.s.Update(open.env, other); cmd != nil {
				t.Fatalf("%s: %v ran the action", name, other)
			}
		}
		first, cmd, _ := open.s.Update(open.env, testKeyRunes("y"))
		if cmd == nil {
			t.Fatalf("%s: y did not confirm", name)
		}
		if _, second, _ := first.Update(open.env, testKeyEnter()); second != nil {
			t.Fatalf("%s: a second key ran the action again", name)
		}
	}
}
