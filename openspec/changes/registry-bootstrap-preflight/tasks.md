# Tasks: Registry Bootstrap Preflight

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 220-340 |
| 1200-line budget risk | Low |
| 400-line budget risk | Low |
| Chained PRs recommended | No |
| Suggested split | Single PR |
| Delivery strategy | exception-ok |
| Chain strategy | pending |

Decision needed before apply: No
Chained PRs recommended: No
Chain strategy: pending
400-line budget risk: Low

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Add `--no-start` CLI/config contract | Single PR | `go test ./cmd/registry -run 'TestBootstrap'` | N/A — flag parsing is proven in package tests | `cmd/registry/main.go`, `cmd/registry/main_test.go` |
| 2 | Add bind preflight, recovery error, and control-flow split | Single PR | `go test ./internal/infra/install/linux -run TestBootstrap` | N/A — startup flow is stubbed in package tests without a real service | `internal/infra/install/linux/bootstrap.go`, `internal/infra/install/linux/bootstrap_test.go` |

## Phase 1: CLI Contract

- [x] 1.1 Add `NoStart bool` to `internal/infra/install/linux/bootstrap.go:BootstrapConfig` and keep validation compatible with artifact generation.
- [x] 1.2 Parse `--no-start` in `cmd/registry/main.go:parseBootstrapConfig` and pass it unchanged through `run(...)` to the bootstrap runner.
- [x] 1.3 RED: extend `cmd/registry/main_test.go` for `--no-start` parsing and runner config propagation before changing production code.

## Phase 2: Preflight Behavior

- [x] 2.1 RED: add `internal/infra/install/linux/bootstrap_test.go` cases for occupied local bind failure before `systemctl`, non-local bind bypass, and `NoStart` skipping preflight/startup.
- [x] 2.2 Implement local-bind classification and temporary `net.Listen("tcp", cfg.Addr)` preflight in `internal/infra/install/linux/bootstrap.go`.
- [x] 2.3 Add a typed recovery error in `internal/infra/install/linux/bootstrap.go` that renders `ss`, `systemctl stop`, and bootstrap rerun commands from config.

## Phase 3: Control-Flow Wiring

- [x] 3.1 Update `Bootstrapper.Run` in `internal/infra/install/linux/bootstrap.go` so artifacts write first, `NoStart` returns success, and occupied local binds fail before `daemon-reload`.
- [x] 3.2 Extend `cmd/registry/main_test.go` to assert CLI-visible occupied-bind failure text survives the existing top-level error path.
- [x] 3.3 Preserve default-start success and readiness-failure coverage in `internal/infra/install/linux/bootstrap_test.go` after the new branch split.

## Phase 4: Verification

- [x] 4.1 Run `go test ./cmd/registry ./internal/infra/install/linux` and fix regressions only in the touched bootstrap files.
- [x] 4.2 Run `go test ./...` to confirm the wider bootstrap flow still passes after the `installation-modes` contract change.
- [x] 4.3 Update this `openspec/changes/registry-bootstrap-preflight/tasks.md` checklist during apply; no extra docs file is required for this slice.
