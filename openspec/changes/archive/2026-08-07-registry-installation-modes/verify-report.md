```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:7ba462c8b6a383e1328081c4a401afce567c3c2a1a5f16561610a17ead8d8778
verdict: pass
blockers: 0
critical_findings: 0
requirements: 4/4
scenarios: 8/8
test_command: go test ./... -count=1 && bash docs/verification/scripts/install-release-smoke.sh --release-dist ./dist
test_exit_code: 0
test_output_hash: sha256:6eeb5ea208e934aa3e09ff43c6bb9dadea6a0e011c8e206e516ba826d849db99
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: registry-installation-modes
**Version**: N/A
**Mode**: Standard

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 12 |
| Tasks complete | 12 |
| Tasks incomplete | 0 |

### Build & Tests Execution
**Build**: ✅ Passed
```text
$ go build ./...
(no output)
```

**Tests**: ✅ Full Go suite and installer smoke harness passed
```text
$ go test ./... -count=1
ok  	registry/cmd/registry	1.387s
ok  	registry/internal/app/auth	0.151s
ok  	registry/internal/app/registry	0.343s
?   	registry/internal/domain/auth	[no test files]
ok  	registry/internal/domain/registry	0.011s
ok  	registry/internal/infra/auth/postgres	0.265s
ok  	registry/internal/infra/install/linux	0.018s
ok  	registry/internal/infra/metadata/sqlite	0.176s
ok  	registry/internal/infra/storage/fsblob	0.022s
ok  	registry/internal/ports	0.014s
ok  	registry/internal/protocol/http	1.160s
ok  	registry/internal/tui	0.034s
$ bash docs/verification/scripts/install-release-smoke.sh --release-dist ./dist
Installer release smoke scenarios passed. Root: /tmp/registry-install-smoke.xkSYHJ
```

**Coverage**: Per-package coverage reported / threshold: 0% → ✅ Above
```text
$ go test -cover ./... -count=1
ok  	registry/cmd/registry	1.427s	coverage: 76.8% of statements
ok  	registry/internal/app/auth	0.147s	coverage: 30.3% of statements
ok  	registry/internal/app/registry	0.348s	coverage: 60.0% of statements
	registry/internal/domain/auth		coverage: 0.0% of statements
ok  	registry/internal/domain/registry	0.021s	coverage: 64.0% of statements
ok  	registry/internal/infra/auth/postgres	0.272s	coverage: 57.7% of statements
ok  	registry/internal/infra/install/linux	0.031s	coverage: 64.0% of statements
ok  	registry/internal/infra/metadata/sqlite	0.186s	coverage: 58.3% of statements
ok  	registry/internal/infra/storage/fsblob	0.030s	coverage: 61.7% of statements
ok  	registry/internal/ports	0.018s	coverage: 60.0% of statements
ok  	registry/internal/protocol/http	1.165s	coverage: 71.5% of statements
ok  	registry/internal/tui	0.038s	coverage: 72.2% of statements
```

**Static analysis**: ✅ Passed
```text
$ go vet ./...
(no output)
```

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| Truthful Mode Contract | Supported mode is selected | `cmd/registry/main_test.go > TestRunBootstrapPassesParsedConfigToRunner`; `docs/verification/scripts/install-release-smoke.sh > run_bootstrap_success_case` | ✅ COMPLIANT |
| Truthful Mode Contract | Unsupported mode is requested | `cmd/registry/main_test.go > TestBootstrapParseConfigRejectsUnsupportedModeAndWhitespacePaths/rejects unsupported mode` | ✅ COMPLIANT |
| Linux Bootstrap Artifacts | Supported Linux host receives runnable artifacts | `internal/infra/install/linux/bootstrap_test.go > TestTemplateRendering`; `internal/infra/install/linux/bootstrap_test.go > TestBootstrapRunReportsSuccessAfterActivationAndReadiness`; `docs/verification/scripts/install-release-smoke.sh > run_bootstrap_success_case` | ✅ COMPLIANT |
| Linux Bootstrap Artifacts | Unsupported environment is not overstated | `internal/infra/install/linux/bootstrap_test.go > TestDetectHost/rejects alpine`; `cmd/registry/main_test.go > TestRunBootstrapPropagatesHostAndRuntimeFailures/unsupported os release`; `cmd/registry/main_test.go > TestRunBootstrapPropagatesHostAndRuntimeFailures/missing systemd`; `docs/verification/scripts/install-release-smoke.sh > run_bootstrap_failure_case(bootstrap-unsupported-distro)` | ✅ COMPLIANT |
| Default Service Activation and Reachability | Bootstrap reaches minimum success | `internal/infra/install/linux/bootstrap_test.go > TestBootstrapRunReportsSuccessAfterActivationAndReadiness`; `cmd/registry/main_test.go > TestRunBootstrapPassesParsedConfigToRunner`; `docs/verification/scripts/install-release-smoke.sh > run_bootstrap_success_case` | ✅ COMPLIANT |
| Default Service Activation and Reachability | Service does not become reachable | `internal/infra/install/linux/bootstrap_test.go > TestBootstrapRunWritesArtifactsAndRollsBackOnProbeFailure`; `cmd/registry/main_test.go > TestRunBootstrapPropagatesHostAndRuntimeFailures/systemctl enable failure`; `cmd/registry/main_test.go > TestRunBootstrapPropagatesHostAndRuntimeFailures/probe failure`; `docs/verification/scripts/install-release-smoke.sh > run_bootstrap_failure_case(bootstrap-start-failure)` | ✅ COMPLIANT |
| Scoped Rollback | Operator rolls back a completed bootstrap | `internal/infra/install/linux/bootstrap_test.go > TestBootstrapRollbackRemovesGeneratedArtifacts`; `docs/verification/scripts/install-release-smoke.sh > run_bootstrap_rollback_case` | ✅ COMPLIANT |
| Scoped Rollback | Rollback after failed activation | `internal/infra/install/linux/bootstrap_test.go > TestBootstrapRunWritesArtifactsAndRollsBackOnProbeFailure`; `docs/verification/scripts/install-release-smoke.sh > run_bootstrap_failure_case(bootstrap-start-failure)` | ✅ COMPLIANT |

**Compliance summary**: 8/8 scenarios compliant

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| Truthful Mode Contract | ✅ Implemented | `install.sh` defaults `BOOTSTRAP_MODE` to `daemon-sqlite`, forwards bootstrap flags through `run_bootstrap`, and `cmd/registry/main.go` delegates validation to `installlinux.ValidateConfig`/`ValidateMode`, which only allow `daemon-sqlite`. |
| Linux Bootstrap Artifacts | ✅ Implemented | `internal/infra/install/linux/detect.go` gates Linux + systemd + Debian/Ubuntu/Linux Mint/RHEL 9/10, while `Bootstrapper.writeArtifacts` renders `registry.env`, the systemd unit, the SQLite DB file, and the receipt declared in the design. |
| Default Service Activation and Reachability | ✅ Implemented | `Bootstrapper.Run` executes `systemctl daemon-reload`, `systemctl enable --now`, and `waitUntilReachable`, which now has a passing runtime test proving `/v2/` normalization and HTTP `401` readiness acceptance. |
| Scoped Rollback | ✅ Implemented | `Bootstrapper.Rollback` replays the receipt to stop/disable the service and remove generated paths, while `install.sh` reports bootstrap failure without uninstalling the verified binary. |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| Bootstrap owner | ✅ Yes | `install.sh` still owns release discovery/download/checksum verification, then invokes `registry bootstrap` for host policy and service orchestration. |
| Artifact model | ✅ Yes | The implementation writes `/etc/registry/registry.env`, the service unit, the SQLite DB path under the storage root, and `bootstrap-state.json` as a receipt for rollback. |
| Service management | ✅ Yes | The Go bootstrapper hard-codes `systemctl daemon-reload` and `systemctl enable --now <service>.service`, matching the root-owned systemd-only design. |
| Distro truthfulness | ✅ Yes | `supportedDistribution` accepts Debian, Ubuntu, Linux Mint, and RHEL 9/10, rejects Alpine explicitly, and fails when `/run/systemd/system` is absent. |
| Startup/rollback contract | ✅ Yes | The code polls `<public-url>/v2/` for HTTP `200` or `401` and removes generated artifacts on rollback while keeping the installed binary in place. |

### Issues Found
**CRITICAL**:
- None.

**WARNING**:
- None.

**SUGGESTION**:
- Consider adding a future non-stub systemd-host integration harness if CI/runtime support becomes available; current final verification is already satisfied by the real Go bootstrap success-path test plus installer smoke coverage.

### Verdict
PASS
The added `Bootstrapper.Run` success-path runtime test closes the last verification gap, so all 4 requirements and 8 scenarios now have compliant runtime-backed evidence.
