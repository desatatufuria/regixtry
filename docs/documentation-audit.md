# Documentation audit

This is a living audit-of-the-docs document: it tracks what the reader-facing docs claim, what the code actually confirms, and where the two might drift. It does not describe the product itself — see `README.md` and `docs/` for that.

This refresh is part of a parallel doc-rewrite pass across `README.md` and `docs/` (auth/ACL, features, architecture, protocol/API). This file's own job is limited to auditing accuracy; it does not re-verify every other writer's output line by line, only its own inventory below.

## Documented functionality

- Binary release installer and build-from-source.
- CLI subcommands: `serve`, `tui`, `bootstrap`, `bootstrap-admin`, `setup`, `feature`, `uninstall`, `upgrade`.
- The `feature` subcommand family: `list`, `show`, `status`, `install`, `upgrade`, `rollback`, `enable`, `disable`, `configure` — the operator-facing surface for managed features (Trivy, Gitleaks, signing).
- Bind, public URL, TLS, storage, SQLite, PostgreSQL, auth, and timeout configuration.
- Layered architecture and filesystem/SQLite/PostgreSQL persistence.
- Bearer challenge, Basic token exchange, access tokens, scopes, grants, and roles.
- `/auth/token`, `/admin/v1`, and Registry `/v2` API routes.
- Push/pull, chunked uploads, tags, catalog, and HEAD.
- Repository access control completion (`registry-acl-v1`): the registry-wide read-only role (`is_read_only`, grant-independent pull access), delegated repo-admin grant management scoped to exactly one repository (`/admin/v1/repositories/{repo}/grants`), and bounded-TTL, revocable robot accounts (`/admin/v1/robots`) permanently excluded from password login and from the default human user listing.
- Supply-chain scanning and signing as managed features: Trivy vulnerability scanning (`internal/infra/scanning/trivy/`), Gitleaks secret scanning (`internal/infra/scanning/gitleaks/`), and cosign signature verification (`internal/domain/signing/`), each registered through the shared feature-runtime pattern (`internal/app/scanning/scheduler.go`).
- Signing policy administration: `GET`/`PUT /admin/v1/signing-policy` (multiple trusted public keys per policy, globally and per repository) and `GET /admin/v1/signing-policy/key-usage`, with a fail-closed pull gate when a repository's policy requires a valid signature and none is found.
- Blob garbage collection: `POST /admin/v1/gc/reports`, `GET /admin/v1/gc/reports/{id}`, `POST /admin/v1/gc/reports/{id}/delete` (gated by `REGISTRY_GC_DELETE_ENABLED`), and manifest/tag deletion: `DELETE /v2/{repo}/manifests/{ref}` (gated by `REGISTRY_DELETE_ENABLED`).
- Repository scan overview and history: `GET /admin/v1/scan-runs?repository=&limit=` and `GET /admin/v1/scan-runs/{id}`, the per-manifest `GET /v2/<repo>/manifests/<ref>/scan-status`, and the operator console's scan-history views.
- Compose local, systemd, CI/CD, TUI, operations, security, and troubleshooting.

## Partially understood functionality

- Exact compliance with the full OCI Distribution Specification is not demonstrated by a complete conformance test.
- Multi-arch/index manifest support was not confirmed with sufficient confidence from the inspected implementation.
- Docker daemon behavior against insecure HTTP depends on external daemon configuration, outside this repository's control.
- Operational backup/restore has no dedicated commands in the code.
- The exact set of scan/signing policy override combinations exposed through the TUI (per-repository overrides vs. registry defaults) was not exhaustively traced against every admin screen.

## Apparently incomplete functionality

- Upload cancellation: the `DELETE` route exists in the dispatcher but returns `UNSUPPORTED`.
- The storage interface defines cancellation, but the public API does not expose it functionally.
- The TUI can list and change user enablement and manage repo-admin grants and robot accounts, but does not cover every admin API mutation (e.g., admin-API pagination and user deletion stay deferred, per `docs/roadmap.md`).
- The job runner (including scan scheduling) is inline; there is no persistent asynchronous queue.

## Potentially dead or unexposed code

- Interfaces and service methods exist for broader operations, including user deletion and some repository actions, without an equivalent public HTTP route in this version.
- No code is marked dead with certainty: additional coverage/runtime analysis would be needed to distinguish forward-looking seams from genuinely unused code.

## Verified important absences

- No health endpoint separate from `/v2/`.
- No metrics endpoint or confirmed structured logging.
- No remote storage, replication, or multi-tenancy.
- No granular authorization independent of the existing grants/scopes/read-only-role/robot-account model.
- No silent refresh-token flow in the TUI.
- No provenance attestation beyond the shipped cosign signature verification (no SLSA-style build provenance).

## Tests and contradictions

The repository contains unit tests for domain, application, stores, router, auth, lifecycle, releases, scanning (Trivy/Gitleaks runners and runtime managers), signing, and TUI, plus smoke scripts under `docs/verification/scripts/`. The documentation uses the contracts observed in handlers and structs; examples should not be read as a full Docker/OCI certification.

No open contradiction between `docs/roadmap.md` and the codebase remains as of this audit: the roadmap's `registry-acl-v1` and supply-chain scanning/signing workstreams are both marked complete, matching the code confirmed above. (A prior roadmap revision listed signing/scanning as a non-goal; that line has been corrected in the same doc pass this audit is part of.)

## Open questions

- Should a formal subset of OCI Distribution be declared and tested against a conformance suite?
- Should the TUI receive the remaining administrative mutations, or stay a deliberately partial client?
- Is an official backup/restore guide needed for SQLite, blobs, and PostgreSQL?
- Should scan-on-push (as opposed to on-demand/scheduled rescans) be added, and if so, should it block manifest publication?
