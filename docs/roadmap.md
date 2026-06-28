# Roadmap

This roadmap keeps v1 narrow on purpose: a correct local registry first, platform expansion later.

## V1 workstreams

| Group | Outcome |
| --- | --- |
| Repository foundation | Stable module, docs baseline, GitFlow workflow, and review slices |
| Registry protocol | OCI-compatible push, pull, catalog, tags, manifests, and blobs |
| Local durability | Filesystem blob storage, SQLite metadata, and safe upload lifecycle |
| Operator console | Thin Bubble Tea client for inspection and maintenance basics |

## V1 acceptance boundary

- Single tenant only.
- Local storage only.
- Optional anonymous pull by configuration.
- Read-oriented operator console with clearly limited maintenance actions.

## Post-v1 candidates

- Multi-tenant isolation and stronger authorization models.
- Replication, retention, garbage collection controls, and remote object storage.
- Signing, scanning, provenance workflows, and richer admin APIs.
- Expanded operator automations and asynchronous job orchestration.

## Sequencing rule

Do not promote post-v1 items into active work unless the v1 boundary remains explicit in `README.md`, `docs/glossary.md`, and the relevant OpenSpec change artifacts.
