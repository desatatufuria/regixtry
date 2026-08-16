# Registry and Docker/OCI

## `docker login` flow

With auth enabled, Docker requests `/v2/`, receives a Bearer challenge, calls `/auth/token` with Basic credentials, and retries `/v2/` with the issued access token. The token is short-lived and kept in the client process only; Regixtry stores just its hash.

## Push

```bash
docker login registry.example.com
docker tag my-image:latest registry.example.com/team/my-image:latest
docker push registry.example.com/team/my-image:latest
```

The handler supports monolithic and chunked uploads through `POST`, `PATCH`, `PUT`. The only supported digest algorithm is SHA-256. A manifest is rejected if it references a blob that does not exist.

## Pull

```bash
docker pull registry.example.com/team/my-image:latest
```

Resolution accepts either a tag or a digest. Blobs and manifests respond to `GET` and `HEAD`.

## Routes

| Method | Endpoint | Purpose |
| --- | --- | --- |
| GET/HEAD | `/v2/` | Registry check; requires auth when auth is enabled |
| GET | `/v2/_catalog` | List repositories |
| GET | `/v2/<repo>/tags/list` | List tags for a repository |
| POST | `/v2/<repo>/blobs/uploads/` | Start a blob upload |
| GET/HEAD | `/v2/<repo>/blobs/uploads/<id>` | Read upload state |
| PATCH | `/v2/<repo>/blobs/uploads/<id>` | Append a chunk |
| PUT | `/v2/<repo>/blobs/uploads/<id>?digest=sha256:...` | Complete the upload |
| DELETE | `/v2/<repo>/blobs/uploads/<id>` | Not implemented; returns `UNSUPPORTED` |
| GET/HEAD | `/v2/<repo>/blobs/<digest>` | Read a blob |
| PUT | `/v2/<repo>/manifests/<tag-or-digest>` | Publish a manifest |
| GET/HEAD | `/v2/<repo>/manifests/<tag-or-digest>` | Read a manifest |
| DELETE | `/v2/<repo>/manifests/<digest>` | Delete a manifest, cascading to every tag pointing at it; opt-in via `REGISTRY_DELETE_ENABLED`, otherwise `UNSUPPORTED` |
| DELETE | `/v2/<repo>/manifests/<tag>` | Untag only, leaving the manifest and its other tags intact; opt-in via `REGISTRY_DELETE_ENABLED`, otherwise `UNSUPPORTED` |
| GET | `/v2/<repo>/manifests/<tag-or-digest>/scan-status` | CI-facing Trivy scan verdict for the resolved digest |
| GET | `/v2/<repo>/manifests/<tag-or-digest>/signature-status` | CI-facing cosign signature verdict for the resolved digest |

The two verdict routes use the same pull-credential authorization as a manifest read (no admin session required), and they always answer `200` with the current verdict — they report a gate outcome, they are never themselves subject to one. `scan-status` returns one of `unscanned`, `in_progress`, `failed`, `clean`, `blocked`; `signature-status` returns one of `unsigned`, `unverifiable`, `untrusted`, `mismatched`, `verified`. Both include whether a pull would currently be blocked by policy.

## Blob garbage collection

Manifest and tag deletes (`REGISTRY_DELETE_ENABLED`) remove metadata rows only; they never unlink the underlying blob files under `blobsRoot`. Reclaiming that storage is a separate, opt-in, two-step admin flow:

| Route | Method | Purpose |
| --- | --- | --- |
| `/admin/v1/gc/reports` | POST | Compute and persist a report of unreferenced, grace-expired blob candidates; always reachable regardless of `REGISTRY_GC_DELETE_ENABLED` |
| `/admin/v1/gc/reports/{id}` | GET | Fetch a previously computed report by id, with its full candidate list |
| `/admin/v1/gc/reports/{id}/delete` | POST | Unlink the report's candidates that are still unreferenced and grace-expired at delete time; gated by `REGISTRY_GC_DELETE_ENABLED` (default `false`, returns `UNSUPPORTED`/`501` while disabled) |

**Grace window**: a blob is never a candidate if its file was written within the last 24 hours, regardless of whether anything references it yet. This protects a blob committed by an in-flight push before its manifest has been published — the fixed, non-configurable interval between `CommitUpload` and `PublishManifest`.

**Expiry is hygiene, not safety**: a computed report expires 24 hours after it was computed and is pruned on the next report request. This bounds how long a stale report can be acted on, but it is not what keeps deletion safe — a delete request re-runs the same mark-sweep-grace computation at delete time and only unlinks digests present in *both* the original report and that fresh recomputation. A digest referenced by a manifest published after the report was computed is excluded from the delete, even if the report is still valid.

**Audit trail**: every report row records `computed_at`, `expires_at`, and `requested_by`. On delete, the same row transitions once to a terminal `deleted` state recording `deleted_at`, `deleted_by`, `deleted_count`, `bytes_reclaimed`, and a per-candidate outcome (`deleted`, `retained`, `missing`, or `failed`). Report rows are never deleted by the pruning hygiene above once they reach `deleted` state — they are the only record of what was permanently reclaimed.

**No recovery**: deletion of a blob file is irreversible. There is no undo, no trash, and no backup taken by this feature — a reclaimed blob can only be restored by re-pushing the content that produced it (or from an external `blobsRoot` backup, if one exists). Garbage collection is manual-trigger only in v1; nothing runs on a schedule or in the background.

## Compatibility and verified limits

| Area | Status |
| --- | --- |
| Docker Registry-style `/v2/` | Implemented, per the routes above |
| JSON manifests | Implemented via `manifestEnvelope` |
| SHA-256 digest | Implemented |
| Tags and catalog | Implemented |
| Upload chunking | Implemented with `PATCH` |
| Upload cancellation | Not implemented; returns `UNSUPPORTED` |
| Manifest/tag deletes | Implemented, opt-in via `REGISTRY_DELETE_ENABLED` (default `false`); metadata-only, never touches blob files |
| Blob deletes | Not exposed directly; blob removal happens only through garbage collection |
| Garbage collection | Implemented: opt-in, report-then-delete flow gated by `REGISTRY_GC_DELETE_ENABLED` (default `false`); manual admin trigger only, no scheduler |
| Replication/remote storage | No adapter found |
| Multi-tenant | No; single-tenant resolver (`ports.NewSingleTenantResolver`) |
| Multi-arch/indexes | Could not be confirmed from current code |
| Full OCI conformance | Could not be confirmed from current code |

Repository names are validated by `internal/domain/regixtry/repository.go`. There is no separate administrative namespace: `team/my-image` is simply a repository name.
