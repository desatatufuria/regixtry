# Proposal: Secret-Scan Status Endpoint (secret-scan-status)

## Intent

Verified (`internal/protocol/http/admin_handlers.go:581-599`): the only way to learn whether a pushed image
carries leaked secrets is `GET /admin/v1/secret-scan-findings`, gated behind `requireAdminPrincipal` — a
credential CI pipelines do not and should not hold. Meanwhile the two sibling checks CI already relies on,
vulnerability scan (`scan-status`) and signature (`signature-status`), are both exposed under plain
`ActionPull` (`router.go:145-178`, `389-434`). Secret-scan detection exists but its verdict is unreachable
from an ordinary pull-gated CI check — a parity gap, not a missing feature.

Success: a CI job holding only pull credentials for a repository can ask "does this reference have leaked
secrets" and get a compact verdict, the same way it already asks about vulnerabilities and signatures.

## Scope

### In Scope

- `GET /v2/<repo>/manifests/<ref>/secret-scan-status` — gated by plain `ports.ActionPull` on that repository,
  structurally mirroring `scan-status`/`signature-status`.
- Route dispatch: third `strings.HasSuffix(reference, "/secret-scan-status")` branch in `handleV2`, checked
  against the trimmed reference before the `handleManifest` fallthrough — same anti-collision technique as the
  two existing branches.
- `handleManifestSecretScanStatus` handler mirroring `handleManifestScanStatus` (405+Allow on non-GET,
  `withPrincipal` authorization, `writeError`/`writeJSON` conventions).
- `Service.SecretScanStatus(ctx, repositoryName, reference)` in `internal/app/regixtry/queries.go`, internally
  reusing the existing `s.GetSecretScanFindings` → `ListSecretScanRuns` digest-match path — **not**
  `GetActiveSecretScanRunByDigest`, which only tracks queued/running rows for dedup and would misreport
  completed scans as `unscanned`.
- Compact verdict shape: `Repository`, `Reference`, `Digest`, `State`, `Policy{Enabled}`, and (when a run
  exists) `Scan{Status, FindingCount, FinishedAt}`. No `would_block_pull`, no severity threshold — gitleaks
  never gates a pull (`service_scanning.go:324-328`).
- States: `unscanned`, `in_progress`, `failed`, `clean`, `findings_present`.
- Dedicated test files for app and HTTP layers, mirroring `signature_status_test.go` (all-states,
  route-collision guard, pull-authorization-required).
- One `docs/api.md` table row after line 157.

### Out of Scope

- `/admin/v1/secret-scan-findings` stays exactly as-is — additive change, not a replacement. No change to its
  auth gate or response shape.
- No new severity/blocking model for secrets — gitleaks findings never fail a push or pull today, and this
  endpoint does not introduce that.
- No pagination or per-finding detail in the new endpoint — verdict only, matching the sibling endpoints.

## Capabilities

### New Capabilities

- `secret-scan-status`: CI-facing, pull-gated compact verdict for whether a manifest reference has known
  secret-scan findings, structurally parallel to the existing (unspecced) `scan-status`/`signature-status`
  behavior.

### Modified Capabilities

None — the admin findings endpoint's behavior and gate are unchanged.

## Approach

Third mirror of an already-shipped, twice-repeated pattern (recommended in exploration over extending the
admin route, which was rejected: it would break the blanket admin gate and leak unredacted findings to CI
callers instead of a compact verdict).

| Decision | Approach | Why |
|---|---|---|
| Query reuse | Reuse `GetSecretScanFindings`'s `ListSecretScanRuns` + digest-match, not `GetActiveSecretScanRunByDigest` | Latter is a queued/running-only dedup guard, not a latest-run lookup — confirmed by its SQL filter |
| Response shape | Compact verdict, no `would_block_pull`, no severity threshold | gitleaks never gates anything; those fields would be always-`false`/meaningless |
| Auth | Plain `ports.ActionPull`, identical to the two siblings | No new auth model; secrets are as pull-relevant as vulns/signatures |
| Admin endpoint | Left untouched | Different audience (operators, full findings) vs. this endpoint (CI, verdict only) |

## Affected Areas

| Area | Impact | Description |
|---|---|---|
| `internal/app/regixtry/queries.go` | Modified | `SecretScanStatusResult`/`SecretScanStatusScan` structs, state consts, `Service.SecretScanStatus` |
| `internal/protocol/http/router.go` | Modified | New `handleV2` branch + `handleManifestSecretScanStatus` handler |
| `internal/app/regixtry/secret_scan_status_test.go` | New | App-layer state/query coverage |
| `internal/protocol/http/secret_scan_status_test.go` | New | Route-collision, auth, and status-code coverage |
| `docs/api.md` | Modified | One table row documenting the new endpoint |

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Reaching for `GetActiveSecretScanRunByDigest` by name, misreporting completed scans as `unscanned` | Med | Exploration flags this explicitly; task-level test asserts a completed run yields `clean`/`findings_present`, not `unscanned` |
| Route branch ordering collides with a tag literally named `secret-scan-status` | Low | Reuses the proven trimmed-reference check already guarding the two sibling branches; add a collision test |
| `SecretScanRunStatusQueued` mapped but never persisted in practice, going untested | Low | Map for symmetry; note as acceptable if unreachable in current scan pipeline |

## Rollback Plan

`git revert` the change commit. No schema migration and no new config flag — the endpoint is purely additive
route dispatch plus a read-only query method. A reverted binary simply stops answering the new route (falls
through to the existing 404/`MANIFEST_UNKNOWN` manifest-not-found path); no data or behavior elsewhere changes.

## Dependencies

- `registry-foundation` (shipped) — `/v2` router dispatch and pull authorization are reused, not replaced.
- Existing gitleaks scan pipeline and `ports.MetadataStore.ListSecretScanRuns` (shipped) — read-only reuse.
- No new external dependency.

## Success Criteria

- [ ] A pull-authorized caller gets a correct verdict for each of the five states on a real digest.
- [ ] A completed scan (clean or with findings) never reports `unscanned`, even when a stale queued/running
      row also exists for the same repository.
- [ ] A caller without pull authorization on the repository is rejected the same way `scan-status` rejects one.
- [ ] `/admin/v1/secret-scan-findings` behavior, response shape, and auth gate are provably unchanged.
- [ ] A tag literally named `secret-scan-status` still routes to `handleManifest`, not the new handler.
- [ ] `docs/api.md` documents the new endpoint alongside its two siblings.

## Proposal question round — resolved

The task brief already resolves the exploration's open questions, so no live round was needed:
(1) omit `would_block_pull` and severity threshold — gitleaks never gates; (2) keep `Policy{Enabled}` only;
(3) states are `unscanned`, `in_progress`, `failed`, `clean`, `findings_present`; (4) reuse
`GetSecretScanFindings`, never `GetActiveSecretScanRunByDigest`; (5) admin endpoint stays untouched.

### Open questions for `sdd-spec`/`sdd-design`

1. Field name for the finding count — `finding_count` (matches `ports.SecretScanRun.FindingCount` 1:1) vs.
   `findings_count`.
2. Whether `SecretScanRunStatusQueued` needs an explicit test given it may be unreachable from the current
   scan pipeline, or whether mapping it in the switch without a dedicated test is acceptable.
