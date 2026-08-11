package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
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

	theme := newAdminTheme()
	columns := []bubbletable.Column{bubbletable.NewColumn("name", "Name", 10)}

	for _, pageSize := range []int{3, 5, 10} {
		pageSize := pageSize
		t.Run(fmt.Sprintf("pageSize=%d", pageSize), func(t *testing.T) {
			t.Parallel()

			rows := make([]bubbletable.Row, 0, pageSize*2)
			for i := 0; i < pageSize*2; i++ {
				rows = append(rows, bubbletable.NewRow(bubbletable.RowData{"name": fmt.Sprintf("row-%d", i)}))
			}

			table := newAdminBubbleTable(columns, rows, 0, theme, pageSize)
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

	theme := newAdminTheme()
	columns := []bubbletable.Column{bubbletable.NewColumn("name", "Name", 10)}
	rows := []bubbletable.Row{bubbletable.NewRow(bubbletable.RowData{"name": "alpha"})}

	table := newAdminBubbleTable(columns, rows, 0, theme, minTableRows)
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

// TestRebuildAdminTablesBakesPrimaryAndCompactPageSizeIntoTables integrates
// tableRoles into rebuildAdminTables: Features/ScanRuns (operator-navigable
// primary lists) get the primary pageSize, FeatureRows/Findings/
// SecretFindings (secondary detail tables) get the compact pageSize.
func TestRebuildAdminTablesBakesPrimaryAndCompactPageSizeIntoTables(t *testing.T) {
	t.Parallel()

	model := NewModel(&fakeQueryService{})
	model.viewport = viewportSize{Width: defaultViewportWidth, Height: defaultViewportHeight}

	rowCount := 40
	model.adminView.Features = make([]ports.FeatureSummary, rowCount)
	model.adminView.TrivyScanRuns = make([]ports.ScanRun, rowCount)
	model.adminView.TrivyScanRunDetail.Findings = make([]ports.ScanRunFinding, rowCount)
	model.adminView.SecretFindings = make([]ports.SecretFinding, rowCount)
	for i := 0; i < rowCount; i++ {
		model.adminView.Features[i] = ports.FeatureSummary{Name: fmt.Sprintf("feature-%d", i)}
		model.adminView.TrivyScanRuns[i] = ports.ScanRun{ID: fmt.Sprintf("run-%d", i)}
		model.adminView.TrivyScanRunDetail.Findings[i] = ports.ScanRunFinding{VulnerabilityID: fmt.Sprintf("CVE-%d", i)}
		model.adminView.SecretFindings[i] = ports.SecretFinding{RuleID: fmt.Sprintf("rule-%d", i)}
	}
	model.adminView.FeaturePage = ports.FeaturePage{
		Sections: []ports.FeatureSection{{ID: "rows-section", Kind: "rows", Rows: make([]ports.FeatureRow, rowCount)}},
	}

	layout := model.adminTablesLayout()
	model.rebuildAdminTables(layout)

	wantPrimary, wantCompact := tableRoles(layout)
	if got, want := model.adminView.Layout.Primary, wantPrimary; got != want {
		t.Fatalf("adminView.Layout.Primary = %d, want %d", got, want)
	}
	if got, want := model.adminView.Layout.Compact, wantCompact; got != want {
		t.Fatalf("adminView.Layout.Compact = %d, want %d", got, want)
	}

	assertTableHeight := func(t *testing.T, label string, table bubbletable.Model, wantPageSize int) {
		t.Helper()
		got := lipgloss.Height(table.View())
		want := wantPageSize + tableChromeRows
		if got != want {
			t.Errorf("%s height = %d, want pageSize(%d)+tableChromeRows(%d) = %d", label, got, wantPageSize, tableChromeRows, want)
		}
	}

	assertTableHeight(t, "Features (primary)", model.adminView.Tables.Features, wantPrimary)
	assertTableHeight(t, "ScanRuns (primary)", model.adminView.Tables.ScanRuns, wantPrimary)
	assertTableHeight(t, "Findings (compact)", model.adminView.Tables.Findings, wantCompact)
	assertTableHeight(t, "SecretFindings (compact)", model.adminView.Tables.SecretFindings, wantCompact)
	rowsTable, ok := model.adminView.Tables.FeatureRows["rows-section"]
	if !ok {
		t.Fatalf("FeatureRows[%q] missing", "rows-section")
	}
	assertTableHeight(t, "FeatureRows (compact)", rowsTable, wantCompact)
}
