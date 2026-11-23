package tui

import (
	"context"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"math/rand"
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

func projectGrant(project string, role domainauth.RepoRole, month time.Month, day int) ports.AdminProjectGrant {
	at := projectDay(month, day)
	return ports.AdminProjectGrant{Project: project, Role: role, CreatedAt: at, UpdatedAt: at}
}

// editProjectClient is carol (u-4): project roles on odabank (Writer) and
// gosse (Reader) next to the exact grants team/docs (Reader) and team/api.
func editProjectClient() *fakeAdminClient {
	client := editTestClient()
	client.projectsAPI.userGrants = map[string][]ports.AdminProjectGrant{
		"u-4": {
			projectGrant("gosse", domainauth.RepoRoleReader, time.October, 2),
			projectGrant("odabank", domainauth.RepoRoleWriter, time.November, 10),
		},
	}
	client.projectsAPI.members = map[string][]ports.AdminProjectMember{
		"odabank": {projectMember("carol", false, domainauth.RepoRoleWriter, time.November, 10)},
		"gosse":   {projectMember("carol", false, domainauth.RepoRoleReader, time.October, 2)},
	}
	return client
}

func editProjectRender(s editUserScreen, w, h int) string {
	env := usersTestEnv(nil, w, h)
	return renderEdit(s, env)
}

func TestEditUserListsProjectRolesNextToRepositoryGrants(t *testing.T) {
	t.Parallel()
	client := editProjectClient()
	s, env := editOpen(t, client, "u-4", "carol")
	if fmt.Sprint(client.projectsAPI.userGrantReads) != "[u-4]" {
		t.Fatalf("project role reads = %v, want one for u-4", client.projectsAPI.userGrantReads)
	}
	rows := s.accessRows()
	var got []string
	for _, row := range rows {
		got = append(got, row.label()+"|"+grantRoleLabel(row.role))
	}
	if strings.Join(got, ",") != "gosse/*|Reader,odabank/*|Writer,team/docs|Reader,team/api|Writer" {
		t.Fatalf("rows = %v, want the project roles first, then the exact grants", got)
	}
	big := usersTestEnv(client, 200, 40)
	out := renderEdit(s, big)
	requireContains(t, out, "PROJECT ACCESS AND REPOSITORY GRANTS", "g grant access", "REPOSITORY", "ROLE", "SOURCE",
		"gosse/*", "odabank/*", "Writer", "project", "team/docs", "team/api", "repo", "2 project roles, 2 repository grants.")
	_ = env
}

func TestEditUserProjectRowsAreSelectableAndRevocableWithTheFocusPattern(t *testing.T) {
	t.Parallel()
	client := editProjectClient()
	s, env := editOpen(t, client, "u-4", "carol")
	s, _ = editPress(t, s, env, testKeyTab())
	out := renderEdit(s, env)
	requireContains(t, out, "▸ gosse/*", "x revoke", "g grant access")
	s, _ = editPress(t, s, env, testKeyDown())
	s, _ = editPress(t, s, env, testKeyRunes("x"))
	body := ansi.Strip(s.View(newAdminTheme(), env).Overlay)
	requireDialog(t, body, "Revoke Project writer?",
		"carol loses Writer access to every repository in odabank, unless an exact repository grant also gives it. It applies from the next request; no token is revoked.",
		"carol on project odabank", "Project writer since 2026-11-10", "Grant a project role again to restore it.", "Revoke", "y / enter", "Cancel")
	if len(client.projectsAPI.deleteMemberCalls) != 0 {
		t.Fatal("x revoked without confirmation")
	}
	// Two confirm keys: the project role is revoked once, no repository grant is touched.
	next, cmd, _ := s.Update(env, testKeyEnter())
	s = next.(editUserScreen)
	if _, again, _ := s.Update(env, testKeyRunes("y")); again != nil {
		t.Fatal("a second confirm key while the revoke is in flight dispatched a command")
	}
	settled, _ := settleScreen(s, env, cmd)
	s = settled.(editUserScreen)
	if fmt.Sprint(client.projectsAPI.deleteMemberCalls) != "[odabank|carol]" || client.deleteRepoGrantErr != nil || client.lastDeleteRepoGrantRepo != "" {
		t.Fatalf("project revokes %v, repository revoke %q", client.projectsAPI.deleteMemberCalls, client.lastDeleteRepoGrantRepo)
	}
	if s.notice != "Revoked carol on project odabank." || s.noticeTone != userNoticeSuccess {
		t.Fatalf("notice = %q (%v)", s.notice, s.noticeTone)
	}
	var left []string
	for _, row := range s.accessRows() {
		left = append(left, row.label())
	}
	if strings.Join(left, ",") != "gosse/*,team/docs,team/api" {
		t.Fatalf("rows after the revoke = %v, want only odabank/* gone and the exact grants kept", left)
	}
}

func TestEditUserRepositoryRowStillRevokesTheRepositoryGrant(t *testing.T) {
	t.Parallel()
	client := newRevokeClient()
	client.projectsAPI.userGrants = map[string][]ports.AdminProjectGrant{"u-4": {projectGrant("odabank", domainauth.RepoRoleWriter, time.November, 10)}}
	s, env := revokeFocus(t, client)
	s, _ = editPress(t, s, env, testKeyDown()) // team/docs after odabank/*
	s, _ = editPress(t, s, env, testKeyRunes("x"), testKeyEnter())
	if got := client.deletes(); len(got) != 1 || got[0] != [2]string{"team/docs", "carol"} || len(client.projectsAPI.deleteMemberCalls) != 0 {
		t.Fatalf("repository revokes %v, project revokes %v", got, client.projectsAPI.deleteMemberCalls)
	}
}

func TestEditUserStaleReloadCannotBringARevokedProjectRoleBack(t *testing.T) {
	t.Parallel()
	client := editProjectClient()
	s, env := editOpen(t, client, "u-4", "carol")
	stale := editUserLoadedMsg{gen: s.gen, seq: editUserLoadSeq.Add(1), userID: "u-4", users: usersTestUsers(), grants: s.grants, projectGrants: s.projectGrants}
	s, _ = editPress(t, s, env, testKeyTab(), testKeyDown(), testKeyRunes("x"), testKeyEnter())
	next, _, _ := s.Update(env, stale) // read that started before the revoke lands after it
	s = next.(editUserScreen)
	for _, row := range s.accessRows() {
		if row.label() == "odabank/*" {
			t.Fatal("a stale read brought odabank/* back")
		}
	}
}

func TestEditUserProjectRevokeOutcomes(t *testing.T) {
	t.Parallel()
	t.Run("already gone refreshes", func(t *testing.T) {
		t.Parallel()
		client := editProjectClient()
		client.projectsAPI.deleteMemberErr = adminNotFoundError{message: "x", detail: "no grant"}
		client.projectsAPI.userGrants["u-4"] = client.projectsAPI.userGrants["u-4"][:1] // gone on the server meanwhile
		s, env := editOpen(t, client, "u-4", "carol")
		client.projectsAPI.userGrants["u-4"] = nil
		s, _ = editPress(t, s, env, testKeyTab(), testKeyRunes("x"), testKeyEnter())
		if s.noticeTone != userNoticeError || !strings.Contains(s.notice, "Could not revoke carol on project gosse:") {
			t.Fatalf("notice = %q (%v)", s.notice, s.noticeTone)
		}
		for _, row := range s.accessRows() {
			if row.project {
				t.Fatalf("the stale project row stayed: %v", s.accessRows())
			}
		}
	})
	t.Run("failure keeps the list and says why", func(t *testing.T) {
		t.Parallel()
		client := editProjectClient()
		client.projectsAPI.deleteMemberErr = fmt.Errorf("connection reset")
		s, env := editOpen(t, client, "u-4", "carol")
		s, _ = editPress(t, s, env, testKeyTab(), testKeyRunes("x"), testKeyEnter())
		if s.notice != "Could not revoke carol on project gosse: connection reset." || len(s.projectGrants) != 2 || s.confirm.Active() || s.revoking {
			t.Fatalf("notice %q, project roles %d, confirm %v, revoking %v", s.notice, len(s.projectGrants), s.confirm.Active(), s.revoking)
		}
	})
	t.Run("expired session", func(t *testing.T) {
		t.Parallel()
		msg := editUserProjectRevokedMsg{gen: 1, project: "odabank", username: "carol", err: NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)}
		if reason, expired := msg.expiredReason(); !expired || reason == "" {
			t.Fatal("expired result not reported as expired")
		}
	})
}

func TestEditUserProjectRolesFailingToLoadDoNotHideTheRepositoryGrants(t *testing.T) {
	t.Parallel()
	client := editProjectClient()
	client.projectsAPI.userGrantsErr = fmt.Errorf("project roles exploded")
	s, env := editOpen(t, client, "u-4", "carol")
	out := renderEdit(s, usersTestEnv(client, 200, 40))
	requireContains(t, out, "team/docs", "team/api", "Could not load project roles: project roles exploded.")
	_ = env
	if len(s.grants) != 2 || len(s.projectGrants) != 0 {
		t.Fatalf("grants %d, project roles %d", len(s.grants), len(s.projectGrants))
	}
}

func TestEditUserWithNoAccessAtAll(t *testing.T) {
	t.Parallel()
	client := editTestClient()
	client.grants = map[string][]ports.AdminRepoGrant{}
	s, env := editOpen(t, client, "u-4", "carol")
	requireContains(t, renderEdit(s, env), "carol has no project roles and no repository grants. Press g to grant access.")
	s, _ = editPress(t, s, env, testKeyTab(), testKeyRunes("x"))
	if s.grantsFocused || s.confirm.Active() {
		t.Fatal("tab or x acted on an empty access list")
	}
}

func TestEditUserProjectRevokeDialogTargetSurvivesReloads(t *testing.T) {
	t.Parallel()
	for seed := int64(0); seed < 100; seed++ {
		rng := rand.New(rand.NewSource(seed))
		client := editProjectClient()
		var projects []ports.AdminProjectGrant
		for i := range 1 + rng.Intn(6) {
			projects = append(projects, projectGrant(fmt.Sprintf("project-%02d", i), domainauth.RepoRoleReader, time.March, 1+i))
		}
		client.projectsAPI.userGrants["u-4"] = projects
		s, env := editOpen(t, client, "u-4", "carol")
		s, _ = editPress(t, s, env, testKeyTab())
		for range rng.Intn(10) {
			s, _ = editPress(t, s, env, testKeyDown())
		}
		row, _ := s.selectedRow()
		s, _ = editPress(t, s, env, testKeyRunes("x"))
		if !s.confirm.Active() {
			t.Fatalf("seed %d: no dialog", seed)
		}
		shuffled := append([]ports.AdminProjectGrant(nil), projects...)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		repoGrants := []ports.AdminRepoGrant{{UserID: "u-4", Repository: regixtrydomain.MustParseRepositoryRef("team/zzz"), Role: domainauth.RepoRoleReader}}
		next, _, _ := s.Update(env, editUserLoadedMsg{gen: s.gen, seq: editUserLoadSeq.Add(1), userID: s.userID, users: usersTestUsers(), grants: repoGrants, projectGrants: shuffled})
		s = next.(editUserScreen)
		s, _ = editPress(t, s, env, testKeyEnter())
		if row.project {
			if got := fmt.Sprint(client.projectsAPI.deleteMemberCalls); got != "["+row.name+"|carol]" {
				t.Fatalf("seed %d: revoked %s, the dialog named project %s", seed, got, row.name)
			}
		}
	}
}

func TestEditUserAccessSectionFitsEverySize(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 1, 100} {
		for _, size := range geometrySizes {
			t.Run(fmt.Sprintf("%d rows at %dx%d", count, size.w, size.h), func(t *testing.T) {
				t.Parallel()
				client := editTestClient()
				var projects []ports.AdminProjectGrant
				var repos []ports.AdminRepoGrant
				for i := range count {
					if i%2 == 0 {
						projects = append(projects, projectGrant(fmt.Sprintf("project-%03d-%s", i, strings.Repeat("long-", 10)), domainauth.RepoRoleWriter, time.March, 1+i%27))
						continue
					}
					repos = append(repos, ports.AdminRepoGrant{UserID: "u-4", Repository: regixtrydomain.MustParseRepositoryRef(fmt.Sprintf("team/repo-%03d-%send", i, strings.Repeat("long-", 10))), Role: domainauth.RepoRoleReader})
				}
				client.grants = map[string][]ports.AdminRepoGrant{"u-4": repos}
				client.projectsAPI.userGrants = map[string][]ports.AdminProjectGrant{"u-4": projects}
				env := usersTestEnv(client, size.w, size.h)
				s := newEditUserScreen("u-4", "carol")
				next, _ := settleScreen(s, env, s.Init(env))
				s = next.(editUserScreen)
				repoAccessNoOverflow(t, "edit user", ansi_(s.View(newAdminTheme(), env).Body), size.w, size.h)
				if count == 0 {
					return
				}
				s, _ = editPress(t, s, env, testKeyTab())
				for range 150 {
					s, _ = editPress(t, s, env, testKeyDown())
				}
				body := ansi_(s.View(newAdminTheme(), env).Body)
				repoAccessNoOverflow(t, "edit user focused", body, size.w, size.h)
				row, _ := s.selectedRow()
				if !strings.Contains(body, "▸ "+row.label()[:8]) {
					t.Fatalf("the selected row %q is not on screen:\n%s", row.label(), body)
				}
				s, _ = editPress(t, s, env, testKeyRunes("x"))
				frame := s.View(newAdminTheme(), env)
				repoAccessNoOverflow(t, "edit user dialog", ansi_(frame.Body), size.w, size.h)
			})
		}
	}
}

// ansi_ strips the escape sequences of a rendered view.
func ansi_(s string) string { return ansi.Strip(s) }

func TestEditUserProjectAccessGoldens(t *testing.T) {
	cases := []struct {
		name  string
		build func(w, h int) (editUserScreen, screenEnv)
	}{
		{"edit_user_project_access", func(w, h int) (editUserScreen, screenEnv) {
			s, _ := editOpen(t, editProjectClient(), "u-4", "carol")
			return s, usersTestEnv(editProjectClient(), w, h)
		}},
		{"edit_user_project_access_focused", func(w, h int) (editUserScreen, screenEnv) {
			s, env := editOpen(t, editProjectClient(), "u-4", "carol")
			env = usersTestEnv(editProjectClient(), w, h)
			s, _ = editPress(t, s, env, testKeyTab(), testKeyDown())
			return s, env
		}},
		{"edit_user_project_revoke", func(w, h int) (editUserScreen, screenEnv) {
			s, env := editOpen(t, editProjectClient(), "u-4", "carol")
			env = usersTestEnv(editProjectClient(), w, h)
			s, _ = editPress(t, s, env, testKeyTab(), testKeyDown(), testKeyRunes("x"))
			return s, env
		}},
		{"edit_user_project_roles_unavailable", func(w, h int) (editUserScreen, screenEnv) {
			c := editProjectClient()
			c.projectsAPI.userGrantsErr = fmt.Errorf("project roles exploded")
			s, _ := editOpen(t, c, "u-4", "carol")
			return s, usersTestEnv(c, w, h)
		}},
	}
	for _, tc := range cases {
		for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}, {220, 60}} {
			t.Run(fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h), func(t *testing.T) {
				s, env := tc.build(size.w, size.h)
				frame := s.View(newAdminTheme(), env)
				out := ansi.Strip(frame.Body)
				if frame.Overlay != "" {
					out = ansi.Strip(compositeOverlay(frame.Body, frame.Overlay, size.w, size.h))
				}
				repoAccessNoOverflow(t, tc.name, out, size.w, size.h)
				assertProjectsGolden(t, fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h), out)
			})
		}
	}
}

// TestEditUserProjectRoleRevokeThroughTheRealStack: carol holds a project role
// on odabank, an exact grant on odabank/api and a role on odabank2; revoking
// the odabank row from the screen removes that one project role only.
func TestEditUserProjectRoleRevokeThroughTheRealStack(t *testing.T) {
	w := newProjectsWire(t, true)
	ctx := context.Background()
	for _, name := range []string{"odabank", "odabank2"} {
		if _, err := w.client.CreateProject(ctx, w.session, name); err != nil {
			t.Fatal(err)
		}
	}
	carol := w.user("carol", false)
	for _, project := range []string{"odabank", "odabank2"} {
		if _, err := w.client.PutProjectMember(ctx, w.session, ports.AdminPutProjectGrantInput{Project: project, Username: "carol", Role: domainauth.RepoRoleReader}); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := w.store.PutRepoGrant(ctx, domainauth.RepoGrant{UserID: carol.ID, Repository: regixtrydomain.MustParseRepositoryRef("odabank/api"), Role: domainauth.RepoRoleWriter, CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	env := w.env(w.session, 200, 40)

	s := newEditUserScreen(carol.ID, "carol")
	next, _ := settleScreen(s, env, s.Init(env))
	s = next.(editUserScreen)
	var labels []string
	for _, row := range s.accessRows() {
		labels = append(labels, row.label())
	}
	if strings.Join(labels, ",") != "odabank/*,odabank2/*,odabank/api" {
		t.Fatalf("rows = %v", labels)
	}
	s, _ = editPress(t, s, env, testKeyTab(), testKeyRunes("x"), testKeyEnter())
	if s.notice != "Revoked carol on project odabank." {
		t.Fatalf("notice = %q (%v)", s.notice, s.noticeTone)
	}
	grants, err := w.client.ListUserProjectGrants(ctx, w.session, carol.ID)
	if err != nil || len(grants) != 1 || grants[0].Project != "odabank2" {
		t.Fatalf("project roles after the revoke = %v, %v: odabank2 must stay", grants, err)
	}
	repoGrants, err := w.client.ListUserGrants(ctx, w.session, carol.ID)
	if err != nil || len(repoGrants) != 1 || repoGrants[0].Repository.String() != "odabank/api" {
		t.Fatalf("repository grants after the revoke = %v, %v: the exact grant must stay", repoGrants, err)
	}
	labels = nil
	for _, row := range s.accessRows() {
		labels = append(labels, row.label())
	}
	if strings.Join(labels, ",") != "odabank2/*,odabank/api" {
		t.Fatalf("rows after the reload = %v", labels)
	}
}
