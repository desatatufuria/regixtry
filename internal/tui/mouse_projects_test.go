package tui

import (
	"context"
	"math/rand"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// The project writes the mouse fuzz must see: recordingClient (mouse_fuzz_test.go)
// embeds the admin client, so a method it does not wrap is not recorded.

func (c recordingClient) CreateProject(ctx context.Context, s AdminSession, name string) (ports.ProjectSummary, error) {
	c.rec.record("CreateProject")
	return c.AdminClient.CreateProject(ctx, s, name)
}

func (c recordingClient) DeleteProject(ctx context.Context, s AdminSession, name string) error {
	c.rec.record("DeleteProject")
	return c.AdminClient.DeleteProject(ctx, s, name)
}

func (c recordingClient) PutProjectMember(ctx context.Context, s AdminSession, in ports.AdminPutProjectGrantInput) (ports.AdminProjectMember, error) {
	c.rec.record("PutProjectMember")
	return c.AdminClient.PutProjectMember(ctx, s, in)
}

func (c recordingClient) DeleteProjectMember(ctx context.Context, s AdminSession, project, username string) error {
	c.rec.record("DeleteProjectMember")
	return c.AdminClient.DeleteProjectMember(ctx, s, project, username)
}

// projectFuzzTargets are the project screens the fuzz starts on. The list and
// the members are also in mouseCases (so the shared fuzz covers them); the
// grant form and the Create robot form on its Project scope are only here.
func projectFuzzTargets() []mouseCase {
	return []mouseCase{
		{name: "projects", build: func(t *testing.T, w, h int) Model {
			return projectsModelAt(t, membersClient(), w, h, projectsRepos...)
		}},
		{name: "project members", build: func(t *testing.T, w, h int) Model {
			return membersModelAt(t, membersClient(), w, h, "odabank")
		}},
		{name: "grant project role", build: func(t *testing.T, w, h int) Model {
			return grantRoleModelAt(t, membersClient(), w, h)
		}},
		{name: "create robot on a project", build: func(t *testing.T, w, h int) Model {
			m := projectsAt(t, membersClient(), w, h, projectsRepos...)
			next, cmd := m.mountCreateRobot()
			return projectsPress(t, drain(next.(Model), cmd, 0), testKeyTab(), testKeyRight())
		}},
	}
}

// TestMouseEventsNeverWriteOnTheProjectScreens is the mouse fuzz of the
// project screens: a seeded mix of keys and mouse events at every size. Keys
// may write (they do: the recorder must see it); no mouse event may.
func TestMouseEventsNeverWriteOnTheProjectScreens(t *testing.T) {
	keyWrites := 0
	t.Cleanup(func() {
		if keyWrites == 0 && !t.Failed() {
			t.Error("no key ever caused a write: the recorder is not wired to the project screens")
		}
	})
	for _, c := range projectFuzzTargets() {
		t.Run(c.name, func(t *testing.T) {
			for seed := int64(0); seed < 12; seed++ {
				rng := rand.New(rand.NewSource(seed))
				size := geometrySizes[rng.Intn(len(geometrySizes))]
				m := c.build(t, size.w, size.h)
				rec := &writeRecorder{}
				m.adminClient = recordingClient{AdminClient: m.adminClient, rec: rec}
				for step := range 150 {
					var msg tea.Msg
					mouse := true
					x, y := rng.Intn(size.w+6)-2, rng.Intn(size.h+6)-2
					switch rng.Intn(10) {
					case 0, 1, 2, 3:
						msg = mouseClickAt(x, y)
					case 4, 5:
						msg = mouseWheel([]tea.MouseButton{tea.MouseWheelUp, tea.MouseWheelDown, tea.MouseWheelLeft}[rng.Intn(3)])
					case 6:
						msg = tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}
					case 7:
						msg = tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft}
					case 8:
						msg = tea.MouseClickMsg{X: x, Y: y, Button: []tea.MouseButton{tea.MouseRight, tea.MouseMiddle}[rng.Intn(2)]}
					default:
						msg, mouse = fuzzKeys[rng.Intn(len(fuzzKeys))], false
					}
					writes := rec.count()
					updated, cmd := m.Update(msg)
					m = drain(updated.(Model), cmd, 0)
					if step%15 == 0 {
						_ = m.View()
					}
					if !mouse {
						keyWrites += rec.count() - writes
					}
					if mouse && rec.count() != writes {
						t.Fatalf("seed %d step %d: %T at (%d,%d) wrote %v", seed, step, msg, x, y, rec.calls[writes:])
					}
				}
			}
		})
	}
}

func projectsMouseCase() mouseCase {
	return mouseCase{
		name: "projects",
		build: func(t *testing.T, w, h int) Model {
			return projectsModelAt(t, projectsClient(), w, h)
		},
		rows:      []string{"gosse", "metalab", "odabank", "sandbox"},
		listWidth: func(w int) int { return w - shellSidebarWidth - 2*shellContentPadX },
		selected: func(m Model) string {
			return m.adminScreens[slotProjects].(adminProjectsScreen).selectedName
		},
		opens: func(m Model) bool { return m.screen == screenAdminProjectMembers },
		block: func(t *testing.T, m Model) Model { return testSendKey(t, m, testKeyRunes("n")) },
	}
}

func projectMembersMouseCase() mouseCase {
	return mouseCase{
		name: "project members",
		build: func(t *testing.T, w, h int) Model {
			return membersModelAt(t, membersClient(), w, h, "odabank")
		},
		rows: []string{"ana", "bob", "carol", "ci-publisher", "dave"},
		listWidth: func(w int) int {
			bodyW := w - shellSidebarWidth - 2*shellContentPadX
			return bodyW - min(membersPanelWidth, max(bodyW/3, 1)) - 2
		},
		selected: func(m Model) string {
			return m.adminScreens[slotProjectMembers].(adminProjectMembersScreen).selectedUser
		},
		// Enter on a member does nothing: a second click must not act either.
		block: func(t *testing.T, m Model) Model { return testSendKey(t, m, testKeyRunes("x")) },
	}
}

// A click on the repositories panel focuses it and selects the row; a click on
// a member brings the focus back. Neither ever opens a dialog.
func TestMouseOnProjectMembersFocusesThePanes(t *testing.T) {
	t.Parallel()
	for _, size := range geometrySizes {
		m := membersModelAt(t, membersClient(), size.w, size.h, "odabank")
		leftW := size.w - shellSidebarWidth - 2*shellContentPadX
		panelW := min(membersPanelWidth, max(leftW/3, 1))
		leftW -= panelW + 2
		x, y, ok := cellInPanel(m, "odabank/payments", shellSidebarWidth+shellContentPadX+leftW+2)
		if !ok {
			t.Fatalf("%dx%d: odabank/payments is not drawn in the repositories panel", size.w, size.h)
		}
		m = sendMouse(t, m, mouseClickAt(x, y))
		s := membersScreenOf(t, m)
		if !s.focusRepos || s.repoCursor != 1 {
			t.Fatalf("%dx%d: focusRepos=%v cursor=%d after a click on odabank/payments", size.w, size.h, s.focusRepos, s.repoCursor)
		}
		mx, my, ok := cellInList(m, projectMembersMouseCase(), "carol")
		if !ok {
			t.Fatalf("%dx%d: carol is not drawn", size.w, size.h)
		}
		m = sendMouse(t, m, mouseClickAt(mx, my))
		s = membersScreenOf(t, m)
		if s.focusRepos || s.selectedUser != "carol" || s.confirm.Active() {
			t.Fatalf("%dx%d: focusRepos=%v selected=%q confirm=%v after a click on carol", size.w, size.h, s.focusRepos, s.selectedUser, s.confirm.Active())
		}
	}
}

// cellInPanel finds text at or right of column from, in the rendered view.
func cellInPanel(m Model, text string, from int) (x, y int, ok bool) {
	for row, line := range splitView(m) {
		runes := []rune(line)
		if len(runes) <= from {
			continue
		}
		if i := indexRunes(string(runes[from:]), text); i >= 0 {
			return from + i, row, true
		}
	}
	return 0, 0, false
}

// The mouse never reaches a dialog's buttons: with every dialog of the project
// screens open, clicks and the wheel change nothing and send nothing.
func TestMouseNeverConfirmsAProjectDialog(t *testing.T) {
	t.Parallel()
	cases := map[string]func(t *testing.T) (Model, *fakeAdminClient){
		"create project dialog": func(t *testing.T) (Model, *fakeAdminClient) {
			c := membersClient()
			return projectsPress(t, projectsModelAt(t, c, 150, 40), projectCreateKeys("zeta")...), c
		},
		"delete project confirm": func(t *testing.T) (Model, *fakeAdminClient) {
			c := membersClient()
			return projectsPress(t, projectsModelAt(t, c, 150, 40), testKeyDown(), testKeyDown(), testKeyDown(), testKeyRunes("d")), c
		},
		"delete project refusal": func(t *testing.T) (Model, *fakeAdminClient) {
			c := membersClient()
			return projectsPress(t, projectsModelAt(t, c, 150, 40), testKeyDown(), testKeyDown(), testKeyRunes("d")), c
		},
		"revoke member confirm": func(t *testing.T) (Model, *fakeAdminClient) {
			c := membersClient()
			return projectsPress(t, membersModelAt(t, c, 150, 40, "odabank"), testKeyRunes("x")), c
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m, client := build(t)
			before := projectsRender(m)
			for y := range 40 {
				for x := 0; x < 150; x += 3 {
					m = sendMouse(t, m, mouseClickAt(x, y))
				}
			}
			m = sendMouse(t, m, mouseWheel(tea.MouseWheelDown))
			if projectsRender(m) != before {
				t.Fatal("a mouse event changed the screen behind a dialog")
			}
			if len(client.projectsAPI.createCalls)+len(client.projectsAPI.deleteCalls)+len(client.projectsAPI.deleteMemberCalls)+len(client.projectsAPI.putCalls) != 0 {
				t.Fatalf("the mouse sent a write: create %v delete %v revoke %v put %v", client.projectsAPI.createCalls, client.projectsAPI.deleteCalls, client.projectsAPI.deleteMemberCalls, client.projectsAPI.putCalls)
			}
		})
	}
}

func splitView(m Model) []string { return strings.Split(projectsRender(m), "\n") }

// indexRunes is strings.Index in cells (runes), not bytes.
func indexRunes(haystack, needle string) int {
	i := strings.Index(haystack, needle)
	if i < 0 {
		return -1
	}
	return len([]rune(haystack[:i]))
}
