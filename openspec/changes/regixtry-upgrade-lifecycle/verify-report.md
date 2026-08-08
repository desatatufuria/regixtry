```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:db56fcbad9803b6d6ad48561340de494774ae5ac9d5a3c541d6c48131f6c0ce4
verdict: pass_with_warnings
blockers: 0
critical_findings: 0
requirements: 6/6
scenarios: 14/14
test_command: GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go test ./cmd/regixtry ./internal/infra/install/linux ./internal/infra/install/releases -count=1 -v -run 'TestRunUpgrade|TestBootstrapperUpgrade|TestLoadInstalledIntent|TestReadLifecycleProvenance|TestGitHubRelease'
test_exit_code: 0
test_output_hash: sha256:47fdac6eb8bf4b069047c418eb6e07610271287071acbef7e0c5faa876280ca0
build_command: GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: regixtry-upgrade-lifecycle
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
$ gofmt -l internal/infra/install/linux/upgrade.go internal/infra/install/linux/upgrade_test.go internal/infra/install/linux/bootstrap.go
(no output)
```

**Focused runtime evidence — swap and rollback path**: ✅ Passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go test ./internal/infra/install/linux -run 'TestBootstrapperUpgrade' -count=1 -v
=== RUN   TestBootstrapperUpgradeStagesBeforeStoppingAndPreservesMetadataDB
--- PASS: TestBootstrapperUpgradeStagesBeforeStoppingAndPreservesMetadataDB (0.00s)
=== RUN   TestBootstrapperUpgradeRestoresAfterSystemctlFailure
--- PASS: TestBootstrapperUpgradeRestoresAfterSystemctlFailure (0.00s)
=== RUN   TestBootstrapperUpgradeRestoresAfterProbeFailure
--- PASS: TestBootstrapperUpgradeRestoresAfterProbeFailure (0.01s)
=== RUN   TestBootstrapperUpgradeRestoresProvenanceAfterProvenanceWriteFailure
--- PASS: TestBootstrapperUpgradeRestoresProvenanceAfterProvenanceWriteFailure (0.00s)
PASS
ok  	regixtry/internal/infra/install/linux	0.020s
```

**Focused runtime evidence — upgrade contract suite**: ✅ Passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go test ./cmd/regixtry ./internal/infra/install/linux ./internal/infra/install/releases -count=1 -v -run 'TestRunUpgrade|TestBootstrapperUpgrade|TestLoadInstalledIntent|TestReadLifecycleProvenance|TestGitHubRelease'
=== RUN   TestRunUpgradePassesConfigToGoOwnedLifecycleRunner
--- PASS: TestRunUpgradePassesConfigToGoOwnedLifecycleRunner (0.00s)
=== RUN   TestRunUpgradeRejectsUnmanagedTargetTruthfully
--- PASS: TestRunUpgradeRejectsUnmanagedTargetTruthfully (0.00s)
=== RUN   TestRunUpgradeSameShapeDoesNotPromptAndPreservesNonInteractiveFlow
--- PASS: TestRunUpgradeSameShapeDoesNotPromptAndPreservesNonInteractiveFlow (0.00s)
PASS
ok  	regixtry/cmd/regixtry	0.043s
=== RUN   TestReadLifecycleProvenanceAcceptsV1AndV2
=== PAUSE TestReadLifecycleProvenanceAcceptsV1AndV2
=== RUN   TestLoadInstalledIntentRecoversFromManagedEnv
=== PAUSE TestLoadInstalledIntentRecoversFromManagedEnv
=== RUN   TestLoadInstalledIntentReportsMissingLifecycleCriticalValues
=== PAUSE TestLoadInstalledIntentReportsMissingLifecycleCriticalValues
=== RUN   TestBootstrapperUpgradeStagesBeforeStoppingAndPreservesMetadataDB
--- PASS: TestBootstrapperUpgradeStagesBeforeStoppingAndPreservesMetadataDB (0.00s)
=== RUN   TestBootstrapperUpgradeRestoresAfterSystemctlFailure
--- PASS: TestBootstrapperUpgradeRestoresAfterSystemctlFailure (0.00s)
=== RUN   TestBootstrapperUpgradeRestoresAfterProbeFailure
--- PASS: TestBootstrapperUpgradeRestoresAfterProbeFailure (0.01s)
=== RUN   TestBootstrapperUpgradeRestoresProvenanceAfterProvenanceWriteFailure
--- PASS: TestBootstrapperUpgradeRestoresProvenanceAfterProvenanceWriteFailure (0.00s)
=== CONT  TestReadLifecycleProvenanceAcceptsV1AndV2
=== CONT  TestLoadInstalledIntentReportsMissingLifecycleCriticalValues
--- PASS: TestReadLifecycleProvenanceAcceptsV1AndV2 (0.00s)
--- PASS: TestLoadInstalledIntentReportsMissingLifecycleCriticalValues (0.00s)
=== CONT  TestLoadInstalledIntentRecoversFromManagedEnv
--- PASS: TestLoadInstalledIntentRecoversFromManagedEnv (0.00s)
PASS
ok  	regixtry/internal/infra/install/linux	0.028s
=== RUN   TestGitHubReleaseResolveLatestAndTag
=== PAUSE TestGitHubReleaseResolveLatestAndTag
=== RUN   TestGitHubReleaseDownloadVerifiedBinaryRejectsWrongChecksum
=== PAUSE TestGitHubReleaseDownloadVerifiedBinaryRejectsWrongChecksum
=== RUN   TestGitHubReleaseDownloadVerifiedBinaryRejectsMultiEntryArchive
=== PAUSE TestGitHubReleaseDownloadVerifiedBinaryRejectsMultiEntryArchive
=== CONT  TestGitHubReleaseResolveLatestAndTag
=== CONT  TestGitHubReleaseDownloadVerifiedBinaryRejectsMultiEntryArchive
=== CONT  TestGitHubReleaseDownloadVerifiedBinaryRejectsWrongChecksum
--- PASS: TestGitHubReleaseResolveLatestAndTag (0.00s)
--- PASS: TestGitHubReleaseDownloadVerifiedBinaryRejectsWrongChecksum (0.01s)
--- PASS: TestGitHubReleaseDownloadVerifiedBinaryRejectsMultiEntryArchive (0.01s)
PASS
ok  	regixtry/internal/infra/install/releases	0.015s
```

**Build**: ✅ Passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go build ./...
(no output)
```

**Coverage**: ➖ Not re-run in this focused continuation
```text
Focused remediation verification re-ran the upgrade-specific runtime suites and a repository-wide build.
The previous full verification already established 14/14 scenario coverage; this continuation only re-validated the binary-swap remediation.
```

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| Binary Lifecycle Entrypoints | Setup command is available | `cmd/regixtry/main_test.go > TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance` | ✅ COMPLIANT |
| Binary Lifecycle Entrypoints | Upgrade command is available | `cmd/regixtry/main_test.go > TestRunUpgradePassesConfigToGoOwnedLifecycleRunner` | ✅ COMPLIANT |
| Binary Lifecycle Entrypoints | Unsupported upgrade target is rejected | `cmd/regixtry/main_test.go > TestRunUpgradeRejectsUnmanagedTargetTruthfully` | ✅ COMPLIANT |
| Upgrade Version Selection And Prompts | Latest release is selected by default | `internal/infra/install/releases/github_test.go > TestGitHubReleaseResolveLatestAndTag` | ✅ COMPLIANT |
| Upgrade Version Selection And Prompts | Prompting is gated to risky or incomplete cases | `cmd/regixtry/main_test.go > TestRunUpgradeSameShapeDoesNotPromptAndPreservesNonInteractiveFlow` | ✅ COMPLIANT |
| Upgrade Staging And Validation | Staged upgrade reaches success | `internal/infra/install/linux/upgrade_test.go > TestBootstrapperUpgradeStagesBeforeStoppingAndPreservesMetadataDB` | ✅ COMPLIANT |
| Upgrade Staging And Validation | Staging failure prevents service interruption | `internal/infra/install/releases/github_test.go > TestGitHubReleaseDownloadVerifiedBinaryRejectsWrongChecksum`; `docs/verification/scripts/install-release-smoke.sh > upgrade-ref checksum mismatch keeps old binary` | ✅ COMPLIANT |
| Truthful Mode Contract | Supported lifecycle target is selected | `cmd/regixtry/main_test.go > TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance` | ✅ COMPLIANT |
| Truthful Mode Contract | Unsupported mode or target is requested | `docs/verification/scripts/install-release-smoke.sh > unsupported daemon-sqlite host path`; `cmd/regixtry/main_test.go > TestRunUpgradeRejectsUnmanagedTargetTruthfully` | ✅ COMPLIANT |
| Scoped Rollback | Recorded lifecycle state is cleaned up | `internal/infra/install/linux/provenance_test.go > TestBootstrapperUninstallReportsDriftTruthfully`; `docs/verification/scripts/install-release-smoke.sh > uninstall drift report` | ✅ COMPLIANT |
| Scoped Rollback | Drift prevents complete cleanup | `internal/infra/install/linux/provenance_test.go > TestBootstrapperUninstallReportsDriftTruthfully` | ✅ COMPLIANT |
| Scoped Rollback | Failed upgrade restores prior runnable state | `internal/infra/install/linux/upgrade_test.go > TestBootstrapperUpgradeRestoresAfterSystemctlFailure`; `internal/infra/install/linux/upgrade_test.go > TestBootstrapperUpgradeRestoresAfterProbeFailure`; `internal/infra/install/linux/upgrade_test.go > TestBootstrapperUpgradeRestoresProvenanceAfterProvenanceWriteFailure` | ✅ COMPLIANT |
| Upgrade Intent Preservation Compatibility | Existing install upgrades without config drift | `internal/infra/install/linux/provenance_test.go > TestLoadInstalledIntentRecoversFromManagedEnv`; `internal/infra/install/linux/upgrade_test.go > TestBootstrapperUpgradeStagesBeforeStoppingAndPreservesMetadataDB` | ✅ COMPLIANT |
| Upgrade Intent Preservation Compatibility | Missing required intent allows guided recovery only when needed | `internal/infra/install/linux/provenance_test.go > TestLoadInstalledIntentReportsMissingLifecycleCriticalValues` | ✅ COMPLIANT |

**Compliance summary**: 14/14 scenarios compliant

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| Binary Lifecycle Entrypoints | ✅ Implemented | `cmd/regixtry/main.go` wires `upgrade` to `runUpgrade`, parses `--ref/--yes/--state-path`, and uses the Go lifecycle runner instead of deferred messaging. |
| Upgrade Version Selection And Prompts | ✅ Implemented | `internal/infra/install/releases/github.go` resolves `/latest` or `/tags/<ref>`, while `runUpgrade` stays non-interactive and `requireUpgradeConfirmation()` gates only major-version jumps behind `--yes`. |
| Upgrade Staging And Validation | ✅ Implemented | `internal/infra/install/linux/upgrade.go` now stages a same-directory swap file, renames the current binary to a backup path, activates the prepared binary with `os.Rename`, and still requires restart plus `200/401` readiness before success. |
| Truthful Mode Contract | ✅ Implemented | Unsupported or unmanaged targets bubble failures without success messaging, and installer UX points operators to binary-owned `setup`, `upgrade`, and `uninstall` commands. |
| Scoped Rollback | ✅ Implemented | `restoreUpgradeSnapshot()` restores binary, env, unit, bootstrap receipt, and original lifecycle provenance; the focused regression tests prove rollback after restart failure, readiness failure, and post-restart provenance-write failure. |
| Upgrade Intent Preservation Compatibility | ✅ Implemented | `loadInstalledIntent()` preserves storage, DB, public URL, TLS, and opaque auth DSN from v1 provenance + env without forcing re-entry. |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| Separate upgrade backend from setup | ✅ Yes | `internal/infra/install/linux/upgrade.go` remains a sibling flow and `bootstrap.go` exposes upgrade-safe artifact rendering/probing without recreating `metadata.db`. |
| Keep release resolution in Go, not shell | ✅ Yes | `internal/infra/install/releases/github.go` mirrors installer lookup/checksum/archive rules and `upgrade.go` depends on it directly. |
| Reconstruct intent from provenance first, then env and managed paths | ✅ Yes | `internal/infra/install/linux/intent.go` prioritizes provenance intent, then recovers missing values from `regixtry.env` and classified managed paths. |
| Evolve provenance to v2 while keeping v1 readable | ✅ Yes | `provenance.go` accepts v1/v2 and successful upgrades enrich ref/version plus structured intent in v2. |
| Atomic swap and success-only provenance persistence | ✅ Yes | `swapInstalledBinary()` now writes the replacement into the target directory, renames the current binary to a same-filesystem backup, renames the prepared file into place, and tests prove no direct binary overwrite plus cleanup of swap artifacts. |

### Issues Found
**CRITICAL**:
- None.

**WARNING**:
- `docs/verification/scripts/install-release-smoke.sh` still contains the real Linux + systemd upgrade-success proof behind `REGIXTRY_SMOKE_REAL_UPGRADE=1`, but this verification environment did not execute that host-level path. The remaining warning is evidence-only, not an implementation defect in the swap remediation.

**SUGGESTION**:
- Run `REGIXTRY_SMOKE_REAL_UPGRADE=1 bash docs/verification/scripts/install-release-smoke.sh` on a real Linux + systemd host or dedicated CI lane so successful in-place upgrade proof becomes routine instead of conditional.

### Verdict
PASS WITH WARNINGS
The binary replacement remediation is sufficient: direct overwrite is gone, the implementation now uses target-directory staging plus backup-and-rename rollback semantics, and the focused runtime suites pass. The only remaining warning is the still-conditional real-systemd success evidence.
