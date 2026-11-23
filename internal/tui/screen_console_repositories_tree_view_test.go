package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	appregixtry "regixtry/internal/app/regixtry"
)

var updateConsoleTreeGoldens = flag.Bool("update-console-tree", false, "rewrite the console folder tree golden files")

// expandAll opens every folder of the tree.
func expandAll(s consoleRepositoriesScreen) consoleRepositoriesScreen {
	walkTree(s.tree.root, func(n *folderNode) {
		if n.isFolder() {
			s = s.withExpanded(n.path, true)
		}
	})
	return s
}

// deepRepos is n repositories of project "proj", depth levels deep in all
// (depth 1 is proj/<name>), with long segment names.
func deepRepos(n, depth int) []appregixtry.RepositorySummary {
	out := make([]appregixtry.RepositorySummary, n)
	for i := range out {
		parts := []string{"proj"}
		for d := 1; d < depth; d++ {
			parts = append(parts, fmt.Sprintf("a-rather-long-folder-name-level-%d-group-%02d", d, (i/(d*3+1))%7))
		}
		parts = append(parts, fmt.Sprintf("service-with-a-long-name-%05d", i))
		out[i] = appregixtry.RepositorySummary{Name: strings.Join(parts, "/"), TagCount: i % 90, LastPushed: consoleTime.Add(-time.Duration(i) * time.Minute)}
	}
	return out
}

func treeLines(m Model) []string { return strings.Split(ansi.Strip(m.View().Content), "\n") }

func TestTreeGeometryAtEverySizeCatalogAndDepth(t *testing.T) {
	t.Parallel()
	for _, size := range consoleSizes {
		for _, n := range []int{1, 100, 5000} {
			for _, depth := range []int{1, 4, 8} {
				for _, atEnd := range []bool{false, true} {
					s := expandAll(newConsoleRepositoriesScreen(deepRepos(n, depth)).narrowedTo("proj"))
					if atEnd {
						s.selected = len(s.rows()) - 1
					}
					m := consoleMouseModel(t, screenRepositories, s, size.w, size.h)
					name := fmt.Sprintf("%dx%d n=%d depth=%d end=%v", size.w, size.h, n, depth, atEnd)
					lines := treeLines(m)
					if len(lines) != size.h {
						t.Fatalf("%s: %d rows, want exactly %d", name, len(lines), size.h)
					}
					selected := 0
					for i, line := range lines {
						if w := ansi.StringWidth(line); w > size.w {
							t.Fatalf("%s: row %d is %d columns wide:\n%s", name, i, w, line)
						}
						if strings.Contains(line, "│ ▸ ") {
							selected++
						}
					}
					out := strings.Join(lines, "\n")
					if selected != 1 {
						t.Fatalf("%s: %d selected rows drawn, want exactly one:\n%s", name, selected, out)
					}
					for _, want := range []string{"q quit", "REPOS", "LAST PUSH", "Console › Projects › proj"} {
						if !strings.Contains(out, want) {
							t.Fatalf("%s lacks %q:\n%s", name, want, out)
						}
					}
					if panel := strings.Contains(out, "SELECTED"); panel != (size.w >= treePanelMinWidth) {
						t.Fatalf("%s: detail panel shown = %v", name, panel)
					}
				}
			}
		}
	}
}

func TestTreeADeepNameKeepsItsNameVisibleAtTheNarrowestSize(t *testing.T) {
	t.Parallel()
	s := expandAll(newConsoleRepositoriesScreen(deepRepos(30, 8)).narrowedTo("proj"))
	s.selected = len(s.rows()) - 1
	out := ansi.Strip(consoleRender(s, consoleEnvFor(newConsoleService(), 150, 24)))
	if !strings.Contains(out, "service-with-a-long-name-") {
		t.Fatalf("the name of a depth-8 repository is not visible:\n%s", out)
	}
	if indent := treeIndent(8, 20); indent != "…"+strings.Repeat(" ", 5) {
		t.Fatalf("a deep indent in a narrow column = %q, want it capped with an ellipsis", indent)
	}
	if indent := treeIndent(2, 100); indent != "    " {
		t.Fatalf("indent = %q, want two cells a level", indent)
	}
}

func TestTreeAFrameOf5000RepositoriesRendersUnder20ms(t *testing.T) {
	s := expandAll(newConsoleRepositoriesScreen(deepRepos(5000, 4)).narrowedTo("proj"))
	s.selected = 2500
	env := consoleEnvFor(newConsoleService(), 200, 40)
	theme := newAdminTheme()
	best := time.Hour
	for range 15 {
		start := time.Now()
		_ = s.View(theme, env)
		best = min(best, time.Since(start))
	}
	if limit := 20 * time.Millisecond * treeFrameBudgetFactor; best > limit {
		t.Fatalf("a frame of 5000 repositories took %v, want under %v", best, limit)
	}
}

// ---- mouse ----

func treeMouseModelAt(t *testing.T) Model {
	t.Helper()
	return consoleMouseModel(t, screenRepositories, newConsoleRepositoriesScreen(consoleRepos(treeNames...)).narrowedTo("odabank"), 150, 24)
}

// cellOf is the cell where text starts in the rendered view.
func cellOf(t *testing.T, m Model, text string) (x, y int) {
	t.Helper()
	for y, line := range treeLines(m) {
		if i := strings.Index(line, text); i >= 0 {
			return ansi.StringWidth(line[:i]), y
		}
	}
	t.Fatalf("%q is not on the screen:\n%s", text, strings.Join(treeLines(m), "\n"))
	return 0, 0
}

func treeScreenOf(m Model) consoleRepositoriesScreen {
	return m.adminScreens[slotConsoleRepositories].(consoleRepositoriesScreen)
}

func TestTreeClickSelectsARowAndNothingElse(t *testing.T) {
	t.Parallel()
	m := treeMouseModelAt(t)
	x, y := cellOf(t, m, "● payments")
	m = sendMouse(t, m, mouseClickAt(x+4, y))
	got := treeScreenOf(m)
	if selectedPath(got) != "odabank/payments" || m.screen != screenRepositories || got.confirm.Active() || len(got.rows()) != 3 {
		t.Fatalf("click: selected %q screen %q rows %v", selectedPath(got), m.screen, rowPaths(got))
	}
}

func TestTreeClickOnTheFoldMarkerTogglesTheFolder(t *testing.T) {
	t.Parallel()
	m := treeMouseModelAt(t)
	x, y := cellOf(t, m, "● payments")
	m = sendMouse(t, m, mouseClickAt(x-2, y)) // the fold slot, two cells before the name
	got := treeScreenOf(m)
	if selectedPath(got) != "odabank/payments" || len(got.rows()) != 4 {
		t.Fatalf("marker click: selected %q rows %v, want payments selected and open", selectedPath(got), rowPaths(got))
	}
	if out := strings.Join(treeLines(m), "\n"); !strings.Contains(out, "▾ ● payments") || !strings.Contains(out, "strike") {
		t.Fatalf("the open folder is not drawn:\n%s", out)
	}
	m = sendMouse(t, m, mouseClickAt(x-2, y))
	if got := treeScreenOf(m); len(got.rows()) != 3 {
		t.Fatalf("second marker click did not close it: %v", rowPaths(got))
	}
	// A click on a leaf's empty fold slot only selects it.
	x, y = cellOf(t, m, "● ledger")
	m = sendMouse(t, m, mouseClickAt(x-2, y))
	if got := treeScreenOf(m); selectedPath(got) != "odabank/ledger" || len(got.rows()) != 3 {
		t.Fatalf("click on a leaf's fold slot: selected %q rows %v", selectedPath(got), rowPaths(got))
	}
}

func TestTreeSecondClickOnTheSelectedRowOpensTagsOrEntersTheFolder(t *testing.T) {
	t.Parallel()
	m := treeMouseModelAt(t)
	x, y := cellOf(t, m, "ledger")
	m = sendMouse(t, m, mouseClickAt(x, y)) // already selected: Enter
	if m.screen != screenTags {
		t.Fatalf("click on the selected repository: screen %q, want the tags", m.screen)
	}
	m = treeMouseModelAt(t)
	x, y = cellOf(t, m, "zeta/")
	m = sendMouse(t, sendMouse(t, m, mouseClickAt(x, y)), mouseClickAt(x, y))
	if got := treeScreenOf(m); got.rootPath != "odabank/zeta" || m.screen != screenRepositories {
		t.Fatalf("second click on a folder: root %q screen %q", got.rootPath, m.screen)
	}
}

func TestTreeWheelScrollsAndAClickOutsideTheRowsDoesNothing(t *testing.T) {
	t.Parallel()
	m := treeMouseModelAt(t)
	m = sendMouse(t, m, mouseWheel(tea.MouseWheelDown))
	if selectedPath(treeScreenOf(m)) != "odabank/payments" {
		t.Fatalf("wheel down: %q", selectedPath(treeScreenOf(m)))
	}
	m = sendMouse(t, m, mouseWheel(tea.MouseWheelUp))
	if selectedPath(treeScreenOf(m)) != "odabank/ledger" {
		t.Fatalf("wheel up: %q", selectedPath(treeScreenOf(m)))
	}
	before := treeScreenOf(m).selected
	m = sendMouse(t, m, mouseClickAt(0, 0))
	m = sendMouse(t, m, mouseClickAt(149, 23))
	if treeScreenOf(m).selected != before || m.screen != screenRepositories {
		t.Fatal("a click outside the rows acted")
	}
}

func TestTreeMouseIsIgnoredWhileF2IsOff(t *testing.T) {
	t.Parallel()
	m := testSendKey(t, treeMouseModelAt(t), testKeyF2())
	x, y := cellOf(t, m, "● payments")
	for _, msg := range []tea.Msg{mouseClickAt(x-2, y), mouseClickAt(x+4, y), mouseWheel(tea.MouseWheelDown)} {
		m = sendMouse(t, m, msg)
	}
	if got := treeScreenOf(m); got.selected != 0 || len(got.rows()) != 3 {
		t.Fatalf("with the mouse off: selected %d rows %v", got.selected, rowPaths(got))
	}
}

func TestTreeMouseNeverOpensADeleteAndADialogBlocksIt(t *testing.T) {
	t.Parallel()
	m := treeMouseModelAt(t)
	rec := &writeRecorder{}
	m.service = recordingService{QueryService: m.service, rec: rec}
	for y := range 24 {
		for x := 0; x < 150; x += 3 {
			m = sendMouse(t, m, mouseClickAt(x, y))
			if m.screen != screenRepositories {
				m = treeMouseModelAt(t)
				m.service = recordingService{QueryService: m.service, rec: rec}
			}
			if treeScreenOf(m).confirm.Active() {
				t.Fatalf("a click at (%d,%d) opened a confirmation", x, y)
			}
		}
	}
	if rec.count() != 0 {
		t.Fatalf("the mouse wrote %v", rec.calls)
	}
	// A dialog opened by the key blocks the mouse: it stays and nothing is sent.
	m = testSendKey(t, treeMouseModelAt(t), testKeyRunes("d"))
	x, y := cellOf(t, m, "Cancel")
	m = sendMouse(t, m, mouseClickAt(x, y))
	if !treeScreenOf(m).confirm.Active() {
		t.Fatal("a click closed or confirmed the delete dialog")
	}
}

// ---- goldens ----

func assertTreeGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "console_tree", name+".golden")
	if *updateConsoleTreeGoldens {
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
		t.Fatalf("read golden %s (run with -update-console-tree): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("rendered view differs from %s (run with -update-console-tree to inspect)\n--- want\n%s\n--- got\n%s", path, strings.TrimRight(string(want), "\n"), strings.TrimRight(got, "\n"))
	}
}

// treeDriven builds a tree state with the keys an operator would press.
func treeDriven(project string, names []string, keys ...keyMsg) consoleRepositoriesScreen {
	var s adminScreen = newConsoleRepositoriesScreen(consoleRepos(names...)).narrowedTo(project)
	s, _ = consoleKeys(s, treeEnv(), keys...)
	return s.(consoleRepositoriesScreen)
}

var treeGoldenStates = []consoleState{
	{"tree_dual_root", func(t *testing.T) (screen, adminScreen) {
		names := []string{"odabank/payments", "odabank/payments/ledger", "odabank/payments/strike", "odabank/payments/strike/api", "odabank/payments/strike/worker", "odabank/payments/strike/worker/queue", "odabank/payments/strike/worker/retry", "odabank/risk", "odabank/risk/scoring", "odabank/portal"}
		// payments (a repository and a folder) is the first row: f, then open strike and
		// worker and put the cursor on queue.
		keys := []keyMsg{testKeyRunes("f"), testKeyDown(), testKeyDown(), testKeyRunes("l"), testKeyDown(), testKeyDown(), testKeyRunes("l"), testKeyDown()}
		return screenRepositories, treeDriven("odabank", names, keys...)
	}},
	{"tree_project", func(t *testing.T) (screen, adminScreen) {
		names := []string{"metalab/platform/runtime", "metalab/platform/runtime/api", "metalab/platform/ingress", "metalab/data/etl", "metalab/data/warehouse", "metalab/docs", "metalab/tools"}
		return screenRepositories, treeDriven("metalab", names, testKeyDown(), testKeyDown(), testKeyRunes("l"), testKeyDown(), testKeyDown(), testKeyRunes("l"))
	}},
	{"tree_ungrouped", func(t *testing.T) (screen, adminScreen) {
		return screenRepositories, treeDriven(projectUngroupedLabel, []string{"govault-api", "odabank", "tools", "team/x"}, testKeyDown())
	}},
	{"tree_empty", func(t *testing.T) (screen, adminScreen) {
		return screenRepositories, treeDriven("odabank", nil)
	}},
	{"tree_error", func(t *testing.T) (screen, adminScreen) {
		s := treeDriven("odabank", treeNames, testKeyRunes("l"))
		s.notice, s.noticeFail = "Could not refresh repositories: connection refused", true
		return screenRepositories, s
	}},
	{"tree_confirm", func(t *testing.T) (screen, adminScreen) {
		s := treeDriven("odabank", treeNames)
		s.confirm = s.deleteConfirm(s.rows()[0].node.repository)
		return screenRepositories, s
	}},
}

func TestConsoleTreeGoldens(t *testing.T) {
	for _, st := range treeGoldenStates {
		for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
			name := fmt.Sprintf("%s_%dx%d", st.name, size.w, size.h)
			t.Run(name, func(t *testing.T) {
				assertTreeGolden(t, name, ansi.Strip(consoleModelFor(t, st, size.w, size.h).View().Content))
			})
		}
	}
}

func TestTreeStatesFitEverySize(t *testing.T) {
	t.Parallel()
	for _, st := range treeGoldenStates {
		for _, size := range consoleSizes {
			lines := treeLines(consoleModelFor(t, st, size.w, size.h))
			if len(lines) != size.h {
				t.Fatalf("%s at %dx%d: %d rows", st.name, size.w, size.h, len(lines))
			}
			for i, line := range lines {
				if ansi.StringWidth(line) > size.w {
					t.Fatalf("%s at %dx%d: row %d overflows", st.name, size.w, size.h, i)
				}
			}
		}
	}
}

func TestTreeNamesGoThroughTheTerminalEscapeSanitizer(t *testing.T) {
	t.Parallel()
	hostile := "proj/evil\x1b]0;owned\x07name"
	s := newConsoleRepositoriesScreen(consoleRepos(hostile)).narrowedTo("proj")
	m := consoleMouseModel(t, screenRepositories, s, 200, 40)
	raw := m.View().Content
	if strings.Contains(raw, "\x07") || strings.Contains(raw, "\x1b]") {
		t.Fatalf("a terminal escape from a repository name reached the screen: %q", raw)
	}
}

// TestUnfilteredRepositoriesTableIsByteIdenticalToBefore pins the flat table:
// its goldens are those of the commit before the tree (their hashes), and the
// screen still renders exactly them.
func TestUnfilteredRepositoriesTableIsByteIdenticalToBefore(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"repositories_150x24": "cbbd2f136c7ed8e40c70eff6266723d18d074917e8ea2c0175e4271ab03e08bc",
		"repositories_200x40": "f0c142d04ba57e91aab7629685d1532fb5ff601fc3c9d17046698f24236d8722",
	}
	for name, hash := range want {
		raw, err := os.ReadFile(filepath.Join("testdata", "console", name+".golden"))
		if err != nil {
			t.Fatal(err)
		}
		if sum := sha256.Sum256(raw); hex.EncodeToString(sum[:]) != hash {
			t.Fatalf("%s.golden changed: the unfiltered table must stay byte-identical", name)
		}
	}
	for _, st := range consoleStates {
		if st.name != "repositories" {
			continue
		}
		for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
			raw, _ := os.ReadFile(filepath.Join("testdata", "console", fmt.Sprintf("repositories_%dx%d.golden", size.w, size.h)))
			if got := ansi.Strip(consoleModelFor(t, st, size.w, size.h).View().Content); got != string(raw) {
				t.Fatalf("the unfiltered table at %dx%d differs from its golden", size.w, size.h)
			}
		}
	}
}
