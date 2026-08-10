# Proposal: Trivy Service Runtime

## Intent

Replace the `trivy` feature's host-binary runtime assumption with a service-first runtime that works for localhost containers and remote URLs. This aligns Regixtry with operator-managed scanner services, mirrors Harbor's external scanner direction, and uses Trivy server health/version endpoints instead of local binary probing.

## Scope

### In Scope
- Define a service-oriented `trivy` config contract: `service_url`, auth, TLS, health/version, and registry-reachable address.
- Preserve existing feature identity, enablement, schedule, timeout, and concurrency behavior while switching runtime semantics to `external_service`.
- Add a temporary migration bridge that can import legacy binary-backed Trivy settings into the new feature state without making binary mode the steady-state model.

### Out of Scope
- New scan policy semantics, CI pipeline changes, or non-Trivy feature work.
- Long-term support for dual binary/service runtimes.

## Capabilities

### New Capabilities
- `trivy`: Service-backed configuration, status, validation, and operator-facing runtime behavior for the `trivy` feature.

### Modified Capabilities
- `installation-modes`: Lifecycle/bootstrap behavior stops assuming Regixtry owns a local Trivy binary and defines transitional import of legacy Trivy settings.

## Approach

Re-use the existing feature authority seam (`scan_settings` via service/admin/TUI) and change the contract, not the feature name. Make `external_service` the target runtime, probe `/healthz` and `/version`, drop Regixtry-owned `cache_dir`, and add explicit registry address configuration so off-host scanners can reach the registry. Keep compatibility code narrow and explicitly temporary.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/ports/regixtry.go` | Modified | Service config DTOs replace binary-oriented fields |
| `internal/app/regixtry/service_scanning.go` | Modified | Normalize service config and remote scan target rules |
| `internal/app/regixtry/feature_registry.go` | Modified | Health/version/status move from local binary checks to service probes |
| `cmd/regixtry/main.go` | Modified | Runtime wiring and legacy flag bridge change |
| `internal/infra/install/linux/bootstrap.go` | Modified | Transitional lifecycle import/compatibility rules |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Mixed legacy and service config becomes ambiguous | Med | Define one canonical stored shape and one-way bridge rules |
| Remote scanner cannot reach registry | High | Add explicit registry-reachable address validation and docs |
| Weak auth/TLS defaults | Med | Require explicit URL scheme, model auth/TLS separately, keep insecure TLS opt-in only |

## Rollback Plan

Revert to the prior binary-backed runtime wiring, keep legacy config fields readable, and disable service-only validation until the service contract is reworked.

## Dependencies

- Trivy client/server contract: `/healthz`, `/version`, token/header auth, and `http|https` server URLs.
- Documentation updates for localhost-container and remote-service deployment examples.

## Success Criteria

- [ ] Specs clearly define `trivy` as service-first without renaming the feature.
- [ ] The proposal preserves migration truth for existing binary-backed installs.
- [ ] Remote and localhost-container deployment models have explicit configuration and registry reachability rules.
