# Delta for lifecycle-cli

## Current Repository Facts

- `cmd/regixtry/main.go` already routes a `feature` subcommand (`case "feature": return runFeature(ctx, args[1:], stdout)`) alongside `setup` and `uninstall`.

## ADDED Requirements

### Requirement: Feature Identity Accepted By The Feature Subcommand

The `regixtry feature` subcommand MUST accept a feature identity argument and dispatch lifecycle actions (install, upgrade, rollback, status) to the managed-feature-runtime instance matching that identity. `gitleaks` MUST be accepted as a valid feature identity alongside `trivy`.

#### Scenario: Feature subcommand installs Gitleaks

- GIVEN the operator runs `regixtry feature gitleaks install`
- WHEN the command executes
- THEN the system SHALL dispatch the install lifecycle action to the Gitleaks managed-feature-runtime instance

#### Scenario: Unknown feature identity is rejected

- GIVEN the operator runs `regixtry feature <unknown> status`
- WHEN no managed-feature-runtime instance is registered for that identity
- THEN the system MUST report the feature identity as unrecognized rather than silently defaulting to Trivy
