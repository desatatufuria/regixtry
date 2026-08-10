# Tasks: Managed Trivy Runtime

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 1100-1500 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | Unit 1 -> Unit 2 -> Unit 3 -> Unit 4 |
| Delivery strategy | single-pr |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Runtime schema, contracts, migration projection | PR 1 | `go test ./internal/infra/metadata/sqlite ./internal/app/regixtry -run TrivyRuntime` | N/A: storage/domain slice | Revert `internal/ports`, sqlite store, feature projection only |
| 2 | Managed download/verify/install/rollback engine | PR 2 | `go test ./internal/infra/scanning/trivy -run Runtime` | `go test ./cmd/regixtry -run TestFeatureRuntimeLifecycle` | Revert runtime manager/releases/CLI wiring |
| 3 | Scan execution + scheduler/batch integration | PR 3 | `go test ./internal/infra/scanning/trivy ./internal/app/regixtry ./internal/app/scanning -run Trivy|Scheduled` | managed scan against staged fake binary | Revert runner/service/scheduler changes |
| 4 | Admin/TUI/docs/verification | PR 4 | `go test ./internal/protocol/http ./internal/tui ./cmd/regixtry -run Feature` | TUI/admin status flow smoke | Revert HTTP/TUI/docs/verify assets |

## Phase 1: Foundation

- [x] 1.1 RED: add sqlite/app tests for `trivy_runtime_state` persistence, separated intent, and legacy `/tmp/README.sh` migration-required status in `internal/infra/metadata/sqlite/store_test.go` and `internal/app/regixtry/service_test.go`.
- [x] 1.2 GREEN: add `TrivyRuntimeState` contracts plus metadata-store methods in `internal/ports/regixtry.go` and project managed runtime truth from `internal/app/regixtry/feature_registry.go`.
- [x] 1.3 GREEN: implement sqlite schema/migration + CRUD for `trivy_runtime_state` and legacy-state reads in `internal/infra/metadata/sqlite/store.go`.

## Phase 2: Managed Runtime Lifecycle

- [x] 2.1 RED: add failing tests for checksum mismatch, staged activation success, pre-activation rollback, and retained previous version in `internal/infra/scanning/trivy/runtime_manager_test.go` and `releases_test.go`.
- [x] 2.2 GREEN: create `internal/infra/scanning/trivy/releases.go` for release resolution, checksum verification, extract helpers, and receipt payloads.
- [x] 2.3 GREEN: create `internal/infra/scanning/trivy/runtime_manager.go` for install/upgrade/rollback/status orchestration, atomic active-pointer swap, and receipt persistence.
- [x] 2.4 GREEN: wire `regixtry feature install|upgrade|rollback|status trivy` in `cmd/regixtry/main.go` and `cmd/regixtry/main_test.go` without changing `setup` ownership.

## Phase 3: Scan Execution Integration

- [x] 3.1 RED: add failing runner/service tests proving manual and scheduled scans use the active managed binary and never execute legacy paths in `internal/infra/scanning/trivy/runner_test.go`, `internal/app/regixtry/service_test.go`, and `internal/app/scanning/scheduler_test.go`.
- [x] 3.2 GREEN: replace HTTP probing/execution with managed binary health/run flow in `internal/infra/scanning/trivy/runner.go`.
- [x] 3.3 GREEN: update `internal/app/regixtry/service_scanning.go` and `feature_registry.go` to resolve runtime state, keep scan intent stable, and surface degraded or migration-required status truthfully.

## Phase 4: Operator Surfaces

- [x] 4.1 RED: add failing admin/CLI/TUI tests for separated intent/runtime state and truthful legacy degradation in `internal/protocol/http/admin_handlers_test.go`, `internal/tui/admin_client_test.go`, and `internal/tui/model_test.go`.
- [x] 4.2 GREEN: expose managed runtime actions/status in `internal/protocol/http/admin_handlers.go`, `internal/tui/admin_client.go`, and `internal/tui/model.go`.

## Phase 5: Verification and Docs

- [x] 5.1 REFACTOR: remove superseded service-runtime assumptions, keep focused package tests green, then run `go test ./...`.
- [x] 5.2 Update docs and verification assets for managed lifecycle/runtime status in `docs/` and `openspec/changes/managed-trivy-runtime/verify-report.md` inputs.
