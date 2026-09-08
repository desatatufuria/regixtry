# Proposal: OCI 1.1 Referrers API (oci-referrers-api)

## Intent

regixtry stores subject-bearing artifacts but cannot answer "what refers to this digest?". Verified:
`handleV2` (`router.go:153-195`) has no `referrers/` case, so `GET /v2/{repo}/referrers/{digest}` falls to `default:`
and returns `404 NAME_UNKNOWN`. `manifest.Subject` is parsed (`service.go:421`), validated (`service.go:312-319`),
and stored inside the `payload` BLOB — but it is indexed nowhere, and `ResolveManifest` (`store.go:319`) reads it
back as `nil`. The registry accepts attestations, SBOMs, and cosign v3 bundle referrers, then makes them
undiscoverable by the standard mechanism.

Success: a client that pushed a signature, SBOM, or attestation can list it by subject digest through the standard
OCI endpoint, on content pushed both before and after this ships.

## Scope

### In Scope

- **`GET /v2/<name>/referrers/<digest>`** — returns an OCI Image Index whose `manifests[]` are the manifests in that
  repository whose `subject.digest` equals the requested digest. `200` with an empty `manifests[]` when there are
  none — never `404`, even for a subject digest that is absent or was deleted.
- **`?artifactType=` filtering**, with `OCI-Filters-Applied: artifactType` set only when a filter was actually applied.
- **One new column `manifests.subject_digest TEXT NOT NULL DEFAULT ''`**, following the exact inline
  `CREATE TABLE` + idempotent `ALTER TABLE ... ADD COLUMN` pattern already used for `pushed_by` (`store.go:1914`).
- **A one-time backfill** populating `subject_digest` for pre-existing rows by re-parsing the stored `payload`.
- **`ActionInspect` authorization**, matching the list-shaped `tags/list` precedent (`router.go:484`), not `ActionPull`.
- **Tenant- and repository-scoped lookups**, like `ResolveManifest`/`ListTags` — never globally scoped.
- **Test-first coverage at every layer.** Strict TDD is on and `handleV2`'s dispatch switch has no covering tests today.

### Out of Scope

- **Relaxing the push-time subject-must-exist check** (`service.go:312-319`). This change makes attestations
  *discoverable*; it does **not** remove the `provenance: false` workaround on `docker/build-push-action`. That is a
  push-path change and a separate proposal.
- **Legacy cosign tag artifacts in Referrers responses** — see Decision 2.
- **Orphaned-referrer detection, cleanup, or delete-time blocking** — see Decision 3.
- **An `artifact_type` column** — see Decision 4.
- **Referrer-aware GC or retention.** GC still computes blob candidates only (`service_gc.go`) and never reads
  `manifests.digest` or `subject`.
- **TUI referrer views** and **`internal/domain/signing` changes** — no code in that package is touched.
- **Scan behavior.** `isReferrerArtifactPush` (`service.go:441-471`) keeps suppressing push scans for referrer
  artifacts exactly as today; Referrers reads never queue, trigger, or report a scan or rescan.

## Capabilities

### New Capabilities

- `referrers-discovery`: listing the artifacts that refer to a subject digest through the OCI 1.1 Referrers endpoint,
  with artifact-type filtering, empty-result semantics, and list-shaped authorization.

### Modified Capabilities

- None. `registry-protocol`'s "Repository Discovery and Access Modes" requirement is unchanged — Referrers is an
  additional endpoint, not a change to catalog/tag/manifest behavior — and the new column is implementation, not a
  spec-level change to `registry-storage`. This mirrors `manifest-blob-delete`, which introduced `manifest-deletion`
  as its own capability rather than widening `registry-protocol`.

## Approach

Index the one field the query needs; derive everything else from bytes already stored.

| Decision | Approach | Why |
|---|---|---|
| Indexing | One `subject_digest` column, written in `PublishManifest` (`store.go:234-241`) | It is the `WHERE` clause. Without it, every referrers request scans and parses every payload in the repository |
| Response shaping | `ListReferrers` returns matched rows (`digest`, `media_type`, `size`, `payload`); the **app layer** parses each payload into response descriptors | Keeps OCI JSON parsing in the layer that already owns `manifestEnvelope`/`parseManifestPayload`; the SQLite adapter stays dumb. The matched set is "referrers of one digest" — small |
| Filtering | `?artifactType=` applied in the app layer over that matched set | Correct fallback semantics need the parsed payload anyway; SQL-side filtering would need a second column to drift out of sync |
| Authorization | `ActionInspect` | List-shaped like `tags/list`; `Action.Scope()` already makes a plain pull token satisfy it, so no client change is needed |
| Push path | Untouched | The 409 subject check and scan suppression keep their current behavior exactly |

**Benchmark**: the distribution-spec makes Referrers optional and specifies a fallback tag schema `<alg>-<digest>`
resolving to an image index of referrers — which is the same shape as cosign's existing `BundleIndexTag`, so regixtry
already serves the fallback and this change adds the native endpoint alongside it. zot indexes `subject` in its
metadata store exactly as proposed here; Docker distribution, GHCR, ACR, and Harbor all expose the endpoint with
`artifactType` filtering and `OCI-Filters-Applied`. `sdd-spec` MUST confirm the empty-result status code and header
name against the current spec text rather than inheriting them from this paragraph.

**Docs/roadmap impact**: README gains the Referrers endpoint; the `provenance: false` note MUST stay, because this
change does not remove that workaround.

## Resolved Decisions

### Decision 1 — Backfill `subject_digest` for pre-existing rows: **yes, one-time and idempotent**

The `pushed_by` precedent did not backfill because it *could not*: the pushing principal is not recoverable from
stored data. `subject_digest` is different — it is deterministically present in the `payload` BLOB every existing row
already carries. The precedent is about unrecoverable provenance, not a policy against backfill, so it does not bind.

Not backfilling would be actively misleading, not merely incomplete: Referrers answers `200` with an empty list for
"no referrers", so a client cannot distinguish "nothing refers to this" from "pushed before we started indexing". A
registry that already holds signed images would report them unsigned. The backfill runs once at migration, in Go,
after the `ALTER TABLE`, gated by a persisted completion marker so it is not a full payload scan on every boot.

### Decision 2 — Legacy cosign tag artifacts in Referrers: **no, the two mechanisms stay fully separate**

The `.sig` and bundle-index manifests carry no `subject` **and** no `artifactType`; synthesizing descriptors for them
would mean inventing a value for a spec-defined field, putting untrue data in the response. It would also make a
single response part indexed truth and part reconstructed tag convention — hard to explain, hard to test, and it
couples a new read path to `internal/domain/signing`'s naming.

Nobody loses a capability: cosign v3's actual bundle referrer manifest **does** carry `subject`
(`testdata/bundle-referrer-manifest.json:21-25`), so it is discoverable through this endpoint the day it ships, and
legacy artifacts keep their working, separately tested tag path — which is itself the spec's sanctioned fallback.
Recorded as a known limitation and a follow-up candidate, not a silent gap.

### Decision 3 — Orphaned/dangling referrers: **accepted as pre-existing debt, explicitly out of scope**

The gap already exists: `DeleteManifestByDigest` (`store.go:328-410`) deletes unconditionally without checking
whether another manifest's `subject` points at the digest. It is `manifest-blob-delete`'s, not this change's, and it
exists whether or not Referrers ships.

A delete-time check was rejected as a security regression on the use case `manifest-blob-delete` shipped for: it
would make a signed manifest carrying a leaked secret harder to withdraw than an unsigned one, because its own
signature would block the delete. A GC-report detector was rejected as a new reporting surface with no consumer.

What this change does owe is defined behavior, and it has it: an orphan's row keeps its `subject_digest`, so it is
still returned, and a query for a deleted or never-pushed subject returns `200` with the surviving referrers or an
empty list. No new failure mode. Handed to a future GC/retention change as a named input.

### Decision 4 — `artifactType` persistence: **not persisted; derived at query time**

`artifactType` is added to `manifestEnvelope` so the existing parser produces it, and the OCI fallback to
`config.mediaType` is applied **when building the response**, never at write time. A stored derived value becomes
permanently wrong if the fallback rule changes, and cannot be corrected without another backfill.

No `artifact_type` column is added. The referrers response descriptor also needs `annotations`, which only exist in
the payload, so the payload parse is unavoidable on the matched set — a column would be duplicated state that can
drift from the bytes it was derived from, for no query the column is needed to answer. `ArtifactType` goes on
`domain.Manifest` (a top-level OCI manifest field), **not** on `domain.Descriptor`, which is shared with config and
layers and participates in blob validation.

## Affected Areas

| Area | Impact | Description |
|---|---|---|
| `internal/protocol/http/router.go` | Modified | `case suffix == "referrers/..."` in `handleV2`; new `handleReferrers` parallel to `handleTags`; `?artifactType=` and `OCI-Filters-Applied` |
| `internal/app/regixtry/queries.go` | Modified | `Referrers(ctx, repository, digest, artifactType)`, authorizing `ActionInspect`; payload → descriptor mapping and fallback |
| `internal/app/regixtry/service.go` | Modified | `manifestEnvelope` gains `ArtifactType`; `parseManifestPayload` threads it through. Push validation and scan suppression unchanged |
| `internal/ports/regixtry.go` | Modified | `MetadataStore.ListReferrers(ctx, tenant, repository, subjectDigest)` |
| `internal/infra/metadata/sqlite/store.go` | Modified | `subject_digest` column + `ALTER TABLE`; one-time backfill; `PublishManifest` persists it; `ListReferrers` |
| `internal/domain/regixtry/manifest.go` | Modified | `ArtifactType` field on `Manifest`, populated by `NewManifest` |
| `internal/domain/regixtry/descriptor.go` | Unchanged | Deliberately not widened — see Decision 4 |
| `internal/domain/signing` | Unchanged | See Decision 2 |
| `internal/app/regixtry/service_gc.go` | Unchanged | GC still computes blob candidates only |
| `README.md` | Modified | Referrers endpoint; `provenance: false` note retained |

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| `ListReferrers` copies `ListReferencedBlobDigests`' global scoping instead of `ResolveManifest`'s tenant+repo scoping, leaking referrers cross-tenant | High | Cross-tenant and cross-repository negative tests are mandatory, not optional; named explicitly for `sdd-design` |
| Backfill is slow or re-runs on every boot on a large registry | Med | Persisted completion marker; one pass, bounded by manifest count; timing asserted on a seeded store |
| `handleV2` dispatch has zero tests, so a new case can silently misroute `manifests/` or `tags/list` | Med | Characterization tests for current dispatch land before the `referrers/` case, mirroring `manifest-blob-delete/design.md` sequencing |
| A repository literally containing a path segment `referrers` misroutes | Med | `splitRepositoryPath` behavior asserted directly, following the `/scan-status` collision reasoning at `router.go:163-168` |
| Empty-result semantics implemented as `404`, breaking conformance | Med | Explicit scenario for an unknown subject digest asserting `200` + empty `manifests[]` |
| Read is expected to also fix `provenance: false` | Med | Named a non-goal in Scope, Intent, and README |
| `subject_digest` and `payload` disagree after a repush | Low | `PublishManifest`'s `ON CONFLICT DO UPDATE` must include `subject_digest` alongside `payload`, unlike `pushed_by` which is deliberately insert-only |
| `ResolveManifest` still returns `Subject: nil`, so a future caller assumes subject is unavailable | Low | Documented as a known read-path inconsistency; out of scope to fix here, flagged for `sdd-design` |

## Rollback Plan

`git revert` the change commits. The reverted binary is byte-compatible with the migrated database: `subject_digest`
is `NOT NULL DEFAULT ''` and is never read by old code, so an extra column is inert — SQLite needs no down-migration
and none is written. The endpoint reverts to `404 NAME_UNKNOWN`, which is exactly today's behavior, so no client can
regress from a state it depended on. The backfill only writes a column that did not previously exist; it never
modifies `payload`, `digest`, `pushed_by`, tags, or blob files, so it is not destructive and does not need undoing.
Re-applying after a revert re-runs the same idempotent migration and backfill.

## Dependencies

- `registry-foundation` (in flight) — `/v2` dispatch and the SQLite manifest schema are extended, not replaced.
- `registry-auth-v1` (shipped) — `ActionInspect` and its shared challenge scope are reused as-is.
- `manifest-blob-delete` (shipped) — owns the orphaned-referrer gap this change documents but does not fix.
- No new external dependency; no new configuration flag; no new scope action.
- Strict TDD: every layer lands test-first with `go test ./...`, each assertion proven able to fail first.

## Success Criteria

- [ ] `GET /v2/<name>/referrers/<digest>` returns an OCI Image Index of every manifest in that repository whose `subject.digest` matches.
- [ ] A subject digest with no referrers returns `200` and an empty `manifests[]`, not `404`.
- [ ] A subject digest that does not exist in the repository returns `200` and an empty `manifests[]`, not `404`.
- [ ] After deleting a subject manifest, its surviving referrers are still returned, and nothing 500s.
- [ ] `?artifactType=` filters the result and sets `OCI-Filters-Applied: artifactType`; an unfiltered request does not set the header.
- [ ] Each response descriptor carries `artifactType` from the manifest when present, falling back to `config.mediaType`, plus its `annotations`.
- [ ] Content pushed **before** this change ships is returned, proving the backfill ran.
- [ ] The backfill is idempotent: running it twice changes no row and produces the same results.
- [ ] Referrers in another repository or another tenant are never returned, asserted directly.
- [ ] A plain `pull`-scoped token is accepted; an unauthorized principal is rejected.
- [ ] A cosign v3 bundle referrer manifest is discoverable via Referrers; a legacy `.sig` artifact is not, and its tag-based path still works unchanged.
- [ ] Push, pull, tag listing, catalog, delete, scan queueing, and signature verification behavior are unchanged.

## Proposal question round — resolved

Answered above so `sdd-spec`/`sdd-design` do not reopen them: (1) **backfill**, because `subject_digest` is
recoverable from `payload` and the `pushed_by` precedent is about unrecoverable data; (2) **legacy cosign stays
separate**, because synthesizing an `artifactType` would fabricate spec-defined data and cosign v3 is already
subject-bearing; (3) **orphaned referrers are accepted debt**, because a delete-time check would make signed bad
content harder to withdraw than unsigned; (4) **`artifactType` is derived at query time, not stored**, because the
payload parse is unavoidable for `annotations` and a stored derived value can drift.

**Assumptions the user may want to correct**: this change deliberately does not touch the `provenance: false`
workaround; Referrers is not exposed in the TUI; and no opt-in flag gates the endpoint (unlike
`REGISTRY_DELETE_ENABLED`), because a read-only listing of already-stored content adds no new authority.

### Open questions for `sdd-design`

1. The completion-marker mechanism for the one-time backfill — a `schema_meta`-style row, or a cheaper guard — given
   this repo has no versioned migration system, only the flat idempotent statement list.
2. Whether `subject_digest` needs an index, or whether `(tenant, repository_id, subject_digest)` selectivity on
   expected registry sizes makes one premature.
3. Whether `ListReferrers` returns raw rows or a `ports.ReferrerRow` type, and where exactly the payload→descriptor
   mapping lives so the SQLite adapter never parses OCI JSON.
4. Response ordering — the spec does not mandate one; whether to sort by digest for deterministic, testable output.
5. Whether `ResolveManifest` should start populating `Subject` in the same change or stay untouched.
6. Whether a `referrers/` path segment can collide with a legal repository name under `splitRepositoryPath`.
