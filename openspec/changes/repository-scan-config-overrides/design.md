# Design: Per-Repository Scan Config Overrides (repository-scan-config-overrides)

## Technical Approach

One new generic table, three (plus one list) `MetadataStore` methods, one
per-feature codec registry in the app layer, one resolution helper called at the
three existing settings-resolution points, conditional argv in both runners, one
feature-nested admin resource, and one TUI modal. Every line/column reference
below was read from the current tree, not estimated.

The whole change is a projection: an override row is resolved into the
already-existing `ports.ScanSettings` value that already flows to the runners,
so no runner interface, no scan-run schema, and no gate code changes at all.

## Architecture Decisions

### Decision 1: One generic `repository_feature_overrides` table with a JSON payload — a deliberate, scoped exception

**Choice**: append one `CREATE TABLE IF NOT EXISTS` to `Store.init()`'s
`statements` slice (`store.go:1002-1190`), storing the feature-specific fields as
an opaque JSON document.

```sql
CREATE TABLE IF NOT EXISTS repository_feature_overrides (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant TEXT NOT NULL,
    repository TEXT NOT NULL,
    feature_name TEXT NOT NULL,
    payload TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(tenant, repository, feature_name)
);
```

`id INTEGER PRIMARY KEY AUTOINCREMENT` + `UNIQUE(...)` follows `repositories`
(`store.go:1004-1010`), not the composite-`PRIMARY KEY` style of `scan_settings`
(`store.go:1070`); either is idiomatic here, and the surrogate key keeps the
uniqueness constraint nameable for the upsert's `ON CONFLICT` target.

**This is the store's first JSON column, and it is a deliberate exception, not
drift.** Every settings table in this file is 100% typed columns evolved with
additive `ALTER TABLE ADD COLUMN` (`store.go:1072-1077` — six of them on
`scan_settings` alone). That idiom is deliberately *not* followed here, and must
not be "fixed" back into typed columns later, because:

- The stated requirement is that storage and resolution are **not**
  trivy/gitleaks-specific. Typed columns force a migration and new interface
  methods per feature; a future image-signing override must reuse this table with
  **zero** schema change (proposal Success Criteria, last-but-two bullet).
- Type safety is not lost, only moved: it is enforced in Go at exactly one
  boundary per feature (Decision 3), the same way `admin_handlers.go` already
  decodes untyped request bodies into typed structs.
- The blast radius is bounded: no query ever filters, sorts, or joins on a field
  *inside* `payload`. Every read is by the full `(tenant, repository,
  feature_name)` key or by `(tenant, feature_name)` for the list. If a future
  requirement needs per-field SQL queryability, that is the signal to revisit —
  not stylistic consistency.

| Option | Tradeoff | Decision |
|---|---|---|
| One generic table, typed JSON payload | New pattern for this store; no per-field SQL queryability | **Chosen** |
| Per-feature typed tables (`repository_trivy_overrides`, …) | Matches the store idiom exactly, but every future feature needs a migration + new port methods — fails the stated reuse requirement | Rejected (explore.md approach 2) |
| EAV (`field_key`/`field_value` rows) | Zero migrations, but destroys the typed-struct convention every settings type in `ports` follows | Rejected (explore.md approach 1) |

### Decision 2: Store methods return typed `NotFound`, not a `found bool`

**Choice**: four methods on `ports.MetadataStore` (`ports/regixtry.go:22-55`),
keeping the interface's existing shape.

```go
GetRepositoryFeatureOverride(ctx context.Context, tenant string, repository string, feature string) ([]byte, error)
ListRepositoryFeatureOverrides(ctx context.Context, tenant string, feature string) ([]RepositoryFeatureOverride, error)
UpsertRepositoryFeatureOverride(ctx context.Context, tenant string, repository string, feature string, payload []byte) error
DeleteRepositoryFeatureOverride(ctx context.Context, tenant string, repository string, feature string) error
```

The change brief sketched `(payload []byte, found bool, err error)`. **Confirmed
against the interface and deliberately not adopted**: zero of the 33 existing
`MetadataStore` methods return a `found` boolean. Row absence is *always* a typed
`domain.NewNotFoundError` (`GetScanSettings` `store.go:459`, `GetScanPolicySettings`
`store.go:513`, `GetUpload` `store.go:87`), and every caller already branches on
`domain.IsCode(err, domain.ErrorCodeNotFound)` — including the exact
row-presence-boundary precedent this feature copies, `loadFeatureSettings`
(`feature_registry.go:242-255`). A third return value would be a new convention
for one method family and would make `applyRepositoryOverride` the only
resolution site in the package that does not read like its siblings.

`Delete` returns `NotFound` when zero rows are affected, mirroring `DeleteUpload`
(`store.go:179-195`) exactly. `Upsert` uses
`INSERT … ON CONFLICT(tenant, repository, feature_name) DO UPDATE SET payload =
excluded.payload, updated_at = excluded.updated_at`, and stores `updated_at` as
`time.RFC3339Nano` like every other row in this store.

`ports.RepositoryFeatureOverride` is the list/API projection only — the
resolution path never uses it:

```go
type RepositoryFeatureOverride struct {
    Repository string          `json:"repository"`
    Feature    string          `json:"feature"`
    Payload    json.RawMessage `json:"payload"`
    UpdatedAt  time.Time       `json:"updated_at"`
}
```

### Decision 3: Typed override structs in `ports`; one codec registry in the app layer is the only place `[]byte` becomes a type

**Choice**: the structs live in `internal/ports/regixtry.go` beside
`ScanSettings` (`ports/regixtry.go:81-97`) and `ScanPolicySettings`
(`:75-79`) — **not** in `internal/infra/scanning/trivy|gitleaks`. The app layer,
the HTTP layer, and the TUI all need these types, and under this codebase's
hexagonal layering the app package must not import an infra adapter (verified:
`service_scanning.go` imports only `context/fmt/net/net/url/strings/time`, `uuid`,
`domain`, `ports`). `ports` is where shared contract types already live.

```go
// TrivyOverride is one repository's full replacement of the global Trivy
// scan configuration (full-row-replace: present -> all fields apply).
type TrivyOverride struct {
    Enabled          bool   `json:"enabled"`
    IgnoreFilePath   string `json:"ignore_file_path,omitempty"`
    IgnorePolicyPath string `json:"ignore_policy_path,omitempty"`
}

type GitleaksOverride struct {
    Enabled    bool   `json:"enabled"`
    ConfigPath string `json:"config_path,omitempty"`
}
```

**The generic-to-typed boundary** is a small registry in a new
`internal/app/regixtry/repository_overrides.go`, keyed by the same feature-name
constants that already key `scan_settings`/`feature_runtime_state`
(`trivyFeatureName` `feature_registry.go:13`, `gitleaksFeatureName` `:20`):

```go
type repositoryOverrideCodec struct {
    // Normalize strictly decodes, validates, and re-marshals an inbound
    // payload into the exact bytes stored. Unknown fields are rejected, so a
    // gitleaks body PUT at the trivy resource is a 400, not a silent no-op.
    Normalize func(raw []byte) ([]byte, error)
    // Apply is the ONLY place a stored []byte becomes a typed struct. It
    // layers the override onto the feature's resolved global ScanSettings.
    Apply func(raw []byte, settings ports.ScanSettings) (ports.ScanSettings, error)
}

var repositoryOverrideCodecs = map[string]repositoryOverrideCodec{
    trivyFeatureName:    {Normalize: normalizeTrivyOverride, Apply: applyTrivyOverride},
    gitleaksFeatureName: {Normalize: normalizeGitleaksOverride, Apply: applyGitleaksOverride},
}

func applyTrivyOverride(raw []byte, settings ports.ScanSettings) (ports.ScanSettings, error) {
    var override ports.TrivyOverride
    if err := json.Unmarshal(raw, &override); err != nil { // lenient: see below
        return ports.ScanSettings{}, domain.NewValidationError("stored trivy override is unreadable")
    }
    settings.Enabled = override.Enabled
    settings.IgnoreFilePath = override.IgnoreFilePath
    settings.IgnorePolicyPath = override.IgnorePolicyPath
    return settings, nil
}
```

**Strict on the way in, lenient on the way out** — deliberate asymmetry.
`Normalize` uses a `json.Decoder` with `DisallowUnknownFields`, matching
`decodeAdminJSON` (`admin_handlers.go:747-774`). `Apply` uses plain
`json.Unmarshal`, so a payload written by a newer binary carrying a field this
binary does not know still resolves instead of hard-failing every scan after a
rollback (the rollback plan in the proposal depends on this).

`Normalize` also enforces the argv-safety rules from the threat matrix: every
non-empty path must be absolute (`filepath.IsAbs`) and is `filepath.Clean`ed. A
relative path or one beginning with `-` is a 400. This is validation, not
existence checking — existence is Decision 6.

Adding image-signing later is exactly one map entry plus one struct in `ports`;
nothing in the store, resolution, HTTP dispatch, or TUI plumbing is feature-aware.

### Decision 4: `ScanSettings` grows three resolve-time-only fields; no new merged struct

**Choice**: extend `ports.ScanSettings` rather than introduce
`EffectiveScanSettings` and change `ScanRunner`/`SecretScanRunner`.

```go
// Per-repository override projection (resolved per run, never persisted).
IgnoreFilePath   string `json:"-"` // trivy --ignorefile
IgnorePolicyPath string `json:"-"` // trivy --ignore-policy
ConfigPath       string `json:"-"` // gitleaks --config
```

| Option | Tradeoff | Decision |
|---|---|---|
| Fields on `ScanSettings`, `json:"-"`, absent from all SQL | Widens a struct that also models a persisted row | **Chosen** |
| New `EffectiveScanSettings` wrapper passed to `Run` | Changes `ports.ScanRunner.Run` (`ports/regixtry.go:186-188`) and `SecretScanRunner.Run` (`:221-223`), i.e. both real runners, every fake, and every existing runner test — for zero behavioral gain | Rejected |

**The precedent is exact, not analogous.** `BinaryPath` and `CacheDir`
(`ports/regixtry.go:91-92`) are already `json:"-"`, already absent from both the
`SELECT` and the `INSERT` of `Get`/`UpsertScanSettings` (`store.go:436-498`), and
are already injected at resolution time from a *different* store row —
`resolveManagedScanSettings` fills them from `feature_runtime_state`
(`service_scanning.go:580-581`), and `resolveManagedScanSettingsForTenant` does
the same (`:290-291`). A per-repository override is the identical mechanism with a
different source row. Because the three new fields appear in neither SQL
statement, they are inert for persistence: a stray `UpsertScanSettings` of a
resolved struct cannot leak them into the global row.

**Resolution sites — three, plus the gitleaks leg.** One helper:

```go
// applyRepositoryOverride is the row-presence boundary: a row for
// (tenant, repository, feature) replaces the feature's global settings in
// full; NotFound means "use the global row", never an error.
func (s *Service) applyRepositoryOverride(ctx context.Context, tenant, repository, feature string, settings ports.ScanSettings) (ports.ScanSettings, error) {
    raw, err := s.metadata.GetRepositoryFeatureOverride(ctx, tenant, repository, feature)
    if err != nil {
        if domain.IsCode(err, domain.ErrorCodeNotFound) {
            return settings, nil
        }
        return ports.ScanSettings{}, err
    }
    codec, ok := repositoryOverrideCodecs[feature]
    if !ok {
        return settings, nil
    }
    return codec.Apply(raw, settings)
}
```

Called from, in each case immediately after the global settings are resolved and
before the run is queued or executed:

| Call site | Current line | Change |
|---|---|---|
| `queueScheduledScan` | `service_scanning.go:227-248` | apply for `repositoryName`+`trivy` onto the passed-in `settings`; `!Enabled` → return without queueing (the sweep skips this repository) |
| `queuePushScan` | `service_scanning.go:261-271` | apply after `resolveManagedScanSettingsForTenant`; `!Enabled` → silent return, matching its existing fail-quiet posture |
| `QueueManualScan` | `service_scanning.go:112-140` | apply after `resolveManagedScanSettings`; `!Enabled` → `domain.NewValidationError` like the existing global-disabled branch (`:117-119`) |
| `executeSecretScanLeg` | `service_scanning.go:352-412` | apply for `gitleaks` after `resolveManagedSecretScanSettings` (`:357`); `!Enabled` → silent return, same as today's `!settings.Enabled` branch (`:358-360`) |

**The trivy and gitleaks legs resolve at different depths, and that asymmetry is
inherent, not sloppy.** The Trivy leg's settings are resolved by the *caller* and
threaded through `executeScanRun`'s `settings` parameter (`:295`, used at `:315`
and `:325`), so the override must be applied at queue time in all three queue
functions. The gitleaks leg resolves its own settings inside its own goroutine
(`:357`) and already knows `repository` (its own parameter), so it applies its
own override there. Neither leg's `Run` signature changes.

**`Enabled=false` suppresses both the scheduled sweep and push scanning** for that
repository (proposal, resolved question 4), which is exactly what the table above
produces — every trigger path passes through one of those four sites.

### Decision 5: Conditional argv appended to each runner's existing literal slice

**Trivy Exec Surface** — `internal/infra/scanning/trivy/runner.go:65-70`. Flags
must precede the positional image reference, so the appends go between the
existing `--cache-dir` block and the `imageRef` append:

```
trivy image --format json
            [--cache-dir <settings.CacheDir>]        (existing, conditional)
            [--ignorefile <settings.IgnoreFilePath>] (new, conditional)
            [--ignore-policy <settings.IgnorePolicyPath>] (new, conditional)
            <imageRef>
```

**Gitleaks Exec Surface** — `internal/infra/scanning/gitleaks/runner.go:72-80`.
The existing argv is a fixed literal slice whose only variables are
adapter-generated paths; `--config` is appended after it, since every gitleaks
flag already follows the positional `dir <path>`:

```
gitleaks dir <staged.ScanDir> --report-format json --report-path <reportPath>
         --no-banner --redact --exit-code 0 --max-archive-depth 2
         [--config <settings.ConfigPath>]            (new, conditional)
```

Both use `if p := strings.TrimSpace(settings.X); p != "" { args = append(args, "--flag", p) }`,
identical in shape to the existing `--cache-dir` conditional. **No override → no
flag → byte-identical argv to today** (proposal Success Criteria bullet 2), which
is what makes the argv-snapshot tests the gitleaks work already established
(`gitleaks-managed-feature/tasks.md:74`) still valid unchanged for the no-override
case. Path values are separate argv elements passed to `exec.CommandContext`
(`trivy/runner.go:29-32`, `gitleaks/runner.go:30-33`) — never a shell string, never
`--flag=value` — so no quoting or interpolation surface is introduced.

### Decision 6: Pre-flight readability check inside each runner, not in the app layer and not left to the CLI

**Choice**: immediately before argv construction, each runner opens every
non-empty override path and closes it; failure returns an error.

```go
func requireReadableFile(label string, path string) error {
    if strings.TrimSpace(path) == "" {
        return nil
    }
    file, err := os.Open(path) // Open, not Stat: proves readability, not mere existence
    if err != nil {
        return fmt.Errorf("%s is not readable", label)
    }
    defer file.Close()
    info, err := file.Stat()
    if err != nil || info.IsDir() {
        return fmt.Errorf("%s is not a regular file", label)
    }
    return nil
}
```

**Why the check exists at all — the two CLIs do not agree, and one of them fails
open.** Documented upstream behavior: gitleaks treats an unreadable `--config` as
fatal and exits non-zero, while Trivy's ignore-file parsing treats a missing
`.trivyignore` as simply "no ignore rules" (its default path does not have to
exist) and continues scanning; `--ignore-policy` is read directly and does error.
If that Trivy behavior holds, a typo'd `IgnoreFilePath` would scan with *no*
suppressions while the operator believes suppressions are active — precisely the
"never silently scan with different rules than configured" failure the proposal
forbids (resolved question 1). Relying on divergent CLI behavior would also make
the run's `Error` message depend on which scanner failed and how.

**This was not verified by execution.** This phase has no shell and neither binary
is available in the tree; the claim above is from upstream documentation and is
recorded as an apply-time verification (Open Questions).

**Why in the runner, not the app layer**: the app package performs no filesystem
syscalls today (verified above — it imports no `os`), while both runners already
do (`gitleaks/runner.go` imports `os`; trivy adds it). More importantly the
failure needs no new plumbing: a runner error already lands on the run as
`Status: Failed` + `Error` —

- Trivy: `executeScanRun` `service_scanning.go:325-334` sets
  `ScanRunStatusFailed` and `run.Error = err.Error()`, then persists.
- Gitleaks: `executeSecretScanLeg` `service_scanning.go:397-406` does the same for
  `SecretScanRun`, *after* the Running row has been persisted (`:394`), so the
  failure is visible in history rather than vanishing like the pre-run early
  returns do.

A pre-flight check in the app layer would duplicate this and would have to invent
its own failure branch in three queue functions.

### Decision 7: Feature-nested admin resource; the slash-bearing repository is the last path segment

**Choice**: reuse the existing repository-in-an-admin-path solution verbatim.

| | Method | Path |
|---|---|---|
| List (TUI annotation) | `GET` | `/admin/v1/features/{feature}/repository-overrides` |
| Read one | `GET` | `/admin/v1/features/{feature}/repository-overrides/{repository}` |
| Set one | `PUT` | `/admin/v1/features/{feature}/repository-overrides/{repository}` |
| Clear one | `DELETE` | `/admin/v1/features/{feature}/repository-overrides/{repository}` |

Repository names contain slashes (`library/alpine`). **This codebase already
solved that exact problem**: `/admin/v1/users/{userID}/grants/{repository}`
(`admin_handlers.go:535-541`) puts the slash-bearing value **last** and splits with
`adminNestedResource(resource, "/grants/")` (`:712-725`), which does a
`SplitN(…, 2)` and guards that the *leading* id contains no `/`. Feature names
contain no slash, so `adminNestedResource(resource, "/repository-overrides/")`
works unchanged — zero new parsing code. The collection form mirrors
`HasSuffix(resource, "/grants")` → `adminNestedUserID` (`:528-534, 708-710`).

| Option | Tradeoff | Decision |
|---|---|---|
| `features/{feature}/repository-overrides/{repository}` | Reuses `adminNestedResource` unchanged; nests the override under the feature it configures | **Chosen** |
| `repositories/{repository}/overrides/{feature}` (brief's sketch) | Reads well, but the slash-bearing segment is in the *middle*: needs new `strings.LastIndex` parsing (`Index` would break on a repository containing an `overrides` path segment) — new machinery for a solved problem | Rejected |
| `?repository=` query parameter (`scan-runs`, `secret-scan-findings` precedent, `admin_handlers.go:278, 342`) | Sidesteps slashes entirely, but a query-parameter-addressed `DELETE` is not a resource, and the TUI already sends unescaped slash-bearing repositories on a path today (`admin_client.go:301`) | Rejected |

**Dispatch ordering is load-bearing.** The new `case` goes **first** in
`handleAdminFeatureResource`'s switch (`admin_handlers.go:72-189`), ahead of
`Contains(resource, "/actions/")` and the `HasSuffix` family. That switch matches
`HasSuffix(resource, "/config")` and `"/status"`; a repository legitimately named
`team/config` would otherwise be swallowed by the `/config` branch. Ordering plus
a RED test is the whole mitigation (threat matrix row).

**Wire shape**: the body *is* the typed feature object, so
`DisallowUnknownFields` rejects a gitleaks body sent to the trivy resource.

```
PUT /admin/v1/features/trivy/repository-overrides/library/alpine
{"enabled": true, "ignore_file_path": "/etc/regixtry/ignore/alpine.trivyignore"}
```

`GET` returns `200` with that object plus `updated_at`, or **`404` when no
override exists** — the row-presence boundary made explicit on the wire, matching
`GetScanSettings`'s `NotFound`. (Rejected: `200 {"override": false, …}`; it invents
a second way to express absence.) `DELETE` returns `204`, or `404` when nothing was
there (`DeleteUpload` precedent, `store.go:190-192`). Unknown feature → `404`
route error via the codec-registry lookup. Authorization is inherited from
`handleAdmin`'s `requireAdminPrincipal` (`admin_handlers.go:23-26`) — no new
permission surface (proposal, resolved question 2).

Service methods stay feature-agnostic and take raw bytes so that feature
knowledge lives only in the codec registry:
`GetRepositoryOverride`, `ListRepositoryOverrides`, `SetRepositoryOverride(ctx,
repository, feature string, raw []byte)`, `ClearRepositoryOverride`. Each calls
`parseRepository` first, so the stored key is the canonical name.

### Decision 8: New `repositoryOverrideModal`, mirroring `scanPolicyModal`'s exact 3-piece shape

Opened with `o` on a highlighted Repository Alerts row. The template is
`scanPolicyModal`, followed piece for piece.

**1. `session.go`** — beside `scanPolicyModal` (`session.go:126-157`):

```go
type repositoryOverrideField int

const (
    repositoryOverrideFieldFeature repositoryOverrideField = iota
    repositoryOverrideFieldEnabled
    repositoryOverrideFieldPathPrimary   // trivy: ignore file | gitleaks: config
    repositoryOverrideFieldPathSecondary // trivy: ignore policy (skipped for gitleaks)
    repositoryOverrideFieldClear         // action row, not an input
)

type repositoryOverrideModal struct {
    Open          bool
    Repository    string
    Feature       string // trivyFeatureName | gitleaksFeatureName
    Focus         repositoryOverrideField
    Exists        bool // false => this repository inherits the global row
    Enabled       bool
    PathPrimary   string
    PathSecondary string
    Loading       bool
    Error         string
}

func (m repositoryOverrideModal) Active() bool { return m.Open }
```

`AdminViewState` gains `RepositoryOverrideModal repositoryOverrideModal`, next to
`ScanPolicyModal` (`session.go:267`), and is zeroed in `clearSelectedAdminDetails`
(`model.go:2795-2817`) and `applyFeaturePage` (`:2841-2848`) alongside the other
modals. `nextRepositoryOverrideField` wraps and **skips
`…PathSecondary` when `Feature == gitleaksFeatureName`**, mirroring
`nextScanPolicyField` (`session.go:152-157`).

**2. `model.go`** — one `Active()` branch appended to the chain at
`model.go:1059-1078`, after `ScanPolicyModal` and before `ScanHistoryModal`, plus
a dedicated `updateRepositoryOverrideModalKey` modeled on
`updateScanPolicyModalKey` (`model.go:1326-1350`): Esc clears, Tab cycles, Space
toggles `Enabled` / cycles `Feature`, runes append to the focused path field
(`appendTrivyConfigModalRunes` pattern, `:1312-1316`), Enter submits.

The opener is a new `case` inside `updateAdminFeaturesKey`'s switch, adjacent to
the `Enter` drill-down (`model.go:1227-1243`) and reusing its exact selection
helper:

```go
case m.isSelectedTrivyFeature() && m.adminView.TrivyTab == trivyTabRepositoryAlerts && isRuneKey(msg, 'o'):
    summary, ok := selectedScanSummary(m.adminView) // model.go:2896-2902
    if !ok { return m, nil }
    m.adminView.RepositoryOverrideModal = repositoryOverrideModal{
        Open: true, Repository: summary.Repository,
        Feature: trivyFeatureName, Loading: true,
    }
    return m, m.loadRepositoryOverrideCmd(summary.Repository, trivyFeatureName)
```

This `case` sits inside the switch that ends at `model.go:1255`, therefore *before*
the `featureActionForKey` fallback at `:1257` — so `o` cannot be stolen by a
feature action, and cannot steal one either (a RED test pins this).

`Enter` means **save** on every focus except `…FieldClear`, where it means
**clear**; when `Exists` is false the clear row is inert and reports "already
inheriting global" instead of issuing a `DELETE` that would 404. No new key
vocabulary is introduced — deliberate, because the path fields consume printable
runes, so a `d`-for-delete binding would be unreachable while typing a path, and
the TUI has no `Ctrl`-key precedent anywhere.

Two `tea.Msg` types (`…LoadedMsg`, `…SavedMsg`) follow
`adminScanPolicyLoadedMsg`/`…UpdatedMsg` (`model.go:250-258`); three commands
follow `loadScanPolicyCmd`/`updateScanPolicyCmd` (`:2349-2367`); three
`AdminClient` methods follow `GetScanPolicy`/`UpdateScanPolicy`
(`admin_client.go:35-36, 245-260`), with the clear using `requestNoContent(…,
MethodDelete, …, StatusNoContent)` exactly like `DeleteUserGrant` (`:300-303`) —
including *not* `PathEscape`-ing the repository, since escaping the slashes would
defeat the server-side split.

**3. `admin_views.go`** — `renderRepositoryOverrideModal` beside
`renderScanPolicyModal` (`admin_views.go:618-629`), plus a fifth branch on
`renderAdminWorkspace`'s single `compositeOverlay` tail (`:34-54`) and `o` in
`adminFeatureHelp`. Row arithmetic, using the same 2-rows-per-field cost and
4-row `theme.section` chrome as Decision 6 of `scan-policy-gate/design.md`:

| Modal state | Heading | Status line | Fields | Clear row | Blank+help | Inner | +chrome | Total |
|---|---|---|---|---|---|---|---|---|
| Trivy, no error | 1 | 1 | 4 × 2 = 8 | 1 | 2 | 13 | 4 | **17** |
| Trivy + error | 1 | 1 | 8 | 1 | 4 | 15 | 4 | **19** |
| Gitleaks, no error | 1 | 1 | 3 × 2 = 6 | 1 | 2 | 11 | 4 | **15** |
| Gitleaks + error | 1 | 1 | 6 | 1 | 4 | 13 | 4 | **17** |

Worst case 19 rows against the 24-row `minViewportHeight` floor — the same class
as the existing 17-row Trivy Config modal, with 5 rows of margin. The heading
carries the repository (`Repository Override — library/alpine`) and the 1-row
status line carries the inheritance state (`Loading…` / `override active` /
`inheriting global settings`), which is how the modal answers "which repository"
and "override or inherited" without spending a 2-row field on either.

### Decision 9: The pull gate sees override-adjusted counts — traced, not asserted

The proposal states this coupling; here is the actual data path, each hop read
from the current tree:

1. `applyRepositoryOverride` sets `settings.IgnoreFilePath` / `IgnorePolicyPath`
   (Decision 4) at queue time.
2. `executeScanRun` passes that same `settings` value to
   `scanRunner.Run(ctx, target, settings)` — `service_scanning.go:325`.
3. The Trivy runner appends `--ignorefile` / `--ignore-policy` (Decision 5).
   Both flags act on Trivy's **result filtering**, before the report is emitted,
   so suppressed vulnerabilities are absent from `payload.Results`.
4. The runner counts severities by walking exactly that decoded
   `payload.Results` — `trivy/runner.go:89-101` (`case "CRITICAL": result.Critical++`).
   Nothing else produces those integers.
5. `executeScanRun` copies them onto the run: `run.Critical = result.Critical`,
   `run.High = result.High` — `service_scanning.go:336-337` — then persists via
   `persistAsyncScanRunDetail` (`:343`).
6. On pull, `OpenManifest` → `enforceScanPolicy` (`service_scanning.go:94-110`)
   loads the newest run for the digest with `GetLatestScanRunByDigest` (`:99`)
   and passes it to `scanPolicyViolated` (`:78-86`), which reads
   `run.Critical` / `run.High` — **the same two integers written in step 5**.

Therefore the gate provably observes the override's effect. Two consequences worth
stating because they are not obvious from the proposal's one-line claim:

- **Only after a rescan.** Step 6 reads the latest *stored* run. Setting an
  override does not re-evaluate existing completed runs, so a digest already
  blocked stays blocked until a new run for that digest completes. The override
  API deliberately does not trigger a rescan (out of scope); the operator uses the
  existing manual rescan.
- **Nothing in the gate changes.** `scanPolicyViolated`, `enforceScanPolicy`,
  `GetLatestScanRunByDigest`, and `ScanPolicySettings` are untouched; the gate
  stays global-only, exactly as the proposal's Out of Scope requires.

## Data Flow

    SET     TUI 'o' -> PUT /admin/v1/features/trivy/repository-overrides/library/alpine
              adminNestedResource -> service.SetRepositoryOverride
                codec.Normalize (strict decode + absolute-path validation)
                  -> UpsertRepositoryFeatureOverride(tenant, repo, feature, payload)

    SCAN    queueScheduledScan | queuePushScan | QueueManualScan
              global ScanSettings (scan_settings row)
                -> applyRepositoryOverride  --NotFound--> global unchanged
                       |
                       +--found--> codec.Apply -> Enabled/IgnoreFilePath/IgnorePolicyPath
                                        |
                                        +-- !Enabled -> no run queued
              -> dedupAndQueueScanRun -> go executeScanRun(settings)
                    -> trivy Runner.Run: readability pre-flight -> argv +flags
                    -> result.Critical/High -> run.Critical/High -> scan_runs

            executeScanRun -> go executeSecretScanLeg
              gitleaks ScanSettings -> applyRepositoryOverride(gitleaks)
                -> Runner.Run: pre-flight -> argv +--config

    PULL    OpenManifest -> enforceScanPolicy -> GetLatestScanRunByDigest
              -> scanPolicyViolated(run.Critical, run.High)  [unchanged code]

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/ports/regixtry.go` | Modify | `TrivyOverride`, `GitleaksOverride`, `RepositoryFeatureOverride`; 3 `ScanSettings` fields; 4 `MetadataStore` methods |
| `internal/infra/metadata/sqlite/store.go` | Modify | `repository_feature_overrides` DDL in `init()`; Get/List/Upsert/Delete |
| `internal/app/regixtry/repository_overrides.go` | Create | Codec registry, per-feature normalize/apply, `applyRepositoryOverride`, 4 service methods |
| `internal/app/regixtry/service_scanning.go` | Modify | Override resolution in `queueScheduledScan`, `queuePushScan`, `QueueManualScan`, `executeSecretScanLeg` |
| `internal/infra/scanning/trivy/runner.go` | Modify | `--ignorefile` / `--ignore-policy` argv + readability pre-flight |
| `internal/infra/scanning/gitleaks/runner.go` | Modify | `--config` argv + readability pre-flight |
| `internal/protocol/http/admin_handlers.go` | Modify | `repository-overrides` collection + resource cases (first in the switch), handlers |
| `internal/tui/session.go` | Modify | `repositoryOverrideField`, `repositoryOverrideModal`, `Active()`, `AdminViewState` field |
| `internal/tui/model.go` | Modify | `o` opener, `Active()` branch, modal key handler, 2 msgs, 3 cmds, modal reset in 2 places |
| `internal/tui/admin_client.go` | Modify | `Get/List/Set/ClearRepositoryOverride` on the client interface + HTTP impl |
| `internal/tui/admin_views.go` | Modify | `renderRepositoryOverrideModal`, overlay branch, `o` in `adminFeatureHelp` |
| `internal/tui/admin_tables.go` | Modify | Repository Alerts row annotation for `Enabled=false` (from the list endpoint) |

`internal/protocol/http/router.go` is **not** in this list: the admin surface is
reached through `handleAdmin`'s existing dispatch, and no `/v2` route changes.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | `Normalize` rejects unknown fields, relative paths, and paths starting with `-` | Table-driven per codec |
| Unit | `Apply` round-trips a payload into `ScanSettings`; a payload with an unknown extra field still applies (rollback tolerance) | Table-driven, no store |
| Unit | Upsert/Get/Delete round-trip; `Get` on an absent row returns typed `ErrorCodeNotFound`; `Delete` on an absent row too | `t.TempDir()` store |
| Unit | Same repository, two features → two independent rows; a third fabricated feature name round-trips with no migration | Store test (proves the reuse claim) |
| Unit | Trivy argv: no override → byte-identical to today; ignore file only; policy only; both; flags precede the image ref | Argv-snapshot via fake exec |
| Unit | Gitleaks argv: no override → byte-identical to today's literal slice; `--config` appended when set | Argv-snapshot via fake exec |
| Unit | Unreadable / directory / permission-denied path → runner error before exec is called | Fake exec asserting it was never invoked |
| Unit | `renderRepositoryOverrideModal` ≤ 19 rows (trivy+error) and ≤ 17 (gitleaks+error); shows repository and inheritance state | `lipgloss.Height` |
| Unit | `nextRepositoryOverrideField` skips the secondary path field for gitleaks | Table-driven |
| Integration | Override present → that run fails/queues per `Enabled`; other repositories' runs unchanged in argv and outcome | Service test with 2 repositories |
| Integration | `Enabled=false` suppresses scheduled, push, and manual queueing (3 cases) | Service test |
| Integration | Bad path → `ScanRun.Status == failed` with `Error` populated; secret leg → `SecretScanRun.Status == failed` | Service test |
| Integration | Ignore-file override changes `run.Critical` → `OpenManifest` allows a digest it previously blocked, **after a rescan**; before rescan it still blocks | Service test (proposal's last success criterion) |
| Integration | `GET` 404 when inheriting, 200 when set; `PUT` 400 on gitleaks body at the trivy resource; `DELETE` 204 then 404 | Handler test |
| Integration | Repository named `team/config` routes to the override handler, not the `/config` branch | Router test (threat matrix) |
| Integration | `o` opens the modal bound to the highlighted repository; save then clear round-trips and the modal reflects inheritance | Model test |

## Threat Matrix

Included because this design adds subprocess argv construction and HTTP path
dispatch. No VCS/PR automation, executable-file classification, or commit/push
boundary is touched.

| Boundary | Applicability | Design response | Planned RED test |
|---|---|---|---|
| Documentation-like paths | N/A — no file-classification or execution decision is made from a path's name | — | — |
| Git repository / commit / push state | N/A — no VCS invocation anywhere in this change | — | — |
| PR commands | N/A — no PR automation | — | — |
| **Subprocess argv (added)** | **Applicable** — operator-supplied strings reach `exec.CommandContext` argv | Fixed literal flag names; each path is one separate argv element, never a shell string and never `--flag=value`; `Normalize` requires absolute, `filepath.Clean`ed paths and rejects a leading `-`, so a value cannot be reparsed as a flag | A path of `-oJSON` and a relative path are both rejected at `PUT`; a stored path never appears fused to its flag in argv |
| **Arbitrary local file read (added)** | **Applicable** — an override points the scanner at any host path | Admin-only surface (`requireAdminPrincipal`), same trust level as the existing operator-set `ScanSettings.TLSCACertPath` (`ports/regixtry.go:89`); file contents are never echoed into API responses, run errors, or findings | Failure message names the field, never the path contents |
| **HTTP path dispatch (added)** | **Applicable** — new nested admin resource whose last segment contains slashes | `adminNestedResource` reused unchanged (slash-bearing value last); the new case is **first** in `handleAdminFeatureResource`'s switch, ahead of the `HasSuffix("/config"\|"/status")` family | Repository `team/config` routes to the override handler; unknown feature → 404 |
| **Fail-open scan config (added)** | **Applicable** — a broken override could silently scan with global rules | Readability pre-flight fails the run (`Status: Failed` + `Error`) instead of degrading (Decision 6) | Unreadable path produces a failed run, not a completed one |

## Migration / Rollout

Additive only: one `CREATE TABLE IF NOT EXISTS` appended to the existing `init()`
statement slice, whose loop already tolerates re-runs (`store.go:1002-1190` and
the trailing error filter). No `ALTER TABLE`, no existing-column change, no
manifest or blob change.

Behavioral rollout is inert by construction: with zero override rows, every
resolution takes the `NotFound` branch and returns the global settings unchanged,
and both runners build today's exact argv. A reverted binary leaves
`repository_feature_overrides` orphaned and unread, and every repository resolves
to the global row exactly as before.

## Open Questions

- [ ] **Trivy `--ignorefile` failure behavior (needs the real binary).** Decision 6
      assumes Trivy tolerates a missing ignore file (scans with no suppressions,
      exit 0) while `--ignore-policy` errors. Not executable in this phase.
      `sdd-apply` must confirm both against the installed Trivy before relying on
      the pre-flight as the *only* guard, and record the observed behavior.
- [ ] **Suppressed findings and the report.** Decision 9 step 3 assumes ignored
      vulnerabilities are absent from `payload.Results` rather than re-emitted
      under a suppressed/modified-findings key by newer Trivy versions. If newer
      versions do re-emit them, `trivy/runner.go:89-101` would keep counting them
      and the gate coupling would silently not hold — verify against the installed
      version before closing the gate test.
- [ ] **Live-render confirmation.** The 19-row worst case is arithmetic from
      `renderScanPolicyModal`'s established per-field cost; `sdd-verify` should
      confirm at 150×24 that the modal shows its full bottom border and help line.
- [ ] **List endpoint scope.** `ListRepositoryFeatureOverrides` exists only so the
      Repository Alerts table can render "scanning disabled" for a repository whose
      override sets `Enabled=false` (proposal, resolved question 4). A repository
      that has *never* been scanned still has no summary row to annotate; confirm
      with `sdd-spec` whether injecting a synthetic row is required, or whether
      annotating existing rows satisfies the requirement.
- [ ] **Payload size bound.** `SetRepositoryOverride` reads the request body
      without an explicit `MaxBytesReader`, matching `decodeAdminJSON`'s current
      behavior. Confirm that inherited posture is acceptable for an opaque-payload
      endpoint, or add a bound.
