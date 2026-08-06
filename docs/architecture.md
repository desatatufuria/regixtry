# Architecture

The v1 system is a single Go binary with two entry modes: `serve` for the registry API and `tui` for the operator console. The runtime stays monolithic, but the codebase keeps strict boundaries so auth, storage, and future platform work can evolve without rewriting core registry logic.

## Approved architectural scope

This document describes the approved v1 boundary only.

- Single tenant is the default and only supported tenancy model.
- Local filesystem blobs remain authoritative for content bytes, SQLite keeps registry metadata, and Postgres now holds auth state.
- The registry protocol is the public contract.
- The Bubble Tea console is an operator client, not a second backend.
- Auth is now delivered for registry enforcement, bootstrap flows, the narrow `/admin/v1` operator API, and a read-only admin TUI path, while jobs, alternate storage, pagination, and admin mutations remain incomplete seams or later work.

## Layer map

| Layer | Responsibility | Must not do |
| --- | --- | --- |
| Protocol edge | Expose OCI-compatible HTTP handlers and auth challenge responses | Own registry business rules or storage details |
| Application services | Coordinate push, pull, publish, browse, inspect, and maintenance workflows | Depend on HTTP or Bubble Tea types |
| Domain | Define digests, references, manifests, upload state, and invariants | Read files, execute SQL, or render UI |
| Infrastructure adapters | Implement filesystem blob storage, SQLite metadata, Postgres-backed auth state, and tenant defaults | Reinterpret domain policy in adapter-specific ways |
| Operator console | Present service-backed views for repositories, tags, manifests, blobs, and uploads | Read storage directly or compute registry truth locally |

## Runtime flows

### Registry flow

`OCI/Docker client -> HTTP handlers -> auth middleware/challenge -> application services -> blob store + metadata store`

- Blob uploads start in staging storage.
- Digest validation happens before promotion into durable blob storage.
- Manifest publication succeeds only after referenced blobs are durably available.
- Metadata publishing makes content discoverable for catalog, tags, and pull resolution.
- When auth is enabled, protected requests challenge through Docker-compatible Bearer semantics and application services receive the resolved principal through context.

### Auth flow

`OCI/Docker client -> /auth/token -> Basic credentials or admin-preissued token -> auth service -> Postgres auth state -> short-lived bearer token -> retry /v2/*`

- `/auth/token` is the token exchange endpoint advertised by the Bearer challenge.
- Postgres stores users, password hashes, preissued credential tokens, bearer revocation state, and repository grants.
- SQLite remains the source of registry metadata; repository authorization links the two stores by validated repository names rather than shared foreign keys.
- The current implementation covers registry auth, bootstrap flows, the authenticated `/admin/v1` operator API, and the TUI's login plus read-only admin browsing path; admin mutations and richer client ergonomics remain pending work.

### Operator admin flow

`Operator client -> /auth/token -> Bearer access token -> /admin/v1/* -> admin handler -> auth service -> Postgres auth state`

- The operator API is a thin transport over existing admin service methods.
- `/admin/v1` accepts only Bearer access tokens and requires `Principal.IsAdmin`.
- Admin handlers use explicit `401/403/404/409/422` semantics so operator feedback stays precise without changing `/v2/*` registry behavior.
- Delete-user, broad profile edits, and pagination remain deferred for a later slice.

### Operator flow

`Bubble Tea TUI -> inspection query service + admin HTTP client -> application services or /auth/token + /admin/v1/* -> metadata store + Postgres auth state`

- The console remains a thin client and now uses two explicit seams: local inspection queries plus an authenticated admin HTTP client.
- Inspection uses service-provided read models, while admin reads go through `/auth/token` and Bearer-protected `/admin/v1/*` endpoints.
- Admin session material stays in process memory only and must be cleared on logout, exit, or expiry.
- Unsupported v1 mutations must be shown as unavailable rather than implemented in the UI.

## External boundaries

| Boundary | Decision |
| --- | --- |
| Public protocol | OCI Distribution / Docker Registry HTTP API |
| Out-of-scope control plane | Docker Engine APIs and general host/runtime management |
| Console data source | Application/query services, never direct storage reads |
| Persistence contract | Filesystem for blobs, SQLite for registry metadata, Postgres for auth state |

## Core ports

| Port | Purpose | V1 default |
| --- | --- | --- |
| `BlobStore` | Manage upload staging, blob promotion, and blob reads | Filesystem-backed adapter |
| `MetadataStore` | Publish and resolve manifests, tags, catalog, and upload state | SQLite-backed adapter |
| `AccessController` | Authorize actions and produce challenge behavior | Principal-aware access seam with Bearer challenge support |
| `AuthService` | Issue/verify auth tokens, bootstrap the first admin, and enforce operator admin safety rules | Postgres-backed service over auth tables |
| `TenantResolver` | Resolve active tenant context | Single-tenant resolver |
| `JobRunner` | Future async maintenance and background work boundary | Deferred seam for post-v1 |

## Seams kept intentionally small

### Storage seam

Filesystem blobs are authoritative for content bytes, while SQLite provides restart-safe metadata and cheap inspection queries. This keeps v1 local and low-resource without coupling application logic to file paths or SQL details.

### Auth seam

V1 still avoids a broad platform-RBAC scope, but the registry now has a concrete auth subsystem: Postgres-backed users, repository grants, admin credential tokens, `/auth/token`, short-lived bearer access tokens, scoped `WWW-Authenticate` challenges, and a narrow `/admin/v1` operator API. The seam stays explicit so later pagination, richer operator clients, and future auth backends do not leak protocol concerns into domain logic.

### Tenant seam

V1 resolves a single tenant by default, but the boundary stays explicit so future tenancy changes do not force protocol or storage rewrites.

### TUI boundary

The Bubble Tea console is an operator client, not a second backend. Any maintenance action exposed in v1 must go through application services or authenticated HTTP APIs so the UI remains replaceable and does not become the source of registry rules. The shipped console now supports operator login plus read-only user/grant/admin-token browsing over `/auth/token` and `/admin/v1/*`, but it still defers admin mutations, silent refresh, and persisted credentials.

## Non-goals that shape the design

- No multi-tenant policy model in v1.
- No remote object storage adapter in v1.
- No broad asynchronous control plane or background-job feature set in v1.
- No deletion/retention platform semantics until later scope explicitly approves them.

## Repository direction
The planned code layout is:

- `cmd/registry/` for binary entrypoints.
- `internal/app/auth/` for auth workflows and token issuance/verification.
- `internal/app/registry/` for workflows and queries.
- `internal/domain/auth/` for users, grants, principals, tokens, and auth invariants.
- `internal/domain/registry/` for registry types and invariants.
- `internal/ports/` for seams between core logic and adapters.
- `internal/infra/auth/postgres/` for Postgres-backed auth persistence and schema bootstrap.
- `internal/infra/storage/fsblob/` and `internal/infra/metadata/sqlite/` for v1 adapters.
- `internal/protocol/http/` for OCI-compatible delivery.
- `internal/tui/` for the thin operator console.

## Documentation alignment rule

If the architecture boundary changes, update `README.md`, `docs/glossary.md`, and the active OpenSpec design/spec artifacts in the same work unit so contributors are never forced to infer the real scope from code alone.
