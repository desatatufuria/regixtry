# Delta for operator-admin-tui

## Baseline Note

This delta's baseline is `openspec/specs/operator-admin-tui/spec.md` plus
two still-unarchived changes' deltas, which it directly MODIFIES/REMOVES:
`repository-scan-config-overrides` (`Repository-Scoped Override Modal On
The Repository Alerts Row`) and `image-signing` (`Signing Is A Third
Feature Cycle Option In The Override Modal`). Both prior decisions
(`repository-scan-config-overrides/design.md` Decision 8;
`image-signing/design.md` Decision 11) are explicitly reversed by this
change and MUST be annotated as superseded, naming this change, before or
during archival. This change's delta MUST archive together with, or after,
both of those changes.

## ADDED Requirements

### Requirement: Gitleaks Repository-Scoped Override Entry Point

The operator MUST be able to open a dedicated override modal, bound to a
highlighted row on Gitleaks' own repository list screen, without ever
selecting or entering Trivy's screen. The modal MUST behave like the
existing per-repository override modal: showing effective config (override
values, or an explicit global-settings indication), and supporting set/clear
that round-trips through the admin API.

#### Scenario: Gitleaks override opens without entering Trivy
- GIVEN the operator is on Gitleaks' repository list screen with a row
  highlighted
- WHEN the operator presses the override key
- THEN the override modal SHALL open bound to that repository for the
  Gitleaks feature, without the operator ever selecting Trivy's screen

#### Scenario: Gitleaks override set and clear round-trip
- GIVEN the Gitleaks override modal is open for a repository
- WHEN the operator sets or clears an override
- THEN the change SHALL persist through the admin API and be reflected back
  in the modal and the repository row

### Requirement: Signing Repository-Scoped Override Entry Point

The operator MUST be able to open a dedicated override modal, bound to a
highlighted row on Signing's own repository list screen, without ever
selecting or entering Trivy's screen, with the same effective-config
display and set/clear round-trip behavior as the existing override modal.

#### Scenario: Signing override opens without entering Trivy
- GIVEN the operator is on Signing's repository list screen with a row
  highlighted
- WHEN the operator presses the override key
- THEN the override modal SHALL open bound to that repository for the
  Signing feature, without the operator ever selecting Trivy's screen

#### Scenario: Signing override set and clear round-trip
- GIVEN the Signing override modal is open for a repository
- WHEN the operator sets or clears an override
- THEN the change SHALL persist through the admin API and be reflected back
  in the modal and the repository row

### Requirement: Secret Scan Findings Reachable From Operations

The operator MUST be able to reach Secret Scan Findings from a new
Operations entry point without selecting or entering Trivy's screen. The
existing Trivy Repository Alerts drill-down path to secret findings MUST
continue to work, unchanged, alongside the new entry point.

#### Scenario: Operations entry reaches secret findings directly
- GIVEN the operator opens the Operations domain
- WHEN the operator selects Secret Scan Findings
- THEN the secret findings view SHALL open without the operator having
  selected Trivy's screen

#### Scenario: Trivy drill-down still reaches secret findings
- GIVEN the operator is on Trivy's Repository Alerts row and presses `Enter`
- WHEN the drill-down opens
- THEN it MUST still reach the same secret findings content as before this
  change

## MODIFIED Requirements

### Requirement: Repository-Scoped Override Modal On The Repository Alerts Row

The operator MUST be able to open a dedicated override modal for a
highlighted row on any Security & Compliance screen's repository list
(Trivy, Gitleaks, or Signing), bound to a key not already used on that
screen (`o`). The `Feature` the modal edits MUST be fixed by which screen
opened it and MUST NOT be changeable by any key while the modal is open;
no feature-cycling mechanism exists. The modal MUST show that repository's
current effective configuration for that fixed feature: the override's
values when one is set, or an explicit indication that the repository is
using the global settings when none is set. The operator MUST be able to
set a new override or clear an existing one from the modal, and both
actions MUST round-trip through the admin API and be reflected back in the
modal and in the opening screen's repository row. The modal MUST composite
as a floating overlay over the unshrunk base workspace, consistent with
every other admin modal, and MUST NOT exceed the terminal's visible rows
at minimum viable size.
(Previously: gated to Trivy's Repository Alerts row only, with `Feature`
mutable via a cycle across `trivy`/`gitleaks`/`signing`.)

#### Scenario: Opening the modal on a highlighted row shows the effective config
- GIVEN the operator has a repository row highlighted on a Security &
  Compliance screen, and that repository has no override for that screen's
  feature
- WHEN the operator presses `o`
- THEN the modal SHALL open bound to that repository and that feature and
  SHALL show that the repository is using the global settings

#### Scenario: Opening the modal shows an existing override
- GIVEN the operator has a repository row highlighted on a Security &
  Compliance screen, and that repository has an override set for that
  screen's feature
- WHEN the operator presses `o`
- THEN the modal SHALL open bound to that repository and that feature and
  SHALL show the override's current values

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

#### Scenario: The override key is scoped to the opening screen's row only
- GIVEN the operator is on a screen with no repository row available or
  highlighted
- WHEN the operator presses `o`
- THEN the TUI MUST NOT open the override modal

#### Scenario: The modal's Feature cannot be changed once open
- GIVEN the override modal is open, opened from one Security & Compliance
  screen
- WHEN the operator presses any key
- THEN no key MUST change which feature the modal edits

#### Scenario: Override modal stays within the terminal viewport
- GIVEN the terminal is at minimum viable height and the override modal is
  open
- WHEN the modal is rendered
- THEN the total rendered frame MUST NOT exceed the terminal's visible rows

## REMOVED Requirements

### Requirement: Signing Is A Third Feature Cycle Option In The Override Modal

(Reason: the modal's `Feature` field and its cycling mechanism
[`repositoryOverrideFeatureCycle`, `nextRepositoryOverrideFeatureName`] are
deleted; `Feature` is now immutable context fixed by the opening screen, per
the reversal of `image-signing/design.md` Decision 11.)
(Migration: Signing's per-repository overrides are now edited through
Signing's own dedicated override entry point, see `Signing
Repository-Scoped Override Entry Point` above, not by cycling the modal's
Feature field.)
