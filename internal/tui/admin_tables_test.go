package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	bubbletable "github.com/evertras/bubble-table/table"
	"regixtry/internal/ports"
)

// TestNewAdminBubbleTableHeightMatchesPageSizePlusChrome is the Phase 4 task
// 4.1 RED test: newAdminBubbleTable's rendered height must be exactly
// pageSize+tableChromeRows once a page is filled (viewport.go's
// tableChromeRows constant, verified against
// evertras/bubble-table@v0.19.2 — border top/bottom + header + separator +
// footer). This is the identity rebuildAdminTables' primary/compact split
// relies on to size tables from a consoleLayout row budget.
func TestNewAdminBubbleTableHeightMatchesPageSizePlusChrome(t *testing.T) {
	t.Parallel()

	columns := []bubbletable.Column{bubbletable.NewColumn("name", "Name", 10)}

	for _, pageSize := range []int{3, 5, 10} {
		pageSize := pageSize
		t.Run(fmt.Sprintf("pageSize=%d", pageSize), func(t *testing.T) {
			t.Parallel()

			rows := make([]bubbletable.Row, 0, pageSize*2)
			for i := 0; i < pageSize*2; i++ {
				rows = append(rows, bubbletable.NewRow(bubbletable.RowData{"name": fmt.Sprintf("row-%d", i)}))
			}

			table := newAdminBubbleTable(columns, rows, 0, pageSize)
			got := lipgloss.Height(table.View())
			want := pageSize + tableChromeRows
			if got != want {
				t.Fatalf("table height = %d, want pageSize(%d)+tableChromeRows(%d) = %d", got, pageSize, tableChromeRows, want)
			}
		})
	}
}

// TestNewAdminBubbleTableUsesThemeBorderColorAndVisibleFooter guards
// spec.md "Consistent Table Theme Styling": border color from the theme and
// a visible footer showing page position.
func TestNewAdminBubbleTableUsesThemeBorderColorAndVisibleFooter(t *testing.T) {
	t.Parallel()

	columns := []bubbletable.Column{bubbletable.NewColumn("name", "Name", 10)}
	rows := []bubbletable.Row{bubbletable.NewRow(bubbletable.RowData{"name": "alpha"})}

	table := newAdminBubbleTable(columns, rows, 0, minTableRows)
	if !strings.Contains(table.View(), "1/1") {
		t.Fatalf("table view = %q, want a visible footer with page position", table.View())
	}
}

// TestTableRolesAssignsPrimaryAdaptiveAndCompactFloorAtThree is the Phase 4
// task 4.4 RED test: design.md decision #6 — primary tables get the whole
// per-table budget available (adaptive), compact tables are capped at
// compactTableRows(5) and floored at minTableRows(3) when the budget shrinks
// below that.
func TestTableRolesAssignsPrimaryAdaptiveAndCompactFloorAtThree(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		sectionRows int
		wantPrimary int
		wantCompact int
	}{
		{name: "generous budget: primary adaptive, compact capped at 5", sectionRows: 40, wantPrimary: 34, wantCompact: 5},
		{name: "tight budget: compact floors at 3", sectionRows: 7, wantPrimary: minTableRows, wantCompact: minTableRows},
		{name: "budget below minimum: both floor at 3", sectionRows: 2, wantPrimary: minTableRows, wantCompact: minTableRows},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			layout := consoleLayout{SectionRows: tc.sectionRows}
			primary, compact := tableRoles(layout)
			if primary != tc.wantPrimary {
				t.Errorf("primary = %d, want %d", primary, tc.wantPrimary)
			}
			if compact != tc.wantCompact {
				t.Errorf("compact = %d, want %d", compact, tc.wantCompact)
			}
		})
	}
}

// TestNewAdminBubbleTableShowsPositionIndicatorWhenRowsExceedPageSize is the
// Phase 5 task 5.1 RED test (spec.md "Visible Position Indicator for Hidden
// Rows", "Table shows position when rows are hidden"): a 47-row table with a
// pageSize below 47 must show a position indicator reflecting the visible
// range and total row count.
//
// Deviation from tasks.md's file suggestion (model_test.go): this asserts
// directly on table.View() rather than a full Model screen render, matching
// design.md's Testing Strategy table ("Table height identity...; border
// color and paged footer present | Assert on table.View()", Unit layer).
// Routing through a full admin screen would confound this table-local
// assertion with the outer renderSection clip, which is orthogonal to what
// this test verifies (same narrow-scope precedent as Phase 3/4's documented
// file-location deviations).
func TestNewAdminBubbleTableShowsPositionIndicatorWhenRowsExceedPageSize(t *testing.T) {
	t.Parallel()

	columns := []bubbletable.Column{bubbletable.NewColumn("name", "Name", 10)}
	const totalRows = 47
	rows := make([]bubbletable.Row, 0, totalRows)
	for i := 0; i < totalRows; i++ {
		rows = append(rows, bubbletable.NewRow(bubbletable.RowData{"name": fmt.Sprintf("row-%d", i)}))
	}

	const pageSize = 3
	table := newAdminBubbleTable(columns, rows, 0, pageSize)

	wantMaxPages := (totalRows-1)/pageSize + 1
	wantIndicator := fmt.Sprintf("%d/%d", 1, wantMaxPages)
	view := table.View()
	if !strings.Contains(view, wantIndicator) {
		t.Fatalf("table view = %q, want position indicator %q reflecting visible range/total of %d rows", view, wantIndicator, totalRows)
	}
}

// TestNewAdminBubbleTableShowsNoMisleadingIndicatorWhenAllRowsFit is the
// Phase 5 task 5.2 RED test (spec.md "No indicator when all rows are
// visible"): when a table's row count fits entirely within its pageSize, the
// footer MUST NOT imply hidden content that does not exist.
func TestNewAdminBubbleTableShowsNoMisleadingIndicatorWhenAllRowsFit(t *testing.T) {
	t.Parallel()

	columns := []bubbletable.Column{bubbletable.NewColumn("name", "Name", 10)}
	rows := []bubbletable.Row{
		bubbletable.NewRow(bubbletable.RowData{"name": "alpha"}),
		bubbletable.NewRow(bubbletable.RowData{"name": "beta"}),
		bubbletable.NewRow(bubbletable.RowData{"name": "gamma"}),
	}

	table := newAdminBubbleTable(columns, rows, 0, 10) // pageSize(10) > len(rows)(3)

	view := table.View()
	if !strings.Contains(view, "1/1") {
		t.Fatalf("table view = %q, want a non-misleading full-count indicator (1/1) when all rows fit", view)
	}
	if strings.Contains(view, "2/") {
		t.Fatalf("table view = %q, want no indicator implying a second page when all %d rows fit within pageSize", view, len(rows))
	}
}

// TestNewAdminBubbleTableAppliesThemeBorderForegroundColor is the Phase 5
// task 5.4 RED test (spec.md "Consistent Table Theme Styling", "Table
// renders with themed border and footer" — the border-color half; the
// footer-visible half is already guarded by
// TestNewAdminBubbleTableUsesThemeBorderColorAndVisibleFooter above).
//
// bubble-table's own v2-lipgloss rendering (charm.land/lipgloss/v2, since
// evertras/bubble-table v0.22.3) emits TrueColor ANSI by default in this
// non-TTY test environment with no profile forcing needed -- unlike v1
// lipgloss, whose color profile this test used to force explicitly. The
// expected sequence is derived by rendering paletteBorderHex through the exact
// same v2 style constructor the table itself uses (bubbleTableBaseStyle),
// not by independently recomputing it via v1's termenv.RGBColor: that path
// has its own rounding difference from a direct hex-to-decimal conversion
// (found live: termenv computed blue=105 for #4C566A, whose direct hex
// value is 106 -- a mismatch that only ever coincidentally cancelled out
// because both this test and the table's own (pre-migration) rendering
// went through that exact same v1 termenv path).
func TestNewAdminBubbleTableAppliesThemeBorderForegroundColor(t *testing.T) {
	t.Parallel()

	columns := []bubbletable.Column{bubbletable.NewColumn("name", "Name", 10)}
	rows := []bubbletable.Row{bubbletable.NewRow(bubbletable.RowData{"name": "alpha"})}

	table := newAdminBubbleTable(columns, rows, 0, minTableRows)

	rendered := lipgloss.NewStyle().Foreground(lipgloss.Color(paletteBorderHex)).Render("X")
	wantPrefix, _, found := strings.Cut(rendered, "X")
	if !found {
		t.Fatalf("v2 style rendering of paletteBorderHex = %q, want an ANSI prefix before the literal %q", rendered, "X")
	}
	if !strings.Contains(table.View(), wantPrefix) {
		t.Fatalf("table view does not contain the border color's ANSI sequence %q derived from paletteBorderHex — border MUST use paletteBorderHex", wantPrefix)
	}
}

// TestAdminFindingLinkResolution is the RED test for the shared link
// resolver (adminFindingLink), the single resolution both
// openSelectedAdminFindingLink (model.go, the Enter-key opener) uses:
// PrimaryURL first, else the constructed NVD URL from VulnerabilityID, and
// "" whenever nothing usable resolves -- including when the resolved value
// isn't a valid http(s) URL (isHTTPURL), the same defense-in-depth guard
// openURLInBrowser applies.
func TestAdminFindingLinkResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		finding ports.ScanRunFinding
		want    string
	}{
		{"PrimaryURL wins", ports.ScanRunFinding{VulnerabilityID: "CVE-1", PrimaryURL: "https://example.com/1"}, "https://example.com/1"},
		{"falls back to NVD", ports.ScanRunFinding{VulnerabilityID: "CVE-2"}, nvdVulnerabilityURL("CVE-2")},
		{"nothing resolves", ports.ScanRunFinding{}, ""},
		{"non-http PrimaryURL rejected", ports.ScanRunFinding{VulnerabilityID: "CVE-3", PrimaryURL: "javascript:alert(1)"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := adminFindingLink(tc.finding); got != tc.want {
				t.Fatalf("adminFindingLink(%+v) = %q, want %q", tc.finding, got, tc.want)
			}
		})
	}
}
