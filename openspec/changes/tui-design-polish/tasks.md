# Tasks: TUI Design Polish (Slice 1)

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~700–950 (prod ~180–230, tests ~520–720) |
| 800-line budget risk | Medium |
| Chained PRs recommended | No |
| Suggested split | Single PR, phase-ordered commits |
| Delivery strategy | ask-on-risk |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: No
Chain strategy: pending
800-line budget risk: Medium

Rationale: five decisions across 5 files, but each is shallow (2–20 line
production diffs); the weight is test coverage for four previously-untested
render functions (`renderAdminModal`, `renderTrivyConfigModal`,
`renderToggleField`, `trivyConfigModal`) plus 4 mandatory live-render
debug-and-delete passes. Total sits near but plausibly under the 800-line
budget for this session — flag for the orchestrator to confirm before apply
rather than pre-splitting into chained PRs.

### Suggested Work Units

| Unit | Goal | PR | Focused test | Harness | Rollback boundary |
|------|------|----|--------------|---------|--------------------|
| 1 | Severity hex (Decision 3/P4) | single PR, commit 1 | `go test ./internal/tui/... -run Severity` | Debug print, findings row (2.4) | Revert `admin_theme.go` hex line |
| 2 | Theme flatten (Decision 5/P1a) | commit 2 | `go test ./internal/tui/... -run 'ThemeInput\|TrivyConfigModal\|ToggleField'` | Debug print, Trivy modal (1.5) | Revert `admin_theme.go` token block |
| 3 | Modal overlay (Decision 1/P1) | commit 3 | `go test ./internal/tui/... -run 'AdminWorkspace\|ConfirmModal\|TrivyConfigModal'` | Debug print, Confirm overlay (3.6) | Revert `admin_views.go` workspace tail |
| 4 | Status kind (Decision 2/P3) | commit 4 | `go test ./internal/tui/... -run 'ClassifyStatusText\|StatusKind\|ScreenError'` | N/A — pure classification, covered by unit tests | Revert `statusKind` additions |
| 5 | Status de-box (Decision 4/P5) | commit 5 | `go test ./internal/tui/... -run 'AdminStatus\|ContentBudget'` | Debug print, status line (5.5) | Revert `renderAdminStatus`/comment |

## Ordering Note (deviation from requested sequencing)

Decisions 2/3/4 (design.md numbering) are independent of each other and of
1/5, per design.md. The launch brief asked to put "Decision 4" first as the
smallest/lowest-risk (a one-hex-token change) — but per design.md, Decision 3
(P4, `severityMedium` hex) is the one-line hex change, and Decision 4 (P5,
status de-box) is explicitly the design's own **"highest-risk item"**
("This is the highest-risk item and the answer is: change nothing in
`contentBudget`"). Phases below order Decision 3 first (true lowest risk) and
Decision 4 last among the independents (design-flagged highest risk, and it
shares `renderAdminStatus` with Decision 2, so sequencing after keeps that
function's signature change and its de-box change from landing in the same
diff).

## Phase 1: Severity Color Fix (Decision 3, P4) — independent, lowest risk

- [x] 1.1 RED `admin_theme_test.go` (new file): `severityHigh` and
      `severityMedium` foreground hexes differ; `severityMedium ==
      "#C0A16B"`; ramp stays ordered low→critical by luminance.
- [x] 1.2 GREEN: set `severityMedium` foreground to `#C0A16B` in
      `admin_theme.go`.
- [x] 1.3 Confirm 1.1 GREEN: `go test ./internal/tui/... -run Severity`.
- [x] 1.4 Live-render verification: throwaway debug test rendering a findings
      row styled `severityMedium` next to `severityHigh` and a gold-selected
      row; `ansi.Strip` + `fmt.Println`; inspect hue separation by eye; delete
      before finishing.

## Phase 2: Theme Token Flattening (Decision 5, P1a) — foundation for Phase 3

- [x] 2.1 RED `admin_theme_test.go`: `theme.input`/`theme.inputFocus` each
      render exactly 1 row (`lipgloss.Height`) and contain no `─`/`│` runes.
- [x] 2.2 RED `admin_views_test.go`: `renderTrivyConfigModal` <= 20 rows with
      and without an error present (currently 27/29); modal height identical
      across every `trivyConfigField` focus position (table-driven, no
      reflow); characterizes `renderToggleField` compacted to 2 rows.
- [x] 2.3 GREEN: flatten `theme.input`/`theme.inputFocus` in
      `admin_theme.go` — drop `Border`/`BorderForeground`; focus becomes
      `Foreground(selected)`, `Background(accent)`, `Bold(true)`; keep
      `Width(30)` on both. `renderTextField`/`renderSecretField`/
      `renderToggleField` are not edited.
- [x] 2.4 Confirm 2.1–2.2 GREEN: `go test ./internal/tui/... -run
      'ThemeInput|TrivyConfigModal|ToggleField'`.
- [x] 2.5 Live-render verification: throwaway debug test rendering the
      compacted `trivyConfigModal` at height 24; `ansi.Strip` +
      `fmt.Println`; confirm bottom border and `Enter: save | Esc: cancel`
      help line both visible, no reflow between focus positions; delete
      before finishing.

## Phase 3: Modal Overlay Unification (Decision 1, P1) — depends on Phase 2

- [x] 3.1 RED `admin_views_test.go`: characterization test for
      `renderAdminModal` (Confirm) — first coverage this function has ever
      had; asserts standalone rendered content/height.
- [x] 3.2 RED `model_test.go` (extends `:216`): `lipgloss.Height(View()) <=
      viewport.Height` with Confirm **and** Trivy modal open, heights 24–60
      (boundary/table-driven); modal-open height equals modal-closed height
      (no page-height growth).
- [x] 3.3 RED: Trivy modal renders full bottom border and
      `Enter: save | Esc: cancel` help line at height 24 (the floor).
- [x] 3.4 GREEN: rewrite `renderAdminWorkspace` (`admin_views.go`) — single
      `renderAdminScreen` call, switch selects at most one modal
      (ScanHistory keeps its own `adminScanHistoryModalRows` budget; Confirm
      and Trivy get none), tail is
      `compositeOverlay(base, modalView, layout.Width, layout.Height)`;
      delete the `lipgloss.JoinVertical` stacking at `admin_views.go:43-47`.
- [x] 3.5 Confirm 3.1–3.3 GREEN: `go test ./internal/tui/... -run
      'AdminWorkspace|ConfirmModal|TrivyConfigModal'`.
- [x] 3.6 Live-render verification: throwaway debug test rendering the
      unified Confirm-modal overlay over the base workspace; `ansi.Strip` +
      `fmt.Println`; confirm floating margin and no unbudgeted page-height
      growth by eye; delete before finishing.

## Phase 4: Status Kind Threading (Decision 2, P3) — independent

- [x] 4.1 RED `admin_views_test.go`: table-driven — `classifyStatusText`
      preserves every existing substring case, ported verbatim from the
      current switch.
- [x] 4.2 RED `admin_views_test.go`: table-driven over `statusKind` values —
      `statusStyle`/`renderAdminStatus` selects style from the explicit kind
      regardless of text, including `statusKindError` with the text
      `"connection refused"`.
- [x] 4.3 RED `model_test.go`: `screenError` output contains `theme.error`'s
      hex in both body and status line for a message containing none of
      "expired"/"invalid"/"error".
- [x] 4.4 GREEN: add `statusKind` type + constants (`statusKindAuto`,
      `statusKindNeutral`, `statusKindSuccess`, `statusKindWarning`,
      `statusKindError`), `classifyStatusText`, `statusStyle` helpers
      (`admin_views.go`); `renderAdminStatus(theme, status, kind)`; thread
      `+kind` through `renderConsoleWorkspace` (3 callers); `screenError`
      (`model.go`) passes `errText` as body and `statusKindError` explicitly;
      `renderInspectionWorkspace` passes `statusKindAuto` unchanged (0 of ~11
      callers touched); `contentBudget` (`viewport.go`) passes
      `statusKindNeutral` unchanged.
- [x] 4.5 Confirm 4.1–4.3 GREEN: `go test ./internal/tui/... -run
      'ClassifyStatusText|StatusKind|ScreenError'`.

## Phase 5: Status Panel De-boxing (Decision 4, P5) — design-flagged highest risk

- [x] 5.1 RED `admin_views_test.go`: `renderAdminStatus` returns exactly 1
      row and no border runes for any status/kind.
- [x] 5.2 RED `viewport_test.go`: `contentBudget` gains exactly 5 rows vs.
      the boxed-status baseline at fixed height 150x24; `SectionRows` is not
      clamped to `minTableRows` by the stale bordered-status assumption.
- [x] 5.3 GREEN: remove the `theme.section` wrap and "Status" subheading
      from `renderAdminStatus` (`admin_views.go`); make **no** change to
      `sectionChromeRows` or any `contentBudget` arithmetic — the chrome
      measurement at `viewport.go:115` self-adjusts from the de-boxed
      render; update the stale "6-row bordered section" comment at
      `viewport.go:105-107` to say 1 row.
- [x] 5.4 Confirm 5.1–5.2 GREEN: `go test ./internal/tui/... -run
      'AdminStatus|ContentBudget'`.
- [x] 5.5 Live-render verification: throwaway debug test rendering the
      de-boxed status line next to the help line at height 24; `ansi.Strip`
      + `fmt.Println`; confirm matching bare decoration and no clipped body
      content; delete before finishing.

## Phase 6: Non-Regression

- [x] 6.1 `go build ./...`, `go vet ./...`, `gofmt -l .` clean.
- [x] 6.2 Full `go test ./...` green.
- [x] 6.3 `tui-smoke.sh` (or project's smoke script, if present) passing.
- [x] 6.4 Combined live-render pass: one throwaway debug test rendering the
      final build's Trivy modal, Confirm overlay, de-boxed status line, and
      severity colors together; `ansi.Strip` + `fmt.Println`; inspect by
      eye; delete before finishing.
- [x] 6.5 Resolve design.md's remaining Open Questions from the 6.4 pass:
      `#C0A16B` vs `#D4AF37` perceptual separation; whether the 30-wide
      gold-filled focused input reads heavier than the border it replaced
      (fallback noted in design.md: gold foreground instead of fill, 1 row
      either way).
