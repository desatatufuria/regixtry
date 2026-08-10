# Feature Configuration Specification

## Purpose

Define how built-in optional features are managed through feature-owned state instead of install-owned setup truth.

## Requirements

### Requirement: Feature-Owned Runtime Authority

Optional feature settings MUST live in authoritative feature state. Runtime services, admin APIs, and operator CLI flows SHALL read and write that state, and base setup MUST NOT remain the long-term authority after migration.

#### Scenario: Runtime uses feature-owned state

- GIVEN authoritative Trivy feature state exists
- WHEN runtime or admin reads feature configuration
- THEN the system SHALL use the persisted feature state as the source of truth

### Requirement: Legacy Setup Import Bridge

For one migration slice, `regixtry setup` MAY accept legacy `--trivy-*` inputs. If authoritative feature state is absent, the system MUST import those values once. If authoritative feature state already exists, the system MUST NOT overwrite it.

#### Scenario: Legacy setup seeds missing feature state

- GIVEN no authoritative Trivy feature state exists
- WHEN setup receives legacy `--trivy-*` inputs
- THEN the system MUST import them into feature-owned state

#### Scenario: Existing feature state wins

- GIVEN authoritative Trivy feature state already exists
- WHEN setup or upgrade replays legacy feature inputs
- THEN the system MUST preserve the existing feature-owned state

### Requirement: Feature-Oriented Management Model

The system MUST expose built-in capabilities through a feature-oriented operator model. The CLI MUST provide `regixtry feature list`, `show <name>`, `status <name>`, `enable <name>`, `disable <name>`, and `configure <name> ...`. CLI and admin surfaces SHOULD enforce the same validation and MAY expose feature-specific fields inside `configure`.

#### Scenario: Operator lists built-in features

- GIVEN one or more built-in capabilities are supported
- WHEN the operator runs `regixtry feature list`
- THEN the system SHALL return the supported feature names without implying dynamic plugin discovery

#### Scenario: Operator configures a built-in feature

- GIVEN Trivy is a supported built-in feature
- WHEN an operator runs `regixtry feature configure trivy ...`
- THEN the system SHALL apply the validated change to feature-owned state

#### Scenario: Unknown feature name is rejected

- GIVEN an operator supplies an unsupported feature name
- WHEN a named feature command is validated
- THEN the system MUST reject the request without attempting plugin discovery

### Requirement: Feature Status Distinguishes Capability From Engine Health

Built-in feature status MUST distinguish capability state from engine-specific runtime state. `status <name>` SHALL report whether the named built-in capability is configured or enabled, and feature-specific engines such as Trivy SHOULD also report validation-style runtime feedback such as binary availability, version, or readiness when that evidence exists.

#### Scenario: Trivy status reports capability and engine state

- GIVEN Trivy is a built-in capability backed by an external runtime engine
- WHEN the operator runs `regixtry feature status trivy`
- THEN the system SHALL report feature-owned capability state
- AND it SHOULD include validation-style engine or runtime feedback when available

### Requirement: No Dynamic Plugin Loader

The system MUST treat features as built-in named capabilities. It MUST NOT discover, download, or load arbitrary feature plugins at runtime.

#### Scenario: Unsupported plugin-style request is rejected

- GIVEN an operator attempts to register an arbitrary feature plugin
- WHEN the feature model validates the request
- THEN the system MUST reject it as out of scope
