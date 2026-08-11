# Tasks: Trivy TUI Runtime Alert Tabs

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 650-900 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | WU1 -> WU2 -> WU3 |
| Delivery strategy | single-pr |
| Chain strategy | pending |

Decision needed before apply: Resolved via `size:exception`
Chained PRs recommended: Yes
Chain strategy: not used (`single-pr` size exception accepted)
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Backend/runtime contract narrowing | PR 1 slice | `go test ./internal/app/regixtry ./internal/tui -run 'TestService.*FeaturePage|TestHTTPAdminClient.*ScanRuns'` | N/A - contract slice only | `internal/app/regixtry/feature_registry.go`, `internal/tui/admin_client*` |
| 2 | Trivy tab state and config modal | PR 2 slice | `go test ./internal/tui -run 'TestModel.*Trivy.*(Runtime|Config)'` | N/A - model/view tests prove UX flow | `internal/tui/{session.go,model.go,admin_views.go}` runtime tab/modal paths |
| 3 | Repository alerts drill-down, docs, final verify | PR 3 slice | `go test ./internal/tui -run 'TestModel.*Trivy.*Alert'` | `go test ./...` final gate | alert list/detail rendering plus `docs/tui.md` |

## Phase 1: Contracts and page shape

- [x] 1.1 RED: update `internal/app/regixtry/service_test.go` and `internal/tui/admin_client_test.go` for Trivy runtime-only page shape and `ListScanRuns` request/query/decoding.
- [x] 1.2 GREEN: add `ListScanRuns` in `internal/tui/admin_client.go` and narrow `internal/app/regixtry/feature_registry.go` so Trivy page stays runtime/config/action focused.

## Phase 2: Trivy runtime tab and modal

- [x] 2.1 RED: add `internal/tui/model_test.go` cases for default `Runtime` tab, non-Trivy no-tabs fallback, config modal open/cancel/submit, and unsupported fields staying hidden.
- [x] 2.2 GREEN: extend `internal/tui/session.go` with Trivy-only tab/modal draft state and update `internal/tui/model.go` for tab switching, draft edits, submit, and reload.
- [x] 2.3 REFACTOR: update `internal/tui/admin_views.go` to render tabs and modal without regressing the generic feature shell.

## Phase 3: Repository alerts drill-down

- [x] 3.1 RED: add `internal/tui/model_test.go` cases for scan-run loading, empty-state recovery, repository selection, Enter drill-down, and Esc return inside Trivy.
- [x] 3.2 GREEN: wire `internal/tui/model.go` to load scan runs on `Repository Alerts`, track selection/detail state, and keep operators on the Trivy screen.
- [x] 3.3 REFACTOR: render repository alert list/detail in `internal/tui/admin_views.go` from `ports.ScanRun` only; do not parse `FeatureRow` text.

## Phase 4: Docs and verification

- [x] 4.1 Update `docs/tui.md` with the Runtime/Repository Alerts split, modal-only config scope, and explicit deferrals.
- [x] 4.2 During apply, run focused package tests for each RED->GREEN step, then `go test ./...`; carry the final evidence into `openspec/changes/trivy-tui-runtime-alert-tabs/verify-report.md` in verify.
