# Proposal: Admin Scan History Modal

## Intent

After `tui-table-viewport-fixed-size` shipped, the overflow bug is gone but the Repository Alerts screen is still unreadable. It lists one row per scan run, so a repository the scheduler rescans repeatedly appears as many near-identical rows an operator cannot tell apart, and there is no column showing when a repository was last checked. Drilling into a run then stacks run detail, the Findings table, and the Secret Findings table inline on the same screen — the exact crowded composition that already required one urgent post-verify viewport fix.

Operators need to answer "which repositories look bad, and when were they last checked?" from the list, then inspect one repository's scan history in a focused surface, one execution at a time, per scanning feature.

## Scope

### In Scope
- Repository Alerts becomes a repository summary: one row per repository, including a last-execution/check date column.
- Enter on a repository opens a modal instead of stacking detail inline; the inline detail block is removed.
- Modal carries one tab per scanning feature — Vulnerabilities (Trivy) and Leaks (gitleaks) today — with tab handling that admits a third feature without restructuring.
- Inside the modal, operators navigate that repository's execution history; each position shows exactly that execution's findings, never several runs merged.
- Modal rendering is bounded by the terminal viewport budget on its own, not appended below an already-budgeted body.

### Out of Scope
- Scan execution, scheduling, or trigger behavior. This is a read/browse redesign.
- Changing the shipped viewport mechanism (`contentBudget`/`fitLines`/`renderSection` remain the primitives this builds on).
- Implementing a third scanning feature's tab.

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `operator-admin-tui`: repository alerts MUST be summarized per repository with a last-execution date, and per-repository scan history MUST be inspected in a viewport-bounded, per-feature tabbed modal showing one execution at a time.

## Approach

Reshape the base list into a per-repository summary derived from scan-run data. Add modal state on `AdminViewState` following the existing `TrivyConfigModal` state/`Active()`/dedicated-key-handler pattern, but **not** its rendering pattern: the current `lipgloss.JoinVertical(body, modal)` stacking bypasses the row budget entirely and is only safe because today's modals are tiny fixed blocks. The modal must own a viewport budget through `consoleLayout`/`fitLines`. Reuse `buildAdminFindingsTable` and `buildAdminSecretFindingsTable` as-is; replace the composition-specific `pageSize` constants with modal-specific sizing. Leaks findings resolve through the existing `(repository, digest)` lookup using the currently navigated run's digest.

## Affected Areas

| Area | Impact | Description |
|---|---|---|
| `internal/tui/admin_views.go` | Modified | Repository-summary rendering; modal render replacing inline detail |
| `internal/tui/session.go` | Modified | Modal state, active tab, history cursor |
| `internal/tui/model.go` | Modified | Modal key routing, history/tab navigation, findings loading |
| `internal/tui/admin_tables.go` | Modified | Summary table columns; modal-scoped page sizing |
| `internal/infra/metadata/sqlite/store.go` | Modified | Chronological / latest-per-repository run access |
| `internal/ports/regixtry.go` | Modified | Port shape for the above, if a new query is chosen |
| `internal/tui/model_test.go` | Modified | RED coverage for summary rows, tabs, history nav, containment |

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Modal reintroduces viewport overflow | High | Hard constraint: no additive stacking; modal budgets itself and is covered by a boundary-height test |
| Wrong timestamp makes "last check" misleading | Med | Q2 resolved before design; null-safe fallback required |
| App-layer aggregation over a large run set is slow | Med | Q1 tradeoff decided in design with an explicit limit |
| History navigation mixes runs' findings | Med | Findings keyed to the navigated run's digest; scenario-level coverage |
| Two-tab design hardcodes a binary toggle again | Med | Ordered tab set from the start (Q4) |

## Rollback Plan

Revert the `internal/tui` changes and any new metadata query. Presentation and read-path only — no schema, migration, API contract, or persisted data changes — so a rolled-back binary behaves exactly as the current `tui-table-viewport-fixed-size` build.

## Dependencies

- Builds on `tui-table-viewport-fixed-size` (already merged); no new external dependency.

## Success Criteria

- [ ] Repository Alerts shows exactly one row per repository, with a last-execution date.
- [ ] Enter opens a modal; no scan detail is stacked inline anymore.
- [ ] The modal never renders taller than the terminal, at any terminal height.
- [ ] Vulnerabilities and Leaks tabs are reachable, and adding a third tab requires no restructuring.
- [ ] Navigating history shows one execution's findings at a time in both tabs.
- [ ] A repository with no secret scan for the navigated digest still renders a coherent Leaks tab.

## Proposal question round — resolved

Confirmed by the user on 2026-08-12:

1. **Repository-summary aggregation**: app-layer grouping over `ListScanRuns`, no SQL schema/query surface change. Revisit if the run set grows large enough to matter.
2. **Last execution date**: `FinishedAt` when present, falling back to `CreatedAt` with a visible in-progress/queued marker. Never renders blank.
3. **Modal viewport containment**: **a nested budget of its own** — NOT the recommended full-body-replacement default. The modal computes and owns its own `consoleLayout`/`fitLines` budget within the space available while the base screen's body remains partially visible behind/around it. This is the most safety-critical decision in this change; `sdd-design` must work out the exact nested-budget arithmetic carefully, since it's more failure-prone than a full-replace approach — treat it with the same "measured, not guessed" rigor as `contentBudget`'s status/help accounting.
4. **Tab-cycling mechanics**: ordered extensible tab slice with an index cursor and next/prev cycling, sized for N features from day one.
5. **History navigation UX**: prev/next paging one execution at a time, with a visible position indicator (e.g. `2/17`) and the run's date.
6. **Leaks tab with no secret scan for the navigated digest**: always-present tab with an explicit empty state ("no secret scan for this execution") — never hidden, so the tab set doesn't flicker while paging history.
