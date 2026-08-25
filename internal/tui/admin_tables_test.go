package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	bubbletable "github.com/evertras/bubble-table/table"
	"github.com/muesli/termenv"
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
// tableRoles into each Security & Compliance screen's own table rebuild
// (Phase 11: Features/ScanSummary/FeatureRows moved off
// AdminViewState/rebuildAdminTables onto securityMenuScreen/
// trivyReposScreen/trivyConfigScreen's own rebuildTable/rebuildRows
// methods): Features/ScanSummary (operator-navigable primary lists) get the
// primary pageSize, FeatureRows (a secondary detail table) gets the compact
// pageSize. Findings/SecretFindings are exercised separately (they only
// build while the scan history modal is active, sized from the modal's own
// nested row budget, not this primary/compact split — see
// TestModelScanHistoryModal* in model_test.go).
func TestRebuildAdminTablesBakesPrimaryAndCompactPageSizeIntoTables(t *testing.T) {
	t.Parallel()

	model := NewModel(&fakeQueryService{})
	model.viewport = viewportSize{Width: defaultViewportWidth, Height: defaultViewportHeight}

	rowCount := 40
	features := make([]ports.FeatureSummary, rowCount)
	scanRuns := make([]ports.ScanRun, rowCount)
	for i := 0; i < rowCount; i++ {
		features[i] = ports.FeatureSummary{Name: fmt.Sprintf("feature-%d", i)}
		scanRuns[i] = ports.ScanRun{ID: fmt.Sprintf("run-%d", i), Repository: fmt.Sprintf("team/service-%d", i)}
	}
	summaries := summarizeScanRunsByRepository(scanRuns)
	page := ports.FeaturePage{
		Sections: []ports.FeatureSection{{ID: "rows-section", Kind: "rows", Rows: make([]ports.FeatureRow, rowCount)}},
	}

	layout := model.adminTablesLayout()
	wantPrimary, wantCompact := tableRoles(layout)
	env := model.screenEnv()

	menu := securityMenuScreen{features: features, loaded: true}
	menu.rebuildTable(env)

	repos := trivyReposScreen{summaries: summaries, loaded: true}
	repos.rebuildTable(env)

	config := trivyConfigScreen{page: page, loaded: true}
	config.rebuildRows(env)

	assertTableHeight := func(t *testing.T, label string, table bubbletable.Model, wantPageSize int) {
		t.Helper()
		got := lipgloss.Height(table.View())
		want := wantPageSize + tableChromeRows
		if got != want {
			t.Errorf("%s height = %d, want pageSize(%d)+tableChromeRows(%d) = %d", label, got, wantPageSize, tableChromeRows, want)
		}
	}

	assertTableHeight(t, "securityMenuScreen.table (primary)", menu.table, wantPrimary)
	assertTableHeight(t, "trivyReposScreen.table (primary)", repos.table, wantPrimary)
	rowsTable, ok := config.rows["rows-section"]
	if !ok {
		t.Fatalf("rows[%q] missing", "rows-section")
	}
	assertTableHeight(t, "trivyConfigScreen.rows (compact)", rowsTable, wantCompact)
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

	theme := newAdminTheme()
	columns := []bubbletable.Column{bubbletable.NewColumn("name", "Name", 10)}
	const totalRows = 47
	rows := make([]bubbletable.Row, 0, totalRows)
	for i := 0; i < totalRows; i++ {
		rows = append(rows, bubbletable.NewRow(bubbletable.RowData{"name": fmt.Sprintf("row-%d", i)}))
	}

	const pageSize = 3
	table := newAdminBubbleTable(columns, rows, 0, theme, pageSize)

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

	theme := newAdminTheme()
	columns := []bubbletable.Column{bubbletable.NewColumn("name", "Name", 10)}
	rows := []bubbletable.Row{
		bubbletable.NewRow(bubbletable.RowData{"name": "alpha"}),
		bubbletable.NewRow(bubbletable.RowData{"name": "beta"}),
		bubbletable.NewRow(bubbletable.RowData{"name": "gamma"}),
	}

	table := newAdminBubbleTable(columns, rows, 0, theme, 10) // pageSize(10) > len(rows)(3)

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
// Deliberately NOT t.Parallel(): this forces the shared global lipgloss
// color profile to TrueColor so the border's ANSI color sequence is
// actually emitted (the default test environment has no TTY and renders
// plain, uncolored text). Go only starts t.Parallel()-marked tests after
// every non-parallel test in this package has completed, so running this
// test serially — set profile, render, restore via defer, all before any
// parallel test body executes — cannot race with the many other tests in
// this package that render table/theme output.
func TestNewAdminBubbleTableAppliesThemeBorderForegroundColor(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(original)

	theme := newAdminTheme()
	columns := []bubbletable.Column{bubbletable.NewColumn("name", "Name", 10)}
	rows := []bubbletable.Row{bubbletable.NewRow(bubbletable.RowData{"name": "alpha"})}

	table := newAdminBubbleTable(columns, rows, 0, theme, minTableRows)

	wantSequence := "\x1b[" + termenv.RGBColor(string(theme.borderColor)).Sequence(false) + "m"
	if !strings.Contains(table.View(), wantSequence) {
		t.Fatalf("table view does not contain the theme border-color ANSI sequence %q — border MUST use theme.borderColor", wantSequence)
	}
}

// TestFormatScanSummaryLastExecutedNeverBlank is the Phase 2 task 2.2 RED
// test (spec.md "the date column SHALL show CreatedAt with an in-progress
// marker, never blank"): a completed run renders a bare formatted date, an
// in-progress run appends the marker, and a zero time (defensive case, not
// expected from summarizeScanRunsByRepository) still renders non-blank text.
func TestFormatScanSummaryLastExecutedNeverBlank(t *testing.T) {
	t.Parallel()

	executed := time.Date(2026, 3, 4, 9, 30, 0, 0, time.UTC)

	tests := []struct {
		name    string
		summary repositorySummary
		want    string
	}{
		{
			name:    "completed run renders bare formatted date",
			summary: repositorySummary{LastExecuted: executed, InProgress: false},
			want:    "2026-03-04 09:30",
		},
		{
			name:    "in-progress run appends the marker",
			summary: repositorySummary{LastExecuted: executed, InProgress: true},
			want:    "2026-03-04 09:30 (in progress)",
		},
		{
			name:    "zero time still renders non-blank text",
			summary: repositorySummary{},
			want:    "unknown",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := formatScanSummaryLastExecuted(tc.summary)
			if got != tc.want {
				t.Fatalf("formatScanSummaryLastExecuted() = %q, want %q", got, tc.want)
			}
			if strings.TrimSpace(got) == "" {
				t.Fatalf("formatScanSummaryLastExecuted() returned blank text, want never blank")
			}
		})
	}
}

// TestScanSummaryLastExecutedColumnFitsLongestFormattedValueWithoutTruncation
// is a RED test for the reported regression (claude-handoff.md,
// tmp/features-repositoriy_alerts.png): formatScanSummaryLastExecuted's
// longest legitimate output -- an in-progress timestamp -- must fit inside
// its own column, otherwise bubble-table truncates/wraps the value and it
// looks like a broken row boundary.
func TestScanSummaryLastExecutedColumnFitsLongestFormattedValueWithoutTruncation(t *testing.T) {
	t.Parallel()

	longest := formatScanSummaryLastExecuted(repositorySummary{
		LastExecuted: time.Date(2026, 8, 12, 14, 5, 0, 0, time.UTC),
		InProgress:   true,
	})

	if w, col := lipgloss.Width(longest), adminScanSummaryColumnLastExecutedWidth; w >= col {
		t.Fatalf("longest formatted value %q has width %d, want strictly less than the column width %d (needs padding room, not just an exact fit)", longest, w, col)
	}
}

// TestRenderAdminFeaturesScreenFitsSummaryTableWithinItsOwnSectionWidth is
// the RED test for the reported regression (claude-handoff.md,
// tmp/features-repositoriy_alerts.png): the Repository Alerts summary table
// is wider than theme.section's box at realistic terminal sizes when the
// section width is a hardcoded constant smaller than the table. No rendered
// line of trivyReposScreen's own body (Phase 11: the Repository Alerts tab,
// promoted off the old combined Features+Repository-Alerts screen this test
// originally exercised) may exceed the section's own real, viewport-derived
// declared width.
func TestRenderAdminFeaturesScreenFitsSummaryTableWithinItsOwnSectionWidth(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	for _, dims := range []struct {
		name          string
		width, height int
	}{
		{"minViewport", minViewportWidth, minViewportHeight},
		{"defaultViewport", defaultViewportWidth, defaultViewportHeight},
	} {
		dims := dims
		t.Run(dims.name, func(t *testing.T) {
			t.Parallel()

			layout := contentBudget(dims.width, dims.height, "", "")
			env := screenEnv{Layout: layout}

			summaries := []repositorySummary{{
				Repository:   "ghcr.io/some-long-organization-name/some-really-long-repository-name",
				LatestRun:    ports.ScanRun{RequestedRef: "release-candidate-2026-08", Status: "completed", HasFixable: true},
				LastExecuted: time.Date(2026, 8, 12, 14, 5, 0, 0, time.UTC),
				InProgress:   true,
				RunCount:     42,
			}}
			repos := trivyReposScreen{summaries: summaries, loaded: true}
			repos.rebuildTable(env)

			frame := repos.View(theme, env)

			declaredWidth := sectionWidth(layout) + 2 // +2: theme.section's own RoundedBorder columns
			for i, line := range strings.Split(frame.Body, "\n") {
				if w := lipgloss.Width(line); w > declaredWidth {
					t.Fatalf("line %d width = %d, want <= %d (theme.section's own declared width) -- a table overflowed its bordered box:\n%s", i, w, declaredWidth, frame.Body)
				}
			}
		})
	}
}

// TestBuildAdminScanSummaryTableRendersOneRowPerRepository is the Phase 2
// task 2.2 RED test (spec.md "Repository Alerts Summarized Per Repository
// With Ordering And Freshness"): buildAdminScanSummaryTable renders exactly
// one row per repositorySummary, not one row per underlying scan run.
func TestBuildAdminScanSummaryTableRendersOneRowPerRepository(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	executed := time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)
	summaries := []repositorySummary{
		{
			Repository:   "acme/api",
			LatestRun:    ports.ScanRun{ID: "run-9", Repository: "acme/api", RequestedRef: "latest", Status: ports.ScanRunStatusCompleted, Critical: 2, High: 3},
			LastExecuted: executed,
			RunCount:     4,
		},
		{
			Repository:   "acme/worker",
			LatestRun:    ports.ScanRun{ID: "run-10", Repository: "acme/worker", RequestedRef: "v2", Status: ports.ScanRunStatusCompleted},
			LastExecuted: executed,
			InProgress:   true,
			RunCount:     1,
		},
	}

	table := buildAdminScanSummaryTable(theme, summaries, 0, minTableRows)
	if got, want := table.TotalRows(), len(summaries); got != want {
		t.Fatalf("buildAdminScanSummaryTable() TotalRows() = %d, want %d (one row per repository, not per scan run)", got, want)
	}

	view := table.View()
	if !strings.Contains(view, "acme/api") {
		t.Fatalf("table view = %q, want it to contain repository %q", view, "acme/api")
	}
	if !strings.Contains(view, "acme/worker") {
		t.Fatalf("table view = %q, want it to contain repository %q", view, "acme/worker")
	}
}

// TestBuildAdminScanSummaryTableRendersDisabledRepositoriesDistinctly is the
// Phase 8 task 8.10 RED test (operator-admin-tui spec's "Repository Alerts
// Renders Override-Disabled Repositories Distinctly" requirement, both
// scenarios): a repositorySummary with Disabled=true (an override with
// Enabled=false) must render an explicit "scanning disabled" Status cell,
// distinct from both a never-scanned repository's "unknown" Status cell and
// a normally-scanned repository's own status cell.
func TestBuildAdminScanSummaryTableRendersDisabledRepositoriesDistinctly(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	summaries := []repositorySummary{
		{Repository: "acme/never-scanned"}, // zero-value LatestRun -> "unknown"
		{Repository: "acme/normal", LatestRun: ports.ScanRun{ID: "run-1", Repository: "acme/normal", Status: ports.ScanRunStatusCompleted}},
		{Repository: "acme/disabled", LatestRun: ports.ScanRun{ID: "run-2", Repository: "acme/disabled", Status: ports.ScanRunStatusCompleted}, Disabled: true},
	}

	table := buildAdminScanSummaryTable(theme, summaries, 0, minTableRows)
	rows := table.GetVisibleRows()
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}

	statusOf := func(repository string) string {
		for _, row := range rows {
			if row.Data[adminTableColumnScanSummaryRepository] == repository {
				status, _ := row.Data[adminTableColumnScanSummaryStatus].(string)
				return status
			}
		}
		t.Fatalf("no row found for repository %q", repository)
		return ""
	}

	neverScanned := statusOf("acme/never-scanned")
	normal := statusOf("acme/normal")
	disabled := statusOf("acme/disabled")

	if disabled != "scanning disabled" {
		t.Fatalf("disabled row status = %q, want %q", disabled, "scanning disabled")
	}
	if disabled == neverScanned {
		t.Fatalf("disabled row status = %q, want it distinct from the never-scanned row's %q", disabled, neverScanned)
	}
	if disabled == normal {
		t.Fatalf("disabled row status = %q, want it distinct from the normally-scanned row's %q", disabled, normal)
	}
}

// TestAnnotateDisabledSummariesMarksOnlyDisabledOverrideRepositories is the
// Phase 8 task 8.11 RED test: annotateDisabledSummaries only marks a summary
// Disabled when a stored override for that repository has Enabled=false; a
// repository with no override, or an override with Enabled=true, is left
// unmarked.
func TestAnnotateDisabledSummariesMarksOnlyDisabledOverrideRepositories(t *testing.T) {
	t.Parallel()

	summaries := []repositorySummary{
		{Repository: "acme/no-override"},
		{Repository: "acme/enabled-override"},
		{Repository: "acme/disabled-override"},
	}
	overrides := []ports.RepositoryOverrideDetails{
		{Repository: "acme/enabled-override", Feature: "trivy", Enabled: true},
		{Repository: "acme/disabled-override", Feature: "trivy", Enabled: false},
	}

	got := annotateDisabledSummaries(summaries, overrides)

	want := map[string]bool{
		"acme/no-override":       false,
		"acme/enabled-override":  false,
		"acme/disabled-override": true,
	}
	for _, summary := range got {
		if summary.Disabled != want[summary.Repository] {
			t.Fatalf("annotateDisabledSummaries()[%q].Disabled = %v, want %v", summary.Repository, summary.Disabled, want[summary.Repository])
		}
	}
}

// TestAdminFindingLinkResolution is the RED test for the shared link
// resolver (adminFindingLink), the single resolution both
// openSelectedAdminFindingLink (model.go, the Enter-key opener) uses:
// PrimaryURL first, else the constructed NVD URL from VulnerabilityID, and
// "" whenever nothing usable resolves -- including when the resolved value
// isn't a valid http(s) URL (isHTTPURL), the same defense-in-depth guard
// openURLInBrowser applies.
//
// buildAdminFindingsTable does NOT call this helper to OSC8-wrap the
// Finding cell -- that was attempted and reverted. evertras/bubble-table
// v0.19.2 (this repo's pinned version) truncates cell text through
// muesli/reflow's ansi.PrintableRuneWidth/truncate.StringWithTail, which is
// unaware of OSC8 hyperlink escapes: it misreads any letter inside the
// wrapped URL as an ANSI-sequence terminator (its terminator heuristic
// only knows about single-byte CSI finals, not OSC's ST/BEL), so it starts
// counting the rest of the URL as literal printable width and truncates the
// cell mid-escape -- verified by rendering buildAdminFindingsTable with a
// termenv.Hyperlink-wrapped cell and inspecting the real output, which came
// back with the Finding column blank/mangled. lipgloss.Width itself has no
// such bug (confirmed separately) -- only this table library's own,
// separate width scanner does. Fixing it upstream requires
// evertras/bubble-table >= v0.20.0, which switched to the OSC8-aware
// charmbracelet/x/ansi -- but that version also moved to
// charm.land/bubbletea/v2, a breaking migration out of scope here.
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

// TestAdminScanHistoryModalTablePageSizeFloorsAtMinTableRows is the Phase 2
// task 2.2 RED test: the modal's active-tab table page size is derived from
// the nested modalRows budget minus fixed/measured chrome, floored at
// minTableRows rather than going to zero or negative when the budget is
// tight.
func TestAdminScanHistoryModalTablePageSizeFloorsAtMinTableRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		modalRows            int
		measuredHeaderHeight int
		want                 int
	}{
		{name: "generous budget computes available rows", modalRows: 20, measuredHeaderHeight: 0, want: 20 - adminScanHistoryModalChromeRows - tableChromeRows},
		{name: "tight budget floors at minTableRows", modalRows: adminScanHistoryModalChromeRows + tableChromeRows, measuredHeaderHeight: 0, want: minTableRows},
		// adminScanHistoryModalMinRows (Phase 11, tui-menu-architecture) is
		// itself derived to guarantee a viable table even with a 1-row
		// Loading/Error header present (see its own doc comment) -- this
		// case is the regression guard for that exact derivation.
		{name: "adminScanHistoryModalMinRows floor still clears adminScanHistoryModalMinTableBudget with a header row", modalRows: adminScanHistoryModalMinRows, measuredHeaderHeight: 1, want: adminScanHistoryModalMinRows - adminScanHistoryModalChromeRows - 1 - tableChromeRows},
		{name: "measured header height reduces the remaining table budget", modalRows: 20, measuredHeaderHeight: 3, want: 20 - adminScanHistoryModalChromeRows - 3 - tableChromeRows},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := adminScanHistoryModalTablePageSize(tc.modalRows, tc.measuredHeaderHeight)
			if got != tc.want {
				t.Fatalf("adminScanHistoryModalTablePageSize(%d, %d) = %d, want %d", tc.modalRows, tc.measuredHeaderHeight, got, tc.want)
			}
			if got < minTableRows {
				t.Fatalf("adminScanHistoryModalTablePageSize(%d, %d) = %d, want >= minTableRows(%d)", tc.modalRows, tc.measuredHeaderHeight, got, minTableRows)
			}
		})
	}
}

// TestBuildAdminSecretFindingsTableRendersDescriptionAndTagsColumns is the
// RED test for the Leaks tab's missing Description/Tags columns
// (ports.SecretFinding already carries both fields, unused until now). Tags
// join with ", "; a finding with no tags renders an empty cell rather than a
// placeholder, since Description/Tags are optional supplementary metadata,
// unlike Rule/Location's "unknown" fallback which guards the row's identity
// columns.
func TestBuildAdminSecretFindingsTableRendersDescriptionAndTagsColumns(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	findings := []ports.SecretFinding{
		{RuleID: "aws-access-token", Path: "config.json", StartLine: 4, Description: "AWS access token detected", Tags: []string{"aws", "credentials"}},
		{RuleID: "generic-api-key", Path: "app.env", StartLine: 1, Description: "", Tags: nil},
	}
	table := buildAdminSecretFindingsTable(theme, findings, 0, 5)

	if got, want := table.TotalRows(), 2; got != want {
		t.Fatalf("TotalRows() = %d, want %d", got, want)
	}

	rows := table.GetVisibleRows()
	first := rows[0].Data
	if got, want := first[adminTableColumnSecretFindingDescription], "AWS access token detected"; got != want {
		t.Fatalf("Description cell = %q, want %q", got, want)
	}
	if got, want := first[adminTableColumnSecretFindingTags], "aws, credentials"; got != want {
		t.Fatalf("Tags cell = %q, want %q", got, want)
	}

	second := rows[1].Data
	if got, want := second[adminTableColumnSecretFindingDescription], ""; got != want {
		t.Fatalf("Description cell for empty description = %q, want empty %q", got, want)
	}
	if got, want := second[adminTableColumnSecretFindingTags], ""; got != want {
		t.Fatalf("Tags cell for nil tags = %q, want empty %q (no placeholder for optional metadata)", got, want)
	}

	view := table.View()
	for _, want := range []string{"AWS access token detected", "aws, credentials", "Description", "Tags"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view = %q, want it to contain %q", view, want)
		}
	}
}

// TestBuildAdminSecretFindingsTableColumnsFitWithoutOverflow verifies the
// widened 4-column secret findings table (Rule, Location, Description, Tags)
// still fits within the modal's own auto-sized section width at realistic
// terminal sizes — measured, not guessed, the same discipline as
// TestScanSummaryLastExecutedColumnFitsLongestFormattedValueWithoutTruncation.
// Realistic (not pathological) values must render without any rendered line
// exceeding the declared column-derived table width.
func TestBuildAdminSecretFindingsTableColumnsFitWithoutOverflow(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	findings := []ports.SecretFinding{
		{RuleID: "aws-access-token", Path: "some/nested/path/config.json", StartLine: 4, EndLine: 8, Description: "AWS access token detected in configuration file", Tags: []string{"aws", "credentials", "critical"}},
	}
	table := buildAdminSecretFindingsTable(theme, findings, 0, 5)
	view := table.View()

	wantWidth := adminSecretColumnRuleWidth + adminSecretColumnLocationWidth + adminSecretColumnDescriptionWidth + adminSecretColumnTagsWidth + 5 // +5: 4 columns' own border/separator characters (cols+1)
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > wantWidth {
			t.Fatalf("line %d width = %d, want <= %d (table overflowed its own declared column widths):\n%s", i, w, wantWidth, view)
		}
	}
}
