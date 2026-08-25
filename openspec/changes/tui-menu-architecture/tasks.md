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

- [ ] 8.1 Characterization test `TestEveryOverridePathReachableBeforeIsReachableAfter`
      (T2.0) — one case per currently-reachable path enumerated from the proposal's
      Success Criteria (Trivy override via `o` on Repository Alerts; feature-cycle to
      gitleaks/signing via Space; secret findings via Trivy's `Enter` drill-down).
      File: `internal/tui/model_test.go`. Passes immediately against Slice 1's
      still-present legacy path.
- [ ] 8.2 Confirm 8.1 GREEN against pre-reversal code:
      `go test ./internal/tui/... -run TestEveryOverridePathReachableBeforeIsReachableAfter -v`.

## Phase 9: Override Editor Primitive — Slice 2

- [ ] 9.1 RED `internal/tui/override_editor_test.go` (new):
      `TestOverrideEditorFeatureIsImmutable` (T2.2) — every key in the map, plus
      Space, asserted against `Feature()`; none changes it. Fails to compile:
      `overrideEditor` does not exist yet.
- [ ] 9.2 GREEN (compile prerequisite) `internal/tui/override_editor.go` (new):
      `overrideEditor` (`open`, `repository`, unexported `feature`, `fields
      []overrideField` built once at construction, `focus`, `exists`, `enabled`,
      `pathPrimary`/`pathSecondary`/`unsignedSelfRead`, `loading`, `err`),
      `newOverrideEditor(feature, repository)`, `Feature()`, `update(env, msg)`
      (Decision F). Per-feature field sets: trivy = `{Enabled, PathPrimary,
      PathSecondary, Clear}`; gitleaks = `{Enabled, PathPrimary, Clear}`; signing =
      `{Enabled, PathPrimary, UnsignedSelfRead, Clear}`. Delete
      `repositoryOverrideFieldFeature` from the `overrideField` enum.
- [ ] 9.3 Confirm 9.1 GREEN.

## Phase 10: Feature Cycle Deletion + Symbol-Absence Proof — Slice 2

- [ ] 10.1 RED `internal/tui/session_test.go`: `TestNoFeatureCycleSymbolsRemain`
      (T2.3) — `go/parser` scan over `internal/tui` confirming
      `repositoryOverrideFeatureCycle`/`nextRepositoryOverrideFeatureName` are absent.
      Confirm RED for the honest reason: both symbols still exist pre-deletion.
- [ ] 10.2 GREEN `internal/tui/model.go`: delete
      `repositoryOverrideFeatureCycle`/`nextRepositoryOverrideFeatureName`
      (`:2317-2333`), `updateRepositoryOverrideModalKey` (`:2258-2315`), and the
      Trivy-gated `o` branch in `updateAdminFeaturesKey` (`:1941-1958`).
- [ ] 10.3 GREEN `internal/tui/session.go`: delete `repositoryOverrideModal`
      (`:352-364`), `nextRepositoryOverrideField` (`:378-390`).
- [ ] 10.4 Confirm 10.1 (T2.3) GREEN.

## Phase 11: Security & Compliance Domain Menu + Trivy Peer Screens — Slice 2

- [ ] 11.1 RED compile-level interface assertions for the three new screen types
      (`var _ adminScreen = securityMenuScreen{}` etc.) in the corresponding new test
      files, confirmed failing to compile before creation.
- [ ] 11.2 GREEN `internal/tui/screen_security_menu.go` (new): `securityMenuScreen` —
      `screenAdminFeatures` repurposed as the S&C domain menu; migrates `Features`,
      `SelectedFeature`, `Tables.Features` off `AdminViewState`.
- [ ] 11.3 GREEN `internal/tui/screen_trivy_config.go` (new): `trivyConfigScreen`
      (`screenSecurityTrivy`) — migrates `TrivyConfigModal`, `ScanPolicy`,
      `ScanPolicyModal`, trivy's `FeaturePage`, `Tables.FeatureRows["trivy"]`; delete
      `updateTrivyConfigModalKey`, `updateScanPolicyModalKey`.
- [ ] 11.4 GREEN `internal/tui/screen_trivy_repos.go` (new): `trivyReposScreen`
      (`screenSecurityTrivyRepos`) — migrates `TrivySummaries`, `TrivyScanRuns`,
      `TrivySelectedAlert`, `TrivyAlertsLoaded`, `TrivyOverrides`,
      `Tables.ScanSummary`, `RepositoryOverrideModal` → `editor overrideEditor`
      (feature fixed = trivy); `TrivyTab` becomes screen identity, `Tab` switches
      `trivyConfigScreen` ↔ `trivyReposScreen` (Decision I, confirmed); delete
      `TrivyTab` type + consts (`session.go:69-74`).
- [ ] 11.5 GREEN `internal/tui/model.go`: add `screenSecurityTrivy`/
      `screenSecurityTrivyRepos` consts + slots; wire into `adminScreens`.
- [ ] 11.6 Confirm no regression: re-run the Identity & Access / Browse subset of T1.8
      (Phase 1.1) — still byte-identical.

## Phase 12: Gitleaks + Signing Peer Screens — Slice 2

- [ ] 12.1 RED `internal/tui/screen_gitleaks_repos_test.go` (new):
      `TestFeatureOverrideRowsAreCatalogUnionStoredOverrides` (T2.7), incl. the
      empty-catalog case. Fails to compile: `featureOverridesScreen`/
      `featureOverrideRow` do not exist yet.
- [ ] 12.2 GREEN (compile prerequisite) `internal/tui/screen_gitleaks_repos.go` (new):
      `featureOverridesScreen` shared type (`id`, `feature`, `rows
      []featureOverrideRow`, `selected`, `table`, `loaded`, `editor overrideEditor`,
      `err`), `featureOverrideRow{Repository, HasOverride, Detail}`; load command =
      `ListRepositoryOverrides(ctx, session, feature)` merged against
      `env.KnownRepositories` (catalog ∪ stored overrides, Decision J); empty catalog
      → stored-override rows plus an explicit empty state.
- [ ] 12.3 GREEN `internal/tui/screen_signing_config.go` (new): `signingConfigScreen`
      — migrates `SigningPolicy`, `SigningPolicyModal`; delete
      `updateSigningPolicyModalKey`.
- [ ] 12.4 GREEN `internal/tui/screen_signing_repos.go` (new): `signingReposScreen`,
      backed by `featureOverridesScreen(feature="signing")`.
- [ ] 12.5 GREEN `internal/tui/model.go`: instantiate
      `featureOverridesScreen(feature="gitleaks")` for `screenSecurityGitleaksRepos`;
      add `screenSecurityGitleaksRepos`/`screenSecuritySigningConfig`/
      `screenSecuritySigningRepos` consts + slots; delete the `updateGitleaks*`
      handlers now moved into sub-models.
- [ ] 12.6 Confirm 12.1 (T2.7) GREEN.

## Phase 13: Discoverable Per-Feature Override Entry Points — Slice 2 (the reported defect fix)

- [ ] 13.1 RED `internal/tui/model_test.go`: `TestGitleaksOverrideOpensWithoutEnteringTrivy`
      (T2.1a) — the key sequence from Gitleaks' own screen never touches a Trivy
      screen id.
- [ ] 13.2 RED `internal/tui/model_test.go`: `TestSigningOverrideOpensWithoutEnteringTrivy`
      (T2.1b) — same for Signing.
- [ ] 13.3 RED `internal/tui/model_test.go`: `TestOverrideKeyIsInertWithoutAHighlightedRow`
      (T2.5) — `o` on a screen with no repository row highlighted does not open the
      modal.
- [ ] 13.4 RED `internal/tui/admin_views_test.go`: `TestOverrideModalStaysWithinViewport`
      (T2.6, minimum viable height) — preserved MODIFIED-requirement scenario.
- [ ] 13.5 GREEN: wire `o` on `trivyReposScreen`/gitleaks repos screen/
      `signingReposScreen` to open `overrideEditor` bound to the highlighted row and
      that screen's fixed feature (`newOverrideEditor(feature, repository)`).
- [ ] 13.6 GREEN `internal/tui/admin_views.go`: delete `adminFeatureHelp`
      (`:980-1002`) — the defect's own artifact; per-screen help now comes from each
      screen's `Keys()`.
- [ ] 13.7 GREEN `internal/tui/admin_tables.go`: table builders take explicit rows
      instead of reading `AdminViewState`.
- [ ] 13.8 Confirm 13.1–13.4 GREEN:
      `go test ./internal/tui/... -run 'TestGitleaksOverrideOpensWithoutEnteringTrivy|TestSigningOverrideOpensWithoutEnteringTrivy|TestOverrideKeyIsInertWithoutAHighlightedRow|TestOverrideModalStaysWithinViewport' -v`.

## Phase 14: Field Migration Closure Proof — Slice 2

- [ ] 14.1 RED `internal/tui/session_test.go`: `TestMigratedScreensHaveZeroFieldsOnAdminViewState`
      (T2.4) — `reflect` field-name set vs. an explicit allowlist. Confirm RED: the
      17 fields still present pre-deletion.
- [ ] 14.2 GREEN `internal/tui/session.go`: delete the remaining migrated
      `AdminViewState` fields per design's State Migration table — `TrivyConfigModal`,
      `ScanPolicy`, `ScanPolicyModal`, trivy's `FeaturePage`,
      `Tables.FeatureRows["trivy"]`, `TrivySummaries`, `TrivyScanRuns`,
      `TrivySelectedAlert`, `TrivyAlertsLoaded`, `TrivyOverrides`,
      `Tables.ScanSummary`, `RepositoryOverrideModal`, `SigningPolicy`,
      `SigningPolicyModal`, `Features`, `SelectedFeature`, `Tables.Features`.
      `ScanHistoryModal` stays (migrates in Slice 3).
- [ ] 14.3 Confirm 14.1 (T2.4) GREEN.

## Phase 15: Reachability Re-Proof — Slice 2

- [ ] 15.1 Re-assert T2.0 (Phase 8.1) per-path against the reversed code — each
      previously-reachable path (now reached via its own peer screen) is still
      reachable; update the test's per-path key sequences to the new screens while
      preserving the "reachable" invariant per path, per the proposal's mitigation
      ("asserted per path, not summarized").
- [ ] 15.2 Confirm 15.1 GREEN.

## Phase 16: Superseded-Decision Annotations — Slice 2

- [ ] 16.1 Edit `openspec/changes/repository-scan-config-overrides/design.md`
      Decision 8: add a "Superseded by `tui-menu-architecture`" annotation naming
      this change, without deleting the original decision text.
- [ ] 16.2 Edit `openspec/changes/image-signing/design.md` Decision 11: same
      annotation.

## Phase 17: Slice 2 Verification Gate — hard structural gate, no Slice 3 task begins until this passes

- [ ] 17.1 Full suite: `go build ./... && go vet ./... && gofmt -l . && go test -count=1 ./...`
      — all clean.
- [ ] 17.2 Explicit checklist against `operator-admin-tui` spec scenarios: Gitleaks
      override opens without entering Trivy; Gitleaks override set/clear round-trip;
      Signing override opens without entering Trivy; Signing override set/clear
      round-trip; opening the modal on a highlighted row shows the effective config
      (both no-override and existing-override cases); operator sets/clears an
      override; the override key is scoped to the opening screen's row only; the
      modal's `Feature` cannot be changed once open; the modal stays within the
      viewport.
- [ ] 17.3 Explicit checklist against `tui-navigation-architecture` spec scenarios
      landing in Slice 2: "Migrated screen has zero fields on `AdminViewState`" for
      Trivy config/repos, Gitleaks config/repos, Signing config/repos, and the
      security domain menu.
- [ ] 17.4 Confirm proposal Success Criteria items landing in Slice 2:
      `repositoryOverrideFeatureCycle`/`nextRepositoryOverrideFeatureName` no longer
      exist and no key mutates a modal's `Feature`; every override path reachable
      before is reachable after, asserted per path; Decision 8/Decision 11 are
      annotated as superseded, naming this change.
- [ ] 17.5 **STRUCTURAL GATE**: Slice 2 is independently apply-/verify-able and
      mergeable, with the reversal complete and the two superseded design docs
      reverting together with this slice's code if ever rolled back. Do not begin
      Phase 18 until this task and 17.1–17.4 are recorded done.

## Phase 18: Domain Menu + Operations Entry Screens — Slice 3

- [ ] 18.1 RED `internal/tui/model_test.go`: `TestSecurityDomainListsThreePeers`
      (T3.2a) — Security & Compliance domain lists Trivy/Gitleaks/Signing as three
      peer entries.
- [ ] 18.2 RED `internal/tui/model_test.go`: `TestOperationsListsBothResultsScreens`
      (T3.2b) — Operations domain lists Scan Runs and Secret Scan Findings.
- [ ] 18.3 GREEN `internal/tui/screen_admin_menu.go` (new): `screenAdminMenu` —
      post-login landing, 4 domain rows (Browse / Security & Compliance / Identity &
      Access / Operations); two levels, domain rows → existing screens, per Decision
      I as confirmed by the user (not a single flat grouped list).
- [ ] 18.4 GREEN `internal/tui/screen_operations.go` (new): `screenAdminOperations`,
      2 rows (Scan Runs, Secret Scan Findings).
- [ ] 18.5 GREEN `internal/tui/model.go`: post-login routing
      `adminIntentOperator` → `screenAdminMenu` instead of `screenAdminUsers`;
      `adminIntentRepoGrants` untouched; update `screenAdminUsers`' Esc target.
- [ ] 18.6 Confirm 18.1–18.2 GREEN.

## Phase 19: Scan Runs + Secret Scan Findings Dual-Entry Sub-Model — Slice 3

- [ ] 19.1 RED `internal/tui/model_test.go`: `TestOperationsEntryReachesSecretFindingsWithoutTrivy`
      (T3.0).
- [ ] 19.2 RED `internal/tui/model_test.go`: `TestTrivyDrillDownStillReachesSecretFindings`
      (T3.1a).
- [ ] 19.3 RED `internal/tui/model_test.go`: `TestScanHistoryEscReturnsToOpener`
      (T3.1b) — both openers (Operations entry, Trivy's Enter drill-down); Esc pops
      back to the correct `returnTo`.
- [ ] 19.4 GREEN `internal/tui/screen_scan_runs.go` (new): `scanRunsScreen`
      (`ListRepositoryScanSummaries`).
- [ ] 19.5 GREEN `internal/tui/screen_scan_history.go` (new): `scanHistoryScreen`
      with a `returnTo screen` field — one sub-model reached from Operations ▸ Secret
      Scan Findings AND Trivy ▸ Repository Alerts ▸ `Enter`; carries the
      `Enter`-opens-advisory-link path (`GetSecretScanFindings`,
      `ListRepositoryScanSummaries`, `GetScanRunDetail` — no new backend calls, D9).
- [ ] 19.6 GREEN `internal/tui/admin_scan_history.go`: render helpers take the
      sub-model instead of `AdminViewState`; `ScanHistoryModal` migrates off
      `AdminViewState`.
- [ ] 19.7 GREEN `internal/tui/model.go`: wire `screenAdminOperations` rows to
      `scanRunsScreen`/`scanHistoryScreen(returnTo=screenAdminOperations)`; Trivy's
      Repository Alerts `Enter` now opens
      `scanHistoryScreen(returnTo=screenSecurityTrivyRepos)`.
- [ ] 19.8 Confirm 19.1–19.3 GREEN.

## Phase 20: Subprocess-Path Threat Proof (carried by the move) — Slice 3

- [ ] 20.1 RED `internal/tui/openurl_test.go` / `internal/tui/model_test.go`:
      `TestFindingLinkOpenUsesExplicitArgvAndHTTPSchemeGuard` (T3.3) — the findings
      `Enter` opener's `isHTTPURL` scheme guard and explicit-argv `exec.Command`
      construction (`openurl.go:18-44`), now inside `scanHistoryScreen`'s `tea.Cmd`,
      asserted unchanged: exact URL, non-`http(s)` link yields no exec at all.
- [ ] 20.2 GREEN: confirm `scanHistoryScreen` only calls `openAdminURLCmd`, and the
      `exec.Command` call stays inside a `tea.Cmd`, never in `Update`.
- [ ] 20.3 Confirm 20.1 (T3.3) GREEN.

## Phase 21: Identity & Access Naming Tidy (no state migration) — Slice 3

- [ ] 21.1 GREEN: naming/grouping tidy only for Users/Robots/Repository Grants/Tokens
      under the Identity & Access domain label in `screenAdminMenu` — zero
      behavior/state change; the legacy adapter (Decision C) is untouched, D5's
      boundary holds.
- [ ] 21.2 Confirm the Identity & Access subset of T1.8 (Phase 1.1) still
      byte-identical.

## Phase 22: Documentation — Slice 3

- [ ] 22.1 Update `docs/tui.md`: navigation map (four domains, two-level menu) and
      full per-screen key table reflecting keymap-derived help.
- [ ] 22.2 Update `docs/architecture.md`: TUI screen-ownership model (`adminScreen`
      contract, router, legacy adapter boundary, D5 phasing).
- [ ] 22.3 Update `docs/code-reference.md`: new files (`screen.go`,
      `admin_router.go`, `admin_keys.go`, `confirm.go`, `override_editor.go`, all
      `screen_*.go`) and their exported symbols.
- [ ] 22.4 Update `docs/roadmap.md`: mark `tui-menu-architecture` delivered across
      its three slices; note the feature-cycle removal and the new per-feature
      override entry points.

## Phase 23: Snapshot Regeneration — Slice 3

- [ ] 23.1 Regenerate `docs/verification/scripts/tui-smoke.sh` snapshots (T3.4) —
      deliberate, reviewed as intended output, not silently accepted; diff limited to
      the navigation/help/layout changes introduced across all three slices.
- [ ] 23.2 Confirm regenerated snapshots reviewed and committed.

## Phase 24: Slice 3 Verification Gate — final gate

- [ ] 24.1 Full suite: `go build ./... && go vet ./... && gofmt -l . && go test -count=1 ./...`
      — all clean.
- [ ] 24.2 Explicit checklist against `operator-admin-tui` spec scenarios landing in
      Slice 3: Operations entry reaches secret findings directly; Trivy drill-down
      still reaches secret findings.
- [ ] 24.3 Explicit checklist against `tui-navigation-architecture` spec scenarios
      landing in Slice 3: Security & Compliance lists three peer screens; Operations
      lists both results screens.
- [ ] 24.4 Confirm remaining proposal Success Criteria: secret scan findings
      reachable from an Operations entry point while the Trivy drill-down still
      works; `go test ./...`, `go vet ./...`, `go build ./...` pass; `tui-smoke.sh`
      snapshots regenerated and reviewed; no diff outside `internal/tui/`,
      `go.mod`/`go.sum`, `openspec/`, and `docs/`.
- [ ] 24.5 Final `reflect`-based check: zero remaining `AdminViewState` fields for
      every screen in design's full State Migration table, across all three slices
      combined.
- [ ] 24.6 Final structural confirmation: all three chained slices complete; each
      independently revertable per design's Migration/Rollout section, with the two
      superseded-decision annotations reverting together with Slice 2's code.
