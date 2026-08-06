# Proposal: Registry Operator Admin TUI

## Intent

Turn the TUI into a secure operator client of the authenticated admin backend so admin access follows the same trust boundary as every other admin surface.

## Scope

### In Scope
- Add interactive operator login through `/auth/token` using username/password.
- Keep bearer session state in memory only and clear it on logout, exit, or expiry.
- Provide read-only admin views backed by `/admin/v1` for users, grants, and admin tokens.
- Preserve local registry inspection while removing any local privileged admin shortcut path.

### Out of Scope
- Admin mutations from the TUI, including create, enable/disable, password reset, grant writes, token creation, and token revocation.
- Persisted credentials, silent refresh, or admin-token-first login.

## Capabilities

### New Capabilities
- `operator-admin-tui`: authenticated TUI login/session handling plus read-only admin browsing over backend APIs.

### Modified Capabilities
- None.

## Approach

Add a thin admin HTTP client to the TUI boundary. The first slice stops at login, session lifecycle, and GET-only admin views so the team can prove transport, auth errors, expiry UX, and trust-boundary correctness before any destructive workflows. Benchmark comparable registry/operator consoles before locking detailed interaction patterns in design.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `cmd/registry/main.go` | Modified | Replace local admin wiring with authenticated client/session startup. |
| `internal/tui/` | Modified | Add login/session state and read-only admin screens. |
| `internal/protocol/http/` | Referenced | Reuse existing `/auth/token` and `/admin/v1/*` contracts. |
| `docs/architecture.md`, `docs/roadmap.md` | Modified | Keep trust-boundary and rollout docs aligned. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Session expiry creates confusing failures | Med | Design explicit expired-session and re-login UX. |
| TUI grows a second backend model | Med | Keep all admin reads behind HTTP client seams only. |
| Secret persistence weakens security | Low | Ban disk storage for password, admin token, and bearer data. |

## Rollback Plan

Keep current inspection-only TUI as the fallback. If the slice destabilizes operator workflows, remove the admin client wiring and restore the existing non-admin local behavior while leaving backend admin APIs unchanged.

## Dependencies

- Existing `/auth/token` and `/admin/v1/*` API behavior remains the source of truth.
- Follow-up design must define expiry, logout, and unauthenticated empty-state UX.

## Success Criteria

- [ ] Operators can log in from the TUI and reach read-only admin views without any local admin bypass.
- [ ] Password, admin-token, and bearer session material never persist beyond process memory.
- [ ] Session expiry and logout return the TUI to a safe unauthenticated state.
