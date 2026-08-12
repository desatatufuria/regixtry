package tui

import (
	"testing"
	"time"

	"regixtry/internal/ports"
)

// TestSummarizeScanRunsByRepositoryDedupsRepeatedRescansIntoOneRow is the
// Phase 1 task 1.1 RED test: spec.md "Repeated rescans collapse into one
// dated row" — several scan runs for the same repository must collapse into
// exactly one repositorySummary row.
func TestSummarizeScanRunsByRepositoryDedupsRepeatedRescansIntoOneRow(t *testing.T) {
	t.Parallel()

	finishedOld := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	finishedNew := time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC)

	runs := []ports.ScanRun{
		{ID: "run-1", Repository: "acme/api", FinishedAt: &finishedOld, CreatedAt: finishedOld},
		{ID: "run-2", Repository: "acme/api", FinishedAt: &finishedNew, CreatedAt: finishedNew},
	}

	got := summarizeScanRunsByRepository(runs)

	if len(got) != 1 {
		t.Fatalf("summarizeScanRunsByRepository() returned %d rows, want 1 (dedup by repository)", len(got))
	}
	if got[0].RunCount != 2 {
		t.Fatalf("RunCount = %d, want 2", got[0].RunCount)
	}
	if got[0].LatestRun.ID != "run-2" {
		t.Fatalf("LatestRun.ID = %q, want %q (the most recently executed run)", got[0].LatestRun.ID, "run-2")
	}
	if !got[0].LastExecuted.Equal(finishedNew) {
		t.Fatalf("LastExecuted = %v, want %v", got[0].LastExecuted, finishedNew)
	}
	if got[0].InProgress {
		t.Fatalf("InProgress = true, want false (latest run has FinishedAt)")
	}
}

// TestSummarizeScanRunsByRepositoryFallsBackToCreatedAtWithInProgressMarker
// is the Phase 1 task 1.1 RED test: spec.md "the date column SHALL show
// CreatedAt with an in-progress marker, never blank" when the latest run has
// no FinishedAt.
func TestSummarizeScanRunsByRepositoryFallsBackToCreatedAtWithInProgressMarker(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 2, 5, 9, 30, 0, 0, time.UTC)

	runs := []ports.ScanRun{
		{ID: "run-1", Repository: "acme/web", FinishedAt: nil, CreatedAt: created},
	}

	got := summarizeScanRunsByRepository(runs)

	if len(got) != 1 {
		t.Fatalf("summarizeScanRunsByRepository() returned %d rows, want 1", len(got))
	}
	if !got[0].InProgress {
		t.Fatalf("InProgress = false, want true (FinishedAt is nil)")
	}
	if got[0].LastExecuted.IsZero() {
		t.Fatalf("LastExecuted is zero, want %v (never blank)", created)
	}
	if !got[0].LastExecuted.Equal(created) {
		t.Fatalf("LastExecuted = %v, want CreatedAt fallback %v", got[0].LastExecuted, created)
	}
}

// TestSummarizeScanRunsByRepositoryHandlesMultipleRepositoriesIndependently
// triangulates task 1.1 across two different repositories, proving the
// dedup/fallback logic is keyed by repository, not global.
func TestSummarizeScanRunsByRepositoryHandlesMultipleRepositoriesIndependently(t *testing.T) {
	t.Parallel()

	finished := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	created := time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)

	runs := []ports.ScanRun{
		{ID: "run-a1", Repository: "acme/api", FinishedAt: &finished, CreatedAt: finished},
		{ID: "run-b1", Repository: "acme/worker", FinishedAt: nil, CreatedAt: created},
	}

	got := summarizeScanRunsByRepository(runs)

	if len(got) != 2 {
		t.Fatalf("summarizeScanRunsByRepository() returned %d rows, want 2", len(got))
	}

	byRepo := make(map[string]repositorySummary, len(got))
	for _, s := range got {
		byRepo[s.Repository] = s
	}

	api, ok := byRepo["acme/api"]
	if !ok {
		t.Fatalf("missing row for acme/api")
	}
	if api.InProgress {
		t.Fatalf("acme/api InProgress = true, want false")
	}

	worker, ok := byRepo["acme/worker"]
	if !ok {
		t.Fatalf("missing row for acme/worker")
	}
	if !worker.InProgress {
		t.Fatalf("acme/worker InProgress = false, want true")
	}
}

// TestAdminScanHistoryModalRowsStaysBoundedAcrossHeightRange is a RED
// property test for the overlay-compositing rewrite: unlike the superseded
// row-split, adminScanHistoryModalRows no longer measures or depends on the
// base page's own rendered height (the modal is a floating overlay,
// independent of the base's budget) -- it must simply stay within
// [adminScanHistoryModalMinRows, l.Height-sectionChromeRows] for every
// terminal height in a wide range.
func TestAdminScanHistoryModalRowsStaysBoundedAcrossHeightRange(t *testing.T) {
	t.Parallel()

	for height := 10; height <= 80; height++ {
		l := contentBudget(140, height, "", "")

		rows := adminScanHistoryModalRows(l)

		if rows < adminScanHistoryModalMinRows {
			t.Fatalf("height=%d: adminScanHistoryModalRows() = %d, want >= adminScanHistoryModalMinRows(%d)", height, rows, adminScanHistoryModalMinRows)
		}
		maxRows := l.Height - sectionChromeRows
		if maxRows < adminScanHistoryModalMinRows {
			maxRows = adminScanHistoryModalMinRows
		}
		if rows > maxRows {
			t.Fatalf("height=%d: adminScanHistoryModalRows() = %d, want <= %d (must never exceed what the terminal can hold)", height, rows, maxRows)
		}
	}
}

// TestAdminScanHistoryModalRowsLeavesAVisibleMarginOnATallTerminal proves the
// modal does not simply consume the entire terminal height on a generously
// tall terminal (claude-handoff.md: "centered/bounded in the viewport"): its
// row budget must be strictly less than the full available height so the
// base page stays visible around it.
func TestAdminScanHistoryModalRowsLeavesAVisibleMarginOnATallTerminal(t *testing.T) {
	t.Parallel()

	l := contentBudget(140, 60, "", "")

	rows := adminScanHistoryModalRows(l)
	fullyAvailable := l.Height - sectionChromeRows

	if rows >= fullyAvailable {
		t.Fatalf("adminScanHistoryModalRows() = %d, want strictly less than %d (a tall terminal must still show a margin around the floating modal)", rows, fullyAvailable)
	}
}

// TestCycleIndexWrapsForwardPastTheEnd is the Phase 1 task 1.5 RED test:
// unlike boundedIndex (which clamps), cycleIndex wraps past the last index
// back to 0.
func TestCycleIndexWrapsForwardPastTheEnd(t *testing.T) {
	t.Parallel()

	got := cycleIndex(2, 3) // last valid index (size-1)
	if got != 2 {
		t.Fatalf("cycleIndex(2, 3) = %d, want 2 (still in range)", got)
	}

	got = cycleIndex(3, 3) // one past the end
	if got != 0 {
		t.Fatalf("cycleIndex(3, 3) = %d, want 0 (wraps to first)", got)
	}
}

// TestCycleIndexWrapsBackwardBeforeTheStart triangulates task 1.5: moving
// before index 0 wraps to the last valid index, proving cycleIndex is
// distinct from the clamping boundedIndex in both directions.
func TestCycleIndexWrapsBackwardBeforeTheStart(t *testing.T) {
	t.Parallel()

	got := cycleIndex(-1, 3)
	if got != 2 {
		t.Fatalf("cycleIndex(-1, 3) = %d, want 2 (wraps to last)", got)
	}
}

// TestCycleIndexHandlesZeroSize guards the degenerate empty-slice case so
// callers never index out of range while a tab slice is still empty.
func TestCycleIndexHandlesZeroSize(t *testing.T) {
	t.Parallel()

	got := cycleIndex(5, 0)
	if got != 0 {
		t.Fatalf("cycleIndex(5, 0) = %d, want 0", got)
	}
}
