# Managed Feature Runtime Specification

## Purpose

Define a feature-agnostic contract for Regixtry-owned managed binary runtimes (per-feature state, install, upgrade, rollback, status), replacing the Trivy-only plumbing so a second managed feature (Gitleaks) can be added as configuration, not a parallel implementation.

## Current Repository Facts

- `ports.MetadataStore.Get/UpsertTrivyRuntimeState` hardcodes the feature identity — feature is not a parameter.
- `trivy_runtime_state` (`internal/infra/metadata/sqlite/store.go:499-561`) keys on `tenant` alone; no `feature` column.
- `projectFeatureRuntime` (`internal/app/regixtry/feature_registry.go:126`) is hardcoded to Trivy for every `ListFeatures` entry.
- `internal/infra/install/releases/github.go` duplicates Trivy's download/verify/extract logic, with the binary name hardcoded.

## Requirements

### Requirement: Feature-Parameterized Runtime State Port

The metadata port MUST expose runtime-state read and write operations parameterized by feature identity, and the persisted store MUST key state on `(tenant, feature)` rather than `tenant` alone.

#### Scenario: Two features persist independent runtime state

- GIVEN Trivy and Gitleaks both have runtime state persisted for the same tenant
- WHEN either feature's runtime state is written
- THEN the system SHALL persist it under its own `(tenant, feature)` key without altering the other feature's row

#### Scenario: Runtime-state read requires explicit feature identity

- GIVEN a runtime-state read is requested
- WHEN no feature identity is supplied
- THEN the system MUST reject the call rather than default to a single hardcoded feature

### Requirement: Feature-Keyed Runtime Projection

`ListFeatures` MUST project each registered feature's runtime status from that feature's own runtime-state row, not from a shared or hardcoded source.

#### Scenario: ListFeatures reports each feature's own runtime state

- GIVEN Trivy is active and Gitleaks is installing
- WHEN `ListFeatures` is called
- THEN the Trivy entry SHALL show Trivy's state and the Gitleaks entry SHALL show Gitleaks' state, with neither entry reporting the other's status

#### Scenario: Unregistered feature yields no runtime projection

- GIVEN a feature identity has no registered runtime manager
- WHEN `ListFeatures` builds that feature's entry
- THEN the system MUST NOT substitute another feature's runtime state

### Requirement: Shared GitHub-Release Binary Staging

The system MUST use one shared component for resolving, downloading, checksum-verifying, and extracting a managed feature's release binary. Feature-specific asset naming and binary naming MUST be supplied as parameters, not duplicated per feature.

#### Scenario: Feature-specific asset resolved and staged

- GIVEN a managed feature declares its release repository and asset-naming pattern
- WHEN the shared staging component resolves a release for that feature
- THEN the system SHALL download, verify, and extract using the feature's own parameters through the single shared implementation

#### Scenario: Checksum mismatch fails closed

- GIVEN a staged archive's checksum does not match published release checksums
- WHEN staging verification runs
- THEN the system MUST fail the staging operation and MUST NOT activate the mismatched binary

### Requirement: Managed Binary Lifecycle Sequence

For any managed feature, the system MUST resolve the release, verify its checksum, extract the binary, activate atomically, probe the activated binary, and roll back on failure — preserving Trivy's proven sequence as the shared contract.

#### Scenario: New feature installs through the shared sequence

- GIVEN a new managed feature (e.g. Gitleaks) is installed for the first time
- WHEN install runs
- THEN the system SHALL execute resolve, verify, extract, activate, and probe in order and report readiness only if all steps succeed

#### Scenario: Failed verification or probe rolls back atomically

- GIVEN a staged version fails checksum verification or fails its post-activation probe
- WHEN activation is attempted
- THEN the system MUST keep or restore the previously active version for that feature and record the failure truthfully in that feature's runtime state
