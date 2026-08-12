# Apply Progress: Admin Scan History Modal

## Phase 1-5 (prior batches) — COMPLETE, 24/24 tasks

Summary preserved from Engram `sdd/admin-scan-history-modal/apply-progress`
(full history there): Phase 1 (Pure Logic, 5/5), Phase 2 (Modal State &
Rendering unwired, 4/4), Phase 3 (Wiring — Keys & History Navigation, 8/8),
Phase 4 (Removal Sweep, 5/5), Phase 5 (Non-Regression, 2/2), plus a
Remediation Pass closing sdd-verify CRITICAL-1 (missing empty-Leaks-state
test). All marked `[x]` in `tasks.md`.

## Phase 6 (this batch) — Visual Regression Fix, 5/5

The user ran the shipped Phase 1-5 build and found two real regressions,
documented with screenshots and a written acceptance spec in
`claude-handoff.md`. The existing Phase 1-5 test suite (e.g.
`TestRenderAdminScanHistoryModalNeverAppliesFitLinesOverComposite`) checked
"stays within height budget" and "no orphaned indicator" — necessary but not
sufficient to catch "stacked instead of overlaid" or "wider than its own
border".

### Bug 1: modal was a stacked panel, not a true overlay

**Root cause**: `renderAdminWorkspace` split the outer section's row budget
between the base page and the modal (`adminScanHistoryRowSplit` /
`adminScanHistoryModalRowSplit`), rendering both via
`lipgloss.JoinVertical(body, modalView)` in the same vertical flow — the
design.md-documented "Nested budget by row split, not overlay, not
stacking" decision. Implemented as literally two stacked bordered boxes,
confirmed in `tmp/feature-respository-alerts.vulnerabilities.png`.

**Fix**: real overlay compositing.
- New `internal/tui/admin_overlay.go`: `compositeOverlay(base, overlay,
  width, height)` builds a `cellbuf.Buffer` canvas, writes `base` across the
  whole canvas via `cellbuf.SetContentRect`, then writes `overlay` into a
  centered sub-rectangle via a second `cellbuf.SetContentRect` call —
  overwriting only that rectangle's cells, never touching anything outside
  it. `cellbuf.Render` re-emits the composited grid as ANSI. Chosen over
  hand-rolled string slicing specifically because both `base` and `overlay`
  carry real lipgloss SGR escape sequences; `cellbuf` parses ANSI into a
  cell grid and re-emits it, so every splice happens at cell boundaries,
  never mid-escape-sequence. `github.com/charmbracelet/x/cellbuf` was
  already resolved as an indirect dependency (`go.mod`); this change
  promotes it to direct (`go mod tidy`).
- `renderAdminWorkspace` (`admin_views.go`) now renders the base page at its
  full, unshrunk `layout` (identical to a modal-closed render) and
  composites the modal on top via `compositeOverlay(baseWorkspace,
  modalView, layout.Width, layout.Height)`.
- `adminScanHistoryModalRows` (`admin_scan_history.go`) replaces
  `adminScanHistoryRowSplit`/`adminScanHistoryModalRowSplit`: the modal's
  row budget is now derived independently from the terminal's own height
  (bounded, floored at `adminScanHistoryModalMinRows`, leaving a visible
  margin via `adminScanHistoryModalVerticalMargin`) rather than carved out
  of the base's measured height.
- `rebuildAdminTables` (`admin_tables.go`) updated to call
  `adminScanHistoryModalRows(layout)` directly.
- Doc comments referencing "Nested budget by row split, not overlay, not
  stacking" updated in `admin_scan_history.go`, `admin_views.go`,
  `session.go`.
- Key/tab/navigation/digest-per-run logic (Phase 3) was NOT touched — only
  the rendering/compositing path changed.

### Bug 2: table columns overflowed their bordered box width

**Root cause**: `theme.section` (`admin_theme.go`) had a hardcoded
`.Width(88)` — explicitly flagged as a deferred issue in the
`tui-table-viewport-fixed-size` proposal's own Q5 ("out of scope for this
change"). `buildAdminScanSummaryTable`'s 8 columns summed to a natural
rendered width of 99 (measured), wider than the 88-wide section wrapping
it, so lipgloss word-wrapped the table's own lines mid-box, corrupting
borders — visible in `tmp/features-repositoriy_alerts.png`.

**Fix**: made section width responsive and gave tables real breathing room.
- `sectionWidth(l consoleLayout)` (`viewport.go`) derives `theme.section`'s
  width from the real terminal width (`l.Width - sectionHorizontalOverhead`,
  floored at `minSectionContentWidth`), replacing the hardcoded constant.
  Wired into `renderSection` via `theme.section.Width(sectionWidth(l))`.
  `theme.section` itself no longer carries a `Width` at construction; call
  sites without a `consoleLayout` (unwired forms like Create User) fall back
  to auto-sizing to their own content.
- Widened every admin table's column definitions
  (`adminFeaturesColumn*Width`, `adminFindingsColumn*Width`,
  `adminSecretColumn*Width`, `adminScanSummaryColumn*Width` in
  `admin_tables.go`), each measured against real longest values —
  `adminScanSummaryColumnLastExecutedWidth` (34) against
  `formatScanSummaryLastExecuted`'s longest output ("YYYY-MM-DD HH:MM (in
  progress)", 30 chars wide).
- Raised `minViewportWidth` 90→132 and `defaultViewportWidth` 100→150,
  `defaultViewportHeight` 40→44 (`viewport.go`) — the floor terminal size
  must actually fit the widened Repository Alerts table (natural width 127
  measured after widening); a floor narrower than that would still corrupt
  borders regardless of how responsive the section width is.
- `TestModelViewBelowMinimumSizeShowsTerminalTooSmall` updated for the new
  floor (was asserting the literal string `"90x24"`).

## TDD Cycle Evidence

| Area | RED | GREEN | REFACTOR |
|---|---|---|---|
| `sectionWidth`/column widths | New tests reference `sectionWidth`/`adminScanSummaryColumnLastExecutedWidth` before they existed → compile failure (confirmed via `git stash` of the three production files) | Added `sectionWidth`, widened columns, raised viewport dims → tests pass | Extracted named column-width constants instead of inline literals |
| `compositeOverlay` | New `admin_overlay_test.go` referenced `compositeOverlay` before `admin_overlay.go` existed → compile failure (confirmed by moving the file aside) | Implemented `compositeOverlay` on `cellbuf` → all 4 tests pass | n/a — single-purpose helper |
| `renderAdminWorkspace` overlay-vs-stack | Temporarily reverted `renderAdminWorkspace`'s modal branch to the old `lipgloss.JoinVertical` stacking, re-ran `TestRenderAdminWorkspaceKeepsBaseFullSizeAndLayersModalOnTopWhenOpen` → FAILED: `height = 39, want exactly layout.Height(44)` | Restored the `compositeOverlay` call → test passes | n/a |
| `adminScanHistoryModalRows` | Old row-split tests deleted (function removed); new bounded-property tests written against the new function signature | Pass | n/a |

## Work Unit Evidence

| Evidence | Value |
|---|---|
| Focused test command and result | `go test ./internal/tui/...` → `ok` (all packages); targeted runs of every new test name individually also `PASS` |
| Runtime harness | `bash docs/verification/scripts/tui-smoke.sh` → "TUI smoke verified snapshot launch and feature-manager action/help coverage." Additionally hand-verified visually via a temporary throwaway test driving the real `Update()`/`View()` key-press flow (login → f → tab → enter) and printing `View()`: confirmed the modal renders centered, overlapping the base page's own content (Built-in Features table, Repository Alerts section, "Operator: operator" line all remain visible behind/around the modal), never appended below it. |
| Rollback boundary | Two independent commits: `fix(tui): derive section width from viewport instead of hardcoded 88` (Bug 2, `admin_theme.go`/`viewport.go`/`admin_tables.go`/`admin_tables_test.go`/`model_test.go`) and `fix(tui): render scan history modal as a true floating overlay` (Bug 1, `admin_overlay.go`(new)/`admin_overlay_test.go`(new)/`admin_views.go`/`admin_views_test.go`/`admin_scan_history.go`/`admin_scan_history_test.go`/`admin_tables.go`/`session.go`/`go.mod`). Either commit can be reverted independently without breaking the other (Bug 1's overlay compositing does not depend on Bug 2's width constants, and vice versa; both were verified green in isolation before combining). |

## Final verification (this batch)

- `go build ./...` — clean, no output.
- `go vet ./...` — clean, no output.
- `gofmt -l .` — empty (no files need formatting).
- `go mod tidy` — moved `github.com/charmbracelet/x/cellbuf` from indirect
  to direct in `go.mod`; no other changes.
- `go test -count=1 ./...` — all 16 testable packages pass (`internal/domain/auth`
  has no test files, unchanged).
- `bash docs/verification/scripts/tui-smoke.sh` — passes.

**Changed lines this batch**: 13 files, 557 insertions / 155 deletions = 712
changed lines (`git diff --shortstat` against the pre-batch commit,
restricted to `internal/tui/` + `go.mod`). This exceeds the standard 400/500
review budget; the session preflight recorded delivery strategy `single-pr`
with an explicit `size:exception` for this change, which this batch relies
on — reported honestly rather than split, per the runtime attempt ledger's
instruction to "budget accordingly but report the honest number."

## `cellbuf` investigation note

Real investigation was done before using `cellbuf`, not a hand-roll
fallback: read `writer.go`/`buffer.go` from the pinned module cache
(`v0.0.15`) to confirm `cellbuf.NewBuffer`, `cellbuf.SetContentRect` (ANSI-
parsing cell writer, ANSI-safe by construction), and `cellbuf.Render`
(cell-grid → ANSI re-emitter) existed and fit this exact use case
(compositing one already-rendered lipgloss block onto another at an offset).
No fallback to manual ANSI slicing was needed — `cellbuf` fit directly.

Ready for re-`sdd-verify`.
