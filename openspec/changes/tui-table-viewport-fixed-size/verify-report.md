```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:de470684ec18126b6cdd84490d64b47130c2a82e859c08957fb7883155c4e4cf
verdict: fail
blockers: 1
critical_findings: 1
requirements: 6/7
scenarios: 11/12
test_command: go test -count=1 ./...
test_exit_code: 0
test_output_hash: sha256:4d57dda5d35ca77850650ceffc566d3acb0b9bf53fba1a91bd1b90c3ac1437cc
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: tui-table-viewport-fixed-size
**Version**: N/A
**Mode**: Strict TDD

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 36 |
| Tasks complete | 36 |
| Tasks incomplete | 0 |

### Build & Tests Execution
**Build**: PASSED
```text
$ go build ./...          → exit 0, no output
$ go vet ./...             → exit 0, no output
$ gofmt -l .                → exit 0, no output (nothing to format)
$ go mod tidy -diff         → exit 0, no output (go.mod/go.sum clean)
```

**Tests**: PASSED — all 17 packages, fresh run (`-count=1`, no cache), full repo
```text
$ go test -count=1 ./...
ok  	regixtry/cmd/regixtry	14.396s
ok  	regixtry/internal/app/auth	0.213s
ok  	regixtry/internal/app/regixtry	13.882s
ok  	regixtry/internal/app/scanning	0.062s
?   	regixtry/internal/domain/auth	[no test files]
ok  	regixtry/internal/domain/regixtry	0.012s
ok  	regixtry/internal/infra/auth/postgres	1.517s
ok  	regixtry/internal/infra/install/linux	1.930s
ok  	regixtry/internal/infra/install/releases	0.052s
ok  	regixtry/internal/infra/metadata/sqlite	1.789s
ok  	regixtry/internal/infra/release	0.040s
ok  	regixtry/internal/infra/scanning/gitleaks	1.170s
ok  	regixtry/internal/infra/scanning/trivy	1.152s
ok  	regixtry/internal/infra/storage/fsblob	0.036s
ok  	regixtry/internal/ports	0.020s
ok  	regixtry/internal/protocol/http	6.423s
ok  	regixtry/internal/tui	0.228s
EXIT: 0
```
`internal/tui` verbose run (`-v`): 69 top-level `--- PASS`, 0 `--- FAIL`.

`docs/verification/scripts/tui-smoke.sh` (fresh run, this session): PASS — `-snapshot` launch produced `Regixtry Console` output, and the feature-manager coverage tests it re-runs (`TestModelFeatureViewRendersGenericPageAndAllowsDeclaredAction`, `TestModelFeatureViewKeepsMinimalPagesUsable`, `TestModelFeatureSelectionRefreshesPageAndHelpFromBackendActions`) all passed.

**Coverage**: `internal/tui` package 71.8% of statements (`go test -cover`). Changed-file coverage on the new/modified core logic is high: `contentBudget` 90.9–100%, `fitLines` 80%, `renderSection` 100%, `newAdminBubbleTable`/`buildAdmin*Table` 100%, `tableRoles` 100%, `rebuildAdminTables` 100%. `moveAdminFeatureSelection` (the arrow-key selection-move path that drives table paging) sits at 66.7% — this incomplete coverage lines up directly with CRITICAL-01 below.

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| Viewport-Bounded Screen Rendering | Single-table admin screen fits the viewport | `internal/tui/model_test.go:195` `TestModelResizeShorterRebuildsAdminTablesWithoutExceedingViewport` (asserts `lipgloss.Height(afterModel.View()) <= minViewportHeight` for the single-table Features screen with 40 rows) + `internal/tui/admin_tables_test.go:101` `TestRebuildAdminTablesBakesPrimaryAndCompactPageSizeIntoTables` | ✅ COMPLIANT |
| Viewport-Bounded Screen Rendering | Stacked Trivy alerts screen fits the viewport | `internal/tui/model_test.go:289` `TestModelTrivyRepositoryAlertsScreenFitsViewportHeight` — builds 1 feature + 20 scan runs + 20 findings + 20 secret findings at height 24, asserts `lipgloss.Height(view) <= 24` | ✅ COMPLIANT |
| Internal Table and List Scrolling | Long table pages internally | **none found** — see CRITICAL-01 | ❌ UNTESTED |
| Internal Table and List Scrolling | Catalog list scrolls within its section | `internal/tui/model_test.go:246` `TestModelCatalogAndTagsListScreensFitViewportHeight` (24/30/50 rows) + `internal/tui/model_test.go:338` `TestModelPageKeysScrollAndClampCatalogList` (pgdown/home/end scroll and clamp, item visibility asserted) | ✅ COMPLIANT |
| Visible Position Indicator for Hidden Rows | Table shows position when rows are hidden | `internal/tui/admin_tables_test.go:167` `TestNewAdminBubbleTableShowsPositionIndicatorWhenRowsExceedPageSize` — 47 rows, pageSize=3, asserts footer contains `1/16` | ✅ COMPLIANT |
| Visible Position Indicator for Hidden Rows | No indicator when all rows are visible | `internal/tui/admin_tables_test.go:193` `TestNewAdminBubbleTableShowsNoMisleadingIndicatorWhenAllRowsFit` — asserts `1/1` present and no `2/` page marker | ✅ COMPLIANT |
| Live Terminal Resize Refit | Growing the terminal shows more rows | `internal/tui/model_test.go:163` `TestModelResizeTallerRebuildsAdminTablesToShowMoreRowsWithoutRestart` — asserts `Layout.Primary` strictly increases and rendered table height equals `pageSize+tableChromeRows` after resize | ✅ COMPLIANT |
| Live Terminal Resize Refit | Shrinking the terminal re-bounds the screen | `internal/tui/model_test.go:195` `TestModelResizeShorterRebuildsAdminTablesWithoutExceedingViewport` — asserts `Layout.Primary` strictly decreases and `lipgloss.Height(View()) <= minViewportHeight` | ✅ COMPLIANT |
| Minimum Viable Terminal Size | Terminal below minimum shows a clear message | `internal/tui/model_test.go:120` `TestModelViewBelowMinimumSizeShowsTerminalTooSmall` — 60x20, asserts exact message text and absence of screen content | ✅ COMPLIANT |
| Minimum Viable Terminal Size | Resizing back above minimum restores the screen | `internal/tui/model_test.go:138` `TestModelViewResizeAboveMinimumRestoresRendering` | ✅ COMPLIANT |
| Deterministic Snapshot Sizing | Snapshot output is stable across runs | `internal/tui/model_test.go:110` `TestModelNewModelDefaultsViewportTo100x40` + structural proof: `cmd/regixtry/main.go:2220-2230` returns before `tea.NewProgram` is ever constructed on the `--snapshot` path, so no `tea.WindowSizeMsg` can ever reach it and the `100x40` default is the only value it can ever see; confirmed live via `tui-smoke.sh` this session | ✅ COMPLIANT |
| Consistent Table Theme Styling | Table renders with themed border and footer | `internal/tui/admin_tables_test.go:50` `TestNewAdminBubbleTableUsesThemeBorderColorAndVisibleFooter` (footer) + `internal/tui/admin_tables_test.go:229` `TestNewAdminBubbleTableAppliesThemeBorderForegroundColor` (border ANSI sequence, verified against `termenv`'s actual hex→RGB rounding, not a hand-computed value) | ✅ COMPLIANT |

**Compliance summary**: 11/12 scenarios compliant (7 requirements retrieved from spec.md; 12 scenarios retrieved from spec.md — not 14 as stated in the task brief; recount via `grep -c '^#### Scenario:'` confirms 12).

### CRITICAL-01 — "Long table pages internally" scenario has no covering test

**Spec text** (`openspec/changes/tui-table-viewport-fixed-size/specs/operator-admin-tui/spec.md:26-31`):
> GIVEN a table has more rows than its computed height budget allows
> WHEN the operator pages through the table
> THEN only the table's row area scrolls or pages
> AND the table header and surrounding screen chrome remain visible and unchanged

By design (`design.md` decision #5), an admin table's "paging" is driven entirely by moving the row selection: `syncAdminTableHighlights()` (`internal/tui/admin_tables.go:284`) calls `.WithHighlightedRow(...)` on the live table, and the vendored library recomputes `currentPage` from the new row index (`bubble-table@v0.19.2/table/options.go:38-52`, verified by direct source read this session — line 49: `m.currentPage = m.expectedPageForRowIndex(m.rowCursorIndex)`). `table.Update` is never called and PgUp/PgDn are intentionally reserved for the outer pane (confirmed: `scrollableBodyContext()` at `internal/tui/model.go:1671-1679` only returns `ok=true` for `screenRepositories`/`screenTags`, never for admin table screens).

No test in this change exercises this: I grepped every `"down"`/`"up"` key press and `GetHighlightedRowIndex`/`CurrentPage` assertion in `internal/tui/model_test.go`. The only admin-table arrow-key test, `TestModelAdminFeatureTablesKeepScreenShortcutsAuthoritative` (`internal/tui/model_test.go:1358`), uses a 2-row `ScanRuns` table — too few rows to ever cross a page boundary, so it cannot exercise "operator pages through the table" or prove the header stays visible while the page changes. `design.md`'s own Testing Strategy table (line 87) promises an Integration-layer test for "Off-page selection stays visible"; no test with that name or effect exists (`grep -i "offpage\|off-page\|off_page" internal/tui/*_test.go` → 0 matches). This gap was not disclosed in the apply-progress deviations list.

This is a coverage gap, not a proven functional bug — I independently confirmed the underlying library call is correct by reading the vendored source directly, and the app-level wiring (`.WithHighlightedRow` called after every selection move) is a single already-covered call path for the "highlighted row index changes" half. But per this skill's rule ("A spec scenario is compliant only when a covering test passed at runtime"), an unverified-by-test scenario is CRITICAL/UNTESTED regardless of how confident static/source inspection makes it look. The fix is low-risk and small: one integration test building an admin table screen with rows > pageSize, pressing "down" until a page boundary is crossed, and asserting (a) the highlighted row's page changed and (b) the screen's title/context/help lines are unchanged before and after.

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| Viewport-Bounded Screen Rendering | ✅ Implemented | `contentBudget()`/`renderSection()` (`internal/tui/viewport.go:56,125`) wired into every admin/list/text screen render path |
| Internal Table and List Scrolling | ⚠️ Implemented, partially proven | Mechanism verified correct by source inspection (see CRITICAL-01); list-side proven by test, table-side not |
| Visible Position Indicator for Hidden Rows | ✅ Implemented | Native `WithPageSize().WithFooterVisibility(true)` (`internal/tui/admin_tables.go:72-73`) plus `fitLines`' own indicator for the outer pane |
| Live Terminal Resize Refit | ✅ Implemented | `internal/tui/model.go:363-373` — `WindowSizeMsg` case now calls `m.rebuildAdminTables(...)` in addition to setting `m.viewport`, closing the gap that plain-list sections didn't have (they re-fit for free via `View()`) |
| Minimum Viable Terminal Size | ✅ Implemented | Size guard at top of `View()`, `minViewportWidth/Height = 90/24` |
| Deterministic Snapshot Sizing | ✅ Implemented | `NewModel()` defaults to `100x40`; `--snapshot` path structurally never runs `tea.NewProgram`/receives `WindowSizeMsg` |
| Consistent Table Theme Styling | ✅ Implemented | `WithBaseStyle(...).BorderForeground(theme.borderColor)` (`internal/tui/admin_tables.go:67`), `adminTheme.borderColor` exposes the existing `#4C566A` |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| #1 Derive pageSize ourselves (no `WithTargetHeight`) | ✅ Yes | `WithPageSize(pageSize)` used throughout; confirmed `WithTargetHeight` absent from v0.19.2 by design.md's own verification, not re-checked here (out of scope to re-verify a negative) |
| #2 One `contentBudget()` measuring rendered chrome | ✅ Yes, with a justified signature deviation | `contentBudget(width, height, status, help)` takes explicit status/help instead of design.md's originally sketched zero-arg method; justified in `model.go:183-185`'s doc comment and proven necessary by `TestModelContentBudgetWrapsPackageLevelContentBudgetUsingViewportAndGivenStatusHelp` (some screens render `m.notice` or a computed status, not `m.status` — a zero-arg version would under-count chrome for those screens). Reasonable, not corner-cutting. |
| #3/#4 Stateless line-slicing pane, clip inner then wrap | ✅ Yes | `fitLines` + `renderSection` (`viewport.go:88,125`) |
| #5 Selection auto-pages tables; PgUp/PgDn drive outer pane | ✅ Implemented correctly (verified via library source), ⚠️ not proven by an app-level test | See CRITICAL-01 |
| #6 Role-based table budget split (primary adaptive, compact 5/floor 3) | ✅ Yes | `tableRoles()` (`admin_tables.go:241-252`) matches exactly; `TestTableRolesAssignsPrimaryAdaptiveAndCompactFloorAtThree` covers 3 boundary cases |
| #7 Too-small guard in `View()`, not `Update` | ✅ Yes | Guard confirmed at top of `View()`; `--snapshot` path also funnels through `View()` |
| #8 Snapshot size via constructor default `100x40` | ✅ Yes | `NewModel()` default, no snapshot-only special case |
| #9 No zebra striping (`WithRowStyleFunc` not adopted) | ✅ Yes | Not present in `admin_tables.go`; highlight styling preserved |
| Test files placed per design.md's Testing Strategy table rather than tasks.md's literal file suggestions | ✅ Reasonable | Confirmed: design.md's own table explicitly specifies Unit-layer assertions "on `table.View()`" for the indicator/border tests — placing them in `admin_tables_test.go` instead of `model_test.go` avoids confounding the table's native footer with the orthogonal outer-pane clip. Documented in the test files' own doc comments, not silently done. |

### Non-Regression
The 5 pre-existing table build sites (`buildAdminFeaturesTable`, `buildAdminFeatureRowsTable`, `buildAdminScanRunsTable`, `buildAdminFindingsTable`, `buildAdminSecretFindingsTable`) all still route through the sole `newAdminBubbleTable` construction point (`internal/tui/admin_tables.go:82-`). Pre-existing behavioral tests for these tables pass unmodified in behavior against the new 5-arg signature: `TestModelFeatureTablesRenderAlignedRowsAndPreserveBackendValues`, `TestModelTrivyTablesPreserveEmptyStateAndBackendOrdering`, `TestAdminFindingSeverityStylingScopesOnlyVulnerabilityRows`, `TestModelTrivyFindingsMixedSeverityRenderPreservesLabelsAndCounts`, `TestModelSecretFindingsSurfaceAlongsideVulnerabilityResultsWithoutSeverityOrGatingIndicator` — all PASS in the fresh full-suite run.

### TDD Compliance
| Check | Result | Details |
|-------|--------|---------|
| TDD Evidence reported | ✅ | Full RED/GREEN/TRIANGULATE/SAFETY NET/REFACTOR table present in Engram `sdd/tui-table-viewport-fixed-size/apply-progress` (obs #976) for Phase 5; Phases 1-4 summarized as "COMPLETE (copied forward, unchanged)" and cross-checked against git log |
| All tasks have tests | ✅ | 36/36 tasks; git log shows a strict RED-commit-then-GREEN-commit pattern for every phase (`d1362e1`→`0228e33`, `3825892`→`cfa8fff`, `ccfdb2c`→`3ae9548`, `51fb758`→`8d76d02`, `f0042e2`→`e6b96c5`) |
| RED confirmed (tests exist) | ✅ | 55 test functions found across `viewport_test.go`/`model_test.go`/`admin_tables_test.go` combined (`grep -c "^func Test"`) |
| GREEN confirmed (tests pass) | ✅ | All pass in this session's fresh `-count=1` run |
| Triangulation adequate | ✅ | Boundary/table-driven cases present for `contentBudget`, `fitLines`, `tableRoles`, screen heights at 24/30/50 |
| Safety Net for modified files | ✅ | Full-suite green before/after each phase per apply-progress; confirmed independently this session |

**TDD Compliance**: 6/6 checks passed structurally; 1 scenario (CRITICAL-01) reveals a gap the TDD evidence table did not surface, because no task in tasks.md explicitly named this scenario as its own RED test (Phase 4/5 tasks cover indicator/theme/resize but not selection-driven table paging).

### Assertion Quality
No tautologies, ghost loops, or assertion-free tests found across `viewport_test.go`, `admin_tables_test.go`, `model_test.go`. All table-driven tests use non-empty case tables (3-4 cases each). Height/content assertions compare against independently-computed expected values, not hardcoded copies of production output.

**Assertion quality**: ✅ All assertions verify real behavior

### Issues Found
**CRITICAL**: CRITICAL-01 — "Long table pages internally" spec scenario (Requirement: Internal Table and List Scrolling) has no covering test at any layer; see full analysis above. File: `internal/tui/model_test.go` (missing), mechanism at `internal/tui/admin_tables.go:284`, `internal/tui/model.go:1671-1679`.

**WARNING**: None beyond CRITICAL-01's own scope.

**SUGGESTION**:
1. `internal/tui/model.go:2317-2321` `renderConsoleTextSection` has no direct covering test per codegraph's blast-radius analysis, though it is exercised indirectly through several passing `Model.View()` tests (loading/empty/error/manifest/blobs/uploads screens). Not spec-required (spec's list/table scrolling requirement only names catalog/tags/tables), so not a compliance gap — flagged only for future test-naming clarity.
2. `internal/tui/admin_tables.go`'s own apply-progress notes a latent, out-of-scope interaction where a table's native footer could in principle be clipped by the outer `renderSection` when combined content exceeds `SectionRows` on screens where a table shares a section with trailing content. Correctly scoped out of this change (no spec scenario requires it); worth a follow-up ticket if a future screen combines a table with substantial trailing content at a tight terminal height.

### Verdict
**FAIL**
1 CRITICAL finding: the "Long table pages internally" spec scenario has zero covering tests despite the underlying mechanism being independently verified correct via source inspection (both app code and the vendored `bubble-table@v0.19.2` library). All 11 other scenarios are genuinely, non-trivially proven by real, currently-passing tests, and build/vet/fmt/full-suite/smoke are all clean. This is a small, well-isolated gap (one missing integration test) rather than a broken feature — recommend `sdd-apply` add the missing test before archive, not a full re-implementation.
