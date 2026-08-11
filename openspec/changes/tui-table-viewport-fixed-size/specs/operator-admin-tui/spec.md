# Delta for operator-admin-tui

## ADDED Requirements

### Requirement: Viewport-Bounded Screen Rendering

The TUI MUST render every admin screen, including screens that stack multiple tables or list sections, within the current terminal's visible height. No screen composition MUST exceed the terminal viewport such that earlier frames are pushed into terminal scrollback.

#### Scenario: Single-table admin screen fits the viewport

- GIVEN an admin screen shows one table with more rows than fit on screen
- WHEN the screen is rendered
- THEN the total rendered frame height MUST NOT exceed the terminal's visible rows

#### Scenario: Stacked Trivy alerts screen fits the viewport

- GIVEN the Trivy repository alerts screen renders a scan-run table, a vulnerability findings table, and a secret-findings table together
- WHEN the combined content would exceed the terminal height
- THEN the TUI MUST keep the total rendered frame within the terminal viewport
- AND MUST NOT spill any of the three tables into terminal scrollback

### Requirement: Internal Table and List Scrolling

Any table or plain-list section (including the repository catalog and tags screens) whose content exceeds its available height MUST scroll or page within its own bounded area. The section's header/chrome (title, borders, column headers) MUST remain visible while content is scrolled.

#### Scenario: Long table pages internally

- GIVEN a table has more rows than its computed height budget allows
- WHEN the operator pages through the table
- THEN only the table's row area scrolls or pages
- AND the table header and surrounding screen chrome remain visible and unchanged

#### Scenario: Catalog list scrolls within its section

- GIVEN the repository catalog list has more entries than fit in its section height
- WHEN the screen is rendered
- THEN the list MUST scroll or page within its own bordered section
- AND MUST NOT grow the section beyond its allotted height

### Requirement: Visible Position Indicator for Hidden Rows

Whenever a table or list has rows hidden due to height bounding, the TUI MUST display a visible position indicator (e.g. current range and total count) so truncation is never silent.

#### Scenario: Table shows position when rows are hidden

- GIVEN a table has 47 rows and its height budget shows fewer than 47
- WHEN the table is rendered
- THEN the TUI MUST show a position indicator reflecting the visible range and total row count

#### Scenario: No indicator when all rows are visible

- GIVEN a table's row count fits entirely within its height budget
- WHEN the table is rendered
- THEN the TUI MAY omit or show a non-misleading full-count indicator, but MUST NOT imply hidden content that does not exist

### Requirement: Live Terminal Resize Refit

The TUI MUST react to terminal resize events by recomputing the current screen's height budget and re-rendering to fit, without requiring the operator to restart the application.

#### Scenario: Growing the terminal shows more rows

- GIVEN a table screen is displayed with rows hidden due to a small terminal
- WHEN the operator resizes the terminal taller
- THEN the TUI MUST recompute the available height and show more rows without restart

#### Scenario: Shrinking the terminal re-bounds the screen

- GIVEN a table screen is fully visible at the current terminal size
- WHEN the operator resizes the terminal shorter
- THEN the TUI MUST re-fit the screen to the new height and MUST NOT exceed the new viewport

### Requirement: Minimum Viable Terminal Size

Below a minimum viable terminal height of approximately 24 rows, the TUI MUST show a clear message indicating the terminal is too small instead of degrading, truncating, or overflowing silently.

#### Scenario: Terminal below minimum shows a clear message

- GIVEN the terminal height is below the minimum viable size
- WHEN any admin screen would otherwise be rendered
- THEN the TUI MUST display a clear "terminal too small" message in place of the screen content

#### Scenario: Resizing back above minimum restores the screen

- GIVEN the "terminal too small" message is displayed
- WHEN the operator resizes the terminal to at least the minimum viable height
- THEN the TUI MUST restore normal rendering of the current screen

### Requirement: Deterministic Snapshot Sizing

When invoked via the `--snapshot` CLI path, which never receives a real terminal resize event, the TUI MUST apply a fixed, deterministic fallback terminal size so table and list height bounding behaves consistently across runs.

#### Scenario: Snapshot output is stable across runs

- GIVEN the TUI is invoked with `--snapshot`
- WHEN table and list screens are rendered
- THEN the TUI MUST use the same fixed fallback size on every invocation
- AND the resulting snapshot output MUST NOT vary due to the absence of a real resize event

### Requirement: Consistent Table Theme Styling

Every bounded table MUST apply the existing admin theme consistently: themed border color, row emphasis/highlight styling, and a visible footer showing page position.

#### Scenario: Table renders with themed border and footer

- GIVEN any admin table is displayed
- WHEN the table is rendered
- THEN its border MUST use the theme's border color
- AND its footer MUST be visible and show the current page position

## Out of Scope Note

`theme.section`'s hardcoded `Width(88)` horizontal sizing is explicitly out of scope for this change (deferred per proposal Q5). No requirement above depends on changing it, and leaving it unchanged does not violate any requirement here.
