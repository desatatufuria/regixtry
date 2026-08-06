# Apply Progress: Registry Operator Admin TUI Mutations

## Mode
- Standard

## Delivery
- Strategy: exception-ok
- Work unit: 2 — Add users-screen confirmation, mutation flow, and model coverage
- PR boundary: single PR, commit-sized slice 2 of 2

## Completed Tasks
- [x] 1.1 Extend `internal/tui/admin_client.go` `AdminClient` and `HTTPAdminClient` with `EnableUser` / `DisableUser` POST helpers for `/admin/v1/users/{id}:enable|:disable`.
- [x] 1.2 Add `internal/tui/admin_client_test.go` coverage for mutation success, backend conflict, validation failure, invalid-token expiry mapping, and local expired-session rejection.
- [x] 2.1 Add confirmation and in-flight mutation state/messages in `internal/tui/model.go` for `screenAdminUsers` only.
- [x] 2.2 Wire `e` / `d` to open confirmation, `enter` to submit, and `esc` / `n` to cancel without sending a request in `internal/tui/model.go`.
- [x] 2.3 Handle mutation completion in `internal/tui/model.go`: success status plus `ListUsers` refresh, backend conflict/validation as recoverable status, and expired-session fallback to admin login.
- [x] 2.4 Update `renderAdminUsers` in `internal/tui/model.go` to show confirmation/action hints while keeping grants and admin-token screens read-only.
- [x] 3.1 Add `internal/tui/model_test.go` flow for confirmed disable/enable success that refreshes users and preserves selection by user ID when possible.
- [x] 3.2 Add `internal/tui/model_test.go` flow proving confirmation cancel/dismiss sends no mutation request.
- [x] 3.3 Add `internal/tui/model_test.go` flow for backend conflict and validation failures that keep the users screen stable with recoverable status.
- [x] 3.4 Add `internal/tui/model_test.go` flow for invalid/expired mutation responses that clear admin state and return to login with expiry feedback.
- [x] 4.1 Run `go test ./internal/tui -count=1` after the mutation slice lands and fix any admin interface regressions in the touched TUI files.
- [x] 4.2 Run `go test ./...` to confirm no cross-package breakage from the expanded `AdminClient` contract.

## Files Changed
| File | Action | Notes |
|---|---|---|
| `internal/tui/admin_client.go` | Modified | Added enable/disable client methods and shared POST/GET request handling with preserved expiry/error decoding. |
| `internal/tui/admin_client_test.go` | Modified | Added httptest coverage for mutation success, conflict, validation, invalid-token, and local expiry cases. |
| `internal/tui/model.go` | Modified | Added users-screen confirmation state, mutation-in-flight behavior, backend-authoritative success/error handling, and refresh-after-success UX. |
| `internal/tui/model_test.go` | Modified | Added mutation success, in-flight, cancel, recoverable error, and expiry-to-login coverage for the users screen. |
| `openspec/changes/registry-operator-admin-tui-mutations/tasks.md` | Modified | Marked all remaining Phase 2-4 tasks complete for hybrid persistence. |

## Work Unit Evidence
### Work Unit 1
| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test ./internal/tui -count=1 -run 'TestHTTPAdminClient'` → `ok   registry/internal/tui  0.008s` |
| Runtime harness command/scenario and exact result | `N/A` — request mapping is fully covered by `httptest` admin client tests; this slice introduces no additional executable runtime boundary beyond the focused package tests. |
| Rollback boundary | Revert `internal/tui/admin_client.go`, `internal/tui/admin_client_test.go`, and the interface-compilation stubs added in `internal/tui/model_test.go` to remove the client mutation seam without touching the users-screen flow. |

### Work Unit 2
| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test ./internal/tui -count=1 -run 'TestModel.*Admin|TestModel.*Disable|TestModel.*Enable'` → `ok   registry/internal/tui  0.007s` |
| Runtime harness command/scenario and exact result | `N/A` — no admin mutation smoke harness exists; Bubble Tea model tests remain the executable users-screen boundary for this slice. |
| Rollback boundary | Revert `internal/tui/model.go`, `internal/tui/model_test.go`, and the matching task/apply artifacts to remove the users-screen confirmation/mutation flow without undoing the underlying admin client seam. |

## Regression Commands
- `go test ./internal/tui -count=1` → `ok   registry/internal/tui  0.012s`
- `go test ./...` → `ok   registry/cmd/registry  0.762s`; `ok   registry/internal/app/auth (cached)`; `ok   registry/internal/app/registry (cached)`; `?    registry/internal/domain/auth [no test files]`; `ok   registry/internal/domain/registry (cached)`; `ok   registry/internal/infra/auth/postgres (cached)`; `ok   registry/internal/infra/metadata/sqlite (cached)`; `ok   registry/internal/infra/storage/fsblob (cached)`; `ok   registry/internal/ports (cached)`; `ok   registry/internal/protocol/http (cached)`; `ok   registry/internal/tui  0.009s`

## Remaining Tasks
- None.

## Status
- 12/12 tasks complete
- Ready for verify
