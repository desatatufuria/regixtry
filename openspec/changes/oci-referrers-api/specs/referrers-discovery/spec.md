# Referrers Discovery Specification

## Purpose

Define `GET /v2/<name>/referrers/<digest>`: list manifests whose
`subject.digest` matches, so pushed signatures/attestations/SBOMs become
discoverable via the standard OCI mechanism instead of tag convention only.

## Requirements

### Requirement: Listing Returns Matches, Empty List Never 404s

The endpoint MUST return an OCI Image Index whose `manifests[]` holds exactly
the repository's manifests with matching `subject.digest`. A digest with no
referrers — never pushed, or deleted after having referrers — MUST return
`200` with `manifests: []`, never `404`.

#### Scenario: Pushed referrer is listed
- GIVEN a manifest with `subject.digest = D`
- WHEN a client requests `GET /v2/<name>/referrers/D`
- THEN response is `200` and `manifests[]` includes its descriptor

#### Scenario: Digest never existed
- GIVEN `D` was never pushed
- WHEN requesting referrers of `D`
- THEN response is `200` with `manifests: []`

#### Scenario: Subject deleted, survivors still list
- GIVEN subject `D` was deleted and referrer `R` (subject = `D`) survives
- WHEN requesting referrers of `D`
- THEN response is `200`, `R` is listed, no `5xx`

### Requirement: ArtifactType Filtering Sets Header Only When Applied

`?artifactType=<v>` MUST restrict results to matching `artifactType`.
`OCI-Filters-Applied: artifactType` MUST be set only when the parameter was
present.

#### Scenario: Filtered request narrows results and sets header
- GIVEN two referrers of `D` with different `artifactType`
- WHEN requesting `.../referrers/D?artifactType=<v>`
- THEN only matches return and the header is set

#### Scenario: Unfiltered request omits header
- GIVEN the same state
- WHEN requesting `.../referrers/D` with no `artifactType`
- THEN all referrers return and the header is absent

### Requirement: Descriptor ArtifactType Falls Back To Config MediaType

Each descriptor MUST carry `artifactType` from the manifest's own field when
present, else `config.mediaType`, plus the manifest's `annotations`. Neither
value is persisted; both are derived per response.

#### Scenario: ArtifactType present
- GIVEN a manifest sets a top-level `artifactType`
- WHEN it appears in the response
- THEN its descriptor's `artifactType` matches and includes `annotations`

#### Scenario: ArtifactType absent falls back
- GIVEN a manifest has no top-level `artifactType`
- WHEN it appears in the response
- THEN its descriptor's `artifactType` equals `config.mediaType`

### Requirement: Pre-Existing Content Is Discoverable Via Idempotent Backfill

A one-time backfill MUST index pre-existing manifests' `subject.digest` so
they are discoverable, and MUST be idempotent.

#### Scenario: Pre-existing referrer discoverable after backfill
- GIVEN a referrer was pushed before this endpoint shipped
- WHEN the backfill has run and its subject is queried
- THEN the manifest is included in `manifests[]`

#### Scenario: Backfill is idempotent
- GIVEN the backfill already ran once
- WHEN it runs again
- THEN no row changes and results are identical

### Requirement: Referrers Are Scoped To Tenant And Repository

Lookups MUST be scoped by tenant and repository, like `ResolveManifest`, never
globally.

#### Scenario: Cross-repository isolation
- GIVEN `subject.digest = D` exists only in repository `A`
- WHEN requesting referrers of `D` in repository `B`
- THEN response is `200` with `manifests: []`

#### Scenario: Cross-tenant isolation
- GIVEN `subject.digest = D` exists only for tenant `X`
- WHEN tenant `Y` requests referrers of `D` in the same-named repository
- THEN response is `200` with `manifests: []`

### Requirement: Endpoint Authorization Uses ActionInspect

The endpoint MUST authorize `ActionInspect`, matching the list-shaped
`tags/list` precedent, not `ActionPull`.

#### Scenario: Pull-scoped token accepted
- GIVEN a principal with only a pull-scoped token
- WHEN it requests the endpoint
- THEN response is `200`, not `401`/`403`

#### Scenario: Unauthorized principal rejected
- GIVEN a principal with no repository access
- WHEN it requests the endpoint
- THEN response is `401` with a challenge

### Requirement: Legacy Cosign Tag Artifacts Stay Separate

Manifests discoverable only via `SignatureTag`/`BundleIndexTag` convention
(no `subject`) MUST NOT be synthesized into responses; their tag-based path
MUST keep working unchanged. A cosign v3 bundle referrer manifest (which
carries `subject`) MUST be discoverable.

#### Scenario: Legacy `.sig` artifact absent
- GIVEN a legacy `.sig` manifest has no `subject`
- WHEN requesting referrers of the digest it signs
- THEN it is absent from `manifests[]`

#### Scenario: Cosign v3 bundle referrer discoverable
- GIVEN a cosign v3 bundle referrer manifest has `subject.digest = D`
- WHEN requesting referrers of `D`
- THEN it is included in `manifests[]`

### Requirement: No Push-Path Or Scan Behavior Change

This endpoint MUST NOT alter push-time subject validation or scan
queueing/suppression; reads MUST NOT trigger a scan or rescan.

#### Scenario: Push-time validation unchanged
- GIVEN a push whose `subject.digest` does not resolve in-repository
- WHEN it is pushed
- THEN response is still `409`

#### Scenario: Reads never affect scanning
- GIVEN pending or completed scans exist
- WHEN repeated Referrers requests are issued
- THEN no scan or rescan is queued, triggered, or reported
