# Trivy Runtime Specification

## Purpose

Define Regixtry-owned Trivy runtime ownership, lifecycle, and scan execution.

## Requirements

### Requirement: Private Runtime Ownership

The system MUST install Trivy under the Regixtry storage root, SHALL reserve that runtime for Regixtry use only, and MUST separate operator scan intent from runtime lifecycle state.

#### Scenario: Managed runtime is installed privately

- GIVEN the operator installs the `trivy` feature runtime
- WHEN installation succeeds
- THEN the system SHALL place the runtime under the Regixtry-managed Trivy storage tree

#### Scenario: Operator intent remains separate from runtime state

- GIVEN scan settings are already configured
- WHEN runtime lifecycle state changes
- THEN the system MUST preserve operator scan intent independently from runtime metadata

### Requirement: Verified Activation and Rollback Safety

The system MUST verify official Trivy release evidence before activation, MUST fail closed on verification mismatch, and SHALL switch active versions atomically while retaining one rollback target.

#### Scenario: Verified upgrade becomes active

- GIVEN a new Trivy version is staged beside the active version
- WHEN verification and post-activation health checks succeed
- THEN the system SHALL atomically activate the new version and retain the previous version for rollback

#### Scenario: Verification or activation fails

- GIVEN a staged Trivy version cannot be verified or proves unhealthy
- WHEN activation is attempted
- THEN the system MUST keep or restore the previous active version and record the failure truthfully

### Requirement: Runtime Lifecycle Status

The system MUST support install, upgrade, rollback, and status flows for the managed runtime, and status SHALL report version, readiness, verification evidence, rollback availability, and the last lifecycle error when present.

#### Scenario: Runtime status is ready

- GIVEN a verified managed Trivy runtime is active
- WHEN runtime status is requested
- THEN the system SHALL report the active version as ready with current verification evidence

#### Scenario: Legacy service-shaped state is present

- GIVEN persisted Trivy state still reflects the superseded HTTP-service model
- WHEN runtime status is requested
- THEN the system MUST surface a guided migration or degraded state without claiming managed-runtime readiness

### Requirement: Managed Runtime Scan Execution

The system MUST reuse existing manual and scheduled scan orchestration while executing scans through the active managed Trivy runtime instead of the superseded HTTP-service model.

#### Scenario: Manual scan uses the active runtime

- GIVEN a ready managed Trivy runtime exists
- WHEN an operator queues a manual scan
- THEN the system SHALL execute that scan through the active managed runtime

#### Scenario: Scheduled batch uses the active runtime

- GIVEN scheduled scanning is enabled and the runtime is ready
- WHEN a batch run starts
- THEN the system SHALL execute scheduled scans through the same managed runtime contract
