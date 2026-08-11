# Design: Trivy TUI Runtime Alert Tabs

## Technical Approach

Keep the generic feature-page shell intact for all features, but make the Trivy screen stateful inside the TUI. `GetFeaturePage("trivy")` remains the runtime/config/action source, while repository-alert navigation comes from a new TUI-only `ListScanRuns` seam over the existing `/admin/v1/scan-runs` route. This keeps the slice narrow, Bubble Tea-native, and backed by current contracts.

## Architecture Decisions

| Decision | Alternatives considered | Choice / rationale |
|---|---|---|
| Tabs scope | Generic backend tab schema; generic TUI tab contract | Keep tabs Trivy-only in `AdminViewState`. Current `FeaturePage` is intentionally lightweight for non-Trivy features, and existing tests already protect that minimal contract. Generalizing now would add abstraction before a second feature needs it. |
| Config editing state | New full-screen form; overloading `adminConfirmModal` | Add a dedicated Trivy config modal state alongside confirm modal state. The edit flow needs field focus, draft values, validation feedback, and submit/cancel behavior that do not fit the confirm-only modal. Reusing the workspace overlay keeps operators on the Runtime tab. |
| Alerts data source | Parse `FeatureRow` text from `repository-alerts`; extend `FeaturePage` with typed alert rows | Drive alerts from `[]ports.ScanRun` fetched by a new `AdminClient.ListScanRuns(...)`. `FeatureRow` is display-only (`Title/Status/Detail`) and brittle for selection or drill-down; `ScanRun` already carries repository, ref, digest, timestamps, counts, version, and error data honestly. |
| Drill-down model | New screen stack; per-vulnerability persistence; repository-history workflow | Use a minimal same-screen master/detail model: alert list selection plus an optional detail pane/modal for the selected scan run. Enter toggles detail, Esc returns to the list, and no new persistence or routes are introduced. |

## Data Flow

```text
open Features -> load feature list -> load GetFeaturePage("trivy") -> default Trivy tab = Runtime
Runtime tab -> render config/runtime/actions -> open config modal -> PUT feature config -> reload features/page
Repository Alerts tab -> GET /admin/v1/scan-runs?limit=N -> select alert -> show selected ScanRun detail inline/modal
```

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/tui/session.go` | Modify | Add Trivy-only tab, alert-selection, detail-open, and config-modal draft state to `AdminViewState`. |
| `internal/tui/model.go` | Modify | Add Trivy tab switching, config modal commands/messages, scan-run loading, and same-screen drill-down behavior. |
| `internal/tui/admin_views.go` | Modify | Render Trivy tabs, config modal, alert list, and selected scan-run detail while preserving the generic feature shell. |
| `internal/tui/admin_client.go` | Modify | Add `ListScanRuns(ctx, session, repository, limit)` over the existing admin route. |
| `internal/tui/admin_client_test.go` | Modify | Add RED-first route/decoding coverage for scan-run listing. |
| `internal/tui/model_test.go` | Modify | Add RED-first tests for default Runtime tab, non-Trivy fallback, config modal submit/cancel, alert selection, and drill-down. |
| `internal/app/regixtry/feature_registry.go` | Modify | Keep Trivy `FeaturePage` focused on runtime/config/action content; alert navigation moves out of `FeatureRow` parsing. |
| `internal/app/regixtry/service_test.go` | Modify | Update RED-first page-shape expectations to match the narrower runtime-backed page. |
| `docs/tui.md` | Modify | Document the Trivy tab split and explicit deferrals. |

## Interfaces / Contracts

```go
type AdminClient interface {
    ListScanRuns(ctx context.Context, session AdminSession, repository string, limit int) ([]ports.ScanRun, error)
}

type TrivyTab string

const (
    trivyTabRuntime TrivyTab = "runtime"
    trivyTabRepositoryAlerts TrivyTab = "repository-alerts"
)
```

`ports.ScanRun` remains the drill-down payload. No new backend DTO is introduced.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | Tab state transitions, config draft mapping, alert/detail toggles | Strict TDD: add RED cases in `internal/tui/model_test.go` before state/render changes. |
| Integration | Feature-page narrowing and scan-run client/route behavior | RED-first updates in `internal/app/regixtry/service_test.go` and `internal/tui/admin_client_test.go`; keep backend route authoritative. |
| E2E | None in this slice | Keep scope narrow; final gate remains `go test ./...` in verify. |

## Threat Matrix

N/A — no routing, shell, subprocess, VCS/PR automation, executable-file classification, or process-integration boundary is introduced by this UI-focused slice.

## Migration / Rollout

No migration required. Existing feature and scan-run routes stay authoritative; the TUI only changes how Trivy data is organized and edited.

## Open Questions

- [ ] None.
