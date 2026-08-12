# Delta for operator-admin-tui

## Baseline Note

Modifies requirements from two unarchived changes not yet merged into
`openspec/specs/operator-admin-tui/spec.md`: `tui-table-viewport-fixed-size`
(chrome-row accounting) and `admin-scan-history-modal` (`compositeOverlay`).
No prior spec requirement covers status/error styling (P3) or severity color
distinguishability (P4) — both fix real, previously-unspecified runtime
behavior in `internal/tui/admin_views.go` and `admin_theme.go`. Archive
together with, or after, those two changes.

## MODIFIED Requirements

### Requirement: Scan History Modal Viewport Containment

Every admin modal — the scan history modal, `ConfirmModal`, and
`TrivyConfigModal` — MUST composite as a floating overlay over the unshrunk
base workspace via `compositeOverlay`, not append below an already-budgeted
body via vertical stacking. Rendered frame MUST NOT exceed the terminal's
visible rows at any height at or above minimum viable size, and MUST NOT
grow total page height.
(Previously: scoped to the scan history modal only; `ConfirmModal` and
`TrivyConfigModal` stacked below the base body via `lipgloss.JoinVertical`,
growing unbudgeted page height.)

#### Scenario: Modal fits at minimum height with full content

- GIVEN the terminal is at minimum viable height and the scan history modal is open with tabs, a findings table, and history navigation
- WHEN the modal is rendered
- THEN the total rendered frame MUST NOT exceed the terminal's visible rows and MUST NOT push content into scrollback

#### Scenario: Confirm and Trivy config modals float without growing page height

- GIVEN a user enable/disable confirmation or a Trivy configuration edit is triggered
- WHEN `ConfirmModal` or `TrivyConfigModal` is rendered
- THEN it MUST composite over the unshrunk base workspace, and total page height MUST NOT exceed the base workspace's height at any terminal size

### Requirement: Admin Status And Error Styling Uses An Explicit Status Kind

`renderAdminStatus` MUST select its style (error, success, warning, muted)
from an explicit status-kind value carried alongside the status text, not a
substring match on the message. `screenError` MUST render its body and
status line with `theme.error` regardless of the error message's wording.
(Previously: `renderAdminStatus` matched `strings.Contains(lower,
"expired"|"invalid"|"error")`; error text lacking those substrings rendered
with `theme.muted`.)

#### Scenario: Fatal error renders in error styling regardless of wording

- GIVEN `screenError` is active with an error message containing none of "expired", "invalid", or "error"
- WHEN the error screen is rendered
- THEN the body and status line SHALL render with `theme.error`

### Requirement: Severity Levels Are Visually Distinguishable By Hue

`severityLow`, `severityMedium`, `severityHigh`, and `severityCritical` MUST
resolve to distinct foreground hexes so the ramp reads correctly with color
disabled or bold ignored. `severityMedium` MUST NOT share a hex with
`severityHigh`.
(Previously: `severityHigh` and `severityMedium` both resolved to `#EBCB8B`,
differing only by `Bold(true)`.)

#### Scenario: Medium and high severities render with different hexes

- GIVEN a findings table renders rows styled `severityMedium` and `severityHigh`
- WHEN the theme resolves each row's style
- THEN their foreground hex values MUST differ, and the ramp SHALL stay ordered low to critical by decreasing luminance

### Requirement: Viewport-Bounded Screen Rendering

The TUI MUST render every admin screen within the terminal's visible height;
no composition MUST push earlier frames into scrollback. The status line
MUST use the same bare, unboxed decoration as the help line beneath it — no
bordered panel, no "Status" label. `contentBudget`'s chrome-row accounting
MUST change in the same commit as the status decoration, so computed chrome
height always equals the real `lipgloss.Height` of the bare-rendered status
line, not a stale bordered-box assumption.
(Previously: `renderAdminStatus` wrapped status text in `theme.section` — a
`sectionChromeRows`-bordered box — plus a "Status" subheading, contributing
~6 measured rows to `contentBudget`'s chrome; the help line beneath it
renders bare.)

#### Scenario: Status line matches help line decoration

- GIVEN both a status message and help text are present
- WHEN the workspace is rendered
- THEN the status line MUST render with no border and no "Status" label, matching the help line's bare decoration

#### Scenario: contentBudget stays in sync after chrome removal

- GIVEN the terminal is at minimum viable size (150x24) with both status and help present
- WHEN `contentBudget` computes the section row budget
- THEN the computed chrome height MUST equal `lipgloss.Height` of the bare status line, plus the bare help line, plus fixed title/context/body-section overhead
- AND `SectionRows` MUST NOT be clamped to `minTableRows` from a stale bordered-status assumption reserving rows the bare status line no longer needs

## Out of Scope Note

No requirement above changes `theme.section` width (P2), removes dead
tokens (P6), or unifies the scan history modal's border weights (P7).
