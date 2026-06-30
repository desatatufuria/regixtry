# Design: Registry Auth V1

This change adds Docker-compatible auth without moving registry metadata off SQLite. Auth state lives in Postgres; repository state stays in the existing SQLite metadata store.

## Technical Approach

Keep the current `app/registry` + HTTP router shape, but replace the binary anonymous access controller with a principal-aware auth subsystem. The registry keeps serving `/v2/*`; a new auth endpoint issues short-lived bearer access tokens after validating either username/password or an admin-preissued credential token.

## Architecture Decisions

| Decision | Choice | Alternatives considered | Rationale |
|---|---|---|---|
| Auth storage boundary | Postgres stores users, password hashes, preissued tokens, access-token revocation state, and repo grants; SQLite keeps manifests/tags/uploads/catalog only. | Moving catalog metadata to Postgres; dual-writing repo metadata. | Avoids scope creep and preserves existing registry behavior. Cross-store linkage uses validated repository names only, not foreign keys. |
| Admin model | Add global `is_admin` on users; keep `repo-admin` repository-scoped only. Global admins bypass repo grant checks and own user/token administration. | Modeling admin as a wildcard repo grant; requiring repo grants for operators. | Prevents bootstrap deadlocks and keeps operator workflows simple. `repo-admin` remains meaningful for repository actions only. |
| Token model | Two token layers: (1) opaque admin-preissued credential tokens stored hashed in Postgres and used like passwords at the auth endpoint, (2) short-lived bearer access tokens presented to `/v2/*`. | Direct long-lived bearer tokens; JWT-only persistent credentials. | Matches Docker registry challenge flow, keeps leaked bearer blast radius small, and still feels Vault-like: opaque secrets, explicit TTL, accessor-based revoke. |
| TTL/revocation | Access tokens: fixed 15m, non-renewable. Preissued credential tokens: fixed 30d default, optional shorter TTL, explicit revoke only. | Renewable/periodic tokens; no-expiry tokens; per-user custom policy engine. | Conservative by default, low operator complexity, and no renewal machinery in v1. |

## Data Flow

### Registry request flow

`Client -> /v2/* -> HTTP auth middleware -> auth verifier -> principal in context -> app/registry authorize -> SQLite metadata/blob stores`

- Unauthenticated request to protected catalog, tags, pull, or push returns `401` with `WWW-Authenticate: Bearer realm=...,service=...,scope=...`.
- Authenticated catalog returns only repositories where the principal has read-capable access (`repo-reader`, `repo-writer`, `repo-admin`, or global admin).
- Tags/manifests/blobs use the same read-capable rule. If the principal lacks access, v1 returns `401 UNAUTHORIZED` rather than concealment-by-404, keeping current error mapping simple.

### Auth endpoint flow

`Client -> /auth/token -> Basic auth (username + password or preissued token) -> Postgres auth service -> intersect requested scope with grants -> signed/opaque 15m bearer -> client retries /v2/*`

## File Changes

| File | Action | Description |
|---|---|---|
| `go.mod` | Modify | Add Postgres driver (`pgx` stdlib adapter). |
| `cmd/registry/main.go` | Modify | Wire Postgres auth DSN, auth endpoint config, bootstrap subcommand, and TUI auth services. |
| `internal/ports/registry.go` | Modify | Add principal/scope-aware challenge and authorization contracts. |
| `internal/ports/auth.go` | Create | Interfaces for user admin, token issuance, token verification, and grant lookup. |
| `internal/app/auth/service.go` | Create | Login/token issuance, credential validation, revocation, and admin workflows. |
| `internal/domain/auth/*.go` | Create | User, token, grant, principal, and auth errors. |
| `internal/infra/auth/postgres/*.go` | Create | Postgres store and schema bootstrap for auth tables only. |
| `internal/app/registry/service.go` / `queries.go` | Modify | Enforce principal-aware authorization and catalog filtering. |
| `internal/protocol/http/router.go` | Modify | Add `/auth/token`, bearer parsing, scoped challenges, and auth middleware. |
| `internal/tui/model.go` + `internal/tui/admin_*.go` | Modify/Create | First-slice user CRUD, password reset, repo grants, and admin-only token management. |

## Interfaces / Contracts

```go
type Principal struct {
    Subject  string
    Username string
    IsAdmin  bool
}

type AuthorizationService interface {
    Authorize(ctx context.Context, principal *Principal, action ports.Action) error
    Challenge(action ports.Action) ports.Challenge
}
```

Postgres tables: `auth_users`, `auth_tokens`, `auth_repo_grants`. Repository names are stored as strings validated with existing `domain.ParseRepositoryRef`.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | Scope-to-role mapping, TTL checks, revoke behavior, admin bypass | Table-driven auth/domain tests |
| Integration | `/auth/token`, bearer challenge headers, catalog filtering, Postgres+SQLite coexistence | `httptest` + disposable Postgres + temp SQLite/blob store |
| E2E | `docker login/pull/push` with anonymous off; TUI admin CRUD happy path | Extend smoke scripts with auth-enabled scenario |

## Migration / Rollout

No SQLite migration required. Add auth-only Postgres schema bootstrap. Startup MUST fail fast when auth is enabled and no active global admin exists, with guidance to run `registry bootstrap-admin`. That command creates the first admin idempotently and can rotate the password only when explicitly requested.

## Open Questions

- [ ] Final CLI shape for Postgres DSN and bootstrap secret input (`--password-stdin` vs env-only).
