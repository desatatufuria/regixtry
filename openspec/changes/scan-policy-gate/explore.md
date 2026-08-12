# Exploration: Harbor-style vulnerability policy gate (scan-policy-gate)

## Goal

Add a Harbor-style vulnerability policy gate to regixtry: pushes are never
blocked (scanning stays async, as it already is today); a policy check
happens at pull/GET-manifest time, comparing the digest's latest completed
Trivy scan against a configured severity threshold, and rejects the pull if
violated. Confirmed via live web search that this mirrors Harbor's own
"Prevent vulnerable images from running" feature exactly (async scan, gate
on pull, not on push, `PROJECTPOLICYVIOLATION`-style rejection).

Decided requirements (from product conversation, not re-litigated here):

1. Default threshold: CRITICAL.
2. Severity threshold is configurable (at least CRITICAL, and CRITICAL+HIGH).
3. Configurable via both the HTTP admin API and the TUI.
4. Active by default on a fresh install.
5. TUI must visually indicate when the policy is active.
6. A pull must not be blocked just because a scan hasn't completed yet — the
   gate only fires once a scan has actually completed and found something
   at/above the threshold. A voluntary "scan status" read path should exist
   for CI pipelines that want to poll for a verdict before deploying.

## Current State

**Scan data model** (`internal/ports/regixtry.go`): `ScanRun` (line 141)
carries `Critical/High/Medium/Low int` and `Status`
(`ScanRunStatusQueued|Running|Completed|Failed`). `MetadataStore.
GetActiveScanRunByDigest` (`regixtry.go:36`, impl
`internal/infra/metadata/sqlite/store.go:564`) is misleadingly named — its
SQL filters `status IN (queued, running)` only, so it can **never** return a
completed run. No existing method answers "what's the latest scan for this
digest, any status." A new `GetLatestScanRunByDigest` is required; no schema
change needed, `scan_runs` already has every column.

Pushing an image does **not** auto-trigger a scan today (confirmed by
`TestServicePublishManifestDoesNotTriggerSecretScan` and no scan-trigger
call inside `PublishManifest`). Scans start only via manual admin trigger or
the scheduler sweep (default interval 24h). A freshly pushed digest can stay
unscanned for up to that whole interval.

**Pull path**: `internal/protocol/http/router.go:295 handleManifest` →
GET/HEAD → `Service.OpenManifest` (`internal/app/regixtry/queries.go:71`) is
the sole path for actual pulls (`router.go:327`). The sibling
`Service.ResolveManifest` (`queries.go:48`) is used only by the TUI browse
feature (`internal/tui/model.go:2032`) for inspection — must stay unblocked.
`OpenManifest` already resolves the digest right where the gate needs it.

`ports.AccessController` (`internal/ports/regixtry.go:396`) has no digest
parameter and is purely identity-based — extending it for a content-based
policy check is a poor fit. **Insert the check directly in
`Service.OpenManifest`.**

**Settings pattern**: `ScanSettings`/`FeatureConfigureInput` is tightly
coupled to `featureDescriptor`/`FeatureKind`
(`internal/app/regixtry/feature_registry.go:22-44`), hardcoded to exactly 2
managed-runtime features (trivy, gitleaks) with install/upgrade/rollback
lifecycle fields meaningless for a policy toggle. **Recommend a new sibling
`ports.ScanPolicySettings` type** with its own
`Get/UpsertScanPolicySettings` methods, using the same additive-column SQL
idiom already established (`store.go:996-1019`).

**HTTP error convention**: `writeError` (`router.go:552`) has no case
mapping to 403 today. A new `ErrorCodePolicyViolation` + constructor + one
new switch case (→ `StatusForbidden`) is a small, convention-consistent
addition mirroring every other domain error code.

**CI-polling auth gap**: every `/admin/v1/*` route, including the existing
scan-run lookups, requires full admin auth
(`internal/protocol/http/admin_handlers.go:17-26`). A CI pipeline with
ordinary push/pull credentials cannot reach it. Requirement 6's "voluntary
polling" needs a **new registry-scoped endpoint** (reusing
`ActionPull`/`ActionInspect` principal checks), not the admin API.

**TUI**: `trivyConfigModal` (`internal/tui/session.go:111`, rendered by
`internal/tui/admin_views.go:579-593`) is hard-capped at ≤20 rows by
`TestRenderTrivyConfigModalFitsWithinCompactedRowBudget`
(`admin_views_test.go:165-203`), just compacted from 27. Adding fields there
risks that budget — give the policy gate its own small modal instead. **No
lock-icon or checklist-marker vocabulary exists anywhere in the admin TUI**;
the ✓/▸/○ glyphs from `tui-design-polish` only exist in the unrelated CLI
install progress bar and in openspec markdown task lists. All existing
booleans render as plain `"true"/"false"` text. A lock icon would be
genuinely new visual language, not reuse of precedent.

## Affected Areas

- `internal/ports/regixtry.go` — new `ScanPolicySettings` type,
  `GetLatestScanRunByDigest`/`Get/UpsertScanPolicySettings` interface
  methods, new error code.
- `internal/infra/metadata/sqlite/store.go` — new query + new settings
  table/columns (additive `ALTER TABLE` pattern already established).
- `internal/app/regixtry/queries.go` (`OpenManifest`) — gate insertion
  point.
- `internal/domain/regixtry/errors.go` — new `ErrorCodePolicyViolation`.
- `internal/protocol/http/router.go` (`writeError`) — new 403 case.
- `internal/protocol/http/admin_handlers.go` — new admin GET/PATCH for
  policy settings; possibly a new non-admin route for CI polling.
- `internal/tui/session.go`, `internal/tui/admin_views.go`,
  `internal/tui/model.go` — new small config modal + TUI indicator.
- `internal/tui/admin_tables.go` — possible new column/indicator on
  Repository Alerts summary (width-budget risk, recently tuned).

## Approaches Considered

1. **Gate inline in `Service.OpenManifest`, new sibling settings type**
   (recommended). Minimal blast radius, reuses established idioms, keeps
   Trivy modal budget intact, correct auth scope for the future CI
   endpoint. Two small new interface methods to implement/test; a second
   settings table/row to introduce. Effort: Medium.
2. **Extend `AccessController` to carry digest + wire the policy check
   through the authorize seam.** Rejected: mixes identity auth with content
   policy, touches more files for no real benefit, forces both
   `AccessController` implementations to know about vulnerability policy.
3. **Reuse `ScanSettings`/`FeatureConfigureInput` under a
   `feature='vulnerability-policy'` row.** Viable fallback if zero new
   tables is preferred. Zero new interface surface, same PATCH machinery,
   but conceptually messy (carries meaningless managed-runtime fields) and
   doesn't solve the Trivy-modal row-budget problem either way.

## Recommendation

Approach 1. Split delivery into two cohesive pieces for `sdd-tasks` to size:
(a) core gate — settings type, latest-scan lookup, `OpenManifest` check,
403 error, admin API + TUI config + indicator; (b) the CI-poll endpoint,
which touches a different (non-admin) auth surface and is explicitly
voluntary — fine as a follow-up slice in the same change if the 800-line
budget allows, or its own change if not.

## Risks

- `GetActiveScanRunByDigest`'s current (queued/running only) semantics
  could be mistaken for "latest scan" by anyone extending this code without
  reading the SQL — must not be reused for the gate.
- Trivy config modal is at its row-budget ceiling; adding policy fields
  there would break `TestRenderTrivyConfigModalFitsWithinCompactedRowBudget`.
- No scan-on-push trigger today means the gate has weak real-world bite for
  brand-new digests until the next scheduler sweep (default 24h).
- Admin-only auth on all existing scan-run endpoints means the CI-polling
  half of requirement 6 needs a genuinely new, differently-scoped route.
- Adding a "Policy" column/indicator to `buildAdminScanSummaryTable` adds
  terminal-width pressure on a table whose column widths were just tuned.

## Open Questions Requiring a Product Decision

1. Should push auto-queue a (still non-blocking) scan so the gate has real
   teeth quickly, or stay manual/scheduled-only as today?
2. Lock icon vs. plain-text policy indicator in the TUI — there is no
   existing icon vocabulary to reuse.
3. Should a `failed` (errored) scan block the pull, or be treated like "no
   scan yet" (allow)? Not covered by the 6 decided requirements.

## Ready for Proposal

Yes, with the three open questions above resolved first (or explicitly
deferred into the proposal as flagged decisions).
