# Roadmap

This roadmap keeps v1 narrow on purpose: a correct local registry first, platform expansion later.

## Roadmap reading rule

Read this document as a sequencing contract, not a wish list.

- V1 items are the approved delivery path.
- Post-v1 items are intentionally excluded from the current implementation scope.
- If scope changes, update this roadmap and the active OpenSpec artifacts together.

## V1 workstreams

| Group | Outcome | Current state |
| --- | --- | --- |
| Repository foundation | Stable module, docs baseline, GitFlow workflow, and review slices | Implemented and verified |
| Registry protocol | OCI-compatible push, pull, catalog, tags, manifests, and blobs | Implemented on the local single-node runtime |
| Local durability | Filesystem blob storage, SQLite metadata, and safe upload lifecycle | Implemented |
| Registry auth v1 | Postgres-backed auth state, `/auth/token`, bearer challenge interoperability, and repository enforcement | Active workstream; foundation + registry enforcement are implemented, admin/TUI and remaining verification/docs slices are still pending |
| Operator console | Thin Bubble Tea client for inspection and maintenance basics | Inspection flow exists; auth administration is not finished |

## Approved v1 boundary

V1 is complete when ALL of the following are true:

- Single tenant is the only supported runtime model.
- Local runtime storage remains the only supported deployment mode.
- Registry metadata stays in SQLite while auth state lives beside it in Postgres.
- Anonymous pull is configuration-driven rather than hard-coded policy.
- Auth-enabled registry access uses Docker-compatible Bearer challenges plus `/auth/token` token exchange.
- Incomplete uploads never appear as published registry content.
- The operator console exposes visibility first and keeps unsupported mutations explicit.
- Reader-facing docs continue to describe the same scope as the OpenSpec proposal, design, and specs.

## Current implementation checkpoint

- `registry-foundation` is complete and verified.
- `registry-auth-v1` Work Unit 1 (auth foundation) and Work Unit 2 (registry enforcement) are implemented on the active feature branch.
- Local compose runtime has proven authenticated Docker push with Postgres-backed auth enabled.
- Remaining planned work is still real scope: TUI admin workflows, auth-oriented smoke-script expansion, and final reader-facing doc updates tied to those slices.

## Explicit non-goals for the active change

These items must stay out of the active auth-v1 review slices unless the approved scope changes first:

- Multi-tenant isolation and advanced RBAC.
- Replication or remote-object-store adapters.
- Deletion/retention platforms and operator-triggered garbage collection controls.
- Signing, scanning, provenance, and supply-chain automation.
- Broad admin APIs beyond registry protocol and the thin operator console.

## Delivery sequence for `registry-foundation`

| Unit | Target outcome | Review boundary |
| --- | --- | --- |
| PR 0 | Repository/bootstrap baseline | Completed; established `main`, `develop`, and `feature/registry-foundation` |
| PR 1 | Docs and architecture foundation | Keep README/roadmap/glossary/contributing/architecture aligned |
| PR 2 | Storage and protocol core | Domain, ports, filesystem, SQLite, HTTP, and tests |
| PR 3 | Operator console and verification | TUI views plus end-to-end verification artifacts |

## Delivery sequence for `registry-auth-v1`

| Work unit | Target outcome | Status |
| --- | --- | --- |
| PR 1 | Postgres auth domain/store/bootstrap foundation | Completed |
| PR 2 | `/auth/token`, bearer middleware, repository enforcement, and focused tests | Completed |
| PR 3 | TUI admin flows plus auth-aware smoke coverage and remaining doc alignment | In progress / pending |

## Documentation maintenance rule

When roadmap-relevant scope changes:

1. Update this file.
2. Update `README.md` and `docs/glossary.md` if the reader-facing boundary changed.
3. Update the relevant OpenSpec change artifacts (`registry-foundation`, `registry-auth-v1`, or both) if the implementation contract changed.
4. Keep v1 and post-v1 work separated in the same edit.

## Post-v1 candidates

| Area | Deferred direction |
| --- | --- |
| Tenancy and access | Multi-tenant isolation and stronger authorization models |
| Storage and operations | Replication, retention, garbage collection controls, and remote object storage |
| Supply chain | Signing, scanning, provenance workflows |
| Platform control plane | Richer admin APIs, automation, and asynchronous job orchestration |

## Sequencing rule

Do not promote post-v1 items into active work unless the v1 boundary remains explicit in `README.md`, `docs/glossary.md`, and the relevant OpenSpec change artifacts.
