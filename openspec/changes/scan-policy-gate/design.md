# Design: Vulnerability Policy Gate (scan-policy-gate)

## Technical Approach

One global policy record, one new digest-scoped store query, one pure
evaluator, and one insertion point in `Service.OpenManifest`. Everything else
(HTTP surface, TUI modal, push auto-queue) hangs off those four pieces using
patterns already present in the repository. All row/column arithmetic below was
derived by reading the current render functions and SQL, not estimated.

**Correction to the proposal's affected-areas table**: `internal/app/regixtry/commands.go`
**does not exist**. The package is `queries.go`, `feature_registry.go`,
`feature_runtime.go`, `service.go`, `service_scanning.go`, `service_test.go`.
`PublishManifest` lives at `internal/app/regixtry/service.go:176-206`. Decision 4
targets that real location.

## Architecture Decisions

### Decision 1: Dedicated `scan_policy_settings` table, string threshold, code-level default

**Choice**: a new table keyed by tenant alone, appended to `Store.init()`'s
`statements` slice (`store.go:945-1142`), plus a string enum in `ports`.

```sql
CREATE TABLE IF NOT EXISTS scan_policy_settings (
    tenant TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    severity_threshold TEXT NOT NULL DEFAULT 'critical',
    updated_at TEXT NOT NULL,
    PRIMARY KEY(tenant)
);
```

```go
type ScanPolicySettings struct {
    Enabled           bool      `json:"enabled"`
    SeverityThreshold string    `json:"severity_threshold"`
    UpdatedAt         time.Time `json:"updated_at"`
}

const (
    ScanPolicyThresholdCritical     = "critical"
    ScanPolicyThresholdCriticalHigh = "critical_high"
)
```

`Get`/`UpsertScanPolicySettings` mirror `Get`/`UpsertScanSettings`
(`store.go:436-498`) exactly: `QueryRowContext` + `sql.ErrNoRows` →
`domain.NewNotFoundError("scan_policy_settings", tenant)`, and
`INSERT ... ON CONFLICT(tenant) DO UPDATE SET`, with `updated_at` stored as
`time.RFC3339Nano`.

| Option | Tradeoff | Decision |
|---|---|---|
| New `scan_policy_settings` table | One extra `CREATE TABLE IF NOT EXISTS`; no guarded `ALTER` needed | **Chosen** |
| 2 new columns on `scan_settings` under `feature='vulnerability-policy'` | Reuses a table whose other 13 columns (interval, binary_path, tls_*) are meaningless for a policy row and are all `NOT NULL` | Rejected |
| Third `featureDescriptor`/`FeatureConfigureInput` feature | Couples a pure toggle to install/upgrade/rollback lifecycle fields | Rejected (proposal) |
| String enum threshold | Matches `ScanRunStatus*`/`ScanTrigger*` (`ports/regixtry.go:55-61`) | **Chosen** |
| Int severity rank | No severity-rank type exists in `ports`; `highestSeverityRank` is TUI-local | Rejected |

**Default seeding — code-level fallback, not a migration default and not a
startup `Ensure*` call.** `GetScanSettings` has **no** fallback: it returns
`NotFound`, and `main.go:2155` seeds the row via `EnsureScanSettings` at boot.
That precedent is deliberately **not** followed here, because the gate's
default-ON posture would then depend on a boot path having run — a test, an
embedded use, or a reordered startup would silently produce policy-OFF. Instead:

```go
func (s *Service) GetScanPolicySettings(ctx context.Context) (ports.ScanPolicySettings, error) {
    settings, err := s.metadata.GetScanPolicySettings(ctx, s.tenant(ctx))
    if domain.IsCode(err, domain.ErrorCodeNotFound) {
        return ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical}, nil
    }
    return settings, err
}
```

The SQL `DEFAULT`s above are belt-and-braces for hand-inserted rows; the Go
fallback is the authority. A fresh install therefore reports enabled/CRITICAL
with zero rows written, and the first PATCH is what creates the row.

### Decision 2: `GetLatestScanRunByDigest` is `GetActiveScanRunByDigest` minus the status filter

**Choice**: same SELECT list, same ordering, same `scanRunRow` decoder — only
the `status IN (?, ?)` predicate is dropped.

```go
func (s *Store) GetLatestScanRunByDigest(ctx context.Context, tenant, repository, digest string) (ports.ScanRun, error) {
    row := s.db.QueryRowContext(ctx, `
        SELECT id, repository, requested_ref, digest, status, trigger, started_at, finished_at, created_at, updated_at, critical, high, medium, low, trivy_version, db_updated_at, error
        FROM scan_runs
        WHERE tenant = ? AND repository = ? AND digest = ?
        ORDER BY created_at DESC
        LIMIT 1
    `, tenant, repository, digest)
    return scanRunRow(row)
}
```

**Not-found is typed, never a zero value.** `scanRunRow` (`store.go:1260-1269`)
already converts `sql.ErrNoRows` into `domain.NewNotFoundError("scan_run", "")`,
so "never scanned" arrives as an error the gate matches with
`domain.IsCode(err, domain.ErrorCodeNotFound)`. A zero-value `ScanRun` could
otherwise be misread as `Status: "" , Critical: 0` — i.e. "clean". This is the
exact confusion the proposal's top risk names; the typed error is what makes it
structurally impossible.

**The evaluator is a pure function**, in `internal/app/regixtry/`, table-testable
without a store:

```go
func scanPolicyViolated(settings ports.ScanPolicySettings, run ports.ScanRun) bool {
    if !settings.Enabled || run.Status != ports.ScanRunStatusCompleted {
        return false
    }
    if settings.SeverityThreshold == ports.ScanPolicyThresholdCriticalHigh {
        return run.Critical > 0 || run.High > 0
    }
    return run.Critical > 0 // default branch: unknown/empty threshold behaves as CRITICAL
}
```

| Input | Threshold | Result |
|---|---|---|
| `Completed`, `Critical>0` | CRITICAL | **block** |
| `Completed`, `Critical=0, High>0` | CRITICAL | allow |
| `Completed`, `Critical=0, High>0` | CRITICAL+HIGH | **block** |
| `Completed`, all zero | either | allow |
| `Queued` / `Running` / `Failed`, any counts | either | allow |
| `NotFound` (never scanned) | either | allow (gate returns before evaluating) |
| `Enabled=false` | either | allow |

**Fail-open covers scan-state uncertainty, not infrastructure failure.** A
non-`NotFound` store error from either lookup propagates as today's store errors
already do in `OpenManifest`. Swallowing DB errors into "allow" would make the
gate bypassable by anyone able to induce one.

### Decision 3: Gate inlined in `OpenManifest` after resolution; `ResolveManifest` untouched

Current body (`queries.go:71-82`) is 4 statements. The gate needs the resolved
digest, so it goes **after** `s.metadata.ResolveManifest`, which forces the
single-return line to become an explicit variable:

```go
func (s *Service) OpenManifest(ctx context.Context, repositoryName string, reference string) (domain.Manifest, error) {
    repository, err := parseRepository(repositoryName)                                    // unchanged
    if err != nil { return domain.Manifest{}, err }
    if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPull, Repository: repository.String()}); err != nil {
        return domain.Manifest{}, err                                                     // unchanged
    }
    manifest, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repository, reference) // was the return
    if err != nil { return domain.Manifest{}, err }
    if err := s.enforceScanPolicy(ctx, repository.String(), manifest.Digest.String()); err != nil {
        return domain.Manifest{}, err                                                     // NEW
    }
    return manifest, nil
}
```

Ordering rationale: authorization stays first, so an unauthorized caller still
gets 401 and never learns whether a digest exists or is vulnerable. Resolution
stays second, so a missing tag is 404 rather than a policy verdict about a
digest that does not exist.

**`ResolveManifest` and `OpenManifest` are genuinely independent** — verified,
not assumed. Both are `Service` methods on the same struct; neither calls the
other. `ResolveManifest` (`queries.go:48-69`) does its own `parseRepository`,
its own `s.authorize`, its own `s.metadata.ResolveManifest`, then
`ListManifestBlobs`. The only shared callee is the **store's** `ResolveManifest`
port method, which is not being changed. Callers: `OpenManifest` ← `router.go:327`
(pull only); `ResolveManifest` ← the TUI's `QueryService` (`internal/tui/model.go:39-42`)
and `service_scanning.go`. The TUI browse path and the scanner's own digest
resolution therefore cannot inherit the gate.

**Error mapping**: `ErrorCodePolicyViolation ErrorCode = "POLICY_VIOLATION"` +
`NewPolicyViolationError(message string)` in `internal/domain/regixtry/errors.go`
(every existing code already has exactly this constructor pairing), and one new
case in `writeError` (`router.go:559-578`) between `NotFound` and the 400 group:

```go
case domain.ErrorCodePolicyViolation:
    status = stdhttp.StatusForbidden
    code = "DENIED"
```

`DENIED` is the OCI-distribution error code for a refused-but-authenticated
operation, and is already used by the `Conflict` case. `challengeForError`
is not involved: 403 must not emit `WWW-Authenticate`, because re-authenticating
cannot help.

### Decision 4: Push auto-queue is a fire-and-forget goroutine with a pre-captured tenant

**Choice**: append one line to `PublishManifest` (`service.go:201-205`), after
the metadata write succeeds:

```go
if err := s.metadata.PublishManifest(ctx, s.tenant(ctx), repository, tag, manifest, manifest.References()); err != nil {
    return ManifestDetails{}, err
}
go s.queuePushScan(context.Background(), s.tenant(ctx), repository.String(), reference, manifest.Digest.String())
return newManifestDetails(...)
```

Three properties, each with a precedent in this package:

1. **Whole call in a goroutine, not just execution.** `QueueManualScan` and
   `queueScheduledScan` do their settings resolution and 2 store round-trips
   synchronously and only `go s.executeScanRun(...)`. That shape is wrong here:
   `resolveManagedScanSettings` returns a `ValidationError` whenever the Trivy
   runtime is not `Ready` (`service_scanning.go:443-452`), which would surface as
   a **failed push**. Push must never fail or wait because of scanning.
2. **Tenant captured before the goroutine, `context.Background()` inside.** The
   request context is cancelled the moment the push response is written. This is
   the documented precedent on `resolveManagedSecretScanSettings`
   (`service_scanning.go:458-463`): it takes tenant explicitly "because it is
   always called from a goroutine already holding the caller's resolved tenant".
3. **Errors dropped, never surfaced.** Same posture as `executeSecretScanLeg`,
   whose comment states it "must never fail or block the Trivy leg".

`queuePushScan` is a sibling of `queueScheduledScan` with two differences: it
**receives the digest** (already computed by `parseManifestPayload`, so no
`ResolveManifest` round-trip and no re-resolution race against a concurrent
retag), and it gates on `settings.Enabled` only — **not** `ScheduleEnabled`,
which governs the periodic sweep, not push. Dedup is the existing
`GetActiveScanRunByDigest` (`queued|running`) check, which is correct **for this
purpose** and only this one. New constant `ScanTriggerPush = "push"` in `ports`:
`scan_runs.trigger` is `TEXT` with no CHECK constraint and nothing switches
exhaustively on trigger, so this needs no migration.

### Decision 5: Admin GET/PUT beside `scan-settings`; CI route nested under the manifest path

**Admin** — one new `case` in `handleAdmin`'s switch (`admin_handlers.go:34-53`),
modeled line-for-line on `handleAdminScanSettings` (`:189-214`), which is the
comparable existing endpoint:

| | Method | Path | Body |
|---|---|---|---|
| Read | `GET` | `/admin/v1/scan-policy` | — |
| Write | `PUT` | `/admin/v1/scan-policy` | `{"enabled":true,"severity_threshold":"critical_high"}` |

`PUT` (full replacement), not `PATCH`: the record is two fields, and
`handleAdminScanSettings` already uses `PUT` for the same shape. Unknown
`severity_threshold` values are rejected with `domainauth.NewValidationError`
→ 400, so the evaluator's permissive default branch is never reachable from the
API. Admin auth is inherited from `handleAdmin`'s `requireAdminPrincipal`.

**CI scan-status** — `GET /v2/<name>/manifests/<reference>/scan-status`,
dispatched inside the existing `manifests/` case of `handleV2`
(`router.go:153-154`) rather than as a new case before it:

```go
case strings.HasPrefix(suffix, "manifests/"):
    reference := strings.TrimPrefix(suffix, "manifests/")
    if strings.HasSuffix(reference, "/scan-status") {
        r.handleManifestScanStatus(w, req, repository, strings.TrimSuffix(reference, "/scan-status"))
        return
    }
    r.handleManifest(w, req, repository, reference)
```

The `/`-prefixed suffix test is checked against the **trimmed reference**, not
against `suffix`. Testing `suffix` would misroute a tag literally named
`scan-status` (`manifests/scan-status` ends with `/scan-status`); testing the
trimmed reference does not, because `"scan-status"` does not end with
`"/scan-status"`. OCI references contain no `/`, so `<ref>/scan-status` is
otherwise unambiguous.

**Auth scope**: `ports.Action{Verb: ports.ActionPull, Repository: repository}`
via the existing `r.withPrincipal`. Verified reachable by ordinary pull
credentials: `principalAccessController` accepts `ActionPull`/`ActionInspect`
when `Principal.HasReadAccess(repository)` (`defaults.go:112-115`), and
`configurableAccessController` accepts both under `allowAnonymousPull`
(`defaults.go:67`). `ActionPull` is chosen over `ActionInspect` because the two
are treated identically by both controllers, and `ActionPull` is the exact action
the gated operation itself uses — "if you may pull it, you may learn why you
cannot".

**Response — always 200, five distinct states**, never a boolean. A polling CI
must distinguish "wait" from "give up":

| `state` | Source |
|---|---|
| `unscanned` | `GetLatestScanRunByDigest` → `NotFound` |
| `in_progress` | `Status` is `queued` or `running` |
| `failed` | `Status` is `failed` |
| `clean` | `Status` is `completed`, `scanPolicyViolated` false |
| `blocked` | `Status` is `completed`, `scanPolicyViolated` true |

```json
{ "repository": "library/alpine", "reference": "latest", "digest": "sha256:...",
  "state": "blocked", "would_block_pull": true,
  "policy": { "enabled": true, "severity_threshold": "critical" },
  "scan": { "status": "completed", "critical": 2, "high": 5, "medium": 0, "low": 1,
            "finished_at": "2026-08-12T10:04:00Z" } }
```

`scan` is omitted when `unscanned`. `would_block_pull` is `state == "blocked"`
restated as the single boolean a pipeline gates on, so callers need not encode
the state table. The endpoint itself returns 200 in all five states — it reports
a verdict, it is not subject to one.

### Decision 6: Own 11-row modal; badge composed onto the existing tab line at zero row cost

**Modal** — a new `scanPolicyModal` struct in `session.go` (sibling of
`trivyConfigModal:111-124`, which is **not** edited) and a new
`renderScanPolicyModal` in `admin_views.go`, wired as a fourth branch of
`renderAdminWorkspace`'s single `compositeOverlay` tail. Opened with `p` from the
Trivy feature page.

Post-`tui-design-polish` field cost is 2 rows (`theme.input` flattened: label +
1-row value), and `theme.section` chrome is 4:

| Modal | Heading | Fields | Blank+help | Inner | +chrome | Total |
|---|---|---|---|---|---|---|
| Trivy Config (existing) | 1 | 5 × 2 = 10 | 2 | 13 | 4 | **17** |
| **Scan Policy (new)** | 1 | 2 × 2 = 4 | 2 | 7 | 4 | **11** |
| Scan Policy + error | 1 | 4 | 2 (+2) | 9 | 4 | **13** |

11 rows against the 24-row `minViewportHeight` floor leaves 13 rows of margin —
comfortably clear of `TestRenderTrivyConfigModalFitsWithinCompactedRowBudget`'s
20-row class, and the existing modal's budget is untouched because its 5 fields
are unchanged. Threshold is a two-value cycle field toggled with Space
(rendered as `CRITICAL` / `CRITICAL+HIGH`), so it costs the same 2 rows as the
enabled toggle and the modal height is invariant across focus position.

**Badge — composed onto `renderTrivyTabs`'s existing tab line
(`admin_views.go:228-237`), which returns exactly 2 rows and keeps returning 2.**

```go
return theme.subheading.Render("Tabs") + "\n" + runtimeLabel + " | " + alertsLabel + "  " + policyBadge
```

| Budget | Measurement | Verdict |
|---|---|---|
| **Rows** | Line count unchanged: subheading + 1 composed line = 2 | **+0 rows.** `contentBudget` (`viewport.go:109-137`) needs no change; `SectionRows` is unaffected |
| **Columns** | `"Runtime | Repository Alerts"` = 27 + `"  Policy: ON (CRITICAL+HIGH)"` = 28 → **55**. `sectionWidth` at the 150-column floor = `150 - 4` = **146** | 91 columns of slack; cannot wrap |

Rejected: a 9th column on `buildAdminScanSummaryTable`
(`admin_tables.go:237-263`). Its 8 columns already measure
26+16+12+9+7+8+6+34 = **118** content columns plus ~9 separator/border columns =
**~127** against a 146-column section — only ~19 columns of headroom, and the
`tui-table-viewport-fixed-size` work just tuned those widths. A 9th column also
repeats the same global value on every row. Rejected: a dedicated badge line
(+1 row, and rows are the scarce budget here, not columns).

**Placement tradeoff, stated explicitly**: the badge is visible on the Trivy
feature page only, not app-wide. That is where scan state already lives and
where the `p` key is bound, and it is the only placement costing zero rows in a
row-constrained layout. If the spec requires app-wide persistence, the only
zero-row alternative is the workspace context/breadcrumb line — which would put
scan-policy state on the Users screen. Flagged for `sdd-spec`.

The `p: policy` help entry lengthens `adminFeatureHelp` (`admin_views.go:595-607`)
by ~11 columns. No arithmetic change is required: `contentBudget` measures help
with `lipgloss.Height(theme.help.Render(help))` (`viewport.go:123`), so if it
ever wraps, chrome grows and `SectionRows` shrinks automatically.

## Data Flow

    PULL   router.handleManifest ─> Service.OpenManifest
             authorize ─> store.ResolveManifest ─> enforceScanPolicy
                                                     ├─ GetScanPolicySettings (NotFound -> default ON/CRITICAL)
                                                     └─ GetLatestScanRunByDigest (NotFound -> allow)
                                                          └─ scanPolicyViolated -> PolicyViolation -> writeError 403

    PUSH   router.handleManifest ─> Service.PublishManifest ─> store.PublishManifest ─> 201 returned
                                          └─ go queuePushScan(bg, tenant, repo, digest)
                                               settings.Enabled? ─> GetActiveScanRunByDigest (dedup) ─> UpsertScanRun ─> go executeScanRun

    CI     router.handleManifestScanStatus ─> ActionPull ─> same two lookups ─> 200 {state, would_block_pull}

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/ports/regixtry.go` | Modify | `ScanPolicySettings`, 2 threshold constants, `ScanTriggerPush`, 3 `MetadataStore` methods |
| `internal/infra/metadata/sqlite/store.go` | Modify | `scan_policy_settings` DDL in `init()`; `Get/UpsertScanPolicySettings`; `GetLatestScanRunByDigest` |
| `internal/domain/regixtry/errors.go` | Modify | `ErrorCodePolicyViolation` + `NewPolicyViolationError` |
| `internal/app/regixtry/queries.go` | Modify | Gate call in `OpenManifest` |
| `internal/app/regixtry/service.go` | Modify | Push auto-queue goroutine in `PublishManifest` (**not** `commands.go` — no such file) |
| `internal/app/regixtry/service_scanning.go` | Modify | `Get/UpdateScanPolicySettings`, `enforceScanPolicy`, `scanPolicyViolated`, `queuePushScan` |
| `internal/protocol/http/router.go` | Modify | 403 case in `writeError`; `scan-status` dispatch; `handleManifestScanStatus` |
| `internal/protocol/http/admin_handlers.go` | Modify | `scan-policy` GET/PUT case + decode/response helpers |
| `internal/tui/session.go` | Modify | `scanPolicyModal` struct + field enum + `Active()`; `AdminViewState.ScanPolicyModal`, `ScanPolicy` |
| `internal/tui/admin_views.go` | Modify | `renderScanPolicyModal`; badge composed into `renderTrivyTabs`; `p` in `adminFeatureHelp` |
| `internal/tui/model.go` | Modify | `p` key handling, save command, overlay branch |

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | `scanPolicyViolated` over all 7 rows of Decision 2's table | Table-driven, no store |
| Unit | `GetLatestScanRunByDigest` returns the newest run **regardless of status**, incl. `completed` and `failed` | Store test seeding queued+completed+failed for one digest |
| Unit | `GetLatestScanRunByDigest` returns typed `ErrorCodeNotFound`, not a zero `ScanRun` | `domain.IsCode` assertion |
| Unit | `GetScanPolicySettings` returns enabled/CRITICAL with **no row present** | Fresh `t.TempDir()` store |
| Unit | Upsert round-trips both thresholds and toggles enabled | Table-driven |
| Unit | `renderScanPolicyModal` ≤ 13 rows with and without an error; height invariant across focus | `lipgloss.Height` |
| Unit | `renderTrivyTabs` still returns exactly 2 rows with the badge, width ≤ `sectionWidth(150)` | `lipgloss.Height` + `lipgloss.Width` |
| Unit | Badge text reflects enabled/threshold (`Policy: ON (CRITICAL+HIGH)` / `OFF`) | Table-driven |
| Integration | `OpenManifest` 403s on completed+critical at CRITICAL; allows at CRITICAL when only High | Service test |
| Integration | `OpenManifest` allows for queued, running, failed, never-scanned, and policy-disabled | Service test, 5 cases |
| Integration | `ResolveManifest` allows the same violating digest the gate blocks | Service test — the browse-path regression guard |
| Integration | `writeError` maps `ErrorCodePolicyViolation` → 403 with no `WWW-Authenticate` | Router test |
| Integration | `PublishManifest` returns 201 and creates a queued push run; second push does not duplicate | Service test on `queuePushScan` directly (see risk 2) |
| Integration | `PublishManifest` queues nothing when `ScanSettings.Enabled` is false | Service test |
| Integration | scan-status returns all 5 states; reachable with pull-only credentials; 401 without | Router test, table-driven |
| Integration | A tag named `scan-status` still routes to `handleManifest` | Router test |
| Integration | Admin `PUT /admin/v1/scan-policy` rejects an unknown threshold with 400 | Handler test |

## Threat Matrix

Included because this design changes HTTP path dispatch. No shell, subprocess,
VCS/PR, or executable-file boundary is touched.

| Boundary | Applicability | Design response | Planned RED test |
|---|---|---|---|
| Documentation-like paths | N/A — no file-classification or execution boundary | — | — |
| Git repository selection | N/A — no VCS invocation | — | — |
| Commit state | N/A — no VCS invocation | — | — |
| Push state | N/A — "push" here is an OCI manifest PUT, not a Git push | — | — |
| PR commands | N/A — no PR automation | — | — |
| **HTTP path dispatch (added row)** | **Applicable** — new suffix route under `manifests/` | Suffix matched on the **trimmed reference**, not on `suffix`; OCI refs contain no `/` | Tag named `scan-status` routes to `handleManifest`; `<ref>/scan-status` routes to the status handler |
| **Authorization scope of the new route (added row)** | **Applicable** — first non-admin scan-data route | `ActionPull` through the existing `withPrincipal`; no admin data (findings, tokens, other repos) in the payload | Pull-only credential succeeds; no credential 401s; credential for another repository is refused |

## Migration / Rollout

Additive only. `CREATE TABLE IF NOT EXISTS` in the existing `init()` statement
list, whose loop already tolerates re-runs and swallows `duplicate column name`
(`store.go:1144-1152`). No `ALTER TABLE` is needed because the table is new. No
manifest, blob, or existing-column change. A reverted binary leaves
`scan_policy_settings` orphaned and unread, and pulls behave exactly as today.

Behavioral rollout note: the gate is **default ON**, so an upgraded registry
holding completed critical-severity scans begins returning 403 on those pulls
immediately. Fail-open semantics bound the blast radius to digests with a
confirmed completed violating scan; everything unscanned, in-flight, or failed
is unaffected.

## Open Questions

- [ ] **Live-render confirmation (needs a real terminal).** The badge's row cost
      and 55-column width are derived arithmetically from `renderTrivyTabs` and
      `sectionWidth`; the composed line has not been rendered. `sdd-verify` should
      confirm at 150×24 that the tab line does not wrap and that the modal shows
      its full bottom border and help line.
- [ ] **Live-DB confirmation.** The DDL and both queries are written against the
      read schema but have not been executed. `sdd-apply` should confirm
      `init()` is idempotent on an existing database (the `duplicate column name`
      guard does not cover a `CREATE TABLE` conflict — `IF NOT EXISTS` must carry
      it alone) and that `ORDER BY created_at DESC` is stable when two runs share
      a `created_at` string to nanosecond precision (add `id` as a tiebreaker if
      not).
- [ ] Badge visibility is Trivy-feature-page-scoped (Decision 6). Confirm against
      the spec's wording for "persistent policy badge".
- [ ] Should `p` also be bound on the Repository Alerts tab, or Runtime only?
