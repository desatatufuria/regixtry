# Registry

Registry is a low-resource, single-binary OCI registry for internal and OSS use.

## Quick path

1. Treat the OCI Distribution API as the product boundary.
2. Keep v1 single-tenant, local-storage, and operationally simple.
3. Use the docs in `docs/` and the active change in `openspec/` before implementation work.

## V1 scope

- OCI/Docker-compatible push and pull for manifests and blobs.
- Repository and tag browsing plus manifest/blob inspection.
- Local filesystem blob storage with SQLite-backed metadata.
- Keyboard-first TUI for operator visibility and maintenance basics.
- Architecture seams for auth, tenancy, jobs, and alternate storage.

## Non-goals

- Multi-tenant isolation and broad RBAC.
- Replication, signing orchestration, scanning, and remote object storage.
- Treating the Docker Engine API as the registry contract.
- TUI-owned registry rules or direct storage reads from the console.

## API boundary

The product contract is the OCI Distribution / Docker Registry HTTP API surface for registry content exchange. Docker Engine behavior is out of scope unless it uses that registry protocol as a client.

## Repository guide

| Path | Purpose |
| --- | --- |
| `docs/architecture.md` | Layer boundaries, ports, and runtime seams |
| `docs/contributing.md` | GitFlow workflow, branch order, and documentation habits |
| `docs/glossary.md` | Shared protocol and architecture vocabulary |
| `docs/roadmap.md` | V1 and post-v1 capability sequencing |
| `openspec/` | Active change planning, specs, design, and tasks |

## Current status

This repository is in the foundation stage. The bootstrap commit establishes the module, contributor workflow, and architectural boundaries before protocol and storage implementation starts.
