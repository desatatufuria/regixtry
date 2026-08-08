# Apply Progress: Regixtry Lifecycle CLI

## Mode
Standard

## Delivery
- Strategy: exception-ok (`size:exception` accepted)
- Current slice: Work Unit 3 — installer/docs/smoke alignment
- Boundary: downloader-only `install.sh`, smoke coverage for binary-owned lifecycle UX, and operator documentation alignment

## Completed Tasks
- [x] 1.1 Add failing `cmd/regixtry/main_test.go` cases for `setup`/`uninstall` routing, deferred `upgrade`, and no-TTY `setup` without `--mode`.
- [x] 1.2 Create failing `internal/infra/install/linux/provenance_test.go` coverage for missing provenance, drifted cleanup statuses, and installed-binary-last removal order.
- [x] 1.3 Extend failing `internal/infra/install/linux/bootstrap_test.go` cases for unsupported host/mode, `systemctl enable --now`, `disable --now`, and `/v2/` probe failures.
- [x] 1.4 Add failing `docs/verification/scripts/install-release-smoke.sh` assertions proving `install.sh` stays downloader-only, bad archives still fail, and drifted uninstall reports truthfully.
- [x] 2.1 Create `internal/infra/install/linux/provenance.go` with `LifecycleProvenance`, `CleanupItem`, load/save helpers, and uninstall report formatting.
- [x] 2.2 Modify `internal/infra/install/linux/bootstrap.go` to emit normalized managed paths, installed binary metadata, and service state needed by lifecycle provenance.
- [x] 2.3 Implement provenance-driven uninstall cleanup in `internal/infra/install/linux/bootstrap.go`/`provenance.go`, stopping or disabling recorded services before path removal.
- [x] 3.1 Modify `cmd/regixtry/main.go` to add `setup`, `uninstall`, and deferred `upgrade`, plus interactive mode selection and explicit non-TTY guidance.
- [x] 3.2 Wire `daemon-sqlite` setup so success requires installed binary, running service, and `/v2/` reachability; keep `binary-only` as guidance-only with no provenance write.
- [x] 3.3 Modify `install.sh` so it verifies/releases the binary only and never owns lifecycle mode selection or uninstall UX.
- [x] 4.1 Update `docs/verification/scripts/install-release-smoke.sh` to cover downloader-only install, `regixtry setup`, unsupported targets, and drift-aware `regixtry uninstall`.
- [x] 4.2 Update `README.md` with Linux+systemd-only lifecycle support, truthful `setup`/`uninstall` outcomes, and deferred `upgrade` wording.
- [x] 4.3 Verify with `gofmt -w .`, `go test ./...`, `go vet ./...`, and the install-release smoke script after all lifecycle tasks land.

## Work Unit Evidence
| Work Unit | Focused test command and exact result | Runtime harness command/scenario and exact result | Rollback boundary |
|---|---|---|---|
| 1 — Provenance + uninstall safety core | `go test ./internal/infra/install/linux` → exit 0, `ok   regixtry/internal/infra/install/linux 0.013s` | `N/A` — this slice is package-scoped backend logic; fake `systemctl` and readiness probe coverage in package tests are the runtime boundary for Work Unit 1. | `internal/infra/install/linux/{bootstrap.go,bootstrap_test.go,provenance.go,provenance_test.go}` |
| 2 — CLI lifecycle entrypoints + TTY rules | `go test ./cmd/regixtry` → exit 0, `ok   regixtry/cmd/regixtry (cached)` | `go run ./cmd/regixtry setup --mode binary-only` → exit 0, stdout: `Binary placement is complete, but setup is not yet complete.` and `To finish phase-1 setup on a supported Linux + systemd host, run: regixtry setup --mode daemon-sqlite --public-url <url>` | `cmd/regixtry/main.go`, `cmd/regixtry/main_test.go`, `internal/infra/install/linux/provenance.go` |
| 3 — Installer/docs/smoke alignment | `go test ./...` → exit 0; package results: `ok regixtry/cmd/regixtry (cached)`, `ok regixtry/internal/app/auth (cached)`, `ok regixtry/internal/app/regixtry (cached)`, `? regixtry/internal/domain/auth [no test files]`, `ok regixtry/internal/domain/regixtry (cached)`, `ok regixtry/internal/infra/auth/postgres (cached)`, `ok regixtry/internal/infra/install/linux 0.023s`, `ok regixtry/internal/infra/metadata/sqlite (cached)`, `ok regixtry/internal/infra/storage/fsblob (cached)`, `ok regixtry/internal/ports (cached)`, `ok regixtry/internal/protocol/http (cached)`, `ok regixtry/internal/tui (cached)` | `bash "docs/verification/scripts/install-release-smoke.sh"` → exit 0, stdout: `Installer release smoke scenarios passed. Root: /tmp/regixtry-install-smoke.HziXJo`; `go vet ./...` → exit 0; `gofmt -w .` → exit 0 | `install.sh`, `docs/verification/scripts/install-release-smoke.sh`, `README.md`, `openspec/changes/regixtry-lifecycle-cli/{tasks.md,apply-progress.md}` |

## Files Changed
- `cmd/regixtry/main.go` — added `setup`, `uninstall`, and deferred `upgrade` routing, non-TTY mode enforcement, interactive mode prompting, binary-only guidance, and provenance-backed setup/uninstall orchestration.
- `cmd/regixtry/main_test.go` — added CLI lifecycle routing, TTY gating, deferred upgrade, provenance-save rollback, and uninstall-report tests.
- `internal/infra/install/linux/provenance.go` — exported lifecycle provenance planning/path/save helpers so CLI setup can persist provenance without replaying uninstall flags.
- `install.sh` — reduced installer scope to verified binary download and placement, then printed binary-owned lifecycle next steps instead of owning mode selection or bootstrap rollback.
- `docs/verification/scripts/install-release-smoke.sh` — rebuilt smoke coverage around downloader-only install, binary-owned setup guidance, unsupported installer targets, deferred upgrade, and drift-aware uninstall reporting.
- `README.md` — rewrote release-install guidance around binary-owned lifecycle commands, Linux + systemd support truth, and provenance-driven uninstall.
- `openspec/changes/regixtry-lifecycle-cli/tasks.md` — marked cumulative Work Unit 2 and Work Unit 3 task completion.
- `openspec/changes/regixtry-lifecycle-cli/apply-progress.md` — recorded cumulative implementation progress and work-unit evidence.

## Deviations
- To avoid duplicating bootstrap path logic in `cmd/regixtry`, this slice exported minimal lifecycle provenance helpers from `internal/infra/install/linux/provenance.go`; the design mentioned backend exposure as a possible need, but did not name the exact exported helpers.
- The smoke script proves drift-aware uninstall on a non-systemd host by asserting the truthful uninstall report even when service disable fails; that keeps the runtime harness portable while still exercising provenance-driven cleanup.

## Remaining Tasks
- None.

## Status
13/13 tasks complete. Ready for verify.
