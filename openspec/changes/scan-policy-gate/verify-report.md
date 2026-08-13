```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:08902ba9d04825c5f4dc2b509e14cdc5bfdf472c782f8af3aad79fba138dfa88
verdict: fail
blockers: 2
critical_findings: 2
requirements: 6/7
scenarios: 17/19
test_command: go test -count=1 ./...
test_exit_code: 0
test_output_hash: sha256:bc3171e427220f0822de384ad051577c3923f2d4d5eed7027aa58258cbd2f358
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: scan-policy-gate
**Version**: N/A
**Mode**: Strict TDD

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 60 |
| Tasks complete | 60 |
| Tasks incomplete | 0 |

### Build & Tests Execution
**Build**: PASSED
```text
$ go build ./...
(no output, exit 0)
$ go vet ./...
(no output, exit 0)
$ gofmt -l .
(no output — no files need formatting)
```

**Tests**: All 17 packages pass, freshly run at HEAD `87482a9` (independently executed, not trusted from apply-progress)
```text
$ go test -count=1 ./...
ok  	regixtry/cmd/regixtry	3.105s
ok  	regixtry/internal/app/auth	0.149s
ok  	regixtry/internal/app/regixtry	2.734s
ok  	regixtry/internal/app/scanning	0.058s
?   	regixtry/internal/domain/auth	[no test files]
ok  	regixtry/internal/domain/regixtry	0.013s
ok  	regixtry/internal/infra/auth/postgres	0.348s
ok  	regixtry/internal/infra/cliprogress	0.011s
ok  	regixtry/internal/infra/install/linux	0.452s
ok  	regixtry/internal/infra/install/releases	0.048s
ok  	regixtry/internal/infra/metadata/sqlite	0.402s
ok  	regixtry/internal/infra/release	0.013s
ok  	regixtry/internal/infra/scanning/gitleaks	0.277s
ok  	regixtry/internal/infra/scanning/trivy	0.274s
ok  	regixtry/internal/infra/storage/fsblob	0.015s
ok  	regixtry/internal/ports	0.007s
ok  	regixtry/internal/protocol/http	1.466s
ok  	regixtry/internal/tui	0.237s
exit code: 0
```

**Race-sensitive re-run** (the 3 packages this change actually touches), independently executed:
```text
$ go test -race -count=1 ./internal/app/regixtry/... ./internal/protocol/http/... ./internal/infra/metadata/sqlite/...
ok  	regixtry/internal/app/regixtry	5.318s
ok  	regixtry/internal/protocol/http	7.500s
ok  	regixtry/internal/infra/metadata/sqlite	1.604s
exit code: 0
```

**Pre-existing unrelated `-race` failure, independently reproduced on `develop`** (not part of this change's touched packages, confirmed NOT a regression): checked out a disposable `git worktree` at `develop` (`85a1e76`, the exact merge-base with `feature/scan-policy-gate`) and ran `go test -race -count=1 ./internal/tui/...` there. It fails identically — `TestModelTrivyRepositoryAlertsLoadSelectDetailAndRecoverEmptyState`, `TestModelScanHistoryModalHistoryNavigationRefetchesDetailAndSecretsPerCursor`, `TestModelScanHistoryModalRendersWithinViewportAcrossHeights`, `TestModelScanHistoryModalRendersWithinViewportAcrossWidths` — all racing in the scan-history-modal table rebuild path, unrelated to any code this change touches. Worktree removed after the check.

**Git history**: `git log --oneline develop..feature/scan-policy-gate` shows the exact 9 implementation commits plus 4 docs commits described in apply-progress (`ecb9fdf`..`87482a9`), in the claimed order. `git diff --stat develop...feature/scan-policy-gate` shows 27 files changed (21 in `internal/`, 6 openspec docs), 1954 authored insertions in `internal/` (`git diff --shortstat develop...feature/scan-policy-gate -- internal/`).

**Coverage**: no dedicated coverage tool run this pass (informational per the Strict TDD skill, not a hard gate); scenario-level test presence verified directly per row below.

### Spec Compliance Matrix

Actual spec counts, re-derived from the retrieved Engram spec artifact: **7 requirements** (5 in `vulnerability-policy-gate`, 2 ADDED in `operator-admin-tui`), **19 scenarios**.

| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Default Policy State On Fresh Install | Fresh install reports policy enabled at CRITICAL | `service_test.go` `TestServiceGetScanPolicySettingsDefaultsToEnabledCriticalWithNoRowWritten` + `store_test.go` `TestStoreGetScanPolicySettingsReturnsNotFoundWithNoRow` | COMPLIANT |
| Pull-Time Enforcement Is Fail-Open | Completed scan meeting threshold blocks the pull | `service_test.go` `TestServiceOpenManifestBlocksPullOnCompletedViolatingScan` (both threshold cases) | COMPLIANT |
| Pull-Time Enforcement Is Fail-Open | No scan yet allows the pull | `service_test.go` `TestServiceOpenManifestAllowsPullOnNonBlockingScanStates/"no scan run"` | COMPLIANT |
| Pull-Time Enforcement Is Fail-Open | Queued scan allows the pull | same test, `/"queued"` | COMPLIANT |
| Pull-Time Enforcement Is Fail-Open | Running scan allows the pull | same test, `/"running"` | COMPLIANT |
| Pull-Time Enforcement Is Fail-Open | Failed scan allows the pull | same test, `/"failed"` | COMPLIANT |
| Threshold Selection Changes Gate Outcomes | CRITICAL threshold allows a HIGH-only completed scan | `service_scanning_test.go` `TestScanPolicyViolated/"completed high-only at CRITICAL threshold allows"` | COMPLIANT |
| Threshold Selection Changes Gate Outcomes | CRITICAL+HIGH threshold blocks the same completed scan | same file, `/"completed high-only at CRITICAL_HIGH threshold blocks"` + `service_test.go` `TestServiceOpenManifestBlocksPullOnCompletedViolatingScan/"high-only at CRITICAL_HIGH threshold"` | COMPLIANT |
| Push Auto-Queues A Non-Blocking Scan | Push returns before the scan completes | `service_test.go` `TestServicePublishManifestReturnsBeforeScanCompletes` (uses a `blockingScanRunner` to prove non-blocking return) | COMPLIANT |
| Push Auto-Queues A Non-Blocking Scan | Push does not duplicate an in-flight scan | `service_test.go` `TestServiceQueuePushScanDoesNotDuplicateAnInFlightRun` | COMPLIANT |
| Registry-Scoped Scan-Status Is Readable Without Admin | Pull-scoped caller reads a scan verdict | `router_test.go` `TestRouterManifestScanStatusRequiresPullAuthorization/"pull-only credential succeeds"` | COMPLIANT |
| Registry-Scoped Scan-Status Is Readable Without Admin | Caller without pull authorization is rejected | same test, `/"no credential is rejected"` + `/"credential scoped to a different repository is refused"` | COMPLIANT |
| Policy Configuration Has Its Own Modal | Operator toggles policy enabled state | **none found** | **UNTESTED** |
| Policy Configuration Has Its Own Modal | Operator changes the severity threshold | **none found** | **UNTESTED** |
| Policy Configuration Has Its Own Modal | Policy modal is a separate surface from the Trivy config modal | `admin_views_test.go` `TestRenderScanPolicyModalIsASeparateSurfaceFromTrivyConfigModal` | COMPLIANT |
| Policy Configuration Has Its Own Modal | Policy modal stays within the terminal viewport | `admin_views_test.go` `TestRenderScanPolicyModalFitsWithinElevenRowBudget` + `TestRenderScanPolicyModalHeightIsIdenticalAcrossEveryFocusPosition` | COMPLIANT |
| Persistent Policy Status Badge | Badge reflects an enabled policy and its threshold | `admin_views_test.go` `TestRenderTrivyTabsPolicyBadgeTextReflectsStateAndUsesNoIconOrGlyph/"enabled critical"` + `/"enabled critical_high"` | COMPLIANT |
| Persistent Policy Status Badge | Badge reflects a disabled policy | same test, `/"disabled"` | COMPLIANT |
| Persistent Policy Status Badge | Badge uses text, not an icon or glyph | same test (asserts no rune > 126 in the rendered, ANSI-stripped output) | COMPLIANT |

**Compliance summary**: 17/19 scenarios compliant, 6/7 requirements fully covered; 1 requirement (Policy Configuration Has Its Own Modal) has 2 of its 4 scenarios UNTESTED.

### CRITICAL Finding — Untested Persist/Reflect Scenarios (Policy Modal)

Re-derived directly from current on-disk source, not trusted from apply-progress's self-description.

The production code for the policy-modal open/toggle/threshold-cycle/submit/persist/reflect round trip is real and correctly wired:
- `model.go:1064-1065` routes to `updateScanPolicyModalKey` while the modal is active.
- `model.go:1197` opens the modal (bound to `p`), seeding it from `m.adminView.ScanPolicy`.
- `model.go:1326-1347` (`updateScanPolicyModalKey`) handles Tab (cycle focus), Space (toggle enabled / cycle threshold), and Enter (`return m, m.updateScanPolicyCmd(...)`).
- `model.go:2349-2367` (`loadScanPolicyCmd` / `updateScanPolicyCmd`) call `m.adminClient.GetScanPolicy` / `UpdateScanPolicy`.
- `model.go:562-586` handle `adminScanPolicyLoadedMsg` / `adminScanPolicyUpdatedMsg`, writing the result back into `m.adminView.ScanPolicy` and closing the modal with a `"Vulnerability policy saved."` status.

However, **no test in the repository exercises this round trip at runtime**. Confirmed by exhaustive search:
- `grep -rn "adminScanPolicyUpdatedMsg\|adminScanPolicyLoadedMsg\|loadScanPolicyCmd\|updateScanPolicyCmd" internal/tui/*_test.go` → zero matches.
- The `fakeAdminClient`'s `GetScanPolicy`/`UpdateScanPolicy` methods (`model_test.go:2910-2923`) increment `getScanPolicyCalls`/`updateScanPolicyCalls`/`lastScanPolicyInput`, but **no test reads or asserts on any of these three fields** — they exist only to satisfy the `AdminClient` interface.
- No test presses `p` to open the scan policy modal (the sole `"p"` literal in `model_test.go` is an unrelated password string, not a key press).
- `session_test.go`'s `TestScanPolicyModalActiveReflectsOpenField` and `TestNextScanPolicyFieldCyclesBetweenTheTwoFields` are pure unit tests of the `scanPolicyModal` struct's `Active()`/`nextScanPolicyField` — they never construct a `Model`, never call `Update()`, and never touch the `AdminClient`.
- `admin_views_test.go`'s Phase 8 tests (`TestRenderScanPolicyModalFitsWithinElevenRowBudget`, `TestRenderScanPolicyModalHeightIsIdenticalAcrossEveryFocusPosition`, `TestRenderScanPolicyModalIsASeparateSurfaceFromTrivyConfigModal`) call `renderScanPolicyModal` directly with a hand-built `scanPolicyModal` value — they prove layout/row-budget correctness, not persistence.

This is a genuine, checkable gap against the project's own established convention for the analogous surface: `TestModelTrivyConfigModalOpenCancelAndSubmitCurrentSettingsOnly` (`model_test.go:934-988`) is the house pattern for exactly this kind of scenario — open via key, submit via Enter, assert on the fake client's call-count and last-input fields, assert on the post-submit confirmation text in `View()`. No analogous test exists for the scan policy modal.

`tasks.md`'s own Phase 8 plan (8.1-8.3, 8.7) only specified the `Active()`/field-cycling test, the row-budget/height-invariance test, and the separate-surface regression test — it never planned a `Model.Update()`-level integration test for the toggle-and-persist flow. This is a genuine test-plan gap that pre-dates apply (visible in `tasks.md` itself), not an apply-time deviation from a correctly-scoped plan.

Per the hard rule "A spec scenario is compliant only when a covering test passed at runtime," these two scenarios (`Operator toggles policy enabled state`, `Operator changes the severity threshold`) are **UNTESTED**, independent of code correctness observed by direct inspection.

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|---|---|---|
| Fail-open evaluator (`scanPolicyViolated`) | Implemented | `service_scanning.go:78-86`; pure function, 10-case table test (exceeds the 9 documented matrix rows by one — adds an explicit "policy disabled" case) |
| Pull gate insertion point (`enforceScanPolicy` in `OpenManifest`) | Implemented | `queries.go:71-91`; runs after `ResolveManifest`, before returning the manifest; `ResolveManifest` (queries.go:48-69, the TUI browse path) does not call it — confirmed no shared caller exists between the two |
| Push auto-queue settings/dedup (`queuePushScan`) | Implemented | `service_scanning.go:261-271`; gates on `settings.Enabled` only (not `ScheduleEnabled`); shares `dedupAndQueueScanRun` (mutex-guarded) with `QueueManualScan`/`queueScheduledScan` |
| Admin GET/PUT `/admin/v1/scan-policy` | Implemented | `admin_handlers.go:225-265`; unknown threshold rejected with `domainauth.NewValidationError` → 422 via `writeAdminError`'s `ErrorCodeValidation` mapping (`admin_handlers.go:836-837`), not 400 |
| Non-admin scan-status route (`/manifests/<ref>/scan-status`) | Implemented | `router.go:161-165` dispatches on the trimmed `reference`, not the raw suffix (collision-safe, regression-tested); `Action{Verb: ActionPull}` (`router.go:363`); 5-state response (`unscanned`/`in_progress`/`failed`/`clean`/`blocked`, `queries.go:123-127`) |
| `Service.WaitForBackgroundWork()` | Implemented, test-only | `service.go:92-94`; zero call sites outside `*_test.go` (`grep -rn WaitForBackgroundWork cmd/ main.go` → empty); cannot block production request handling |
| `rowid DESC` tiebreak (`GetLatestScanRunByDigest`) | Implemented | `store.go:616-631`; regression test `TestStoreGetLatestScanRunByDigestBreaksCreatedAtTiesByInsertOrder` |
| `CRITICAL+HIGH` display convention | Implemented, consistent | `scanPolicyThresholdLabel` (`admin_views.go:636-645`) renders `"CRITICAL+HIGH"`; the underscore form `critical_high` only ever appears as the internal wire/DB enum value (`ports.ScanPolicyThresholdCriticalHigh`), never as display text — confirmed no leftover `CRITICAL_HIGH` display string anywhere in `internal/tui` |
| 422-not-400 deviation | Implemented, consistent | Confirmed the only two `StatusBadRequest` occurrences in `router.go` (lines 610, 630) are pre-existing, unrelated OCI-digest/auth-validation paths, not scan-policy; the admin scan-policy PUT path exclusively maps `ErrorCodeValidation` → 422 |

### Coherence (Design)
| Decision | Followed? | Notes |
|---|---|---|
| Decision 1: dedicated `scan_policy_settings` table, code-level default (not migration/boot-path default) | Yes | `GetScanPolicySettings` maps NotFound → `{Enabled:true, SeverityThreshold:critical}` in Go, independent of any Ensure*/boot seed |
| Decision 2: `GetLatestScanRunByDigest` = `GetActiveScanRunByDigest` minus status filter; typed NotFound | Yes | Confirmed identical column list/decoder; `rowid DESC` tiebreak added per the design's own flagged Open Question |
| Decision 3: gate inlined in `OpenManifest` after resolution; `ResolveManifest` untouched | Yes | Confirmed by direct read: no shared caller path between the two; browse path (TUI `QueryService`) cannot inherit the gate |
| Decision 4: push auto-queue as a fire-and-forget goroutine, tenant pre-captured | Yes | `service.go:255-259`; tenant captured before `go func()`, `context.Background()` used inside, matching the documented `resolveManagedSecretScanSettings` precedent |
| Decision 5: Admin GET/PUT beside `scan-settings`; CI route nested under `manifests/` | Yes | Confirmed route dispatch and auth scope match exactly; 422 deviation from design's literal "400" text is explicitly documented and consistent |
| Decision 6: own 11-row modal; badge composed at zero row cost | Partially yes — **implementation matches, test coverage does not** | Row budget/height/separate-surface all test-confirmed; the modal's actual persist-and-reflect behavior (the design's stated purpose for the modal existing) has no runtime test — see CRITICAL finding above |

### TDD Compliance
| Check | Result | Details |
|-------|--------|---------|
| TDD Evidence reported | Yes | Full per-phase RED/GREEN table present in Engram `apply-progress` (obs #1004), covering all 10 phases |
| All tasks have tests | Partial | 60/60 tasks marked complete; Phase 8's own task list (8.1-8.3) never planned the persist/reflect integration test, so the gap is invisible at the tasks.md granularity |
| RED confirmed (tests exist) | Yes | All named test files exist and contain the claimed unit/render-level tests |
| GREEN confirmed (tests pass) | Yes | 0 failures across the full suite and the targeted `-race` re-run, independently executed this pass |
| Triangulation adequate | Yes for evaluator; No for modal persist flow | `scanPolicyViolated` has 10 distinct cases (exceeds the 9-row documented matrix); the modal's toggle/threshold-change scenarios have zero cases at the `Model.Update()` layer |
| Safety Net for modified files | Yes | Full suite green before and after this pass; no modified pre-existing test regressed |

**TDD Compliance**: 4/6 checks fully passed, 2 partial (both trace to the same Phase 8 gap)

### Assertion Quality
Sampled `service_scanning_test.go` (`TestScanPolicyViolated`), `service_test.go` (`TestServiceOpenManifestBlocksPullOnCompletedViolatingScan`, `TestServiceOpenManifestAllowsPullOnNonBlockingScanStates`, `TestServicePublishManifestReturnsBeforeScanCompletes`), `router_test.go` (`TestRouterManifestScanStatusReturnsAllFiveStates`, `TestRouterManifestScanStatusRouteCollisionWithTagNamedScanStatus`, `TestRouterManifestScanStatusRequiresPullAuthorization`), and `admin_views_test.go` (badge/modal render tests). No tautologies, no assertion-without-production-call patterns, no ghost loops over possibly-empty collections found. Assertions target real outcomes: exact error codes, exact HTTP status/body shapes, `blockingScanRunner`-proven non-blocking behavior, and byte-level rune scanning for the "text only" badge requirement.

**Assertion quality**: All sampled assertions verify real behavior.

### Issues Found

**CRITICAL**:
1. `Operator toggles policy enabled state` (operator-admin-tui spec) has no covering runtime test. The production code path (`p` key → modal → Space toggle → Enter → `UpdateScanPolicy` → reflect) exists and was read directly, but no test in the repository exercises it — `getScanPolicyCalls`/`updateScanPolicyCalls`/`lastScanPolicyInput` on the test fake are defined but never asserted on anywhere.
2. `Operator changes the severity threshold` (operator-admin-tui spec) has the identical gap — same missing test, same code path, different field.

**WARNING**:
1. Review workload: the session preflight recorded an 800-line budget with a pre-approved `size:exception` (2119 actual attempt-ledger lines vs. 1400 max). Independently re-measured this pass: `git diff --shortstat develop...feature/scan-policy-gate -- internal/` shows 1954 authored insertions in `internal/` (production + test code, excluding openspec planning docs). The exception was already accepted by the user before apply; noted here for archive-time traceability, not as a new blocker. Per apply-progress, this also requires a maintainer-run `gentle-ai sdd-attempt reset` before the SDD attempt ledger itself can close — an accounting step independent of code/test quality.
2. Working tree carries a pre-existing, unrelated `-race` failure in `internal/tui` (scan-history-modal table rebuild). Confirmed via a disposable worktree that it reproduces identically on `develop` at the exact merge-base commit, so it is not a regression from this change — but it remains an open, unrelated defect in the base branch that this change does not fix.
3. The apply-progress narrative describes the evaluator as a "10-case table" while the task instructions this pass described "9 documented cases" — both are accurate: the test file has 10 cases (9 matching the documented fail-open matrix rows plus one bonus "policy disabled" case). No discrepancy, just worth noting for anyone cross-referencing counts.

**SUGGESTION**: None.

### Verdict
**FAIL**

17/19 scenarios and 6/7 requirements are genuinely spec-compliant, independently re-derived from current on-disk source (not the apply report's self-description) and proven by passing tests, including the full fail-open evaluator matrix, the pull gate's 5-state fail-open behavior, push auto-queue settings-respect and dedup, the admin GET/PUT endpoint's 422 convention, the non-admin scan-status route's pull-scoped auth and 5-state response shape, and the `rowid DESC` tiebreak regression. `go build`/`go vet`/`gofmt -l`/`go test -count=1 ./...` are all green, independently re-run this pass, and the 3 touched packages are `-race`-clean; the one `-race` failure in `internal/tui` is confirmed pre-existing on `develop` via a disposable worktree check, not a regression. All 9 described implementation commits plus 4 docs commits genuinely exist with diffs matching their descriptions.

However, 2 of the 4 scenarios under "Policy Configuration Has Its Own Modal" — the two that describe the modal's entire reason for existing (persisting a toggle and a threshold change back through the admin API and reflecting it in the UI) — have zero runtime test coverage. The implementation code is real and, on direct inspection, appears correctly wired, but source inspection is explicitly not proof under this project's verification rules, and the project's own established convention (`TestModelTrivyConfigModalOpenCancelAndSubmitCurrentSettingsOnly` for the analogous Trivy config modal) shows this kind of test is both expected and straightforward to write with the existing `fakeAdminClient` scaffolding.

**Recommendation**: return to `sdd-apply` for a narrow, single-task remediation: add one `Model.Update()`-level test (e.g. `TestModelScanPolicyModalOpenToggleThresholdAndSubmitPersistsAndReflects`) that opens the modal via `p`, exercises Space-toggle and Space-cycle-threshold, submits via Enter, and asserts on `fakeAdminClient.updateScanPolicyCalls`, `lastScanPolicyInput`, and the post-submit `View()` state — mirroring the existing Trivy config modal test. This is a test-only addition; no production code change is indicated by this review. Once green, re-run `sdd-verify` before proceeding to `sdd-archive`.
