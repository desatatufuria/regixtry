# Proposal: Registry Operator Admin API

## Intent

Add a safe first operator-administration surface over authenticated HTTP so registry admins can manage users, grants, and admin credentials without reintroducing local bypass paths.

## Scope

### In Scope
- `/admin/v1` HTTP namespace backed by the existing `AuthService` and bearer-token auth.
- First-slice operator actions: list/create users, enable/disable users, reset passwords, list/put/delete repo grants, and list/create/revoke admin credential tokens.
- Router-level authn/authz, validation, error mapping, and docs/roadmap updates that keep CLI-next and TUI-later explicit.

### Out of Scope
- New local DB-writing admin CLI commands beyond existing `bootstrap-admin`.
- TUI mutation workflows, user deletion, broad `UpdateUser` profile edits, and break-glass bootstrap flows over the normal admin API.

## Capabilities

### New Capabilities
- `operator-admin-http-api`: Authenticated operator API contract, route shape, and transport-level auth/error behavior.
- `operator-user-administration`: Safe user listing, creation, enable/disable, and password reset workflows.
- `operator-access-administration`: Safe repo-grant and admin-credential-token management workflows.

### Modified Capabilities
- None.

## Approach

Expose a thin HTTP layer over existing `AuthService` admin methods and preserve current service safeguards (`requireAdmin`, disabled-user checks, TTL caps, last-active-admin protection). Benchmark registry/operator admin patterns before freezing final endpoint conventions, but keep the first slice transport small and reversible.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/protocol/http/router.go` | Modified | Add authenticated admin routes beside `/auth/token` and `/v2/*`. |
| `internal/protocol/http/router_test.go` | Modified | Add admin API auth, validation, and error-mapping coverage. |
| `internal/ports/auth.go` | Modified | Reuse and, if needed, narrow the existing admin service contract for HTTP use. |
| `internal/app/auth/service.go` | Modified | Preserve current safety rules as the single enforcement boundary. |
| `cmd/registry/main.go` | Modified | Wire the admin API without adding a second admin runtime path. |
| `docs/`, `README.md` | Modified | Keep API/CLI-first roadmap and TUI non-goals explicit. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Admin API leaks irreversible operations too early | Med | Exclude delete/broad update in v1 slice. |
| Transport auth semantics weaken Docker auth behavior | Med | Keep `/auth/token` and `/v2/*` behavior unchanged; add focused router tests. |
| CLI/TUI shortcuts creep back in | High | Keep CLI as future API client only and TUI mutation work out of scope. |

## Rollback Plan

Remove `/admin/v1` route wiring, revert related docs, and keep operator mutation limited to existing bootstrap-admin plus current backend-only admin services.

## Dependencies

- Existing `registry-auth-v1` auth flow and Postgres auth store.
- Benchmark/reference review of operator admin patterns from comparable registries before design lock.

## Success Criteria

- [ ] Operators can perform the in-scope admin actions only through authenticated API calls that reuse existing backend safety rules.
- [ ] No new local shortcut path is introduced in CLI or TUI for admin mutations.
- [ ] Router behavior cleanly distinguishes unauthorized, forbidden, validation, and conflict cases without changing registry auth behavior.
