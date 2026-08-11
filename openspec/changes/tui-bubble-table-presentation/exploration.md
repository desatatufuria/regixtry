## Exploration: Bubble Tea table presentation for admin TUI

### Current State
The current admin TUI still renders row-oriented data as plain joined strings instead of a real table widget. In `internal/tui/admin_views.go:89-95`, the Features inventory is printed as one long formatted line per feature. In `internal/tui/admin_views.go:179-185`, Trivy repository alerts are printed as `repository@ref [status] critical= high=` strings. In `internal/tui/admin_views.go:200-204`, vulnerability findings are printed as pipe-delimited text lines. Generic feature rows also flatten `FeatureRow{Title, Status, Detail}` into plain strings in `internal/tui/admin_views.go:135-148`.

That creates the current pain: poor scanability, no true column alignment, weak selection affordances, and no severity-led visual hierarchy for vulnerability review. The codebase already has the data needed for better presentation: `AdminViewState` stores feature selection, Trivy tab state, scan-run lists, and scan-run detail in `internal/tui/session.go:136-165`; `Model` already loads feature pages, scan runs, and run detail through focused commands in `internal/tui/model.go:1640-1667`; and the admin client/backend contracts already expose `FeatureSummary`, `FeaturePage`, `ScanRun`, and `ScanRunDetail` without requiring persistence changes.

The repo does not currently depend on any table component; `go.mod` contains Bubble Tea and Lip Gloss, but no `github.com/Evertras/bubble-table` dependency today.

### Affected Areas
- `go.mod` — would add the chosen table component dependency; no such dependency exists today.
- `internal/tui/admin_views.go` — verified current rendering seam for plain-text feature rows, repository-alert rows, and finding rows.
- `internal/tui/session.go` — verified UI-state seam; `AdminViewState` already holds feature/trivy selection and is the natural place for TUI-only table state.
- `internal/tui/model.go` — verified interaction seam; existing commands already load feature pages, scan runs, and scan-run detail, so table selection can stay presentation-local.
- `internal/tui/admin_client.go` — already exposes `ListScanRuns` and `GetScanRunDetail`; useful because this slice should reuse existing API seams rather than redesign them.
- `internal/ports/regixtry.go` — current DTOs (`FeatureSummary`, `FeaturePage`, `FeatureRow`, `ScanRun`, `ScanRunDetail`) define the data boundaries this slice should preserve.
- `internal/app/regixtry/feature_registry.go` — backend feature-page composition remains a seam to consume, not redesign, for this presentation-only change.
- `internal/tui/model_test.go`, `internal/tui/admin_client_test.go`, `internal/app/regixtry/service_test.go` — verified RED-first test seams for strict TDD in later phases.

### Approaches
1. **Keep string rendering and polish formatting** — Stay with `fmt.Sprintf`/Lip Gloss and manually align columns.
   - Pros: Smallest diff and no new dependency.
   - Cons: Repeats the current problem in a prettier form, keeps selection/focus logic ad hoc, and makes severity styling/table reuse brittle.
   - Effort: Low

2. **Adopt a localized Bubble Tea table adapter** — Introduce `github.com/Evertras/bubble-table` only inside `internal/tui` and map existing DTOs into focused table models for features, repository alerts, and findings.
   - Pros: Real columns, built-in selection/navigation, row metadata support, and row/cell styling for severity without changing backend contracts. Context7 confirms the library supports configurable columns, row metadata, and row/cell styling.
   - Cons: Adds one dependency and requires careful keybinding integration with the current screen-level shortcuts.
   - Effort: Medium

3. **Create a generic cross-app table framework first** — Abstract a reusable table layer for every current and future TUI list before applying it to Trivy/admin views.
   - Pros: Maximum theoretical reuse.
   - Cons: THIS is exactly how scope explodes. It turns a presentation slice into a framework exercise before the product proves the need.
   - Effort: High

### Recommendation
Choose **Approach 2: adopt a localized Bubble Tea table adapter**, with `github.com/Evertras/bubble-table` as the preferred first option.

Recommended narrow architecture:
- Keep all changes TUI-local. Add table-building helpers under `internal/tui` that convert existing DTOs into table models; do not change persistence, service orchestration, or admin API contracts.
- Use one focused table model per row-oriented view:
  - Features inventory from `[]ports.FeatureSummary`
  - Trivy repository alerts / runs from `[]ports.ScanRun`
  - Trivy findings from `ports.ScanRunDetail.Findings`
  - Optionally generic `FeatureSection.Kind == "rows"` sections when they are already present
- Store only presentation state in `AdminViewState` (focused table, selected row metadata, maybe per-table cursor/filter state). Keep business truth in the existing DTOs already loaded by `Model`.
- Add a TUI-local severity style map (`CRITICAL`, `HIGH`, `MEDIUM`, `LOW`, `UNKNOWN`) and apply it through row/cell styling. Severity colors MUST affect vulnerability-facing tables only; non-vulnerability lists should keep neutral styling.
- Use hidden row metadata for stable selection targets (feature name, scan run ID, finding key) so Enter/refresh can keep calling the existing `loadAdminFeaturePageCmd`, `loadAdminScanRunsCmd`, and `loadAdminScanRunDetailCmd` seams.
- Keep current screen structure intact: this is a rendering and interaction upgrade inside the existing admin/Trivy screens, not a navigation rewrite.

Scope boundaries:
- Do **not** redesign `ports.FeaturePage`, `ports.FeatureRow`, scan persistence, or `/admin/v1` routes just to fit the table widget.
- Do **not** change scan ordering, severity calculation, runtime actions, or policy/config semantics.
- Do **not** introduce generic feature-platform abstractions beyond what the current admin TUI needs for these row-oriented views.
- Do **not** mix this slice with backend vulnerability-detail storage or freshness-policy redesign; those are separate changes already explored elsewhere.

Recommended change name: `tui-bubble-table-presentation`

### Risks
- Table keybindings can fight existing screen-level shortcuts if focus ownership is not explicit.
- If the implementation tries to "improve" backend DTOs for presentation convenience, the slice will stop being UI-only.
- Severity color choices can hurt readability unless they are tested against the existing Lip Gloss theme.
- `strict-tdd.md` was not present anywhere under `/workspace`, even though `openspec/config.yaml` marks strict TDD as active; apply phase owners must verify the authoritative TDD instructions before coding.

### Ready for Proposal
Yes — proceed with a proposal for `tui-bubble-table-presentation`: a presentation-only TUI slice that replaces current plain-text row rendering with a Bubble Tea table component, adds severity-aware styling for vulnerability-facing tables, and stays strictly inside existing admin/TUI seams.
