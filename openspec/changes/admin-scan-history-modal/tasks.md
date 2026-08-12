# Tasks: Admin Scan History Modal

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~1,000–1,400 (prod ~550–700, tests ~450–700) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR1 pure logic → PR2 state/render (unwired) → PR3 wiring → PR4 removal sweep |
| Delivery strategy | single-pr |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

Rationale: new pure logic + property test, new modal render, new nav flow, key routing, 4+ site removal sweep, zero prior coverage — sized like tui-viewport/gitleaks, both of which needed chaining.

### Suggested Work Units

| Unit | Goal | PR | Focused test | Harness | Rollback boundary |
|------|------|----|--------------|---------|--------------------|
| 1 | Pure logic: summary agg, row-split, cycleIndex | PR1 | `go test ./internal/tui/... -run 'RowSplit\|Summarize\|CycleIndex'` | N/A, no UI wired | Delete `admin_scan_history.go`(+test) |
| 2 | Modal state/render, summary table, unwired | PR2 | `go test ./internal/tui/... -run 'ScanHistoryModal\|ScanSummaryTable'` | N/A, dead code path | Revert `session.go`/`admin_views.go`/`admin_tables.go` adds |
| 3 | Key routing, history nav, digest binding, containment | PR3 | `go test ./internal/tui/... -run 'ScanHistory'` | `tui-smoke.sh` scan-history scenario | Revert `model.go` wiring; old flow unaffected |
| 4 | Removal sweep + full regression | PR4 | `go test ./internal/tui/...` | `tui-smoke.sh` full run | Revert deletions; dead code harmless if restored |

## Phase 1: Pure Logic (RED → GREEN)

- [x] 1.1 RED `admin_scan_history_test.go`: `summarizeScanRunsByRepository` dedup, `FinishedAt`→`CreatedAt` fallback, in-progress marker, never blank
- [x] 1.2 GREEN: `repositorySummary`, `summarizeScanRunsByRepository` in new `internal/tui/admin_scan_history.go`
- [x] 1.3 RED: property test `adminScanHistoryRowSplit`, heights 10–80, invariant `(baseRows+4)+(modalRows+4)==outer.SectionRows+4`, degenerate collapse
- [x] 1.4 GREEN: `adminScanHistoryRowSplit`, `adminScanHistoryModalMinRows`
- [x] 1.5 RED+GREEN: `cycleIndex` wraps both directions (distinct from clamping `boundedIndex`)

## Phase 2: Modal State & Rendering (unwired)

- [x] 2.1 `session.go`: `AdminViewState.ScanHistoryModal`, `TrivySummaries`; add `adminScanHistoryTab`/`adminScanHistoryModal` types
- [x] 2.2 RED+GREEN: `buildAdminScanSummaryTable` (`admin_tables.go`) with last-execution column; modal page-size helper replacing `pageSize` constants
- [x] 2.3 RED: rendered-output test — modal never applies `fitLines` over a composite containing a bordered table
- [x] 2.4 GREEN: `renderAdminScanSummary`, `renderAdminScanHistoryModal` (`admin_views.go`), each bordered block clipped once by its owner

## Phase 3: Wiring — Keys & History Navigation

- [x] 3.1 RED+GREEN `runKey`: Enter on summary row opens modal (`loadAdminScanHistoryCmd`/`adminScanHistoryLoadedMsg`), no inline detail
- [x] 3.2 RED+GREEN `runKey`: Tab/Shift+Tab cycles tabs via `updateAdminScanHistoryModalKey`, gated in `updateAdminKey` before `updateAdminFeaturesKey`
- [x] 3.3 RED+GREEN `runKey`: Left/Right pages history, re-fires `loadAdminScanRunDetailCmd`→`loadAdminSecretScanFindingsCmd` per cursor
- [x] 3.4 RED: scenario — findings never leak across runs when paging (digest scoped to navigated run)
- [x] 3.5 GREEN: fix/confirm per-cursor digest binding
- [x] 3.6 RED+GREEN `runKey`: Esc closes modal, restores row-list focus, no residual detail
- [x] 3.7 RED: boundary — `lipgloss.Height(View()) <= viewport.Height`, heights 24–60, modal open with a full findings page (mirrors `TestModelTrivyRepositoryAlertsScreenFitsViewportHeight`)
- [x] 3.8 GREEN: fix sizing until boundary test passes

## Phase 4: Removal Sweep

- [ ] 4.1 `session.go`: remove `TrivyAlertDetailOpen`, `TrivyScanRunDetail`, `SecretFindings`
- [ ] 4.2 `model.go`: remove `isTrivyAlertsDetailOpen`; strip inline-detail branches from `toggleTrivyTab`, `updateAdminFeaturesKey`
- [ ] 4.3 `admin_views.go`: remove inline detail from `renderTrivyRepositoryAlerts`; fold `renderSecretFindingsBody` into the Leaks tab
- [ ] 4.4 `admin_tables.go`: remove `trivyAlertDetailTableRoles`, `trivyDetailFixedLines`, `trivyScreenChromeLines`; update `rebuildAdminTables` call sites
- [ ] 4.5 Search remaining references to every removed identifier; fix any missed call site

## Phase 5: Non-Regression

- [ ] 5.1 `go test ./internal/tui/...` full suite green; `tui-smoke.sh` passing
- [ ] 5.2 State disposition of `TestModelTrivyRepositoryAlertsScreenFitsViewportHeight` and the findings-visibility regression test: pass unchanged or note superseding tests
