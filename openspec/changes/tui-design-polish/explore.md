# Exploration: tui-design-polish

## Goal

Make the whole Bubble Tea TUI (login, admin console, plain "Regixtry
Console" repository browsing, scan-history modal) feel more premium, calm,
and internally consistent — "Apple restraint" — without adding features,
screens, keybindings, or behavioral complexity. This is a follow-up to work
already merged this session (through tag `v0.2.0-rc59`):

1. `adminTheme`'s accent color changed from blue (`#88C0D0`/`#81A1C1`) to
   gold (`#D4AF37`).
2. `subheading`/`context`/`tableHeader` demoted off the accent color so gold
   is reserved for interactive/selected state only (`selected`,
   `inputFocus`, `pill`); `severityMedium` decoupled from `accent` onto
   `warning`.
3. The previously-unstyled plain console screens (`renderManifest`,
   `renderBlobs`, `renderUploads`) now render through `adminTheme` like
   every other screen.

Verified during this exploration: (1) and (3) hold — no screen bypasses
`adminTheme` entirely. This exploration looks for what's left.

## Current State — Findings

All findings are grounded in verbatim reads of the exact lipgloss calls
(`.Width()` present/absent, `.Bold()`, hex colors, which composite path each
modal takes) in the current source. Live terminal-render verification
(the project's established "don't trust code alone, actually render it"
practice) was not performed in this pass — tooling in this exploration run
lacked Bash/file-write access. **A real `--snapshot` render pass should run
during `sdd-design` or `sdd-apply`** before any of these are considered
confirmed-by-eye, not just confirmed-by-source.

1. **Two incompatible modal paradigms.** `ScanHistoryModal` floats via
   `compositeOverlay` (`internal/tui/admin_overlay.go:63`), leaving the base
   page unshrunk. `ConfirmModal`/`TrivyConfigModal` instead use
   `lipgloss.JoinVertical(lipgloss.Left, body, ...)`
   (`internal/tui/admin_views.go:43-47`) — stacked *below* body in the
   vertical flow, growing total height with no corresponding budget
   adjustment in `contentBudget` (`internal/tui/viewport.go:108-132`). This
   is exactly the bug class the overlay rework was built to avoid
   (`admin_views.go:15-23`), but it was never applied to these two modals.

2. **Bordered-panel width is inconsistent by screen type.** `renderSection`
   (`internal/tui/viewport.go:177-184`) explicitly stretches `theme.section`
   to `sectionWidth(layout)` (full terminal width) — used by
   Repositories/Tags/Manifest/Blobs/Uploads/Loading/Empty/Error and
   Users/Features. Every form-shaped screen instead calls
   `theme.section.Render(...)` directly with no `.Width()`, so it
   auto-sizes to content (narrow): login (`internal/tui/model.go:2642-2648`),
   the status panel shown under every screen (`admin_views.go:605-617`),
   Create/Edit User, Change Password, Grants, Add Grant, Tokens, Create
   Token, the Confirm modal, the Trivy Config modal
   (`admin_views.go:421-589`), and the scan-history modal
   (`admin_views.go:372-419`). Net effect: the narrow Status panel sits
   directly under a full-width body box on every list screen, and box width
   visibly changes between screens.

3. **Error screen has a color-coverage gap.** `screenError`'s body
   (`internal/tui/model.go:903-910`) renders raw `errText` with zero theme
   styling, unlike the Trivy Config and scan-history modal errors which
   unconditionally apply `theme.error`. Its only color cue is the status
   line, gated by a brittle substring match
   (`"expired"/"invalid"/"error"`, `admin_views.go:605-617`) — many real
   error messages won't match, leaving a fatal error with no red anywhere
   on screen.

4. **`severityHigh` and `severityMedium` are visually near-identical** —
   both `Foreground(warning)` (`#EBCB8B`), differing only by bold weight
   (`internal/tui/admin_theme.go:70-76`), a side effect of the earlier
   restraint pass not checking that `severityHigh` already owned that
   color.

5. **Status-panel over-decoration relative to its neighbor.** The status
   line gets a full RoundedBorder+Padding box plus a "Status" label (4
   extra chrome rows); the help line right below it is completely bare —
   inconsistent decoration density for two similarly-terse pieces of
   chrome.

6. **Dead theme tokens.** `theme.pill` has zero production call sites;
   `theme.accent` is used only in a test fixture. Neither is wired into any
   actual badge/render path.

7. **Minor:** the scan-history modal's outer box is `RoundedBorder` but its
   executions-rail divider uses `NormalBorder()` — likely intentional,
   flagged for completeness.

No inconsistency found in empty-state copy tone or help-line formatting —
both already read as consistent across every screen checked.

## Prioritized Opportunities

Each independently implementable:

1. **P1** — Convert `ConfirmModal`/`TrivyConfigModal` to floating overlays
   via `compositeOverlay`, matching `ScanHistoryModal`.
2. **P2** — Unify `theme.section` width policy across the ~11 form/status/
   modal call sites (either thread `sectionWidth(layout)` everywhere, or
   introduce a deliberate distinct form-width constant).
3. **P3** — Style the Error screen body with `theme.error`/`theme.text`;
   replace substring-matched error detection with an explicit status-kind
   signal.
4. **P4** — Give `severityMedium` a distinct shade from `severityHigh`.
5. **P5** — Resolve the status-panel vs. help-line decoration mismatch
   (most likely: drop the status box/label).
6. **P6** (minor) — Wire `theme.pill` into an actual badge use, or delete
   it.
7. **P7** (minor) — Confirm/align the scan-history modal's mixed border
   weights.

## Open Questions

1. Is stacking Confirm/TrivyConfig modals below body intentional, or just
   pre-dating the overlay rework? Decides P1 scope.
2. Should forms deliberately stay narrower than browse screens (an
   intentional two-width system), or should every panel share one width?
   Decides P2 direction.
3. Any palette constraint for P4's new severityMedium shade?

## Recommendation

Scope `sdd-propose` as 3-4 slices rather than one large change: P1 alone,
P2 alone (broad ~11-site surface despite trivial per-site diffs), P3+P4+P5
together (small, related), P6/P7 deferred/optional.

## Risks

- No golden/structural test coverage exists for admin screens' rendered
  ANSI output — add narrow characterization tests before touching shared
  theme tokens.
- Live-render verification was not performed in this exploration pass — do
  one real `--snapshot`/render-and-inspect pass during design or apply.
- P2's site count is broad enough to warrant its own PR slice to respect
  the review budget.

## Ready for Proposal

Yes.
