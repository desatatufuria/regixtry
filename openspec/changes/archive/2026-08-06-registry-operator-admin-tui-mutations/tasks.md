# Tasks: Registry Operator Admin TUI Mutations

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 320-480 |
| 1200-line budget risk | Low |
| Chained PRs recommended | No |
| Suggested split | Single PR with 2 commit-sized work units |
| Delivery strategy | exception-ok |
| Chain strategy | pending |

Decision needed before apply: No
Chained PRs recommended: No
Chain strategy: pending
400-line budget risk: Medium

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Add client enable/disable contract and HTTP coverage | Single PR / commit 1 | `go test ./internal/tui -count=1 -run 'TestHTTPAdminClient'` | N/A - request mapping is fully covered by httptest client tests | `internal/tui/admin_client.go`, `internal/tui/admin_client_test.go` |
| 2 | Add users-screen confirmation, mutation flow, and model coverage | Single PR / commit 2 | `go test ./internal/tui -count=1 -run 'TestModel.*Admin|TestModel.*Disable|TestModel.*Enable'` | N/A - no admin mutation smoke harness exists; Bubble Tea model tests are the executable UX boundary | `internal/tui/model.go`, `internal/tui/model_test.go` |

## Phase 1: Client Foundation

- [x] 1.1 Extend `internal/tui/admin_client.go` `AdminClient` and `HTTPAdminClient` with `EnableUser` / `DisableUser` POST helpers for `/admin/v1/users/{id}:enable|:disable`.
- [x] 1.2 Add `internal/tui/admin_client_test.go` coverage for mutation success, backend conflict, validation failure, invalid-token expiry mapping, and local expired-session rejection.

## Phase 2: Users Screen Mutation Flow

- [x] 2.1 Add confirmation and in-flight mutation state/messages in `internal/tui/model.go` for `screenAdminUsers` only.
- [x] 2.2 Wire `e` / `d` to open confirmation, `enter` to submit, and `esc` / `n` to cancel without sending a request in `internal/tui/model.go`.
- [x] 2.3 Handle mutation completion in `internal/tui/model.go`: success status plus `ListUsers` refresh, backend conflict/validation as recoverable status, and expired-session fallback to admin login.
- [x] 2.4 Update `renderAdminUsers` in `internal/tui/model.go` to show confirmation/action hints while keeping grants and admin-token screens read-only.

## Phase 3: Model Verification

- [x] 3.1 Add `internal/tui/model_test.go` flow for confirmed disable/enable success that refreshes users and preserves selection by user ID when possible.
- [x] 3.2 Add `internal/tui/model_test.go` flow proving confirmation cancel/dismiss sends no mutation request.
- [x] 3.3 Add `internal/tui/model_test.go` flow for backend conflict and validation failures that keep the users screen stable with recoverable status.
- [x] 3.4 Add `internal/tui/model_test.go` flow for invalid/expired mutation responses that clear admin state and return to login with expiry feedback.

## Phase 4: Final Regression

- [x] 4.1 Run `go test ./internal/tui -count=1` after the mutation slice lands and fix any admin interface regressions in the touched TUI files.
- [x] 4.2 Run `go test ./...` to confirm no cross-package breakage from the expanded `AdminClient` contract.
