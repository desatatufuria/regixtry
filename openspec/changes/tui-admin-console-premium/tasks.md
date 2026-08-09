# Tasks: TUI Admin Console Premium

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 750-1100 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 -> PR 2 -> PR 3 |
| Delivery strategy | single-pr |
| Chain strategy | pending |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Extend admin client/session contracts for create/reset/grant/token flows | PR 1 | `go test ./internal/tui -run 'TestHTTPAdminClient'` | `go test ./cmd/regixtry -run TestRunTUISnapshot` | `internal/tui/admin_client*.go`, `internal/tui/session.go` |
| 2 | Add premium theme, sidebar, forms, selected-user gating, and confirm modals | PR 2 | `go test ./internal/tui -run 'TestAdminWorkspace|TestAdminMutations'` | `docs/verification/scripts/tui-smoke.sh` | `internal/tui/model.go`, `internal/tui/admin_theme.go`, `internal/tui/admin_views.go` |
| 3 | Finish snapshots, failure persistence, and docs polish | PR 3 | `go test ./internal/tui ./cmd/regixtry` | `docs/verification/scripts/tui-smoke.sh` | `internal/tui/model_test.go`, `cmd/regixtry/main.go` |

## Phase 1: Foundation

- [x] 1.1 Extend `internal/tui/session.go` with selected panel, create/reset/grant/token forms, confirm modal state, and one-time token-secret state.
- [x] 1.2 Add `internal/tui/admin_theme.go` with the required palette, status colors, borders, and reusable Lip Gloss styles.
- [x] 1.3 Extend `internal/tui/admin_client.go` and `internal/tui/admin_client_test.go` for create user, reset password, put/delete grant, create token, revoke token, and `201/204` handling.

## Phase 2: Core Workspace

- [x] 2.1 Refactor `internal/tui/model.go` admin routing into a sidebar workspace with users, grants, and tokens panels anchored to `SelectedUserID`.
- [x] 2.2 Create `internal/tui/admin_views.go` for sidebar, form, table, banner, and short confirmation modal render helpers.
- [x] 2.3 Implement create-user and reset-password submits in `internal/tui/model.go`; refresh users from backend only and never expose post-create `is_admin` edits or delete.
- [x] 2.4 Implement selected-user grant add/change/remove and token create/revoke flows in `internal/tui/model.go`; keep controls blocked with no selection and reveal token secrets once.
- [x] 2.5 Keep fail-closed behavior in `internal/tui/model.go`: validation errors retain the active form, invalid token returns to `screenAdminLogin`, and writes re-fetch backend state.

## Phase 3: Verification

- [x] 3.1 Expand `internal/tui/model_test.go` for workspace shell rendering, recoverable failures, no-selection grant blocking, and unauthenticated admin access blocking.
- [x] 3.2 Expand `internal/tui/model_test.go` for create-admin-at-creation, confirmed enable/disable, reset password, contextual grant writes, token revoke confirmation, and one-time secret clearing.
- [x] 3.3 Update `cmd/regixtry/main.go` and `cmd/regixtry/main_test.go` only as needed to keep TUI wiring isolated and snapshot/smoke coverage passing.

## Phase 4: Cleanup

- [x] 4.1 Remove obsolete admin-only render branches in `internal/tui/model.go` once `admin_views.go` owns the premium layout.
- [x] 4.2 Recheck `openspec/changes/tui-admin-console-premium/{design.md,tasks.md}` against the final scope and verification commands.
