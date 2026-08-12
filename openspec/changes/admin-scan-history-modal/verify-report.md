```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:e291ee16825090d8cf8ba9dd91b0193bc11106ca27817114ea623debd0826e76
verdict: fail
blockers: 2
critical_findings: 2
requirements: 6/7
scenarios: 6/7
test_command: go test -count=1 ./...
test_exit_code: 0
test_output_hash: sha256:72e422411814e34495462d16b71f97b3b1d3de1fbb41b6c87d31308b81ad4b94
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: admin-scan-history-modal
**Version**: N/A
**Mode**: Strict TDD

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 24 |
| Tasks complete | 24 |
| Tasks incomplete | 0 |

### Build & Tests Execution
**Build**: ✅ Passed
```text
$ go build ./...
(no output, exit 0)
$ go vet ./...
(no output, exit 0)
$ gofmt -l .
(no output — no files need formatting)
```

**Tests**: ✅ All packages pass (fresh run, not trusted from prior reports)
```text
$ go test -count=1 ./...
ok  regixtry/cmd/regixtry            2.9s
ok  regixtry/internal/app/auth       0.17s
ok  regixtry/internal/app/regixtry   2.45s
ok  regixtry/internal/app/scanning   0.07s
ok  regixtry/internal/domain/regixtry 0.02s
ok  regixtry/internal/infra/auth/postgres 0.34s
ok  regixtry/internal/infra/install/linux 0.43s
ok  regixtry/internal/infra/install/releases 0.03s
ok  regixtry/internal/infra/metadata/sqlite 0.38s
ok  regixtry/internal/infra/release  0.02s
ok  regixtry/internal/infra/scanning/gitleaks 0.26s
ok  regixtry/internal/infra/scanning/trivy 0.26s
ok  regixtry/internal/infra/storage/fsblob 0.01s
ok  regixtry/internal/ports          0.01s
ok  regixtry/internal/protocol/http  1.26s
ok  regixtry/internal/tui            0.19s
exit code: 0
```

`bash docs/verification/scripts/tui-smoke.sh` — ✅ PASS: "TUI smoke verified snapshot launch and feature-manager action/help coverage."

Targeted re-run of every scan-history-modal test with `-v`: all 21 tests/subtests pass (`TestSummarizeScanRunsByRepository*` x3, `TestAdminScanHistoryRowSplit*` x2, `TestCycleIndex*` x3, `TestBuildAdminScanSummaryTable*`, `TestAdminScanHistoryModal*` x4, `TestRenderAdminScanHistoryModal*` x2, `TestModelRepositoryAlertsScreenShowsOneRowPerRepositoryNotPerScanRun`, `TestModelScanHistoryModal*` x4 (incl. 3 height subtests), `TestModelSecretFindingsSurfaceAlongsideVulnerabilityResultsWithoutSeverityOrGatingIndicator`).

**Coverage**: `internal/tui` 72.9% overall (informational, not a hard threshold). `adminScanHistoryModalTableBody` (the function containing the untested empty-Leaks branch, see CRITICAL-1) sits at 75.0% — the uncovered statements are exactly the "No tabs available" guard and the "No secret findings recorded for this execution." branch, corroborating the missing test independently of the manual trace.

### Spec Compliance Matrix

Actual spec.md counts (re-derived from the file, not the orchestrator's stated "8 scenarios"): **7 requirements** (1 REMOVED + 3 MODIFIED + 3 ADDED), **7 scenarios**.

| Requirement | Scenario | Test | Result |
|---|---|---|---|
| REMOVED: Same-Screen Vulnerability Drill-Down | (removal, no scenario) | Repo-wide grep for `TrivyAlertDetailOpen`, `TrivyScanRunDetail`, raw `SecretFindings` field, `isTrivyAlertsDetailOpen`, `trivyAlertDetailTableRoles`, `trivyDetailFixedLines`, `trivyScreenChromeLines`, `renderTrivyRepositoryAlerts` (impl), `buildAdminScanRunsTable` — zero hits outside openspec planning docs/comments | ✅ COMPLIANT |
| MODIFIED: Repository Alerts Summarized Per Repository With Ordering And Freshness | Repeated rescans collapse into one dated row | `admin_scan_history_test.go:48` `TestSummarizeScanRunsByRepositoryFallsBackToCreatedAtWithInProgressMarker` + `model_test.go:1055` `TestModelRepositoryAlertsScreenShowsOneRowPerRepositoryNotPerScanRun` | ✅ COMPLIANT |
| MODIFIED: Repository Alert Drill-Down Opens History Modal | Enter opens the modal; closing restores the list | `model_test.go:942-1021` `TestModelTrivyRepositoryAlertsLoadSelectDetailAndRecoverEmptyState` (enter→modal, esc→closed, `TrivySelectedAlert` reset, no "Scan History —" residue) | ✅ COMPLIANT |
| MODIFIED: Secret Findings Surface | Operator reviews secret findings for a navigated execution | `model_test.go:1392` `TestModelSecretFindingsSurfaceAlongsideVulnerabilityResultsWithoutSeverityOrGatingIndicator` | ✅ COMPLIANT |
| MODIFIED: Secret Findings Surface | No secret scan for the navigated execution stays visible | **none found** | ❌ UNTESTED |
| ADDED: Scan History Modal Viewport Containment | Modal fits at minimum height with full content | `model_test.go:1328` `TestModelScanHistoryModalRendersWithinViewportAcrossHeights` (heights 24/30/50, 40 findings + 40 secrets, both tabs) | ✅ COMPLIANT |
| ADDED: Per-Feature Modal Tabs | Operator cycles between tabs | `model_test.go:1130` `TestModelScanHistoryModalTabCyclesForwardAndBackwardWrapping` | ✅ COMPLIANT |
| ADDED: Scan Execution History Navigation | Position indicator shows one run's own findings | `model_test.go:1168` `TestModelScanHistoryModalHistoryNavigationRefetchesDetailAndSecretsPerCursor` + `model_test.go:1240` `TestModelScanHistoryModalDigestNeverLeaksAcrossRunsWhenPagingHistory` | ✅ COMPLIANT |

**Compliance summary**: 6/7 scenarios compliant, 1 UNTESTED. Requirement-level: 6/7 complete (Secret Findings Surface is incomplete — one of its two scenarios has no covering test).

### Deep-Dive: The Six Scrutiny Items Requested

1. **One row per repository, not per scan run** (`TestModelRepositoryAlertsScreenShowsOneRowPerRepositoryNotPerScanRun`, `model_test.go:1055`): genuinely proven. Sets up 4 scan runs — 3 for `team/api`, 1 for `library/base` — asserts `TotalRows()==2`, asserts `"team/api"` appears **exactly once** in the rendered view (not 3 times), and asserts the `Runs` column on that one row reads `3`. Non-tautological, realistic multi-run data.

2. **Nested-budget modal never overflows** (`TestModelScanHistoryModalRendersWithinViewportAcrossHeights`, `model_test.go:1328`; `TestRenderAdminScanHistoryModalNeverAppliesFitLinesOverComposite`, `admin_views_test.go:167`): both genuine.
   - The viewport test drives the **real key-press flow** (login→f→tab→enter) at heights 24/30/50 with 40 findings + 40 secrets (large, realistic), asserts `lipgloss.Height(view) <= height` on both the Vulnerabilities and Leaks tabs.
   - The composite test builds a 30-finding table with pageSize 5 (forcing 6 internal pages), asserts the output does **not** contain `"Showing "` (the outer-fitLines indicator that produced the historical orphan bug) while it **does** contain `"1/6"` (the table's own untouched pagination footer) — a positive assertion of the mechanism, not just a suggestively-named test.

3. **Digest isolation across history navigation** (`TestModelScanHistoryModalDigestNeverLeaksAcrossRunsWhenPagingHistory`, `model_test.go:1240`): genuine out-of-order async race, not a happy-path check. It captures the `Cmd` from Enter manually (does not use the auto-run `runKey` helper), resolves the history load, captures `detailCmdForA` without running it, pages Right (which fires a second `detailCmdForB`), and only *then* resolves the stale `detailCmdForA` response — asserting it is discarded (`Detail.Run.ID != "run-a"`, no chained secrets-load `Cmd`) and that the final state and rendered view reflect only `run-b`'s data (`CVE-B` present, `CVE-A` absent).

4. **Removal sweep completeness**: repo-wide grep (not just `internal/tui/`) for `TrivyAlertDetailOpen`, `TrivyScanRunDetail`, the old inline-detail `SecretFindings` field, `isTrivyAlertsDetailOpen`, `trivyAlertDetailTableRoles`, `trivyDetailFixedLines`, `trivyScreenChromeLines`, `renderTrivyRepositoryAlerts` (as an implementation, not a doc reference), `buildAdminScanRunsTable` — all zero hits outside `openspec/changes/**` planning docs and disposition comments. `session.go`'s `AdminViewState` has no raw `SecretFindings []ports.SecretFinding` field; only `Tables.SecretFindings bubbletable.Model` remains, which is the modal's rendering component (a legitimate survivor, distinct from the removed raw slice). Single Enter-key path confirmed in `model.go:1144-1160` (`updateAdminFeaturesKey`), with an explicit comment: "Enter opens the scan history modal, never the old inline detail — the two never fire together." Genuinely complete.

5. **Tab extensibility**: `adminScanHistoryTab{Kind, Label}` is an ordered `[]adminScanHistoryTab` slice (`session.go:142-153`), cycled via `cycleIndex(index, size)` keyed off `len(modal.Tabs)` at both call sites (`model.go:1252,1255`) — not a hardcoded binary toggle. Adding a third tab is one slice entry, confirmed structurally.

6. **Empty Leaks state**: ❌ **No test proves this.** The implementation exists and looks correct (`admin_views.go:298-301`: `case adminScanHistoryTabLeaks: if len(modal.Secrets) == 0 { return theme.muted.Render("No secret findings recorded for this execution.") }`), and the tab itself is structurally always present (it's a fixed slice entry, not conditionally added). But exhaustive search (`rg "No secret findings recorded"` across `internal/tui/`) finds the message only in the implementation, never asserted by any test. The two tests that exercise the Leaks tab (`TestModelSecretFindingsSurfaceAlongsideVulnerabilityResultsWithoutSeverityOrGatingIndicator`, `TestModelScanHistoryModalHistoryNavigationRefetchesDetailAndSecretsPerCursor`) both configure `secretScanFindings` map entries for every run in play, so the empty branch never executes in any test. Coverage data corroborates this: `adminScanHistoryModalTableBody` is only 75.0% covered, and the uncovered statements are precisely this branch and the sibling "No tabs available" guard.

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|---|---|---|
| Repository summary aggregation | ✅ Implemented | `summarizeScanRunsByRepository`, `formatScanSummaryLastExecuted` — both directly unit-tested |
| Modal nested-budget arithmetic | ✅ Implemented | `adminScanHistoryRowSplit`, property-tested across heights 10–80 with 7 measured base-inner-height values, invariant holds, non-negativity holds |
| Digest-per-cursor binding | ✅ Implemented | `adminScanHistoryDetailMatchesCursor`, `adminScanHistorySecretsMatchCursor` guard stale async responses |
| Leaks empty-state rendering | ⚠️ Implemented but unverified by test | Code path exists (`admin_views.go:299-301`); no test exercises it |
| Ordered tab cycling | ✅ Implemented | `cycleIndex` wraps both directions, table-driven + zero-size guard test |

### Coherence (Design)
| Decision | Followed? | Notes |
|---|---|---|
| Nested budget by row split, not overlay/stacking | ✅ Yes | `adminScanHistoryRowSplit` splits `outer.SectionRows` into `baseRows`+`modalRows`; invariant test matches design.md's formula exactly |
| No `fitLines` over a composite containing a bordered block | ✅ Yes | Directly tested (`TestRenderAdminScanHistoryModalNeverAppliesFitLinesOverComposite`); modal wrapped by `theme.section.Render` directly, table keeps its own pagination |
| App-layer summary over widened `ListScanRuns` window | ✅ Yes | `summarizeScanRunsByRepository` is pure, fed by `loadAdminScanRunsCmd`; no SQL/port change (confirmed: no diffs to `internal/infra/metadata/sqlite/store.go` or `internal/ports/regixtry.go` beyond what already existed) |
| Ordered tab slice with wrapping cursor | ✅ Yes | `newAdminScanHistoryTabs()`, `cycleIndex` |
| Keys gated in `updateAdminKey` before `updateAdminFeaturesKey` | ✅ Yes | `model.go:997-999`: `if m.adminView.ScanHistoryModal.Active() { return m.updateAdminScanHistoryModalKey(msg) }` precedes the screen-switch dispatch |

### TDD Compliance
| Check | Result | Details |
|---|---|---|
| TDD Evidence reported | ❌ | The retrievable Engram `apply-progress` artifact (topic-key upsert; only the latest of 5 revisions is retained, obs #985) contains no formal "TDD Cycle Evidence" table — it documents Phase 5's non-regression sign-off narratively, with only a passing reference to earlier phases ("Phases 1–4 unchanged from prior save"), which no longer resolves to distinct content because the upsert overwrote it. |
| All tasks have tests | ✅ | 24/24 tasks map to test files/functions confirmed present by direct inspection |
| RED confirmed (tests exist) | ✅ | All named test files/functions exist and were run directly |
| GREEN confirmed (tests pass) | ✅ | 0/0 failures across the full suite and the targeted scan-history-modal subset |
| Triangulation adequate | ✅ | Multi-case coverage confirmed for `summarizeScanRunsByRepository` (3 tests), `cycleIndex` (3 tests), `adminScanHistoryRowSplit` (2 tests incl. property test) |
| Safety Net for modified files | ➖ | Not independently verifiable from the current artifact; git history shows modified files (`model.go`, `session.go`, `admin_tables.go`, `admin_views.go`) had substantial pre-existing test suites that remained green throughout |

**TDD Compliance**: 4/6 checks pass without qualification.

**Process finding (WARNING, not blocking on its own)**: git commit chronology shows Phases 2, 3, and 4 each committed the `feat` (GREEN) commit before the corresponding `test` (RED) commit — e.g. `4fc0e3a` (feat, 11:17:54) before `4c5e3f4` (test, 11:18:00); `bc8a463` (feat, 12:06:21) before `99c0f87` (test, 12:06:26); `7abe09c` (feat, 13:33:15) before `f948cbd` (test, 13:33:20). Each test commit message honestly self-discloses this ("This coverage was written after the corresponding production code... Genuineness was verified retroactively instead: with the production changes stashed, this exact test suite fails to compile/panics against the pre-phase baseline; restoring makes it pass") and describes the stash/verify/restore technique used to cross-check genuineness. Only Phase 1 (`317b0db` test before `1c9c3b3` feat) followed literal RED-before-GREEN commit ordering. This is a real, consistent deviation from Strict TDD's letter across 3 of 4 phases — honestly disclosed and independently cross-verified each time, not a fabricated GREEN.

---

### Test Layer Distribution
| Layer | Tests | Files | Tools |
|---|---|---|---|
| Unit | 9 | `admin_scan_history_test.go` | Go `testing` |
| Integration (Model.Update()/View() key-press flow) | 12 | `model_test.go`, `admin_views_test.go`, `admin_tables_test.go` | Go `testing`, `lipgloss.Height` |
| E2E (smoke) | 1 | `docs/verification/scripts/tui-smoke.sh` | shell + `go test` |
| **Total** | **22** | **5** | |

---

### Changed File Coverage
| File | Line % | Rating |
|---|---|---|
| `internal/tui/admin_scan_history.go` | 92.9% avg (individual funcs 75–100%) | ✅ Excellent |
| `internal/tui/admin_tables.go` (scan-summary additions) | 100% on new build/format functions | ✅ Excellent |
| `internal/tui/admin_views.go` (modal render additions) | 75–100% across new functions; `adminScanHistoryModalTableBody` at 75.0% | ⚠️ Acceptable (uncovered branch is CRITICAL-1) |
| `internal/tui/model.go` (`updateAdminScanHistoryModalKey`, `pageAdminScanHistory`) | 91.7%, 85.7% | ✅ Excellent |

**Average changed file coverage**: `internal/tui` package overall 72.9% (includes long-standing lower-coverage legacy admin flows unrelated to this change — not representative of this change's own files, which are 75–100%).

---

### Assertion Quality
No tautologies, no assertion-without-production-call patterns, and no unguarded ghost loops found in the sampled highest-risk files (`admin_scan_history_test.go`, `admin_views_test.go`, and the scan-history-modal tests in `model_test.go`). The one `for range` loop over query results (`model_test.go:1099`) is guarded by a post-loop `found` check that fails the test if the target row is absent, so it is not a vacuous ghost loop.

**Assertion quality**: ✅ No CRITICAL or WARNING issues found in sampled files (not an exhaustive scan of every test in the package)

---

### Quality Metrics
**Linter**: ➖ No dedicated linter configured/detected beyond `go vet` (clean, 0 issues)
**Type Checker**: N/A (Go compiler is the type checker; `go build ./...` clean)

### Issues Found

**CRITICAL**:
1. Spec scenario "No secret scan for the navigated execution stays visible" (MODIFIED requirement "Secret Findings Surface") has **no covering test**. The implementation (`admin_views.go:299-301`) appears correct and the tab is structurally always present, but nothing asserts the empty-state message renders or that the tab stays visible/selectable when `modal.Secrets` is empty. Coverage data corroborates: `adminScanHistoryModalTableBody` sits at 75.0%, missing exactly this branch.
2. The retrievable Engram `apply-progress` artifact (`sdd/admin-scan-history-modal/apply-progress`) contains no formal "TDD Cycle Evidence" table, as Strict TDD Mode requires apply to report. The topic-key upsert design retained only the latest (Phase 5) revision's content; earlier phase-specific RED/GREEN/TRIANGULATE/SAFETY-NET detail is no longer retrievable, even though equivalent evidence exists in commit messages (independently cross-checked in this pass).

**WARNING**:
1. Commit chronology shows Phases 2, 3, and 4 each committed `feat` (GREEN) before the paired `test` (RED) commit, honestly self-disclosed in each test commit message as "retroactive RED" with a stash/verify/restore genuineness check. This is a real deviation from literal RED-before-GREEN Strict TDD ordering across 3 of 4 phases, though not a fabricated-green case — every test commit documents how genuineness was independently confirmed.
2. `tasks.md`'s Review Workload Forecast is internally inconsistent: "400-line budget risk: High" and "Chained PRs recommended: Yes" alongside "Delivery strategy: single-pr" and "Chain strategy: pending" — the actual diff for this change is ~2,150 changed lines (1,747 insertions + 404 deletions from the prior change's tip), well over the 400-line reviewer budget, with no resolved chaining decision recorded. This does not affect functional correctness but is unresolved delivery-strategy guidance the orchestrator should reconcile before archive/PR.

**SUGGESTION**: None.

### Verdict
**FAIL**
Two CRITICAL findings block a clean pass: one genuine spec-scenario compliance gap (empty Leaks state is implemented but never tested — a real regression risk on the requirement explicitly designed as "never hidden, so the tab set doesn't flicker while paging history"), and one Strict TDD process-evidence gap (no retrievable TDD Cycle Evidence table for this change). Everything else — the two most safety-critical claims (nested-budget viewport containment and digest isolation), the original user-facing bug fix (one row per repository), the full removal sweep, and tab extensibility — is genuinely proven by non-tautological tests I read and re-ran myself, and the full build/vet/gofmt/test/smoke suite is green.
