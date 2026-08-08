# Delta for lifecycle-cli

## MODIFIED Requirements

### Requirement: Binary Lifecycle Entrypoints

The `regixtry` binary MUST expose `setup`, `upgrade`, and `uninstall` lifecycle commands. `setup` SHALL remain the phase-1 operator entrypoint, and `upgrade` SHALL run only for lifecycle-managed Linux + systemd installs.
(Previously: `upgrade` could remain reserved and report deferred status.)

#### Scenario: Setup command is available

- GIVEN a supported operator host
- WHEN the operator runs `regixtry setup`
- THEN the system SHALL enter the binary-owned lifecycle flow

#### Scenario: Upgrade command is available

- GIVEN a lifecycle-managed install on Linux + systemd
- WHEN the operator runs `regixtry upgrade`
- THEN the system SHALL enter the binary-owned upgrade flow

#### Scenario: Unsupported upgrade target is rejected

- GIVEN an unsupported target or unmanaged install
- WHEN the operator runs `regixtry upgrade`
- THEN the system MUST fail without claiming an upgrade happened

## ADDED Requirements

### Requirement: Upgrade Version Selection And Prompts

`regixtry upgrade` MUST default to the latest compatible release and MAY accept an explicit target ref or version. The system MUST stay non-interactive for same-shape upgrades and MAY prompt only when required intent cannot be reconstructed or operator confirmation is required for a risky transition such as a major-version upgrade.

#### Scenario: Latest release is selected by default

- GIVEN a lifecycle-managed install with no explicit target
- WHEN the operator runs `regixtry upgrade`
- THEN the system SHALL resolve the latest compatible release

#### Scenario: Prompting is gated to risky or incomplete cases

- GIVEN the target is same-shape and current intent is reconstructable
- WHEN the operator runs `regixtry upgrade`
- THEN the system MUST NOT prompt for routine reconfiguration

### Requirement: Upgrade Staging And Validation

The system MUST stage and checksum-verify the target binary before stopping the running service, MUST replace managed artifacts only after staging succeeds, and MUST declare upgrade success only after restart plus lifecycle readiness validation succeed.

#### Scenario: Staged upgrade reaches success

- GIVEN a valid target release and healthy recorded service
- WHEN staging, replacement, restart, and readiness checks succeed
- THEN the system SHALL report a successful upgrade

#### Scenario: Staging failure prevents service interruption

- GIVEN the target binary cannot be verified
- WHEN staging fails before replacement
- THEN the system MUST leave the current service and binary unchanged
