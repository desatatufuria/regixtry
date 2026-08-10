## Exploration: Regixtry-managed private Trivy runtime

### Current State
`optional-trivy-rescans` already created the durable scan subsystem that matters: `scan_settings`, `scan_runs`, `scan_scheduler_state`, digest-based manual/scheduled queueing, and the bounded scheduler loop. `separate-feature-config-from-setup` then moved Trivy authority behind the generic `trivy` feature seam (`feature list|show|status|configure`, admin feature routes, and TUI projections) instead of keeping setup as the long-term owner. `trivy-service-runtime` reused those seams but changed the runtime contract to `external_service` with `service_url`, `registry_reachable_url`, auth/TLS settings, `/healthz` + `/version` probes, and an assumed `POST /scan` API.

That last assumption is the break. Real Trivy client/server mode is documented as `trivy image --server ...`, and Trivy’s own client uses a Twirp RPC path rather than a public `POST /scan` endpoint. We already verified in production-like testing that `/scan` returns 404 while `/healthz` and `/version` work. So the current problem is not a small bug in the transport; it is that Regixtry modeled Trivy as an external HTTP scanner product surface when Trivy’s real product surface is still a Trivy-controlled client/runtime.

For Regixtry, a managed private runtime fits better because the product already owns bounded scan orchestration, lifecycle UX, and operator-facing feature management, while operators want Trivy install/upgrade behavior to stay inside Regixtry rather than leaking into host PATH or a separately operated scanner service.

### Affected Areas
- `internal/app/regixtry/feature_registry.go` — keeps the reusable `trivy` feature identity and shared status/config projection, but currently reports `external_service` health from `service_url`-based probing.
- `internal/app/regixtry/service_scanning.go` — already owns manual queueing, scheduled batches, digest resolution, concurrency gates, and scan execution handoff; this is the main reuse seam for managed runtime execution.
- `internal/app/scanning/scheduler.go` — already provides lease-based periodic execution and should remain the batch trigger unchanged in shape.
- `internal/infra/scanning/trivy/runner.go` — should be superseded from HTTP transport to managed-binary execution/probe behavior.
- `internal/ports/regixtry.go` — currently mixes operator config with runtime/service fields; it needs a cleaner split between feature intent and managed runtime state.
- `internal/infra/metadata/sqlite/store.go` — already persists scan settings/runs/scheduler state and is the right place for new managed-runtime metadata persistence.
- `cmd/regixtry/main.go` — already wires feature commands, setup migration hooks, scheduler startup, and upgrade/bootstrap runners; it is the right CLI/lifecycle seam for `feature install|upgrade|rollback trivy`.
- `internal/protocol/http/admin_handlers.go` — already exposes authoritative scan-settings and manual scan routes; can extend status/install actions without inventing a new authority seam.
- `internal/tui/admin_client.go` and `internal/tui/admin_views.go` — already consume generic feature/status APIs, so TUI can inherit managed-runtime state with minimal structural change.
- `internal/infra/install/linux/{bootstrap.go,provenance.go,upgrade.go}` — base setup should stay feature-light, but the existing download/provenance/rollback patterns are reusable for a Trivy-managed artifact lifecycle.

### Approaches
1. **Repair the external HTTP service model** — Keep `trivy-service-runtime`, but replace `POST /scan` with Trivy’s real client/server RPC contract.
   - Pros: Reuses most of the current service-runtime code and preserves service_url/auth/TLS fields.
   - Cons: Regixtry would still own a fragile imitation of Trivy’s client behavior, still needs a separate service operator model, and still does not solve the user’s real product requirement of private Regixtry-managed runtime ownership.
   - Effort: Medium

2. **Support dual steady-state runtimes** — Keep both `external_service` and `managed_binary` as first-class long-term modes.
   - Pros: Maximum compatibility and easiest migration story for experiments already done with service mode.
   - Cons: Doubles validation, docs, tests, status semantics, upgrade rules, and operator confusion. This is architecture bloat when the product direction is already clear.
   - Effort: High

3. **Make Trivy a Regixtry-managed private runtime** — Regixtry installs, verifies, upgrades, rolls back, and executes its own Trivy binary and shared cache/runtime for internal use only.
   - Pros: Matches the product goal, reuses the existing scan orchestration seams, avoids host PATH drift, avoids reverse-engineering Trivy’s service API, and keeps operator UX inside `regixtry feature ...`.
   - Cons: Requires binary download/verification, managed filesystem layout, runtime-state persistence, and lifecycle-safe upgrades.
   - Effort: Medium

### Recommendation
Choose **Approach 3: Regixtry-managed private Trivy runtime**.

Why this fits better:
- The product already centralizes scan intent and orchestration inside Regixtry; the missing piece is managed runtime ownership, not a new remote transport.
- Trivy’s documented client/server contract still expects Trivy-aware client behavior, so an “external HTTP service” abstraction is the wrong product boundary for this repository.
- The user explicitly wants installable/upgradable private runtime behavior through feature commands. That aligns with Regixtry-managed artifacts, not host-global binaries and not a separately operated HTTP scanner.

Recommended architecture:
- **Keep `scan_settings` as operator intent only**: `enabled`, `schedule_enabled`, `interval`, `timeout`, and `max_concurrency` stay authoritative for feature behavior.
- **Move runtime ownership into dedicated managed-runtime state**: add a separate persisted Trivy runtime record (for example `trivy_runtime_state`) instead of continuing to overload `scan_settings` with transport fields.
- **Install path**: store the private runtime under the Regixtry storage root, not host PATH. Recommended layout:
  - `<storageRoot>/features/trivy/bin/<version>/trivy`
  - `<storageRoot>/features/trivy/bin/current` (symlink or atomically switched pointer)
  - `<storageRoot>/features/trivy/cache/`
  - `<storageRoot>/features/trivy/downloads/`
  - `<storageRoot>/features/trivy/receipts/`
- **Versioning**: persist `desired_version`, `installed_version`, `previous_version`, install source URL/asset, installed path, installed_at, and verification evidence.
- **Checksum/signature verification**: download official Trivy release assets plus checksum/signature material, verify before activation, and fail closed on mismatch. Reuse the existing Regixtry lifecycle download/provenance pattern where possible, but adapt it to third-party Trivy release metadata.
- **Health/status**: `feature status trivy` should report `not_installed | installing | ready | degraded | rollback_available`, current binary version, cache path, last DB freshness evidence, last verification time, and last install/upgrade error if any.
- **Upgrade/rollback**: install new versions side-by-side, verify them first, then atomically switch `current`. Keep the previous version for one-command rollback until cleanup. Never overwrite the active binary in place.
- **Lifecycle commands**: add feature-owned commands such as `regixtry feature install trivy`, `regixtry feature upgrade trivy [--version ...]`, and `regixtry feature rollback trivy`. Base `setup` remains base-only; `upgrade --all` may delegate to feature upgrades, but feature ownership should remain explicit.

How manual and batch scanning fit:
- **Manual scans** reuse `QueueManualScan(...)`, digest resolution, active-run dedupe, and `scan_runs` persistence exactly as they exist today.
- **Periodic batches** reuse `Scheduler.Run()` and `RunScheduledScans()` exactly as they exist today.
- **Execution changes only at the runner boundary**: `executeScanRun(...)` should keep calling `scanRunner.Run(...)`, but the runner should execute the currently installed managed binary with the private cache/runtime paths rather than calling an HTTP service.
- **Status evidence stays unified**: both manual and scheduled runs should continue persisting Trivy version, DB freshness, counts, and errors in `scan_runs`.

What to supersede vs reuse from `trivy-service-runtime`:
- **Reuse**
  - Built-in feature identity and routing in `internal/app/regixtry/feature_registry.go`
  - `scan_settings` as the feature authority seam for enable/schedule/timeout/concurrency
  - `QueueManualScan`, `RunScheduledScans`, digest target resolution, active-run dedupe, and `scan_runs` persistence in `internal/app/regixtry/service_scanning.go`
  - Lease-based scheduler in `internal/app/scanning/scheduler.go`
  - Admin/TUI/CLI feature surfaces already projecting through shared service methods
  - Lifecycle download/provenance/rollback patterns in `internal/infra/install/linux/*`
- **Supersede**
  - `external_service` as the Trivy steady-state runtime model
  - `service_url`, `registry_reachable_url`, `auth_token`, `tls_ca_cert_path`, and `tls_insecure_skip_verify` as primary Trivy configuration fields
  - HTTP runtime probing in `probeFeatureRuntime(...)`
  - HTTP `/healthz` + `/version` + assumed `POST /scan` transport in `internal/infra/scanning/trivy/runner.go`
  - Service-oriented docs/spec language introduced by `trivy-service-runtime`

Recommended OpenSpec change name: **`managed-trivy-runtime`**.

### Risks
- Secure third-party binary management is real operational complexity; checksum/signature verification and atomic activation cannot be hand-waved.
- Shared cache/database behavior during concurrent scans and upgrades needs explicit locking or upgrade guards to avoid corrupting runtime state.
- Migration must be truthful: existing `trivy-service-runtime` config should not silently appear supported if Regixtry now expects managed runtime ownership instead.
- If runtime state is mixed back into `scan_settings`, the project will recreate the same config/lifecycle coupling that `separate-feature-config-from-setup` already fixed.

### Ready for Proposal
Yes — the reusable seams are already implemented and verified. The proposal should explicitly supersede `trivy-service-runtime`, preserve the existing feature/config/scan/scheduler/admin/TUI seams, and scope the new work around private runtime installation, verification, status, upgrade, rollback, and managed-binary execution.
