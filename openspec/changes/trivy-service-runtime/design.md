# Design: Trivy Service Runtime

## Technical Approach

Keep `trivy` as the feature name and existing authority seam (`scan_settings` via service/admin/CLI/TUI), but replace local-binary runtime semantics with a service-backed contract. The design converts runtime-specific DTOs, persistence, health checks, and scan execution to HTTP/TLS-aware behavior while preserving enablement, scheduling, timeout, and concurrency semantics from the proposal.

## Architecture Decisions

| Decision | Choice | Alternatives considered | Rationale |
|---|---|---|---|
| Config DTO shape | Replace `cache_dir`/`binary_path` with `service_url`, `registry_address`, `auth_token`, `tls_ca_cert_path`, `tls_insecure_skip_verify` on `ScanSettings`, `FeatureDetails`, and `FeatureConfigureInput` | Keep binary fields; dual binary/service DTOs | One canonical service contract avoids long-term dual-runtime ambiguity. |
| Feature identity | Keep feature name `trivy` and `FeatureKindBuiltin`; add runtime mode/detail inside `FeatureRuntime` | Reclassify feature kind to `external_service` | `FeatureKind` already models inventory ownership, not transport. This preserves list/admin/TUI semantics while exposing runtime truth. |
| Auth model | Support persisted static bearer token initially; redact it from reads/status and CLI/TUI output | Env-only secret refs; arbitrary header map | Write-only bearer token keeps one authority seam now. Broader secret indirection can come later without blocking service runtime. |
| Persistence migration | Additive SQLite migration on `scan_settings`; service fields become authoritative, legacy binary fields become ignored bridge data for one release | New table; destructive schema swap | This repo has no migration framework yet. Additive columns minimize rollout risk and keep upgrades reversible. |
| Execution path | Replace local `exec.CommandContext` runner with HTTP-backed Trivy service adapter; reuse same adapter for `/healthz` and `/version` probes | Keep shelling out with `trivy image --server`; dual runner modes | The change goal is service-native runtime, not local client indirection. One HTTP transport centralizes auth/TLS behavior and removes host binary ownership. |

## Data Flow

```text
feature configure/setup import
  -> Service.NormalizeScanSettings
  -> SQLite scan_settings (service fields authoritative)
  -> GetFeatureStatus -> HTTP probe /healthz -> GET /version
manual/scheduled scan
  -> scanTarget(registry_address, repository, digest)
  -> TrivyServiceRunner.Run(service_url, auth, tls, target)
  -> ScanResult persisted as today
```

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/ports/regixtry.go` | Modify | Redefine scan/feature DTOs around service fields and runtime metadata. |
| `internal/app/regixtry/service_scanning.go` | Modify | Validate service config, build registry-reachable scan target, preserve schedule semantics. |
| `internal/app/regixtry/feature_registry.go` | Modify | Project service-backed status and legacy-import warnings instead of `exec.LookPath`. |
| `internal/infra/scanning/trivy/runner.go` | Replace | Convert local exec adapter into HTTP service runner/probe transport. |
| `internal/infra/metadata/sqlite/store.go` | Modify | Add service columns and one-way legacy bridge reads/writes. |
| `cmd/regixtry/main.go` | Modify | Rename flags/env wiring, seed defaults, and keep deprecated setup bridge. |
| `internal/tui/admin_views.go` / `internal/tui/admin_client.go` | Modify | Show service fields/runtime state; keep TUI mutations to enable/disable. |
| `*_test.go` across app/CLI/TUI/sqlite | Modify | Add RED coverage for migration, validation, probe, and execution boundaries. |

## Interfaces / Contracts

```go
type ScanSettings struct {
    Enabled, ScheduleEnabled bool
    Interval, Timeout        time.Duration
    MaxConcurrency           int
    ServiceURL               string `json:"service_url"`
    RegistryAddress          string `json:"registry_address"`
    AuthToken                string `json:"-"`
    TLSCACertPath            string `json:"tls_ca_cert_path,omitempty"`
    TLSInsecureSkipVerify    bool   `json:"tls_insecure_skip_verify,omitempty"`
    UpdatedAt                time.Time `json:"updated_at,omitempty"`
}
```

`FeatureRuntime` gains `Mode`, `Health`, `Version`, and `Detail`. `auth_token` is accepted on configure requests but never returned.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | DTO validation, legacy import normalization, registry target building, TLS/auth request shaping | New table-driven tests in `internal/app/regixtry/service_test.go` and Trivy runner tests. |
| Integration | SQLite additive migration and admin JSON projection/redaction | Store tests plus admin client/router tests with `httptest`. |
| E2E | CLI/setup migration messaging and service-backed feature status | Extend `cmd/regixtry/main_test.go`; full `go test ./...` remains verify gate. |

## Threat Matrix

| Boundary | Applicability | Design response | Planned RED tests |
|---|---|---|---|
| Documentation-like paths | Applicable — legacy `binary_path` may contain arbitrary path-like input during bridge | Never execute or validate imported binary paths; treat them only as deprecated migration evidence and require service fields for runtime readiness | Import `README.sh`/similar legacy path and assert no subprocess execution plus `unconfigured` runtime |
| Git repository selection | N/A — no git/process cwd behavior | None | None |
| Commit state | N/A — no VCS write path | None | None |
| Push state | N/A — no push/ref behavior | None | None |
| PR commands | N/A — no PR automation | None | None |

## Migration / Rollout

Release N adds service fields and deprecated `--trivy-service-*`/`--trivy-registry-address` configuration as the primary path. Legacy binary-backed setup/serve inputs are accepted only for one-way import of shared knobs (`enabled`, schedule, timeout, interval, concurrency) and produce an operator warning that `service_url` plus `registry_address` are still required. Safe defaults: feature disabled by default, schedule disabled, TLS verification on, auth optional, `/healthz` required for ready state, `/version` best-effort detail, and no fallback to local exec. Release N+1 removes binary-style flags from normal docs/UX and may delete legacy columns once migration coverage is proven.

## Open Questions

- [ ] None blocking.
