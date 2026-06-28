# Design: Registry Foundation

## Technical Approach

Build a Go monolith with strict internal boundaries: Distribution HTTP protocol at the edge, application services for workflows, domain types/rules for registry invariants, and filesystem/SQLite adapters underneath. This matches the proposal’s distribution-first direction and the four delta specs by keeping push/pull correctness in the service, keeping the Bubble Tea console read-oriented, and keeping v1 local, single-tenant, and low-resource.

## Architecture Decisions

| Topic | Options | Decision | Rationale |
|------|---------|----------|-----------|
| Runtime shape | Separate services; single process | Single binary with `serve` and `tui` commands | Preserves minimal deployment while keeping the TUI an optional operator client boundary. |
| Metadata model | Files-only tree; SQLite + blobs | Filesystem blobs/uploads + SQLite metadata index | Local files stay authoritative for content, while SQLite keeps catalog/tags/inspection cheap and restart-safe. |
| TUI coupling | Read storage directly; call service APIs | TUI calls application/query interfaces only | Prevents UI-owned domain logic and protects future remote/operator use. |
| Future extensibility | Hardcode v1 assumptions everywhere; isolate seams now | Add small ports for auth, tenant, jobs, remote storage | Cheap now, expensive later if skipped. |

## Data Flow

`Docker/OCI client -> HTTP handlers -> application services -> blob store + metadata store`

`Bubble Tea TUI -> query/action client -> application services -> metadata store`

Push flow: upload blob to `uploads/`, validate digest, promote to content-addressed `blobs/`, validate manifest references, then publish metadata in one application transaction. Pull flow: resolve repo/tag/digest from metadata, stream manifest/blob from local storage, apply auth challenge policy if configured.

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `go.mod` | Create | Initialize the Go module and runtime dependencies. |
| `cmd/registry/main.go` | Create | Single entrypoint with `serve` and `tui` subcommands. |
| `internal/app/registry/service.go` | Create | Push/pull, publish, browse, and inspect workflows. |
| `internal/app/registry/queries.go` | Create | Read models for catalog, tags, manifests, blobs, uploads. |
| `internal/domain/registry/*.go` | Create | Digests, repository refs, manifests, upload state, errors. |
| `internal/protocol/http/*.go` | Create | OCI-compatible handlers, routing, auth challenge middleware. |
| `internal/tui/*.go` | Create | Bubble Tea models and service-backed screens only. |
| `internal/infra/storage/fsblob/*.go` | Create | Blob and upload filesystem adapter. |
| `internal/infra/metadata/sqlite/*.go` | Create | SQLite schema and metadata repository adapter. |
| `internal/ports/*.go` | Create | `BlobStore`, `MetadataStore`, `AccessController`, `TenantResolver`, `JobRunner`. |
| `docs/architecture.md` | Create | Layer boundaries, flows, and future seams. |
| `README.md`, `docs/glossary.md`, `docs/roadmap.md`, `docs/contributing.md` | Create | Product boundary, protocol glossary, roadmap, GitFlow workflow. |

## Interfaces / Contracts

```go
type BlobStore interface { BeginUpload(ctx context.Context) (UploadID, error); CommitBlob(ctx context.Context, id UploadID, expected Digest) (Descriptor, error); OpenBlob(ctx context.Context, dgst Digest) (io.ReadSeekCloser, error) }
type MetadataStore interface { PublishManifest(ctx context.Context, tenant string, repo string, manifest Manifest, blobs []Descriptor) error; ResolveReference(ctx context.Context, tenant string, repo string, ref string) (ManifestRecord, error); Catalog(ctx context.Context, tenant string, page Page) (CatalogPage, error) }
type AccessController interface { Authorize(ctx context.Context, action Action) error; Challenge() Challenge }
type TenantResolver interface { Resolve(ctx context.Context) string }
```

Default v1 adapters are `singleTenantResolver`, `configurableAccessController`, `fsblob.Store`, and `sqlite.Store`.

## Testing Strategy

| Layer | What to Test | Approach |
|-------|-------------|----------|
| Unit | Digest validation, manifest reference checks, access-mode decisions | `go test` for domain/app packages with table tests |
| Integration | HTTP push/pull, upload recovery, SQLite+filesystem consistency | `httptest`, temp dirs, disposable SQLite DB |
| E2E | Docker CLI compatibility and TUI read paths | Start local server, run `docker pull/push`, Bubble Tea model tests/smoke checks |

## Migration / Rollout

No migration required. Implementation should first create bootstrap docs/module files, then server/storage foundations, then TUI read-only views.

## Open Questions

- [ ] Non-blocking: should anonymous pull default to off in dev mode as well, or only in production-facing examples?
