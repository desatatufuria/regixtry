# Design: Secret-Scan Status Endpoint (secret-scan-status)

## Technical Approach

A third mirror of the `scan-status`/`signature-status` pattern (router.go:145-178, 389-434;
queries.go:155-206, 259-311). One new route branch, one new handler, one new `Service` method,
zero new auth model, zero schema change. The only real shape divergence from the two siblings:
gitleaks never gates a pull (`service_scanning.go:324-328`), so the response carries no
`would_block_pull` and no severity threshold, and its digest-match query deliberately avoids
`GetActiveSecretScanRunByDigest` — a queued/running-only concurrency-dedup guard that would
misreport a completed scan as `unscanned` (exploration.md "Critical gotcha").

Every line reference below was read from the working tree, not estimated.

## Architecture Decisions

### Decision 1: duplicate the 6-line digest-match loop, don't refactor `GetSecretScanFindings`

| Option | Tradeoff | Decision |
|---|---|---|
| `SecretScanStatus` calls `s.metadata.ListSecretScanRuns` + its own digest-match loop | Six lines duplicated from `GetSecretScanFindings` (service_scanning.go:198-206) | **Chosen** |
| Extract a shared `findLatestSecretScanRun` helper, used by both `GetSecretScanFindings` and `SecretScanStatus` | Touches `service_scanning.go`, which the proposal's Affected Areas table does not list — widens the diff on an already-shipped, out-of-scope admin path for a six-line saving | Rejected |
| Call `s.GetSecretScanFindings` directly, discard `Findings` | Fetches `GetSecretScanRunDetail` (a second query, full findings) purely to read `Status`/`FindingCount`, both already present on the `SecretScanRun` `ListSecretScanRuns` returns | Rejected |

`ListSecretScanRuns` already populates `FindingCount` per row (`store.go:1274-1278`), so the
compact endpoint needs nothing `GetSecretScanRunDetail` adds.

### Decision 2: `Policy.Enabled` reads `GetScanSettings(gitleaks)` + repository override, not a policy-gate type

There is no `SecretScanPolicySettings` type — gitleaks has no gate to configure, unlike
`ports.ScanPolicySettings{Enabled, SeverityThreshold}` for Trivy. The only "is scanning on for
this repo" signal is the same one `executeSecretScanLeg` itself gates on
(`service_scanning.go:380-391`): `s.metadata.GetScanSettings(ctx, tenant, gitleaksFeatureName)`
then `s.applyRepositoryOverride(ctx, tenant, repository, gitleaksFeatureName, settings)`.

`GetScanSettings` returns `domain.ErrorCodeNotFound` on zero rows (`store.go:678-680`) — a real
case in fresh installs/tests before `EnsureScanSettings` has run for gitleaks. Unlike
`GetScanPolicySettings`'s fail-open-to-`true` default (a security gate that must never silently
turn off), gitleaks is informational only, so `NotFound` defaults to `Enabled: false`: safer to
under-report than to claim scanning is on when no row says so.

### Decision 3: state is `run.Status` plus `FindingCount`, not `run.Status` alone

`ScanStatus` derives `blocked` vs `clean` from `scanPolicyViolated(settings, run)` — a policy
question. Secret scan has no policy, so `Completed` splits on the finding count itself:
`FindingCount == 0` → `clean`, `FindingCount > 0` → `findings_present`. `Queued`/`Running` →
`in_progress`; `Failed` → `failed`; no matching run → `unscanned`.

### Decision 4: `finding_count`, matching `ports.SecretScanRun.FindingCount` verbatim

Resolves proposal open question 1. No transformation between the store row and the wire field —
one name, one meaning, across both layers.

## Data Flow

    GET /v2/library/app/manifests/sha256:abc…/secret-scan-status
      handleV2  reference := "sha256:abc…/secret-scan-status"
        strings.HasSuffix(reference, "/secret-scan-status") -> true
        -> handleManifestSecretScanStatus(repo, "sha256:abc…")
             action := {Verb: ActionPull, Repository: "library/app"}
             -> withPrincipal            [401 on missing/denied credential]
             -> Service.SecretScanStatus(repo, "sha256:abc…")
                  parseRepository -> authorize(ActionPull)
                  ResolveManifest -> digest
                  GetScanSettings(gitleaks) [NotFound -> Enabled:false] -> applyRepositoryOverride
                  ListSecretScanRuns(50) -> first Digest == digest match
                    no match            -> state=unscanned
                    Queued/Running      -> state=in_progress
                    Failed              -> state=failed
                    Completed, count=0  -> state=clean
                    Completed, count>0  -> state=findings_present
        -> 200 {"repository","reference","digest","state","policy":{"enabled"},"scan":{...}}

    GET /v2/library/app/manifests/secret-scan-status   (tag literally named "secret-scan-status")
      handleV2  reference := "secret-scan-status"   (no leading "/")
        strings.HasSuffix(reference, "/secret-scan-status") -> false
        -> handleManifest   [ordinary manifest GET/HEAD/PUT]

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/app/regixtry/queries.go` | Modify | `SecretScanStatusResult`/`SecretScanStatusPolicy`/`SecretScanStatusScan` structs, five state consts, `Service.SecretScanStatus` |
| `internal/protocol/http/router.go` | Modify | Third `strings.HasSuffix(reference, "/secret-scan-status")` branch in `handleV2` (before the `handleManifest` fallthrough) + `handleManifestSecretScanStatus` handler |
| `internal/app/regixtry/secret_scan_status_test.go` | New | App-layer all-five-states, `Enabled:false` default, pull-authorization-required coverage |
| `internal/protocol/http/secret_scan_status_test.go` | New | Route reachability, route-collision, pull-authorization coverage |
| `docs/api.md` | Modify | One table row after line 157, mirroring lines 156-157 |
| `internal/protocol/http/admin_handlers.go` | Unchanged | Explicit non-goal — `handleAdminSecretScanFindings` and its gate untouched |
| `internal/app/regixtry/service_scanning.go` | Unchanged | `GetSecretScanFindings` untouched (Decision 1) |

## Interfaces / Contracts

```go
// internal/app/regixtry/queries.go
type SecretScanStatusResult struct {
	Repository string                  `json:"repository"`
	Reference  string                  `json:"reference"`
	Digest     string                  `json:"digest"`
	State      string                  `json:"state"`
	Policy     SecretScanStatusPolicy  `json:"policy"`
	Scan       *SecretScanStatusScan   `json:"scan,omitempty"`
}

type SecretScanStatusPolicy struct {
	Enabled bool `json:"enabled"`
}

type SecretScanStatusScan struct {
	Status       string     `json:"status"`
	FindingCount int        `json:"finding_count"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

const (
	SecretScanStatusUnscanned       = "unscanned"
	SecretScanStatusInProgress      = "in_progress"
	SecretScanStatusFailed          = "failed"
	SecretScanStatusClean           = "clean"
	SecretScanStatusFindingsPresent = "findings_present"
)

func (s *Service) SecretScanStatus(ctx context.Context, repositoryName string, reference string) (SecretScanStatusResult, error)
```

```
GET /v2/<repo>/manifests/<tag-or-digest>/secret-scan-status
  200  {"repository","reference","digest","state","policy":{"enabled"},"scan":{"status","finding_count","finished_at"}?}
  401  UNAUTHORIZED + WWW-Authenticate: …scope="repository:<repo>:pull"   (missing/denied credential)
  404  MANIFEST_UNKNOWN                                                   (reference does not resolve)
  <any other method>  405  Allow: GET
```

## Testing Strategy

Strict TDD; both new test files are RED before `SecretScanStatus`/the router branch exist.

| Layer | What to Test | Approach |
|---|---|---|
| Unit | All five states resolve correctly for a seeded run per state | Table-driven, `internal/app/regixtry/secret_scan_status_test.go`, mirrors `signature_status_test.go` |
| Unit | A completed run with a stale queued/running row for the *same repository, different digest* still resolves `clean`/`findings_present`, never `unscanned` | Guards against reaching for `GetActiveSecretScanRunByDigest` (Decision 1, exploration's "Critical gotcha") |
| Unit | `GetScanSettings(gitleaks)` `NotFound` → `Policy.Enabled == false`, no error | Table-driven |
| Unit | Repository override flips `Policy.Enabled` independent of the tenant-wide row | Mirrors `repository_overrides_test.go`'s gitleaks case |
| Unit | Authorization is `ActionPull`; a denied caller gets an authorization error | Mirrors `TestServiceSignatureStatusRequiresPullAuthorization` |
| Integration | `GET .../secret-scan-status` reachable, returns 200 with the documented shape for each state | `internal/protocol/http/secret_scan_status_test.go` |
| Integration | A tag literally named `secret-scan-status` still routes to `handleManifest` | Mirrors `TestRouterManifestSignatureStatusRouteCollisionWithTagNamedSignatureStatus` |
| Integration | Pull-only credential succeeds; no credential is `401` | Mirrors `TestRouterManifestSignatureStatusRequiresPullAuthorization` |
| Integration | Non-GET method answers `405` + `Allow: GET` | Table-driven |
| Integration | `/admin/v1/secret-scan-findings` response shape, status codes, and auth gate are byte-identical before/after | Existing admin test suite unmodified, run as a regression guard |

## Threat Matrix

Applicable: this change adds a route branch to an existing dispatcher (`handleV2`). No shell, no
subprocess, no VCS/PR automation, no executable-file classification — the generic
threat-matrix.md rows (documentation-like paths, Git repository selection, commit/push state, PR
commands) are all **N/A**, none of that surface exists here.

| Boundary | Applicability | Design response | Planned RED test |
|---|---|---|---|
| Route collision / path-shadowing | **Applicable** — a tag literally named `secret-scan-status` must not be swallowed by the new branch | Suffix match against the *trimmed* reference (`/secret-scan-status`, not the bare tag name), the identical technique already proven by the two sibling branches | A pushed tag `secret-scan-status` still resolves via `handleManifest` |
| Privilege reuse | N/A — reuses `ActionPull` verbatim, no new verb, no new `AccessController` arm | — | — |
| Capability/data disclosure | **Applicable** — the compact verdict must never carry per-finding detail (rule ID, path, matched text) | `SecretScanStatusScan` declares only `Status`/`FindingCount`/`FinishedAt`; `ports.SecretFinding` is never referenced | Serialized response asserted to omit `rule_id`/`path`/finding-array keys |

## Migration / Rollout

No migration, no flag, no schema change. Purely additive: a reverted binary stops answering the
new route, falling through to the existing `manifests/<ref>` GET/PUT/HEAD handling or `404`. No
other behavior changes.

## Open Questions

- [x] Field name for the finding count — resolved: `finding_count` (Decision 4).
- [ ] `SecretScanRunStatusQueued` may be unreachable from the current scan pipeline
      (`executeSecretScanLeg` inserts rows as `Running`, never `Queued`). Mapped in the switch for
      symmetry with `ScanStatus`'s own `Queued`/`Running` pairing; accepted as untestable in
      practice per the exploration's own note — no task should block on covering it.
