# Tasks: Optional Trivy Rescans

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 900-1300 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 -> PR 2 -> PR 3 |
| Delivery strategy | single-pr |
| Chain strategy | not selected (single-pr override) |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: not selected (single-pr override)
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Setup defaults + SQLite scan persistence | PR 1 | `go test ./internal/infra/install/linux ./internal/infra/metadata/sqlite` | `go test ./cmd/regixtry -run TestRunSetup` | `internal/infra/install/linux/*`, `internal/infra/metadata/sqlite/*`, `internal/ports/*` |
| 2 | Trivy runner + scan app service + admin API/manual history | PR 2 | `go test ./internal/infra/scanning/trivy ./internal/app/regixtry ./internal/protocol/http` | `go test ./internal/protocol/http -run TestRouterAdminScan` | `internal/infra/scanning/trivy/*`, `internal/app/regixtry/*`, `internal/protocol/http/*` |
| 3 | Scheduler recovery + docs/verify updates | PR 3 | `go test ./internal/app/scanning ./cmd/regixtry` | `docs/verification/scripts/install-release-smoke.sh` | `internal/app/scanning/*`, `cmd/regixtry/*`, `docs/*`, `README.md` |

## Phase 1: Foundation

- [x] 1.1 RED: add `internal/infra/install/linux/{bootstrap,provenance}_test.go` cases for disabled defaults, bounded setup input, provenance recording, and truthful uninstall cleanup.
- [x] 1.2 GREEN: extend `internal/infra/install/linux/{bootstrap.go,templates.go,provenance.go}` and `cmd/regixtry/main.go` to capture optional Trivy env/defaults without enabling rescans by default.
- [x] 1.3 RED: add `internal/infra/metadata/sqlite/store_test.go` cases for `scan_settings`, `scan_runs`, and `scan_scheduler_state`, covering default-disabled reads plus completed/failed run persistence across reopen.
- [x] 1.4 GREEN: extend `internal/ports/{regixtry.go,auth.go}` and `internal/infra/metadata/sqlite/store.go` with scan settings/run/scheduler contracts and additive schema/bootstrap CRUD.

## Phase 2: Scan Execution Service

- [x] 2.1 RED: create `internal/infra/scanning/trivy/runner_test.go` for explicit argv, `exec.CommandContext` use, timeout, non-zero exit, malformed JSON, and context cancellation.
- [x] 2.2 GREEN: create `internal/infra/scanning/trivy/runner.go` to build fixed Trivy commands, parse JSON summaries, and persist scanner/db freshness evidence.
- [x] 2.3 RED: add `internal/app/regixtry/service_test.go` cases for settings bounds, tag-to-digest manual trigger queuing, unpublished-target rejection, and publish remaining non-blocking while runs exist.
- [x] 2.4 GREEN/REFACTOR: add scan orchestration in `internal/app/regixtry/` to resolve manifests, dedupe active digest runs, and expose latest/history queries.

## Phase 3: Admin Surfaces

- [x] 3.1 RED: extend `internal/protocol/http/router_test.go` for `GET/PUT /admin/v1/scan-settings`, `POST/GET /admin/v1/scan-runs`, auth enforcement, and backend-authoritative error/status payloads.
- [x] 3.2 GREEN: extend `internal/protocol/http/admin_handlers.go` and related admin services for scan settings, manual triggers, history, and latest-status routes.

## Phase 4: Scheduler + Verification

- [x] 4.1 RED: create `internal/app/scanning/scheduler_test.go` for single-owner lease, stale-state reclaim, bounded concurrency, and overlap prevention before implementing `internal/app/scanning/scheduler.go` plus `cmd/regixtry/main.go` wiring/shutdown.
- [x] 4.2 GREEN/REFACTOR: wire startup scheduler ownership, heartbeat recovery, and settings-aware periodic batches without blocking publish flows.
- [x] 4.3 Update `README.md`, `docs/`, and verification guidance with optional rescan setup/admin API expectations and deferred TUI follow-up notes; finish with `go test ./...`.

## Deferred Follow-up (Not In This Slice)

- Rich TUI settings/history/manual rescan actions on top of the admin API from this change.
- Broader UX improvements and non-essential observability polish after backend behavior is verified.
