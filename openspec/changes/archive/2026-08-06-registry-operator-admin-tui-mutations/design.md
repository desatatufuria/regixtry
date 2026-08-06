# Design: Registry Operator Admin TUI Mutations

## Technical Approach

Add a narrow write path to the existing authenticated admin users screen by extending `HTTPAdminClient` with enable/disable calls to the already-supported backend `POST /admin/v1/users/{id}:enable|:disable` routes, then layering a confirmation state and mutation status handling into the Bubble Tea model. This follows the proposal and delta spec: only single-user enable/disable is added, confirmation is mandatory, and backend responses stay authoritative for conflicts and expiry.

## Architecture Decisions

| Decision | Options | Choice / Rationale |
|---|---|---|
| Mutation ownership | Local optimistic rules vs backend-authoritative write | Keep backend-authoritative writes. The service/router already enforce last-active-admin protection and expose clear conflict/validation responses, so the TUI should not duplicate safety policy. |
| Confirmation UX | Inline toggle, extra pane, or modal-like confirmation state | Add a focused confirmation substate on `screenAdminUsers`. It matches the current single-screen Bubble Tea model, keeps scope narrow, and avoids broader layout work. |
| Post-success state | Optimistic local patch vs reload users | Reload via `ListUsers` after success. The users list is the source displayed today, and refresh keeps selection/status aligned with server truth without partial local reconciliation. |

## Data Flow

1. Operator logs in and lands on `screenAdminUsers`.
2. On a selected user, `e` or `d` opens confirmation with target username and resulting state.
3. `enter` confirms; `esc`/`n` cancels without sending a request.
4. Model enters a mutation-in-flight state, blocks repeat submissions, and calls admin client enable/disable.
5. On success, show a success status and immediately reload `/admin/v1/users`, preserving selection by user ID when possible.
6. On backend conflict or validation failure, keep the current screen and show recoverable feedback.
7. On expired/invalid session, clear admin session state and return to login with expiry-specific feedback.

```text
Admin Users screen
  -> confirmation state
  -> HTTPAdminClient EnableUser/DisableUser
  -> POST /admin/v1/users/{id}:enable|:disable
  -> success -> ListUsers refresh
  -> conflict/validation -> status on users screen
  -> invalid_token -> expire session -> admin login
```

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/tui/admin_client.go` | Modify | Extend `AdminClient` and `HTTPAdminClient` with enable/disable mutation methods, reusing bearer auth, expiry checks, and JSON error decoding. |
| `internal/tui/admin_client_test.go` | Modify | Add request/response coverage for enable/disable success, conflict, validation, and invalid-token expiry mapping. |
| `internal/tui/model.go` | Modify | Add users-screen confirmation state, in-flight mutation handling, refresh-after-success flow, and clear status messaging. |
| `internal/tui/model_test.go` | Modify | Cover confirm/cancel flow, success refresh, conflict feedback, validation feedback, and expiry-driven relogin. |

## Interfaces / Contracts

```go
type AdminClient interface {
    Login(ctx context.Context, username, password string) (AdminSession, error)
    ListUsers(ctx context.Context, session AdminSession) ([]ports.AdminUser, error)
    ListUserGrants(ctx context.Context, session AdminSession, userID string) ([]ports.AdminRepoGrant, error)
    ListUserAdminTokens(ctx context.Context, session AdminSession, userID string) ([]ports.AdminToken, error)
    EnableUser(ctx context.Context, session AdminSession, userID string) (ports.AdminUser, error)
    DisableUser(ctx context.Context, session AdminSession, userID string) (ports.AdminUser, error)
}
```

The TUI status contract stays message-based:
- success: `User "alice" disabled. Refreshing users...`
- conflict: backend message preserved (for self-disable / last-active-admin protection)
- validation: backend message preserved as recoverable status
- expiry: `Session expired. Log in again.` and admin state cleared

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | Admin client mutation requests and error mapping | Extend `internal/tui/admin_client_test.go` with httptest coverage for `:enable` / `:disable`. |
| Integration | Backend mutation protection | Keep relying on existing auth/router coverage for enable/disable routes and last-active-admin conflict behavior. |
| E2E | TUI mutation interaction | Add/update `internal/tui/model_test.go` flow tests for confirm, cancel, success refresh, conflict, and expiry relogin. |

## Threat Matrix

N/A — no routing, shell, subprocess, VCS/PR automation, executable-file classification, or process-integration boundary is introduced by this TUI/client slice.

## Migration / Rollout

No migration required. Rollout is code-only and can ship with the existing admin backend routes.

## Open Questions

- [ ] None blocking. Reuse backend error text unless later UX review asks for friendlier conflict copy.
