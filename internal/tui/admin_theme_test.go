package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// srgbLinearize converts one sRGB channel (0-1) to its linear-light value,
// the first step of the WCAG relative luminance formula.
func srgbLinearize(c float64) float64 {
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// relativeLuminance computes the WCAG relative luminance of a "#RRGGBB" hex
// string, matching the values design.md's palette table was derived from
// (e.g. "#A7B1C2" ≈ 0.43).
func relativeLuminance(t *testing.T, hex string) float64 {
	t.Helper()
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		t.Fatalf("relativeLuminance(%q): want a 6-digit hex color", hex)
	}
	r, err := strconv.ParseInt(hex[0:2], 16, 64)
	if err != nil {
		t.Fatalf("relativeLuminance(%q): invalid red channel: %v", hex, err)
	}
	g, err := strconv.ParseInt(hex[2:4], 16, 64)
	if err != nil {
		t.Fatalf("relativeLuminance(%q): invalid green channel: %v", hex, err)
	}
	b, err := strconv.ParseInt(hex[4:6], 16, 64)
	if err != nil {
		t.Fatalf("relativeLuminance(%q): invalid blue channel: %v", hex, err)
	}
	rl := srgbLinearize(float64(r) / 255)
	gl := srgbLinearize(float64(g) / 255)
	bl := srgbLinearize(float64(b) / 255)
	return 0.2126*rl + 0.7152*gl + 0.0722*bl
}

// styleForegroundHex extracts a lipgloss.Style's foreground as a plain hex
// string, failing loudly if the style was not built with lipgloss.Color
// (the theme's only foreground constructor).
func styleForegroundHex(t *testing.T, style lipgloss.Style) string {
	t.Helper()
	fg := style.GetForeground()
	if fg == nil {
		t.Fatalf("style.GetForeground() = nil, want a color")
	}
	r, g, b, _ := fg.RGBA()
	return fmt.Sprintf("#%02X%02X%02X", r>>8, g>>8, b>>8)
}

// TestSeverityRampIsFourDistinctColors replaces the earlier luminance
// ordering (critical < medium < low < high), which was an artifact of the
// old hues and no longer holds in the design palette: what matters is that
// the four severities stay pairwise distinguishable by color.
func TestSeverityRampIsFourDistinctColors(t *testing.T) {
	t.Parallel()

	got := map[string]string{
		"critical": styleForegroundHex(t, severityStyle("CRITICAL")),
		"medium":   styleForegroundHex(t, severityStyle("MEDIUM")),
		"low":      styleForegroundHex(t, severityStyle("LOW")),
		"high":     styleForegroundHex(t, severityStyle("HIGH")),
	}
	seen := map[string]string{}
	for name, hex := range got {
		if other, dup := seen[hex]; dup {
			t.Fatalf("severity %s and %s share foreground %s", name, other, hex)
		}
		seen[hex] = name
	}
}

// TestThemeInputAndInputFocusRenderSingleRowWithNoBorder is the Phase 2 task
// 2.1 RED test (design.md Decision 5): theme.input and theme.inputFocus
// currently carry NormalBorder (top+bottom), costing 2 rows per field for
// content that is a single line. Flattening drops the border entirely, so
// both styles must render exactly 1 row and contain no border-drawing
// runes, with focus re-expressed purely through color.
func TestThemeInputAndInputFocusRenderSingleRowWithNoBorder(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	tests := []struct {
		name  string
		style lipgloss.Style
	}{
		{name: "input", style: theme.input},
		{name: "inputFocus", style: theme.inputFocus},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rendered := tc.style.Render("some-value")
			if got := lipgloss.Height(rendered); got != 1 {
				t.Fatalf("%s height = %d, want exactly 1 (border dropped, flattened to a single content row)", tc.name, got)
			}
			for _, borderRune := range []rune{'─', '│', '┌', '┐', '└', '┘'} {
				if strings.ContainsRune(rendered, borderRune) {
					t.Fatalf("%s rendered = %q, want no border rune %q — theme.input/theme.inputFocus must not carry Border() after flattening", tc.name, rendered, borderRune)
				}
			}
		})
	}
}

// TestThemeInputFocusDiffersFromInputWithoutAnyBorderRune is the Phase 2 task
// 2.1 RED test's companion: since the border can no longer carry the focus
// signal, the focused and unfocused renders of the same value must still
// differ (focus re-expressed via theme.selected's gold fill/foreground), and
// neither may contain a border rune.
func TestThemeInputFocusDiffersFromInputWithoutAnyBorderRune(t *testing.T) {
	// lipgloss v2 always renders full-fidelity ANSI, so no color profile needs
	// forcing for the styled output below to be distinguishable from plain text.

	theme := newAdminTheme()

	unfocused := theme.input.Render("some-value")
	focused := theme.inputFocus.Render("some-value")

	if unfocused == focused {
		t.Fatalf("theme.input.Render() == theme.inputFocus.Render() = %q, want focus to visibly differ from unfocused", unfocused)
	}
	for _, rendered := range []string{unfocused, focused} {
		for _, borderRune := range []rune{'─', '│', '┌', '┐', '└', '┘'} {
			if strings.ContainsRune(rendered, borderRune) {
				t.Fatalf("rendered = %q, want no border rune %q", rendered, borderRune)
			}
		}
	}
}
