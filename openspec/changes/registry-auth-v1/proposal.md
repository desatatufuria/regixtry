# Proposal: Registry Auth V1

## Intent

Add a v1 authentication and repository-authorization model so operators can restrict discovery and registry access without migrating existing registry metadata off SQLite.

## Scope

### In Scope
- Postgres-backed auth subsystem for local users, password login, admin-preissued tokens, revocation, and conservative expiry.
- Repository-scoped fixed roles: `repo-reader`, `repo-writer`, `repo-admin`.
- Immediate auth challenge for catalog/tags/discovery when anonymous pull is off.
- Minimal TUI administration: user CRUD, password reset, and repository grant management.

### Out of Scope
- AppRole or external credential backends.
- Self-service token minting, audit/reporting UX, and full metadata migration to Postgres.

## Capabilities

### New Capabilities
- `registry-authentication`: Login, token validation, admin token issuance, revocation, and bearer challenge behavior.
- `repository-authorization`: Repository-scoped role evaluation for pull, push, and discovery actions.
- `operator-user-administration`: TUI workflows for user CRUD, password reset, and repository grants.

### Modified Capabilities
- None.

## Approach

Use a dedicated Postgres auth subsystem beside the current SQLite metadata store. Separate authentication from authorization, introduce principal/claims-aware access decisions, and keep the first slice focused on registry protection plus minimal operator management.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/ports/registry.go` | Modified | Add principal/claims and richer challenge/auth contracts. |
| `internal/app/registry/` | Modified | Enforce repository-scoped roles for registry and discovery actions. |
| `internal/protocol/http/router.go` | Modified | Add login/token flow and challenge behavior. |
| `cmd/registry/main.go` | Modified | Wire Postgres auth config and operator access model. |
| `internal/tui/` | Modified | Add user administration screens and grant management. |
| `internal/infra/auth/` | New | Postgres-backed auth persistence and token/user services. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Scope expands into full platform auth rewrite | Med | Freeze v1 to local users, fixed roles, and minimal TUI admin. |
| Docker auth compatibility gaps | Med | Keep standard bearer challenge semantics explicit in specs/design. |
| Split SQLite/Postgres operations add friction | Med | Limit Postgres to auth only and document bootstrap clearly. |

## Rollback Plan

Disable auth wiring, remove Postgres auth dependency from runtime configuration, and revert to the existing anonymous-access controller while preserving registry metadata in SQLite.

## Dependencies

- Postgres runtime availability for auth storage.
- Benchmark/reference review of Vault-like token flows and registry auth challenge behavior.

## Success Criteria

- [ ] Operators can authenticate local users and manage admin-issued tokens and repository grants.
- [ ] Anonymous discovery endpoints challenge immediately when anonymous pull is off.
- [ ] Authorized users can access only repositories allowed by their fixed role grants.
