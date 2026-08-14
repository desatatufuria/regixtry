# Proposal: Manifest and Tag Deletion (manifest-blob-delete)

## Intent

regixtry content is append-only. Verified: `handleManifest` (`router.go:314-364`) answers `405 Allow: PUT, GET, HEAD`
for DELETE, and the only DELETE anywhere on `/v2/...` is upload cancellation, which refuses with OCI `UNSUPPORTED`
(`router.go:276-277`).

The operational cost is concrete: a mis-pushed image carrying a leaked secret stays pullable forever, a tag pinned to a
vulnerable digest cannot be withdrawn, and the only remediation is editing the registry's data out of band. regixtry
already *detects* bad content (Trivy scans, Gitleaks, signature verification, policy gate) but offers no way to *remove*
it — detection without withdrawal is the gap.

Success: an authorized CI job or operator withdraws a bad manifest, or untags a single reference, through the standard
OCI DELETE endpoint, on a registry where the operator explicitly enabled deletion.

## Scope

### In Scope

- **`DELETE /v2/<name>/manifests/<digest>`** — deletes the `manifests` row; the existing `ON DELETE CASCADE` FKs
  (`store.go:1251-1290`) remove every `tags` row pointing to it and all its `manifest_blobs` rows atomically.
  `202 Accepted`; `404 MANIFEST_UNKNOWN` when absent.
- **`DELETE /v2/<name>/manifests/<tag>`** — untag only: removes that one `tags` row, leaving the manifest and every
  other tag on it intact. Digest-vs-tag disambiguation reuses `domain.ParseDigest`, as `OpenManifest` does today.
- **Opt-in server flag** `REGISTRY_DELETE_ENABLED` / `--delete-enabled`, default `false`, following the exact
  `parseBoolEnv("REGISTRY_TRIVY_ENABLED", false)` pattern at `main.go:747`. When off, DELETE answers the same
  acknowledged-but-refused OCI `UNSUPPORTED` shape the upload-cancel branch already uses — not a bare 405.
- **A distinct `ports.ActionDelete` verb**, threaded through `intersectRequestedActions` and Docker-scope derivation
  (`internal/app/auth/service.go:669-729`) so `repository:<name>:delete` is a real, requestable scope action. A token
  scoped only `pull,push` MUST NOT authorize delete.
- **`repo-writer` tier authorizes delete** (`HasWriteAccess`), symmetric with push — not `repo-admin`.
- **Hard delete of metadata rows.** No tombstone, no undo window.
- **Test-first coverage at every layer** (store, service, router). Strict TDD is on and every touched call site has no
  covering tests today.

### Out of Scope

- **Retention/policy engine** (rules choosing *what* to delete) — separate future change; this ships only the primitive
  such an engine would call.
- **Blob garbage collection / disk reclamation** — separate future change. This change NEVER touches blob files on
  disk; only metadata rows.
- **`DELETE /v2/<name>/blobs/<digest>`** — not exposed. Blob removal stays GC-only; direct per-blob delete is unsafe
  without the cross-manifest reference scan the deferred GC change will own.
- **Soft-delete / tombstone / undo window** — deliberately not built. Blob files survive on disk, so re-pushing the
  same content is the undo path.
- **TUI delete screens** — API-only change. A natural follow-up, not part of this change.
- No `manifest_blobs(digest)` index and no per-blob refcount column are added; a stale counter is worse than none for
  the future mark-and-sweep sweep.

## Capabilities

### New Capabilities

- `manifest-deletion`: client-initiated withdrawal of a published manifest by digest or a single tag by name, gated by
  an opt-in server flag, with cascade semantics and OCI-compliant status/error codes.

### Modified Capabilities

- `repository-authorization`: a `repo-writer` grant MUST authorize deletion on its repository; a reader MUST NOT, and
  `repo-admin` remains reserved for grant management, not content removal.
- `registry-authentication`: issued bearer tokens and challenges MUST carry `delete` as a distinct Docker scope action,
  derived from the actor's `repo-writer`-or-higher role and never implied by `push`.

## Approach

Metadata-only delete now; blob GC deferred — the split every registry investigated already makes.

| Decision | Approach | Why |
|---|---|---|
| Delete vs GC | Delete removes rows; blob files untouched | Blobs are shared across manifests (`manifest_blobs` has no digest uniqueness); eager unlink corrupts other images and races concurrent pushes |
| Cascade | Rely on the existing FK cascade, one transaction | Delete-by-digest is atomic and untags everything on that digest in one DB operation — no new cascade logic |
| Enablement | Reuse `parseBoolEnv(..., false)` + the `UNSUPPORTED` refusal shape | Both precedents already exist in this codebase; no new config mechanism |
| Scope action | New `ActionDelete` verb, explicit `delete` scope | Reusing `ActionPush` would silently let push-only tokens delete |
| Not-found | `domain.NewNotFoundError` on zero rows affected | Mirrors `DeleteUpload` / `DeleteRepositoryFeatureOverride` convention (`ports/regixtry.go:85-88`) |

**Benchmark**: Docker/distribution ships delete off by default behind `REGISTRY_STORAGE_DELETE_ENABLED` and reclaims
disk only through a separate offline `registry garbage-collect` mark-and-sweep; Harbor splits tag retention from
garbage collection the same way; the OCI distribution-spec (end-9/end-10) makes DELETE optional, `202 Accepted`, and
explicitly permits supporting delete by digest, by tag, both, or neither. `delete` is a standard scope action in
Docker Hub, GHCR, and GitLab registry token scopes (`repository:name:pull,push,delete`).

**Design note for `sdd-design`**: name the new store/service method around *delete manifest*, never *delete and GC*, and
keep it independent of any GC trigger so a later `SweepUnreferencedBlobs` can be added without renaming or coupling.

## Affected Areas

| Area | Impact | Description |
|---|---|---|
| `internal/protocol/http/router.go` | Modified | `case MethodDelete` in `handleManifest`; flag check → `UNSUPPORTED`; updated `Allow` list on the 405 branch |
| `internal/app/regixtry/service.go` | Modified | `DeleteManifest(ctx, repository, reference)` — parse, authorize `ActionDelete`, resolve reference, call store |
| `internal/ports/regixtry.go` | Modified | `ActionDelete` verb; `MetadataStore.DeleteManifest` / tag-delete method |
| `internal/infra/metadata/sqlite/store.go` | Modified | Transactional delete by digest and by tag; typed not-found on zero rows |
| `internal/app/auth/service.go` | Modified | `intersectRequestedActions` derives `delete` from `repo-writer`+ |
| `internal/domain/auth/scope.go` | Modified | `actionDelete` scope-string constant |
| `internal/domain/auth/principal.go` | Unchanged | `HasWriteAccess` reused as-is |
| `cmd/regixtry/main.go` | Modified | `DeleteEnabled` config field + `flags.BoolVar`, threaded into router construction |
| Schema | Unchanged | Existing cascading FKs are sufficient; no migration |

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Digest-delete silently removes tags the client did not name | High | Explicit scenario test; response detail naming removed tags rather than a bare bodyless 202 |
| Push-only tokens gain delete authority | High | Distinct `ActionDelete` verb; negative test asserting a `pull,push` token is rejected on DELETE |
| Client tooling does not request the `delete` scope, breaking real-world deletes | Med | Verified standard across Docker Hub/GHCR/GitLab; design must confirm challenge/scope round-trip with a real client |
| Deleting last tag leaves an unreachable-but-stored manifest | Med | Accepted and documented: untagged manifests stay addressable by digest until the deferred GC change |
| Every touched call site is untested today | Med | Strict TDD: characterization tests for current 405 behavior land before the DELETE branch |
| Flag left enabled by default in some deploy path | Med | Default `false` asserted by test on the config parse, not just in code |
| Scope creeps toward eager blob deletion | Low | Named as an explicit non-goal; `sdd-design` must not touch `fsblob.Store` |

## Rollback Plan

`git revert` the change commits. There is **no schema migration** — the change relies on FK cascades that already
exist — so a reverted binary is byte-compatible with the database and simply answers 405 again on DELETE. The flag
defaults to `false`, so any deployment that never opted in is unaffected by both the change and its revert.
Operationally, setting `REGISTRY_DELETE_ENABLED=false` disables the capability without a code revert or restart of
anything but the registry process. Already-deleted metadata rows are not restored by the revert, but their blob files
were never touched, so the same content can be re-pushed to restore the manifest and its tags.

## Dependencies

- `registry-auth-v1` (shipped) — `RepoRole`/`Principal`/`HasWriteAccess` are reused, not replaced.
- `registry-foundation` (shipped) — the `/v2` router dispatch and SQLite metadata schema are extended.
- Deferred blob GC change — depends on this one, not the reverse.
- No new external dependency.

## Success Criteria

- [ ] With the flag off, `DELETE /v2/<name>/manifests/<ref>` answers the acknowledged-but-refused `UNSUPPORTED` shape, not a bare 405.
- [ ] With the flag on, deleting by digest returns `202` and the manifest, all its tags, and all its `manifest_blobs` rows are gone.
- [ ] Deleting by digest a manifest carrying three tags removes all three, asserted explicitly.
- [ ] Deleting by tag removes only that tag; the manifest and its other tags remain pullable.
- [ ] Deleting an absent digest or tag returns `404` with the correct OCI error code.
- [ ] A `repo-reader` is rejected; a `repo-writer` succeeds; a `repo-admin` succeeds.
- [ ] A token scoped `pull,push` only is rejected on DELETE; a token scoped `pull,push,delete` succeeds.
- [ ] The issued token/challenge for a `repo-writer` includes the `delete` action.
- [ ] No blob file on disk is removed by any delete path, asserted directly.
- [ ] Push, pull, tag listing, and catalog behavior are unchanged with the flag off and with it on.

## Proposal question round — resolved

Decided with the user before this proposal; encoded above so `sdd-spec`/`sdd-design` do not reopen them: (1) delete is
opt-in behind `REGISTRY_DELETE_ENABLED`, default off; (2) `repo-writer` authorizes it, not `repo-admin` — delete is
symmetric with push; (3) both digest-delete (cascading) and tag-only delete ship, since the schema supports both at
near-zero extra cost; (4) hard delete, no tombstone — untouched blob files make re-push the undo path; (5) `delete` is
a real, distinct scope action verb, never a silent reuse of `push`.

### Open questions for `sdd-design`

1. The `202` response body shape for a cascading digest-delete — bodyless per spec, or a detail listing the tags that
   were removed (the risk table argues for detail; the OCI spec neither requires nor forbids it).
2. Whether the flag is read at router construction or per request, and how the disabled path interacts with
   authorization ordering (refuse before or after authenticating the principal).
3. Whether tag-delete needs its own `MetadataStore` method or one `DeleteManifestReference` that branches on
   digest-vs-tag internally.
4. Exact `delete` placement in `intersectRequestedActions` so a future action verb cannot inherit writer derivation by
   default.
5. Whether anonymous-pull-enabled instances need an explicit rejection test for unauthenticated DELETE.
