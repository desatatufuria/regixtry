package tui

import (
	"fmt"
	"strings"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

func grantRoleScreenOf(t *testing.T, m Model) grantProjectRoleScreen {
	t.Helper()
	s, ok := m.adminScreens[slotGrantProjectRole].(grantProjectRoleScreen)
	if !ok {
		t.Fatalf("slot holds %T, want grantProjectRoleScreen", m.adminScreens[slotGrantProjectRole])
	}
	return s
}

// grantRoleModelAt opens the Grant project role form of odabank from the
// members screen ("g" with nobody selected on an empty list).
func grantRoleModelAt(t *testing.T, client AdminClient, w, h int) Model {
	t.Helper()
	m := projectsAt(t, client, w, h, projectsRepos...)
	updated, cmd := m.Update(openGrantProjectRoleMsg{project: "odabank"})
	return repoAccessRun(t, updated.(Model), cmd)
}

func typeText(text string) []keyMsg { return repoAccessType(text) }

func TestGrantProjectRoleShowsTheBoard(t *testing.T) {
	t.Parallel()
	m := grantRoleModelAt(t, membersClient(), 150, 30)
	out := projectsRender(m)
	requireContains(t, out, "Admin › Access › Projects › odabank › Grant role", "Grant project role", "project odabank",
		"Member type", "(*) User", "( ) Robot", "Member", "Project role", "(*) Project reader", "( ) Project writer", "( ) Project admin",
		"Pick a member to see what changes.", "Grant role", "enter", "Cancel", "esc",
		"tab next field", "↑↓ pick member", "←→ type, role", "enter grant", "esc cancel")
	m = projectsPress(t, m, testKeyTab())
	m = projectsPress(t, m, typeText("da")...)
	out = projectsRender(m)
	requireContains(t, out, "▸ dave  user", "dana  user")
	requireAbsent(t, out, "erin", "ci-publisher")
	m = projectsPress(t, m, testKeyDown(), testKeyEnter())
	if s := grantRoleScreenOf(t, m); s.member != "dana" || s.focus != grantProjectFieldRole {
		t.Fatalf("member %q focus %v, want dana accepted and the role field next", s.member, s.focus)
	}
	requireContains(t, projectsRender(m), "dana becomes Project reader of odabank", "Read access to every repository in odabank, existing and future. Applies from the next request.")
}

func TestGrantProjectRoleGrantsAndReturnsToTheMembers(t *testing.T) {
	t.Parallel()
	client := membersClient()
	m := grantRoleModelAt(t, client, 150, 30)
	m = projectsPress(t, m, testKeyTab())
	m = projectsPress(t, m, typeText("erin")...)
	m = projectsPress(t, m, testKeyEnter(), testKeyRight()) // accept erin, then Project writer
	if s := grantRoleScreenOf(t, m); s.role != domainauth.RepoRoleWriter {
		t.Fatalf("role = %q", s.role)
	}
	updated, cmd := m.Update(testKeyEnter())
	m = updated.(Model)
	if _, again := m.Update(testKeyEnter()); again != nil {
		t.Fatal("a second Enter while the grant is in flight dispatched a command")
	}
	m = repoAccessRun(t, m, cmd)
	want := ports.AdminPutProjectGrantInput{Project: "odabank", Username: "erin", Role: domainauth.RepoRoleWriter}
	if len(client.projectsAPI.putCalls) != 1 || client.projectsAPI.putCalls[0] != want {
		t.Fatalf("put calls = %+v, want exactly %+v", client.projectsAPI.putCalls, want)
	}
	if m.screen != screenAdminProjectMembers {
		t.Fatalf("screen = %q, want the members after the grant", m.screen)
	}
	requireContains(t, projectsRender(m), "erin is now Project writer of odabank.", "erin", "6 members")
}

func TestGrantProjectRoleChangesTheRoleOfAMember(t *testing.T) {
	t.Parallel()
	client := membersClient()
	m := membersModelAt(t, client, 150, 30, "odabank")
	m = projectsPress(t, m, testKeyDown(), testKeyRunes("g")) // bob, reader
	m = projectsPress(t, m, testKeyRight(), testKeyRight(), testKeyEnter())
	want := ports.AdminPutProjectGrantInput{Project: "odabank", Username: "bob", Role: domainauth.RepoRoleAdmin}
	if len(client.projectsAPI.putCalls) != 1 || client.projectsAPI.putCalls[0] != want {
		t.Fatalf("put calls = %+v, want %+v", client.projectsAPI.putCalls, want)
	}
	requireContains(t, projectsRender(m), "bob is now Project admin of odabank.")
}

func TestGrantProjectRoleRefusesAdminForARobotBeforeSendingAnything(t *testing.T) {
	t.Parallel()
	client := membersClient()
	m := grantRoleModelAt(t, client, 150, 30)
	m = projectsPress(t, m, testKeyRight()) // Robot
	m = projectsPress(t, m, testKeyTab())
	m = projectsPress(t, m, typeText("release")...)
	m = projectsPress(t, m, testKeyEnter(), testKeyRight(), testKeyRight()) // Project admin
	out := projectsRender(m)
	requireContains(t, out, "Project admin is not available for robots",
		"Robots can be Project reader or Project writer only. Project admin is for users.")
	requireAbsent(t, out, "release-bot becomes")
	m = projectsPress(t, m, testKeyEnter())
	if len(client.projectsAPI.putCalls) != 0 || m.screen != screenAdminGrantProjectRole {
		t.Fatalf("Enter on robot + admin: calls %v, screen %s", client.projectsAPI.putCalls, m.screen)
	}
	// Back to a role robots can have: the same form grants.
	m = projectsPress(t, m, testKeyLeft(), testKeyEnter())
	if len(client.projectsAPI.putCalls) != 1 || client.projectsAPI.putCalls[0].Role != domainauth.RepoRoleWriter || client.projectsAPI.putCalls[0].Username != "release-bot" {
		t.Fatalf("put calls = %+v", client.projectsAPI.putCalls)
	}
}

func TestGrantProjectRoleShowsTheAPIRefusalsAndStaysOpen(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		robot bool
		err   error
		want  string
	}{
		{"robot already holds a grant", true, adminStatusError{status: 409, message: "x", detail: "robot already has a grant; a robot holds one grant"},
			"release-bot already has a grant. A robot holds one grant: remove it first."},
		{"project admin cannot grant admin", false, adminStatusError{status: 403, message: "x", detail: "project administrators cannot grant project admin"},
			"Could not grant role: project administrators cannot grant project admin."},
		{"read-only user", false, adminStatusError{status: 422, message: "x", detail: "read-only users can only be readers"},
			"Could not grant role: read-only users can only be readers."},
		{"unknown member", false, adminNotFoundError{message: "x", detail: "user not found"}, "Could not grant role: user not found."},
		{"failure", false, fmt.Errorf("connection reset"), "Could not grant role: connection reset."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := membersClient()
			client.projectsAPI.putErr = tc.err
			m := grantRoleModelAt(t, client, 150, 30)
			if tc.robot {
				m = projectsPress(t, m, testKeyRight())
			}
			name := "dave"
			if tc.robot {
				name = "release-bot"
			}
			m = projectsPress(t, m, testKeyTab())
			m = projectsPress(t, m, typeText(name)...)
			m = projectsPress(t, m, testKeyEnter(), testKeyEnter())
			s := grantRoleScreenOf(t, m)
			if m.screen != screenAdminGrantProjectRole || s.applying || s.member != name {
				t.Fatalf("screen %s applying %v member %q: the form must stay open as it was", m.screen, s.applying, s.member)
			}
			requireContains(t, projectsRender(m), tc.want)
		})
	}
}

func TestGrantProjectRoleWithoutAListTypesTheExactUsername(t *testing.T) {
	t.Parallel()
	client := membersClient()
	client.usersErr = adminStatusError{status: 403, message: "x", detail: "forbidden"}
	client.listRobotsErr = adminStatusError{status: 403, message: "x", detail: "forbidden"}
	m := grantRoleModelAt(t, client, 150, 30)
	requireContains(t, projectsRender(m), "The list is not available to this session. Type the exact username.")
	m = projectsPress(t, m, testKeyTab())
	m = projectsPress(t, m, typeText("erin")...)
	m = projectsPress(t, m, testKeyEnter(), testKeyEnter())
	if len(client.projectsAPI.putCalls) != 1 || client.projectsAPI.putCalls[0].Username != "erin" {
		t.Fatalf("put calls = %+v", client.projectsAPI.putCalls)
	}
}

func TestGrantProjectRoleNeedsAMember(t *testing.T) {
	t.Parallel()
	client := membersClient()
	m := grantRoleModelAt(t, client, 150, 30)
	m = projectsPress(t, m, testKeyEnter()) // on Member type: nobody picked
	m = projectsPress(t, m, testKeyTab(), testKeyTab(), testKeyEnter())
	requireContains(t, projectsRender(m), "Pick who gets the role first.")
	if len(client.projectsAPI.putCalls) != 0 {
		t.Fatalf("a grant without a member was sent: %+v", client.projectsAPI.putCalls)
	}
}

func TestGrantProjectRoleEscCancelsAndIsIgnoredWhileApplying(t *testing.T) {
	t.Parallel()
	client := membersClient()
	m := grantRoleModelAt(t, client, 150, 30)
	m = projectsPress(t, m, testKeyEsc())
	if m.screen != screenAdminProjectMembers || len(client.projectsAPI.putCalls) != 0 {
		t.Fatalf("Esc: screen %s, calls %v", m.screen, client.projectsAPI.putCalls)
	}
	m = grantRoleModelAt(t, client, 150, 30)
	m = projectsPress(t, m, testKeyTab())
	m = projectsPress(t, m, typeText("dave")...)
	m = projectsPress(t, m, testKeyEnter())
	updated, cmd := m.Update(testKeyEnter())
	m = updated.(Model)
	if _, esc := m.Update(testKeyEsc()); esc != nil {
		t.Fatal("Esc left the form while the grant was in flight")
	}
	_ = repoAccessRun(t, m, cmd)
}

func TestGrantProjectRoleExpiredSessionRedirectsToLogin(t *testing.T) {
	t.Parallel()
	expired := NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	client := membersClient()
	m := grantRoleModelAt(t, client, 150, 30)
	client.projectsAPI.putErr = expired
	m = projectsPress(t, m, testKeyTab())
	m = projectsPress(t, m, typeText("dave")...)
	m = projectsPress(t, m, testKeyEnter(), testKeyEnter())
	if m.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want the login", m.screen)
	}
	// The principal reads expire the same way.
	client = membersClient()
	client.usersErr = expired
	if m := grantRoleModelAt(t, client, 150, 30); m.screen != screenAdminLogin {
		t.Fatalf("screen = %q after an expired principals read", m.screen)
	}
}

func TestGrantProjectRoleMessagesOfAnotherInstanceAreDropped(t *testing.T) {
	t.Parallel()
	m := grantRoleModelAt(t, membersClient(), 150, 30)
	s := grantRoleScreenOf(t, m)
	expired := NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	for _, msg := range []any{
		grantProjectRolePrincipalsMsg{gen: s.gen + 1, usersErr: expired},
		grantProjectRoleDoneMsg{gen: s.gen + 1, project: "odabank", username: "dave", err: expired},
		grantProjectRoleDoneMsg{gen: s.gen + 1, project: "odabank", username: "dave"},
	} {
		updated, _ := m.Update(msg)
		m = updated.(Model)
		if m.screen != screenAdminGrantProjectRole {
			t.Fatalf("%T of another instance changed the screen to %s", msg, m.screen)
		}
	}
}

func TestGrantProjectRolePreviewFollowsTheRole(t *testing.T) {
	t.Parallel()
	m := grantRoleModelAt(t, membersClient(), 150, 30)
	m = projectsPress(t, m, testKeyTab())
	m = projectsPress(t, m, typeText("dave")...)
	m = projectsPress(t, m, testKeyEnter(), testKeyRight())
	requireContains(t, projectsRender(m), "dave becomes Project writer of odabank", "Read and write access to every repository in odabank, existing and future.")
	m = projectsPress(t, m, testKeyRight())
	requireContains(t, projectsRender(m), "dave becomes Project admin of odabank", "the right to give others Reader or Writer access to it")
	if strings.Contains(projectsRender(m), "Project admin is not available for robots") {
		t.Fatal("the robot refusal showed for a user")
	}
}
