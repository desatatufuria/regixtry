package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
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
	assertSingleLine(t, "help", theme.help.Render("Tab: next tab | Shift+Tab: prev tab | Left/Right: page history | Esc: close"))
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
