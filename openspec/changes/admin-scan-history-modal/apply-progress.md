# Apply Progress: Admin Scan History Modal

## Phase 1-5 (prior batches) — COMPLETE, 24/24 tasks

Summary preserved from Engram `sdd/admin-scan-history-modal/apply-progress`
(full history there): Phase 1 (Pure Logic, 5/5), Phase 2 (Modal State &
Rendering unwired, 4/4), Phase 3 (Wiring — Keys & History Navigation, 8/8),
Phase 4 (Removal Sweep, 5/5), Phase 5 (Non-Regression, 2/2), plus a
Remediation Pass closing sdd-verify CRITICAL-1 (missing empty-Leaks-state
test). All marked `[x]` in `tasks.md`.

## Phase 6 (this batch) — Visual Regression Fix, 5/5

The user ran the shipped Phase 1-5 build and found two real regressions,
documented with screenshots and a written acceptance spec in
`claude-handoff.md`. The existing Phase 1-5 test suite (e.g.
`TestRenderAdminScanHistoryModalNeverAppliesFitLinesOverComposite`) checked
"stays within height budget" and "no orphaned indicator" — necessary but not
sufficient to catch "stacked instead of overlaid" or "wider than its own
border".

### Bug 1: modal was a stacked panel, not a true overlay

**Root cause**: `renderAdminWorkspace` split the outer section's row budget
between the base page and the modal (`adminScanHistoryRowSplit` /
`adminScanHistoryModalRowSplit`), rendering both via
`lipgloss.JoinVertical(body, modalView)` in the same vertical flow — the
design.md-documented "Nested budget by row split, not overlay, not
stacking" decision. Implemented as literally two stacked bordered boxes,
confirmed in `tmp/feature-respository-alerts.vulnerabilities.png`.

**Fix**: real overlay compositing.
- New `internal/tui/admin_overlay.go`: `compositeOverlay(base, overlay,
  width, height)` builds a `cellbuf.Buffer` canvas, writes `base` across the
  whole canvas via `cellbuf.SetContentRect`, then writes `overlay` into a
  centered sub-rectangle via a second `cellbuf.SetContentRect` call —
  overwriting only that rectangle's cells, never touching anything outside
  it. `cellbuf.Render` re-emits the composited grid as ANSI. Chosen over
  hand-rolled string slicing specifically because both `base` and `overlay`
  carry real lipgloss SGR escape sequences; `cellbuf` parses ANSI into a
  cell grid and re-emits it, so every splice happens at cell boundaries,
  never mid-escape-sequence. `github.com/charmbracelet/x/cellbuf` was
  already resolved as an indirect dependency (`go.mod`); this change
  promotes it to direct (`go mod tidy`).
- `renderAdminWorkspace` (`admin_views.go`) now renders the base page at its
  full, unshrunk `layout` (identical to a modal-closed render) and
  composites the modal on top via `compositeOverlay(baseWorkspace,
  modalView, layout.Width, layout.Height)`.
- `adminScanHistoryModalRows` (`admin_scan_history.go`) replaces
  `adminScanHistoryRowSplit`/`adminScanHistoryModalRowSplit`: the modal's
  row budget is now derived independently from the terminal's own height
  (bounded, floored at `adminScanHistoryModalMinRows`, leaving a visible
  margin via `adminScanHistoryModalVerticalMargin`) rather than carved out
  of the base's measured height.
- `rebuildAdminTables` (`admin_tables.go`) updated to call
  `adminScanHistoryModalRows(layout)` directly.
- Doc comments referencing "Nested budget by row split, not overlay, not
  stacking" updated in `admin_scan_history.go`, `admin_views.go`,
  `session.go`.
- Key/tab/navigation/digest-per-run logic (Phase 3) was NOT touched — only
  the rendering/compositing path changed.

### Bug 2: table columns overflowed their bordered box width

**Root cause**: `theme.section` (`admin_theme.go`) had a hardcoded
`.Width(88)` — explicitly flagged as a deferred issue in the
`tui-table-viewport-fixed-size` proposal's own Q5 ("out of scope for this
change"). `buildAdminScanSummaryTable`'s 8 columns summed to a natural
rendered width of 99 (measured), wider than the 88-wide section wrapping
it, so lipgloss word-wrapped the table's own lines mid-box, corrupting
borders — visible in `tmp/features-repositoriy_alerts.png`.

**Fix**: made section width responsive and gave tables real breathing room.
- `sectionWidth(l consoleLayout)` (`viewport.go`) derives `theme.section`'s
  width from the real terminal width (`l.Width - sectionHorizontalOverhead`,
  floored at `minSectionContentWidth`), replacing the hardcoded constant.
  Wired into `renderSection` via `theme.section.Width(sectionWidth(l))`.
  `theme.section` itself no longer carries a `Width` at construction; call
  sites without a `consoleLayout` (unwired forms like Create User) fall back
  to auto-sizing to their own content.
- Widened every admin table's column definitions
  (`adminFeaturesColumn*Width`, `adminFindingsColumn*Width`,
  `adminSecretColumn*Width`, `adminScanSummaryColumn*Width` in
  `admin_tables.go`), each measured against real longest values —
  `adminScanSummaryColumnLastExecutedWidth` (34) against
  `formatScanSummaryLastExecuted`'s longest output ("YYYY-MM-DD HH:MM (in
  progress)", 30 chars wide).
- Raised `minViewportWidth` 90→132 and `defaultViewportWidth` 100→150,
  `defaultViewportHeight` 40→44 (`viewport.go`) — the floor terminal size
  must actually fit the widened Repository Alerts table (natural width 127
  measured after widening); a floor narrower than that would still corrupt
  borders regardless of how responsive the section width is.
- `TestModelViewBelowMinimumSizeShowsTerminalTooSmall` updated for the new
  floor (was asserting the literal string `"90x24"`).

## TDD Cycle Evidence

| Area | RED | GREEN | REFACTOR |
|---|---|---|---|
| `sectionWidth`/column widths | New tests reference `sectionWidth`/`adminScanSummaryColumnLastExecutedWidth` before they existed → compile failure (confirmed via `git stash` of the three production files) | Added `sectionWidth`, widened columns, raised viewport dims → tests pass | Extracted named column-width constants instead of inline literals |
| `compositeOverlay` | New `admin_overlay_test.go` referenced `compositeOverlay` before `admin_overlay.go` existed → compile failure (confirmed by moving the file aside) | Implemented `compositeOverlay` on `cellbuf` → all 4 tests pass | n/a — single-purpose helper |
| `renderAdminWorkspace` overlay-vs-stack | Temporarily reverted `renderAdminWorkspace`'s modal branch to the old `lipgloss.JoinVertical` stacking, re-ran `TestRenderAdminWorkspaceKeepsBaseFullSizeAndLayersModalOnTopWhenOpen` → FAILED: `height = 39, want exactly layout.Height(44)` | Restored the `compositeOverlay` call → test passes | n/a |
| `adminScanHistoryModalRows` | Old row-split tests deleted (function removed); new bounded-property tests written against the new function signature | Pass | n/a |

## Work Unit Evidence

| Evidence | Value |
|---|---|
| Focused test command and result | `go test ./internal/tui/...` → `ok` (all packages); targeted runs of every new test name individually also `PASS` |
| Runtime harness | `bash docs/verification/scripts/tui-smoke.sh` → "TUI smoke verified snapshot launch and feature-manager action/help coverage." Additionally hand-verified visually via a temporary throwaway test driving the real `Update()`/`View()` key-press flow (login → f → tab → enter) and printing `View()`: confirmed the modal renders centered, overlapping the base page's own content (Built-in Features table, Repository Alerts section, "Operator: operator" line all remain visible behind/around the modal), never appended below it. |
| Rollback boundary | Two independent commits: `fix(tui): derive section width from viewport instead of hardcoded 88` (Bug 2, `admin_theme.go`/`viewport.go`/`admin_tables.go`/`admin_tables_test.go`/`model_test.go`) and `fix(tui): render scan history modal as a true floating overlay` (Bug 1, `admin_overlay.go`(new)/`admin_overlay_test.go`(new)/`admin_views.go`/`admin_views_test.go`/`admin_scan_history.go`/`admin_scan_history_test.go`/`admin_tables.go`/`session.go`/`go.mod`). Either commit can be reverted independently without breaking the other (Bug 1's overlay compositing does not depend on Bug 2's width constants, and vice versa; both were verified green in isolation before combining). |

## Final verification (this batch)

- `go build ./...` — clean, no output.
- `go vet ./...` — clean, no output.
- `gofmt -l .` — empty (no files need formatting).
- `go mod tidy` — moved `github.com/charmbracelet/x/cellbuf` from indirect
  to direct in `go.mod`; no other changes.
- `go test -count=1 ./...` — all 16 testable packages pass (`internal/domain/auth`
  has no test files, unchanged).
- `bash docs/verification/scripts/tui-smoke.sh` — passes.

**Changed lines this batch**: 13 files, 557 insertions / 155 deletions = 712
changed lines (`git diff --shortstat` against the pre-batch commit,
restricted to `internal/tui/` + `go.mod`). This exceeds the standard 400/500
review budget; the session preflight recorded delivery strategy `single-pr`
with an explicit `size:exception` for this change, which this batch relies
on — reported honestly rather than split, per the runtime attempt ledger's
instruction to "budget accordingly but report the honest number."

## `cellbuf` investigation note

Real investigation was done before using `cellbuf`, not a hand-roll
fallback: read `writer.go`/`buffer.go` from the pinned module cache
(`v0.0.15`) to confirm `cellbuf.NewBuffer`, `cellbuf.SetContentRect` (ANSI-
parsing cell writer, ANSI-safe by construction), and `cellbuf.Render`
(cell-grid → ANSI re-emitter) existed and fit this exact use case
(compositing one already-rendered lipgloss block onto another at an offset).
No fallback to manual ANSI slicing was needed — `cellbuf` fit directly.

## Phase 7 (this batch) — Overlay Margin Fix, 3/3

Phase 6's own tests all passed (exact canvas height, base marker present,
modal title present) but still looked broken when actually rendered and read
by eye — the same class of gap the Phase 6 investigation itself was created
to close. The orchestrator wrote a throwaway debug test that built a
realistic `AdminViewState` (2 repositories in `TrivySummaries`, a
`ScanHistoryModal` open with 3 findings) at `defaultViewportWidth`/
`defaultViewportHeight`, called `renderAdminWorkspace`, stripped ANSI via
`github.com/charmbracelet/x/ansi`'s `Strip`, and printed the result — with
the modal both closed and open — and found real defects no existing test
caught.

**Root cause**: `compositeOverlay` (`admin_overlay.go`) wrote the base onto
the canvas, then wrote the overlay into its own exact centered rectangle,
overwriting nothing else. Whatever base content (text or border characters)
happened to sit immediately outside that exact rectangle survived
unmodified, right up to the overlay's own border — a base table's own
border character landed flush against the modal's border/corner with zero
gap, and base text that continued past the modal's left edge was hard-cut
with nothing separating it visually from the modal. `cellbuf.SetContentRect`
itself worked exactly as documented (clears its own rect, writes content,
truncates at bounds) — this was not a `cellbuf` semantics bug, it was a
missing design requirement: nothing in the Phase 6 design called for a
margin around the overlay's own footprint.

**Modal-width investigation**: also checked whether the modal's own target
width (auto-sized to content via `theme.section.Render` with no `.Width()`
set) was the real problem, per the orchestrator's hypothesis that it might
be rendering near-full-canvas-width. Measured directly: with the debug
fixture's 3-finding Findings table as content, the modal auto-sized to ~93
columns against a 150-column canvas (defaultViewportWidth) — margins of ~28
columns on each side, genuinely centered, not stretched to fill the canvas.
The visual "cut mid-word"/"fused border" defects were confirmed via the
debug print to be a compositing-margin problem, not a modal-oversizing
problem; the modal's own auto-sizing to content was left unchanged.

**Fix**: `compositeOverlay` now blanks a small `overlayHorizontalMargin`
(2 columns) buffer immediately left/right of its own footprint — via a
second `cellbuf.ClearRect` call on a rect expanded by the margin, intersected
with the canvas bounds — before drawing the overlay content. This guarantees
a real blank gap always separates the modal's left/right border from
whatever base content survives beside it.

**Deliberately no vertical margin**: an initial version also blanked a
1-row margin above/below the overlay's footprint. This immediately broke an
existing regression test (`TestCompositeOverlayPreservesBaseContentOutsideOverlayFootprint`)
and, more importantly, corrupted a REAL row in the debug fixture — the
screen's own help line, which happened to sit exactly one row below the
modal's bottom edge in that layout, got half-blanked by the vertical margin
across the modal's own column span. Unlike width (fixed per terminal), the
row directly above/below the overlay can legitimately hold meaningful base
content right up to the overlay's edge; blanking it unconditionally risked
destroying real content instead of empty canvas. Reverted to horizontal-only
margin after directly observing this regression in the debug print, not
guessing. The modal already gets a real vertical gap in practice from
`adminScanHistoryModalRows`'s own margin below most terminal-height floors.

**Blank space below the modal (orchestrator's item 3) — investigated,
confirmed harmless, not fixed**: the debug print showed a large blank area
below the modal before the canvas ends. Root cause confirmed directly: with
the modal open, `compositeOverlay`'s `cellbuf` canvas is always exactly
`layout.Height` rows (44 in the fixture) because `cellbuf.Render` always
emits exactly `height` lines. The debug fixture's actual content (2-row
Repository Alerts table, 3-finding modal) only naturally fills ~31 of those
44 rows — the same asymmetry the modal-CLOSED render does NOT show, because
the closed path returns unpadded natural-height content instead of a fixed
canvas. This is the exact case the orchestrator flagged as possibly
"expected/harmless (the debug test's tiny fixture doesn't have enough
content)" — confirmed true by direct measurement, not assumed. A real
terminal renders this the same way a bubbletea alt-screen program already
would (unused rows below your `View()` output simply show as blank), so this
was left as-is.

**TDD Cycle Evidence**:
| Area | RED | GREEN |
|---|---|---|
| `compositeOverlay` margin | New `TestCompositeOverlayLeavesBlankMarginAroundOverlayFootprint` referenced `overlayHorizontalMargin` before it existed → compile failure | Implemented `overlayHorizontalMargin` + the `ClearRect` call → passes |
| `renderAdminWorkspace` margin (integration) | New `TestRenderAdminWorkspaceLeavesVisibleMarginAroundModalWhenOpen` confirmed RED against the real render by temporarily no-op'ing the `ClearRect` call (`_ = marginRect`): failed with `row 15 col 27 = "e", want a blank margin column left of the modal` — the exact "text cut mid-word at the modal's edge" defect, reproduced by a real assertion, not just eyeballing | Restored the `ClearRect` call → passes |
| Existing `TestCompositeOverlayPreservesBaseContentOutsideOverlayFootprint` | Broke when a vertical margin was tried (row 1/4 markers partially blanked) | Fixture repositioned so "untouched" markers sit outside the (horizontal-only) margin's column span; passes, and the vertical-margin approach itself was reverted |

**Work Unit Evidence**:
- Focused test: `go test ./internal/tui/... -run 'TestCompositeOverlay|TestRenderAdminWorkspace'` → all pass; full `go test ./internal/tui/...` → ok
- Runtime harness: N/A for a pure rendering/compositing fix with no async `Cmd`/`Model.Update()` boundary (consistent with sibling overlay tests' scope); the mandated visual-inspection technique (temporary debug test printing ANSI-stripped `renderAdminWorkspace` output, both modal-closed and modal-open, read by eye) stands in as the closest thing to a runtime harness for a rendering defect that unit assertions alone did not catch previously — deleted before finishing per instruction, replaced by the two permanent tests above
- Rollback boundary: single commit `fix(tui): keep the scan history modal from fusing into base content`, touching only `admin_overlay.go` (+`overlayHorizontalMargin` const and the margin `ClearRect` call) and two test files; reverts cleanly without touching Phase 6's `sectionWidth`/column-width/overlay-plumbing work

**Full verification (this batch)**: `go build ./...` clean; `go vet ./...`
clean; `gofmt -l .` empty; `go test -count=1 ./...` — all 16 testable
packages pass.

**Changed lines this batch**: 3 files (`internal/tui/admin_overlay.go`,
`internal/tui/admin_overlay_test.go`, `internal/tui/admin_views_test.go`),
206 insertions / 8 deletions = 214 changed lines (`git diff --shortstat`).
Well within the 350-line attempt-token budget.

Ready for re-`sdd-verify`.
