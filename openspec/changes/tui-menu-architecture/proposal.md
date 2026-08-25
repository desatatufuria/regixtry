# Proposal: TUI Menu Architecture — Per-Screen Ownership and Domain-Grouped Navigation

## Intent

Three security features share one override editor that only Trivy can open.
`repositoryOverrideModal` (`session.go:352-364`) is the sole per-repository override
editor for Trivy, Gitleaks **and** Signing, but its only opener is gated on Trivy
(`model.go:1941-1958`: `isSelectedTrivyFeature() && TrivyTab == trivyTabRepositoryAlerts && 'o'`,
hardcoded `Feature: trivyFeatureName`). Once open, Space cycles the modal's `Feature`
field through `[trivy, gitleaks, signing]` (`model.go:2322`), so Gitleaks' and Signing's
per-repository overrides are edited from inside Trivy's key-routing path. `adminFeatureHelp`
(`admin_views.go:980-1002`) advertises `o: override` only under the Trivy branch —
Gitleaks and Signing have **no discoverable entry point of their own**.

A second instance of the same bleed, not previously recorded: `newAdminScanHistoryTabs()`
is constructed at exactly one call site — `model.go:1970`, the Trivy Repository Alerts
`Enter` branch — so Gitleaks' secret findings (`adminScanHistoryTabLeaks`,
`admin_views.go:393-397`) are reachable **only** by drilling into Trivy's screen.

This is not accidental drift. It is two documented prior decisions
(`repository-scan-config-overrides/design.md` Decision 8;
`image-signing/design.md` Decision 11) that must be **explicitly reversed**, not patched.
The systemic cause is that no per-screen ownership primitive exists: all TUI state lives in
three flat god-structs — `AdminViewState` (`session.go:470-575`, 30+ sibling fields for every
admin screen), `adminTablesState` (`session.go:458-468`), and `adminConfirmModal`
(`session.go:157-167`, one `Kind`-discriminated union for every screen's confirm payload).
A screen is added via four uncoordinated edits (a `screen` const, an `Update` case, a `View`
case, new `AdminViewState` fields) with zero compiler-enforced isolation, so piggybacking on an
existing screen is always the cheapest path. It happened again in miniature in the immediately
prior change: `TagsModel.PendingDelete` (`model.go:85-91`) is now a **second, parallel**
confirm-before-destructive-action pattern next to `adminConfirmModal`. This is self-inflicted.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | **Explicitly reverse `repository-scan-config-overrides` Decision 8 and `image-signing` Decision 11.** Both are named as superseded in the design doc and in the spec deltas; the `image-signing` requirement *"Signing Is A Third Feature Cycle Option In The Override Modal"* is **REMOVED**, not amended. | Both decisions were correct at the time: only Trivy had a Repository Alerts row, so a shared entry point was the cheapest honest option. That premise is dead — three features now need the same surface. Reversing silently would leave two design docs asserting the opposite of the shipped code, which is how this drift becomes invisible again. |
| D2 | **The override action is selection-driven, uniformly.** `o` opens the override editor for **the feature whose screen the operator is on**, bound to the highlighted repository row. `repositoryOverrideFeatureCycle`, `nextRepositoryOverrideFeatureName`, and the modal's `Feature` *field* are **deleted**; `Feature` survives only as immutable context supplied by the opening screen. | A cycle inside a modal is a hidden mode: the operator must already know Gitleaks' override exists to find it. Deriving the feature from the selection makes the entry point discoverable, makes `adminFeatureHelp`'s Trivy-only hint correct-by-construction, and makes a fourth feature one more screen instead of one more cycle entry. |
| D3 | **Gitleaks and Signing each get their own repository list on their own screen**, so D2 has something to select. This surfaces data that already exists: `ListRepositoryOverrides(ctx, session, feature)` (`admin_client.go:75-77`) is already generic and already called for Trivy. **Zero new `AdminClient` methods, zero backend change.** | Not a new capability — the override rows are already stored, already fetched per feature, and already editable; they simply have no per-feature surface. If `sdd-design` finds any of this needs a new route or client method, that is a **flag to raise**, not an assumption to make. |
| D4 | **Per-screen encapsulated sub-models replace flat sibling fields.** Each migrated screen owns a small model-like struct with its own state plus `Update`/`View`, composed by a thin parent router. `AdminViewState` stops being the state bag for migrated screens. | This is the idiomatic Bubble Tea nested-composition pattern and the only structural answer to "why did the bleed happen". Field-level ownership is what turns a cross-screen mutation from the path of least resistance into a visible, deliberate act. |
| D5 | **Phased migration — this change does NOT migrate all 23 screens.** In scope: the **Security & Compliance** screens (where the violation lives) and the **Operations** results screens. **Identity & Access** (Users/Robots/Grants/Tokens) and **Browse** (Repositories→Tags→Manifest→Blobs/Uploads) keep their current state on `AdminViewState` behind an explicit adapter and migrate in a later change. | `internal/tui` is ~8,750 non-test lines plus ~10,500 test lines (`model.go` alone is 4,696). A 23-screen big-bang rewrite is unreviewable against an 800-line budget and would land the risky part and the mechanical part in the same diff. Phasing puts the primitive and the actual defect in reviewable slices and leaves the untouched screens provably untouched. |
| D6 | **Adopt `bubbles/key.Map` + `bubbles/help`**: every migrated screen declares bindings as data and renders its footer from that data. `github.com/charmbracelet/bubbles` is **already in the module graph** as an *indirect* dependency (`go.mod:20`, v0.11.0, via `evertras/bubble-table`) — this promotes it to a direct require, adding no new module to the supply chain. `sdd-design` MUST verify the exact version whose `help.Model` is compatible with `bubbletea v1.3.10` and record any bump. | Hand-written help strings can drift from real key handling with nothing to catch it — which is exactly the `o: override` defect. Deriving help from the same `key.Map` the update loop matches against makes that class of drift unrepresentable. |
| D7 | **One confirm-before-destructive-action primitive**, small and screen-attachable (embedded/composed into a screen's own sub-model), replacing **both** `adminConfirmModal`'s union struct and `TagsModel.PendingDelete`. It MUST NOT be a new shared state bag. | Two patterns for one concept is how the next divergence starts. A `Kind`-discriminated union carrying `UserID`/`Username`/`FeatureName`/`Repository`/`Accessor` is the god-struct disease at modal scale; the fix is composition, not a bigger union. |
| D8 | **Four navigation domains**, config separated from results: **Browse** (unchanged drill-down), **Security & Compliance** (Trivy, Gitleaks, Signing as three peer *config* screens), **Identity & Access** (Users, Robots, Repository Grants, Tokens — naming/grouping tidy only), **Operations** (Scan Runs, Secret Scan Findings — *results/history*). | Benchmarks agree (see below): lagazit's fixed peer panels each own one domain with no cross-panel mutation; k9s separates resource navigation from configuration; btop/htop keep config in its own screen, never nested in a content panel; Harbor splits per-project security *config* from scan *results*. "Configure the scanner" and "read what it found" are different jobs with different audiences and cadence. |
| D9 | **Secret Scan Findings gets an Operations entry point**, reusing the existing scan-history modal content and existing client calls (`GetSecretScanFindings`, `ListRepositoryScanSummaries`, `GetScanRunDetail`). No new data, no new backend call. | This is the second confirmed instance of D1's bleed (`newAdminScanHistoryTabs()` reachable only from `model.go:1970`). Fixing only the override half would leave the same defect shipping under a different name. |
| D10 | **Blob GC gets no TUI surface here.** Confirmed absent today (zero GC concepts anywhere in `internal/tui`). Success criteria instead assert that adding a screen is a *bounded, additive* act under the new pattern. | Inventing GC UI inside an information-architecture change is exactly the overbuild this proposal exists to stop. The architecture's value is demonstrated by making the future addition cheap, not by pre-building it. |
| D11 | **No behavior change to the Manifest `d`/`x` "unsupported delete" notice** (`model.go:1535-1539`), even though it makes `d` mean "really delete" on Tags and "not supported" on Manifest. Recorded as a follow-up, not fixed here. | It is a genuine UX-consistency smell, but removing or rewiring it is a product decision about delete semantics, not an IA restructure. D6 will make the inconsistency *visible* in generated help — which is the right first step. |
| D12 | **No backend or API change is expected.** `AdminClient` is already feature-agnostic for overrides (`admin_client.go:71-79`). Any discovered need for a backend change is an explicit flag to the user, not an in-flight scope expansion. | The bleed is purely TUI screen/key ownership. Keeping the backend out of the diff also keeps the review budget spent on the part that is actually wrong. |

**Correction of fact.** The exploration artifact records "25 screen constants". The actual count in
`model.go:125-158` is **23**. Sizing and phasing above use 23.

**Supply-chain scope note** (per `openspec/config.yaml` `rules.proposal`): CI-first image scanning
and registry-driven rescan behavior are **untouched**. Scan scheduling, push-triggered scans, policy
gating, signature verification, and override *resolution semantics* are unchanged. This change moves
where an operator **presses a key**, not what the registry does.

## Scope

### In Scope
- Reversal of Decision 8 / Decision 11: Trivy, Gitleaks, Signing become three peer Security &
  Compliance screens, each owning its own config editor **and** its own per-repository override
  entry point.
- Deletion of `repositoryOverrideFeatureCycle` / `nextRepositoryOverrideFeatureName` and the
  modal's mutable `Feature` field; `nextRepositoryOverrideField`'s feature-conditional skipping
  becomes per-screen field sets.
- A per-screen sub-model contract (state + `Update` + `View` + `key.Map`), plus a thin parent
  router, applied to the Security & Compliance and Operations screens.
- `bubbles/key` + `bubbles/help` adoption on migrated screens; `bubbles` promoted from indirect
  to direct in `go.mod`.
- One reusable confirm primitive; `adminConfirmModal` and `TagsModel.PendingDelete` both retired
  onto it.
- Domain regrouping of the admin menu into Browse / Security & Compliance / Identity & Access /
  Operations, with an Operations entry point for Scan Runs and Secret Scan Findings.
- An explicit adapter keeping non-migrated screens working unchanged on `AdminViewState`.
- Docs: `docs/tui.md` (navigation map, key table), `docs/architecture.md` (TUI screen-ownership
  model), `docs/code-reference.md`, `docs/roadmap.md`.

### Out of Scope
- Migrating Identity & Access and Browse screens to sub-models (later change; D5).
- Any Blob GC TUI surface (D10).
- Manifest `d`/`x` notice semantics (D11).
- Any backend, HTTP, storage, scanning, or signing behavior change (D12).
- New admin capabilities, new data, new `AdminClient` methods, new routes.
- Replacing `evertras/bubble-table` with `bubbles/list`, or replacing the hand-rolled
  `viewport.go` with `bubbles/viewport` — both are real debt, both are separate changes.
- Theme/visual redesign.

## Capabilities

### New Capabilities
- `tui-navigation-architecture`: per-screen state ownership, keymap-derived help that cannot drift
  from actual key handling, a single confirm-before-destructive-action primitive, and the
  domain-grouped navigation model.

### Modified Capabilities
- `operator-admin-tui`: the override entry point becomes per-feature and selection-driven; each
  security feature MUST expose its own discoverable override entry; secret scan findings MUST be
  reachable without entering Trivy's screen. The delta MUST **REMOVE** `image-signing`'s
  requirement *"Signing Is A Third Feature Cycle Option In The Override Modal"* and **MODIFY**
  `repository-scan-config-overrides`' *"Repository-Scoped Override Modal On The Repository Alerts
  Row"*. Both source deltas are still unarchived in their change folders, so this change's delta
  must be archived together with, or after, them.

## Approach

Introduce the per-screen primitive first, prove it on one small screen, then migrate the Security &
Compliance domain onto it — which is where the reversal lands. A screen sub-model owns its data,
selection, forms, and modals; the parent holds a screen registry and routes messages, and cannot
reach into a child's fields. Each screen declares a `key.Map`; the update loop matches against it
and the footer renders from the same value, so help and behavior share one source. The confirm
primitive is composed into a screen, carrying only what that screen needs. Non-migrated screens
keep reading `AdminViewState` through a narrow adapter, so their diff stays near zero and the
migration boundary is visible in review rather than implied.

## Benchmarks

| Source | Pattern taken | Applied as |
|---|---|---|
| **lazygit** | Fixed peer panels, each owning exactly one domain; no panel mutates another's state | D2, D4 — feature screens as peers, no cross-screen mutation |
| **k9s** | Resource navigation separated from configuration; per-view keybindings surfaced in a generated help overlay | D6, D8 |
| **btop / htop** | Configuration lives in a dedicated screen, never nested inside a content panel | D8 — config screens split from results screens |
| **Harbor** (registry peer) | Per-project security *configuration* administered separately from scan *results* | D8, D9 |
| **Bubble Tea** (upstream) | Nested model composition (`tea.Model` per component); `bubbles/key` + `bubbles/help` as the canonical keybinding/help pairing | D4, D6 |

## Affected Areas

| Area | Impact | Description |
|---|---|---|
| `internal/tui/model.go` | Modified | Remove the Trivy-gated `o` branch and the feature cycle; extract Security & Compliance + Operations routing into sub-models; parent router |
| `internal/tui/session.go` | Modified | `repositoryOverrideModal.Feature` becomes immutable context; `adminConfirmModal` retired; migrated screens' fields leave `AdminViewState` |
| `internal/tui/admin_views.go` | Modified | `adminFeatureHelp` replaced by keymap-derived help; per-screen render moves into sub-models |
| `internal/tui/admin_scan_history.go`, `admin_tables.go` | Modified | Scan-history/secret-findings content reachable from Operations; table state moves with its screen |
| `internal/tui/` (new files) | New | Screen sub-model contract, parent router, confirm primitive, per-screen keymaps |
| `internal/tui/*_test.go` | Modified | Strict TDD; ~10,500 existing test lines reference the flat structs |
| `go.mod` / `go.sum` | Modified | `charmbracelet/bubbles` indirect → direct (+ any version bump per D6) |
| `openspec/changes/repository-scan-config-overrides/design.md`, `openspec/changes/image-signing/design.md` | Modified | Decision 8 / Decision 11 marked superseded by this change |
| `docs/tui.md`, `docs/architecture.md`, `docs/code-reference.md`, `docs/roadmap.md` | Modified | Navigation map, key table, screen-ownership model, roadmap entry |
| `internal/protocol/http/`, `internal/app/`, `internal/infra/` | **Unchanged** | D12 — no backend change expected |

## Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| **Silent capability loss**: an override path reachable today stops being reachable | Med | High | Success criteria enumerate every currently-reachable override/results path; a characterization test per path lands **before** any restructuring |
| Large blast radius in `model_test.go` (5,252 lines) forces mass test rewrites that mask a behavior change | **High** | High | Migrate one screen at a time; behavior-level tests written first and kept green across the move; a test that must change is a review flag, not a chore |
| Refactor scope creep into the untouched 20 screens | High | Med | D5 phasing is a hard boundary; the adapter makes the boundary explicit and its removal a later change |
| `bubbles` v0.11.0 API incompatible with `bubbletea` v1.3.10 → unplanned dependency bump | Med | Med | D6 makes version verification an explicit design deliverable, before any implementation task |
| New nesting is a *fourth* pattern rather than a replacement, leaving four coexisting styles | Med | High | D7 retires both confirm patterns; migrated screens must have **zero** fields left on `AdminViewState` — asserted, not assumed |
| TUI snapshot smoke (`docs/verification/scripts/tui-smoke.sh`) breaks on layout/help changes | High | Low | Expected; snapshots regenerated deliberately and reviewed as intended output |
| Operator muscle memory breaks (`o` on Trivy's Repository Alerts row still works, but the cycle is gone) | High | Low | `o` keeps its meaning on Trivy; only the hidden cycle disappears. Docs note the removal; the replacement is more discoverable, not less |
| **Total work far exceeds the 800-line review budget** | **High** | Med | See below — chained slices required |

### Review budget

`Decision needed before apply: Yes` — the full scope is realistically **1,800–3,000 changed lines**
including tests. `400-line budget risk: High` (against this session's cached 800-line budget:
**High**). `sdd-tasks` MUST forecast against chained slices; the natural boundary:

1. **Slice 1 — primitive, no behavior change**: screen sub-model contract, parent router,
   `key.Map`/`help` adoption, confirm primitive, proven on one small screen. Zero user-visible change.
2. **Slice 2 — the reversal**: three peer Security & Compliance screens, per-feature override entry,
   cycle deleted, D8/D11 marked superseded. This is the slice that fixes the reported defect.
3. **Slice 3 — Operations + regrouping**: Scan Runs and Secret Scan Findings entry points, domain
   menu, Identity & Access naming tidy, docs.

Beyond size, this split isolates the only slice with user-visible behavior change (2) from the two
mechanical ones.

## Rollback Plan

`git revert` the change commits. This change touches **no** persisted state, no schema, no HTTP
route, and no wire format (D12), so revert is complete and symmetric: the TUI returns to the
Trivy-gated `o` key and the Space cycle with no data migration and no operator action. The only
non-code residue is the `go.mod` promotion of `charmbracelet/bubbles`, which reverts with the same
commit; if a version bump was needed (D6), reverting restores v0.11.0 as indirect. Because delivery
is chained (see Review budget), each slice reverts independently — reverting Slice 3 or 2 leaves
Slice 1's primitive in place and harmless. Superseded-decision annotations in the two prior
`design.md` files must be reverted together with the code, or the docs will claim a reversal that no
longer exists.

## Dependencies

- `charmbracelet/bubbles` (already present as indirect, `go.mod:20`) promoted to direct; version to
  be confirmed in design (D6).
- Existing `AdminClient` override and scan methods — consumed unchanged (D3, D12).
- `repository-scan-config-overrides` and `image-signing` remain **unarchived**; their
  `operator-admin-tui` deltas are this change's spec baseline and must archive with or after it.
- No new external dependency, no backend prerequisite.

## Delivery constraint

**This branch MUST NOT be merged to `develop` until the user has explicitly reviewed and approved
it.** This applies to the entire SDD cycle for `tui-menu-architecture` — every slice, not only this
proposal. Strict TDD (`openspec/config.yaml` `strict_tdd: true`) is mandatory for all implementation
work: characterization tests for existing reachable paths land RED before any screen moves.

## Success Criteria

- [ ] Selecting the Gitleaks feature and pressing the override key opens Gitleaks' override editor,
      bound to the highlighted repository — without ever entering Trivy's screen.
- [ ] The same holds for Signing.
- [ ] `repositoryOverrideFeatureCycle` and `nextRepositoryOverrideFeatureName` no longer exist; no
      key mutates a modal's `Feature` value anywhere in `internal/tui`.
- [ ] Every screen's rendered help footer is derived from the same `key.Map` its update loop matches
      against; a test proves a binding removed from the map disappears from the footer.
- [ ] Secret scan findings are reachable from an Operations entry point without drilling into
      Trivy's Repository Alerts row; the Trivy drill-down still works.
- [ ] Exactly one confirm-before-destructive-action primitive exists: `adminConfirmModal` and
      `TagsModel.PendingDelete` are both gone, and delete-tag plus every admin confirm behave
      identically to today (proven by tests written before the move).
- [ ] Migrated screens hold **zero** state fields on `AdminViewState`; non-migrated screens are
      byte-identical in behavior.
- [ ] Every override and results path reachable before the change is reachable after it — asserted
      per path, not summarized.
- [ ] `repository-scan-config-overrides/design.md` Decision 8 and `image-signing/design.md`
      Decision 11 are annotated as superseded, naming this change.
- [ ] `go test ./...`, `go vet ./...`, `go build ./...` pass; `tui-smoke.sh` snapshots regenerated
      and reviewed as intended.
- [ ] No diff outside `internal/tui/`, `go.mod`/`go.sum`, `openspec/`, and `docs/`.

## Proposal question round

The direction was approved by the user in-session before this proposal was written; this executor
could not ask directly. The following are **executor judgment calls** made per D-numbering, open for
correction before `sdd-spec`/`sdd-design`:

1. **D5 phasing boundary** — only Security & Compliance and Operations migrate now; Identity &
   Access and Browse stay on `AdminViewState` behind an adapter. Widening this is the single biggest
   lever on cost. *(Genuine fork: accept the adapter as temporary debt, or migrate everything and
   accept a much larger, longer review.)*
2. **D3** — Gitleaks and Signing each get their own repository list. This is the only place the
   change adds rendered surface that does not exist today; it is a projection of already-fetched
   data, but it is new pixels. Confirm this is wanted rather than, say, a shared
   repository-picker screen parameterized by feature.
3. **D9** — Secret Scan Findings promoted to an Operations entry point while the Trivy drill-down
   is kept. Alternative: move it and remove the drill-down (cleaner IA, breaks muscle memory).
4. **D11** — the Manifest `d`/`x` "unsupported delete" notice is deliberately left alone.
5. **D2** — `o` keeps its current meaning on Trivy's Repository Alerts row rather than being
   renamed for consistency across the three new screens.
