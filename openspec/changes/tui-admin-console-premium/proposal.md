# Proposal: TUI Admin Console Premium

## Intent

Deliver a premium-feeling admin workspace that lets operators complete the existing backend-admin flows from the TUI without falling back to local shortcuts, ad hoc scripts, or future-only routes.

## Scope

### In Scope
- Premium admin shell with sidebar navigation, selected-user context, main-panel forms, and short confirmation modals.
- API-backed user actions: create user, create admin user at creation time, enable/disable, and reset password.
- API-backed selected-user grant and admin-token flows: add/change/remove grants, create token, revoke token, one-time secret reveal.

### Out of Scope
- Post-creation `is_admin` edits, delete user, API expansion, direct DB/store writes, silent refresh.
- A second control plane separate from `/auth/token` and `/admin/v1`.

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `operator-admin-tui`: expand the current read-mostly admin slice into a premium API-first mutation workspace over the existing HTTP contract.

## Approach

Extend the current Bubble Tea admin flow instead of replacing it. Keep `/auth/token` and `/admin/v1` as the only authority, grow `internal/tui/admin_client.go` to cover the missing mutation routes, and redesign `internal/tui/model.go` into a themed admin workspace with sidebar, forms, and modal confirmations. This keeps session expiry, backend validation, last-active-admin protection, disabled-user checks, and admin-token TTL limits enforced by the existing service/HTTP layers.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/tui/model.go` | Modified | Replace plain admin screens with premium workspace state, forms, and confirmations. |
| `internal/tui/session.go` | Modified | Preserve selected-user and session lifecycle across richer admin flows. |
| `internal/tui/admin_client.go` | Modified | Add create/reset/grant/token mutation methods using current HTTP routes only. |
| `internal/protocol/http/admin_handlers.go` | Referenced | Existing route/payload contract remains the authority for scope. |
| `internal/ports/auth.go` | Referenced | DTOs define create user, reset password, grant, and token payloads. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Workspace bloat inside one model | Med | Extract admin view/theme/form helpers while keeping one program. |
| Secret or session mishandling | Med | Reveal token secret once, clear auth/form state on logout or expiry, never invent local success. |

## Rollback Plan

Disable the premium admin workspace path and retain the current authenticated admin screens; because all mutations stay behind the same HTTP routes, rollback is limited to TUI state/client changes.

## Dependencies

- Current `/auth/token` and `/admin/v1` routes already implemented by `internal/protocol/http/admin_handlers.go` and `internal/app/auth/service.go`.

## Success Criteria

- [ ] Operators can complete every in-scope action from the TUI using only the current HTTP API surface.
- [ ] The TUI never performs optimistic local admin mutations; backend responses remain authoritative.
- [ ] The premium redesign keeps grants and tokens scoped to the selected user and reveals admin-token secrets once.
