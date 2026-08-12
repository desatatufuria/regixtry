package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"regixtry/internal/ports"
)

// TestRenderAdminScanSummaryShowsEmptyStateThenPopulatedTable is the Phase 2
// task 2.2/2.4 RED test: renderAdminScanSummary shows the same two empty
// states renderTrivyRepositoryAlerts already established (spec.md
// "Repository Alerts Summarized Per Repository With Ordering And
// Freshness"), then the built ScanSummary table once TrivySummaries and the
// table are populated.
func TestRenderAdminScanSummaryShowsEmptyStateThenPopulatedTable(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	view := AdminViewState{}
	got := strings.Join(renderAdminScanSummary(theme, view), "\n")
	if !strings.Contains(got, "Loading repository alerts requires switching into the tab.") {
		t.Fatalf("renderAdminScanSummary() = %q, want the not-yet-loaded message", got)
	}

	view.TrivyAlertsLoaded = true
	got = strings.Join(renderAdminScanSummary(theme, view), "\n")
	if !strings.Contains(got, "No repository alerts found.") {
		t.Fatalf("renderAdminScanSummary() = %q, want the loaded-but-empty message", got)
	}

	view.TrivySummaries = []repositorySummary{{Repository: "acme/api", RunCount: 1}}
	view.Tables.ScanSummary = buildAdminScanSummaryTable(theme, view.TrivySummaries, 0, minTableRows)
	got = strings.Join(renderAdminScanSummary(theme, view), "\n")
	if !strings.Contains(got, "acme/api") {
		t.Fatalf("renderAdminScanSummary() = %q, want the populated ScanSummary table containing %q", got, "acme/api")
	}
}

// TestAdminScanHistoryModalTabBarHighlightsActiveTabAndListsAllTabs is the
// Phase 2 task 2.4 RED test: the tab bar lists every tab in order and
// highlights only the active one (design.md "Ordered tab slice with a
// wrapping cursor").
func TestAdminScanHistoryModalTabBarHighlightsActiveTabAndListsAllTabs(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	modal := adminScanHistoryModal{Tabs: newAdminScanHistoryTabs(), ActiveTab: 1}

	got := adminScanHistoryModalTabBar(theme, modal)
	if !strings.Contains(got, "Vulnerabilities") {
		t.Fatalf("adminScanHistoryModalTabBar() = %q, want it to contain %q", got, "Vulnerabilities")
	}
	if !strings.Contains(got, "Leaks") {
		t.Fatalf("adminScanHistoryModalTabBar() = %q, want it to contain %q", got, "Leaks")
	}

	activeLabel := theme.selected.Render("Leaks")
	if !strings.Contains(got, activeLabel) {
		t.Fatalf("adminScanHistoryModalTabBar() = %q, want the active tab (Leaks, index 1) styled with theme.selected", got)
	}
	inactiveLabel := theme.selected.Render("Vulnerabilities")
	if strings.Contains(got, inactiveLabel) {
		t.Fatalf("adminScanHistoryModalTabBar() = %q, want only the active tab styled with theme.selected", got)
	}
}

// TestAdminScanHistoryModalFooterReportsPositionDateAndInProgressMarker is
// the Phase 2 task 2.4 RED test (design.md "the TUI SHALL show 2/17, the
// run's date"): no runs shows a zero position, and a populated cursor shows
// 1-based position, total count, and the in-progress marker when applicable.
func TestAdminScanHistoryModalFooterReportsPositionDateAndInProgressMarker(t *testing.T) {
	t.Parallel()

	empty := adminScanHistoryModal{}
	if got, want := adminScanHistoryModalFooter(empty), "Execution 0/0"; got != want {
		t.Fatalf("adminScanHistoryModalFooter() = %q, want %q", got, want)
	}

	finished := time.Date(2026, 4, 1, 8, 15, 0, 0, time.UTC)
	created := time.Date(2026, 4, 2, 9, 0, 0, 0, time.UTC)
	modal := adminScanHistoryModal{
		Runs: []ports.ScanRun{
			{ID: "run-1", FinishedAt: &finished, CreatedAt: finished},
			{ID: "run-2", FinishedAt: nil, CreatedAt: created},
			{ID: "run-3", FinishedAt: &finished, CreatedAt: finished},
		},
		Cursor: 1,
	}

	got := adminScanHistoryModalFooter(modal)
	want := fmt.Sprintf("Execution 2/3 — %s (in progress)", created.UTC().Format("2006-01-02 15:04"))
	if got != want {
		t.Fatalf("adminScanHistoryModalFooter() = %q, want %q", got, want)
	}
}

// TestAdminScanHistoryModalTableBodySubstitutesSingleLineWhenBudgetTooSmall
// is the Phase 2 task 2.3/2.4 RED test (design.md "the table is replaced by
// a single-line substitute, never sliced" — the fix for the historical
// orphaned "Showing x-y of N" bug): when tableBudget cannot hold a bordered
// table at all, the modal never returns a bordered table body, it returns
// one substitute line instead.
func TestAdminScanHistoryModalTableBodySubstitutesSingleLineWhenBudgetTooSmall(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	modal := adminScanHistoryModal{
		Tabs:      newAdminScanHistoryTabs(),
		ActiveTab: 0,
		Detail:    ports.ScanRunDetail{Findings: []ports.ScanRunFinding{{VulnerabilityID: "CVE-1"}}},
	}
	view := AdminViewState{}

	got := adminScanHistoryModalTableBody(theme, modal, view, adminScanHistoryModalMinTableBudget-1)
	if lipgloss.Height(got) != 1 {
		t.Fatalf("adminScanHistoryModalTableBody() height = %d, want exactly 1 (single-line substitute, never a sliced bordered table)", lipgloss.Height(got))
	}
	if !strings.Contains(got, "too small") {
		t.Fatalf("adminScanHistoryModalTableBody() = %q, want a message explaining the terminal is too small", got)
	}
}

// TestRenderAdminScanHistoryModalChromeLinesAreSingleLine is the Phase 2
// task 2.4 RED test (design.md "Modal chrome accounting": title, tab bar,
// footer, and help are each guarded single-row), mirroring
// TestViewportChromeInvariantTitleContextHelpAreSingleLine's convention for
// the base screen's chrome.
func TestRenderAdminScanHistoryModalChromeLinesAreSingleLine(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	created := time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)
	modal := adminScanHistoryModal{
		Open:       true,
		Repository: "acme/api",
		Tabs:       newAdminScanHistoryTabs(),
		ActiveTab:  1,
		Runs:       []ports.ScanRun{{ID: "run-1", CreatedAt: created}},
		Cursor:     0,
	}

	assertSingleLine := func(t *testing.T, label, rendered string) {
		t.Helper()
		if got := lipgloss.Height(rendered); got != 1 {
			t.Fatalf("%s height = %d, want exactly 1 (modal chrome invariant)", label, got)
		}
	}

	assertSingleLine(t, "title", theme.subheading.Render(fmt.Sprintf("Scan History — %s", adminFirstNonEmpty(modal.Repository, "unknown"))))
	assertSingleLine(t, "tab bar", adminScanHistoryModalTabBar(theme, modal))
	assertSingleLine(t, "footer", theme.muted.Render(adminScanHistoryModalFooter(modal)))
	assertSingleLine(t, "help", theme.help.Render("Tab/Shift+Tab: tabs | Left/Right: history | Up/Down: select | Enter/click: open | Esc: close"))
}

// TestRenderAdminScanHistoryModalRendersExecutionsColumnWithCursorHighlighted
// is the RED test for the executions side panel (executions side panel
// feature): the modal renders a left-hand executions rail alongside the
// existing right column, listing compact per-run labels with the currently
// navigated run (modal.Cursor) highlighted the same way
// adminScanHistoryModalTabBar highlights the active tab.
func TestRenderAdminScanHistoryModalRendersExecutionsColumnWithCursorHighlighted(t *testing.T) {
	// Not t.Parallel(): forces the global lipgloss color profile so
	// theme.selected actually emits an ANSI sequence to assert on, following
	// TestNewAdminBubbleTableAppliesThemeBorderForegroundColor's precedent
	// (go test runs with no tty, so lipgloss otherwise auto-detects "no
	// color" and theme.selected.Render would differ from plain text only by
	// its Padding(0,1), which is indistinguishable from this modal's own
	// unrelated column padding).
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(original)

	theme := newAdminTheme()
	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	runs := make([]ports.ScanRun, 0, 5)
	for i := 0; i < 5; i++ {
		finished := base.AddDate(0, 0, i)
		runs = append(runs, ports.ScanRun{ID: fmt.Sprintf("run-%d", i), FinishedAt: &finished})
	}
	modal := adminScanHistoryModal{
		Open:       true,
		Repository: "acme/api",
		Tabs:       newAdminScanHistoryTabs(),
		Runs:       runs,
		Cursor:     2,
		Detail:     ports.ScanRunDetail{Findings: []ports.ScanRunFinding{{VulnerabilityID: "CVE-1"}}},
	}
	view := AdminViewState{}
	view.Tables.Findings = buildAdminFindingsTable(theme, modal.Detail.Findings, 0, minTableRows)

	got := renderAdminScanHistoryModal(theme, modal, view, 30)

	if !strings.Contains(got, "Executions") {
		t.Fatalf("renderAdminScanHistoryModal() = %q, want the executions rail heading present", got)
	}
	for i, run := range runs {
		label := adminScanHistoryModalExecutionRowLabel(i, run)
		if !strings.Contains(got, label) {
			t.Fatalf("renderAdminScanHistoryModal() = %q, want it to contain run label %q", got, label)
		}
	}

	cursorLabel := adminScanHistoryModalExecutionRowLabel(modal.Cursor, runs[modal.Cursor])
	highlighted := theme.selected.Render(cursorLabel)
	if !strings.Contains(got, highlighted) {
		t.Fatalf("renderAdminScanHistoryModal() = %q, want the cursor run label %q styled with theme.selected", got, cursorLabel)
	}
	otherLabel := adminScanHistoryModalExecutionRowLabel(0, runs[0])
	otherHighlighted := theme.selected.Render(otherLabel)
	if strings.Contains(got, otherHighlighted) {
		t.Fatalf("renderAdminScanHistoryModal() = %q, want only the cursor run styled with theme.selected", got)
	}
}

// TestRenderAdminScanHistoryModalExecutionsColumnIsAScrollableWindowNotFullList
// is the RED test guarding against unconditionally rendering all
// adminScanHistoryWindowLimit (50) runs in the executions rail: with a tight
// modal row budget and the cursor near the start, a run label far past the
// visible window (near the end of 50) must not appear.
func TestRenderAdminScanHistoryModalExecutionsColumnIsAScrollableWindowNotFullList(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	runs := make([]ports.ScanRun, 0, adminScanHistoryWindowLimit)
	for i := 0; i < adminScanHistoryWindowLimit; i++ {
		finished := base.AddDate(0, 0, i)
		runs = append(runs, ports.ScanRun{ID: fmt.Sprintf("run-%d", i), FinishedAt: &finished})
	}
	modal := adminScanHistoryModal{
		Open:       true,
		Repository: "acme/api",
		Tabs:       newAdminScanHistoryTabs(),
		Runs:       runs,
		Cursor:     0,
		Detail:     ports.ScanRunDetail{Findings: []ports.ScanRunFinding{{VulnerabilityID: "CVE-1"}}},
	}
	view := AdminViewState{}
	view.Tables.Findings = buildAdminFindingsTable(theme, modal.Detail.Findings, 0, minTableRows)

	got := renderAdminScanHistoryModal(theme, modal, view, adminScanHistoryModalMinRows)

	lastLabel := adminScanHistoryModalExecutionRowLabel(adminScanHistoryWindowLimit-1, runs[adminScanHistoryWindowLimit-1])
	if strings.Contains(got, lastLabel) {
		t.Fatalf("renderAdminScanHistoryModal() = %q, want the executions rail to be a bounded scrollable window, not the full %d-run list (last run's label %q should not be visible while cursor is at 0 with a tight row budget)", got, adminScanHistoryWindowLimit, lastLabel)
	}
	firstLabel := adminScanHistoryModalExecutionRowLabel(0, runs[0])
	if !strings.Contains(got, firstLabel) {
		t.Fatalf("renderAdminScanHistoryModal() = %q, want the first run's label %q visible since cursor is at 0", got, firstLabel)
	}
}

// TestRenderAdminScanHistoryModalNeverAppliesFitLinesOverComposite is the
// Phase 2 task 2.3 RED test (design.md "No fitLines over a composite
// containing a bordered block" — the bug that produced an orphaned "Showing
// x-y of N" line with no table above it): renderAdminScanHistoryModal never
// slices its composed content through fitLines/renderSection. The active
// tab's own bordered table keeps its own pagination footer untouched, and
// no outer "Showing " indicator ever appears in the output.
// TestAdminScanHistoryModalTableBodyShowsEmptyLeaksStateAndKeepsTabVisible is
// the sdd-verify CRITICAL-1 remediation test (spec.md "Secret Findings
// Surface" — "No secret scan for the navigated execution stays visible"):
// when the navigated execution's digest has no secret scan (modal.Secrets is
// empty) and the operator is on the Leaks tab, the modal MUST show an
// explicit empty-state message rather than a blank or crashing body, and the
// Leaks tab MUST stay present in the tab bar rather than being hidden or
// removed.
func TestAdminScanHistoryModalTableBodyShowsEmptyLeaksStateAndKeepsTabVisible(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	modal := adminScanHistoryModal{
		Open:       true,
		Repository: "acme/api",
		Tabs:       newAdminScanHistoryTabs(),
		ActiveTab:  1, // Leaks tab
		Runs:       []ports.ScanRun{{ID: "run-1", CreatedAt: time.Now()}},
		Detail:     ports.ScanRunDetail{Findings: []ports.ScanRunFinding{{VulnerabilityID: "CVE-1"}}},
		Secrets:    nil, // no secret scan recorded for this execution's digest
	}
	view := AdminViewState{}

	body := adminScanHistoryModalTableBody(theme, modal, view, adminScanHistoryModalMinTableBudget)
	if !strings.Contains(body, "No secret findings recorded for this execution.") {
		t.Fatalf("adminScanHistoryModalTableBody() = %q, want the explicit empty-state message when modal.Secrets is empty", body)
	}

	got := renderAdminScanHistoryModal(theme, modal, view, 20)
	if !strings.Contains(got, "No secret findings recorded for this execution.") {
		t.Fatalf("renderAdminScanHistoryModal() = %q, want the explicit empty-state message on the Leaks tab", got)
	}
	if !strings.Contains(got, "Leaks") {
		t.Fatalf("renderAdminScanHistoryModal() = %q, want the Leaks tab to stay present in the tab bar, never hidden", got)
	}
	if !strings.Contains(got, "Vulnerabilities") {
		t.Fatalf("renderAdminScanHistoryModal() = %q, want the Vulnerabilities tab to remain in the tab bar alongside Leaks", got)
	}
}

// TestRenderAdminWorkspaceKeepsBaseFullSizeAndLayersModalOnTopWhenOpen is
// the RED test for the reported regression (claude-handoff.md: "The modal
// must be rendered above the Feature Page ... and must not be appended
// below the page content. The underlying Features page must remain visible
// behind the modal and must not reflow when it opens"). It proves two
// distinct properties the pre-existing suite did not cover:
//
//  1. The base page renders at its FULL, unshrunk size when the modal is
//     open -- identical to its own standalone render at the very same
//     layout, not a row-split-reduced one.
//  2. The modal is composited ON TOP of the base (result height ==
//     layout.Height, the canvas), never appended below it as a trailing
//     block (which would grow the result past the canvas height).
func TestRenderAdminWorkspaceKeepsBaseFullSizeAndLayersModalOnTopWhenOpen(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	now := time.Date(2026, time.August, 12, 11, 0, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", ExpiresAt: now.Add(10 * time.Minute)}

	view := AdminViewState{
		Features: []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		FeaturePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: trivyFeatureName, Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
		},
		TrivyTab: trivyTabRepositoryAlerts,
		ScanHistoryModal: adminScanHistoryModal{
			Open:       true,
			Repository: "acme/api",
			Tabs:       newAdminScanHistoryTabs(),
			Runs:       []ports.ScanRun{{ID: "run-1", Repository: "acme/api", CreatedAt: now}},
			Detail:     ports.ScanRunDetail{Findings: []ports.ScanRunFinding{{VulnerabilityID: "CVE-1"}}},
		},
	}
	view.Tables.Features = buildAdminFeaturesTable(theme, view.Features, 0, minTableRows)
	view.Tables.Findings = buildAdminFindingsTable(theme, view.ScanHistoryModal.Detail.Findings, 0, minTableRows)

	layout := contentBudget(defaultViewportWidth, defaultViewportHeight, "", adminScreenHelp(screenAdminFeatures, view))

	// Property 1: the base page (rendered independently, at the very same
	// unreduced layout the modal-open path is given) must be byte-identical
	// to what renderAdminWorkspace's modal-open branch composites as its
	// base -- proving it is never shrunk via a reduced SectionRows the way
	// the superseded row-split design did.
	standaloneContext, standaloneBody, standaloneHelp := renderAdminScreen(theme, screenAdminFeatures, session, view, nil, layout, now)
	wantBase := renderConsoleWorkspace("Regixtry Admin", standaloneContext, standaloneBody, "", standaloneHelp)

	got := renderAdminWorkspace(screenAdminFeatures, session, view, nil, "", layout, now)

	// Property 2: layered on top, not appended below -- the composite must
	// fit exactly within the canvas (layout.Width x layout.Height), never
	// grow past it the way lipgloss.JoinVertical(base, modal) would once
	// base is rendered at its full size AND the modal is rendered on top of
	// it (base+modal stacked would be far taller than one canvas).
	if h := lipgloss.Height(got); h != layout.Height {
		t.Fatalf("renderAdminWorkspace() height = %d, want exactly layout.Height(%d) -- the modal must be composited on top of the base, never appended below it", h, layout.Height)
	}

	// The base's own bottom-of-screen marker (only present once the full,
	// unshrunk base is rendered) must survive in the composite.
	if !strings.Contains(wantBase, "Operator: operator") {
		t.Fatalf("test setup invalid: standalone base render = %q, want it to contain the Operator marker", wantBase)
	}
	if !strings.Contains(got, "Operator: operator") {
		t.Fatalf("renderAdminWorkspace() = %q, want the base page's full content (Operator marker) to survive unshrunk when the modal opens", got)
	}

	// The modal's own title must be present, proving it was actually
	// rendered and composited, not silently dropped.
	if !strings.Contains(got, "Scan History — acme/api") {
		t.Fatalf("renderAdminWorkspace() = %q, want the modal title present", got)
	}
}

// TestRenderAdminWorkspaceLeavesVisibleMarginAroundModalWhenOpen is the
// permanent, real-render replacement for the throwaway visual-debug test
// used to find this regression: it proves the actual end-to-end
// renderAdminWorkspace output -- not just compositeOverlay's synthetic unit
// tests -- has a real blank gap between the modal's own left/right border
// and whatever base content survives beside it, at the modal's own
// vertical midpoint. Without this, a modal that happens to sit beside a
// wide, unshrunk base table would read as corruption (a base border
// character fused directly against the modal's edge) rather than a
// floating dialog.
func TestRenderAdminWorkspaceLeavesVisibleMarginAroundModalWhenOpen(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	now := time.Date(2026, time.August, 12, 11, 0, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", ExpiresAt: now.Add(10 * time.Minute)}

	view := AdminViewState{
		Features: []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		FeaturePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: trivyFeatureName, Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
		},
		TrivyTab: trivyTabRepositoryAlerts,
		// Two rows (matching a real captured repro), not one, so the
		// Repository Alerts summary table -- widened to its real production
		// column widths -- is actually dense enough at the modal's own row
		// range to make this a genuine regression guard: a sparse
		// single-row fixture leaves enough natural blank space that the
		// margin assertions below would pass even without the fix.
		TrivySummaries: []repositorySummary{
			{Repository: "web-dvwa", LatestRun: ports.ScanRun{ID: "run-1", Repository: "web-dvwa", CreatedAt: now}, LastExecuted: now, RunCount: 13},
			{Repository: "alpine-vuln", LatestRun: ports.ScanRun{ID: "run-2", Repository: "alpine-vuln", CreatedAt: now}, LastExecuted: now, RunCount: 12},
		},
		ScanHistoryModal: adminScanHistoryModal{
			Open:       true,
			Repository: "web-dvwa",
			Tabs:       newAdminScanHistoryTabs(),
			Runs:       []ports.ScanRun{{ID: "run-1", Repository: "web-dvwa", CreatedAt: now}},
			Detail: ports.ScanRunDetail{Findings: []ports.ScanRunFinding{
				{VulnerabilityID: "CVE-2016-9841", Severity: "CRITICAL", PackageName: "rsync", Fixable: true},
				{VulnerabilityID: "CVE-2017-12424", Severity: "CRITICAL", PackageName: "login", Fixable: true},
			}},
		},
	}
	view.Tables.Features = buildAdminFeaturesTable(theme, view.Features, 0, minTableRows)
	view.Tables.ScanSummary = buildAdminScanSummaryTable(theme, view.TrivySummaries, 0, minTableRows)
	view.Tables.Findings = buildAdminFindingsTable(theme, view.ScanHistoryModal.Detail.Findings, 0, minTableRows)

	layout := contentBudget(defaultViewportWidth, defaultViewportHeight, "", adminScreenHelp(screenAdminFeatures, view))

	// Compute the modal's own footprint the exact same way
	// renderAdminWorkspace does, so this test does not hardcode numbers that
	// would silently drift out of sync with the production sizing.
	standaloneContext, standaloneBaseBody, standaloneHelp := renderAdminScreen(theme, screenAdminFeatures, session, view, nil, layout, now)
	standaloneBaseWorkspace := renderConsoleWorkspace("Regixtry Admin", standaloneContext, standaloneBaseBody, "", standaloneHelp)
	modalView := renderAdminScanHistoryModal(theme, view.ScanHistoryModal, view, adminScanHistoryModalRows(layout, lipgloss.Height(standaloneBaseBody)))
	overlayWidth := lipgloss.Width(modalView)
	overlayHeight := lipgloss.Height(modalView)
	x := (layout.Width - overlayWidth) / 2
	// y mirrors compositeOverlay's own centering formula exactly: anchored to
	// the base workspace's own actual rendered height, not the raw
	// layout.Height canvas (admin_overlay.go's compositeOverlay doc comment).
	baseHeight := lipgloss.Height(standaloneBaseWorkspace)
	if baseHeight > layout.Height {
		baseHeight = layout.Height
	}
	y := (baseHeight - overlayHeight) / 2
	if y < 0 {
		y = 0
	}

	got := renderAdminWorkspace(screenAdminFeatures, session, view, nil, "", layout, now)
	lines := strings.Split(ansi.Strip(got), "\n")

	// Check every row of the modal's own footprint, not just the midpoint:
	// the real captured regression showed the fused-border defect on
	// several (not all) of the modal's rows, wherever the base's own
	// content happened to be dense at that exact row.
	for row := y; row < y+overlayHeight; row++ {
		if row < 0 || row >= len(lines) {
			t.Fatalf("test setup invalid: modal row %d out of range (got %d lines)", row, len(lines))
		}
		runes := []rune(lines[row])
		for m := 1; m <= overlayHorizontalMargin; m++ {
			if leftCol := x - m; leftCol >= 0 && leftCol < len(runes) {
				if runes[leftCol] != ' ' {
					t.Fatalf("renderAdminWorkspace() row %d col %d = %q, want a blank margin column left of the modal (base content directly touching the modal's own edge)", row, leftCol, string(runes[leftCol]))
				}
			}
			if rightCol := x + overlayWidth + m - 1; rightCol >= 0 && rightCol < len(runes) {
				if runes[rightCol] != ' ' {
					t.Fatalf("renderAdminWorkspace() row %d col %d = %q, want a blank margin column right of the modal", row, rightCol, string(runes[rightCol]))
				}
			}
		}
	}
}

// TestRenderAdminWorkspaceModalNeverExtendsPastBaseBodysOwnBottomBorder is the
// RED regression test for the height-budget bug found by real-render visual
// inspection at realistic terminal heights (claude-handoff.md follow-up).
// Two independent defects combined to produce it:
//
//  1. adminScanHistoryModalRows derived the modal's row budget purely from
//     l.Height, with no awareness of how tall the base Feature Page body
//     ACTUALLY renders. The base body is content-driven
//     (renderSection/fitLines only ever trims, never pads to fill
//     l.SectionRows), so it stays roughly constant height regardless of
//     terminal height, while the modal's budget kept scaling up with
//     l.Height.
//  2. Even with the modal's own SIZE correctly capped, compositeOverlay
//     centered it within the FULL raw terminal canvas rather than within the
//     base workspace's own actual rendered footprint -- which is
//     mathematically incapable of keeping ANY overlay (down to 1 row) inside
//     a ~29-row base footprint once the terminal exceeds roughly 60 rows, no
//     matter how tightly the budget is capped. This is why the assertion
//     below spans heights up to 80, not just the height (50) where the bug
//     was first confirmed by hand: a size-only fix passes at 24/30/40 by
//     coincidence but is provably unable to pass at 50/80.
//
// This asserts, at a realistic range of terminal heights including the one
// where the bug was confirmed absent-by-coincidence (24) and several where it
// reproduced (30, 40, 50, 80): the modal's own composited bottom row (its
// centered offset, anchored to the base workspace's own rendered height, plus
// its own rendered height) never lands past the base body's own composited
// bottom row (title + context + the base body's actual measured height) --
// computed via lipgloss.Height bookkeeping on the real production render
// functions, not guessed or hardcoded, and not string-scanned for border
// glyphs (fragile once ANSI styling is involved).
func TestRenderAdminWorkspaceModalNeverExtendsPastBaseBodysOwnBottomBorder(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	now := time.Date(2026, time.August, 12, 11, 0, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", ExpiresAt: now.Add(10 * time.Minute)}

	view := AdminViewState{
		Features: []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		FeaturePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: trivyFeatureName, Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
		},
		TrivyTab: trivyTabRepositoryAlerts,
		// A handful of repositories -- realistic content, well under 30 rows
		// total once combined with the Built-in Features table -- so the base
		// body renders at its natural, short, content-driven height instead of
		// being clipped/padded to fill the terminal (design.md: renderSection
		// only ever TRIMS content longer than the budget, never stretches
		// shorter content to fill it).
		TrivySummaries: []repositorySummary{
			{Repository: "web-dvwa", LatestRun: ports.ScanRun{ID: "run-1", Repository: "web-dvwa", CreatedAt: now}, LastExecuted: now, RunCount: 13},
			{Repository: "alpine-vuln", LatestRun: ports.ScanRun{ID: "run-2", Repository: "alpine-vuln", CreatedAt: now}, LastExecuted: now, RunCount: 12},
		},
		ScanHistoryModal: adminScanHistoryModal{
			Open:       true,
			Repository: "web-dvwa",
			Tabs:       newAdminScanHistoryTabs(),
			Runs:       []ports.ScanRun{{ID: "run-1", Repository: "web-dvwa", CreatedAt: now}},
			Detail: ports.ScanRunDetail{Findings: []ports.ScanRunFinding{
				{VulnerabilityID: "CVE-2016-9841", Severity: "CRITICAL", PackageName: "rsync", Fixable: true},
				{VulnerabilityID: "CVE-2017-12424", Severity: "CRITICAL", PackageName: "login", Fixable: true},
			}},
		},
	}
	view.Tables.Features = buildAdminFeaturesTable(theme, view.Features, 0, minTableRows)
	view.Tables.ScanSummary = buildAdminScanSummaryTable(theme, view.TrivySummaries, 0, minTableRows)
	view.Tables.Findings = buildAdminFindingsTable(theme, view.ScanHistoryModal.Detail.Findings, 0, minTableRows)

	for _, height := range []int{24, 30, 40, 50, 80} {
		height := height
		t.Run(fmt.Sprintf("height=%d", height), func(t *testing.T) {
			t.Parallel()

			layout := contentBudget(minViewportWidth, height, "", adminScreenHelp(screenAdminFeatures, view))

			// The base body's own bottom row within the composited canvas:
			// title (1 row, guarded invariant) + context (1 row) + the base
			// body's real measured height, ending at the body's own closing
			// border row.
			baseContext, baseBody, baseHelp := renderAdminScreen(theme, screenAdminFeatures, session, view, nil, layout, now)
			baseBodyHeight := lipgloss.Height(baseBody)
			baseBottomRow := 2 + baseBodyHeight - 1
			baseWorkspace := renderConsoleWorkspace("Regixtry Admin", baseContext, baseBody, "", baseHelp)

			modalRows := adminScanHistoryModalRows(layout, baseBodyHeight)
			modalView := renderAdminScanHistoryModal(theme, view.ScanHistoryModal, view, modalRows)

			overlayHeight := lipgloss.Height(modalView)
			if overlayHeight > layout.Height {
				overlayHeight = layout.Height
			}
			// Mirrors compositeOverlay's own centering formula exactly (see
			// admin_overlay.go's compositeOverlay and this codebase's existing
			// admin_overlay_test.go idiom of recomputing it inline rather than
			// guessing a number): anchored to the base WORKSPACE's own actual
			// rendered height, not the raw layout.Height canvas.
			workspaceHeight := lipgloss.Height(baseWorkspace)
			if workspaceHeight > layout.Height {
				workspaceHeight = layout.Height
			}
			y := (workspaceHeight - overlayHeight) / 2
			if y < 0 {
				y = 0
			}
			modalBottomRow := y + overlayHeight - 1

			if modalBottomRow > baseBottomRow {
				t.Fatalf("height=%d: modal's composited bottom row = %d, want <= %d (the base Feature Page body's own bottom border row) -- the modal renders below where the base page's own bordered box closes, floating in blank canvas instead of over the base page (modalRows=%d, baseBodyHeight=%d, overlayHeight=%d, y=%d)",
					height, modalBottomRow, baseBottomRow, modalRows, baseBodyHeight, overlayHeight, y)
			}
		})
	}
}

func TestRenderAdminScanHistoryModalNeverAppliesFitLinesOverComposite(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	findings := make([]ports.ScanRunFinding, 0, 30)
	for i := 0; i < 30; i++ {
		findings = append(findings, ports.ScanRunFinding{Severity: "HIGH", VulnerabilityID: fmt.Sprintf("CVE-2026-%04d", i), PackageName: "openssl"})
	}
	const pageSize = 5 // 30 findings over pageSize 5 -> 6 pages, forces the table's own internal pagination

	view := AdminViewState{}
	view.Tables.Findings = buildAdminFindingsTable(theme, findings, 0, pageSize)

	modal := adminScanHistoryModal{
		Open:       true,
		Repository: "acme/api",
		Tabs:       newAdminScanHistoryTabs(),
		ActiveTab:  0,
		Runs:       []ports.ScanRun{{ID: "run-1", CreatedAt: time.Now()}},
		Detail:     ports.ScanRunDetail{Findings: findings},
	}

	got := renderAdminScanHistoryModal(theme, modal, view, 20)

	if strings.Contains(got, "Showing ") {
		t.Fatalf("renderAdminScanHistoryModal() output contains an outer fitLines indicator (\"Showing \" substring) — the modal must never apply fitLines over a composite containing a bordered table:\n%s", got)
	}
	if !strings.Contains(got, "1/6") {
		t.Fatalf("renderAdminScanHistoryModal() output = %q, want the Findings table's own pagination footer (1/6) present and untouched", got)
	}
}
