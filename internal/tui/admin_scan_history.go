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

// adminScanHistoryModalMinRows is the floor of modal inner rows
// adminScanHistoryModalRows will always try to preserve: enough to show a
// tab bar row, at least one table row (with its bordered chrome), and a
// footer row.
const adminScanHistoryModalMinRows = 8

// adminScanHistoryModalVerticalMargin reserves rows above/below the modal's
// own content budget so it visibly floats over the base page
// (claude-handoff.md: "must be rendered above the Feature Page,
// centered/bounded in the viewport") instead of consuming the entire
// terminal height even on a tall terminal.
const adminScanHistoryModalVerticalMargin = 6

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

// adminScanHistoryModalRows computes the scan history modal's own content
// row budget from the terminal's full layout, independent of the base
// page's row budget now that the modal is a true floating overlay
// (claude-handoff.md) composited on top of the base via compositeOverlay,
// rather than the superseded "Nested budget by row split, not overlay, not
// stacking" design that carved the modal's rows out of the base's own
// budget. Bounded so the modal always leaves a visible margin around it
// (adminScanHistoryModalVerticalMargin) and never exceeds what the terminal
// can hold, floored at adminScanHistoryModalMinRows.
func adminScanHistoryModalRows(l consoleLayout) int {
	usable := l.Height - sectionChromeRows
	if usable < adminScanHistoryModalMinRows {
		usable = adminScanHistoryModalMinRows
	}

	rows := l.Height - adminScanHistoryModalVerticalMargin - sectionChromeRows
	if rows < adminScanHistoryModalMinRows {
		rows = adminScanHistoryModalMinRows
	}
	if rows > usable {
		rows = usable
	}
	return rows
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
