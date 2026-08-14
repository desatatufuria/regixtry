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
| Blob deletes | Not exposed; blob removal stays garbage-collection-only |
| Garbage collection | No implementation found |
| Replication/remote storage | No adapter found |
| Multi-tenant | No; single-tenant resolver (`ports.NewSingleTenantResolver`) |
| Multi-arch/indexes | Could not be confirmed from current code |
| Full OCI conformance | Could not be confirmed from current code |

Repository names are validated by `internal/domain/regixtry/repository.go`. There is no separate administrative namespace: `team/my-image` is simply a repository name.
