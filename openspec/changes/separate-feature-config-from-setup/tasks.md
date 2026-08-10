# Tasks: Separate Feature Config From Setup

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 850-1150 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 generic feature model+lifecycle, PR 2 CLI/admin/TUI projections, PR 3 docs+smoke |
| Delivery strategy | single-pr |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Introduce generic feature registry and base-only lifecycle truth | PR 1 | `go test ./internal/app/regixtry ./internal/infra/install/linux` | `N/A` service+lifecycle seam | `internal/app/regixtry`, `internal/infra/install/linux` |
| 2 | Project the `trivy` feature across CLI/admin/TUI over `scan_settings` with Trivy status detail | PR 2 | `go test ./cmd/regixtry ./internal/protocol/http ./internal/tui` | `N/A` command/router scope | `cmd/regixtry`, `internal/protocol/http`, `internal/tui` |
| 3 | Prove migration and operator docs | PR 3 | `go test ./...` | `docs/verification/scripts/install-release-smoke.sh` | docs, smoke scripts, OpenSpec notes |

## Phase 1: Foundation

- [x] 1.1 RED: add `internal/app/regixtry/service_test.go` cases for registry lookup, `trivy` status projection, unknown-feature rejection, and import-if-missing when no authoritative row exists.
- [x] 1.2 GREEN: create `internal/app/regixtry/feature_registry.go`, update `internal/ports/regixtry.go`, and extend `internal/app/regixtry/service_scanning.go` with generic feature DTOs backed by `scan_settings` and Trivy as the first concrete feature.
- [x] 1.3 REFACTOR: centralize shared feature validation/status mapping so CLI, HTTP, and TUI consume one generic feature contract.

## Phase 2: Lifecycle Migration

- [x] 2.1 RED: add `internal/infra/install/linux/{bootstrap_test.go,provenance_test.go,upgrade_test.go}` coverage for base-only provenance, truthful uninstall reporting, legacy `--trivy-*` import, and existing feature state winning on replay.
- [x] 2.2 GREEN: update `internal/infra/install/linux/{bootstrap.go,templates.go,provenance.go,intent.go,upgrade.go}` so lifecycle owns only base bootstrap truth and imports legacy Trivy values only when `trivy` feature state is missing.
- [x] 2.3 REFACTOR: trim bootstrap intent/config helpers to base fields only and isolate temporary migration helpers for later flag removal.

## Phase 3: Operator Surfaces

- [x] 3.1 RED: extend `cmd/regixtry/main_test.go` for `feature list`, `show|status trivy`, `enable|disable trivy`, `configure trivy ...`, unknown-feature rejection, and Trivy health feedback under `status`.
- [x] 3.2 GREEN: update `cmd/regixtry/main.go` to route generic feature commands through the shared service contract while keeping legacy setup flags as migration-only inputs.
- [x] 3.3 RED: extend `internal/protocol/http/router_test.go` plus `internal/tui/{admin_client_test.go,session_test.go,model_test.go}` for backend-authoritative feature status reads, auth gating, and optional `trivy` mutations.
- [x] 3.4 GREEN: keep `/admin/v1/scan-settings` authoritative in `internal/protocol/http/admin_handlers.go` and add only thin feature projections in `internal/tui/{admin_client.go,session.go,model.go,admin_views.go}`.

## Phase 4: Verification and Docs

- [x] 4.1 RED/GREEN: update lifecycle smoke coverage for `setup -> feature configure trivy -> feature status trivy -> upgrade -> uninstall`, then prove the full repo with `go test ./...`.
- [x] 4.2 Update `README.md`, `docs/`, and OpenSpec notes to describe the generic feature model, `trivy` as the first concrete feature, legacy Trivy import, and deferred standalone doctor flows.
