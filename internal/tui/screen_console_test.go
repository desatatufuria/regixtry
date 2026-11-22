package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	appregixtry "regixtry/internal/app/regixtry"
)

var consoleTime = time.Date(2026, time.November, 21, 8, 41, 0, 0, time.UTC)

// consoleService is a registry whose deletes really remove: a reload shows
// what a real one would, and every delete is recorded in order.
type consoleService struct {
	*fakeQueryService
	repositories []appregixtry.RepositorySummary
	tags         map[string][]appregixtry.TagDetails
	failRepo     map[string]error
	failTag      error
	deletedRepos []string
	deletedTags  []string
}

func newConsoleService(repositories ...appregixtry.RepositorySummary) *consoleService {
	return &consoleService{fakeQueryService: &fakeQueryService{}, repositories: repositories, tags: map[string][]appregixtry.TagDetails{}}
}

func (c *consoleService) RepositorySummaries(context.Context, int, string) ([]appregixtry.RepositorySummary, error) {
	return slices.Clone(c.repositories), nil
}

func (c *consoleService) TagDetails(_ context.Context, repository string, _ int, _ string) ([]appregixtry.TagDetails, error) {
	return slices.Clone(c.tags[repository]), nil
}

func (c *consoleService) DeleteRepository(_ context.Context, repository string) (appregixtry.RepositoryDeletionDetails, error) {
	if err := c.failRepo[repository]; err != nil {
		return appregixtry.RepositoryDeletionDetails{}, err
	}
	c.deletedRepos = append(c.deletedRepos, repository)
	c.repositories = slices.DeleteFunc(c.repositories, func(r appregixtry.RepositorySummary) bool { return r.Name == repository })
	return appregixtry.RepositoryDeletionDetails{Repository: repository}, nil
}

func (c *consoleService) DeleteManifest(_ context.Context, repository, tag string) (appregixtry.DeletionDetails, error) {
	if c.failTag != nil {
		return appregixtry.DeletionDetails{}, c.failTag
	}
	c.deletedTags = append(c.deletedTags, repository+":"+tag)
	c.tags[repository] = slices.DeleteFunc(c.tags[repository], func(t appregixtry.TagDetails) bool { return t.Name == tag })
	return appregixtry.DeletionDetails{Repository: repository, Reference: tag}, nil
}

func consoleRepos(names ...string) []appregixtry.RepositorySummary {
	out := make([]appregixtry.RepositorySummary, len(names))
	for i, name := range names {
		out[i] = appregixtry.RepositorySummary{Name: name, TagCount: i + 1, LastPushed: consoleTime.Add(-time.Duration(i) * 24 * time.Hour)}
	}
	return out
}

func consoleEnvFor(service QueryService, width, height int) screenEnv {
	return screenEnv{Client: &fakeAdminClient{}, Service: service, Layout: consoleLayout{Width: width, Height: height}, Now: func() time.Time { return consoleTime }}
}

// consoleStep sends one message and returns the new screen and its command.
func consoleStep(s adminScreen, env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd) {
	next, cmd, _ := s.Update(env, msg)
	return next, cmd
}

// consoleSettle runs a command and feeds its message back, as the parent does,
// until nothing is left; messages for the parent (open*, navigate) end it.
func consoleSettle(s adminScreen, env screenEnv, cmd tea.Cmd) (adminScreen, []tea.Msg) {
	var seen []tea.Msg
	for cmd != nil {
		msg := cmd()
		seen = append(seen, msg)
		switch msg.(type) {
		case openProjectsMsg, openRepositoriesMsg, openTagsMsg, openManifestMsg, navigateMsg:
			return s, seen
		}
		s, cmd = consoleStep(s, env, msg)
	}
	return s, seen
}

func consoleKeys(s adminScreen, env screenEnv, keys ...keyMsg) (adminScreen, tea.Cmd) {
	var cmd tea.Cmd
	for _, k := range keys {
		s, cmd = consoleStep(s, env, k)
	}
	return s, cmd
}

func consoleRender(s adminScreen, env screenEnv) string {
	frame := s.View(newAdminTheme(), env)
	out := frame.Body
	if frame.Overlay != "" {
		out = compositeOverlay(out, frame.Overlay, env.Layout.Width, env.Layout.Height)
	}
	return ansi.Strip(out)
}

func reposScreen(t *testing.T, items []appregixtry.RepositorySummary) consoleRepositoriesScreen {
	t.Helper()
	return newConsoleRepositoriesScreen(items)
}

// ---- Repositories ----

func TestConsoleRepositoriesKeysMoveOpenAndAskForOtherScreens(t *testing.T) {
	t.Parallel()
	env := consoleEnvFor(newConsoleService(), 150, 24)
	var s adminScreen = reposScreen(t, consoleRepos("a/one", "a/two", "b/three"))

	for _, tc := range []struct {
		keys []keyMsg
		want int
	}{
		{[]keyMsg{testKeyDown()}, 1},
		{[]keyMsg{testKeyRunes("j"), testKeyRunes("j"), testKeyRunes("j")}, 2},
		{[]keyMsg{testKeyDown(), testKeyUp(), testKeyRunes("k")}, 0},
		{[]keyMsg{testKeyEnd()}, 2},
		{[]keyMsg{testKeyEnd(), testKeyHome()}, 0},
		{[]keyMsg{testKeyPgDown()}, 2},
	} {
		got, _ := consoleKeys(s, env, tc.keys...)
		if sel := got.(consoleRepositoriesScreen).selected; sel != tc.want {
			t.Fatalf("keys %v: selected = %d, want %d", tc.keys, sel, tc.want)
		}
	}

	moved, _ := consoleKeys(s, env, testKeyDown())
	_, cmd := consoleStep(moved, env, testKeyEnter())
	if got, ok := cmd().(openTagsMsg); !ok || got.repository != "a/two" {
		t.Fatalf("Enter asked for %#v, want the tags of a/two", cmd())
	}
	_, cmd = consoleStep(s, env, testKeyRunes("p"))
	if got, ok := cmd().(openProjectsMsg); !ok || len(got.repositories) != 3 {
		t.Fatalf("p asked for %#v, want the projects of 3 repositories", cmd())
	}
}

func TestConsoleRepositoriesEscClearsTheProjectFilterAndOtherwiseDoesNothing(t *testing.T) {
	t.Parallel()
	env := consoleEnvFor(newConsoleService(), 150, 24)
	s := reposScreen(t, consoleRepos("a/one", "a/two", "b/three")).narrowedTo("a")
	if got := len(s.visible()); got != 2 {
		t.Fatalf("narrowed to a shows %d rows, want 2", got)
	}
	next, cmd := consoleStep(s, env, testKeyEsc())
	if cmd != nil || next.(consoleRepositoriesScreen).filtered || len(next.(consoleRepositoriesScreen).visible()) != 3 {
		t.Fatalf("Esc must show every repository, cmd=%v", cmd)
	}
	again, cmd := consoleStep(next, env, testKeyEsc())
	if cmd != nil || again.(consoleRepositoriesScreen).filtered {
		t.Fatalf("Esc at the root must do nothing")
	}
}

func TestConsoleRepositoriesDeleteDialogNamesTargetAndWhatTheServiceDoes(t *testing.T) {
	t.Parallel()
	env := consoleEnvFor(newConsoleService(), 150, 24)
	s, _ := consoleKeys(reposScreen(t, consoleRepos("a/one", "a/two")), env, testKeyDown(), testKeyRunes("d"))
	confirm := s.(consoleRepositoriesScreen).confirm
	if !confirm.Active() || confirm.target != "a/two" || confirm.buttonLabel != "Delete" || !confirm.destructive() {
		t.Fatalf("dialog = %+v, want an active destructive Delete of a/two", confirm)
	}
	for _, want := range []string{"2 tags", "garbage collection", "cannot be undone"} {
		if !strings.Contains(confirm.message+" "+confirm.consequence+" "+confirm.targetDetail, want) {
			t.Fatalf("dialog lacks %q: %+v", want, confirm)
		}
	}
	out := consoleRender(s, env)
	for _, want := range []string{"Delete repository a/two", "Cancel", "y / enter"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view lacks %q:\n%s", want, out)
		}
	}
}

func TestConsoleRepositoriesDeleteRemovesExactlyTheNamedRepositoryWhateverHappensAfterD(t *testing.T) {
	t.Parallel()
	service := newConsoleService(consoleRepos("r/a", "r/b", "r/c")...)
	env := consoleEnvFor(service, 150, 24)
	s := adminScreen(newConsoleRepositoriesScreen(service.repositories))
	s, _ = consoleKeys(s, env, testKeyRunes("d")) // r/a
	// The cursor moves and a reload in another order lands before the confirm.
	s, _ = consoleKeys(s, env, testKeyDown(), testKeyDown())
	_ = s // the dialog swallows the moves
	repos := s.(consoleRepositoriesScreen)
	reordered := []appregixtry.RepositorySummary{service.repositories[2], service.repositories[0], service.repositories[1]}
	s, _ = consoleStep(s, env, consoleCatalogMsg{gen: repos.gen, stamp: consoleStamp{seq: 1}, result: reordered})
	_, cmd := consoleStep(s, env, testKeyEnter())
	cmd()
	if !slices.Equal(service.deletedRepos, []string{"r/a"}) {
		t.Fatalf("deleted %v, want exactly [r/a]", service.deletedRepos)
	}
}

func TestConsoleRepositoriesOneDeletePerConfirmation(t *testing.T) {
	t.Parallel()
	service := newConsoleService(consoleRepos("r/a", "r/b")...)
	env := consoleEnvFor(service, 150, 24)
	var s adminScreen = newConsoleRepositoriesScreen(service.repositories)
	s, _ = consoleKeys(s, env, testKeyRunes("d"))
	s, first := consoleStep(s, env, testKeyEnter())
	if first == nil {
		t.Fatal("Enter sent nothing")
	}
	for _, k := range []keyMsg{testKeyEnter(), testKeyRunes("y"), testKeyEnter()} {
		var cmd tea.Cmd
		s, cmd = consoleStep(s, env, k)
		if cmd != nil {
			t.Fatalf("%v while the delete is in flight sent a second command", k)
		}
	}
	// Esc closes the dialog but the delete is still on its way: no new one starts.
	s, _ = consoleStep(s, env, testKeyEsc())
	s, _ = consoleKeys(s, env, testKeyRunes("d"))
	if s.(consoleRepositoriesScreen).confirm.Active() {
		t.Fatal("d opened a dialog while a delete was in flight")
	}
	first()
	if len(service.deletedRepos) != 1 {
		t.Fatalf("deletes = %v, want one", service.deletedRepos)
	}
}

func TestConsoleRepositoriesDeleteErrorKeepsTheScreenAndShowsTheMessage(t *testing.T) {
	t.Parallel()
	service := newConsoleService(consoleRepos("r/a", "r/b")...)
	service.failRepo = map[string]error{"r/a": errors.New("repository deletion is not enabled")}
	env := consoleEnvFor(service, 150, 24)
	var s adminScreen = newConsoleRepositoriesScreen(service.repositories)
	s, cmd := consoleKeys(s, env, testKeyRunes("d"), testKeyEnter())
	s, _ = consoleSettle(s, env, cmd)
	repos := s.(consoleRepositoriesScreen)
	if repos.confirm.Active() || repos.shared.isBusy() || len(repos.items) != 2 || !repos.noticeFail {
		t.Fatalf("after a failed delete: confirm=%v busy=%v items=%d fail=%v", repos.confirm.Active(), repos.shared.isBusy(), len(repos.items), repos.noticeFail)
	}
	if out := consoleRender(s, env); !strings.Contains(out, "repository deletion is not enabled") || !strings.Contains(out, "r/a") {
		t.Fatalf("the error is not visible next to the list:\n%s", out)
	}
	// A new delete is possible after the failure.
	s, _ = consoleKeys(s, env, testKeyRunes("d"))
	if !s.(consoleRepositoriesScreen).confirm.Active() {
		t.Fatal("d does nothing after a failed delete")
	}
}

func TestConsoleRepositoriesAReloadStartedBeforeTheDeleteNeverBringsTheRowBack(t *testing.T) {
	t.Parallel()
	service := newConsoleService(consoleRepos("r/a", "r/b", "r/c")...)
	env := consoleEnvFor(service, 150, 24)
	var s adminScreen = newConsoleRepositoriesScreen(service.repositories)
	before := slices.Clone(service.repositories) // a read taken before the delete
	s, cmd := consoleKeys(s, env, testKeyDown(), testKeyRunes("d"), testKeyEnter())
	deleted := cmd()
	s, reload := consoleStep(s, env, deleted)
	if len(s.(consoleRepositoriesScreen).items) != 2 {
		t.Fatalf("the deleted row is still listed")
	}
	// The old read lands after the delete, then the new reload.
	s, _ = consoleStep(s, env, consoleCatalogMsg{gen: s.(consoleRepositoriesScreen).gen, stamp: consoleStamp{seq: 0}, result: before})
	for _, r := range s.(consoleRepositoriesScreen).items {
		if r.Name == "r/b" {
			t.Fatal("a list read before the delete resurrected r/b")
		}
	}
	s, _ = consoleSettle(s, env, reload)
	if names := s.(consoleRepositoriesScreen).names(); !slices.Equal(names, []string{"r/a", "r/c"}) {
		t.Fatalf("names = %v, want [r/a r/c]", names)
	}
	// Once the registry really has a repository of that name again, a read
	// started after the delete shows it.
	service.repositories = append(service.repositories, appregixtry.RepositorySummary{Name: "r/b"})
	s, again := consoleStep(s, env, consoleCatalogMsg{gen: s.(consoleRepositoriesScreen).gen, stamp: consoleStamp{seq: 5, deletes: 1}, result: service.repositories})
	_ = again
	if !slices.Contains(s.(consoleRepositoriesScreen).names(), "r/b") {
		t.Fatal("a read started after the delete must be trusted")
	}
}

func TestConsoleRepositoriesDropAResultOfAnotherInstance(t *testing.T) {
	t.Parallel()
	env := consoleEnvFor(newConsoleService(), 150, 24)
	var s adminScreen = reposScreen(t, consoleRepos("r/a", "r/b"))
	gen := s.(consoleRepositoriesScreen).gen
	s, _ = consoleStep(s, env, consoleCatalogMsg{gen: gen + 99, result: consoleRepos("x/y")})
	s, cmd := consoleStep(s, env, repositoryDeletedMsg{gen: gen + 99, repository: "r/a", err: errors.New("denied")})
	if cmd != nil || len(s.(consoleRepositoriesScreen).items) != 2 {
		t.Fatal("a foreign result changed the screen")
	}
}

func TestConsoleRepositoriesTabAndGNeedAnAdminAPI(t *testing.T) {
	t.Parallel()
	env := consoleEnvFor(newConsoleService(), 150, 24)
	env.Client = nil
	for _, k := range []keyMsg{testKeyTab(), testKeyRunes("g")} {
		s, _ := consoleStep(reposScreen(t, consoleRepos("r/a")), env, k)
		if got := s.(consoleRepositoriesScreen); got.notice != consoleUnavailableNotice || !got.noticeFail {
			t.Fatalf("%v without an admin API: notice = %q", k, got.notice)
		}
	}
	s, _ := consoleStep(reposScreen(t, nil), consoleEnvFor(newConsoleService(), 150, 24), testKeyRunes("g"))
	if !strings.Contains(s.(consoleRepositoriesScreen).notice, "Select a repository") {
		t.Fatalf("g with nothing selected: notice = %q", s.(consoleRepositoriesScreen).notice)
	}
}

func TestConsoleRepositoriesApplyDeletesOfOtherScreens(t *testing.T) {
	t.Parallel()
	env := consoleEnvFor(newConsoleService(), 150, 24)
	var s adminScreen = reposScreen(t, consoleRepos("a/x", "a/y", "b/z"))
	s, _ = consoleStep(s, env, tagDeletedMsg{gen: 42, repository: "b/z", tag: "latest"})
	if got := s.(consoleRepositoriesScreen).items[2].TagCount; got != 2 {
		t.Fatalf("TAGS of b/z = %d, want 2 after one tag was deleted", got)
	}
	s, _ = consoleStep(s, env, projectDeletedMsg{gen: 42, project: "a", results: []projectDeletionResult{{repository: "a/x"}, {repository: "a/y", err: errors.New("denied")}}})
	if names := s.(consoleRepositoriesScreen).names(); !slices.Equal(names, []string{"a/y", "b/z"}) {
		t.Fatalf("names = %v, want only the failed repository to remain with b/z", names)
	}
}

// ---- Projects ----

func TestConsoleProjectsSortKeyCyclesAndShowsTheIndicator(t *testing.T) {
	t.Parallel()
	env := consoleEnvFor(newConsoleService(), 150, 24)
	repos := []appregixtry.RepositorySummary{
		{Name: "alpha/a", TagCount: 1, LastPushed: consoleTime.Add(-72 * time.Hour)},
		{Name: "beta/b", TagCount: 2, LastPushed: consoleTime},
		{Name: "flat", TagCount: 3, LastPushed: consoleTime.Add(-24 * time.Hour)},
	}
	var s adminScreen = newConsoleProjectsScreen(repos)
	names := func() []string {
		var out []string
		for _, p := range s.(consoleProjectsScreen).projects {
			out = append(out, p.Name)
		}
		return out
	}
	if want := []string{"(ungrouped)", "alpha", "beta"}; !slices.Equal(names(), want) {
		t.Fatalf("by name = %v, want %v", names(), want)
	}
	if out := consoleRender(s, env); !strings.Contains(out, "sort: name") {
		t.Fatalf("no sort indicator:\n%s", out)
	}
	s, _ = consoleStep(s, env, testKeyRunes("s"))
	if want := []string{"beta", "(ungrouped)", "alpha"}; !slices.Equal(names(), want) {
		t.Fatalf("by date = %v, want %v", names(), want)
	}
	if out := consoleRender(s, env); !strings.Contains(out, "sort: date") {
		t.Fatalf("indicator did not change:\n%s", out)
	}
	s, _ = consoleStep(s, env, testKeyRunes("s"))
	if want := []string{"(ungrouped)", "alpha", "beta"}; !slices.Equal(names(), want) {
		t.Fatalf("back by name = %v, want %v", names(), want)
	}
}

func TestConsoleProjectsEnterAsksForItsRepositoriesAndEscGoesBack(t *testing.T) {
	t.Parallel()
	env := consoleEnvFor(newConsoleService(), 150, 24)
	var s adminScreen = newConsoleProjectsScreen(consoleRepos("a/x", "b/y"))
	s, _ = consoleStep(s, env, testKeyDown())
	_, cmd := consoleStep(s, env, testKeyEnter())
	if got, ok := cmd().(openRepositoriesMsg); !ok || got.project != "b" {
		t.Fatalf("Enter asked for %#v, want project b", cmd())
	}
	_, cmd = consoleStep(s, env, testKeyEsc())
	if got, ok := cmd().(navigateMsg); !ok || got.To != screenRepositories {
		t.Fatalf("Esc asked for %#v, want the repositories", cmd())
	}
}

func TestConsoleProjectsDeleteTargetsTheRepositoriesCountedInTheDialog(t *testing.T) {
	t.Parallel()
	service := newConsoleService(consoleRepos("team/a", "team/b", "ops/c")...)
	env := consoleEnvFor(service, 150, 24)
	var s adminScreen = newConsoleProjectsScreen(service.repositories)
	s, _ = consoleStep(s, env, testKeyDown()) // team (ops, team)
	s, _ = consoleKeys(s, env, testKeyRunes("d"))
	confirm := s.(consoleProjectsScreen).confirm
	if confirm.target != "team" || !strings.Contains(confirm.message, "2 repositories") || !strings.Contains(confirm.consequence, "garbage collection") {
		t.Fatalf("dialog = %+v", confirm)
	}
	// A repository is pushed into the project before the confirm: the dialog
	// counted two, so only those two go.
	service.repositories = append(service.repositories, appregixtry.RepositorySummary{Name: "team/late"})
	s, _ = consoleStep(s, env, consoleCatalogMsg{gen: s.(consoleProjectsScreen).gen, stamp: consoleStamp{seq: 1}, result: service.repositories})
	s, cmd := consoleStep(s, env, testKeyEnter())
	s, _ = consoleSettle(s, env, cmd)
	if !slices.Equal(service.deletedRepos, []string{"team/a", "team/b"}) {
		t.Fatalf("deleted %v, want [team/a team/b]", service.deletedRepos)
	}
}

func TestConsoleProjectsPartialFailureIsReportedAndKeepsTheFailedRepository(t *testing.T) {
	t.Parallel()
	service := newConsoleService(consoleRepos("team/a", "team/b")...)
	service.failRepo = map[string]error{"team/b": errors.New("forbidden")}
	env := consoleEnvFor(service, 150, 24)
	var s adminScreen = newConsoleProjectsScreen(service.repositories)
	s, cmd := consoleKeys(s, env, testKeyRunes("d"), testKeyEnter())
	s, _ = consoleSettle(s, env, cmd)
	projects := s.(consoleProjectsScreen)
	if !projects.noticeFail || !strings.Contains(projects.notice, "partially deleted: 1 succeeded, 1 failed") || !strings.Contains(projects.notice, "team/b (forbidden)") {
		t.Fatalf("notice = %q (fail=%v)", projects.notice, projects.noticeFail)
	}
	if len(projects.projects) != 1 || projects.projects[0].RepositoryCount != 1 {
		t.Fatalf("projects = %+v, want team with the one repository that failed", projects.projects)
	}
	if projects.confirm.Active() || projects.shared.isBusy() {
		t.Fatal("the dialog or the busy flag is still set")
	}
}

func TestConsoleProjectsDeleteIsOnePerConfirmationAndDeletesStayDeleted(t *testing.T) {
	t.Parallel()
	service := newConsoleService(consoleRepos("team/a", "ops/b")...)
	env := consoleEnvFor(service, 150, 24)
	var s adminScreen = newConsoleProjectsScreen(service.repositories)
	before := slices.Clone(service.repositories)
	s, first := consoleKeys(s, env, testKeyRunes("d"), testKeyEnter())
	var second tea.Cmd
	s, second = consoleStep(s, env, testKeyEnter())
	if second != nil {
		t.Fatal("a second Enter sent a second delete")
	}
	s, reload := consoleStep(s, env, first())
	s, _ = consoleStep(s, env, consoleCatalogMsg{gen: s.(consoleProjectsScreen).gen, stamp: consoleStamp{seq: 0}, result: before})
	if got := len(s.(consoleProjectsScreen).projects); got != 1 {
		t.Fatalf("projects = %d, want 1: an older read brought the deleted project back", got)
	}
	s, _ = consoleSettle(s, env, reload)
	if got := s.(consoleProjectsScreen).projects[0].Name; got != "team" {
		t.Fatalf("remaining project = %q, want team", got)
	}
}

func TestConsoleProjectsEmpty(t *testing.T) {
	t.Parallel()
	env := consoleEnvFor(newConsoleService(), 150, 24)
	var s adminScreen = newConsoleProjectsScreen(nil)
	s, cmd := consoleKeys(s, env, testKeyRunes("d"), testKeyEnter(), testKeyRunes("s"), testKeyDown())
	if cmd != nil || s.(consoleProjectsScreen).confirm.Active() {
		t.Fatal("keys on an empty list must do nothing")
	}
	if out := consoleRender(s, env); !strings.Contains(out, "No projects yet") {
		t.Fatalf("no empty state:\n%s", out)
	}
}

// ---- Tags ----

func tagsFixture() []appregixtry.TagDetails {
	return []appregixtry.TagDetails{
		{Name: "latest", CreatedAt: consoleTime, PushedBy: "ci", SigningEnabled: true, SignatureState: appregixtry.SignatureStatusVerified},
		{Name: "1.0", CreatedAt: consoleTime.Add(-48 * time.Hour), SigningEnabled: true, SignatureState: appregixtry.SignatureStatusUnsigned},
		{Name: "0.9", CreatedAt: consoleTime.Add(-96 * time.Hour), PushedBy: "alice"},
	}
}

func loadedTags(t *testing.T, service *consoleService, env screenEnv) adminScreen {
	t.Helper()
	service.tags["r/a"] = tagsFixture()
	var s adminScreen = newConsoleTagsScreen("r/a", sortByNameAsc)
	s, _ = consoleSettle(s, env, s.Init(env))
	if !s.(consoleTagsScreen).loaded {
		t.Fatal("the tags did not load")
	}
	return s
}

func TestConsoleTagsLoadingErrorAndEmptyStates(t *testing.T) {
	t.Parallel()
	service := newConsoleService()
	env := consoleEnvFor(service, 150, 24)
	var s adminScreen = newConsoleTagsScreen("r/a", sortByNameAsc)
	if out := consoleRender(s, env); !strings.Contains(out, "Loading tags...") {
		t.Fatalf("no loading state:\n%s", out)
	}
	failed, _ := consoleStep(s, env, consoleTagsMsg{gen: s.(consoleTagsScreen).gen, err: errors.New("denied")})
	if out := consoleRender(failed, env); !strings.Contains(out, "Could not load tags: denied") || !strings.Contains(out, "Console › Repositories › r/a › Tags") {
		t.Fatalf("the load error is not shown on the screen:\n%s", out)
	}
	// Esc still goes back from the error.
	if _, cmd := consoleStep(failed, env, testKeyEsc()); cmd == nil {
		t.Fatal("Esc did nothing on a failed load")
	}
	empty, _ := consoleSettle(s, env, s.Init(env))
	if out := consoleRender(empty, env); !strings.Contains(out, "r/a has no published tags yet.") {
		t.Fatalf("no empty state:\n%s", out)
	}
}

func TestConsoleTagsShowColumnsSortedAndTheSortIndicator(t *testing.T) {
	t.Parallel()
	service := newConsoleService()
	env := consoleEnvFor(service, 150, 24)
	s := loadedTags(t, service, env)
	out := consoleRender(s, env)
	for _, want := range []string{"TAG", "CREATED", "PUSHED BY", "SIGNED", "2026-11-21 08:41", "ci", "yes", "no", "n/a", "unknown", "sort: name"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view lacks %q:\n%s", want, out)
		}
	}
	order := func() string {
		var names []string
		for _, tag := range s.(consoleTagsScreen).items {
			names = append(names, tag.Name)
		}
		return strings.Join(names, ",")
	}
	if order() != "0.9,1.0,latest" {
		t.Fatalf("by name = %s", order())
	}
	s, _ = consoleStep(s, env, testKeyRunes("s"))
	if order() != "latest,1.0,0.9" || !strings.Contains(consoleRender(s, env), "sort: date") {
		t.Fatalf("by date = %s", order())
	}
}

func TestConsoleTagsDeleteRemovesExactlyTheNamedTag(t *testing.T) {
	t.Parallel()
	service := newConsoleService()
	env := consoleEnvFor(service, 150, 24)
	s := loadedTags(t, service, env)
	s, _ = consoleKeys(s, env, testKeyDown(), testKeyRunes("d")) // 1.0
	confirm := s.(consoleTagsScreen).confirm
	if confirm.target != "r/a:1.0" || !confirm.destructive() || !strings.Contains(confirm.consequence, "other tags") {
		t.Fatalf("dialog = %+v", confirm)
	}
	// Sort changes and a reload happen under the dialog.
	s, _ = consoleStep(s, env, testKeyRunes("s"))
	s, _ = consoleStep(s, env, consoleTagsMsg{gen: s.(consoleTagsScreen).gen, stamp: consoleStamp{seq: 1}, result: slices.Clone(service.tags["r/a"])})
	s, cmd := consoleStep(s, env, testKeyEnter())
	s, _ = consoleSettle(s, env, cmd)
	if !slices.Equal(service.deletedTags, []string{"r/a:1.0"}) {
		t.Fatalf("deleted %v, want [r/a:1.0]", service.deletedTags)
	}
	tags := s.(consoleTagsScreen)
	if len(tags.items) != 2 || tags.confirm.Active() || tags.shared.isBusy() || tags.noticeFail {
		t.Fatalf("after the delete: items=%d confirm=%v busy=%v", len(tags.items), tags.confirm.Active(), tags.shared.isBusy())
	}
}

func TestConsoleTagsDeleteErrorKeepsTheScreenAndTheListReloadNeverResurrects(t *testing.T) {
	t.Parallel()
	service := newConsoleService()
	env := consoleEnvFor(service, 150, 24)
	s := loadedTags(t, service, env)
	service.failTag = errors.New("manifest deletion is not enabled")
	s, cmd := consoleKeys(s, env, testKeyRunes("d"), testKeyEnter())
	s, _ = consoleSettle(s, env, cmd)
	if tags := s.(consoleTagsScreen); len(tags.items) != 3 || !tags.noticeFail || !strings.Contains(consoleRender(s, env), "manifest deletion is not enabled") {
		t.Fatalf("a failed delete must keep the list and show the error")
	}
	service.failTag = nil
	before := slices.Clone(service.tags["r/a"])
	s, cmd = consoleKeys(s, env, testKeyRunes("d"), testKeyEnter())
	s, reload := consoleStep(s, env, cmd())
	s, _ = consoleStep(s, env, consoleTagsMsg{gen: s.(consoleTagsScreen).gen, stamp: consoleStamp{seq: 0}, result: before})
	if got := len(s.(consoleTagsScreen).items); got != 2 {
		t.Fatalf("items = %d, want 2: a read taken before the delete resurrected the tag", got)
	}
	s, _ = consoleSettle(s, env, reload)
	if got := len(s.(consoleTagsScreen).items); got != 2 {
		t.Fatalf("items after the reload = %d, want 2", got)
	}
}

func TestConsoleTagsEnterOpensTheManifestAndOneDeletePerConfirmation(t *testing.T) {
	t.Parallel()
	service := newConsoleService()
	env := consoleEnvFor(service, 150, 24)
	s := loadedTags(t, service, env)
	_, cmd := consoleStep(s, env, testKeyEnter())
	if got, ok := cmd().(openManifestMsg); !ok || got.repository != "r/a" || got.tag != "0.9" {
		t.Fatalf("Enter asked for %#v", cmd())
	}
	s, _ = consoleKeys(s, env, testKeyRunes("d"))
	s, first := consoleStep(s, env, testKeyEnter())
	for _, k := range []keyMsg{testKeyEnter(), testKeyRunes("y")} {
		if _, again := consoleStep(s, env, k); again != nil {
			t.Fatalf("%v sent a second delete", k)
		}
	}
	first()
	if len(service.deletedTags) != 1 {
		t.Fatalf("deleted %v, want one", service.deletedTags)
	}
}

func TestConsoleTagsDropAResultOfAnotherInstance(t *testing.T) {
	t.Parallel()
	service := newConsoleService()
	env := consoleEnvFor(service, 150, 24)
	s := loadedTags(t, service, env)
	gen := s.(consoleTagsScreen).gen
	s, _ = consoleStep(s, env, consoleTagsMsg{gen: gen + 1, result: nil})
	s, _ = consoleStep(s, env, tagDeletedMsg{gen: gen + 1, repository: "r/a", tag: "latest", err: errors.New("denied")})
	if len(s.(consoleTagsScreen).items) != 3 {
		t.Fatal("a foreign result changed the tags")
	}
}

// Whatever keys, reloads and results arrive between d and the confirm, the
// delete is the one the dialog named.
func TestConsoleDeleteTargetIsAlwaysTheOneNamedInTheDialog(t *testing.T) {
	t.Parallel()
	for seed := range 200 {
		service := newConsoleService(consoleRepos("r/a", "r/b", "r/c", "r/d", "r/e")...)
		env := consoleEnvFor(service, 150, 24)
		var s adminScreen = newConsoleRepositoriesScreen(service.repositories)
		gen := s.(consoleRepositoriesScreen).gen
		for i := range seed % 5 {
			_ = i
			s, _ = consoleStep(s, env, testKeyDown())
		}
		s, _ = consoleStep(s, env, testKeyRunes("d"))
		named := s.(consoleRepositoriesScreen).confirm.target
		for i := range (seed*7)%9 + 1 {
			switch (seed + i) % 6 {
			case 0:
				s, _ = consoleStep(s, env, testKeyDown())
			case 1:
				s, _ = consoleStep(s, env, testKeyUp())
			case 2:
				shuffled := slices.Clone(service.repositories)
				slices.Reverse(shuffled)
				s, _ = consoleStep(s, env, consoleCatalogMsg{gen: gen, stamp: consoleStamp{seq: i}, result: shuffled})
			case 3:
				s, _ = consoleStep(s, env, consoleCatalogMsg{gen: gen, stamp: consoleStamp{seq: i}})
			case 4:
				s, _ = consoleStep(s, env, testKeyRunes("d"))
			case 5:
				s, _ = consoleStep(s, env, testKeyRunes("s"))
			}
		}
		_, cmd := consoleStep(s, env, testKeyRunes("y"))
		if cmd == nil {
			t.Fatalf("seed %d: y sent nothing", seed)
		}
		cmd()
		if !slices.Equal(service.deletedRepos, []string{named}) {
			t.Fatalf("seed %d: deleted %v, the dialog named %q", seed, service.deletedRepos, named)
		}
	}
	_ = fmt.Sprint
}
