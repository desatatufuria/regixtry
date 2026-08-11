# Delta for trivy-runtime

## MODIFIED Requirements

### Requirement: Private Runtime Ownership

The system MUST install Trivy under the Regixtry storage root, SHALL reserve that runtime for Regixtry use only, and MUST separate operator scan intent from runtime lifecycle state. Trivy's runtime state MUST be persisted as one instance of the feature-keyed `managed-feature-runtime` contract, keyed on `(tenant, "trivy")`, not on `tenant` alone.
(Previously: runtime state was persisted keyed on `tenant` alone with the feature implicitly Trivy.)

#### Scenario: Managed runtime is installed privately

- GIVEN the operator installs the `trivy` feature runtime
- WHEN installation succeeds
- THEN the system SHALL place the runtime under the Regixtry-managed Trivy storage tree

#### Scenario: Operator intent remains separate from runtime state

- GIVEN scan settings are already configured
- WHEN runtime lifecycle state changes
- THEN the system MUST preserve operator scan intent independently from runtime metadata

#### Scenario: Trivy runtime state does not leak into another feature's row

- GIVEN both Trivy and another managed feature have runtime state persisted for the same tenant
- WHEN Trivy's runtime state is written
- THEN the system MUST write only the `(tenant, "trivy")` row and MUST NOT alter the other feature's row

### Requirement: Runtime Lifecycle Status

The system MUST support install, upgrade, rollback, and status flows for the managed Trivy runtime using the shared feature-keyed runtime-state contract, and status SHALL report version, readiness, verification evidence, rollback availability, and the last lifecycle error when present.
(Previously: status flows read and wrote runtime state without a feature key, since Trivy was the only managed feature.)

#### Scenario: Runtime status is ready

- GIVEN a verified managed Trivy runtime is active
- WHEN runtime status is requested
- THEN the system SHALL report the active version as ready with current verification evidence

#### Scenario: Legacy service-shaped state is present

- GIVEN persisted Trivy state still reflects the superseded HTTP-service model
- WHEN runtime status is requested
- THEN the system MUST surface a guided migration or degraded state without claiming managed-runtime readiness

#### Scenario: ListFeatures shows Trivy's own status only

- GIVEN Trivy and another managed feature are both registered
- WHEN `ListFeatures` is called
- THEN the Trivy entry SHALL reflect only Trivy's `(tenant, "trivy")` runtime state
