# Delta for lifecycle-cli

## ADDED Requirements

### Requirement: Feature Management Entrypoints

The `regixtry` binary MUST expose a feature-oriented operator model for built-in capabilities through `regixtry feature list`, `show <name>`, `status <name>`, `enable <name>`, `disable <name>`, and `configure <name> ...`. These actions SHALL target feature-owned state, SHOULD reuse the same validation as admin-facing feature management, and MUST reject unknown names without implying arbitrary plugin loading.

#### Scenario: Operator lists supported built-in features

- GIVEN one or more built-in capabilities are supported
- WHEN the operator runs `regixtry feature list`
- THEN the system SHALL enumerate supported feature names

#### Scenario: Operator inspects named feature status

- GIVEN Trivy is a supported built-in feature
- WHEN the operator runs `regixtry feature status trivy`
- THEN the system SHALL read authoritative feature state
- AND it SHOULD report Trivy engine or runtime validation feedback when available

#### Scenario: Unknown feature command target is rejected

- GIVEN an operator supplies an unsupported feature name
- WHEN the operator runs a named `regixtry feature` command
- THEN the system MUST fail with a clear unsupported-feature result

### Requirement: Legacy Setup Migration Bridge

For one migration slice, `regixtry setup` MAY continue accepting legacy `--trivy-*` flags. The system MUST import those values only when authoritative feature state is missing, and it SHOULD direct operators toward feature management commands afterward.

#### Scenario: Existing feature state is not overwritten

- GIVEN authoritative feature state already exists
- WHEN setup is run with legacy `--trivy-*` flags
- THEN the system MUST preserve the existing feature state

## MODIFIED Requirements

### Requirement: Binary Lifecycle Entrypoints

The `regixtry` binary MUST expose `setup`, `uninstall`, and `upgrade` lifecycle commands. `setup` SHALL remain the base bootstrap entrypoint. `upgrade` and `uninstall` MUST preserve feature-owned state as authoritative and MUST NOT treat legacy setup feature inputs as long-term lifecycle truth.

(Previously: The requirement exposed `setup` and `uninstall` while allowing `upgrade` to remain deferred in this slice.)

#### Scenario: Setup command is available

- GIVEN a supported operator host
- WHEN the operator runs `regixtry setup`
- THEN the system SHALL enter the binary-owned base lifecycle flow

#### Scenario: Upgrade preserves feature authority

- GIVEN authoritative feature state already exists
- WHEN the operator runs `regixtry upgrade`
- THEN the system MUST keep feature-owned state authoritative after lifecycle replay
