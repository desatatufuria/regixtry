package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

var projectsRepos = []string{"odabank/api", "odabank/payments", "odabank/web", "odabank2/web", "odabank", "team/api"}

func projectMember(name string, robot bool, role domainauth.RepoRole, month time.Month, day int) ports.AdminProjectMember {
	at := projectDay(month, day)
	return ports.AdminProjectMember{Username: name, IsRobot: robot, Role: role, CreatedAt: at, UpdatedAt: at}
}

// membersClient serves the board's odabank: five members, three repositories.
func membersClient() *fakeAdminClient {
	return &fakeAdminClient{
		projectsAPI: fakeProjectsAPI{
			projects: projectsBoardSample(),
			members: map[string][]ports.AdminProjectMember{
				"odabank": {
					projectMember("ana", false, domainauth.RepoRoleAdmin, time.November, 3),
					projectMember("bob", false, domainauth.RepoRoleReader, time.November, 4),
					projectMember("carol", false, domainauth.RepoRoleWriter, time.November, 10),
					projectMember("ci-publisher", true, domainauth.RepoRoleWriter, time.January, 12),
					projectMember("dave", false, domainauth.RepoRoleReader, time.November, 12),
				},
			},
		},
		users: []ports.AdminUser{
			{ID: "u-ana", Username: "ana", Enabled: true}, {ID: "u-bob", Username: "bob", Enabled: true},
			{ID: "u-carol", Username: "carol", Enabled: true}, {ID: "u-dave", Username: "dave", Enabled: true},
			{ID: "u-dana", Username: "dana", Enabled: true}, {ID: "u-erin", Username: "erin", Enabled: true},
		},
		robots: []ports.AdminRobot{
			{ID: "r-ci", Username: "ci-publisher", Repository: "", Project: "odabank", Role: domainauth.RepoRoleWriter, Enabled: true},
			{ID: "r-free", Username: "release-bot", Enabled: true},
		},
	}
}

func membersModelAt(t *testing.T, client AdminClient, w, h int, project string) Model {
	t.Helper()
	m := projectsAt(t, client, w, h, projectsRepos...)
	updated, cmd := m.Update(openProjectMembersMsg{project: project})
	return repoAccessRun(t, updated.(Model), cmd)
}

func membersScreenOf(t *testing.T, m Model) adminProjectMembersScreen {
	t.Helper()
	s, ok := m.adminScreens[slotProjectMembers].(adminProjectMembersScreen)
	if !ok {
		t.Fatalf("slot holds %T, want adminProjectMembersScreen", m.adminScreens[slotProjectMembers])
	}
	return s
}

func memberNames(s adminProjectMembersScreen) string {
	var names []string
	for _, member := range s.members {
		names = append(names, member.Username)
	}
	return strings.Join(names, ",")
}

func TestProjectMembersShowTheBoard(t *testing.T) {
	t.Parallel()
	client := membersClient()
	m := membersModelAt(t, client, 150, 30, "odabank")
	if m.screen != screenAdminProjectMembers {
		t.Fatalf("screen = %q", m.screen)
	}
	out := projectsRender(m)
	requireContains(t, out, "Admin › Access › Projects › odabank › Members", "odabank",
		"project, created 2026-11-02, 3 repositories, 5 members",
		"MEMBERS", "MEMBER", "TYPE", "PROJECT ROLE", "SINCE",
		"▸ ana", "Project admin", "carol", "Project writer", "ci-publisher", "robot", "Project reader", "10 Nov",
		"REPOSITORIES IN THIS PROJECT", "odabank/api", "odabank/payments", "odabank/web",
		"3 repositories. Browse them in Console › Projects › odabank.",
		"A project role covers every repository inside odabank, now and later. A repository grant can only add access.",
		"tab repositories", "↑↓ move", "g add member", "x revoke", "esc back", "q quit")
	requireAbsent(t, out, "odabank2/web", "team/api")
	// Exactly one pushed repository named odabank (flat) must not be counted.
	if client.projectsAPI.getCalls != 1 || len(client.projectsAPI.memberReads) != 1 {
		t.Fatalf("reads: project %d, members %v", client.projectsAPI.getCalls, client.projectsAPI.memberReads)
	}
}

func TestProjectMembersFollowEveryPage(t *testing.T) {
	t.Parallel()
	client := membersClient()
	client.projectsAPI.memberPageSize = 2
	m := membersModelAt(t, client, 150, 30, "odabank")
	if got := memberNames(membersScreenOf(t, m)); got != "ana,bob,carol,ci-publisher,dave" {
		t.Fatalf("members = %s, want every page in order", got)
	}
	if fmt.Sprint(client.projectsAPI.memberReads) != "[odabank| odabank|c2 odabank|c4]" {
		t.Fatalf("reads = %v, want the three pages by their cursors", client.projectsAPI.memberReads)
	}
}

func TestProjectMembersTabMovesFocusBetweenThePanes(t *testing.T) {
	t.Parallel()
	client := membersClient()
	m := membersModelAt(t, client, 150, 30, "odabank")
	m = projectsPress(t, m, testKeyTab())
	out := projectsRender(m)
	requireContains(t, out, "tab members", "↑↓ move", "esc back")
	requireAbsent(t, out, "g add member", "x revoke", "▸ ana")
	m = projectsPress(t, m, testKeyDown(), testKeyDown())
	if s := membersScreenOf(t, m); s.repoCursor != 2 || s.selectedUser != "ana" {
		t.Fatalf("repo cursor %d, selected member %q", s.repoCursor, s.selectedUser)
	}
	requireContains(t, projectsRender(m), "▸ odabank/web")
	// g and x do nothing while the repositories have the focus.
	m = projectsPress(t, m, testKeyRunes("x"), testKeyRunes("g"))
	if membersScreenOf(t, m).confirm.Active() || m.screen != screenAdminProjectMembers {
		t.Fatal("x or g acted while the repositories had the focus")
	}
	m = projectsPress(t, m, testKeyTab())
	requireContains(t, projectsRender(m), "tab repositories", "x revoke")
}

func TestProjectMembersStates(t *testing.T) {
	t.Parallel()
	t.Run("loading", func(t *testing.T) {
		t.Parallel()
		m := projectsAt(t, membersClient(), 150, 30, projectsRepos...)
		updated, _ := m.Update(openProjectMembersMsg{project: "odabank"})
		out := projectsRender(updated.(Model))
		requireContains(t, out, "Loading members…")
		requireAbsent(t, out, "g add member", "x revoke")
	})
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		client := membersClient()
		client.projectsAPI.members = nil
		m := membersModelAt(t, client, 150, 30, "odabank")
		out := projectsRender(m)
		requireContains(t, out, "No members yet. Press g to give a user or robot a project role. Global", "administrators always have access.", "g add member", "0 members")
		requireAbsent(t, out, "x revoke")
		m = projectsPress(t, m, testKeyRunes("x"))
		if membersScreenOf(t, m).confirm.Active() {
			t.Fatal("x opened a dialog on an empty list")
		}
	})
	t.Run("error and retry", func(t *testing.T) {
		t.Parallel()
		client := membersClient()
		client.projectsAPI.membersErr = map[string]error{"odabank": fmt.Errorf("connection refused")}
		m := membersModelAt(t, client, 150, 30, "odabank")
		requireContains(t, projectsRender(m), "Could not load members: connection refused.", "Press r to retry.", "r retry")
		client.projectsAPI.membersErr = nil
		m = projectsPress(t, m, testKeyRunes("r"))
		requireContains(t, projectsRender(m), "carol", "5 members")
	})
	t.Run("unknown project", func(t *testing.T) {
		t.Parallel()
		client := membersClient()
		client.projectsAPI.membersErr = map[string]error{"ghost": adminNotFoundError{message: "x", detail: "project not found"}}
		m := membersModelAt(t, client, 150, 30, "ghost")
		out := projectsRender(m)
		requireContains(t, out, "Project ghost does not exist.", "esc back")
		requireAbsent(t, out, "g add member")
	})
	t.Run("a session that cannot read the project record still lists members", func(t *testing.T) {
		t.Parallel()
		client := membersClient()
		client.projectsAPI.getErr = adminStatusError{status: 403, message: "x", detail: "forbidden"}
		m := membersModelAt(t, client, 150, 30, "odabank")
		requireContains(t, projectsRender(m), "project, 3 repositories, 5 members", "carol")
	})
}

func TestProjectMembersRevokeConfirmsThenRevokesExactlyThatMember(t *testing.T) {
	t.Parallel()
	client := membersClient()
	m := membersModelAt(t, client, 150, 40, "odabank")
	m = projectsPress(t, m, testKeyDown(), testKeyDown(), testKeyRunes("x")) // carol
	requireContains(t, projectsRender(m), "Revoke Project writer?",
		"carol loses Writer access to every repository in odabank, unless an exact repository grant also gives it. It applies from the next request; no token is revoked.",
		"carol on project odabank", "Project writer since 2026-11-10", "Grant a project role again to restore it.",
		"Revoke", "y / enter", "Cancel", "esc")
	if len(client.projectsAPI.deleteMemberCalls) != 0 {
		t.Fatal("x revoked without confirmation")
	}
	updated, cmd := m.Update(testKeyEnter())
	m = updated.(Model)
	if _, again := m.Update(testKeyRunes("y")); again != nil {
		t.Fatal("a second confirm key while the revoke is in flight dispatched a command")
	}
	m = repoAccessRun(t, m, cmd)
	if fmt.Sprint(client.projectsAPI.deleteMemberCalls) != "[odabank|carol]" {
		t.Fatalf("calls = %v", client.projectsAPI.deleteMemberCalls)
	}
	s := membersScreenOf(t, m)
	if memberNames(s) != "ana,bob,ci-publisher,dave" || s.confirm.Active() || s.revoking {
		t.Fatalf("members %s confirm=%v revoking=%v", memberNames(s), s.confirm.Active(), s.revoking)
	}
	requireContains(t, projectsRender(m), "Revoked carol on project odabank.", "4 members")
}

func TestProjectMembersEscCancelsTheRevoke(t *testing.T) {
	t.Parallel()
	client := membersClient()
	m := membersModelAt(t, client, 150, 40, "odabank")
	m = projectsPress(t, m, testKeyRunes("x"), testKeyEsc())
	if membersScreenOf(t, m).confirm.Active() || len(client.projectsAPI.deleteMemberCalls) != 0 {
		t.Fatal("Esc did not cancel, or a revoke was sent")
	}
}

func TestProjectMembersRevokeOutcomes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		member     int // index of the member to revoke
		err        error
		want       string
		wantRows   string
		wantReload bool
	}{
		{"already revoked", 2, adminNotFoundError{message: "x", detail: "no grant"},
			"carol no longer has a role on project odabank. The list was refreshed.", "ana,bob,carol,ci-publisher,dave", true},
		{"a project admin cannot revoke an admin", 0, adminStatusError{status: 403, message: "x", detail: "project administrators cannot modify another project administrator's grant"},
			"Only a global administrator can revoke a project admin.", "ana,bob,carol,ci-publisher,dave", false},
		{"forbidden for a non-admin row shows the API text", 1, adminStatusError{status: 403, message: "x", detail: "forbidden: not an administrator of odabank"},
			"Could not revoke bob: forbidden: not an administrator of odabank.", "ana,bob,carol,ci-publisher,dave", false},
		{"other failure keeps the list", 2, fmt.Errorf("connection reset"),
			"Could not revoke carol: connection reset.", "ana,bob,carol,ci-publisher,dave", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := membersClient()
			m := membersModelAt(t, client, 150, 40, "odabank")
			for range tc.member {
				m = projectsPress(t, m, testKeyDown())
			}
			m = projectsPress(t, m, testKeyRunes("x"))
			reads := len(client.projectsAPI.memberReads)
			client.projectsAPI.deleteMemberErr = tc.err
			m = projectsPress(t, m, testKeyEnter())
			s := membersScreenOf(t, m)
			if s.confirm.Active() || s.revoking {
				t.Fatalf("confirm=%v revoking=%v after the result", s.confirm.Active(), s.revoking)
			}
			requireContains(t, projectsRender(m), tc.want)
			if memberNames(s) != tc.wantRows {
				t.Fatalf("members = %s, want %s", memberNames(s), tc.wantRows)
			}
			if (len(client.projectsAPI.memberReads) > reads) != tc.wantReload {
				t.Fatalf("reload = %v, want %v", len(client.projectsAPI.memberReads) > reads, tc.wantReload)
			}
		})
	}
}

func TestProjectMembersStaleReloadCannotBringARevokedRowBack(t *testing.T) {
	t.Parallel()
	client := membersClient()
	m := membersModelAt(t, client, 150, 40, "odabank")
	s := membersScreenOf(t, m)
	stale := adminProjectMembersLoadedMsg{gen: s.gen, seq: projectMembersLoadSeq.Add(1), members: append([]ports.AdminProjectMember(nil), s.members...)}
	m = projectsPress(t, m, testKeyDown(), testKeyDown(), testKeyRunes("x"), testKeyEnter())
	updated, _ := m.Update(stale) // read that started before the revoke lands after it
	m = updated.(Model)
	if got := memberNames(membersScreenOf(t, m)); got != "ana,bob,ci-publisher,dave" {
		t.Fatalf("members = %s: a stale read brought carol back", got)
	}
}

func TestProjectMembersConfirmTargetIsTheMemberThatWasOpened(t *testing.T) {
	t.Parallel()
	for seed := range 25 {
		client := membersClient()
		m := membersModelAt(t, client, 150, 40, "odabank")
		target := []string{"ana", "bob", "carol", "ci-publisher", "dave"}[seed%5]
		for i := 0; membersScreenOf(t, m).selectedUser != target && i < 10; i++ {
			m = projectsPress(t, m, testKeyDown())
		}
		m = projectsPress(t, m, testKeyRunes("x"))
		s := membersScreenOf(t, m)
		if !s.confirm.Active() || s.confirm.target != target+" on project odabank" {
			t.Fatalf("seed %d: confirm target = %q, want %q", seed, s.confirm.target, target)
		}
		reordered := []ports.AdminProjectMember{projectMember("zed", false, domainauth.RepoRoleReader, time.March, 1), projectMember("amy", false, domainauth.RepoRoleReader, time.March, 2)}
		if seed%2 == 0 {
			reordered = append(reordered, s.members[2:]...)
			reordered = append(reordered, s.members[:2]...)
		}
		updated, _ := m.Update(adminProjectMembersLoadedMsg{gen: s.gen, seq: projectMembersLoadSeq.Add(1), members: reordered})
		m = updated.(Model)
		m = projectsPress(t, m, testKeyEnter())
		if got := fmt.Sprint(client.projectsAPI.deleteMemberCalls); got != "[odabank|"+target+"]" {
			t.Fatalf("seed %d: calls = %s, want what the dialog named (%s)", seed, got, target)
		}
	}
}

func TestProjectMembersExpiredSessionRedirectsToLogin(t *testing.T) {
	t.Parallel()
	expired := NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	t.Run("load", func(t *testing.T) {
		t.Parallel()
		client := membersClient()
		client.projectsAPI.membersErr = map[string]error{"odabank": expired}
		if m := membersModelAt(t, client, 150, 30, "odabank"); m.screen != screenAdminLogin {
			t.Fatalf("screen = %q", m.screen)
		}
	})
	t.Run("revoke", func(t *testing.T) {
		t.Parallel()
		client := membersClient()
		m := membersModelAt(t, client, 150, 30, "odabank")
		client.projectsAPI.deleteMemberErr = expired
		m = projectsPress(t, m, testKeyRunes("x"), testKeyEnter())
		if m.screen != screenAdminLogin {
			t.Fatalf("screen = %q", m.screen)
		}
	})
}

func TestProjectMembersMessagesOfAnotherInstanceAreDropped(t *testing.T) {
	t.Parallel()
	m := membersModelAt(t, membersClient(), 150, 30, "odabank")
	s := membersScreenOf(t, m)
	expired := NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	for _, msg := range []any{
		adminProjectMembersLoadedMsg{gen: s.gen + 1, seq: projectMembersLoadSeq.Add(1), err: expired},
		adminProjectMemberRevokedMsg{gen: s.gen + 1, username: "carol", err: expired},
		adminProjectMemberRevokedMsg{gen: s.gen + 1, username: "carol"},
	} {
		updated, _ := m.Update(msg)
		m = updated.(Model)
		if m.screen != screenAdminProjectMembers || len(membersScreenOf(t, m).members) != 5 {
			t.Fatalf("%T of another instance changed the screen", msg)
		}
	}
}

func TestProjectMembersGOpensGrantRoleAndEscGoesBackToProjects(t *testing.T) {
	t.Parallel()
	m := membersModelAt(t, membersClient(), 150, 30, "odabank")
	// On a member: the form opens on that member, to change the role.
	m = projectsPress(t, m, testKeyDown(), testKeyDown(), testKeyRunes("g"))
	if m.screen != screenAdminGrantProjectRole {
		t.Fatalf("screen = %q, want the grant form", m.screen)
	}
	g := grantRoleScreenOf(t, m)
	if g.member != "carol" || g.role != domainauth.RepoRoleWriter || g.kind != grantKindUser || g.focus != grantProjectFieldRole || g.project != "odabank" {
		t.Fatalf("form = %+v, want carol, writer, user, on the role field", g)
	}
	m = projectsPress(t, m, testKeyEsc())
	if m.screen != screenAdminProjectMembers {
		t.Fatalf("Esc went to %q, want the members", m.screen)
	}
	m = projectsPress(t, m, testKeyEsc())
	if m.screen != screenAdminProjects {
		t.Fatalf("Esc went to %q, want the Projects list", m.screen)
	}
}

func TestProjectMembersAreListedByNameNotByServerOrder(t *testing.T) {
	t.Parallel()
	client := membersClient()
	list := client.projectsAPI.members["odabank"]
	reversed := []ports.AdminProjectMember{list[4], list[2], list[0], list[3], list[1]}
	client.projectsAPI.members["odabank"] = reversed
	m := membersModelAt(t, client, 150, 30, "odabank")
	if got := memberNames(membersScreenOf(t, m)); got != "ana,bob,carol,ci-publisher,dave" {
		t.Fatalf("members = %s, want them by name", got)
	}
}
