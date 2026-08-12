# Tasks: TUI Table Viewport Fixed Size

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~850-950 (additions + deletions) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 → PR 2 → PR 3 → PR 4 → PR 5 |
| Delivery strategy | single-pr |
| Chain strategy | feature-branch-chain (suggested — needs confirmation) |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: feature-branch-chain
400-line budget risk: High

Rationale: zero existing `WindowSizeMsg`/`Height`/`Viewport`/`PageSize` test coverage means every RED test is authored from scratch (est. 300+ new test lines alone); the `newAdminBubbleTable` signature change touches 5 build-site callers plus `rebuildAdminTables`, `session.go`, and `admin_views.go` threading a new `consoleLayout` param through ~15 render functions; `model.go`'s `View()` switch has 11 call sites needing layout threading. This mirrors the gitleaks-managed-feature precedent (~2.25x budget → 6 PRs); here the estimate is ~2.1-2.4x → 5 PRs.

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | `viewport.go` foundation: `consoleLayout`, `contentBudget()`, `fitLines()`, `renderSection()` | PR 1 (base: tracker) | `go test ./internal/tui/... -run Viewport` | N/A — pure unit, no runtime harness needed | Delete `viewport.go`/`viewport_test.go`; no other file references them yet |
| 2 | Resize capture, alt-screen, snapshot default, too-small guard | PR 2 (base: PR 1) | `go test ./internal/tui/... -run 'WindowSize\|TooSmall\|SnapshotDefault'` | `go run ./cmd/regixtry --snapshot` (deterministic size check) | Revert `model.go` `WindowSizeMsg` case + `main.go` `WithAltScreen()`; independent of PR 3-5 |
| 3 | Outer-pane containment for plain lists/text sections | PR 3 (base: PR 2) | `go test ./internal/tui/... -run 'ConsoleWorkspace\|ListSection\|PageKey'` | `scripts/tui-smoke.sh` (unchanged output at 100x40) | Revert `renderConsoleWorkspace`/`renderConsoleListSection` layout param; tables untouched |
| 4 | `newAdminBubbleTable` pageSize threading + primary/compact roles + Trivy stacking | PR 4 (base: PR 3) | `go test ./internal/tui/... -run 'AdminBubbleTable\|TrivyAlerts'` | N/A — headless table assertions cover the stacking case directly | Revert `admin_tables.go`/`session.go`/`admin_views.go` signature changes as one unit |
| 5 | Position indicator, theme assertions, resize-refit regression, full non-regression | PR 5 (base: PR 4) | `go test ./internal/tui/...` (full package) | `scripts/tui-smoke.sh` + manual resize via `tea.WithAltScreen()` session | Test-only PR; revert leaves prior PRs' behavior intact |

## Phase 1: Viewport Foundation (`internal/tui/viewport.go`)

- [x] 1.1 RED `viewport_test.go`: table-driven `contentBudget()` cases (status present/absent, help present/absent) — asserts chrome accounting
- [x] 1.2 GREEN `viewport.go`: `consoleLayout` struct, `contentBudget()`, constants (`minViewportWidth/Height=90/24`, `defaultViewportWidth/Height=100/40`, `tableChromeRows=6`, `sectionChromeRows=4`, `minTableRows/compactTableRows=3/5`)
- [x] 1.3 RED `viewport_test.go`: `fitLines()` clamping + indicator text (visible range/total; omitted when all rows fit)
- [x] 1.4 GREEN `viewport.go`: `fitLines()`, `renderSection()`
- [x] 1.5 RED `viewport_test.go`: invariant — every current screen's title/context/help render to exactly 1 line each (guards design risk #3's budget assumption)
- [x] 1.6 GREEN: fix any multi-line chrome 1.5 finds (none found — invariant passed immediately, no production fix needed)

## Phase 2: Resize Capture, Alt-Screen, Snapshot Default (`model.go`, `main.go`)

- [x] 2.1 RED `model_test.go`: `Update(tea.WindowSizeMsg{...})` sets `Model.viewport` width/height (Req: Live Terminal Resize Refit)
- [x] 2.2 GREEN `model.go`: add `viewport`, `bodyScroll` fields to `Model`; `tea.WindowSizeMsg` case in `Update()`
- [x] 2.3 RED `model_test.go`: `NewModel()` default viewport is `100x40` (decision #8, `--snapshot` fallback)
- [x] 2.4 GREEN `model.go`: `NewModel()` sets the default
- [x] 2.5 RED `model_test.go`: `View()` below `90x24` renders "Terminal too small" / "Regixtry needs at least 90x24..." message, no table/list (Req: Minimum Viable Terminal Size)
- [x] 2.6 GREEN `model.go`: size guard at the top of `View()` (design decision #7)
- [x] 2.7 RED `model_test.go`: resizing back above minimum restores normal rendering
- [x] 2.8 GREEN: confirm 2.6 satisfies 2.7 (passed immediately, no additional code needed — guard in 2.6 is stateless/dynamic)
- [x] 2.9 `main.go`: add `tea.WithAltScreen()` to `tea.NewProgram` (line ~2232); mechanical, verified via `scripts/tui-smoke.sh`
- [x] 2.10 (unlisted, added per orchestrator instruction) GREEN `model.go`: thin `func (m Model) contentBudget() consoleLayout` wrapper around Phase 1's package-level `contentBudget()`, matching design.md's originally specified method signature now that `Model.viewport` exists

## Phase 3: Outer-Pane Containment (`model.go`, `admin_theme.go`)

- [x] 3.1 RED `model_test.go`: catalog/tags list screens at 24/30/50 rows — `lipgloss.Height(View()) <= h` (Req: Internal Table and List Scrolling, "Catalog list scrolls within its section")
- [x] 3.2 GREEN `model.go`: `renderConsoleListSection`/`renderConsoleTextSection` take `consoleLayout`, route body through `renderSection`; update all 11 `View()` switch call sites (`renderConsoleWorkspace`/`renderInspectionWorkspace` left unchanged — body already fully rendered by the section builders before reaching them; see apply-progress deviations)
- [x] 3.3 RED `model_test.go`: PgUp/PgDn/Home/End scroll and clamp a long catalog list within its section
- [x] 3.4 GREEN `model.go`: page-key handling drives `bodyScroll`, clamps at bounds
- [x] 3.5 `admin_theme.go`: expose `borderColor` field (existing `#4C566A`); no new test here — covered by 5.4

## Phase 4: Table Height Budget and Roles (`admin_tables.go`, `session.go`, `admin_views.go`)

- [x] 4.1 RED (new) `admin_tables_test.go`: `newAdminBubbleTable(..., pageSize)` height identity `pageSize+6` (`tableChromeRows`)
- [x] 4.2 GREEN `admin_tables.go`: `newAdminBubbleTable` gains trailing `pageSize int` param; `WithNoPagination()`→`WithPageSize(pageSize).WithFooterVisibility(true)`; `WithBaseStyle(...).BorderForeground(theme.borderColor)`
- [x] 4.3 GREEN `admin_tables.go`: thread `pageSize` through the 5 build-site callers (`buildAdminFeaturesTable`, `buildAdminFeatureRowsTable`, `buildAdminScanRunsTable`, `buildAdminFindingsTable`, `buildAdminSecretFindingsTable`)
- [x] 4.4 RED `admin_tables_test.go`: `rebuildAdminTables` assigns primary (adaptive) vs compact (5 rows, floor 3) budgets per decision #6
- [x] 4.5 GREEN `session.go`: add `AdminViewState.Layout consoleLayout`; `admin_tables.go` `rebuildAdminTables(layout)` computes primary/compact split
- [x] 4.6 GREEN `admin_views.go`: `renderAdminWorkspace`/`renderAdminScreen` accept layout, thread into `renderSection` wrapping across affected screen renderers
- [x] 4.7 RED `model_test.go`: Trivy repository-alerts screen (4 stacked tables + ~20 fixed lines) at 24 rows — `lipgloss.Height(View()) <= 24`, none of the 3 findings tables spill into scrollback (Req: "Stacked Trivy alerts screen fits the viewport")
- [x] 4.8 GREEN: verify 4.5/4.6 satisfy 4.7; adjust compact-table floor if not (satisfied without adjustment — see apply-progress deviations)

## Phase 5: Indicator, Theme, Resize-Refit Regression, Non-Regression

- [x] 5.1 RED `model_test.go`: 47-row table with budget < 47 shows a position indicator with visible range/total (Req: Visible Position Indicator for Hidden Rows)
- [x] 5.2 RED `model_test.go`: table where all rows fit shows no misleading "hidden content" indicator
- [x] 5.3 GREEN: confirm native bubble-table footer (`CurrentPage`/`MaxPages`) plus `fitLines` indicator satisfy 5.1/5.2; patch gaps
- [x] 5.4 RED `model_test.go`: table border uses `theme.borderColor`; footer visible with page position (Req: Consistent Table Theme Styling)
- [x] 5.5 RED `model_test.go`: resize taller mid-session shows more rows without restart; resize shorter re-bounds without exceeding the new viewport (Req: Live Terminal Resize Refit, both scenarios)
- [x] 5.6 GREEN: confirm Phase 2 `WindowSizeMsg` wiring is sufficient; patch gaps
- [x] 5.7 Non-regression: `go test ./internal/tui/...` full suite + `scripts/tui-smoke.sh`; confirm the 5 pre-existing tables (features, feature-rows, scan-runs, findings, secret-findings) and gitleaks/trivy screens still render through `newAdminBubbleTable` unchanged in behavior
