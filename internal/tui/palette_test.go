package tui

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// The approved design's palette, written out literally on purpose: the test
// pins the theme to the design, not to the production constants.
const (
	designPageBG     = "#0F1117"
	designPanelBG    = "#161A23"
	designBorder     = "#2A3042"
	designText       = "#D7DCE8"
	designMuted      = "#8A92A8"
	designAccent     = "#7AA2F7"
	designSelectedBG = "#1C2540"
	designWarning    = "#E0AF68"
	designSuccess    = "#9ECE6A"
	designError      = "#F7768E"
)

func colorHex(c color.Color) string {
	if c == nil {
		return "<nil>"
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02X%02X%02X", r>>8, g>>8, b>>8)
}

func TestThemeRolesUseTheDesignPalette(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	fg := []struct {
		role  string
		style lipgloss.Style
		want  string
	}{
		{"title", theme.title, designText},
		{"subheading", theme.subheading, designText},
		{"text", theme.text, designText},
		{"input", theme.input, designText},
		{"context", theme.context, designMuted},
		{"muted", theme.muted, designMuted},
		{"help", theme.help, designMuted},
		{"accent", theme.accent, designAccent},
		{"selected", theme.selected, designAccent},
		{"inputFocus", theme.inputFocus, designAccent},
		{"success", theme.success, designSuccess},
		{"warning", theme.warning, designWarning},
		{"severityHigh", theme.severityHigh, designWarning},
		{"error", theme.error, designError},
		{"severityCritical", theme.severityCritical, designError},
		{"pill", theme.pill, designPageBG},
	}
	for _, tc := range fg {
		if got := colorHex(tc.style.GetForeground()); got != tc.want {
			t.Errorf("%s foreground = %s, want %s", tc.role, got, tc.want)
		}
	}

	bg := []struct {
		role  string
		style lipgloss.Style
		want  string
	}{
		{"selected", theme.selected, designSelectedBG},
		{"inputFocus", theme.inputFocus, designSelectedBG},
		{"input", theme.input, designPanelBG},
		{"pill", theme.pill, designAccent},
	}
	for _, tc := range bg {
		if got := colorHex(tc.style.GetBackground()); got != tc.want {
			t.Errorf("%s background = %s, want %s", tc.role, got, tc.want)
		}
	}

	if got := colorHex(theme.borderColor); got != designBorder {
		t.Errorf("borderColor = %s, want %s", got, designBorder)
	}
	if got := colorHex(theme.section.GetBorderTopForeground()); got != designBorder {
		t.Errorf("section border = %s, want %s", got, designBorder)
	}
}

func contrastRatio(t *testing.T, a, b string) float64 {
	t.Helper()
	la, lb := relativeLuminance(t, a), relativeLuminance(t, b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func TestPaletteContrastOnPageAndPanel(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	fgs := map[string]string{
		"text":    colorHex(theme.text.GetForeground()),
		"muted":   colorHex(theme.muted.GetForeground()),
		"accent":  colorHex(theme.accent.GetForeground()),
		"warning": colorHex(theme.warning.GetForeground()),
		"success": colorHex(theme.success.GetForeground()),
		"error":   colorHex(theme.error.GetForeground()),
		"medium":  colorHex(severityStyle("MEDIUM").GetForeground()),
	}
	for name, fg := range fgs {
		for bgName, bg := range map[string]string{"page": designPageBG, "panel": designPanelBG} {
			ratio := contrastRatio(t, fg, bg)
			t.Logf("%s %s on %s %s = %.2f:1", name, fg, bgName, bg, ratio)
			if ratio < 4.5 {
				t.Errorf("%s %s on %s %s = %.2f:1, want >= 4.5:1", name, fg, bgName, bg, ratio)
			}
		}
	}
	// Selected rows: accent on the selected background.
	sel := newAdminTheme().selected
	ratio := contrastRatio(t, colorHex(sel.GetForeground()), colorHex(sel.GetBackground()))
	t.Logf("selected accent on selected bg = %.2f:1", ratio)
	if ratio < 4.5 {
		t.Errorf("selected row contrast = %.2f:1, want >= 4.5:1", ratio)
	}
	// Primary button: page color on accent.
	pill := newAdminTheme().pill
	ratio = contrastRatio(t, colorHex(pill.GetForeground()), colorHex(pill.GetBackground()))
	t.Logf("primary button = %.2f:1", ratio)
	if ratio < 4.5 {
		t.Errorf("primary button contrast = %.2f:1, want >= 4.5:1", ratio)
	}
}

// colorLiteral matches a hex color or a lipgloss.Color call with a string
// literal; named constants (lipgloss.Color(someHex)) are fine.
var colorLiteral = regexp.MustCompile(`#[0-9a-fA-F]{6}\b|Color\("`)

// paletteLiteralAllowlist lists the only production files allowed to hold a
// color literal: the single place the palette is defined.
var paletteLiteralAllowlist = map[string]bool{"admin_theme.go": true}

func TestNoColorLiteralsOutsideTheTheme(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || paletteLiteralAllowlist[f] {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if m := colorLiteral.FindString(line); m != "" {
				t.Errorf("%s:%d holds a color literal %q; define it in admin_theme.go", f, i+1, m)
			}
		}
	}
}

// legacyName finds identifiers or words from the retired theme, in production
// and test files alike. Built from pieces so this file does not match itself.
var legacyName = regexp.MustCompile(`(?i)(^|[^a-z])` + "no" + `rd`)

func TestNoLegacyThemeNamesRemain(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if legacyName.MatchString(line) {
				t.Errorf("%s:%d mentions the retired theme: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// The focused grant pane border uses the accent (never the selected-row
// background); the focus color must stay visible against the page.
func TestFocusBorderColorsAreVisibleAgainstThePage(t *testing.T) {
	t.Parallel()

	for name, hex := range map[string]string{
		"focused pane border (accent)": paletteAccentHex,
	} {
		ratio := contrastRatio(t, strings.ToUpper(hex), designPageBG)
		t.Logf("%s %s on page = %.2f:1", name, hex, ratio)
		if ratio < 3 {
			t.Errorf("%s = %.2f:1, want >= 3:1", name, ratio)
		}
	}
	data, err := os.ReadFile("screen_grant_access_view.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "paletteSelectedBGHex") {
		t.Error("screen_grant_access_view.go uses the selected-row background; borders must use the accent or border token")
	}
	t.Logf("unfocused border %s on page = %.2f:1 (design value, not asserted)", paletteBorderHex, contrastRatio(t, strings.ToUpper(paletteBorderHex), designPageBG))
}

func TestSeverityStylesUseTheDesignPalette(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"CRITICAL": designError, "HIGH": designWarning, "MEDIUM": designAccent, "LOW": designMuted, "UNKNOWN": designText,
	}
	for value, want := range cases {
		if got := colorHex(severityStyle(value).GetForeground()); got != want {
			t.Errorf("severityStyle(%s) = %s, want %s", value, got, want)
		}
	}
}
