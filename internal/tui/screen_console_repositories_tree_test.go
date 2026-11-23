package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func treeScreenFor(project string, names ...string) consoleRepositoriesScreen {
	return newConsoleRepositoriesScreen(consoleRepos(names...)).narrowedTo(project)
}

func asRepos(s adminScreen) consoleRepositoriesScreen { return s.(consoleRepositoriesScreen) }

// selectedPath is the path of the row under the cursor, "" when none.
func selectedPath(s consoleRepositoriesScreen) string {
	row, ok := s.selectedRow()
	if !ok {
		return ""
	}
	if row.self {
		return "self:" + row.node.path
	}
	return row.node.path
}

func rowPaths(s consoleRepositoriesScreen) []string {
	var out []string
	for _, r := range s.rows() {
		out = append(out, r.node.path)
	}
	return out
}

func treeEnv() screenEnv { return consoleEnvFor(newConsoleService(), 150, 24) }

var treeNames = []string{"odabank/ledger", "odabank/payments", "odabank/payments/strike", "odabank/payments/strike/worker", "odabank/zeta/deep/one"}

func TestTreeFirstLevelIsVisibleAndFoldersStartCollapsed(t *testing.T) {
	t.Parallel()
	s := treeScreenFor("odabank", treeNames...)
	if got := rowPaths(s); !slices.Equal(got, []string{"odabank/ledger", "odabank/payments", "odabank/zeta"}) {
		t.Fatalf("rows = %v", got)
	}
	if s.rootPath != "odabank" || len(s.visible()) != 5 {
		t.Fatalf("root %q, visible %d", s.rootPath, len(s.visible()))
	}
}

func TestTreeRightAndLExpandLeftAndHCollapseOrMoveToTheParent(t *testing.T) {
	t.Parallel()
	for _, expand := range []keyMsg{testKeyRight(), testKeyRunes("l")} {
		for _, collapse := range []keyMsg{testKeyLeft(), testKeyRunes("h")} {
			var s adminScreen = treeScreenFor("odabank", treeNames...)
			s, _ = consoleKeys(s, treeEnv(), testKeyDown(), expand) // payments
			if got := rowPaths(asRepos(s)); !slices.Equal(got, []string{"odabank/ledger", "odabank/payments", "odabank/payments/strike", "odabank/zeta"}) {
				t.Fatalf("%v expanded wrongly: %v", expand, got)
			}
			// Expanding an open folder or a leaf changes nothing.
			again, _ := consoleKeys(s, treeEnv(), expand)
			if !slices.Equal(rowPaths(asRepos(again)), rowPaths(asRepos(s))) {
				t.Fatal("expanding twice changed the rows")
			}
			// Left on the child goes to the parent, left again collapses it.
			s, _ = consoleKeys(s, treeEnv(), testKeyDown(), collapse)
			if selectedPath(asRepos(s)) != "odabank/payments" {
				t.Fatalf("%v on a child left the cursor on %q", collapse, selectedPath(asRepos(s)))
			}
			s, _ = consoleKeys(s, treeEnv(), collapse)
			if got := rowPaths(asRepos(s)); len(got) != 3 || selectedPath(asRepos(s)) != "odabank/payments" {
				t.Fatalf("%v did not collapse: %v", collapse, got)
			}
			// Left on a first-level leaf has no parent row: nothing moves.
			s, _ = consoleKeys(s, treeEnv(), testKeyUp(), collapse)
			if selectedPath(asRepos(s)) != "odabank/ledger" {
				t.Fatalf("left on a first-level row moved the cursor to %q", selectedPath(asRepos(s)))
			}
		}
	}
}

func TestTreeEnterOpensTagsOfARepositoryNodeAndEntersAFolderOnlyNode(t *testing.T) {
	t.Parallel()
	var s adminScreen = treeScreenFor("odabank", treeNames...)
	_, cmd := consoleStep(s, treeEnv(), testKeyEnter()) // ledger
	if got, ok := cmd().(openTagsMsg); !ok || got.repository != "odabank/ledger" {
		t.Fatalf("Enter on a repository asked for %#v", cmd())
	}
	_, cmd = consoleKeys(s, treeEnv(), testKeyDown(), testKeyEnter()) // payments: repository and folder
	if got, ok := cmd().(openTagsMsg); !ok || got.repository != "odabank/payments" {
		t.Fatalf("Enter on a repository that is also a folder asked for %#v", cmd())
	}
	s, cmd = consoleKeys(s, treeEnv(), testKeyDown(), testKeyDown(), testKeyEnter()) // zeta: folder only
	if cmd != nil || asRepos(s).rootPath != "odabank/zeta" || selectedPath(asRepos(s)) != "odabank/zeta/deep" {
		t.Fatalf("Enter on a folder: root %q, selected %q, cmd %v", asRepos(s).rootPath, selectedPath(asRepos(s)), cmd)
	}
}

func TestTreeFEntersAnyFolderAndBackspaceAndEscGoUpOneLevel(t *testing.T) {
	t.Parallel()
	for _, back := range []keyMsg{testKeyBackspace(), testKeyEsc()} {
		var s adminScreen = treeScreenFor("odabank", treeNames...)
		s, _ = consoleKeys(s, treeEnv(), testKeyDown(), testKeyRunes("f")) // payments, a repository too
		repos := asRepos(s)
		if repos.rootPath != "odabank/payments" || len(repos.rows()) != 2 || selectedPath(repos) != "self:odabank/payments" {
			t.Fatalf("f: root %q rows %v selected %q (the own row must be pinned first)", repos.rootPath, rowPaths(repos), selectedPath(repos))
		}
		if got := strings.Join(repos.treeCrumbs(), " › "); got != "Console › Projects › odabank › payments" {
			t.Fatalf("breadcrumb = %q", got)
		}
		// f on the pinned row, or on a leaf, does nothing.
		same, _ := consoleKeys(s, treeEnv(), testKeyRunes("f"))
		if asRepos(same).rootPath != "odabank/payments" {
			t.Fatal("f on the own row moved the root")
		}
		s, cmd := consoleStep(s, treeEnv(), back)
		repos = asRepos(s)
		if cmd != nil || repos.rootPath != "odabank" || !repos.filtered || selectedPath(repos) != "odabank/payments" {
			t.Fatalf("%v: root %q filtered %v selected %q, want the project root with the cursor on payments", back, repos.rootPath, repos.filtered, selectedPath(repos))
		}
		// At the project root Esc keeps its meaning: the whole catalog again.
		s, _ = consoleStep(s, treeEnv(), back)
		if asRepos(s).filtered || asRepos(s).rootPath != "" {
			t.Fatalf("%v at the project root must leave the project", back)
		}
	}
}

func TestTreeDeleteOnlyActsOnARepositoryNode(t *testing.T) {
	t.Parallel()
	var s adminScreen = treeScreenFor("odabank", treeNames...)
	s, _ = consoleKeys(s, treeEnv(), testKeyDown(), testKeyDown(), testKeyRunes("d")) // zeta: folder only
	if got := asRepos(s); got.confirm.Active() || got.notice != "" {
		t.Fatalf("d on a folder: confirm %v notice %q, want nothing", got.confirm.Active(), got.notice)
	}
	s, _ = consoleKeys(s, treeEnv(), testKeyUp(), testKeyRunes("d")) // payments
	if got := asRepos(s).confirm; !got.Active() || got.target != "odabank/payments" {
		t.Fatalf("d on a repository that is also a folder: %+v", got)
	}
	// g on a folder says to select a repository.
	var g adminScreen = treeScreenFor("odabank", treeNames...)
	g, _ = consoleKeys(g, treeEnv(), testKeyDown(), testKeyDown(), testKeyRunes("g"))
	if !strings.Contains(asRepos(g).notice, "Select a repository") {
		t.Fatalf("g on a folder: %q", asRepos(g).notice)
	}
}

func TestTreePAndTabKeepTheirMeaning(t *testing.T) {
	t.Parallel()
	var s adminScreen = treeScreenFor("odabank", treeNames...)
	_, cmd := consoleStep(s, treeEnv(), testKeyRunes("p"))
	if got, ok := cmd().(openProjectsMsg); !ok || len(got.repositories) != len(treeNames) {
		t.Fatalf("p asked for %#v", cmd())
	}
	env := treeEnv()
	env.Client = nil
	s, _ = consoleStep(s, env, testKeyTab())
	if asRepos(s).notice != consoleUnavailableNotice {
		t.Fatalf("Tab without an admin API: %q", asRepos(s).notice)
	}
}

func TestTreeNavigationKeysScrollTheVisibleRows(t *testing.T) {
	t.Parallel()
	var s adminScreen = newConsoleRepositoriesScreen(manyRepos(1000)).narrowedTo("project07")
	rows := len(asRepos(s).rows())
	for _, tc := range []struct {
		key  keyMsg
		want int
	}{{testKeyEnd(), rows - 1}, {testKeyPgDown(), min(consolePage, rows-1)}, {testKeyDown(), 1}, {testKeyHome(), 0}} {
		var got adminScreen
		got, _ = consoleStep(s, treeEnv(), tc.key)
		if asRepos(got).selected != tc.want {
			t.Fatalf("%v: selected %d, want %d of %d", tc.key, asRepos(got).selected, tc.want, rows)
		}
	}
}

func TestTreeAReloadKeepsExpansionAndSelectionByPath(t *testing.T) {
	t.Parallel()
	var s adminScreen = treeScreenFor("odabank", treeNames...)
	s, _ = consoleKeys(s, treeEnv(), testKeyDown(), testKeyRight(), testKeyDown()) // payments open, cursor on strike
	repos := asRepos(s)
	// The catalog comes back in another order and with a new repository
	// sorting before everything: the open folder stays open, the cursor stays
	// on strike.
	reversed := slices.Clone(treeNames)
	slices.Reverse(reversed)
	reloaded := consoleRepos(append([]string{"odabank/aaa"}, reversed...)...)
	s, _ = consoleStep(s, treeEnv(), consoleCatalogMsg{gen: repos.gen, stamp: consoleStamp{seq: 1}, result: reloaded})
	got := asRepos(s)
	if selectedPath(got) != "odabank/payments/strike" || !slices.Contains(rowPaths(got), "odabank/payments/strike") {
		t.Fatalf("after reload: selected %q rows %v", selectedPath(got), rowPaths(got))
	}
}

func TestTreeAReloadThatDropsTheSelectedRowFallsBackToTheNearestAncestor(t *testing.T) {
	t.Parallel()
	var s adminScreen = treeScreenFor("odabank", treeNames...)
	s, _ = consoleKeys(s, treeEnv(), testKeyDown(), testKeyRight(), testKeyDown()) // on strike
	repos := asRepos(s)
	without := consoleRepos("odabank/ledger", "odabank/payments", "odabank/zeta/deep/one")
	s, _ = consoleStep(s, treeEnv(), consoleCatalogMsg{gen: repos.gen, stamp: consoleStamp{seq: 1}, result: without})
	if selectedPath(asRepos(s)) != "odabank/payments" {
		t.Fatalf("selected %q, want the surviving ancestor odabank/payments", selectedPath(asRepos(s)))
	}
	// Re-rooted on a folder that disappears: the nearest surviving ancestor
	// becomes the root, never above the project.
	var r adminScreen = treeScreenFor("odabank", treeNames...)
	r, _ = consoleKeys(r, treeEnv(), testKeyDown(), testKeyDown(), testKeyEnter(), testKeyEnter()) // zeta, deep
	if asRepos(r).rootPath != "odabank/zeta/deep" {
		t.Fatalf("root = %q", asRepos(r).rootPath)
	}
	r, _ = consoleStep(r, treeEnv(), consoleCatalogMsg{gen: asRepos(r).gen, stamp: consoleStamp{seq: 1}, result: consoleRepos("odabank/ledger", "odabank/payments")})
	if asRepos(r).rootPath != "odabank" || !asRepos(r).filtered {
		t.Fatalf("root = %q filtered %v, want the project root", asRepos(r).rootPath, asRepos(r).filtered)
	}
}

func TestTreeIgnoresAStaleGenerationAndAnErrorKeepsTheTree(t *testing.T) {
	t.Parallel()
	var s adminScreen = treeScreenFor("odabank", treeNames...)
	gen := asRepos(s).gen
	s, _ = consoleStep(s, treeEnv(), consoleCatalogMsg{gen: gen + 7, result: consoleRepos("odabank/other")})
	if got := rowPaths(asRepos(s)); len(got) != 3 || got[0] != "odabank/ledger" {
		t.Fatalf("a foreign catalog changed the tree: %v", got)
	}
	s, _ = consoleStep(s, treeEnv(), consoleCatalogMsg{gen: gen, stamp: consoleStamp{seq: 1}, err: errors.New("boom")})
	got := asRepos(s)
	if !got.noticeFail || !strings.Contains(got.notice, "boom") || len(got.rows()) != 3 {
		t.Fatalf("error state: notice %q rows %d", got.notice, len(got.rows()))
	}
	if out := consoleRender(s, treeEnv()); !strings.Contains(out, "boom") || !strings.Contains(out, "ledger") {
		t.Fatalf("the error is not visible next to the tree:\n%s", out)
	}
}

func TestTreeEmptyCatalogShowsTheEmptyMessageAndKeysDoNothing(t *testing.T) {
	t.Parallel()
	var s adminScreen = newConsoleRepositoriesScreen(nil).narrowedTo("odabank")
	for _, k := range []keyMsg{testKeyDown(), testKeyRight(), testKeyLeft(), testKeyEnter(), testKeyRunes("f"), testKeyRunes("d"), testKeyEnd()} {
		var cmd tea.Cmd
		s, cmd = consoleStep(s, treeEnv(), k)
		if cmd != nil {
			t.Fatalf("%v on an empty tree returned a command", k)
		}
	}
	if out := consoleRender(s, treeEnv()); !strings.Contains(out, "No repositories in this folder.") || !strings.Contains(out, "0 repositories, 0 tags") {
		t.Fatalf("empty view:\n%s", out)
	}
}

func TestTreeFlatRepositoriesShowUnderTheUngroupedBucketWithTheirRealName(t *testing.T) {
	t.Parallel()
	var s adminScreen = newConsoleRepositoriesScreen(consoleRepos("odabank", "govault-api", "team/x")).narrowedTo(projectUngroupedLabel)
	if got := rowPaths(asRepos(s)); !slices.Equal(got, []string{"(ungrouped)/govault-api", "(ungrouped)/odabank"}) {
		t.Fatalf("rows = %v", got)
	}
	_, cmd := consoleKeys(s, treeEnv(), testKeyDown(), testKeyEnter())
	if got, ok := cmd().(openTagsMsg); !ok || got.repository != "odabank" {
		t.Fatalf("Enter asked for %#v, want the real name odabank", cmd())
	}
	if got := strings.Join(asRepos(s).treeCrumbs(), " › "); got != "Console › Projects › (ungrouped)" {
		t.Fatalf("breadcrumb = %q", got)
	}
}

func TestTreeDeleteOfThePinnedOwnRowAndFlatTableStayAsBefore(t *testing.T) {
	t.Parallel()
	var s adminScreen = treeScreenFor("odabank", treeNames...)
	s, _ = consoleKeys(s, treeEnv(), testKeyDown(), testKeyRunes("f"), testKeyRunes("d")) // own row of payments
	if got := asRepos(s).confirm; !got.Active() || got.target != "odabank/payments" {
		t.Fatalf("d on the own row: %+v", got)
	}
	// Without a project the screen is the flat table it always was.
	flat := newConsoleRepositoriesScreen(consoleRepos(treeNames...))
	if flat.filtered || len(flat.rows()) != 0 {
		t.Fatal("the unfiltered screen must not be a tree")
	}
}
