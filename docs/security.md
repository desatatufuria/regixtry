# Security

## Confirmed by the code

- User passwords are verified with bcrypt.
- Stored tokens and pre-issued credentials keep only a SHA-256 hash of the secret, never the plaintext.
- Access tokens expire after 15 minutes (`TokenKindAccess`); admin credential tokens (`TokenKindAdminCredential`, used for both human admin tokens and robot secrets) are bounded to 30 days by default and are revocable.
- Repository grants apply the roles `repo-reader`, `repo-writer`, `repo-admin` (`RepoRole.AllowsRead/AllowsWrite/AllowsAdmin`).
- `/admin/v1` requires a valid Bearer token for every route. Almost every route additionally requires `Principal.IsAdmin` — with one deliberate, narrow exception described below.
- TLS termination directly on the binary requires both a certificate and a key together; the configuration rejects an inconsistent combination (one present, one missing).
- The installer validates SHA-256 checksums of release artifacts.

## `/admin/v1` authority: one deliberate exception

`handleAdmin` (`internal/protocol/http/admin_handlers.go`) gates every subpath behind `requireAdminPrincipal` — global admin only — **except** the `repositories/` prefix, which it recognizes and routes *before* that gate is applied. This is intentional and narrow, not a general weakening of `/admin/v1`:

- `GET /admin/v1/repositories/{repo}/grants`
- `PUT /admin/v1/repositories/{repo}/grants/{username}`
- `DELETE /admin/v1/repositories/{repo}/grants/{username}`

These three routes only require an authenticated principal (`requireAuthenticatedPrincipal` — any valid Bearer token). The actual authorization decision is made in the service layer by `requireAdminOrRepoAdmin`, which passes for a global admin or for a principal holding a `repo-admin` grant on that exact repository. Every other route under `/admin/v1` — including every future one added to `handleAdmin`'s switch — stays behind the blanket admin gate by construction; becoming delegate-eligible requires deliberately adding a route to this allow-list.

One implementation detail worth knowing if you're reasoning about this boundary from the domain layer: authorization here reads `actor.Grants` directly rather than calling `Principal.HasRepoAdminAccess`. That method additionally requires a push-scoped token, but TUI/admin-API logins request zero scopes — using it here would make delegation permanently unreachable on exactly the tokens this endpoint is meant to serve.

### Delegated repo-admin: security boundary

A repo-admin delegate is a genuine, bounded privilege escalation *within one repository* — not a scoped-down global admin. What it can do:

- List, grant, and revoke `repo-reader`/`repo-writer` on the one repository it administers.

What it structurally cannot do, enforced server-side (not just hidden in the UI):

- Grant or self-assign `repo-admin` on that repository — rejected outright by `PutRepositoryGrant`.
- Modify or remove another repo-admin's grant on that same repository.
- Reach any other `/admin/v1` route, or manage grants on any repository it does not administer — `requireAdminOrRepoAdmin` re-checks the exact target repository on every call.

Only global `IsAdmin` can grant `repo-admin` or touch another repo-admin's grant. Treat a `repo-admin` grant as "trusted to manage membership of this one repository," not "trusted with the registry."

## Robot accounts

Robot accounts (`User.IsRobot`) are CI/CD machine identities with a narrower attack surface than human accounts by design:

- **Two independent layers block password login.** `Service.LoginWithPassword` rejects any `IsRobot` user before it inspects the password at all; separately, a robot's stored password hash is the `RobotPasswordHash` sentinel, which is not a valid bcrypt hash, so `bcrypt.CompareHashAndPassword` fails on it even if the `IsRobot` guard were ever removed. This is deliberate defense in depth, not redundancy by accident.
- **Admin-only, no delegation.** Creation (`POST /admin/v1/robots`), listing, and deletion (`DELETE /admin/v1/robots/{id}`) all require global `IsAdmin` — a repo-admin delegate cannot create or manage robots even on its own repository.
- **Hard delete, unlike human users.** `DeleteRobot` performs a real row delete, not a disable. The handler rejects any target where `!IsRobot`, so this endpoint cannot be repurposed to hard-delete a human account. Deletion relies on `ON DELETE CASCADE` on the robot's grant and token foreign keys, so no orphaned grant or token survives a robot deletion.
- **One grant, one secret, shown once.** A robot is bound to a single `(repository, role)` pair at creation, and its credential secret is a normal `TokenKindAdminCredential` token — same TTL ceiling, same revocability as a human admin token — returned only in the creation response.

## The registry-wide read-only role

`IsReadOnly` grants pull access to *every* repository with no per-repository grant needed. Its scope is deliberately narrow:

- Never grants push, on any repository — `hasGrantedRepositoryAccess`'s read-only branch is only ever evaluated against `RepoRole.AllowsRead`, so it structurally cannot widen to write or catalog-admin checks even if a caller mistakenly probes with a different predicate.
- Grants no admin-surface access — a read-only user cannot reach `/admin/v1` routes or the delegated repository-grants namespace.
- Is mutually exclusive with `IsRobot` at the domain level (`User.Validate` rejects a robot marked read-only).

## TUI local browsing is not access-controlled — by design

**This is the single most important thing an operator should understand about this system's boundaries.** The TUI's local Console — repository list, tags, manifest inspection, blobs, uploads, signature status — performs **no per-user authorization at all** when talking to the local registry. See [`docs/architecture.md`](architecture.md#3-tuis-dual-localhttp-nature) for how this local channel is wired.

- `tui.QueryService` (`internal/tui/model.go`) — the interface backing `RepositorySummaries`, `TagDetails`, `ResolveManifest`, `Uploads`, and `SignatureStatus` — takes no `Principal` or actor argument on any of its five methods. There is no identity to check access against at that layer.
- `runTUI` (`cmd/regixtry/main.go`) wires this local query path through `localOperatorAccessController{}`, whose `Authorize` method ignores both its arguments and unconditionally returns `nil`.

**Net effect:** anyone with a local TUI session can browse every repository's tags, manifests, blobs, and upload state, regardless of what grants that user actually holds. This is separate from, and does not affect, the TUI's *authenticated admin area* (users, grants, robots, tokens, features), which — when `-api-base-url` is configured — authenticates against `/auth/token` and is subject to the normal `/admin/v1` authorization rules described above.

The real, enforced access-control boundaries in this system are exactly two:

1. **The Docker registry protocol** — `/v2/...` — grant- and scope-checked via `Principal.HasReadAccess`/`HasWriteAccess`.
2. **The admin API** — `/admin/v1/...` — checked via `Principal.IsAdmin` or, for the one delegated namespace above, `requireAdminOrRepoAdmin`.

Local TUI inspection is not, and was never designed to be, a third enforcement boundary. If you need to restrict who can see which repositories, that restriction has to happen at the network/host level — e.g., not exposing local TUI access to users who shouldn't see everything — not by relying on the Console to hide anything.

## Operational recommendations

- Prefer `-password-stdin` over `-password` for `bootstrap-admin`.
- Do not expose PostgreSQL or the local HTTP listener without a controlled network boundary.
- Use direct HTTPS or a TLS-terminating reverse proxy on untrusted networks.
- Protect `regixtry.env`, bootstrap state, the SQLite database, and the blob filesystem.
- Store an admin token's or robot's secret only at creation time — it cannot be retrieved again.
- Treat local TUI access as equivalent to full read access to every repository in the registry; restrict who can reach it accordingly.

These are recommendations, not guarantees enforced by the program.

## Risks and limits

- `local-http` transmits credentials and tokens unencrypted if used outside a trusted network.
- The generated env file may contain the Postgres DSN; its real permissions derive from how bootstrap wrote it and should be reviewed operationally.
- No metrics, rate limiting, or audit logging of access-control events are confirmed in the code.
- ⚠️ A formal review of the complete OCI-facing attack surface has not been confirmed from the current code.
