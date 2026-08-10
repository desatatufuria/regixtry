# Tasks: Feature Runtime Operator UX

## Review Workload Forecast

| Field | Value |
|---|---|
| Estimated changed lines | 650-900 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | Runtime projection/progress -> CLI/TUI UX -> docs/verification |
| Delivery strategy | single-pr |
| Chain strategy | size-exception |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: size-exception
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|---|---|---|---|---|---|
| 1 | Add runtime DTO/progress seams and latest-version projection | Single PR | `go test ./internal/app/regixtry ./internal/infra/scanning/trivy ./internal/protocol/http/...` | `regixtry feature status trivy` against temp managed runtime | `internal/app/regixtry`, `internal/infra/scanning/trivy`, `internal/ports` |
| 2 | Render CLI progress/table/status and thin TUI actions/help | Single PR | `go test ./cmd/regixtry ./internal/tui` | `regixtry feature list` plus TUI feature refresh/action flow | `cmd/regixtry`, `internal/tui` |
| 3 | Refresh docs and verification guidance | Single PR | `go test ./...` | `docs/verification/scripts/tui-smoke.sh` notes only | `docs/`, `openspec/changes/feature-runtime-operator-ux` |

## Phase 1: RED Foundation

- [x] 1.1 In `internal/app/regixtry/service_test.go`, add RED cases for current/latest/update projection, `unknown` fallback, and preserved `/tmp/README.sh` migration-required safety.
- [x] 1.2 In `internal/infra/scanning/trivy/runtime_manager_test.go`, add RED coverage for staged install/upgrade callbacks and latest-release lookup reuse/failure handling.
- [x] 1.3 In `cmd/regixtry/main_test.go`, add RED coverage for staged install/upgrade output, truthful failure stop, `feature list` table columns, and `feature status` version/update fields.
- [x] 1.4 In `internal/tui/model_test.go`, add RED state/help tests for valid install/upgrade/rollback/enable/disable keys, unavailable-key guidance, and refresh-after-action behavior.

## Phase 2: Runtime Projection and Progress

- [x] 2.1 Extend `internal/ports/regixtry.go` and `internal/app/regixtry/service.go` with latest-version/update DTO fields plus an optional runtime progress callback seam.
- [x] 2.2 Update `internal/infra/scanning/trivy/runtime_manager.go` and `releases.go` to emit coarse stages, reuse `ResolveRelease("")`, and return safe lookup failures without changing managed-runtime authority.
- [x] 2.3 Update `internal/app/regixtry/feature_registry.go` and any affected HTTP projections to resolve current/latest/update only on explicit reads and keep request-scoped behavior.

## Phase 3: CLI and TUI UX

- [x] 3.1 Update `cmd/regixtry/main.go` to print staged install/upgrade progress, fixed-width `feature list` tables, and richer `feature status` output; keep final success/failure truthful.
- [x] 3.2 Update `internal/tui/model.go` with a pure feature-action availability helper and thin action handlers that refresh list/status after install, upgrade, rollback, enable, and disable.
- [x] 3.3 Update `internal/tui/admin_views.go` and `internal/tui/admin_client.go` so help text only advertises valid keys and the feature screen surfaces start/end result messaging without new flows.

## Phase 4: Verification and Docs

- [x] 4.1 Run GREEN/REFACTOR passes until `go test ./...` stays green; keep RED-first task order intact in changed tests.
- [x] 4.2 Update `docs/cli.md`, `docs/installation.md`, and `docs/verification/phase-4-operator-console.md` for progress output, version columns, and feature-screen actions/help.
- [x] 4.3 Update `openspec/changes/feature-runtime-operator-ux/design.md` verification notes if implementation narrows scope, and document any manual `feature list`/TUI smoke expectations under `docs/verification/scripts/` usage.
