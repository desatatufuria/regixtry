# Fix Feature tables and scan-history modal

The current implementation regressed the Features screen. Finish the work below before considering the change complete.

## Desired behavior

### Built-in Features table

- Preserve its existing readable, aligned layout.
- Column values must stay within their columns without clipping or overlapping adjacent cells.

### Repository Alerts table

- Render **one row per repository**, not one row per scan execution.
- Keep the summary columns shown in the current design: repository, reference, status, critical, high, fixable, runs, and last execution.
- Long repository names and timestamps must not split table structure, overlap columns, or be cut in a way that makes the data unreadable.
- Size columns from the available viewport and content deliberately. If a value cannot fit, use the existing TUI's consistent truncation/wrapping policy without breaking borders or alignment.
- Up/Down changes the selected repository row.

### Scan-history modal

- Pressing `Enter` on a Repository Alerts row must open a **true modal overlay**.
- The modal must be rendered above the Feature Page, centered/bounded in the viewport, and must not be appended below the page content.
- The underlying Features page must remain visible behind the modal and must not reflow when it opens.
- `Esc` closes the modal and restores focus to the Repository Alerts table.
- The modal title identifies the selected repository, for example: `Scan History — web-dvwa`.
- It has `Vulnerabilities` and `Leaks` tabs.
- It shows the findings for the selected execution in a correctly aligned table with readable columns: severity, finding, package, installed, fixed, and fixable.
- It shows the current execution position and timestamp, for example: `Execution 1/50 — 2026-08-12 14:05`.
- Left/Right navigates execution history for that repository; each execution displays its own vulnerabilities and leaks.
- Tab/Shift+Tab switches between Vulnerabilities and Leaks.
- The findings table must not cut text, corrupt borders, or render values over adjacent cells. Apply an intentional width/truncation/wrapping strategy consistent with the terminal viewport.

## Explicitly not desired

- Do not add scan executions as repeated rows in Repository Alerts.
- Do not render scan-history content underneath the Feature Page panel.
- Do not leave an open modal participating in normal page layout.
- Do not solve column overflow by allowing table borders and cells to misalign.

## Acceptance checklist

- [ ] A narrow terminal still preserves valid table borders and column alignment.
- [ ] Repository Alerts has exactly one summary row per repository.
- [ ] Enter opens the scan history as an overlay, not beneath the page.
- [ ] Esc closes the overlay cleanly.
- [ ] Left/Right changes the selected repository's execution in the modal.
- [ ] Vulnerabilities and Leaks are tabbed within the modal.
- [ ] Long values remain readable or are consistently truncated/wrapped without damaging table geometry.
- [ ] Add or update regression tests for summary aggregation, table sizing, modal overlay placement, and keyboard navigation.

## Visual references

These screenshots are the observed regression and intended modal appearance:

- `tmp/features.png` — current Features screen regression.
- `tmp/features-repositoriy_alerts.png` — Repository Alerts table misalignment.
- `tmp/feature-respository-alerts.vulnerabilities.png` — current scan history renders below the page instead of as an overlay.
- `tmp/vulnerabilitys-leaks.png` — intended visual shape of the scan-history modal.
- `tmp/feature-page.png` — Repository Alerts should be a summary table; selecting a row opens the modal with execution history.
