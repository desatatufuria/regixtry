# Design: Repository Access Control Completion (registry-acl-v1)

## Technical Approach

Three additive slices over the shipped `RepoGrant`/`Principal`/`Token` primitives.
No policy evaluator, no capability bindings, no new authorization model — the
discarded `feature/acl-policy-engine` shape is not reintroduced anywhere below.

1. **Read-only** — one `User`/`Principal` boolean and one branch in
   `hasGrantedRepositoryAccess` (`principal.go:53-65`), plus the matching branch in
   `intersectRequestedActions` (`service.go:700-731`) so read-only tokens actually
   carry `pull`.
2. **Delegation** — a *new sibling namespace* `/admin/v1/repositories/{repo}/grants`
   dispatched **above** `requireAdminPrincipal`, plus `requireAdminOrRepoAdmin` in
   the service layer. Every shipped `/admin/v1/*` route keeps its blanket gate
   untouched.
3. **Robots** — an `is_robot` flag on `auth_users` with a sentinel password hash.
   Robots are ordinary users owning ordinary grants, authenticating with the
   existing `TokenKindAdminCredential` + `LoginWithPreissuedToken` path, so
   `RepoGrant`, `Token`, `intersectRequestedActions`, and TTL/revocation are reused
   **unchanged**. Two new HTTP routes total.

Every line reference below was read from `develop`, not estimated.

## Architecture Decisions

### Decision 1: Robot rows carry a sentinel `password_hash`; `NOT NULL` is preserved

Confirms the proposal's preference.

| Option | Tradeoff | Decision |
|---|---|---|
| `is_robot BOOLEAN NOT NULL DEFAULT FALSE` + sentinel hash | Two identity kinds in one table; every listing query must choose | **Chosen** |
| Relax `password_hash` to nullable | Needs a down-migration on rollback, and `User.Validate` (`user.go:35-37`) requires a non-empty hash for *every* user — relaxing it removes a guard that protects human rows too | Rejected |
| Separate `auth_robots` table | Doubles store/CRUD surface; `auth_repo_grants` and `auth_tokens` both FK to `auth_users(id)`, so grants and tokens would need a parallel schema for materially the same outcome | Rejected |

```go
// internal/domain/auth/user.go
// RobotPasswordHash is stored in auth_users.password_hash for robot rows. It is
// deliberately NOT a valid bcrypt hash: bcrypt.CompareHashAndPassword returns
// ErrHashTooShort for it, so password login fails on the crypto layer even if
// the IsRobot guard is ever removed. Two independent layers, on purpose.
const RobotPasswordHash = "robot:no-password"
```

`User` gains `IsRobot bool` and `IsReadOnly bool`. `Validate` gains one rule:
a robot MUST NOT be `IsAdmin` and MUST NOT be `IsReadOnly` (a machine identity has
no global role).

### Decision 2: Robots bind to exactly one `(repository, role)` at creation — enforced in the service, not the schema

`auth_repo_grants` is keyed `(user_id, repository)`, so a robot *could* hold many
grants for free. The 1:1 rule is therefore a **product contract**, checked in
`CreateRobot`, not a storage constraint: `POST /admin/v1/robots` accepts exactly one
`repository` + `role` and writes exactly one grant. Widening a robot later is a
deliberate global-admin act through the existing grant routes, not an accident.
This keeps `RepoGrant`, `ListRepoGrants`, and `intersectRequestedActions` byte-identical.

### Decision 3: The narrow HTTP gate is one early-return prefix ABOVE the blanket gate — the highest-risk item

`handleAdmin` (`admin_handlers.go:19-60`) is restructured so the delegate-eligible
namespace returns **before** `requireAdminPrincipal` is ever reached:

```go
func (r *Router) handleAdmin(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	if r.admin == nil || r.auth == nil { /* unchanged 404 */ }
	subpath := strings.Trim(strings.TrimPrefix(req.URL.Path, "/admin/v1"), "/")
	if subpath == "" { /* unchanged 404 */ }

	// The ONLY delegate-eligible namespace. It is an explicit opt-in prefix
	// that returns early; authority is decided in the service layer, which
	// already has the repository argument. Everything else — including every
	// future case added to the switch below — stays under requireAdminPrincipal
	// by construction. A new route can only become delegate-eligible by being
	// added to this allow-list on purpose.
	if strings.HasPrefix(subpath, "repositories/") {
		principal, ok := r.requireAuthenticatedPrincipal(w, req)
		if !ok { return }
		r.handleAdminRepositoryResource(w, req, *principal, strings.TrimPrefix(subpath, "repositories/"))
		return
	}

	principal, ok := r.requireAdminPrincipal(w, req) // UNCHANGED
	if !ok { return }
	switch { /* every shipped case UNCHANGED */ }
}
```

| Option | Tradeoff | Decision |
|---|---|---|
| Early-return prefix allow-list above the blanket gate | One extra branch; the default for anything unlisted is still global admin | **Chosen** |
| `map[subpath]gate` table consulted per route | A route missing from the table needs a default, and a wrong default is silent; the failure mode is "new route is accidentally open" | Rejected |
| Subpath check *inside* `requireAdminPrincipal` | Puts an allow-list inside the function whose entire contract is "require admin"; every caller silently inherits it | Rejected |
| Narrow the existing `/admin/v1/users/{id}/grants` routes | Forces a delegate to know user IDs, which forces exposing the user directory — directly violating "user CRUD stays global-admin-only" | Rejected |

**Failure direction**: if someone forgets, a new delegate route requires global admin
(annoying), never the reverse (a breach).

`requireAuthenticatedPrincipal` is `requireAdminPrincipal` (`:1003-1020`) minus the
`IsAdmin` check, reusing the same `adminChallenge` and `writeAdminError` shapes.
**No `router.go` change**: `/admin/v1/` (`router.go:52`) is already a Go `ServeMux`
subtree pattern and matches `/admin/v1/repositories/...`.

Route table (repository refs contain `/`, so parsing is ordering-sensitive):

| Method | Path | Authority | Service |
|---|---|---|---|
| GET | `/admin/v1/repositories/{repo}/grants` | global admin **or** `repo-admin` on `{repo}` | `ListRepositoryGrants` |
| PUT | `/admin/v1/repositories/{repo}/grants/{username}` | same | `PutRepositoryGrant` (role in body) |
| DELETE | `/admin/v1/repositories/{repo}/grants/{username}` | same | `DeleteRepositoryGrant` |

`handleAdminRepositoryResource` splits on `strings.LastIndex(resource, "/grants/")`
and `strings.HasSuffix(resource, "/grants")` — a username can never contain `/`, but a
repository can literally be named `team/grants`. This is the same ordering hazard
already documented at `admin_handlers.go:78-84` and MUST get its own pinning test.

Grants are addressed by **username**, not user ID: a delegate must be able to name the
person without reading the user directory. Resolution is `store.GetUserByUsername`.

### Decision 4: `requireAdminOrRepoAdmin` reads `actor.Grants` directly — NOT `Principal.HasRepoAdminAccess`

```go
// internal/app/auth/service.go, beside requireAdmin (:733-739)
func requireAdminOrRepoAdmin(actor domainauth.Principal, repository string) error {
	if actor.IsAdmin {
		return nil
	}
	for _, grant := range actor.Grants {
		if grant.Repository.String() == repository && grant.Role.AllowsAdmin() {
			return nil
		}
	}
	return domainauth.NewForbiddenError("repository administrator privileges are required")
}
```

**Load-bearing**: `Principal.HasRepoAdminAccess` (`principal.go:23-29`) additionally
requires a `push` **token scope**. A TUI/admin-API login requests no scopes, and
`grantedScopes` returns `nil` for an empty request (`service.go:670-672`), so
`Principal.Scopes` is empty on exactly the tokens delegation runs on. Reusing that
helper would make delegation permanently dead code. Verified, not assumed.

Escalation bounds, enforced in `PutRepositoryGrant`/`DeleteRepositoryGrant` for
non-`IsAdmin` actors only:

- Requested role MUST be `repo-reader` or `repo-writer`. `repo-admin` is rejected —
  self-assignment is covered by the same rule, so no identity comparison is needed.
- The target's **existing** grant MUST NOT be `repo-admin` (a delegate cannot demote
  or delete a peer repository administrator).
- `IsReadOnly` is never consulted here, so a read-only principal can never delegate.

`requireAdmin` and the shipped `PutRepoGrant`/`DeleteRepoGrant`/`ListRepoGrants`
(`service.go:402, 506, 540`) keep `requireAdmin` **unchanged**; the new methods are
siblings, so the user-centric routes cannot regress.

### Decision 5: `IsReadOnly`, granted through the `allows` probe rather than a role comparison

Naming (proposal open question 4): field `IsReadOnly`, JSON/column `is_read_only`,
TUI label `Read-only`. Rejected `IsReadOnlyAdmin` (the word "admin" implies admin-surface
visibility, which is exactly what this role must not have) and `IsAuditor` (a role name
for a boolean).

```go
func (p Principal) hasGrantedRepositoryAccess(repository string, allows func(RepoRole) bool) bool {
	if p.IsAdmin {
		return true
	}
	// Registry-wide read: probe the caller's own predicate with RepoRoleReader
	// instead of comparing strings. Of the three call sites' predicates
	// (grant.go:34-44) only AllowsRead is true for RepoRoleReader, so this
	// grants read everywhere and can never widen to write or admin — even if a
	// fourth predicate is added later.
	if p.IsReadOnly && allows(RepoRoleReader) {
		return true
	}
	/* existing grant loop unchanged */
}
```

`intersectRequestedActions` gains an `isReadOnly bool` parameter and one branch before
the grant loop (`allowPull = requested.AllowsPull()`), otherwise a read-only user's
token carries zero scopes and every pull fails despite the domain check passing.
Admin exclusion is structural: `requireAdmin` and `requireAdminOrRepoAdmin` never read
`IsReadOnly`, so every `/admin/v1/*` route stays 403 for them.

### Decision 6: Robots reuse `TokenKindAdminCredential`; the login guard sits in `LoginWithPassword`, not `getActiveUserByUsername`

`TokenKind` (`token.go:8-16`) stays a closed two-value enum. A robot credential is an
ordinary `TokenKindAdminCredential` token, so `CreateAdminToken`'s TTL ceiling
(`service.go:361-367`, rejects `> DefaultAdminTokenTTL`, no never-expire path),
`RevokeAdminToken`, and `ListAdminTokens` are reused with **zero** changes.
`docker login -u <robot> -p <secret>` then flows through `LoginWithPreissuedToken`
→ `issueAccessToken` → `grantedScopes` → `intersectRequestedActions` over the robot's
one grant. No new scope derivation.

**Deliberate deviation from the proposal's wording**, with the same security outcome:
the guard is in `LoginWithPassword` (`service.go:285-295`), immediately after
`getActiveUserByUsername`, and **not inside** `getActiveUserByUsername` itself —
that helper is shared with `LoginWithPreissuedToken` (`:298`), which is the robot's only
working credential path. Guarding the shared helper would make robots unusable.
Two permanent layers remain: the per-attempt `IsRobot` rejection on the password path,
and the sentinel hash (Decision 1) that bcrypt can never match.

Robot admin surface is therefore just **two new routes**; everything else reuses the
shipped user routes, which already accept any user ID:

| Need | Route | New? |
|---|---|---|
| Create robot (+ its single grant) | `POST /admin/v1/robots` | **New** |
| List robots | `GET /admin/v1/robots` | **New** |
| Issue / list / revoke robot token | `/admin/v1/users/{id}/admin-tokens[/{accessor}]` | Reused |
| Enable / disable / delete robot | `/admin/v1/users/{id}:enable\|:disable`, `DELETE` | Reused |

Store: `ListUsers` gains `WHERE is_robot = FALSE` (robots leave the human listing);
new `ListRobots` uses `WHERE is_robot = TRUE`. Safe for `ensureAnotherActiveAdmin`
and `HasActiveGlobalAdmin` because a robot can never be `IsAdmin` (Decision 1).
`VerifyAccessToken`'s `GetUserByID` is **not** filtered — robot tokens must verify.

### Decision 7: Delegates get their own TUI entry point, not a narrowed admin screen

The TUI never learns `IsAdmin`: `/auth/token` returns only `access_token`/`expires_in`
(`admin_client.go:122-126`), and `openAdmin` (`model.go:3029-3063`) routes straight to
`screenAdminUsers`, whose first act is `ListUsers` — a guaranteed 403 for a delegate.

| Option | Tradeoff | Decision |
|---|---|---|
| New repository-centric screens reached from the existing Console Repositories screen | Two new screens; delegate never enters the admin workspace | **Chosen** |
| Reuse `screenAdminEditUserGrants` with narrower data | It is keyed by `SelectedUserID` and populated from the user list a delegate cannot fetch; user-centric where the delegate's model is repository-centric | Rejected |
| Branch the admin TUI on a capability flag | Requires a new `/auth/token` response field and a new client-side trust surface for zero benefit | Rejected |

- `screenRepoAdminGrants` (grants on one repository) and `screenRepoAdminAddGrant`
  (username + role), opened with a key on the selected row of the existing Console
  Repositories screen. They call only `/admin/v1/repositories/{repo}/grants`.
- `Model` gains `adminIntent` (`adminIntentOperator | adminIntentRepoGrants`), set before
  `screenAdminLogin` and consumed once on successful auth, so the shared login screen
  routes to the right destination. `AdminSession` is unchanged.
- Both screens join `isAdminScreen`/`isAdminPrincipalScreen` (`model.go:3187-3203`) so the
  shipped session-expiry and logout plumbing covers them; `screenRepoAdminGrants` joins
  `canLogoutAdminFromCurrentScreen` (`:3205-3218`).
- Global-admin screens: `screenAdminRobots` + `screenAdminCreateRobot` mirroring
  `screenAdminUsers`/`screenAdminCreateUser` (list, create, enable/disable/delete, issue
  token — reusing the existing token screens), and a `Read-only` toggle on the create/edit
  user forms beside the existing `Admin` toggle.

### Decision 8: The schema migration is additive; idempotency needs the existing swallow generalized

```sql
ALTER TABLE auth_users ADD COLUMN is_robot BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE auth_users ADD COLUMN is_read_only BOOLEAN NOT NULL DEFAULT FALSE;
```

`ADD COLUMN IF NOT EXISTS` is **not** usable: `migrations.go` runs against PostgreSQL in
production and against modernc SQLite in `store_test.go` (`_ "modernc.org/sqlite"`), and
SQLite has no such clause. `bootstrapSchema` (`:47-59`) already swallows exactly one
hardcoded duplicate-column message pair; generalize it over a small column list so the
shipped `scope` behavior is preserved byte-for-byte:

```go
var tolerateDuplicateColumns = []string{"scope", "is_robot", "is_read_only"}

func isDuplicateColumnError(err error, column string) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate column name: "+column) || // modernc SQLite
		strings.Contains(message, `column "`+column+`" of relation "auth_users" already exists`) // PostgreSQL
}
```

## Data Flow

    DELEGATION  PUT /admin/v1/repositories/library/alpine/grants/bob {"role":"repo-writer"}
      handleAdmin
        -> HasPrefix(subpath, "repositories/")   [early return, ABOVE requireAdminPrincipal]
        -> requireAuthenticatedPrincipal          [401 if unauthenticated; no IsAdmin check]
        -> handleAdminRepositoryResource          [LastIndex("/grants/") split]
        -> PutRepositoryGrant(actor, repo, username, role)
             requireAdminOrRepoAdmin(actor, repo) [scans actor.Grants, NOT token scopes]
             !actor.IsAdmin -> role must be reader|writer, existing role must not be admin
             GetUserByUsername -> store.PutRepoGrant   [unchanged]

    READ-ONLY   GET /v2/<repo>/manifests/<ref>
      Principal{IsReadOnly:true} -> HasReadAccess
        -> hasGrantedRepositoryAccess(repo, AllowsRead)
             IsAdmin? no -> IsReadOnly && allows(RepoRoleReader) -> true
        -> scopeAllowsRepository(pull)  [from intersectRequestedActions' read-only branch]
      GET /admin/v1/users -> requireAdminPrincipal -> 403   (IsReadOnly never consulted)

    ROBOT       POST /admin/v1/robots {name, repository, role, ttl}   [global admin only]
        -> auth_users row {is_robot:true, password_hash:RobotPasswordHash}
        -> one auth_repo_grants row
        -> CreateAdminToken  [TTL ceiling + revocation reused unchanged]
      docker login -u robot$ci -p <secret>
        -> LoginWithPreissuedToken -> issueAccessToken -> intersectRequestedActions
      docker login -u robot$ci -p <password>
        -> LoginWithPassword -> IsRobot -> InvalidCredentials  (and bcrypt could not match anyway)

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/domain/auth/user.go` | Modify | `IsRobot`, `IsReadOnly`, `RobotPasswordHash`, robot validation rule |
| `internal/domain/auth/principal.go` | Modify | `IsReadOnly` field; read branch in `hasGrantedRepositoryAccess` |
| `internal/domain/auth/grant.go` | Unchanged | Role enum already sufficient |
| `internal/domain/auth/token.go` | Unchanged | `TokenKind` stays a closed two-value enum (Decision 6) |
| `internal/app/auth/service.go` | Modify | `requireAdminOrRepoAdmin`; `List/Put/DeleteRepositoryGrant`; `CreateRobot`/`ListRobots`; `LoginWithPassword` robot guard; `intersectRequestedActions` read-only branch; read-only plumbing in `CreateUser`/`UpdateUser` |
| `internal/app/auth/service_grants_test.go` | Create | Characterization tests for today's admin-only grant CRUD (must land first) |
| `internal/ports/auth.go` | Modify | `AdminRobot*` DTOs, `is_read_only` on `AdminCreateUserInput`/`UpdateUserInput`/`AdminUser`, repository-grant + robot methods on `AuthService`/`AdminHTTPService`/`AuthStore` |
| `internal/protocol/http/admin_handlers.go` | Modify | Early-return prefix, `requireAuthenticatedPrincipal`, `handleAdminRepositoryResource`, robot collection handlers |
| `internal/protocol/http/router.go` | Unchanged | `/admin/v1/` subtree already matches the new namespace |
| `internal/infra/auth/postgres/migrations.go` | Modify | Two additive `ALTER TABLE`s; generalized duplicate-column tolerance |
| `internal/infra/auth/postgres/store.go` | Modify | Robot/read-only columns in every `auth_users` query; `ListUsers` robot filter; `ListRobots`; `ListRepoGrantsByRepository` |
| `internal/tui/model.go` | Modify | 4 new screens, `adminIntent`, screen-set updates, load/mutate commands |
| `internal/tui/session.go` | Modify | Repo-grant + robot view state and forms; read-only form field |
| `internal/tui/admin_views.go` | Modify | Robot list/create views, repository-grant views, read-only column and help text |
| `internal/tui/admin_client.go` | Modify | Repository-grant and robot client methods |
| `docs/`, roadmap | Modify | Delegation, robots, read-only role |

## Interfaces / Contracts

```
GET    /admin/v1/repositories/library/alpine/grants        -> [{username, role, created_at, updated_at}]
PUT    /admin/v1/repositories/library/alpine/grants/bob    {"role":"repo-writer"}  -> 200
       403 when the actor is neither global admin nor repo-admin on that repository
       403 when a non-global-admin sends "repo-admin", or targets an existing repo-admin
DELETE /admin/v1/repositories/library/alpine/grants/bob    -> 204

POST   /admin/v1/robots   {"name":"ci","repository":"library/alpine","role":"repo-writer","ttl_seconds":604800}
       201 {"robot":{...}, "secret":"<shown once>", "accessor":"...", "expires_at":"..."}
       400 above the DefaultAdminTokenTTL ceiling            [global admin only]
GET    /admin/v1/robots   -> [{id, username, repository, role, enabled, created_at}]

POST   /admin/v1/users    {..., "is_read_only": true}       [global admin only, unchanged gate]
```

## Testing Strategy

Strict TDD, and `PutRepoGrant`/`DeleteRepoGrant`/`ListRepoGrants` have **zero** coverage
today. **Sequencing implication for `sdd-tasks`**: characterization tests pinning today's
admin-only behavior of those three methods MUST be a task that lands and passes *before*
any delegation task — the RED test for delegation is otherwise written against code whose
current behavior was never captured.

| Layer | What to Test | Approach |
|---|---|---|
| Unit | Characterization: each of the three grant methods rejects a non-admin actor and succeeds for an admin (current behavior, before any change) | Table-driven, `internal/app/auth` |
| Unit | `hasGrantedRepositoryAccess`: read-only allows read on any repository, denies write and admin, and an unflagged principal is unchanged | Table-driven over all three predicates |
| Unit | `intersectRequestedActions`: read-only yields `pull` only; admin and grant paths byte-identical | Table-driven |
| Unit | `requireAdminOrRepoAdmin`: admin passes; repo-admin on the exact repo passes; repo-admin on another repo, repo-writer, read-only, and empty-`Scopes` principals are asserted separately | Table-driven |
| Unit | Delegate is rejected for `repo-admin`, for self-assigning `repo-admin`, and for touching an existing repo-admin — three separate cases | Table-driven |
| Unit | `User.Validate` rejects a robot that is admin or read-only; `RobotPasswordHash` never satisfies `bcrypt.CompareHashAndPassword` | Table-driven |
| Unit | `LoginWithPassword` rejects a robot; `LoginWithPreissuedToken` accepts one | Two service tests |
| Unit | Robot token creation above the TTL ceiling is rejected; revocation takes effect immediately | Service tests |
| Unit | TUI: `adminIntent` routes post-login to `screenRepoAdminGrants`; new screens are in `isAdminScreen`/`isAdminPrincipalScreen`; robot screens render | Direct `Model.Update()` |
| Integration | **Every** non-grant `/admin/v1/*` route still 403s a non-admin authenticated principal, enumerated route by route | Handler table test (highest-risk guard) |
| Integration | `/admin/v1/repositories/.../grants` 401s when unauthenticated, 403s a non-delegate, 200s a delegate | Handler tests |
| Integration | A repository literally named `team/grants` routes correctly under all three methods | Handler test (Decision 3 hazard) |
| Integration | Read-only principal reads catalog/tags/manifests, is rejected on push, and is rejected on the admin user **and** grant listings | Router tests |
| Integration | Robots absent from `ListUsers`, present in `ListRobots`; robot pull/push follows its grant | Store + service tests |
| Integration | Migration re-run is idempotent on a store that already has both columns | `store_test.go`, modernc SQLite |
| Integration | Users with neither flag nor delegation behave byte-identically | Existing suite passes unmodified |

## Threat Matrix

Applicable: this change adds HTTP route dispatch and an authorization boundary.
No subprocess, no argv, no shell, no VCS/PR automation, no executable-file classification.

| Boundary | Applicability | Design response | Planned RED test |
|---|---|---|---|
| Documentation-like paths | N/A — no path drives an execution decision | — | — |
| Git / PR automation | N/A — none invoked | — | — |
| Subprocess argv | N/A — no subprocess | — | — |
| **HTTP path dispatch (gate narrowing)** | **Applicable** — a new namespace bypasses the blanket admin gate | Single explicit prefix, early-return above the gate; everything unlisted stays global-admin by default | Every non-grant `/admin/v1/*` route 403s a non-admin, enumerated |
| **Path parsing with `/` in the resource** | **Applicable** — repository refs contain `/`; a repo named `team/grants` could shadow the sub-resource split | `LastIndex("/grants/")` + `HasSuffix("/grants")`; username can never contain `/` | A repository named `team/grants` routes correctly for GET/PUT/DELETE |
| **Privilege escalation via delegation** | **Applicable** — a delegate could mint peers or self-promote | Role allow-list plus an existing-role check, both service-layer, both applied only when `!actor.IsAdmin` | Three separate rejection tests |
| **Machine identity authenticating as a human** | **Applicable** — robots share `auth_users` | Per-attempt `IsRobot` guard on the password path plus a sentinel hash bcrypt cannot match | Password login as a robot fails with both layers, and with the guard removed |
| **Identity disclosure** | **Applicable** — grant-by-username reveals whether a username exists to a delegate | Accepted and bounded: a delegate must name the person to grant, and the response carries no user attributes beyond the grant | Delegate responses contain no user ID, hash, flags, or other repositories |

## Migration / Rollout

Additive only: two `ALTER TABLE ... ADD COLUMN ... DEFAULT FALSE` statements appended to
`migrationStatements()`, tolerated on re-run by the generalized duplicate-column guard.
No dropped or narrowed column, no data rewrite, no `password_hash` change.

Inert on boot: with both columns defaulting to `FALSE` and no robot rows, every code path
above takes its existing branch, so authorization is byte-identical until an operator sets a
flag or creates a robot.

Rollback is `git revert`. A reverted binary ignores both columns: robot rows become inert
`auth_users` rows that cannot log in (the sentinel hash survives the revert), and read-only
principals fall back to grant-only access. Delegated grants are ordinary
`auth_repo_grants` rows and keep working. Operationally, revoking robot tokens and clearing
the read-only flag restores pre-change authorization with no deploy.

## Open Questions

- [ ] Should `POST /admin/v1/robots` return an issued token in the same response (as designed)
      or force a second call to the reused token route? The single-call shape means the secret
      is shown once at creation, matching Harbor; the two-call shape is one fewer branch.
- [ ] Robot username namespacing (e.g. a reserved `robot$` prefix) is not designed here.
      `usernamePattern` (`user.go:9`) rejects `$`, so a prefix convention would need a pattern
      change; the `is_robot` flag already separates the kinds without one.
- [ ] `ListRepoGrantsByRepository` has no index: `auth_repo_grants`' primary key is
      `(user_id, repository)`, so a per-repository listing is a scan. Acceptable at current
      scale; a `repository` index is a one-line additive migration if it is ever measured.
