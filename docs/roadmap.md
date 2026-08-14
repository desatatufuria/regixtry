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
| Regixtry protocol | OCI-compatible push, pull, catalog, tags, manifests, and blobs | Implemented on the local single-node runtime |
| Local durability | Filesystem blob storage, SQLite metadata, and safe upload lifecycle | Implemented |
| Regixtry auth v1 | Postgres-backed auth state, `/auth/token`, bearer challenge interoperability, and repository enforcement | Implemented and verified |
| Operator admin API | Narrow `/admin/v1` user/grant/admin-token administration over the shared auth service | Implemented and repo-verified; pagination, delete-user, and richer clients stay deferred |
| Operator console | Thin Bubble Tea client for inspection plus authenticated, read-only admin browsing | Implemented for login, users, grants, and admin tokens; admin mutations and richer client ergonomics stay deferred |
| Repository access control completion | Registry-wide read-only role, delegated repo-admin grant management scoped to one repository, and bounded-TTL, revocable robot accounts | Implemented across the auth domain/service, `/admin/v1`, and the operator console |

## Approved v1 boundary

V1 is complete when ALL of the following are true:

- Single tenant is the only supported runtime model.
- Local runtime storage remains the only supported deployment mode.
- Regixtry metadata stays in SQLite while auth state lives beside it in Postgres.
- Anonymous pull is configuration-driven rather than hard-coded policy.
- Auth-enabled registry access uses Docker-compatible Bearer challenges plus `/auth/token` token exchange.
- Incomplete uploads never appear as published registry content.
- The operator console exposes visibility first and keeps unsupported mutations explicit.
- Reader-facing docs continue to describe the same scope as the OpenSpec proposal, design, and specs.

## Current implementation checkpoint

- `registry-foundation` is complete and verified.
- `registry-auth-v1` is complete and verified for the shipped auth-enabled registry flow.
- `registry-operator-admin-api` is complete and verified for authenticated `/admin/v1` user, grant, and admin-token administration.
- `registry-operator-admin-tui` is now complete for authenticated login plus GET-only admin browsing over the shipped backend API.
- `registry-acl-v1` is complete: a registry-wide read-only role (`is_read_only`, grant-independent pull access), delegated repo-admin grant management scoped to exactly one repository (`/admin/v1/repositories/{repo}/grants`, reached from the console's Console Repositories screen), and bounded-TTL, revocable robot accounts (`/admin/v1/robots`, the `screenAdminRobots`/`screenAdminCreateRobot` TUI screens) that are permanently excluded from password login and from the default human user listing.
- Manual checks against the local Compose helper runtime have demonstrated authenticated Docker push with Postgres-backed auth enabled, but that helper runtime is still supporting evidence rather than the primary automated verification contract.
- Remaining planned work is still real scope: optional admin-API pagination, and any future auth-oriented smoke expansion for richer clients.

## Explicit non-goals for the active change

These items must stay out of the active auth-v1 review slices unless the approved scope changes first:

- Multi-tenant isolation and advanced RBAC.
- Replication or remote-object-store adapters.
- Deletion/retention platforms and operator-triggered garbage collection controls.
- Signing, scanning, provenance, and supply-chain automation.
- Platform-style admin APIs beyond the narrow `/admin/v1` surface and thin operator console.

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
| PR 3 | Auth verification close-out and doc alignment | Completed |

## Delivery sequence for `registry-operator-admin-api`

| Work unit | Target outcome | Status |
| --- | --- | --- |
| PR 1 | Narrow admin service/store contract and safety coverage | Completed |
| PR 2 | `/admin/v1/users` list/create/enable/disable/reset-password routes | Completed |
| PR 3 | `/admin/v1` grants and admin-token routes with nested revoke coverage | Completed |
| PR 4 | Reader-facing docs plus repo-wide verification refresh | Completed |

## Delivery sequence for `registry-operator-admin-tui`

| Work unit | Target outcome | Status |
| --- | --- | --- |
| PR 1 | CLI wiring, admin HTTP client seam, and in-memory session primitives | Completed |
| PR 2 | Bubble Tea login gate, authenticated admin navigation, and logout/expiry handling | Completed |
| PR 3 | Admin client integration tests plus Bubble Tea behavior coverage | Completed |
| PR 4 | Architecture/roadmap close-out for the shipped read-only admin TUI path | Completed |

## Delivery sequence for `registry-acl-v1`

| Work unit | Target outcome | Status |
| --- | --- | --- |
| PR 1 | Registry-wide read-only role: domain/service/store plumbing and the TUI toggle | Completed |
| PR 2 | Delegated repo-admin grants — backend: `/admin/v1/repositories/{repo}/grants` and the `requireAdminOrRepoAdmin` authority gate | Completed |
| PR 3 | Delegated repo-admin grants — TUI: `screenRepoAdminGrants`/`screenRepoAdminAddGrant`, reached from Console Repositories | Completed |
| PR 4 | Robot accounts — backend: `is_robot`, `/admin/v1/robots`, and the permanent password-login guard | Completed |
| PR 5 | Robot accounts — TUI plus this docs/roadmap close-out: `screenAdminRobots`/`screenAdminCreateRobot`, reusing the existing token screens | Completed |

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
