# Design: Feature Runtime Operator UX

## Technical Approach

Keep the slice inside the existing managed-runtime path. Reuse the Trivy runtime manager for staged install/upgrade reporting and latest-release resolution, extend feature projections with operator-facing version/update fields, render `feature list` with a stdlib table, and keep Bubble Tea feature actions as thin key bindings over the existing admin API.

## Architecture Decisions

| Decision | Alternatives considered | Choice / rationale |
|---|---|---|
| Progress seam reuse | New operation engine; stream progress through admin API/TUI | Add an optional runtime progress callback in the existing feature runtime manager config and emit coarse stages (`resolve`, `download`, `verify`, `extract`, `activate`, `probe`, `complete`). This mirrors `internal/infra/install/linux/upgrade.go`, keeps CLI local, and avoids widening HTTP/TUI contracts. |
| Latest-version resolution | Background polling; persisted update metadata | Resolve latest on explicit reads only (`feature list`, `feature status`, TUI refresh) by reusing `ResolveRelease("")`. Projection degrades to `unknown` on network failure so visibility never breaks. |
| CLI table rendering | Raw tab-separated lines; third-party table package | Use `text/tabwriter` in `cmd/regixtry/main.go` for a fixed-width table with `NAME`, `KIND`, `ENABLED`, `CONFIGURED`, `CURRENT`, `LATEST`, `UPDATE`. Stdlib keeps the change narrow and testable. |
| Bubble Tea action model | New feature workflow/screens; optimistic local rules | Keep `updateAdminFeaturesKey` thin and drive allowed keys from a pure availability helper derived from backend-authoritative `FeatureDetails.Runtime`. Render matching help text in `admin_views.go`; TUI shows start/end status only, not streaming progress. |

## Data Flow

```text
feature install/upgrade -> runtime manager progress callback -> CLI progress writer
feature list/status/TUI refresh -> Service project runtime -> ResolveRelease("") -> summary/details projection
TUI key -> admin client existing endpoint -> refreshed feature status -> availability helper -> updated help text
```

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/ports/regixtry.go` | Modify | Extend `FeatureSummary` / `FeatureRuntime` with latest-version and update-state fields. |
| `internal/app/regixtry/service.go` | Modify | Add narrow progress/latest-resolution seams to the runtime manager interface/config. |
| `internal/app/regixtry/feature_registry.go` | Modify | Project current/latest/update fields on explicit reads only. |
| `internal/infra/scanning/trivy/{runtime_manager.go,releases.go}` | Modify | Emit staged runtime progress and reuse release lookup for latest-version inspection. |
| `cmd/regixtry/main.go` | Modify | Add feature-runtime progress writer reuse, summary table rendering, and richer status output. |
| `cmd/regixtry/main_test.go` | Modify | Add RED-first CLI table/progress/update-visibility coverage. |
| `internal/tui/{model.go,admin_views.go}` | Modify | Add thin install/upgrade/rollback action availability and contextual help. |
| `internal/tui/model_test.go` | Modify | Add RED-first feature-action availability and rendering tests. |

## Interfaces / Contracts

```go
type FeatureRuntimeProgress struct { Stage, Detail string }

type FeatureRuntime struct {
    Version string
    LatestVersion string
    UpdateStatus string // unknown|up-to-date|available
    RollbackAvailable bool
}
```

`UpdateStatus` is derived, not persisted. `FeatureSummary` carries the same current/latest/update projection needed by `feature list` and the TUI sidebar.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | Availability helper, runtime projection, progress-stage mapping | Strict TDD: write RED table-driven tests before helper/projection changes. |
| Integration | Runtime manager latest-resolution/progress callbacks, CLI status/list rendering | Fake release client + temp storage; extend `cmd/regixtry/main_test.go` and runtime manager tests. |
| E2E | None in this slice | Keep scope narrow; `go test ./...` remains the required gate. |

## Threat Matrix

| Boundary | Minimum adversarial cases | Applicability | Design response | Planned RED tests |
|---|---|---|---|---|
| Documentation-like paths | `requirements.txt`, `CMakeLists.txt`, executable Markdown/MDX, `README.sh` | Applicable: runtime status already classifies legacy executable paths and this slice keeps process-facing runtime UX | Never treat latest-version metadata or progress as authority to execute arbitrary legacy paths; managed runtime actions still operate only on verified Regixtry-owned binaries | Preserve `/tmp/README.sh` migration-required coverage while adding latest/update projection assertions |
| Git repository selection | `git -C`, relative paths, absolute paths | N/A: no git boundary | None | None |
| Commit state | staged, `commit -a`, empty index | N/A: no commit automation | None | None |
| Push state | tracking branch, first push, explicit refspec | N/A: no push automation | None | None |
| PR commands | explicit `--head`, environment prefix, composed commands | N/A: no PR automation | None | None |

## Migration / Rollout

No migration required. Latest/update fields are projected on read and existing runtime mutation endpoints stay unchanged.

## Open Questions

- [ ] None.
