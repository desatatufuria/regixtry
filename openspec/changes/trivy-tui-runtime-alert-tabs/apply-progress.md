# Apply Progress: Trivy TUI Runtime Alert Tabs

## Change

- Change: `trivy-tui-runtime-alert-tabs`
- Mode: Strict TDD
- Delivery mode: single PR (`size:exception` accepted)
- Review boundary: Trivy runtime tabs, modal-only config editing, repository-alert drill-down, docs, and apply evidence

## Completed Tasks

- [x] 1.1 RED: update `internal/app/regixtry/service_test.go` and `internal/tui/admin_client_test.go` for Trivy runtime-only page shape and `ListScanRuns` request/query/decoding
- [x] 1.2 GREEN: add `ListScanRuns` in `internal/tui/admin_client.go` and narrow `internal/app/regixtry/feature_registry.go` so Trivy page stays runtime/config/action focused
- [x] 2.1 RED: add `internal/tui/model_test.go` cases for default `Runtime` tab, non-Trivy no-tabs fallback, config modal open/cancel/submit, and unsupported fields staying hidden
- [x] 2.2 GREEN: extend `internal/tui/session.go` with Trivy-only tab/modal draft state and update `internal/tui/model.go` for tab switching, draft edits, submit, and reload
- [x] 2.3 REFACTOR: update `internal/tui/admin_views.go` to render tabs and modal without regressing the generic feature shell
- [x] 3.1 RED: add `internal/tui/model_test.go` cases for scan-run loading, empty-state recovery, repository selection, Enter drill-down, and Esc return inside Trivy
- [x] 3.2 GREEN: wire `internal/tui/model.go` to load scan runs on `Repository Alerts`, track selection/detail state, and keep operators on the Trivy screen
- [x] 3.3 REFACTOR: render repository alert list/detail in `internal/tui/admin_views.go` from `ports.ScanRun` only; do not parse `FeatureRow` text
- [x] 4.1 Update `docs/tui.md` with the Runtime/Repository Alerts split, modal-only config scope, and explicit deferrals
- [x] 4.2 During apply, run focused package tests for each RED->GREEN step, then `go test ./...`; carry the final evidence into `openspec/changes/trivy-tui-runtime-alert-tabs/verify-report.md` in verify

## Files Changed

| File | Action | What changed |
| --- | --- | --- |
| `internal/app/regixtry/feature_registry.go` | Modified | Narrowed the Trivy backend page to config/runtime sections only |
| `internal/app/regixtry/service_test.go` | Modified | Replaced ordered multi-section expectations with runtime-only page assertions |
| `internal/tui/admin_client.go` | Modified | Added `ListScanRuns` over `/admin/v1/scan-runs` with repository and limit query handling |
| `internal/tui/admin_client_test.go` | Modified | Added scan-run route/query/decoding coverage |
| `internal/tui/session.go` | Modified | Added Trivy-only tab, config modal, and alert detail state |
| `internal/tui/model.go` | Modified | Added Trivy tab switching, config modal submit/cancel, scan-run loading, and detail toggles |
| `internal/tui/model_test.go` | Modified | Added Trivy runtime/config and repository-alert behavior coverage |
| `internal/tui/admin_views.go` | Modified | Rendered Trivy tabs, config modal overlay, and scan-run-backed alert detail |
| `docs/tui.md` | Modified | Documented the Trivy tab split, modal-only settings scope, and explicit deferrals |
| `openspec/changes/trivy-tui-runtime-alert-tabs/tasks.md` | Modified | Marked apply tasks complete and recorded the resolved size exception |
| `openspec/changes/trivy-tui-runtime-alert-tabs/apply-progress.md` | Created | Recorded cumulative strict-TDD apply evidence for verify handoff |

## TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1.1 | `internal/app/regixtry/service_test.go`, `internal/tui/admin_client_test.go` | Unit / Integration | ✅ `go test -count=1 ./internal/app/regixtry ./internal/tui -run 'TestServiceGetFeaturePageBuildsOrderedTrivySectionsAndDeclaredActions|TestHTTPAdminClientFeaturePageAndActionRoutes|TestModelFeatureViewRendersGenericPageAndAllowsDeclaredAction|TestModelFeatureViewKeepsMinimalPagesUsable|TestModelFeatureSelectionRefreshesPageAndHelpFromBackendActions'` → exit 0; `ok regixtry/internal/app/regixtry 0.068s`, `ok regixtry/internal/tui 0.044s` | ✅ `go test -count=1 ./internal/app/regixtry ./internal/tui -run 'TestServiceGetFeaturePageBuildsRuntimeOnlyTrivySectionsAndDeclaredActions|TestHTTPAdminClientListScanRuns'` failed with `client.ListScanRuns undefined` and Trivy section IDs still including `runs`, `vulnerabilities`, and `repository-alerts` | ✅ same focused command passed: `ok regixtry/internal/app/regixtry 0.071s`, `ok regixtry/internal/tui 0.043s` | ✅ repository-filtered query case + omitted-filter case | ✅ backend page kept narrow and the scan-run client stayed a thin seam |
| 1.2 | `internal/app/regixtry/service_test.go`, `internal/tui/admin_client_test.go` | Unit / Integration | ✅ same baseline | ✅ same RED command as 1.1 proved missing client method and stale page shape | ✅ same focused command passed after `ListScanRuns` and page narrowing landed | ✅ runtime-only page plus two HTTP query permutations | ✅ removed backend alert/runs parsing pressure from the page contract |
| 2.1 | `internal/tui/model_test.go` | Unit | ✅ same baseline | ✅ `go test -count=1 ./internal/tui -run 'TestModelTrivyFeatureDefaultsToRuntimeTabAndKeepsNonTrivyUntabbed|TestModelTrivyConfigModalOpenCancelAndSubmitCurrentSettingsOnly'` failed because the Trivy page still lacked `Repository Alerts` tab chrome and no `Edit Trivy Configuration` modal rendered | ✅ same focused command passed: `ok regixtry/internal/tui 0.049s` | ✅ Trivy default + non-Trivy fallback + modal open/cancel/submit + hidden unsupported fields | ✅ Trivy-only behavior stayed isolated from the generic feature shell |
| 2.2 | `internal/tui/model_test.go` | Unit | ✅ same baseline | ✅ same RED command as 2.1 proved missing tab/modal state transitions | ✅ same focused command passed after adding Trivy tab/modal state and submit flow | ✅ default runtime tab plus modal submit/cancel branches | ✅ modal parsing/validation stayed local to the Trivy feature flow |
| 2.3 | `internal/tui/model_test.go` | Unit | ✅ same baseline | ✅ same RED command as 2.1 covered missing tab and modal rendering | ✅ same focused command passed after view updates | ✅ tab labels, runtime rendering, and unsupported-field absence all asserted in view output | ✅ extracted Trivy rendering without changing non-Trivy behavior |
| 3.1 | `internal/tui/model_test.go` | Unit | ✅ same baseline | ⚠️ `go test -count=1 ./internal/tui -run 'TestModelTrivyRepositoryAlertsLoadSelectDetailAndRecoverEmptyState'` passed on first execution because repository-alert behavior already existed in the in-flight Trivy state refactor; no failing RED was observed for this late-added coverage | ✅ same focused command passed: `ok regixtry/internal/tui 0.033s` | ✅ alert list/detail subtest + empty-state recovery subtest | ✅ kept the new assertions as approval-style protection for the remaining alert work |
| 3.2 | `internal/tui/model_test.go` | Unit | ✅ same baseline | ⚠️ same note as 3.1; behavior was already present when the focused alert test landed | ✅ same focused command passed: `ok regixtry/internal/tui 0.033s` | ✅ selection move, Enter detail, Esc close, and empty-data recovery | ✅ scan-run loading stayed on the current Trivy screen with no new navigation stack |
| 3.3 | `internal/tui/model_test.go` | Unit | ✅ same baseline | ⚠️ same note as 3.1; render assertions passed immediately once written | ✅ same focused command passed: `ok regixtry/internal/tui 0.033s` | ✅ non-empty and empty alert rendering paths | ✅ alert rendering reads `ports.ScanRun` directly, not `FeatureRow` text |
| 4.1 | `docs/tui.md` | Docs | ➖ docs update | ✅ documentation still described backend-declared Trivy runs/vulnerability sections instead of the new Trivy-only tabs and modal scope | ✅ docs updated in English to match the shipped UX | ✅ feature-manager overview + key bindings + deferrals | ➖ none needed |
| 4.2 | `go test` evidence | Suite | ✅ focused package commands above | ✅ apply work was not complete until the full suite proved the changed TUI/backend contracts together | ✅ `go test -count=1 ./...` passed: `ok regixtry/cmd/regixtry 2.342s`, `ok regixtry/internal/app/auth 0.134s`, `ok regixtry/internal/app/regixtry 1.290s`, `ok regixtry/internal/app/scanning 0.058s`, `? regixtry/internal/domain/auth [no test files]`, `ok regixtry/internal/domain/regixtry 0.012s`, `ok regixtry/internal/infra/auth/postgres 0.290s`, `ok regixtry/internal/infra/install/linux 0.306s`, `ok regixtry/internal/infra/install/releases 0.028s`, `ok regixtry/internal/infra/metadata/sqlite 0.252s`, `ok regixtry/internal/infra/scanning/trivy 0.169s`, `ok regixtry/internal/infra/storage/fsblob 0.010s`, `ok regixtry/internal/ports 0.009s`, `ok regixtry/internal/protocol/http 1.329s`, `ok regixtry/internal/tui 0.077s` | ✅ focused feature tests plus full-suite gate | ➖ none needed |

## Test Summary

- Total tests written/updated: 5 focused Trivy scenarios across service, HTTP client, and TUI model layers
- Total focused commands passing: 4 focused commands and 1 full-suite command
- Layers used: Unit (`internal/app/regixtry`, `internal/tui`), Integration (`internal/tui/admin_client_test.go` HTTP route coverage), Suite (`go test ./...`)
- Approval tests: Partial — the late-added repository-alert coverage in task 3.1 passed immediately and is recorded above as approval-style protection rather than a clean RED
- Pure functions/helpers added: Trivy config parsing/validation helpers and tab/render helpers inside the TUI path

## Work Unit Evidence

| Work unit | Focused test command and exact result | Runtime harness command/scenario and exact result | Rollback boundary |
| --- | --- | --- | --- |
| WU1 — backend/runtime contract narrowing | `go test -count=1 ./internal/app/regixtry ./internal/tui -run 'TestServiceGetFeaturePageBuildsRuntimeOnlyTrivySectionsAndDeclaredActions|TestHTTPAdminClientListScanRuns'` → exit 0; `ok regixtry/internal/app/regixtry 0.071s`, `ok regixtry/internal/tui 0.043s` | N/A — contract slice only; no runtime boundary beyond package tests in this work unit | `internal/app/regixtry/feature_registry.go`, `internal/tui/admin_client.go`, related tests |
| WU2 — Trivy tab state and config modal | `go test -count=1 ./internal/tui -run 'TestModelTrivyFeatureDefaultsToRuntimeTabAndKeepsNonTrivyUntabbed|TestModelTrivyConfigModalOpenCancelAndSubmitCurrentSettingsOnly'` → exit 0; `ok regixtry/internal/tui 0.049s` | N/A — model/view tests are the runtime boundary for this Bubble Tea-only slice | `internal/tui/session.go`, `internal/tui/model.go`, `internal/tui/admin_views.go`, `internal/tui/model_test.go` runtime/modal paths |
| WU3 — repository alerts, docs, final suite | `go test -count=1 ./internal/tui -run 'TestModelTrivyRepositoryAlertsLoadSelectDetailAndRecoverEmptyState'` → exit 0; `ok regixtry/internal/tui 0.033s` | `go test -count=1 ./...` → exit 0; full suite passed across 15 package results with `internal/domain/auth` reporting `[no test files]` | repository-alert rendering/state, `docs/tui.md`, `openspec/changes/trivy-tui-runtime-alert-tabs/{tasks.md,apply-progress.md}` |

## Deviations from Design

None — implementation stayed within the Trivy-only tab/modal approach, used `ListScanRuns` for alert drill-down, and kept the generic feature shell intact for non-Trivy pages.

## Issues Found

- Task 3.1 coverage landed after part of the repository-alert behavior already existed in the same apply batch, so that row is preserved honestly as approval-style evidence instead of a clean RED-first cycle.

## Remaining Tasks

- [ ] `openspec/changes/trivy-tui-runtime-alert-tabs/verify-report.md` still needs to be authored in `sdd-verify` using this evidence.

## Status

10/10 apply tasks complete. Ready for `sdd-verify`.
