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
	minViewportWidth, minViewportHeight         = 90, 24
	defaultViewportWidth, defaultViewportHeight = 100, 40

	// tableChromeRows is the fixed row overhead a bubble-table adds around
	// its rows: border top/bottom + header + separator + footer.
	// Verified against evertras/bubble-table@v0.19.2: height == pageSize+6.
	tableChromeRows = 6

	// sectionChromeRows is the fixed row overhead of theme.section: a
	// RoundedBorder (2 rows) plus Padding(1) (2 rows).
	sectionChromeRows = 4

	minTableRows     = 3
	compactTableRows = 5
)

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
// the status panel is a 6-row bordered section when present and 0 rows when
// absent (design.md decision #2).
func contentBudget(width, height int, status, help string) consoleLayout {
	theme := newAdminTheme()

	// title + context are guarded to always be exactly one row each by
	// TestViewportChromeInvariantTitleContextHelpAreSingleLine.
	chrome := 2
	if strings.TrimSpace(status) != "" {
		chrome += lipgloss.Height(renderAdminStatus(theme, status))
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
	return theme.section.Render(content)
}
