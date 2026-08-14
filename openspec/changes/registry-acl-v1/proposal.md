# Proposal: Repository Access Control Completion (registry-acl-v1)

## Intent

regixtry's repo-scoped RBAC (`RepoGrant`, `RepoRole`, `Principal`) ships reader/writer/admin,
but every access decision still funnels through one global `IsAdmin`. Three verified gaps:

1. **No delegation.** A `repo-admin` grant holder cannot manage grants on their own repository.
   `handleAdmin` rejects non-admins before route dispatch (`admin_handlers.go:19-33`,
   `requireAdminPrincipal:1003-1020`), and `requireAdmin` (`service.go:733-739`) rejects again.
2. **No machine identity.** Every `Token` FKs to an `auth_users` row with
   `password_hash NOT NULL` (`migrations.go:11-32`); CI must borrow a human's credentials, and
   offboarding that human breaks the pipeline.
3. **No read-only tier.** `hasGrantedRepositoryAccess` (`principal.go:53-65`) short-circuits to
   full access when `IsAdmin`, so "read everything" means full admin or one grant per repository.

The cost is operational: one bottleneck human, shared credentials, and auditors/SREs handed
write+admin to get read.

## Scope

### In Scope

- **Delegated repo-admin.** A narrower authenticated-principal check for the grants sub-routes
  only, plus a service-layer `requireAdminOrRepoAdmin(actor, repository)`. A delegate MAY grant
  `repo-reader`/`repo-writer` on repositories where they hold `repo-admin`; they MUST NOT grant,
  transfer, or self-assign `repo-admin` — only global `IsAdmin` creates repo-admins. Delegates
  see only their own repositories' grants.
- **Robot accounts.** A distinct, non-interactive identity that can never log in with a password,
  owns scoped `RepoGrant`s, and issues access tokens through the existing
  `intersectRequestedActions` scope derivation unchanged. Tokens are revocable at any time and
  bounded by a maximum TTL ceiling mirroring `DefaultAdminTokenTTL`; **no never-expiring token**.
- **Registry-wide read-only role.** A second global flag on `User`/`Principal`, honored in
  `hasGrantedRepositoryAccess` for reads only. It sees registry content (catalog, repositories,
  tags, manifests) and MUST NOT see the admin user/grant listing or any `/admin/v1/*` mutation.
- **TUI**: robot account screens mirroring existing user/token screens; grant screens filtered to
  the delegate's own repositories; read-only flag on user create/edit forms.
- **Tests first** for `PutRepoGrant`/`DeleteRepoGrant`/`ListRepoGrants`, which have zero coverage
  today (strict TDD is on) — the delegation logic lands against currently-untested code.
- Roadmap/docs update covering the three new access concepts.

### Out of Scope

- Projects/groups hierarchy, teams, namespaces.
- Fine-grained action policies beyond reader/writer/admin.
- External IdP/OIDC group mapping.
- **Audit log of grant changes** — valuable, deliberately a separate future change.
- Time-boxed or break-glass access.
- **`feature/acl-policy-engine` is not reused.** That discarded branch introduced a typed
  Subject/Resource/Action/Context/Decision policy engine with capability bindings. This proposal
  builds additively on the existing `RepoGrant`/`RepoRole`/`Token` primitives; no new
  authorization model, no policy evaluator, no capability bindings.

## Capabilities

### New Capabilities

- `robot-accounts`: non-interactive machine identity lifecycle — creation, repository grant
  binding, bounded-TTL token issuance, revocation, and permanent exclusion from password login.

### Modified Capabilities

- `repository-authorization`: authorization MUST recognize a registry-wide read-only principal
  that grants read on every repository without write, admin, or administrative visibility.
- `operator-access-administration`: grant management MUST admit a repository-scoped delegate,
  bounded so a delegate cannot mint `repo-admin`.
- `operator-admin-http-api`: the blanket global-admin door gate MUST admit a narrower
  authenticated check for grant routes only, leaving user CRUD, token, and feature routes
  unchanged.
- `operator-user-administration`: user records MUST carry the read-only global role, and robot
  identities MUST NOT appear in human user listings by default.
- `operator-admin-tui`: operator MUST manage robot accounts, set the read-only role, and see
  grant screens scoped to their delegated repositories.

## Approach

Exploration's recommended approach for each gap, unchanged:

| Gap | Approach | Why |
|---|---|---|
| Delegation | Narrow the HTTP gate for grant routes only; decide authority in the service layer, which already receives `repository` | Smallest surface; rest of `/admin/v1/*` stays global-admin-only |
| Robots | Robot flag on `auth_users`, reusing `RepoGrant`/`Token`/`intersectRequestedActions` unchanged | Harbor's robot-account precedent: a distinct identity class, not a parallel resource hierarchy; avoids doubling store/CRUD surface |
| Read-only | Second global boolean checked in `hasGrantedRepositoryAccess` for read | Mirrors the existing `IsAdmin` short-circuit; naturally excluded from `requireAdmin`, so it cannot drift into delegation territory |

**Benchmark**: Harbor (robot accounts as a distinct identity class with expiry), GitLab deploy
tokens, and Quay robot accounts all converge on a revocable, expiring, repository-scoped machine
credential rather than a service-account hierarchy — matching the decisions bound above.

**Design note for `sdd-design`**: prefer storing an unusable placeholder `password_hash` for robot
rows over relaxing `password_hash NOT NULL`. It keeps the migration purely additive and keeps
rollback safe. Rejecting robot rows in `LoginWithPassword`/`getActiveUserByUsername` must be a
permanent guard, not a creation-time-only check.

Slicing into reviewable work units happens in `sdd-tasks` (suggested order: read-only role → the
delegation slice → robots). The three share only the `requireAdmin`/`Principal` seam, so ordering
is a review-load choice, not a technical dependency. One proposal, one spec set.

## Affected Areas

| Area | Impact | Description |
|---|---|---|
| `internal/domain/auth/principal.go` | Modified | Read-only global role path in `hasGrantedRepositoryAccess` |
| `internal/domain/auth/token.go` | Modified | Robot-sourced token kind or robot-sourced `Principal` |
| `internal/domain/auth/grant.go` | Unchanged | Role enum already sufficient |
| `internal/app/auth/service.go` | Modified | `requireAdminOrRepoAdmin`; grant CRUD callers; robot lifecycle; TTL ceiling |
| `internal/protocol/http/admin_handlers.go` | Modified | Narrower gate for grant sub-routes; robot routes |
| `internal/infra/auth/postgres/migrations.go`, `store.go` | Modified | Additive robot columns; robot-aware queries |
| `internal/ports/auth.go` | Modified | Robot CRUD; grant-management interface |
| `internal/tui/model.go`, `admin_views.go`, `session.go` | Modified | Robot screens; delegate-scoped grants; read-only field |
| `docs/`, roadmap | Modified | Three new access concepts documented |

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Narrowed HTTP gate leaks into user CRUD, token, or feature routes sharing `handleAdmin` dispatch | High | Route-level opt-in, never a default; negative tests asserting each non-grant route still requires global admin |
| Delegated repo-admin escalates to `repo-admin` or global admin | High | Bound explicitly in spec; delegate role-value allowlist enforced at the service layer; explicit rejection test |
| Robot rows authenticate as humans or appear in user listings | Med | Permanent guard in `LoginWithPassword`/`getActiveUserByUsername`; robot exclusion asserted in list queries |
| Grant CRUD has zero test coverage today; delegation lands on untested code | Med | Strict TDD: characterization tests for current admin-only behavior land before any delegation change |
| Two independent global flags (`IsAdmin` + read-only) grow `Principal` surface | Low | Documented precedent limit: a third global flag must be weighed against a role enum instead |
| `feature/acl-policy-engine` concepts reintroduced during design/tasks | Low | Named as an explicit non-goal here; `sdd-design` must not introduce a policy evaluator |
| Long-lived robot tokens outlive their purpose | Med | Mandatory max-TTL ceiling, revocation at any time, no never-expire option |

## Rollback Plan

`git revert` the change commits. Schema changes are additive (new nullable robot/read-only
columns; no dropped or narrowed columns, no data rewrite), so a reverted binary simply ignores
them: robot rows become inert `auth_users` rows that cannot log in, and the read-only column is
unread, reverting those principals to grant-only access. Repository grants, tokens, and manifests
are untouched. Operationally, revoking robot tokens and clearing the read-only flag restores
pre-change authorization without a deploy. The `password_hash NOT NULL` constraint is deliberately
preserved (see Approach) precisely so rollback needs no down-migration.

## Dependencies

- `registry-auth-v1` (shipped) — `RepoGrant`/`RepoRole`/`Principal` primitives are extended, not replaced.
- `registry-operator-admin-api` (shipped) — its admin namespace gate and grant routes are modified.
- PostgreSQL-backed auth mode; no new external dependency.

## Success Criteria

- [ ] A `repo-admin` delegate can grant and revoke `repo-reader`/`repo-writer` on their own repository.
- [ ] The same delegate is rejected when granting `repo-admin`, when self-assigning it, and when acting on any repository they do not administer — each asserted separately.
- [ ] Every non-grant `/admin/v1/*` route still requires global `IsAdmin`, proven by test after the gate narrows.
- [ ] A robot account can pull/push per its grants, and can never authenticate with a password.
- [ ] Robot token creation is rejected above the TTL ceiling, and revocation takes effect immediately.
- [ ] Robots do not appear in the human user listing by default.
- [ ] A read-only principal can list the catalog and read tags/manifests on every repository, is rejected on every push, and is rejected on the admin user and grant listings.
- [ ] Existing global-admin and per-repository grant behavior is byte-identical for users with neither new flag nor delegation.
- [ ] No policy engine, capability binding, or new authorization model is introduced.

## Proposal question round — resolved

Decided with the user before this proposal; encoded above so `sdd-spec`/`sdd-design` do not
reopen them: (1) a repo-admin delegate may grant `repo-reader`/`repo-writer` only — never
`repo-admin`, which stays global-admin-only; (2) robot tokens are revocable at any time and
capped by a maximum TTL ceiling mirroring `DefaultAdminTokenTTL`, with no never-expire option;
(3) the registry-wide read-only role sees registry content only and never the admin user/grant
listing — operational and administrative visibility stay separate.

### Open questions for `sdd-design`

1. Robot identity storage shape: flag column vs. an identity-kind enum on `auth_users`, and the
   exact placeholder-hash strategy that keeps `password_hash NOT NULL` intact.
2. Whether a robot binds to exactly one `(repository, role)` at creation or owns ordinary
   multi-repository grants.
3. The exact route/dispatch split for grant sub-routes so the narrowed gate cannot be inherited
   by a future admin route added to the same dispatcher by default.
4. The naming and JSON field for the read-only global role across `AdminCreateUserInput`,
   `UpdateUserInput`, and the TUI form.
5. Whether delegates reach grant management through the existing admin TUI flow or a separate
   non-admin entry point, given `isAdminScreen` assumes an `IsAdmin` session today.
