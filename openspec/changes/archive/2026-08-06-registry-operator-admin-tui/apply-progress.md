# Apply Progress: Registry Operator Admin TUI

## Change

- `registry-operator-admin-tui`

## Mode

- Standard

## Completed Tasks

- [x] 1.1 Extend `cmd/registry/main.go` and `cmd/registry/main_test.go` to accept/validate admin API base URL for `tui`, drop the local admin notice path, and pass inspection + admin seams into `tui.NewModel`.
- [x] 1.2 Create `internal/tui/admin_client.go` with `AdminClient`, login/list methods, bearer request helper, JSON decoding, and invalid-token normalization for `/auth/token` and `/admin/v1/*`.
- [x] 1.3 Create `internal/tui/session.go` and `internal/tui/session_test.go` for `AdminSession`, expiry checks, logout/reset helpers, and admin-cache clearing semantics.
- [x] 2.1 Refactor `internal/tui/model.go` to add unauthenticated, authenticating, authenticated-admin, and expired-session states plus username/password inputs and submit/cancel actions.
- [x] 2.2 Update `internal/tui/model.go` to gate admin entry behind login, preserve existing inspection navigation, and show recoverable auth/expiry status banners.
- [x] 2.3 Update `internal/tui/model.go` to load read-only users, selected-user grants, and selected-user admin tokens through `AdminClient` with no create/enable/reset/revoke actions rendered.
- [x] 2.4 Add authenticated header/logout handling in `internal/tui/model.go` so logout or expiry clears in-memory session data and returns to login.
- [x] 3.1 Create `internal/tui/admin_client_test.go` with `httptest` coverage for successful login, invalid credentials, list reads, and `401 invalid_token` expiry mapping.
- [x] 3.2 Extend `internal/tui/model_test.go` for spec scenarios: successful login, invalid credentials, unauthenticated admin blocking, read-only browsing, and expiry-driven relogin.
- [x] 3.3 Run `go test ./...`, then add/update any focused fixture helpers in `internal/tui/model_test.go` or `cmd/registry/main_test.go` needed to keep snapshot-style assertions stable.
- [x] 4.1 Update `docs/architecture.md` to document the HTTP admin seam, in-memory session boundary, and removal of the local privileged TUI shortcut.
- [x] 4.2 Update `docs/roadmap.md` to mark this slice as authenticated, GET-only admin browsing and keep admin mutations explicitly out of scope.

## Files Changed

| File | Action | What Was Done |
| --- | --- | --- |
| `cmd/registry/main.go` | Modified | Added `-api-base-url` parsing/validation, removed the local admin notice branch, and wired the HTTP admin seam into `tui.NewModel`. |
| `cmd/registry/main_test.go` | Modified | Added CLI validation coverage for admin API base URLs and updated auth-enabled snapshot expectations to remove the local admin shortcut notice. |
| `internal/tui/admin_client.go` | Created | Added `AdminClient`, HTTP login/read methods, bearer request helpers, and invalid-token expiry normalization. |
| `internal/tui/admin_client_test.go` | Created | Added `httptest` coverage for login, admin GET reads, backend invalid-credential handling, and invalid-token expiry normalization. |
| `internal/tui/model.go` | Modified | Added login/authentication states, admin entry gating from inspection screens, read-only users/grants/tokens views, and logout/expiry handling over the HTTP admin client. |
| `internal/tui/model_test.go` | Modified | Added behavior coverage for login gating, successful and failed login, read-only admin navigation, and expiry-driven relogin. |
| `internal/tui/session.go` | Created | Added in-memory session primitives plus expanded admin view state for selected-user read-only navigation. |
| `internal/tui/session_test.go` | Created | Added unit coverage for expiry detection and session/view reset behavior. |
| `docs/architecture.md` | Modified | Documented the shipped admin TUI read path, HTTP admin seam, and in-memory session boundary as part of the approved v1 architecture. |
| `docs/roadmap.md` | Modified | Marked the authenticated, GET-only admin TUI slice as shipped and kept admin mutations explicitly deferred. |

## Deviations from Design

None — implementation matches design for the Work Unit 4 boundary.

## Issues Found

- The repository already contained unrelated uncommitted changes outside this slice; this batch stayed scoped to documentation/task alignment for the shipped admin TUI read path.

## Remaining Tasks

- None.

## Workload / PR Boundary

- Mode: stacked PR slice
- Current work unit: 4
- Boundary: Documentation/task close-out only for the shipped admin TUI read path; no runtime behavior changes or new admin scope.
- Estimated review budget impact: low for PR 4 because the slice is limited to architecture, roadmap, and SDD artifact alignment.

## Verification

- Documentation-only slice; no additional runtime commands were required.

## Status

- 12/12 tasks complete.
- Ready for verify.
