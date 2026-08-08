# Tasks: Regixtry Lifecycle CLI

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 750-950 |
| 1200-line budget risk | Medium |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 → PR 2 → PR 3 |
| Delivery strategy | exception-ok |
| Chain strategy | size-exception |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: size-exception
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Provenance + uninstall safety core | PR 1 | `go test ./internal/infra/install/linux` | N/A - fake `systemctl`/probe coverage in package tests | `internal/infra/install/linux/{bootstrap.go,provenance.go,provenance_test.go,bootstrap_test.go}` |
| 2 | CLI lifecycle entrypoints + TTY rules | PR 2 | `go test ./cmd/regixtry` | `go run ./cmd/regixtry setup --mode binary-only` | `cmd/regixtry/main.go`, `cmd/regixtry/main_test.go` |
| 3 | Installer/docs/smoke alignment | PR 3 | `go test ./...` | `docs/verification/scripts/install-release-smoke.sh` | `install.sh`, `docs/verification/scripts/install-release-smoke.sh`, `README.md` |

## Phase 1: RED Safety Net

- [x] 1.1 Add failing `cmd/regixtry/main_test.go` cases for `setup`/`uninstall` routing, deferred `upgrade`, and no-TTY `setup` without `--mode`.
- [x] 1.2 Create failing `internal/infra/install/linux/provenance_test.go` coverage for missing provenance, drifted cleanup statuses, and installed-binary-last removal order.
- [x] 1.3 Extend failing `internal/infra/install/linux/bootstrap_test.go` cases for unsupported host/mode, `systemctl enable --now`, `disable --now`, and `/v2/` probe failures.
- [x] 1.4 Add failing `docs/verification/scripts/install-release-smoke.sh` assertions proving `install.sh` stays downloader-only, bad archives still fail, and drifted uninstall reports truthfully.

## Phase 2: Lifecycle Backend Foundation

- [x] 2.1 Create `internal/infra/install/linux/provenance.go` with `LifecycleProvenance`, `CleanupItem`, load/save helpers, and uninstall report formatting.
- [x] 2.2 Modify `internal/infra/install/linux/bootstrap.go` to emit normalized managed paths, installed binary metadata, and service state needed by lifecycle provenance.
- [x] 2.3 Implement provenance-driven uninstall cleanup in `internal/infra/install/linux/bootstrap.go`/`provenance.go`, stopping or disabling recorded services before path removal.

## Phase 3: CLI and Installer Wiring

- [x] 3.1 Modify `cmd/regixtry/main.go` to add `setup`, `uninstall`, and deferred `upgrade`, plus interactive mode selection and explicit non-TTY guidance.
- [x] 3.2 Wire `daemon-sqlite` setup so success requires installed binary, running service, and `/v2/` reachability; keep `binary-only` as guidance-only with no provenance write.
- [x] 3.3 Modify `install.sh` so it verifies/releases the binary only and never owns lifecycle mode selection or uninstall UX.

## Phase 4: Verification and Operator Docs

- [x] 4.1 Update `docs/verification/scripts/install-release-smoke.sh` to cover downloader-only install, `regixtry setup`, unsupported targets, and drift-aware `regixtry uninstall`.
- [x] 4.2 Update `README.md` with Linux+systemd-only lifecycle support, truthful `setup`/`uninstall` outcomes, and deferred `upgrade` wording.
- [x] 4.3 Verify with `gofmt -w .`, `go test ./...`, `go vet ./...`, and the install-release smoke script after all lifecycle tasks land.
