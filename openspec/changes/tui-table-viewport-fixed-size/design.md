# Design: TUI Table Viewport Fixed Size

## Technical Approach

Two layers of containment, one budget. `Model` stores terminal size from `tea.WindowSizeMsg` and derives one `consoleLayout`. The **outer layer** clips/scrolls every bordered section to its row budget — this alone satisfies "no screen exceeds the terminal" for tables and plain lists alike. The **inner layer** gives each `bubbletable.Model` a derived page size so long tables page internally with a pinned header and a native `N/M` footer instead of being cut by the outer pane. `tea.WithAltScreen()` makes the frame a fixed canvas.

## Correction to the proposal's premise (blocking, resolved)

`WithTargetHeight`, `WithBorderForeground`, and `WithRowBorder` **do not exist in `bubble-table@v0.19.2`** — zero occurrences in the whole module (verified against `/tmp/opencode/gomodcache/github.com/evertras/bubble-table@v0.19.2`). Available and used instead: `WithPageSize`, `WithFooterVisibility`, `WithBaseStyle`, `WithMinimumHeight`, `BorderRounded`, `MaxPages`/`CurrentPage`.

The proposal's *intent* (decision #2: adaptive exact-height fitting, not a hardcoded row count) is preserved: we compute `pageSize` from live terminal height on every resize. No dependency change.

## Architecture Decisions

| # | Decision | Choice | Rejected | Rationale |
|---|---|---|---|---|
| 1 | Height fitting | Derive `pageSize` ourselves from measured chrome | `WithTargetHeight` | Does not exist at the pinned version; upgrading bubble-table is a dependency change the proposal excludes |
| 2 | Chrome accounting | One `contentBudget()` measuring rendered parts with `lipgloss.Height`, consumed by both the pane and the table layout | Hardcoded line count | `renderAdminStatus` is a 6-line bordered section when present, 0 when absent — chrome is not constant. One function = no off-by-one drift (proposal's stated risk) |
| 3 | Catalog/tags containment | Stateless line-slicing pane in `renderSection`, shared with all sections | `bubbles/viewport` | `bubbles v0.11.0` is *indirect*; promoting it pins an old API and adds a stateful component with its own keymap that collides with app-owned keys. Slicing reuses PgUp/PgDn and the same indicator |
| 4 | Clipping site | Clip a section's **inner** content, then `theme.section.Render` | Slice the composed frame | Slicing the rendered box would eat its bottom border |
| 5 | Table paging input | Selection (`WithHighlightedRow`) auto-pages navigable tables; PgUp/PgDn drive the outer pane | Forward keys into `table.Update` | `table.Update` is never called today; forwarding would let tables consume app-owned keys (`/`, arrows). `WithHighlightedRow` already sets `currentPage` (options.go:49) |
| 6 | Multi-table budget split | Role-based: primary table adaptive, secondary tables compact (5 rows, floor 3) | Equal thirds | Trivy alerts stacks **4** tables plus ~20 fixed text lines inside one section — at 24 rows the fixed text alone exceeds the budget, so no split works. The outer pane is the guarantee; roles are the ergonomics |
| 7 | Too-small guard | In `View()`, before dispatch | In `Update` | `View()` is the single funnel for every screen *and* the `--snapshot` path |
| 8 | Snapshot size | Constructor default `100x40` in `NewModel` | Snapshot-only injection | Removes the special case entirely; real sizes simply overwrite it |
| 9 | Zebra striping | Not adopted | `WithRowStyleFunc` | Library docs: it "will override any HighlightStyle settings" — highlight is load-bearing for selection |

## Data Flow

    tea.WindowSizeMsg ──→ Model.viewport ──→ contentBudget() ──→ consoleLayout
                                                  │                  │
                          rebuildAdminTables ◀────┘                  ▼
                                  │                          renderSection
                          WithPageSize(n) ──→ table.View() ──→ (clip + indicator)
                                                                     │
                                                     renderConsoleWorkspace ──→ frame ≤ height

## Interfaces / Contracts

```go
// internal/tui/viewport.go (new)
const (
    minViewportWidth, minViewportHeight         = 90, 24
    defaultViewportWidth, defaultViewportHeight = 100, 40
    tableChromeRows   = 6 // borders+header+separator+footer; verified: height == pageSize+6
    sectionChromeRows = 4 // RoundedBorder(2) + Padding(1)(2)
    minTableRows, compactTableRows = 3, 5
)

type consoleLayout struct{ Width, Height, SectionRows, Scroll, Primary, Compact int }

func (m Model) contentBudget() consoleLayout
func fitLines(inner string, budget, scroll int) (fitted, indicator string)
func renderSection(theme adminTheme, inner string, l consoleLayout) string

// admin_tables.go — sole construction point, new trailing param
func newAdminBubbleTable(cols []bubbletable.Column, rows []bubbletable.Row,
    highlighted int, theme adminTheme, pageSize int) bubbletable.Model
//   .WithPageSize(pageSize).WithFooterVisibility(true)   // replaces WithNoPagination/false
//   .WithBaseStyle(lipgloss.NewStyle().Align(lipgloss.Left).BorderForeground(theme.borderColor))
```

Styling scope (Q8), limited to what v0.19.2 actually offers: border color via `WithBaseStyle().BorderForeground` (baseStyle is `Inherit`-ed by header, rows and footer) reusing the existing `#4C566A`, exposed as a new `adminTheme.borderColor` field — same palette, no new colors; plus the native paged footer (`CurrentPage/MaxPages`). No `Foreground` on the base style: it would fight `severityStyledCell`.

Too-small message (plain, not `theme.section` — that box is 88 wide): `Terminal too small` / `Regixtry needs at least 90x24. Current: WxH.` / `Resize, or press q to quit.`

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/tui/viewport.go` | Create | Layout struct, budget math, `fitLines`, `renderSection`, constants |
| `internal/tui/viewport_test.go` | Create | Budget/clipping/indicator unit RED tests |
| `internal/tui/model.go` | Modify | `viewport`/`bodyScroll` fields, `WindowSizeMsg` case, `View()` size guard, page-key handling, `renderConsoleWorkspace`/`renderConsoleListSection`/`renderConsoleTextSection` take layout |
| `internal/tui/admin_tables.go` | Modify | `newAdminBubbleTable` page-size param + styling; `rebuildAdminTables` assigns primary/compact roles |
| `internal/tui/session.go` | Modify | `AdminViewState.Layout consoleLayout` carries the budget to admin renderers |
| `internal/tui/admin_views.go` | Modify | Screens route through `renderSection`; `renderAdminWorkspace` accepts layout |
| `internal/tui/admin_theme.go` | Modify | Expose `borderColor` (existing `#4C566A`) |
| `cmd/regixtry/main.go` | Modify | Add `tea.WithAltScreen()` to `tea.NewProgram` (line 2232). `--snapshot` (2220) never runs the loop, so alt-screen cannot affect it |
| `internal/tui/model_test.go` | Modify | Screen-level RED tests |

## Testing Strategy

| Layer | What | Approach |
|---|---|---|
| Unit | `contentBudget`, `fitLines` clamping, indicator text, page-size floor | Table-driven in `viewport_test.go` |
| Unit | Table height identity `pageSize+6`; border color and paged footer present | Assert on `table.View()` |
| Integration | Every screen at 24/30/50 rows — incl. Trivy alerts with detail + secret findings — `lipgloss.Height(View()) <= h`, `Width <= w` | `model_test.go`, table-driven |
| Integration | Off-page selection stays visible; PgUp/PgDn/Home/End scroll and clamp lists; resize smaller→larger refits without restart | `Update(tea.WindowSizeMsg{})` + `tea.KeyMsg` |
| Integration | 40x10 shows "Terminal too small" and no table | Assert message + absence |
| Invariant | Every screen's title/context/help render to exactly 1 line | Guards the budget assumption |
| Smoke | `tui-smoke.sh` `grep -q "Regixtry Console"` still passes at the 100x40 default | Existing script, unchanged |

## Threat Matrix

N/A — no routing, shell, subprocess, VCS/PR automation, executable-file classification, or process-integration boundary. Presentation-only.

## Migration / Rollout

No migration. No schema, API, or persisted data touched.

## Open Questions

- [ ] None blocking. Decision #6 accepts that the Trivy alerts screen relies on outer-pane scrolling at 24 rows; splitting that screen is a navigation change and stays out of scope.
