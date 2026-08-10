# Proposal: Managed Trivy Runtime

## Intent

Supersede the `trivy-service-runtime` external HTTP model with a Regixtry-owned private Trivy runtime. Keep feature identity as `trivy`, preserve the existing scan orchestration seams, and make install/upgrade/rollback/status explicit and truthful.

## Scope

### In Scope
- Add managed `trivy` runtime lifecycle flows: install, upgrade, rollback, and status.
- Reuse existing queueing, scheduler, admin, TUI, and feature-config seams while switching execution from HTTP service calls to the active managed binary.
- Persist runtime version, activation, rollback, and verification evidence separately from operator scan settings.

### Out of Scope
- Keeping `external_service` as a supported steady-state runtime.
- New scan policy semantics, CI-first scanning changes, or non-Trivy feature work.

## Capabilities

### New Capabilities
- `trivy-runtime`: Managed private Trivy binary ownership, verification, activation, rollback, and runtime status for the built-in `trivy` feature.

### Modified Capabilities
- `lifecycle-cli`: add feature-owned `trivy` install/upgrade/rollback flows without expanding base setup ownership.
- `operator-admin-tui`: expose managed runtime state and actions through existing admin and TUI feature surfaces.

## Approach

Keep `scan_settings` as feature intent only (`enabled`, schedule, timeout, concurrency). Store runtime state separately under the storage root (`features/trivy/bin`, `cache`, `downloads`, `receipts`), install versions side-by-side, verify official release artifacts before activation, atomically switch the active binary, and retain the previous version for rollback. Benchmark alignment: Harbor keeps scanners as operator-managed integrations and GitLab keeps Trivy in CI; Regixtry instead owns a private runtime because it already owns bounded in-product rescan orchestration.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/app/regixtry/{feature_registry.go,service_scanning.go}` | Modified | Replace external-service runtime assumptions while reusing scan orchestration |
| `internal/infra/scanning/trivy/runner.go` | Modified | Execute/probe the active managed binary |
| `internal/infra/metadata/sqlite/store.go` | Modified | Persist managed runtime state and rollback evidence |
| `cmd/regixtry/main.go` | Modified | Add feature lifecycle CLI wiring |
| `internal/protocol/http/`, `internal/tui/` | Modified | Surface runtime status/actions through existing admin/TUI seams |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Third-party binary verification is incomplete | High | Fail closed on checksum/signature mismatch and persist evidence |
| Upgrade corrupts shared runtime/cache state | Med | Stage side-by-side, switch atomically, keep rollback target |
| Legacy service config becomes ambiguous | Med | Mark `trivy-service-runtime` superseded and migrate truthfully |

## Rollback Plan

Restore the previous managed binary pointer and persisted runtime state, stop using the new version, and temporarily reinstate the prior runtime contract while leaving scan settings unchanged.

## Dependencies

- Official Trivy release assets plus checksum/signature metadata.
- Existing rescan scheduler, feature APIs, and admin/TUI projections.

## Success Criteria

- [ ] Specs keep the feature name `trivy` while replacing the external-service runtime model.
- [ ] Managed install, upgrade, rollback, and status behavior are explicit and verifiable.
- [ ] Existing manual/scheduled scan orchestration reuses current seams with managed-binary execution.

## Proposal question round

- Should rollback remain operator-invoked only, or auto-trigger after failed post-upgrade health verification?
- Should unsupported legacy `external_service` settings hard-fail immediately or surface a guided migration state first?
