# Design: Admin Scan History Modal

## Technical Approach

Repository Alerts becomes a per-repository summary table. Enter opens an `adminScanHistoryModal` that owns a **nested row budget carved out of the same single-section budget** the base screen already has, so the base list stays visible above it and the total never exceeds the terminal. New pure logic lands in `internal/tui/admin_scan_history.go`; rendering stays in `admin_views.go`, sizing in `admin_tables.go`.

## Architecture Decisions

### Decision: Nested budget by row split, not overlay, not stacking

**Choice**: `renderAdminWorkspace` splits the outer `consoleLayout.SectionRows` into `baseRows` + `modalRows`, paying for the modal's second bordered shell out of that same budget:

```go
// admin_scan_history.go
func adminScanHistoryRowSplit(outer consoleLayout, baseInnerHeight int) (baseRows, modalRows int) {
    usable := outer.SectionRows - sectionChromeRows // the modal's own border+padding
    modalRows = usable - baseInnerHeight
    if modalRows < adminScanHistoryModalMinRows { modalRows = adminScanHistoryModalMinRows }
    if modalRows > usable { modalRows = usable }
    baseRows = usable - modalRows // <= 0 -> base omitted, modal renders alone
    return baseRows, modalRows
}
```

Invariant: `(baseRows+4) + (modalRows+4) == outer.SectionRows + 4` — exactly the rows one section of `outer.SectionRows` occupies today. `baseInnerHeight` is the **measured** `lipgloss.Height` of the already-rendered base inner content, following `trivyAlertDetailTableRoles`' "measure, don't guess" discipline.

**Alternatives considered**: `lipgloss.JoinVertical(body, modal)` (today's modal pattern); full body replacement; centered floating overlay via `lipgloss.Place`.
**Rationale**: Stacking is additive and unbudgeted — the exact overflow class just fixed. Full replacement was rejected by the user. A centered overlay would truncate the fixed-width `bubble-table` columns horizontally; rows are the safety dimension, so the modal keeps full width. At very small terminals the split degrades gracefully into modal-only rendering.

### Decision: No `fitLines` over a composite containing a bordered block

**Choice**: each block is clipped exactly once, by its own owner. The base body is clipped by `renderSection` at `baseRows`. The modal composes its inner content arithmetically — variable blocks measured, the findings table sized from the remainder — and is wrapped by `theme.section.Render` directly. When the budget cannot hold a bordered table, the table is **replaced by a single-line substitute**, never sliced.

**Alternatives considered**: concatenate modal inner blocks and let one final `fitLines` clip them.
**Rationale**: that is precisely the bug that produced an orphaned `Showing x-y of N` line with no table above it.

### Decision: App-layer summary over a widened `ListScanRuns` window

**Choice**: `summarizeScanRunsByRepository([]ports.ScanRun) []repositorySummary`, pure, fed by `loadAdminScanRunsCmd("", adminScanRunsSummaryLimit /*200*/)`.
**Alternatives considered**: new SQL `GROUP BY`/window query and port surface.
**Rationale**: proposal Q1 forbids schema/query change; 200 rows is free client-side. Limitation: a repository whose runs all fall outside the newest 200 does not appear — revisit per Q1.

### Decision: Chronological history by client-side re-sort

**Choice**: `loadAdminScanHistoryCmd(repository)` reuses `ListScanRuns(repository, 50)` and re-sorts descending by effective time in the TUI.
**Alternatives considered**: an `ORDER BY created_at` variant or sort parameter on the port.
**Rationale**: ordering is a presentation need; a port change would ripple into sqlite plus every fake/mock.

### Decision: Ordered tab slice with a wrapping cursor

**Choice**: `Tabs []adminScanHistoryTab` + `ActiveTab int` + new `cycleIndex(index, size)` (existing `boundedIndex` clamps, so it cannot cycle).
**Rationale**: Q4 requires N-feature extensibility; adding a third tab is one slice entry.

## Data Flow

    Enter on summary row
      └─> loadAdminScanHistoryCmd(repo) ──> adminScanHistoryLoadedMsg (runs, chronological)
            └─> loadAdminScanRunDetailCmd(run.ID) ──> adminScanRunDetailLoadedMsg (Vulnerabilities tab)
                  └─> loadAdminSecretScanFindingsCmd(run.Repository, run.Digest) ──> adminSecretScanFindingsLoadedMsg (Leaks tab)

Left/Right moves the history cursor and re-runs the detail→secrets chain for that run's ID/digest, so tabs never mix executions. The existing detail→secrets chaining is reused verbatim.

## Interfaces / Contracts

```go
type repositorySummary struct {
    Repository   string
    LatestRun    ports.ScanRun
    LastExecuted time.Time // FinishedAt, else CreatedAt
    InProgress   bool      // FinishedAt nil -> show marker, never blank
    RunCount     int
}

type adminScanHistoryModal struct {
    Open       bool
    Repository string
    Tabs       []adminScanHistoryTab // {Kind, Label}; Leaks always present
    ActiveTab  int
    Runs       []ports.ScanRun // newest first
    Cursor     int
    Detail     ports.ScanRunDetail
    Secrets    []ports.SecretFinding
    Loading    bool
    Error      string
}
func (m adminScanHistoryModal) Active() bool { return m.Open }
```

Modal chrome accounting: `adminScanHistoryModalChromeRows = sectionChromeRows + 4` (title, tab bar, `Execution 2/17 — date` footer, help — each guarded single-row by test). Table page size = `modalRows - chrome - measuredHeaderHeight - tableChromeRows`, floored at `minTableRows`.

Keys (`updateAdminScanHistoryModalKey`, gated in `updateAdminKey` before `updateAdminFeaturesKey`): Tab/Shift+Tab cycle tabs, Left/Right page history, Up/Down move the active table highlight, Esc closes.

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/tui/admin_scan_history.go` | Create | Summary aggregation, modal state, tabs, row-split/sizing arithmetic |
| `internal/tui/session.go` | Modify | Add `ScanHistoryModal`, `TrivySummaries`; remove `TrivyAlertDetailOpen`, `TrivyScanRunDetail`, `SecretFindings` |
| `internal/tui/admin_views.go` | Modify | Summary render; modal render; remove inline detail block from `renderTrivyRepositoryAlerts`; fold `renderSecretFindingsBody` into the Leaks tab |
| `internal/tui/admin_tables.go` | Modify | Summary table columns (+ last-execution); delete `trivyAlertDetailTableRoles`, `trivyDetailFixedLines`, `trivyScreenChromeLines` |
| `internal/tui/model.go` | Modify | Modal key routing, history cmd/msg, `cycleIndex`; remove `isTrivyAlertsDetailOpen` |
| `internal/tui/model_test.go`, `viewport_test.go` | Modify | RED coverage below |

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | `summarizeScanRunsByRepository` dedup, `FinishedAt`→`CreatedAt` fallback, in-progress marker | Table-driven on `[]ports.ScanRun` |
| Unit | `adminScanHistoryRowSplit` invariant and degenerate collapse | Property test over heights 10–80 |
| Unit | `cycleIndex` wraps both directions | Table-driven |
| Integration | Modal open/close, tab cycle, history paging binds findings to the cursor's digest | `runKey` on `Model` |
| Integration | `lipgloss.Height(View()) <= viewport.Height` with modal open, heights 24–60 | Boundary test |
| Integration | No orphan indicator: an indicator line implies a table border above it | Rendered-output assertion |

## Threat Matrix

N/A — no routing, shell, subprocess, VCS/PR automation, executable-file classification, or process-integration boundary. Read-path TUI presentation only.

## Migration / Rollout

No migration required. No schema, port, or API change.

## Open Questions

- None blocking.
