# Proposal: Separate Feature Config From Setup

## Intent

Split base install/bootstrap truth from optional feature truth so operators manage Trivy as a capability, not as a permanent side effect of `regixtry setup`.

## Scope

### In Scope
- Narrow `setup` ownership to base service/bootstrap fields only.
- Introduce a feature-oriented config contract for Trivy across CLI/admin surfaces and runtime persistence.
- Add a compatibility bridge from legacy `setup --trivy-*` values into authoritative feature state.

### Out of Scope
- Dynamic plugin loading or arbitrary third-party feature discovery.
- Additional built-in features beyond the existing Trivy-backed capability in this change.

## Capabilities

### New Capabilities
- `feature-configuration`: Manage optional capabilities through feature-owned state, validation, migration, and operator-facing commands.

### Modified Capabilities
- `installation-modes`: setup truth shrinks to base bootstrap assets while preserving truthful uninstall/rollback.
- `lifecycle-cli`: lifecycle commands stop treating feature flags as long-term install-owned truth and preserve a legacy migration bridge.
- `operator-admin-tui`: admin-facing feature configuration/read surfaces may expand beyond users/grants/tokens for Trivy management.

## Approach

- Keep bootstrap ownership limited to binary/service paths, network/TLS, storage, and auth/runtime essentials.
- Move Trivy settings authority to feature persistence (`scan_settings`) with one-time seeding/import when no authoritative row exists.
- Add a feature CLI namespace for `list|show|status|enable|disable|configure`; reuse the same validation as admin APIs.
- Treat features as built-in capabilities, while allowing engines such as Trivy to report their own version/runtime health through feature status.
- In this slice, place Trivy validation or doctor-style guidance under `feature status trivy` rather than adding a separate `doctor` command.
- Preserve `setup --trivy-*` for one migration slice, then deprecate and remove feature ownership from lifecycle replay.
- Benchmark alignment: Harbor treats scanner connections and schedules as admin-managed configuration, while GitLab treats Trivy scanning as security/CI configuration rather than installer truth.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `cmd/regixtry/main.go` | Modified | Separate setup flags from feature-management commands and legacy bridge logic. |
| `internal/infra/install/linux/` | Modified | Remove long-term feature ownership from bootstrap, provenance, templates, intent, and upgrade replay. |
| `internal/app/regixtry/service_scanning.go` | Modified | Seed/import defaults only when authoritative feature state is absent. |
| `internal/protocol/http/`, `internal/tui/` | Modified | Align admin-facing feature operations with shared runtime authority. |
| `openspec/specs/*`, `README.md`, `docs/` | Modified | Document the new lifecycle boundary, migration path, and operator UX. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Upgrade still rewrites Trivy state from lifecycle intent | High | Change replay to prefer authoritative feature state after migration. |
| Uninstall loses truth for feature-created assets | Medium | Track feature lifecycle evidence separately from base bootstrap provenance. |
| CLI/admin paths diverge | Medium | Centralize validation and persistence in app-service seams. |

## Rollback Plan

Revert feature CLI/admin entrypoints, keep `setup --trivy-*` as sole source, and restore bootstrap/provenance replay of Trivy env values until migration is reworked.

## Dependencies

- Existing `scan_settings` persistence and `/admin/v1/scan-settings` endpoints remain the authority seam.
- Follow strict TDD repository rules for later spec/design/apply phases with `go test ./...`.

## Success Criteria

- [ ] Setup specs and lifecycle specs clearly separate base install truth from optional feature truth.
- [ ] Trivy can be managed through `regixtry feature list|show|status|enable|disable|configure` without requiring rerunning setup.
- [ ] Legacy `setup --trivy-*` installs migrate without upgrade/uninstall overstating ownership.
