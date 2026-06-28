# Tasks: Registry Foundation

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 1100-1600 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 0 bootstrap -> PR 1 docs/foundation -> PR 2 storage/protocol core -> PR 3 console/verification |
| Delivery strategy | ask-on-risk |
| Chain strategy | feature-branch-chain |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: feature-branch-chain
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 0 | Bootstrap repo and GitFlow base | PR 0 | First commit on `main`; create `develop`; branch `feature/registry-foundation` from `develop`. |
| 1 | Docs and architecture foundation | PR 1 | Base = `feature/registry-foundation`; keep roadmap/README/contributing/docs together. |
| 2 | Registry storage + protocol core | PR 2 | Base = PR 1 branch; include domain, ports, fs/sqlite, HTTP, tests. |
| 3 | TUI read models + end-to-end verification | PR 3 | Base = PR 2 branch; keep Bubble Tea screens and Docker/TUI checks together. |

## Phase 1: Bootstrap and Documentation Foundation

- [x] 1.1 Create `go.mod`, `.gitignore`, `README.md`, `docs/roadmap.md`, `docs/glossary.md`, and `docs/contributing.md` with v1 scope, non-goals, API boundary, and GitFlow workflow.
- [x] 1.2 Create `docs/architecture.md` describing single-binary layers, ports, storage/auth seams, and TUI thin-client boundary from `design.md`.
- [x] 1.3 Make the first repository commit on `main`, create `develop`, then create `feature/registry-foundation`; document branch/PR order in `docs/contributing.md`.

## Phase 2: Domain and Storage Foundation

- [ ] 2.1 Create `internal/domain/registry/` types for digest, repository reference, manifest, upload state, descriptors, and registry errors with unit tests.
- [ ] 2.2 Create `internal/ports/` interfaces for `BlobStore`, `MetadataStore`, `AccessController`, `TenantResolver`, and `JobRunner` plus config-ready single-tenant/auth defaults.
- [ ] 2.3 Create `internal/infra/storage/fsblob/` for upload staging, digest validation, blob promotion, and blob reads with temp-dir integration tests.
- [ ] 2.4 Create `internal/infra/metadata/sqlite/` schema and repositories for catalog, tags, manifest references, blob linkage, and restart-safe upload metadata with SQLite integration tests.

## Phase 3: Registry Service and Protocol Wiring

- [ ] 3.1 Create `internal/app/registry/service.go` and `queries.go` for publish, resolve, catalog, tags, manifest/blob inspection, and upload-state queries.
- [ ] 3.2 Create `internal/protocol/http/` routing and OCI-compatible handlers for blob upload, manifest publish/read, catalog, tags, and auth-challenge responses.
- [ ] 3.3 Create `cmd/registry/main.go` with `serve` and `tui` commands, filesystem/SQLite wiring, config for anonymous pull, and smoke-start tests.

## Phase 4: Operator Console and Verification

- [ ] 4.1 Create `internal/tui/` Bubble Tea models for repositories, tags, manifests, blobs, uploads, empty state, and unavailable v1 mutations via service queries only.
- [ ] 4.2 Add protocol integration tests for push/pull success, digest mismatch rejection, anonymous pull on/off, missing blob publish rejection, and incomplete upload invisibility.
- [ ] 4.3 Add end-to-end verification notes/scripts under `docs/` for Docker push/pull compatibility and TUI inspection smoke coverage; keep them aligned with PR work units.
