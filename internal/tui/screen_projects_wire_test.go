package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// projectsSettle runs cmd and feeds every message to the screen until no
// command is left, flattening batches; messages meant for the Model (open
// requests, navigation) are returned instead of delivered.
func projectsSettle(s adminScreen, env screenEnv, cmd tea.Cmd) (adminScreen, []tea.Msg) {
	var outgoing []tea.Msg
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if next == nil {
			continue
		}
		msg := next()
		switch typed := msg.(type) {
		case tea.BatchMsg:
			queue = append(queue, typed...)
			continue
		case openProjectMembersMsg, navigateMsg:
			outgoing = append(outgoing, msg)
			continue
		}
		var follow tea.Cmd
		s, follow, _ = s.Update(env, msg)
		queue = append(queue, follow)
	}
	return s, outgoing
}

func projectsScreenPress(t *testing.T, s adminProjectsScreen, env screenEnv, keys ...keyMsg) (adminProjectsScreen, []tea.Msg) {
	t.Helper()
	var all []tea.Msg
	for _, k := range keys {
		next, cmd, _ := s.Update(env, k)
		settled, out := projectsSettle(next, env, cmd)
		s = settled.(adminProjectsScreen)
		all = append(all, out...)
	}
	return s, all
}

func selectProject(t *testing.T, s adminProjectsScreen, env screenEnv, name string) adminProjectsScreen {
	t.Helper()
	for range len(s.projects) {
		if s.selectedName == name {
			return s
		}
		s, _ = projectsScreenPress(t, s, env, testKeyDown())
	}
	t.Fatalf("project %q is not on screen: %v", name, projectNames(s))
	return s
}

// TestProjectsScreenThroughTheRealStack drives the Projects screen's commands
// through the real HTTP client, router, services and stores: create from the
// dialog, the live list with counts (odabank apart from odabank2), a delete
// refused while the project holds a repository, allowed when empty.
func TestProjectsScreenThroughTheRealStack(t *testing.T) {
	w := newProjectsWire(t, true)
	env := w.env(w.session, 160, 30)

	s := newAdminProjectsScreen()
	next, _ := projectsSettle(s, env, s.Init(env))
	s = next.(adminProjectsScreen)
	if len(s.projects) != 0 || !s.loaded {
		t.Fatalf("a new registry lists %d projects", len(s.projects))
	}
	for _, name := range []string{"odabank", "odabank2", "sandbox"} {
		s, _ = projectsScreenPress(t, s, env, projectCreateKeys(name)...)
		s, _ = projectsScreenPress(t, s, env, testKeyEnter())
		if s.dialog.open || s.selectedName != name {
			t.Fatalf("after creating %s: dialog open=%v selected=%q", name, s.dialog.open, s.selectedName)
		}
	}
	if got := strings.Join(projectNames(s), ","); got != "odabank,odabank2,sandbox" {
		t.Fatalf("projects = %s", got)
	}

	// The same name again: the API's 409 becomes the board's text, nothing else.
	s, _ = projectsScreenPress(t, s, env, projectCreateKeys("odabank")...)
	s, _ = projectsScreenPress(t, s, env, testKeyEnter())
	if !s.dialog.open || s.dialog.err != "Project odabank already exists." {
		t.Fatalf("duplicate create: open=%v err=%q", s.dialog.open, s.dialog.err)
	}
	s, _ = projectsScreenPress(t, s, env, testKeyEsc())

	w.push("odabank/api")
	w.push("odabank2/web")
	w.push("odabank2/worker")
	s, _ = projectsScreenPress(t, s, env, testKeyEsc()) // no dialog open: leaves the screen
	s = newAdminProjectsScreen()
	next, _ = projectsSettle(s, env, s.Init(env))
	s = next.(adminProjectsScreen)
	counts := map[string]int{}
	for _, p := range s.projects {
		counts[p.Name] = p.Repositories
	}
	if counts["odabank"] != 1 || counts["odabank2"] != 2 || counts["sandbox"] != 0 {
		t.Fatalf("repository counts = %v: odabank and odabank2 must be counted apart", counts)
	}

	// Non-empty: refused on the screen, nothing deleted.
	s = selectProject(t, s, env, "odabank")
	s, _ = projectsScreenPress(t, s, env, testKeyRunes("d"))
	if !s.refusal.active() || !strings.Contains(s.refusal.text, `project "odabank" still has 1 repository; delete or finish them first`) {
		t.Fatalf("refusal = %#v", s.refusal)
	}
	s, _ = projectsScreenPress(t, s, env, testKeyEsc())
	if projects, _ := w.client.ListProjects(context.Background(), w.session); len(projects) != 3 {
		t.Fatalf("the refusal deleted something: %d projects", len(projects))
	}

	// Empty: confirm, then gone (and its siblings untouched).
	s = selectProject(t, s, env, "sandbox")
	s, _ = projectsScreenPress(t, s, env, testKeyRunes("d"), testKeyEnter())
	if got := strings.Join(projectNames(s), ","); got != "odabank,odabank2" || !strings.Contains(s.status, "Deleted project sandbox.") {
		t.Fatalf("after delete: projects = %s, status = %q", got, s.status)
	}

	// The rendered list shows the real counts.
	out := ansi.Strip(s.View(newAdminTheme(), env).Body)
	if !strings.Contains(out, "odabank2") {
		t.Fatalf("view lacks odabank2:\n%s", out)
	}
}

func TestProjectsScreenDeletionDisabledThroughTheRealStack(t *testing.T) {
	w := newProjectsWire(t, false)
	env := w.env(w.session, 160, 30)
	s := newAdminProjectsScreen()
	next, _ := projectsSettle(s, env, s.Init(env))
	s = next.(adminProjectsScreen)
	s, _ = projectsScreenPress(t, s, env, projectCreateKeys("sandbox")...)
	s, _ = projectsScreenPress(t, s, env, testKeyEnter())
	s, _ = projectsScreenPress(t, s, env, testKeyRunes("d"), testKeyEnter())
	if s.refusal.title != "Deletion is disabled" || s.confirm.Active() {
		t.Fatalf("refusal = %#v confirm=%v, want the deletion-disabled notice", s.refusal, s.confirm.Active())
	}
	if len(s.projects) != 1 {
		t.Fatalf("the project was removed from the list: %v", projectNames(s))
	}
}

// TestProjectsScreenForNonAdminIsNotAllowed: a user without the global admin
// right gets the API's 403, shown as the not-allowed state.
func TestProjectsScreenForNonAdminIsNotAllowed(t *testing.T) {
	w := newProjectsWire(t, true)
	w.user("carol", false)
	env := w.env(w.loginAs("carol"), 160, 30)
	s := newAdminProjectsScreen()
	next, _ := projectsSettle(s, env, s.Init(env))
	s = next.(adminProjectsScreen)
	if !s.forbidden {
		t.Fatalf("forbidden = false, loadErr = %q", s.loadErr)
	}
}
