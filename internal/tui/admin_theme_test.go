package tui

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
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
	color, ok := style.GetForeground().(lipgloss.Color)
	if !ok {
		t.Fatalf("style.GetForeground() = %#v, want a lipgloss.Color", style.GetForeground())
	}
	return string(color)
}

// TestSeverityMediumHasItsOwnDistinctHex is the Phase 1 task 1.1 RED test
// (design.md Decision 3 / spec.md "Severity Levels Are Visually
// Distinguishable By Hue"): severityHigh and severityMedium currently both
// resolve to warning's "#EBCB8B", differing only by Bold(true) — this test
// requires them to differ by hue, with severityMedium set to the confirmed
// "#C0A16B" bronze.
func TestSeverityMediumHasItsOwnDistinctHex(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	high := styleForegroundHex(t, theme.severityHigh)
	medium := styleForegroundHex(t, theme.severityMedium)

	if medium == high {
		t.Fatalf("severityMedium foreground = %q, want it to differ from severityHigh foreground %q", medium, high)
	}
	if medium != "#C0A16B" {
		t.Fatalf("severityMedium foreground = %q, want %q (design.md Decision 3, confirmed)", medium, "#C0A16B")
	}
}

// TestSeverityRampLuminanceOrdering guards the real luminance relationships
// design.md's palette table asserts once severityMedium moves off warning's
// hex: critical (errorColor, darkest/most alarming) < medium (new bronze) <
// low (muted grey) < high (warning amber, brightest). This is computed from
// the theme's real hex values via the WCAG relative luminance formula, not
// hardcoded, so it fails if any severity's underlying color drifts.
func TestSeverityRampLuminanceOrdering(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	critical := relativeLuminance(t, styleForegroundHex(t, theme.severityCritical))
	medium := relativeLuminance(t, styleForegroundHex(t, theme.severityMedium))
	low := relativeLuminance(t, styleForegroundHex(t, theme.severityLow))
	high := relativeLuminance(t, styleForegroundHex(t, theme.severityHigh))

	if !(critical < medium) {
		t.Fatalf("luminance(critical)=%.4f, luminance(medium)=%.4f, want critical < medium", critical, medium)
	}
	if !(medium < low) {
		t.Fatalf("luminance(medium)=%.4f, luminance(low)=%.4f, want medium < low", medium, low)
	}
	if !(low < high) {
		t.Fatalf("luminance(low)=%.4f, luminance(high)=%.4f, want low < high", low, high)
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
	// Not t.Parallel(): forces the global lipgloss color profile so the
	// focus/unfocus foreground+background actually emit distinguishable ANSI
	// sequences (go test runs with no tty, so lipgloss otherwise
	// auto-detects "no color" and the two renders would be byte-identical),
	// following TestRenderAdminScanHistoryModalRendersExecutionsColumnWithCursorHighlighted's
	// precedent in this same package.
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(original)

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
