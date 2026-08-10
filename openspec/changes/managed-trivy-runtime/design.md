# Design: Managed Trivy Runtime

## Technical Approach

Replace the HTTP `trivy-service-runtime` with a Regixtry-managed Trivy runtime while keeping `scan_settings` for feature intent only. Add a dedicated runtime state record plus managed filesystem layout under the storage root, execute scans through the active managed binary, and surface lifecycle/status through existing feature CLI/admin/TUI seams.

## Architecture Decisions

| Decision | Alternatives considered | Choice / rationale |
|---|---|---|
| Runtime layout | Reuse arbitrary `binary_path`; single mutable binary | Use `${storageRoot}/features/trivy/{bin/versions/<version>/trivy,bin/active,trivy-cache,downloads,receipts}`. Side-by-side versions plus atomic `active` switch give rollback safety and avoid trusting operator-supplied executables. |
| Persistence split | Keep all state in `scan_settings`; store only files on disk | Keep `scan_settings` for intent (`enabled`, schedule, timeout, concurrency, reachable registry) and add `trivy_runtime_state` for active/previous version, receipt, health, timestamps, and last error. This keeps feature intent stable across install/rollback events. |
| Download + verification | Direct download to active path; no retained evidence | Stage into `downloads/`, verify official checksum asset before extract/activate, persist a receipt with asset URLs and SHA256 evidence, then rename/symlink-swap atomically. Activation failure restores the previous pointer and leaves the prior receipt active. |
| Upgrade/rollback policy | Auto-rollback after any degraded probe; delete previous version immediately | Auto-revert only before activation completes. After a successful switch, degraded health becomes explicit status and rollback remains operator-invoked, avoiding surprise reversions during inline scheduled batches. |
| Legacy migration | Keep `external_service` working; execute legacy `binary_path` | Service-backed settings become `migration-required` until managed install completes. Legacy `binary_path`/`cache_dir` are read only as migration evidence; Regixtry never executes arbitrary legacy paths. |

## Data Flow

```text
feature install/upgrade/rollback
  -> runtime manager
  -> download + verify + extract
  -> atomic active-pointer swap
  -> persist trivy_runtime_state + receipt

manual/scheduled scan
  -> GetScanSettings + GetTrivyRuntimeState
  -> resolve active binary + managed cache
  -> exec trivy image --format json <target>
  -> persist ScanRun result/version/db timestamp
```

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/ports/regixtry.go` | Modify | Add runtime-state contracts and richer managed runtime projection. |
| `internal/app/regixtry/{feature_registry.go,service_scanning.go}` | Modify | Separate intent from runtime state, project migration/runtime status, and execute scans via managed runtime resolver. |
| `internal/infra/scanning/trivy/runner.go` | Modify | Replace HTTP calls with managed binary execution and health probing. |
| `internal/infra/scanning/trivy/runtime_manager.go` | Create | Install/upgrade/rollback/status orchestration, receipt writing, and atomic activation. |
| `internal/infra/scanning/trivy/releases.go` | Create | Trivy release resolution, checksum verification, and extraction helpers. |
| `internal/infra/metadata/sqlite/store.go` | Modify | Add `trivy_runtime_state` table/methods and legacy migration reads. |
| `cmd/regixtry/main.go` | Modify | Extend `feature` CLI with managed runtime install/upgrade/rollback/status actions. |
| `internal/protocol/http/admin_handlers.go` / `internal/tui/*` | Modify | Expose runtime actions/status in admin API and feature screen. |

## Interfaces / Contracts

```go
type TrivyRuntimeState struct {
    Status string // uninstalled|installing|ready|degraded|migration-required
    ActiveVersion string
    PreviousVersion string
    ActiveBinaryPath string
    CacheDir string
    ReceiptPath string
    LastVerifiedAt *time.Time
    LastHealthCheckAt *time.Time
    LastDBUpdatedAt *time.Time
    LastError string
}
```

`FeatureRuntime` should project managed truth (`mode=managed`, health, version, detail, rollback availability) from this state instead of from `service_url`.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit (RED first) | Migration projection, state transitions, checksum/receipt parsing, active-pointer rollback | Table-driven tests with temp dirs and fake release client / command runner. |
| Integration | SQLite runtime-state persistence, CLI feature runtime flows, admin handlers | `t.TempDir()`, reopened DB assertions, HTTP handler tests, command-facing tests in `cmd/regixtry/main_test.go`. |
| E2E | Managed install then scheduled/manual scan against active binary | Extend smoke coverage after unit/integration pass; keep `go test ./...` green first per strict TDD. |

## Threat Matrix

| Boundary | Minimum adversarial cases | Applicability | Design response | Planned RED tests |
|---|---|---|---|---|
| Documentation-like paths | `requirements.txt`, `CMakeLists.txt`, executable Markdown/MDX, `README.sh` | Applicable: legacy `binary_path` can point at non-binaries | Never execute legacy/operator-provided paths; only execute verified managed binary under Regixtry-owned layout | Store migration test with `/tmp/README.sh`; runner must ignore it and report migration-required until install |
| Git repository selection | `git -C`, relative paths, absolute paths | N/A: no git selection | None | None |
| Commit state | staged, `commit -a`, empty index | N/A: no commit automation | None | None |
| Push state | tracking branch, first push, explicit refspec | N/A: no push automation | None | None |
| PR commands | explicit `--head`, environment prefix, composed commands | N/A: no PR automation | None | None |

## Migration / Rollout

Add SQLite migration for `trivy_runtime_state` without removing legacy `scan_settings` columns yet. Existing service-backed rows remain readable but status becomes `migration-required` until a managed install succeeds. Older binary-backed rows preserve shared intent knobs only; managed installs always write new private cache/binary paths and keep the previous active version for rollback.

## Open Questions

- [ ] Should checksum verification be sufficient for v1, or must signature verification block activation when Trivy publishes a stable signed asset set?
