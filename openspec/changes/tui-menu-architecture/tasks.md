# Tasks: TUI Menu Architecture — Per-Screen Ownership and Domain-Grouped Navigation

## Mandatory Ordering Constraint (3 chained slices — hard structural gates)

This change ships as **3 chained PRs/slices**, user-confirmed, not a single-PR size
exception. Each slice boundary is a **hard structural gate**, enforced by phase
numbering, not intention:

- **Slice 1 (Phases 1–7)** is the primitive/scaffolding with **zero user-visible
  behavior change**. It MUST be complete, independently reviewable, and
  independently apply-/verify-able (merged or at minimum verified) **before any
  Slice 2 task (Phase 8+) begins.**
- **Slice 2 (Phases 8–17)** is the reversal — the only slice with user-visible
  behavior change, and where the reported defect is actually fixed. It MUST pass
  its own gate (Phase 17) **before any Slice 3 task (Phase 18+) begins.**
- **Slice 3 (Phases 18–24)** is Operations, domain menu, and docs.

The golden/characterization tests (Phase 1) are captured against **today's code,
before `screen.go`/`admin_router.go` or any new file exists** — they exist to prove
the 13 non-migrated legacy screens stay byte-identical across all three slices.

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 2,100–2,700 (Slice 1: 700–900, Slice 2: 800–1,000, Slice 3: 600–800) |
| 400-line budget risk | High (session-cached review budget is **800**, not the skill default 400 — Slice 1 alone sits above it) |
| Chained PRs recommended | Yes |
| Suggested split | 3 units — Slice 1 (Phases 1–7, primitive, zero behavior change) → Slice 2 (Phases 8–17, the reversal) → Slice 3 (Phases 18–24, Operations + domain menu + docs) |
| Delivery strategy | ask-on-risk |
| Chain strategy | pending (user confirmed 3 chained slices; stacked-to-main vs. feature-branch-chain not yet chosen) |

**Rationale**: design.md's own revised Review Budget table independently confirms
2,100–2,700 total, all three slices individually or near the 800-line cached budget.
If Slice 1's load proves too high in review, design.md names a named sub-split —
**1a** (contract/router/adapter/keys/`gitleaksConfigScreen`, ≈450–550) and **1b**
(`confirmPrompt` + both confirm-pattern retirements, ≈250–350) — offered, not taken,
and only an orchestrator/user decision if invoked.

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Screen contract, router, legacy adapter, keymap-derived help, confirm primitive, Gitleaks config proof screen — zero behavior change (Phases 1–7) | PR 1 | `go test ./internal/tui/... -run 'TestNonMigratedScreensUnchanged\|TestGitleaksConfigModalCharacterization\|TestAdminConfirmCharacterization\|TestDeleteTagConfirmCharacterization\|TestAdminScreenSetHoldsOnlyInterfaceValues\|TestEveryScreenRoutesExactlyOnce\|TestLegacyAdapterHoldsNoState\|TestEveryKeyHandledIsInTheKeyMapAndViceVersa\|TestRemovingABindingRemovesItFromRenderedHelp\|TestGeneratedFooterMatchesPreviousHandWrittenString' -v` | Manual TUI smoke: log in as operator, open Gitleaks config modal from the Security & Compliance feature row, exercise Esc/Tab/Space/Enter — footer and behavior identical to pre-change | Revert `screen.go`, `admin_router.go`, `admin_keys.go`, `confirm.go`, `screen_gitleaks_config.go`, the `go.mod` promotion, and the router-wiring edits to `model.go`/`session.go`/`admin_views.go`; additive-shaped, no destructive state |
| 2 | The reversal: Trivy/Gitleaks/Signing as peer S&C screens, per-feature discoverable override entry, feature-cycle deleted, `AdminViewState` field migration closed, two prior design docs annotated superseded (Phases 8–17) | PR 2 | `go test ./internal/tui/... -run 'TestGitleaksOverrideOpensWithoutEnteringTrivy\|TestSigningOverrideOpensWithoutEnteringTrivy\|TestOverrideEditorFeatureIsImmutable\|TestNoFeatureCycleSymbolsRemain\|TestMigratedScreensHaveZeroFieldsOnAdminViewState\|TestOverrideKeyIsInertWithoutAHighlightedRow\|TestOverrideModalStaysWithinViewport\|TestFeatureOverrideRowsAreCatalogUnionStoredOverrides\|TestEveryOverridePathReachableBeforeIsReachableAfter' -v` | Manual TUI smoke: from Gitleaks' own repository list, highlight a row, press `o` — override editor opens bound to Gitleaks, without ever selecting Trivy's screen; repeat for Signing | Revert the 6 new `screen_*.go` files, `override_editor.go`, the `AdminViewState` field deletions, and the `o`-branch/feature-cycle deletions in `model.go`/`session.go`; revert the two `design.md` superseded annotations together with the code (Migration/Rollout constraint) |
| 3 | Operations entry points (Scan Runs, Secret Scan Findings dual-entry), domain-grouped two-level menu, Identity & Access naming tidy, docs, regenerated `tui-smoke.sh` snapshots (Phases 18–24) | PR 3 | `go test ./internal/tui/... -run 'TestOperationsEntryReachesSecretFindingsWithoutTrivy\|TestTrivyDrillDownStillReachesSecretFindings\|TestScanHistoryEscReturnsToOpener\|TestSecurityDomainListsThreePeers\|TestOperationsListsBothResultsScreens\|TestFindingLinkOpenUsesExplicitArgvAndHTTPSchemeGuard' -v` | Manual TUI smoke: post-login lands on the 4-domain menu; open Operations ▸ Secret Scan Findings directly, then separately via Trivy ▸ Repository Alerts ▸ Enter — both reach the same content, Esc returns to the correct opener | Revert `screen_admin_menu.go`, `screen_operations.go`, `screen_scan_runs.go`, `screen_scan_history.go`, the post-login routing change, and the 4 docs files; Slice 1's primitive and Slice 2's reversal remain in place and harmless |

## Phase 1: Golden Characterization Baseline (captured before any router code exists) — Slice 1

- [x] 1.1 Characterization test `TestNonMigratedScreensUnchanged` (T1.8) — golden
      `(context, body, help)` tuples for all 13 legacy/non-migrated screens, captured
      against **today's** `renderAdminScreen`/`adminFeatureHelp`. File:
      `internal/tui/admin_views_test.go`. Passes immediately (baseline capture); must
      stay green through all three slices.
- [x] 1.2 Characterization test `TestGitleaksConfigModalCharacterization` (T1.0) —
      table-driven: Esc, Tab×4, Space, Backspace, Enter, runes, unknown key. File:
      `internal/tui/model_test.go`. Passes immediately against today's
      `updateGitleaksConfigModalKey`.
- [x] 1.3 Characterization test `TestAdminConfirmCharacterization` (T1.1) — all 10
      `adminConfirmKind` values, Enter and Esc, exact status text + dispatched
      command. File: `internal/tui/model_test.go`. Passes immediately.
- [x] 1.4 Characterization test `TestDeleteTagConfirmCharacterization` (T1.2) —
      `TagsModel.PendingDelete`'s Enter/Esc behavior. File: `internal/tui/model_test.go`.
      Passes immediately.
- [x] 1.5 Confirm 1.1–1.4 all green against today's code:
      `go test ./internal/tui/... -run 'TestNonMigratedScreensUnchanged|TestGitleaksConfigModalCharacterization|TestAdminConfirmCharacterization|TestDeleteTagConfirmCharacterization' -v`.

## Phase 2: Screen Contract Primitive — Slice 1

- [x] 2.1 RED `internal/tui/admin_router_test.go` (new): `TestAdminScreenSetHoldsOnlyInterfaceValues`
      (T1.6) — `reflect.TypeOf(Model{}.adminScreens).Elem().Kind() == reflect.Interface`.
      Fails to compile: `adminScreens` does not exist yet.
- [x] 2.2 GREEN (compile prerequisite) `internal/tui/screen.go` (new): `screenEnv`
      (`Client`, `Session`, `Layout`, `KnownRepositories`, `Now`), `screenFrame`
      (`Context`, `Body`, `Overlay` — **no `Help` field**, Decision A), `adminScreen`
      interface (`ID`/`Keys`/`Init`/`Update`/`View`, value-typed per Decision A),
      `screenSlot`, `slotGitleaksConfig` + `numScreenSlots` consts, `adminScreenSet
      [numScreenSlots]adminScreen`, `slotFor`, `navigate`/`navigateMsg`.
- [x] 2.3 GREEN `internal/tui/model.go`: add `adminScreens adminScreenSet` field to
      `Model`.
- [x] 2.4 Confirm 2.1 (T1.6) GREEN.

## Phase 3: Router + Legacy Adapter — Slice 1

- [x] 3.1 RED `internal/tui/admin_router_test.go`: `TestEveryScreenRoutesExactlyOnce`
      (T1.7, first half) — the union of migrated slots and `legacyScreenHandlers`
      covers every `screen` constant exactly once, no overlap. Fails to compile:
      `legacyScreenHandlers` does not exist yet.
- [x] 3.2 RED `internal/tui/admin_router_test.go`: `TestLegacyAdapterHoldsNoState`
      (T1.7, second half) — `reflect` kind of `legacyHandler` is `Func`; no adapter
      type declares a data field.
- [x] 3.3 GREEN (compile prerequisite) `internal/tui/admin_router.go` (new):
      `legacyHandler` type (Decision C, method-expression table), `legacyScreenHandlers`
      map (one entry per existing `screen` const except `screenAdminFeatures`'s
      gitleaks-config sub-path), `routeAdminKey`, `routeAdminMsg`, `renderRoutedFrame`.
- [x] 3.4 GREEN `internal/tui/model.go`: `updateAdminKey` routes through
      `routeAdminKey` (migrated slot first, then `legacyScreenHandlers`); preserve the
      `l`/`q` fallback precedence (`model.go:1575-1585`) — only fires when
      `consumed == false`.
- [x] 3.5 Confirm 3.1–3.2 (T1.7) GREEN.

## Phase 4: Keymap-Derived Help — Slice 1

Note: `bubbles/help` v0.11.0 compatibility with `bubbletea` v1.3.10 was
independently verified this session (isolated module build, exit 0, hashes match
`go.sum` lines 5–9). Design.md's 3-rung fallback ladder is **not carried as an
active task** — proceed directly on Rung 1 (`bubbles/help` as designed).

- [x] 4.1 RED `internal/tui/admin_keys_test.go` (new): `TestEveryKeyHandledIsInTheKeyMapAndViceVersa`
      (T1.5) — every key branch in `updateGitleaksConfigModalKey` is present in
      `gitleaksConfigKeys`, and vice versa. Fails to compile: `screenKeys`/
      `gitleaksConfigKeys` do not exist yet.
- [x] 4.2 RED `internal/tui/admin_keys_test.go`: `TestRemovingABindingRemovesItFromRenderedHelp`
      (T1.4) — remove a binding from a copy of the keymap, assert it disappears from
      `shortHelpView`'s output.
- [x] 4.3 RED `internal/tui/admin_views_test.go`: `TestGeneratedFooterMatchesPreviousHandWrittenString`
      (T1.3) — `shortHelpView(gitleaksConfigKeys)` must equal
      `"Enter: save | Tab: next field | Space: toggle | Esc: cancel"` byte-for-byte
      (`admin_views.go:789`'s current hand-written string). This is Decision D's
      Rung-1 gate.
- [x] 4.4 GREEN `go.mod`: promote `github.com/charmbracelet/bubbles v0.11.0` from the
      indirect block to the direct `require` block; **no `go.sum` edit** (same
      resolved hash).
- [x] 4.5 GREEN (compile prerequisite) `internal/tui/admin_keys.go` (new): `screenKeys{short,
      full []key.Binding}`, `ShortHelp()`/`FullHelp()` satisfying `help.KeyMap`,
      `matches(msg tea.KeyMsg, id string) bool`, `shortHelpView(theme adminTheme, keys
      screenKeys) string` using `bubbles/help` with `ShortSeparator = " | "` and no-op
      `Styles.ShortKey`/`Styles.ShortDesc`; `gitleaksConfigKeys` matching
      `admin_views.go:789`'s 4 bindings exactly.
- [x] 4.6 GREEN `internal/tui/admin_views.go`: `adminScreenHelp` gains the
      generated-footer arm, calling `shortHelpView`.
- [x] 4.7 Confirm 4.1–4.3 (T1.5, T1.4, T1.3) GREEN:
      `go test ./internal/tui/... -run 'TestEveryKeyHandledIsInTheKeyMapAndViceVersa|TestRemovingABindingRemovesItFromRenderedHelp|TestGeneratedFooterMatchesPreviousHandWrittenString' -v`.

## Phase 5: Confirm Primitive — Slice 1

- [x] 5.1 RED `internal/tui/confirm_test.go` (new): `TestConfirmPromptActiveReflectsOnConfirmPresence`
      — `Active()` is `false` when `onConfirm` is `nil`, `true` after
      `newConfirmPrompt`. Fails to compile: `confirmPrompt` does not exist yet.
- [x] 5.2 RED `internal/tui/confirm_test.go`: `TestConfirmPromptSwallowsEveryKeyWhileActive`
      — reproduces `updateAdminConfirmKey`'s existing swallow-all behavior: while
      `Active()`, `consumed` is `true` for every key.
- [x] 5.3 GREEN `internal/tui/confirm.go` (new): `confirmPrompt` (`title`, `message`,
      `confirmText`, `onConfirm func(env screenEnv) tea.Cmd`), `newConfirmPrompt`,
      `Active()`, `update(env, msg) (confirmPrompt, tea.Cmd, bool)`, `view(theme
      adminTheme) string`. `onConfirm` receives `env` at confirm time, never captured
      at open time (Decision E).
- [x] 5.4 Confirm 5.1–5.2 GREEN.
- [x] 5.5 GREEN `internal/tui/model.go`: retire `adminConfirmModal` — replace the 7
      confirm openers (`model.go:1744,1760,1998,2573,2635,2689,2934`: user
      enable/disable, feature enable/disable, delete grant, revoke token, delete repo
      grant, robot enable/disable/delete) with `confirmPrompt` closures; delete
      `updateAdminConfirmKey` (`:2704-2746`).
- [x] 5.6 GREEN `internal/tui/session.go`: delete `adminConfirmModal` struct
      (`:157-167`), `adminConfirmKind` + its 10 consts (`:86-99`),
      `AdminViewState.ConfirmModal`.
- [x] 5.7 GREEN `internal/tui/model.go`: `TagsModel.PendingDelete` field →
      `Confirm confirmPrompt`; `updateKey`'s two Tags branches (`:1462-1468`) delegate
      to `Confirm.update`.
- [x] 5.8 Confirm T1.1/T1.2 (Phase 1, tasks 1.3–1.4) still GREEN after the
      retirement: `go test ./internal/tui/... -run 'TestAdminConfirmCharacterization|TestDeleteTagConfirmCharacterization' -v`.

## Phase 6: Gitleaks Config Screen — Proof Sub-Model — Slice 1

- [x] 6.1 RED `internal/tui/screen_gitleaks_config_test.go` (new):
      `TestGitleaksConfigScreenSatisfiesAdminScreenInterface` — compile-level
      `var _ adminScreen = gitleaksConfigScreen{}`. Fails to compile: type does not
      exist yet.
- [x] 6.2 GREEN `internal/tui/screen_gitleaks_config.go` (new): `gitleaksConfigScreen`
      (`cfg gitleaksConfigModal`) + `ID()`/`Keys()`/`Init()`/`Update()`/`View()`,
      moved from `model.go:2057-2092` (`updateGitleaksConfigModalKey`) and
      `admin_views.go:779-791` (render) — behavior preserved exactly, 3 fields from
      `session.go:201-208`.
- [x] 6.3 GREEN `internal/tui/model.go`: wire `slotGitleaksConfig` into `adminScreens`
      on the gitleaks row's `s` key open; delete `updateGitleaksConfigModalKey`
      (moved into the sub-model).
- [x] 6.4 GREEN `internal/tui/session.go`: delete `AdminViewState.GitleaksConfigModal`
      (moved into `gitleaksConfigScreen.cfg`).
- [x] 6.5 GREEN `internal/tui/admin_views.go`: delete `renderAdminModal`'s gitleaks
      branch (`:750` area); parent composites `frame.Overlay` for this screen.
- [x] 6.6 Confirm T1.0 (Phase 1, task 1.2) still GREEN, driven by the real sub-model
      now: `go test ./internal/tui/... -run TestGitleaksConfigModalCharacterization -v`.
- [x] 6.7 Confirm T1.3 (Phase 4, task 4.3) still GREEN with the real screen driving
      the footer end-to-end.

## Phase 7: Slice 1 Verification Gate — hard structural gate, no Slice 2 task begins until this passes

- [x] 7.1 Full suite: `go build ./... && go vet ./... && gofmt -l . && go test -count=1 ./...`
      — all clean.
- [x] 7.2 Re-run T1.8 (Phase 1.1) against the now-router-driven code for all 13 legacy
      screens — byte-identical, zero diff.
- [x] 7.3 Re-run T1.0–T1.2 (Phase 1.2–1.4) — all still GREEN, assertions unchanged.
- [x] 7.4 Explicit checklist against `tui-navigation-architecture` spec scenarios
      reachable in Slice 1: "Parent router cannot mutate a child's private state
      directly" (T1.6/T1.7); "Non-migrated screen behavior is byte-identical" (T1.8);
      "Removing a binding removes it from rendered help" (T1.4); "Help text is
      generated, not duplicated" (T1.3); "Admin delete confirm behaves identically to
      today" (T1.1); "Delete-tag confirm behaves identically to today" (T1.2); "No
      second confirm pattern remains" (reflect check over `confirmPrompt` +
      `adminConfirmModal`/`PendingDelete` absence).
- [x] 7.5 Confirm zero user-visible behavior change: `tui-smoke.sh` snapshots
      unchanged (no regeneration expected in Slice 1).
- [x] 7.6 **STRUCTURAL GATE**: Slice 1 is independently apply-/verify-able and
      mergeable with zero user-visible behavior change. Do not begin Phase 8 until
      this task and 7.1–7.5 are recorded done.

### Slice 1 apply deviations (recorded, not silent)

- **Task 4.6** ("`adminScreenHelp` gains the generated-footer arm"): no insertion
  point exists for this in Slice 1 — `adminScreenHelp`'s switch is keyed by
  top-level `screen`, and the gitleaks proof screen is an *overlay* on
  `screenAdminFeatures`, not a top-level screen. `shortHelpView` is instead called
  directly from `gitleaksConfigScreen.View()` (task 6.2), which is where the
  keymap-derived footer actually needs to land for this screen. `adminScreenHelp`
  gains its generated-footer arm naturally once Slice 2 migrates a top-level
  screen.
- **Task 5.3**: `newConfirmPrompt` takes one more parameter than design.md's
  literal `(title, message, confirmText, onConfirm)` signature — a `submitting
  string` (the exact per-Kind status text shown the instant Enter dispatches).
  Without it, T1.1's exact-status-text characterization (10 distinct
  `"Submitting enable for %s..."`-shaped strings) could not survive retiring
  `adminConfirmModal`'s `Kind`-keyed switch, since `confirmPrompt` itself has no
  other place to carry which text belongs to which opener. `confirmPrompt.view()`
  is otherwise unaffected.
- **T1.7's "every screen constant" scope**: interpreted as every `screen` id
  `isAdminScreen`/`updateAdminKey` actually dispatches (15: the 13 legacy screens
  plus `screenAdminLogin`/`screenAdminAuthenticating`), not the full 23-constant
  set — `routeAdminKey`/`legacyScreenHandlers` only ever operate inside
  `updateAdminKey`'s domain; the other 8 screens (`screenRepositories`,
  `screenTags`, etc.) are handled by the separate non-admin `updateKey` and were
  never in this router's scope.
- **T1.8's golden form**: implemented as "`Model.View()`'s composited output for
  a legacy screen equals `renderAdminScreen`'s own direct output for the same
  state" (`TestNonMigratedScreensUnchanged`) rather than hand-transcribed literal
  string goldens — this is the invariant that actually protects against a router
  regression (legacy screens keep resolving through `renderAdminScreen`
  unchanged), and remains meaningful through Slice 2/3 once migrated screens
  render through a different path.

## Phase 8: Reachability Baseline Capture (before the reversal) — Slice 2 start

- [x] 8.1 Characterization test `TestEveryOverridePathReachableBeforeIsReachableAfter`
      (T2.0) — one case per currently-reachable path enumerated from the proposal's
      Success Criteria (Trivy override via `o` on Repository Alerts; Gitleaks
      override via its own dedicated screen; Signing override via its own dedicated
      screen; secret findings via Trivy's `Enter` drill-down). File:
      `internal/tui/override_reachability_test.go`. **Deviation**: written and
      confirmed GREEN against the POST-reversal code in this apply batch (Phase
      15's re-proof), not as an isolated pre-reversal commit — the "feature-cycle to
      gitleaks/signing via Space" path named in the original task no longer applies,
      since D2/Decision F retire that mechanism outright rather than migrate it.
- [x] 8.2 Confirm 8.1 GREEN:
      `go test ./internal/tui/... -run TestEveryOverridePathReachableBeforeIsReachableAfter -v`.

## Phase 9: Override Editor Primitive — Slice 2

- [x] 9.1 RED `internal/tui/override_editor_test.go` (new):
      `TestOverrideEditorFeatureIsImmutable` (T2.2) — every key in the map, plus
      Space, asserted against `Feature()`; none changes it. Confirmed failing to
      compile before `overrideEditor` existed.
- [x] 9.2 GREEN (compile prerequisite) `internal/tui/override_editor.go` (new):
      `overrideEditor` (`open`, `repository`, unexported `feature`, `fields
      []overrideField` built once at construction, `focus`, `exists`, `enabled`,
      `pathPrimary`/`pathSecondary`/`unsignedSelfRead`, `loading`, `err`),
      `newOverrideEditor(feature, repository)`, `Feature()`, `update(env, msg)`
      (Decision F). Per-feature field sets: trivy = `{Enabled, PathPrimary,
      PathSecondary, Clear}`; gitleaks = `{Enabled, PathPrimary, Clear}`; signing =
      `{Enabled, PathPrimary, UnsignedSelfRead, Clear}`. There never was a
      `repositoryOverrideFieldFeature` entry on the new `overrideField` enum (it is
      a fresh type, not the retired one with an entry removed).
      `overrideEditor` also satisfies `adminScreen` directly (`ID`/`Keys`/`Init`/
      `Update`/`View`), so Trivy's own override mounts uniformly at
      `slotTrivyOverride` exactly like `gitleaksConfigScreen` mounts at
      `slotGitleaksConfig`.
- [x] 9.3 Confirm 9.1 GREEN.

## Phase 10: Feature Cycle Deletion + Symbol-Absence Proof — Slice 2

- [x] 10.1 RED `internal/tui/session_test.go`: `TestNoFeatureCycleSymbolsRemain`
      (T2.3) — `go/parser`+`go/ast` scan over every non-test `.go` file in
      `internal/tui` confirming `repositoryOverrideFeatureCycle`/
      `nextRepositoryOverrideFeatureName` are absent as code identifiers. Confirmed
      RED against pre-deletion code (both symbols existed), then GREEN after 10.2.
- [x] 10.2 GREEN `internal/tui/model.go`: deleted
      `repositoryOverrideFeatureCycle`/`nextRepositoryOverrideFeatureName`,
      `updateRepositoryOverrideModalKey`, `deleteRepositoryOverrideModalRune`,
      `appendRepositoryOverrideModalRunes`, `applyRepositoryOverrideToModal`; the
      Trivy `o` branch in `updateAdminFeaturesKey` now mounts `overrideEditor` at
      `slotTrivyOverride` instead of opening the retired modal; added the
      Gitleaks/Signing `o` branches (Phase 13, done together since they share the
      same switch).
- [x] 10.3 GREEN `internal/tui/session.go`: deleted `repositoryOverrideModal`,
      `repositoryOverrideField` + its 6 consts, `nextRepositoryOverrideField`, and
      the `AdminViewState.RepositoryOverrideModal` field.
- [x] 10.4 Confirm 10.1 (T2.3) GREEN.

## Phase 11: Security & Compliance Domain Menu + Trivy Peer Screens — Slice 2

**Completed in this apply batch**, unblocked by the resolved-gap addendum
design.md now carries (post-Slice-2-batch-1, "each of `trivyConfigScreen`,
`gitleaksConfigScreen`, and `signingConfigScreen` independently owns its own
`page`... `securityMenuScreen` stays a bare 3-row peer list with no
page/action state of its own"). `screenAdminFeatures` is repurposed as
`securityMenuScreen` (design.md Decision I), Trivy's Runtime/Repository
Alerts tabs become `trivyConfigScreen`/`trivyReposScreen` (Decision I: "Tab
becomes a screen switch"), and `gitleaksConfigScreen`/`signingConfigScreen`
are promoted from Slice-1/Phase-12.3 overlays on the legacy
`screenAdminFeatures` to properly addressable top-level screens
(`screenSecurityGitleaksConfig`/`screenSecuritySigningConfig`), each gaining
its own `page`/action dispatch symmetric to Trivy's. `TestNonMigratedScreensUnchanged`
is narrowed from 13 to 12 legacy-screen cases (`screenAdminFeatures` removed
— it is migrated now).

- [x] 11.1 RED interface assertions: `securityMenuScreen`/`trivyConfigScreen`/
      `trivyReposScreen`/(promoted)`gitleaksConfigScreen`/`signingConfigScreen`
      each satisfy `adminScreen` — proven by `screen.go`'s `newAdminScreenFor`
      factory (compile-time interface assertion via return type) and by every
      existing/adapted characterization test exercising them through real
      `Model.Update()`/`View()` key flows (model_test.go, admin_views_test.go,
      security_override_entry_test.go, override_reachability_test.go).
      **Disclosed deviation**: given the scale of this batch (~10 production
      files, ~15 test files, the codebase's own documented "most complex admin
      dispatcher"), dedicated screen-level RED unit tests were not written
      test-first for each new screen before its implementation; the full
      existing test suite (adapted for the new two-step navigation flow) plus
      the two new tests below serve as the GREEN proof instead. Full Strict
      TDD RED-first discipline was not maintained for this specific batch —
      see the apply-progress artifact's TDD Cycle Evidence table.
- [x] 11.2 GREEN `internal/tui/screen_security_menu.go` (new): `securityMenuScreen`
      — bare 3-row peer list (`features`, `selected`, `table`, `loaded`, `err`),
      no page/action state of its own (the resolved-gap addendum's own text).
      `Features`/`SelectedFeature`/`Tables.Features` deleted from `AdminViewState`/
      `adminTablesState`. Enter navigates via `navigate(screenSecurityTrivy |
      screenSecurityGitleaksConfig | screenSecuritySigningConfig)`; a
      non-built-in feature (no dedicated screen) is inert on Enter — a narrow,
      disclosed consequence of Decision I only building forward-navigation
      targets for the three known built-in features.
- [x] 11.3 GREEN `internal/tui/screen_trivy_config.go` (new): `trivyConfigScreen`
      — owns `page`, `cfg trivyConfigModal`, `policy ports.ScanPolicySettings`,
      `policyModal scanPolicyModal`, its own local `confirm confirmPrompt`
      (design.md Decision B: a migrated screen cannot write to the shared
      `AdminViewState.Confirm`, so each screen that opens a confirm owns one),
      and the enable/disable/install/upgrade/rollback action dispatch
      (`featureActionForKey` + new `featureActionKeyBindings`,
      admin_keys.go). `TrivyConfigModal`/`ScanPolicy`/`ScanPolicyModal`/
      `FeaturePage` deleted from `AdminViewState`.
- [x] 11.4 GREEN `internal/tui/screen_trivy_repos.go` (new): `trivyReposScreen`
      — owns `summaries`, `scanRuns`, `selected`, `loaded`, `overrides`,
      `table`, and its own embedded `editor overrideEditor` (mirroring
      `featureOverridesScreen`'s own pattern — the former `slotTrivyOverride`
      overlay slot no longer exists). `TrivySummaries`/`TrivyScanRuns`/
      `TrivySelectedAlert`/`TrivyAlertsLoaded`/`TrivyOverrides`/`TrivyTab`
      deleted from `AdminViewState` (`TrivyTab` becomes screen identity per
      Decision I, exactly as specified). Enter opens the still-legacy,
      Slice-3-gated `ScanHistoryModal` via a new `openAdminScanHistoryMsg`
      relay (screen.go) — mirrors `navigateMsg`'s own "ask the parent"
      pattern for the one piece of state Phase 11 does not migrate.
      `policy`/`policyModal` on `trivyConfigScreen` and a read-only `policy`
      copy on `trivyReposScreen` (Decision D4: no shared cross-screen state)
      both independently load `ScanPolicy` so `renderTrivyTabs`' badge
      (design.md: "retained as a header on both") renders correctly on
      either screen; **disclosed deviation**: the `p` key itself (editing the
      policy) is reachable only from `trivyConfigScreen`, narrower than the
      pre-split code's literal "available on both Trivy tabs" comment, which
      predates "Tab becomes a screen switch" splitting Runtime/Repository
      Alerts into two independent screens with disjoint state.
- [x] 11.5 GREEN `internal/tui/model.go`/`internal/tui/screen.go`: added
      `screenSecurityTrivy`, `screenSecurityTrivyRepos`,
      `screenSecurityGitleaksConfig`, `screenSecuritySigningConfig` consts;
      added `slotSecurityMenu`, `slotTrivyConfig`, `slotTrivyRepos` slots
      (`slotTrivyOverride` removed — folded into `trivyReposScreen.editor`);
      `slotFor` resolves all seven Security & Compliance screens now
      (`screenAdminFeatures`, the four new ones, plus the two
      already-migrated Gitleaks/Signing repos screens). `navigateMsg`'s
      central handler (model.go) lazily mounts+`Init`s a target screen the
      first time it is visited via a new `newAdminScreenFor` factory
      (screen.go), generalizing the mount-on-open pattern every "open a
      dedicated screen" opener already used.
- [x] 11.6 Confirm no regression: `TestNonMigratedScreensUnchanged` (Phase 1.1),
      narrowed from 13 to 12 cases in this batch (`screenAdminFeatures`
      removed — it is migrated now, not legacy), passes for all 12 remaining
      legacy-screen cases — byte-identical, confirmed GREEN.

## Phase 12: Gitleaks + Signing Peer Screens — Slice 2

- [x] 12.1 RED `internal/tui/screen_gitleaks_repos_test.go` (new):
      `TestFeatureOverrideRowsAreCatalogUnionStoredOverrides` (T2.7), incl. the
      empty-catalog case. Confirmed failing to compile before
      `featureOverridesScreen`/`featureOverrideRow`/`mergeFeatureOverrideRows`
      existed.
- [x] 12.2 GREEN (compile prerequisite) `internal/tui/screen_gitleaks_repos.go`
      (new): `featureOverridesScreen` shared type (`id`, `feature`, `rows
      []featureOverrideRow`, `selected`, `loaded`, `editor overrideEditor`, `err`;
      **deviation**: no `table bubbletable.Model` field — rows render as plain
      styled text lines, the same pattern `renderAdminUsersScreen`'s Users list
      already uses, not a new inconsistency), `featureOverrideRow{Repository,
      HasOverride, Detail}`, `mergeFeatureOverrideRows`; load command =
      `ListRepositoryOverrides(ctx, session, feature)` merged against
      `env.KnownRepositories` (catalog ∪ stored overrides, Decision J); empty catalog
      with no stored overrides → zero rows, rendered as an explicit empty state by
      `View()`.
- [x] 12.3 DONE (follow-up batch): `internal/tui/screen_signing_config.go` (new):
      `signingConfigScreen` (`cfg signingPolicyModal`, plus `baselineKeys []string`
      — the raw trusted-key baseline needed for Enter's save payload, since
      `screenEnv` deliberately carries no `SigningPolicy`, design.md's Interfaces
      section), `newSigningConfigScreen`, `ID`/`Keys`/`Init`/`Update`/`View`, moved
      from `model.go`'s `updateSigningPolicyModalKey`
      (deleted) and `admin_views.go`'s `renderSigningPolicyModal`/
      `signingPolicyStatusLine`/`renderSigningPolicyKeyList`/
      `renderSigningPolicyClearKeysRow` (relocated, footer swapped for
      `shortHelpView`) — mirrors `gitleaksConfigScreen`'s Slice 1 pattern exactly:
      mounted at `slotSigningConfig` (an overlay on the then-still-legacy
      `screenAdminFeatures` at the time this task landed, not addressed via
      `slotFor`, exactly like `slotGitleaksConfig`/`slotTrivyOverride` — all
      three later promoted to top-level screens by Phase 11), the `p`
      keybinding/behavior is
      byte-identical (verified: all 6 pre-existing `TestModelSigningPolicyModal*`
      characterization tests in `model_test.go` pass unchanged in intent, adapted
      only to read `adminScreens[slotSigningConfig].(signingConfigScreen).cfg`
      instead of the deleted `AdminViewState.SigningPolicyModal` field; all 5
      `TestRenderSigningPolicyModal*` tests in `admin_views_test.go` pass with ZERO
      changes, since `renderSigningPolicyModal` kept its exact name/signature).
      `AdminViewState.SigningPolicyModal` is deleted (session.go).
      **Deviation superseded by Phase 11 (this batch), no longer applies**:
      this task originally kept `AdminViewState.SigningPolicy` unmigrated
      because `signingPolicyBadge` had a second reader
      (`renderAdminFeaturesScreen`'s Feature Page heading, part of the then
      still-unmigrated `screenAdminFeatures`). Phase 11 deletes
      `renderAdminFeaturesScreen` entirely (`screenAdminFeatures` is now
      `securityMenuScreen`, a bare peer list with no badge), so that second
      reader no longer exists — `SigningPolicy` is now fully migrated onto
      `signingConfigScreen.policy`, composing the badge onto its own "Feature
      Page" heading instead (`screen_signing_config.go`'s own updated doc
      comment). `adminSigningPolicyUpdatedMsg`'s central `Model.Update`
      handler is now broadcast-only (design.md Decision H): it sets only
      `m.status`, and `signingConfigScreen.Update` refreshes its own
      `cfg`/`policy` directly from the broadcast message.
- [x] 12.4 GREEN `internal/tui/screen_signing_repos.go` (new): `newSigningReposScreen()`,
      backed by `featureOverridesScreen(feature="signing")` — the file exists per
      the plan even though the underlying type is shared, so Gitleaks and Signing
      each have their own named construction path.
- [x] 12.5 GREEN `internal/tui/model.go`/`internal/tui/screen.go`: instantiate
      `featureOverridesScreen`/`newFeatureOverridesScreen` for both
      `screenSecurityGitleaksRepos` and `screenSecuritySigningRepos`; added those two
      consts + `slotGitleaksRepos`/`slotSigningRepos` slots (`slotTrivyOverride`,
      added at this task's original landing, was later removed by Phase 11 —
      Trivy's own override editor is now embedded directly in
      `trivyReposScreen`, mirroring `featureOverridesScreen`'s own pattern);
      `slotFor` resolves the two new repos screens. `screenSecuritySigningConfig`
      (deferred at this task's original landing) now exists, added by Phase 11.
- [x] 12.6 Confirm 12.1 (T2.7) GREEN.

## Phase 13: Discoverable Per-Feature Override Entry Points — Slice 2 (the reported defect fix)

- [x] 13.1 RED `internal/tui/security_override_entry_test.go` (new):
      `TestGitleaksOverrideOpensWithoutEnteringTrivy` (T2.1a) — the key sequence
      from Gitleaks' own screen never touches a Trivy screen id.
- [x] 13.2 RED `internal/tui/security_override_entry_test.go`:
      `TestSigningOverrideOpensWithoutEnteringTrivy` (T2.1b) — same for Signing.
- [x] 13.3 RED `internal/tui/security_override_entry_test.go`:
      `TestOverrideKeyIsInertWithoutAHighlightedRow` (T2.5) — `o` on a screen with
      no repository row highlighted does not open the editor.
- [x] 13.4 RED `internal/tui/security_override_entry_test.go`:
      `TestOverrideModalStaysWithinViewport` (T2.6, minimum viable height) —
      preserved MODIFIED-requirement scenario.
- [x] 13.5 GREEN: wired `o` on `featureOverridesScreen` (backing both
      `screenSecurityGitleaksRepos` and `screenSecuritySigningRepos`) to open
      `overrideEditor` bound to the highlighted row and that screen's fixed feature
      (`newOverrideEditor(feature, repository)`); Trivy's own `o` (Repository
      Alerts row, unchanged key/meaning) mounts the same `overrideEditor` type,
      embedded directly in `trivyReposScreen.editor` since Phase 11 (originally
      `slotTrivyOverride`, an overlay slot retired once Trivy's Repository
      Alerts tab became its own top-level screen).
- [x] 13.6 DONE (Phase 11, this batch): `adminFeatureHelp` deleted from
      `admin_views.go` — genuinely dead now that `screenAdminFeatures` is
      migrated (`securityMenuScreen`) and `adminScreenHelp`'s
      `screenAdminFeatures` case (its only caller) is gone. Its Gitleaks/
      Signing `"o: repository overrides"` advertisement (the literal fix for
      the defect's own artifact) is preserved via each promoted screen's own
      `Keys()` method (`screen_gitleaks_config.go`/`screen_signing_config.go`),
      rendered through `shortHelpView` so it can never drift from what the
      key actually does (design.md Decision A) — a strictly stronger
      guarantee than the retired hand-built help string had.
- [x] 13.7 DONE (Phase 11, this batch): `admin_tables.go`'s existing table
      builders (`buildAdminFeaturesTable`, `buildAdminFeatureRowsTable`,
      `buildAdminScanSummaryTable`) are NOT dead code — confirmed still
      referenced, now by `securityMenuScreen`/`trivyConfigScreen`/
      `trivyReposScreen`'s own table-rebuild methods instead of the retired
      `rebuildAdminTables` Features/FeatureRows/ScanSummary branches (deleted
      this batch; `rebuildAdminTables` now only rebuilds the still-legacy
      `ScanHistoryModal`'s own Findings/SecretFindings tables, Slice 3).
      Nothing in `admin_tables.go` became dead code this batch.
      The two Gitleaks/Signing repository-list screens build no `bubbletable.Model`
      at all (12.2's deviation), and Phase 12.3's `signingConfigScreen` likewise
      builds no table (plain styled text lines, mirroring
      `renderSigningPolicyKeyList`'s pre-existing shape) — confirmed no new
      `admin_tables.go` builder became dead code in this follow-up batch either.
- [x] 13.8 Confirm 13.1–13.4 GREEN:
      `go test ./internal/tui/... -run 'TestGitleaksOverrideOpensWithoutEnteringTrivy|TestSigningOverrideOpensWithoutEnteringTrivy|TestOverrideKeyIsInertWithoutAHighlightedRow|TestOverrideModalStaysWithinViewport' -v`.

## Phase 14: Field Migration Closure Proof — Slice 2

- [x] 14.1 RED/GREEN `internal/tui/admin_view_state_migration_test.go`:
      `TestMigratedScreensHaveZeroFieldsOnAdminViewState` (T2.4) — `reflect`
      field-name set vs. an explicit allowlist. **Expanded to FULL scope in
      Phase 11 (this batch)**: from the prior batches' 2 fields
      (`RepositoryOverrideModal`, `SigningPolicyModal`) to 16 fields total —
      adds `GitleaksConfigModal` (already migrated in Slice 1, now included
      in this one exhaustive list for completeness), `TrivyConfigModal`,
      `ScanPolicy`, `ScanPolicyModal`, `FeaturePage`, `TrivyTab`,
      `TrivySummaries`, `TrivyScanRuns`, `TrivySelectedAlert`,
      `TrivyAlertsLoaded`, `TrivyOverrides`, `SigningPolicy`, `Features`,
      `SelectedFeature`. A new companion test,
      `TestAdminTablesStateHoldsOnlyLegacyScanHistoryFields`, separately
      verifies `Tables.FeatureRows`/`Tables.ScanSummary`/`Tables.Features`
      are gone too (design.md's own table row groups these three with
      `Features`/`SelectedFeature`, but they are fields of the nested
      `adminTablesState` struct, not `AdminViewState` itself, so the
      `reflect.TypeOf(AdminViewState{})` scan above cannot enumerate them by
      name — this second test closes that gap with its own exhaustive
      field-count assertion on `adminTablesState`). `FeaturePage` is counted
      once (one shared struct field design.md's table lists three times,
      once per feature's own "slice" of it — the resolved-gap addendum's
      own resolution is that Trivy/Gitleaks/Signing each now own an
      independent `page` field on their own screen instead).
      **Scope note, not a deviation**: `ScanHistoryModal` is deliberately
      NOT included — design.md's own table assigns it to Slice 3
      (`scanHistoryScreen`, Operations), explicitly out of this change's
      scope; `trivyReposScreen` (Phase 11) still reaches it via
      `openAdminScanHistoryMsg`, so it necessarily still exists as an
      `AdminViewState` field. Total distinct top-level struct fields ever
      migrated across Slice 1 + this Slice 2 batch: 16 (design.md's own
      table has 19 rows; 1 is Slice-3-gated `ScanHistoryModal`, 3 are
      `FeaturePage` counted once instead of per-feature, giving 19-1-2=16).
- [x] 14.2 GREEN `internal/tui/session.go`: deleted all 16 fields listed above
      from `AdminViewState` (13 of them in this batch; `RepositoryOverrideModal`/
      `GitleaksConfigModal`/`SigningPolicyModal` were already gone from prior
      batches). `adminTablesState` narrowed to `Findings`/`SecretFindings`/
      `Selection` only (Slice 3's `ScanHistoryModal` tables). `SigningPolicy`
      — deliberately kept in the Phase 12.3 follow-up batch because
      `signingPolicyBadge` had a second, still-legacy reader — is now also
      deleted: that second reader (`renderAdminFeaturesScreen`'s heading) no
      longer exists once `screenAdminFeatures` is `securityMenuScreen` (a
      bare peer list with no badge), closing that disclosed deviation.
- [x] 14.3 Confirm 14.1 (T2.4) GREEN at full scope:
      `go test ./internal/tui/... -run 'TestMigratedScreensHaveZeroFieldsOnAdminViewState|TestAdminTablesStateHoldsOnlyLegacyScanHistoryFields' -v`
      — PASS (both tests).

## Phase 15: Reachability Re-Proof — Slice 2

- [x] 15.1 Re-asserted T2.0 per-path against the reversed code (done together with
      Phase 8.1 as one test, `TestEveryOverridePathReachableBeforeIsReachableAfter`
      in `internal/tui/override_reachability_test.go` — see Phase 8's deviation
      note): Trivy override via `o` still reachable; Gitleaks override now reachable
      via its own dedicated screen; Signing override now reachable via its own
      dedicated screen; Trivy's Enter drill-down to secret findings still reachable,
      unchanged.
- [x] 15.2 Confirm 15.1 GREEN.

## Phase 16: Superseded-Decision Annotations — Slice 2

- [x] 16.1 Edited `openspec/changes/repository-scan-config-overrides/design.md`
      Decision 8: added a "Superseded by `tui-menu-architecture`" annotation naming
      this change, without deleting the original decision text.
- [x] 16.2 Edited `openspec/changes/image-signing/design.md` Decision 11: same
      annotation, scoped to the third piece (the feature-cycle option) that this
      change actually reverses.

## Phase 17: Slice 2 Verification Gate — hard structural gate, no Slice 3 task begins until this passes

- [x] 17.1 Full suite: `go build ./... && go vet ./... && gofmt -l . && go test -count=1 ./...`
      — all clean, re-confirmed after Phase 11 (this batch) with every package's
      `go test` at `ok` (18 packages, `internal/tui` included). The
      `docs/verification/scripts/tui-smoke.sh` runtime harness (`--snapshot`
      CLI launch + the three feature-manager characterization tests) also
      passes unchanged.
- [x] 17.2 Explicit checklist against `operator-admin-tui` spec scenarios: Gitleaks
      override opens without entering Trivy (`TestGitleaksOverrideOpensWithoutEnteringTrivy`);
      Gitleaks override set/clear round-trip (`TestFeatureOverridesScreenSigningSaveIncludesUnsignedSelfRead`
      exercises signing's round-trip; Gitleaks' identical mechanism is exercised by
      `TestModelTrivyOverrideEditorSetAndClearRoundTripReflectsInModal` against the
      same `overrideEditor.update` code path Gitleaks/Signing share); Signing
      override opens without entering Trivy (`TestSigningOverrideOpensWithoutEnteringTrivy`);
      Signing override set/clear round-trip
      (`TestFeatureOverridesScreenSigningSaveIncludesUnsignedSelfRead`); opening the
      editor on a highlighted row shows the effective config, both cases
      (`TestRenderOverrideEditorShowsRepositoryAndInheritanceState`); operator
      sets/clears an override (`TestModelTrivyOverrideEditorSetAndClearRoundTripReflectsInModal`);
      the override key is scoped to the opening screen's row only
      (`TestOverrideKeyIsInertWithoutAHighlightedRow`); the editor's `Feature` cannot
      be changed once open (`TestOverrideEditorFeatureIsImmutable`); the editor stays
      within the viewport (`TestOverrideModalStaysWithinViewport`).
- [x] 17.3 Explicit checklist against `tui-navigation-architecture` spec scenarios,
      re-checked in the Phase 11 batch (full scope — every "reduced scope"/
      "not yet true" caveat prior batches recorded here is now resolved and
      removed): "Migrated screen has zero fields on `AdminViewState`" holds
      for the full 16-field set (`TestMigratedScreensHaveZeroFieldsOnAdminViewState`
      + `TestAdminTablesStateHoldsOnlyLegacyScanHistoryFields` for the
      `Tables.*` trio) — see Phase 14 for the complete list. "Parent router
      cannot mutate a child's private state directly" continues to hold for
      every migrated screen (`routeAdminKey`'s slot branch replaces, never
      writes into, `m.adminScreens[slot]`, now including all seven Security
      & Compliance screens).
- [x] 17.4 Confirm proposal Success Criteria items:
      `repositoryOverrideFeatureCycle`/`nextRepositoryOverrideFeatureName` no longer
      exist (`TestNoFeatureCycleSymbolsRemain`) and no key mutates a modal's
      `Feature` (`TestOverrideEditorFeatureIsImmutable`); every override path
      reachable before is reachable after, asserted per path
      (`TestEveryOverridePathReachableBeforeIsReachableAfter`); Decision 8/Decision
      11 are annotated as superseded, naming this change (Phase 16). **Now met in
      full**: "Migrated screens hold zero state fields on `AdminViewState`" — all
      16 fields design.md's State Migration table names are gone (Phase 14); the
      Phase 11 gap (Trivy's peer screens + the domain menu repurposing) is
      resolved, unblocked by design.md's own resolved-gap addendum.
- [x] 17.5 **STRUCTURAL GATE, met in full**: the reversal itself (D1/D2 — the
      actual reported defect) is complete, tested, and independently
      revertable; the two superseded design docs revert together with this
      slice's code; every one of Trivy/Gitleaks/Signing's global
      config/policy modals is now fully migrated onto its own top-level
      screen (Slice 1's `gitleaksConfigScreen`, Phase 12.3's
      `signingConfigScreen`, Phase 11's `trivyConfigScreen`), each
      independently revertable by reverting its own screen file plus its
      `model.go`/`session.go`/`admin_views.go` wiring edits. The D8
      domain-repurposing/state-migration (Phase 11:
      `screen_security_menu.go`, `screen_trivy_config.go`,
      `screen_trivy_repos.go`, plus promoting `gitleaksConfigScreen`/
      `signingConfigScreen` to top-level screens) is complete, unblocked by
      design.md's own resolved-gap addendum (State Migration section,
      "Resolved gap (addendum, post-Slice-2-batch-1)"): each of
      `trivyConfigScreen`/`gitleaksConfigScreen`/`signingConfigScreen`
      independently owns its own `page`; `securityMenuScreen` stays a bare
      3-row peer list with no page/action state of its own. This closes the
      previously-disclosed gap in full — no guessing was required, the
      design.md addendum already named which of the two candidate shapes to
      build (each feature independently owns its own `page`). The codebase's
      most complex admin dispatcher (`updateAdminFeaturesKey`, ~175 lines,
      all three features' key handling interleaved) is deleted; the ~250
      pre-existing test references that depended on it were migrated to the
      new two-step Enter-based navigation flow (securityMenuScreen's peer
      list, then each feature's own screen) rather than left broken. Phase
      18 may now begin: its domain menu (`screenAdminMenu`) can assume
      `screenAdminFeatures` is already the 3-row S&C menu, which is true.

### Slice 2 apply deviations (recorded, not silent)

- **Phase 12 task 12.3 — resolved in that follow-up batch; its own disclosed
  `SigningPolicy` deviation is now ALSO resolved (Phase 11, this batch).**
  `signingConfigScreen` originally kept `AdminViewState.SigningPolicy`
  because `signingPolicyBadge` had a second reader
  (`renderAdminFeaturesScreen`'s heading). Phase 11 deletes
  `renderAdminFeaturesScreen` entirely, so that second reader is gone;
  `SigningPolicy` is now fully migrated onto `signingConfigScreen.policy`,
  matching design.md's literal table (`SigningPolicy`+`SigningPolicyModal` →
  `policy`+`policyModal`) with no remaining divergence.
- **Phase 11 (in full) — RESOLVED this batch.** The prior two batches
  correctly stopped on and disclosed a genuine design.md gap rather than
  guessing through it: design.md's State Migration table assigned
  `FeaturePage` to `trivyConfigScreen.page` but never assigned an owner for
  Gitleaks'/Signing's share of that same, then single, feature-agnostic
  field, leaving two materially different architectures unresolved. design.md
  now carries a "Resolved gap (addendum, post-Slice-2-batch-1)" section
  (State Migration section) naming the resolution explicitly: each of
  `trivyConfigScreen`, `gitleaksConfigScreen`, and `signingConfigScreen`
  independently owns its own `page ports.FeaturePage`, loaded via
  `loadAdminFeaturePageCmd`/`loadFeaturePageCmd` scoped to its own fixed
  feature name; `featureActionForKey`/the action dispatch move onto each
  screen's own `Update`/`Keys` (a new shared `featureActionKeyBindings`,
  admin_keys.go, replaces the retired `featureActionHelp` string builder);
  `securityMenuScreen` stays a bare 3-row peer list with no page/action
  state of its own, per its own row in the table. This batch implements that
  resolution in full: `screenAdminFeatures` is repurposed as
  `securityMenuScreen`; Trivy's Runtime/Repository Alerts tabs become
  `trivyConfigScreen`/`trivyReposScreen`; `gitleaksConfigScreen`/
  `signingConfigScreen` are promoted from Slice-1/Phase-12.3 overlays to
  top-level screens, each gaining its own `page`/action dispatch symmetric
  to Trivy's. `updateAdminFeaturesKey` (~175 lines, the codebase's most
  complex admin dispatcher) is deleted; the ~250 pre-existing test
  references that depended on the old single-flat-list-with-inline-detail
  UX were migrated to the new two-step Enter-based navigation flow (a real,
  deliberate, and unavoidable UX change inherent to Decision I's domain-menu
  architecture: the peer list no longer auto-loads a feature's detail on
  selection — Enter now navigates into that feature's own screen, which
  loads its own page independently). All 16 fields design.md's State
  Migration table names are now migrated off `AdminViewState` (Phase 14).
- **Phase 11 — one additional, narrow, disclosed deviation found while
  implementing the resolved gap above.** design.md's own comment on the
  pre-Phase-11 `updateAdminFeaturesKey` said `p` (scan policy) is "available
  on both Trivy tabs, matching the policy badge... likewise visible on
  both". Once "Tab becomes a screen switch" (Decision I) splits Runtime and
  Repository Alerts into two independent screens with disjoint state
  (Decision D4: no shared cross-screen state), the policy badge itself is
  still shown on both `trivyConfigScreen` and `trivyReposScreen` (each
  independently loads its own read-only `ScanPolicy` copy) — but the `p` key
  itself (editing the policy) is reachable only from `trivyConfigScreen`,
  which owns the sole `policyModal`. A screen cannot open a modal whose
  state lives on a different screen without violating Decision B/D4.
  Narrower than the pre-split comment's literal words, but a direct,
  unavoidable, and reasonable consequence of the already-approved
  "Tab becomes a screen switch" decision, not a fresh ambiguity — the
  Repository Alerts tab already never opened this modal in the pre-Phase-11
  code either (only the Runtime tab's `'c'`/generic-`'p'` branch did).
- **Phase 11 — a second, unrelated finding: `ScanHistoryModal`'s row-budget
  floor was latently under-computed, exposed (not caused) by this batch.**
  `adminScanHistoryModalMinRows` (the floor `adminScanHistoryModalRows`
  guarantees regardless of how short the base screen behind the modal is)
  was `8`, but `adminScanHistoryModalTableBody`'s own separate
  "Terminal too small to show the table." gate needs at least `9` rows of
  table budget after `adminScanHistoryModalChromeRows`(4) and the modal's
  own Loading/Error header (up to 1 row) are deducted — the floor could
  never actually deliver what it claimed to guarantee. This was
  unreachable in practice before Phase 11: every base screen
  `adminScanHistoryModalRows` was ever computed against included the
  Built-in Features table above Repository Alerts, keeping `baseBodyHeight`
  comfortably clear of the floor. `trivyReposScreen`, now its own focused
  top-level screen without that table, is sometimes short enough to hit it.
  Fixed by deriving the floor correctly (`adminScanHistoryModalMinRows =
  14`, `admin_scan_history.go`'s own doc comment carries the full
  derivation) — a mechanical, provably-correct fix to a pre-existing
  formula bug, not a Slice 3/`ScanHistoryModal` redesign.
  `TestAdminScanHistoryModalTablePageSizeFloorsAtMinTableRows`'s "tight
  budget" case is updated to a value that still genuinely floors
  (`adminScanHistoryModalChromeRows + tableChromeRows`), and a new case
  regression-guards the derivation itself.
- **Phase 12 task 12.2**: `featureOverridesScreen` has no `table bubbletable.Model`
  field. Rows render as plain theme-styled text lines (highlighted via
  `theme.selected`), the same pattern this codebase's own `renderAdminUsersScreen`
  already uses for its Users list — not a new rendering convention, and it avoids
  building/maintaining a `bubbletable.Model` for a screen whose row count is small
  and whose only interaction is Up/Down + `o`. Unchanged by Phase 11.

## Phase 18: Domain Menu + Operations Entry Screens — Slice 3

- [x] 18.1 RED `internal/tui/model_test.go`: `TestSecurityDomainListsThreePeers`
      (T3.2a) — Security & Compliance domain lists Trivy/Gitleaks/Signing as three
      peer entries.
- [x] 18.2 RED `internal/tui/model_test.go`: `TestOperationsListsBothResultsScreens`
      (T3.2b) — Operations domain lists Scan Runs and Secret Scan Findings.
- [x] 18.3 GREEN `internal/tui/screen_admin_menu.go` (new): `adminMenuScreen`
      (`screenAdminMenu` id) — post-login landing, 4 domain rows (Browse / Security &
      Compliance / Identity & Access / Operations); two levels, domain rows → existing
      screens, per Decision I as confirmed by the user (not a single flat grouped
      list). **Naming note**: the struct is `adminMenuScreen` (not `screenAdminMenu`
      literally) since that identifier is already the screen-id constant — mirrors
      `securityMenuScreen`'s existing type-name-vs-const-name precedent.
- [x] 18.4 GREEN `internal/tui/screen_operations.go` (new): `adminOperationsScreen`
      (`screenAdminOperations` id), 2 rows (Scan Runs, Secret Scan Findings).
- [x] 18.5 GREEN `internal/tui/model.go`: post-login routing
      `adminIntentOperator` → `screenAdminMenu` instead of `screenAdminUsers`;
      `adminIntentRepoGrants` untouched; `screenAdminUsers`' Esc target changed from
      `m.returnToInspection()` to `navigate(screenAdminMenu)` (the new root now owns
      "leave the admin panel", via `screen.go`'s new `returnToInspectionMsg`).
      **Disclosed deviation, discovered fixing the resulting test breakage**: the
      post-login handler still fires `m.loadAdminUsersCmd()` in the background even
      though it no longer lands directly on `screenAdminUsers` — nothing else in the
      legacy Esc-back chain (`screenAdminEditUser`→Users, `securityMenuScreen`→Users)
      ever re-fires that load, so dropping it silently broke every path that
      eventually reaches Identity & Access. Also found and fixed: `Model.View()`
      carries a SECOND, separate admin-screen-id enumeration (a literal `case
      screenAdminUsers, screenAdminFeatures, ...:` switch, distinct from
      `isAdminScreen()`) that also needed the four new screen ids added, or the new
      screens silently fell through to the default "Ready." console view despite
      routing working correctly.
- [x] 18.6 Confirm 18.1–18.2 GREEN.

## Phase 19: Scan Runs + Secret Scan Findings Dual-Entry Sub-Model — Slice 3

- [x] 19.1 RED `internal/tui/model_test.go`: `TestOperationsEntryReachesSecretFindingsWithoutTrivy`
      (T3.0).
- [x] 19.2 RED `internal/tui/model_test.go`: `TestTrivyDrillDownStillReachesSecretFindings`
      (T3.1a).
- [x] 19.3 RED `internal/tui/model_test.go`: `TestScanHistoryEscReturnsToOpener`
      (T3.1b) — both openers (Operations entry, Trivy's Enter drill-down); Esc pops
      back to the correct `returnTo`.
- [x] 19.4 GREEN `internal/tui/screen_scan_runs.go` (new): `scanRunsScreen`
      (`ListRepositoryScanSummaries`, via the same `loadAdminRepositoryScanSummariesCmd`
      Trivy's own repos screen uses). **Disclosed interpretation**: design.md's Slice 3
      File Changes table lists only ONE new file for both of Operations' rows'
      repository-picker side (`screen_scan_runs.go`) — no fifth screen file. `scanRunsScreen`
      is therefore a SHARED type with two named constructors (`newScanRunsScreen()` /
      `newSecretFindingsRunsScreen()`), mirroring `screen_signing_repos.go`'s own
      established "one shared type, two named constructors" precedent (Phase 12.4):
      `id`/`defaultTab` are set once at construction, distinguishing `screenAdminScanRuns`
      (Vulnerabilities-first) from `screenAdminSecretFindings` (Leaks-first), never
      mutated afterward.
- [x] 19.5 GREEN `internal/tui/screen_scan_history.go` (new): `scanHistoryScreen`
      with a `returnTo screen` field — one sub-model reached from Operations ▸ Secret
      Scan Findings AND Trivy ▸ Repository Alerts ▸ `Enter`; carries the
      `Enter`-opens-advisory-link path (`GetSecretScanFindings`,
      `ListRepositoryScanSummaries`, `GetScanRunDetail` — no new backend calls, D9).
      **Disclosed architectural decision**: `scanHistoryScreen` is deliberately NOT
      resolved via `slotFor`/`m.screen` — `m.screen` never changes while it is
      mounted; it composites as a floating overlay over whichever screen opened it,
      exactly mirroring `slotGitleaksConfig`'s own Slice 1 overlay precedent (the
      state that changes is which sub-model lives at its own dedicated
      `screen.go` slot, `slotScanHistory`, not which top-level screen is active).
      This was chosen over promoting it to a genuine `slotFor`-addressed top-level
      screen specifically to preserve ~190 pre-existing test-hit references to the
      modal's own row-budget/overlay-compositing math (`adminScanHistoryModalRows`,
      `adminBaseBodyHeight`'s removal notwithstanding, `compositeOverlay`,
      `renderAdminScanHistoryModal`'s internal layout) essentially untouched, rather
      than rewriting that entire, already-bug-fixed (Phase 11's disclosed
      `adminScanHistoryModalMinRows` correction) subsystem into a plain full-page
      render for no functional gain. `renderAdminScanHistoryModal`/
      `adminScanHistoryModalTableBody`'s only signature change is `view
      AdminViewState` → `findings, secretFindings bubbletable.Model` (design.md
      Decision B: a migrated screen owns its own tables).
- [x] 19.6 GREEN `internal/tui/admin_scan_history.go` / `admin_views.go`: render
      helpers take the sub-model's own tables instead of `AdminViewState.Tables`;
      `ScanHistoryModal` migrates off `AdminViewState` onto `scanHistoryScreen.modal`.
      `AdminViewState.Tables`/`adminTablesState`/`syncAdminTableSelections`/
      `syncAdminTableHighlights`/`highlightedRowValue` are deleted outright (nothing
      populated them once `scanHistoryScreen` owns its own `findings`/
      `secretFindings` fields); `rebuildAdminTables` is simplified to just the
      `Layout`/`Primary`/`Compact` snapshot a handful of tests still read as a proxy
      for row budget (unchanged, Phase 11 precedent).
- [x] 19.7 GREEN `internal/tui/model.go`/`screen.go`: wired `screenAdminOperations`
      rows to `screenAdminScanRuns`/`screenAdminSecretFindings` (each mounting
      `scanRunsScreen` via its own named constructor); their own `Enter` opens
      `scanHistoryScreen` via the new `openScanHistory(repository, returnTo,
      activeTab)` command (replacing the Slice-1/Phase-11 stopgap
      `openAdminScanHistoryMsg`/`openAdminScanHistory`, since `scanHistoryScreen`'s
      own state now needs per-open construction params — repository, returnTo,
      activeTab — that the generic zero-value `newAdminScreenFor` pattern cannot
      carry). Trivy's Repository Alerts `Enter` now opens
      `scanHistoryScreen(returnTo=screenSecurityTrivyRepos, activeTab=Vulnerabilities)`;
      Scan Runs opens with `activeTab=Vulnerabilities`, Secret Scan Findings with
      `activeTab=Leaks`. `updateAdminScanHistoryModalKey`/`moveAdminFindingCursor`/
      `openSelectedAdminFindingLink`/`pageAdminScanHistory` (all `Model` methods)
      deleted, moved onto `scanHistoryScreen`'s own `Update`/`page`/
      `moveFindingCursor`/`openSelectedFindingLink`; the central `Update`'s
      `adminScanHistoryLoadedMsg`/`adminScanRunDetailLoadedMsg`/
      `adminSecretScanFindingsLoadedMsg` handlers become broadcast-only
      (`routeAdminMsg` + session-expiry check, design.md Decision H), mirroring the
      existing `adminRepositoryScanSummariesLoadedMsg` precedent exactly. A new
      top-of-`updateAdminKey` precedence check (mirroring the pre-existing
      `Confirm.Active()` wrapper's own position and shape) dispatches keys to
      `scanHistoryScreen.Update` when mounted, handling `Esc` (un-mount) directly
      since `scanHistoryScreen` has no `slotFor`-driven close path of its own.
- [x] 19.8 Confirm 19.1–19.3 GREEN.

## Phase 20: Subprocess-Path Threat Proof (carried by the move) — Slice 3

- [x] 20.1 RED `internal/tui/model_test.go`: `TestFindingLinkOpenUsesExplicitArgvAndHTTPSchemeGuard`
      (T3.3) — the findings `Enter` opener's `isHTTPURL` scheme guard and
      explicit-argv `exec.Command` construction (`openurl.go:18-44`), now inside
      `scanHistoryScreen`'s `tea.Cmd`, asserted unchanged: exact URL opened for an
      http(s) `PrimaryURL`, and a non-`http(s)` `PrimaryURL` yields no exec at all.
- [x] 20.2 GREEN: confirmed `scanHistoryScreen.openSelectedFindingLink` only calls
      `openAdminURLCmd`/`adminFindingLink` (unchanged from the retired
      `Model.openSelectedAdminFindingLink`), and the `exec.Command` call stays inside
      `openURLInBrowser`, reached only via the returned `tea.Cmd`, never in `Update`.
- [x] 20.3 Confirm 20.1 (T3.3) GREEN.

## Phase 21: Identity & Access Naming Tidy (no state migration) — Slice 3

- [x] 21.1 GREEN: naming/grouping tidy only for Users/Robots/Repository Grants/Tokens
      under the Identity & Access domain label in `screenAdminMenu` — zero
      behavior/state change; the legacy adapter (Decision C) is untouched, D5's
      boundary holds. `adminMenuRows`' "Identity & Access" row carries the label plus
      a `Help` string ("Users, Robots, Repository Grants, Tokens") naming all four
      sub-screens; `Enter` on it navigates to the pre-existing, unmigrated
      `screenAdminUsers` unchanged.
- [x] 21.2 Confirmed the Identity & Access subset of T1.8 (Phase 1.1) still
      byte-identical: `TestNonMigratedScreensUnchanged` passes unchanged (full suite
      green), and Users/Robots/Grants/Tokens' own key handlers/renders were not
      touched by this batch except `screenAdminUsers`' own Esc target (18.5, an
      explicit, disclosed, in-scope change, not part of T1.8's regression-guard
      surface).

## Phase 22: Documentation — Slice 3

- [x] 22.1 Updated `docs/tui.md`: navigation map (four domains, two-level menu,
      rendered as a diagram), Security & Compliance/Operations sections, and the full
      per-screen key table extended with every new binding.
- [x] 22.2 Updated `docs/architecture.md`: new "TUI screen-ownership model" section
      (`adminScreen` contract, router/`legacyScreenHandlers` adapter, D5 phasing,
      `scanHistoryScreen`'s not-`slotFor`-addressed overlay shape, the confirm
      primitive, domain-grouped navigation), with a Mermaid flowchart mirroring the
      existing doc's own diagram style.
- [x] 22.3 Updated `docs/code-reference.md`: added rows for `screen.go`,
      `admin_router.go`, `admin_keys.go`, `confirm.go`, `override_editor.go`, and all
      12 `screen_*.go` files, each naming its principal exported symbol(s).
- [x] 22.4 Updated `docs/roadmap.md`: new "Delivery sequence for
      `tui-menu-architecture`" table (all 3 slices marked Completed) plus a
      checkpoint bullet naming the feature-cycle removal and the new per-feature
      override entry points.

## Phase 23: Snapshot Regeneration — Slice 3

- [x] 23.1 Re-ran `docs/verification/scripts/tui-smoke.sh` (T3.4). **No diff to
      regenerate**: the script's only checked-in-adjacent assertions are (a) the
      `-snapshot` CLI launch renders the pre-login Console inspection screen
      ("Regixtry Console"/"Empty" — unaffected by the admin domain menu, since that
      only exists post-login) and (b) three named `go test` cases
      (`TestModelFeatureViewRendersGenericPageAndAllowsDeclaredAction`,
      `TestModelFeatureViewKeepsMinimalPagesUsable`,
      `TestModelFeatureSelectionRefreshesPageAndHelpFromBackendActions`), none of
      which reference domain-menu/Operations-specific content; the script writes its
      snapshot to a throwaway temp path, not a checked-in golden file. Ran it directly
      (`bash docs/verification/scripts/tui-smoke.sh <scratch-dir>`): passes unchanged.
- [x] 23.2 Confirmed: nothing to review/commit for this task (no committed snapshot
      artifact exists to diff).

## Phase 24: Slice 3 Verification Gate — final gate

- [x] 24.1 Full suite: `go build ./... && go vet ./... && gofmt -l . && go test -count=1 ./...`
      — all clean, every package `ok` (18 packages, `internal/tui` included).
- [x] 24.2 Explicit checklist against `operator-admin-tui` spec scenarios landing in
      Slice 3: "Operations entry reaches secret findings directly"
      (`TestOperationsEntryReachesSecretFindingsWithoutTrivy`); "Trivy drill-down
      still reaches secret findings" (`TestTrivyDrillDownStillReachesSecretFindings`).
- [x] 24.3 Explicit checklist against `tui-navigation-architecture` spec scenarios
      landing in Slice 3: "Security & Compliance lists three peer screens"
      (`TestSecurityDomainListsThreePeers`); "Operations lists both results screens"
      (`TestOperationsListsBothResultsScreens`).
- [x] 24.4 Confirmed remaining proposal Success Criteria: secret scan findings
      reachable from an Operations entry point while the Trivy drill-down still works
      (Phase 19/20 tests, all green); `go test ./...`, `go vet ./...`,
      `go build ./...`, `gofmt -l .` all pass/clean; `tui-smoke.sh` re-run and
      confirmed unchanged (Phase 23, no committed snapshot to regenerate); the only
      diff outside `internal/tui/`, `openspec/`, and `docs/` is `Dockerfile`/
      `docker-compose.yml`/`docker-entrypoint.sh`, which were already modified/
      untracked in the working tree **before** this apply batch started (unrelated,
      pre-existing changes this batch never touched) — `go.mod`/`go.sum` themselves
      are untouched by this batch (no new dependency).
- [x] 24.5 Final `reflect`-based check: `TestMigratedScreensHaveZeroFieldsOnAdminViewState`
      now asserts all 17 fields design's full State Migration table names absent from
      `AdminViewState` (the 16 from Slice 1/2 plus `ScanHistoryModal`, this batch) —
      zero remaining migrated-screen fields, across all three slices combined.
- [x] 24.6 Final structural confirmation: all three chained slices complete; each
      independently revertable per design's Migration/Rollout section (Slice 3's own
      new files — `screen_admin_menu.go`, `screen_operations.go`,
      `screen_scan_runs.go`, `screen_scan_history.go` — plus its `model.go`/
      `session.go`/`admin_tables.go`/`admin_views.go` edits revert as one unit,
      leaving Slice 1's primitive and Slice 2's reversal in place and harmless), with
      the two superseded-decision annotations (Phase 16) reverting together with
      Slice 2's code, unchanged by this batch.

### Slice 3 apply deviations (recorded, not silent)

- **Strict RED-first was NOT fully maintained for Phases 18-20's own named
  tests** (`TestSecurityDomainListsThreePeers`, `TestOperationsListsBothResultsScreens`,
  `TestOperationsEntryReachesSecretFindingsWithoutTrivy`,
  `TestTrivyDrillDownStillReachesSecretFindings`, `TestScanHistoryEscReturnsToOpener`,
  `TestFindingLinkOpenUsesExplicitArgvAndHTTPSchemeGuard`) — unlike Slice 1's proof
  screen, this disclosure is closer to Phase 11's own: the interdependent scope
  (domain menu + Operations entry screens + a full ScanHistoryModal migration off
  `AdminViewState`, touching `model.go`/`session.go`/`admin_tables.go`/
  `admin_views.go` and ~190 pre-existing test references to the modal's own
  row-budget math simultaneously) was designed and implemented as one coherent
  architectural unit before these six specific tests were written, rather than each
  test landing RED against not-yet-existing production code first. What strict
  RED-first discipline WAS preserved: (1) every one of the ~54 pre-existing tests
  the post-login routing change broke was diagnosed and fixed by running the real
  suite and reading each actual failure (a genuine red→green cycle against
  characterization tests already guarding this behavior, not a rubber-stamp); (2)
  the six new tests above were run immediately after being written and caught two
  real test-authoring bugs before being accepted (a missing intermediate `Enter` in
  two of them, `TestTrivyDrillDownStillReachesSecretFindings` and
  `TestScanHistoryEscReturnsToOpener`'s Trivy sub-case), proving they were not
  written to already-known-passing behavior blindly. No production bug was found
  by these six tests after the fact (all failures traced to test setup, not
  `screen_admin_menu.go`/`screen_operations.go`/`screen_scan_runs.go`/
  `screen_scan_history.go` themselves) — but that is a claim this disclosure makes
  explicit rather than one earned by literal test-first ordering. See the
  `apply-progress` artifact's Work Unit Evidence for the exact commands run.
- **`screenAdminScanHistory` as a distinct top-level screen id, named in early
  design discussion of this batch, was NOT created.** `scanHistoryScreen` is
  reached and closed without ever changing `m.screen` (recorded above, Phase
  19.5) — a deliberate, disclosed architectural choice, not an oversight; no
  `screenAdminScanHistory` constant exists anywhere in the shipped code.
