# Delta for operator-admin-tui

## Baseline Note

Adds behavior alongside several unarchived changes not yet merged into
`openspec/specs/operator-admin-tui/spec.md`, most relevantly
`admin-scan-history-modal` (the highlighted-Repository-Alerts-row +
`compositeOverlay` modal pattern this change's new modal follows) and
`scan-policy-gate` (the "own dedicated modal, not an extension of an
existing one" precedent, and the persistent-badge precedent this change's
row-level rendering follows at row rather than page granularity). This is
new operator-facing surface: no prior requirement covers a per-repository
override modal or per-row disabled-state rendering. Archive together with,
or after, those changes.

## ADDED Requirements

### Requirement: Repository-Scoped Override Modal On The Repository Alerts Row

The operator MUST be able to open a dedicated override modal for a
highlighted row in the Repository Alerts view, bound to a key not already
used on that screen (`o`). The modal MUST show that repository's current
effective configuration for the relevant feature: the override's values
when one is set, or an explicit indication that the repository is using the
global settings when none is set. The operator MUST be able to set a new
override or clear an existing one from the modal, and both actions MUST
round-trip through the admin API and be reflected back in the modal and in
the Repository Alerts row. The modal MUST composite as a floating overlay
over the unshrunk base workspace, consistent with every other admin modal,
and MUST NOT exceed the terminal's visible rows at minimum viable size.

#### Scenario: Opening the modal on a highlighted row shows the effective config

- GIVEN the operator has a Repository Alerts row highlighted, and that
  repository has no override
- WHEN the operator presses `o`
- THEN the modal SHALL open bound to that repository and SHALL show that the
  repository is using the global settings

#### Scenario: Opening the modal shows an existing override

- GIVEN the operator has a Repository Alerts row highlighted, and that
  repository has an override set
- WHEN the operator presses `o`
- THEN the modal SHALL open bound to that repository and SHALL show the
  override's current values

#### Scenario: Operator sets an override from the modal

- GIVEN the modal is open for a repository with no override
- WHEN the operator submits new override values
- THEN the TUI SHALL persist the override through the admin API and reflect
  the new values back in the modal

#### Scenario: Operator clears an override from the modal

- GIVEN the modal is open for a repository with an existing override
- WHEN the operator clears the override
- THEN the TUI SHALL delete it through the admin API and the modal SHALL
  show the repository using the global settings

#### Scenario: The override key is scoped to the Repository Alerts row only

- GIVEN the operator is on a different admin screen or the Repository Alerts
  view has no row highlighted
- WHEN the operator presses `o`
- THEN the TUI MUST NOT open the override modal

#### Scenario: Override modal stays within the terminal viewport

- GIVEN the terminal is at minimum viable height and the override modal is
  open
- WHEN the modal is rendered
- THEN the total rendered frame MUST NOT exceed the terminal's visible rows

### Requirement: Repository Alerts Renders Override-Disabled Repositories Distinctly

When a repository's effective configuration has scanning disabled through an
override, the Repository Alerts view MUST render that repository's row as
explicitly "scanning disabled," distinct from both a normally-scanned
repository's row and from a repository that has never been scanned.

#### Scenario: Disabled-via-override repository is visually distinct from an unscanned repository

- GIVEN a repository has never had a scan run, and a different repository
  has scanning disabled through its override
- WHEN the Repository Alerts view renders both rows
- THEN the disabled repository's row MUST show an explicit "scanning
  disabled" state, and MUST NOT render identically to the never-scanned
  repository's row

#### Scenario: Disabled-via-override repository is visually distinct from a normally-scanned repository

- GIVEN a repository has completed scans and no disabling override, and a
  different repository has scanning disabled through its override
- WHEN the Repository Alerts view renders both rows
- THEN the disabled repository's row MUST show an explicit "scanning
  disabled" state, distinct from the normally-scanned repository's row

## Out of Scope Note

No requirement above adds fields to `trivyConfigModal` or `scanPolicyModal`,
changes `contentBudget`/`fitLines`/`renderSection`, or extends the policy
badge introduced by `scan-policy-gate`. The override modal is its own
sibling surface, per the same "own small modal, not an extension" precedent
`scanPolicyModal` established.
