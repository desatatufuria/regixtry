# Delta for Installation Modes

## MODIFIED Requirements

### Requirement: Linux Bootstrap Artifacts

For `daemon-sqlite`, the system MUST generate the runtime and service artifacts required for registry service activation on Linux + systemd hosts. The lifecycle flow MUST NOT install, validate, or own a host Trivy binary as part of steady-state setup. The lifecycle flow MAY accept legacy Trivy bootstrap inputs only to import compatible `trivy` feature state when no service-backed feature config exists yet. The system MUST reject non-Linux or non-systemd targets as unsupported rather than implying deferred environments succeeded.

(Previously: Setup generated Linux runtime artifacts while implicitly allowing local Trivy-binary ownership assumptions.)

#### Scenario: Supported host receives runnable registry artifacts

- GIVEN the operator runs setup on Linux + systemd
- WHEN artifact generation completes
- THEN the system SHALL leave required registry runtime and service artifacts ready for activation

#### Scenario: Unsupported environment is not overstated

- GIVEN the operator runs setup on a non-Linux or non-systemd target
- WHEN compatibility is evaluated
- THEN the system MUST not report successful setup for that environment

#### Scenario: Legacy Trivy inputs are treated as a temporary bridge

- GIVEN setup receives legacy Trivy bootstrap inputs and no persisted feature config exists yet
- WHEN setup imports Trivy-related state
- THEN the system MUST bridge that state into the service-backed `trivy` contract without establishing host-binary ownership as steady state
