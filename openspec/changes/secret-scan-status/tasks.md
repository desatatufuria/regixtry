# Tasks: Secret-Scan Status Endpoint (secret-scan-status)

## TDD Ordering Note

Strict TDD: every state/auth/route assertion is RED before its GREEN. Unlike
`manifest-blob-delete`, no existing behavior needs a characterization test
first — `queries.go`/`router.go` compile unchanged until Phase 1/2's GREEN
tasks land, so a RED integration test naturally fails via the existing
`handleManifest` fallthrough (wrong status/shape), not a compile error.

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~450–600 (prod ~90–120: `queries.go` ~70, `router.go` ~40 incl. handler+branch; tests ~330–450: app-layer ~160–220, HTTP-layer ~170–230; docs +1) |
| 400-line budget risk | Low |
| Chained PRs recommended | No |
| Suggested split | Single PR |
| Delivery strategy | single-pr |
| Chain strategy | N/A — no chaining needed |

Decision needed before apply: No
Chained PRs recommended: No
Chain strategy: N/A
400-line budget risk: Low

**Rationale**: this is a third structural mirror of an already-shipped,
twice-repeated pattern (`scan-status`/`signature-status`) — zero new auth
model, zero schema change, three modified files plus two new test files, all
named exactly by design.md. Estimated total sits around 450–600 lines. That
is at or modestly above the skill's literal 400-line default, but this
session's operative review budget — cached per the preceding
`manifest-blob-delete` change (~1100–1500 lines, `single-pr` + explicit
`size:exception`) — is **1000 lines**, not 400. 450–600 fits comfortably
under 1000 with no chaining, no split, and no exception needed. Compare to
`manifest-blob-delete`'s 4-PR chain: that change touched domain/auth
(new scope grammar), ports, app/auth, sqlite store, HTTP router, and
`main.go`, with zero pre-existing coverage on three functions. This change
touches exactly one app-layer file and one HTTP-layer file, reuses
`ActionPull` verbatim, and adds no config flag.

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Full change (Phases 1–3) | PR 1 (single) | `go test ./internal/app/regixtry/... ./internal/protocol/http/... -run SecretScanStatus -v` | Manual: start the server, `curl -H "Authorization: Bearer <pull-token>" https://<host>/v2/<repo>/manifests/<digest>/secret-scan-status`; confirm `200` with the documented verdict shape for a seeded run | Revert the `queries.go`/`router.go`/`docs/api.md` delta; purely additive route — a reverted binary falls through to the existing `manifests/<ref>` GET/PUT/HEAD handling or `404`, no other behavior changes |

## Phase 1: App Layer — Types, Constants, Service Method (`internal/app/regixtry/queries.go`)

- [x] 1.1 GREEN (compile prerequisite): declare `SecretScanStatusResult`,
      `SecretScanStatusPolicy`, `SecretScanStatusScan` structs and the five
      `SecretScanStatus*` state consts per design.md's Interfaces/Contracts;
      add `Service.SecretScanStatus(ctx, repositoryName, reference)` as a
      signature-only stub (e.g. returns `SecretScanStatusResult{}, nil`) so
      1.2–1.6 compile against real types.
- [x] 1.2 RED `internal/app/regixtry/secret_scan_status_test.go` (new):
      table-driven, all five states for a seeded run per state —
      `unscanned` (no matching run), `in_progress` (`Queued`/`Running`),
      `failed`, `clean` (`Completed`, 0 findings), `findings_present`
      (`Completed`, ≥1 finding). Mirrors `signature_status_test.go`'s
      table-driven shape.
- [x] 1.3 RED (same file): a completed run for the target digest resolves
      `clean`/`findings_present` even when a stale `queued`/`running` row
      exists for the same repository at a *different* digest — guards
      against reaching for `GetActiveSecretScanRunByDigest` (design.md
      Decision 1, exploration's "Critical gotcha").
- [x] 1.4 RED (same file): `GetScanSettings(ctx, tenant, gitleaks)` returning
      `domain.ErrorCodeNotFound` yields `Policy.Enabled == false`, no error
      (design.md Decision 2 — informational-only fail-closed default, unlike
      `GetScanPolicySettings`'s fail-open).
- [x] 1.5 RED (same file): a repository-level override flips `Policy.Enabled`
      independent of the tenant-wide row — mirrors
      `repository_overrides_test.go`'s gitleaks case.
- [x] 1.6 RED (same file): a principal without pull access to the repository
      is rejected — mirrors `TestServiceSignatureStatusRequiresPullAuthorization`.
- [x] 1.7 GREEN `queries.go`: implement `Service.SecretScanStatus` —
      `parseRepository` → `authorize(ActionPull)` → `ResolveManifest` →
      `GetScanSettings(gitleaks)` (`NotFound` → `Enabled:false`) →
      `applyRepositoryOverride` → `ListSecretScanRuns` + digest-match loop
      (duplicated, not extracted — design.md Decision 1) → state derivation
      per Decision 3 (`Queued`/`Running`→`in_progress`, `Failed`→`failed`,
      `Completed`+`FindingCount==0`→`clean`, `Completed`+`FindingCount>0`→
      `findings_present`, no match→`unscanned`); `finding_count` verbatim
      from `ports.SecretScanRun.FindingCount` (Decision 4).
- [x] 1.8 Confirm Phase 1 GREEN: `go test ./internal/app/regixtry/... -run SecretScanStatus -v`.

## Phase 2: HTTP Layer — Route Branch and Handler (`internal/protocol/http/router.go`)

- [x] 2.1 RED `internal/protocol/http/secret_scan_status_test.go` (new):
      `GET .../secret-scan-status` with a pull-authorized principal returns
      `200` with the documented shape (`repository`, `reference`, `digest`,
      `state`, `policy.enabled`, and `scan{status,finding_count,finished_at}`
      only when a run exists); `unscanned` verdict omits the `scan` key.
- [x] 2.2 RED (same file): a manifest reference that is exactly the tag
      `secret-scan-status` (no `/` prefix segment) still routes to
      `handleManifest`, not the new handler — mirrors
      `TestRouterManifestSignatureStatusRouteCollisionWithTagNamedSignatureStatus`
      (threat matrix: route collision / path-shadowing, **Applicable**).
- [x] 2.3 RED (same file): no credential → `401` + `WWW-Authenticate`; a
      principal without pull access → `401` — mirrors
      `TestRouterManifestSignatureStatusRequiresPullAuthorization`.
- [x] 2.4 RED (same file): any non-`GET` method → `405` + `Allow: GET`,
      table-driven.
- [x] 2.5 RED (same file): serialized response never contains
      `would_block_pull` or a severity-threshold key at any level, and never
      contains `rule_id`/`path`/a finding-array key — threat matrix:
      capability/data disclosure, **Applicable**
      (`ports.SecretFinding` must never be referenced from
      `SecretScanStatusScan`).
- [x] 2.6 GREEN `router.go`: add the third
      `strings.HasSuffix(reference, "/secret-scan-status")` branch in
      `handleV2`, before the `handleManifest` fallthrough, calling
      `strings.TrimSuffix(reference, "/secret-scan-status")` — same
      technique as the `/scan-status` and `/signature-status` branches; add
      `handleManifestSecretScanStatus(w, req, repository, reference)`
      mirroring `handleManifestSignatureStatus` (405+`Allow` on non-GET,
      `withPrincipal(action)` with `ActionPull`, `Service.SecretScanStatus`,
      `writeError`/`writeJSON("MANIFEST_UNKNOWN")` conventions).
- [x] 2.7 Confirm Phase 2 GREEN:
      `go test ./internal/protocol/http/... -run SecretScanStatus -v`; then
      run the existing admin test suite unmodified as a regression guard
      that `/admin/v1/secret-scan-findings` response shape, status codes,
      and auth gate are byte-identical (proposal Success Criteria).

## Phase 3: Documentation and Full Regression

- [x] 3.1 `docs/api.md`: insert one table row after line 157, mirroring
      lines 156–157 — `GET | /v2/<repo>/manifests/<tag-or-digest>/secret-scan-status | CI-facing secret-scan verdict; pull-credential auth; always 200`.
- [x] 3.2 Confirm full `go test ./...` (zero regressions) and `gofmt -l .`
      (clean).
