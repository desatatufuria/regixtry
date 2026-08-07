```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:ab28d676ac0eea6dfffeb791353f0dd8f0833804de6fc33938328ae3b9a4d351
verdict: pass_with_warnings
blockers: 0
critical_findings: 0
requirements: 4/4
scenarios: 6/6
test_command: /tmp/opencode/registry-release-installer-rerun/test-command.sh
test_exit_code: 0
test_output_hash: sha256:78a78a4e501c87b1e1ddb656d9bd41ffb1c045a58dae32150af55f60fc15974d
build_command: GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: registry-release-installer
**Version**: N/A
**Mode**: Standard

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 14 |
| Tasks complete | 14 |
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

**Focused runtime evidence — release metadata helpers**: ✅ Passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go test ./cmd/registry -count=1 -run 'TestDefaultBuildValue|TestReleaseMetadataUsesDefaultsAndOverrides'
ok  	registry/cmd/registry	0.010s
```

**Focused runtime evidence — installer smoke harness**: ✅ Passed
```text
$ bash docs/verification/scripts/install-release-smoke.sh
Installer release smoke scenarios passed. Root: /tmp/registry-install-smoke.eeWaqG
```

**Focused runtime evidence — unsupported non-Linux guardrail**: ✅ Passed
```text
$ env REGISTRY_INSTALL_OS=darwin REGISTRY_INSTALL_RELEASES_PAGE_URL=https://example.invalid/releases bash ./install.sh --dir /tmp/opencode/registry-release-installer-nonlinux-check
[registry-install] ERROR: automated release installation is only available for Linux in this slice
[registry-install] Manual options:
[registry-install] - Download a verified Linux release from: https://example.invalid/releases
[registry-install] - Or build from source manually with: git clone https://github.com/desatatufuria/workspace.git && cd workspace && go build -o registry ./cmd/registry
```

**Tests**: ✅ Full Go suite passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go test ./... -count=1
ok  	registry/cmd/registry	1.128s
ok  	registry/internal/app/auth	0.124s
ok  	registry/internal/app/registry	0.307s
?   	registry/internal/domain/auth	[no test files]
ok  	registry/internal/domain/registry	0.005s
ok  	registry/internal/infra/auth/postgres	0.229s
ok  	registry/internal/infra/metadata/sqlite	0.153s
ok  	registry/internal/infra/storage/fsblob	0.010s
ok  	registry/internal/ports	0.006s
ok  	registry/internal/protocol/http	1.054s
ok  	registry/internal/tui	0.015s
```

**Coverage**: Per-package coverage reported / threshold: 0% → ✅ Above
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go test -cover ./... -count=1
ok  	registry/cmd/registry	1.170s	coverage: 72.8% of statements
ok  	registry/internal/app/auth	0.142s	coverage: 30.3% of statements
ok  	registry/internal/app/registry	0.327s	coverage: 60.0% of statements
	registry/internal/domain/auth		coverage: 0.0% of statements
ok  	registry/internal/domain/registry	0.008s	coverage: 64.0% of statements
ok  	registry/internal/infra/auth/postgres	0.244s	coverage: 57.7% of statements
ok  	registry/internal/infra/metadata/sqlite	0.157s	coverage: 58.3% of statements
ok  	registry/internal/infra/storage/fsblob	0.018s	coverage: 61.7% of statements
ok  	registry/internal/ports	0.008s	coverage: 60.0% of statements
ok  	registry/internal/protocol/http	0.974s	coverage: 71.5% of statements
ok  	registry/internal/tui	0.022s	coverage: 72.2% of statements
```

**Static analysis**: ✅ Passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go vet ./...
(no output)
```

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| Release-backed Linux installation | Install from a published Linux release | `docs/verification/scripts/install-release-smoke.sh > success-amd64-space-dir`; `docs/verification/scripts/install-release-smoke.sh > success-arm64` | ✅ COMPLIANT |
| Release-backed Linux installation | Unsupported non-Linux target | `env REGISTRY_INSTALL_OS=darwin ... bash ./install.sh --dir /tmp/opencode/registry-release-installer-nonlinux-check` | ✅ COMPLIANT |
| Mandatory checksum verification | Checksum verification succeeds | `docs/verification/scripts/install-release-smoke.sh > success-amd64-space-dir`; `docs/verification/scripts/install-release-smoke.sh > success-arm64` | ✅ COMPLIANT |
| Mandatory checksum verification | Checksum asset missing or mismatched | `docs/verification/scripts/install-release-smoke.sh > missing-checksum`; `docs/verification/scripts/install-release-smoke.sh > checksum-mismatch` | ✅ COMPLIANT |
| Hard failure with manual guidance | Release asset lookup fails | `docs/verification/scripts/install-release-smoke.sh > missing-release-asset` | ✅ COMPLIANT |
| Stable installed binary name | Installed command name remains stable | `docs/verification/scripts/install-release-smoke.sh > success-amd64-space-dir`; `docs/verification/scripts/install-release-smoke.sh > success-arm64` | ✅ COMPLIANT |

**Compliance summary**: 6/6 scenarios compliant

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| Tagged releases publish Linux assets plus checksums | ✅ Implemented | `.goreleaser.yaml` defines `linux/amd64` and `linux/arm64` tarballs plus `registry_<version>_checksums.txt`; `.github/workflows/release.yml` is tag-only and explicitly fails if either archive or the checksum file is missing from `dist/`. |
| Installer resolves release assets without source-build fallback | ✅ Implemented | `install.sh` resolves `/latest` or `/tags/<ref>` metadata, selects one matching archive plus checksum asset, and never invokes `git` or `go`; failures route through `fail_with_guidance` instead of any automatic rebuild path. |
| Verification is mandatory before extraction/install | ✅ Implemented | `install.sh` requires `sha256sum`, looks up the selected asset entry in the checksum file, rejects malformed entries, and validates the tarball before extraction. |
| Reader and maintainer docs match shipped scope | ✅ Implemented | `README.md` documents Linux-only verified release installs and manual fallback; `docs/contributing.md` records GitHub Releases as source of truth and the checksum-contract update rule. |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| Publish `registry_<version>_linux_<arch>.tar.gz` plus `registry_<version>_checksums.txt` | ✅ Yes | `.goreleaser.yaml` and the workflow artifact checks use the exact contract, and `install.sh` matches archive/checksum names with wildcard selectors. |
| Keep `--ref` but narrow it to release tags / release names | ✅ Yes | `install.sh` still exposes `--ref`, validates it against a tag-safe pattern, and maps it only to the releases API `/tags/<ref>` endpoint. |
| Use GoReleaser plus a tag-triggered GitHub Actions workflow | ✅ Yes | `.github/workflows/release.yml` runs only on `v*` tags, executes `go test ./cmd/registry`, then publishes through `goreleaser/goreleaser-action@v7`. |

### Issues Found
**CRITICAL**:
- None.

**WARNING**:
- OpenSpec `apply-progress.md` now reports `14/14 tasks complete`, but the persisted Engram `sdd/registry-release-installer/apply-progress` artifact still shows `13/13 tasks complete`; implementation evidence is complete, but hybrid planning artifacts remain out of sync.

**SUGGESTION**:
- Refresh the Engram `sdd/registry-release-installer/apply-progress` artifact so both planning backends carry the same final task count before archive consumes the change history.

### Verdict
PASS WITH WARNINGS
All 14 tasks are complete and runtime evidence now proves all 6 release-installer scenarios, but the Engram copy of `apply-progress` still lags the corrected OpenSpec task count.
