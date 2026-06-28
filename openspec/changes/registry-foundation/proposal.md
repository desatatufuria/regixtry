# Proposal: Registry Foundation

## Intent

Build a low-resource, single-binary OCI registry for internal and OSS use. This change locks the v1 boundary, docs baseline, and future seams before implementation.

## Scope

### In Scope
- Single-tenant v1 with local filesystem storage and OCI/Docker-compatible push/pull.
- Repository/tag browsing, manifest/blob inspection, and a thin operator TUI for visibility and maintenance basics.
- Early docs/roadmap foundation: vision, non-goals, API glossary, GitFlow/contribution workflow, architecture overview, and roadmap groups.
- Architecture seams for auth, tenancy, storage, metadata, registry API, TUI, and background jobs.

### Out of Scope
- Multi-tenant isolation, complex RBAC, replication, scanning, signing orchestration, remote storage, and broad admin APIs.
- Treating Docker Engine API as the registry contract.
- Full auth implementation beyond architecture-prepared seams.

## Capabilities

### New Capabilities
- `registry-protocol`: OCI Distribution API-compatible push, pull, manifests, blobs, tags, and repository listing.
- `registry-storage`: Local content-addressed storage, upload lifecycle, and GC-safe blob/metadata handling.
- `operator-console`: Keyboard-first TUI for inspection, operational visibility, and maintenance basics.
- `project-docs-foundation`: Vision, scope boundaries, glossary, roadmap, contribution workflow, and documentation habits.

### Modified Capabilities
- None.

## Approach

Adopt a distribution-first minimal registry. Keep registry semantics in the core service, keep the TUI as an operator client, and preserve seams for later auth, storage, and multi-tenancy.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `openspec/changes/registry-foundation/proposal.md` | New | Product contract for later SDD phases |
| `openspec/changes/registry-foundation/specs/` | New | Future delta specs for the listed capabilities |
| `openspec/specs/` | Modified later | Main capability specs added after approval |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Scope expands toward Harbor-like platform features | Med | Freeze v1 non-goals in specs/design |
| Registry API gets confused with Docker Engine API | Med | Keep protocol boundary explicit in docs/specs |
| TUI absorbs domain logic | Low | Enforce service/client separation in design |

## Rollback Plan

If the direction proves wrong, revert to bootstrap-only state by removing this change’s proposal/spec/design/task artifacts and stopping implementation before runtime becomes a default path.

## Dependencies

- OCI Distribution / Docker Registry HTTP API as the protocol surface.
- Bubble Tea-based TUI only as an operator console extra.

## Success Criteria

- [ ] Later specs can map directly to the four named capabilities without re-scoping v1.
- [ ] Design can preserve single-tenant/local-storage simplicity while keeping auth and future expansion as seams.
- [ ] Documentation/roadmap expectations are explicit before implementation starts.

## Proposal Question Round

- Confirm whether deletion/retention belongs in v1 maintenance basics or lands after read-only inspection.
- Confirm whether any local-only mode may allow anonymous pull.
