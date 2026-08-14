# Authentication

## Activation

Auth is activated by passing `-auth-postgres-dsn` or setting `REGISTRY_AUTH_POSTGRES_DSN`. On startup, `internal/infra/auth/postgres/migrations.go` creates the required tables in Postgres if they do not already exist. The runtime refuses to start if auth is configured but no active global admin exists (`Service.EnsureBootstrapAdmin`).

## Bootstrap

```bash
printf '%s\n' '<admin-password>' | regixtry bootstrap-admin \
  -auth-postgres-dsn 'postgres://USER:PASSWORD@HOST:5432/regixtry_auth?sslmode=disable' \
  -username admin -password-stdin
```

`-password` exists but the CLI warns that it exposes the secret in argv; prefer `-password-stdin`. `-rotate-password` rotates the password of an existing admin instead of failing with a conflict.

## Challenge and token exchange

A protected request that lacks valid credentials receives `401` with `WWW-Authenticate: Bearer realm="...",service="...",scope="..."`. The client resolves the challenge against `/auth/token` using HTTP Basic:

```bash
curl -u USERNAME:PASSWORD \
  'https://registry.example.com/auth/token?service=regixtry&scope=repository:team/image:pull'
```

The response carries `token`, `access_token`, `expires_in` (`900`), `issued_at`, and, when applicable, `scope`. The same endpoint also accepts a pre-issued admin credential token in place of a password as the Basic secret — this is how `docker login` authenticates both human admins and robot accounts:

```bash
printf '%s\n' '<PASSWORD>' | docker login registry.example.com -u USERNAME --password-stdin
docker logout registry.example.com
```

### Two token kinds

`internal/domain/auth/token.go` defines exactly two token kinds — there is no separate kind for robots:

| Kind | TTL | Used for |
| --- | --- | --- |
| `TokenKindAccess` | 15 minutes (`AccessTokenTTL`) | Short-lived Bearer tokens issued to Docker clients after `/auth/token` |
| `TokenKindAdminCredential` | Up to 30 days (`DefaultAdminTokenTTL`), revocable | Pre-issued credentials used both for human "admin tokens" (issued via the TUI or `/admin/v1/users/{id}/admin-tokens` for scripting) and for robot account secrets |

Robots authenticate through `LoginWithPreissuedToken`, the exact same code path human admin credential tokens use — no separate robot login mechanism exists.

Password credentials are verified with bcrypt; stored tokens and pre-issued credentials keep only a SHA-256 hash of the secret, never the plaintext.

## Robot account login is deliberately double-blocked from password auth

Robot accounts (`User.IsRobot`) can never authenticate with `LoginWithPassword`, enforced by two independent layers:

1. `Service.LoginWithPassword` rejects any user with `IsRobot == true` before it even inspects the supplied password.
2. Independently, a robot's stored `password_hash` is the `RobotPasswordHash` sentinel constant (`internal/domain/auth/user.go`) — a value that is deliberately not a valid bcrypt hash. Even if the `IsRobot` guard above were ever removed, `bcrypt.CompareHashAndPassword` still fails on this value.

Robots are created and deleted only through the global-admin-only `/admin/v1/robots` API (see [users.md](users.md)); they have no self-service login or password-reset path.

## Authorization model

Repository roles: `repo-reader`, `repo-writer`, `repo-admin` (`domainauth.RepoRole`, `internal/domain/auth/grant.go`). Reader allows pulling, writer allows pulling and pushing, and admin additionally allows managing that one repository's grants through the delegate API (below). `RepoRole.AllowsRead/AllowsWrite/AllowsAdmin` define this hierarchy: reader implies neither write nor admin, writer implies read, admin implies both.

Beyond per-repository grants, `Principal` (`internal/domain/auth/principal.go`) recognizes two account-wide modifiers:

- **`IsAdmin`** — a global administrator. `hasGrantedRepositoryAccess` short-circuits to full access on every repository for any read/write/admin check, bypassing per-repository grants entirely. A token still must carry a scope compatible with the requested operation; global admin removes the grant check, not the scope check.
- **`IsReadOnly`** — the registry-wide read-only role. `hasGrantedRepositoryAccess` grants read access on every repository with no explicit grant required, and this can never widen to write or catalog-admin access — the read-only probe is only ever evaluated against `RepoRole.AllowsRead`. See [security.md](security.md) for the security scope of this role.

Valid token scopes are `repository:<name>:pull`, `repository:<name>:push` (or both), and `regixtry:catalog:*`.

## `/admin/v1` route inventory

Every route under `/admin/v1` requires `Authorization: Bearer <access-token>`. Authorization then splits into two tiers:

### Global-admin-only routes (`Principal.IsAdmin`)

- `GET /admin/v1/users`
- `POST /admin/v1/users`
- `POST /admin/v1/users/{id}:enable`
- `POST /admin/v1/users/{id}:disable`
- `POST /admin/v1/users/{id}:reset-password`
- `GET /admin/v1/users/{id}/grants`
- `PUT /admin/v1/users/{id}/grants/{repository}`
- `DELETE /admin/v1/users/{id}/grants/{repository}`
- `GET /admin/v1/users/{id}/admin-tokens`
- `POST /admin/v1/users/{id}/admin-tokens`
- `DELETE /admin/v1/users/{id}/admin-tokens/{accessor}`
- `GET /admin/v1/robots`
- `POST /admin/v1/robots`
- `DELETE /admin/v1/robots/{id}`
- `/admin/v1/features...`, `/admin/v1/scan-settings`, `/admin/v1/scan-policy`, `/admin/v1/signing-policy`, `/admin/v1/scan-runs...`, `/admin/v1/secret-scan-findings` (feature/scanning administration, unchanged by this ACL work)

These routes are dispatched behind `handleAdmin`'s blanket `requireAdminPrincipal` gate (`internal/protocol/http/admin_handlers.go`), which rejects any non-admin principal with `403`.

### Authenticated-but-not-necessarily-admin route (delegated repo-admin)

- `GET /admin/v1/repositories/{repo}/grants`
- `PUT /admin/v1/repositories/{repo}/grants/{username}`
- `DELETE /admin/v1/repositories/{repo}/grants/{username}`

This is the **one deliberate, narrow exception** to "every `/admin/v1` route requires global admin." `handleAdmin` checks the `repositories/` URL prefix *before* the blanket `requireAdminPrincipal` gate and routes it through `requireAuthenticatedPrincipal` instead, which only requires a valid Bearer token — not `IsAdmin`. Authorization for these three routes is then decided in the service layer by `requireAdminOrRepoAdmin` (`internal/app/auth/service.go`), which passes for a global admin *or* for a principal holding a `repo-admin` grant on that exact repository (read from `actor.Grants`, populated for every login regardless of requested scopes). See [security.md](security.md) for the escalation bounds enforced on this delegated path, and [users.md](users.md) for usage examples.

No other route under `/admin/v1` is reachable by a non-admin principal; this exception applies only to the `repositories/` prefix and only to grant management on the caller's own administered repository.

Full request/response bodies are documented in [api.md](api.md). There is no delete-user endpoint — human users are only ever enabled or disabled, never hard-deleted (robots are the one exception; see [users.md](users.md)).
