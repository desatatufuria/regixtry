# Blob Garbage Collection Specification

## Purpose

Make unreferenced blob storage under `blobsRoot` visible via a persisted, global report, and reclaimable via an opt-in, TOCTOU-safe delete that recomputes and intersects at delete time.

## Current Repository Facts

- `DeleteManifestByDigest` / `DeleteTag` remove metadata rows only; blob files under `blobsRoot/<algorithm>/<hex>` are never unlinked today.
- `ports.MetadataStore` has no mark query; `ports.BlobStore` has no enumeration or delete-by-digest method.
- `REGISTRY_DELETE_ENABLED` gates metadata-only deletion and MUST NOT be reused for blob deletion.
- In-flight uploads live under a separate `uploads/<id>/` tree with no mark set.

## Requirements

### Requirement: Global Non-Tenant-Scoped Mark Set

The mark query MUST be `SELECT DISTINCT digest FROM manifest_blobs` with no tenant predicate. A blob referenced by any tenant MUST be excluded from every candidate set regardless of which tenant context triggered the report or delete. This is a binding invariant: the candidate set MUST NOT gain a `tenant` filter in a future change without an explicit spec revision.

#### Scenario: Blob shared across tenants stays protected
- GIVEN tenant A and tenant B both reference the same digest
- WHEN a report is computed under tenant A's context
- THEN that digest MUST NOT appear in the candidate list

#### Scenario: Report is usable from a different tenant context
- GIVEN a report was computed while tenant A's context was active
- WHEN an admin fetches or deletes against that report id under tenant B's context
- THEN the request MUST succeed as if issued under tenant A

### Requirement: Grace-Period Protection

A candidate digest MUST be excluded from the sweep if its blob file's commit/mtime is within a fixed, non-configurable 24-hour grace window of computation time.

#### Scenario: Recently written blob is protected
- GIVEN an unreferenced blob written 1 hour ago
- WHEN a report is computed
- THEN that digest MUST be excluded from candidates

#### Scenario: Aged unreferenced blob is a candidate
- GIVEN an unreferenced blob written 25 hours ago
- WHEN a report is computed
- THEN that digest MUST appear in candidates

### Requirement: Uploads Directory Exclusion

The sweep MUST NOT enumerate, read, or unlink anything under `uploads/`. Abandoned-upload cleanup is out of scope for this change.

#### Scenario: Uploads tree is never touched
- GIVEN in-progress uploads exist under `uploads/<id>/`
- WHEN a report or delete runs
- THEN no file under `uploads/` MUST be enumerated or unlinked

### Requirement: Report Computation and Persistence

An admin-triggered report request MUST compute the mark-sweep-grace candidate set and persist it as one `gc_reports` row plus its `gc_report_candidates` rows in a single transaction before responding, recording `computed_at`, `expires_at` (`computed_at` + 24h), the grace cutoff used, candidate count/bytes, duration, and `requested_by` from `ports.PrincipalFromContext`. The report endpoint MUST be reachable regardless of `REGISTRY_GC_DELETE_ENABLED`.

#### Scenario: Report is persisted before response
- GIVEN an authenticated admin requests a report
- WHEN computation completes
- THEN a `gc_reports` row and its candidate rows MUST exist and the response MUST include the report id

#### Scenario: Report path ignores the delete flag
- GIVEN `REGISTRY_GC_DELETE_ENABLED` is false
- WHEN an admin requests a report
- THEN the report MUST be computed and persisted normally

### Requirement: Report Retrieval

A persisted report MUST be fetchable by id, returning its full candidate list, unfiltered by requester tenant. Fetching an unknown report id MUST return not-found.

#### Scenario: Unknown report id
- GIVEN no report exists with the requested id
- WHEN it is fetched
- THEN the response MUST indicate not-found

### Requirement: Report Expiry and Pruning

A `reported`-state row expires 24 hours after `computed_at`. Expired `reported`-state rows MUST be pruned when a new report is computed. `deleted`-state rows MUST be retained indefinitely. Expiry is a hygiene mechanism only, never the delete-safety mechanism (see Recompute-and-Intersect).

#### Scenario: Expired preview report is pruned on next report
- GIVEN a `reported`-state report is older than its `expires_at`
- WHEN a new report is computed
- THEN the expired row and its candidates MUST be deleted

#### Scenario: Deleted-state report survives pruning
- GIVEN a report has transitioned to `deleted` state and is older than `expires_at`
- WHEN a new report is computed
- THEN the `deleted` report row MUST remain

### Requirement: Delete Requires a Valid Prior Report

A delete request MUST reference an existing report id and MUST be rejected without unlinking anything if that report is missing, unknown, expired, or already in `deleted` state (single-use).

#### Scenario: Delete against an expired or already-deleted report is rejected
- GIVEN a report is expired, or already `deleted`
- WHEN a delete request references it
- THEN it MUST be rejected and no blob MUST be unlinked

#### Scenario: Delete without a report reference is rejected
- WHEN a delete request omits a report id
- THEN it MUST be rejected without deleting anything

### Requirement: Recompute-and-Intersect at Delete Time

At delete time the mark-sweep-grace computation MUST be re-run, and only digests present in BOTH the referenced report's candidates AND the fresh recomputation MUST be unlinked. This recompute-and-intersect is the sole delete safety mechanism.

#### Scenario: Digest referenced after report is not deleted
- GIVEN a digest was a report candidate, and a manifest was published referencing it before the delete request
- WHEN delete runs against that report
- THEN that digest MUST NOT be unlinked and MUST be recorded with a not-deleted outcome

#### Scenario: Still-unreferenced digest is deleted
- GIVEN a digest was a report candidate and remains unreferenced and past grace at delete time
- WHEN delete runs against that report
- THEN that digest's blob file MUST be unlinked

### Requirement: Delete Gated by a Distinct Flag

Delete MUST be gated by `REGISTRY_GC_DELETE_ENABLED` / `-gc-delete-enabled` (default `false`), a flag distinct from `REGISTRY_DELETE_ENABLED`. Authorization MUST be checked before the flag is consulted. When the flag is false, the endpoint MUST return `501` via a new `ErrorCodeUnsupported` mapped in `writeAdminError`, with a message naming `REGISTRY_GC_DELETE_ENABLED`.

#### Scenario: Unauthorized caller rejected before flag check
- GIVEN the flag is false and the caller lacks delete authorization
- WHEN a delete request is made
- THEN the response MUST be an authorization error, not a flag-disabled error

#### Scenario: Authorized caller blocked by disabled flag
- GIVEN the flag is false and the caller is authorized
- WHEN a delete request is made
- THEN the response MUST be `501` with a message naming `REGISTRY_GC_DELETE_ENABLED`, and no blob file MUST be unlinked

### Requirement: Audit Trail on Report Transition

On delete completion, the report row MUST transition to `deleted` state, recording `deleted_at`, `deleted_by` (from `ports.PrincipalFromContext`), `deleted_count`, `bytes_reclaimed`, and per-candidate outcome. The row MUST be frozen once terminal.

#### Scenario: Terminal row records audit fields
- GIVEN a delete request completes
- WHEN the report row is read afterward
- THEN it MUST show `deleted_at`, `deleted_by`, `deleted_count`, and `bytes_reclaimed`

#### Scenario: Partial failure is recorded per digest, not all-or-nothing
- GIVEN one candidate digest fails to unlink while others succeed
- WHEN the delete completes
- THEN the report MUST still transition to `deleted`, with the failing digest's outcome recorded distinctly from successful ones

### Requirement: Out of Scope for v1

The system MUST NOT ship a scheduler or background trigger for GC (manual admin trigger only), MUST NOT clean up abandoned uploads under `uploads/` as part of this capability, and MUST NOT expose a TUI surface for report or delete. These exclusions are binding for this change.

#### Scenario: No unattended deletion occurs
- GIVEN no admin has triggered a delete request
- WHEN time passes with unreferenced, grace-expired blobs present
- THEN no blob file MUST be unlinked
