# Design: TUI Admin Console Premium

## Technical Approach

Extend the existing Bubble Tea program instead of introducing a second admin app. `cmd/regixtry/main.go` will keep wiring `tui.NewModel(...)` with an optional HTTP admin client, while the authenticated branch of `internal/tui/model.go` becomes a premium workspace driven only by `/auth/token` and existing `/admin/v1` routes. This implements the proposal and the `operator-admin-tui` delta without expanding backend APIs.

## Architecture Decisions

| Area | Options | Decision | Rationale |
|------|---------|----------|-----------|
| Admin shell | Replace TUI vs extend current model | Extend current model | Preserves login/session flow, local inspection views, and current CLI wiring with lower review risk. |
| Styling | Plain strings vs themed helpers | Add Lip Gloss-based theme helpers in `internal/tui/` | Required for the premium palette and keeps render rules centralized instead of scattering ANSI formatting. |
| Mutation source | Local optimistic updates vs backend refresh | Refresh from backend after every write | Matches the API-first contract and preserves server-side rules like last-active-admin and token TTL validation. |
| Client surface | Ad hoc per-method logic vs generic JSON helpers + typed methods | Extend `internal/tui/admin_client.go` with typed create/reset/grant/token methods | Current client already encapsulates auth/expiry handling; new routes should reuse the same normalization path. |

## Data Flow

Sidebar selection remains the anchor for all per-user actions.

```text
Operator input
  -> Model route/form state (`internal/tui/model.go` + `session.go`)
  -> AdminClient request (`internal/tui/admin_client.go`)
  -> `/auth/token` or `/admin/v1/...`
  -> HTTP handler validation (`internal/protocol/http/admin_handlers.go`)
  -> auth service/store
  -> typed response
  -> model refreshes users/grants/tokens from backend
  -> themed workspace rerender
```

Write flows stay fail-closed: the active form/modal remains visible on validation errors, invalid-token responses force `screenAdminLogin`, and grant/token panels are disabled until `SelectedUserID` is set.

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `internal/tui/model.go` | Modify | Replace read-mostly admin screens with sidebar/workspace routing, form state, modal state, success/error banners, and post-mutation refresh logic. |
| `internal/tui/session.go` | Modify | Extend admin view/session state with selected panel, create-user form, password-reset form, grant form, token form, modal state, and one-time token secret state. |
| `internal/tui/admin_client.go` | Modify | Add create user, reset password, put/delete grant, create token, and revoke token methods; support JSON bodies plus `201 Created` and `204 No Content` responses. |
| `internal/tui/admin_theme.go` | Create | Premium palette, borders, typography, status colors, and reusable Lip Gloss styles. |
| `internal/tui/admin_views.go` | Create | Sidebar, main-panel, form, table/list, and short-modal render helpers to keep `model.go` reviewable. |
| `internal/tui/model_test.go` | Modify | Cover workspace routing, form validation persistence, contextual grant/token actions, modal confirmation paths, and one-time secret reveal behavior. |
| `internal/tui/admin_client_test.go` | Modify | Expand HTTP-client coverage to all in-scope mutation routes and status-code handling. |
| `cmd/regixtry/main.go` | Modify | Keep current wiring, but ensure any new theme dependency remains isolated to TUI construction. |

## Interfaces / Contracts

```go
type AdminClient interface {
    Login(ctx context.Context, username, password string) (AdminSession, error)
    ListUsers(ctx context.Context, session AdminSession) ([]ports.AdminUser, error)
    CreateUser(ctx context.Context, session AdminSession, input ports.AdminCreateUserInput) (ports.AdminUser, error)
    ResetPassword(ctx context.Context, session AdminSession, input ports.AdminResetPasswordInput) error
    ListUserGrants(ctx context.Context, session AdminSession, userID string) ([]ports.AdminRepoGrant, error)
    PutUserGrant(ctx context.Context, session AdminSession, input ports.AdminPutRepoGrantInput) (ports.AdminRepoGrant, error)
    DeleteUserGrant(ctx context.Context, session AdminSession, userID, repository string) error
    ListUserAdminTokens(ctx context.Context, session AdminSession, userID string) ([]ports.AdminToken, error)
    CreateUserAdminToken(ctx context.Context, session AdminSession, input ports.AdminCreateTokenInput) (ports.AdminCreatedToken, error)
    RevokeUserAdminToken(ctx context.Context, session AdminSession, userID, accessor string) error
    EnableUser(ctx context.Context, session AdminSession, userID string) (ports.AdminUser, error)
    DisableUser(ctx context.Context, session AdminSession, userID string) (ports.AdminUser, error)
}
```

The TUI must honor existing payloads: `POST /admin/v1/users` returns `AdminUser`, token creation accepts `ttl_seconds` and returns `AdminCreatedToken`, and reset/delete/revoke routes succeed with `204` and no local shadow state.

## Testing Strategy

| Layer | What to Test | Approach |
|-------|-------------|----------|
| Unit | Workspace state transitions, selected-user gating, modal dismissal, one-time secret clearing | Table-driven `internal/tui/model_test.go` cases using the fake admin client. |
| Integration | Admin HTTP client request paths, body encoding, `200/201/204`, invalid-token mapping | `httptest` coverage in `internal/tui/admin_client_test.go`. |
| E2E | Snapshot of authenticated premium shell and basic action prompts | Extend existing TUI snapshot/smoke path after unit coverage lands. |

## Threat Matrix

N/A — no routing, shell, subprocess, VCS/PR automation, executable-file classification, or process-integration boundary is being introduced or changed; this design consumes existing HTTP routes only.

## Migration / Rollout

No migration required. Rollout is a TUI-only replacement of the authenticated admin workspace behind the existing `tui` command and current API configuration.

## Open Questions

- [ ] None.
