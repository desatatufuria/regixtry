package tui

import (
	"time"

	"regixtry/internal/ports"
)

// adminScanHistoryModalMinRows is the floor of modal inner rows the row
// split will always try to preserve: enough to show a tab bar row, at least
// one table row (with its bordered chrome), and a footer row. When the
// terminal budget cannot honor both this floor and the measured base inner
// height, adminScanHistoryRowSplit degrades to giving the modal the entire
// usable budget and the base section is omitted (baseRows == 0). Phase 2
// refines the modal's own internal chrome accounting
// (adminScanHistoryModalChromeRows) on top of this floor.
const adminScanHistoryModalMinRows = 8

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

// adminScanHistoryRowSplit splits the outer section's row budget between the
// base repository list (already measured at baseInnerHeight) and the scan
// history modal, both drawn from the same single-section budget so the
// total never exceeds the terminal (design.md "Nested budget by row split,
// not overlay, not stacking").
//
// Invariant: (baseRows+sectionChromeRows) + (modalRows+sectionChromeRows) ==
// outer.SectionRows + sectionChromeRows -- exactly the rows one section of
// outer.SectionRows occupies today, because baseRows+modalRows always equals
// usable by construction, regardless of clamping.
//
// When usable cannot honor both adminScanHistoryModalMinRows and the
// measured base content, the split degrades to giving the modal the entire
// usable budget (baseRows == 0, base omitted, modal renders alone).
func adminScanHistoryRowSplit(outer consoleLayout, baseInnerHeight int) (baseRows, modalRows int) {
	usable := outer.SectionRows - sectionChromeRows // the modal's own border+padding
	modalRows = usable - baseInnerHeight
	if modalRows < adminScanHistoryModalMinRows {
		modalRows = adminScanHistoryModalMinRows
	}
	if modalRows > usable {
		modalRows = usable
	}
	baseRows = usable - modalRows // <= 0 -> base omitted, modal renders alone
	return baseRows, modalRows
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
