# Delta for installation-modes

## MODIFIED Requirements

### Requirement: Truthful Mode Contract

The system MUST present install and upgrade lifecycle outcomes through binary-owned commands. In this slice, `daemon-sqlite` SHALL remain the only supported automated install mode, and in-place upgrade SHALL remain supported only for lifecycle-managed Linux + systemd targets. Unsupported modes, unmanaged installs, or unsupported targets MUST fail without implying success.
(Previously: the contract covered setup only and explicitly avoided implying upgrade support.)

#### Scenario: Supported lifecycle target is selected

- GIVEN an operator runs phase-1 setup for `daemon-sqlite` on Linux + systemd
- WHEN the lifecycle contract is evaluated
- THEN the system SHALL continue with the supported binary-owned flow

#### Scenario: Unsupported mode or target is requested

- GIVEN an operator requests another mode, an unmanaged install, or a non-systemd target
- WHEN validation runs
- THEN the system MUST fail with a clear unsupported result

### Requirement: Scoped Rollback

Rollback, uninstall, and failed in-place upgrade recovery MUST be provenance-driven best-effort cleanup or restoration. During upgrade failure, the system SHALL restore the previous binary and prior generated env and unit artifacts, and it MUST report restored, skipped, or unresolved items truthfully.
(Previously: scoped rollback only described cleanup for rollback or uninstall.)

#### Scenario: Recorded lifecycle state is cleaned up

- GIVEN a prior setup recorded service and runtime artifacts
- WHEN rollback or uninstall runs
- THEN the system SHALL remove recorded artifacts best effort and leave a truthful cleanup report

#### Scenario: Drift prevents complete cleanup

- GIVEN some recorded artifacts no longer match host state
- WHEN rollback or uninstall runs
- THEN the system MUST report which cleanup steps were skipped or incomplete

#### Scenario: Failed upgrade restores prior runnable state

- GIVEN a staged upgrade replaced managed artifacts
- WHEN restart or readiness validation fails
- THEN the system SHALL restore the prior runnable lifecycle state

## ADDED Requirements

### Requirement: Upgrade Intent Preservation Compatibility

For lifecycle-managed upgrades, the system MUST reconstruct runtime intent from lifecycle provenance plus backward-compatible env data, preserve storage, database, public URL, listen, TLS, and auth settings, and keep existing config and provenance readable across old and new installs.

#### Scenario: Existing install upgrades without config drift

- GIVEN lifecycle provenance lacks newer structured fields but `regixtry.env` is present
- WHEN the operator runs `regixtry upgrade`
- THEN the system SHALL preserve equivalent runtime intent without requiring re-entry

#### Scenario: Missing required intent allows guided recovery only when needed

- GIVEN required upgrade intent cannot be reconstructed from provenance or env
- WHEN the operator runs `regixtry upgrade`
- THEN the system MAY prompt for only the missing lifecycle-critical values or fail truthfully
