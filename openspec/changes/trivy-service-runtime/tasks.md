# Tasks: Trivy Service Runtime

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 950-1350 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 contract+store, PR 2 probe+scan runtime, PR 3 CLI/admin/TUI/docs |
| Delivery strategy | single-pr |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Service config contract, SQLite migration, legacy import bridge | PR 1 | `go test ./internal/app/regixtry ./internal/infra/metadata/sqlite ./internal/infra/install/linux` | `go test ./cmd/regixtry -run 'TestRunSetup|TestFeatureConfigure'` | `internal/ports`, `internal/app/regixtry`, `internal/infra/metadata/sqlite`, `internal/infra/install/linux` |
| 2 | HTTP probe + scan execution with registry-reachable target rules | PR 2 | `go test ./internal/infra/scanning/trivy ./internal/app/regixtry` | `go test ./cmd/regixtry -run 'TestFeatureStatus|TestFeatureScan'` | `internal/infra/scanning/trivy`, scan orchestration paths |
| 3 | CLI/admin/TUI/docs projection and full verification | PR 3 | `go test ./cmd/regixtry ./internal/protocol/http ./internal/tui` | `go test ./...` | `cmd/regixtry`, `internal/protocol/http`, `internal/tui`, docs |

## Phase 1: Contract and RED Safety Net

- [x] 1.1 RED: extend `internal/app/regixtry/service_test.go` for `service_url` scheme validation, `registry_reachable_url` fallback rejection, legacy `README.sh` bridge safety, and runtime `external_service` projection.
- [x] 1.2 RED: add `internal/infra/metadata/sqlite/store_test.go` and `internal/infra/install/linux/{bootstrap,upgrade}_test.go` cases for additive service columns, auth redaction, one-way binary import, and existing service config winning.
- [x] 1.3 GREEN/REFACTOR: update `internal/ports/regixtry.go`, `internal/app/regixtry/{service_scanning.go,feature_registry.go}`, `internal/infra/metadata/sqlite/store.go`, and `internal/infra/install/linux/*.go` to make service fields authoritative.

## Phase 2: Service Runtime and Scan Execution

- [x] 2.1 RED: rewrite `internal/infra/scanning/trivy/runner_test.go` for `/healthz` + `/version` probing, bearer auth, TLS CA/insecure modes, scan request shaping, and no local subprocess execution.
- [x] 2.2 RED: extend `internal/app/regixtry/service_test.go` for scanner-facing target building, loopback-public-URL rejection, and ready only after health plus version succeed.
- [x] 2.3 GREEN/REFACTOR: replace `internal/infra/scanning/trivy/runner.go` with HTTP service transport and update `internal/app/regixtry/{feature_registry.go,service_scanning.go}` to use probe results and registry-reachable scan targets.

## Phase 3: Operator Surfaces

- [x] 3.1 RED: extend `cmd/regixtry/main_test.go` for `feature configure/status trivy`, deprecated setup bridge warnings, and service-only defaults; extend `internal/protocol/http/router_test.go` plus `internal/tui/{admin_client_test.go,model_test.go}` for auth-token write-only behavior and runtime/detail projection.
- [x] 3.2 GREEN/REFACTOR: update `cmd/regixtry/main.go`, `internal/protocol/http/admin_handlers.go`, and `internal/tui/{admin_client.go,admin_views.go,model.go}` to expose service fields, health/version detail, and migration messaging without echoing secrets.

## Phase 4: Verification and Docs

- [x] 4.1 Update `README.md`, `docs/`, and OpenSpec notes with localhost-container vs remote-service examples, `registry_reachable_url`, probe expectations, and deprecated binary import guidance.
- [x] 4.2 Verify focused RED→GREEN package loops per task, then finish with `go test ./...`.
