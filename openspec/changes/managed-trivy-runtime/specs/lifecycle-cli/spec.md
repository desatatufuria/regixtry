# Delta for lifecycle-cli

## MODIFIED Requirements

### Requirement: Binary Lifecycle Entrypoints

The `regixtry` binary MUST expose `setup` and `uninstall` lifecycle commands. `setup` SHALL remain the base install entrypoint, and feature-owned Trivy runtime lifecycle MUST be exposed through `regixtry feature install trivy`, `regixtry feature upgrade trivy`, and `regixtry feature rollback trivy`. Base `upgrade` MAY orchestrate feature upgrades, but it MUST keep `trivy` runtime ownership explicit.
(Previously: `upgrade` stayed deferred and no feature-owned Trivy runtime lifecycle commands existed.)

#### Scenario: Setup command is available

- GIVEN a supported operator host
- WHEN the operator runs `regixtry setup`
- THEN the system SHALL enter the binary-owned lifecycle flow

#### Scenario: Feature runtime lifecycle command is requested

- GIVEN the operator manages the built-in `trivy` feature
- WHEN the operator runs a supported `regixtry feature ... trivy` lifecycle command
- THEN the system MUST execute the matching managed-runtime lifecycle flow without reassigning base setup ownership

## ADDED Requirements

### Requirement: Truthful Trivy Runtime Status Command

The CLI MUST expose truthful `trivy` runtime status through existing feature surfaces and MUST distinguish operator intent from runtime lifecycle state.

#### Scenario: Feature status reports separated state

- GIVEN Trivy scan settings and managed runtime state both exist
- WHEN the operator requests `trivy` feature status
- THEN the system SHALL report configuration intent separately from runtime lifecycle readiness
