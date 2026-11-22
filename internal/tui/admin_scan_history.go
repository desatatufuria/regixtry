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

// repositorySummary is one aggregated row for the Repository Alerts summary
// table: the latest scan run for a repository, plus how many runs exist and
// when the repository was last executed. Produced by
// summarizeScanRunsByRepository.
type repositorySummary struct {
	Repository   string
	LatestRun    ports.ScanRun
	LastExecuted time.Time // FinishedAt, else CreatedAt
	InProgress   bool      // FinishedAt nil -> show marker, never blank
	RunCount     int
	// Disabled is set by annotateDisabledSummaries (admin_tables.go) when a
	// stored repository override sets Enabled=false for this repository
	// (operator-admin-tui spec's "Repository Alerts Renders Override-
	// Disabled Repositories Distinctly" requirement). Never set by
	// summarizeScanRunsByRepository itself.
	Disabled bool
}

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

// summarizeScanRunsByRepository collapses scan runs into one row per
// repository (spec.md "Repeated rescans collapse into one dated row"). The
// most recently executed run (by effectiveScanRunTime) becomes LatestRun,
// RunCount tracks how many runs were folded in, and LastExecuted/InProgress
// follow the FinishedAt->CreatedAt fallback so the date column is never
// blank. Row order follows first-seen repository order in runs.
func summarizeScanRunsByRepository(runs []ports.ScanRun) []repositorySummary {
	order := make([]string, 0, len(runs))
	byRepo := make(map[string]*repositorySummary, len(runs))

	for _, run := range runs {
		effTime, inProgress := effectiveScanRunTime(run)

		summary, ok := byRepo[run.Repository]
		if !ok {
			order = append(order, run.Repository)
			byRepo[run.Repository] = &repositorySummary{
				Repository:   run.Repository,
				LatestRun:    run,
				LastExecuted: effTime,
				InProgress:   inProgress,
				RunCount:     1,
			}
			continue
		}

		summary.RunCount++
		if effTime.After(summary.LastExecuted) {
			summary.LatestRun = run
			summary.LastExecuted = effTime
			summary.InProgress = inProgress
		}
	}

	summaries := make([]repositorySummary, 0, len(order))
	for _, repo := range order {
		summaries = append(summaries, *byRepo[repo])
	}
	return summaries
}

// repositorySummariesFromScanSummaries maps ports.RepositoryScanSummary rows
// (already one row per repository, collapsed and severity-ordered
// server-side by ListLatestScanRunPerRepository) straight onto the TUI's own
// repositorySummary shape -- no re-grouping, unlike
// summarizeScanRunsByRepository, which this replaces on the production load
// path (repository-alerts-scan-coverage fix).
func repositorySummariesFromScanSummaries(scanSummaries []ports.RepositoryScanSummary) []repositorySummary {
	summaries := make([]repositorySummary, 0, len(scanSummaries))
	for _, scanSummary := range scanSummaries {
		effTime, inProgress := effectiveScanRunTime(scanSummary.Run)
		summaries = append(summaries, repositorySummary{
			Repository:   scanSummary.Run.Repository,
			LatestRun:    scanSummary.Run,
			LastExecuted: effTime,
			InProgress:   inProgress,
			RunCount:     scanSummary.RunCount,
		})
	}
	return summaries
}

// scanRunsFromScanSummaries extracts each ports.RepositoryScanSummary's
// latest run, for AdminViewState.TrivyScanRuns -- which, since this fix, no
// longer holds every raw scan_runs row, only each repository's single
// latest one (one entry per TrivySummaries row).
func scanRunsFromScanSummaries(scanSummaries []ports.RepositoryScanSummary) []ports.ScanRun {
	runs := make([]ports.ScanRun, 0, len(scanSummaries))
	for _, scanSummary := range scanSummaries {
		runs = append(runs, scanSummary.Run)
	}
	return runs
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
