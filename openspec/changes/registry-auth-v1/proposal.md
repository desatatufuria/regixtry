# Proposal: Registry Auth V1

## Intent

Add a v1 authentication and repository-authorization model so operators can restrict discovery and registry access without migrating existing registry metadata off SQLite.

## Scope

### In Scope
- Postgres-backed auth subsystem for local users, password login, admin-preissued tokens, revocation, and conservative expiry.
- Repository-scoped fixed roles: `repo-reader`, `repo-writer`, `repo-admin`.
- Immediate auth challenge for catalog/tags/discovery when anonymous pull is off.
- Auth-enabled TUI inspection with an explicit notice that local admin mutations remain deferred.

### Out of Scope
- AppRole or external credential backends.
- Self-service token minting, audit/reporting UX, and full metadata migration to Postgres.

## Capabilities

### New Capabilities
- `registry-authentication`: Login, token validation, admin token issuance, revocation, and bearer challenge behavior.
- `repository-authorization`: Repository-scoped role evaluation for pull, push, and discovery actions.
- `operator-user-administration`: Inspection-only auth-enabled TUI behavior plus a security notice while local admin mutations remain deferred.

### Modified Capabilities
- None.

## Approach

Use a dedicated Postgres auth subsystem beside the current SQLite metadata store. Separate authentication from authorization, introduce principal/claims-aware access decisions, and keep the shipped slice focused on registry protection plus an inspection-only operator console.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/ports/registry.go` | Modified | Add principal/claims and richer challenge/auth contracts. |
| `internal/app/registry/` | Modified | Enforce repository-scoped roles for registry and discovery actions. |
| `internal/protocol/http/router.go` | Modified | Add login/token flow and challenge behavior. |
| `cmd/registry/main.go` | Modified | Wire Postgres auth config and operator access model. |
| `internal/tui/` | Modified | Keep inspection available in auth-enabled mode and show the deferred-admin security notice. |
| `internal/infra/auth/` | New | Postgres-backed auth persistence and token/user services. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Scope expands into full platform auth rewrite | Med | Freeze v1 to local users, fixed roles, and inspection-only TUI behavior. |
| Docker auth compatibility gaps | Med | Keep standard bearer challenge semantics explicit in specs/design. |
| Split SQLite/Postgres operations add friction | Med | Limit Postgres to auth only and document bootstrap clearly. |

## Rollback Plan

Disable auth wiring, remove Postgres auth dependency from runtime configuration, and revert to the existing anonymous-access controller while preserving registry metadata in SQLite.

## Dependencies

- Postgres runtime availability for auth storage.
- Benchmark/reference review of Vault-like token flows and registry auth challenge behavior.

## Success Criteria

- [ ] Operators can authenticate local users while the shipped auth-enabled TUI stays inspection-only until a safe operator login flow exists.
- [ ] Anonymous discovery endpoints challenge immediately when anonymous pull is off.
- [ ] Authorized users can access only repositories allowed by their fixed role grants.
