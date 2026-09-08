# Exploration: OCI 1.1 Referrers API support

## Current State

**Router dispatch** — `internal/protocol/http/router.go`'s `handleV2` (line 123) switches on `suffix` after `splitRepositoryPath`. Cases today: `blobs/uploads`, `blobs/uploads/<id>`, `blobs/<digest>`, `manifests/<ref>` (with scan-status/signature-status/secret-scan-status sub-suffix carve-outs), and `tags/list`. There is no `referrers/` case, so `GET /v2/{repo}/referrers/{digest}` falls to `default:` (line 193) and returns 404 `NAME_UNKNOWN`.

**Push-time subject validation** — `internal/app/regixtry/service.go`'s `PublishManifest` (line 274, subject check at 312-319): if `manifest.Subject != nil`, it calls `s.metadata.ResolveManifest(ctx, tenant, repository, manifest.Subject.Digest.String())` and returns `domain.NewConflictError` (409) if the subject digest isn't already a manifest in the same repository. This forces `provenance: false` on `docker/build-push-action` workflows when the base image isn't already resolvable in-repo at attestation-push time — confirmed necessary live for the `govault-csi-provider` release pipeline.

**Nothing indexes `subject` outside the payload.** `manifests` table columns (`internal/infra/metadata/sqlite/store.go:1682-1694`): `id, tenant, repository_id, digest, media_type, size, payload, created_at, pushed_by`. No `subject_digest` column — `Manifest.Subject` is parsed from the payload but only ever lives inside the stored `payload` BLOB. There is also **no persisted `artifactType` anywhere**: `manifestEnvelope` (service.go:504-510) has `MediaType, Config, Layers, Subject, Annotations` — no top-level `ArtifactType`, and `domain.Descriptor` has no `ArtifactType` field either. Per the OCI 1.1 spec, each Referrers response descriptor's `artifactType` should be the manifest's own `artifactType` if present, else fall back to `config.mediaType` — today's schema can supply the fallback but not the primary field.

**Migration pattern (confirmed, directly reusable)** — the schema setup in store.go (~line 1670) is a flat `[]string` of `CREATE TABLE IF NOT EXISTS ...` (with the target column already included, e.g. `manifests.pushed_by`) followed by matching idempotent `ALTER TABLE ... ADD COLUMN ...` statements for pre-existing databases (`ALTER TABLE manifests ADD COLUMN pushed_by TEXT NOT NULL DEFAULT '';` at line 1914). The runner (lines 1961-1969) executes every statement in order and swallows only `"duplicate column name"` errors. A `subject_digest TEXT NOT NULL DEFAULT ''` column follows this exact pattern. No numbered/versioned migration system exists — this flat idempotent-statement list *is* the whole migration mechanism project-wide.

**Cosign's existing, separate, working mechanism** — `internal/domain/signing`:
- `SignatureTag` maps a digest to the legacy `sha256-<hex>.sig` tag. The `.sig` manifest it resolves to has **no `subject` field at all** — pure tag-name convention, predating OCI 1.1.
- `BundleIndexTag` maps a digest to the modern `sha256-<hex>` tag (no suffix). The OCI Image Index it resolves to is also **subject-less** — a plain index whose `manifests[]` array points at the referrer manifest by digest.
- The manifest that array points at — the actual Sigstore Bundle referrer manifest — **does** carry `subject` (confirmed via `internal/domain/signing/testdata/bundle-referrer-manifest.json:21-25`). This is the OCI-1.1-native document cosign v3 already pushes; only *discovery* of it today is tag-based, never via a Referrers API call.
- `isReferrerArtifactPush` (service.go:455-471) is the one place these two signals (`manifest.Subject != nil` OR tag matches `SignatureTag`/`BundleIndexTag`) are already unified — but only to suppress an unwanted vulnerability scan on push, not for discovery.

**GC does not touch manifest rows or `subject`** — `internal/app/regixtry/service_gc.go`'s `gcCandidates` only computes candidate **blob files** (`ListBlobs` minus `ListReferencedBlobDigests`, which is `SELECT DISTINCT digest FROM manifest_blobs`, store.go:412-435). It never reads `manifests.digest` or `subject`, and there is no manifest-level GC anywhere — manifest rows are only removed by `manifest-blob-delete`'s explicit `DeleteManifestByDigest`/`DeleteTag`. `DeleteManifestByDigest` (store.go:336-410) unconditionally deletes and cascades `tags`/`manifest_blobs` for that digest — it does **not** check whether any other manifest's `subject` points at the digest being deleted. So an orphaned referrer is already possible today, independent of this change: the referrer's own row and blobs survive (its own `manifest_blobs` rows keep it referenced for blob GC), but `ResolveManifest` on the now-missing subject 404s. Nothing currently detects, reports, or cleans this up.

**Authorization plumbing (confirmed, straightforward to reuse)** — every `/v2/` route follows `action := ports.Action{Verb: ..., Repository: repository}` → `withPrincipal` (authenticate only) → service method → `s.authorize(ctx, action)`. Reads use two verbs depending on shape: `handleManifest`'s GET/HEAD uses `ActionPull` (re-asserted service-side in `OpenManifest`), while the **list-shaped** `handleTags` uses `ActionInspect` (router.go:484, service-side in `Tags`, queries.go:562) — `ActionInspect` intentionally shares `ActionPull`'s challenge scope (`Action.Scope()`) so a plain pull token already satisfies it. A Referrers endpoint is list-shaped like `tags/list`, not single-manifest, so `ActionInspect` is the precedent-consistent choice.

## Affected Areas

- `internal/protocol/http/router.go` — `handleV2` needs a `referrers/` case; new `handleReferrers` handler (parallel to `handleTags`), including `?artifactType=` filtering and the `OCI-Filters-Applied` response header per spec.
- `internal/app/regixtry/queries.go` — new `Referrers(ctx, repositoryName, digest, artifactType string) (ReferrersResult, error)`, authorizing `ActionInspect`.
- `internal/ports/regixtry.go` — new `MetadataStore` method, e.g. `ListReferrers(ctx, tenant, repository, subjectDigest string) ([]ReferrerDescriptor, error)`.
- `internal/infra/metadata/sqlite/store.go` — schema: `subject_digest` column (inline `CREATE TABLE` + matching `ALTER TABLE ... ADD COLUMN`); `PublishManifest` (store.go:197-282) must persist `manifest.Subject.Digest.String()` (or `""`); new `ListReferrers` filtering `WHERE tenant = ? AND repository_id = ? AND subject_digest = ?`.
- `internal/app/regixtry/service.go` — `PublishManifest`'s existing subject-must-exist check is untouched (out of scope for this change).
- `internal/domain/regixtry/manifest.go` / `descriptor.go` — likely needs an `ArtifactType` field to populate the Referrers response's `artifactType`.
- `internal/domain/signing` — no code changes required unless Approach 2 below is chosen.
- `openspec/changes/manifest-blob-delete/` — confirmed on disk (`design.md`, `tasks.md`, `verify-report.md`); useful shape/artifact template for this change.

## Approaches

1. **Index `subject_digest` as its own column; Referrers reads only OCI-1.1-native subject-bearing manifests; legacy cosign tag-based signatures stay undiscoverable via Referrers.**
   - Pros: Smallest, most spec-literal change. No modification to the already-working, separately tested `signing` package. Matches the framing "alongside, not replacing" the legacy mechanism.
   - Cons: A Referrers-API-only client still cannot discover legacy `.sig` signatures — only subject-bearing artifacts (Sigstore Bundle referrer manifest, attestations/SBOMs).
   - Effort: Medium.

2. **Same as (1), plus synthesize legacy `.sig`/bundle-index tag artifacts into the Referrers response** by resolving `SignatureTag`/`BundleIndexTag` at query time and injecting them even though their stored rows have `subject_digest = ''`.
   - Pros: Closes discovery for Referrers-only clients.
   - Cons: Couples the new read path back into `internal/domain/signing`'s tag-name conventions; unclear `artifactType` for a `.sig` manifest that has none; mixes "what's indexed" with "what's reconstructable by convention."
   - Effort: Medium-High.

3. **Backfill `subject_digest` for existing rows** (re-parse stored `payload`, extract `subject.digest`, populate the column) instead of leaving pre-migration rows at `''` forever.
   - Pros: Referrers works for content pushed before this change ships.
   - Cons: The `pushed_by` precedent did **not** backfill historical values — accepted `''` as the untracked-history default. Backfill would be a new pattern.
   - Effort: Low, additive to (1).

## Recommendation

Approach 1, optionally combined with Approach 3's backfill. Approach 2 (surfacing legacy cosign tags through Referrers) should be an explicit open question for `sdd-propose`, not assumed — the two mechanisms are meant to stay alongside each other, and unifying discovery is a real behavior change to a separately-owned, separately-tested subsystem.

## Risks

- Orphaned referrers are already possible today via `manifest-blob-delete`; this change makes the gap queryable but doesn't fix it — needs an explicit design decision (accept as debt / delete-time check / GC-report-style detector).
- Missing `artifactType` persistence is a second, coupled schema gap the migration should scope explicitly. (Source: [OCI distribution-spec](https://github.com/opencontainers/distribution-spec/blob/main/spec.md))
- `subject_digest` lookups must be tenant+repository scoped like `ResolveManifest`/`ListTags`, not globally scoped like `ListReferencedBlobDigests` — easy to copy the wrong precedent.
- Zero existing test coverage on `handleV2`'s dispatch switch and `handleTags`; strict TDD means characterization tests should land first, mirroring `manifest-blob-delete/design.md`'s sequencing.

## Open Questions for Proposal/Design

1. Backfill `subject_digest` for pre-existing rows, or accept `''` for untracked history (matching the `pushed_by` precedent)?
2. Should legacy cosign signatures be synthesized into Referrers responses (Approach 2), or stay fully separate (Approach 1)?
3. How should orphaned/dangling referrers be handled — accepted as existing debt, a delete-time check, or a GC-report-style detector?
4. How does `artifactType` get persisted and populated, given no field for it exists today?

## Ready for Proposal

Yes. Ground truth confirmed against current code for router dispatch, subject validation, schema/migration pattern, GC scope, cosign's separate mechanism, and the `ActionInspect` authorization precedent.
