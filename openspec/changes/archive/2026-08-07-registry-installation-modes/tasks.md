# Tasks: Registry Installation Modes

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 850-1050 |
| 1200-line budget risk | Low |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | WU1 bootstrap core -> WU2 installer wiring -> WU3 smoke/docs |
| Delivery strategy | exception-ok |
| Chain strategy | stacked-to-main |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Add Linux bootstrap package, distro gating, templates, receipt, rollback | PR 1 | `go test ./internal/infra/install/linux ./cmd/registry -run 'Test(Bootstrap|Detect|Template)'` | `N/A - command-runner and probe stubs cover host actions here` | `internal/infra/install/linux/*`, bootstrap-only CLI plumbing |
| 2 | Wire `install.sh` to `registry bootstrap --mode daemon-sqlite` with quoted paths and checksum guard reuse | PR 2 | `go test ./cmd/registry -run 'TestRunBootstrap'` | `bash docs/verification/scripts/install-release-smoke.sh` | `install.sh`, bootstrap invocation flags, no binary-download rollback |
| 3 | Extend smoke coverage and operator docs for supported distros, reachability, and rollback limits | PR 3 | `go test ./...` | `bash docs/verification/scripts/install-release-smoke.sh --release-dist ./dist` | `docs/verification/scripts/install-release-smoke.sh`, `README.md` |

## Phase 1: Contract and RED Tests

- [x] 1.1 Add RED cases in `cmd/registry/main_test.go` for malformed `--mode` and space-containing bootstrap paths; expect early failure before artifact writes.
- [x] 1.2 Add RED cases in `cmd/registry/main_test.go` for unsupported `/etc/os-release`, missing `/run/systemd/system`, `systemctl enable --now` failure, and `/v2/` probe failure.
- [x] 1.3 Add RED cases in `docs/verification/scripts/install-release-smoke.sh` for wrong release asset, malformed archive, and missing `registry` entry while preserving manual-guidance failures.

## Phase 2: Bootstrap Foundation

- [x] 2.1 Create `internal/infra/install/linux/detect.go` for Linux-only distro/systemd detection covering Debian, Ubuntu, Linux Mint, RHEL 9.x, and RHEL 10.x; keep Alpine rejected.
- [x] 2.2 Create `internal/infra/install/linux/templates.go` to render `/etc/registry/registry.env` and `/etc/systemd/system/registry.service` from `serve` inputs.
- [x] 2.3 Create `internal/infra/install/linux/bootstrap.go` for plan/apply/receipt/rollback/readiness polling, writing `/etc/registry/bootstrap-state.json` and `/var/lib/registry/{content,metadata.db}`.
- [x] 2.4 Update `cmd/registry/main.go` to add `bootstrap` subcommand, `BootstrapConfig`, flag parsing, and bootstrap/rollback execution.

## Phase 3: Installer and Integration

- [x] 3.1 Update `install.sh` to parse bootstrap flags, preserve verified binary install, and invoke `registry bootstrap --mode daemon-sqlite` by default after install.
- [x] 3.2 Extend `cmd/registry/main_test.go` with integration-style command-runner and HTTP-probe stubs proving success, failed activation rollback, and binary retention.
- [x] 3.3 Extend `docs/verification/scripts/install-release-smoke.sh` to exercise daemon-sqlite success, unsupported distro failure, start failure, and rollback cleanup.

## Phase 4: Operator Documentation

- [x] 4.1 Update `README.md` with truthful bootstrap commands, supported distros, root/systemd assumptions, `/v2/` reachability checks, and rollback scope.
- [x] 4.2 Confirm docs/spec wording stays aligned with fixed slice inputs: Linux-only daemon+SQLite, default start enabled, minimum success = reachable service, binary kept on rollback.
