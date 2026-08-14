# Tasks: Repository Access Control Completion (registry-acl-v1)

## Mandatory Ordering Constraint (design.md Testing Strategy)

`PutRepoGrant`/`DeleteRepoGrant`/`ListRepoGrants` (`service.go:506,540` /
`ListRepoGrants` caller at `:19` in ports) have **zero** test coverage today
(confirmed via codegraph blast-radius scan, not assumed). Task 2.1/2.2 — a
characterization test pinning today's admin-only behavior of all three
methods — MUST land and pass **before** task 2.4 (`requireAdminOrRepoAdmin`)
or any later delegation task. Every other behavior-changing task in this
file follows RED (failing test) → GREEN (implementation), Strict TDD.

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~2800–3400 (prod ~1550–1750, tests ~1250–1650) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | 5 units (see below) |
| Delivery strategy | single-pr |
| Chain strategy | pending |

**Rationale**: three additive, code-independent slices (read-only role,
delegated repo-admin grants, robot accounts) share only the
`requireAdmin`/`Principal` seam. Each touches domain, service, ports, store,
HTTP, and TUI layers plus TDD RED/GREEN pairs and threat-matrix tests
(HTTP gate narrowing, `/`-in-path parsing, privilege escalation, machine
identity, identity disclosure). This session's cached review budget is
**1000** changed lines (not the skill default 400); even the largest single
slice (delegated grants backend, Unit 2, ~750–950 lines) is close to that
ceiling, and the combined change is 2.8–3.4x over it. `single-pr` therefore
requires an explicit `size:exception` before `sdd-apply`, or the orchestrator
must ask the user for a chain strategy.

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Read-only role (Phase 1) | PR 1 | `go test ./internal/domain/auth/... ./internal/app/auth/... ./internal/infra/auth/... ./internal/tui/... -run ReadOnly -v` | Manual: create read-only user, `docker pull`/`push` a granted repo, `curl /admin/v1/users` expect 403 | Revert `principal.go`/`service.go` read-only branches and TUI toggle; `is_read_only` column stays inert (defaults `FALSE`), no down-migration |
| 2 | Delegated repo-admin grants — backend (Phase 2) | PR 2 | `go test ./internal/app/auth/... ./internal/protocol/http/... -run 'RepositoryGrant\|RequireAdminOrRepoAdmin\|Delegate' -v` | Manual: `curl -H "Authorization: Bearer <delegate-token>" -X PUT /admin/v1/repositories/team/app/grants/bob` | Revert `requireAdminOrRepoAdmin`, `List/Put/DeleteRepositoryGrant`, `handleAdmin` restructure; no schema change |
| 3 | Delegated repo-admin grants — TUI (Phase 3) | PR 3 | `go test ./internal/tui/... -run 'RepoAdminGrants\|AdminIntent' -v` | Manual: launch TUI as a delegate, Console Repositories → grants screen, add/remove a grant | Revert `screenRepoAdminGrants`/`screenRepoAdminAddGrant`, `adminIntent`, `admin_client.go` methods |
| 4 | Robot accounts — backend (Phase 4) | PR 4 | `go test ./internal/domain/auth/... ./internal/app/auth/... ./internal/infra/auth/... ./internal/protocol/http/... -run Robot -v` | Manual: `curl -X POST /admin/v1/robots`, `docker login -u robot$ci -p <secret>`, then `-p <password>` expect failure | Revert `CreateRobot`/`ListRobots`/`LoginWithPassword` guard; `is_robot` column stays inert |
| 5 | Robot accounts — TUI + docs (Phase 5) | PR 5 | `go test ./internal/tui/... -run AdminRobot -v` | Manual: TUI create robot, issue token, confirm one-time secret display | Revert `screenAdminRobots`/`screenAdminCreateRobot`; docs revert |

## Phase 1: Registry-Wide Read-Only Role (Slice 1, smallest — PR 1)

- [x] 1.1 Migration: add `is_read_only BOOLEAN NOT NULL DEFAULT FALSE` to
      `auth_users`; add `"is_read_only"` to `tolerateDuplicateColumns`
      (`migrations.go`, Decision 8).
- [x] 1.2 Add `IsReadOnly bool` to `User` (`user.go`) and `Principal`
      (`principal.go`).
- [x] 1.3 RED `principal_test.go`: `hasGrantedRepositoryAccess` — read-only
      allows read on any repository, denies write/admin, unflagged principal
      unchanged — table-driven (`principal.go:53-65`).
- [x] 1.4 GREEN: implement the read-only probe branch in
      `hasGrantedRepositoryAccess` (Decision 5).
- [x] 1.5 RED `service_test.go`: `intersectRequestedActions` — read-only
      yields `pull` only; admin/grant paths byte-identical — table-driven
      (`service.go:700-731`).
- [x] 1.6 GREEN: add `isReadOnly bool` param and pull-only branch to
      `intersectRequestedActions` and its call sites.
- [x] 1.7 RED `store_test.go`: `auth_users` queries round-trip
      `is_read_only` across `UpsertUser`/`ListUsers`/`GetUserByID`/
      `GetUserByUsername`.
- [x] 1.8 GREEN: add `is_read_only` to every `auth_users` SELECT/INSERT and
      `scanUserRow` (`store.go`).
- [x] 1.9 RED `service_test.go`/handler test: `AdminCreateUserInput`/
      `UpdateUserInput`/`AdminUser` carry `is_read_only`; create-then-list
      reflects it (spec: operator-user-administration scenarios).
- [x] 1.10 GREEN: add `is_read_only` to `ports.AdminCreateUserInput`/
      `UpdateUserInput`/`AdminUser`; wire through `CreateUser`/`UpdateUser`.
- [x] 1.11 Integration RED (router test): read-only principal reads
      catalog/tags/manifests on any repository, rejected on push, rejected
      on admin user/grant listings.
- [x] 1.12 GREEN: confirm `issueAccessToken`/login populates
      `Principal.IsReadOnly` from `User.IsReadOnly`.
- [x] 1.13 RED `session_test.go`: create/edit user form exposes a `Read-only`
      toggle, independent of the `Admin` toggle.
- [x] 1.14 GREEN: add the `Read-only` field to TUI create/edit user forms
      (`session.go`, `admin_views.go`, `admin_client.go` request body).
- [x] 1.15 Confirm Phase 1 GREEN (see Unit 1 focused test command).

## Phase 2: Delegated Repo-Admin Grants — Backend (Slice 2a — PR 2)

- [x] 2.1 Characterization RED `internal/app/auth/service_grants_test.go`
      (new file): `PutRepoGrant`/`DeleteRepoGrant`/`ListRepoGrants` each
      reject a non-admin actor and succeed for an admin — today's behavior,
      table-driven, zero production change.
- [x] 2.2 Confirm 2.1 is GREEN against current unmodified code; record the
      baseline. **Blocks every task below (Mandatory Ordering Constraint).**
- [x] 2.3 RED `service_test.go`: `requireAdminOrRepoAdmin` — admin passes;
      repo-admin on the exact repository passes; repo-admin on another
      repository, repo-writer, read-only, and empty-`Scopes` principals
      rejected — table-driven.
- [x] 2.4 GREEN: implement `requireAdminOrRepoAdmin` beside `requireAdmin`
      (`service.go:733-739`, Decision 4 — reads `actor.Grants`, not
      `Principal.HasRepoAdminAccess`).
- [x] 2.5 RED: delegate rejected for requesting `repo-admin`, for
      self-assigning `repo-admin`, and for touching an existing repo-admin
      grant — three separate cases (threat matrix: privilege escalation).
- [x] 2.6 RED: delegate's `ListRepositoryGrants` is scoped to their own
      repository only.
- [x] 2.7 GREEN: implement `List/Put/DeleteRepositoryGrant` in `service.go`
      as siblings to `PutRepoGrant` et al. — `requireAdminOrRepoAdmin` gate,
      escalation bounds, username resolution via `GetUserByUsername`.
- [x] 2.8 RED `store_test.go`: `ListRepoGrantsByRepository` returns grants
      for one repository across users. (Interleaved with 2.7/2.9/2.10 in one
      commit — `service.go`'s new methods require the store method to exist
      to compile, so it could not be deferred to a later commit; see apply
      report Deviations.)
- [x] 2.9 GREEN: implement `ListRepoGrantsByRepository` (`store.go`).
- [x] 2.10 GREEN: add repository-grant DTOs and the new methods to
      `AuthService`/`AdminHTTPService`/`AuthStore` (`ports/auth.go`).
- [x] 2.11 RED `admin_handlers_test.go`: GET/PUT/DELETE
      `/admin/v1/repositories/{repo}/grants` — 401 unauthenticated, 403
      non-delegate, 200/204 delegate on own repository, 403 delegate on
      another repository.
- [x] 2.12 RED: a repository literally named `team/grants` routes correctly
      for GET/PUT/DELETE (threat matrix: `/`-in-path parsing hazard,
      `admin_handlers.go:78-84` precedent).
- [x] 2.13 RED: every **non-grant** `/admin/v1/*` route still 403s a
      non-admin authenticated principal, enumerated route by route (threat
      matrix: HTTP gate narrowing — highest-risk guard). (This test passes
      immediately against unmodified code — those routes are untouched by
      design — and continues to pass after 2.15; it is the exhaustive
      regression guard, not a state that must flip.)
- [x] 2.14 RED: delegate grant responses carry no user ID, hash, flags, or
      other repositories (threat matrix: identity disclosure).
- [x] 2.15 GREEN: restructure `handleAdmin` with the early-return
      `repositories/` prefix above `requireAdminPrincipal`; add
      `requireAuthenticatedPrincipal`; add `handleAdminRepositoryResource`
      (`admin_handlers.go`, Decision 3).
- [x] 2.16 Confirm 2.3, 2.5–2.6, 2.8, 2.11–2.14 GREEN (see Unit 2 focused
      test command). Also ran full `go test ./...` (zero regressions) and
      `gofmt -l .` (clean).

## Phase 3: Delegated Repo-Admin Grants — TUI (Slice 2b — PR 3, depends on Phase 2)

- [x] 3.1 RED `model_test.go`: `adminIntent` routes post-login to
      `screenRepoAdminGrants` when set, `screenAdminUsers` otherwise.
- [x] 3.2 GREEN: add `adminIntent` (`adminIntentOperator|adminIntentRepoGrants`)
      to `Model`, consumed once on successful auth (`model.go:3029-3063`).
- [x] 3.3 RED: `screenRepoAdminGrants`/`screenRepoAdminAddGrant` join
      `isAdminScreen`/`isAdminPrincipalScreen`; `screenRepoAdminGrants` joins
      `canLogoutAdminFromCurrentScreen`.
- [x] 3.4 GREEN: add both screens to the screen sets
      (`model.go:3187-3218`).
- [x] 3.5 RED `admin_views_test.go`: `screenRepoAdminGrants` renders the
      operator's own repository's grants; `screenRepoAdminAddGrant`'s role
      field never offers `repo-admin`.
- [x] 3.6 GREEN: implement `screenRepoAdminGrants`/`screenRepoAdminAddGrant`
      render functions (`admin_views.go`).
- [x] 3.7 RED `admin_client_test.go`: repository-grant client methods call
      `/admin/v1/repositories/{repo}/grants`.
- [x] 3.8 GREEN: add repository-grant methods to `admin_client.go`.
- [x] 3.9 RED `model_test.go`: the Console Repositories screen's grant action
      sets `adminIntent` and reaches `screenAdminLogin`; put/delete grant
      commands wired.
- [x] 3.10 GREEN: wire the key handler and load/mutate commands (`model.go`).
- [x] 3.11 Confirm Phase 3 GREEN (see Unit 3 focused test command).

### Remediation: task 3.5 edit-path gap (post sdd-verify, CRITICAL)

`nextDelegateGrantRole` (the cycle used by "n" Add Grant) was exhaustive and
correct, but `updateRepoAdminGrantsKey`'s "e" (edit) handler copied an
existing grant's `Role` unfiltered into the form. `ListRepositoryGrants` is
scoped to a repository but not filtered by role, so a delegate's own grants
list can legitimately contain a peer's `repo-admin` grant — pressing "e" on
that row populated `screenRepoAdminAddGrant` with `Role: repo-admin`,
contradicting task 3.5's guarantee. Not a security bypass (backend
`requireAdminOrRepoAdmin` independently rejected any resulting submission);
a UI-trust gap where the interface offered what the backend would refuse.

- [x] 3.5r RED `model_test.go`:
      `TestUpdateRepoAdminGrantsKeyEditRefusesRepoAdminGrant` exercises the
      real "e" key handler via `model.Update()` against a grants list
      containing a `repo-admin` row; confirmed failing pre-fix (screen
      transitioned to `screenRepoAdminAddGrant` with `Role: repo-admin`).
- [x] 3.5r GREEN: `updateRepoAdminGrantsKey`'s "e" handler now refuses to
      open the edit form for a `repo-admin` grant row (status message,
      mirrors the "Already inheriting global settings." refusal precedent
      for actions that don't apply to the current row/state), instead of
      clamping the role. Triangulation:
      `TestUpdateRepoAdminGrantsKeyEditAllowsNonRepoAdminGrant` proves the
      guard is specific to `repo-admin` and does not block editing
      Reader/Writer grants.

## Phase 4: Robot Accounts — Backend (Slice 3a — PR 4)

- [x] 4.1 Migration: add `is_robot BOOLEAN NOT NULL DEFAULT FALSE` to
      `auth_users`; add `"is_robot"` to `tolerateDuplicateColumns`.
- [x] 4.2 Add `IsRobot bool` to `User`; add sentinel `RobotPasswordHash`
      constant (`user.go`, Decision 1).
- [x] 4.3 RED `user_test.go`: `User.Validate` rejects a robot that is
      `IsAdmin` or `IsReadOnly`; `RobotPasswordHash` never satisfies
      `bcrypt.CompareHashAndPassword` — table-driven.
- [x] 4.4 GREEN: implement the robot validation rule in `User.Validate`.
- [x] 4.5 RED `service_test.go`: `LoginWithPassword` rejects a robot;
      `LoginWithPreissuedToken` accepts one.
- [x] 4.6 GREEN: add the `IsRobot` guard immediately after
      `getActiveUserByUsername` in `LoginWithPassword`
      (`service.go:285-295`, Decision 6 — NOT inside the shared helper).
- [x] 4.7 RED `service_test.go`: `CreateRobot` persists exactly one
      `auth_repo_grants` row for the requested repository+role (Decision 2);
      robot token TTL above the ceiling is rejected; revocation takes effect
      immediately. (Interleaved with 4.9/4.10 in one RED-then-GREEN commit
      pair — `Service.ListRobots` requires `AuthStore.ListRobots` to exist on
      the interface to compile, so it could not be deferred to a later
      commit; see apply report Deviations, same pattern as Phase 2's
      task 2.8.)
- [x] 4.8 GREEN: implement `CreateRobot`/`ListRobots` (`service.go`),
      reusing `CreateAdminToken`/`RevokeAdminToken` unchanged.
- [x] 4.9 RED `store_test.go`: `ListUsers` excludes robot rows
      (`WHERE is_robot = FALSE`); `ListRobots` returns only robot rows.
- [x] 4.10 GREEN: add `is_robot` to `auth_users` queries; implement the
      `ListUsers` filter and `ListRobots` (`store.go`).
- [x] 4.11 RED `service_test.go`: `AdminRobot*` DTOs round-trip; robot
      pull/push follows its grant, denied on an ungranted repository.
- [x] 4.12 GREEN: add `AdminRobot*` DTOs and the new methods to
      `AuthService`/`AdminHTTPService`/`AuthStore` (`ports/auth.go`).
- [x] 4.13 RED `admin_handlers_test.go`: `POST /admin/v1/robots` creates a
      robot + its grant + a token (global admin only); `GET /admin/v1/robots`
      lists robots; non-admin rejected.
- [x] 4.14 GREEN: add the two robot routes to `handleAdmin`'s dispatch
      (`admin_handlers.go`).
- [x] 4.15 RED: password login as a robot fails with both layers (`IsRobot`
      guard, sentinel hash) tested independently — with the guard notionally
      removed, the sentinel hash alone still blocks bcrypt (threat matrix:
      machine identity authenticating as human). (Both layers already pass
      against 4.4/4.6's landed code — an exhaustive pinning proof, not a
      state that must flip, same pattern as Phase 2's task 2.13.)
- [x] 4.16 Confirm 4.3, 4.5, 4.7, 4.9, 4.11, 4.13, 4.15 GREEN (see Unit 4
      focused test command). Also ran full `go test ./...` (zero
      regressions) and `gofmt -l .` (clean).

## Phase 5: Robot Accounts — TUI + Docs (Slice 3b — PR 5, depends on Phase 4)

- [x] 5.1 RED `session_test.go`: robot create form fields (name, repository,
      role, ttl); robot list view state.
- [x] 5.2 GREEN: add robot view state and forms to `session.go`.
- [x] 5.3 RED `admin_views_test.go`: `screenAdminRobots` list/enable/
      disable/delete mirrors `screenAdminUsers`; `screenAdminCreateRobot`
      shows the one-time secret.
- [x] 5.4 GREEN: implement `screenAdminRobots`/`screenAdminCreateRobot`
      (`admin_views.go`).
- [x] 5.5 RED `admin_client_test.go`: robot client methods (create, list,
      enable/disable/delete, issue token reusing existing token routes).
      Deviation: no dedicated "delete" client method — see apply report;
      no `DeleteAdminUser`/`DELETE /admin/v1/users/{id}` route exists on
      this branch's backend to call.
- [x] 5.6 GREEN: add robot methods to `admin_client.go`.
- [x] 5.7 GREEN: wire both screens into `model.go` screen sets and key
      handlers.
- [x] 5.8 Confirm Phase 5 GREEN (see Unit 5 focused test command).
- [x] 5.9 Update `docs/` and roadmap for delegation, robots, and the
      read-only role.
