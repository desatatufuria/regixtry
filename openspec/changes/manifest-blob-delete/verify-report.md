```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:8288576acf944433a56919ec1d19dc37e727258a8eecbf85e6e77d17a2a436eb
verdict: pass_with_warnings
blockers: 0
critical_findings: 0
requirements: 2/2
scenarios: 6/6
test_command: go test -count=1 ./...
test_exit_code: 0
test_output_hash: sha256:44789b6516d40c17bb92a8392cc02de0d6319443d6b2a2c2c844fc65fe022048
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: manifest-blob-delete
**Version**: N/A (delta spec, no version tag)
**Mode**: Strict TDD

**Scope of this verify run**: Phase 1 of 4 only (PR 1 of 4 — scope/auth foundation, branch
`feature/manifest-blob-delete-01-scope-auth-foundation` against base `feature/manifest-blob-delete`).
Phases 2 (store layer), 3 (service layer), and 4 (HTTP layer/config/docs) are explicitly
out of scope for this verify pass and remain unimplemented (`tasks.md` 2.1–4.12 all `[ ]`).
Requirement/scenario counts below cover only the `registry-authentication` and
`repository-authorization` ADDED requirements Phase 1 targets; the `manifest-deletion` domain
(4 requirements / 8 scenarios) belongs entirely to Phases 2–4 and is out of scope here. This
report independently re-verified the apply agent's self-report rather than trusting it.

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total (Phase 1) | 13 |
| Tasks complete (Phase 1) | 13 |
| Tasks incomplete (Phase 1) | 0 |
| Tasks deferred (Phases 2–4, out of scope this PR) | 26 (2.1–4.12), all correctly `[ ]` |

### Build & Tests Execution
**Build**: ✅ Passed
```text
$ go build ./...
(no output, exit 0)
```

**Vet**: ✅ Passed — `go vet ./...` exit 0, no output.
**Format**: ✅ Clean — `gofmt -l .` exit 0, zero files listed.

**Tests**: ✅ 100% passed / ❌ 0 failed / ⚠️ 0 skipped
```text
$ go test -count=1 ./...
ok  	regixtry/cmd/regixtry	4.160s
ok  	regixtry/internal/app/auth	0.249s
ok  	regixtry/internal/app/regixtry	4.400s
ok  	regixtry/internal/app/scanning	0.055s
ok  	regixtry/internal/domain/auth	0.006s
ok  	regixtry/internal/domain/regixtry	0.018s
ok  	regixtry/internal/domain/signing	0.144s
ok  	regixtry/internal/infra/auth/postgres	0.503s
ok  	regixtry/internal/infra/cliprogress	0.014s
ok  	regixtry/internal/infra/install/linux	0.623s
ok  	regixtry/internal/infra/install/releases	0.095s
ok  	regixtry/internal/infra/metadata/sqlite	0.742s
ok  	regixtry/internal/infra/release	0.023s
ok  	regixtry/internal/infra/scanning/gitleaks	0.401s
ok  	regixtry/internal/infra/scanning/trivy	0.386s
ok  	regixtry/internal/infra/storage/fsblob	0.008s
ok  	regixtry/internal/ports	0.007s
ok  	regixtry/internal/protocol/http	2.829s
ok  	regixtry/internal/tui	0.470s
```
Independently re-ran the focused suites named in the self-report plus `internal/ports` (which
the self-report's "Full regression check" instruction implied but the apply agent's own focused
command line omitted): `go test ./internal/domain/auth/... ./internal/app/auth/... ./internal/protocol/http/... ./internal/ports/... -v` — 315+ subtests, zero `--- FAIL`. All pre-existing
grants/robots/read-only-role/delegated-repo-admin test names (`TestRobotPasswordHashNeverSatisfiesBcryptComparison`,
`TestPrincipalHasGrantedRepositoryAccessReadOnlyProbe`, `TestServicePutRepositoryGrantRejectsDelegateEscalation`,
`TestRequireAdminOrRepoAdmin`, `TestAdminRepositoryGrantRoutesAuthenticationAndDelegateAuthority`, etc.
from earlier today's `registry-acl-v1` work) still pass, unchanged, with the same assertion
shape — not weakened to accommodate this change.

**Coverage**: not measured (no coverage tool run this pass) → ➖ Not available

### Spec Compliance Matrix

**In scope this PR** — `registry-authentication` and `repository-authorization` ADDED requirements
only. Per design.md Decision 6, `AccessController.Authorize(Action{Verb: ActionDelete, ...})` is
the exact, sole authorization decision point a real `DELETE` HTTP request will invoke once Phase 4
wires the router (`router.go` → `service.authorize(ActionDelete)` → `AccessController.Authorize`,
identical to the existing PUT/push path — there is no separate scope-denial branch anywhere on
`/v2`). Phase 1's tests invoke that exact decision point directly with the same inputs; the only
remaining work is mechanical HTTP plumbing tracked separately in Phase 4 (tasks 4.1–4.10).

| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| Delete Is A Distinct, Explicitly-Requestable Scope Action | Writer-or-higher principal is issued the delete action | `internal/app/auth/service_test.go > TestIntersectRequestedActionsDeleteDerivation/writer_requesting_delete_gets_it` | ✅ COMPLIANT — `intersectRequestedActions` is the exact function `/auth/token` calls via `grantedScopes` to build issued token scope. |
| Delete Is A Distinct, Explicitly-Requestable Scope Action | Push-only token is rejected on delete | `internal/domain/auth/principal_test.go > TestPrincipalHasDeleteAccess/writer_with_pull,push_scope_only_fails`; `internal/ports/defaults_test.go > TestPrincipalAccessControllerAuthorizesDeleteForWriterAndAdminOnly` | ✅ COMPLIANT — the authorization decision point is exercised directly with `Verb: ActionDelete` and a `pull,push`-only-scoped principal, and correctly denies. |
| Delete Is A Distinct, Explicitly-Requestable Scope Action | Explicitly-scoped delete token succeeds | `internal/domain/auth/principal_test.go > TestPrincipalHasDeleteAccess/writer_with_delete_scope_passes`, `.../admin_with_delete_scope_passes`; `internal/ports/defaults_test.go > TestPrincipalAccessControllerAuthorizesDeleteForWriterAndAdminOnly` | ✅ COMPLIANT — same decision point, `pull,push,delete`-scoped writer/admin principal correctly authorized. |
| Repo-Writer Authorizes Manifest Deletion, Symmetric With Push | Repo-writer and repo-admin succeed on delete | `internal/domain/auth/principal_test.go > TestPrincipalHasDeleteAccess` (writer/admin cases); `internal/ports/defaults_test.go > TestPrincipalAccessControllerAuthorizesDeleteForWriterAndAdminOnly` | ✅ COMPLIANT |
| Repo-Writer Authorizes Manifest Deletion, Symmetric With Push | Repo-reader is rejected on delete | `internal/domain/auth/principal_test.go > TestPrincipalHasDeleteAccess/reader_with_delete_scope_fails`; `internal/ports/defaults_test.go > TestPrincipalAccessControllerAuthorizesDeleteForWriterAndAdminOnly/reader_fails` | ✅ COMPLIANT |
| Repo-Writer Authorizes Manifest Deletion, Symmetric With Push | Delete authorization is repository-scoped | `internal/domain/auth/principal_test.go > TestPrincipalHasDeleteAccess` (via `hasGrantedRepositoryAccess`, the same repository-keyed grant lookup `HasWriteAccess`/`HasReadAccess` already use, unmodified this phase) | ✅ COMPLIANT |

**Compliance summary**: 6/6 in-scope scenarios compliant at the authorization-decision level —
the actual business logic these requirements describe. See WARNING 1 below for the one remaining,
explicitly out-of-scope caveat: no live HTTP request can reach this logic until Phase 4 wires
`router.go`, so end-to-end wire-level behavior is unverifiable until that PR lands.

### Correctness (Static Evidence)
| Requirement / Claim | Status | Notes |
|------|--------|-------|
| `HasDeleteAccess` does NOT reuse `HasWriteAccess` | ✅ Confirmed | Read `internal/domain/auth/principal.go` directly: `HasDeleteAccess` calls `p.scopeAllowsRepository(repository, Scope.AllowsDelete)`, a distinct predicate from `HasWriteAccess`'s `Scope.AllowsPush`. Table-driven test proves a `pull,push`-only-scoped writer principal returns `false` (`writer_with_pull,push_scope_only_fails`). |
| `scope.go`'s fix is real and correctly scoped | ✅ Confirmed | `normalizeScopeActions` allow-list now accepts `actionDelete`; canonical sort-weight function: `pull→0, push→1, delete→2, default→3`. This is a natural insertion after the pre-existing `pull(0)/push(1)` ordering, not an arbitrary append — `default` (catalog wildcard `*`) still sorts last. Round-trip test confirms `delete,pull,push` canonicalizes to `pull,push,delete`, and `pull,unknown` still fails. |
| `intersectRequestedActions`'s new branch is genuinely independent | ✅ Confirmed | Read `internal/app/auth/service.go` directly: `allowDelete` is its own `if grant.Role.AllowsWrite() && requested.AllowsDelete()` statement, never folded into the push branch (`AllowsPush() \|\| AllowsDelete()` pattern does NOT appear). Test `writer_requesting_pull,push_never_gets_delete` proves a repo-writer who does not explicitly request `delete` scope does not receive it — write access alone does not imply delete. |
| `configurableAccessController` has no delete arm | ✅ Confirmed | Read `internal/ports/defaults.go` directly: `configurableAccessController.Authorize` only checks `ActionVerb == ActionPull \|\| ActionInspect \|\| ActionCatalog` (anonymous pull) and `ActionVerb == ActionPush` (anonymous push); no `ActionDelete` branch exists anywhere in that method, so it falls through to `NewUnauthorizedError` by construction. Confirmed with a passing test even with `AllowAnonymousPull: true, AllowAnonymousPush: true` both set. |
| No reachable HTTP behavior change yet | ✅ Confirmed | `git diff feature/manifest-blob-delete...HEAD -- internal/protocol/http/router.go` is empty — zero production changes to the router this phase. `router_test.go` gained one new characterization test only. Live-ran `TestRouterManifestMethodDispatchCharacterizesCurrentBehavior`: DELETE on `manifests/<ref>` still returns `405` with `Allow: PUT, GET, HEAD` today; PUT/GET/HEAD unchanged. |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| Decision 4 — `delete` added to scope vocabulary, canonical weight `pull(0)/push(1)/delete(2)` | ✅ Yes | Matches design.md's exact code shape, including the error message text update (`"...must be pull, push, and/or delete"`). |
| Decision 5 — `HasDeleteAccess` does not reuse `HasWriteAccess`; `intersectRequestedActions` gets its own `allowDelete` statement, never folded into push | ✅ Yes | Matches design.md's exact code shape verbatim, including the code comments explaining the "own statement" rationale (proposal Q4 — no future verb can inherit writer derivation by being forgotten). |
| `ActionDelete.Scope()` returns `"repository:<name>:delete"`, not `"pull,delete"` | ✅ Yes | Confirmed in `internal/ports/regixtry.go` diff and `TestActionScopeForDeleteAction`. |

### TDD Compliance
| Check | Result | Details |
|-------|--------|---------|
| TDD Evidence reported | ✅ | Full "TDD Cycle Evidence" table found in apply-progress artifact, 13 rows. |
| All tasks have tests | ✅ | 13/13 Phase 1 tasks map to a test file; GREEN-only tasks (1.4, 1.6, 1.8, 1.10, 1.12) are paired with an immediately-preceding RED task in the same file. |
| RED confirmed (tests exist) | ✅ | All 9 reported test functions independently confirmed to exist in the working tree: `TestParseScopeAcceptsPullPushDelete`, `TestActionScopeForDeleteAction`, `TestPrincipalHasDeleteAccess`, `TestIntersectRequestedActionsDeleteDerivation`, `TestPrincipalAccessControllerAuthorizesDeleteForWriterAndAdminOnly`, `TestConfigurableAccessControllerNeverAuthorizesAnonymousDelete`, `TestRouterManifestMethodDispatchCharacterizesCurrentBehavior`. |
| GREEN confirmed (tests pass) | ✅ | 9/9 test files verified passing via independent re-run (`go test ./internal/domain/auth/... ./internal/app/auth/... ./internal/protocol/http/... ./internal/ports/... -v`), zero `--- FAIL`. |
| Triangulation adequate | ✅ | `HasDeleteAccess` 5 cases, `intersectRequestedActions` delete derivation 5 cases, `ParseScope` 4 cases, `principalAccessController` delete 3 cases — all multi-case tables with varying expected outcomes (not all-same-value). |
| Safety Net for modified files | ✅ | Pre-existing suites for every modified file (`scope.go`, `principal.go`, `service.go`, `defaults.go`, `regixtry.go`) passed before and after, per apply-progress and independently re-confirmed via the full `go test ./...` run. |

**TDD Compliance**: 6/6 checks passed

---

### Test Layer Distribution
| Layer | Tests | Files | Tools |
|-------|-------|-------|-------|
| Unit | 8 | 5 (`scope_test.go`, `action_scope_test.go`, `principal_test.go`, `service_test.go`, `defaults_test.go`) | Go `testing`, table-driven |
| Integration | 1 | 1 (`router_test.go`, `httptest`) | Go `net/http/httptest` |
| E2E | 0 | 0 | not applicable this phase (no runnable endpoint yet) |
| **Total** | **9** | **6** | |

---

### Changed File Coverage
Coverage analysis skipped — no coverage tool run this pass (informational only, not a
blocking omission per skill rules).

---

### Assertion Quality
Scanned all 6 touched/created test files (`scope_test.go`, `principal_test.go`,
`service_test.go`, `defaults_test.go`, `action_scope_test.go`, `router_test.go`) for banned
patterns (tautologies, orphan empty checks, ghost loops, mock-heavy ratios, implementation-detail
coupling). Zero matches for `toBe(true)`/`assert True`/similar tautology patterns; zero mock
usage in the touched files (all tests exercise real production functions/structs directly);
every table-driven case asserts a concrete, varying expected value (`true`/`false` mixes,
distinct canonical strings, distinct action slices) rather than a single repeated trivial value.

**Assertion quality**: ✅ All assertions verify real behavior

---

### Quality Metrics
**Linter**: ➖ Not run this pass (not in cached capabilities/toolchain for this session)
**Type Checker**: ✅ No errors — `go vet ./...` exit 0, `go build ./...` exit 0
**Format**: ✅ No errors — `gofmt -l .` exit 0

### Issues Found
**CRITICAL**: None

**WARNING**:
1. All 6 in-scope scenarios are proven only at the authorization-decision level
   (`AccessController.Authorize`/`intersectRequestedActions`, invoked directly with the same
   inputs a real request would carry). No wire-level HTTP `DELETE` request can reach this logic
   until Phase 4 wires `handleManifest`'s `case stdhttp.MethodDelete` — confirmed by an empty
   `git diff ... -- internal/protocol/http/router.go`. This is expected, intentional scope for a
   4-PR chain, not a defect in this PR; Phase 4's `sdd-verify` run should re-confirm these same
   scenarios against a live router.
2. Coverage and lint tooling were not run this pass (not available/cached for this
   session) — informational only, does not block.

**SUGGESTION**: None

### Verdict
**PASS WITH WARNINGS**

Phase 1 (scope/auth foundation) is complete, correct, and additive. Independent re-inspection
of `scope.go`, `principal.go`, `service.go` (app/auth), and `defaults.go` confirms every claim
in the apply agent's self-report: `HasDeleteAccess` genuinely does not reuse `HasWriteAccess`
(a `pull,push`-only token correctly fails `HasDeleteAccess`); the scope canonicalization fix is
real and consistently ordered; `intersectRequestedActions`'s delete branch is a standalone
statement that requires explicit `delete` scope request rather than being implied by write
access; `configurableAccessController` structurally cannot authorize anonymous delete; and
`router.go` is byte-for-byte unchanged, so DELETE still answers `405` everywhere today. Full
`go build`, `go vet`, `gofmt -l .`, and `go test -count=1 ./...` are all clean with zero
regressions across all 19 packages, including every pre-existing grants/robots/read-only-role/
delegated-repo-admin test. The sole warning is that wire-level HTTP verification of these
scenarios is deferred to Phase 4 by explicit, documented design — not a quality gap in this PR.
