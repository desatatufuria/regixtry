# Proposal: TUI Design Polish (Slice 1)

## Intent

The gold-accent, restraint, and console-theming passes shipped this session
left four grounded cohesion defects in `internal/tui`. Two are real usability
bugs, not taste: `ConfirmModal`/`TrivyConfigModal` still stack below body via
`lipgloss.JoinVertical` (`admin_views.go:43-47`) while `ScanHistoryModal`
floats via `compositeOverlay` — the exact un-budgeted-height bug class the
overlay rework already fixed once; and `screenError` renders `errText` with
zero `theme.error` styling, so a fatal error can show no red anywhere.
Constraint: improve cohesion **without adding complexity** — no new screens,
keys, or behavior.

## Scope

### In Scope
- **P1** — Composite `ConfirmModal`/`TrivyConfigModal` through `compositeOverlay`, one modal paradigm.
- **P3** — Style `screenError`'s body with `theme.error`; replace `renderAdminStatus`'s substring match (`admin_views.go:609`) with an explicit status-kind signal.
- **P4** — Give `severityMedium` a distinct shade; it and `severityHigh` are both `#EBCB8B`, differing only by bold (`admin_theme.go:71-75`).
- **P5** — Drop the status panel's box + "Status" label so it matches the bare help line below it.

### Out of Scope (deferred to a follow-up change)
- **P2** — `theme.section` width unification across ~11 call sites; own slice per the exploration's sizing concern.
- **P6** — Dead `theme.pill` / `theme.accent` tokens.
- **P7** — Scan-history modal's mixed border weights.

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `operator-admin-tui`: every admin modal MUST render as a viewport-bounded floating overlay that never grows page height; a fatal error MUST render with error styling from an explicit status kind, not text matching; severity levels MUST be distinguishable by hue, not weight alone.

## Approach

P1 reuses the existing `ScanHistoryModal` branch shape in `renderAdminWorkspace`: render the base workspace unshrunk, then `compositeOverlay`. Confirm/Trivy modals are small fixed blocks, so they need a bounded height cap, not the nested-budget arithmetic the scan-history modal required. P3 adds a status-kind value alongside `m.status` and threads it to `renderAdminStatus`, defaulting `screenError` to error kind. P4 changes one theme token. P5 removes chrome, so `sectionChromeRows`/`contentBudget` accounting must move with it.

## Affected Areas

| Area | Impact | Description |
|---|---|---|
| `internal/tui/admin_views.go` | Modified | Overlay branch for both modals; status panel de-boxing |
| `internal/tui/model.go` | Modified | Status-kind field; `screenError` body styling |
| `internal/tui/admin_theme.go` | Modified | `severityMedium` shade |
| `internal/tui/viewport.go` | Modified | Chrome-row accounting after P5 |
| `internal/tui/*_test.go` | Modified | RED coverage per fix |

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| P5 desyncs `contentBudget` row math | High | Chrome-row constant and test updated in the same commit; boundary-height test |
| No golden coverage for admin ANSI output today | High | Add narrow characterization tests before touching shared tokens |
| Overlay changes clip small modals at low heights | Med | Height-bounded overlay plus across-widths/heights test |
| New shade re-collides with gold `#D4AF37` | Med | Live `--snapshot` render pass before merge |
| Status-kind threading leaks into behavior | Low | Presentation-only field; no key or flow changes |

## Rollback Plan

`git revert` the `internal/tui` commits. Presentation-only — no schema,
migration, API contract, port, or persisted-data change — same low-risk
profile as this session's earlier theme passes. A rolled-back binary behaves
exactly as the current build.

## Dependencies

- Builds on the merged gold-accent/restraint/console-theming passes and `admin-scan-history-modal`'s `compositeOverlay`. No new external dependency.

## Success Criteria

- [ ] Confirm and Trivy Config modals float over an unshrunk base page; neither grows total page height at any terminal size.
- [ ] `screenError` renders its message in `theme.error` for any error text, including messages containing none of "expired"/"invalid"/"error".
- [ ] `severityHigh` and `severityMedium` resolve to different foreground hexes; severity reads as an ordered ramp with color disabled or bold ignored.
- [ ] The status line and the help line share one decoration weight, and `contentBudget` still fits within the terminal.
- [ ] One live `--snapshot` render pass confirms all four before merge.

## Proposal question round — resolved

The exploration's three open questions, resolved here so `sdd-design` does
not re-derive them:

1. **Was stacking Confirm/Trivy below body intentional?** No — treat it as
   pre-dating the overlay rework. `admin_views.go:15-23` documents overlay as
   the superseding paradigm; these two sites were simply never migrated.
   Converging on one paradigm is the fix.
2. **One width for every panel, or a narrower form width?** Forms keep a
   narrower, content-hugging width. A two-width system (fixed-width sheets vs.
   full-width lists) is a legitimate form pattern, not an inconsistency. The
   genuine defect in P5 is the status-panel/help-line **decoration** mismatch,
   not width variance. **Note for whoever picks up P2:** target accidental
   inconsistency, do not impose one universal width everywhere.
3. **Palette constraint for `severityMedium`?** None exists. Stay in the
   Nord-adjacent amber family so the ramp stays coherent: low → muted
   `#A7B1C2`, medium → a lower-luminance amber distinct from both warning
   `#EBCB8B` and accent gold `#D4AF37` (candidate `#C0A16B`), high → warning,
   critical → error `#BF616A`. Confirm the exact hex by snapshot render in
   design or apply.

### Assumptions needing user review
- Four fixes ship as one slice (they share the `internal/tui` surface and stay well under the 800-line budget); split further only if review load demands it.
- P5 resolves by removing chrome, not by boxing the help line — restraint means cut, not add.
