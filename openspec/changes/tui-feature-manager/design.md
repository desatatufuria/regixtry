# Design: TUI Feature Manager

## Technical Approach

Keep `/admin/v1/features` as the shared inventory, but replace `FeatureDetails` as the TUI detail contract with a backend-built `FeaturePage`. The backend assembles one page per feature; the Bubble Tea side stays a thin shell for rendering, selection, confirmation, and refresh.

## Architecture Decisions

| Decision | Alternatives considered | Choice / rationale |
|---|---|---|
| Shared list vs per-feature detail | Add more optional fields to `FeatureDetails`; split each feature into separate screens | Keep a generic summary list plus a per-feature page. The list stays navigation-only (`name`, `kind`, enabled/configured, generic badges). Rich detail moves into feature-owned sections, so future features fit without inheriting Trivy vocabulary. |
| DTO/API shape | Arbitrary schema interpreter; raw backend HTML/text blobs | Add explicit Go structs: `FeaturePage`, `FeatureSection`, `FeatureField`, `FeatureRow`, `FeatureAction`. Enough for Trivy richness without a meta-framework. |
| Action transport | Keep hard-coded TUI keybindings; expose raw method/path in payload | Backend declares actions, but execution stays typed through `POST /admin/v1/features/{name}/actions/{actionID}`. The shell only maps visible actions to shortcuts and confirmation. |
| Trivy richness vs premature abstraction | Build a generic plugin framework now | Support only two reusable content shapes in v1: field sections and row/list sections. Avoid schema languages or plugin loaders until a second feature proves the gap. |

## Data Flow

```text
Features screen open
  -> GET /admin/v1/features
  -> operator selects feature
  -> GET /admin/v1/features/{name}
  -> backend builds FeaturePage(header, sections, actions)
  -> TUI renders page + action help

operator triggers action
  -> confirm if declared
  -> POST /admin/v1/features/{name}/actions/{actionID}
  -> backend performs typed mutation
  -> TUI refreshes summary list + selected page
```

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/ports/regixtry.go` | Modify | Add `FeaturePage`/section/action DTOs; keep `FeatureSummary`; retire `FeatureDetails` as the TUI page contract. |
| `internal/app/regixtry/feature_registry.go` | Modify | Build shared summaries plus per-feature pages; add Trivy page assembly helpers. |
| `internal/app/regixtry/service_scanning.go` | Modify | Provide Trivy page data for recent runs, vulnerability, and repository-alert summaries. |
| `internal/protocol/http/admin_handlers.go` | Modify | Serve `GET /admin/v1/features/{name}` as `FeaturePage` and add typed action execution route. |
| `internal/tui/admin_client.go` | Modify | Decode `FeaturePage`; replace fixed feature-status reads with page reads and generic action execution. |
| `internal/tui/session.go` | Modify | Store selected `FeaturePage` plus focused action metadata instead of `FeatureStatus`. |
| `internal/tui/model.go` | Modify | Remove local feature-action availability logic; keep shell behavior (selection, refresh, confirm, feedback). |
| `internal/tui/admin_views.go` | Modify | Render generic header/sections/actions and Trivy page content through shared section renderers. |
| `internal/protocol/http/router_test.go`, `internal/tui/{admin_client_test.go,model_test.go}` | Modify | Add RED coverage for page payloads, action execution, minimal-page handling, and Trivy-specific sections. |

## Interfaces / Contracts

```go
type FeaturePage struct {
    Summary FeatureSummary   `json:"summary"`
    Header  []FeatureField   `json:"header,omitempty"`
    Sections []FeatureSection `json:"sections,omitempty"`
    Actions []FeatureAction   `json:"actions,omitempty"`
}

type FeatureSection struct {
    ID string `json:"id"`
    Title string `json:"title"`
    Kind string `json:"kind"` // fields | rows
    Fields []FeatureField `json:"fields,omitempty"`
    Rows []FeatureRow `json:"rows,omitempty"`
}
```

Trivy page sections for v1: `config`, `runtime`, `runs`, `vulnerabilities`, and `repository-alerts`. Actions initially cover existing operator flows only: `refresh`, `enable`, `disable`, `install-runtime`, `upgrade-runtime`, and `rollback-runtime`.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | Page assembly, section ordering, action declaration, minimal non-Trivy pages | RED-first tests in app/ports before DTO/service changes. |
| Integration | `/admin/v1/features` page payloads and typed action route behavior | Extend router tests for summary list, page reads, action POSTs, auth, and unknown action/name failures. |
| E2E | Feature screen renders backend-declared pages and refreshes after actions | Extend TUI model/client tests first, then keep `go test ./...` green per strict TDD. |

## Threat Matrix

| Boundary | Minimum adversarial cases | Applicability | Design response | Planned RED tests |
|---|---|---|---|---|
| Documentation-like paths | `requirements.txt`, `CMakeLists.txt`, executable Markdown/MDX, `README.sh` | N/A: this change does not classify or execute files | None | None |
| Git repository selection | `git -C`, relative paths, absolute paths | N/A: no git selection | None | None |
| Commit state | staged, `commit -a`, empty index | N/A: no commit automation | None | None |
| Push state | tracking branch, first push, explicit refspec | N/A: no push automation | None | None |
| PR commands | explicit `--head`, environment prefix, composed commands | N/A: no PR automation | None | None |

## Migration / Rollout

No storage migration required. Roll out the new page DTO and TUI client in one slice; action routes can reuse current feature/runtime service methods.

## Open Questions

- [ ] Should repository-alert rows open a dedicated Trivy subview in this change, or stay read-only summaries until a second UX slice?
