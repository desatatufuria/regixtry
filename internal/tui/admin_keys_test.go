package tui

import (
	"sort"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
)

// TestEveryKeyHandledIsInTheKeyMapAndViceVersa is the tui-menu-architecture
// change's Phase 4 task 4.1 (T1.5) RED test (design.md Decision D): every
// discrete action key updateGitleaksConfigModalKey's original switch handled
// (Esc/Tab/Space/Enter — free-text editing via Backspace/plain runes is
// deliberately excluded, mirroring the pre-change hand-written help string,
// which never enumerated them either) must be present in gitleaksConfigKeys,
// and gitleaksConfigKeys must declare no binding beyond that set — both
// directions of drift are unrepresentable once matches() is the only
// key-matching path a migrated screen may use.
func TestEveryKeyHandledIsInTheKeyMapAndViceVersa(t *testing.T) {
	t.Parallel()

	want := []string{"Enter", "Tab", "Space", "Esc"}
	got := make([]string, 0, len(gitleaksConfigKeys.short))
	for _, b := range gitleaksConfigKeys.short {
		got = append(got, b.Help().Key)
	}

	sortedWant := append([]string(nil), want...)
	sortedGot := append([]string(nil), got...)
	sort.Strings(sortedWant)
	sort.Strings(sortedGot)

	if len(sortedGot) != len(sortedWant) {
		t.Fatalf("gitleaksConfigKeys.short labels = %v, want exactly %v", got, want)
	}
	for i := range sortedWant {
		if sortedGot[i] != sortedWant[i] {
			t.Fatalf("gitleaksConfigKeys.short labels = %v, want exactly %v", got, want)
		}
	}
}

// TestRemovingABindingRemovesItFromRenderedHelp is the tui-menu-architecture
// change's Phase 4 task 4.2 (T1.4) RED test (spec.md "Removing a binding
// removes it from rendered help"): removing a binding from a COPY of a
// screen's key.Map must remove it from shortHelpView's rendered output,
// without needing any change to the render code — the footer is generated
// from Keys(), not hand-maintained.
func TestRemovingABindingRemovesItFromRenderedHelp(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	full := shortHelpView(theme, gitleaksConfigKeys)
	if !strings.Contains(full, "Esc") {
		t.Fatalf("shortHelpView(full) = %q, want it to contain the Esc binding before removal", full)
	}

	reduced := screenKeys{short: append([]key.Binding(nil), gitleaksConfigKeys.short[:len(gitleaksConfigKeys.short)-1]...)}
	removedLabel := gitleaksConfigKeys.short[len(gitleaksConfigKeys.short)-1].Help().Key

	got := shortHelpView(theme, reduced)
	if strings.Contains(got, removedLabel) {
		t.Fatalf("shortHelpView(reduced) = %q, want the removed binding %q absent", got, removedLabel)
	}
}
