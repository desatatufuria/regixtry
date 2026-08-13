# Delta for repository-config-overrides

## Baseline Note

Builds on the `repository-config-overrides` capability introduced by the
unarchived `repository-scan-config-overrides` change
(`openspec/changes/repository-scan-config-overrides/specs/repository-config-overrides/spec.md`;
no merged baseline exists yet in `openspec/specs/`). Only "Override Storage
Is Generic Across Feature Names" changes. Full-row-replace resolution,
fail-closed server-local path handling, disabling-override trigger
suppression, cascade-delete inertness, and admin authorization/HTTP
resource requirements are unaffected and not restated here.

## MODIFIED Requirements

### Requirement: Override Storage Is Generic Across Feature Names

The system MUST support storing and retrieving a repository-scoped
configuration override for any feature name without requiring a schema or
storage-shape change to support a new feature. A feature that has never
stored an override before MUST be able to persist and retrieve one through
the same generic mechanism used by every other feature. The settings type
an override resolves to MAY differ in field shape from any other feature's
settings type; the mechanism MUST NOT assume every feature's override
payload shares the same fields.
(Previously: guaranteed generic storage/retrieval by feature name, but did
not state that a feature's settings type may differ in shape from every
other feature's settings type.)

#### Scenario: Override stored for an existing feature
- GIVEN a repository and a feature that already has override support
  wired up (for example Trivy)
- WHEN an override is stored for that repository and feature
- THEN a later read for that repository and feature MUST return it

#### Scenario: A new feature stores an override with no schema change
- GIVEN a feature name that has never had an override stored before
- WHEN an override is stored for that feature name for some repository
- THEN the system MUST persist and retrieve it through the same generic
  mechanism, without any migration or schema change

#### Scenario: A feature's override settings differ in shape from existing features
- GIVEN the `signing` feature's override settings have different fields
  than the `trivy` and `gitleaks` features' override settings
- WHEN an override is stored and resolved for the `signing` feature
- THEN the system MUST persist and resolve it correctly without requiring
  its settings type to match any other feature's shape
