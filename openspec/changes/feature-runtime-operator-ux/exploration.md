## Exploration: feature-runtime-operator-ux

### Current State
Operator pain is real today. `feature install` and `feature upgrade` finish with a final success line, but they do not stream progress, so long downloads/extracts look hung. `feature list` is still a raw tab-separated inventory emitted from `cmd/regixtry/main.go`, backed by `Service.ListFeatures(...)`, which currently returns only `Name`, `Kind`, `Enabled`, and `Configured`. `feature status` is better: it already projects runtime health, version, and rollback availability through `Service.GetFeatureStatus(...)` and `projectFeatureRuntime(...)`, but it does not report whether the installed runtime is already on the latest release.

The good news is the codebase already has strong seams. Managed runtime lifecycle actions exist behind `FeatureRuntimeManager` (`Install`, `Upgrade`, `Rollback`, `Status`) and are wired through CLI, app service, admin HTTP endpoints, and the Bubble Tea admin client. The TUI admin features screen is also already cursor-based and can refresh status, enable, disable, and even trigger install via `i`; but the current render/help text still looks like a read-mostly view and does not expose upgrade/rollback or render a useful summary table.

There is also a verified precedent for progress and latest-version awareness: `internal/infra/install/linux/upgrade.go` already resolves latest releases, computes `UpToDate`, emits preflight data, and reports staged progress callbacks. The feature runtime path in `internal/infra/scanning/trivy/runtime_manager.go` has similarly clear internal stages (`resolve`, download, verify, extract, activate, probe), but no reporting callback is surfaced yet.

### Affected Areas
- `cmd/regixtry/main.go` — owns `feature` CLI routing, current tab-separated `feature list` output, status rendering, and runtime action success messages; it also already contains a proven progress UX pattern for top-level `upgrade`.
- `cmd/regixtry/main_test.go` — already verifies `feature list`, `feature status`, managed runtime install/upgrade/rollback messaging, and top-level upgrade progress ordering.
- `internal/app/regixtry/feature_registry.go` — current projection seam for `ListFeatures`, `GetFeature`, `GetFeatureStatus`, runtime version, and rollback availability.
- `internal/app/regixtry/feature_runtime.go` / `internal/app/regixtry/service.go` — current runtime-manager interface seam that can grow a minimal read/update-progress contract.
- `internal/ports/regixtry.go` — `FeatureSummary`, `FeatureRuntime`, `FeatureDetails`, and `TrivyRuntimeState` define what CLI/TUI/admin can render today.
- `internal/infra/scanning/trivy/runtime_manager.go` — current managed runtime install/upgrade/rollback implementation with explicit lifecycle stages and release-resolution logic.
- `internal/protocol/http/admin_handlers.go` — existing `/admin/v1/features`, `/status`, `:install`, `:upgrade`, `:rollback`, `:enable`, and `:disable` endpoints that the TUI already reuses.
- `internal/tui/admin_client.go` — already exposes install/upgrade/rollback/enable/disable feature mutations over the admin API.
- `internal/tui/model.go` — current cursor-driven feature screen logic, including selection, refresh, enable/disable confirms, and install action wiring.
- `internal/tui/admin_views.go` — current feature screen rendering and help text, which still show a read-mostly list instead of an action-aware operator panel.

### Approaches
1. **Incremental seam reuse** — extend the existing feature DTOs and runtime manager with on-demand update metadata plus install/upgrade progress callbacks, then improve the current CLI and Bubble Tea surfaces.
   - Pros: Reuses verified seams across CLI, app, HTTP, and TUI; small blast radius; no new framework; fits strict TDD well.
   - Cons: Keeps the slice intentionally feature-specific for now; update checks stay on-demand instead of cached.
   - Effort: Medium

2. **Generic feature operations framework** — introduce a new cross-feature action engine, persistent operation model, and broader TUI workflow before improving the current `trivy` operator UX.
   - Pros: More abstract if many built-in features arrive later.
   - Cons: Overengineered for one built-in feature; larger review/test surface; delays operator value.
   - Effort: High

### Recommendation
Choose **Incremental seam reuse**.

Recommended minimal architecture:
- **Progress reporting**: mirror the existing lifecycle-upgrade pattern and add a tiny callback/event seam for managed runtime `install` and `upgrade`. Report coarse operator-relevant stages only: resolve, download, verify, extract, activate, probe, complete. CLI should print simple stage progress so the command never looks hung. TUI can stay thin in this slice and show start/end status text rather than full streaming progress.
- **Update checks**: add an on-demand read path that compares the installed runtime version to the latest resolvable release when operators explicitly run `feature list`, `feature status`, or refresh the feature screen. Project `current version`, `latest version`, and `update available` / `up to date` through `FeatureRuntime` and `FeatureSummary`; do not invent background polling.
- **Table/list rendering**: keep `feature list` as plain terminal output, but render a fixed-width table with useful columns such as `NAME`, `KIND`, `ENABLED`, `CONFIGURED`, `CURRENT`, `LATEST`, and `UPDATE`.
- **Thin TUI actions**: keep the existing Bubble Tea admin features screen and expose only simple cursor actions that map to existing endpoints: refresh, install, upgrade, enable, disable, rollback where state allows. Prefer conditional help text and visible state-based availability over new screens or workflows.

Explicit anti-overengineering guardrails:
- Do **not** add a new framework, job system, websocket, or background worker.
- Do **not** generalize beyond the current built-in `trivy` feature in this slice.
- Do **not** add background update daemons or automatic checks during normal server startup.
- Do **not** redesign the whole TUI; extend the current admin features screen only.
- Do **not** expand this exploration into setup/lifecycle ownership redesign that belongs to earlier changes.

Recommended OpenSpec change name: `feature-runtime-operator-ux`

### Risks
- Latest-version checks add a network dependency to list/status refresh, so failures must degrade to `unknown` instead of breaking feature visibility.
- Persisting update-availability metadata would unnecessarily widen the change; projection-on-read is safer for this slice.
- TUI actions will confuse operators if help text does not reflect which actions are actually available for the selected runtime state.
- If CLI progress formatting is copied instead of lightly shared, the UX could drift between top-level lifecycle upgrade and feature runtime operations.

### Ready for Proposal
Yes — propose a narrowly scoped operator UX change that reuses the existing feature service, admin API, runtime manager, and Bubble Tea screen to add progress, update awareness, a useful table view, and explicit cursor actions without introducing a new framework.
