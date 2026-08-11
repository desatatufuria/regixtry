# Proposal: Trivy TUI Runtime Alert Tabs

## Intent

Deliver the smallest Trivy TUI UX slice that reduces the current wall-of-text feature screen. Operators need fast separation between runtime operations and repository alert review without redesigning the whole feature framework.

## Scope

### In Scope
- Add two Trivy-only tabs in the admin TUI: `Runtime` and `Repository Alerts`.
- Move current Trivy configuration editing into a modal backed only by existing `FeatureConfigureInput` fields.
- Add basic repository alert navigation and drill-down using existing scan-run data and the current `/admin/v1/scan-runs` route.

### Out of Scope
- Persisted per-vulnerability detail expansion, rich CVE browsing, or cross-screen workflows.
- Exclusions, allowlists, policy engines, or approval systems.
- Generalizing tabs into a reusable backend schema before a second feature proves the need.

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `operator-admin-tui`: extend the admin feature experience so the Trivy page supports two focused tabs, modal-based configuration, and repository-alert drill-down from existing scan-run records.

## Approach

Keep the generic feature-manager shell and localize complexity to Trivy. Use `GetFeaturePage("trivy")` for runtime summary/actions, add a thin `ListScanRuns` admin-client seam for alert navigation, and store Trivy-only tab/modal/selection state in `AdminViewState`. Benchmark note: Harbor and GitLab both separate scan status summaries from deeper vulnerability/result views; this slice follows that operator-first split without copying their broader policy/report systems. Roadmap note: keep CI-first scanning and later registry/rescan enrichment explicitly deferred.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/tui/{model.go,admin_views.go,session.go}` | Modified | Add Trivy tab state, modal flow, and alert selection/drill-down UX. |
| `internal/tui/admin_client.go` | Modified | Expose scan-run lookup for repository filtering and detail fetch. |
| `internal/app/regixtry/feature_registry.go` | Modified | Keep Trivy page assembly aligned with the narrower runtime tab. |
| `openspec/specs/operator-admin-tui/spec.md` | Modified | Capture the new Trivy navigation behavior. |
| `docs/tui.md` | Modified | Document the Trivy screen split and deferred items. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Alert UX becomes brittle by parsing `FeatureRow` text | Med | Drive repository alerts from `ScanRun` data only. |
| Scope drifts into policy/detail systems | Med | Reject fields and workflows not already backed by current APIs/data. |
| Review size grows through abstraction | Low | Keep all new state Trivy-specific for this slice. |

## Rollback Plan

Revert the Trivy tab/modal state, remove scan-run lookup from the admin client, and restore the current linear feature-page rendering.

## Dependencies

- Existing `operator-admin-tui` spec and current `/admin/v1/features` plus `/admin/v1/scan-runs` routes.
- Strict TDD follow-on phases using `go test ./...`.

## Success Criteria

- [ ] Operators can switch between `Runtime` and `Repository Alerts` on the Trivy screen.
- [ ] Configuration is edited through a modal using only existing settings fields.
- [ ] Repository alert drill-down works from existing scan-run data without new persistence or policy systems.
