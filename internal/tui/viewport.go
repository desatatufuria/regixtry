package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Terminal viewport sizing. contentBudget() computes how many rows are left
// for a screen's bounded body content after accounting for chrome (title,
// context, optional status, optional help, and the body section's own
// border+padding). Chrome that varies in height (like the bordered status
// panel) is measured with lipgloss.Height on its actual rendered form rather
// than assumed — see design.md decision #2. Chrome that is guaranteed to be
// exactly one row (title, context, help) is guarded by
// TestViewportChromeInvariantTitleContextHelpAreSingleLine.
//
// contentBudget/fitLines/renderSection are package-level functions here
// because Model does not yet carry a viewport field (added in Phase 2).
// Phase 2 wires a Model.contentBudget() method around this pure logic.
const (
	// minViewportWidth/minViewportHeight are the floor terminal size the TUI
	// guarantees correct rendering at (View() shows terminalTooSmallTemplate
	// below these). minViewportWidth was raised from the historical 90
	// alongside sectionWidth()'s fix for the hardcoded theme.section
	// Width(88): 90 columns was already narrower than the Repository Alerts
	// summary table's own natural rendered width (measured 99 before this
	// change, wider still after widening its columns for breathing room), so
	// even a fully responsive section width could not have honored the
	// contract at the old floor without truncating/wrapping table borders.
	//
	// Raised a second time, from 132 to 150, for the scan history modal's
	// executions side panel (adminScanHistoryModalExecutionsColumnStyle):
	// measured against the modal's own worst case (Leaks tab, the 4-column
	// secret findings table at 99 columns wide, plus the executions rail's
	// ~19 columns, plus the modal's own border+padding overhead) the
	// composited modal reaches ~122 columns. At the old 132 floor that left
	// only ~5 columns of margin around the overlay -- not corrupted, but
	// tight enough that base content near the terminal's edges (e.g. the
	// "Operator: <name>" status line, TestRenderAdminWorkspace*MarginAround*)
	// could end up partially covered by the overlay's own footprint at
	// realistic viewport widths. 150 restores a comfortable real margin.
	minViewportWidth, minViewportHeight = 150, 24
	// defaultViewportWidth/defaultViewportHeight are the initial size used
	// before the first tea.WindowSizeMsg arrives. Kept the same ~18-20
	// column gap above the floor as before the floor was raised, so tables
	// and rows still have real breathing room out of the box.
	defaultViewportWidth, defaultViewportHeight = 170, 44

	// tableChromeRows is the fixed row overhead a bubble-table adds around
	// its rows: border top/bottom + header + separator + footer.
	// Verified against evertras/bubble-table@v0.19.2: height == pageSize+6.
	tableChromeRows = 6

	// sectionChromeRows is the fixed row overhead of theme.section: a
	// RoundedBorder (2 rows) plus Padding(1) (2 rows).
	sectionChromeRows = 4

	// sectionHorizontalOverhead is theme.section's total non-content-area
	// horizontal overhead once wrapped by theme.app: theme.section's own
	// RoundedBorder (2 columns; lipgloss.Style.Width already accounts for
	// Padding internally, see sectionWidth) plus theme.app's outer
	// Padding(0, 1) (2 columns).
	sectionHorizontalOverhead = 4

	// minSectionContentWidth floors sectionWidth() so a narrow
	// consoleLayout.Width passed directly in a unit test never produces a
	// degenerate or negative lipgloss.Style.Width value.
	minSectionContentWidth = 40

	minTableRows     = 3
	compactTableRows = 5
)

// sectionWidth derives theme.section's content width (the value passed to
// lipgloss.Style.Width, which already includes the section's own Padding)
// from the real terminal width carried by consoleLayout, replacing the
// historical hardcoded Width(88) (tui-table-viewport-fixed-size proposal's
// deferred Q5, "theme.section's hardcoded Width(88): deferred, out of scope
// for this change"). This is what makes the section box actually match the
// terminal instead of a fixed width narrower than the tables it wraps.
func sectionWidth(l consoleLayout) int {
	w := l.Width - sectionHorizontalOverhead
	if w < minSectionContentWidth {
		w = minSectionContentWidth
	}
	return w
}

// consoleLayout carries the computed row/column budget for the current
// screen, derived once per terminal size from contentBudget().
type consoleLayout struct {
	Width       int
	Height      int
	SectionRows int
	Scroll      int
	Primary     int
	Compact     int
}

// contentBudget computes the row budget available to a screen's bounded body
// section, given the terminal width/height and the status/help chrome that
// will be rendered around it. status/help are measured via lipgloss.Height
// on their real rendered form rather than assumed to be a fixed size, since
// the status panel is a bare, unboxed 1-row line when present (design.md
// Decision 4 -- de-boxed from its historical 6-row bordered section) and 0
// rows when absent (design.md decision #2).
func contentBudget(width, height int, status, help string) consoleLayout {
	theme := newAdminTheme()

	// title + context are guarded to always be exactly one row each by
	// TestViewportChromeInvariantTitleContextHelpAreSingleLine.
	chrome := 2
	if strings.TrimSpace(status) != "" {
		// statusKindNeutral: kind only sets Foreground/Bold, never
		// lipgloss.Height, so this measurement stays exact regardless of
		// which kind the real render will end up using (design.md Decision
		// 2 — contentBudget's own signature is unchanged).
		chrome += lipgloss.Height(renderAdminStatus(theme, status, statusKindNeutral))
	}
	if strings.TrimSpace(help) != "" {
		chrome += lipgloss.Height(theme.help.Render(help))
	}
	chrome += sectionChromeRows

	sectionRows := height - chrome
	if sectionRows < minTableRows {
		sectionRows = minTableRows
	}

	return consoleLayout{
		Width:       width,
		Height:      height,
		SectionRows: sectionRows,
	}
}

// fitLines slices inner (a "\n"-joined block of rows) down to budget rows
// starting at scroll, clamping scroll to a valid range. When every line
// already fits within budget, fitted is the content unchanged and indicator
// is empty ("MUST NOT imply hidden content that does not exist"). Otherwise
// one row of the budget is reserved for indicator, which reports the visible
// range and total row count.
func fitLines(inner string, budget, scroll int) (fitted, indicator string) {
	if budget < 1 {
		budget = 1
	}

	lines := strings.Split(inner, "\n")
	total := len(lines)
	if total <= budget {
		return inner, ""
	}

	visible := budget - 1
	if visible < 1 {
		visible = 1
	}

	maxScroll := total - visible
	if scroll < 0 {
		scroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}

	end := scroll + visible
	if end > total {
		end = total
	}

	fitted = strings.Join(lines[scroll:end], "\n")
	indicator = fmt.Sprintf("Showing %d-%d of %d", scroll+1, end, total)
	return fitted, indicator
}

// renderSection fits inner within l.SectionRows (appending a position
// indicator when rows are hidden), then wraps it in the themed bordered
// section shared by every screen.
func renderSection(theme adminTheme, inner string, l consoleLayout) string {
	fitted, indicator := fitLines(inner, l.SectionRows, l.Scroll)
	content := fitted
	if indicator != "" {
		content = fitted + "\n" + theme.muted.Render(indicator)
	}
	return theme.section.Width(sectionWidth(l)).Render(content)
}
