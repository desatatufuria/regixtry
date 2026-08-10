# Design: Separate Feature Config From Setup

## Technical Approach

Keep `setup` and lifecycle provenance base-only, then introduce a generic built-in feature inventory keyed by capability identity. The model stays open for future built-in features (for example `backup`, `replication`, `webhooks`, or `observability`), but this slice implements only `trivy` as the first concrete feature. Each feature descriptor declares an implementation kind (`builtin`, `external_binary`, `external_service`); Trivy continues to use `scan_settings` plus `/admin/v1/scan-settings` as the authority seam.

## Architecture Decisions

| Decision | Choice | Alternatives considered | Rationale |
|---|---|---|---|
| Feature identity model | Feature names are operator-facing capability identities; initial feature is `trivy` | Abstract key like `scanning`; setup-owned flags | The agreed model treats features as named capabilities, so the CLI and docs should say `trivy` directly. |
| Implementation model | Every feature descriptor includes `kind: builtin\|external_binary\|external_service` | Ad-hoc per-feature metadata; plugin loader | A shared kind model scales cleanly to future capabilities without implying runtime plugin discovery. |
| Runtime/version ownership | Version and health belong to runtime details, not the abstract feature record | `feature.version`; engine-version on the generic feature itself | This keeps identity stable while allowing external binaries or services to report their own runtime evidence. |
| CLI/output split | `feature list` returns generic inventory; `feature show/status <name>` return generic fields plus feature-specific sections | Separate top-level commands per feature; one overloaded status view | Operators get one consistent inventory model while each feature can expose its own config and runtime details. |
| Authority seam | Reuse `Service.Get/Update/EnsureScanSettings` and `/admin/v1/scan-settings`; add thin feature projection helpers instead of new persistence | New generic feature store now | Existing Trivy persistence already matches the needed state and avoids CLI/admin drift. |

## Data Flow

    built-in feature registry (`trivy`; future examples include `backup`, `replication`, ...)
                     |
    feature CLI -----+----> feature projection layer ----> feature authority
    admin API -------'                |                      |
                                      |                      '--> `scan_settings` for `trivy`
                                      '--> runtime probe ---------> binary/service health
    setup/upgrade legacy flags ----------------------------> import-if-missing for `trivy`

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `cmd/regixtry/main.go` | Modify | Normalize `feature list|show|status|enable|disable|configure <name>` around feature identities; keep legacy Trivy setup flags as migration-only input. |
| `internal/app/regixtry/service_scanning.go` | Modify | Split import-if-missing from normal reads/writes and add Trivy-specific projection/status helpers over `ScanSettings`. |
| `internal/app/regixtry/feature_registry.go` | Create | Define built-in feature descriptors, implementation kind metadata, and generic/show/status projection rules. |
| `internal/ports/regixtry.go` | Modify | Add generic feature DTOs plus Trivy runtime detail DTOs without moving version onto the abstract feature. |
| `internal/protocol/http/admin_handlers.go` | Modify | Keep `/admin/v1/scan-settings` authoritative and expose thin feature list/show/status projections if this slice includes admin reads. |
| `internal/infra/install/linux/{bootstrap.go,templates.go,provenance.go,intent.go,upgrade.go}` | Modify | Remove steady-state Trivy ownership from bootstrap/lifecycle replay and preserve truthful uninstall reporting. |
| `cmd/regixtry/main_test.go`, `internal/app/regixtry/service_test.go`, `internal/protocol/http/router_test.go`, `internal/infra/install/linux/*_test.go` | Modify/Create | Strict-TDD RED coverage for generic feature inventory, Trivy-specific projection, migration, runtime status, and lifecycle truthfulness. |

## Interfaces / Contracts

```go
type FeatureKind string

const (
	FeatureKindBuiltin        FeatureKind = "builtin"
	FeatureKindExternalBinary FeatureKind = "external_binary"
	FeatureKindExternalService FeatureKind = "external_service"
)

type FeatureSummary struct {
	Name    string      `json:"name"`
	Kind    FeatureKind `json:"kind"`
	Enabled bool        `json:"enabled"`
}

type FeatureRuntime struct {
	Health  string `json:"health,omitempty"`
	Version string `json:"version,omitempty"`
}
```

CLI contract:
- `regixtry feature list`
- `regixtry feature show trivy`
- `regixtry feature status trivy`
- `regixtry feature enable trivy`
- `regixtry feature disable trivy`
- `regixtry feature configure trivy [--schedule-enabled --interval --timeout --cache-dir --binary-path --max-concurrency]`

`list` shows generic inventory only. `show/status trivy` include generic fields (`name`, `kind`, `enabled`) plus Trivy config/runtime fields such as `binary_path`, `schedule_enabled`, `runtime.health`, and `runtime.version`.

## Testing Strategy

| Layer | What to Test | Approach |
|-------|-------------|----------|
| Unit | feature registry resolution, implementation-kind mapping, import-if-missing semantics, Trivy runtime projection | RED-first table-driven tests. |
| Integration | CLI routing for generic inventory vs Trivy detail, `/admin/v1/scan-settings` authority reuse, upgrade migration when state is absent | Temp SQLite + stubs + `httptest`; full `go test ./...` after GREEN. |
| E2E | `setup -> feature list/show/status trivy -> upgrade -> uninstall` remains truthful and generic-model aligned | Extend lifecycle smoke/docs proof. |

## Threat Matrix

| Boundary | Minimum adversarial cases | Applicability | Design response | Planned RED tests |
|---|---|---|---|---|
| Documentation-like paths | `requirements.txt`, `CMakeLists.txt`, executable Markdown/MDX, `README.sh` | N/A — no executable classification | None | None |
| Git repository selection | `git -C`, relative paths, absolute paths | N/A — no Git operations | None | None |
| Commit state | staged, `commit -a`, empty index | N/A — no commit automation | None | None |
| Push state | tracking branch, first push, explicit refspec | N/A — no push automation | None | None |
| PR commands | explicit `--head`, environment prefix, composed commands | N/A — no PR automation | None | None |

Process-integration requirement: feature CLI routing and lifecycle replay MUST keep explicit argv/systemctl behavior and add RED tests for identity validation, legacy Trivy import, and failing external-binary runtime probes.

## Migration / Rollout

Compatibility-first rollout: keep `setup --trivy-*` for one migration slice, import them only when `trivy` feature state is absent, and direct operators to `regixtry feature ...` afterward. No dynamic plugin loader is introduced; future capabilities are added by registering more built-in descriptors in code.

## Open Questions

- [ ] Decide whether this slice needs admin/TUI feature list/status reads or only CLI normalization.
