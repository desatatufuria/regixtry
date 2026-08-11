# Design: TUI Bubble Table Presentation

## Technical Approach

Keep the change inside `internal/tui`: adapt existing admin DTOs into Bubble Tea table models, render those models inside the current admin screens, and keep all backend routes, DTOs, ordering, and command flows unchanged. This implements the proposal and the `operator-admin-tui` delta by replacing string-joined row views with aligned tables while preserving current screen transitions.

## Architecture Decisions

| Decision | Options | Choice | Rationale |
|---|---|---|---|
| Localized adapter vs framework sprawl | Generic cross-app table layer; TUI-local adapter | TUI-local adapter in `internal/tui/admin_tables.go` | Current row views are only in admin/Trivy seams. A local adapter solves the UX problem without inventing a platform abstraction the product has not earned. |
| Table library | Keep manual `fmt.Sprintf`; use `bubble-table` | Use `github.com/Evertras/bubble-table` behind local helpers | Context7 confirms columns, hidden row metadata, configurable keymaps, and row/cell styling. That gives real table behavior with less custom code than rebuilding selection, pagination, and styling by hand. |
| Severity styling scope | Style all tables; style only vulnerability data | Style only vulnerability findings cells/rows | Matches spec scope: findings use backend severity emphasis; feature summaries, generic `rows`, and scan-run tables keep neutral admin styling. |
| Focus and keybindings | Let table defaults own the screen; screen mediates focus | Screen-level update functions remain authoritative | Existing `updateAdminFeaturesKey` already owns `Esc`, `Tab`, refresh, config, detail, and action flows. The design keeps those shortcuts stable and lets table navigation run only for row movement/filtering within the active table. |
| Backend contracts | Reshape DTOs for presentation; adapt in TUI | Preserve `ports.FeaturePage`, `FeatureSection`, `ScanRun`, and `ScanRunDetail` exactly | The slice is presentation-only. Adapters consume backend truth and attach hidden row metadata for selection targets instead of changing API shapes. |

## Data Flow

```text
AdminClient DTOs -> Model loads existing data -> admin table adapter builds table.Model
      -> AdminViewState stores table + selected metadata -> admin_views renders table.View()
      -> screen key handler reads selected metadata -> existing load/refresh/action cmds
```

Feature pages keep using `loadAdminFeaturePageCmd`; Trivy alerts keep using `loadAdminScanRunsCmd` and `loadAdminScanRunDetailCmd`. Table state is disposable UI state derived from backend payloads.

## File Changes

| File | Action | Description |
|---|---|---|
| `go.mod`, `go.sum` | Modify | Add `github.com/Evertras/bubble-table` if final implementation confirms compatibility. |
| `internal/tui/admin_tables.go` | Create | Table adapter/builders, column definitions, keymap overrides, hidden metadata helpers, severity style helpers. |
| `internal/tui/admin_views.go` | Modify | Replace plain-text row rendering with table rendering and keep empty-state messaging. |
| `internal/tui/session.go` | Modify | Add presentation-only admin table state for features, generic row sections, scan runs, and findings. |
| `internal/tui/model.go` | Modify | Rebuild table state after loads, route table-focused keys through existing admin screen handlers, and preserve current commands/actions. |
| `internal/tui/admin_theme.go` | Modify | Add neutral table styles plus severity palette compatible with the current theme. |
| `internal/tui/model_test.go` | Modify | Strict-TDD RED/GREEN coverage for table rendering state, key ownership, and unchanged command flows. |

## Interfaces / Contracts

```go
type adminTableState struct {
    Features   table.Model
    FeatureRows table.Model
    ScanRuns   table.Model
    Findings   table.Model
}
```

Hidden row metadata will carry stable identifiers such as feature name, scan run ID, and finding identity. Existing `ports` DTOs remain unchanged and remain the only source of displayed values.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | Adapter column mapping, severity style selection, hidden metadata extraction | New table-focused tests in `internal/tui/model_test.go` built RED first under strict TDD. |
| Integration | Admin feature/trivy update flow still triggers existing commands and preserves current status/empty-state semantics | Message-driven model tests around `adminFeaturesLoadedMsg`, `adminFeaturePageLoadedMsg`, `adminScanRunsLoadedMsg`, and key events. |
| E2E | N/A for this phase | `go test ./...` remains the required implementation gate; no new smoke flow is needed for design. |

## Threat Matrix

N/A — no routing, shell, subprocess, VCS/PR automation, executable-file classification, or process-integration boundary is introduced by this presentation-only TUI change.

## Migration / Rollout

No migration required.

## Open Questions

- [ ] Confirm the pinned `bubble-table` version that compiles with the repository's Bubble Tea version before apply.
- [ ] `strict-tdd.md` was not found under `/workspace`; apply must confirm the authoritative instructions location before coding.
