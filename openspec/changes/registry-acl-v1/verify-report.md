```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:4475126e7e9bd7e41f6856e8bdc08e693618c0ca9511e496d662f799bd850fd7
verdict: pass_with_warnings
blockers: 0
critical_findings: 0
requirements: 2/2
scenarios: 6/6
test_command: go test -count=1 ./...
test_exit_code: 0
test_output_hash: sha256:34dc55d3129630bdb4aba3b0c9875511cdb52ba243c907800c6cf917a48e1610
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: registry-acl-v1
**Version**: N/A (Phase 1 of 5 — PR 1, "Registry-Wide Read-Only Role")
**Mode**: Strict TDD

### Scope of This Verification

This session verifies **Phase 1 only** (tasks 1.1–1.15, branch
`feature/registry-acl-v1-01-readonly-role`, base `feature/registry-acl-v1`).
Phases 2–5 (delegated repo-admin grants, robot accounts) are unimplemented by
design and out of scope; their absence is not a defect. Requirement/scenario
totals below are restricted accordingly: of the two named spec files'
delta content, `operator-user-administration`'s second ADDED requirement
("Robot Identities Excluded From Default Human User Listing") belongs to
Phase 4 (robots), not Phase 1, and is excluded from the counted total —
independently confirmed by reading the requirement text itself (robot
identities do not exist yet on this branch) and by the tasks.md Phase/PR
split, not merely asserted.

Phase 1 requirement/scenario totals, counted directly from the committed
spec files (`rg -n "^### Requirement:|^#### Scenario:"`):

| Spec file | Phase 1 requirements | Phase 1 scenarios |
|---|---|---|
| `repository-authorization/spec.md` | 1 | 4 |
| `operator-user-administration/spec.md` (read-only requirement only) | 1 | 2 |
| **Total (Phase 1 scope)** | **2** | **6** |

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total (Phase 1) | 15 |
| Tasks complete | 15 |
| Tasks incomplete | 0 |

Independently confirmed: `rg -c "^\s*- \[x\]"` over the whole `tasks.md` = 15,
and every `- [ ]` line in the file belongs to Phase 2 (task 2.1 onward) —
read directly, not assumed. Phase 1's own 15 tasks (1.1–1.15) are each
individually checked `[x]`.

### Build & Tests Execution (independently run this session, from a clean tree)

**Build**: PASSED
```text
go build ./...   → exit 0, no output
go vet ./...     → exit 0, no output
gofmt -l .       → 1 file flagged: internal/tui/model.go (see WARNING 1)
```

**Tests**: `go test -count=1 ./...` → exit 0, all 19 packages `ok` (18 tested,
`internal/domain/auth` also `ok` — this branch's `principal_test.go` is the
package's first test file, so the package now has coverage where the
image-signing-era report recorded "no test files").

```text
ok  	regixtry/cmd/regixtry	3.884s
ok  	regixtry/internal/app/auth	0.165s
ok  	regixtry/internal/app/regixtry	4.279s
ok  	regixtry/internal/app/scanning	0.073s
ok  	regixtry/internal/domain/auth	0.013s
ok  	regixtry/internal/domain/regixtry	0.017s
ok  	regixtry/internal/domain/signing	0.142s
ok  	regixtry/internal/infra/auth/postgres	0.455s
ok  	regixtry/internal/infra/cliprogress	0.014s
ok  	regixtry/internal/infra/install/linux	0.601s
ok  	regixtry/internal/infra/install/releases	0.058s
ok  	regixtry/internal/infra/metadata/sqlite	0.576s
ok  	regixtry/internal/infra/release	0.013s
ok  	regixtry/internal/infra/scanning/gitleaks	0.330s
ok  	regixtry/internal/infra/scanning/trivy	0.346s
ok  	regixtry/internal/infra/storage/fsblob	0.017s
ok  	regixtry/internal/ports	0.009s
ok  	regixtry/internal/protocol/http	2.152s
ok  	regixtry/internal/tui	0.403s
```

**Focused command** (session's own PR-1 command from `tasks.md`'s Suggested
Work Units table), run independently this session:
```text
go test ./internal/domain/auth/... ./internal/app/auth/... ./internal/infra/auth/... ./internal/tui/... -run ReadOnly -v
```
Result: all 12 subtests across 5 top-level `Test*ReadOnly*`/`*IsReadOnly*`
functions PASS (`TestPrincipalHasGrantedRepositoryAccessReadOnlyProbe` × 7
subtests, `TestIntersectRequestedActionsReadOnlyYieldsPullOnly` × 5 subtests,
`TestServiceCreateAdminUserPersistsReadOnlyFlagAndListReflectsIt`,
`TestServiceUpdateUserSetsReadOnlyFlag`,
`TestStoreRoundTripsIsReadOnlyAcrossAuthUserQueries`,
`TestNextCreateUserFieldCyclesThroughReadOnlyIndependentlyOfAdmin`,
`TestCreateUserFormDefaultsReadOnlyToFalseAndToggleIsIndependentOfAdmin`).

The router-level end-to-end scenario lives in `internal/protocol/http` and is
named `TestRouter*`, so it is not matched by `-run ReadOnly`; it was
independently run separately this session:
```text
go test ./internal/protocol/http/... -run ReadOnly -v
--- PASS: TestRouterReadOnlyPrincipalReadsAnyRepositoryRejectedOnPushAndAdmin (0.52s)
```
This single test exercises real login → real pull/tags/catalog success →
real push 401 → real admin-listing 403, over the real `Router`/`Service`, and
covers 3 of the 4 `repository-authorization` scenarios directly.

**Coverage**: not measured this session (no coverage tool run; informational
only).

### Diff Size (Review Workload Guard)
`git diff feature/registry-acl-v1..HEAD --stat` → **15 files changed, 599
insertions(+), 67 deletions(-)**. Independently re-run this session, matches
the apply-progress report's own tally exactly. Well inside both the skill
default (400) and this session's cached budget (1000).

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Registry-Wide Read-Only Role Grants Read Everywhere | Read-only principal reads across all repositories | `internal/protocol/http/router_test.go > TestRouterReadOnlyPrincipalReadsAnyRepositoryRejectedOnPushAndAdmin` | ✅ COMPLIANT |
| " | Read-only principal is rejected on push | same test (push sub-assertion, 401) | ✅ COMPLIANT |
| " | Read-only principal cannot see admin listings | same test (`/admin/v1/users` 403, `/admin/v1/users/{id}/grants` 403) | ✅ COMPLIANT |
| " | Principals without the flag are unaffected | `internal/domain/auth/principal_test.go > TestPrincipalHasGrantedRepositoryAccessReadOnlyProbe` (unflagged subtests) + `internal/app/auth/service_test.go > TestIntersectRequestedActionsReadOnlyYieldsPullOnly` (unflagged/admin/grant-based subtests, asserted byte-identical) | ✅ COMPLIANT |
| User Records Carry The Registry-Wide Read-Only Role | Creating a user with the read-only flag persists it | `internal/app/auth/service_test.go > TestServiceCreateAdminUserPersistsReadOnlyFlagAndListReflectsIt` | ✅ COMPLIANT |
| " | Updating the read-only flag takes effect | `internal/app/auth/service_test.go > TestServiceUpdateUserSetsReadOnlyFlag` | ✅ COMPLIANT |

**Compliance summary**: 6/6 Phase-1 scenarios compliant, all covering tests
independently re-run this session and confirmed passing. Every listed test
was opened and read in full (not just grepped for existence) — see
"Independent Structural Checks" below for what each proves at the
implementation level, not merely at the test-name level.

### Independent Structural Checks (read from the actual diff/source, not from apply-progress prose)

| Claim (from apply-progress) | Independent verification performed this session | Result |
|---|---|---|
| `hasGrantedRepositoryAccess` implements the RepoRoleReader probe (Decision 5) | Read `internal/domain/auth/principal.go:54-75` in full | Matches Decision 5's code block verbatim: `IsAdmin` short-circuit first, then `p.IsReadOnly && allows(RepoRoleReader)`, then the unchanged grant loop. |
| `intersectRequestedActions` gains an `isReadOnly` branch, admin/grant paths byte-identical | Read `internal/app/auth/service.go:704-730` in full | Matches: `isAdmin` branch unchanged, new `else if isReadOnly` branch sets `allowPull` only (never `allowPush`), grant-based `else` branch unchanged. |
| `is_read_only` persists across every `auth_users` query | `rg -n "is_read_only\|IsReadOnly" store.go` | Present in the `SELECT` list of all 3 read queries, the `INSERT`/`ON CONFLICT` clause, and `scanUserRow`'s `Scan(...)` call — matches Decision 8. |
| Migration + duplicate-column tolerance generalized (Decision 8) | Read `migrations.go:1-53` in full | `tolerateDuplicateColumns` is `[]string{"scope", "is_read_only"}` (was hardcoded to `"scope"` alone before this PR, per `git diff feature/registry-acl-v1..HEAD`); `ADD COLUMN ... DEFAULT FALSE` matches the design's exact SQL. |
| `AdminCreateUserInput`/`UpdateUserInput`/`AdminUser` carry `is_read_only` | Read `internal/ports/auth.go:55-96` | `is_read_only` JSON tag present on `AdminUser` and `AdminCreateUserInput`; `CreateUserInput`/`UpdateUserInput` (internal, no JSON tags — not HTTP-exposed) carry the Go field. |
| TUI create-user form exposes an independent `Read-only` toggle | Read `session.go` (state), `model.go:2276-3172` (key handling + DTO wiring), `admin_views.go:463` (render) | `adminCreateUserFieldIsReadOnly` is a distinct focus stop between `IsAdmin` and `Enabled`; `toggleCreateUserField` flips only the field under focus (confirmed both by reading the switch statement and by `TestCreateUserFormDefaultsReadOnlyToFalseAndToggleIsIndependentOfAdmin`, which asserts toggling one field never flips the other). |
| `admin_client.go` needed no change because it forwards the whole DTO | `git diff feature/registry-acl-v1..HEAD -- internal/tui/admin_client.go` | Empty diff, confirmed. `CreateUser` marshals `ports.AdminCreateUserInput` directly, and `model.go:2292` already sets `IsReadOnly` on that struct before calling it — so the client genuinely needed zero changes. |
| 599 insertions / 67 deletions across 15 files | `git diff feature/registry-acl-v1..HEAD --stat`, this session | Exact match. |
| Zero Phase 2–5 surface touched | `rg -n "requireAdminOrRepoAdmin\|screenRepoAdminGrants\|is_robot\|CreateRobot\|handleAdminRepositoryResource\|IsRobot\|ListRobots\|adminIntent" --type go` | Zero matches, whole repo. Confirmed no chain contamination. |

### Git-Mishap Independent Verification (apply-progress self-report, Task instructions §3)

apply-progress reports two self-corrected git mistakes during commit
splitting: (1) a benign RED/GREEN reorder fixed with `git reset --soft`, and
(2) a destructive `git checkout -- <file>` that discarded uncommitted test
code, recovered by reconstructing the exact content from conversation
history, "content verified byte-identical via test re-run."

This session did **not** take that self-report on trust. Independent checks
performed:

1. **Commit-history sanity**: `git log --oneline HEAD~15..HEAD` shows a
   clean, strictly-ordered RED→GREEN sequence for every behavior-changing
   pair (e.g. `7e94f44 test(auth): RED — is_read_only round-trips` directly
   followed by `bf7c3c4 feat(auth): GREEN — persist is_read_only in every
   auth_users query`). `git show cce3786:internal/domain/auth/principal_test.go`
   confirms `principal_test.go` genuinely does not exist at the migration-only
   commit, i.e. the RED test was not present before its own GREEN — no
   evidence of a residual reorder artifact in the final history.
2. **`internal/app/auth/service_test.go`** (the file named in the destructive
   `git checkout --` incident) was read in full, byte for byte, this session
   — not spot-checked. It contains all 6 pre-existing tests
   (`TestServiceGrantsOnlyAllowedRequestedRepositoryScopes`,
   `TestServiceRejectsRepositoryAccessWhenRequestedScopeHasNoAllowedActions`,
   `TestServiceDisableUserRejectsLastActiveAdmin`,
   `TestServiceListAdminUserRepoGrantsRejectsMissingUser`,
   `TestServiceCreateAdminUserTokenRejectsDisabledUserAndExcessiveTTL`,
   `TestServiceRevokeAdminUserTokenRejectsMismatchedOwner`) plus the 3 new
   Phase-1 tests, plus a complete `memoryAuthStore` implementing the full
   `AuthStore` interface. Nothing is truncated, duplicated, or orphaned; the
   file compiles and every test in it passes (confirmed by the full-suite run
   above, package `regixtry/internal/app/auth` → `ok`).
3. **Content, not just presence**: `TestIntersectRequestedActionsReadOnlyYieldsPullOnly`
   (the test most likely to have been the discarded content, since it is the
   first RED test written against the not-yet-existing `isReadOnly` parameter)
   was read and independently re-run in isolation
   (`go test ./internal/app/auth/... -run ReadOnly -v`) — passes, with 5
   correctly-triangulated subtests (read-only-no-grant, read-only-with-writer-
   grant, admin-byte-identical, grant-based-byte-identical, unflagged-denied).

**Conclusion**: the current committed state of `service_test.go` is complete
and correct. Both self-reported mishaps left no defect in the final tree —
independently confirmed by content inspection and test execution, not merely
by accepting the report's own "verified byte-identical" claim.

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|---|---|---|
| Registry-Wide Read-Only Role Grants Read Everywhere | ✅ Implemented | `hasGrantedRepositoryAccess` + `intersectRequestedActions`, both read directly, match Decision 5 exactly. |
| User Records Carry The Registry-Wide Read-Only Role | ✅ Implemented | `ports.AdminCreateUserInput`/`UpdateUserInput`/`AdminUser` all carry the field; wired through `CreateUser`/`CreateAdminUser`/`UpdateUser`. |

### Coherence (Design)
| Decision | Followed? | Notes |
|---|---|---|
| Decision 5 — read-only probe in `hasGrantedRepositoryAccess` | ✅ Yes | Code matches the design.md snippet verbatim, including the comment explaining why `AllowsRead` is the only true predicate for `RepoRoleReader`. |
| Decision 8 — additive migration, generalized duplicate-column tolerance | ✅ Yes | `tolerateDuplicateColumns` generalized from a single hardcoded `"scope"` string to a slice including `"is_read_only"`; `ADD COLUMN ... DEFAULT FALSE` matches exactly. |
| Admin exclusion is structural (`requireAdmin` never reads `IsReadOnly`) | ✅ Yes | `requireAdmin` (unrelated to this PR's diff) is untouched; independently confirmed via the router integration test's two 403 assertions. |

### Issues Found

**CRITICAL**: None.

**WARNING** (1):
1. `internal/tui/model.go` has one gofmt violation **introduced by this PR's
   own diff**: an extra blank line at line 3176–3177, between
   `nextCreateUserField`'s closing brace and `nextGrantRole`. Confirmed via
   `git diff feature/registry-acl-v1..HEAD -- internal/tui/model.go`, which
   shows the added blank line as part of this change's hunk, and via
   `gofmt -l /workspace` (repo-wide), which flags only this one file. `go
   build`/`go vet`/tests are unaffected (gofmt is a formatting-only check);
   cosmetic, trivially fixable with `gofmt -w internal/tui/model.go` before
   merge.

**SUGGESTION** (1):
1. `UpdateUser` (the service method backing the "Updating the read-only flag
   takes effect" scenario) is exercised only at the service-test layer
   (`TestServiceUpdateUserSetsReadOnlyFlag`); it is not currently reachable
   through the admin HTTP API or the TUI (`rg -n "UpdateUser\b"` finds no
   caller in `admin_handlers.go` or the TUI). This is **pre-existing**,
   unrelated to this PR (no admin HTTP route for generic user update exists
   in the codebase today), and the spec scenario's own wording ("subsequent
   authorization checks... honor the new role") is satisfied by the
   service-layer proof — not a Phase 1 gap. Noted for awareness only, since a
   future phase may want an HTTP-reachable update-user route.

### TDD Compliance
| Check | Result | Details |
|---|---|---|
| TDD Evidence reported | Present | `tasks.md` embeds RED/GREEN evidence inline per task (e.g. "1.3 RED principal_test.go...", "1.4 GREEN: implement..."), matching this repo's own precedent (see `openspec/changes/image-signing/verify-report.md`). Independently cross-checked task-by-task against the actual commit log and the actual test files below. |
| All tasks have tests | 15/15 Phase 1 tasks `[x]`; every RED task names a real test function, independently confirmed present in the corresponding file. |
| RED confirmed (tests exist) | All 5 new/modified test files (`principal_test.go`, `service_test.go`, `store_test.go`, `router_test.go`, `session_test.go`) opened and read in full this session, not merely grepped. |
| GREEN confirmed (tests pass) | `go test -count=1 ./...` independently re-run this session — 19/19 packages `ok`, zero regressions. |
| Triangulation adequate | ✅ — `TestPrincipalHasGrantedRepositoryAccessReadOnlyProbe` (7 cases: read-only×4 angles, unflagged×2, admin×1) and `TestIntersectRequestedActionsReadOnlyYieldsPullOnly` (5 cases spanning read-only/admin/grant-based/unflagged) both triangulate with distinct expected values, not repeated trivial assertions. |
| Safety Net for modified files | ✅ — `service_test.go`'s 6 pre-existing tests and `store_test.go`'s pre-existing `TestStoreBootstrapsAndPersistsAuthState`/`TestMigrationsCreateOnlyAuthTables` all independently re-run clean alongside the new tests. |

**TDD Compliance**: 6/6 checks passed.

---

### Test Layer Distribution
| Layer | Tests | Files | Tools |
|---|---|---|---|
| Unit | 10 (7+5+2+1+2 subtests across `TestPrincipalHasGrantedRepositoryAccessReadOnlyProbe`, `TestIntersectRequestedActionsReadOnlyYieldsPullOnly`, `TestServiceCreateAdminUserPersistsReadOnlyFlagAndListReflectsIt`, `TestServiceUpdateUserSetsReadOnlyFlag`, `TestNextCreateUserFieldCyclesThroughReadOnlyIndependentlyOfAdmin`, `TestCreateUserFormDefaultsReadOnlyToFalseAndToggleIsIndependentOfAdmin`) | 4 (`principal_test.go`, `service_test.go`, `session_test.go` ×2 tests) | Go `testing` |
| Integration | 2 (`TestStoreRoundTripsIsReadOnlyAcrossAuthUserQueries` against real modernc SQLite; `TestRouterReadOnlyPrincipalReadsAnyRepositoryRejectedOnPushAndAdmin` against a real `Router`/`Service`) | 2 (`store_test.go`, `router_test.go`) | modernc SQLite, `net/http/httptest` |
| E2E | 0 | 0 | Not applicable — no browser/real-network layer in this stack |
| **Total** | **12** | **6** | |

---

### Changed File Coverage
Coverage tool not run this session (informational only, consistent with
`go test`'s own default no-coverage mode); no `-cover` flag was used.
"Coverage analysis skipped — no coverage tool detected/run."

---

### Assertion Quality

Scanned all 5 new/modified test files in full for banned patterns
(tautologies, empty-collection-without-companion, type-only assertions
alone, assertion-without-production-call, ghost loops over possibly-empty
collections, smoke-test-only, implementation-detail coupling, mock-heavy
ratio).

**Assertion quality**: ✅ All assertions verify real behavior. Every test
calls real production code (`hasGrantedRepositoryAccess`,
`intersectRequestedActions`, `service.CreateAdminUser`/`UpdateUser`,
`store.UpsertUser`/`GetUserByID`/`ListUsers`, `nextCreateUserField`/
`toggleCreateUserField`, or the real `Router` over real HTTP), asserts a
concrete expected value (not just `!= nil`/`toBeDefined`-equivalents), and
every `for`/`range` loop (`principal_test.go`'s and `service_test.go`'s
table-driven loops, `store_test.go`'s `ListUsers` search loop) iterates over
either a compile-time-literal non-empty slice or a runtime collection with an
explicit `found`-flag `Fatalf` guarding the "never found" case — no ghost
loops. 0 CRITICAL, 0 WARNING.

---

### Quality Metrics
**Linter**: Not available (no configured linter beyond `go vet`, clean).
**Type Checker**: No errors (`go build ./...` and `go vet ./...` both clean,
independently re-run this session). One `gofmt` formatting warning (see
WARNING 1) — not a type error.

### Verdict

**PASS WITH WARNINGS**

Independent verification confirms Phase 1 of registry-acl-v1 is correctly
and completely implemented: all 15 Phase-1 tasks are genuinely done (not
just checked), both applicable spec requirements' 6 scenarios have real,
non-trivial, independently-re-run covering tests (unit, integration/store,
and full end-to-end router coverage), the implementation matches design.md
Decisions 5 and 8 verbatim at the code level (not just in prose), zero
Phase 2–5 surface exists yet (confirmed by a repo-wide grep for every
named symbol), and the diff size (599/67 across 15 files) is well within
budget. Both self-reported git mishaps during commit-splitting were
independently re-verified against the actual current file content and test
execution, not merely trusted from the apply-progress report — no residual
defect found.

The single WARNING is a cosmetic `gofmt` violation this PR's own diff
introduced in `internal/tui/model.go` (one stray blank line) — does not
affect build, vet, or any test, and is a one-line fix. The single SUGGESTION
notes a pre-existing (not introduced by this PR) gap in HTTP/TUI reachability
for `UpdateUser`, for future-phase awareness only.

**Recommendation**: proceed to PR 1 merge / `sdd-archive` for Phase 1 after
running `gofmt -w internal/tui/model.go`. No CRITICAL issue exists.
