package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

func membersPress(t *testing.T, s adminScreen, env screenEnv, keys ...keyMsg) (adminScreen, []tea.Msg) {
	t.Helper()
	var all []tea.Msg
	for _, k := range keys {
		next, cmd, _ := s.Update(env, k)
		settled, out := projectsSettle(next, env, cmd)
		s = settled
		all = append(all, out...)
	}
	return s, all
}

func openMembers(t *testing.T, env screenEnv, project string) adminProjectMembersScreen {
	t.Helper()
	s := newAdminProjectMembersScreen(project, "")
	next, _ := projectsSettle(s, env, s.Init(env))
	return next.(adminProjectMembersScreen)
}

// grantThroughTheForm fills the Grant project role form the way an operator
// would and returns the outgoing request (the members to open) or the form.
func grantThroughTheForm(t *testing.T, env screenEnv, project, member string, robot bool, rights ...keyMsg) (grantProjectRoleScreen, []tea.Msg) {
	t.Helper()
	s := newGrantProjectRoleScreen(openGrantProjectRoleMsg{project: project})
	next, _ := projectsSettle(s, env, s.Init(env))
	keys := []keyMsg{}
	if robot {
		keys = append(keys, testKeyRight())
	}
	keys = append(keys, testKeyTab())
	keys = append(keys, repoAccessType(member)...)
	keys = append(keys, testKeyEnter())
	keys = append(keys, rights...)
	keys = append(keys, testKeyEnter())
	settled, out := membersPress(t, next, env, keys...)
	return settled.(grantProjectRoleScreen), out
}

// TestProjectMembersThroughTheRealStack drives the members and grant screens
// through the real HTTP client, router, services and stores: grant, list,
// change a role, revoke, odabank apart from odabank2, a project robot as a
// member, and the refusals the API owns.
func TestProjectMembersThroughTheRealStack(t *testing.T) {
	w := newProjectsWire(t, true)
	ctx := context.Background()
	for _, name := range []string{"odabank", "odabank2"} {
		if _, err := w.client.CreateProject(ctx, w.session, name); err != nil {
			t.Fatal(err)
		}
	}
	w.user("carol", false)
	w.user("dave", false)
	w.user("viewer", true)
	env := w.env(w.session, 160, 30, "odabank/api", "odabank2/web")

	// Grant carol writer on odabank through the form: it asks for the members.
	form, out := grantThroughTheForm(t, env, "odabank", "carol", false, testKeyRight())
	if len(out) != 1 {
		t.Fatalf("outgoing = %v (form err %q)", out, form.err)
	}
	if open, ok := out[0].(openProjectMembersMsg); !ok || open.project != "odabank" || open.toast != "carol is now Project writer of odabank." {
		t.Fatalf("outgoing = %#v", out[0])
	}
	// A user the form cannot refuse itself: the API's 422 for a read-only user.
	form, out = grantThroughTheForm(t, env, "odabank", "viewer", false, testKeyRight())
	if len(out) != 0 || !strings.Contains(form.err, "Could not grant role:") {
		t.Fatalf("read-only writer grant: out %v err %q", out, form.err)
	}
	if _, out = grantThroughTheForm(t, env, "odabank", "dave", false); len(out) != 1 {
		t.Fatalf("dave grant outgoing = %v", out)
	}

	members := openMembers(t, env, "odabank")
	if got := memberNames(members); got != "carol,dave" {
		t.Fatalf("odabank members = %s", got)
	}
	if other := openMembers(t, env, "odabank2"); len(other.members) != 0 {
		t.Fatalf("odabank2 members = %s: odabank roles must not leak", memberNames(other))
	}
	if members.info.Name != "odabank" || !members.hasInfo {
		t.Fatalf("project record: %+v", members.info)
	}

	// Revoke carol from the screen; dave and odabank2 are untouched.
	var s adminScreen = members
	s, _ = membersPress(t, s, env, testKeyRunes("x"), testKeyEnter())
	members = s.(adminProjectMembersScreen)
	if memberNames(members) != "dave" || !strings.Contains(members.status, "Revoked carol on project odabank.") {
		t.Fatalf("after revoke: members %s status %q", memberNames(members), members.status)
	}
	if grants, _ := w.client.ListUserProjectGrants(ctx, w.session, userID(t, w, "carol")); len(grants) != 0 {
		t.Fatalf("carol keeps %v", grants)
	}
	// Revoking again is a 404: the screen says so and refreshes.
	stale := members
	stale.members = append([]ports.AdminProjectMember{{Username: "carol", Role: domainauth.RepoRoleWriter}}, members.members...)
	stale.selectedUser = "carol"
	s, _ = membersPress(t, stale, env, testKeyRunes("x"), testKeyEnter())
	if got := s.(adminProjectMembersScreen); memberNames(got) != "dave" || !strings.Contains(got.status, "no longer has a role") {
		t.Fatalf("second revoke: members %s status %q", memberNames(got), got.status)
	}

	// A project robot is listed as a robot member and cannot be made admin.
	if _, err := w.client.CreateRobot(ctx, w.session, ports.AdminCreateRobotInput{Name: "release-bot", Project: "odabank", Role: domainauth.RepoRoleWriter}); err != nil {
		t.Fatal(err)
	}
	members = openMembers(t, env, "odabank")
	robots := 0
	for _, m := range members.members {
		if m.IsRobot && m.Username == "release-bot" {
			robots++
		}
	}
	if robots != 1 {
		t.Fatalf("members = %s, want the project robot once", memberNames(members))
	}
	if !strings.Contains(ansiBody(members, env), "robot") {
		t.Fatal("the robot is not shown as a robot")
	}
}

func userID(t *testing.T, w *projectsWire, name string) string {
	t.Helper()
	users, err := w.client.ListUsers(context.Background(), w.session)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if u.Username == name {
			return u.ID
		}
	}
	t.Fatalf("no user %s", name)
	return ""
}

func ansiBody(s adminScreen, env screenEnv) string {
	return ansi.Strip(s.View(newAdminTheme(), env).Body)
}

// TestProjectAdminSeesOnlyItsProjectAndCannotGrantAdmin: a session that is
// admin of odabank (not a global administrator) lists odabank's members only,
// grants Reader and Writer there, and gets the API's refusals for everything
// else, shown cleanly.
func TestProjectAdminSeesOnlyItsProjectAndCannotGrantAdmin(t *testing.T) {
	w := newProjectsWire(t, true)
	ctx := context.Background()
	for _, name := range []string{"odabank", "odabank2"} {
		if _, err := w.client.CreateProject(ctx, w.session, name); err != nil {
			t.Fatal(err)
		}
	}
	w.user("pat", false) // project admin of odabank
	w.user("ana", false) // another project admin of odabank
	w.user("dave", false)
	w.user("erin", false) // member of odabank2
	for _, g := range []struct{ project, user string }{{"odabank", "pat"}, {"odabank", "ana"}} {
		if _, err := w.auth.PutProjectGrant(ctx, w.admin, g.project, g.user, domainauth.RepoRoleAdmin); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.auth.PutProjectGrant(ctx, w.admin, "odabank2", "erin", domainauth.RepoRoleReader); err != nil {
		t.Fatal(err)
	}
	env := w.env(w.loginAs("pat"), 160, 30, "odabank/api")

	members := openMembers(t, env, "odabank")
	if got := memberNames(members); got != "ana,pat" || members.hasInfo {
		t.Fatalf("project admin sees %s (record read: %v), want odabank's members and no project record", got, members.hasInfo)
	}
	other := openMembers(t, env, "odabank2")
	if len(other.members) != 0 || other.loadErr == "" {
		t.Fatalf("odabank2 as a project admin of odabank: members %s err %q, want a refusal", memberNames(other), other.loadErr)
	}
	if strings.Contains(ansiBody(other, env), "erin") {
		t.Fatal("the other project's member leaked into the view")
	}

	// Grant reader: allowed. The member list of the form is not available.
	if _, out := grantThroughTheForm(t, env, "odabank", "dave", false); len(out) != 1 {
		t.Fatalf("a project admin granting a reader: outgoing %v", out)
	}
	// Grant admin: refused by the API, shown on the form, nothing changed.
	form, out := grantThroughTheForm(t, env, "odabank", "dave", false, testKeyRight(), testKeyRight())
	if len(out) != 0 || !strings.HasPrefix(form.err, "Could not grant role: ") || !strings.Contains(form.err, "administrator") {
		t.Fatalf("admin grant by a project admin: out %v err %q", out, form.err)
	}
	if got := memberNames(openMembers(t, env, "odabank")); got != "ana,dave,pat" {
		t.Fatalf("members = %s, dave must be a reader and nobody else changed", got)
	}
	// Revoke another project admin: refused with the board's text.
	var s adminScreen = openMembers(t, env, "odabank") // ana is first
	s, _ = membersPress(t, s, env, testKeyRunes("x"), testKeyEnter())
	got := s.(adminProjectMembersScreen)
	if got.status != "Only a global administrator can revoke a project admin." || memberNames(got) != "ana,dave,pat" {
		t.Fatalf("revoking a project admin: status %q members %s", got.status, memberNames(got))
	}
	// Revoke a reader: allowed.
	s, _ = membersPress(t, got, env, testKeyDown(), testKeyRunes("x"), testKeyEnter())
	if after := s.(adminProjectMembersScreen); memberNames(after) != "ana,pat" {
		t.Fatalf("after revoking dave: %s", memberNames(after))
	}
}
