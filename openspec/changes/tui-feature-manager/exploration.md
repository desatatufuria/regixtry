## Exploration: tui-feature-manager

### Current State
The current admin TUI already exposes a "Features" screen, but it is still effectively a thin Trivy operator view rather than a generic feature manager. The Bubble Tea flow is generic only at the list-selection level: `updateAdminFeaturesKey` supports refresh, enable/disable, and runtime install/upgrade/rollback, then `renderAdminFeaturesScreen` prints one fixed detail block that assumes schedule fields, scanner connectivity fields, concurrency, and runtime version metadata for every feature. On the backend, `FeatureDetails` and `FeatureConfigureInput` are also Trivy-shaped because they are direct projections of `ScanSettings`, while `ListFeatures` injects one shared runtime projection for every listed feature. That means the current TUI is both too feature-specific for heterogeneous future features and too thin for Trivy itself, because it cannot yet model richer Trivy-only sections like config, runtime, runs, vulnerability views, or repository alerts.

### Affected Areas
- `internal/tui/model.go` — Bubble Tea feature navigation, selection refresh, and hard-coded feature actions live here.
- `internal/tui/admin_views.go` — the current Features screen renders one fixed summary/detail layout with Trivy-specific fields.
- `internal/tui/session.go` — admin view state currently stores a single `FeatureStatus` payload for the selected feature.
- `internal/tui/admin_client.go` — admin DTO decoding and feature mutation routes are shaped around fixed feature details and Trivy runtime actions.
- `internal/ports/regixtry.go` — `FeatureSummary`, `FeatureDetails`, `FeatureConfigureInput`, and `FeatureRuntime` define the current feature contract.
- `internal/app/regixtry/feature_registry.go` — built-in feature registry, feature summary/detail assembly, and settings projection are currently backed by `ScanSettings` and one Trivy runtime projection.
- `internal/app/regixtry/feature_runtime.go` — runtime mutations validate feature names but return `TrivyRuntimeState`, so runtime actions are not yet feature-agnostic.
- `internal/protocol/http/admin_handlers.go` — `/admin/v1/features` routes expose fixed detail/config/runtime payloads and fixed runtime mutation endpoints.
- `openspec/specs/operator-admin-tui/spec.md` — current main spec covers authenticated admin browsing and mutations, but not a generic feature-manager information architecture.

### Approaches
1. **Keep extending the current fixed feature DTOs** — Add more optional fields to `FeatureDetails` and teach the TUI to hide unsupported ones.
   - Pros: Smallest immediate diff, reuses existing handlers and tests.
   - Cons: Bakes more Trivy assumptions into the shared contract, scales poorly for heterogeneous features, and turns the TUI into a giant optional-field renderer.
   - Effort: Low

2. **Introduce a lightweight generic feature-manager contract** — Keep a common feature summary, but replace the fixed detail pane with per-feature sections and per-feature actions described by the backend.
   - Pros: Supports heterogeneous future features, keeps the Bubble Tea shell reusable, and lets Trivy grow richer sections without forcing every feature into scan/runtime vocabulary.
   - Cons: Requires DTO and handler refactoring plus a new rendering model in the TUI.
   - Effort: Medium

### Recommendation
Adopt **Approach 2** with a deliberately lightweight architecture: a generic feature-manager shell plus backend-authoritative per-feature sections/actions.

Recommended shape:

- Keep a **common summary list** for all features: name, kind, enabled/configured state, and a small set of generic badges or status lines.
- Replace `FeatureDetails` as the TUI-facing detail contract with a **feature page payload** composed of:
  - common header metadata,
  - ordered `sections[]` where each section has a stable id, title, kind, and display items,
  - ordered `actions[]` where each action has id, label, kind, availability, and optional confirmation copy.
- Treat runtime/config/runs/vulnerabilities/repository-alerts as **feature-specific sections**, not universal fields.
- Treat install/upgrade/rollback/enable/disable as **declared actions**, not hard-coded global keybindings for every feature.
- Keep the Bubble Tea side thin: one generic feature-manager screen owns selection, focused section/action, refresh, and mutation feedback; feature-specific rendering is driven by section/action metadata instead of Trivy branching spread through the model.

For Trivy specifically, the first feature page can expose sections such as:

- Summary
- Configuration
- Runtime
- Recent runs
- Vulnerability views
- Repository alerts

Repository alerts should link to vulnerability detail through a typed action/route model, but that navigation belongs to Trivy's section payload, not to the global feature-manager contract.

### Risks
- The current backend contract is tightly coupled to `ScanSettings`, so a naïve refactor can accidentally break existing admin feature tests and `/admin/v1/features` clients.
- If the new generic model is too abstract, we will overengineer before the second real feature exists.
- If actions stay hard-coded in Bubble Tea while sections become generic, the system will still leak Trivy assumptions through keyboard flows.
- Trivy already needs richer subviews, so the proposal must distinguish reusable shell behavior from Trivy-only navigation.

### Ready for Proposal
Yes — the repository has clear seams and enough verified evidence to propose a change centered on a generic feature-manager shell with backend-declared per-feature sections/actions. The proposal should explicitly keep the reusable contract small, preserve current enable/disable/runtime operator flows where still relevant, and move Trivy-only concepts out of shared feature DTOs.
