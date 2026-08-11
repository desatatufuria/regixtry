# Delta for operator-admin-tui

## ADDED Requirements

### Requirement: Structured Admin Table Presentation

The TUI MUST render admin data that is currently presented as row-oriented joined text as aligned tables with stable column labels. This MUST cover built-in feature summaries, backend-provided feature sections where `Kind == "rows"`, Trivy scan-run lists, and Trivy vulnerability findings. The TUI MUST preserve the existing screen structure, commands, and backend-driven empty states while adding table focus and selection only as presentation state.

#### Scenario: Feature rows render as tables

- GIVEN an authenticated operator opens a built-in feature page with summary rows or `Kind == "rows"` sections
- WHEN the page is rendered in the admin TUI
- THEN the TUI MUST show those rows in aligned columns with a visible selected row
- AND the same backend-provided records MUST remain the source of truth for displayed values

#### Scenario: Empty table data keeps backend meaning

- GIVEN a feature page or scan list returns zero rows from the backend
- WHEN the operator opens that screen
- THEN the TUI MUST show an empty-state message instead of placeholder table data
- AND the empty state MUST preserve the meaning of the backend response without inventing local records

### Requirement: Severity-Aware Vulnerability Emphasis

The TUI MUST apply severity-aware color emphasis only to vulnerability-facing tables. Severity styling MUST reflect the backend-provided severity value for each finding and MUST NOT alter row ordering, counts, or non-vulnerability table styling. Non-vulnerability tables SHOULD continue using neutral theme styling unless they already expose a different backend-authored state.

#### Scenario: Vulnerability findings use severity styling

- GIVEN a vulnerability findings table contains findings with different backend severities
- WHEN the table is rendered
- THEN the TUI MUST visually distinguish severity levels using theme-compatible styling
- AND the displayed severity labels and counts MUST match backend-provided values exactly

#### Scenario: Non-vulnerability tables stay neutral

- GIVEN the operator is viewing feature summaries, generic row sections, or scan-run tables
- WHEN those tables are rendered
- THEN the TUI MUST NOT apply severity-specific color semantics to those rows
- AND selection and status styling MAY still use the existing neutral admin theme

### Requirement: Presentation Scope and Backend Authority

This change MUST remain a presentation-only slice. The TUI MUST keep existing backend routes, DTO shapes, persistence behavior, scan-policy decisions, and command flows authoritative. The change MUST NOT require backend redesign, policy changes, or new admin workflows beyond table navigation and row selection needed to present existing data.

#### Scenario: Existing admin actions keep current behavior

- GIVEN an operator refreshes data, changes tabs, or opens a selected admin record
- WHEN the table presentation is active
- THEN the TUI MUST use the same backend interactions and action semantics that existed before the table slice
- AND any selection state introduced for tables MUST remain local UI state only

#### Scenario: Backend and policy redesign stay out of scope

- GIVEN a backend feature payload, scan result, or policy outcome is already defined
- WHEN this slice is implemented
- THEN the TUI MUST consume that existing truth without redefining DTOs, persistence rules, or policy logic
- AND no new behavior SHALL be specified for backend redesign, scan-policy changes, or cross-app table frameworks
