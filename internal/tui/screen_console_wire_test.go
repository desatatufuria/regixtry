package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	appregixtry "regixtry/internal/app/regixtry"
)

func consoleModel(t *testing.T, service *consoleService, options ...Option) Model {
	t.Helper()
	m := NewModel(service, append([]Option{WithAdminClient(&fakeAdminClient{})}, options...)...)
	m = runCmd(t, m, m.Init())
	return charResize(t, m, 150, 24)
}

func consoleView(m Model) string { return ansi.Strip(m.View().Content) }

func TestConsoleStartupMountsTheRepositoriesScreenAndAnEmptyCatalogIsItsEmptyState(t *testing.T) {
	t.Parallel()
	m := consoleModel(t, newConsoleService(consoleRepos("team/a")...))
	if _, ok := m.adminScreens[slotConsoleRepositories].(consoleRepositoriesScreen); !ok || m.screen != screenRepositories {
		t.Fatalf("screen = %q, slot = %T", m.screen, m.adminScreens[slotConsoleRepositories])
	}
	if got := m.knownRepositories(); !slices.Equal(got, []string{"team/a"}) {
		t.Fatalf("known repositories = %v", got)
	}
	empty := consoleModel(t, newConsoleService())
	if out := consoleView(empty); empty.screen != screenRepositories || !strings.Contains(out, "No repositories have been published yet.") {
		t.Fatalf("an empty catalog must show the Repositories empty state:\n%s", out)
	}
}

func TestConsoleDrillDownAndBackKeepsEveryExistingFunction(t *testing.T) {
	t.Parallel()
	service := newConsoleService(consoleRepos("ops/c", "team/a", "team/b")...)
	service.tags["team/a"] = tagsFixture()
	service.manifests = nil
	m := consoleModel(t, service)

	// p opens the projects, Enter filters the repositories to one project.
	m = runKey(t, m, "p")
	if m.screen != screenProjects || !strings.Contains(consoleView(m), "Console › Projects") {
		t.Fatalf("p: screen = %q", m.screen)
	}
	m = runKey(t, m, "down")
	m = runKey(t, m, "enter")
	if m.screen != screenRepositories || !strings.Contains(consoleView(m), "Console › Projects › team › Repositories") || strings.Contains(consoleView(m), "ops/c") {
		t.Fatalf("Enter on a project: screen = %q\n%s", m.screen, consoleView(m))
	}
	// Enter on a repository loads its tags on the Tags screen.
	m = runKey(t, m, "enter")
	if m.screen != screenTags || !strings.Contains(consoleView(m), "Console › Repositories › team/a › Tags") || !strings.Contains(consoleView(m), "latest") {
		t.Fatalf("Enter on a repository: screen = %q\n%s", m.screen, consoleView(m))
	}
	// Esc goes up to the filtered repositories, Esc again shows all, then Esc
	// is a no-op at the root.
	m = runKey(t, m, "esc")
	if m.screen != screenRepositories || strings.Contains(consoleView(m), "ops/c") {
		t.Fatalf("Esc from the tags must return to the filtered repositories")
	}
	m = runKey(t, m, "esc")
	if !strings.Contains(consoleView(m), "ops/c") {
		t.Fatalf("Esc must show every repository")
	}
	m = runKey(t, m, "esc")
	if m.screen != screenRepositories {
		t.Fatalf("Esc at the root left the Repositories screen")
	}
	// Projects: Esc goes back to the repositories.
	m = runKey(t, runKey(t, m, "p"), "esc")
	if m.screen != screenRepositories {
		t.Fatalf("Esc from the projects: screen = %q", m.screen)
	}
}

func TestConsoleManifestStillOpensFromATagAndEscReturnsToTheTagsScreen(t *testing.T) {
	t.Parallel()
	service := newConsoleService(consoleRepos("team/a")...)
	service.tags["team/a"] = tagsFixture()
	service.manifests = map[string]appregixtry.ManifestDetails{"team/a:0.9": {Repository: "team/a", Reference: "0.9"}}
	m := consoleModel(t, service)
	m = runKey(t, runKey(t, m, "enter"), "s") // tags, sorted by date
	m = runKey(t, m, "enter")                 // the first row by date is latest
	if m.screen != screenManifest {
		t.Fatalf("Enter on a tag: screen = %q", m.screen)
	}
	m = runKey(t, m, "esc")
	tags, ok := m.adminScreens[slotConsoleTags].(consoleTagsScreen)
	if m.screen != screenTags || !ok || tags.sortMode != sortByDateDesc || len(tags.items) != 3 {
		t.Fatalf("Esc from the manifest: screen = %q, tags screen kept = %v", m.screen, ok)
	}
}

func TestConsoleQuitKeysAndTabAndGrants(t *testing.T) {
	t.Parallel()
	m := consoleModel(t, newConsoleService(consoleRepos("team/a")...))
	for _, k := range []keyMsg{testKeyRunes("q"), testKeyCtrl('c')} {
		if _, cmd := m.Update(k); !didQuit(cmd) {
			t.Fatalf("%v did not quit on the Repositories screen", k)
		}
	}
	// q quits from a dialog too, as it did.
	open := runKey(t, m, "d")
	if _, cmd := open.Update(testKeyRunes("q")); !didQuit(cmd) {
		t.Fatal("q must still quit with a dialog open")
	}
	if got := runKey(t, m, "tab"); got.screen != screenAdminLogin {
		t.Fatalf("Tab: screen = %q, want the admin login", got.screen)
	}
	if got := runKey(t, m, "g"); got.screen != screenAdminLogin || got.adminIntent != adminIntentRepoGrants || got.adminIntentRepository != "team/a" {
		t.Fatalf("g: screen = %q intent = %v %q", got.screen, got.adminIntent, got.adminIntentRepository)
	}
	// A dialog swallows Tab: the admin panel does not open over a delete.
	if got := runKey(t, open, "tab"); got.screen != screenRepositories || !got.adminScreens[slotConsoleRepositories].(consoleRepositoriesScreen).confirm.Active() {
		t.Fatalf("Tab with a dialog open: screen = %q", got.screen)
	}
}

func TestConsoleWithoutAnAdminAPITabSaysSo(t *testing.T) {
	t.Parallel()
	m := NewModel(newConsoleService(consoleRepos("team/a")...))
	m = runCmd(t, m, m.Init())
	m = charResize(t, runKey(t, m, "tab"), 150, 24)
	if m.screen != screenRepositories || !strings.Contains(consoleView(m), consoleUnavailableNotice) {
		t.Fatalf("screen = %q\n%s", m.screen, consoleView(m))
	}
}

func TestConsoleDeleteThroughTheModelRemovesTheRowAndKeepsItGone(t *testing.T) {
	t.Parallel()
	service := newConsoleService(consoleRepos("team/a", "team/b", "team/c")...)
	m := consoleModel(t, service)
	m = runKey(t, m, "down")
	m = runKey(t, m, "d")
	if out := consoleView(m); !strings.Contains(out, "Delete repository team/b") {
		t.Fatalf("no dialog:\n%s", out)
	}
	m = runKey(t, m, "y")
	if !slices.Equal(service.deletedRepos, []string{"team/b"}) {
		t.Fatalf("deleted %v", service.deletedRepos)
	}
	if got := m.knownRepositories(); !slices.Equal(got, []string{"team/a", "team/c"}) {
		t.Fatalf("known repositories = %v", got)
	}
}

func TestConsoleProjectDeleteThroughTheModelPrunesTheRepositoriesScreen(t *testing.T) {
	t.Parallel()
	service := newConsoleService(consoleRepos("ops/c", "team/a", "team/b")...)
	m := consoleModel(t, service)
	m = runKey(t, runKey(t, runKey(t, m, "p"), "down"), "d")
	m = runKey(t, m, "enter")
	if !slices.Equal(service.deletedRepos, []string{"team/a", "team/b"}) {
		t.Fatalf("deleted %v", service.deletedRepos)
	}
	m = runKey(t, m, "esc")
	if m.screen != screenRepositories || strings.Contains(consoleView(m), "team/") || !strings.Contains(consoleView(m), "ops/c") {
		t.Fatalf("the Repositories screen still lists the deleted project:\n%s", consoleView(m))
	}
}

func TestConsoleTagsDeleteThroughTheModelUpdatesTheRepositoryTagCount(t *testing.T) {
	t.Parallel()
	service := newConsoleService(consoleRepos("team/a")...)
	service.tags["team/a"] = tagsFixture()
	m := consoleModel(t, service)
	m = runKey(t, m, "enter")
	m = runKey(t, runKey(t, m, "d"), "y")
	if !slices.Equal(service.deletedTags, []string{"team/a:0.9"}) {
		t.Fatalf("deleted %v", service.deletedTags)
	}
	if got := m.adminScreens[slotConsoleRepositories].(consoleRepositoriesScreen).items[0].TagCount; got != 0 {
		t.Fatalf("TAGS = %d, want 0 (the fixture started with 1 and one tag was deleted)", got)
	}
}

func TestConsoleKeysTypedAsTextNeverActAsCommandsWhenPasted(t *testing.T) {
	t.Parallel()
	service := newConsoleService(consoleRepos("team/a")...)
	m := consoleModel(t, service)
	updated, _ := m.Update(tea.PasteMsg{Content: "d"})
	if updated.(Model).adminScreens[slotConsoleRepositories].(consoleRepositoriesScreen).confirm.Active() {
		t.Fatal("a pasted d opened the delete dialog")
	}
}

func TestConsoleStaleResultsAreDroppedByTheModel(t *testing.T) {
	t.Parallel()
	service := newConsoleService(consoleRepos("team/a")...)
	service.tags["team/a"] = tagsFixture()
	m := consoleModel(t, service)
	m = runKey(t, m, "enter")
	old := m.adminScreens[slotConsoleTags].(consoleTagsScreen).gen
	m = runKey(t, runKey(t, m, "esc"), "enter") // a fresh Tags screen
	if m.adminScreens[slotConsoleTags].(consoleTagsScreen).gen == old {
		t.Fatal("a new visit must mount a screen with a new generation")
	}
	updated, _ := m.Update(consoleTagsMsg{gen: old, result: nil})
	if got := len(updated.(Model).adminScreens[slotConsoleTags].(consoleTagsScreen).items); got != 3 {
		t.Fatalf("a result of the old visit changed the tags: %d", got)
	}
}

func TestConsoleLeavingTheStartupLoginLandsOnAMountedRepositoriesScreen(t *testing.T) {
	t.Parallel()
	m := NewModel(newConsoleService(consoleRepos("team/a")...), WithAdminClient(&fakeAdminClient{}), WithStartupLogin())
	m = runCmd(t, m, m.Init())
	m = charResize(t, runKey(t, m, "esc"), 150, 24)
	if m.screen != screenRepositories || m.adminScreens[slotConsoleRepositories] == nil {
		t.Fatalf("screen = %q, slot = %v", m.screen, m.adminScreens[slotConsoleRepositories])
	}
	if !strings.Contains(consoleView(m), "No repositories have been published yet.") {
		t.Fatalf("view:\n%s", consoleView(m))
	}
}

// A delete in flight blocks every other delete, even when the screens are
// left and mounted again before its result arrives.
func TestConsoleASecondDeleteCannotStartWhileOneIsInFlightAcrossScreens(t *testing.T) {
	t.Parallel()
	service := newConsoleService(consoleRepos("p3/a", "p3/b")...)
	service.tags["p3/a"] = tagsFixture()
	m := consoleModel(t, service)
	// Confirm the repository delete but do not deliver its result.
	updated, _ := m.Update(testKeyRunes("d"))
	updated, cmd := updated.(Model).Update(testKeyRunes("y"))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("y sent nothing")
	}
	// Esc closes the dialog, Esc again, then p mounts a fresh Projects screen.
	m = runKey(t, runKey(t, m, "esc"), "esc")
	m = runKey(t, m, "p")
	m = runKey(t, m, "d")
	if m.adminScreens[slotConsoleProjects].(consoleProjectsScreen).confirm.Active() {
		t.Fatal("d opened a delete dialog on a fresh Projects screen while a delete was in flight")
	}
	// The same through Tags: Esc to the repositories, Enter on a repository.
	m = runKey(t, runKey(t, m, "esc"), "enter")
	m = runKey(t, m, "d")
	if m.adminScreens[slotConsoleTags].(consoleTagsScreen).confirm.Active() {
		t.Fatal("d opened a delete dialog on a fresh Tags screen while a delete was in flight")
	}
	// Once the result lands, deleting works again.
	updated, _ = m.Update(cmd())
	m = runKey(t, updated.(Model), "d")
	if !m.adminScreens[slotConsoleTags].(consoleTagsScreen).confirm.Active() {
		t.Fatal("d does nothing after the delete finished")
	}
}

// The result of a delete asked from a visit that was left still removes the
// row from the visit that replaced it.
func TestConsoleALateTagDeleteResultRemovesTheTagFromTheCurrentScreen(t *testing.T) {
	t.Parallel()
	service := newConsoleService(consoleRepos("team/a")...)
	service.tags["team/a"] = tagsFixture()
	m := consoleModel(t, service)
	m = runKey(t, m, "enter")
	updated, _ := m.Update(testKeyRunes("d"))
	updated, cmd := updated.(Model).Update(testKeyRunes("y"))
	m = updated.(Model)
	late := cmd() // the delete ran; its result is still on its way
	m = runKey(t, runKey(t, m, "esc"), "enter")
	if got := len(m.adminScreens[slotConsoleTags].(consoleTagsScreen).items); got != 3 {
		t.Fatalf("test setup: the new Tags screen lists %d tags, want 3 (the read ran before the delete)", got)
	}
	updated, _ = m.Update(late)
	m = updated.(Model)
	for _, tag := range m.adminScreens[slotConsoleTags].(consoleTagsScreen).items {
		if tag.Name == "0.9" {
			t.Fatal("the deleted tag is still listed on the current Tags screen")
		}
	}
	if m.console.isBusy() {
		t.Fatal("the delete is still in flight after its result")
	}
}
