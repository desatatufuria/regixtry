# Apply Progress: Registry Bootstrap Preflight

## Mode

Standard

## Delivery

- Strategy: `exception-ok`
- Review budget: 1200 lines
- Actual authored diff scope: single implementation slice within the forecasted range; no `size:exception` was needed.

## Completed Tasks

- [x] 1.1 Add `NoStart bool` to `internal/infra/install/linux/bootstrap.go:BootstrapConfig` and keep validation compatible with artifact generation.
- [x] 1.2 Parse `--no-start` in `cmd/registry/main.go:parseBootstrapConfig` and pass it unchanged through `run(...)` to the bootstrap runner.
- [x] 1.3 RED: extend `cmd/registry/main_test.go` for `--no-start` parsing and runner config propagation before changing production code.
- [x] 2.1 RED: add `internal/infra/install/linux/bootstrap_test.go` cases for occupied local bind failure before `systemctl`, non-local bind bypass, and `NoStart` skipping preflight/startup.
- [x] 2.2 Implement local-bind classification and temporary `net.Listen("tcp", cfg.Addr)` preflight in `internal/infra/install/linux/bootstrap.go`.
- [x] 2.3 Add a typed recovery error in `internal/infra/install/linux/bootstrap.go` that renders `ss`, `systemctl stop`, and bootstrap rerun commands from config.
- [x] 3.1 Update `Bootstrapper.Run` in `internal/infra/install/linux/bootstrap.go` so artifacts write first, `NoStart` returns success, and occupied local binds fail before `daemon-reload`.
- [x] 3.2 Extend `cmd/registry/main_test.go` to assert CLI-visible occupied-bind failure text survives the existing top-level error path.
- [x] 3.3 Preserve default-start success and readiness-failure coverage in `internal/infra/install/linux/bootstrap_test.go` after the new branch split.
- [x] 4.1 Run `go test ./cmd/registry ./internal/infra/install/linux` and fix regressions only in the touched bootstrap files.
- [x] 4.2 Run `go test ./...` to confirm the wider bootstrap flow still passes after the `installation-modes` contract change.
- [x] 4.3 Update this `openspec/changes/registry-bootstrap-preflight/tasks.md` checklist during apply; no extra docs file is required for this slice.

## Work Unit Evidence

| Work Unit | Focused test command and exact result | Runtime harness command/scenario and exact result | Rollback boundary |
|---|---|---|---|
| 1 — `--no-start` CLI/config contract | `go test ./cmd/registry -run 'TestBootstrap'` → exit 0 (`ok   registry/cmd/registry 0.005s`) | `N/A` — flag parsing and runner wiring are proven in package tests; no separate runtime boundary exists for this unit. | Revert `cmd/registry/main.go` and `cmd/registry/main_test.go` to remove `--no-start` parsing and propagation without touching bootstrap runtime behavior. |
| 2 — local bind preflight and control-flow split | `go test ./internal/infra/install/linux -run TestBootstrap` → exit 0 (`ok   registry/internal/infra/install/linux 0.011s`) | `N/A` — bootstrap startup is stubbed in package tests and this slice does not introduce a real external systemd harness. | Revert `internal/infra/install/linux/bootstrap.go` and `internal/infra/install/linux/bootstrap_test.go` to remove preflight, recovery output, and artifact-only flow while preserving prior startup behavior. |
| 3 — touched-package regression pass | `go test ./cmd/registry ./internal/infra/install/linux` → exit 0 (`ok   registry/cmd/registry (cached)` / `ok   registry/internal/infra/install/linux (cached)`) | `N/A` — this verification step reuses package-level bootstrap harnesses only. | Reverting the four touched source/test files removes this slice without affecting unrelated packages. |
| 4 — wider regression pass | `go test ./...` → exit 0 (`ok   registry/cmd/registry 0.934s`; `ok   registry/internal/infra/install/linux 0.010s`; remaining packages cached or passing) | `N/A` — the broader verification remained at repository package-test scope; no Docker/systemd smoke harness was required for this change. | No extra rollback beyond the touched bootstrap CLI/runtime files. |

## Files Changed

| File | Action | Notes |
|---|---|---|
| `cmd/registry/main.go` | Modified | Added `--no-start` flag parsing for bootstrap. |
| `cmd/registry/main_test.go` | Modified | Added no-start parsing coverage, propagation assertions, and CLI-visible occupied-bind guidance coverage. |
| `internal/infra/install/linux/bootstrap.go` | Modified | Added `NoStart`, local bind preflight, and typed recovery guidance before service start. |
| `internal/infra/install/linux/bootstrap_test.go` | Modified | Added no-start, occupied-local-bind, and non-local-bypass coverage while preserving success/failure path assertions. |

## Deviations

None — implementation matches the planned slice. The design's open question was resolved by deriving the recovery example port from the occupied port plus one so the rerun command stays concrete.

## Issues

None.
