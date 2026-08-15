# Exploration: manifest-blob-delete

## Goal

Investigate adding a DELETE capability for manifests/tags/blobs to regixtry,
which today only grows content. Scoped to the delete primitive itself, NOT a
retention/GC policy engine (separate future change).

## Current State (verified against `feature/manifest-blob-delete`, branched from `develop`)

### No delete route exists anywhere on the registry surface
- `internal/protocol/http/router.go:145-179` (`handleV2`) — the switch that
  dispatches `blobs/uploads`, `blobs/uploads/<id>`, `blobs/<digest>`,
  `manifests/<ref>`, `tags/list` has no branch for a bare `blobs/<digest>`
  DELETE and no manifest-delete branch either; both `blobs/<digest>` and
  `manifests/<ref>` share single handlers that only switch on method
  internally.
- `handleBlobRead` (router.go:284-312) only handles GET/HEAD; any other
  method (including DELETE) falls through to the `default:` 405 branch with
  `Allow: GET, HEAD`. Confirmed verbatim — no DELETE case.
- `handleManifest` (router.go:314-364) only handles PUT/GET/HEAD; DELETE
  falls through to the `default:` 405 branch with `Allow: PUT, GET, HEAD`.
  Confirmed verbatim — no DELETE case.
- The ONE existing DELETE in the whole `/v2/...` surface is
  `handleUploadState`'s `case stdhttp.MethodDelete:` (router.go:276-277 —
  upload cancellation), which returns a fixed
  `domain.NewValidationError("upload cancellation is not implemented in this
  slice")` mapped to OCI error code `"UNSUPPORTED"`. This is the one
  confirmed precedent for "delete not implemented" signaling in this
  codebase — it is a validation-error-shaped 4xx, not a 405, i.e. the route
  exists and is recognized but explicitly refuses the verb. Worth deciding
  whether manifest/blob DELETE should use this same "acknowledged but
  disabled" shape (paired with an opt-in flag, see below) vs. a bare 405.
- OCI distribution-spec confirms DELETE is fully optional
  (end-9 manifests, end-10 blobs, both `202 Accepted` on success,
  `404/400/405` on failure) — "Registries MAY implement deletion or they MAY
  disable it. ... a registry MAY implement tag deletion, while others MAY
  allow deletion only by manifest." regixtry currently exercises the
  "disable it" branch for uploads only; manifests/blobs currently 405
  outright (no route match), not even an acknowledged-but-disabled response.

### `ListManifestBlobs` — confirmed exact shape, one direction only
- `internal/ports/regixtry.go:48` —
  `ListManifestBlobs(ctx, tenant, repository, manifestDigest) ([]domain.Descriptor, error)`.
- `internal/infra/metadata/sqlite/store.go:499-539` — implementation joins
  `manifest_blobs mb JOIN manifests m ON m.id = mb.manifest_id JOIN
  repositories r ON r.id = m.repository_id WHERE ... m.digest = ?`, ordered
  by `mb.position`. This is genuinely one-manifest-to-its-blobs only.
- **No aggregate "which manifests reference this blob, across the
  repository/tenant" query exists today** — confirmed by reading the full
  `MetadataStore` interface (`internal/ports/regixtry.go:23-89`): every
  `List*`/`Get*` method there is scoped by manifest, tag, or repository, none
  by blob digest. A reference-count/mark-and-sweep GC (see below) would need
  a new query such as `ListManifestsReferencingBlob(ctx, tenant, digest)` or
  equivalent, or the offline mark-and-sweep style Docker/Harbor both use
  (scan all manifests → build a live digest set → sweep blobs not in it),
  which sidesteps needing a per-blob reverse-reference query at all.

### Schema is fully relational with cascading FKs already in place
`internal/infra/metadata/sqlite/store.go:1251-1290`:
```
repositories(id) <--(ON DELETE CASCADE)-- manifests(repository_id)
manifests(id)    <--(ON DELETE CASCADE)-- tags(manifest_id)
manifests(id)    <--(ON DELETE CASCADE)-- manifest_blobs(manifest_id)
```
- `manifests` UNIQUE(tenant, repository_id, digest) — one row per manifest
  digest per repo.
- `tags` UNIQUE(tenant, repository_id, name), `manifest_id NOT NULL` — a tag
  always points to exactly one manifest; multiple tags CAN point to the same
  `manifest_id` (retag scenario, confirmed via `PublishManifest`'s
  `ON CONFLICT(tenant, repository_id, digest) DO UPDATE` upsert-by-digest
  logic at store.go:227-234).
- **Important consequence**: deleting a `manifests` row by digest
  automatically cascades and deletes every `tags` row pointing to it (FK
  `ON DELETE CASCADE`), and every `manifest_blobs` row for it. This means a
  "delete manifest by digest" operation is naturally atomic and untags
  everything pointing to that digest in one DB operation — matching the OCI
  spec's own reasoning for restricting DELETE-by-tag in some
  implementations (deleting a shared digest via one of several tags is
  ambiguous about whether the other tags should survive).
- `manifest_blobs` stores blob `digest`/`media_type`/`size` as plain columns
  (not a JOIN to a separate `blobs` table — there is no `blobs` metadata
  table; blob existence is filesystem-only via `fsblob.Store.BlobExists`).
  So a blob's "still referenced" status can ONLY be answered by querying
  `manifest_blobs` across ALL manifests/repositories/tenants for that
  digest — there is no single source of truth blob row to decrement a
  refcount on. This favors mark-and-sweep (scan-all) over live reference
  counting for this codebase's current schema shape.
- `uploads` table is NOT relationally tied to `manifests`/`repositories` (plain
  `repository TEXT`, no FK) — irrelevant to manifest/blob delete, no
  cascade interaction.

### Authorization predicates — confirmed shape
`internal/domain/auth/principal.go:20-30`:
```go
func (p Principal) HasWriteAccess(repository string) bool {
    return p.hasGrantedRepositoryAccess(repository, RepoRole.AllowsWrite) && p.scopeAllowsRepository(repository, Scope.AllowsPush)
}
func (p Principal) HasRepoAdminAccess(repository string) bool {
    if !p.hasGrantedRepositoryAccess(repository, RepoRole.AllowsAdmin) { return false }
    return p.scopeAllowsRepository(repository, Scope.AllowsPush)
}
```
`internal/domain/auth/grant.go:34-44`: `RepoRoleReader.AllowsRead()` only;
`RepoRoleWriter.AllowsWrite()` (write+read); `RepoRoleAdmin.AllowsAdmin()`
(admin+write+read, strictly superset). So `repo-writer` is exactly the same
credential that already gates PUT (push) on manifests/blobs
(`ports.ActionPush`, checked in `PublishManifest`/`CompleteUpload`). There is
no separate "delete" scope/action verb today — `ports.Action.Verb` is one of
`ActionPull`/`ActionPush`/`ActionCatalog`/`ActionInspect` (confirmed via
router.go usage); a delete action would need a new `ports.ActionDelete` verb
(or reuse `ActionPush`) plus Docker-scope derivation
(`internal/app/auth/service.go:669-729`, `intersectRequestedActions`) would
need to decide whether `delete` maps from `repo-writer` or is admin-gated.

Note: `Principal.IsReadOnly` (registry-wide read boolean) already exists in
`principal.go` — the prior `registry-acl-v1` gap #3 recommendation appears
already implemented on this codebase's current state, confirmed via direct
read of `hasGrantedRepositoryAccess` (principal.go:54-75).

## Industry precedent (verified via WebSearch/WebFetch)

- **Docker/distribution reference registry**: deletion is off by default;
  `REGISTRY_STORAGE_DELETE_ENABLED=true` (or `storage.delete.enabled: true`
  in config yaml) must be set before DELETE routes even activate. Actual
  disk reclamation is a SEPARATE, offline, explicit operation:
  `registry garbage-collect [--dry-run] /etc/docker/registry/config.yml`.
  GC requires the registry to be in read-only mode or stopped entirely
  ("stop-the-world garbage collection") — concurrent pushes during GC risk
  a layer being deleted out from under an in-flight image. GC algorithm is
  **mark-and-sweep**, not reference counting: scan all manifests, build the
  set of digests they reference, then sweep every blob not in that set.
  Deleting a manifest via the API only "removes references... and makes
  them eligible for garbage collection" — it does NOT free storage
  immediately; that requires the separate `garbage-collect` run.
- **OCI distribution-spec** (spec.md, end-9/end-10): DELETE for manifests
  and blobs both return `202 Accepted`, is fully optional per registry, and
  registries MAY choose to support manifest deletion by digest only (not by
  tag) precisely to avoid the ambiguity of deleting a shared digest via one
  of several tag names — resolved automatically in regixtry's schema anyway
  via the FK cascade described above. The spec is silent on GC — it only
  standardizes the deletion API surface, not how/when storage is reclaimed.
- **Harbor**: mirrors the same two-operation split — tag retention
  (explicitly the "WHAT/WHEN to keep" policy engine, out of scope here,
  matching this change's own stated non-goal) is a separate concept from
  garbage collection, which is also offline/read-only-mode and also
  reclaims only blobs with zero remaining manifest references, on an
  admin-triggered or scheduled basis.
- **Consensus across all three**: manifest/tag delete (metadata mutation,
  fast, immediate) and blob GC (storage reclamation, slow, requires a
  consistency-safe window) are treated as two distinct operations by every
  registry investigated, not one atomic "delete = free disk" operation.

## The Central Design Question: delete-vs-GC split

**Recommendation: split into two operations, ship metadata delete now, defer
blob GC to a separate future change** (matching Docker/Harbor precedent and
the change's own explicit non-goal scoping):

1. **This change**: `DELETE /v2/<name>/manifests/<digest>` — deletes the
   `manifests` row by digest, which cascades (existing FK) to remove every
   `tags` row and every `manifest_blobs` row pointing to it. This is a pure
   metadata operation: no blob files are touched, no cross-manifest
   reference scan is needed, response is `202 Accepted` per spec. Optionally
   also implement `DELETE /v2/<name>/manifests/<tag>` (untag only — delete
   the single `tags` row, leave the manifest and other tags on it intact) as
   a strictly smaller, lower-risk variant of the same primitive; the OCI
   spec explicitly permits registries to support one, both, or neither.
   `DELETE /v2/<name>/blobs/<digest>` is explicitly NOT recommended for this
   change (see below) — leave it 405 or "UNSUPPORTED" like uploads today.
2. **Deferred future change**: an offline/maintenance-mode blob GC sweep
   (mark-and-sweep over `manifest_blobs` across all tenants/repositories,
   analogous to `registry garbage-collect`), which is the only safe way to
   answer "is this blob file still referenced anywhere" given the current
   schema has no reverse-reference query and no per-blob refcount column.

**Why not eager blob deletion at manifest-delete time**: blobs are shared
across manifests (layers reused between tags/images, confirmed by the
`manifest_blobs` schema allowing the same digest to appear under many
`manifest_id` rows with no uniqueness constraint on `digest` itself).
Deleting a manifest's blob files immediately, without checking every OTHER
manifest across every OTHER repository/tenant for the same digest, WILL
corrupt other images sharing that layer. A safe immediate-delete would
require, per blob, a live scan of `manifest_blobs` for other referencing
rows before unlinking the file — expensive per-delete-call, and still racy
against a concurrent push publishing a new manifest referencing that same
blob mid-delete (the exact race Docker's docs warn about, which is why they
require read-only/offline mode for GC rather than doing it inline).

**Future-proofing note for the deferred GC engine**: whatever this change
builds should not paint the future engine into a corner. Concretely: (a)
keep manifest-delete transactional and independent of any GC trigger — GC
should be able to run as a standalone sweep at any later time, not
dispatched synchronously from delete; (b) do not add a per-blob refcount
column now, since a later mark-and-sweep implementation would need a fresh,
consistency-checked scan anyway (a stale counter is worse than no counter);
(c) whatever `ports.ActionDelete`/`HasWriteAccess`-gated method this change
adds to `MetadataStore`/`Service` should be named around "delete manifest",
not "delete and GC", so a later `SweepUnreferencedBlobs`-style method can be
added independently without renaming this one.

## Authorization Recommendation

**`repo-writer` (i.e., reuse `HasWriteAccess`), not `repo-admin`.** Reasoning:
- `RepoRoleWriter.AllowsWrite()` is exactly the credential that already
  gates manifest/blob PUT (push) — CI pipelines that can push a bad image
  can already push a broken/vulnerable manifest today; letting the same
  writer delete/replace it is symmetric, not a privilege escalation, and
  matches how most registries treat push+delete as the same tier (Docker's
  reference registry does not gate DELETE by a separate role at all — it's
  purely the `REGISTRY_STORAGE_DELETE_ENABLED` server-wide flag that gates
  it, no extra per-repo role check beyond ordinary write auth).
- `repo-admin` (`HasRepoAdminAccess`) is reserved in this codebase for grant
  management (`PutRepoGrant`/`DeleteRepoGrant`/`ListRepoGrants`, confirmed
  in the prior `registry-acl-v1` exploration) — overloading it for
  manifest delete would conflate "who can manage repo permissions" with
  "who can delete registry content," two different concerns.
- Counter-consideration (flagged as an open question below, not resolved
  here): delete is more destructive/less reversible than push (a bad push
  can be overwritten by a good push; a delete needs the client to still
  have the original layers to re-push). If the product wants a stricter
  bar, gating on `repo-admin` instead is a one-line change
  (`HasWriteAccess` → `HasRepoAdminAccess`) — flagged for product decision.

**Opt-in server flag: yes, recommended, reusing the existing config
pattern.** `cmd/regixtry/main.go` already has the exact precedent needed —
`REGISTRY_TRIVY_ENABLED` via `flags.BoolVar(&cfg.TrivyEnabled,
"trivy-enabled", parseBoolEnv("REGISTRY_TRIVY_ENABLED", false), ...)`
(main.go:747). A `REGISTRY_DELETE_ENABLED`/`--delete-enabled` flag
following the identical `parseBoolEnv(..., false)` default-off pattern is a
direct, low-effort adoption of Docker's own precedent and requires no new
config mechanism — just a new field on the existing config struct and one
more `flags.BoolVar` line alongside the Trivy ones (main.go:747-753).
Default should be `false` (opt-in), matching Docker's own default and this
being a destructive, rarely-needed-for-most-deployments capability exactly
as Docker's own docs frame it.

## Affected Areas

- `internal/protocol/http/router.go` — `handleManifest` needs a
  `case stdhttp.MethodDelete:` branch (and its `Allow` header list on the
  `default:` 405 updated); `handleV2`'s dispatch switch is otherwise
  unaffected since `manifests/<ref>` already routes through `handleManifest`.
  If tag-only delete is included, no new route is needed — same handler,
  same path shape, digest-vs-tag is already disambiguated by
  `parseManifestPayload`-adjacent logic (`domain.ParseDigest` vs plain tag
  string, same pattern used in `PublishManifest`/`OpenManifest`).
- `internal/app/regixtry/service.go` / `queries.go` — new
  `DeleteManifest(ctx, repositoryName, reference)` service method following
  `PublishManifest`'s shape: parse repository, authorize
  (`ports.ActionDelete` or reuse `ActionPush`, TBD), resolve reference to a
  manifest, call a new `MetadataStore.DeleteManifest`. Needs a decision on
  whether `ports.Action` gets a new `ActionDelete` verb or reuses
  `ActionPush` (Docker-scope derivation implications).
- `internal/ports/regixtry.go` — `MetadataStore` interface needs a new
  `DeleteManifest(ctx, tenant, repository, reference) error` method
  (reference = digest, or digest-or-tag if tag-delete is included);
  `ports.Action` may need a new `ActionDelete` constant.
- `internal/infra/metadata/sqlite/store.go` — new `DeleteManifest`
  implementation: `DELETE FROM manifests WHERE tenant = ? AND repository_id
  = ? AND digest = ?` inside a transaction (cascades handle tags/
  manifest_blobs automatically per the FK schema above); must return a
  typed `domain.NewNotFoundError` when zero rows affected, mirroring
  `DeleteUpload`/`DeleteRepositoryFeatureOverride`'s existing pattern
  (confirmed convention at `internal/ports/regixtry.go:85-88` comment).
- `internal/domain/auth/principal.go` — no change needed if reusing
  `HasWriteAccess`/`HasRepoAdminAccess` as-is (both already exist and are
  correctly shaped); only touched if a brand-new predicate is desired
  instead of reusing an existing one.
- `cmd/regixtry/main.go` — new `REGISTRY_DELETE_ENABLED` flag/config field,
  following the `TrivyEnabled`/`parseBoolEnv` pattern exactly
  (main.go:747-753); the flag needs to be threaded into
  `Service`/`Router` construction so `handleManifest`'s DELETE case can
  check it and return the same `"UNSUPPORTED"` OCI error code as the
  existing upload-cancellation precedent when disabled.
- `internal/protocol/http/router.go` docker-error-code path — the
  `"UNSUPPORTED"` error code used by `handleUploadState`'s DELETE case
  (router.go:277) is the direct precedent to reuse when
  `REGISTRY_DELETE_ENABLED` is false.
- No TUI changes are implied by this change per the brief's scope (delete
  capability, not a delete UI) — flagged as an open question below in case
  the product wants a manual delete action surfaced in the console.

## Non-Goals (explicit, to prevent scope creep into `sdd-propose`)

- **Retention/policy engine** (auto-selecting WHAT to delete via rules like
  "keep last N tags", "keep tags newer than 30 days", pattern-based rules) —
  explicitly out of scope, matches Harbor's separate "tag retention" concept.
  This change only builds the manual DELETE primitive a future policy
  engine would call.
- **Blob garbage collection / disk reclamation** — explicitly out of scope
  per the delete-vs-GC split recommendation above; this change only deletes
  metadata rows (manifests/tags/manifest_blobs via cascade), never touches
  blob files on disk. A future change implements the offline mark-and-sweep
  sweep.
- **`DELETE /v2/<name>/blobs/<digest>`** (deleting an individual blob
  directly, bypassing manifest reference checking) — explicitly NOT
  recommended for this change; it is the one operation genuinely unsafe to
  expose without the GC engine's cross-manifest reference scan, and no
  registry investigated exposes it as a routine, unchecked operation.
- **Soft-delete / undo window** — not designed here; flagged as an open
  question below, not assumed either way.
- **TUI delete screens** — not designed here; the brief scopes this to "the
  DELETE capability itself" (the API), and no existing TUI precedent for a
  destructive action was found to extend.

## Risks

- No covering tests exist today for any of the touched call sites
  (`handleManifest`, `PublishManifest`, `ListManifestBlobs` all show
  "no covering tests found" or only indirect coverage via
  `codegraph_explore` blast-radius) — Strict TDD is enabled for this
  project, so `DeleteManifest` at every layer (store, service, router) needs
  test-first coverage, not retrofitted tests.
- The FK-cascade delete-by-digest behavior (deleting a manifest removes ALL
  tags pointing to it, silently) is correct per OCI-spec reasoning but is a
  surprising side effect if not clearly documented/tested — a client
  deleting one tag's digest without realizing three other tags point to the
  same digest will unexpectedly lose all of them. This needs an explicit
  scenario test and probably a response detail (e.g., which tags were
  removed) rather than a bare 202 with no body.
- Reusing `ActionPush`/`HasWriteAccess` for delete (recommended above) means
  Docker-scope-derived tokens that were only ever meant to authorize push
  will also authorize delete without any explicit "delete" scope ever being
  requested/granted — `intersectRequestedActions`
  (`internal/app/auth/service.go:669-729`) would need review to confirm
  whether reusing the push action is acceptable or whether a distinct
  `delete` Docker scope action is expected by client tooling (this may
  affect real-world Docker/`crane`/`oras` client compatibility, since some
  clients construct scope strings assuming standard action names —
  worth confirming `delete` is itself a recognized Docker scope action
  string during design, not assumed here).
- `manifest_blobs` has no `digest` uniqueness/index beyond
  `PRIMARY KEY(manifest_id, position)` — a future cross-manifest "which
  manifests reference this blob" query (needed for eager-delete or an
  online refcount approach, both explicitly NOT recommended here) would
  currently require a full-table scan without a new index on
  `manifest_blobs(digest)`; irrelevant to this change's recommended
  metadata-only scope, but worth flagging for whoever designs the deferred
  GC change.

## Open Questions Requiring a Product Decision

1. Should `DELETE /v2/<name>/manifests/<reference>` be enabled by default,
   or require the explicit `REGISTRY_DELETE_ENABLED` opt-in flag (Docker's
   precedent, recommended default: opt-in/false)?
2. Should delete require `repo-writer` (symmetric with push, recommended
   above) or the stricter `repo-admin` (given delete's higher
   irreversibility relative to push)?
3. Should regixtry support tag-only delete (`DELETE .../manifests/<tag>`,
   untag without touching the manifest or its other tags) in addition to
   digest delete, or digest-only (simpler, avoids the "which tag did you
   mean" ambiguity OCI spec discussion raises)? Recommend supporting both
   since the schema trivially supports both (tags row delete vs manifests
   row delete) at near-zero extra cost.
4. Should there be a soft-delete/undo window (e.g., tombstone the manifest
   row for N days before physically removing it) or is a hard, immediate
   delete acceptable given blob files are untouched anyway (i.e., a client
   can always re-push the same content to "undo")?
5. Should `DELETE /v2/<name>/blobs/<digest>` ever be exposed in a later
   change, or should blob removal always be GC-only (never client-triggered
   per-blob)? Recommend GC-only, but this is a product call, not purely
   technical.
6. Does reusing `ActionPush`'s existing push scope for delete authorization
   break compatibility with any real-world registry client that expects a
   distinct `delete` Docker scope action string? Needs confirmation during
   design before committing to the "reuse ActionPush" recommendation above.

## Ready for Proposal

Yes — current-state claims from the brief are all confirmed (with the
`ListManifestBlobs` reverse-reference gap explicitly verified as a true
gap), industry precedent research is complete (Docker opt-in flag + offline
mark-and-sweep GC, OCI spec's optional/digest-vs-tag DELETE semantics,
Harbor's retention-vs-GC split), and the central manifest-delete-vs-blob-GC
design question has a clear, evidence-backed recommendation (split into two
operations; this change ships metadata-only delete, defers GC). Six open
questions above should be carried into `sdd-propose` for explicit
product decisions before spec/design work begins, particularly #2 (role
gating) and #6 (Docker scope compatibility), since both affect the shape of
the authorization work at the service/router layer.
