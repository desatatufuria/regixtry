# Proposal: Vulnerability Policy Gate (scan-policy-gate)

## Intent

regixtry scans images but never acts on the result: a digest with known
CRITICAL CVEs pulls exactly like a clean one. Operators cannot stop vulnerable
images from reaching runtime. Mirror Harbor's "Prevent vulnerable images from
running": never block push, gate the pull.

## Scope

### In Scope
- Global `ScanPolicySettings` (enabled + severity threshold); fresh install
  defaults ON / CRITICAL; thresholds CRITICAL and CRITICAL+HIGH.
- Pull-time gate in `Service.OpenManifest` → 403 policy violation when the
  digest's latest **completed** scan meets or exceeds the threshold.
- Fail-open on uncertainty: no scan, queued, running, or FAILED never blocks.
  Fail-closed only on a confirmed completed violating scan.
- Push auto-queues a non-blocking scan (existing async path + in-flight dedup),
  so the gate has bite in minutes instead of a 24h scheduler gap.
- Admin HTTP GET/PATCH for policy; TUI config surface plus a colored text badge
  (e.g. `Policy: ON (CRITICAL)`).
- New registry-scoped (non-admin) scan-status endpoint so CI holding ordinary
  pull credentials can poll a verdict before deploying.

### Out of Scope
- Per-repository or per-tenant policy variation. Deliberate scoping decision:
  one global policy, matching existing `ScanSettings` granularity.
- OCI Referrers API, SBOM, cosign/signature policy — separate future features.
- `ResolveManifest` and the TUI browse path: inspection stays unblocked.
- Blocking push; new fields inside `trivyConfigModal`; any glyph/icon
  vocabulary (text badge only).

## Capabilities

### New Capabilities
- `vulnerability-policy-gate`: policy settings and defaults, pull-time
  enforcement, fail-open semantics, push-triggered scan, CI scan-status path.

### Modified Capabilities
- `operator-admin-tui`: operator MUST view and change policy state/threshold
  from the TUI and MUST see a persistent policy badge.

## Approach

New sibling `ports.ScanPolicySettings` + `Get/UpsertScanPolicySettings` via the
established additive-SQL idiom — **not** `FeatureConfigureInput`, which is bound
to the two managed-runtime features' install/upgrade/rollback lifecycle. New
`MetadataStore.GetLatestScanRunByDigest` (latest run, any status);
`GetActiveScanRunByDigest` filters `queued|running` only and can never answer
this — reuse it solely for push dedup. Gate inlined in `OpenManifest` (sole pull
path), not `AccessController` (identity-based, no digest). New
`ErrorCodePolicyViolation` + one `writeError` case → 403. Policy gets its own
small TUI modal; badge sits at feature-page/header level, not as a table column
(`sdd-design` confirms placement).

## Affected Areas

| Area | Impact | Description |
|---|---|---|
| `internal/ports/regixtry.go` | Modified | `ScanPolicySettings`, 3 store methods |
| `internal/infra/metadata/sqlite/store.go` | Modified | Latest-scan query, policy settings storage |
| `internal/app/regixtry/queries.go` | Modified | Gate in `OpenManifest` |
| `internal/app/regixtry/commands.go` | Modified | Push auto-queue with dedup |
| `internal/domain/regixtry/errors.go` | Modified | `ErrorCodePolicyViolation` |
| `internal/protocol/http/router.go` | Modified | 403 case; scan-status route |
| `internal/protocol/http/admin_handlers.go` | Modified | Policy GET/PATCH |
| `internal/tui/*` | Modified | Policy modal + badge |

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| `GetActiveScanRunByDigest` misused as "latest scan" | High | New explicit method; test asserting completed runs are returned |
| Default-ON gate breaks existing pulls after upgrade | Med | Fail-open semantics; only completed violating scans block; documented 403 |
| Push auto-queue floods the scanner | Med | Reuse in-flight dedup before queueing |
| TUI badge/table width regression | Med | Header-level badge, own modal; `trivyConfigModal` untouched |
| Non-admin scan-status route leaks data | Med | Reuse `ActionPull`/`ActionInspect` principal checks |

## Rollback Plan

`git revert` the change commits. Storage additions are additive
(`CREATE TABLE IF NOT EXISTS` / guarded `ALTER TABLE`), so a reverted binary
ignores the policy rows and pulls behave exactly as today. No destructive
migration, no manifest or blob format change.

## Dependencies

- Existing Trivy scan pipeline (`ScanRun`, scheduler, `QueueManualScan`).
- No new external dependency.

## Success Criteria

- [ ] Fresh install reports policy enabled at CRITICAL with no configuration.
- [ ] Pull of a digest whose latest completed scan has ≥1 critical returns 403.
- [ ] Pull is allowed when the digest has no scan, a queued/running scan, or a
      failed scan.
- [ ] Threshold switches between CRITICAL and CRITICAL+HIGH via admin API and
      TUI, and changes gate outcomes accordingly.
- [ ] Push returns immediately and a scan run exists for the new digest without
      duplicating an in-flight run.
- [ ] Non-admin pull credentials read a scan verdict from the scan-status route.
- [ ] TUI shows the policy badge; `ResolveManifest`/browse stays unblocked.

## Proposal question round — resolved

The exploration's three open questions were resolved by the user before this
proposal; encoded above so `sdd-spec`/`sdd-design` do not re-derive them.

1. **Auto-queue scan on push?** Yes, non-blocking, reusing the existing async
   queue path and in-flight dedup.
2. **Icon or text for the TUI indicator?** Colored text badge. No icon
   vocabulary exists in the admin TUI today; do not invent one here.
3. **Does a FAILED scan block?** No. Identical to "no scan yet" — fail-open.

### Assumptions needing user review
- All seven decided points ship as one cohesive change; `sdd-tasks` may slice
  the CI scan-status endpoint into a chained PR if the line budget is tight.
- Threshold values are limited to CRITICAL and CRITICAL+HIGH for this slice.
