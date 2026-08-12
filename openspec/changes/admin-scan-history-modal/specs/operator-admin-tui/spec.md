# Delta for operator-admin-tui

## Baseline Note

Modifies requirements from four unarchived changes (`trivy-tui-runtime-alert-tabs`, `trivy-vulnerability-details-and-db-freshness`, `gitleaks-managed-feature`, `tui-table-viewport-fixed-size`) not yet merged into `openspec/specs/operator-admin-tui/spec.md`. Archive this change together with, or after, those four.

## REMOVED Requirements

### Requirement: Same-Screen Vulnerability Drill-Down

(Reason: inline stacking of run detail, findings, and secret findings is replaced by a viewport-bounded modal.)
(Migration: see MODIFIED "Drill-Down Opens History Modal" and ADDED "Modal Viewport Containment".)

## MODIFIED Requirements

### Requirement: Repository Alerts Summarized Per Repository With Ordering And Freshness

The `Repository Alerts` tab MUST show one row per repository (not per scan run), ordered by severity then fixability across each repository's latest run. Each row MUST show a last-execution date — `FinishedAt`, else `CreatedAt` with an in-progress marker — and MUST NOT be blank.
(Previously: scan-run-level rows, no per-repository aggregation or freshness date.)

#### Scenario: Repeated rescans collapse into one dated row

- GIVEN a repository has several scan runs and no `FinishedAt` on its latest
- WHEN the operator opens `Repository Alerts`
- THEN the TUI SHALL show exactly one row for that repository
- AND the date column SHALL show `CreatedAt` with an in-progress marker, never blank

### Requirement: Repository Alert Drill-Down Opens History Modal

Selecting a repository row MUST open a modal instead of inline scan-run detail, findings, and secret findings. The inline detail block MUST be removed.
(Previously: inline drill-down, no modal.)

#### Scenario: Enter opens the modal; closing restores the list

- GIVEN a repository row is focused
- WHEN the operator presses Enter
- THEN the TUI SHALL open the modal with no inline detail on the base screen
- AND closing SHALL restore focus to the row list, no residual detail

### Requirement: Secret Findings Surface

The modal's Leaks tab MUST show persisted secret findings (rule ID, location) for the navigated execution's digest, with no severity indicator, staying present when that execution lacks a secret scan.
(Previously: shown inline for the selected image, not tied to a specific execution.)

#### Scenario: Operator reviews secret findings for a navigated execution

- GIVEN the navigated execution's digest has persisted secret findings
- WHEN the operator is on the Leaks tab
- THEN the TUI SHALL list each finding's rule ID and location, without secret text

#### Scenario: No secret scan for the navigated execution stays visible

- GIVEN the navigated execution's digest has no secret scan
- WHEN the operator is on the Leaks tab
- THEN the TUI SHALL show an explicit empty state
- AND the Leaks tab MUST stay present, not hidden

## ADDED Requirements

### Requirement: Scan History Modal Viewport Containment

The modal MUST own its row-budget accounting through the same `consoleLayout`/`fitLines` mechanism the base screen uses, not be appended below an already-budgeted body. Its rendered frame MUST NOT exceed the terminal's visible rows at any height at or above the minimum viable size.

#### Scenario: Modal fits at minimum height with full content

- GIVEN the terminal is at minimum viable height and the modal is open with tabs, a findings table, history navigation, and a full findings page
- WHEN the modal is rendered
- THEN the total rendered frame MUST NOT exceed the terminal's visible rows
- AND MUST NOT push any content into terminal scrollback

### Requirement: Per-Feature Modal Tabs

The modal MUST present an ordered, extensible tab slice — Vulnerabilities and Leaks today — with next/prev cycling. A third feature's tab MUST NOT require restructuring.

#### Scenario: Operator cycles between tabs

- GIVEN the modal is open on the Vulnerabilities tab
- WHEN the operator triggers next-tab
- THEN the TUI SHALL show the Leaks tab; cycling past the last wraps to the first

### Requirement: Scan Execution History Navigation

Inside the modal, prev/next navigation MUST move through execution history one run at a time, with a position indicator (e.g. `2/17`) and its date. Findings MUST stay scoped to that run's digest; other runs' findings MUST NOT merge.

#### Scenario: Position indicator shows one run's own findings

- GIVEN 17 recorded runs exist for a repository
- WHEN the operator navigates to position 2
- THEN the TUI SHALL show `2/17`, the run's date, and only its findings
- AND Leaks findings SHALL use that run's own digest, not another's

## Out of Scope Note

No requirement changes `contentBudget`, `fitLines`, `renderSection`, or scan execution/scheduling/trigger behavior.
