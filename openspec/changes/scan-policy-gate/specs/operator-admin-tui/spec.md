# Delta for operator-admin-tui

## Baseline Note

Adds behavior alongside several unarchived changes not yet merged into
`openspec/specs/operator-admin-tui/spec.md`, most relevantly
`admin-scan-history-modal` and `tui-design-polish` (the modal-viewport and
`compositeOverlay` containment pattern every admin modal must follow) and
`gitleaks-managed-feature`/`managed-trivy-runtime` (existing
`trivyConfigModal`, which this change explicitly does not extend). No prior
requirement covers policy configuration or a policy status badge — this is
new operator-facing surface, not a change to existing login, session,
browsing, or mutation-feedback requirements. Archive together with, or
after, those changes.

## ADDED Requirements

### Requirement: Policy Configuration Has Its Own Modal

The operator MUST be able to view and change the vulnerability policy's
enabled-state and severity threshold from a dedicated policy modal. This
modal MUST NOT be added inside `trivyConfigModal`. The modal MUST composite
as a floating overlay over the unshrunk base workspace, consistent with
every other admin modal, and MUST NOT exceed the terminal's visible rows at
minimum viable size.

#### Scenario: Operator toggles policy enabled state

- GIVEN the operator opens the policy modal
- WHEN the operator toggles the policy from enabled to disabled
- THEN the TUI SHALL persist the change and reflect it back in the modal

#### Scenario: Operator changes the severity threshold

- GIVEN the operator opens the policy modal with threshold `CRITICAL`
- WHEN the operator selects `CRITICAL+HIGH`
- THEN the TUI SHALL persist the new threshold and reflect it in the modal

#### Scenario: Policy modal is a separate surface from the Trivy config modal

- GIVEN the operator opens `trivyConfigModal`
- WHEN the operator inspects its fields
- THEN policy enabled-state and threshold controls MUST NOT appear there;
  they MUST only appear in the dedicated policy modal

#### Scenario: Policy modal stays within the terminal viewport

- GIVEN the terminal is at minimum viable height and the policy modal is open
- WHEN the modal is rendered
- THEN the total rendered frame MUST NOT exceed the terminal's visible rows

### Requirement: Persistent Policy Status Badge

The TUI MUST show a persistent, colored text badge reflecting the current
policy state and threshold (for example, `Policy: ON (CRITICAL)`). The
badge MUST render at feature-page or header level, not as a column inside
an existing table. The badge MUST use text only; it MUST NOT introduce an
icon or glyph.

#### Scenario: Badge reflects an enabled policy and its threshold

- GIVEN the policy is enabled with threshold `CRITICAL`
- WHEN the admin feature page renders
- THEN the badge SHALL display an enabled state and the `CRITICAL` threshold
  as text

#### Scenario: Badge reflects a disabled policy

- GIVEN the policy is disabled
- WHEN the admin feature page renders
- THEN the badge SHALL display a disabled state as text

#### Scenario: Badge uses text, not an icon or glyph

- GIVEN the badge is rendered in any policy state
- WHEN its content is inspected
- THEN it MUST consist of text characters conveying state and threshold, and
  MUST NOT rely on an icon or glyph to convey meaning

## Out of Scope Note

No requirement above adds fields to `trivyConfigModal`, changes
`contentBudget`/`fitLines`/`renderSection`, or introduces an icon/glyph
vocabulary elsewhere in the admin TUI.
