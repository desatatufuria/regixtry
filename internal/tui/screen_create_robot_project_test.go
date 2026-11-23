package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// robotProjectFixture is a Create robot form with the board's projects.
func robotProjectFixture(w, h int) (*accessClient, screenEnv, adminScreen) {
	client := newAccessClient()
	client.projectsAPI.projects = projectsBoardSample()
	env := accessEnvSized(client, w, h, createRobotKnown...)
	return client, env, newCreateRobotScreen()
}

// toProject types a name and switches the scope to Project, leaving the focus
// on the Project field.
func toProject(t *testing.T, s adminScreen, env screenEnv, name string) adminScreen {
	t.Helper()
	keys := append(accessType(name), testKeyTab(), testKeyRight(), testKeyTab())
	s, _ = accessPress(t, s, env, keys...)
	return s
}

func TestCreateRobotScopeSwitchesBetweenRepositoryAndProject(t *testing.T) {
	t.Parallel()
	client, env, s := robotProjectFixture(150, 40)
	out := accessView(s, env)
	mustContain(t, out, "Scope", "(*) Repository", "( ) Project", "Repository", "tab next field", "↑↓ pick repository", "←→ scope, role, lifetime")
	if client.projectsAPI.listCalls != 0 {
		t.Fatalf("projects read before the scope is Project: %d", client.projectsAPI.listCalls)
	}
	s, _ = accessPress(t, s, env, testKeyTab(), testKeyRight())
	out = accessView(s, env)
	mustContain(t, out, "(*) Project", "( ) Repository", "▸ gosse  2 repositories", "metalab  10 repositories", "odabank  3 repositories", "sandbox  0 repositories", "↑↓ pick project")
	if client.projectsAPI.listCalls != 1 {
		t.Fatalf("project reads = %d, want one on the first switch", client.projectsAPI.listCalls)
	}
	// Back and forth: the list is not read again, and each scope keeps its text.
	s, _ = accessPress(t, s, env, testKeyLeft(), testKeyRight())
	if client.projectsAPI.listCalls != 1 {
		t.Fatalf("project reads = %d after switching back and forth", client.projectsAPI.listCalls)
	}
}

func TestCreateRobotOnAProjectSendsTheProjectNotTheRepository(t *testing.T) {
	t.Parallel()
	client, env, s := robotProjectFixture(150, 40)
	client.createRobot = ports.AdminCreatedRobot{
		Robot:  ports.AdminRobot{ID: "r-9", Username: "release-bot", Project: "odabank", Role: domainauth.RepoRoleWriter},
		Secret: accessSecret, Accessor: "act_1", ExpiresAt: accessNow.Add(30 * day),
	}
	s = toProject(t, s, env, "release-bot")
	s, _ = accessPress(t, s, env, accessType("oda")...)
	mustContain(t, accessView(s, env), "▸ odabank  3 repositories")
	// Enter accepts the suggestion, then Role: Right selects Writer.
	s, _ = accessPress(t, s, env, testKeyEnter(), testKeyRight())
	mustContain(t, accessView(s, env), "Writer on every repository in odabank",
		"Includes repositories pushed later. Use scope Repository to limit the robot to one repository.")
	s, _ = accessPress(t, s, env, testKeyEnter())
	in := client.lastCreateRobotInput
	if in.Name != "release-bot" || in.Project != "odabank" || in.Repository != "" || in.Role != domainauth.RepoRoleWriter || client.createRobotCalls != 1 {
		t.Fatalf("CreateRobot input = %#v (calls %d)", in, client.createRobotCalls)
	}
	out := accessView(s, env)
	mustContain(t, out, "✓ Robot release-bot created, Writer on project odabank", "Scope", "project odabank", "Role", "Writer", "Covers", "every repository in odabank")
	mustNotContain(t, out, "Repository  ")
}

func TestCreateRobotOnARepositoryStillSendsTheRepository(t *testing.T) {
	t.Parallel()
	client, env, s := robotProjectFixture(150, 40)
	s = fillRobot(t, s, env, "release-bot", "team/api")
	s, _ = accessPress(t, s, env, testKeyEnter(), testKeyEnter())
	in := client.lastCreateRobotInput
	if in.Repository != "team/api" || in.Project != "" || client.createRobotCalls != 1 {
		t.Fatalf("CreateRobot input = %#v", in)
	}
}

func TestCreateRobotOnAProjectRefusesAdminBeforeSendingAnything(t *testing.T) {
	t.Parallel()
	client, env, s := robotProjectFixture(150, 40)
	s = toProject(t, s, env, "release-bot")
	s, _ = accessPress(t, s, env, accessType("odabank")...)
	s, _ = accessPress(t, s, env, testKeyEnter(), testKeyRight(), testKeyRight()) // Admin
	out := accessView(s, env)
	mustContain(t, out, "Robots on a project can be Reader or Writer only. Admin is for users.")
	mustNotContain(t, out, "on every repository in odabank")
	s, _ = accessPress(t, s, env, testKeyEnter())
	if got := s.(createRobotScreen); client.createRobotCalls != 0 || got.creating {
		t.Fatalf("Enter on project + Admin: calls %d creating %v", client.createRobotCalls, got.creating)
	}
	// Admin is fine on a repository: the refusal follows the scope.
	s, _ = accessPress(t, s, env, testKeyShiftTab(), testKeyShiftTab(), testKeyLeft())
	mustNotContain(t, accessView(s, env), "Robots on a project can be Reader or Writer only.")
}

func TestCreateRobotOnAProjectRefusesAnUnknownProjectWithoutARequest(t *testing.T) {
	t.Parallel()
	client, env, s := robotProjectFixture(150, 40)
	s = toProject(t, s, env, "release-bot")
	s, _ = accessPress(t, s, env, accessType("ghost")...)
	mustContain(t, accessView(s, env), "No match. Pick one from the list.")
	s, _ = accessPress(t, s, env, testKeyTab(), testKeyEnter()) // Role, then create
	if got := s.(createRobotScreen); got.err != "Project ghost does not exist. Pick one from the list." || client.createRobotCalls != 0 {
		t.Fatalf("err = %q calls = %d", got.err, client.createRobotCalls)
	}
}

func TestCreateRobotOnAProjectShowsTheAPIAnswers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"unknown project", adminNotFoundError{message: "x", detail: "project not found"}, "Project odabank does not exist. Pick one from the list."},
		{"refusal", adminStatusError{status: 422, message: "x", detail: "robots cannot be project admin"}, "Could not create robot: robots cannot be project admin."},
		{"failure", fmt.Errorf("connection reset"), "Could not create robot: connection reset."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client, env, s := robotProjectFixture(150, 40)
			client.createRobotErr = tc.err
			s = toProject(t, s, env, "release-bot")
			s, _ = accessPress(t, s, env, accessType("odabank")...)
			s, _ = accessPress(t, s, env, testKeyEnter(), testKeyEnter())
			got := s.(createRobotScreen)
			if got.done || got.creating || got.err != tc.want {
				t.Fatalf("done %v creating %v err %q, want the form open with %q", got.done, got.creating, got.err, tc.want)
			}
		})
	}
}

func TestCreateRobotProjectListStates(t *testing.T) {
	t.Parallel()
	t.Run("no projects", func(t *testing.T) {
		t.Parallel()
		client, env, s := robotProjectFixture(150, 40)
		client.projectsAPI.projects = nil
		s = toProject(t, s, env, "bot")
		mustContain(t, accessView(s, env), "No projects yet. Create one in Admin › Access › Projects.")
	})
	t.Run("list unavailable still allows a typed name", func(t *testing.T) {
		t.Parallel()
		client, env, s := robotProjectFixture(150, 40)
		client.projectsAPI.listErr = adminStatusError{status: 403, message: "x", detail: "forbidden"}
		s = toProject(t, s, env, "bot")
		mustContain(t, accessView(s, env), "Could not load projects: forbidden. Type the exact name.")
		s, _ = accessPress(t, s, env, accessType("odabank")...)
		s, _ = accessPress(t, s, env, testKeyEnter(), testKeyEnter())
		if client.createRobotCalls != 1 || client.lastCreateRobotInput.Project != "odabank" {
			t.Fatalf("calls %d input %#v", client.createRobotCalls, client.lastCreateRobotInput)
		}
	})
	t.Run("loading", func(t *testing.T) {
		t.Parallel()
		_, env, s := robotProjectFixture(150, 40)
		s, _ = accessPress(t, s, env, testKeyTab())
		next, cmd, _ := s.Update(env, testKeyRight())
		_ = cmd // the read is not delivered
		mustContain(t, accessView(next, env), "Loading…")
	})
	t.Run("expired session", func(t *testing.T) {
		t.Parallel()
		msg := createRobotProjectsMsg{gen: 1, err: NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)}
		if reason, expired := msg.expiredReason(); !expired || reason == "" {
			t.Fatal("expired result not reported")
		}
	})
	t.Run("a result of another instance is dropped", func(t *testing.T) {
		t.Parallel()
		_, env, s := robotProjectFixture(150, 40)
		next, _, _ := s.Update(env, createRobotProjectsMsg{gen: s.(createRobotScreen).gen + 1, projects: projectsBoardSample()})
		if next.(createRobotScreen).projectsRead != projectsNotRead {
			t.Fatal("a foreign project list landed on the form")
		}
	})
}

func TestCreateRobotProjectScopeFitsEverySize(t *testing.T) {
	t.Parallel()
	for _, size := range geometrySizes {
		for _, count := range []int{0, 1, 100} {
			t.Run(fmt.Sprintf("%d projects at %dx%d", count, size.w, size.h), func(t *testing.T) {
				t.Parallel()
				client, env, s := robotProjectFixture(size.w, size.h)
				client.projectsAPI.projects = nil
				for i := range count {
					client.projectsAPI.projects = append(client.projectsAPI.projects, ports.ProjectSummary{Name: fmt.Sprintf("project-%03d-%s", i, strings.Repeat("long-", 8)) + "end", Repositories: i})
				}
				s = toProject(t, s, env, "release-bot")
				steps := [][]keyMsg{nil, accessType("project"), {testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown()},
					{testKeyEnter(), testKeyRight()}, {testKeyRight()}, {testKeyTab(), testKeyRight(), testKeyRight(), testKeyRight(), testKeyRight(), testKeyRight()}}
				for i, keys := range steps {
					s, _ = accessPress(t, s, env, keys...)
					view := accessView(s, env)
					if lines := accessLines(view); len(lines) > size.h {
						t.Fatalf("step %d: %d rows, terminal has %d\n%s", i, len(lines), size.h, view)
					}
					repoAccessNoOverflow(t, fmt.Sprintf("step %d", i), s.View(newAdminTheme(), env).Body, size.w, size.h)
					mustContain(t, view, "Lifetime", "esc")
				}
			})
		}
	}
}

func TestCreateRobotProjectScopeGoldens(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T, w, h int) (adminScreen, screenEnv)
	}{
		{"create_robot_project_list", func(t *testing.T, w, h int) (adminScreen, screenEnv) {
			_, env, s := robotProjectFixture(w, h)
			s, _ = accessPress(t, s, env, testKeyTab(), testKeyRight())
			return s, env
		}},
		{"create_robot_project_summary", func(t *testing.T, w, h int) (adminScreen, screenEnv) {
			_, env, s := robotProjectFixture(w, h)
			s = toProject(t, s, env, "release-bot")
			s, _ = accessPress(t, s, env, accessType("odabank")...)
			s, _ = accessPress(t, s, env, testKeyEnter(), testKeyRight())
			return s, env
		}},
		{"create_robot_project_admin_refused", func(t *testing.T, w, h int) (adminScreen, screenEnv) {
			_, env, s := robotProjectFixture(w, h)
			s = toProject(t, s, env, "release-bot")
			s, _ = accessPress(t, s, env, accessType("odabank")...)
			s, _ = accessPress(t, s, env, testKeyEnter(), testKeyRight(), testKeyRight())
			return s, env
		}},
		{"create_robot_project_unknown", func(t *testing.T, w, h int) (adminScreen, screenEnv) {
			_, env, s := robotProjectFixture(w, h)
			s = toProject(t, s, env, "release-bot")
			s, _ = accessPress(t, s, env, accessType("ghost")...)
			s, _ = accessPress(t, s, env, testKeyTab(), testKeyEnter())
			return s, env
		}},
		{"create_robot_project_no_projects", func(t *testing.T, w, h int) (adminScreen, screenEnv) {
			client, env, s := robotProjectFixture(w, h)
			client.projectsAPI.projects = nil
			s = toProject(t, s, env, "release-bot")
			return s, env
		}},
		{"create_robot_project_done", func(t *testing.T, w, h int) (adminScreen, screenEnv) {
			client, env, s := robotProjectFixture(w, h)
			client.createRobot = ports.AdminCreatedRobot{
				Robot:  ports.AdminRobot{ID: "r-9", Username: "release-bot", Project: "odabank", Role: domainauth.RepoRoleWriter},
				Secret: accessSecret, Accessor: "act_c07e1b52d9a4", ExpiresAt: accessNow.Add(30 * day),
			}
			s = toProject(t, s, env, "release-bot")
			s, _ = accessPress(t, s, env, accessType("odabank")...)
			s, _ = accessPress(t, s, env, testKeyEnter(), testKeyRight(), testKeyEnter())
			return s, env
		}},
	}
	for _, tc := range cases {
		for _, size := range []struct{ w, h int }{{150, 24}, {220, 60}} {
			t.Run(fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h), func(t *testing.T) {
				s, env := tc.build(t, size.w, size.h)
				body := ansiStrip(s.View(newAdminTheme(), env).Body)
				repoAccessNoOverflow(t, tc.name, body, size.w, size.h)
				assertProjectsGolden(t, fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h), body)
			})
		}
	}
}

func ansiStrip(s string) string { return accessStrip(s) }

// TestCreateRobotOnAProjectThroughTheRealStack creates a project robot from
// the form through the real client, router, services and stores: the robot
// holds a role on the project only (odabank, never odabank2), appears as a
// project member, and the Robots list shows it as odabank/*.
func TestCreateRobotOnAProjectThroughTheRealStack(t *testing.T) {
	w := newProjectsWire(t, true)
	ctx := context.Background()
	for _, name := range []string{"odabank", "odabank2"} {
		if _, err := w.client.CreateProject(ctx, w.session, name); err != nil {
			t.Fatal(err)
		}
	}
	env := w.env(w.session, 160, 40)
	var s adminScreen = newCreateRobotScreen()
	s, _ = accessPress(t, s, env, append(accessType("release-bot"), testKeyTab(), testKeyRight(), testKeyTab())...)
	s, _ = accessPress(t, s, env, accessType("odabank")...)
	s, _ = accessPress(t, s, env, testKeyEnter(), testKeyRight(), testKeyEnter())
	got := s.(createRobotScreen)
	if !got.done || got.created.project != "odabank" || got.created.secret == "" {
		t.Fatalf("done %v created %+v err %q", got.done, got.created, got.err)
	}
	members, err := w.client.ListProjectMembers(ctx, w.session, "odabank", 0, "")
	if err != nil || len(members.Members) != 1 || !members.Members[0].IsRobot || members.Members[0].Role != domainauth.RepoRoleWriter {
		t.Fatalf("odabank members = %+v, %v", members, err)
	}
	if other, _ := w.client.ListProjectMembers(ctx, w.session, "odabank2", 0, ""); len(other.Members) != 0 {
		t.Fatalf("odabank2 members = %+v: the robot must not cover it", other)
	}
	robots := newRobotsScreen()
	next, _ := accessSettle(robots, env, robots.Init(env))
	view := accessView(next, env)
	mustContain(t, view, "release-bot", "odabank/*", "Writer")
	mustNotContain(t, view, "none")
	// Admin on a project: the form refuses it before anything is sent.
	s = newCreateRobotScreen()
	s, _ = accessPress(t, s, env, append(accessType("admin-bot"), testKeyTab(), testKeyRight(), testKeyTab())...)
	s, _ = accessPress(t, s, env, accessType("odabank")...)
	s, _ = accessPress(t, s, env, testKeyEnter(), testKeyRight(), testKeyRight(), testKeyEnter())
	if got := s.(createRobotScreen); got.done || got.creating {
		t.Fatalf("a project robot with Admin was created: %+v", got)
	}
	// An unknown project: the form refuses it itself, and the API's 404 reads the same.
	s = newCreateRobotScreen()
	s, _ = accessPress(t, s, env, append(accessType("ghost-bot"), testKeyTab(), testKeyRight(), testKeyTab())...)
	s, _ = accessPress(t, s, env, accessType("ghost")...)
	s, _ = accessPress(t, s, env, testKeyEnter(), testKeyEnter())
	if got := s.(createRobotScreen); got.err != "Project ghost does not exist. Pick one from the list." {
		t.Fatalf("err = %q", got.err)
	}
	st := s.(createRobotScreen)
	st.projectsRead = projectsNotRead // as if the list were stale: the API answers 404
	s, _ = accessPress(t, st, env, testKeyEnter())
	if got := s.(createRobotScreen); got.err != "Project ghost does not exist. Pick one from the list." || got.done {
		t.Fatalf("API 404: err = %q done = %v", got.err, got.done)
	}
}
