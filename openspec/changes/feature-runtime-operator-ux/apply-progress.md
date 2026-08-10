# Apply Progress: Feature Runtime Operator UX

## Implementation Progress

**Change**: `feature-runtime-operator-ux`
**Mode**: Strict TDD
**Delivery**: single-pr with accepted `size:exception`

### Completed Tasks
- [x] 1.1 RED service projection tests for current/latest/update and unknown fallback
- [x] 1.2 RED runtime manager tests for staged callbacks and latest lookup reuse
- [x] 1.3 RED CLI tests for staged output, truthful failure, table rendering, and status fields
- [x] 1.4 RED TUI tests for valid actions, unavailable guidance, and refresh-after-action flow
- [x] 2.1 Added DTO fields and runtime progress seam
- [x] 2.2 Emitted staged install/upgrade progress and added latest-version lookup reuse
- [x] 2.3 Projected current/latest/update on explicit reads with unknown fallback
- [x] 3.1 Rendered staged CLI progress, tabular `feature list`, and richer `feature status`
- [x] 3.2 Added pure feature-action availability rules and thin runtime action handlers
- [x] 3.3 Made feature-screen help state-aware and kept result messaging in-screen
- [x] 4.1 Reached green on focused loops and `go test ./...`
- [x] 4.2 Updated CLI, installation, and verification docs
- [x] 4.3 Documented manual `feature list` / `feature status` / TUI smoke expectations

### Files Changed
| File | Action | What was done |
|---|---|---|
| `internal/ports/regixtry.go` | Modified | Added feature summary/runtime latest-version, update-state, and runtime progress DTO fields. |
| `internal/app/regixtry/service.go` | Modified | Extended the runtime-manager seam with progress callbacks and latest-version lookup. |
| `internal/app/regixtry/feature_runtime.go` | Modified | Added CLI-facing install/upgrade helpers that pass staged progress callbacks without widening admin flows. |
| `internal/app/regixtry/feature_registry.go` | Modified | Projected current/latest/update values on explicit reads with safe `unknown` fallback. |
| `internal/infra/scanning/trivy/runtime_manager.go` | Modified | Emitted coarse install/upgrade stages and reused release resolution for latest-version awareness. |
| `cmd/regixtry/main.go` | Modified | Added staged progress output, fixed-width feature table rendering, and richer status output. |
| `internal/tui/model.go` | Modified | Added pure feature-action availability rules, unavailable-action guidance, and post-mutation refresh flow. |
| `internal/tui/admin_views.go` | Modified | Rendered dynamic feature help plus current/latest/update fields in the feature screen. |
| `internal/app/regixtry/service_test.go` | Modified | Added RED-first projection and unknown-fallback coverage. |
| `internal/infra/scanning/trivy/runtime_manager_test.go` | Modified | Added RED-first staged progress and latest-version lookup tests. |
| `cmd/regixtry/main_test.go` | Modified | Added RED-first CLI progress, failure, and table/status coverage. |
| `internal/tui/model_test.go` | Modified | Added RED-first feature action/help/refresh coverage. |
| `docs/cli.md`, `docs/installation.md`, `docs/verification/phase-4-operator-console.md` | Modified | Documented staged progress, version columns, update-state reads, and TUI help expectations. |
| `openspec/changes/feature-runtime-operator-ux/tasks.md` | Modified | Marked all apply tasks complete. |

### Work Unit Evidence
| Unit | Focused test command and exact result | Runtime harness command and exact result | Rollback boundary |
|---|---|---|---|
| 1 | `go test ./internal/app/regixtry ./internal/infra/scanning/trivy ./internal/protocol/http/...` → `ok regixtry/internal/app/regixtry (cached)` / `ok regixtry/internal/infra/scanning/trivy (cached)` / `ok regixtry/internal/protocol/http (cached)` | `go test ./cmd/regixtry -run 'TestFeatureRuntimeLifecycleCommandsUseManagedRuntimeActions|TestRunFeatureCommandsManageBuiltInTrivyState'` → `ok regixtry/cmd/regixtry 0.277s` | `internal/ports`, `internal/app/regixtry`, `internal/infra/scanning/trivy`, `internal/protocol/http` |
| 2 | `go test ./cmd/regixtry ./internal/tui` → `ok regixtry/cmd/regixtry (cached)` / `ok regixtry/internal/tui (cached)` | `go test ./cmd/regixtry -run 'TestRunFeatureCommandsManageBuiltInTrivyState|TestFeatureRuntimeLifecycleCommandsUseManagedRuntimeActions|TestFeatureRuntimeLifecycleCommandStopsOnTruthfulFailure' && go test ./internal/tui -run 'TestModelFeatureViewShowsManagedRuntimeMigrationStateAndInstallAction|TestModelFeatureViewUnavailableActionShowsGuidance|TestModelFeatureRuntimeActionRefreshesListAndStatus'` → `ok regixtry/cmd/regixtry 0.369s` / `ok regixtry/internal/tui 0.049s` | `cmd/regixtry`, `internal/tui` |
| 3 | `go test ./...` → `ok regixtry/cmd/regixtry (cached)` / `ok regixtry/internal/app/auth (cached)` / `ok regixtry/internal/app/regixtry (cached)` / `ok regixtry/internal/app/scanning (cached)` / `ok regixtry/internal/domain/regixtry (cached)` / `ok regixtry/internal/infra/auth/postgres (cached)` / `ok regixtry/internal/infra/install/linux (cached)` / `ok regixtry/internal/infra/install/releases (cached)` / `ok regixtry/internal/infra/metadata/sqlite (cached)` / `ok regixtry/internal/infra/scanning/trivy (cached)` / `ok regixtry/internal/infra/storage/fsblob (cached)` / `ok regixtry/internal/ports 0.002s` / `ok regixtry/internal/protocol/http (cached)` / `ok regixtry/internal/tui (cached)` | `docs/verification/scripts/tui-smoke.sh` notes reviewed and aligned in `docs/verification/phase-4-operator-console.md`; no additional runtime script changes required | `docs/`, `openspec/changes/feature-runtime-operator-ux` |

### TDD Cycle Evidence
| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 1.1 | `internal/app/regixtry/service_test.go` | Unit | `go test ./internal/app/regixtry ./internal/protocol/http/...` → `ok` / `ok` | ✅ `go test ./internal/app/regixtry -run 'TestService(ListFeaturesReturnsBuiltinTrivyInventory|GetFeatureStatusFallsBackToUnknownLatestVersion|GetFeatureStatusReportsLegacyRuntimeMigrationRequired)'` → `FAIL` with missing `CurrentVersion` / `LatestVersion` / `UpdateStatus` fields | ✅ same focused command → `ok regixtry/internal/app/regixtry 0.191s` | ✅ list projection + ready fallback + migration-required safety | ✅ extracted read-side fallback helper and kept projection logic narrow |
| 1.2 | `internal/infra/scanning/trivy/runtime_manager_test.go` | Integration | `go test ./internal/infra/scanning/trivy` → `ok regixtry/internal/infra/scanning/trivy (cached)` | ✅ `go test ./internal/infra/scanning/trivy -run 'TestRuntimeManager(InstallAndUpgradeEmitProgressStages|LatestVersionUsesReleaseLookupAndPropagatesFailure)'` → `FAIL` with missing progress signature / `LatestVersion` method | ✅ same focused command → `ok regixtry/internal/infra/scanning/trivy 0.237s` | ✅ install stages + upgrade stages + latest success + latest failure | ✅ extracted `emitFeatureRuntimeProgress` helper |
| 1.3 | `cmd/regixtry/main_test.go` | Integration | `go test ./cmd/regixtry` → `ok regixtry/cmd/regixtry 3.043s` | ✅ `go test ./cmd/regixtry -run 'Test(RunFeatureCommandsManageBuiltInTrivyState|FeatureRuntimeLifecycleCommandsUseManagedRuntimeActions|FeatureRuntimeLifecycleCommandStopsOnTruthfulFailure)'` → `FAIL` with undefined runtime-progress DTOs | ✅ same focused command → `ok regixtry/cmd/regixtry 0.353s` | ✅ table columns + staged success + truthful failure + richer status fields | ✅ extracted table/progress writers |
| 1.4 | `internal/tui/model_test.go` | Integration | `go test ./internal/tui` → `ok regixtry/internal/tui (cached)` | ✅ `go test ./internal/tui -run 'TestModelFeature(ViewShowsManagedRuntimeMigrationStateAndInstallAction|ViewUnavailableActionShowsGuidance|FeatureRuntimeActionRefreshesListAndStatus)'` → `FAIL` with missing summary/runtime latest/update fields | ✅ same focused command → `ok regixtry/internal/tui 0.042s` | ✅ migration-required install help + unavailable upgrade guidance + refresh-after-action flow | ✅ extracted pure feature action availability helper |
| 2.1 | `internal/app/regixtry/service_test.go`, `cmd/regixtry/main_test.go` | Unit/Integration | Covered by package baselines | ✅ failing DTO/progress signature tests existed first | ✅ `go test ./internal/app/regixtry ./cmd/regixtry` → `ok regixtry/internal/app/regixtry (cached)` / `ok regixtry/cmd/regixtry (cached)` | ✅ DTO fields exercised through both service and CLI paths | ✅ progress stayed optional and CLI-only |
| 2.2 | `internal/infra/scanning/trivy/runtime_manager_test.go` | Integration | Covered by package baseline | ✅ staged callback and latest lookup tests added first | ✅ `go test ./internal/infra/scanning/trivy` → `ok regixtry/internal/infra/scanning/trivy 0.293s` | ✅ install + upgrade + lookup failure paths | ✅ reused `ResolveRelease("")` without adding background state |
| 2.3 | `internal/app/regixtry/service_test.go` | Unit | Covered by package baseline | ✅ projection tests failed before implementation | ✅ `go test ./internal/app/regixtry` → `ok regixtry/internal/app/regixtry 2.492s` | ✅ ready/current/latest/update + migration-required fallback cases | ✅ request-scoped projection kept inside existing registry seam |
| 3.1 | `cmd/regixtry/main_test.go` | Integration | Covered by package baseline | ✅ CLI output tests failed before implementation | ✅ `go test ./cmd/regixtry` → `ok regixtry/cmd/regixtry 3.132s` | ✅ table view + install/upgrade stages + truthful failure + status fields | ✅ stdlib `tabwriter` reused instead of new framework |
| 3.2 | `internal/tui/model_test.go` | Integration | Covered by package baseline | ✅ TUI action/help tests failed before implementation | ✅ `go test ./internal/tui` → `ok regixtry/internal/tui 0.059s` | ✅ enable/disable/install/upgrade availability and refresh behavior | ✅ pure availability helper kept key handling thin |
| 3.3 | `internal/tui/model_test.go` | Integration | Covered by package baseline | ✅ dynamic-help assertions existed before view changes | ✅ `go test ./internal/tui -run 'TestModelFeatureViewShowsManagedRuntimeMigrationStateAndInstallAction|TestModelFeatureViewUnavailableActionShowsGuidance|TestModelFeatureRuntimeActionRefreshesListAndStatus'` → `ok regixtry/internal/tui (cached)` | ✅ help text recomputed for migration-required, up-to-date, and post-upgrade states | ✅ reused existing admin screen, no new flow added |
| 4.1 | Changed Go tests | Integration | Focused loops were green before full run | ✅ final verify command chosen before the suite run | ✅ `go test ./...` → full repository pass | ✅ package loops plus full-suite confirmation | ✅ ran `gofmt -w` before final suite |
| 4.2 | Docs reviewed against changed behavior | Docs | N/A | ✅ doc gaps identified from failing/new CLI and TUI behavior tests | ✅ docs updated to match staged progress, version columns, and help behavior | ➖ single documentation behavior set | ✅ examples kept request-scoped and framework-free |
| 4.3 | Verification docs reviewed against script usage | Docs | N/A | ✅ manual smoke expectations identified before doc update | ✅ verification doc now states `feature list` / `feature status` / TUI help expectations | ➖ single documentation behavior set | ✅ no scope narrowing required; verification notes stayed additive |

### Test Summary
- **Total tests written/updated**: `internal/app/regixtry/service_test.go`, `internal/infra/scanning/trivy/runtime_manager_test.go`, `cmd/regixtry/main_test.go`, `internal/tui/model_test.go`
- **Total tests passing**: `go test ./...` passed for every package in the repository.
- **Layers used**: Unit and integration.
- **Approval tests**: None — this slice was behavior-extending rather than behavior-preserving refactoring.
- **Pure functions/helpers added**: feature action availability helper, feature table writer, and runtime progress emitter/writer helpers.

### Deviations from Design
None — implementation matches design.

### Issues Found
- Existing workspace changes were already present in `internal/app/regixtry/service_scanning.go`; this apply batch did not modify that file.

### Remediation Batch
- Verify finding addressed: `Thin Feature Mutation Flow / Failed cursor action stays thin and truthful`.
- Scope kept test-only: added executable TUI coverage in `internal/tui/model_test.go`; no production files changed.

#### Remediation Work Unit Evidence
| Evidence | Result |
|---|---|
| Focused test command and exact result | `go test ./internal/tui -run 'TestModelFeatureRuntimeActionFailureStaysOnFeatureScreen'` → `ok   regixtry/internal/tui 0.031s` |
| Runtime harness command/scenario and exact result | `go test ./internal/tui -run 'TestModelFeature(ViewShowsManagedRuntimeMigrationStateAndInstallAction|ViewUnavailableActionShowsGuidance|FeatureRuntimeActionRefreshesListAndStatus|RuntimeActionFailureStaysOnFeatureScreen)'` → `ok   regixtry/internal/tui 0.040s` |
| Rollback boundary | `internal/tui/model_test.go` only |

#### Remediation TDD Cycle Evidence
| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| verify remediation | `internal/tui/model_test.go` | Integration | `go test ./internal/tui -run 'TestModelFeature(ViewShowsManagedRuntimeMigrationStateAndInstallAction|ViewUnavailableActionShowsGuidance|FeatureRuntimeActionRefreshesListAndStatus)'` → `ok regixtry/internal/tui (cached)` | ✅ Added `TestModelFeatureRuntimeActionFailureStaysOnFeatureScreen` before any code changes to prove the previously unverified failure path | ✅ `go test ./internal/tui -run 'TestModelFeatureRuntimeActionFailureStaysOnFeatureScreen'` → `ok regixtry/internal/tui 0.031s` | ✅ Failure-path test now complements existing success and unavailable-action coverage in the same screen/action seam | ➖ None needed — production behavior already satisfied the spec; only executable proof was missing |

### Remaining Tasks
- [ ] None in apply scope.

### Workload / PR Boundary
- Mode: `size:exception`
- Current work unit: full change
- Boundary: runtime projection/progress + CLI/TUI UX + docs/verification
- Estimated review budget impact: above the normal 400-line guideline and explicitly approved for this run.

### Status
12/12 tasks complete. Ready for `sdd-verify`.
