package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"regixtry/internal/ports"
)

// usersModelAt returns a Model on the Users screen at the given size, with an
// authenticated session and the screen's own loads already applied.
func usersModelAt(t *testing.T, client AdminClient, w, h int) Model {
	t.Helper()
	m := newAdminReadyModelWithCatalog(t, []string{"team/api"}, client)
	m.now = func() time.Time { return usersTestNow }
	m = charResize(t, m, w, h)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: usersTestNow.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	updated, cmd := m.Update(navigateMsg{To: screenAdminUsers})
	return runCmd(t, updated.(Model), cmd)
}

func usersScreenOf(t *testing.T, m Model) usersScreen {
	t.Helper()
	s, ok := m.adminScreens[slotUsers].(usersScreen)
	if !ok {
		t.Fatalf("slot holds %T, want usersScreen", m.adminScreens[slotUsers])
	}
	return s
}

// modelPress sends the keys one by one, running every command they return.
func modelPress(t *testing.T, m Model, keys ...keyMsg) Model {
	t.Helper()
	for _, k := range keys {
		updated, cmd := m.Update(k)
		m = runCmd(t, updated.(Model), cmd)
	}
	return m
}

// mountCreateUser puts a fresh New user form on show, focused on field.
func mountCreateUser(m *Model, field createUserField) {
	s := newCreateUserScreen()
	s.focus = field
	m.adminScreens[slotCreateUser], m.screen = s, screenAdminCreateUser
}

// mountResetPassword puts a fresh Reset password form for alice on show.
func mountResetPassword(m *Model) {
	m.adminScreens[slotChangePassword], m.screen = newResetPasswordScreen("u-1", "alice"), screenAdminChangePassword
}

func createUserOf(m Model) createUserScreen {
	s, _ := m.adminScreens[slotCreateUser].(createUserScreen)
	return s
}

func resetPasswordOf(m Model) resetPasswordScreen {
	s, _ := m.adminScreens[slotChangePassword].(resetPasswordScreen)
	return s
}

func TestSidebarUsersLoadsTheListWithOneExpiringCall(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	m := usersModelAt(t, client, 150, 24)
	if m.screen != screenAdminUsers || client.listExpiringCalls != 1 || client.listUsersCalls != 1 {
		t.Fatalf("screen=%q expiring=%d users=%d", m.screen, client.listExpiringCalls, client.listUsersCalls)
	}
	if out := overviewRender(m); !strings.Contains(out, "Admin › Access › Users") || !strings.Contains(out, "alice") {
		t.Fatalf("the Users screen is not on show:\n%s", out)
	}
}

func TestUsersListIsReloadedOnEveryVisit(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	m := usersModelAt(t, client, 150, 24)
	m = modelPress(t, m, testKeyDown(), testKeyEsc())
	if m.screen != screenAdminOverview {
		t.Fatalf("Esc went to %q", m.screen)
	}
	m = openSidebarItem(t, m, shellNavUsers)
	if client.listUsersCalls != 2 || client.listExpiringCalls != 2 {
		t.Fatalf("reads after the second visit: users=%d expiring=%d, want 2 each", client.listUsersCalls, client.listExpiringCalls)
	}
}

func TestUsersEnterOpensTheProfileAndEscComesBackOnTheSameUser(t *testing.T) {
	t.Parallel()
	client := editTestClient()
	m := usersModelAt(t, client, 150, 24)
	m = modelPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyEnter())
	if m.screen != screenAdminEditUser {
		t.Fatalf("Enter opened %q", m.screen)
	}
	out := overviewRender(m)
	if !strings.Contains(out, "Admin › Access › Users › carol") || !strings.Contains(out, "team/docs") || !strings.Contains(out, "laptop") {
		t.Fatalf("profile not loaded:\n%s", out)
	}
	m = modelPress(t, m, testKeyEsc())
	if m.screen != screenAdminUsers {
		t.Fatalf("Esc went to %q", m.screen)
	}
	if user, _ := usersScreenOf(t, m).current(); user.Username != "carol" {
		t.Fatalf("the list came back on %q, want carol", user.Username)
	}
}

func TestResetPasswordFlowThroughTheModelNeverShowsThePassword(t *testing.T) {
	t.Parallel()
	client := editTestClient()
	m := usersModelAt(t, client, 150, 24)
	m = modelPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyRunes("p"))
	if m.screen != screenAdminChangePassword {
		t.Fatalf("p opened %q", m.screen)
	}
	m = modelPress(t, m, testKeyRunes("a-long-secret"), testKeyTab(), testKeyRunes("a-long-secret"), testKeyEnter())
	if client.resetPasswordCalls != 1 || client.lastResetPasswordInput.UserID != "u-4" || client.lastResetPasswordInput.NewPassword != "a-long-secret" {
		t.Fatalf("calls=%d input=%+v", client.resetPasswordCalls, client.lastResetPasswordInput)
	}
	if m.screen != screenAdminEditUser {
		t.Fatalf("after saving the screen is %q, want the profile", m.screen)
	}
	view := overviewRender(m)
	if !strings.Contains(view, `Password reset for "carol".`) || strings.Contains(view, "a-long-secret") || strings.Contains(m.status, "a-long-secret") {
		t.Fatalf("notice missing or password leaked (status %q):\n%s", m.status, view)
	}
	if m.adminScreens[slotChangePassword] != nil && resetPasswordOf(m).password != "" {
		t.Fatal("the reset form kept the password")
	}
}

func TestEditUserGrantAccessComesBackToTheProfile(t *testing.T) {
	t.Parallel()
	client := editTestClient()
	m := usersModelAt(t, client, 150, 24)
	m = modelPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyEnter(), testKeyRunes("g"))
	if m.screen != screenAdminGrantAccess {
		t.Fatalf("g opened %q", m.screen)
	}
	before := client.listGrantsCalls
	m = modelPress(t, m, testKeyEsc())
	if m.screen != screenAdminEditUser {
		t.Fatalf("Esc from Grant access went to %q, want the profile", m.screen)
	}
	if client.listGrantsCalls <= before {
		t.Fatal("the profile was not re-read after granting access")
	}
}

func TestUsersGrantAccessComesBackToTheSameUser(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, usersTestClient(), 150, 24)
	m = modelPress(t, m, testKeyDown(), testKeyDown(), testKeyRunes("g"), testKeyEsc())
	if m.screen != screenAdminUsers {
		t.Fatalf("screen = %q", m.screen)
	}
	if user, _ := usersScreenOf(t, m).current(); user.Username != "bob" {
		t.Fatalf("the list came back on %q, want bob", user.Username)
	}
}

func TestTokensOfAUserGoBackToTheirProfile(t *testing.T) {
	t.Parallel()
	client := editTestClient()
	m := usersModelAt(t, client, 150, 24)
	m = modelPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyRunes("t"))
	tokens, _ := m.adminScreens[slotTokens].(tokensScreen)
	if m.screen != screenAdminEditUserTokens || tokens.principalID != "u-4" || tokens.principalName != "carol" || tokens.robot {
		t.Fatalf("t opened %q for %#v", m.screen, tokens)
	}
	m = modelPress(t, m, testKeyEsc())
	if m.screen != screenAdminEditUser {
		t.Fatalf("Esc from Tokens went to %q, want the profile", m.screen)
	}
	if !strings.Contains(overviewRender(m), "Admin › Access › Users › carol") {
		t.Fatalf("not carol's profile:\n%s", overviewRender(m))
	}
}

func TestTokensOfARobotStillGoBackToTheRobots(t *testing.T) {
	t.Parallel()
	client := editTestClient()
	client.robots = []ports.AdminRobot{{ID: "r-1", Username: "ci-bot", Repository: "team/api", Enabled: true}}
	m := usersModelAt(t, client, 150, 24)
	m = openSidebarItemFrom(t, m, shellNavRobots)
	m = modelPress(t, m, testKeyRunes("t"))
	if m.screen != screenAdminEditUserTokens {
		t.Fatalf("t on a robot opened %q", m.screen)
	}
	m = modelPress(t, m, testKeyEsc())
	if m.screen != screenAdminRobots {
		t.Fatalf("Esc from a robot's Tokens went to %q, want the Robots list", m.screen)
	}
}

// openSidebarItemFrom goes up to the Overview and opens a sidebar item, for
// tests that start on another screen.
func openSidebarItemFrom(t *testing.T, m Model, id shellNavID) Model {
	t.Helper()
	m = modelPress(t, m, testKeyEsc())
	return openSidebarItem(t, m, id)
}

func TestGrantAccessFromTokensComesBackToTokensThenTheProfile(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, editTestClient(), 150, 24)
	m = modelPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyRunes("t"), testKeyRunes("g"))
	if m.screen != screenAdminGrantAccess {
		t.Fatalf("g from Tokens opened %q", m.screen)
	}
	m = modelPress(t, m, testKeyEsc())
	if m.screen != screenAdminEditUserTokens {
		t.Fatalf("Esc from Grant access went to %q, want the Tokens", m.screen)
	}
	m = modelPress(t, m, testKeyEsc())
	if m.screen != screenAdminEditUser {
		t.Fatalf("Esc from Tokens went to %q", m.screen)
	}
}

func TestNewUserFlowThroughTheModel(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	client.createUser = ports.AdminUser{ID: "u-9", Username: "grace", Enabled: true}
	m := usersModelAt(t, client, 150, 24)
	m = modelPress(t, m, testKeyRunes("n"))
	if m.screen != screenAdminCreateUser {
		t.Fatalf("n opened %q", m.screen)
	}
	// q on any field never quits the program from the form.
	for _, k := range []keyMsg{testKeyTab(), testKeyTab(), testKeyTab()} {
		m = modelPress(t, m, k)
		if _, cmd := m.Update(testKeyRunes("q")); didQuit(cmd) {
			t.Fatalf("q on form field %v quit", createUserOf(m).focus)
		}
	}
	m = modelPress(t, m, testKeyTab()) // back to the username
	m = modelPress(t, m, testKeyRunes("grace"), testKeyTab(), testKeyRunes("hunter2hunter"), testKeyEnter())
	if m.screen != screenAdminUsers || client.createUserCalls != 1 {
		t.Fatalf("screen=%q calls=%d", m.screen, client.createUserCalls)
	}
	if client.listUsersCalls != 2 || usersScreenOf(t, m).selectedID != "u-9" {
		t.Fatalf("the list was not reloaded on the new user: reads=%d selected=%q", client.listUsersCalls, usersScreenOf(t, m).selectedID)
	}
	if strings.Contains(overviewRender(m), "hunter2hunter") || strings.Contains(m.status, "hunter2hunter") {
		t.Fatal("the password leaked into the view or the status")
	}
}

func TestNewUserFormEscGoesBackToTheListAndAFreshFormIsEmpty(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, usersTestClient(), 150, 24)
	m = modelPress(t, m, testKeyRunes("n"), testKeyRunes("grace"), testKeyTab(), testKeyRunes("typed-secret"), testKeyEsc())
	if m.screen != screenAdminUsers {
		t.Fatalf("Esc went to %q", m.screen)
	}
	m = modelPress(t, m, testKeyRunes("n"))
	if got := createUserOf(m); got.username != "" || got.password != "" {
		t.Fatalf("a new form kept %q / %q from the last visit", got.username, got.password)
	}
}

func TestExpiredSessionFromAnyUserScreenReturnsToLoginAndDropsThePasswords(t *testing.T) {
	t.Parallel()
	expired := NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	tests := []struct {
		name  string
		setup func(t *testing.T, m Model) (Model, userScreenMsg)
	}{
		{"users load", func(t *testing.T, m Model) (Model, userScreenMsg) {
			s := usersScreenOf(t, m)
			return m, usersLoadedMsg{gen: s.gen, usersErr: expired}
		}},
		{"disable", func(t *testing.T, m Model) (Model, userScreenMsg) {
			s := usersScreenOf(t, m)
			return m, userEnabledMsg{gen: s.gen, err: expired}
		}},
		{"profile load", func(t *testing.T, m Model) (Model, userScreenMsg) {
			m = modelPress(t, m, testKeyEnter())
			s, _ := m.adminScreens[slotEditUser].(editUserScreen)
			return m, editUserLoadedMsg{gen: s.gen, grantsErr: expired}
		}},
		{"create", func(t *testing.T, m Model) (Model, userScreenMsg) {
			m = modelPress(t, m, testKeyRunes("n"))
			return m, userCreatedMsg{gen: createUserOf(m).gen, err: expired}
		}},
		{"reset", func(t *testing.T, m Model) (Model, userScreenMsg) {
			m = modelPress(t, m, testKeyRunes("p"))
			return m, userPasswordResetMsg{gen: resetPasswordOf(m).gen, err: expired}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := usersModelAt(t, editTestClient(), 150, 24)
			m, msg := tt.setup(t, m)
			updated, _ := m.Update(msg)
			got := updated.(Model)
			if got.screen != screenAdminLogin || got.adminAuth != adminAuthStateExpired {
				t.Fatalf("screen=%q auth=%v, want the expired login", got.screen, got.adminAuth)
			}
			for slot := range got.adminScreens {
				switch got.adminScreens[slot].(type) {
				case usersScreen, editUserScreen, createUserScreen, resetPasswordScreen:
					t.Fatalf("slot %d still holds %T after the session expired", slot, got.adminScreens[slot])
				}
			}
		})
	}
}

func TestAResultOfAScreenInstanceThatIsGoneNeverLogsOut(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, usersTestClient(), 150, 24)
	stale := NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	updated, cmd := m.Update(usersLoadedMsg{gen: usersScreenOf(t, m).gen + 1000, usersErr: stale})
	got := updated.(Model)
	if got.screen != screenAdminUsers || got.adminAuth != adminAuthStateAuthenticated || cmd != nil {
		t.Fatalf("a foreign expired result changed the model: screen=%q auth=%v", got.screen, got.adminAuth)
	}
}

func TestLogoutDropsTheUserScreens(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, usersTestClient(), 150, 24)
	m = modelPress(t, m, testKeyRunes("n"), testKeyTab(), testKeyRunes("typed-secret"), testKeyEsc())
	m = modelPress(t, m, testKeyRunes("l"))
	if m.screen != screenAdminLogin {
		t.Fatalf("l on the list went to %q, want the login", m.screen)
	}
	for slot := range m.adminScreens {
		switch m.adminScreens[slot].(type) {
		case usersScreen, editUserScreen, createUserScreen, resetPasswordScreen:
			t.Fatalf("slot %d still holds %T after logout", slot, m.adminScreens[slot])
		}
	}
}

func TestLAndQAreTypedInTheSearchAndNotLogoutOrQuit(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, usersTestClient(), 150, 24)
	m = modelPress(t, m, testKeyRunes("/"), testKeyRunes("l"), testKeyRunes("q"))
	if s := usersScreenOf(t, m); s.query != "lq" || m.screen != screenAdminUsers {
		t.Fatalf("query=%q screen=%q", s.query, m.screen)
	}
	if _, cmd := m.Update(testKeyCtrl('c')); !didQuit(cmd) {
		t.Fatal("ctrl+c does not quit in the search")
	}
}

func TestQAndLWhileTheDisablePromptIsOpenDoNothing(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, usersTestClient(), 150, 24)
	m = modelPress(t, m, testKeyRunes("d"))
	for _, k := range []keyMsg{testKeyRunes("l"), testKeyRunes("q")} {
		updated, cmd := m.Update(k)
		if didQuit(cmd) || updated.(Model).screen != screenAdminUsers {
			t.Fatalf("%q acted under the prompt (screen %q)", keyText(k), updated.(Model).screen)
		}
	}
	if !usersScreenOf(t, m).confirm.Active() {
		t.Fatal("the prompt closed")
	}
}

func TestUsersViewThroughTheModelFitsTheTerminal(t *testing.T) {
	t.Parallel()
	for _, size := range geometrySizes {
		m := usersModelAt(t, usersTestClient(), size.w, size.h)
		assertFitsTerminal(t, ansi.Strip(m.View().Content), size.w, size.h)
		m = modelPress(t, m, testKeyRunes("d"))
		assertFitsTerminal(t, ansi.Strip(m.View().Content), size.w, size.h)
	}
}

func TestFailedUsersLoadThroughTheModelStaysOnTheScreenWithTheError(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	client.usersErr = errors.New("boom")
	m := usersModelAt(t, client, 150, 24)
	if m.screen != screenAdminUsers || !strings.Contains(overviewRender(m), "Could not load users: boom") {
		t.Fatalf("screen=%q:\n%s", m.screen, overviewRender(m))
	}
}

func TestUsersDisableUsesTheSharedPromptAndItsGuardThroughTheModel(t *testing.T) {
	t.Parallel()
	for _, confirmKey := range []keyMsg{testKeyEnter(), testKeyRunes("y")} {
		client := usersTestClient()
		client.disableUser = ports.AdminUser{ID: "u-2", Username: "alice"}
		m := usersModelAt(t, client, 150, 24)
		m = modelPress(t, m, testKeyDown(), testKeyRunes("d"))
		prompt := usersScreenOf(t, m).confirm
		if !prompt.Active() || prompt.target != "alice" || prompt.consequence == "" {
			t.Fatalf("the shared prompt is not set up: %+v", prompt)
		}
		if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Confirm") || !strings.Contains(view, "Cancel") {
			t.Fatalf("the shared dialog is not drawn:\n%s", view)
		}
		// Press confirm twice before the result arrives: the command runs once.
		updated, cmd := m.Update(confirmKey)
		m = updated.(Model)
		if cmd == nil || !usersScreenOf(t, m).confirm.inFlight {
			t.Fatalf("%q did not start the command", keyText(confirmKey))
		}
		if _, again := m.Update(confirmKey); again != nil {
			t.Fatal("a second confirm key ran the command again")
		}
		_ = runCmd(t, m, cmd)
		if client.disableCalls != 1 {
			t.Fatalf("DisableUser called %d times, want 1", client.disableCalls)
		}
	}
}
