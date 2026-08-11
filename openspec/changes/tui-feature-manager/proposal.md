# Proposal: TUI Feature Manager

## Intent

Turn the admin TUI Features screen into a generic feature-manager shell so future features are not forced into Trivy-specific detail fields, while Trivy can expose richer operator views through backend-declared payloads.

## Scope

### In Scope
- Replace fixed shared feature details with a lightweight feature page contract: common header, ordered sections, and declared actions.
- Keep a shared feature summary list for navigation, status, and generic badges.
- Refactor TUI, admin API, and app-layer feature assembly so Trivy-specific views (configuration, runtime, runs, vulnerability views, repository alerts) come from Trivy payloads, not global fields.
- Preserve current operator mutation flows where still relevant, but drive them from backend-declared actions.

### Out of Scope
- Adding a second built-in feature implementation.
- Reworking unrelated admin/TUI navigation outside the Features area.
- Building a generic plugin system or highly abstract schema language.

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `operator-admin-tui`: extend the admin features experience from a fixed Trivy-shaped detail pane to a generic feature-manager shell with backend-authoritative sections and actions.

## Approach

Adopt a thin shell / rich payload split. Keep feature summaries shared, but replace `FeatureDetails`-style universal fields with a small page model assembled by the backend per feature. The TUI remains responsible for selection, refresh, focus, confirmations, and feedback; feature semantics stay in backend-declared sections/actions. Favor explicit structs over a flexible meta-framework.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/ports/regixtry.go` | Modified | Replace fixed feature detail/config contracts with generic page/section/action DTOs. |
| `internal/app/regixtry/feature_registry.go` | Modified | Build per-feature payloads, starting with richer Trivy sections. |
| `internal/protocol/http/admin_handlers.go` | Modified | Serve the new feature page contract through `/admin/v1/features`. |
| `internal/tui/admin_client.go` | Modified | Decode generic feature payloads and declared actions. |
| `internal/tui/model.go`, `internal/tui/admin_views.go`, `internal/tui/session.go` | Modified | Render and operate the generic feature-manager shell. |
| `openspec/specs/operator-admin-tui/spec.md` | Modified | Capture the new admin feature-manager behavior. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Overabstracting before a second feature exists | Med | Keep the shared contract to summary + sections + actions only. |
| Breaking existing Trivy/admin flows | Med | Preserve current actions, evolve tests/specs first, and keep backend authority. |
| Client/API drift during refactor | Low | Change TUI DTOs and `/admin/v1/features` contract in one slice. |

## Rollback Plan

Revert the feature page contract, TUI shell changes, and `/admin/v1/features` payload updates together to restore the current Trivy-shaped screen and DTOs.

## Dependencies

- Existing `operator-admin-tui` spec and current `/admin/v1/features` flows.
- Follow-on spec, design, and strict-TDD implementation phases.

## Success Criteria

- [ ] The proposal keeps the reusable contract limited to shared summaries plus backend-declared sections/actions.
- [ ] Trivy can expose richer feature-specific sections without adding new universal fields.
- [ ] Future non-Trivy features can fit the shell without inheriting schedule/runtime vocabulary.
