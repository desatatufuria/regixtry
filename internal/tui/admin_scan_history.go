package tui

import (
	"sort"
	"time"

	"regixtry/internal/ports"
)

// adminScanHistoryWindowLimit bounds loadAdminScanHistoryCmd's ListScanRuns
// call for one repository's drill-down history (design.md "Chronological
// history by client-side re-sort" — reuses ListScanRuns(repository, 50)
// rather than a new port/query).
const adminScanHistoryWindowLimit = 50

// effectiveScanRunTime returns a scan run's last-execution timestamp:
// FinishedAt when present, else CreatedAt with inProgress=true so callers
// can render an in-progress marker instead of leaving the date column blank
// (spec.md "Repository Alerts Summarized Per Repository With Ordering And
// Freshness").
func effectiveScanRunTime(run ports.ScanRun) (t time.Time, inProgress bool) {
	if run.FinishedAt != nil {
		return *run.FinishedAt, false
	}
	return run.CreatedAt, true
}

// sortScanRunsChronologically returns a NEW slice of runs ordered newest
// first by effectiveScanRunTime (design.md "Chronological history by
// client-side re-sort"). The scan history modal's prev/next navigation moves
// through this order one run at a time; it is deliberately independent of
// the severity/fixability ordering compareScanRuns uses for the Repository
// Alerts summary table.
func sortScanRunsChronologically(runs []ports.ScanRun) []ports.ScanRun {
	sorted := append([]ports.ScanRun(nil), runs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left, _ := effectiveScanRunTime(sorted[i])
		right, _ := effectiveScanRunTime(sorted[j])
		return left.After(right)
	})
	return sorted
}

// adminScanHistoryDetailMatchesCursor reports whether a loaded scan-run
// detail still corresponds to the modal's CURRENTLY navigated run (spec.md
// "Leaks findings SHALL use that run's own digest, not another's"). Guards
// against a stale async response for a run the operator has since paged away
// from overwriting the currently-displayed run's data — the exact race
// design.md flags as "Digest-per-run correctness".
func adminScanHistoryDetailMatchesCursor(modal adminScanHistoryModal, detail ports.ScanRunDetail) bool {
	if len(modal.Runs) == 0 {
		return false
	}
	current := modal.Runs[boundedIndex(modal.Cursor, len(modal.Runs))]
	return detail.Run.ID == current.ID
}

// adminScanHistorySecretsMatchCursor is adminScanHistoryDetailMatchesCursor's
// counterpart for the secret-findings leg of the chain: a secret-findings
// response is only applied when its repository+digest still match the
// modal's currently navigated run.
func adminScanHistorySecretsMatchCursor(modal adminScanHistoryModal, repository string, digest string) bool {
	if len(modal.Runs) == 0 {
		return false
	}
	current := modal.Runs[boundedIndex(modal.Cursor, len(modal.Runs))]
	return current.Repository == repository && current.Digest == digest
}

// cycleIndex wraps index into [0, size) by modulo, unlike boundedIndex
// (model.go) which clamps to the range. Used by the scan history modal's tab
// cycling (Tab/Shift+Tab), where moving past the last tab wraps to the first
// and moving before the first wraps to the last (design.md "Ordered tab
// slice with a wrapping cursor").
func cycleIndex(index, size int) int {
	if size <= 0 {
		return 0
	}
	index %= size
	if index < 0 {
		index += size
	}
	return index
}
