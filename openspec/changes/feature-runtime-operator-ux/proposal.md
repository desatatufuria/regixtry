# Proposal: Feature Runtime Operator UX

## Intent

Make feature runtime operations feel trustworthy for operators: install/upgrade must show progress, list/status must expose latest-version awareness on demand, and the existing Bubble Tea feature screen must surface the actions operators already have without adding new orchestration layers.

## Scope

### In Scope
- Add coarse staged progress feedback for `feature install` and `feature upgrade`.
- Project `current`, `latest`, and update availability on demand in feature list/status reads.
- Replace raw `feature list` output with a fixed-width operator table.
- Expose thin cursor-based install/upgrade/rollback/enable/disable actions in the current TUI feature screen.

### Out of Scope
- New frameworks, daemons, background polling, websockets, or generic operation engines.
- Automatic update checks during startup or persistent update metadata.
- Broad TUI redesign beyond the current feature screen.

## Capabilities

### New Capabilities
- `feature-runtime-operations`: Operator-facing feature runtime progress, latest-version awareness, and table/status rendering across CLI, service, and admin API projections.

### Modified Capabilities
- `operator-admin-tui`: Extend the authenticated feature screen from read-mostly browsing to state-aware feature actions with contextual help.

## Approach

Reuse the existing managed runtime seam. Add a tiny progress callback for runtime install/upgrade, extend `FeatureSummary`/`FeatureRuntime` projections with on-demand latest-version metadata, and keep refresh-time resolution best-effort (`unknown` on network failure). Benchmark note: Harbor favors table-first operator inventory views, and GHCR keeps version handling explicit; this proposal follows that operator-visible, no-background-magic model.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `cmd/regixtry/main.go` | Modified | Progress lines, table rendering, status messaging |
| `internal/app/regixtry/feature_registry.go` | Modified | On-demand runtime/update projection |
| `internal/ports/regixtry.go` | Modified | Feature summary/runtime DTO expansion |
| `internal/infra/scanning/trivy/runtime_manager.go` | Modified | Install/upgrade stage callbacks and latest lookup reuse |
| `internal/tui/{model.go,admin_views.go,admin_client.go}` | Modified | Thin feature actions and contextual help |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Latest-version lookup slows refresh | Med | Resolve only on explicit reads; degrade to `unknown` |
| TUI actions misrepresent availability | Med | Drive help/actions from selected runtime state |
| Progress UX drifts from lifecycle upgrade | Low | Reuse the existing staged-progress pattern |

## Rollback Plan

Revert DTO/progress callback additions and restore current feature list/TUI rendering. Existing runtime install/upgrade/rollback endpoints remain unchanged, so rollback is code-only.

## Dependencies

- Existing managed runtime release-resolution path in `internal/infra/install/linux/upgrade.go`

## Success Criteria

- [ ] Operators see staged feedback during feature install and upgrade instead of a silent wait.
- [ ] `feature list`, `feature status`, and TUI refresh show current/latest/update state without requiring background services.
- [ ] The Bubble Tea feature screen exposes only valid cursor actions for the selected feature/runtime state.
