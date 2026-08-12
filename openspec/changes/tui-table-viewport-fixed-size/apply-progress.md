# Apply Progress: TUI Table Viewport Fixed Size

## Status
36/36 original tasks complete. sdd-verify PASSed. Change merged to `develop`.
This file adds a **post-verify regression fix** found during real-world RC
testing of the merged change (not a new numbered task).

## Batch: Original implementation (Phases 1-5)
All 36 tasks complete — see Engram `sdd/tui-table-viewport-fixed-size/tasks`
(obs #975) for the full task list and `tasks.md` in this directory for the
`[x]` marks.

## Batch: Remediation — CRITICAL-01 coverage gap (pre-existing, Engram obs #976)
REMEDIATION pass closing sdd-verify's CRITICAL-01 finding: all 36/36 tasks
were already complete; verify FAILed on a coverage gap (missing test for
"Long table pages internally"), not a functional bug. Added
`TestModelAdminScanRunsTablePagesOnArrowKeyNavigationPastPageBoundary`
(model_test.go, +120 lines); zero production code changed. Commit `9686c34`.
Full detail preserved in Engram obs #976.

## Batch: Post-verify regression fix — Findings table clipped when Trivy detail is open

### Bug report (real-world RC testing)
On a reasonably tall terminal, opening a Trivy scan-run's detail (Enter on a
row in the Repository Alerts screen) no longer showed the vulnerability
Findings table. The operator instead saw the ScanRuns table (sized to the
full "primary" budget), the "Selected Scan Run" detail header, and then an
orphaned `"Showing 1-51 of 82"` line with no Findings table visible above it.

### Root cause
`renderTrivyRepositoryAlerts` (`internal/tui/admin_views.go:193`) composes
the ScanRuns table, the "Selected Scan Run" detail text, the Findings table,
and the Secret Findings body into one joined string, which is embedded inside
`renderAdminFeaturesScreen`'s larger composed content (Built-in Features
table + tabs + the Trivy block + trailing operator/session lines) and passed
through `renderSection` → `fitLines` (`internal/tui/viewport.go:88-120`).
`fitLines` is a blind line-slicer with zero awareness of widget boundaries —
when the combined content exceeds the section's row budget, it truncates
mid-widget, which is what dropped the Findings table entirely while leaving
its own trailing position indicator visible.

The reason the combined content was oversized: `tableRoles(layout)`
(`internal/tui/admin_tables.go:241`) always gave the ScanRuns table the FULL
`primary` budget regardless of whether a detail view (with its own Findings
table) was also being rendered below it on the same screen, starving the
Findings table of any room before the outer clip had to act.

### Fix
`internal/tui/admin_tables.go`:
- Added `trivyDetailFixedLines` (13) and `trivyScreenChromeLines` (8)
  constants documenting the fixed non-table row counts around the Trivy
  detail block and the surrounding Features-screen chrome (Built-in
  Features heading, tabs, trailing operator/session lines).
- Added `trivyAlertDetailTableRoles(l consoleLayout, featuresTableHeight int,
  hasSecretFindings bool) (scanRuns, findings int)`: when a Trivy scan-run
  detail is open, ScanRuns collapses to the compact role (`tableRoles`'s
  `compact` value) since browsing the full run list is no longer the point;
  the freed budget is computed — using the Features table's real measured
  height via `lipgloss.Height` (same measurement style as
  `contentBudget`'s status/help chrome) plus the documented fixed line
  counts — and given to Findings instead, floored at `minTableRows`.
- `rebuildAdminTables`: when `m.adminView.TrivyAlertDetailOpen` is true, use
  `trivyAlertDetailTableRoles` for the ScanRuns/Findings pageSizes instead of
  `layout.Primary`/`layout.Compact`. Non-detail-open behavior (Features
  screen, ScanRuns without detail) is unchanged — still uses
  `layout.Primary`/`layout.Compact` as before.

### TDD Cycle Evidence
| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| Regression fix | `internal/tui/model_test.go` | Integration (full `Model.View()`) | ✅ full `internal/tui` suite green pre-change | ✅ Written first, confirmed failing against unmodified `admin_tables.go` via `git stash` | ✅ Passed after fix | ➖ Single scenario (bug reproduction is inherently one concrete case; existing `TestModelTrivyRepositoryAlertsScreenFitsViewportHeight` at the 24-row floor and `TestRebuildAdminTablesBakesPrimaryAndCompactPageSizeIntoTables` already cover the non-detail-open and generic-budget paths) | ➖ None needed — implementation is already a small, single-purpose function with documented constants |

New test:
`TestModelTrivyRepositoryAlertsDetailShowsFindingsTableOnReasonablyTallTerminal`
(`internal/tui/model_test.go`) — builds a Model with a 50-row terminal
(`height := 50`), `TrivyAlertDetailOpen = true`, 30 scan runs, and 80
findings, calls `rebuildAdminTables` + `View()`, and asserts:
```go
if !strings.Contains(view, "Findings") {
    t.Fatalf("view = %q, want the \"Findings\" section heading to survive the outer clip", view)
}
if !strings.Contains(view, findings[0].VulnerabilityID) {
    t.Fatalf("view = %q, want the Findings table's own bordered box (rows like %q) to actually render, not just its section heading", view, findings[0].VulnerabilityID)
}
```
Confirmed RED against unmodified `admin_tables.go` via `git stash push --
internal/tui/admin_tables.go` (view showed content cut off right after the
"Selected Scan Run" detail fields, with no "Findings" heading or CVE data
anywhere in the rendered output — total composed content was 76 lines
against a 42-line visible budget). Confirmed GREEN after the fix (`git stash
pop` + fix) with zero clipping of the Findings table.

Note: at the pre-existing minimum-viewport regression test's height (24
rows, `TestModelTrivyRepositoryAlertsScreenFitsViewportHeight`), the combined
chrome (Built-in Features table + tabs + ScanRuns + 6 detail fields + Secret
Findings) already exceeds the row budget before any table renders a single
row — this is the same accepted structural limit that test's own doc comment
documents ("no per-table budget split can make all of this fit at the
minimum 24-row terminal"); the outer-pane clip remains the safety net there,
unchanged by this fix. The new regression test intentionally uses a taller
(50-row), more realistic terminal to demonstrate the fix genuinely restores
Findings-table visibility once there is room for it.

### Work Unit Evidence
| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test ./internal/tui/... -run TestModelTrivyRepositoryAlertsDetailShowsFindingsTableOnReasonablyTallTerminal -v` → PASS. Full package: `go test ./internal/tui/... -v` → all PASS, 0 failures. |
| Runtime harness command/scenario and exact result | `go test ./...` (full repo, all packages) → all `ok`/no test files, 0 failures. `go vet ./...` → clean. `gofmt -l .` → empty (clean). |
| Rollback boundary | Two files: `internal/tui/admin_tables.go` (new `trivyDetailFixedLines`/`trivyScreenChromeLines` constants, new `trivyAlertDetailTableRoles` function, and the `TrivyAlertDetailOpen` branch in `rebuildAdminTables`) and `internal/tui/model_test.go` (one new test). Reverting both files fully restores prior (buggy) behavior with no impact on unrelated code. |

### Changed lines
`git diff --shortstat internal/tui/admin_tables.go internal/tui/model_test.go`
→ 2 files changed, 128 insertions(+), 2 deletions(-) = **128 changed lines**
(well under the 250-line attempt-token budget).

### Deviations from design
None — implementation matches the orchestrator-provided fix design exactly:
ScanRuns collapses to a compact-scale pageSize when
`TrivyAlertDetailOpen` is true, Findings gets a computed larger pageSize
than the fixed `layout.Compact`, and the split arithmetic is grounded in
real measured/documented row counts rather than guessed constants. The
non-detail-open path (Features screen, ScanRuns without detail) is
unchanged, preserving `TestModelTrivyRepositoryAlertsScreenFitsViewportHeight`
and `TestRebuildAdminTablesBakesPrimaryAndCompactPageSizeIntoTables`.

### Issues found
None beyond the reported bug itself.

### Status
Regression fix complete and verified locally (unit/integration tests,
`go vet`, `gofmt`). Commits not yet created for this batch — orchestrator to
decide on committing/pushing per the session preflight instructions (RED
then GREEN, conventional commits, no AI attribution). Ready for independent
verification (`sdd-verify` or orchestrator review).
