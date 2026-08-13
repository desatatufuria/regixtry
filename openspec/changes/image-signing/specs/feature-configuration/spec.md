# Delta for feature-configuration

## Baseline Note

Builds on `feature-configuration`'s current stacked state:
`separate-feature-config-from-setup`
(`openspec/changes/separate-feature-config-from-setup/specs/feature-configuration/spec.md`)
plus the unarchived `repository-scan-config-overrides` delta on top of it
(`openspec/changes/repository-scan-config-overrides/specs/feature-configuration/spec.md`;
no merged baseline exists yet in `openspec/specs/`). This change adds new
behavior only — override-resolution order, the legacy setup import bridge,
the feature-oriented CLI model, status reporting, and the no-dynamic-
plugin-loader requirement are unaffected and not restated here.

## ADDED Requirements

### Requirement: Builtin Features May Have No Runtime Manager

A feature registered as `FeatureKindBuiltin` MAY have no associated
runtime manager. When a feature has no runtime manager, the system MUST
expose only Enable, Disable, and Configure actions for it through the
admin API and operator CLI, and MUST NOT expose Install, Upgrade, or
Rollback for that feature under any surface. Its reported version, if any,
is informational only and MUST NOT imply a managed-runtime lifecycle.

#### Scenario: Signing feature registers without a runtime manager
- GIVEN the `signing` feature is `FeatureKindBuiltin` with no runtime
  manager configured
- WHEN an operator lists or inspects the `signing` feature
- THEN the system SHALL report it as configurable and enableable, without
  runtime-lifecycle fields implying an installable engine

#### Scenario: Install, Upgrade, and Rollback are unavailable
- GIVEN the `signing` feature has no runtime manager
- WHEN an operator or the admin API attempts Install, Upgrade, or Rollback
  for `signing`
- THEN the system MUST reject or omit the action, exposing only Enable,
  Disable, and Configure

#### Scenario: Reported version is informational only
- GIVEN the `signing` feature has no runtime manager
- WHEN its feature summary is read
- THEN any reported version value MUST be informational and MUST NOT
  imply install/upgrade/rollback tracking
