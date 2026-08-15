# Exploration: secret-scan-status CI-facing endpoint (parity gap fix)

## Current State (verified)

Two existing CI-facing status endpoints share one proven shape:

**Route dispatch** — `internal/protocol/http/router.go:145-178` (`handleV2`): once `suffix` starts with `manifests/`, the trimmed `reference` is checked with `strings.HasSuffix(reference, "/scan-status")` (line 161), then `"/signature-status"` (line 169), before falling through to `handleManifest` (line 173). The comment at 155-160 explains why the check is against the *trimmed reference*, not the raw suffix — it avoids misrouting a tag literally named `scan-status`.

**Handlers** — `handleManifestScanStatus`/`handleManifestSignatureStatus` (router.go:389-434): reject non-GET (405 + `Allow`), build `action := ports.Action{Verb: ports.ActionPull, Repository: repository}`, call `r.withPrincipal(w, req, action)`, call `r.service.*Status(ctx, repository, reference)`, on error `writeError(w, req, err, r.challengeForError(action, err), "MANIFEST_UNKNOWN")`, else `writeJSON(w, 200, result)`.

**Service methods** — `internal/app/regixtry/queries.go`: `Service.ScanStatus` (155-206) and `Service.SignatureStatus` (259+) both: `parseRepository` → `s.authorize(ctx, ports.Action{Verb: ports.ActionPull, ...})` → `s.metadata.ResolveManifest` → digest → policy lookup → digest-scoped latest-run lookup (NotFound is fail-open to `unscanned`/`unsigned`) → 5-state verdict. Response shapes are compact: `ScanStatusResult{Repository,Reference,Digest,State,WouldBlockPull,Policy{Enabled,SeverityThreshold},Scan *{Status,Critical,High,Medium,Low,FinishedAt}}` — never a per-vulnerability list.

**Secret-scan exposure today** — admin-only. `handleAdminSecretScanFindings` (`internal/protocol/http/admin_handlers.go:581-599`) sits behind the blanket `requireAdminPrincipal` gate on all `/admin/v1/*`, with no per-route `ActionPull` check. `Service.GetSecretScanFindings` (`internal/app/regixtry/service_scanning.go:195-208`) calls `s.metadata.ListSecretScanRuns(ctx, tenant, repository, 50)` (ordered `created_at DESC`, store.go:1259 — newest first), loops for the first digest match, then returns the **full** `SecretScanRunDetail{Run, Findings []SecretFinding}` — confirming the admin endpoint dumps the full findings list, unlike the compact verdict shape the CI-facing siblings use.

## Critical gotcha found

`ports.MetadataStore.GetActiveSecretScanRunByDigest` (`internal/ports/regixtry.go:68`, impl `internal/infra/metadata/sqlite/store.go:1167-1176`) looks like the secret-scan analog of `GetLatestScanRunByDigest` but is **not**: its SQL filters `WHERE status IN (queued, running)` — it's a concurrency-dedup guard only, used at `service_scanning.go:400` to skip re-triggering an in-flight scan. It silently excludes completed/failed runs. A naive implementation reaching for this "obviously named" function would incorrectly report `unscanned` for any digest with a completed or failed scan. The correct reuse target is `GetSecretScanFindings`'s existing `ListSecretScanRuns` + digest-match path.

Also confirmed: gitleaks scanning never gates anything (`service_scanning.go:324-328`: "a gitleaks failure or absence must never fail or block the [push]"). There is no secret-scan equivalent of `scanPolicyViolated`/`ScanPolicySettings.SeverityThreshold` — so `would_block_pull` and a severity threshold have no real meaning here, unlike the two siblings. This is the one genuine (small) shape divergence.

Naming: `secret-scan-status` matches the existing `scan-status`/`signature-status` convention exactly; no reason to deviate.

## Affected Areas
- `internal/app/regixtry/queries.go` — add `SecretScanStatusResult`/`SecretScanStatusScan` structs + state consts + `Service.SecretScanStatus(ctx, repositoryName, reference)`, internally calling the existing `s.GetSecretScanFindings(ctx, repository.String(), digest)` (not duplicating the loop), projecting down to the compact shape.
- `internal/protocol/http/router.go` — third `strings.HasSuffix(reference, "/secret-scan-status")` branch in `handleV2` (before line 173's fallthrough) + `handleManifestSecretScanStatus` handler mirroring `handleManifestScanStatus` (389-409) exactly.
- `internal/app/regixtry/secret_scan_status_test.go` (new) and `internal/protocol/http/secret_scan_status_test.go` (new) — dedicated files mirroring the more recent `signature_status_test.go` convention (all-states, route-collision guard, pull-authorization-required).
- `docs/api.md` — one table row after line 157, mirroring lines 156-157.
- **Not touched**: `admin_handlers.go`'s `handleAdminSecretScanFindings` stays exactly as-is (explicit non-goal).

## Approaches
1. **Third mirror of the proven scan-status/signature-status shape** (recommended) — Pros: zero new patterns/auth model, reuses tested query path, small diff. Cons: none material. Effort: Low.
2. **Extend the admin route to also accept pull credentials** — rejected: breaks the blanket admin gate and would leak the full unredacted findings list to CI callers instead of a compact verdict.

## Recommendation
Approach 1 — a near-exact structural mirror of an already-shipped, twice-repeated pattern.

## Open Questions (minor, with recommended defaults — none blocking)
1. Include `would_block_pull` always-`false`, or omit it? **Recommend omit** — gitleaks never gates a pull.
2. Keep `Policy{Enabled}` without a severity field? **Recommend keep** — CI still needs to know if scanning is even on.
3. `finding_count` vs `findings_count`? **Recommend `finding_count`** — matches `ports.SecretScanRun.FindingCount` 1:1.
4. State vocabulary: **Recommend** `unscanned`, `in_progress`, `failed`, `clean`, `findings_present` — not `blocked`, since nothing is ever blocked.

## Risks
- `GetActiveSecretScanRunByDigest` naming trap (see above) — must not be used for this endpoint.
- No dedicated app-package unit tests found for `ScanStatus`/`SignatureStatus` today (only end-to-end coverage); new work should add its own dedicated test file regardless.
- `SecretScanRunStatusQueued` is declared but appears never persisted by `executeSecretScanLeg` — map it for symmetry anyway, but it may be untestable in practice.

## Ready for Proposal
Yes. Recommend proceeding directly to `sdd-propose`.
