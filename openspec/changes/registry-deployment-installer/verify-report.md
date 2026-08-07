```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:aaed369efa94b9c48418eb4d74030d9a20ca6c21e603ae2b848e5ec14698ebe1
verdict: fail
blockers: 1
critical_findings: 1
requirements: 5/5
scenarios: 9/9
test_command: bash docs/verification/scripts/install-release-smoke.sh
test_exit_code: 1
test_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: registry-deployment-installer
**Version**: N/A
**Mode**: Standard

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 10 |
| Tasks complete | 10 |
| Tasks incomplete | 0 |

### Build & Tests Execution
**Build**: ✅ Passed
```text
$ go build ./...
(no output)
```

**Tests**: ❌ Canonical installer smoke harness failed
```text
$ bash docs/verification/scripts/install-release-smoke.sh
(exit 1, no captured stdout/stderr; output hash sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855)
```

**Harness failure trace**: ❌ Reproduced
```text
$ KEEP_ROOT=1 bash docs/verification/scripts/install-release-smoke.sh /tmp/opencode/sdd-verify-registry-deployment-installer/keep-root
(exit 1)
interactive-binary-only.log:
1
[registry-install] Resolved release v1.2.3 for linux/amd64
[registry-install] Installed registry to .../interactive-binary-only/bin/registry
./install.sh: line 247: /dev/tty: No such device or address
[registry-install] ERROR: no installer mode was selected and no controlling TTY is available
```

**Additional targeted runtime evidence — interactive binary-only chooser**: ✅ Passed
```text
$ printf '1
' | script -qec 'bash ./install.sh --dir /tmp/opencode/sdd-verify-registry-deployment-installer/manual-cases/int-bin/bin' /tmp/opencode/sdd-verify-registry-deployment-installer/manual-cases/int-bin.log
(exit 0, log hash sha256:38387cf66e84606ddce2a1d5b333950afde0786cd3908f214fe66ee33b3c68dd)
```

**Additional targeted runtime evidence — non-interactive binary-only mode**: ✅ Passed
```text
$ bash ./install.sh --dir /tmp/opencode/sdd-verify-registry-deployment-installer/manual-cases/flag-bin/bin --mode binary-only
(exit 0, log hash sha256:934eafe811fe0b102499dbb7bb9b7bee9892e2a88c9636dd6cc8cb22e44bce8a)
```

**Additional targeted runtime evidence — interactive daemon/service chooser**: ✅ Passed
```text
$ printf '2
' | script -qec 'bash ./install.sh --dir /tmp/opencode/sdd-verify-registry-deployment-installer/manual-cases/int-service/bin --public-url http://127.0.0.1:35387 --addr 127.0.0.1:5113 --storage-root /tmp/opencode/sdd-verify-registry-deployment-installer/manual-cases/int-service/storage --state-path /tmp/opencode/sdd-verify-registry-deployment-installer/manual-cases/int-service/etc/bootstrap-state.json --unit-path /tmp/opencode/sdd-verify-registry-deployment-installer/manual-cases/int-service/systemd/registry.service --service registry' /tmp/opencode/sdd-verify-registry-deployment-installer/manual-cases/int-service.log
(exit 0, log hash sha256:ebd230ac17fcc145f27d7bdc1c58057170b8196e73284cf2eec506d2a5857aaa; bootstrap stub hash sha256:1cf160285a0ae3e2f7b7282e1f0ade2a63b198458ea3a5490e032c480674ee8b)
```

**Additional targeted runtime evidence — rejection and rollback paths**: ✅ Passed
```text
$ bash ./install.sh ... > missing-mode.log
(exit 1, hash sha256:8dc8ca0ed4a4c5d1f473396e85e1580663759ff34a5ec9fbce0c7abe359e468e)
$ bash ./install.sh --mode postgres-auth ... > unsupported-mode.log
(exit 1, hash sha256:381c9eea4cafc9c19a17c72ba61f959783149aa8c9a6d0c17f9bd0d94f25711a)
$ bash ./install.sh --mode daemon-sqlite ... bootstrap-unsupported-distro ... > unsupported-distro.log
(exit 1, hash sha256:661a1a810aba15b4d856a5692f3bbefea3eb39108bdbfe0d9a82b6dc941ed7f3)
$ bash ./install.sh --mode daemon-sqlite ... bootstrap-start-failure ... > start-failure.log
(exit 1, hash sha256:1b40b86ac73dfe79c435769ced27ea70ff39cd894ffe83f85303bb859e629fbb)
$ bash ./install.sh --mode daemon-sqlite ... --rollback > rollback.log
(exit 0, hash sha256:5003e215b54d41f4fec5a0465e1eeb17045a53d27ba43ed57e142701a7ce97b0; binary preserved and generated artifacts absent after rollback)
$ bash ./install.sh --mode binary-only --rollback ... > binary-rollback.log
(exit 1, hash sha256:c3d50399e86e6202417aa1b7b2fc1a3a5d06b32d6b86af094ba3b40013f2d3fd)
```

**Tests**: ✅ Full Go suite passed
```text
$ go test ./...
ok  	registry/cmd/registry	(cached)
ok  	registry/internal/app/auth	(cached)
ok  	registry/internal/app/registry	(cached)
?   	registry/internal/domain/auth	[no test files]
ok  	registry/internal/domain/registry	(cached)
ok  	registry/internal/infra/auth/postgres	(cached)
ok  	registry/internal/infra/install/linux	0.008s
ok  	registry/internal/infra/metadata/sqlite	(cached)
ok  	registry/internal/infra/storage/fsblob	(cached)
ok  	registry/internal/ports	(cached)
ok  	registry/internal/protocol/http	(cached)
ok  	registry/internal/tui	(cached)
```

**Coverage**: Per-package coverage reported / threshold: 0% → ✅ Above
```text
$ go test -cover ./...
ok  	registry/cmd/registry	0.955s	coverage: 76.8% of statements
ok  	registry/internal/app/auth	(cached)	coverage: 30.3% of statements
ok  	registry/internal/app/registry	(cached)	coverage: 60.0% of statements
	registry/internal/domain/auth		coverage: 0.0% of statements
ok  	registry/internal/domain/registry	(cached)	coverage: 64.0% of statements
ok  	registry/internal/infra/auth/postgres	(cached)	coverage: 57.7% of statements
ok  	registry/internal/infra/install/linux	0.013s	coverage: 64.0% of statements
ok  	registry/internal/infra/metadata/sqlite	(cached)	coverage: 58.3% of statements
ok  	registry/internal/infra/storage/fsblob	(cached)	coverage: 61.7% of statements
ok  	registry/internal/ports	(cached)	coverage: 60.0% of statements
ok  	registry/internal/protocol/http	(cached)	coverage: 71.5% of statements
ok  	registry/internal/tui	0.012s	coverage: 72.2% of statements
```

**Static analysis**: ✅ Passed
```text
$ go vet ./...
(no output)
```

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| Deferred Mode Guidance | Deferred paths are explained | `manual-cases/int-bin.log`; `manual-cases/flag-bin.log`; `manual-cases/unsupported-mode.log` | ✅ COMPLIANT |
| Truthful Mode Contract | Interactive operator chooses | `manual-cases/int-bin.log`; `manual-cases/int-service.log` | ✅ COMPLIANT |
| Truthful Mode Contract | Automation stays non-interactive | `manual-cases/flag-bin.log`; `manual-cases/missing-mode.log`; `manual-cases/unsupported-mode.log` | ✅ COMPLIANT |
| Linux Bootstrap Artifacts | Supported service host receives runnable artifacts | `manual-cases/int-service.log`; `manual-cases/int-service.bootstrap.log` | ✅ COMPLIANT |
| Linux Bootstrap Artifacts | Unsupported service host is rejected truthfully | `manual-cases/unsupported-distro.log` | ✅ COMPLIANT |
| Default Service Activation and Reachability | Binary-only install succeeds without daemon activation | `manual-cases/int-bin.log`; `manual-cases/flag-bin.log` | ✅ COMPLIANT |
| Default Service Activation and Reachability | Service path still requires reachability | `manual-cases/start-failure.log`; `manual-cases/start-failure.bootstrap.log` | ✅ COMPLIANT |
| Scoped Rollback | Service rollback preserves the binary | `manual-cases/rollback-apply.log`; `manual-cases/rollback.log`; filesystem post-checks` | ✅ COMPLIANT |
| Scoped Rollback | Binary-only path has no service rollback claim | `manual-cases/binary-rollback.log` | ✅ COMPLIANT |

**Compliance summary**: 9/9 scenarios compliant

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| Deferred Mode Guidance | ✅ Implemented | `install.sh` emits manual-today guidance through `deferred_mode_guidance()` and reuses it from help text, interactive prompts, unsupported-mode failures, missing-mode failures, and binary-only success messaging. `README.md` documents the same deferred scope in the install section. |
| Truthful Mode Contract | ✅ Implemented | `install.sh` accepts only `binary-only` or `daemon-sqlite`, prompts through `/dev/tty` when no explicit mode is set, and fails non-interactive runs without a selected mode instead of silently defaulting. |
| Linux Bootstrap Artifacts | ✅ Implemented | `install.sh` delegates service installs to `registry bootstrap --mode daemon-sqlite` unchanged, while `README.md` keeps Linux+systemd host scope explicit. |
| Default Service Activation and Reachability | ✅ Implemented | `binary-only` stops after verified install and prints next steps; `daemon-sqlite` success and failure both flow through the unchanged bootstrap command and preserve the installed binary on failure. |
| Scoped Rollback | ✅ Implemented | `install.sh` allows `--rollback` only with `daemon-sqlite`, reports rollback completion, and refuses to claim rollback work for `binary-only`. |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| Add installer-facing `binary-only` while reusing `daemon-sqlite` | ✅ Yes | `resolve_installer_mode()` chooses between the two installer outcomes and `run_bootstrap()` still hard-codes `daemon-sqlite` delegation. |
| Prompt through `/dev/tty` for interactive runs | ✅ Yes | `prompt_installer_mode()` opens `/dev/tty` and requires an explicit choice, which matches the design and passes when executed under a controlling terminal. |
| Reuse existing bootstrap ownership unchanged | ✅ Yes | Service-path runs still execute `registry bootstrap --mode daemon-sqlite` with the existing bootstrap flags and rollback semantics. |
| Keep docs truthful with shipped behavior | ✅ Yes | `README.md` now matches the implemented chooser, supported automated modes, deferred paths, non-interactive contract, and rollback scope. |

### Issues Found
**CRITICAL**:
- The approved smoke harness command `bash docs/verification/scripts/install-release-smoke.sh` currently fails before completing verification. Its `run_installer_with_pty()` helper opens a PTY but does not provide the installer a controlling `/dev/tty`, so the first interactive chooser case aborts with `./install.sh: line 247: /dev/tty: No such device or address`. Because the canonical verification harness exits non-zero, the SDD verify gate remains FAILED even though targeted runtime probes show the implementation itself matches the spec.

**WARNING**:
- `openspec/changes/registry-deployment-installer/state.yaml` is still absent, so readiness had to be derived from proposal/spec/design/tasks/apply-progress artifacts instead of a persisted state file.

**SUGGESTION**:
- Update `docs/verification/scripts/install-release-smoke.sh` to launch interactive chooser cases with a real controlling TTY (for example via `script`, or an equivalent PTY setup that binds `/dev/tty`) and then rerun verify so the canonical harness, not only ad hoc targeted probes, proves the interactive contract.

### Verdict
FAIL
The implementation appears compliant with all 5 requirements and 9 scenarios under targeted runtime evidence, but the approved installer smoke harness still exits non-zero. That harness failure is a CRITICAL verification blocker under the SDD gate, so this change is not archive-ready yet.
