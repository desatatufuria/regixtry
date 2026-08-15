# Manifest Deletion Specification

## Purpose

Define client-initiated withdrawal of a published manifest by digest, or of a
single tag by name, via the standard OCI `DELETE` endpoint, gated by an
opt-in server flag.

## Requirements

### Requirement: Delete Is Gated Behind An Opt-In Server Flag

The system MUST expose `REGISTRY_DELETE_ENABLED` (default `false`). When off,
`DELETE /v2/<name>/manifests/<reference>` MUST answer the same
acknowledged-but-refused OCI `UNSUPPORTED` error the existing
upload-cancellation `DELETE` branch uses, never a bare `405`.

#### Scenario: Flag off refuses with UNSUPPORTED
- GIVEN `REGISTRY_DELETE_ENABLED` is unset
- WHEN a client sends `DELETE /v2/<name>/manifests/<digest-or-tag>`
- THEN the response is the OCI `UNSUPPORTED` error, not a bare `405`

#### Scenario: Flag on permits delete processing
- GIVEN `REGISTRY_DELETE_ENABLED` is `true`
- WHEN a client sends the same `DELETE` request
- THEN the request is authorized and processed instead of refused

### Requirement: Delete By Digest Cascades To Tags And Manifest Blobs

`DELETE /v2/<name>/manifests/<digest>` MUST atomically remove the manifest
row, every `tags` row referencing it, and every `manifest_blobs` row for it.

#### Scenario: Deleting a multi-tagged digest removes all its tags
- GIVEN a digest has three tags pointing to it
- WHEN a client deletes that digest
- THEN the response is `202 Accepted` and all three tags stop resolving

#### Scenario: Unknown digest returns MANIFEST_UNKNOWN
- GIVEN no manifest exists for the given digest
- WHEN a client deletes that digest
- THEN the response is `404 MANIFEST_UNKNOWN`

### Requirement: Delete By Tag Untags Without Touching The Manifest

`DELETE /v2/<name>/manifests/<tag>` MUST remove only that tag's row; the
manifest and its other tags MUST remain pullable.

#### Scenario: Deleting one tag leaves siblings and the manifest intact
- GIVEN a manifest has tags `a` and `b`
- WHEN a client deletes tag `a`
- THEN the response is `202 Accepted`, `a` is gone, and `b`/the digest remain
  pullable

#### Scenario: Unknown tag returns MANIFEST_UNKNOWN
- GIVEN no such tag exists in the repository
- WHEN a client deletes it
- THEN the response is `404 MANIFEST_UNKNOWN`

### Requirement: Delete Is Metadata-Only And Leaves Other Operations Unchanged

Deletion MUST NOT remove or modify any blob file on disk. Push, pull, tag
listing, and catalog behavior for content not targeted by a delete MUST be
unaffected, with the flag on or off.

#### Scenario: Blob files survive a digest delete
- GIVEN a manifest is deleted by digest
- WHEN blob storage is inspected afterward
- THEN every blob file that existed before still exists on disk

#### Scenario: Unrelated repository operations are unaffected
- GIVEN the flag is `true` and no delete has been issued
- WHEN a client pushes, pulls, lists tags, or queries the catalog
- THEN behavior is identical to a registry with the flag off
