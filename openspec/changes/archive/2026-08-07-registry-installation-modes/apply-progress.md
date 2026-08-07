# Apply Progress: Registry Installation Modes

## Change
- Name: `registry-installation-modes`
- Mode: Standard
- Delivery strategy: `exception-ok`
- Chain strategy: `stacked-to-main`
- Current work unit: `WU3 smoke/docs close-out`
- Review boundary: `docs/verification/scripts/install-release-smoke.sh`, `README.md`, and installation-mode spec wording alignment for the daemon+SQLite slice
- Size exception: accepted by maintainer input for the overall change plan; this batch stayed inside the stacked WU3 slice.

## Completed Tasks
- [x] 1.1 Add RED cases in `cmd/registry/main_test.go` for malformed `--mode` and space-containing bootstrap paths; expect early failure before artifact writes.
- [x] 1.2 Add RED cases in `cmd/registry/main_test.go` for unsupported `/etc/os-release`, missing `/run/systemd/system`, `systemctl enable --now` failure, and `/v2/` probe failure.
- [x] 2.1 Create `internal/infra/install/linux/detect.go` for Linux-only distro/systemd detection covering Debian, Ubuntu, Linux Mint, RHEL 9.x, and RHEL 10.x; keep Alpine rejected.
- [x] 2.2 Create `internal/infra/install/linux/templates.go` to render `/etc/registry/registry.env` and `/etc/systemd/system/registry.service` from `serve` inputs.
- [x] 2.3 Create `internal/infra/install/linux/bootstrap.go` for plan/apply/receipt/rollback/readiness polling, writing `/etc/registry/bootstrap-state.json` and `/var/lib/registry/{content,metadata.db}`.
- [x] 2.4 Update `cmd/registry/main.go` to add `bootstrap` subcommand, `BootstrapConfig`, flag parsing, and bootstrap/rollback execution.
- [x] 3.1 Update `install.sh` to parse bootstrap flags, preserve verified binary install, and invoke `registry bootstrap --mode daemon-sqlite` by default after install.
- [x] 3.2 Extend `cmd/registry/main_test.go` with integration-style command-runner and HTTP-probe stubs proving success, failed activation rollback, and binary retention.
- [x] 1.3 Add RED cases in `docs/verification/scripts/install-release-smoke.sh` for wrong release asset, malformed archive, and missing `registry` entry while preserving manual-guidance failures.
- [x] 3.3 Extend `docs/verification/scripts/install-release-smoke.sh` to exercise daemon-sqlite success, unsupported distro failure, start failure, and rollback cleanup.
- [x] 4.1 Update `README.md` with truthful bootstrap commands, supported distros, root/systemd assumptions, `/v2/` reachability checks, and rollback scope.
- [x] 4.2 Confirm docs/spec wording stays aligned with fixed slice inputs: Linux-only daemon+SQLite, default start enabled, minimum success = reachable service, binary kept on rollback.

## Work Unit Evidence
| Evidence | Required value |
|---|---|
| Focused test command and exact result | `go test ./...` -> exit 0; package results: `ok registry/cmd/registry (cached)`, `ok registry/internal/app/auth (cached)`, `ok registry/internal/app/registry (cached)`, `ok registry/internal/infra/auth/postgres (cached)`, `ok registry/internal/infra/install/linux (cached)`, `ok registry/internal/infra/metadata/sqlite (cached)`, `ok registry/internal/infra/storage/fsblob (cached)`, `ok registry/internal/ports (cached)`, `ok registry/internal/protocol/http (cached)`, `ok registry/internal/tui (cached)`, plus `registry/internal/domain/auth [no test files]`. |
| Runtime harness command/scenario and exact result | `bash docs/verification/scripts/install-release-smoke.sh --release-dist ./dist` -> exit 0; `Installer release smoke scenarios passed. Root: /tmp/registry-install-smoke.vN3SpM` after exercising bootstrap success, unsupported distro failure, start failure, rollback cleanup, and archive/manual-guidance failures. |
| Rollback boundary | Revert `docs/verification/scripts/install-release-smoke.sh`, `README.md`, and `openspec/changes/registry-installation-modes/specs/installation-modes/spec.md` without touching `install.sh`, `cmd/registry/*`, or `internal/infra/install/linux/*`. |

## Remaining Tasks
- None.

## Focused Remediation: verify success evidence
- [x] Added a passing runtime test for `internal/infra/install/linux.Bootstrapper.Run` on a supported Linux host.
- [x] Proved the readiness-success contract accepts `/v2/` HTTP `401` after `systemctl enable --now` and keeps generated artifacts/receipt in place.

### Remediation Evidence
| Evidence | Required value |
|---|---|
| Focused test command and exact result | `go test ./internal/infra/install/linux -run 'TestBootstrapRun(ReportsSuccessAfterActivationAndReadiness|WritesArtifactsAndRollsBackOnProbeFailure)|TestBootstrapRollbackRemovesGeneratedArtifacts' -count=1` -> exit 0; `ok  registry/internal/infra/install/linux  0.009s`. |
| Runtime harness command/scenario and exact result | `go test ./internal/infra/install/linux -run 'TestBootstrapRunReportsSuccessAfterActivationAndReadiness' -count=1` -> exit 0; proves the real `Bootstrapper.Run` success path on temp filesystem roots, successful `systemctl` stubs, `/v2/` probe normalization, HTTP `401` readiness acceptance, and receipt/artifact persistence after activation. |
| Rollback boundary | Revert `internal/infra/install/linux/bootstrap_test.go` and this remediation note in `openspec/changes/registry-installation-modes/apply-progress.md`; no production code paths changed. |

## Notes
- The implementation enforces the truthful Linux-only support set for host bootstrap: Debian, Ubuntu, Linux Mint, RHEL 9.x, and RHEL 10.x.
- Alpine host bootstrap remains rejected and deferred.
- The OpenSpec installation-mode spec wording now matches the fixed slice boundary and no longer claims Alpine host bootstrap support.
- `install.sh` now defaults to `daemon-sqlite` bootstrap with `http://127.0.0.1:5000` as the fallback public URL while preserving the verified binary on bootstrap failure.
- The smoke harness now keeps release-archive/manual-guidance checks separate from daemon+SQLite bootstrap shell coverage by serving dedicated bootstrap-capable fixture archives during script execution.
- Verification gap narrowed without expanding production scope: success evidence now comes from the real Go bootstrapper instead of only the installer smoke stub path.
