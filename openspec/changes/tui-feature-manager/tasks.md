# Tasks: TUI Feature Manager

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 700-1000 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | Unit 1 -> Unit 2 -> Unit 3 |
| Delivery strategy | single-pr |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | DTOs, page assembly, list/page API | PR 1 | `go test ./internal/app/regixtry ./internal/protocol/http` | `go run ./cmd/regixtry feature list` and `feature status trivy` | `internal/ports`, `internal/app/regixtry`, `internal/protocol/http` |
| 2 | Bubble Tea shell, page rendering, typed actions | PR 2 | `go test ./internal/tui ./internal/protocol/http` | `go run ./cmd/regixtry tui -snapshot -api-base-url ...` | `internal/tui/*` |
| 3 | Docs, smoke updates, full verification | PR 3 | `go test ./...` | `docs/verification/scripts/tui-smoke.sh` | `docs/`, verification scripts, final task checks |

## Phase 1: Contracts and RED coverage

- [x] 1.1 RED: extend `internal/app/regixtry/service_test.go` for lightweight summaries, minimal feature pages, ordered sections, declared actions, and Trivy-only `config/runtime/runs/vulnerabilities/repository-alerts` sections.
- [x] 1.2 RED: extend `internal/protocol/http/router_test.go` for `GET /admin/v1/features/{name}` page payloads, `POST /admin/v1/features/{name}/actions/{actionID}`, and unknown feature/action failures.
- [x] 1.3 RED: extend `internal/tui/admin_client_test.go` and `internal/tui/model_test.go` for generic page decoding, minimal-page rendering, selection refresh, and backend-authoritative action help.

## Phase 2: Backend contracts and page assembly

- [x] 2.1 GREEN: replace `FeatureDetails` as the page DTO in `internal/ports/regixtry.go` with `FeaturePage`, `FeatureSection`, `FeatureField`, `FeatureRow`, and `FeatureAction`, keeping `FeatureSummary` lightweight.
- [x] 2.2 GREEN: refactor `internal/app/regixtry/feature_registry.go` and `internal/app/regixtry/service_scanning.go` to build feature pages plus richer Trivy runs, vulnerability, and repository-alert sections.
- [x] 2.3 GREEN: update `internal/protocol/http/admin_handlers.go` to return `FeaturePage` from `GET /admin/v1/features/{name}` and route typed feature actions through `POST /admin/v1/features/{name}/actions/{actionID}`.
- [x] 2.4 REFACTOR: remove obsolete fixed-detail response helpers and keep summary-list payloads stable across `/admin/v1/features` callers.

## Phase 3: Bubble Tea feature-manager shell

- [x] 3.1 GREEN: update `internal/tui/admin_client.go` to fetch `FeaturePage` and execute declared feature actions without local availability rules.
- [x] 3.2 GREEN: update `internal/tui/session.go` and `internal/tui/model.go` to store selected pages, focused actions, confirm declared mutations, and refresh summary list plus page after action completion.
- [x] 3.3 GREEN: update `internal/tui/admin_views.go` to render shared headers, field/row sections, empty minimal pages, and Trivy-rich sections inside the generic shell.
- [x] 3.4 REFACTOR: remove remaining Trivy-shaped status assumptions from feature help text, selection resets, and page-loading status messages.

## Phase 4: Documentation and verification

- [x] 4.1 Update `docs/tui.md` and `docs/verification/phase-4-operator-console.md` for generic feature pages, declared actions, and richer Trivy sections.
- [x] 4.2 Update `docs/verification/scripts/tui-smoke.sh` to assert the new feature-manager snapshot output and action/help expectations.
- [x] 4.3 Verify strict TDD completion with `go test ./...` and record results for `openspec/changes/tui-feature-manager/verify-report.md` handoff.
