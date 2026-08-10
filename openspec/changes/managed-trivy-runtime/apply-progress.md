# Apply Progress: managed-trivy-runtime

## Status

- Delivery mode: single PR with approved `size:exception`
- Strict TDD mode: active
- Progress: 14 / 14 tasks complete

## Completed Tasks

- [x] 1.1 RED: add sqlite/app tests for `trivy_runtime_state` persistence, separated intent, and legacy `/tmp/README.sh` migration-required status in `internal/infra/metadata/sqlite/store_test.go` and `internal/app/regixtry/service_test.go`.
- [x] 1.2 GREEN: add `TrivyRuntimeState` contracts plus metadata-store methods in `internal/ports/regixtry.go` and project managed runtime truth from `internal/app/regixtry/feature_registry.go`.
- [x] 1.3 GREEN: implement sqlite schema/migration + CRUD for `trivy_runtime_state` and legacy-state reads in `internal/infra/metadata/sqlite/store.go`.
- [x] 2.1 RED: add failing tests for checksum mismatch, staged activation success, pre-activation rollback, and retained previous version in `internal/infra/scanning/trivy/runtime_manager_test.go` and `releases_test.go`.
- [x] 2.2 GREEN: create `internal/infra/scanning/trivy/releases.go` for release resolution, checksum verification, extract helpers, and receipt payloads.
- [x] 2.3 GREEN: create `internal/infra/scanning/trivy/runtime_manager.go` for install/upgrade/rollback/status orchestration, atomic active-pointer swap, and receipt persistence.
- [x] 2.4 GREEN: wire `regixtry feature install|upgrade|rollback|status trivy` in `cmd/regixtry/main.go` and `cmd/regixtry/main_test.go` without changing `setup` ownership.
- [x] 3.1 RED: add failing runner/service tests proving manual and scheduled scans use the active managed binary and never execute legacy paths in `internal/infra/scanning/trivy/runner_test.go`, `internal/app/regixtry/service_test.go`, and `internal/app/scanning/scheduler_test.go`.
- [x] 3.2 GREEN: replace HTTP probing/execution with managed binary health/run flow in `internal/infra/scanning/trivy/runner.go`.
- [x] 3.3 GREEN: update `internal/app/regixtry/service_scanning.go` and `feature_registry.go` to resolve runtime state, keep scan intent stable, and surface degraded or migration-required status truthfully.
- [x] 4.1 RED: add failing admin/CLI/TUI tests for separated intent/runtime state and truthful legacy degradation in `internal/protocol/http/admin_handlers_test.go`, `internal/tui/admin_client_test.go`, and `internal/tui/model_test.go`.
- [x] 4.2 GREEN: expose managed runtime actions/status in `internal/protocol/http/admin_handlers.go`, `internal/tui/admin_client.go`, and `internal/tui/model.go`.
- [x] 5.1 REFACTOR: remove superseded service-runtime assumptions, keep focused package tests green, then run `go test ./...`.
- [x] 5.2 Update docs and verification assets for managed lifecycle/runtime status in `docs/` and `openspec/changes/managed-trivy-runtime/verify-report.md` inputs.

## Exact Test Evidence

- `go test ./internal/infra/metadata/sqlite -run 'TestStorePersistsTrivyRuntimeStateAcrossReopenAndDerivesLegacyMigrationState'` → `ok`
- `go test ./internal/app/regixtry -run 'TestServiceGetFeatureStatus(SeparatesIntentFromManagedRuntimeState|ReportsLegacyRuntimeMigrationRequired)'` → `ok`
- `go test ./internal/infra/scanning/trivy -run 'Test(VerifyArchiveChecksumRejectsMismatchesAndExtractsManagedBinary|RuntimeManager(InstallStagesActivationAndRetainsRollbackTarget|RestoresPreviousRuntimeWhenActivationProbeFails))'` → `ok`
- `go test ./cmd/regixtry -run 'TestFeatureRuntimeLifecycleCommandsUseManagedRuntimeActions'` → `ok`
- `go test ./internal/infra/scanning/trivy -run 'TestRunnerUsesManagedBinaryForProbeAndScanAndRejectsLegacyPaths'` → `ok`
- `go test ./internal/app/regixtry -run 'TestServiceManualAndScheduledScansUseManagedRuntimeStateAndIgnoreLegacyPaths'` → `ok`
- `go test ./internal/protocol/http -run 'TestAdminFeatureStatusSeparatesIntentFromManagedRuntimeAndShowsMigrationTruth'` → `ok`
- `go test ./internal/tui -run 'Test(HTTPAdminClientFeatureRoutes|ModelFeatureViewShowsManagedRuntimeMigrationStateAndInstallAction)'` → `ok`
- `go test ./...` → `ok`

## TDD Cycle Evidence

| Phase | RED | GREEN | REFACTOR |
|---|---|---|---|
| Runtime schema/contracts/migration projection | ✅ | ✅ | ✅ |
| Managed download/verify/install/rollback engine | ✅ | ✅ | ✅ |
| Scan execution + scheduler compatibility | ✅ | ✅ | ✅ |
| Admin/TUI/runtime surfaces | ✅ | ✅ | ✅ |
| Docs and verification updates | ✅ | ✅ | ✅ |

## Notes

- `scan_settings` now holds feature intent only; managed runtime lifecycle state lives separately in `trivy_runtime_state`.
- Legacy service or binary-oriented Trivy settings are treated only as migration evidence and never as execution authority.
- All planned tasks are complete and the change is ready for verify.
