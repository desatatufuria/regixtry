# Tasks: TUI Bubble Table Presentation

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 550-750 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 adapter/state/rendering → PR 2 severity/focus/docs/verify |
| Delivery strategy | single-pr |
| Chain strategy | pending |
| Maintainer size exception | Accepted |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High
Single-PR size exception: Accepted by maintainer for this run

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Add table adapter, dependency pin, and neutral admin table state/rendering | PR 1 | `go test ./internal/tui -run 'TestAdmin.*Table'` | N/A — model/view slice only | `go.mod`, `go.sum`, `internal/tui/admin_tables.go`, `internal/tui/session.go`, neutral rendering in `admin_views.go` |
| 2 | Wire severity styling, key ownership, docs, and full verification | PR 2 | `go test ./internal/tui -run 'TestAdmin.*(Severity|Keys|Actions)'` | N/A — no new runtime boundary | `internal/tui/model.go`, `internal/tui/admin_theme.go`, `internal/tui/model_test.go`, docs/verification notes |

## Phase 1: Foundation

- [x] 1.1 Confirm authoritative strict TDD instructions and compatible `bubble-table` version before coding; record the chosen version in `go.mod`/`go.sum` and note any blocker in `openspec/changes/tui-bubble-table-presentation/design.md` follow-up comments if needed.
- [x] 1.2 RED: add `internal/tui/model_test.go` cases for aligned feature/rows tables, empty-state preservation, and unchanged backend-authoritative values from `adminFeaturePageLoadedMsg`/`adminScanRunsLoadedMsg`.
- [x] 1.3 Create `internal/tui/admin_tables.go` with table builders, hidden row metadata helpers, and neutral column definitions for features, generic rows, scan runs, and findings.

## Phase 2: Presentation Wiring

- [x] 2.1 GREEN: extend `internal/tui/session.go` with presentation-only admin table state for features, feature rows, scan runs, findings, and selected-row metadata.
- [x] 2.2 GREEN: update `internal/tui/model.go` to rebuild table state from existing DTO load messages without changing `loadAdminFeaturePageCmd`, `loadAdminScanRunsCmd`, or `loadAdminScanRunDetailCmd` behavior.
- [x] 2.3 GREEN: replace plain joined-row rendering in `internal/tui/admin_views.go` with table rendering plus current empty-state/status messaging for feature summaries, `Kind == "rows"`, scan runs, and findings.

## Phase 3: Severity and Interaction

- [x] 3.1 RED: add `internal/tui/model_test.go` cases proving severity styling appears only for vulnerability findings and stays neutral for feature summaries, generic rows, and scan runs.
- [x] 3.2 GREEN: update `internal/tui/admin_theme.go` and `internal/tui/admin_tables.go` with theme-compatible severity styles for findings only.
- [x] 3.3 RED: add `internal/tui/model_test.go` cases proving table navigation coexists with `Esc`, `Tab`, refresh, detail, config, and action shortcuts in `updateAdminFeaturesKey`.
- [x] 3.4 GREEN/REFACTOR: update `internal/tui/model.go` key handling so screen-level shortcuts remain authoritative while active tables own row movement/filtering only.

## Phase 4: Verification and Docs

- [x] 4.1 REFACTOR: simplify duplicated table/view helpers across `internal/tui/admin_tables.go`, `internal/tui/admin_views.go`, and `internal/tui/model.go` after GREEN tests pass.
- [x] 4.2 Run `go test ./...` and record focused + full-suite results for the table scenarios in verification notes or follow-on `verify-report.md` inputs.
- [x] 4.3 Update change-local docs (`openspec/changes/tui-bubble-table-presentation/tasks.md` checkboxes during apply, plus verification notes if added) so severity scope, focus behavior, and single-PR budget risk remain explicit.
