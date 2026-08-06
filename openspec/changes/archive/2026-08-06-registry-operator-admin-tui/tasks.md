# Tasks: Registry Operator Admin TUI

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 1050-1350 |
| 400-line budget risk | High |
| 1200-line budget risk | Medium |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 CLI+client/session -> PR 2 admin state+views -> PR 3 tests -> PR 4 docs |
| Delivery strategy | auto-forecast |
| Chain strategy | stacked-to-main |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Wire authenticated admin dependencies and session primitives | PR 1 | `cmd/registry/main.go`, `internal/tui/admin_client.go`, `internal/tui/session.go`, tests included |
| 2 | Add Bubble Tea login/admin navigation without mutation affordances | PR 2 | Base on PR 1; keep inspection flow intact while gating admin screens |
| 3 | Lock behavior with integration/UI tests | PR 3 | Base on PR 2; finish `model_test` and client integration tests |
| 4 | Close out architecture and roadmap alignment for the shipped read-only admin TUI path | PR 4 | Base on PR 3; documentation-only slice |

## Phase 1: Foundation / Bootstrap

- [x] 1.1 Extend `cmd/registry/main.go` and `cmd/registry/main_test.go` to accept/validate admin API base URL for `tui`, drop the local admin notice path, and pass inspection + admin seams into `tui.NewModel`.
- [x] 1.2 Create `internal/tui/admin_client.go` with `AdminClient`, login/list methods, bearer request helper, JSON decoding, and invalid-token normalization for `/auth/token` and `/admin/v1/*`.
- [x] 1.3 Create `internal/tui/session.go` and `internal/tui/session_test.go` for `AdminSession`, expiry checks, logout/reset helpers, and admin-cache clearing semantics.

## Phase 2: Core Admin UI

- [x] 2.1 Refactor `internal/tui/model.go` to add unauthenticated, authenticating, authenticated-admin, and expired-session states plus username/password inputs and submit/cancel actions.
- [x] 2.2 Update `internal/tui/model.go` to gate admin entry behind login, preserve existing inspection navigation, and show recoverable auth/expiry status banners.
- [x] 2.3 Update `internal/tui/model.go` to load read-only users, selected-user grants, and selected-user admin tokens through `AdminClient` with no create/enable/reset/revoke actions rendered.
- [x] 2.4 Add authenticated header/logout handling in `internal/tui/model.go` so logout or expiry clears in-memory session data and returns to login.

## Phase 3: Verification

- [x] 3.1 Create `internal/tui/admin_client_test.go` with `httptest` coverage for successful login, invalid credentials, list reads, and `401 invalid_token` expiry mapping.
- [x] 3.2 Extend `internal/tui/model_test.go` for spec scenarios: successful login, invalid credentials, unauthenticated admin blocking, read-only browsing, and expiry-driven relogin.
- [x] 3.3 Run `go test ./...`, then add/update any focused fixture helpers in `internal/tui/model_test.go` or `cmd/registry/main_test.go` needed to keep snapshot-style assertions stable.

## Phase 4: Documentation / Rollout

- [x] 4.1 Update `docs/architecture.md` to document the HTTP admin seam, in-memory session boundary, and removal of the local privileged TUI shortcut.
- [x] 4.2 Update `docs/roadmap.md` to mark this slice as authenticated, GET-only admin browsing and keep admin mutations explicitly out of scope.
