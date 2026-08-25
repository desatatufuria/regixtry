# Design: TUI Menu Architecture — Per-Screen Ownership and Domain-Grouped Navigation

## Technical Approach

Nested Bubble Tea composition, delivered in three chained slices (user-confirmed),
with the primitive proven on one real Security & Compliance surface before the
reversal lands:

- **Contract** — `adminScreen`, a value-typed interface (`Update` returns a new
  `adminScreen`, never a pointer), plus `screenKeys` (a `help.KeyMap`) and
  `screenFrame` (context/body/overlay — **deliberately no `Help` field**, so a
  sub-model *cannot* return a hand-written footer).
- **Router** — `Model.adminScreens`, a fixed-size **array** of `adminScreen`
  interface values, and a package-level, stateless `legacyScreenHandlers` table
  for the screens D5 leaves on `AdminViewState`. One lookup, two arms, boundary
  visible in review.
- **Reversal** — `screenAdminFeatures` becomes the Security & Compliance domain
  menu (it already lists exactly `builtInFeatures` = trivy/gitleaks/signing —
  `feature_registry.go:32-36`); each feature gains a config screen and its own
  repository-override list screen. `repositoryOverrideModal.Feature` becomes an
  unexported, constructor-only field of `overrideEditor`.
- **Operations** — one shared scan-history sub-model with a `returnTo` screen, so
  the new Operations entry point and Trivy's existing `Enter` drill-down are the
  same code reached two ways (D9 requires both to work).

All twelve proposal decisions (D1–D12) and the user's confirmed delivery choices
are binding inputs here, not open questions.

## Architecture Decisions

### Decision A: the sub-model contract is a value-typed interface, and `screenFrame` has no `Help` field

| Option | Tradeoff | Decision |
|---|---|---|
| `adminScreen` interface, value receivers, `Update` returns `adminScreen` | Parent holds only a boxed value; `Update`'s third return says whether the key was consumed, preserving today's exact global-key precedence | **Chosen** |
| `tea.Model` verbatim (`Update(tea.Msg) (tea.Model, tea.Cmd)`) | No `env` for `AdminClient`/`AdminSession`, so every screen must cache a session copy that goes stale on re-login or expiry; no consumed flag, so the `l`/`q` fallbacks at `model.go:1575-1585` cannot be reproduced | Rejected |
| Pointer receivers (`*trivyConfigScreen`) | Breaks Bubble Tea's value-copy discipline that this whole `Model` is built on; two copies of `Model` would alias one screen | Rejected |

The missing `Help` field is load-bearing: the router renders the footer from
`s.Keys()` and there is no other path for a footer to reach the frame, which makes
the spec's "help cannot drift" requirement structural rather than reviewed.

### Decision B: the parent's only handle on a child is an interface value held in an **array**

`Model.adminScreens` is `[numScreenSlots]adminScreen` — an array, not a slice or map.

| Option | Tradeoff | Decision |
|---|---|---|
| Array of interface values, dense `screenSlot` index | Arrays are values: copying `Model` copies the screen set, so value semantics hold. Interface-typed elements make `m.adminScreens[slot].cursor = 1` a compile error — you cannot address a field through an interface | **Chosen** |
| `map[screen]adminScreen` | Maps are reference types: a "discarded" `Model` copy would observe the mutation, silently breaking the "parent replaces its held value" property the spec asserts. Same defect for a slice (shared backing array) | Rejected |
| Named struct fields of concrete types (`trivy trivyConfigScreen`) | Value semantics fine, but `m.router.trivy.enabled = true` compiles from anywhere in package `tui` — the exact bleed this change exists to stop | Rejected |
| Child package `internal/tui/screens` with unexported fields | Strongest boundary, but requires exporting `adminTheme`, `consoleLayout`, ~40 render helpers and every `is*Key` predicate. Correct endgame after all 23 screens migrate; unaffordable inside this change's budget | Rejected (recorded as follow-up) |

Testable, not merely asserted: `TestAdminScreenSetHoldsOnlyInterfaceValues` uses
`reflect.TypeOf(Model{}.adminScreens).Elem().Kind() == reflect.Interface`.

### Decision C: the `AdminViewState` adapter is a package-level function table with zero data

```go
type legacyHandler func(Model, tea.KeyMsg) (tea.Model, tea.Cmd)

// legacyScreenHandlers is THE adapter (D5). It holds no state of its own —
// AdminViewState stays exactly where it is on Model, and every value here is a
// Go method expression on an existing, unmodified handler. It cannot become a
// god-object because it has no fields: adding a screen here adds one line, and
// removing one is how a screen graduates to a sub-model.
var legacyScreenHandlers = map[screen]legacyHandler{
	screenAdminUsers:        Model.updateAdminUsersKey,
	screenAdminEditUser:     Model.updateAdminEditUserKey,
	// … Identity & Access + Browse-adjacent admin screens, unchanged …
}
```

The router resolves a screen id in exactly one place: migrated slot first, then
this table. Two guard tests keep the boundary honest —
`TestEveryScreenRoutesExactlyOnce` (union covers all `screen` constants, no
overlap) and `TestLegacyAdapterHoldsNoState` (`legacyHandler`'s underlying kind is
`reflect.Func`, and no adapter type declares a data field).

### Decision D: `charmbracelet/bubbles` is promoted at **v0.11.0** with no version bump, and no `go.sum` change

**Verification method** (no network and no `go` toolchain available to this
executor — stated plainly rather than implied):

1. `go.sum:5` carries `github.com/charmbracelet/bubbles v0.11.0 h1:…`. Go records
   an `h1:` module hash only for modules whose **source is required to build**, so
   bubbles v0.11.0 is already compiled into this binary today, against
   `bubbletea v1.3.10` and `lipgloss v1.1.0` (`go.mod:6-7`), which MVS selects over
   bubbles' own `bubbletea v0.21.0` / `lipgloss v0.5.0` requirements (`go.sum:7,13`).
2. `admin_tables.go:109-116` already calls `bubbletable.DefaultKeyMap()` and
   `keys.RowSelectToggle.SetKeys("ctrl+space")`. Those fields are
   `bubbles/key.Binding` values and `SetKeys` is its method — so **`bubbles/key`
   compiles and runs at v0.11.0 in this exact build graph**, proven from source in
   this repository, not assumed.
3. `bubbles/help` is the residual unknown: it is a separate package that may not be
   in today's build, and I cannot compile it here.

**Naming correction carried into the spec's intent**: `bubbles` has never exported a
type called `key.Map`. The canonical pairing is a project-defined struct of
`key.Binding` fields satisfying the `help.KeyMap` interface (`ShortHelp() []key.Binding`,
`FullHelp() [][]key.Binding`). `screenKeys` below is that struct. Wherever the
proposal and spec say "`bubbles/key.Map`", read "a `key.Binding` set implementing
`help.KeyMap`" — the requirement is unchanged.

`go.mod` change (move line 20 out of the indirect block; **`go.sum` needs no edit**,
which is the whole point of the promotion):

```
 require (
+	github.com/charmbracelet/bubbles v0.11.0
 	github.com/charmbracelet/bubbletea v1.3.10
 	github.com/charmbracelet/lipgloss v1.1.0
```

**Fallback ladder, decided now so no implementer improvises.** The gate is the
Slice 1 byte-identity test (T1.3): the generated footer for the proof screen must
equal today's hand-written `"Enter: save | Tab: next field | Space: toggle | Esc: cancel"`
(`admin_views.go:789`) exactly.

| Rung | Trigger | Action |
|---|---|---|
| 1 | `bubbles/help` compiles and T1.3 passes with `ShortSeparator = " | "` and no-op `Styles.ShortKey/ShortDesc` (the surrounding `theme.muted`/`renderConsoleWorkspace` keeps doing the styling) | Ship as-is |
| 2 | `bubbles/help` does not compile at v0.11.0 against bubbletea v1.3.10 | Bump to `bubbles v0.21.0` (its `go.mod` targets bubbletea v1.3.x + lipgloss v1.1.0). **Re-run `evertras/bubble-table` v0.19.2's consumers** — `admin_tables_test.go` and `viewport_test.go`'s `tableChromeRows` assertions are the tripwire |
| 3 | Rung 2 breaks bubble-table, or T1.3 cannot be made byte-identical | Keep bubbles at v0.11.0 for `key` only; render the footer with an in-repo `shortHelpView(keys screenKeys) string` (~30 lines, `admin_keys.go`) |

Rung 3 still satisfies every spec scenario: the anti-drift property lives in
"the footer is generated from `Keys()`", not in whose code generates it.

### Decision E: the confirm primitive replaces the `Kind` union with a **closure**, and the closure takes `env`

```go
// confirmPrompt is THE confirm-before-destructive-action primitive (D7). The
// payload adminConfirmModal carried as sibling fields (UserID/Username/
// FeatureName/Repository/Accessor) is captured by onConfirm instead, so adding a
// confirm never widens a shared struct. It is a plain composable value: a
// migrated sub-model and a legacy model (TagsModel) embed it identically.
type confirmPrompt struct {
	title       string
	message     string
	confirmText string
	// onConfirm receives env at CONFIRM time, never at OPEN time: capturing
	// AdminSession in the closure would fire a command with a token that expired
	// or was replaced while the prompt was on screen.
	onConfirm func(env screenEnv) tea.Cmd
}

func newConfirmPrompt(title, message, confirmText string, onConfirm func(screenEnv) tea.Cmd) confirmPrompt
func (c confirmPrompt) Active() bool { return c.onConfirm != nil }
// update returns (next, cmd, consumed). While Active, consumed is true for EVERY
// key — reproducing updateAdminConfirmKey's existing swallow-all behavior exactly.
func (c confirmPrompt) update(env screenEnv, msg tea.KeyMsg) (confirmPrompt, tea.Cmd, bool)
func (c confirmPrompt) view(theme adminTheme) string
```

Rejected: a narrower `Kind` union (still one struct that must know every screen's
payload); an `interface{ Run() tea.Cmd }` payload per confirm (10 new types for 10
call sites, ceremony with no added safety).

Retirement map — 10 `adminConfirmKind` values collapse to 7 openers, each passing
its own closure: `model.go:1744,1760` (enable/disable user), `:1998` (feature
enable/disable), `:2573` (delete grant), `:2635` (revoke token), `:2689` (delete
repo grant), `:2934` (robot enable/disable/delete). `updateAdminConfirmKey`
(`:2704-2746`) and its `switch modal.Kind` disappear entirely.

### Decision F: `overrideEditor` fixes `Feature` at construction and precomputes its field order

```go
// overrideEditor replaces repositoryOverrideModal. feature is unexported, set
// only by newOverrideEditor, and read only through Feature(). There is no
// setter and no key path that writes it: the spec's "no key changes which
// feature the modal edits" is a property of the type, not of a code review.
type overrideEditor struct {
	open        bool
	repository  string
	feature     string
	fields      []overrideField // ordered, per-feature, built ONCE at construction
	focus       int             // index into fields; Tab is (focus+1) % len(fields)
	exists      bool
	enabled     bool
	pathPrimary, pathSecondary, unsignedSelfRead string
	loading     bool
	err         string
}

func newOverrideEditor(feature, repository string) overrideEditor
func (e overrideEditor) Feature() string
func (e overrideEditor) update(env screenEnv, msg tea.KeyMsg) (overrideEditor, tea.Cmd, bool)
```

`fields` is the concrete replacement for `nextRepositoryOverrideField(field, feature)`'s
run-time feature-conditional skipping (`session.go:378-390`): trivy →
`{Enabled, PathPrimary, PathSecondary, Clear}`; gitleaks → `{Enabled, PathPrimary, Clear}`;
signing → `{Enabled, PathPrimary, UnsignedSelfRead, Clear}`. `repositoryOverrideFieldFeature`
is deleted from the `overrideField` enum, so a Feature row is unrepresentable rather than
merely unreachable. `repositoryOverrideFeatureCycle` and
`nextRepositoryOverrideFeatureName` (`model.go:2317-2333`) are deleted.

### Decision G: Slice 1's proof screen is the **Gitleaks config modal**

| Candidate | Why not |
|---|---|
| `screenAdminCreateToken` | Reads `SelectedUserID`/`SelectedUsername`, state owned by a screen that stays legacy — the proof would immediately need a cross-screen escape hatch |
| `screenAdminCreateRobot` | Self-contained, but it is **Identity & Access**: migrating it violates D5's boundary and the spec's regression-guard requirement |
| `screenAdminChangePassword`, `screenRepoAdminAddGrant` | Same cross-screen-selection problem as Create Token |
| **Gitleaks config modal** → `gitleaksConfigScreen` | **Chosen** |

Why it is the right one: it is the smallest Security & Compliance surface (3 fields,
`session.go:201-208`; 6 key branches, `model.go:2057-2092`; one render function,
`admin_views.go:779-791`; one inbound message, `adminFeatureConfiguredMsg`); its state
is 100% its own on `AdminViewState.GitleaksConfigModal`, so nothing else has to move;
it sits in the domain Slice 2 restructures, so the work is not throwaway; and
`admin_views.go:789` is a **hand-written help line** — the exact drift class this change
exists to kill, which makes the keymap-derived-help proof real rather than a toy.
It stays opened by `s` on the gitleaks feature row, renders identically, and swallows
every key while open — zero user-visible change, byte-proven by T1.3.

The confirm primitive cannot be proven on a config modal (nothing destructive), so
Slice 1 proves it where it already exists: `TagsModel.PendingDelete` → `confirmPrompt`
(no sub-model migration needed — that is the point of a composable value) and the
`adminConfirmModal` retirement.

### Decision H: key messages go to the active screen; non-key messages are broadcast

Today every async message is handled centrally and writes `AdminViewState`
regardless of which screen is active (`model.go:612+`). Routing async results only
to the active screen would silently change behavior when an operator navigates away
mid-load. So: `tea.KeyMsg` → active screen only (after overlay/confirm precedence);
every other `tea.Msg` → every occupied slot in `adminScreens`, each ignoring what it
does not own via a type switch. Cost is 8 no-op type switches per message; the gain
is that Slice 1 and 2 are behaviour-preserving by construction.

### Decision I: each navigation domain maps onto an **existing** screen wherever one exists

| Domain | Target | New? |
|---|---|---|
| Browse | `screenRepositories` (leaves admin) | no |
| Security & Compliance | `screenAdminFeatures`, repurposed as the domain menu — it already lists exactly trivy/gitleaks/signing | no |
| Identity & Access | `screenAdminUsers` (unchanged, keeps its own `b: robots`) | no |
| Operations | `screenAdminOperations` | **yes** (2 rows) |

Only two genuinely new menu screens ship: `screenAdminMenu` (post-login landing, 4
domain rows) and `screenAdminOperations`. Post-login routing changes
`adminIntentOperator` → `screenAdminMenu` instead of `screenAdminUsers`;
`adminIntentRepoGrants` is untouched.

Trivy's `Tab` keeps working: `trivyTabRuntime`/`trivyTabRepositoryAlerts` stop being a
field and become two screen ids, with `Tab` asking the router to switch between them.
`renderTrivyTabs` is retained as a header on both, so the pair still looks like tabs.
Gitleaks and Signing get the same `Tab` affordance for free.

### Decision J: Gitleaks'/Signing's repository rows are `catalog ∪ stored overrides`

Trivy's list is scan-driven (`ListRepositoryScanSummaries`). Gitleaks and Signing have
no scan-summary surface, and `ListRepositoryOverrides(ctx, session, feature)`
(`admin_client.go:75-77`) returns only repositories that **already** have an override —
which alone would make "add an override to a repository that has none" impossible.
The row set is therefore the union of the already-loaded Console catalog
(`m.repositories.Names()`, already threaded to admin renders as `knownRepositories`)
and the stored override rows, with one status column (`override active` /
`inheriting global settings`). **Zero new `AdminClient` methods, zero backend change**
(D3, D12 hold). If the catalog is empty the screen shows only stored-override rows plus
an explicit empty state naming why.

## Data Flow

    tea.KeyMsg
      updateAdminKey
        ├─ active screen's overlay/confirm? ── consumed ──→ done   [precedence preserved]
        ├─ 'l' logout / 'q' quit fallbacks                          [model.go:1575-1585]
        └─ routeAdminKey(m.screen)
              ├─ slotFor(id) ok ─→ adminScreens[slot].Update(env, msg) ─→ (adminScreen, cmd, consumed)
              │                       └─ parent REPLACES the slot; it cannot write into it
              └─ legacyScreenHandlers[id] ─→ Model.updateAdminXKey(m, msg)   [AdminViewState, unchanged]

    any other tea.Msg ──→ broadcast to every occupied slot ──→ each ignores what it does not own

    View()
      renderAdminWorkspace
        ├─ slotFor(id) ok ─→ screenFrame{Context, Body, Overlay}
        │                     help := shortHelpView(theme, s.Keys())   ← ONLY footer source
        └─ legacy id      ─→ renderAdminScreen(...) (context, body, help)  [unchanged]

    Secret findings, two entries, one sub-model (D9):
      Operations ▸ Secret Scan Findings ──┐
                                          ├─→ scanHistoryScreen{returnTo: <caller>}
      Trivy ▸ Repository Alerts ▸ Enter ──┘        Esc pops back to returnTo

## Interfaces / Contracts

### `internal/tui/screen.go` (new)

```go
// screenEnv is everything a sub-model may READ from the outside world. It is
// passed per call, never stored on a screen: a cached AdminSession goes stale on
// re-login or expiry, and a cached consoleLayout goes stale on resize.
type screenEnv struct {
	Client            AdminClient
	Session           AdminSession
	Layout            consoleLayout
	KnownRepositories []string
	Now               func() time.Time
}

// screenFrame is a sub-model's rendered output. There is deliberately NO Help
// field: the footer is generated by the router from Keys(), which is what makes
// keymap/help drift unrepresentable rather than merely discouraged.
type screenFrame struct {
	Context string
	Body    string
	Overlay string // "" when the screen has no modal open
}

type adminScreen interface {
	ID() screen
	Keys() screenKeys
	Init(env screenEnv) tea.Cmd
	// Update returns the screen's NEW value. consumed reports whether a
	// tea.KeyMsg was handled here; false lets the router fall through to the
	// global key fallbacks, preserving today's precedence exactly.
	Update(env screenEnv, msg tea.Msg) (next adminScreen, cmd tea.Cmd, consumed bool)
	View(theme adminTheme, env screenEnv) screenFrame
}

// screenSlot indexes adminScreenSet. Dense and compile-time bounded.
type screenSlot int

const (
	slotGitleaksConfig screenSlot = iota // Slice 1
	// … Slice 2/3 slots …
	numScreenSlots
)

// adminScreenSet is an ARRAY, not a slice or map (Decision B): Model is copied by
// value on every Update, and a reference type here would let a discarded copy
// observe another copy's mutation.
type adminScreenSet [numScreenSlots]adminScreen

func slotFor(id screen) (screenSlot, bool)
func navigate(to screen) tea.Cmd // emits navigateMsg{To}; the router owns m.screen
```

### `internal/tui/admin_keys.go` (new)

```go
// screenKeys is this project's help.KeyMap implementation. bubbles exports no
// type named key.Map (Decision D); the canonical pairing is a set of
// key.Binding values satisfying help.KeyMap.
type screenKeys struct {
	short []key.Binding
	full  [][]key.Binding
}

func (k screenKeys) ShortHelp() []key.Binding   { return k.short }
func (k screenKeys) FullHelp() [][]key.Binding  { return k.full }

// matches is the ONLY key-matching path a migrated screen may use: a key not in
// the map cannot be handled, and a key in the map cannot be missing from help.
func (k screenKeys) matches(msg tea.KeyMsg, id string) bool

var gitleaksConfigKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "save")),
	key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "next field")),
	key.NewBinding(key.WithKeys(" "), key.WithHelp("Space", "toggle")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "cancel")),
}}
```

## State Migration — field by field

**Trivy (existing state → new sub-models).** Every listed `AdminViewState` field is
**deleted** from the struct in Slice 2; `TestMigratedScreensHaveZeroFieldsOnAdminViewState`
fails until it is.

| `AdminViewState` field (session.go) | New owner | New field |
|---|---|---|
| `TrivyConfigModal` (:490) | `trivyConfigScreen` (`screenSecurityTrivy`) | `cfg trivyConfigModal` |
| `ScanPolicy` (:499) | `trivyConfigScreen` | `policy ports.ScanPolicySettings` |
| `ScanPolicyModal` (:500) | `trivyConfigScreen` | `policyModal scanPolicyModal` |
| `FeaturePage` (:479), trivy's slice | `trivyConfigScreen` | `page ports.FeaturePage` |
| `Tables.FeatureRows["trivy"]` (:460) | `trivyConfigScreen` | `rows bubbletable.Model` |
| `TrivyTab` (:489) | *deleted* — becomes screen identity (Decision I) | — |
| `TrivySummaries` (:522) | `trivyReposScreen` (`screenSecurityTrivyRepos`) | `summaries []repositorySummary` |
| `TrivyScanRuns` (:511) | `trivyReposScreen` | `runs []ports.ScanRun` |
| `TrivySelectedAlert` (:512) | `trivyReposScreen` | `selected int` |
| `TrivyAlertsLoaded` (:513) | `trivyReposScreen` | `loaded bool` |
| `TrivyOverrides` (:567) | `trivyReposScreen` | `overrides []ports.RepositoryOverrideDetails` |
| `Tables.ScanSummary` (:466) | `trivyReposScreen` | `table bubbletable.Model` |
| `RepositoryOverrideModal` (:531) | `trivyReposScreen` | `editor overrideEditor` (feature fixed = `trivy`) |
| `ScanHistoryModal` (:526) | `scanHistoryScreen` (Operations, Slice 3) | whole struct + `returnTo screen` |
| `GitleaksConfigModal` (:494) | `gitleaksConfigScreen` (**Slice 1**) | `cfg gitleaksConfigModal` |
| `FeaturePage` (:479), gitleaks' slice | `gitleaksConfigScreen` | `page ports.FeaturePage` |
| `SigningPolicy` (:505), `SigningPolicyModal` (:506) | `signingConfigScreen` | `policy`, `policyModal` |
| `FeaturePage` (:479), signing's slice | `signingConfigScreen` | `page ports.FeaturePage` |
| `Features`, `SelectedFeature`, `Tables.Features` | `securityMenuScreen` (`screenAdminFeatures`, repurposed) | `features`, `selected`, `table` |

**Resolved gap (addendum, post-Slice-2-batch-1):** `AdminViewState.FeaturePage` was a
single field shared across all three features today (set generically by
`loadAdminFeaturePageCmd(m.selectedFeatureName())` for whichever feature was
selected in the flat list). The rows above were originally written only for
Trivy's slice, leaving Gitleaks'/Signing's ownership unassigned — flagged and
correctly stopped-on, not guessed through, by the apply agent that completed
Phase 12.3. Resolution, consistent with Decision D4 (no shared cross-screen
state) and Decision F/J's existing symmetric treatment of the three features:
**each of `trivyConfigScreen`, `gitleaksConfigScreen`, and `signingConfigScreen`
independently owns its own `page ports.FeaturePage`**, loaded via
`loadAdminFeaturePageCmd` scoped to that screen's own fixed feature name — never
a shared field on `securityMenuScreen` or anywhere else. `featureActionForKey`/
`featureActionHelp` (model.go:3900,3923 — the enable/disable/install/upgrade
action dispatch, currently reading the shared field) move onto each screen's own
`Update`/`Keys`, called with that screen's own `page`. `securityMenuScreen`
itself stays exactly what its own row already says: a bare 3-row peer list
(`features`, `selected`, `table`) with no page/action state of its
own — Enter on a row navigates into that feature's own screen, which loads its
own page independently. `gitleaksConfigScreen`/`signingConfigScreen` (currently
overlays mounted on the still-legacy `screenAdminFeatures`, per Slice 1/this
batch) become properly addressable top-level screens via `slotFor` once
`securityMenuScreen` replaces `screenAdminFeatures` — this was always implied by
the `Features`/`SelectedFeature`/`Tables.Features` row above, not a new
architectural change.

**Gitleaks / Signing override screens (new state, modeled on `trivyReposScreen`).**

```go
// featureOverridesScreen backs screenSecurityGitleaksRepos and
// screenSecuritySigningRepos. They are two DEDICATED screens (confirmed D3), not
// one parameterized picker: feature is fixed at construction, exactly as in
// overrideEditor, so neither screen can be navigated into the other's feature.
type featureOverridesScreen struct {
	id         screen
	feature    string // "gitleaks" | "signing" — constructor-only, no setter
	rows       []featureOverrideRow // catalog ∪ stored overrides (Decision J)
	selected   int
	table      bubbletable.Model
	loaded     bool
	editor     overrideEditor
	err        string
}

type featureOverrideRow struct {
	Repository string
	HasOverride bool
	Detail      ports.RepositoryOverrideDetails // zero when !HasOverride
}
```

Its load command is `ListRepositoryOverrides(ctx, session, s.feature)` — the same
already-generic call Trivy makes — merged against `env.KnownRepositories`. `o` on a
highlighted row calls `newOverrideEditor(s.feature, row.Repository)` and
`GetRepositoryOverride`; set/clear reuse `SetRepositoryOverride`/`ClearRepositoryOverride`
verbatim.

## File Changes

### Slice 1 — primitive, zero user-visible change

| File | Action | Description |
|---|---|---|
| `internal/tui/screen.go` | Create | `screenEnv`, `screenFrame`, `adminScreen`, `screenSlot`, `adminScreenSet`, `slotFor`, `navigate`/`navigateMsg` |
| `internal/tui/admin_router.go` | Create | `routeAdminKey`, `routeAdminMsg`, `legacyScreenHandlers` (the D5 adapter), `renderRoutedFrame` |
| `internal/tui/admin_keys.go` | Create | `screenKeys` (+`help.KeyMap`), `shortHelpView`, `gitleaksConfigKeys` |
| `internal/tui/confirm.go` | Create | `confirmPrompt` + `newConfirmPrompt`/`update`/`view` |
| `internal/tui/screen_gitleaks_config.go` | Create | `gitleaksConfigScreen`, the proof sub-model (moved from `model.go:2057-2092` + `admin_views.go:779-791`) |
| `internal/tui/model.go` | Modify | `Model.adminScreens adminScreenSet`; `updateAdminKey` routes through the router; delete `updateGitleaksConfigModalKey`, `updateAdminConfirmKey` (:2704-2746); 7 confirm openers pass closures; `TagsModel.PendingDelete` → `Confirm confirmPrompt`; `updateKey`'s two Tags branches (:1462-1468) delegate |
| `internal/tui/session.go` | Modify | Delete `adminConfirmModal` (:157-167), `adminConfirmKind` (+10 consts, :86-99), `AdminViewState.ConfirmModal`, `GitleaksConfigModal` |
| `internal/tui/admin_views.go` | Modify | Delete `renderAdminModal` (:750); `renderAdminWorkspace` composites `frame.Overlay`; `adminScreenHelp` gains the generated-footer arm |
| `go.mod` | Modify | `bubbles v0.11.0` indirect → direct (**no `go.sum` change**) |

### Slice 2 — the reversal (the only user-visible behavior change)

| File | Action | Description |
|---|---|---|
| `internal/tui/screen_security_menu.go` | Create | `securityMenuScreen` — `screenAdminFeatures` repurposed as the S&C domain menu |
| `internal/tui/screen_trivy_config.go`, `screen_trivy_repos.go` | Create | Trivy's two sub-models; `Tab` becomes a screen switch |
| `internal/tui/screen_gitleaks_repos.go`, `screen_signing_config.go`, `screen_signing_repos.go` | Create | Gitleaks/Signing peers; `featureOverridesScreen` shared type |
| `internal/tui/override_editor.go` | Create | `overrideEditor` (Decision F) |
| `internal/tui/session.go` | Modify | Delete `repositoryOverrideModal` (:352-364), `nextRepositoryOverrideField` (:378-390), `repositoryOverrideFieldFeature`, `TrivyTab` type + consts (:69-74), and all 17 migrated `AdminViewState` fields |
| `internal/tui/model.go` | Modify | Delete `updateAdminFeaturesKey`'s Trivy-gated `o` branch (:1941-1958), `repositoryOverrideFeatureCycle`/`nextRepositoryOverrideFeatureName` (:2317-2333), `updateRepositoryOverrideModalKey` (:2258-2315), `updateTrivyConfigModalKey`, `updateScanPolicyModalKey`, `updateSigningPolicyModalKey`, `updateGitleaks*`; add 6 screen consts + slots |
| `internal/tui/admin_views.go` | Modify | Delete `adminFeatureHelp` (:980-1002) — the defect's own artifact; per-screen renders move into sub-models |
| `internal/tui/admin_tables.go` | Modify | Table builders take explicit rows instead of reading `AdminViewState` |
| `openspec/changes/repository-scan-config-overrides/design.md` | Modify | Decision 8 annotated superseded by `tui-menu-architecture` |
| `openspec/changes/image-signing/design.md` | Modify | Decision 11 annotated superseded by `tui-menu-architecture` |

### Slice 3 — Operations, domain menu, docs

| File | Action | Description |
|---|---|---|
| `internal/tui/screen_admin_menu.go` | Create | `screenAdminMenu`, 4 domain rows, post-login landing |
| `internal/tui/screen_operations.go` | Create | `screenAdminOperations` (2 rows) |
| `internal/tui/screen_scan_runs.go` | Create | `scanRunsScreen` (`ListRepositoryScanSummaries`) |
| `internal/tui/screen_scan_history.go` | Create | `scanHistoryScreen` with `returnTo` — the one sub-model behind both D9 entry points; carries the `Enter`-opens-advisory-link path |
| `internal/tui/admin_scan_history.go` | Modify | Render helpers take the sub-model instead of `AdminViewState` |
| `internal/tui/model.go` | Modify | Post-login routing → `screenAdminMenu`; `screenAdminUsers` Esc target |
| `docs/tui.md`, `docs/architecture.md`, `docs/code-reference.md`, `docs/roadmap.md` | Modify | Navigation map, per-screen key table, screen-ownership model, cycle-removal note |
| `docs/verification/scripts/tui-smoke.sh` snapshots | Modify | Regenerated deliberately; reviewed as intended output |

## Testing Strategy

Strict TDD (`openspec/config.yaml` `strict_tdd: true`). **The characterization tests
land RED before a single line moves** — that is the proposal's own mitigation for
"Silent capability loss", and it is what turns the spec's regression-guard scenario
from an assertion into a proof.

### Slice 1 — RED before any move

| # | Test | File | Proves |
|---|---|---|---|
| T1.0 | `TestGitleaksConfigModalCharacterization` (table: Esc, Tab×4, Space, Backspace, Enter, runes, unknown key) | `model_test.go` | Captures the modal's exact pre-move behavior, including swallow-all. Must pass unchanged after the move |
| T1.1 | `TestAdminConfirmCharacterization` — all 10 `adminConfirmKind` values, Enter and Esc, asserting the exact status text and dispatched command | `model_test.go` | Written before `confirmPrompt` exists; the only proof the union retirement changed nothing |
| T1.2 | `TestDeleteTagConfirmCharacterization` | `model_test.go` | `PendingDelete`'s Enter/Esc behavior survives the swap |
| T1.3 | `TestGeneratedFooterMatchesPreviousHandWrittenString` | `admin_views_test.go` | Generated footer == `"Enter: save | Tab: next field | Space: toggle | Esc: cancel"`, byte for byte. **This is Decision D's fallback gate** |
| T1.4 | `TestRemovingABindingRemovesItFromRenderedHelp` | `admin_keys_test.go` | Spec scenario "Removing a binding removes it from rendered help" |
| T1.5 | `TestEveryKeyHandledIsInTheKeyMapAndViceVersa` | `admin_keys_test.go` | Both directions of drift: an unmapped key is not handled, a mapped key is |
| T1.6 | `TestAdminScreenSetHoldsOnlyInterfaceValues` | `admin_router_test.go` | Decision B, via `reflect` |
| T1.7 | `TestEveryScreenRoutesExactlyOnce` + `TestLegacyAdapterHoldsNoState` | `admin_router_test.go` | Decision C; no screen unrouted, no screen double-routed, adapter carries no data |
| T1.8 | `TestNonMigratedScreensUnchanged` — golden `(context, body, help)` for all 13 legacy screens, captured **before** the router exists | `admin_views_test.go` | The spec's regression-guard scenario, proven not asserted |

### Slice 2 — RED before the reversal

| # | Test | File | Proves |
|---|---|---|---|
| T2.0 | `TestEveryOverridePathReachableBeforeIsReachableAfter` — one case per currently-reachable path, enumerated from Success Criteria | `model_test.go` | The "silent capability loss" risk, per path |
| T2.1 | `TestGitleaksOverrideOpensWithoutEnteringTrivy` / `…Signing…` | `model_test.go` | Spec ADDED requirements; the key sequence must never touch a Trivy screen id |
| T2.2 | `TestOverrideEditorFeatureIsImmutable` — every key in the map, plus Space, asserted against `Feature()` | `override_editor_test.go` | Spec scenario "The modal's Feature cannot be changed once open" |
| T2.3 | `TestNoFeatureCycleSymbolsRemain` — `go/parser` over `internal/tui` | `session_test.go` | `repositoryOverrideFeatureCycle`, `nextRepositoryOverrideFeatureName` absent |
| T2.4 | `TestMigratedScreensHaveZeroFieldsOnAdminViewState` — `reflect` field-name set vs. an explicit allowlist | `session_test.go` | Spec scenario "Migrated screen has zero fields on AdminViewState"; also fails on any *new* field added without a deliberate allowlist edit |
| T2.5 | `TestOverrideKeyIsInertWithoutAHighlightedRow` | `model_test.go` | Spec scenario "scoped to the opening screen's row only" |
| T2.6 | `TestOverrideModalStaysWithinViewport` (min viable height) | `admin_views_test.go` | Preserved MODIFIED-requirement scenario |
| T2.7 | `TestFeatureOverrideRowsAreCatalogUnionStoredOverrides` incl. empty-catalog case | `screen_gitleaks_repos_test.go` | Decision J |

### Slice 3

| # | Test | File | Proves |
|---|---|---|---|
| T3.0 | `TestOperationsEntryReachesSecretFindingsWithoutTrivy` | `model_test.go` | Spec ADDED scenario |
| T3.1 | `TestTrivyDrillDownStillReachesSecretFindings` + `TestScanHistoryEscReturnsToOpener` (both openers) | `model_test.go` | D9: both paths, and `returnTo` is honored per opener |
| T3.2 | `TestSecurityDomainListsThreePeers` / `TestOperationsListsBothResultsScreens` | `model_test.go` | Spec domain-menu scenarios |
| T3.3 | `TestFindingLinkOpenUsesExplicitArgvAndHTTPSchemeGuard` (existing `openURLInBrowser` swap) | `openurl_test.go`, `model_test.go` | Threat-matrix row below survives the move into a sub-model |
| T3.4 | Regenerate `tui-smoke.sh` snapshots | `docs/verification/` | Reviewed as intended output, not silently accepted |

## Threat Matrix

The change moves key routing and moves an existing subprocess-invoking key path into
a sub-model. No new subprocess, no VCS/PR automation, no file classification.

| Boundary | Applicability | Design response | Planned RED tests |
|---|---|---|---|
| Documentation-like paths / executable classification | N/A — no file is classified or executed | — | — |
| Git repository selection / commit state / push state / PR commands | N/A — no VCS interaction anywhere in `internal/tui` | — | — |
| **Subprocess invocation** — the findings `Enter` opener (`openurl.go:18-44`, `exec.Command` with explicit argv on `open`/`rundll32`/`xdg-open`) moves into `scanHistoryScreen` | **Applicable** | The move is mechanical: `adminFindingLink`'s `isHTTPURL` scheme guard and the explicit-argv construction are not touched, and the exec stays inside a `tea.Cmd`, never in `Update`. The sub-model may only call `openAdminURLCmd` | T3.3: swapped `openURLInBrowser` asserts the exact URL and that a non-`http(s)` link yields no exec at all |
| **Key routing / dispatch** — one id must never resolve to two handlers, and none to zero | **Applicable** | Single lookup: migrated slot, then `legacyScreenHandlers`; global `l`/`q` fallbacks only when `consumed == false` | T1.7 (exactly-once routing), T1.5 (unmapped key is not handled) |
| **Destructive-action gate** — `confirmPrompt` replaces two confirm patterns | **Applicable** | `onConfirm` is nil until explicitly set; `Active()` is `onConfirm != nil`; `env` is supplied at confirm time so no stale session token can be used | T1.1, T1.2 |

## Migration / Rollout

No persisted state, no schema, no HTTP route, no wire format (D12). Rollout is the
three chained PRs in order; each reverts independently, and reverting Slice 3 or 2
leaves Slice 1's primitive in place and inert. The two superseded-decision
annotations must revert with Slice 2's code, or the docs will claim a reversal that
no longer exists. `repository-scan-config-overrides` and `image-signing` remain
unarchived: this delta archives together with, or after, both.

## Review Budget

**Revised, and the revision is stated rather than left to drift.** The proposal
forecast 1,800–3,000 total lines. Designing it concretely moved work *earlier*: the
`adminConfirmModal` retirement (10 kinds, 7 openers, one handler, one renderer) has
to land in Slice 1, because leaving it for Slice 3 would mean two confirm patterns
coexisting across two PRs — exactly what D7 forbids.

| Slice | Estimate | Note |
|---|---|---|
| 1 — primitive + proof screen + both confirm retirements | **700–900** | Above the 800-line budget at the top of its range |
| 2 — the reversal | **800–1,000** | The only slice with user-visible behavior change |
| 3 — Operations + domain menu + docs | **600–800** | Includes regenerated snapshots (excluded from authored risk count) |
| **Total** | **2,100–2,700** | Inside the proposal's 1,800–3,000 — **no drift** |

If Slice 1's review load proves too high, the named split point is **1a** (contract,
router, adapter, keys, `gitleaksConfigScreen`, ≈450–550) and **1b** (`confirmPrompt`
+ both retirements, ≈250–350). This is offered, not taken: the user explicitly
confirmed three slices, and splitting is an orchestrator/user decision.

`Decision needed before apply: Yes` · `Chained PRs recommended: Yes` ·
`800-line budget risk: High`

## Open Questions

- **`bubbles/help` compiles at v0.11.0 against bubbletea v1.3.10** — could not be
  verified here (no toolchain, no module cache, no network). `bubbles/key` **is**
  verified from repository source (Decision D, evidence 2). The Decision D ladder
  makes this non-blocking, and T1.3 is the gate that selects the rung. This is the
  proposal's explicit "MUST verify in design" item, answered as far as this
  environment permits and closed with a pre-decided fallback rather than a guess.
- Two executor-level calls, flagged for correction rather than buried: Trivy's `Tab`
  becomes a **screen switch** rather than staying a field (Decision I), and the
  four-domain menu is **two levels** (domain rows → existing screens) rather than one
  grouped list (Decision I). Both were chosen to keep non-migrated screens' behavior
  byte-identical; a single grouped list would have changed how Identity & Access is
  entered.
