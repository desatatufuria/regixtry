# Delta for Operator Admin TUI

## Baseline Note

Baseline is the unarchived `image-signing` delta at
`openspec/changes/image-signing/specs/operator-admin-tui/spec.md`, which
added the dedicated global signing modal and the override modal's
`signing` Feature option. This delta extends both to show and edit
trusted identities alongside trusted keys.

## MODIFIED Requirements

### Requirement: Dedicated Global Signing Policy Modal

The operator MUST be able to open a dedicated signing policy modal,
sibling to `scanPolicyModal` and not an extension of it, showing the
global `Enabled` flag, configured trusted keys, and configured trusted
identities. The operator MUST be able to change and save all three,
round-tripping through the admin API and reflected back in the modal.
(Previously: modal showed only `Enabled` and trusted keys, with no
identity list.)

#### Scenario: Modal shows current global signing policy
- GIVEN the global signing policy has trusted keys and trusted identities
  configured
- WHEN the operator opens the signing modal
- THEN it SHALL show the current `Enabled` state, trusted keys, and
  trusted identities

#### Scenario: Operator saves a policy change
- GIVEN the signing modal is open
- WHEN the operator submits a changed `Enabled` value, key set, or
  identity set
- THEN the TUI SHALL persist it through the admin API and reflect the new
  values back in the modal

### Requirement: Signing Is A Third Feature Cycle Option In The Override Modal

The existing `repositoryOverrideModal` MUST support `signing` as a third
value in its Feature field cycle, alongside `trivy` and `gitleaks`. No
new modal is introduced for per-repository signing configuration; the
existing modal's fields MUST adapt to signing's settings shape —
including trusted identities — when `signing` is selected.
(Previously: adapted fields covered trusted keys only, with no identity
list.)

#### Scenario: Operator cycles to the signing feature
- GIVEN the repository override modal is open with Feature focused
- WHEN the operator cycles through Feature values
- THEN `signing` SHALL appear as a third option alongside `trivy` and
  `gitleaks`

#### Scenario: Modal fields adapt to signing's settings shape including identities
- GIVEN the operator has selected `signing` in the Feature field
- WHEN the modal renders its fields
- THEN it SHALL present signing's override fields — trusted keys and
  trusted identities — not Trivy/gitleaks' scan-path fields

## Out of Scope Note

No requirement above adds a new modal for per-repository signing
configuration; that reuses the existing `repositoryOverrideModal`. Only
the global policy gets a dedicated modal, now showing both anchor types.
