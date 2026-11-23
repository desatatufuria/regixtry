package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

func membersMany(n int) []ports.AdminProjectMember {
	roles := []domainauth.RepoRole{domainauth.RepoRoleReader, domainauth.RepoRoleWriter, domainauth.RepoRoleAdmin}
	members := make([]ports.AdminProjectMember, 0, n)
	for i := range n {
		role := roles[i%3]
		robot := i%4 == 3
		if robot && role == domainauth.RepoRoleAdmin {
			role = domainauth.RepoRoleWriter
		}
		members = append(members, projectMember(fmt.Sprintf("member-%03d-%s", i, strings.Repeat("long-", 10)), robot, role, time.Month(1+i%11), 1+i%27))
	}
	return members
}

func projectRepos(project string, n int) []string {
	repos := make([]string, 0, n)
	for i := range n {
		repos = append(repos, fmt.Sprintf("%s/service-%03d-%s", project, i, strings.Repeat("name-", 10)))
	}
	return repos
}

func TestProjectMembersGeometryAtEverySize(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 1, 100} {
		for _, size := range geometrySizes {
			t.Run(fmt.Sprintf("%d members at %dx%d", count, size.w, size.h), func(t *testing.T) {
				t.Parallel()
				client := membersClient()
				client.projectsAPI.members = map[string][]ports.AdminProjectMember{"odabank": membersMany(count)}
				m := projectsAt(t, client, size.w, size.h, projectRepos("odabank", max(count, 3))...)
				updated, cmd := m.Update(openProjectMembersMsg{project: "odabank"})
				m = repoAccessRun(t, updated.(Model), cmd)
				label := fmt.Sprintf("members, %d at %dx%d", count, size.w, size.h)
				repoAccessNoOverflow(t, label, m.View().Content, size.w, size.h)
				if count > 0 {
					m = projectsPress(t, m, testKeyRunes("x"))
					repoAccessNoOverflow(t, label+", revoke dialog", m.View().Content, size.w, size.h)
					m = projectsPress(t, m, testKeyEsc())
					for range 150 {
						m = projectsPress(t, m, testKeyDown())
					}
					out := projectsRender(m)
					repoAccessNoOverflow(t, label+", scrolled", m.View().Content, size.w, size.h)
					if selected := membersScreenOf(t, m).selectedUser; !strings.Contains(out, "▸ "+selected[:10]) {
						t.Fatalf("%s: the selected member %q is not on screen:\n%s", label, selected, out)
					}
				}
				m = projectsPress(t, m, testKeyTab())
				for range 150 {
					m = projectsPress(t, m, testKeyDown())
				}
				repoAccessNoOverflow(t, label+", repositories focused", m.View().Content, size.w, size.h)
				if repos := membersScreenOf(t, m).repositories(m.screenEnv()); len(repos) > 0 {
					selected := repos[boundedIndex(membersScreenOf(t, m).repoCursor, len(repos))]
					if !strings.Contains(projectsRender(m), "▸ "+selected[:10]) {
						t.Fatalf("%s: the selected repository %q is not on screen", label, selected)
					}
				}
			})
		}
	}
}

func TestGrantProjectRoleGeometryAtEverySize(t *testing.T) {
	t.Parallel()
	for _, size := range geometrySizes {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			t.Parallel()
			client := membersClient()
			for i := range 100 {
				client.users = append(client.users, ports.AdminUser{ID: fmt.Sprintf("u-%d", i), Username: fmt.Sprintf("someone-%03d-%s", i, strings.Repeat("long-", 8)), Enabled: true})
			}
			m := grantRoleModelAt(t, client, size.w, size.h)
			label := fmt.Sprintf("grant at %dx%d", size.w, size.h)
			repoAccessNoOverflow(t, label, m.View().Content, size.w, size.h)
			steps := [][]keyMsg{
				{testKeyTab()},
				typeText("someone"),
				{testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown()},
				{testKeyEnter(), testKeyRight(), testKeyRight()},
				{testKeyShiftTab(), testKeyShiftTab(), testKeyRight()}, // robot + admin: refusal
			}
			for i, keys := range steps {
				m = projectsPress(t, m, keys...)
				repoAccessNoOverflow(t, fmt.Sprintf("%s step %d", label, i), m.View().Content, size.w, size.h)
				if i == 2 {
					s := grantRoleScreenOf(t, m)
					if rows := s.rows(); len(rows) > 0 && !strings.Contains(projectsRender(m), "▸ "+rows[boundedIndex(s.suggestion, len(rows))].Name[:10]) {
						t.Fatalf("%s: the highlighted suggestion is not on screen", label)
					}
				}
			}
			requireContains(t, projectsRender(m), "Project admin is not available for robots")
			s := grantRoleScreenOf(t, m)
			s.err = "Could not grant role: " + strings.Repeat("a very long reason ", 20) + "."
			m.adminScreens[slotGrantProjectRole] = s
			repoAccessNoOverflow(t, label+", long error", m.View().Content, size.w, size.h)
		})
	}
}

func TestProjectMembersAndGrantViewGoldens(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T, w, h int) Model
	}{
		{"members", func(t *testing.T, w, h int) Model { return membersModelAt(t, membersClient(), w, h, "odabank") }},
		{"members_carol_revoke", func(t *testing.T, w, h int) Model {
			return projectsPress(t, membersModelAt(t, membersClient(), w, h, "odabank"), testKeyDown(), testKeyDown(), testKeyRunes("x"))
		}},
		{"members_repositories_focused", func(t *testing.T, w, h int) Model {
			return projectsPress(t, membersModelAt(t, membersClient(), w, h, "odabank"), testKeyTab(), testKeyDown())
		}},
		{"members_empty", func(t *testing.T, w, h int) Model {
			c := membersClient()
			c.projectsAPI.members = nil
			return membersModelAt(t, c, w, h, "odabank")
		}},
		{"members_error", func(t *testing.T, w, h int) Model {
			c := membersClient()
			c.projectsAPI.membersErr = map[string]error{"odabank": fmt.Errorf("connection refused")}
			return membersModelAt(t, c, w, h, "odabank")
		}},
		{"members_unknown_project", func(t *testing.T, w, h int) Model {
			c := membersClient()
			c.projectsAPI.membersErr = map[string]error{"ghost": adminNotFoundError{message: "x", detail: "project not found"}}
			return membersModelAt(t, c, w, h, "ghost")
		}},
		{"grant_empty", func(t *testing.T, w, h int) Model { return grantRoleModelAt(t, membersClient(), w, h) }},
		{"grant_user_dave_writer", func(t *testing.T, w, h int) Model {
			m := projectsPress(t, grantRoleModelAt(t, membersClient(), w, h), testKeyTab())
			m = projectsPress(t, m, typeText("da")...)
			return projectsPress(t, m, testKeyEnter(), testKeyRight())
		}},
		{"grant_robot_admin_refused", func(t *testing.T, w, h int) Model {
			m := projectsPress(t, grantRoleModelAt(t, membersClient(), w, h), testKeyRight(), testKeyTab())
			m = projectsPress(t, m, typeText("release")...)
			return projectsPress(t, m, testKeyEnter(), testKeyRight(), testKeyRight())
		}},
		{"grant_error", func(t *testing.T, w, h int) Model {
			c := membersClient()
			c.projectsAPI.putErr = adminStatusError{status: 403, message: "x", detail: "project administrators cannot grant project admin"}
			m := projectsPress(t, grantRoleModelAt(t, c, w, h), testKeyTab())
			m = projectsPress(t, m, typeText("dave")...)
			return projectsPress(t, m, testKeyEnter(), testKeyRight(), testKeyRight(), testKeyEnter())
		}},
	}
	for _, tc := range cases {
		for _, size := range []struct{ w, h int }{{150, 24}, {220, 60}} {
			t.Run(fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h), func(t *testing.T) {
				m := tc.build(t, size.w, size.h)
				repoAccessNoOverflow(t, tc.name, m.View().Content, size.w, size.h)
				assertProjectsGolden(t, fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h), projectsRender(m))
			})
		}
	}
}
