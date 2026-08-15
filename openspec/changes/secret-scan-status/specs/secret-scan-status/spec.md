# Secret-Scan Status Specification

## Purpose

Define a CI-facing, pull-gated endpoint that reports the secret-scan verdict
for a manifest reference as a compact state, structurally mirroring the
existing `scan-status` and `signature-status` endpoints. Gitleaks findings
never gate a pull, so this endpoint reports a verdict — it is never itself
subject to one.

## Requirements

### Requirement: Route Dispatches On The Trimmed Reference Suffix

`GET /v2/<repo>/manifests/<ref>/secret-scan-status` MUST be checked against
the manifest reference with the `manifests/` prefix already trimmed, the same
anti-collision technique the `scan-status` and `signature-status` branches
use, before falling through to the ordinary manifest handler.

#### Scenario: Suffix-matched reference routes to the status handler
- GIVEN a request path `manifests/<ref>/secret-scan-status`
- WHEN `handleV2` dispatches it
- THEN the request reaches the secret-scan-status handler with reference
  `<ref>`, not the ordinary manifest handler

#### Scenario: A tag literally named "secret-scan-status" is not misrouted
- GIVEN a manifest reference that is exactly the tag `secret-scan-status`
  (no `/` prefix segment)
- WHEN a client requests `GET /v2/<repo>/manifests/secret-scan-status`
- THEN the request routes to the ordinary manifest handler, not the
  secret-scan-status handler

### Requirement: Endpoint Requires Pull Authorization On The Repository

The system MUST authorize every request with `ports.Action{Verb: ActionPull,
Repository: <repo>}`, identical to the two sibling status endpoints. No new
authorization model is introduced.

#### Scenario: Unauthenticated request is rejected
- GIVEN no principal is attached to the request
- WHEN a client calls `GET .../secret-scan-status`
- THEN the response is `401 UNAUTHORIZED` with a `WWW-Authenticate` challenge

#### Scenario: Authenticated caller without pull access is rejected
- GIVEN a principal without read/pull access to the target repository
- WHEN that principal calls `GET .../secret-scan-status`
- THEN the response is `401 UNAUTHORIZED`, matching how `scan-status` rejects
  the same caller

#### Scenario: Pull-authorized caller receives a verdict
- GIVEN a principal with pull access to the repository
- WHEN that principal calls `GET .../secret-scan-status`
- THEN the response is `200 OK` with the verdict body

### Requirement: Non-GET Requests Return 405 With An Allow Header

#### Scenario: POST is rejected
- GIVEN a client sends `POST .../secret-scan-status`
- WHEN the handler processes it
- THEN the response is `405` with header `Allow: GET`

### Requirement: Verdict Reuses The Digest-Match Query Path

`Service.SecretScanStatus` MUST resolve the verdict via the same query path
`GetSecretScanFindings` uses — `ListSecretScanRuns` followed by a digest
match — and MUST NOT call `GetActiveSecretScanRunByDigest`, which filters to
`queued`/`running` rows only and would misreport a completed scan.

#### Scenario: A completed scan is never reported as unscanned
- GIVEN a digest has one completed secret-scan run (clean or with findings)
  and, for the same repository, an unrelated stale row with status `queued`
  or `running`
- WHEN a pull-authorized caller requests the verdict for that digest
- THEN the state reflects the completed run's outcome, never `unscanned`

### Requirement: Verdict Reports One Of Five States

The state MUST be exactly one of `unscanned`, `in_progress`, `failed`,
`clean`, `findings_present`.

#### Scenario: No matching run yields unscanned
- GIVEN no secret-scan run exists for the resolved digest
- WHEN the verdict is requested
- THEN `state` is `unscanned` and no `scan` object is present

#### Scenario: Queued or running run yields in_progress
- GIVEN the matching run's status is `queued` or `running`
- WHEN the verdict is requested
- THEN `state` is `in_progress`

#### Scenario: Failed run yields failed
- GIVEN the matching run's status is `failed`
- WHEN the verdict is requested
- THEN `state` is `failed`

#### Scenario: Completed run with no findings yields clean
- GIVEN the matching run is `completed` with zero findings
- WHEN the verdict is requested
- THEN `state` is `clean`

#### Scenario: Completed run with findings yields findings_present
- GIVEN the matching run is `completed` with one or more findings
- WHEN the verdict is requested
- THEN `state` is `findings_present`

### Requirement: Response Shape Is Compact With No Blocking Fields

The response body MUST include `repository`, `reference`, `digest`, `state`,
and `policy.enabled`. When a matching run exists, it MUST also include a
`scan` object with `status`, `finding_count`, and `finished_at`. The response
MUST NOT include `would_block_pull` or a severity-threshold field: gitleaks
never gates a pull or a push.

#### Scenario: Unscanned verdict omits the scan object
- GIVEN state is `unscanned`
- WHEN the response is serialized
- THEN no `scan` field is present in the JSON body

#### Scenario: Scanned verdict never includes blocking fields
- GIVEN any state other than `unscanned`
- WHEN the response is serialized
- THEN the body contains no `would_block_pull` key and no severity-threshold
  key at any level

### Requirement: The Admin Findings Endpoint Is Unaffected

`GET /admin/v1/secret-scan-findings` MUST remain gated by
`requireAdminPrincipal`, and MUST continue returning the full
`SecretScanRunDetail` (run plus per-finding list) unchanged. This addition
introduces no new route, field, or behavior on that endpoint.

#### Scenario: Admin endpoint behavior is unchanged
- GIVEN the admin findings endpoint existed before this change
- WHEN a caller with a valid admin session requests it
- THEN the response shape, status codes, and auth gate are identical to
  before this change
