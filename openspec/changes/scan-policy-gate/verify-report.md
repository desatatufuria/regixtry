```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:d0ffdcdbe6eb26a3e7219f8ac963c6707d86af671ef038cac724f56f26bd4b8a
verdict: pass
blockers: 0
critical_findings: 0
requirements: 7/7
scenarios: 19/19
test_command: go test -count=1 ./...
test_exit_code: 0
test_output_hash: sha256:32fde9cc2d605fcf6121599b5d369157f002b94b3d3a5753bc6e7cb1de809d38
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report — Re-verification

**Change**: scan-policy-gate
**Version**: N/A
**Mode**: Strict TDD
**Prior pass**: FAIL (17/19 scenarios, 6/7 requirements) — see `sdd/scan-policy-gate/verify-report` obs #1005 / this file's git history for the original report. This is a from-scratch re-verification, not a rubber stamp of the remediation commit's own claims.

### What changed since the FAIL

Commit `1319b49` (`test(scan-policy): add coverage for the policy modal persist/reflect round trip`) — test-only, 2 files, 68 insertions:
- `internal/tui/model_test.go`: adds `TestModelScanPolicyModalOpenToggleSubmitPersistsAndReflectsCurrentSettings`.
- `openspec/changes/scan-policy-gate/tasks.md`: adds checked task 8.8 documenting the remediation.

No production code changed in this commit. `git show 1319b49 --stat` confirms the exact 2-file, 68-insertion diff.

### Independent Source Verification of the New Test

Read `internal/tui/model_test.go` directly (not trusted from the task description). Findings:

1. **Genuine `Model.Update()` path, not a shortcut.** The test uses `newAdminReadyModel` → `runAdminLogin` → `runKey(t, updated, "f")` / `"p"` / `" "` / `"tab"` / `"enter"`. `runKey` (line 3180) constructs a real `tea.KeyMsg` and calls `model.Update(msg)` — the exact same dispatch path a live terminal session uses. `runAdminLogin` (line 3171) itself chains `runKey` calls for tab/username/tab/password/enter — no internal helper bypasses `Update()`. This is the identical helper pattern used by the already-verified `TestModelTrivyConfigModalOpenCancelAndSubmitCurrentSettingsOnly`, the house convention the prior verify pass named as the expected model.
2. **Real persisted-value assertions, not "no error" checks.** After the toggle+cycle+submit sequence the test asserts:
   - `adminClient.updateScanPolicyCalls == 1` (call actually reached the fake `AdminClient`)
   - `adminClient.lastScanPolicyInput == ports.ScanPolicySettings{Enabled: false, SeverityThreshold: ports.ScanPolicyThresholdCriticalHigh}` (exact struct equality on the value sent to the backend)
   - `submitted.adminView.ScanPolicy == ports.ScanPolicySettings{Enabled: false, SeverityThreshold: ports.ScanPolicyThresholdCriticalHigh}` (exact struct equality on the value reflected back into the model after submit)
   - `!submitted.adminView.ScanPolicyModal.Active()` (modal closes on submit)
   - `submitted.View()` contains `"Vulnerability policy saved"` (user-visible confirmation text)
   These are concrete, falsifiable value assertions — not tautologies or presence-only checks.
3. **Covers both required scenarios in one round trip.** The test performs `runKey(updated, " ")` to toggle `Enabled` (initial `true` → asserted final `false`), then `runKey(updated, "tab")` + `runKey(updated, " ")` to cycle `SeverityThreshold` from the seeded `CRITICAL` to `CRITICAL+HIGH`, then submits once via `enter`. Both spec scenarios ("Operator toggles policy enabled state" and "Operator changes the severity threshold") are exercised and independently asserted in the same test — the spec does not require separate test functions per scenario, only that each scenario has a covering runtime test, which this satisfies for both.
4. **Pre-submit state is also verified.** The test asserts `adminClient.getScanPolicyCalls == 1` and `updated.adminView.ScanPolicy` equals the seeded settings immediately after login (before the modal opens), and separately asserts the opened modal's `View()` contains `"Enabled"`, `"Severity Threshold"`, `"CRITICAL"` — proving the modal is seeded from the real backend read, not a hardcoded fixture.

Verdict: this is a genuine, non-vacuous integration test that closes the previously-identified gap.

### Build & Tests Execution (independently re-run this pass)

**Build**: PASSED
```text
$ go build ./...
(no output, exit 0)
$ go vet ./...
(no output, exit 0)
$ gofmt -l .
(no output — no files need formatting)
```

**Tests**: All 17 packages pass, freshly run at HEAD `1319b49`
```text
$ go test -count=1 ./...
ok  	regixtry/cmd/regixtry	3.194s
ok  	regixtry/internal/app/auth	0.153s
ok  	regixtry/internal/app/regixtry	2.817s
ok  	regixtry/internal/app/scanning	0.054s
?   	regixtry/internal/domain/auth	[no test files]
ok  	regixtry/internal/domain/regixtry	0.015s
ok  	regixtry/internal/infra/auth/postgres	0.361s
ok  	regixtry/internal/infra/cliprogress	0.016s
ok  	regixtry/internal/infra/install/linux	0.466s
ok  	regixtry/internal/infra/install/releases	0.043s
ok  	regixtry/internal/infra/metadata/sqlite	0.431s
ok  	regixtry/internal/infra/release	0.014s
ok  	regixtry/internal/infra/scanning/gitleaks	0.296s
ok  	regixtry/internal/infra/scanning/trivy	0.277s
ok  	regixtry/internal/infra/storage/fsblob	0.012s
ok  	regixtry/internal/ports	0.017s
ok  	regixtry/internal/protocol/http	1.512s
ok  	regixtry/internal/tui	0.248s
exit code: 0
```

**New test isolated run** (confirms it passes standalone, not only inside the full suite):
```text
$ go test -run TestModelScanPolicyModalOpenToggleSubmitPersistsAndReflectsCurrentSettings -v ./internal/tui/...
--- PASS: TestModelScanPolicyModalOpenToggleSubmitPersistsAndReflectsCurrentSettings (0.02s)
PASS
ok  	regixtry/internal/tui	(cached)
```

**New test isolated `-race` run** (race-clean on its own):
```text
$ go test -race -run TestModelScanPolicyModalOpenToggleSubmitPersistsAndReflectsCurrentSettings -count=1 -v ./internal/tui/...
--- PASS: TestModelScanPolicyModalOpenToggleSubmitPersistsAndReflectsCurrentSettings (0.09s)
PASS
ok  	regixtry/internal/tui	1.374s
```

**Full-package `-race` run of `./internal/tui/...`** (broader scope than the prior verify pass, which only race-tested 3 non-tui packages plus a filtered 4-test check on `develop`):
```text
$ go test -race -count=1 ./internal/tui/...
FAIL	regixtry/internal/tui	1.557s   (151 data races reported, ~90 tests marked FAIL)
```
Every reported race stack traces to `github.com/evertras/bubble-table/table.NewRow` (`row.go:36`) via `buildAdminFeaturesTable`/`rebuildAdminTables` (`internal/tui/admin_tables.go:148,381`) — the same pre-existing shared-state hazard the prior verify pass identified, not a new one. Because most `internal/tui` tests build admin tables and run with `t.Parallel()`, this one upstream-library race cascades broadly once the detector fires; this is a scheduling/detector artifact of running the full package, not ~90 independent defects.

**Independently reproduced on `develop` this pass, at the exact same merge-base (`85a1e76`) used previously**, via a disposable `git worktree` (removed after the check):
```text
$ go test -race -count=1 ./internal/tui/...   # on develop @ 85a1e76
FAIL	regixtry/internal/tui	1.484s   (154 data races reported, 86 tests marked FAIL)
```
`develop` shows an equal-or-larger blast radius (86 failing tests / 154 races vs. 151 races on the feature branch) for the identical command against the identical root cause. This confirms, more rigorously than the prior pass's 4-test spot check, that the race is pre-existing in the base branch and not worsened by `scan-policy-gate`. The new scan-policy test itself is not a contributor — its isolated `-race` run above is clean.

**Git history**: `git log --oneline develop..feature/scan-policy-gate` shows the 9 implementation commits, 5 docs/verify commits (including `18c6429` recording the prior FAIL and `1319b49` the remediation), in order. `git show 1319b49 --stat` confirms the remediation is test-only (2 files, 68 insertions, 0 deletions, 0 production files touched).

### Spec Compliance Matrix (re-derived from current on-disk specs)

**7 requirements** (5 in `vulnerability-policy-gate`, 2 ADDED in `operator-admin-tui`), **19 scenarios** — counts unchanged from the prior pass, confirmed by re-reading both spec files in full this pass.

All 17 previously-COMPLIANT rows are unchanged (implementation and tests untouched by the remediation commit) and were re-confirmed passing in this pass's full suite run. The 2 previously-UNTESTED rows are now:

| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Policy Configuration Has Its Own Modal | Operator toggles policy enabled state | `model_test.go` `TestModelScanPolicyModalOpenToggleSubmitPersistsAndReflectsCurrentSettings` (Space-toggle on `Enabled`, asserts `lastScanPolicyInput.Enabled == false` and `adminView.ScanPolicy.Enabled == false` post-submit) | COMPLIANT |
| Policy Configuration Has Its Own Modal | Operator changes the severity threshold | same test (Tab+Space cycles `SeverityThreshold` to `CriticalHigh`, asserts `lastScanPolicyInput.SeverityThreshold == CriticalHigh` and `adminView.ScanPolicy.SeverityThreshold == CriticalHigh` post-submit) | COMPLIANT |

**Compliance summary**: 19/19 scenarios compliant, 7/7 requirements fully covered.

### Correctness (Static Evidence)

Unchanged from the prior pass — no production code changed by the remediation commit. All 8 previously-confirmed rows (fail-open evaluator, pull gate insertion point, push auto-queue dedup, admin GET/PUT 422 convention, non-admin scan-status route, `WaitForBackgroundWork` test-only isolation, `rowid DESC` tiebreak, `CRITICAL+HIGH` display convention) stand as independently re-derived in the prior pass and re-confirmed green this pass via the full suite re-run.

### Coherence (Design)

Decision 6 ("own 11-row modal; badge composed at zero row cost") is now **fully Yes** — the prior pass's "Partially yes" (implementation matched, test coverage did not) is resolved: the modal's persist-and-reflect behavior, the design's stated purpose for the modal existing, now has runtime test coverage. All other 5 design decisions were already fully confirmed in the prior pass and remain unchanged (no production code touched).

### TDD Compliance

| Check | Result | Details |
|-------|--------|---------|
| TDD Evidence reported | Yes | Task 8.8 documents RED/GREEN for the remediation test; full per-phase evidence for Phases 1-8 remains in Engram `apply-progress` (obs #1004) |
| All tasks have tests | Yes | 8.8 closes the exact gap the prior pass identified at the tasks.md granularity |
| RED confirmed (tests exist) | Yes | `TestModelScanPolicyModalOpenToggleSubmitPersistsAndReflectsCurrentSettings` exists and was read in full this pass |
| GREEN confirmed (tests pass) | Yes | 0 failures across the full suite, the isolated run, and the isolated `-race` run, all independently executed this pass |
| Triangulation adequate | Yes | Both remaining scenarios (toggle Enabled, cycle SeverityThreshold) now have explicit, distinct value assertions within the one integration test |
| Safety Net for modified files | Yes | Full suite green before and after; no pre-existing test regressed |

**TDD Compliance**: 6/6 checks fully passed.

### Assertion Quality

Re-sampled the new test plus the previously-sampled files (`service_scanning_test.go`, `service_test.go`, `router_test.go`, `admin_views_test.go`). No tautologies, no assertion-without-production-call patterns. The new test's assertions target exact struct-equality values (`ports.ScanPolicySettings{...}`) and exact call counts, not presence-only or error-only checks.

**Assertion quality**: All sampled assertions verify real behavior.

### Issues Found

**CRITICAL**: None.

**WARNING**:
1. The `-race` blast radius in `internal/tui` is wider than the prior pass characterized (this pass ran the full package rather than a name-filtered subset). Re-confirmed pre-existing and not a regression via a fresh `develop`-worktree check this pass (86 failing tests / 154 races on `develop` vs. 151 races on the feature branch, same root cause: `buildAdminFeaturesTable` via `evertras/bubble-table`). This remains an open, unrelated defect in the base branch that this change does not fix and is not expected to fix.
2. Review workload / attempt-ledger accounting note carries over unchanged from the prior pass: the session preflight recorded a pre-accepted `size:exception`, and a maintainer-run `gentle-ai sdd-attempt reset` remains required before the SDD attempt ledger can close — independent of code/test quality, noted here for archive-time traceability only.

**SUGGESTION**: None.

### Verdict

**PASS**

19/19 scenarios and 7/7 requirements are genuinely spec-compliant, independently re-derived from current on-disk source (not the remediation commit's self-description) and proven by passing tests. `go build`/`go vet`/`gofmt -l`/`go test -count=1 ./...` are all green, independently re-run this pass. The remediation test was read in full, confirmed to exercise the real `Model.Update()` key-handling path (not an internal-helper shortcut), confirmed to assert exact persisted/reflected struct values (not just absence of error), and confirmed to cover both previously-missing scenarios (Enabled toggle, SeverityThreshold cycle) in one round trip. The new test is independently `-race`-clean in isolation. The pre-existing, unrelated `internal/tui` `-race` failure (bubble-table row-counter race) was re-confirmed this pass, at wider scope than before, to reproduce equally or worse on `develop` at the same merge-base — not a regression, not caused by this change, not required to be fixed by this change.

**Recommendation**: proceed to `sdd-archive`.
