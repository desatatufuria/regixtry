## Exploration: registry-operator-admin-tui-mutations

### Current State
The authenticated admin TUI foundation already exists. `cmd/registry/main.go` wires `tui.NewHTTPAdminClient(...)` into `tui.NewModel(...)`, `internal/tui/admin_client.go` logs in through `/auth/token` and reads `/admin/v1/users`, `/grants`, and `/admin-tokens`, and `internal/tui/model.go` renders authenticated admin screens with login, logout, expiry handling, and read-only navigation.

The mutation boundary already exists on the backend, not in the TUI. `internal/protocol/http/admin_handlers.go` exposes `POST /admin/v1/users`, `POST /users/{id}:enable`, `POST /users/{id}:disable`, `POST /users/{id}:reset-password`, `PUT/DELETE /users/{id}/grants/{repository}`, and `POST/DELETE /users/{id}/admin-tokens...`, while `internal/app/auth/service.go` preserves the real safety rules such as `requireAdmin`, password validation, admin-token TTL caps, disabled-user checks, and last-active-admin protection.

Today the TUI deliberately stops at read-only admin browsing. `internal/tui/admin_client.go` exposes only login plus GET methods, `internal/tui/model.go` only supports selected-user browsing and read-only key hints, and `internal/tui/model_test.go` explicitly asserts that admin browsing has no mutation affordances.

### Affected Areas
- `internal/tui/admin_client.go` — must grow mutation methods for the existing safe backend routes and keep session-expiry/error normalization consistent with the GET client.
- `internal/tui/model.go` — owns the admin state machine, key handling, status banners, and screen rendering where mutation entry, confirmation, success/error feedback, and post-mutation refresh would live.
- `internal/tui/session.go` — likely needs any extra transient mutation/selection state kept separate from persisted session data.
- `internal/tui/model_test.go` — currently locks the read-only admin UX; it will define the regression line for the first mutation slice.
- `internal/tui/admin_client_test.go` — should add HTTP-level coverage for mutation verbs, conflict/validation handling, and invalid-token expiry mapping on write paths.
- `internal/protocol/http/admin_handlers.go` — backend contract reference for allowed methods and payload shapes; likely unchanged for the first TUI-only slice.
- `internal/ports/auth.go` — defines the DTOs the TUI must honor for user, grant, password-reset, and admin-token flows.
- `internal/app/auth/service.go` — remains the single enforcement boundary for last-active-admin protection, password rules, grant validation, and token TTL limits.

### Approaches
1. **Users-first reversible mutations** — add enable/disable actions from the existing users screen, with confirmation and immediate list refresh, while keeping grants/tokens/password/user creation read-only.
   - Pros: Reuses the current selected-user UX; avoids new secret-entry forms; exercises write-path auth, conflict, and refresh behavior with the smallest UI blast radius.
   - Cons: Still needs careful handling for last-active-admin conflicts and self-disable/session fallout.
   - Effort: Low/Medium

2. **Grant-management first** — keep the users screen read-only and add add/remove repository grants from the grants screen.
   - Pros: Fully reversible domain action; avoids password and secret handling.
   - Cons: Needs new repository/role input UX and more model state than users-first; current grants view is not selection-oriented.
   - Effort: Medium

3. **Broad mutation parity** — add create user, enable/disable, reset password, grant writes, and token create/revoke in one pass.
   - Pros: Fastest route to a "complete" admin TUI.
   - Cons: WRONG first slice for review safety; mixes destructive and secret-bearing flows with the first write-path UI, likely blowing past the 1200-line review budget.
   - Effort: High

### Recommendation
Use **Approach 1**.

The safest mutation surfaces to expose first are the ones that are already visible in the current users list, are operationally reversible, and do not require handling new long-lived secrets or operator-entered replacement passwords. That makes **enable user** and **disable user** the best first mutation pair.

The first reviewable slice boundary should be: **mutation-capable users screen only**. Concretely: extend `AdminClient` with enable/disable write methods, add a small confirmation flow and success/error status handling in `internal/tui/model.go`, refresh the users list after mutation, and preserve grants, admin tokens, create user, and reset password as read-only/deferred. This keeps the slice narrow, exercises the authenticated write path safely, and should stay comfortably inside the 1200-line review budget.

### Risks
- Disabling the currently authenticated operator can produce confusing follow-up failures unless the model explicitly handles self-disable and expired/disabled sessions.
- The backend returns meaningful `409` and `422` outcomes; if the TUI collapses them into generic write failures, operators lose the safety context.
- Admin-token creation is intentionally deferred because it returns a one-time secret and would add copy/display security pressure to the first mutation slice.
- Password reset is intentionally deferred because it introduces masked input, confirmation, and higher operator error cost than enable/disable.

### Ready for Proposal
Yes — propose a first mutation slice limited to authenticated user enable/disable from the admin users screen, with grants/tokens/create-user/reset-password explicitly deferred.
