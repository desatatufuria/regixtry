## Exploration: registry-operator-admin-api

### Current State
`registry-auth-v1` already introduced the core auth domain, Postgres auth store, `/auth/token`, bearer validation, repository-scoped grants, and admin-only service methods for user, grant, password, and token management. Those admin capabilities exist inside `internal/app/auth/service.go`, but they are NOT exposed through an operator HTTP API yet.

The only out-of-band operator mutation path today is `registry bootstrap-admin` in `cmd/registry/main.go`. That is acceptable as a break-glass bootstrap path, but it is intentionally narrow. The auth-enabled TUI stays inspection-only and still uses `localOperatorAccessController` for local browsing, with an explicit notice that admin mutations are disabled because the prior local shortcut was unsafe.

### Affected Areas
- `internal/ports/auth.go` — already defines the admin service surface; proposal/design can reuse this as the backend contract instead of inventing a parallel admin model.
- `internal/app/auth/service.go` — contains the real safety rules already worth preserving: `requireAdmin`, token TTL caps, disabled-user checks, and last-active-admin protection on demote/disable/delete.
- `internal/protocol/http/router.go` — currently exposes only `/auth/token` plus `/v2/*`; this is the natural place to add authenticated operator admin endpoints.
- `internal/protocol/http/router_test.go` — will need the first end-to-end coverage for admin API authn/authz, validation, and forbidden/error mapping.
- `cmd/registry/main.go` — runtime wiring already constructs `AuthService`; it will need route wiring only, not a second admin runtime path.
- `cmd/registry/main.go` (`runBootstrapAdmin`) — must remain a bootstrap-only break-glass command, not become the general admin workflow.
- `internal/infra/auth/postgres/store.go` — backs all admin mutations already; API design should stay inside these existing persistence guarantees.
- `internal/domain/auth/*.go` — current error codes and token/user/grant models define what safe API responses and constraints can be expressed.
- `internal/tui/model.go` and `cmd/registry/main.go` (`runTUI`) — should remain out of scope for the first operator-admin slice except for documentation alignment, because the TUI bypass history is the security boundary we must not reopen.

### Approaches
1. **Authenticated admin HTTP API first, CLI second** — expose operator endpoints over the existing `AuthService`, then let any future CLI act as a thin authenticated client.
   - Pros: Reuses the safe backend rules already implemented; keeps one enforcement boundary; avoids direct Postgres mutation shortcuts; gives a stable foundation for later CLI and TUI clients.
   - Cons: Requires careful route design and HTTP error mapping before operators get a friendly UX.
   - Effort: Medium

2. **Direct CLI mutation commands against Postgres/AuthStore** — add more `registry ...` subcommands that write auth state locally.
   - Pros: Fastest operator UX for local environments.
   - Cons: Recreates the same class of boundary bypass that forced TUI admin mutations to be deferred; duplicates business rules across transport paths; weak story for remote administration.
   - Effort: Low/Medium

3. **TUI-first admin revival** — re-enable admin workflows in the local console and add backend/API later.
   - Pros: Familiar operator interface.
   - Cons: Wrong order. It pushes UX ahead of the trust boundary and risks reintroducing local-only shortcuts before authenticated backend administration is proven.
   - Effort: High

### Recommendation
Use **Approach 1**.

The safest first backend surfaces to expose are the ones that are already guarded by `AuthService` and are operationally reversible:
- list users
- create user
- enable/disable user
- reset password
- list repo grants
- put/delete repo grant
- list admin credential tokens
- create admin credential token
- revoke admin credential token

The first slice should explicitly **exclude**:
- direct Postgres-writing CLI admin commands beyond existing `bootstrap-admin`
- TUI mutation workflows
- user deletion
- broad user profile mutation endpoints (`UpdateUser`) unless proposal/design can narrow them safely
- bootstrap or password-rotation break-glass flows over the normal operator API

That boundary keeps operator administration inside the authenticated backend, preserves the existing last-admin safety checks, and avoids reopening the prior "local admin shortcut" class of vulnerability. A sane initial shape is a small authenticated namespace such as `/admin/v1/...`, protected by bearer access tokens obtained through the existing `/auth/token` flow, with a later CLI acting only as an API client.

### Risks
- If the change adds direct DB-writing CLI commands, it bypasses the backend auth boundary and repeats the core mistake that made TUI mutations unsafe.
- Exposing `DeleteUser` in the first slice increases irreversible blast radius when disable + revoke already covers the urgent operator cases.
- Exposing broad `UpdateUser` too early can mix rename/admin-flag changes with lockout-sensitive flows before the API semantics are fully nailed down.
- The current router only has `/auth/token`; admin endpoints need careful forbidden vs unauthorized behavior so Docker auth behavior stays untouched while operator API behavior remains predictable.
- The TUI still uses `localOperatorAccessController`; any accidental coupling between the new admin API and the local TUI path would weaken the trust model again.
- There is no current admin API test layer; without strong router tests, it is easy to ship correct service rules behind incorrect transport/auth mapping.

### Ready for Proposal
Yes — with the proposal narrowed to an authenticated operator admin API foundation, leaving TUI admin UX and any direct-storage CLI mutation paths out of scope.
