## Exploration: Separate base install config from optional feature config

### Current State
`regixtry setup` is currently carrying two responsibilities at once: base host bootstrap and optional Trivy capability configuration. The current Trivy enablement path is wired through setup/serve lifecycle state: `cmd/regixtry/main.go` exposes `--trivy-*` flags on `setup`, those values flow into `internal/infra/install/linux/bootstrap.go`, `templates.go`, and `provenance.go`, and `serve` later seeds runtime scan settings from that managed config.

That works, but it creates operator pain:
- Adding or changing an optional capability feels like re-installing the service instead of managing a feature.
- Upgrade currently reconstructs installed intent from lifecycle provenance + managed env (`internal/infra/install/linux/upgrade.go`, `intent.go`), so feature settings are treated like base installation truth and get replayed during lifecycle operations.
- Runtime scan settings already have a separate authority seam in SQLite, but only after first boot: `Service.EnsureScanSettings` seeds one row if missing, then `UpdateScanSettings` and `/admin/v1/scan-settings` become authoritative.
- There is no first-class CLI surface for feature management yet; the admin HTTP API exists for scan settings/runs, but the TUI client interface still exposes user/token/grant operations only.

### Affected Areas
- `cmd/regixtry/main.go` — setup and serve both carry `--trivy-*` flags; `runSetup` persists them through bootstrap provenance, and `newHandler` calls `EnsureScanSettings(...)` from serve startup.
- `internal/infra/install/linux/bootstrap.go` — `BootstrapConfig` currently mixes base install fields with Trivy feature fields.
- `internal/infra/install/linux/templates.go` — managed env rendering writes `REGISTRY_TRIVY_*` into the service env file, so optional feature config is currently treated as managed runtime bootstrap config.
- `internal/infra/install/linux/provenance.go` — lifecycle provenance stores Trivy values inside `LifecycleIntent`, so uninstall/upgrade treat them as install-owned truth.
- `internal/infra/install/linux/intent.go` and `upgrade.go` — upgrade rebuilds installed intent from provenance + env and then rewrites managed artifacts, which is the main constraint against clean feature independence.
- `internal/app/regixtry/service_scanning.go` — verified seam: `EnsureScanSettings` seeds defaults once, while `UpdateScanSettings` and `QueueManualScan` already operate from persisted runtime state.
- `internal/infra/metadata/sqlite/store.go` and `internal/ports/regixtry.go` — scan settings and run history already exist as runtime persistence, which is the strongest seam for future feature config.
- `internal/protocol/http/admin_handlers.go` — `/admin/v1/scan-settings` and `/admin/v1/scan-runs` already provide backend-authoritative mutation/read surfaces for one optional capability.
- `internal/tui/admin_client.go` — currently lacks scan settings/run methods, so the TUI is not yet a feature-config surface.
- `openspec/specs/installation-modes/spec.md` and `openspec/specs/lifecycle-cli/spec.md` — current specs frame setup/uninstall around truthful bootstrap and provenance-driven cleanup, so any new model must keep base lifecycle truthful while shrinking setup scope.

### Approaches
1. **Dedicated feature configuration layer** — Keep `setup` responsible only for base installation/bootstrap, and manage optional capabilities through feature-owned config persisted outside lifecycle bootstrap intent.
   - Pros: Clean separation of concerns; matches the existing SQLite/admin API seam; future capabilities can follow one model; upgrades stop conflating optional behavior with install truth.
   - Cons: Requires a new feature-management contract across CLI, runtime boot, and provenance boundaries; migration needs careful back-compat for existing Trivy installs.
   - Effort: Medium

2. **Keep setup as the entrypoint, but add setup sub-modes for features** — Continue routing optional capability config through lifecycle setup while making it look more modular.
   - Pros: Smaller code delta; reuses current setup/upgrade/provenance flow.
   - Cons: This does NOT solve the architectural problem; operators still have to revisit setup semantics, and upgrade/provenance still own feature config incorrectly.
   - Effort: Low

3. **Admin-API-only feature management** — Remove feature config from setup and rely only on admin HTTP/TUI surfaces for optional capabilities.
   - Pros: Very clean runtime authority model; fits existing scan-settings endpoints.
   - Cons: Weak bootstrap story for headless operators; no non-HTTP lifecycle surface; future capabilities would depend on the service already being reachable before initial enablement.
   - Effort: Medium

### Recommendation
Recommend **Approach 1: dedicated feature configuration layer**.

Recommended architecture:
- **Base install config** should own only what the service needs to exist and boot truthfully: binary/service paths, storage/database paths, network/public URL/TLS mode, service name, and auth backend/runtime essentials.
- **Feature config** should own optional capability state such as Trivy enablement, cadence, timeout, cache/binary path, and future feature-specific limits.
- **Runtime authority** for feature config should live in feature persistence (`scan_settings` today, analogous stores later), with app-service validation and admin API mutation.
- **CLI surface** should be feature-oriented, not plugin-oriented: for example `regixtry feature trivy show|configure|enable|disable` or an equivalent `regixtry features ...` namespace. The point is CAPABILITY management, not dynamic plugin loading.
- **Startup behavior** should seed feature defaults only when the feature has never been configured yet. That keeps first-boot convenience without letting setup remain the long-term owner.
- **Lifecycle provenance** should stop treating feature values as base install truth. If feature-managed files/resources ever need cleanup evidence, track them in a separate feature-lifecycle section rather than in base bootstrap intent.

Recommended migration from current Trivy-through-setup behavior:
- Keep existing `setup --trivy-*` behavior temporarily as a compatibility path for one transition slice.
- On startup or explicit migration, import legacy Trivy values from managed env/provenance into `scan_settings` only when no authoritative feature row exists.
- Mark `setup --trivy-*` as deprecated in docs/help once the feature CLI/admin path exists.
- Update upgrade so base lifecycle replay does not overwrite authoritative feature config after migration.
- After one compatibility cycle, remove Trivy feature ownership from setup/bootstrap intent and leave setup responsible only for base install defaults.

Recommended OpenSpec change name: **`separate-feature-config-from-setup`**.

### Risks
- Upgrade currently rebuilds managed runtime artifacts from installed intent, so leaving feature fields in that path will keep re-coupling base and feature config.
- If feature cleanup is not modeled separately, uninstall/provenance could become less truthful for future capabilities that create their own managed assets.
- A CLI feature surface must share the same validation and storage authority as the admin API, or the product will fork behavior across operators.

### Ready for Proposal
Yes — proceed with a proposal for `separate-feature-config-from-setup` centered on a base-lifecycle contract, a feature-configuration contract, a migration bridge for legacy Trivy setup flags, and a first-class capability CLI/admin model that keeps optional features independent from full setup.
