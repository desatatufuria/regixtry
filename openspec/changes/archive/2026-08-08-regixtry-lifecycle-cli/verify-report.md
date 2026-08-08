```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:2922ee95d4843d58bc634a070949db41432f6052464d4bc7bc5c766db1c01dc1
verdict: pass
blockers: 0
critical_findings: 0
requirements: 7/7
scenarios: 14/14
test_command: GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go test ./... -count=1
test_exit_code: 0
test_output_hash: sha256:754bc042eb0ce348492e0f5cc27c89b3bdd3a1743b3506935d1b8e362fd8daa0
build_command: GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: regixtry-lifecycle-cli
**Version**: N/A
**Mode**: Standard

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 13 |
| Tasks complete | 13 |
| Tasks incomplete | 0 |

### Build & Tests Execution
**Formatting**: ✅ Passed
```text
$ gofmt -l .
(no output)
```

**Build**: ✅ Passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go build ./...
(no output)
```

**Focused runtime evidence — CLI lifecycle commands**: ✅ Passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go test ./cmd/regixtry -count=1 -v -run 'TestRunSetupRequiresModeWithoutTTY|TestRunSetupInteractivePromptSupportsBinaryOnly|TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance|TestRunSetupRollsBackWhenProvenanceSaveFails|TestRunUninstallUsesProvenanceStatePathAndPrintsReport|TestRunUpgradeReportsDeferredLifecycleStatus|TestRunBootstrapPropagatesHostAndRuntimeFailures'
=== RUN   TestRunSetupRequiresModeWithoutTTY
--- PASS: TestRunSetupRequiresModeWithoutTTY (0.00s)
=== RUN   TestRunSetupInteractivePromptSupportsBinaryOnly
--- PASS: TestRunSetupInteractivePromptSupportsBinaryOnly (0.00s)
=== RUN   TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance
--- PASS: TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance (0.00s)
=== RUN   TestRunSetupRollsBackWhenProvenanceSaveFails
--- PASS: TestRunSetupRollsBackWhenProvenanceSaveFails (0.00s)
=== RUN   TestRunUninstallUsesProvenanceStatePathAndPrintsReport
--- PASS: TestRunUninstallUsesProvenanceStatePathAndPrintsReport (0.00s)
=== RUN   TestRunUpgradeReportsDeferredLifecycleStatus
--- PASS: TestRunUpgradeReportsDeferredLifecycleStatus (0.00s)
=== RUN   TestRunBootstrapPropagatesHostAndRuntimeFailures
=== RUN   TestRunBootstrapPropagatesHostAndRuntimeFailures/unsupported_os_release
=== RUN   TestRunBootstrapPropagatesHostAndRuntimeFailures/missing_systemd
=== RUN   TestRunBootstrapPropagatesHostAndRuntimeFailures/systemctl_enable_failure
=== RUN   TestRunBootstrapPropagatesHostAndRuntimeFailures/probe_failure
=== RUN   TestRunBootstrapPropagatesHostAndRuntimeFailures/occupied_local_bind_recovery_guidance
--- PASS: TestRunBootstrapPropagatesHostAndRuntimeFailures (0.00s)
    --- PASS: TestRunBootstrapPropagatesHostAndRuntimeFailures/unsupported_os_release (0.00s)
    --- PASS: TestRunBootstrapPropagatesHostAndRuntimeFailures/missing_systemd (0.00s)
    --- PASS: TestRunBootstrapPropagatesHostAndRuntimeFailures/systemctl_enable_failure (0.00s)
    --- PASS: TestRunBootstrapPropagatesHostAndRuntimeFailures/probe_failure (0.00s)
    --- PASS: TestRunBootstrapPropagatesHostAndRuntimeFailures/occupied_local_bind_recovery_guidance (0.00s)
PASS
ok  	regixtry/cmd/regixtry	0.009s
```

**Focused runtime evidence — Linux bootstrap and uninstall**: ✅ Passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go test ./internal/infra/install/linux -count=1 -v -run 'TestBootstrapRunWritesArtifactsAndRollsBackOnProbeFailure|TestBootstrapRunRejectsUnsupportedMode|TestBootstrapRunRejectsUnsupportedHost|TestBootstrapPlanEmitsLifecycleProvenance|TestBootstrapRunReportsSuccessAfterActivationAndReadiness|TestBootstrapperUninstallReportsDriftTruthfully|TestUninstallCleanupTargetsRemovesInstalledBinaryLast|TestBootstrapperUninstallReturnsFailuresInReport|TestBootstrapRunSkipsLocalPreflightForNonLocalBind|TestBootstrapRunFailsBeforeServiceStartWhenLocalBindIsOccupied'
=== RUN   TestBootstrapRunWritesArtifactsAndRollsBackOnProbeFailure
=== PAUSE TestBootstrapRunWritesArtifactsAndRollsBackOnProbeFailure
=== RUN   TestBootstrapRunRejectsUnsupportedMode
=== PAUSE TestBootstrapRunRejectsUnsupportedMode
=== RUN   TestBootstrapRunRejectsUnsupportedHost
=== PAUSE TestBootstrapRunRejectsUnsupportedHost
=== RUN   TestBootstrapPlanEmitsLifecycleProvenance
=== PAUSE TestBootstrapPlanEmitsLifecycleProvenance
=== RUN   TestBootstrapRunReportsSuccessAfterActivationAndReadiness
=== PAUSE TestBootstrapRunReportsSuccessAfterActivationAndReadiness
=== RUN   TestBootstrapRunFailsBeforeServiceStartWhenLocalBindIsOccupied
=== PAUSE TestBootstrapRunFailsBeforeServiceStartWhenLocalBindIsOccupied
=== RUN   TestBootstrapRunSkipsLocalPreflightForNonLocalBind
=== PAUSE TestBootstrapRunSkipsLocalPreflightForNonLocalBind
=== RUN   TestBootstrapperUninstallReportsDriftTruthfully
=== PAUSE TestBootstrapperUninstallReportsDriftTruthfully
=== RUN   TestUninstallCleanupTargetsRemovesInstalledBinaryLast
=== PAUSE TestUninstallCleanupTargetsRemovesInstalledBinaryLast
=== RUN   TestBootstrapperUninstallReturnsFailuresInReport
=== PAUSE TestBootstrapperUninstallReturnsFailuresInReport
=== CONT  TestBootstrapRunWritesArtifactsAndRollsBackOnProbeFailure
=== CONT  TestBootstrapRunFailsBeforeServiceStartWhenLocalBindIsOccupied
=== CONT  TestUninstallCleanupTargetsRemovesInstalledBinaryLast
--- PASS: TestUninstallCleanupTargetsRemovesInstalledBinaryLast (0.00s)
=== CONT  TestBootstrapperUninstallReturnsFailuresInReport
=== CONT  TestBootstrapRunSkipsLocalPreflightForNonLocalBind
=== CONT  TestBootstrapperUninstallReportsDriftTruthfully
=== CONT  TestBootstrapPlanEmitsLifecycleProvenance
=== CONT  TestBootstrapRunRejectsUnsupportedHost
--- PASS: TestBootstrapPlanEmitsLifecycleProvenance (0.00s)
--- PASS: TestBootstrapRunRejectsUnsupportedHost (0.00s)
=== CONT  TestBootstrapRunReportsSuccessAfterActivationAndReadiness
=== CONT  TestBootstrapRunRejectsUnsupportedMode
--- PASS: TestBootstrapRunRejectsUnsupportedMode (0.00s)
--- PASS: TestBootstrapperUninstallReturnsFailuresInReport (0.00s)
--- PASS: TestBootstrapRunSkipsLocalPreflightForNonLocalBind (0.00s)
--- PASS: TestBootstrapperUninstallReportsDriftTruthfully (0.00s)
--- PASS: TestBootstrapRunReportsSuccessAfterActivationAndReadiness (0.00s)
--- PASS: TestBootstrapRunFailsBeforeServiceStartWhenLocalBindIsOccupied (0.00s)
--- PASS: TestBootstrapRunWritesArtifactsAndRollsBackOnProbeFailure (0.01s)
PASS
ok  	regixtry/internal/infra/install/linux	0.015s
```

**Focused runtime evidence — installer smoke harness**: ✅ Passed
```text
$ bash docs/verification/scripts/install-release-smoke.sh
Installer release smoke scenarios passed. Root: /tmp/regixtry-install-smoke.fVwXTs
```

**Tests**: ✅ Full Go suite passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go test ./... -count=1
ok  	regixtry/cmd/regixtry	1.488s
ok  	regixtry/internal/app/auth	0.130s
ok  	regixtry/internal/app/regixtry	0.351s
?   	regixtry/internal/domain/auth	[no test files]
ok  	regixtry/internal/domain/regixtry	0.005s
ok  	regixtry/internal/infra/auth/postgres	0.269s
ok  	regixtry/internal/infra/install/linux	0.020s
ok  	regixtry/internal/infra/metadata/sqlite	0.176s
ok  	regixtry/internal/infra/storage/fsblob	0.017s
ok  	regixtry/internal/ports	0.015s
ok  	regixtry/internal/protocol/http	1.033s
ok  	regixtry/internal/tui	0.021s
```

**Coverage**: Per-package coverage reported / threshold: 0% → ✅ Above
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go test -cover ./... -count=1
ok  	regixtry/cmd/regixtry	1.482s	coverage: 75.9% of statements
ok  	regixtry/internal/app/auth	0.162s	coverage: 30.3% of statements
ok  	regixtry/internal/app/regixtry	0.348s	coverage: 60.0% of statements
	regixtry/internal/domain/auth		coverage: 0.0% of statements
ok  	regixtry/internal/domain/regixtry	0.017s	coverage: 64.0% of statements
ok  	regixtry/internal/infra/auth/postgres	0.287s	coverage: 57.7% of statements
ok  	regixtry/internal/infra/install/linux	0.042s	coverage: 69.7% of statements
ok  	regixtry/internal/infra/metadata/sqlite	0.184s	coverage: 58.3% of statements
ok  	regixtry/internal/infra/storage/fsblob	0.031s	coverage: 61.7% of statements
ok  	regixtry/internal/ports	0.013s	coverage: 60.0% of statements
ok  	regixtry/internal/protocol/http	1.048s	coverage: 71.5% of statements
ok  	regixtry/internal/tui	0.013s	coverage: 72.2% of statements
```

**Static analysis**: ✅ Passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go vet ./...
(no output)
```

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| Binary Lifecycle Entrypoints | Setup command is available | `cmd/regixtry/main_test.go > TestRunSetupInteractivePromptSupportsBinaryOnly`; `docs/verification/scripts/install-release-smoke.sh > run_lifecycle_cases` | ✅ COMPLIANT |
| Binary Lifecycle Entrypoints | Deferred lifecycle command is requested | `cmd/regixtry/main_test.go > TestRunUpgradeReportsDeferredLifecycleStatus`; `docs/verification/scripts/install-release-smoke.sh > run_lifecycle_cases` | ✅ COMPLIANT |
| Setup Outcome Contract | Setup reaches phase-1 success | `cmd/regixtry/main_test.go > TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance`; `internal/infra/install/linux/bootstrap_test.go > TestBootstrapRunReportsSuccessAfterActivationAndReadiness` | ✅ COMPLIANT |
| Setup Outcome Contract | Setup stays failed on partial completion | `internal/infra/install/linux/bootstrap_test.go > TestBootstrapRunWritesArtifactsAndRollsBackOnProbeFailure`; `cmd/regixtry/main_test.go > TestRunBootstrapPropagatesHostAndRuntimeFailures/probe_failure` | ✅ COMPLIANT |
| Provenance-Driven Uninstall | Recorded install is removed | `cmd/regixtry/main_test.go > TestRunUninstallUsesProvenanceStatePathAndPrintsReport`; `internal/infra/install/linux/provenance_test.go > TestBootstrapperUninstallReportsDriftTruthfully`; `docs/verification/scripts/install-release-smoke.sh > run_lifecycle_cases` | ✅ COMPLIANT |
| Provenance-Driven Uninstall | Drifted host is handled truthfully | `internal/infra/install/linux/provenance_test.go > TestBootstrapperUninstallReportsDriftTruthfully`; `internal/infra/install/linux/provenance_test.go > TestBootstrapperUninstallReturnsFailuresInReport`; `docs/verification/scripts/install-release-smoke.sh > run_lifecycle_cases` | ✅ COMPLIANT |
| Truthful Mode Contract | Supported lifecycle target is selected | `cmd/regixtry/main_test.go > TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance`; `internal/infra/install/linux/bootstrap_test.go > TestBootstrapRunSkipsLocalPreflightForNonLocalBind` | ✅ COMPLIANT |
| Truthful Mode Contract | Unsupported mode or target is requested | `internal/infra/install/linux/bootstrap_test.go > TestBootstrapRunRejectsUnsupportedMode`; `cmd/regixtry/main_test.go > TestRunBootstrapPropagatesHostAndRuntimeFailures/unsupported_os_release`; `docs/verification/scripts/install-release-smoke.sh > run_unsupported_target_case` | ✅ COMPLIANT |
| Linux Bootstrap Artifacts | Supported host receives runnable artifacts | `internal/infra/install/linux/bootstrap_test.go > TestBootstrapRunReportsSuccessAfterActivationAndReadiness` | ✅ COMPLIANT |
| Linux Bootstrap Artifacts | Unsupported environment is not overstated | `internal/infra/install/linux/bootstrap_test.go > TestBootstrapRunRejectsUnsupportedHost`; `docs/verification/scripts/install-release-smoke.sh > run_lifecycle_cases` | ✅ COMPLIANT |
| Default Service Activation and Reachability | Setup reaches minimum success | `internal/infra/install/linux/bootstrap_test.go > TestBootstrapRunReportsSuccessAfterActivationAndReadiness` | ✅ COMPLIANT |
| Default Service Activation and Reachability | Service does not become reachable | `internal/infra/install/linux/bootstrap_test.go > TestBootstrapRunWritesArtifactsAndRollsBackOnProbeFailure`; `cmd/regixtry/main_test.go > TestRunBootstrapPropagatesHostAndRuntimeFailures/probe_failure` | ✅ COMPLIANT |
| Scoped Rollback | Recorded lifecycle state is cleaned up | `internal/infra/install/linux/provenance_test.go > TestBootstrapperUninstallReportsDriftTruthfully`; `internal/infra/install/linux/provenance_test.go > TestUninstallCleanupTargetsRemovesInstalledBinaryLast` | ✅ COMPLIANT |
| Scoped Rollback | Drift prevents complete cleanup | `internal/infra/install/linux/provenance_test.go > TestBootstrapperUninstallReportsDriftTruthfully`; `internal/infra/install/linux/provenance_test.go > TestBootstrapperUninstallReturnsFailuresInReport`; `docs/verification/scripts/install-release-smoke.sh > run_lifecycle_cases` | ✅ COMPLIANT |

**Compliance summary**: 14/14 scenarios compliant

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| Binary Lifecycle Entrypoints | ✅ Implemented | `cmd/regixtry/main.go` routes `setup`, `uninstall`, and deferred `upgrade`, and `resolveSetupMode` enforces the TTY/non-TTY contract before setup proceeds. |
| Setup Outcome Contract | ✅ Implemented | `runSetup` validates the `daemon-sqlite` config, delegates to the Linux bootstrap runner, records lifecycle provenance only after a successful run, and reports success only after the backend reaches service activation plus `/v2/` readiness. |
| Provenance-Driven Uninstall | ✅ Implemented | `internal/infra/install/linux/provenance.go` defines normalized lifecycle provenance and cleanup reporting, and `Bootstrapper.Uninstall` uses the persisted provenance path instead of requiring prior operator flags. |
| Truthful Mode Contract | ✅ Implemented | Unsupported setup modes fail in `resolveSetupMode` / `ValidateMode`, and `upgrade` remains explicitly deferred instead of implying unsupported automation exists. |
| Linux Bootstrap Artifacts | ✅ Implemented | `Bootstrapper.plan` and `writeArtifacts` generate the env file, systemd unit, SQLite metadata file, content directory, bootstrap receipt, and lifecycle provenance plan for Linux + systemd installs. |
| Default Service Activation and Reachability | ✅ Implemented | `Bootstrapper.Run` succeeds only after `systemctl enable --now` and `waitUntilReachable` accepts `/v2/` with HTTP `200` or `401`; probe or activation failures roll back and return failure. |
| Scoped Rollback | ✅ Implemented | `rollbackWithReceipt`, `uninstallWithProvenance`, and `uninstallCleanupTargets` remove recorded artifacts best effort, disable the recorded service, keep the installed binary last, and surface removed/missing/failed states truthfully. |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| Add `setup`, `uninstall`, and deferred `upgrade` in `cmd/regixtry/main.go` | ✅ Yes | CLI routing and focused tests show binary-owned lifecycle commands with explicit deferred `upgrade` messaging. |
| Add a separate lifecycle provenance file beyond the bootstrap receipt | ✅ Yes | `internal/infra/install/linux/provenance.go` persists `regixtry-lifecycle-state.json` beside the bootstrap receipt and carries installed-binary plus managed-path cleanup state. |
| Keep bootstrap rollback for backend rollback and add uninstall-specific reporting | ✅ Yes | `Rollback` still consumes the bootstrap receipt, while `Uninstall` reads lifecycle provenance and returns an operator-facing `UninstallReport` with per-item statuses. |

### Issues Found
**CRITICAL**:
- None.

**WARNING**:
- None.

**SUGGESTION**:
- None.

### Verdict
PASS
All 7 requirements and 14 scenarios have passing runtime evidence, the apply-progress metadata is now aligned at 13/13, and the full build/test/vet/smoke suite passed.
