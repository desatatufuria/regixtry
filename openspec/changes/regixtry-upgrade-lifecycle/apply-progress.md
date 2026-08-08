# Apply Progress: Regixtry Upgrade Lifecycle

## Delivery
- Mode: Standard
- Delivery strategy: exception-ok (`size:exception` accepted)
- Scope: single PR covering work units 1 → 3 plus verify-warning remediation

## Completed Tasks
- [x] 1.1 Add failing `cmd/regixtry/main_test.go` cases for Go-owned `upgrade`, unmanaged-target rejection, and no-prompt same-shape upgrades.
- [x] 1.2 Create failing `internal/infra/install/releases/github_test.go` cases for latest/tag lookup, wrong checksum, and multi-entry archive rejection.
- [x] 1.3 Create failing `internal/infra/install/linux/upgrade_test.go` cases for stage-before-stop, `systemctl` restore, probe rollback, and no `metadata.db` rewrite.
- [x] 1.4 Extend `internal/infra/install/linux/provenance_test.go` for v1→v2 loading, intent recovery from `regixtry.env`, and guided-missing-intent handling.
- [x] 2.1 Create `internal/infra/install/linux/intent.go` to reconstruct runtime intent from provenance, env, and managed paths.
- [x] 2.2 Modify `internal/infra/install/linux/provenance.go` to accept v1, persist v2 only on success, and record structured intent plus installed ref/version.
- [x] 2.3 Modify `internal/infra/install/linux/bootstrap.go` to extract upgrade-safe render/probe helpers and keep DB creation setup-only.
- [x] 3.1 Create `internal/infra/install/releases/github.go` for release resolution, download, checksum verification, and single-binary archive validation.
- [x] 3.2 Create `internal/infra/install/linux/upgrade.go` for staging, snapshot, swap, restart, readiness validation, and truthful rollback reporting.
- [x] 3.3 Make `internal/infra/install/linux/{upgrade_test.go,provenance_test.go}` pass for subprocess, executable-classification, and process-integration threat cases.
- [x] 4.1 Modify `cmd/regixtry/main.go` and `cmd/regixtry/main_test.go` to add `parseUpgradeConfig`, `runUpgrade`, `--ref`, `--yes`, and sudo-safe permission guidance.
- [x] 4.2 Modify `install.sh` and `docs/verification/scripts/install-release-smoke.sh` to mirror Go release semantics and cover latest + explicit-ref upgrade smoke paths.
- [x] 4.3 Verify with `gofmt -w .`, `go test ./...`, `go vet ./...`, and `docs/verification/scripts/install-release-smoke.sh`.

## Remediation
- [x] Restore the pre-upgrade lifecycle provenance file during rollback when post-restart provenance persistence fails.
- [x] Add a focused regression test that forces `writeLifecycleProvenance()` to fail after restart/readiness and proves verbatim provenance restoration.
- [x] Extend the smoke harness with an opt-in real Linux + systemd successful upgrade scenario while keeping the default repo harness portable.

## Work Unit Evidence

| Unit | Focused test command and exact result | Runtime harness command/scenario and exact result | Rollback boundary |
|---|---|---|---|
| 1 | `go test ./internal/infra/install/linux -run 'Test(Provenance|Intent)'` → pass | `N/A` — package tests cover provenance/env reconstruction only | `internal/infra/install/linux/{intent.go,provenance.go,provenance_test.go}` |
| 2 | `go test ./internal/infra/install/... -run 'Test(Upgrade|GitHubRelease)'` → pass | `N/A` — stubbed `systemctl` + probe flows exercised stage-before-stop and rollback boundaries in package tests | `internal/infra/install/linux/{bootstrap.go,upgrade.go,upgrade_test.go}` + `internal/infra/install/releases/{github.go,github_test.go}` |
| 3 | `go test ./cmd/regixtry ./internal/infra/install/...` → pass | `bash docs/verification/scripts/install-release-smoke.sh` → pass (`Installer release smoke scenarios passed`) | `cmd/regixtry/{main.go,main_test.go}` + `install.sh` + `docs/verification/scripts/install-release-smoke.sh` |
| Verify warning remediation | `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go test ./internal/infra/install/linux -run 'TestBootstrapperUpgrade' -count=1 -v` → pass (4/4 tests, including provenance-write rollback) | `bash docs/verification/scripts/install-release-smoke.sh` → pass; default harness remains portable and now also contains an opt-in `REGIXTRY_SMOKE_REAL_UPGRADE=1` real-host success path for Linux + systemd environments | `internal/infra/install/linux/{upgrade.go,upgrade_test.go}` + `docs/verification/scripts/install-release-smoke.sh` |

## Final Verification
- `gofmt -w internal/infra/install/linux/upgrade.go internal/infra/install/linux/upgrade_test.go` → pass
- `bash -n docs/verification/scripts/install-release-smoke.sh` → pass
- `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOCACHE="/tmp/opencode/gocache" GOSUMDB=off go test ./internal/infra/install/linux -run 'TestBootstrapperUpgrade' -count=1 -v` → pass
- `bash docs/verification/scripts/install-release-smoke.sh` → pass

## Notes
- Rollback now restores the original lifecycle provenance bytes and file mode in addition to binary, env, unit, and bootstrap receipt state.
- The smoke harness still cannot prove a real-host upgrade in this container by default, but it can now do so on an actual Linux + systemd host when `REGIXTRY_SMOKE_REAL_UPGRADE=1` is enabled with root privileges.
