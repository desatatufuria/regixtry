package tui

import (
	"context"
	"strings"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// TestProjectClientThroughTheRealStack drives every project client method
// through the real router, services and stores: create, list with counts,
// members (paged), roles, revoke, delete refused while the project holds a
// repository and allowed when empty, and odabank never covering odabank2.
func TestProjectClientThroughTheRealStack(t *testing.T) {
	w := newProjectsWire(t, true)
	ctx, c, s := context.Background(), w.client, w.session
	carol := w.user("carol", false)
	w.user("dave", false)

	for _, name := range []string{"odabank", "odabank2", "sandbox"} {
		if p, err := c.CreateProject(ctx, s, name); err != nil || p.Name != name {
			t.Fatalf("CreateProject(%s) = %#v, %v", name, p, err)
		}
	}
	if _, err := c.CreateProject(ctx, s, "odabank"); !IsAdminConflict(err) {
		t.Fatalf("duplicate create error = %v, want a 409", err)
	}
	if _, err := c.CreateProject(ctx, s, "Bad/Name"); !IsAdminUnprocessable(err) {
		t.Fatalf("invalid name error = %v, want a 422", err)
	}
	w.push("odabank/api")
	w.push("odabank2/web")

	projects, err := c.ListProjects(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, p := range projects {
		counts[p.Name] = p.Repositories
	}
	if len(projects) != 3 || counts["odabank"] != 1 || counts["odabank2"] != 1 || counts["sandbox"] != 0 {
		t.Fatalf("projects = %v, want odabank and odabank2 counted apart and sandbox empty", counts)
	}
	if !projects[2].LastPushed.IsZero() || projects[0].LastPushed.IsZero() {
		t.Fatalf("last pushed: odabank %v sandbox %v", projects[0].LastPushed, projects[2].LastPushed)
	}

	for _, name := range []string{"carol", "dave"} {
		if _, err := c.PutProjectMember(ctx, s, ports.AdminPutProjectGrantInput{Project: "odabank", Username: name, Role: domainauth.RepoRoleReader}); err != nil {
			t.Fatal(err)
		}
	}
	if m, err := c.PutProjectMember(ctx, s, ports.AdminPutProjectGrantInput{Project: "odabank", Username: "carol", Role: domainauth.RepoRoleWriter}); err != nil || m.Role != domainauth.RepoRoleWriter {
		t.Fatalf("role change = %#v, %v", m, err)
	}
	if _, err := c.PutProjectMember(ctx, s, ports.AdminPutProjectGrantInput{Project: "odabank", Username: "nobody", Role: domainauth.RepoRoleReader}); !IsAdminNotFound(err) {
		t.Fatalf("unknown user error = %v, want a 404", err)
	}
	page, err := c.ListProjectMembers(ctx, s, "odabank", 1, "")
	if err != nil || len(page.Members) != 1 || page.Next == "" {
		t.Fatalf("first page = %#v, %v", page, err)
	}
	rest, err := c.ListProjectMembers(ctx, s, "odabank", 1, page.Next)
	if err != nil || len(rest.Members) != 1 || rest.Members[0].Username == page.Members[0].Username {
		t.Fatalf("second page = %#v, %v", rest, err)
	}
	if other, err := c.ListProjectMembers(ctx, s, "odabank2", 0, ""); err != nil || len(other.Members) != 0 {
		t.Fatalf("odabank2 members = %#v, %v (odabank roles must not leak)", other, err)
	}
	grants, err := c.ListUserProjectGrants(ctx, s, carol.ID)
	if err != nil || len(grants) != 1 || grants[0].Project != "odabank" || grants[0].Role != domainauth.RepoRoleWriter {
		t.Fatalf("carol project grants = %#v, %v", grants, err)
	}

	if err := c.DeleteProject(ctx, s, "odabank"); !IsAdminConflict(err) || !strings.Contains(err.Error(), `project "odabank" still has 1 repository`) {
		t.Fatalf("delete of a non-empty project error = %v, want the 409 text", err)
	}
	if err := c.DeleteProject(ctx, s, "sandbox"); err != nil {
		t.Fatalf("delete of an empty project = %v", err)
	}
	if err := c.DeleteProject(ctx, s, "sandbox"); !IsAdminNotFound(err) {
		t.Fatalf("second delete error = %v, want a 404", err)
	}
	if err := c.DeleteProjectMember(ctx, s, "odabank", "carol"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteProjectMember(ctx, s, "odabank", "carol"); !IsAdminNotFound(err) {
		t.Fatalf("second revoke error = %v, want a 404", err)
	}
	if grants, _ := c.ListUserProjectGrants(ctx, s, carol.ID); len(grants) != 0 {
		t.Fatalf("carol keeps %v after the revoke", grants)
	}
}

func TestProjectDeletionDisabledIsAn422ThroughTheRealStack(t *testing.T) {
	w := newProjectsWire(t, false)
	ctx := context.Background()
	if _, err := w.client.CreateProject(ctx, w.session, "sandbox"); err != nil {
		t.Fatal(err)
	}
	err := w.client.DeleteProject(ctx, w.session, "sandbox")
	if !IsAdminUnprocessable(err) || !strings.Contains(adminErrorDetail(err), "project deletion is not enabled") {
		t.Fatalf("delete with deletion disabled error = %v", err)
	}
}

func TestProjectRobotAndRoleRefusalsThroughTheRealStack(t *testing.T) {
	w := newProjectsWire(t, true)
	ctx, c, s := context.Background(), w.client, w.session
	if _, err := c.CreateProject(ctx, s, "odabank"); err != nil {
		t.Fatal(err)
	}
	created, err := c.CreateRobot(ctx, s, ports.AdminCreateRobotInput{Name: "release-bot", Project: "odabank", Role: domainauth.RepoRoleWriter})
	if err != nil || created.Robot.Project != "odabank" || created.Robot.Repository != "" || created.Secret == "" {
		t.Fatalf("project robot = %#v, %v", created, err)
	}
	if _, err := c.CreateRobot(ctx, s, ports.AdminCreateRobotInput{Name: "admin-bot", Project: "odabank", Role: domainauth.RepoRoleAdmin}); !IsAdminUnprocessable(err) {
		t.Fatalf("a project robot with admin: error = %v, want a 422", err)
	}
	if _, err := c.CreateRobot(ctx, s, ports.AdminCreateRobotInput{Name: "lost-bot", Project: "ghost", Role: domainauth.RepoRoleReader}); !IsAdminNotFound(err) {
		t.Fatalf("a robot on an unknown project: error = %v, want a 404", err)
	}
	if _, err := c.PutProjectMember(ctx, s, ports.AdminPutProjectGrantInput{Project: "odabank", Username: "release-bot", Role: domainauth.RepoRoleAdmin}); !IsAdminUnprocessable(err) {
		t.Fatalf("admin for a robot: error = %v, want a 422", err)
	}
	w.user("viewer", true)
	if _, err := c.PutProjectMember(ctx, s, ports.AdminPutProjectGrantInput{Project: "odabank", Username: "viewer", Role: domainauth.RepoRoleWriter}); !IsAdminUnprocessable(err) {
		t.Fatalf("writer for a read-only user: error = %v, want a 422", err)
	}
	robots, err := c.ListRobots(ctx, s)
	if err != nil || len(robots) != 1 || robots[0].Project != "odabank" {
		t.Fatalf("robots = %#v, %v", robots, err)
	}
}
