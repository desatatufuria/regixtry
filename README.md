# Registry

Registry is a low-resource, single-binary OCI registry for internal and OSS use. V1 is intentionally narrow: deliver correct local registry behavior first, keep operator visibility simple, and leave platform expansion behind explicit seams instead of mixing it into the first release.

## Quick path

1. Treat the OCI Distribution API as the product boundary.
2. Keep v1 single-tenant, local-storage, and operationally simple.
3. Use `docs/` for reader-facing decisions and `openspec/changes/registry-foundation/` for the active implementation contract.

## Local Docker Compose runtime

Use Docker Compose when you want a disposable local runtime with the registry plus Postgres-backed auth.

### Quick path

1. Start Postgres: `docker compose up -d postgres`
2. Bootstrap the first admin: `docker compose run --rm registry bootstrap-admin -password '<admin-password>'`
3. Start the registry: `docker compose up -d registry`

The registry listens on `127.0.0.1:5000`. Compose keeps SQLite/blob data in the `registry-data` volume and auth state in the `postgres-data` volume.

### Notes

| Topic | Decision |
| --- | --- |
| Image build | Top-level `Dockerfile` builds `cmd/registry` into a single runtime image. |
| Auth wiring | `docker-compose.yml` sets both `REGISTRY_AUTH_POSTGRES_DSN` and `REGISTRY_AUTH_TOKEN_REALM_URL` so the registry shares the same local Postgres service and advertises Docker-compatible bearer challenges that point at `/auth/token`. |
| First startup | Auth-enabled `serve` fails fast until a global admin exists, so bootstrap the admin before bringing up `registry`. |

## V1 outcome

V1 delivers a correct local registry with clear operator visibility.

| Area | Included in v1 |
| --- | --- |
| Registry protocol | OCI/Docker-compatible push and pull for manifests and blobs |
| Discovery | Repository listing, tag browsing, manifest inspection, and blob inspection |
| Storage model | Local filesystem blob storage with SQLite-backed metadata |
| Upload lifecycle | Staged uploads, digest validation, and publish-only-on-valid-content rules |
| Operator experience | Keyboard-first Bubble Tea console for inspection and maintenance basics |
| Future readiness | Auth, tenant, storage, and background-job seams kept explicit |

## Non-goals

These are intentionally OUT of v1:

- Multi-tenant isolation and complex RBAC.
- Replication, remote object storage, or cross-instance synchronization.
- Signing orchestration, scanning, provenance pipelines, or broad admin APIs.
- Deletion/retention platforms, operator-driven garbage collection controls, or Harbor-like feature expansion.
- Treating the Docker Engine API as the registry contract.
- Letting the TUI own registry rules or read storage directly.

## API boundary

The product contract is the OCI Distribution / Docker Registry HTTP API surface for registry content exchange.

| In boundary | Out of boundary |
| --- | --- |
| Manifest push/pull | Docker daemon lifecycle management |
| Blob upload/download | Container runtime control |
| Catalog and tag listing | Host orchestration features |
| Auth challenge-ready registry access behavior | Docker Engine APIs that do not act as registry clients |

If a capability depends on Docker daemon control instead of registry protocol behavior, it is not part of this product unless introduced later as a separate capability.

## Architecture guardrails

- Single Go binary with `serve` and `tui` entry modes.
- Registry semantics live in application/domain layers, not in HTTP handlers or the TUI.
- Filesystem blobs remain authoritative for content bytes.
- SQLite exists to index repositories, tags, manifests, and upload state cheaply.
- Anonymous pull MAY be enabled by configuration, but auth remains a seam rather than a full v1 subsystem.

## Workflow baseline

This repository follows GitFlow plus a feature-branch-chain review strategy for oversized changes.

1. `main` holds the release/bootstrap baseline.
2. `develop` integrates ongoing product work.
3. `feature/registry-foundation` is the tracker branch for the active change.
4. PR 1 targets `feature/registry-foundation`.
5. Later PR slices target the immediate previous PR branch until the tracker branch is ready for `develop`.

Read `docs/contributing.md` before opening or retargeting any PR slice.

## Documentation map

| Path | Purpose |
| --- | --- |
| `docs/architecture.md` | Layer boundaries, runtime flows, ports, and explicit seams |
| `docs/contributing.md` | GitFlow, feature-branch-chain workflow, and documentation duties |
| `docs/glossary.md` | Shared protocol, storage, and architecture vocabulary |
| `docs/roadmap.md` | V1 workstreams, non-goals, and post-v1 sequencing |
| `openspec/changes/registry-foundation/` | Approved proposal, specs, design, and task tracking for the active change |

## Current status

`registry-foundation` is implemented in the repository and has been verified at two levels:

- The Go test suite passes for the current codebase.
- Local smoke verification has confirmed Docker push/pull plus TUI snapshot rendering for the seeded `registry-foundation/smoke` repository.

This does **not** mean the product is feature-complete beyond the documented v1 scope. The repository currently proves the local single-node foundation: OCI/Docker-compatible content flows, SQLite-backed metadata, filesystem blob storage, and a read-oriented operator console.
