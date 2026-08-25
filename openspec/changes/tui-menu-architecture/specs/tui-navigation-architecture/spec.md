# TUI Navigation Architecture Specification

## Purpose

Define the per-screen state-ownership primitive, keymap-derived help, single
confirm-before-destructive-action primitive, and domain-grouped navigation
model that replace the flat `AdminViewState`/`adminConfirmModal` god-structs
for the Security & Compliance and Operations screens (Slice 1 proves the
primitive; Slices 2–3 apply it — see design/tasks for delivery order).

## Current Repository Facts

- All admin screen state lives on three flat structs: `AdminViewState`
  (`session.go:470-575`, 30+ sibling fields), `adminTablesState`
  (`session.go:458-468`), and `adminConfirmModal`
  (`session.go:157-167`, one `Kind`-discriminated union).
- `TagsModel.PendingDelete` (`model.go:85-91`) is a second, parallel
  confirm-before-destructive-action pattern next to `adminConfirmModal`.
- `charmbracelet/bubbles` is present as an indirect dependency
  (`go.mod:20`, v0.11.0) but no screen uses `key.Map`/`help` today.
- `internal/tui` has 23 screen constants; help text is hand-written per
  branch in `adminFeatureHelp` (`admin_views.go:980-1002`).

## Requirements

### Requirement: Per-Screen Sub-Model Ownership For Migrated Screens

Each migrated screen (Trivy config, Trivy override, Gitleaks config,
Gitleaks override, Signing config, Signing override, Scan Runs, Secret Scan
Findings) MUST own a sub-model holding that screen's own state, `Update`,
and `View`, composed by a thin parent router. Migrated screens MUST NOT
read or write fields on `AdminViewState` for their own state.

#### Scenario: Migrated screen has zero fields on AdminViewState
- GIVEN a screen has been migrated to a sub-model
- WHEN `AdminViewState`'s field set is inspected
- THEN no field on it MUST exist that stores that screen's state

#### Scenario: Parent router cannot mutate a child's private state directly
- GIVEN the parent router dispatches a message to a migrated screen's
  sub-model
- WHEN the sub-model returns its updated value
- THEN the parent MUST only replace its held sub-model value, never write
  into the sub-model's fields directly

### Requirement: Non-Migrated Screens Stay On The Adapter, Unchanged

Identity & Access (Users, Robots, Repository Grants, Tokens) and Browse
(Repositories→Tags→Manifest→Blobs/Uploads) MUST continue reading and
writing state through `AdminViewState` behind an explicit adapter, and
their observable behavior MUST be unchanged by this migration.

#### Scenario: Non-migrated screen behavior is byte-identical
- GIVEN a non-migrated screen's pre-change behavior is captured by a
  characterization test
- WHEN the same test runs after this change
- THEN it MUST pass unchanged, asserting no regression, not merely absence
  of a diff

### Requirement: Keymap-Derived Help Cannot Drift

Every migrated screen MUST declare its bindings as a `bubbles/key.Map`
value; that screen's `Update` MUST match against that same map, and its
rendered help/footer MUST be generated from that same map value rather than
a separately hand-written string.

#### Scenario: Removing a binding removes it from rendered help
- GIVEN a migrated screen's `key.Map` has a binding removed
- WHEN that screen's help/footer is rendered
- THEN the removed binding's key and description MUST NOT appear in the
  rendered output

#### Scenario: Help text is generated, not duplicated
- GIVEN a migrated screen's `key.Map` and rendered help/footer
- WHEN both are inspected
- THEN the footer's bindings MUST be derivable from the map with no
  separate hand-written binding list to fall out of sync

### Requirement: One Confirm-Before-Destructive-Action Primitive

Exactly one confirm-before-destructive-action primitive MUST exist,
composed into a screen's own sub-model rather than a shared `Kind`-union.
`adminConfirmModal` and `TagsModel.PendingDelete` MUST both be retired onto
it, with identical observable behavior for every existing confirm use.

#### Scenario: Admin delete confirm behaves identically to today
- GIVEN an operator triggers a confirmed admin delete action
- WHEN the confirmation is accepted or cancelled
- THEN the outcome MUST match the pre-change behavior for that action

#### Scenario: Delete-tag confirm behaves identically to today
- GIVEN an operator triggers delete-tag on the Tags screen
- WHEN the confirmation is accepted or cancelled
- THEN the outcome MUST match `TagsModel.PendingDelete`'s pre-change
  behavior

#### Scenario: No second confirm pattern remains
- WHEN `internal/tui` is inspected for confirm-before-destructive-action
  code paths
- THEN exactly one primitive MUST be found, with `adminConfirmModal` and
  `TagsModel.PendingDelete` absent

### Requirement: Domain-Grouped Admin Navigation Menu

The admin navigation menu MUST group screens into four domains: Browse
(unchanged), Security & Compliance (Trivy, Gitleaks, Signing as three peer
config screens), Identity & Access (Users, Robots, Repository Grants,
Tokens), and Operations (Scan Runs, Secret Scan Findings).

#### Scenario: Security & Compliance lists three peer screens
- WHEN the operator opens the Security & Compliance domain
- THEN Trivy, Gitleaks, and Signing MUST each appear as a peer entry

#### Scenario: Operations lists both results screens
- WHEN the operator opens the Operations domain
- THEN Scan Runs and Secret Scan Findings MUST both appear as entries
