## Exploration: Narrow first TUI slice for Trivy

### Current State
The current admin feature screen renders the Trivy page as one long linear block. `renderAdminFeaturesScreen` in `internal/tui/admin_views.go` prints every backend-declared header and section sequentially, so runtime status, configuration, recent runs, vulnerability summary, and repository alerts all compete in one scroll-less pane. That is the current TUI pain: operators can refresh and trigger actions, but they cannot segment the screen, select repository alerts, or drill into alert context without reading a dense wall of text.

The backend already builds a Trivy-specific page in `internal/app/regixtry/feature_registry.go`. `GetFeaturePage` fetches `GetFeatureStatus(...)` plus `ListScanRuns(ctx, "", 10)` and `buildFeaturePage(...)` emits ordered sections `config`, `runtime`, `runs`, `vulnerabilities`, and `repository-alerts`. That proves the data exists today, but the current contract is display-oriented, not navigation-oriented.

### Affected Areas
- `internal/tui/admin_views.go` — current pain is here; the feature page is rendered as one flat block with no tabs or selectable alert rows.
- `internal/tui/model.go` — verified seam for feature-screen state, key handling, selection changes, and backend-driven action help (`updateAdminFeaturesKey`, `moveAdminFeatureSelection`, `loadAdminFeaturePageCmd`).
- `internal/tui/session.go` — verified seam for adding Trivy-specific UI state to `AdminViewState`; this already stores selected feature, loaded page, and modal state.
- `internal/app/regixtry/feature_registry.go` — verified seam for current Trivy page composition and current repository-alert rows; today it uses `ListScanRuns(ctx, "", 10)` and converts results into generic `FeatureRow` values.
- `internal/ports/regixtry.go` — verified backend data contracts: `FeaturePage`, `FeatureSection`, `FeatureRow`, `FeatureAction`, `FeatureDetails`, `TrivyRuntimeState`, and rich `ScanRun` fields (`repository`, `requested_ref`, `digest`, timestamps, severity counts, `trivy_version`, `error`).
- `internal/protocol/http/admin_handlers.go` — verified backend routes already available for this slice: `GET /admin/v1/features/{name}`, `PUT /admin/v1/features/{name}/config`, runtime mutation endpoints, and `GET /admin/v1/scan-runs?repository=&limit=` for lookup/drill-down.
- `internal/tui/admin_client.go` — verified seam for adding one small scan-run lookup method; it already wraps the feature page/config/runtime routes but does not yet expose scan-run listing.
- `internal/infra/metadata/sqlite/store.go` — verified scan-run query semantics: repository filter exists and rows are returned newest-first.
- `internal/app/regixtry/service_test.go`, `internal/tui/model_test.go`, `internal/tui/admin_client_test.go`, `internal/protocol/http/router_test.go` — verified test seams already cover feature-page shape and admin feature interactions; they are the natural RED-first entry points for strict TDD.

### Approaches
1. **Frontend-only split over the existing generic page** — Keep using only `FeaturePage`, add tabs in the TUI, and reuse `repository-alerts` rows as-is.
   - Pros: Smallest backend delta; fast first slice.
   - Cons: `FeatureRow` only has `Title`, `Status`, and `Detail`, so alert navigation and drill-down stay weak and string-parsed; lookup by repository is awkward.
   - Effort: Low

2. **TUI tabs plus minimal scan-run lookup seam** — Keep `FeaturePage` for runtime summary/actions, move configuration editing into a TUI modal, and add a focused TUI call to `GET /admin/v1/scan-runs` for repository-alert navigation and drill-down.
   - Pros: Uses existing backend data honestly, keeps the change narrow, avoids redesigning the generic feature-page contract, and enables real alert selection/filtering with current stored data.
   - Cons: Requires a small admin-client/model expansion and one extra TUI view state.
   - Effort: Medium

3. **Generalize feature-page tabs and typed rows in the backend contract** — Extend `FeaturePage` into a reusable tabbed/navigation schema for all features.
   - Pros: Most abstract and theoretically reusable.
   - Cons: This is classic overengineering for one Trivy slice; it expands contracts before the product proves the need.
   - Effort: High

### Recommendation
Choose **Approach 2: TUI tabs plus minimal scan-run lookup seam**.

Recommended narrow architecture:
- Add Trivy-only view state inside `AdminViewState`, not a generic framework first:
  - selected tab: `runtime` or `repository-alerts`
  - config modal state: open/closed, editable fields, focused field, and submit/cancel status
  - alert state: selected alert row, optional repository filter/drill-down state
- Keep `GetFeaturePage("trivy")` as the source for summary, runtime status, and backend-declared actions.
- Move configuration out of the inline page and into a modal that edits the fields already backed by `FeatureConfigureInput`: enabled, schedule enabled, interval, timeout, registry reachable URL, and max concurrency. Reuse the existing modal rendering pattern in `renderAdminWorkspace`; extend it for an input modal rather than inventing a new screen flow.
- Add a small `ListScanRuns` method to `AdminClient` that calls the already-existing `GET /admin/v1/scan-runs` endpoint with `repository` and `limit`.
- Build the **Repository Alerts** tab from `[]ports.ScanRun`, not from parsed `FeatureRow` strings. Show a selectable list of repositories/runs with the currently available facts only: repository, requested ref, status, severity counts, digest, timestamps, Trivy version, and error text.
- For drill-down, keep it basic: Enter opens a detail pane/modal for the selected alert or toggles into a repository-scoped list using the filtered scan-run route. Do not invent per-vulnerability persistence or cross-screen workflows in this slice.
- Keep runtime lifecycle actions where they already belong: backend-declared actions plus existing runtime endpoints.

Why this is the right first slice:
- It fixes the immediate UX pain without forcing a dashboard rewrite.
- It uses existing API/data truth instead of fabricating richer vulnerability models.
- It keeps Trivy-specific complexity local to the Trivy screen until a second feature proves broader abstractions are worth it.

### Risks
- If the implementation tries to force alert drill-down through `FeatureRow` text instead of `ScanRun`, the UI will become brittle immediately.
- If the modal attempts to own settings that are not already in `FeatureConfigureInput`, scope will drift.
- If tabs become a generic backend schema now, review size and conceptual complexity will jump for no product gain.
- The existing feature screen has tests, but strict TDD means new RED tests must land before any model/view/client changes.

### Ready for Proposal
Yes — proceed with a proposal for a Trivy-only first slice: two tabs (`Runtime`, `Repository Alerts`), one configuration modal for current settings, one small admin-client seam for scan-run lookup, and basic repository alert drill-down using existing scan-run data only.
