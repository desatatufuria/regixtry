package tui

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	appregixtry "regixtry/internal/app/regixtry"
)

var updateConsoleGoldens = flag.Bool("update-console", false, "rewrite the console browser golden files")

var consoleSizes = []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}}

// consoleState builds a Model showing one state of a console screen.
type consoleState struct {
	name  string
	build func(t *testing.T) (screen, adminScreen)
}

func manyRepos(n int) []appregixtry.RepositorySummary {
	out := make([]appregixtry.RepositorySummary, n)
	for i := range out {
		out[i] = appregixtry.RepositorySummary{Name: fmt.Sprintf("project%02d/service-%04d", i%40, i), TagCount: i % 90, LastPushed: consoleTime.Add(-time.Duration(i) * time.Hour)}
	}
	return out
}

func longTags() []appregixtry.TagDetails {
	out := tagsFixture()
	out = append(out,
		appregixtry.TagDetails{Name: "日本語のタグ名-とても長いタグの名前-とても長いタグの名前-とても長いタグの名前-とても長い", CreatedAt: consoleTime, PushedBy: "ユーザー名の非常に長い名前の人-ユーザー名の非常に長い名前の人", SigningEnabled: true},
		appregixtry.TagDetails{Name: strings.Repeat("v1.2.3-alpine-", 12), CreatedAt: consoleTime.Add(-time.Hour)},
	)
	return out
}

func manyTags(n int) []appregixtry.TagDetails {
	out := make([]appregixtry.TagDetails, n)
	for i := range out {
		out[i] = appregixtry.TagDetails{Name: fmt.Sprintf("1.%d.%d", i/10, i%10), CreatedAt: consoleTime.Add(-time.Duration(i) * time.Minute), PushedBy: "ci", SigningEnabled: i%2 == 0}
	}
	return out
}

func tagsScreenWith(items []appregixtry.TagDetails, mode sortMode) consoleTagsScreen {
	s := newConsoleTagsScreen("team/api", mode)
	sortTagItems(items, mode)
	s.items, s.loaded = items, true
	return s
}

var consoleStates = []consoleState{
	{"repositories", func(t *testing.T) (screen, adminScreen) {
		return screenRepositories, newConsoleRepositoriesScreen(consoleRepos("library/alpine", "team/api", "team/web", "team/docs", "smoke/keyless-live-verify", "govault-api"))
	}},
	{"repositories_project", func(t *testing.T) (screen, adminScreen) {
		return screenRepositories, newConsoleRepositoriesScreen(consoleRepos("library/alpine", "team/api", "team/web", "team/docs")).narrowedTo("team")
	}},
	{"repositories_empty", func(t *testing.T) (screen, adminScreen) {
		return screenRepositories, newConsoleRepositoriesScreen(nil)
	}},
	{"repositories_1000", func(t *testing.T) (screen, adminScreen) {
		s := newConsoleRepositoriesScreen(manyRepos(1000))
		s.selected = 777
		return screenRepositories, s
	}},
	{"repositories_long", func(t *testing.T) (screen, adminScreen) {
		return screenRepositories, newConsoleRepositoriesScreen(consoleRepos("日本語のプロジェクト/とても長いリポジトリ名とても長いリポジトリ名とても長いリポジトリ名とても長い", strings.Repeat("segment/", 20)+"leaf"))
	}},
	{"repositories_error", func(t *testing.T) (screen, adminScreen) {
		s := newConsoleRepositoriesScreen(consoleRepos("team/api", "team/web"))
		s.notice, s.noticeFail = "repository deletion is not enabled", true
		return screenRepositories, s
	}},
	{"repositories_confirm", func(t *testing.T) (screen, adminScreen) {
		s := newConsoleRepositoriesScreen(consoleRepos("team/api", "team/web"))
		s.confirm = s.deleteConfirm(s.items[0])
		return screenRepositories, s
	}},
	{"repositories_confirm_long", func(t *testing.T) (screen, adminScreen) {
		s := newConsoleRepositoriesScreen(consoleRepos("日本語のプロジェクト/とても長いリポジトリ名とても長いリポジトリ名とても長いリポジトリ名とても長い"))
		s.confirm = s.deleteConfirm(s.items[0])
		return screenRepositories, s
	}},
	{"repositories_deleting", func(t *testing.T) (screen, adminScreen) {
		s := newConsoleRepositoriesScreen(consoleRepos("team/api", "team/web"))
		s.confirm = s.deleteConfirm(s.items[0])
		s.confirm.inFlight = true
		s.shared.setBusy(true)
		return screenRepositories, s
	}},
	{"projects", func(t *testing.T) (screen, adminScreen) {
		return screenProjects, newConsoleProjectsScreen(consoleRepos("library/alpine", "team/api", "team/web", "ops/x", "flat"))
	}},
	{"projects_sorted", func(t *testing.T) (screen, adminScreen) {
		s := newConsoleProjectsScreen(consoleRepos("library/alpine", "team/api", "team/web", "ops/x", "flat"))
		s.sortMode = sortByDateDesc
		sortProjectItems(s.projects, s.sortMode)
		return screenProjects, s
	}},
	{"projects_empty", func(t *testing.T) (screen, adminScreen) {
		return screenProjects, newConsoleProjectsScreen(nil)
	}},
	{"projects_1000", func(t *testing.T) (screen, adminScreen) {
		s := newConsoleProjectsScreen(manyRepos(1000))
		s.selected = 39
		return screenProjects, s
	}},
	{"projects_long", func(t *testing.T) (screen, adminScreen) {
		return screenProjects, newConsoleProjectsScreen(consoleRepos("日本語のプロジェクト名とても長いプロジェクト名とても長いプロジェクト名とても長い/x", strings.Repeat("p", 120)+"/y"))
	}},
	{"projects_confirm", func(t *testing.T) (screen, adminScreen) {
		s := newConsoleProjectsScreen(consoleRepos("library/alpine", "team/api", "team/web"))
		s.selected = 1
		s.confirm = s.deleteConfirm(s.projects[1])
		return screenProjects, s
	}},
	{"projects_partial", func(t *testing.T) (screen, adminScreen) {
		s := newConsoleProjectsScreen(consoleRepos("team/api", "team/web"))
		s.notice, s.noticeFail = projectDeletionSummary("team", []projectDeletionResult{{repository: "team/api"}, {repository: "team/web", err: errors.New("forbidden")}}), true
		return screenProjects, s
	}},
	{"tags", func(t *testing.T) (screen, adminScreen) {
		return screenTags, tagsScreenWith(tagsFixture(), sortByNameAsc)
	}},
	{"tags_sorted", func(t *testing.T) (screen, adminScreen) {
		return screenTags, tagsScreenWith(tagsFixture(), sortByDateDesc)
	}},
	{"tags_loading", func(t *testing.T) (screen, adminScreen) {
		return screenTags, newConsoleTagsScreen("team/api", sortByNameAsc)
	}},
	{"tags_error", func(t *testing.T) (screen, adminScreen) {
		s := newConsoleTagsScreen("team/api", sortByNameAsc)
		s.err = "forbidden"
		return screenTags, s
	}},
	{"tags_empty", func(t *testing.T) (screen, adminScreen) {
		return screenTags, tagsScreenWith(nil, sortByNameAsc)
	}},
	{"tags_1000", func(t *testing.T) (screen, adminScreen) {
		s := tagsScreenWith(manyTags(1000), sortByNameAsc)
		s.selected = 500
		return screenTags, s
	}},
	{"tags_long", func(t *testing.T) (screen, adminScreen) {
		return screenTags, tagsScreenWith(longTags(), sortByNameAsc)
	}},
	{"tags_confirm", func(t *testing.T) (screen, adminScreen) {
		s := tagsScreenWith(tagsFixture(), sortByNameAsc)
		s.confirm = s.deleteConfirm(s.items[1])
		return screenTags, s
	}},
	{"tags_confirm_long", func(t *testing.T) (screen, adminScreen) {
		s := tagsScreenWith(longTags(), sortByNameAsc)
		s.confirm = s.deleteConfirm(s.items[3])
		return screenTags, s
	}},
}

func consoleModelFor(t *testing.T, st consoleState, w, h int) Model {
	t.Helper()
	id, built := st.build(t)
	m := NewModel(newConsoleService(), WithAdminClient(&fakeAdminClient{}))
	m.now = func() time.Time { return consoleTime }
	slot, _ := slotFor(id)
	m.adminScreens[slot] = built
	m.screen = id
	return resizeModel(t, m, w, h)
}

func TestConsoleGeometryInEveryStateAtEverySize(t *testing.T) {
	t.Parallel()
	for _, st := range consoleStates {
		for _, size := range consoleSizes {
			m := consoleModelFor(t, st, size.w, size.h)
			lines := strings.Split(ansi.Strip(m.View().Content), "\n")
			if len(lines) != size.h {
				t.Fatalf("%s at %dx%d: %d rows, want exactly %d", st.name, size.w, size.h, len(lines), size.h)
			}
			for i, line := range lines {
				if w := ansi.StringWidth(line); w > size.w {
					t.Fatalf("%s at %dx%d: row %d is %d columns wide:\n%s", st.name, size.w, size.h, i, w, line)
				}
			}
		}
	}
}

func TestConsoleScreensLeaveTheLastRowToTheVersionLine(t *testing.T) {
	t.Parallel()
	for _, st := range consoleStates {
		for _, size := range consoleSizes {
			m := consoleModelFor(t, st, size.w, size.h)
			m.currentVersion = "v1.2.3"
			if rows := len(strings.Split(m.View().Content, "\n")); rows != size.h {
				t.Fatalf("%s at %dx%d with the version line: %d rows, want %d", st.name, size.w, size.h, rows, size.h)
			}
		}
	}
}

func TestConsoleSelectionStaysVisibleInLongLists(t *testing.T) {
	t.Parallel()
	for _, st := range consoleStates {
		if !strings.HasSuffix(st.name, "_1000") {
			continue
		}
		for _, size := range consoleSizes {
			m := consoleModelFor(t, st, size.w, size.h)
			if out := ansi.Strip(m.View().Content); !strings.Contains(out, "▸ ") {
				t.Fatalf("%s at %dx%d: the selected row is not visible:\n%s", st.name, size.w, size.h, out)
			}
		}
	}
}

func TestConsoleDialogShowsTheTargetAtEverySize(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"repositories_confirm", "projects_confirm", "tags_confirm"} {
		for _, st := range consoleStates {
			if st.name != name {
				continue
			}
			for _, size := range consoleSizes {
				out := ansi.Strip(consoleModelFor(t, st, size.w, size.h).View().Content)
				for _, want := range []string{"Delete", "Cancel", "esc", "undone."} {
					if !strings.Contains(out, want) {
						t.Fatalf("%s at %dx%d lacks %q:\n%s", name, size.w, size.h, want, out)
					}
				}
			}
		}
	}
}

func TestConsoleGoldens(t *testing.T) {
	for _, st := range consoleStates {
		if strings.HasSuffix(st.name, "_1000") {
			continue
		}
		for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
			name := fmt.Sprintf("%s_%dx%d", st.name, size.w, size.h)
			t.Run(name, func(t *testing.T) {
				got := ansi.Strip(consoleModelFor(t, st, size.w, size.h).View().Content)
				assertConsoleGolden(t, name, got)
			})
		}
	}
}

func assertConsoleGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "console", name+".golden")
	if *updateConsoleGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update-console): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("rendered view differs from %s (run with -update-console to inspect)\n--- want\n%s\n--- got\n%s", path, strings.TrimRight(string(want), "\n"), strings.TrimRight(got, "\n"))
	}
}

func TestConsoleBreadcrumbKeepsTheLastCrumbWhenNamesAreVeryLong(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("segment/", 25) + "leaf"
	s := newConsoleTagsScreen(long, sortByNameAsc)
	m := NewModel(newConsoleService(), WithAdminClient(&fakeAdminClient{}))
	slot, _ := slotFor(screenTags)
	m.adminScreens[slot], m.screen = s, screenTags
	for _, size := range consoleSizes {
		first := strings.Split(ansi.Strip(resizeModel(t, m, size.w, size.h).View().Content), "\n")[0]
		if !strings.HasSuffix(strings.TrimRight(first, " "), "Tags") || ansi.StringWidth(first) > size.w {
			t.Fatalf("%dx%d: breadcrumb %q lost its last crumb", size.w, size.h, first)
		}
	}
}

func TestFitCrumbsNeverPanicsOnAnEmptyOrSingleTrail(t *testing.T) {
	t.Parallel()
	for _, width := range []int{0, 1, 10, 150} {
		if got := fitCrumbs(nil, width); len(got) != 0 {
			t.Fatalf("width %d: empty trail gave %v", width, got)
		}
		if got := fitCrumbs([]string{}, width); len(got) != 0 {
			t.Fatalf("width %d: empty trail gave %v", width, got)
		}
		if got := fitCrumbs([]string{"Console"}, width); len(got) != 1 {
			t.Fatalf("width %d: single crumb gave %v", width, got)
		}
	}
}
