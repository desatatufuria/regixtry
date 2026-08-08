# Tasks: Regixtry Upgrade Lifecycle

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 850-1150 |
| 1200-line budget risk | Medium |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | Work units 1 → 2 → 3 |
| Delivery strategy | single-pr |
| Chain strategy | size-exception |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: size-exception
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Intent + provenance compatibility foundation | Single PR / Unit 1 | `go test ./internal/infra/install/linux -run 'Test(Provenance|Intent)'` | N/A - package tests cover env/provenance reconstruction only | `internal/infra/install/linux/{intent.go,provenance.go,provenance_test.go}` |
| 2 | Verified staging, swap, and rollback engine | Single PR / Unit 2 | `go test ./internal/infra/install/... -run 'Test(Upgrade|GitHubRelease)'` | N/A - stubbed `systemctl` and probe flows define the runtime boundary | `internal/infra/install/linux/{bootstrap.go,upgrade.go,upgrade_test.go}` + `internal/infra/install/releases/{github.go,github_test.go}` |
| 3 | CLI + installer alignment and end-to-end proof | Single PR / Unit 3 | `go test ./cmd/regixtry ./internal/infra/install/...` | `docs/verification/scripts/install-release-smoke.sh` | `cmd/regixtry/{main.go,main_test.go}` + `install.sh` + smoke script |

## Phase 1: RED Safety Net

- [x] 1.1 Add failing `cmd/regixtry/main_test.go` cases for Go-owned `upgrade`, unmanaged-target rejection, and no-prompt same-shape upgrades.
- [x] 1.2 Create failing `internal/infra/install/releases/github_test.go` cases for latest/tag lookup, wrong checksum, and multi-entry archive rejection.
- [x] 1.3 Create failing `internal/infra/install/linux/upgrade_test.go` cases for stage-before-stop, `systemctl` restore, probe rollback, and no `metadata.db` rewrite.
- [x] 1.4 Extend `internal/infra/install/linux/provenance_test.go` for v1→v2 loading, intent recovery from `regixtry.env`, and guided-missing-intent handling.

## Phase 2: Intent And Provenance Foundation

- [x] 2.1 Create `internal/infra/install/linux/intent.go` to reconstruct runtime intent from provenance, env, and managed paths.
- [x] 2.2 Modify `internal/infra/install/linux/provenance.go` to accept v1, persist v2 only on success, and record structured intent plus installed ref/version.
- [x] 2.3 Modify `internal/infra/install/linux/bootstrap.go` to extract upgrade-safe render/probe helpers and keep DB creation setup-only.

## Phase 3: Upgrade Engine

- [x] 3.1 Create `internal/infra/install/releases/github.go` for release resolution, download, checksum verification, and single-binary archive validation.
- [x] 3.2 Create `internal/infra/install/linux/upgrade.go` for staging, snapshot, swap, restart, readiness validation, and truthful rollback reporting.
- [x] 3.3 Make `internal/infra/install/linux/{upgrade_test.go,provenance_test.go}` pass for subprocess, executable-classification, and process-integration threat cases.

## Phase 4: CLI Wiring And Verification

- [x] 4.1 Modify `cmd/regixtry/main.go` and `cmd/regixtry/main_test.go` to add `parseUpgradeConfig`, `runUpgrade`, `--ref`, `--yes`, and sudo-safe permission guidance.
- [x] 4.2 Modify `install.sh` and `docs/verification/scripts/install-release-smoke.sh` to mirror Go release semantics and cover latest + explicit-ref upgrade smoke paths.
- [x] 4.3 Verify with `gofmt -w .`, `go test ./...`, `go vet ./...`, and `docs/verification/scripts/install-release-smoke.sh`.
