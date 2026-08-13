# Delta for operator-admin-tui

## Baseline Note

Adds behavior alongside several unarchived changes not yet merged into
`openspec/specs/operator-admin-tui/spec.md`, most relevantly
`scan-policy-gate` (the `scanPolicyModal`/`scanPolicyBadge` dedicated-modal
and text-only-badge precedent this change's signing modal and badge
follow) and `repository-scan-config-overrides` (the `repositoryOverrideModal`
Feature-cycling precedent this change's third option extends). This is new
operator-facing surface: no prior requirement covers a signing policy
modal, a signing badge, or a third Feature option on the override modal.

## ADDED Requirements

### Requirement: Dedicated Global Signing Policy Modal

The operator MUST be able to open a dedicated signing policy modal,
sibling to `scanPolicyModal` and not an extension of it, showing the
global `Enabled` flag and configured trusted keys. The operator MUST be
able to change and save these values, round-tripping through the admin
API and reflected back in the modal.

#### Scenario: Modal shows current global signing policy
- GIVEN the global signing policy has trusted keys configured
- WHEN the operator opens the signing modal
- THEN it SHALL show the current `Enabled` state and trusted keys

#### Scenario: Operator saves a policy change
- GIVEN the signing modal is open
- WHEN the operator submits a changed `Enabled` value or key set
- THEN the TUI SHALL persist it through the admin API and reflect the new
  values back in the modal

### Requirement: Signing Status Badge Is Text-Only

The system MUST render a persistent, text-only signing status badge,
following `scanPolicyBadge`'s established precedent of no icon or glyph
vocabulary.

#### Scenario: Badge reflects enabled and disabled states
- GIVEN the global signing policy is enabled
- WHEN the admin view renders the badge
- THEN it SHALL show a text-only "on" indication, and SHALL show a
  text-only "off" indication when the policy is disabled

### Requirement: Signing Is A Third Feature Cycle Option In The Override Modal

The existing `repositoryOverrideModal` MUST support `signing` as a third
value in its Feature field cycle, alongside the existing `trivy` and
`gitleaks` values. No new modal is introduced for per-repository signing
configuration; the existing modal's fields MUST adapt to signing's
settings shape when `signing` is selected.

#### Scenario: Operator cycles to the signing feature
- GIVEN the repository override modal is open with Feature focused
- WHEN the operator cycles through Feature values
- THEN `signing` SHALL appear as a third option alongside `trivy` and
  `gitleaks`

#### Scenario: Modal fields adapt to signing's settings shape
- GIVEN the operator has selected `signing` in the Feature field
- WHEN the modal renders its fields
- THEN it SHALL present signing's override fields, not Trivy/gitleaks'
  scan-path fields

## Out of Scope Note

No requirement above adds a new modal for per-repository signing
configuration; that reuses the existing `repositoryOverrideModal`. Only
the global policy gets a dedicated new modal.
