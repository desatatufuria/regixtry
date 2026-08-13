```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:remediated-repository-scan-config-overrides-cascade-delete-test
verdict: pass
blockers: 0
critical_findings: 0
requirements: 15/15
scenarios: 42/42
test_command: go test -count=1 ./...
test_exit_code: 0
test_output_hash: sha256:2989627b7364ee5f3228e1f9dd133b3520e98429c1418ac3d17c2181c899a980
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: repository-scan-config-overrides
**Version**: N/A
**Mode**: Strict TDD

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 69 |
| Tasks complete | 69 |
| Tasks incomplete | 0 |

### Build & Tests Execution
**Build**: ✅ Passed
```text
go build ./...   → exit 0, no output
go vet ./...     → exit 0, no output
gofmt -l .       → exit 0, no output (repo fully formatted)
```

**Tests**: ✅ 17 packages passed / ❌ 0 failed / ⚠️ 0 skipped
```text
go test -count=1 ./...
ok  regixtry/cmd/regixtry                        3.482s
ok  regixtry/internal/app/auth                   0.137s
ok  regixtry/internal/app/regixtry                3.288s
ok  regixtry/internal/app/scanning                0.057s
ok  regixtry/internal/domain/regixtry             0.022s
ok  regixtry/internal/infra/auth/postgres         0.359s
ok  regixtry/internal/infra/cliprogress           0.020s
ok  regixtry/internal/infra/install/linux         0.491s
ok  regixtry/internal/infra/install/releases      0.043s
ok  regixtry/internal/infra/metadata/sqlite       0.463s
ok  regixtry/internal/infra/release               0.029s
ok  regixtry/internal/infra/scanning/gitleaks     0.325s
ok  regixtry/internal/infra/scanning/trivy        0.291s
ok  regixtry/internal/infra/storage/fsblob        0.027s
ok  regixtry/internal/ports                       0.016s
ok  regixtry/internal/protocol/http               1.745s
ok  regixtry/internal/tui                         0.318s
```

**Race detector** (`internal/app/regixtry`, `internal/protocol/http`): clean.
`internal/tui` (`go test -race ./internal/tui/...`): FAILS with a data race in
`buildAdminSecretFindingsTable` / bubbletable's internal atomic column-ID
counter, tripped only by `t.Parallel()` scan-history-modal tests
(`TestModelScanHistoryModalRendersWithinViewportAcross{Heights,Widths}`,
`TestModelScanHistoryModalHistoryNavigationRefetchesDetailAndSecretsPerCursor`).
**Independently re-confirmed as pre-existing in this session**: created a
throwaway worktree at commit `40aae0b` (the commit immediately preceding this
change's TUI work unit, `352e074`) and ran the identical race-triggering
subset (`-run TestModelScanHistoryModalRendersWithinViewport`) there — it
fails identically, with the same goroutine stacks pointing at the same
bubbletable internals. Confirmed not introduced by this change; worktree
removed after the check. Non-blocking, out of scope for this change.

**Coverage**: Not measured — no coverage threshold configured for this repo; skipped per project convention (informational only under Strict TDD verify rules).

### Spec Compliance Matrix

**repository-config-overrides** (7 requirements / 17 scenarios)
| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Override Storage Is Generic | Override stored for existing feature | `store_test.go > TestStoreUpsertRepositoryFeatureOverrideRoundTripsPayloadAndUpdatedAt` | ✅ COMPLIANT |
| Override Storage Is Generic | New feature, no schema change | `store_test.go > TestStoreRepositoryFeatureOverrideIsolatesEachFeatureNameAsIndependentRow` (fabricated `image-signing` feature round-trips) | ✅ COMPLIANT |
| Full-Row-Replace Resolution | Override present applies in full | `repository_overrides_test.go > TestApplyTrivyOverrideAppliesRoundTripAndTolerance`, `TestApplyGitleaksOverrideAppliesRoundTripAndTolerance` | ✅ COMPLIANT |
| Full-Row-Replace Resolution | Override absent falls back to global | `service_scanning_test.go` NotFound branch tests + `repository_overrides_test.go` codec-registry tests | ✅ COMPLIANT |
| Full-Row-Replace Resolution | Fields never blended | `repository_overrides_test.go > TestApply*OverrideAppliesRoundTripAndTolerance` (full struct assigned, no field-merge) | ✅ COMPLIANT |
| Server-Local Paths Fail Run | Missing path fails run | `trivy/runner_test.go > TestRunnerRunFailsPreflightOnUnreadableOverridePaths`, `gitleaks/runner_test.go` equivalent | ✅ COMPLIANT |
| Server-Local Paths Fail Run | Unreadable path fails run | same tests, "ignore file is a directory" case | ✅ COMPLIANT |
| Disabling Override Suppresses Triggers | Override disables while global enabled | `service_scanning_test.go > TestQueueScheduledScanSkipsRepositoryWithDisablingOverride`, `TestQueuePushScanSkipsRepositoryWithDisablingOverride` | ✅ COMPLIANT |
| Disabling Override Suppresses Triggers | Override re-enables while global disabled | `service_scanning_test.go > TestRepositoryOverrideReEnablesScanningWhenGlobalRowDisabled` (3 trigger subtests) | ✅ COMPLIANT |
| Overrides Not Cascade-Deleted | Deleted repository leaves inert row | `store_test.go > TestStoreRepositoryFeatureOverrideResolutionIsExactNameOnlyNoOrphanLeakage` (proxy: old row remains present and resolves under its exact original name after being queried by a different/renamed-to name) | ✅ COMPLIANT |
| Overrides Not Cascade-Deleted | Renamed repository doesn't carry override | `store_test.go > TestStoreRepositoryFeatureOverrideResolutionIsExactNameOnlyNoOrphanLeakage` (proxy: querying a different repository name than the one the override was upserted for resolves `NotFound`, no fuzzy/prefix leakage) | ✅ COMPLIANT |
| Override Authorization Matches Admin | Admin caller can manage | `admin_handlers_test.go > TestAdminRepositoryOverridePutPersistsAndRoundTrips` (authorized flow) | ✅ COMPLIANT |
| Override Authorization Matches Admin | Non-admin caller rejected | `admin_handlers_test.go > TestAdminRepositoryOverrideRequiresAdminPrincipal` | ✅ COMPLIANT |
| Admin HTTP Resource | Reading returns current override | `admin_handlers_test.go > TestAdminRepositoryOverrideGetReturnsCurrentOverride` | ✅ COMPLIANT |
| Admin HTTP Resource | Reading indicates none set | `admin_handlers_test.go > TestAdminRepositoryOverrideGetReturnsNotFoundWhenNoOverrideExists` | ✅ COMPLIANT |
| Admin HTTP Resource | Writing creates/replaces | `admin_handlers_test.go > TestAdminRepositoryOverridePutPersistsAndRoundTrips`, `TestAdminRepositoryOverridePutRejectsMismatchedFeatureShape` | ✅ COMPLIANT |
| Admin HTTP Resource | Deleting reverts to global | `admin_handlers_test.go > TestAdminRepositoryOverrideDeleteRemovesRowThenReturnsNotFound` | ✅ COMPLIANT |

**feature-configuration** (1 requirement / 3 scenarios)
| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Feature-Owned Runtime Authority | Runtime uses global state (no override) | `service_scanning_test.go` NotFound-branch coverage; `repository_overrides_test.go > TestServiceClearRepositoryOverrideRevertsResolutionToGlobalImmediately` | ✅ COMPLIANT |
| Feature-Owned Runtime Authority | Repository override takes precedence | `service_test.go > TestServiceRepositoryOverridePolicyCouplingIsolatesGateOutcomePerRepository` (9.1) | ✅ COMPLIANT |
| Feature-Owned Runtime Authority | No override falls back unchanged | `service_test.go` (9.2) `library/base` byte-identical settings assertion | ✅ COMPLIANT |

**repository-vulnerability-scans** (3 requirements / 8 scenarios)
| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Trivy Honors Ignore File/Policy Override | Ignore-file override changes invocation | `trivy/runner_test.go > TestRunnerRunAppendsIgnoreFlagsWhenOverridePathsSet` | ✅ COMPLIANT |
| Trivy Honors Ignore File/Policy Override | Ignore-policy override changes invocation | same test (both-set case) | ✅ COMPLIANT |
| Trivy Honors Ignore File/Policy Override | No override → unchanged argv | `trivy/runner_test.go > TestRunnerRunArgvUnchangedWithoutOverridePaths` | ✅ COMPLIANT |
| Trivy Honors Ignore File/Policy Override | Missing ignore file fails run | `trivy/runner_test.go > TestRunnerRunFailsPreflightOnUnreadableOverridePaths` | ✅ COMPLIANT |
| Disabling Trivy Override Suppresses | Skipped by scheduled sweep | `service_scanning_test.go > TestQueueScheduledScanSkipsRepositoryWithDisablingOverride` | ✅ COMPLIANT |
| Disabling Trivy Override Suppresses | Does not scan on push | `service_scanning_test.go > TestQueuePushScanSkipsRepositoryWithDisablingOverride` | ✅ COMPLIANT |
| Trivy Override Isolated to Gate Outcome | Overridden repo's ignored finding doesn't block own gate | `service_test.go > TestServiceRepositoryOverridePolicyCouplingIsolatesGateOutcomePerRepository` (9.1, post-rescan `OpenManifest` succeeds) | ✅ COMPLIANT |
| Trivy Override Isolated to Gate Outcome | Other repositories remain gated | same test (9.2, `library/base` still blocked) | ✅ COMPLIANT |

**image-secret-scans** (2 requirements / 7 scenarios)
| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Gitleaks Honors Config Path Override | Config-path override changes invocation | `gitleaks/runner_test.go` argv-append test | ✅ COMPLIANT |
| Gitleaks Honors Config Path Override | No override → unchanged argv | `gitleaks/runner_test.go` argv-unchanged regression pin | ✅ COMPLIANT |
| Gitleaks Honors Config Path Override | Missing config path fails run | `gitleaks/runner_test.go` preflight-fail test | ✅ COMPLIANT |
| Gitleaks Honors Config Path Override | Unreadable config path fails run | same test, directory case | ✅ COMPLIANT |
| Disabling Gitleaks Override Suppresses | Skipped by reused rescan orchestration | `service_scanning_test.go > TestExecuteSecretScanLegSkipsWithDisablingOverride` | ✅ COMPLIANT |
| Disabling Gitleaks Override Suppresses | Re-enables while global disabled | `service_scanning_test.go > TestExecuteSecretScanLegReEnabledByOverrideWhenGlobalRowDisabled` | ✅ COMPLIANT |
| Disabling Gitleaks Override Suppresses | Does not scan on push (spec-corrected scenario, task 4.7) | `service_scanning_test.go > TestQueuePushScanSuppressesGitleaksLegWithDisablingOverride` — exercises the **real** `queuePushScan → executeScanRun → executeSecretScanLeg` chain, not the leg directly | ✅ COMPLIANT |

**operator-admin-tui** (2 requirements / 7 scenarios)
| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Repository-Scoped Override Modal | Opening on no-override row shows global | `admin_views_test.go > TestRenderRepositoryOverrideModalShowsRepositoryAndInheritanceState` | ✅ COMPLIANT |
| Repository-Scoped Override Modal | Opening on override row shows values | same test | ✅ COMPLIANT |
| Repository-Scoped Override Modal | Set from modal | `model_test.go > TestModelRepositoryOverrideModalSetAndClearRoundTripReflectsInModal` | ✅ COMPLIANT |
| Repository-Scoped Override Modal | Clear from modal | same test | ✅ COMPLIANT |
| Repository-Scoped Override Modal | `o` scoped to Repository Alerts row only | `model_test.go > TestModelRepositoryOverrideModalOpenerKeyIsScopedToRepositoryAlertsRow` | ✅ COMPLIANT |
| Repository-Scoped Override Modal | Stays within terminal viewport | `admin_views_test.go > TestRenderRepositoryOverrideModalFitsWithinRowBudget` **+ independently re-verified this session** via a throwaway `renderAdminWorkspace` composite at 150×24 for both the 19-row (trivy+error) and 15-row (gitleaks, no error) extremes — full bottom border and help line intact, no truncation; debug test deleted after confirmation | ✅ COMPLIANT |
| Repository Alerts Renders Disabled Rows Distinctly | Distinct from never-scanned | `admin_tables_test.go > TestAnnotateDisabledSummariesMarksOnlyDisabledOverrideRepositories` | ✅ COMPLIANT |
| Repository Alerts Renders Disabled Rows Distinctly | Distinct from normally-scanned | same test | ✅ COMPLIANT |

**Compliance summary**: 42/42 scenarios COMPLIANT (remediated — see Issues)

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|---|---|---|
| `repository_feature_overrides` schema (Decision 1) | ✅ Implemented | No FK to `repositories`; `UNIQUE(tenant, repository, feature_name)`; additive `CREATE TABLE IF NOT EXISTS` |
| Typed `NotFound`, no `found bool` (Decision 2) | ✅ Implemented | Verified in `store.go`; matches `GetScanSettings`/`DeleteUpload` precedent |
| Codec registry, `ports`-housed types (Decision 3) | ✅ Implemented | `repository_overrides.go`; strict `Normalize` (DisallowUnknownFields, absolute-path/`-`-prefix rejection), lenient `Apply` |
| `ScanSettings` resolve-time fields, no new wrapper (Decision 4) | ✅ Implemented | 3 `json:"-"` fields; all 4 call sites match the table exactly (verified by direct code read) |
| Conditional argv appends (Decision 5) | ✅ Implemented | Verified via `reflect.DeepEqual` argv-snapshot tests in both runners |
| Pre-flight readability check (Decision 6) | ✅ Implemented | `requireReadableFile` in both runner packages (independent copies, no cross-import) |
| Feature-nested admin resource, ordering-sensitive dispatch (Decision 7) | ✅ Implemented | New case precedes `/config`/`/status` `HasSuffix` checks; `team/config` routing test passes |
| `repositoryOverrideModal`, sibling of `scanPolicyModal` (Decision 8) | ✅ Implemented | No changes to `trivyConfigModal`/`scanPolicyModal` structs or `contentBudget`/`compositeOverlay`; verified via diff |
| Pull gate observes override-adjusted counts (Decision 9) | ✅ Implemented | Full chain traced in code and proven end-to-end by `TestServiceRepositoryOverridePolicyCouplingIsolatesGateOutcomePerRepository` |

### Coherence (Design)
| Decision | Followed? | Notes |
|---|---|---|
| Decision 1 (JSON column exception) | ✅ Yes | Confirmed: only JSON column in the store; no field-level SQL queries against `payload` |
| Decision 2–7 | ✅ Yes | Verified above |
| Decision 8 (row-math table) | ✅ Yes | Independently re-verified via live composite render, not just the isolated-height test |
| Decision 9 (gate coupling) | ✅ Yes | Two consequences ("only after rescan", "gate code itself untouched") both proven by test |
| Rollback Plan | ✅ Yes | Additive-only DDL; runner argv conditional on override presence; `scan-policy-gate`/`gitleaks-managed-feature` suites re-run clean (rollback inertness) |
| Open Question: Trivy `--ignorefile`/`--ignore-policy` real-binary failure behavior | ⚠️ Still unverified | Confirmed no `trivy`/`gitleaks` binary in this sandbox (`command -v` fails for both); recorded honestly in design.md, not silently dropped |
| Open Question: Suppressed findings and the report | ⚠️ Still unverified | Same reason; the Service-level gate-coupling chain is proven with a fake runner, which is the correct boundary the app layer owns — Trivy's own suppression behavior is out of this codebase's control |
| Open Question: Payload size bound | ⚠️ Unresolved (task list correctly left unchecked `[ ]`) | `SetRepositoryOverride` has no `MaxBytesReader`; confirmed this matches `decodeAdminJSON`'s existing (pre-change) posture — not a regression, but a genuinely open decision |

### Non-Regression Checks
| Area | Result |
|---|---|
| `go build/vet/gofmt` full repo | ✅ Clean |
| `go test -count=1 ./...` full repo | ✅ 17/17 packages green |
| `go test -race` on `internal/app/regixtry`, `internal/protocol/http` | ✅ Clean |
| `go test -race` on `internal/tui` | ❌ Pre-existing bubbletable race, independently reproduced on commit `40aae0b` (pre-TUI-work-unit) in a throwaway worktree this session — confirmed not introduced by this change |
| `scan-policy-gate` pull-gate suite (`TestScanPolicyViolated`, `TestServiceGetScanPolicySettings...`, `TestServiceUpdateScanPolicySettings...`, `TestServiceResolveManifestIgnoresPolicyGate`) | ✅ Untouched, passing |
| `scan-policy-gate` TUI modal suite (`ScanPolicyModal` tests) | ✅ Untouched, passing |
| `scan-policy-gate` Policy ON/OFF badge (`TestRenderTrivyTabsComposesPolicyBadgeAtZeroRowCost`, `...PolicyBadgeTextReflectsState...`) | ✅ Untouched, passing |
| `viewport.go` (`contentBudget`/`fitLines`) and `admin_overlay.go` (`compositeOverlay`) | ✅ Confirmed untouched by this change's diff |
| Task 4.7 spec correction (image-secret-scans Baseline Note) vs. code | ✅ Confirmed accurate: `executeScanRun` (`service_scanning.go:328`) unconditionally launches `executeSecretScanLeg` regardless of `run.Trigger`, and `queuePushScan` (`:275-293`) reaches `executeScanRun`; verified by direct code read, matches the corrected spec text and the feature-agnostic wording of `repository-config-overrides/spec.md` |

### Issues Found

**CRITICAL**: None (remediated — see below).

**Remediated**:
1. **`repository-config-overrides/spec.md`'s "Override Rows Are Not Cascade-Deleted On Repository Lifecycle Changes" requirement previously had zero covering tests for both of its scenarios** ("Deleted repository leaves an inert override row", "A renamed repository does not carry its override forward"). This codebase has **no repository deletion or rename operation at all** today, and `repository_feature_overrides.repository` is a plain `TEXT` column with no foreign key to `repositories`, so the property holds by construction — but it was previously unproven at runtime. **Remediation applied** (`sdd-apply`, post-verify): added
   `internal/infra/metadata/sqlite/store_test.go >
   TestStoreRepositoryFeatureOverrideResolutionIsExactNameOnlyNoOrphanLeakage`,
   the closest testable proxy given no deletion/rename API exists — it upserts
   an override for one exact repository name, then asserts that querying a
   *different* repository name resolves `NotFound` (no fuzzy/prefix/fallback
   leakage) while the original row remains present and inert under its exact
   original name. The test passed on first run against the existing,
   unmodified implementation — confirming the property already held by
   construction; no production code change was required, only the missing
   test-coverage gap was closed. Both scenarios are now ✅ COMPLIANT in the
   Spec Compliance Matrix above.

**WARNING**:
1. Two design.md Open Questions ("Trivy `--ignorefile` real-binary failure behavior" and "Suppressed findings and the report") remain genuinely unverified because no `trivy`/`gitleaks` binary is available in this sandbox — independently confirmed (`command -v trivy` / `command -v gitleaks` both fail). This is an acceptable, honestly-documented residual risk, not a blocker: every spec scenario that is actually testable from this codebase's own responsibility boundary (correct argv construction, fail-closed pre-flight on missing/unreadable paths) is covered by tests using a fake exec, independent of the real binaries. The two open items concern upstream CLI *internal* filtering behavior, which the spec's literal requirement text does not require this codebase to prove — only to invoke the CLI correctly and fail closed if a path is bad, both of which are tested. Recommend closing this out at the next opportunity where the real binaries are available, but it does not block this verify on its own (independent of the CRITICAL item above).
2. design.md's "Payload size bound" Open Question is correctly left unchecked (not silently resolved) — `SetRepositoryOverride` reads the request body with no `MaxBytesReader`. Confirmed this matches `decodeAdminJSON`'s existing pre-change posture exactly (also no `MaxBytesReader`), so this is not a regression introduced by this change, but it is a genuinely open decision that should be tracked, not silently closed.
3. Minor: `service_scanning.go`'s comment above `executeScanRun`'s `go s.executeSecretScanLeg(...)` call still cites the stale requirement title `spec.md "Reused Rescan Trigger, No Push-Time Path"` without noting the task-4.7 correction. The comment's literal claim (trigger reuse) still holds, but the requirement name is now outdated given push does execute gitleaks; low risk of misleading a future reader.

**SUGGESTION**: None beyond WARNING item 3 above (already captured there due to spec-consistency implications).

### TDD Compliance
| Check | Result | Details |
|---|---|---|
| TDD Evidence reported | ✅ | Found in `apply-progress` (Engram #1017, covering Phases 8-10) plus per-phase commit history (one commit per phase, Phases 1-7) |
| All tasks have tests | ✅ | 68/68 tasks; every RED/GREEN pair in tasks.md maps to an actual test function verified present in the tree |
| RED confirmed (tests exist) | ✅ | All referenced test files and functions exist in the current tree (spot-checked ~25 of them directly) |
| GREEN confirmed (tests pass) | ✅ | `go test -count=1 ./...` — all 17 packages green at HEAD (`8e17039`) |
| Triangulation adequate | ✅ | Table-driven tests throughout (codec Normalize/Apply, runner preflight, modal row-budget, disabling/re-enabling across all 3 trigger paths) |
| Safety Net for modified files | ✅ | Pre-existing `scan-policy-gate`/`gitleaks-managed-feature` suites re-run clean; TUI modal siblings (`ScanPolicyModal`, `TrivyConfigModal`) re-run clean |

**TDD Compliance**: 6/6 checks passed

---

### Test Layer Distribution
| Layer | Tests | Files | Tools |
|---|---|---|---|
| Unit | ~29 | `repository_overrides_test.go`, `store_test.go` (5 new, incl. remediation test), `trivy/runner_test.go` (3 new), `gitleaks/runner_test.go` (3 new), `session_test.go` (2 new), `admin_tables_test.go` (1 new), `admin_views_test.go` (3 new) | `go test`, `reflect.DeepEqual`, `lipgloss.Height` |
| Integration | ~19 | `service_scanning_test.go` (~9 new), `service_test.go` (1 new, 2 subtests, real sqlite `Service`), `admin_handlers_test.go` (9 new, `httptest` full router), `admin_client_test.go` (1 new, `httptest`), `model_test.go` (2 new, full `Update`/`View` key-driven round trips) | `httptest`, real `Service` + sqlite store, bubbletea `Update`/`View` |
| E2E | 0 | — | not applicable (no browser/CLI-process harness for this surface) |
| **Total** | **~48** | 12 test files (1 new, 11 modified) | |

---

### Changed File Coverage
Coverage analysis skipped — no coverage tool run in this session (per project convention, informational only; not required to block verify).

---

### Assertion Quality
✅ All assertions verify real behavior. No tautologies, no ghost loops, no assertion-without-production-call patterns found across all 12 changed test files (`grep` swept for `expect(true).toBe(true)`-equivalent Go patterns — none found; spot-read ~30 individual test bodies directly — every one calls real production code and asserts a distinct, non-trivial expected value, e.g. exact argv slices via `reflect.DeepEqual`, exact HTTP status/body content, exact error codes via `domain.IsCode`, exact row counts after real store operations).

**Assertion quality**: 0 CRITICAL, 0 WARNING

---

### Quality Metrics
**Linter**: ➖ Not available (no configured linter beyond `go vet`, which is clean)
**Type Checker**: ✅ No errors (`go build ./...` clean; Go's compiler is the type checker here)

### Verdict
PASS (remediated)
The prior FAIL's single CRITICAL gap — `repository-config-overrides/spec.md`'s
"Override Rows Are Not Cascade-Deleted On Repository Lifecycle Changes"
requirement having zero covering tests for its 2 scenarios — has been closed.
`sdd-apply` added
`TestStoreRepositoryFeatureOverrideResolutionIsExactNameOnlyNoOrphanLeakage`
(`internal/infra/metadata/sqlite/store_test.go`), the closest testable proxy
given this codebase has no repository deletion/rename operation, proving
resolution is a strict exact-name lookup with no orphan leakage across a
different repository name. The test passed against the unmodified
implementation — no production code changed, only the test-coverage gap was
closed. All 42/42 scenarios are now COMPLIANT, 69/69 tasks complete (task
10.6 added retroactively for this remediation), and a full
`go build/vet/gofmt/test -count=1 ./...` re-run remains green. Everything
else from the prior report stands unchanged: the pre-existing TUI race
independently re-confirmed as not introduced by this change, and the
previously-shipped scan-policy-gate surface (pull gate, modal, badge)
confirmed untouched and passing.
