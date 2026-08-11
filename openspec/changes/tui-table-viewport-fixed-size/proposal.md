# Proposal: TUI Table Viewport Fixed Size

## Intent

Opening a table-bearing admin screen makes the TUI grow taller than the terminal. Content spills out of the viewport and pushes earlier frames into scrollback instead of the app staying terminal-sized with the table scrolling internally.

The app has no notion of terminal size at all: `Model.Update` has no `tea.WindowSizeMsg` case, and every table is built with `WithNoPagination()` and no height budget. The worst screen (Trivy repository alerts) stacks a scan-run table, a findings table, and a secret-findings table unbounded in one frame. Operators lose their place, cannot see the header while scrolled, and cannot trust that what they see is the whole list.

Secondarily, tables look unfinished: the theme palette exists but table borders, row styling, and footers are unwired.

## Scope

### In Scope
- Capture terminal width/height and keep the rendered frame within it.
- Bound every `bubbletable.Model` to a computed height budget so rows page/scroll inside the table instead of extending the frame.
- Activate the already-wired but inert `PageUp`/`PageDown`/`PageFirst`/`PageLast` keys and show position (e.g. footer `3/47`) so truncation is visible, never silent.
- Apply existing theme styling to tables (border color, row emphasis, footer) for a finished look.
- Define behavior for very small terminals and for the `--snapshot` path, which never receives a real resize message.
- Contain the catalog and tags plain-list screens (`renderConsoleListSection`) within the viewport as well; styling changes stay table-only.

### Out of Scope
- Backend, DTO, persistence, scan-policy, or navigation changes.
- New admin screens or workflows.
- Horizontal/reactive width redesign, including `theme.section`'s hardcoded `Width(88)` (related, separately decided — see Q5).

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `operator-admin-tui`: admin screens MUST render within the terminal viewport; oversized tabular content is paged/scrolled inside the table with visible position, not spilled into scrollback.

## Approach

Introduce a single source of truth for terminal size in `Model`, updated on `tea.WindowSizeMsg`. Subtract known chrome (title, context, status, help, section border/padding) to derive a per-screen row budget, and pass it into `newAdminBubbleTable` — the one construction point for all five tables — replacing `WithNoPagination()`. Reuse `bubble-table` v0.19.2 primitives already available; no dependency change.

## Affected Areas

| Area | Impact | Description |
|---|---|---|
| `internal/tui/model.go` | Modified | Size fields, `WindowSizeMsg` handling, chrome/height accounting |
| `internal/tui/admin_tables.go` | Modified | Height-bounded table construction, styling |
| `internal/tui/session.go` | Modified | Carry the computed row budget |
| `internal/tui/admin_views.go` | Modified | Stop assuming unbounded table height |
| `cmd/regixtry/main.go` | Modified | Alt-screen decision; snapshot fallback size |
| `internal/tui/model_test.go` | Modified | New RED coverage (none exists today) |

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Off-by-one chrome math still overflows by a line | Med | Budget from a single measured composition point; test at exact boundary heights |
| Alt-screen changes the app's overall feel | Med | Surface as an explicit product decision (Q1), not an implementation detail |
| Rows become invisible with no signal | Med | Position indicator is a success criterion, not optional polish |
| Tiny terminals degrade badly | Low | Define a minimum viable size and a graceful message |
| Snapshot/smoke output changes shape | Med | Fixed fallback size keeps `tui-smoke.sh` deterministic |

## Rollback Plan

Revert `internal/tui` and the `tea.NewProgram` construction. All changes are presentation-local; no schema, API, or persisted data is touched, so a rolled-back binary behaves exactly as `v0.2.0-rc49`.

## Dependencies

- `github.com/evertras/bubble-table` v0.19.2 (already pinned; no upgrade needed).

## Success Criteria

- [ ] No admin screen renders taller than the terminal, including the stacked Trivy alerts screen.
- [ ] A table longer than the viewport scrolls/pages internally while its header stays visible.
- [ ] Hidden rows are always signalled by a visible position indicator.
- [ ] Resizing the terminal re-fits the current screen without restart.
- [ ] Table styling reads as intentional and consistent with the existing theme.

## Proposal question round — resolved

Confirmed by the user on 2026-08-11, all recommended defaults accepted:

1. **Alt-screen**: adopt `tea.WithAltScreen()`. The app takes over the full terminal screen and restores the prompt untouched on exit — the only option that structurally guarantees the reported overflow cannot recur.
2. **Table height**: exact-height fitting in intent — tables always use all available space, a taller terminal shows more rows. Mechanism corrected during `sdd-design` (verified 2026-08-11 against the pinned `bubble-table v0.19.2` source: `WithTargetHeight` does not exist in that version): `WithPageSize` is recomputed from live terminal height on every resize, which is adaptive rather than a hardcoded row count and honors the resolved intent without a dependency change.
3. **Scope**: the catalog and tags plain-list screens (`renderConsoleListSection`) are IN SCOPE for viewport containment, alongside the five bubble-table tables. Styling work (theme wiring) stays table-only.
4. **Minimum viable terminal size**: a floor around 24 rows, below which the app shows a clear "terminal too small" message instead of degrading silently.
5. **`theme.section`'s hardcoded `Width(88)`**: deferred, out of scope for this change. Separate horizontal-axis issue, not part of the reported vertical overflow.
