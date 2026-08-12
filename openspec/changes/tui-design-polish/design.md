# Design: TUI Design Polish (Slice 1)

## Technical Approach

Four presentation-only fixes in `internal/tui` (plus P1a, the form compaction
P1 turned out to require), each built on existing primitives
(`compositeOverlay`, `contentBudget`'s measured chrome, `theme.selected`)
rather than new arithmetic. All heights below were derived by reading the
current render functions, not estimated.

## Architecture Decisions

### Decision 1 (P1): One overlay tail, no modal budget arithmetic

**Choice**: `renderAdminWorkspace` renders the base workspace **once**, selects
at most one modal by precedence, and composites. Confirm/Trivy get **no budget
function** — `compositeOverlay` (`admin_overlay.go:74-86`) already clamps
`overlayWidth→width`, `overlayHeight→height` and returns an exactly
`width×height` canvas, so a modal mathematically cannot grow page height.

```go
context, body, help := renderAdminScreen(theme, current, session, view, knownRepositories, layout, now)
base := renderConsoleWorkspace("Regixtry Admin", context, body, status, help, statusKindAuto)

var modalView string
switch {
case view.ScanHistoryModal.Active(): // keeps its nested budget: it is scrollable
    modalView = renderAdminScanHistoryModal(theme, view.ScanHistoryModal, view,
        adminScanHistoryModalRows(layout, lipgloss.Height(body)))
case view.ConfirmModal.Active():
    modalView = renderAdminModal(theme, view.ConfirmModal)
case view.TrivyConfigModal.Active():
    modalView = renderTrivyConfigModal(theme, view.TrivyConfigModal)
}
if modalView == "" {
    return base
}
return compositeOverlay(base, modalView, layout.Width, layout.Height)
```

`lipgloss.JoinVertical` at `admin_views.go:43-47` is deleted. This also removes
the current double `renderAdminScreen` call (once per branch).

**Alternatives considered**: a `adminSmallModalRows` budget mirroring
`adminScanHistoryModalRows`; `lipgloss.Place`.
**Rationale**: the scan-history modal needed real arithmetic because its table
page size must be pre-built to match its budget. Confirm/Trivy render fixed
content with no page sizing, so the only bound they need is the compositor's
own clamp. Adding a budget would be untested arithmetic guarding an invariant
`compositeOverlay` already enforces.

**Measured natural heights** (`theme.input` = NormalBorder + Padding(0,1) = 3
rows; `renderTextField`/`renderToggleField` = label + field = 4 rows;
`theme.section` = 4 rows):

| Modal | Inner rows | + section chrome | Total |
|---|---|---|---|
| Confirm | title+message+blank+help = 4 | 4 | **8** |
| Trivy Config | 1 + 4 + (4×4) + 2 = 23 | 4 | **27** |
| Trivy Config (with error) | 25 | 4 | **29** |

Trivy Config is **not** a small block. At `minViewportHeight = 24`
(`viewport.go:44`) it would be clipped ~3–5 rows, losing its bottom border and
the `Enter: save | Esc: cancel` line. **Resolved by Decision 5**, which
compacts it to 17 rows.

### Decision 2 (P3): Optional explicit kind at the render boundary, not on `Model`

`m.status` has **126 assignment sites in `model.go`**. A `{Text, Kind}` struct
breaks all 126; a parallel `m.statusKind` field breaks none but goes **stale**,
since 126 writers would never reset it. Both rejected.

**Choice**: keep `m.status string`. Carry the kind as a parameter only along
the path that already needs it.

```go
type statusKind int

const (
    statusKindAuto statusKind = iota // classify from text (today's behavior)
    statusKindNeutral
    statusKindSuccess
    statusKindWarning
    statusKindError
)

func renderAdminStatus(theme adminTheme, status string, kind statusKind) string {
    if kind == statusKindAuto {
        kind = classifyStatusText(status) // today's substring switch, moved verbatim
    }
    return statusStyle(theme, kind).Render(status)
}
```

Signature changes and who absorbs them:

| Function | Change | Callers touched |
|---|---|---|
| `renderAdminStatus` | `+kind` | 3 (incl. 1 test) |
| `renderConsoleWorkspace` | `+kind` | 3 |
| `renderInspectionWorkspace` | **unchanged** — passes `statusKindAuto` | 0 of ~11 |
| `contentBudget` | **unchanged** — passes `statusKindNeutral` | 0 of 7 |

`contentBudget` needs no kind because kind only sets Foreground/Bold; it never
changes `lipgloss.Height`, so its measurement stays exact.

`screenError` (`model.go:903-910`) currently passes `errText` as **both** body
and status, so an error like `connection refused` matches no substring and
renders `theme.muted`. It bypasses `renderInspectionWorkspace` and calls
`renderConsoleWorkspace` directly — which is why the 11 other call sites stay
untouched:

```go
return renderConsoleWorkspace("Regixtry Console", "Error",
    renderConsoleTextSection(theme.error.Render(errText), layout),
    errText, help, statusKindError)
```

**Tradeoff**: `classifyStatusText` survives as the *default* for the 126
untyped sites, demoted from sole mechanism to fallback. Migrating all 126 is
deliberately out of scope: mechanical, high review cost, no user-visible gain.

### Decision 3 (P4): `severityMedium = #C0A16B` (confirmed)

The palette sets **no page background** (only `selected` uses `#2E3440` as a
*foreground*), so exact contrast depends on the user's terminal.

| Token | Hex | Rel. luminance |
|---|---|---|
| severityLow / muted | `#A7B1C2` | ≈0.43 |
| **severityMedium (new)** | `#C0A16B` | ≈0.38 |
| severityHigh / warning | `#EBCB8B` | ≈0.63 |
| accent gold | `#D4AF37` | ≈0.45 |
| severityCritical / error | `#BF616A` | ≈0.13 |

Confirmed. Separation from `severityHigh` is ≈1.6:1 luminance — clearly
distinct. Separation from accent gold is only ≈1.16:1 luminance but the two
differ sharply in **saturation** (0.44 vs 0.74 — muted bronze vs bright gold)
and never share a role: gold is always Bold and appears as background/border,
`severityMedium` is unbolded body text. Contrast on a dark terminal
(vs `#2E3440`) is ≈5.1:1, above 4.5:1.

The ramp is ordered by **hue** (cool grey → bronze → amber → red), not
luminance — `severityLow` is lighter than `severityMedium`. With color
disabled, ordering comes from the literal `LOW`/`MEDIUM`/`HIGH` cell text the
table already renders, not from style.

### Decision 4 (P5): No chrome-row constant changes — `contentBudget` self-adjusts

**This is the highest-risk item and the answer is: change nothing in
`contentBudget`.**

No constant accounts for the status panel. `sectionChromeRows = 4`
(`viewport.go:58`) is the **body** section's box and is added unconditionally at
`viewport.go:120`. The status panel's rows come from a **live measurement**:

```go
chrome += lipgloss.Height(renderAdminStatus(theme, status)) // viewport.go:115
```

So removing the box and label changes the measured value automatically.

| | Status panel rows | `contentBudget` chrome (status+help present) | `SectionRows` @ height 44 |
|---|---|---|---|
| **Before** | subheading + text = 2, + section chrome 4 = **6** | 2 + 6 + 1 + 4 = **13** | 31 |
| **After** | bare styled line = **1** | 2 + 1 + 1 + 4 = **8** | 36 |

Net effect: the body gains **5 rows** whenever a status is present. That is
correct, not a leak — those rows genuinely no longer exist.

**Do NOT touch `sectionChromeRows`.** Decrementing it to "pay for" the removed
box would under-budget the body's own border by 4 rows and reintroduce exactly
the overflow class this project has fixed twice.
`adminScanHistoryModalRows` (`admin_scan_history.go:171,176,185`) also consumes
`sectionChromeRows` for the modal's box and is unaffected.

`viewport_test.go:17` already derives its expectation from
`lipgloss.Height(renderAdminStatus(...))`, so it self-adjusts too — no test
arithmetic to edit. Update the stale `viewport.go:105-107` comment
("6-row bordered section") to say 1 row.

### Decision 5 (P1a): Compact the form by flattening two theme tokens

User decision: compact the modal rather than raise `minViewportHeight` app-wide
for one modal. Same "restraint = cut, not add" rule as P5.

**What is spendable.** `theme.input` and `theme.inputFocus`
(`admin_theme.go:77-78`) are byte-identical except for `BorderForeground`:

```go
input:      ...Foreground(text).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1).Width(30),
inputFocus: ...Foreground(text).Border(lipgloss.NormalBorder()).BorderForeground(accent).Padding(0, 1).Width(30),
```

`Padding(0, 1)` is horizontal — it costs **0 rows**. The entire 2-row-per-field
overhead is `NormalBorder` (top + bottom). And the border's *only* job is to
carry a focus color, since nothing else differs between the two styles. So the
border is pure cost: 2 rows to communicate one bit already expressible in color.

Every field costs the same 4 rows, including the toggle —
`renderToggleField` (`admin_views.go:639-649`) puts the 2-character string
`on`/`off` inside the same 30-wide bordered box:

| Field kind | Label | Value | Total |
|---|---|---|---|
| Text / Secret / **Toggle** (all identical) | 1 | border 1 + content 1 + border 1 = 3 | **4** |
| Flattened (all identical) | 1 | content 1 | **2** |

**Choice**: flatten the two tokens; keep `Width(30)` on both so values stay
column-aligned and the modal's width cannot jitter as focus moves. Re-express
focus with `theme.selected`'s existing gold fill — the accent pass already
reserved gold for "interactive/selected state" (`admin_theme.go:53-56`), and
lists, grants, tokens, and suggestions all already signal the active row this
exact way.

```go
input:      lipgloss.NewStyle().Foreground(text).Padding(0, 1).Width(30),
inputFocus: lipgloss.NewStyle().Foreground(selected).Background(accent).Bold(true).Padding(0, 1).Width(30),
```

**`renderTextField`, `renderSecretField`, and `renderToggleField` are not
edited at all.** They already resolve `theme.input` vs `theme.inputFocus` and
emit `label\nvalue`, so flattening the two style definitions compacts every
form in the app. Two changed lines, no new function, no second field idiom —
which matters, because inventing compact-only renderers for one modal would
manufacture exactly the cohesion split P1 and P5 exist to remove.

**Before / after** (`theme.section` = RoundedBorder 2 + Padding(1) 2 = 4):

| Trivy Config modal | Before | After |
|---|---|---|
| Heading | 1 | 1 |
| 5 fields (1 toggle + 4 text) | 5 × 4 = 20 | 5 × 2 = **10** |
| Blank + help | 2 | 2 |
| Inner subtotal | 23 | **13** |
| + section chrome | 4 | 4 |
| **Total** | **27** | **17** |
| **Total with error (+2)** | **29** | **19** |

**10 rows saved; 7 rows of margin** under the 24-row floor (5 with an error
shown). Margin matters concretely: `compositeOverlay` has
`overlayHorizontalMargin` but **deliberately no vertical margin**
(`admin_overlay.go:20-29`), so vertical breathing room can only come from the
modal being shorter than the canvas. At 17 rows against height 24, centering
leaves ~3 base rows visible above and below, so it reads as floating rather
than as a replacement screen.

Height is **invariant across focus position** (both variants are 1 row), so
tabbing between fields causes no reflow. This is why focus is re-expressed in
color rather than by keeping the border on the focused field only: that
alternative holds total height constant but shifts every row below the focused
field by 2 as focus moves.

`theme.section`'s `Padding(1)` is 2 more spendable rows but is shared by ~11
call sites — that is P2, explicitly out of scope. Not touched.

**Blast radius**: 7 other forms shrink by 2 rows per field (Create User −8,
Login −4, Add Grant −4, Create Token −4, Change Password −2, User Search −2).
All are body content inside a `renderSection` budget, so they can only gain
headroom — a shrinking body cannot overflow. Separately noted: `inputWidth: 30`
and `secretWidth: 54` (`admin_theme.go:28-29,81-82`) are assigned but never
read anywhere; dead-token cleanup is P6, out of scope.

### Interaction with Decision 1 (P1)

24 rows is a **hard floor**, enforced in `View()` at `model.go:828-830` before
any screen dispatch — the single funnel for both the interactive program loop
and the `--snapshot` CLI path. Below 150×24 the app renders only
`terminalTooSmallTemplate`, so `renderAdminWorkspace` is unreachable and the
modal is never composited at all.

After compaction (17/19 ≤ 24), `compositeOverlay`'s clamp therefore **never
fires for this modal in production**. It is still required, and Decision 1 does
not change:

- It makes "a modal cannot grow page height" a **structural** invariant rather
  than an arithmetic coincidence that holds only while someone re-checks field
  counts. Adding a 6th Trivy field costs 2 rows; the clamp is what makes that a
  bounded truncation instead of the overflow bug class fixed twice already.
- Unit tests construct `consoleLayout` directly with arbitrary heights,
  bypassing `View()`'s guard entirely.
- The scan-history modal is budget-driven and legitimately approaches the
  canvas, so the clamp is load-bearing there regardless.

## Data Flow

    View() ──> m.contentBudget(status, help) ──> contentBudget ──> renderAdminStatus (measure only, kind=Neutral)
      │
      └──> renderAdminWorkspace ──> renderConsoleWorkspace ──> renderAdminStatus (style, kind)
                │
                └──> compositeOverlay(base, modal, layout.W, layout.H)  ← single tail, all 3 modals

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/tui/admin_views.go` | Modify | Unified overlay tail; `renderAdminStatus` de-boxed + `kind` param; `classifyStatusText`/`statusStyle` helpers |
| `internal/tui/model.go` | Modify | `statusKind` type; `renderConsoleWorkspace` `+kind`; `screenError` body + explicit error kind |
| `internal/tui/admin_theme.go` | Modify | `severityMedium` → `#C0A16B`; flatten `input`/`inputFocus` (drop `Border`, focus via gold fill) — 2 lines, compacts every form |
| `internal/tui/viewport.go` | Modify | **Comment only** — pass `statusKindNeutral`; no arithmetic change |
| `internal/tui/*_test.go` | Modify | RED coverage below |

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | `renderAdminStatus` returns 1 row and no border for any status | `lipgloss.Height` + border-rune assertion |
| Unit | `statusKindError` styles regardless of text (`"connection refused"`) | Table-driven over kinds |
| Unit | `classifyStatusText` preserves every existing substring case | Table-driven, ported from current switch |
| Unit | `severityHigh` != `severityMedium` foreground hex | Direct style comparison |
| Unit | `contentBudget` gains exactly 5 rows vs. boxed status | Before/after row assertion at fixed height |
| Unit | `theme.input` and `theme.inputFocus` each render exactly 1 row | `lipgloss.Height` on both |
| Unit | Focused vs unfocused field differ without any border rune | Assert differing ANSI, assert no `─`/`│` in either |
| Unit | `renderTrivyConfigModal` <= 20 rows with and without an error | `lipgloss.Height` assertion |
| Unit | Modal height is identical at every focus position (no reflow) | Table-driven over all 5 `trivyConfigField` values |
| Integration | `lipgloss.Height(View()) <= viewport.Height` with Confirm **and** Trivy modal open, heights 24–60 | Boundary test (extends `model_test.go:216`) |
| Integration | Trivy modal renders its full bottom border and help line at height 24 | Rendered-output assertion at the floor |
| Integration | Neither modal increases page height vs. modal-closed render | Same-height assertion |
| Integration | `screenError` output contains `theme.error`'s hex in body and status | Rendered-output assertion |

## Threat Matrix

N/A — no routing, shell, subprocess, VCS/PR automation, executable-file
classification, or process-integration boundary. Presentation-only.

## Migration / Rollout

No migration required. No schema, port, or API change.

## Open Questions

- [x] ~~Trivy Config modal is 27–29 rows against a 24-row floor.~~ **Resolved**:
      user chose compaction over raising `minViewportHeight`. Decision 5 flattens
      `input`/`inputFocus` to 17/19 rows, 7 rows under the floor.
- [x] ~~`#C0A16B` vs `#D4AF37` perceptual separation, and `severityMedium`
      legibility inside a gold-background selected row.~~ **Resolved** by
      Phase 6's live TrueColor debug pass (`internal/tui/zzz_debug_final_combined_test.go`,
      deleted after inspection): `severityMedium` renders `rgb(192,161,107)`
      and accent gold renders `rgb(211,175,55)` — the blue channel alone
      differs by 52 (107 vs 55), giving a clearly distinguishable muted-bronze
      vs bright-gold read. Inside a `theme.selected` gold-background row,
      `severityMedium`'s unbolded foreground stayed legible against the fill
      (confirmed by direct visual inspection of the composed ANSI). No change
      made.
- [x] ~~Whether a 30-wide gold-filled focused input reads as heavier than the
      border it replaces.~~ **Resolved**: it does read as a solid block (the
      full 30-column width fills with the gold background, not just a 1-cell
      border outline), but this is consistent with `theme.selected`'s existing
      gold-fill convention already used for lists, grants, tokens, and
      suggestions throughout the app (admin_theme.go's own accent-pass
      rationale). Kept the fill as designed; the gold-foreground fallback was
      not needed.
