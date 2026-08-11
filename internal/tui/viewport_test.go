package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestViewportContentBudgetAccountsForChrome(t *testing.T) {
	t.Parallel()

	const width, height = defaultViewportWidth, defaultViewportHeight

	theme := newAdminTheme()
	statusHeight := lipgloss.Height(renderAdminStatus(theme, "ready"))
	helpHeight := lipgloss.Height(theme.help.Render("q: quit"))

	tests := []struct {
		name   string
		status string
		help   string
	}{
		{name: "status and help absent", status: "", help: ""},
		{name: "status present, help absent", status: "ready", help: ""},
		{name: "status absent, help present", status: "", help: "q: quit"},
		{name: "status and help present", status: "ready", help: "q: quit"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			chrome := 2 + sectionChromeRows // title(1) + context(1) + body section overhead
			if strings.TrimSpace(tc.status) != "" {
				chrome += statusHeight
			}
			if strings.TrimSpace(tc.help) != "" {
				chrome += helpHeight
			}
			want := height - chrome
			if want < minTableRows {
				want = minTableRows
			}

			got := contentBudget(width, height, tc.status, tc.help)
			if got.SectionRows != want {
				t.Fatalf("SectionRows = %d, want %d (chrome accounting for status=%q help=%q)", got.SectionRows, want, tc.status, tc.help)
			}
			if got.Width != width || got.Height != height {
				t.Fatalf("consoleLayout dims = %dx%d, want %dx%d", got.Width, got.Height, width, height)
			}
		})
	}
}

func TestViewportContentBudgetFloorsAtMinimumTableRows(t *testing.T) {
	t.Parallel()

	got := contentBudget(minViewportWidth, minViewportHeight, "ready", "q: quit")
	if got.SectionRows < minTableRows {
		t.Fatalf("SectionRows = %d, want >= %d floor at minimum terminal size", got.SectionRows, minTableRows)
	}
}

func TestViewportFitLinesClampsToBudgetAndReportsIndicator(t *testing.T) {
	t.Parallel()

	inner := strings.Join([]string{"line1", "line2", "line3", "line4", "line5"}, "\n")

	fitted, indicator := fitLines(inner, 3, 0)

	gotLines := strings.Split(fitted, "\n")
	if len(gotLines) != 2 {
		t.Fatalf("fitted lines = %d, want 2 (budget 3 minus 1 row reserved for the indicator)", len(gotLines))
	}
	if gotLines[0] != "line1" || gotLines[1] != "line2" {
		t.Fatalf("fitted = %q, want the first two lines starting at scroll 0", fitted)
	}
	if indicator == "" {
		t.Fatalf("indicator = %q, want a non-empty indicator when rows are hidden", indicator)
	}
	if !strings.Contains(indicator, "1-2") || !strings.Contains(indicator, "5") {
		t.Fatalf("indicator = %q, want it to report the visible range and total (1-2 of 5)", indicator)
	}
}

func TestViewportFitLinesClampsScrollWithinBounds(t *testing.T) {
	t.Parallel()

	inner := strings.Join([]string{"a", "b", "c", "d", "e"}, "\n")

	fitted, indicator := fitLines(inner, 3, 100) // scroll far beyond the available content

	gotLines := strings.Split(fitted, "\n")
	if len(gotLines) != 2 {
		t.Fatalf("fitted lines = %d, want 2 at a clamped scroll", len(gotLines))
	}
	if gotLines[len(gotLines)-1] != "e" {
		t.Fatalf("fitted = %q, want scroll clamped so the last line is visible", fitted)
	}
	if !strings.Contains(indicator, "4-5") {
		t.Fatalf("indicator = %q, want the clamped range 4-5 of 5", indicator)
	}
}

func TestViewportFitLinesOmitsIndicatorWhenAllRowsFit(t *testing.T) {
	t.Parallel()

	inner := strings.Join([]string{"only", "two"}, "\n")

	fitted, indicator := fitLines(inner, 5, 0)

	if fitted != inner {
		t.Fatalf("fitted = %q, want the content unchanged when it fits the budget", fitted)
	}
	if indicator != "" {
		t.Fatalf("indicator = %q, want an empty indicator when no rows are hidden", indicator)
	}
}

func TestViewportRenderSectionWrapsFittedContentInThemedSection(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	inner := strings.Join([]string{"row1", "row2", "row3", "row4"}, "\n")
	layout := consoleLayout{Width: defaultViewportWidth, Height: defaultViewportHeight, SectionRows: 2}

	got := renderSection(theme, inner, layout)

	if !strings.Contains(got, "row1") {
		t.Fatalf("rendered section = %q, want the visible row within the 2-row budget (1 row content + 1 row indicator)", got)
	}
	if strings.Contains(got, "row2") || strings.Contains(got, "row4") {
		t.Fatalf("rendered section = %q, want row2 and row4 clipped out by the 2-row budget", got)
	}
	if !strings.Contains(got, "of 4") {
		t.Fatalf("rendered section = %q, want a position indicator reporting the total row count", got)
	}
}

// TestViewportChromeInvariantTitleContextHelpAreSingleLine guards
// contentBudget's assumption (design.md decision #2 / risk #3) that title,
// context, and help chrome are each exactly one rendered row. If any screen
// starts producing multi-line chrome, contentBudget's fixed "chrome := 2"
// (plus the conditional +1 for help) would silently under-count, and a
// screen would overflow its terminal viewport.
func TestViewportChromeInvariantTitleContextHelpAreSingleLine(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	assertSingleLine := func(t *testing.T, label, rendered string) {
		t.Helper()
		if got := lipgloss.Height(rendered); got != 1 {
			t.Fatalf("%s height = %d, want exactly 1 (chrome invariant)", label, got)
		}
	}

	// Inspection workspace screens: title/context are inlined literal or
	// fmt.Sprintf-composed strings in Model.View(); these mirror those exact
	// shapes to guard the same invariant contentBudget relies on.
	inspectionTitles := []string{
		"Loading",
		"Repositories",
		"Repositories / library/alpine / Tags",
		"Repositories / library/alpine / sha256:abcdef123456 / Manifest",
		"Repositories / library/alpine / latest / Blobs",
		"Repositories / library/alpine / latest / Uploads",
		"Empty",
		"Error",
		"Sign In",
		"Regixtry",
		"Regixtry Admin",
	}
	for _, title := range inspectionTitles {
		assertSingleLine(t, "title "+title, theme.title.Render(title))
	}

	inspectionContexts := []string{
		"",
		"library/alpine",
		"Delete unavailable in v1: some reason",
		"a repository manifest could not be resolved: connection refused",
	}
	for _, context := range inspectionContexts {
		assertSingleLine(t, "context "+context, theme.context.Render(context))
	}

	inspectionHelp := []string{
		"q: quit",
		"Enter: open tags | Tab: admin | q: quit",
		"Enter: inspect manifest | Tab: admin | Esc: back | q: quit",
		"b: blobs | u: uploads | d: unsupported delete | Tab: admin | Esc: back | q: quit",
		"Tab: admin | Esc: back | q: quit",
		"Enter: sign in | Tab: switch field | Esc: back | q: quit",
	}
	for _, help := range inspectionHelp {
		assertSingleLine(t, "help "+help, theme.help.Render(help))
	}

	// Admin workspace screens: context/help come straight from the real
	// production function, not a copy, so this also guards against drift.
	adminScreens := []screen{
		screenAdminUsers,
		screenAdminFeatures,
		screenAdminCreateUser,
		screenAdminEditUser,
		screenAdminChangePassword,
		screenAdminEditUserGrants,
		screenAdminAddGrant,
		screenAdminEditUserTokens,
		screenAdminCreateToken,
	}
	view := newAdminViewState()
	view.SelectedUserID = "user-1"
	view.SelectedUsername = "alice"
	layout := contentBudget(defaultViewportWidth, defaultViewportHeight, "", "")
	for _, current := range adminScreens {
		context, _, help := renderAdminScreen(theme, current, AdminSession{}, view, nil, layout, time.Time{})
		assertSingleLine(t, string(current)+" context", theme.context.Render(context))
		if strings.TrimSpace(help) != "" {
			assertSingleLine(t, string(current)+" help", theme.help.Render(help))
		}
	}
	assertSingleLine(t, "admin title", theme.title.Render("Regixtry Admin"))
}
