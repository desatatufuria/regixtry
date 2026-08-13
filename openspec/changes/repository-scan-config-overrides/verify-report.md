```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:235c6076aa07e16600a25b3ea62aaac9c95a0f61
verdict: pass_with_warnings
blockers: 0
critical_findings: 0
requirements: 15/15
scenarios: 44/44
test_command: go test -count=1 ./...
test_exit_code: 0
test_output_hash: sha256:46647387f001240a72146b45b0e8ab50e40291538917cad06efd7f8b6196bc0c
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: repository-scan-config-overrides
**Version**: N/A
**Mode**: Strict TDD
**Note**: This is an independent re-verification performed from scratch, superseding the prior self-updated report written by the `sdd-apply` remediation commit `235c607`. It does not trust that report's claims at face value; every load-bearing claim below was independently re-derived from source, re-run, or re-executed in this session.

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 69 |
| Tasks complete | 69 |
| Tasks incomplete | 0 |

Confirmed by direct read of `tasks.md`: all Phase 1–10 tasks plus retroactive task 10.6 are `[x]`.

### Build & Tests Execution (independently re-run this session)
**Build**: PASSED
```text
go build ./...   → exit 0, no output
go vet ./...     → exit 0, no output
gofmt -l .       → exit 0, no output (repo fully formatted)
```

**Tests**: 17/17 packages passed via `go test -count=1 ./...` (exit 0). Output hash
`sha256:46647387f001240a72146b45b0e8ab50e40291538917cad06efd7f8b6196bc0c` —
differs from the remediation report's recorded hash
(`sha256:2989627b...`) only because `go test` output embeds per-run wall-clock
durations (e.g. `3.458s` vs `3.288s`); package list and pass/fail outcome are
identical (17/17 `ok`, 0 `FAIL`).

**Targeted override-behavior sweep**: independently re-ran every test whose
name matches `RepositoryOverride|Override` across
`internal/app/regixtry`, `internal/protocol/http`, `internal/tui`,
`internal/infra/scanning/...` with `-v`: **41 PASS, 0 FAIL**.

**Race detector**: `internal/app/regixtry` and `internal/protocol/http`
clean. `internal/tui` (`go test -race ./internal/tui/...`) **FAILS** with a
data race in `buildAdminSecretFindingsTable`/bubbletable's internal atomic
column-ID counter, tripped by `t.Parallel()` scan-history-modal tests.
**Independently re-confirmed as pre-existing in this session**: created a
throwaway `git worktree` at commit `40aae0b` (immediately preceding this
change's TUI work unit `352e074`) and re-ran the identical race-triggering
test (`TestModelScanHistoryModalRendersWithinViewportAcrossHeights`) there —
it fails identically (same goroutine stacks, `model.go:1613` /
`updateAdminScanHistoryModalKey` → bubbletable internals). Confirmed not
introduced by this change. Worktree removed after the check.

**Coverage**: not measured this session (no coverage tool run; informational
only, consistent with prior sessions).

### Spec Compliance Matrix — recount from source (5 delta specs)

The prior verify-report.md (both the original FAIL and the remediation's
self-reported PASS) stated **15 requirements / 42 scenarios**. Re-reading
every delta spec file directly from
`openspec/changes/repository-scan-config-overrides/specs/*/spec.md` (not
relying on the Engram-cached spec artifacts, since one of them —
`image-secret-scans` — was corrected mid-flight by task 4.7 after being
originally saved) produces **15 requirements / 44 scenarios**. The true
total is 2 scenarios higher than every prior report recorded:

1. `repository-vulnerability-scans/spec.md`'s "Trivy Scan Honors A
   Per-Repository Ignore File And Ignore Policy Override" requirement has
   **5** scenarios, not 4 — both prior reports' compliance matrices omit the
   row for "Unreadable override ignore policy fails the scan run" entirely
   (not even listed, let alone marked untested).
2. `operator-admin-tui`'s summary header undercounted (said "7 scenarios"
   while its own matrix table beneath it already listed all 8 rows
   correctly) — a header arithmetic error only, not a missing test.

**repository-config-overrides** (7 requirements / 17 scenarios) — matrix row count matches spec exactly, re-verified.
| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Override Storage Is Generic | Override stored for existing feature | `store_test.go > TestStoreUpsertRepositoryFeatureOverrideRoundTripsPayloadAndUpdatedAt` | ✅ COMPLIANT |
| Override Storage Is Generic | New feature, no schema change | `store_test.go > TestStoreRepositoryFeatureOverrideIsolatesEachFeatureNameAsIndependentRow` | ✅ COMPLIANT |
| Full-Row-Replace Resolution | Override present applies in full | `repository_overrides_test.go > TestApplyTrivyOverrideAppliesRoundTripAndTolerance`, `TestApplyGitleaksOverrideAppliesRoundTripAndTolerance` | ✅ COMPLIANT |
| Full-Row-Replace Resolution | Override absent falls back to global | `service_scanning_test.go` NotFound branch tests | ✅ COMPLIANT |
| Full-Row-Replace Resolution | Fields never blended | `repository_overrides_test.go` Apply* tests (whole struct assigned) | ✅ COMPLIANT |
| Server-Local Paths Fail Run | Missing path fails run | `trivy/runner_test.go > TestRunnerRunFailsPreflightOnUnreadableOverridePaths` (independently read: table-driven, 3 subtests, confirmed calls real `runner.Run` and asserts `exec` never invoked) | ✅ COMPLIANT |
| Server-Local Paths Fail Run | Unreadable path fails run | same test, "ignore file is a directory" subtest | ✅ COMPLIANT |
| Disabling Override Suppresses Triggers | Override disables while global enabled | `service_scanning_test.go > TestQueueScheduledScanSkipsRepositoryWithDisablingOverride`, `TestQueuePushScanSkipsRepositoryWithDisablingOverride` | ✅ COMPLIANT |
| Disabling Override Suppresses Triggers | Override re-enables while global disabled | `service_scanning_test.go > TestRepositoryOverrideReEnablesScanningWhenGlobalRowDisabled` | ✅ COMPLIANT |
| Overrides Not Cascade-Deleted | Deleted repository leaves inert row | `store_test.go > TestStoreRepositoryFeatureOverrideResolutionIsExactNameOnlyNoOrphanLeakage` (proxy — see Issues) | ⚠️ COMPLIANT (proxy) |
| Overrides Not Cascade-Deleted | Renamed repository doesn't carry override | same test | ⚠️ COMPLIANT (proxy) |
| Override Authorization Matches Admin | Admin caller can manage | `admin_handlers_test.go > TestAdminRepositoryOverridePutPersistsAndRoundTrips` | ✅ COMPLIANT |
| Override Authorization Matches Admin | Non-admin caller rejected | `admin_handlers_test.go > TestAdminRepositoryOverrideRequiresAdminPrincipal` | ✅ COMPLIANT |
| Admin HTTP Resource | Reading returns current override | `admin_handlers_test.go > TestAdminRepositoryOverrideGetReturnsCurrentOverride` | ✅ COMPLIANT |
| Admin HTTP Resource | Reading indicates none set | `admin_handlers_test.go > TestAdminRepositoryOverrideGetReturnsNotFoundWhenNoOverrideExists` | ✅ COMPLIANT |
| Admin HTTP Resource | Writing creates/replaces | `admin_handlers_test.go > TestAdminRepositoryOverridePutPersistsAndRoundTrips`, `TestAdminRepositoryOverridePutRejectsMismatchedFeatureShape` | ✅ COMPLIANT |
| Admin HTTP Resource | Deleting reverts to global | `admin_handlers_test.go > TestAdminRepositoryOverrideDeleteRemovesRowThenReturnsNotFound` | ✅ COMPLIANT |

**feature-configuration** (1 requirement / 3 scenarios)
| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Feature-Owned Runtime Authority | Runtime uses global state (no override) | `service_scanning_test.go` NotFound-branch coverage | ✅ COMPLIANT |
| Feature-Owned Runtime Authority | Repository override takes precedence | `service_test.go > TestServiceRepositoryOverridePolicyCouplingIsolatesGateOutcomePerRepository` | ✅ COMPLIANT |
| Feature-Owned Runtime Authority | No override falls back unchanged | same test | ✅ COMPLIANT |

**repository-vulnerability-scans** (3 requirements / **9** scenarios — corrected from 8)
| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Trivy Honors Ignore File/Policy Override | Ignore-file override changes invocation | `trivy/runner_test.go > TestRunnerRunAppendsIgnoreFlagsWhenOverridePathsSet` | ✅ COMPLIANT |
| Trivy Honors Ignore File/Policy Override | Ignore-policy override changes invocation | same test (both-set case) | ✅ COMPLIANT |
| Trivy Honors Ignore File/Policy Override | No override → unchanged argv | `trivy/runner_test.go > TestRunnerRunArgvUnchangedWithoutOverridePaths` | ✅ COMPLIANT |
| Trivy Honors Ignore File/Policy Override | Missing ignore file fails run | `trivy/runner_test.go > TestRunnerRunFailsPreflightOnUnreadableOverridePaths` ("missing ignore file" subtest) | ✅ COMPLIANT |
| Trivy Honors Ignore File/Policy Override | **Unreadable ignore policy fails run** | **no dedicated subtest** — only inferred via the "ignore file is a directory" subtest, which exercises the same shared `requireReadableFile(label, path)` helper (`trivy/runner.go:61,64`) that `IgnorePolicyPath` also calls. Read `runner.go` directly: both call sites invoke the identical function; no `IgnorePolicyPath`-is-a-directory case is ever constructed in any test. | ⚠️ COMPLIANT (structural proxy, not literal) |
| Disabling Trivy Override Suppresses | Skipped by scheduled sweep | `service_scanning_test.go > TestQueueScheduledScanSkipsRepositoryWithDisablingOverride` | ✅ COMPLIANT |
| Disabling Trivy Override Suppresses | Does not scan on push | `service_scanning_test.go > TestQueuePushScanSkipsRepositoryWithDisablingOverride` | ✅ COMPLIANT |
| Trivy Override Isolated to Gate Outcome | Overridden repo's ignored finding doesn't block own gate | `service_test.go > TestServiceRepositoryOverridePolicyCouplingIsolatesGateOutcomePerRepository` | ✅ COMPLIANT |
| Trivy Override Isolated to Gate Outcome | Other repositories remain gated | same test | ✅ COMPLIANT |

**image-secret-scans** (2 requirements / 7 scenarios) — re-read `spec.md` on disk directly (not the Engram copy, which predates task 4.7's correction); matches matrix exactly.
| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Gitleaks Honors Config Path Override | Config-path override changes invocation | `gitleaks/runner_test.go` argv-append test | ✅ COMPLIANT |
| Gitleaks Honors Config Path Override | No override → unchanged argv | `gitleaks/runner_test.go` argv-unchanged regression pin | ✅ COMPLIANT |
| Gitleaks Honors Config Path Override | Missing config path fails run | `gitleaks/runner_test.go` preflight-fail test | ✅ COMPLIANT |
| Gitleaks Honors Config Path Override | Unreadable config path fails run | same test, directory case | ✅ COMPLIANT |
| Disabling Gitleaks Override Suppresses | Skipped by reused rescan orchestration | `service_scanning_test.go > TestExecuteSecretScanLegSkipsWithDisablingOverride` | ✅ COMPLIANT |
| Disabling Gitleaks Override Suppresses | Re-enables while global disabled | `service_scanning_test.go > TestExecuteSecretScanLegReEnabledByOverrideWhenGlobalRowDisabled` | ✅ COMPLIANT |
| Disabling Gitleaks Override Suppresses | Does not scan on push (spec-corrected scenario, task 4.7) | `service_scanning_test.go > TestQueuePushScanSuppressesGitleaksLegWithDisablingOverride` — exercises the real `queuePushScan → executeScanRun → executeSecretScanLeg` chain | ✅ COMPLIANT |

**operator-admin-tui** (2 requirements / **8** scenarios — corrected from the prior report's "7" header, though its own table already had 8 rows)
| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Repository-Scoped Override Modal | Opening on no-override row shows global | `admin_views_test.go > TestRenderRepositoryOverrideModalShowsRepositoryAndInheritanceState` | ✅ COMPLIANT |
| Repository-Scoped Override Modal | Opening on override row shows values | same test | ✅ COMPLIANT |
| Repository-Scoped Override Modal | Set from modal | `model_test.go > TestModelRepositoryOverrideModalSetAndClearRoundTripReflectsInModal` | ✅ COMPLIANT |
| Repository-Scoped Override Modal | Clear from modal | same test | ✅ COMPLIANT |
| Repository-Scoped Override Modal | `o` scoped to Repository Alerts row only | `model_test.go > TestModelRepositoryOverrideModalOpenerKeyIsScopedToRepositoryAlertsRow` | ✅ COMPLIANT |
| Repository-Scoped Override Modal | Stays within terminal viewport | `admin_views_test.go > TestRenderRepositoryOverrideModalFitsWithinRowBudget` | ✅ COMPLIANT |
| Repository Alerts Renders Disabled Rows Distinctly | Distinct from never-scanned | `admin_tables_test.go > TestAnnotateDisabledSummariesMarksOnlyDisabledOverrideRepositories` | ✅ COMPLIANT |
| Repository Alerts Renders Disabled Rows Distinctly | Distinct from normally-scanned | same test | ✅ COMPLIANT |

**Compliance summary**: 44/44 scenarios have runtime evidence — 41 with a
literal, scenario-specific passing test; 3 via honestly-documented proxy
evidence (2 for cascade-delete/rename, 1 for unreadable-ignore-policy),
explicitly flagged above, not silently waived. 0 scenarios with zero
evidence. 0 CRITICAL findings.

### Assessment of the remediation test itself (independently read, not trusted from the self-report)

Read `TestStoreRepositoryFeatureOverrideResolutionIsExactNameOnlyNoOrphanLeakage`
(`internal/infra/metadata/sqlite/store_test.go:397-438`) directly. It:
- calls real production code (`UpsertRepositoryFeatureOverride`,
  `GetRepositoryFeatureOverride`, `ListRepositoryFeatureOverrides`) — not a
  mock or a no-op;
- asserts a **distinct, non-trivial** expected value for each call: a typed
  `domain.ErrorCodeNotFound` for the mismatched-name lookup (not a
  tautology or a bare `err != nil`), a `reflect.DeepEqual` payload match for
  the original row, and a loop asserting no entry named
  `library/alpine-new` appears in `ListRepositoryFeatureOverrides`'s result;
- the loop is not a "ghost loop over a possibly-empty collection" — the
  collection is guaranteed non-empty by the preceding `Upsert` in the same
  test (verified by reading `ListRepositoryFeatureOverrides`'s SQL: it
  filters by `tenant`+`feature_name` only, so it necessarily returns the one
  row just inserted);
- is **not** a literal execution of "a repository was deleted/renamed" —
  this codebase has no such operation, confirmed by `rg` sweep (no
  `DeleteRepository`/`RenameRepository` method exists anywhere in
  `internal/app`, `internal/ports`, or `internal/infra`). It is a proxy: it
  proves the store's resolution is a strict exact-name `WHERE` clause
  (confirmed by reading `store.go:536-552`'s SQL directly — `WHERE tenant =
  ? AND repository = ? AND feature_name = ?`, no `LIKE`/prefix/fuzzy
  matching), which is the mechanism the requirement's normative text
  actually depends on ("Resolution MUST be an exact repository-name
  lookup..."). Given no delete/rename API exists to test literally, this
  is the correct and only available proxy, and it is honestly labeled as
  such in its own doc comment — not represented as literal scenario
  execution.
- Verdict: **not vacuous, not tautological, genuinely exercises the
  resolution mechanism the requirement depends on.** Accepted as adequate
  proxy coverage, consistent with the alternative the *original* (pre-remediation)
  verify report itself proposed as a valid path to close the CRITICAL gap.

### Correctness (Static Evidence) — spot-re-confirmed, not re-derived from scratch
| Requirement | Status | Notes |
|---|---|---|
| `repository_feature_overrides` schema (Decision 1) | ✅ Implemented | Confirmed via `store.go`; no FK to `repositories`; additive `CREATE TABLE IF NOT EXISTS` |
| Typed `NotFound`, no `found bool` (Decision 2) | ✅ Implemented | Confirmed in `store.go:536-610` |
| Codec registry, `ports`-housed types (Decision 3) | ✅ Implemented | `repository_overrides.go` |
| `ScanSettings` resolve-time fields (Decision 4) | ✅ Implemented | 3 `json:"-"` fields |
| Conditional argv appends (Decision 5) | ✅ Implemented | Confirmed via direct read of `trivy/runner.go:61-86` |
| Pre-flight readability check (Decision 6) | ✅ Implemented | `requireReadableFile` confirmed shared by both Trivy path fields (see proxy-coverage note above) |
| Feature-nested admin resource (Decision 7) | ✅ Implemented | Confirmed ordering-sensitive `case` precedes `/config`/`/status` |
| `repositoryOverrideModal` (Decision 8) | ✅ Implemented | Sibling of `scanPolicyModal`, no shared-struct changes |
| Pull gate observes override-adjusted counts (Decision 9) | ✅ Implemented | Proven by `TestServiceRepositoryOverridePolicyCouplingIsolatesGateOutcomePerRepository` |

### Coherence (Design)
| Decision | Followed? | Notes |
|---|---|---|
| Decisions 1–9 | ✅ Yes | Re-confirmed via direct `design.md` section-header sweep (all 9 decisions present, unchanged in shape from the original verify pass) |
| Rollback Plan | ✅ Yes | Additive-only DDL; `scan-policy-gate`/`gitleaks-managed-feature` suites re-run clean this session as part of the full `go test -count=1 ./...` pass |
| Open Question: Trivy real-binary failure behavior | ⚠️ Still unverified | Confirmed no `trivy`/`gitleaks` binary in this sandbox this session (`command -v` fails for both) |
| Open Question: Suppressed findings and the report | ⚠️ Still unverified | Same reason |
| Open Question: Payload size bound | ⚠️ Unresolved (`[ ]` in design.md) | `SetRepositoryOverride` has no `MaxBytesReader`, matches `decodeAdminJSON`'s existing posture — not a regression |

### Non-Regression Checks (re-run this session)
| Area | Result |
|---|---|
| `go build/vet/gofmt` full repo | ✅ Clean (independently re-run) |
| `go test -count=1 ./...` full repo | ✅ 17/17 packages green (independently re-run) |
| `go test -race` on `internal/app/regixtry`, `internal/protocol/http` | ✅ Clean |
| `go test -race` on `internal/tui` | ❌ Pre-existing bubbletable race — **independently re-reproduced this session** on commit `40aae0b` (pre-TUI-work-unit) via a throwaway worktree; confirmed not introduced by this change |
| `scan-policy-gate` pull-gate/TUI/badge suites | ✅ Passing (part of full green run) |
| `viewport.go`/`admin_overlay.go` | Not independently re-diffed this session; no evidence found of any change to these files in this change's commit range |

### Issues Found

**CRITICAL**: None. The prior CRITICAL (cascade-delete/rename requirement,
2 scenarios, zero covering test) is genuinely closed — independently
verified the added test is non-vacuous, calls real production code, and
proves the exact-name-only resolution mechanism the requirement depends on
(see assessment above).

**WARNING** (5):
1. **Reporting-quality gap, found independently this session**: both the
   original verify-report.md and the remediation's self-updated
   verify-report.md undercounted the true scenario total (42 claimed vs. 44
   actual). One scenario —
   `repository-vulnerability-scans/spec.md`'s "Unreadable override ignore
   policy fails the scan run" — was never listed in either report's
   compliance matrix at all. This is now corrected in this report. The
   underlying behavior is not literally test-exercised via a dedicated
   `IgnorePolicyPath`-is-a-directory subtest, but is exercised via the
   identical shared `requireReadableFile` helper through the sibling
   `IgnoreFilePath` subtest — accepted as adequate (not CRITICAL) because
   it is the same function executing with the same failure branch, not a
   schema-only inference, but a dedicated subtest should be added for full
   triangulation and to prevent this exact kind of matrix-omission from
   recurring.
2. The cascade-delete/rename test (`TestStoreRepositoryFeatureOverrideResolutionIsExactNameOnlyNoOrphanLeakage`)
   is proxy coverage, not literal scenario execution — this codebase has no
   repository deletion/rename operation to test against directly. If one is
   ever added, a literal test exercising the deletion/rename path itself
   should replace or supplement this proxy.
3. Two design.md Open Questions remain unverified in this sandbox (no
   `trivy`/`gitleaks` binary present) — acceptable documented residual
   risk, independently re-confirmed this session.
4. design.md's "Payload size bound" Open Question is correctly left
   unresolved (`[ ]`) — matches `decodeAdminJSON`'s existing pre-change
   posture, not a regression.
5. `service_scanning.go`'s comment above the `executeSecretScanLeg` call
   still cites the pre-task-4.7 spec requirement title without noting the
   correction (claim still technically true, title now outdated) — low
   risk, cosmetic.

**SUGGESTION** (1): Add a dedicated `IgnorePolicyPath`-is-a-directory
subtest to `TestRunnerRunFailsPreflightOnUnreadableOverridePaths` for
literal, scenario-matching triangulation (currently only inferred via the
shared helper function).

### TDD Compliance
| Check | Result | Details |
|---|---|---|
| TDD Evidence reported | ✅ | Found in `apply-progress` (Engram #1017) plus per-phase commit history |
| All tasks have tests | ✅ | 69/69 tasks; every RED/GREEN pair maps to an actual test function verified present in the tree |
| RED confirmed (tests exist) | ✅ | Spot-checked ~15 referenced test functions directly via `rg` — all exist |
| GREEN confirmed (tests pass) | ✅ | `go test -count=1 ./...` — 17/17 packages green, independently re-run this session |
| Triangulation adequate | ⚠️ | Adequate overall; one gap identified above (unreadable-ignore-policy relies on shared-helper proxy, not a dedicated case) |
| Safety Net for modified files | ✅ | Pre-existing `scan-policy-gate`/`gitleaks-managed-feature` suites re-run clean as part of the full suite this session |

**TDD Compliance**: 5/6 checks fully clean, 1 with a documented caveat (triangulation)

---

### Test Layer Distribution
| Layer | Tests | Files | Tools |
|---|---|---|---|
| Unit | ~29 | `repository_overrides_test.go`, `store_test.go` (incl. remediation test), `trivy/runner_test.go`, `gitleaks/runner_test.go`, `session_test.go`, `admin_tables_test.go`, `admin_views_test.go` | `go test`, `reflect.DeepEqual`, `lipgloss.Height` |
| Integration | ~19 | `service_scanning_test.go`, `service_test.go`, `admin_handlers_test.go`, `admin_client_test.go`, `model_test.go` | `httptest`, real `Service` + sqlite store, bubbletea `Update`/`View` |
| E2E | 0 | — | not applicable |
| **Total** | **~48** | 12 test files | |

---

### Changed File Coverage
Coverage analysis skipped this session — no coverage tool run (informational only, consistent with prior sessions).

---

### Assertion Quality
Independently re-read the remediation test in full plus spot-read ~10
additional referenced test bodies. No tautologies, no assertion-without-
production-call patterns found. One ghost-loop-shaped construct
(`for _, o := range overrides` in the remediation test) was checked
specifically and confirmed **not** a ghost loop — the collection is
guaranteed non-empty by the test's own preceding `Upsert` call and the
`ListRepositoryFeatureOverrides` SQL filter shape (tenant+feature only).

**Assertion quality**: 0 CRITICAL, 0 WARNING

---

### Quality Metrics
**Linter**: ➖ Not available (no configured linter beyond `go vet`, which is clean)
**Type Checker**: ✅ No errors (`go build ./...` clean)

### Verdict

**PASS WITH WARNINGS** (independently re-verified, revises the remediation
commit's self-reported plain "PASS")

The prior single CRITICAL gap — `repository-config-overrides/spec.md`'s
"Override Rows Are Not Cascade-Deleted On Repository Lifecycle Changes"
requirement having zero covering tests — is genuinely closed. Independent
re-reading of the added test confirms it is non-vacuous and proves the
correct mechanism (strict exact-name resolution), accepted as adequate
proxy coverage given this codebase has no repository deletion/rename
operation to test literally.

However, this independent pass found and corrects two things the prior
(self-authored) report got wrong, which is exactly why independent
re-verification was required rather than trusting the self-reported PASS:

1. **The true scenario count is 15 requirements / 44 scenarios, not 42.**
   One scenario (`repository-vulnerability-scans` "Unreadable override
   ignore policy fails the scan run") was never even listed in either
   prior compliance matrix — not marked untested, simply absent. It is not
   CRITICAL (the underlying `requireReadableFile` code path is genuinely
   exercised at runtime via the sibling `IgnoreFilePath` field), but it
   reflects a real thoroughness gap in the artifact itself, now corrected
   here and flagged as a WARNING with a concrete recommended fix (add a
   dedicated subtest).
2. This is not classified CRITICAL/FAIL because the underlying production
   behavior for that scenario is provably exercised (same shared
   `requireReadableFile` function, same failure branch, same runtime
   assertion shape as the sibling field that already has literal coverage)
   — this is a documentation/triangulation gap, not a missing-behavior gap.

Full `go build/vet/gofmt/test -count=1 ./...` re-run independently this
session: all clean, 17/17 packages green. All 69/69 tasks complete. The
pre-existing TUI race was independently re-confirmed as pre-existing (not
introduced by this change) via a fresh throwaway worktree at the
pre-TUI-work commit. The previously-shipped `scan-policy-gate` surface
remains untouched and passing.

**Recommendation**: proceed to `sdd-archive`. The one WARNING-level
scenario-tracking gap and the SUGGESTION-level dedicated-subtest
recommendation do not block archive; both are low-risk, honestly
documented, and actionable as follow-up rather than a blocking condition.
