# Architecture

The v1 system is a single Go binary with two entry modes: `serve` for the registry API and `tui` for the operator console. The runtime stays monolithic, but the codebase keeps strict boundaries so auth, storage, and future platform work can evolve without rewriting core registry logic.

## Approved architectural scope

This document describes the approved v1 boundary only.

- Single tenant is the default and only supported tenancy model.
- Local filesystem blobs plus SQLite metadata are the only planned v1 storage shape.
- The registry protocol is the public contract.
- The Bubble Tea console is an operator client, not a second backend.
- Auth, jobs, and alternate storage are design seams, not fully delivered subsystems in this phase.

## Layer map

| Layer | Responsibility | Must not do |
| --- | --- | --- |
| Protocol edge | Expose OCI-compatible HTTP handlers and auth challenge responses | Own registry business rules or storage details |
| Application services | Coordinate push, pull, publish, browse, inspect, and maintenance workflows | Depend on HTTP or Bubble Tea types |
| Domain | Define digests, references, manifests, upload state, and invariants | Read files, execute SQL, or render UI |
| Infrastructure adapters | Implement filesystem blob storage, SQLite metadata, config-backed auth, and tenant defaults | Reinterpret domain policy in adapter-specific ways |
| Operator console | Present service-backed views for repositories, tags, manifests, blobs, and uploads | Read storage directly or compute registry truth locally |

## Runtime flows

### Registry flow

`OCI/Docker client -> HTTP handlers -> application services -> blob store + metadata store`

- Blob uploads start in staging storage.
- Digest validation happens before promotion into durable blob storage.
- Manifest publication succeeds only after referenced blobs are durably available.
- Metadata publishing makes content discoverable for catalog, tags, and pull resolution.
- Access policy is applied through an auth seam so anonymous pull and future challenge behavior stay configurable.

### Operator flow

`Bubble Tea TUI -> query/action client -> application services -> metadata store`

- The console is a thin client over application/query interfaces.
- Inspection uses service-provided read models.
- Unsupported v1 mutations must be shown as unavailable rather than implemented in the UI.

## External boundaries

| Boundary | Decision |
| --- | --- |
| Public protocol | OCI Distribution / Docker Registry HTTP API |
| Out-of-scope control plane | Docker Engine APIs and general host/runtime management |
| Console data source | Application/query services, never direct storage reads |
| Persistence contract | Filesystem for blobs, SQLite for metadata |

## Core ports

| Port | Purpose | V1 default |
| --- | --- | --- |
| `BlobStore` | Manage upload staging, blob promotion, and blob reads | Filesystem-backed adapter |
| `MetadataStore` | Publish and resolve manifests, tags, catalog, and upload state | SQLite-backed adapter |
| `AccessController` | Authorize actions and produce challenge behavior | Configurable access seam |
| `TenantResolver` | Resolve active tenant context | Single-tenant resolver |
| `JobRunner` | Future async maintenance and background work boundary | Deferred seam for post-v1 |

## Seams kept intentionally small

### Storage seam

Filesystem blobs are authoritative for content bytes, while SQLite provides restart-safe metadata and cheap inspection queries. This keeps v1 local and low-resource without coupling application logic to file paths or SQL details.

### Auth seam

V1 does not require full RBAC, but handlers and services must depend on an access-control boundary so anonymous pull and future challenge flows do not leak protocol concerns into domain logic.

### Tenant seam

V1 resolves a single tenant by default, but the boundary stays explicit so future tenancy changes do not force protocol or storage rewrites.

### TUI boundary

The Bubble Tea console is an operator client, not a second backend. Any maintenance action exposed in v1 must go through application services so the UI remains replaceable and does not become the source of registry rules.

## Non-goals that shape the design

- No multi-tenant policy model in v1.
- No remote object storage adapter in v1.
- No broad asynchronous control plane or background-job feature set in v1.
- No deletion/retention platform semantics until later scope explicitly approves them.

## Repository direction
git diff --cached --stat
The planned code layout is:

- `cmd/registry/` for binary entrypoints.
- `internal/app/registry/` for workflows and queries.
- `internal/domain/registry/` for registry types and invariants.
- `internal/ports/` for seams between core logic and adapters.
- `internal/infra/storage/fsblob/` and `internal/infra/metadata/sqlite/` for v1 adapters.
- `internal/protocol/http/` for OCI-compatible delivery.
- `internal/tui/` for the thin operator console.

## Documentation alignment rule

If the architecture boundary changes, update `README.md`, `docs/glossary.md`, and the active OpenSpec design/spec artifacts in the same work unit so contributors are never forced to infer the real scope from code alone.
