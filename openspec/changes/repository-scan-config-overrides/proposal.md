# Proposal: Per-Repository Scan Config Overrides (repository-scan-config-overrides)

## Intent

Scan configuration is one global row per tenant+feature (`loadFeatureSettings`,
`feature_registry.go:237`); `domain.RepositoryRef` is a bare `{Name string}` and
no per-repository settings concept exists anywhere (explore.md "Current State").
Operators cannot say "this repository ignores CVE-2023-x" or "this repository
uses its own `.gitleaks.toml`" without changing scanning for every repository.
Mirror Harbor's system-default + per-project-override model for admin/security
config, with a mechanism generic enough that a future image-signing feature
reuses it with zero migration.

## Scope

### In Scope
- Generic override storage + resolution: one table
  `repository_feature_overrides(tenant, repository, feature_name, payload, updated_at)`
  with a per-feature typed JSON payload marshaled by each feature module.
- **Full-row-replace** resolution: override row present → all its typed fields
  apply; absent → the global `ScanSettings` row applies in full. Same
  row-presence boundary as `loadFeatureSettings` (NotFound is "use the global",
  never an error). No sparse per-field merge across the boundary.
- Trivy override fields: `Enabled`, `IgnoreFilePath` (`.trivyignore`),
  `IgnorePolicyPath` (Rego) + new `--ignorefile`/`--ignore-policy` plumbing in
  `trivy/runner.go`.
- Gitleaks override fields: `Enabled`, `ConfigPath` (`.gitleaks.toml`) + new
  `--config` plumbing in `gitleaks/runner.go` (no such flag exists today).
- Admin HTTP GET/PUT/DELETE for one repository+feature override; DELETE removes
  the row and reverts that repository to global.
- New TUI override modal on a highlighted Repository Alerts row, bound to
  `o` (free: `p`, `c`, `e`, `x`, `i`, `u`, `b`, `r`, `q`, `l`, `j`/`k` are
  taken on that screen — `model.go:1188-1254`, `featureActionForKey:2615`).

### Out of Scope
- Image-signing itself — no fields, no code. The JSON payload exists so signing
  needs no migration later; that is deliberate forward-compatibility, not design.
- `ScanPolicySettings` (the pull-gate enabled/threshold) **stays global-only**.
  Its original "matches `ScanSettings` granularity" justification goes stale here,
  but the decision itself does not change: the gate is not becoming per-repo.
- Sparse per-field override; severity knob at the override level (severity stays
  in the global pull gate); `Timeout`/`MaxConcurrency` per repo (global-only
  tuning knobs, kept out to hold this slice minimal).

## Capabilities

### New Capabilities
- `repository-config-overrides`: generic per-repository feature override storage,
  full-row-replace resolution with global fallback, admin GET/PUT/DELETE.

### Modified Capabilities
- `feature-configuration`: settings resolution MUST consult a repository override
  before the global row.
- `repository-vulnerability-scans`: Trivy runs MUST honor per-repo ignore file /
  ignore policy.
- `image-secret-scans`: gitleaks runs MUST honor a per-repo config path.
- `operator-admin-tui`: operator MUST view, set, and clear a repository's
  override from the Repository Alerts row.

## Approach

Approach 3 from explore.md (generic table, typed JSON payload) — the only option
satisfying "not trivy/gitleaks-specific in storage shape" without the EAV
type-safety loss of Approach 1 or the per-feature migration debt of Approach 2.
Store stays feature-agnostic:
`GetRepositoryOverride(tenant, repository, feature) → (payload, found, err)`;
each feature module owns marshal/unmarshal of its typed struct, mirroring how
`normalizeScanSettings` owns `ScanSettings` today. The JSON column is a
deliberate, scoped exception to the store's typed-column/`ALTER TABLE ADD COLUMN`
idiom — `sdd-design` documents it as such. Resolution is injected where
`queueScheduledScan`/`queuePushScan` fetch the global row before
`executeScanRun`/`executeSecretScanLeg` (`service_scanning.go:295,346`), keyed by
`run.Repository`. New modal is a sibling of `scanPolicyModal`, not an extension
of `trivyConfigModal`.

## Affected Areas

| Area | Impact | Description |
|---|---|---|
| `internal/ports/regixtry.go` | Modified | Override types + 3 `MetadataStore` methods |
| `internal/infra/metadata/sqlite/store.go` | Modified | New `repository_feature_overrides` table |
| `internal/app/regixtry/feature_registry.go` | Modified | Override-aware resolution |
| `internal/app/regixtry/service_scanning.go` | Modified | Repository-keyed settings at queue time |
| `internal/infra/scanning/trivy/runner.go` | Modified | `--ignorefile` / `--ignore-policy` argv |
| `internal/infra/scanning/gitleaks/runner.go` | Modified | `--config` argv |
| `internal/protocol/http/admin_handlers.go`, `router.go` | Modified | Override GET/PUT/DELETE routes |
| `internal/tui/session.go`, `model.go`, `admin_views.go` | Modified | `o` binding + override modal |

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| **Per-repo Trivy override changes global pull-gate outcomes for that repo** (see note below) | High | Documented as intended; spec asserts it; TUI modal states the consequence |
| JSON column is a new pattern in a 100%-typed-column store | Med | Scoped exception documented in design; marshal/unmarshal at one boundary only |
| Override row silently applies stale/wrong config after global change | Med | Full-row-replace is explicit; TUI shows "override active" vs "global" |
| Missing/unreadable `IgnoreFilePath`/`ConfigPath` at scan time | Med | Resolve behavior in spec (see open question 1); never silently pass a bad path |
| New modal lengthens the `Active()` dispatch chain (`model.go:1064`) | Low | Own small modal per `scanPolicyModal` precedent; `o` verified free |
| Override rows orphaned when a repository disappears | Low | Resolution is lookup-by-key; orphan rows are inert (see open question 3) |

### Documented consequence: overrides reach the pull gate

A per-repository Trivy override's `IgnoreFilePath`/`IgnorePolicyPath` changes what
Trivy **counts** as a Critical/High finding for that repository's `ScanRun`. The
existing global `ScanPolicySettings` pull gate (`scan-policy-gate`, shipped) reads
exactly those counts (`service_scanning.go:56-62`, `scanPolicyViolated`).
Therefore a per-repository config override **can change pull-gate outcomes for
that repository**, even though the two settings are stored and administered
separately. This is intended: an operator who tells Trivy to ignore a finding for
repository X should expect the gate to stop seeing it for repository X. It is
stated here because storage separation is not behavioral separation, and a reader
must not assume independence. Separately and explicitly: `ScanPolicySettings`
itself (gate enabled + severity threshold) remains global-only and unchanged by
this work.

## Rollback Plan

`git revert` the change commits. Storage is purely additive
(`CREATE TABLE IF NOT EXISTS`), so a reverted binary ignores any override rows and
every repository resolves to the global `ScanSettings` row exactly as today.
Runner argv additions are conditional on override presence — no override, no flag,
identical command line. No manifest, blob, or existing-table change.

## Dependencies

- Existing global `ScanSettings` resolution (`loadFeatureSettings`) and the Trivy
  and gitleaks scan pipelines.
- `scan-policy-gate` (shipped) — read-only relationship; not modified.
- No new external dependency.

## Success Criteria

- [ ] Repository with no override row scans with the global settings, unchanged.
- [ ] Setting a Trivy override for one repository leaves every other repository's
      scans byte-identical in argv and outcome.
- [ ] Trivy override adds `--ignorefile`/`--ignore-policy` to that repository's
      run only; gitleaks override adds `--config` to that repository's run only.
- [ ] DELETE on the override reverts that repository to the global settings.
- [ ] A future feature can store an override with no schema migration (payload
      round-trips through the same table).
- [ ] `o` on a Repository Alerts row opens the modal bound to that repository;
      set and clear round-trip through the admin API and reflect in the TUI.
- [ ] Gate behavior for a repository with an ignore-file override is asserted by a
      test, not assumed.

## Proposal question round — resolved

Decided before this proposal; encoded above so `sdd-spec`/`sdd-design` do not
re-derive them: (1) generic single table with typed JSON payload; (2)
full-row-replace granularity; (3) Trivy fields `Enabled`/`IgnoreFilePath`/
`IgnorePolicyPath`, no severity, no timeout/concurrency; (4) gitleaks fields
`Enabled`/`ConfigPath`, no schedule, no timeout/concurrency; (5) TUI entry via a
new key on a Repository Alerts row; (6) DELETE clears the override.

### Open product questions for user review

1. **Path semantics and failure mode**: are these paths server-local files
      readable by the scanner runtime, and when a path is missing/unreadable at
      scan time does the run FAIL or run without the flag plus a recorded warning?
      *Assumption if unanswered*: server-local path, validated non-empty on write,
      scan FAILS loudly rather than silently scanning with weaker rules.
2. **Authorization**: same admin authz as existing feature config, or narrower?
      *Assumption*: identical to existing admin feature-config authz.
3. **Repository lifecycle**: are override rows cleaned up when a repository is
      deleted/renamed? *Assumption*: rows are inert orphans; no cascade in this slice.
4. **`Enabled=false` semantics**: does it suppress scheduled scans only, or
      push-triggered scans too, and how does Repository Alerts show it?
      *Assumption*: suppresses both; row renders as "scanning disabled", not empty.
