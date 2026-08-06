# Design: Registry Operator Admin API

Adds a narrow `/admin/v1` control surface over the existing auth backend so future CLI-first operator workflows can mutate auth state safely without reintroducing local shortcuts.

## Technical Approach

Expose resource-oriented JSON handlers under `/admin/v1` inside the existing HTTP router, but keep registry protocol behavior isolated. `/admin/v1` accepts only Bearer access tokens issued by `/auth/token`; handlers verify the token, require `Principal.IsAdmin`, then delegate to admin service methods that keep the existing safety rules (`requireAdmin`, password validation, TTL cap, disabled-user checks, last-active-admin protection).

## Architecture Decisions

### Decision: Separate admin transport semantics from registry semantics

| Option | Tradeoff | Decision |
|---|---|---|
| Reuse `writeError` for all routes | Simple, but current auth `FORBIDDEN` collapses to `401` for Docker flows | Add admin-only error mapping so `/admin/v1` can return explicit `401/403/404/409/422` without changing `/v2/*` |

**Rationale**: Docker registry flows need challenge-oriented behavior; admin API clients need precise operator feedback.

### Decision: Keep handlers thin and narrow the service dependency

| Option | Tradeoff | Decision |
|---|---|---|
| Depend on full `ports.AuthService` | Faster, but leaks bootstrap/delete/update methods into HTTP edge | Introduce a focused admin-facing interface in `internal/ports/auth.go` and implement it with the existing auth service |

**Rationale**: The protocol edge should only see the reversible admin actions in scope.

### Decision: CLI remains an authenticated API client, not a local mutator

| Option | Tradeoff | Decision |
|---|---|---|
| Add new DB-writing CLI commands | Convenient, but recreates the bypass we just removed | Keep `bootstrap-admin` as the only local break-glass path; future CLI uses `/auth/token` + `/admin/v1` |

**Rationale**: One backend path means one place to enforce admin safety rules.

## Data Flow

`CLI -> /auth/token -> bearer access token -> /admin/v1/* -> admin handler -> AuthService admin method -> Postgres auth store`

`/v2/* -> existing registry handlers` remains unchanged.

For token revocation and nested resources, handlers resolve path params, verify the actor, call the admin service, and serialize only the allowed fields. Plaintext admin credential secrets appear only in the create-token response and are never returned by list/revoke paths.

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `openspec/changes/registry-operator-admin-api/design.md` | Create | Technical design artifact for this change. |
| `internal/protocol/http/router.go` | Modify | Register `/admin/v1` routes and keep `/auth/token` + `/v2/*` behavior unchanged. |
| `internal/protocol/http/admin_handlers.go` | Create | Admin route parsing, bearer auth, JSON encoding, and admin-specific error mapping. |
| `internal/protocol/http/router_test.go` | Modify | Cover admin authn/authz, validation, conflict, and one-time-secret responses. |
| `internal/ports/auth.go` | Modify | Add a focused admin HTTP service contract and any token lookup needed for scoped revocation. |
| `internal/app/auth/service.go` | Modify | Reuse existing admin methods; tighten list/revoke flows so user-scoped admin routes can return accurate 404/conflict results. |
| `internal/infra/auth/postgres/store.go` | Modify | Add any token lookup/query support needed by nested admin token revocation. |
| `internal/app/auth/service_test.go` | Modify | Cover list/revoke safety and preserved safeguards. |
| `docs/architecture.md`, `docs/roadmap.md`, `README.md` | Modify | Document API/CLI-first administration and TUI non-goals. |

## Interfaces / Contracts

### HTTP surface

| Method | Path | Notes |
|---|---|---|
| `GET` | `/admin/v1/users` | List users without password hashes. |
| `POST` | `/admin/v1/users` | Create user `{username,password,is_admin,enabled}`. |
| `POST` | `/admin/v1/users/{id}:enable` | Reversible state change. |
| `POST` | `/admin/v1/users/{id}:disable` | Must preserve last active admin rule. |
| `POST` | `/admin/v1/users/{id}:reset-password` | Body `{new_password}`. |
| `GET` | `/admin/v1/users/{id}/grants` | List target grants; `404` if user missing. |
| `PUT` | `/admin/v1/users/{id}/grants/{repository}` | Body `{role}`. |
| `DELETE` | `/admin/v1/users/{id}/grants/{repository}` | Reversible access removal. |
| `GET` | `/admin/v1/users/{id}/admin-tokens` | List metadata only, never plaintext secret. |
| `POST` | `/admin/v1/users/{id}/admin-tokens` | Body `{name,ttl_seconds}`; returns secret once. |
| `DELETE` | `/admin/v1/users/{id}/admin-tokens/{accessor}` | Revoke only if the token belongs to that user. |

### Error semantics

- `401 Unauthorized`: missing/invalid/expired/revoked bearer token; respond with Bearer challenge.
- `403 Forbidden`: authenticated principal is not an admin.
- `404 Not Found`: unknown user, grant, or token within the scoped route.
- `409 Conflict`: username collision or last-active-admin protection.
- `422 Unprocessable Entity`: malformed username/password/repository/role/TTL.

## Testing Strategy

| Layer | What to Test | Approach |
|-------|-------------|----------|
| Unit | Admin error mapper and request validation | Table-driven handler tests with fake auth service. |
| Integration | User/grant/token admin routes preserve service safeguards | `httptest` router tests over real auth service/store. |
| E2E | Not in this slice | CLI client remains deferred; no new smoke flow yet. |

## Migration / Rollout

No data migration required. Roll out by wiring the routes only when auth is enabled; rollback removes `/admin/v1` registration and doc references.

## Open Questions

- [ ] Should user list pagination be deferred explicitly for v1, or do we want cursor support in the first CLI slice?
