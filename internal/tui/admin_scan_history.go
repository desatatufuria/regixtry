package tui

import (
	"fmt"
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

// adminScanHistoryModalExecutionsColumnWidth is the fixed content width
// (before its own trailing border column) of the modal's left-hand
// executions rail (renderAdminScanHistoryModal). Measured against the
// longest legitimate row label -- "50. 12-31 23:59" (adminScanHistoryWindowLimit's
// worst case, 2-digit index + ". " + "MM-DD HH:MM") is 15 runes -- with
// headroom for the "Executions" heading itself (10 runes) and padding, the
// same "strictly less than the longest value" discipline as
// adminScanSummaryColumnLastExecutedWidth.
const adminScanHistoryModalExecutionsColumnWidth = 18

// adminScanHistoryModalExecutionRowLabel renders one executions-rail row: the
// run's 1-based index and a compact date, deliberately narrower than
// adminScanHistoryModalFooter's full "2006-01-02 15:04" (too wide for a
// fixed narrow rail column).
func adminScanHistoryModalExecutionRowLabel(index int, run ports.ScanRun) string {
	effTime, _ := effectiveScanRunTime(run)
	return fmt.Sprintf("%d. %s", index+1, effTime.UTC().Format("01-02 15:04"))
}

// adminScanHistoryModalExecutionsWindow computes the [start, end) bounds of a
// scrollable window over `total` runs, holding at most windowSize entries and
// anchored so `cursor` stays visible -- centered on the cursor when the
// window is smaller than total, clamped at both ends so it never scrolls
// past the first/last run. Runs can number up to adminScanHistoryWindowLimit
// (50) while the modal's own row budget is tight, so the executions rail
// must never unconditionally render the full list (claude-handoff.md's
// "never sliced" discipline extended to this new column: a WINDOW, not a
// clip of an already-bordered block).
func adminScanHistoryModalExecutionsWindow(cursor, total, windowSize int) (start, end int) {
	if windowSize <= 0 || total <= 0 {
		return 0, 0
	}
	if windowSize >= total {
		return 0, total
	}
	start = cursor - windowSize/2
	if start < 0 {
		start = 0
	}
	if start+windowSize > total {
		start = total - windowSize
	}
	return start, start + windowSize
}

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

// adminScanHistoryModalRows computes the scan history modal's own content
// row budget from the terminal's full layout, independent of the base
// page's row budget now that the modal is a true floating overlay
// (claude-handoff.md) composited on top of the base via compositeOverlay,
// rather than the superseded "Nested budget by row split, not overlay, not
// stacking" design that carved the modal's rows out of the base's own
// budget. Bounded so the modal always leaves a visible margin around it
// (adminScanHistoryModalVerticalMargin) and never exceeds what the terminal
// can hold, floored at adminScanHistoryModalMinRows.
//
// It IS, however, capped against baseBodyHeight -- the base Feature Page
// body's ACTUAL measured rendered height (lipgloss.Height on
// renderAdminScreen's own body return, the same "measured, not guessed"
// discipline this file already applies elsewhere), not a viewport-derived
// ceiling like l.SectionRows (which scales with terminal height the same way
// l.Height does and would not fix the bug this cap exists for). The base
// body is content-driven, not viewport-filling (renderSection/fitLines only
// ever TRIM content longer than the budget, never pad shorter content to
// fill it), so on a real screen its height stays roughly constant regardless
// of terminal height while an uncapped l.Height-only budget keeps scaling
// up. Past a certain terminal height that mismatch let the modal's own
// bottom rows render below where the base page's own bordered box closes --
// compositeOverlay centers the modal within the FULL terminal canvas, not
// clamped to the base box's own bounds -- floating in blank canvas instead
// of over the base page. Capping so the modal's total rendered height (rows
// plus its own sectionChromeRows border+padding, added once more by
// theme.section.Render in renderAdminScanHistoryModal) never exceeds
// baseBodyHeight-adminScanHistoryModalVerticalMargin keeps the modal inside
// the base box's own footprint. baseBodyHeight<=0 disables the cap (no
// measurement available yet).
func adminScanHistoryModalRows(l consoleLayout, baseBodyHeight int) int {
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

	if baseBodyHeight > 0 {
		maxByBase := baseBodyHeight - adminScanHistoryModalVerticalMargin - sectionChromeRows
		if maxByBase < adminScanHistoryModalMinRows {
			maxByBase = adminScanHistoryModalMinRows
		}
		if rows > maxByBase {
			rows = maxByBase
		}
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
