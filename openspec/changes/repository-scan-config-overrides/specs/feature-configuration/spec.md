# Delta for feature-configuration

## Baseline Note

Builds on the merged `feature-configuration` capability
(`openspec/changes/separate-feature-config-from-setup/specs/feature-configuration/spec.md`).
Only the resolution behavior of "Feature-Owned Runtime Authority" changes;
the legacy setup import bridge, the feature-oriented CLI model, status
reporting, and the no-dynamic-plugin-loader requirements are unaffected by
this change and are not restated here.

## MODIFIED Requirements

### Requirement: Feature-Owned Runtime Authority

Optional feature settings MUST live in authoritative feature state. Runtime
services, admin APIs, and operator CLI flows SHALL read and write that
state, and base setup MUST NOT remain the long-term authority after
migration. When resolving a feature's settings for a specific repository,
the system MUST first consult that repository's feature override
(`repository-config-overrides`); when an override row exists it MUST apply
in full, and only when no override row exists MUST the global feature state
apply.
(Previously: resolution used the global feature-owned state directly, with
no repository-scoped override to consult.)

#### Scenario: Runtime uses feature-owned state

- GIVEN authoritative Trivy feature state exists
- WHEN runtime or admin reads feature configuration for a repository with no
  override
- THEN the system SHALL use the persisted global feature state as the
  source of truth

#### Scenario: Repository-scoped override takes precedence over global feature state

- GIVEN a repository has an override row for a feature, and the feature's
  global state has different field values
- WHEN runtime resolves that feature's settings for that repository
- THEN the system SHALL use the override row's fields in full, not the
  global feature state's fields

#### Scenario: No repository override falls back to global feature state

- GIVEN a repository has no override row for a feature
- WHEN runtime resolves that feature's settings for that repository
- THEN the system SHALL use the global feature state unchanged, identical to
  resolution before this change
