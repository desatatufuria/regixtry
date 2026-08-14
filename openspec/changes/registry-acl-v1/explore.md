# Exploration: registry-acl-v1 (closing gaps in existing RBAC)

## Goal

Close three specific, verified gaps in regixtry's existing repo-scoped RBAC
system without rebuilding it. A prior branch `feature/acl-policy-engine`
attempted a typed Subject/Resource/Action/Context/Decision policy engine with
capability bindings and is explicitly discarded as over-engineered for this
codebase's actual needs — cited here only as a cautionary example, not reused.

## Current State (verified against `develop`, corrections noted)

The existing RBAC foundation is real and matches the brief closely, with one
important correction: authorization for the admin HTTP surface happens at
**two independent layers**, not one.

- `internal/domain/auth/grant.go:1-68` — `RepoGrant{UserID, Repository, Role,
  CreatedAt, UpdatedAt}`, `RepoRole` = `repo-reader | repo-writer |
  repo-admin` with `AllowsRead()/AllowsWrite()/AllowsAdmin()`. Confirmed
  verbatim as described.
- `internal/domain/auth/principal.go:1-85` — `Principal{Subject, UserID,
  Username, IsAdmin, Grants, Scopes, ExpiresAt}`. Its `hasGrantedRepositoryAccess`
  helper (line 53-65) **short-circuits to `true` for every repository when
  `p.IsAdmin`** — this is the concrete mechanism behind gap #3: global admin
  is structurally all-or-nothing, there is no partial "read everywhere" tier.
- `internal/app/auth/service.go` — `ListRepoGrants` (402-411),
  `PutRepoGrant` (506-529), `DeleteRepoGrant` (540-554) each independently
  call `requireAdmin(actor)` (733-739), which only checks `actor.IsAdmin`.
  No delegation path exists at the service layer. `PutRepoGrant`/service
  methods have **no covering unit tests** today (confirmed via codegraph
  blast-radius: "no covering tests found").
- **Correction to the brief**: `internal/protocol/http/admin_handlers.go`'s
  `handleAdmin` (line 19-33) is the single entry point for the entire
  `/admin/v1/*` namespace and calls `requireAdminPrincipal` (line 1003-1020)
  **before any route dispatch** — this rejects with 403 unless
  `principal.IsAdmin`, independent of and prior to the service-layer
  `requireAdmin()` calls. Grant routes (`/admin/v1/users/{id}/grants`,
  `/grants/{repo}` — `handleAdminUserGrantsCollection` line 780-794,
  `handleAdminUserGrantResource` line 796+, dispatched from
  `handleAdminUserResource` line 724-778) sit behind this same blanket gate.
  This means delegated repo-admin (gap #1) cannot be solved by loosening
  `requireAdmin()` alone — the HTTP router's door gate has to change too, or
  grant mutation needs a distinct, narrower route/dispatch path that bypasses
  the blanket global-admin check while still authenticating the principal.
- TUI: `screenAdminEditUserGrants` / `screenAdminAddGrant` confirmed in
  `internal/tui/model.go:128-129` (screen enum) and referenced in
  `internal/tui/admin_views.go:90,92,122,124`. Grant CRUD screens exist and
  are reachable only from the admin-authenticated TUI flow (`isAdminScreen`
  / `isAdminPrincipalScreen`, model.go:3187-3203), which itself assumes a
  human `IsAdmin` operator session (`AdminViewState`, session.go:368-438).
- `internal/domain/auth/token.go:1-106` — confirmed: `TokenKind` is a closed
  two-value enum (`TokenKindAccess`, `TokenKindAdminCredential`),
  `Token.Validate()` (line 31-77) enforces exactly these two kinds. Every
  `Token` requires a non-empty `UserID` and every `auth_tokens` row has
  `FOREIGN KEY(user_id) REFERENCES auth_users(id) ON DELETE CASCADE`
  (`internal/infra/auth/postgres/migrations.go:20-32`) — tokens cannot exist
  without a backing `auth_users` row today, confirming "every token traces
  to a human user."
- `internal/infra/auth/postgres/migrations.go:11-19` — `auth_users.password_hash
  TEXT NOT NULL`: every user row, including any future robot identity reusing
  this table, would need *some* password_hash value (even if a random/unusable
  one) unless the schema changes. `auth_repo_grants` (line 35-43) is also
  hard-FK'd to `auth_users(id)` — grants cannot target a non-`auth_users` row
  without either widening this table or adding a parallel schema.
- No `robot`, `service account`, or `ServiceAccount` identity concept exists
  anywhere in `internal/` today (verified via case-insensitive grep across
  `internal/` — zero matches) — gap #2 confirmed as a true gap, not partially
  built.
- `openspec/changes/registry-auth-v1/exploration.md` (pre-rename, references
  `internal/app/registry` not `internal/app/auth` — confirms it predates the
  `regixtry` module rename) explicitly deferred, in its own words: "token
  self-service policy UX" and "broad TUI user-administration workflows beyond
  minimal admin CRUD/listing." That matches exactly what remains undone today
  — delegation, robot accounts, and a read-only global role were never
  in scope for v1, not accidentally dropped.
- Docker-scope derivation (`internal/app/auth/service.go:669-729`,
  `intersectRequestedActions`) already computes granted `pull`/`push` actions
  per repository from `RepoGrant.Role` — this is the exact mechanism a robot
  account's token would need to reuse unchanged; no new scope-derivation
  logic is implied by gap #2, only a new identity source feeding the same
  function.

## Affected Areas

- `internal/domain/auth/grant.go` — no change needed for gap #1/#3 (role enum
  already sufficient); gap #2 may need a new `RepoRole`-compatible robot
  identity type, not a new role value.
- `internal/domain/auth/principal.go` — `hasGrantedRepositoryAccess` (gap #3:
  needs a registry-wide read-only principal path distinct from `IsAdmin`).
- `internal/app/auth/service.go` — `requireAdmin` (gap #1: needs a
  per-repository delegation variant, e.g. `requireAdminOrRepoAdmin(actor,
  repository)`), `PutRepoGrant`/`DeleteRepoGrant`/`ListRepoGrants` (gap #1
  callers), token issuance path (gap #2: new identity source).
- `internal/protocol/http/admin_handlers.go` — `handleAdmin`/`requireAdminPrincipal`
  (gap #1: the blanket door gate needs a narrower per-route check for grant
  mutation; this is the corrected, larger-than-briefed part of gap #1).
- `internal/domain/auth/token.go` — `TokenKind` enum (gap #2: new kind or
  reuse `TokenKindAccess` with a robot-sourced `Principal`).
- `internal/infra/auth/postgres/migrations.go`, `store.go` — schema changes
  for gap #2 (new table or nullable/robot-flagged `auth_users` columns).
- `internal/tui/model.go`, `internal/tui/admin_views.go`, `internal/tui/session.go`
  — new "Robot Accounts" screen (gap #2) mirroring existing user/token
  screens; grant screens already exist for gap #1, may need a "your repos"
  filtered view for delegated repo-admins.
- `internal/ports/auth.go` — `AuthService`/`AdminHTTPService` interfaces need
  new methods for gap #2 (robot CRUD) and possibly a narrower grant-management
  interface for gap #1.

## Three Candidate Gaps

### Gap 1 — Delegated repo-admin management

**Problem**: a `repo-admin` grant holder cannot manage grants on their own
repository; only global `IsAdmin` can, and that check is enforced twice
(HTTP door gate + service layer).

1. **Narrow the HTTP door gate for grant routes only** — add a second,
   narrower principal check (`requireAuthenticatedPrincipal`, no `IsAdmin`
   requirement) used exclusively by the grants sub-routes, pushing the
   admin-vs-repo-admin distinction down into the service layer where
   `PutRepoGrant`/`DeleteRepoGrant`/`ListRepoGrants` already have the
   `repository` argument needed to check delegated authority.
   - Pros: minimal surface change; reuses existing service methods almost
     as-is; keeps the rest of `/admin/v1/*` (user CRUD, tokens, features)
     fully global-admin-only, unaffected.
   - Cons: introduces a second HTTP-level authorization tier that every
     future admin route addition must remember to choose correctly between.
   - Effort: Medium.
2. **Keep the blanket HTTP gate, add delegation only inside `requireAdmin`
   call sites** — impossible as-is, since non-admins are already rejected
   with 403 before reaching the service layer; would require deleting this
   option or restructuring `handleAdmin` regardless. Not viable without (1)'s
   change, listed only to rule it out explicitly.
   - Effort: N/A (blocked by the HTTP-layer gate; cannot be done in isolation).

**Recommendation**: Approach 1. This is a real, if modest, expansion of the
admin authorization model — not a trivial one-line service change as the
initial brief implied, because of the HTTP-layer gate.

### Gap 2 — Robot/service accounts

**Problem**: no standalone machine identity exists; every token requires a
human `auth_users` row with a password hash.

1. **New `is_robot` flag + nullable password on `auth_users`, reusing
   `RepoGrant`/`Token` unchanged** — robots become ordinary `auth_users` rows
   that can never authenticate via `LoginWithPassword` (guarded by the flag),
   only ever produce a single scoped access-style token, tied 1:1 to a
   `(repository, role)` at creation. Grants and token FKs need zero schema
   change.
   - Pros: maximum reuse of `RepoGrant`, `Token`, `intersectRequestedActions`,
     `ListRepoGrants` unchanged; TUI robot screen can mirror the existing
     user list screen almost verbatim.
   - Cons: `auth_users.password_hash NOT NULL` needs relaxing (migration);
     conflates two identity kinds in one table, so every future `auth_users`
     query needs to remember to filter by kind where it matters (e.g. TUI
     user list should probably exclude robots by default).
   - Effort: Medium.
2. **Separate `auth_robots` table, parallel to `auth_users`** — dedicated
   schema with its own `(name, repository, role, expires_at)` shape, its own
   store methods, reusing only `Token`/`intersectRequestedActions` at the
   edges.
   - Pros: no `auth_users` schema/nullability change; clean separation, no
     risk of a robot silently appearing in a human user listing.
   - Cons: doubles CRUD/store surface (new table, new queries, new admin
     endpoints) instead of reusing `ListUsers`/`CreateUser` shape; more new
     code for materially the same outcome.
   - Effort: Medium-High.

**Recommendation**: Approach 1 (flag on `auth_users`), matching Harbor's
robot-account precedent (a distinct identity class, not a separate resource
hierarchy) while minimizing new schema/store surface in a codebase that
already keeps auth in one Postgres schema.

### Gap 3 — Registry-wide read-only role

**Problem**: `hasGrantedRepositoryAccess` grants full read/write/admin on
every repository when `IsAdmin` is true; there is no way to grant "read
everywhere" without either full admin or one `repo-reader` grant per
repository.

1. **New `Principal.IsReadOnlyAdmin` (or equivalent) boolean, checked in
   `hasGrantedRepositoryAccess` for `AllowsRead` only** — mirrors the exact
   shape of the existing `IsAdmin` short-circuit but scoped to read.
   - Pros: smallest possible change, one new field plus one new branch in an
     already-small function; naturally excluded from `requireAdmin()` (still
     admin-only for user/grant management), so it cannot accidentally expand
     into gap #1's territory.
   - Cons: yet another boolean flag on `User`/`Principal` alongside `IsAdmin`
     — two independent global flags to reason about instead of one role enum;
     needs its own HTTP admin-user-CRUD field (`AdminCreateUserInput`,
     `UpdateUserInput`) and TUI form field.
   - Effort: Low.
2. **Implicit `repo-reader` grant on every existing + future repository at
   creation/push time** — no new field; instead, auto-provision a grant row
   whenever a repository is first created.
   - Pros: no new `Principal` field.
   - Cons: does not solve "read-only on repositories that don't exist yet"
     without hooking repository creation, which regixtry does not gate today
     (repos come into existence implicitly on push) — this becomes a
     structurally worse fit than it first appears, and grant-row bloat scales
     with repository count.
   - Effort: Medium, and arguably wrong shape.

**Recommendation**: Approach 1 — a second boolean global role, not a new
enum or a repo-provisioning hook.

## Recommendation (overall)

Sequence as three independently reviewable, roughly PR-sized slices rather
than one combined change, in this order: Gap 3 (smallest, lowest risk) →
Gap 1 (requires the HTTP-layer correction, medium) → Gap 2 (largest, schema
change). None of the three depend on each other's code, only on the shared
`requireAdmin`/`Principal` seam, so sequencing is a review-load choice, not
a technical dependency.

## Risks

- Strict TDD is enabled for this project; `PutRepoGrant`/`DeleteRepoGrant`/
  `ListRepoGrants` have zero existing unit test coverage today (confirmed via
  codegraph), so gap #1's delegation logic needs tests written test-first
  against currently-untested code, not just extended existing tests.
- The HTTP-layer `requireAdminPrincipal` blanket gate is easy to weaken
  incorrectly — a narrowed check for grant routes must not accidentally
  leak into user CRUD, token issuance, or feature-config routes that share
  `handleAdmin`'s dispatch.
- `auth_users.password_hash NOT NULL` and the FK-cascade shape mean gap #2's
  schema change (nullable password or an `is_robot` flag) touches the same
  table every login/authenticate path reads from — needs care that
  `LoginWithPassword`/`getActiveUserByUsername` correctly and permanently
  reject robot rows, not just at creation time.
- Two independent global boolean flags (`IsAdmin`, plus a new read-only flag
  for gap #3) is a minor but real increase in `Principal`/`User` surface
  area; every future global-role addition should be weighed against this
  precedent before a third flag is added.
- `feature/acl-policy-engine` exists as a real prior attempt at this same
  problem space and was discarded — worth confirming with the user/reviewer
  that no code or design decisions from that branch are unintentionally
  reintroduced during design/tasks.

## Open Questions Requiring a Product Decision

1. Gap 1: should delegated repo-admins be able to grant `repo-admin` to
   others on their repo (privilege escalation risk), or only `repo-reader`/
   `repo-writer`?
2. Gap 2: should robot tokens be revocable/rotatable the same way admin
   credential tokens are today, and do they need a max TTL ceiling like
   `DefaultAdminTokenTTL`?
3. Gap 3: should the read-only global role also see the admin user list
   (read-only), or strictly registry content (catalog/tags/manifests) only?

## Ready for Proposal

Yes — all three gaps have verified, concrete affected-area evidence and a
clear recommended approach each. The one correction from the initial brief
(HTTP-layer `requireAdminPrincipal` gate for gap #1) should be carried into
`sdd-propose` explicitly, since it changes gap #1's effort estimate from
"three service methods" to "HTTP router + service layer."
