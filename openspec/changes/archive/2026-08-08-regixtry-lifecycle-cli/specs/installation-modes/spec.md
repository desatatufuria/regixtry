# Delta for installation-modes

## MODIFIED Requirements

### Requirement: Truthful Mode Contract

The system MUST present install lifecycle outcomes through binary-owned commands. In this slice, `daemon-sqlite` SHALL remain the only supported automated install mode, and the system MUST state Linux + systemd as the only supported lifecycle target. Unsupported modes or targets MUST fail without implying Postgres or upgrade support.
(Previously: install bootstrap was the primary mode contract and mode validation did not shift lifecycle ownership into the binary.)

#### Scenario: Supported lifecycle target is selected

- GIVEN an operator runs phase-1 setup for `daemon-sqlite` on Linux + systemd
- WHEN the lifecycle contract is evaluated
- THEN the system SHALL continue with the supported binary-owned flow

#### Scenario: Unsupported mode or target is requested

- GIVEN an operator requests another mode or a non-systemd target
- WHEN validation runs
- THEN the system MUST fail with a clear unsupported result

### Requirement: Linux Bootstrap Artifacts

For `daemon-sqlite`, the system MUST generate the runtime and service artifacts required for service activation on Linux + systemd hosts. The system MUST reject non-Linux or non-systemd targets as unsupported rather than implying deferred environments succeeded.
(Previously: the requirement enumerated supported Linux distributions instead of the Linux + systemd lifecycle boundary.)

#### Scenario: Supported host receives runnable artifacts

- GIVEN the operator runs setup on Linux + systemd
- WHEN artifact generation completes
- THEN the system SHALL leave required runtime and service artifacts ready for activation

#### Scenario: Unsupported environment is not overstated

- GIVEN the operator runs setup on a non-Linux or non-systemd target
- WHEN compatibility is evaluated
- THEN the system MUST not report successful setup for that environment

### Requirement: Default Service Activation and Reachability

For phase 1, setup success MUST mean the binary is installed, the service is running, and the registry is reachable. The system SHALL treat partial completion as failure and MUST return verification results that distinguish startup or reachability problems.
(Previously: success focused on activated bootstrap artifacts plus reachability, without requiring installed-binary success as part of the contract.)

#### Scenario: Setup reaches minimum success

- GIVEN binary placement and runtime artifact generation succeeded
- WHEN service activation completes and the registry becomes reachable
- THEN the system SHALL report successful installation

#### Scenario: Service does not become reachable

- GIVEN the binary was installed but startup or readiness fails
- WHEN success is evaluated
- THEN the system MUST report installation failure

### Requirement: Scoped Rollback

Rollback and uninstall MUST be provenance-driven best-effort cleanup. The system SHALL remove recorded lifecycle artifacts and stop or disable recorded services without requiring prior operator parameters, and it MUST report removed, skipped, or already-missing items truthfully. The system MUST NOT claim full cleanup for unrecorded or drifted state.
(Previously: rollback assumed a known bootstrap run and focused on deleting generated artifacts while leaving the binary installed.)

#### Scenario: Recorded lifecycle state is cleaned up

- GIVEN a prior setup recorded service and runtime artifacts
- WHEN rollback or uninstall runs
- THEN the system SHALL remove recorded artifacts best effort and leave a truthful cleanup report

#### Scenario: Drift prevents complete cleanup

- GIVEN some recorded artifacts no longer match host state
- WHEN rollback or uninstall runs
- THEN the system MUST report which cleanup steps were skipped or incomplete
