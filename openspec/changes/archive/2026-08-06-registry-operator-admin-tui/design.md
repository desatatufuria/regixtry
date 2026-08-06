# Design: Registry Operator Admin TUI

Implement the first admin slice as a thin Bubble Tea client over the existing authenticated HTTP API. The TUI keeps local inspection behavior, but admin views become unreachable until the operator completes `/auth/token` login and receives an in-memory bearer session.

## Quick path

1. Start the TUI with local inspection dependencies plus a backend API base URL.
2. If admin mode is selected, show login, exchange Basic credentials at `/auth/token`, and store only the bearer token plus expiry metadata in memory.
3. Use that session for GET-only `/admin/v1` reads; on `401 invalid_token` or local expiry, clear session state and return to login with an expiry-specific message.

## Technical Approach

The implementation stays inside the existing thin-client TUI boundary from `docs/architecture.md`. `cmd/registry/main.go` will stop wiring admin behavior through `localOperatorAccessController` and instead pass two seams into `internal/tui`: the current local inspection query service and a new admin HTTP client. The Bubble Tea model becomes a small state machine with unauthenticated, authenticating, authenticated-admin, and expired-session states.

## Architecture Decisions

| Decision | Choice | Alternatives considered | Rationale |
| --- | --- | --- | --- |
| Session representation | Keep `AdminSession` in memory with `bearerToken`, `username`, `expiresAt`, and last expiry reason | Persist token on disk; reuse admin credential tokens | In-memory only satisfies the security requirement and avoids secret recovery risk on low-resource operator hosts. |
| Backend seam | Create a dedicated `AdminClient` interface backed by `net/http` instead of calling auth/application services directly | Direct store/service reuse inside TUI | The TUI must remain a client, not a second backend; HTTP preserves the real trust boundary and exercises the same auth/error contracts as other admin surfaces. |
| Expiry handling | Treat expiry as terminal for the current session; no silent refresh | Background refresh; retry loops | Current backend exposes short-lived access tokens but no refresh contract. Failing closed is simpler, cheaper, and easier to verify. |
| View composition | Keep one Bubble Tea model with nested view states rather than split programs | Separate admin program; modal subprocesses | The repo already uses one `Model`; extending that pattern minimizes churn and preserves snapshot-style tests. |

## Data Flow

`operator -> Bubble Tea login view -> AdminClient.Login() -> /auth/token -> AdminSession`

`AdminSession -> AdminClient.ListUsers/ListGrants/ListAdminTokens -> /admin/v1/* -> admin tables`

`401 invalid_token or now >= expiresAt -> clear session + selected admin data -> login view with "Session expired" status`

Local inspection remains:

`operator -> Bubble Tea inspection views -> QueryService -> appregistry.Service`

## File Changes

| File | Action | Description |
| --- | --- | --- |
| `cmd/registry/main.go` | Modify | Add backend API configuration (for example `--api-base-url`), require it for admin mode, and pass local inspection + HTTP admin dependencies into the TUI model. |
| `internal/tui/model.go` | Modify | Expand the state machine to include login, admin navigation, loading/error banners, logout, and expiry-driven relogin. |
| `internal/tui/admin_client.go` | Create | Define `AdminClient`, HTTP implementation, request helpers, and auth/admin response decoding. |
| `internal/tui/session.go` | Create | Hold `AdminSession`, expiry checks, logout/reset helpers, and admin-screen cache clearing. |
| `internal/tui/model_test.go` | Modify | Add login success/failure, expiry redirect, and read-only admin navigation tests with fake client seams. |
| `docs/architecture.md` / `docs/roadmap.md` | Modify | Reflect that the first TUI admin slice is authenticated, GET-only, and still mutation-free. |

## Interfaces / Contracts

```go
type AdminSession struct {
    Username   string
    BearerToken string
    ExpiresAt  time.Time
    ExpiredReason string
}

type AdminClient interface {
    Login(ctx context.Context, username, password string) (AdminSession, error)
    ListUsers(ctx context.Context, session AdminSession) ([]ports.AdminUser, error)
    ListUserGrants(ctx context.Context, session AdminSession, userID string) ([]ports.AdminRepoGrant, error)
    ListUserAdminTokens(ctx context.Context, session AdminSession, userID string) ([]ports.AdminToken, error)
}
```

`Login` uses HTTP Basic against `/auth/token` and maps the JSON token response into `AdminSession.ExpiresAt` using `expires_in` when present. Admin reads send `Authorization: Bearer <token>`. Any `401` with `WWW-Authenticate` `error="invalid_token"`, or any local `ExpiresAt` breach before dispatch, is normalized to a single session-expired error.

## View Flow

`Inspection Home -> [tab/admin] Login -> Users -> User Detail -> (Grants | Admin Tokens)`

- Login fields: username, password, submit, cancel back to inspection.
- Authenticated header shows operator username and remaining session time.
- Logout is always visible from admin screens.
- Read-only means no create, enable/disable, reset-password, grant write, or token revoke actions are rendered.

## Testing Strategy

| Layer | What to Test | Approach |
| --- | --- | --- |
| Unit | Session reset, expiry detection, response mapping | Table-driven tests for `session.go` and `admin_client.go`. |
| Integration | HTTP client handles `/auth/token`, `/admin/v1/*`, and `401 invalid_token` correctly | `httptest` server with real JSON/status/header combinations. |
| UI | Login flow, relogin after expiry, read-only navigation | Extend `internal/tui/model_test.go` with fake `AdminClient`. |

## Migration / Rollout

No data migration required. Rollout is additive behind the TUI/admin path; fallback remains the existing local inspection flow.

## Out of Scope

- Admin mutations of any kind.
- Persisted credentials or bearer tokens.
- Silent refresh, background token renewal, or preissued-admin-token-first login UX.
- Pagination/search for large admin datasets.
