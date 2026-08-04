# Registry

Registry is a low-resource, single-binary OCI registry for internal and OSS use. V1 is intentionally narrow: deliver correct local registry behavior first, keep operator visibility simple, and leave platform expansion behind explicit seams instead of mixing it into the first release.

## Quick path

1. Treat the OCI Distribution API as the product boundary.
2. Keep v1 single-tenant, local-storage, and operationally simple.
3. Use `docs/` for reader-facing decisions and `openspec/changes/registry-foundation/` plus `openspec/changes/registry-auth-v1/` for the current implementation contracts.

## Local Docker Compose helper runtime

Use Docker Compose when you want a disposable local helper runtime for manual testing with the registry plus Postgres-backed auth.

This top-level Compose setup is a convenience for local bring-up and smoke-style manual checks. It is **not** the primary product verification contract. The canonical verification baseline for this repository remains the Go test/build evidence plus any explicitly captured runtime/manual evidence called out in the verification artifacts.

### Quick path

1. Start Postgres: `docker compose up -d postgres`
2. Bootstrap the first admin: `docker compose run --rm registry bootstrap-admin -password '<admin-password>'`
3. Start the registry: `docker compose up -d registry`
4. Log in from Docker: `docker login localhost:${REGISTRY_PORT:-5517} -u admin -p '<admin-password>'`

Compose publishes the registry on `127.0.0.1:${REGISTRY_PORT:-5517}`. It keeps SQLite/blob data in the `registry-data` volume and auth state in the `postgres-data` volume.

Before the first compose run on a fresh machine, make sure the external Docker network expected by the devcontainer/runtime exists:

```bash
docker network create dtf-netwok
```

### Bootstrap and runtime steps

Use this order whenever auth is enabled:

1. `docker compose up -d postgres`
2. `docker compose run --rm registry bootstrap-admin -username admin -password '<admin-password>'`
3. `docker compose up -d registry`
4. `docker login localhost:${REGISTRY_PORT:-5517} -u admin -p '<admin-password>'`
5. Push or pull images against `localhost:${REGISTRY_PORT:-5517}`.

If you need to rotate the bootstrap password later, rerun the bootstrap command with `-rotate-password`.

### TUI auth administration

The local TUI no longer fabricates an authenticated admin when `-auth-postgres-dsn` is configured. Repository inspection still works, but auth-backed admin mutations stay disabled until a real operator login flow exists.

For now, use `bootstrap-admin` only to create or rotate the initial global admin account, then perform auth administration through authenticated backend flows instead of the local TUI.

Example snapshot run against the compose Postgres service:

```bash
go run ./cmd/registry tui \
  -storage-root ./data \
  -auth-postgres-dsn "postgres://registry:registry@127.0.0.1:5432/registry_auth?sslmode=disable" \
  -snapshot
```

The snapshot will render a notice explaining that local TUI admin actions are intentionally disabled in auth-backed mode.

### Notes

| Topic | Decision |
| --- | --- |
| Image build | Top-level `Dockerfile` builds `cmd/registry` into a single runtime image. |
| Auth wiring | `docker-compose.yml` sets both `REGISTRY_AUTH_POSTGRES_DSN` and `REGISTRY_AUTH_TOKEN_REALM_URL` so the registry shares the same local Postgres service and advertises Docker-compatible bearer challenges that point at `/auth/token`. Keep the advertised realm URL aligned with the host/port clients actually use. |
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
| Access model | Postgres-backed auth state, `/auth/token`, and repository-scoped enforcement for auth-enabled runtime |
| Future readiness | Tenant, storage, and background-job seams kept explicit while broader auth workflows remain intentionally narrow |

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
- Registry metadata stays in SQLite while auth state lives in Postgres when auth is enabled.
- Anonymous pull MAY be enabled by configuration; when disabled, the registry advertises Docker-compatible Bearer challenges that lead clients to `/auth/token`.

## Workflow baseline

This repository follows GitFlow plus a feature-branch-chain review strategy for oversized changes.

1. `main` holds the release/bootstrap baseline.
2. `develop` integrates ongoing product work.
3. `feature/registry-foundation` is the base tracker branch, and `registry-auth-v1` currently advances on feature-branch review slices above that foundation.
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
| `openspec/changes/registry-foundation/` | Baseline proposal, specs, design, and task tracking for the implemented foundation |
| `openspec/changes/registry-auth-v1/` | Active auth-v1 proposal, specs, design, tasks, and apply progress |

## Current status

`registry-foundation` is implemented in the repository, and `registry-auth-v1` has partial implementation on top of it.

- The Go test suite passes for the current codebase.
- Local smoke verification has confirmed Docker push/pull plus TUI snapshot rendering for the seeded `registry-foundation/smoke` repository.
- Manual checks against the local Compose helper runtime have also produced supporting evidence for authenticated Docker push against the auth-enabled runtime.

Those local Compose checks are supporting runtime evidence only. They do **not** mean the repository currently guarantees Compose automation as a first-class externally verified runtime contract.

This does **not** mean the product is feature-complete beyond the documented v1 scope. The repository currently proves the local single-node foundation plus the auth-v1 registry path: OCI/Docker-compatible content flows, SQLite-backed metadata, Postgres-backed auth state, `/auth/token`, bearer challenge interoperability, and an inspection-oriented TUI that shows a security notice instead of allowing local auth-backed admin mutations.
