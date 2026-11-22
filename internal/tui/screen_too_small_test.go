package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var updateTooSmallGoldens = flag.Bool("update-too-small", false, "rewrite the too-small notice golden files")

var tooSmallGoldenSizes = []struct{ w, h int }{
	{80, 24}, {120, 40}, {149, 24}, {150, 23}, {100, 10}, {40, 5}, {10, 3}, {1, 1},
}

func tooSmallSpecAt(w, h int) tooSmallSpec {
	return tooSmallSpec{Width: w, Height: h, MinWidth: minViewportWidth, MinHeight: minViewportHeight}
}

func assertTooSmallGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "too_small", name+".golden")
	if *updateTooSmallGoldens {
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
		t.Fatalf("read golden %s (run with -update-too-small): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("view differs from %s (run with -update-too-small)\n--- want\n%s\n--- got\n%s", path, want, got)
	}
}

func TestTooSmallGoldens(t *testing.T) {
	t.Parallel()
	for _, size := range tooSmallGoldenSizes {
		name := fmt.Sprintf("%dx%d", size.w, size.h)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertTooSmallGolden(t, name, ansi.Strip(renderTooSmall(newAdminTheme(), tooSmallSpecAt(size.w, size.h))))
		})
	}
}

func TestTooSmallNeverOverflowsAtAnySize(t *testing.T) {
	t.Parallel()
	for w := 0; w <= 160; w++ {
		for h := 0; h <= 30; h++ {
			out := renderTooSmall(newAdminTheme(), tooSmallSpecAt(w, h))
			if w < 1 || h < 1 {
				if out != "" {
					t.Fatalf("%dx%d rendered %q, want nothing", w, h, out)
				}
				continue
			}
			lines := strings.Split(out, "\n")
			if len(lines) > h {
				t.Fatalf("%dx%d: %d rows", w, h, len(lines))
			}
			for _, l := range lines {
				if got := ansi.StringWidth(l); got > w {
					t.Fatalf("%dx%d: line %q is %d wide", w, h, ansi.Strip(l), got)
				}
			}
		}
	}
}

func TestTooSmallShowsCurrentAndRequiredSize(t *testing.T) {
	t.Parallel()
	out := ansi.Strip(renderTooSmall(newAdminTheme(), tooSmallSpecAt(120, 20)))
	for _, want := range []string{
		"Terminal too small",
		fmt.Sprintf("Regixtry needs at least %dx%d. Current: 120x20.", minViewportWidth, minViewportHeight),
		"Resize, or press q to quit.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("notice missing %q:\n%s", want, out)
		}
	}
}

func TestTooSmallModelUpdatesLiveAndRestoresScreen(t *testing.T) {
	t.Parallel()
	m := fixedAdminModel(t, minViewportWidth, minViewportHeight)
	m.screen = screenAdminOverview
	normal := m.View().Content

	small := resizeModel(t, m, 100, 20)
	if out := ansi.Strip(small.View().Content); !strings.Contains(out, "Current: 100x20.") {
		t.Fatalf("notice missing 100x20:\n%s", out)
	}
	smaller := resizeModel(t, small, 90, 18)
	if out := ansi.Strip(smaller.View().Content); !strings.Contains(out, "Current: 90x18.") {
		t.Fatalf("notice did not follow the resize:\n%s", out)
	}
	if !reflect.DeepEqual(smaller.adminScreens, m.adminScreens) || smaller.screen != m.screen {
		t.Fatalf("resize while too small changed screen state")
	}
	restored := resizeModel(t, smaller, minViewportWidth, minViewportHeight)
	if restored.View().Content != normal {
		t.Fatalf("screen not restored after growing back")
	}
}

func TestTooSmallKeys(t *testing.T) {
	t.Parallel()
	quits := func(m Model, k keyMsg) bool {
		_, cmd := m.Update(k)
		if cmd == nil {
			return false
		}
		_, ok := cmd().(tea.QuitMsg)
		return ok
	}
	m := resizeModel(t, fixedAdminModel(t, minViewportWidth, minViewportHeight), 100, 20)
	m.screen = screenAdminOverview
	if !quits(m, testKeyCtrl('c')) || !quits(m, testKeyRunes("q")) {
		t.Fatalf("ctrl+c and q must quit while too small")
	}
	for _, k := range []keyMsg{testKeyEnter(), testKeyTab(), testKeyDown(), testKeyRunes("j"), testKeyEsc(), testKeyRunes("l")} {
		next, cmd := m.Update(k)
		if cmd != nil || next.(Model).screen != m.screen || !reflect.DeepEqual(next.(Model).adminScreens, m.adminScreens) {
			t.Fatalf("key %q was acted on while too small", keyString(k))
		}
	}

	login := m
	login.screen = screenAdminLogin
	if quits(login, testKeyRunes("q")) {
		t.Fatalf("q quit while the login text input has focus")
	}
	if !quits(login, testKeyCtrl('c')) {
		t.Fatalf("ctrl+c must quit even with a text input focused")
	}
}
