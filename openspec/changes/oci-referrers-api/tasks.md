# Tasks: OCI 1.1 Referrers API (oci-referrers-api)

## Mandatory Ordering Constraint (design.md Testing Strategy)

`handleV2`'s dispatch switch and `splitRepositoryPath` have **zero** covering
tests today (confirmed via codegraph blast radius). Phase 0 (0.1–0.3) MUST
land and pass **before** any behavior-changing task. Phase 1 (`NewManifest`
gains `artifactType`, 18 call sites) is a hard compile prerequisite for
Phase 2 (`parseManifestPayload` threading) and Phase 6 (`resolveArtifactType`
reads `Manifest.ArtifactType`). Phase 3 (store writes `subject_digest`) is a
hard prerequisite for Phase 5 (`ListReferrers` reads it) and Phase 4 (backfill
targets the same column). Phase 7 (router) depends on Phase 0 (dispatch
safety net), Phase 5–6 (`Service.Referrers`), and Phase 3 (`PublishManifest`
already writing the column, so freshly pushed referrers are discoverable
without waiting on a backfill). Every other behavior-changing task follows
RED → GREEN, Strict TDD.

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~1250–1750 (prod ~350–500, tests ~850–1200, docs+fixture ~50–80) |
| 800-line budget risk | High (session-cached review budget is **800**, not the skill default 400) |
| Chained PRs recommended | Yes |
| Suggested split | 4 units, mapped to design Testing Strategy phases 0–2, 3–4, 5–6, 7–8 |
| Delivery strategy | ask-on-risk |
| Chain strategy | pending |

**Rationale**: `NewManifest` gaining an `artifactType` parameter touches all
41 grep-confirmed call sites of `NewManifest(` across 9 files (design's
"roughly 18 call sites" undercounts test call sites that invoke it multiple
times per table-driven case) — `internal/domain/regixtry/manifest.go` (def),
`internal/app/regixtry/service.go`, `internal/infra/metadata/sqlite/store.go`
(prod, 1 each) plus 6 test files (`manifest_test.go`, `store_test.go` x24,
`service_signing_test.go` x4, `service_signing_bundle_test.go` x2,
`signature_status_test.go` x2, `secret_scan_status_test.go` x2). Even a
minimal 1–2 line diff per call site is ~50–90 lines on its own, before the
new `ArtifactType` field, constructor logic, and RED tests for Phase 1–2.
Combined with the mandatory Phase-0 characterization suite (three
zero-coverage surfaces: `splitRepositoryPath`, `handleV2` dispatch,
`writeJSON`) and 8 threat-matrix RED tests spread across Phases 0, 5, 6, 7,
this is very likely to exceed 800 lines as a single PR. Per design's Risks
section, "Phases 0–2 are a natural first slice, deliverable and independently
green" — used as PR 1's boundary below; remaining phases are grouped in the
design's own sequencing order, not reordered.

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Characterization safety net + `ArtifactType` threading (Phases 0–2, incl. the 18/41-call-site `NewManifest` change) | PR 1 | `go test ./internal/protocol/http/... ./internal/domain/regixtry/... ./internal/app/regixtry/... -run 'SplitRepositoryPath\|HandleV2Dispatch\|WriteJSON\|NewManifest\|ParseManifestPayload' -v` | N/A — no new endpoint yet; proven by unit/characterization suite only | Revert `manifest.go`, `service.go` parse threading, and the 41 call-site diffs; router/dispatch and schema untouched |
| 2 | Store layer: `subject_digest` column + idempotent boot-time backfill (Phases 3–4) | PR 2 | `go test ./internal/infra/metadata/sqlite/... -run 'PublishManifest\|Backfill' -v` | N/A — column/backfill has no reader yet (`ListReferrers` not built) | Revert `store.go` schema/backfill/`PublishManifest` delta; column is `NOT NULL DEFAULT ''`, inert to reverted binaries |
| 3 | `ListReferrers` query + `Service.Referrers` mapping/filtering (Phases 5–6) | PR 3 | `go test ./internal/infra/metadata/sqlite/... ./internal/app/regixtry/... -run 'ListReferrers\|Referrers\|ResolveArtifactType' -v` | N/A — no route calls `Service.Referrers` yet | Revert `ports.ReferrerRow`/`ListReferrers`, `queries.go` additions; no HTTP surface, no new schema beyond PR 2 |
| 4 | HTTP route wiring + full regression proof (Phases 7–8) | PR 4 | `go test ./internal/protocol/http/... -run Referrers -v` | Manual: `curl https://<host>/v2/<name>/referrers/sha256:<digest>` against a running instance seeded via `docker push`; confirm `200`, `manifests[]` shape, and `OCI-Filters-Applied` behavior | Revert router `/referrers/` marker, `handleV2` case, `handleReferrers`, `writeJSONAs` split, README; additive-only route, reverting restores `404 NAME_UNKNOWN` |

## Phase 0: Characterization (PR 1, no behavior change)

- [x] 0.1 RED+confirm GREEN `internal/protocol/http/router_test.go` (new):
      table-driven `splitRepositoryPath` test pinning today's five-marker
      outcomes, incl. `library/referrers/manifests/latest` and
      `library/manifests/manifests/latest`.
- [x] 0.2 RED+confirm GREEN (same file): `handleV2` dispatch table pinning
      today's routing for `blobs/uploads`, `blobs/`, `manifests/<ref>`,
      `/scan-status`, `/signature-status`, `/secret-scan-status`,
      `tags/list`, `_catalog`, and `referrers/<digest>` → `404 NAME_UNKNOWN`.
- [x] 0.3 RED+confirm GREEN (same file): `writeJSON` sets
      `Content-Type: application/json`, pinned before the `writeJSONAs` split.

## Phase 1: Domain — `NewManifest` ArtifactType (PR 1)

- [x] 1.1 RED `internal/domain/regixtry/manifest_test.go`: `NewManifest`
      carries `artifactType`; absent input → `""`, never a fallback at this
      layer.
- [x] 1.2 GREEN `internal/domain/regixtry/manifest.go`: add `ArtifactType`
      field to `Manifest`; add `artifactType` parameter to `NewManifest`.
- [x] 1.3 GREEN: update all 41 `NewManifest` call sites so the codebase
      compiles — prod: `internal/infra/metadata/sqlite/store.go`
      (`ResolveManifest` passes `""`), `internal/app/regixtry/service.go`;
      tests: `manifest_test.go`, `store_test.go`, `service_signing_test.go`,
      `service_signing_bundle_test.go`, `signature_status_test.go`,
      `secret_scan_status_test.go`.

## Phase 2: App Parse — `parseManifestPayload` Threading (PR 1)

- [x] 2.1 RED `internal/app/regixtry/service_test.go`: `parseManifestPayload`
      threads `manifestEnvelope.ArtifactType` into the returned
      `domain.Manifest`; unchanged for payloads without it.
- [x] 2.2 GREEN `internal/app/regixtry/service.go`: add `ArtifactType` to
      `manifestEnvelope`; thread it through `parseManifestPayload` into the
      `NewManifest` call.
- [x] 2.3 Confirm Phase 0–2 GREEN (Unit 1 focused test command); `go build
      ./...` clean.

## Phase 3: Store — `PublishManifest` Writes `subject_digest` (PR 2)

- [x] 3.1 RED `internal/infra/metadata/sqlite/store_test.go`:
      `PublishManifest` writes `subject_digest`; a manifest with no subject
      stores `''`; a repush with a different subject updates the stored
      value.
- [x] 3.2 GREEN `internal/infra/metadata/sqlite/store.go`: `ALTER TABLE
      manifests ADD COLUMN subject_digest TEXT NOT NULL DEFAULT ''` (+
      matching `CREATE TABLE` column); add `subject_digest` to
      `PublishManifest`'s insert column list and `ON CONFLICT DO UPDATE SET`.
- [x] 3.3 GREEN (same file): partial index `idx_manifests_subject ON
      manifests(tenant, repository_id, subject_digest, digest) WHERE
      subject_digest != ''`; `schema_backfills(name TEXT PRIMARY KEY,
      completed_at TEXT NOT NULL)` table, appended to `init()`.

## Phase 4: Store — Idempotent Backfill (PR 2)

- [x] 4.1 RED `internal/infra/metadata/sqlite/store_test.go`: pre-existing
      rows are backfilled after `New()`; a second `New()` changes no row and
      writes no second marker (idempotency).
- [x] 4.2 RED (same file): an unparseable payload leaves `subject_digest =
      ''` and `New()` still succeeds (threat matrix: boot-time migration
      availability).
- [x] 4.3 RED (same file): row updates and the `schema_backfills` marker
      insert commit atomically — assert via a single-transaction probe.
- [x] 4.4 GREEN `internal/infra/metadata/sqlite/store.go`:
      `backfillSubjectDigests()` — marker point-lookup guard; one
      transaction; field-probe parse (`struct{ Subject *struct{ Digest
      string } }`); `UPDATE` only successfully-parsed rows; `INSERT OR
      IGNORE` marker; called from `New()` after `init()`.
- [x] 4.5 Confirm Phase 3–4 GREEN (Unit 2 focused test command).

## Phase 5: Store Query — `ListReferrers` (PR 3)

- [x] 5.1 RED `internal/infra/metadata/sqlite/store_test.go`: `ListReferrers`
      cross-tenant returns empty; cross-repository returns empty — asserted
      directly against seeded rows in both, never inferred from HTTP (threat
      matrix: cross-tenant/cross-repository leakage, highest severity).
- [x] 5.2 RED (same file): ordering is `digest ASC` regardless of insertion
      order; `''` never matches; unknown digest → empty slice, not an error.
- [x] 5.3 RED (same file): `EXPLAIN QUERY PLAN` asserts the partial index is
      used given the literal `AND m.subject_digest != ''` predicate.
- [x] 5.4 GREEN `internal/ports/regixtry.go`: `ReferrerRow{Digest,
      MediaType, Size, Payload}`; `MetadataStore.ListReferrers(ctx, tenant,
      repository, subjectDigest) ([]ReferrerRow, error)`.
- [x] 5.5 GREEN `internal/infra/metadata/sqlite/store.go`: `ListReferrers` —
      `ListTags`'s three predicates + `subject_digest = ?` + literal `!=
      ''`; `ORDER BY m.digest ASC`.

## Phase 6: App Query — `Service.Referrers` (PR 3)

- [x] 6.1 RED `internal/app/regixtry/queries_test.go`: `resolveArtifactType`
      table test — manifest value wins; absent → `config.mediaType`; absent
      + no config → `""`.
- [x] 6.2 RED (same file): `Service.Referrers` authorizes before parsing the
      digest — an unauthorized caller sending a malformed digest gets `401`,
      not `400` (threat matrix: capability disclosure).
- [x] 6.3 RED (same file): `Manifests` is built with `make([]
      ReferrerDescriptor, 0, len(rows))` — assert never `nil` on zero rows.
- [x] 6.4 GREEN `internal/app/regixtry/queries.go`: `ReferrersIndex`,
      `ReferrerDescriptor`, `ociImageIndexMediaType` const,
      `resolveArtifactType`; `Service.Referrers(ctx, repositoryName,
      subjectDigest, artifactType)` — `parseRepository` → `authorize
      (ActionInspect)` → `domain.ParseDigest` → `store.ListReferrers` → map
      via `parseManifestPayload` + `resolveArtifactType` → filter on
      `artifactType`.
- [x] 6.5 Confirm Phase 5–6 GREEN (Unit 3 focused test command).

## Phase 7: Router / HTTP (PR 4)

- [x] 7.1 RED `internal/protocol/http/router_test.go`: `/referrers/`
      appended last to `splitRepositoryPath`'s markers, incl. a repository
      literally named `referrers` (threat matrix: route shadowing, extends
      0.1's table).
- [x] 7.2 RED (same file): `200` + empty `manifests[]` for never-pushed and
      for deleted subjects; assert the raw response body contains
      `"manifests":[]`, not `null` (threat matrix: silent wire
      non-conformance).
- [x] 7.3 RED (same file): `Content-Type:
      application/vnd.oci.image.index.v1+json`; `?artifactType=<v>` filters
      and sets `OCI-Filters-Applied: artifactType`; unfiltered and
      `?artifactType=` (empty/whitespace) set no header.
- [x] 7.4 RED (same file): pull-scoped token → `200`; no-access principal →
      `401` + challenge; non-GET → `405 Allow: GET` (threat matrix:
      privilege reuse).
- [x] 7.5 RED (same file): a seeded multi-referrer repository returns all
      matches in digest order in one response (threat matrix: unbounded
      response, bounded-scope guard).
- [x] 7.6 RED (same file; new fixture
      `testdata/bundle-referrer-manifest.json`): a cosign v3 bundle referrer
      (`subject` set) is listed; a legacy `.sig` manifest (no `subject`) is
      absent and its tag path still resolves.
- [x] 7.7 GREEN `internal/protocol/http/router.go`: split `writeJSON` into
      `writeJSON`/`writeJSONAs`; add `/referrers/` marker (last); add
      `handleV2` case; add `handleReferrers` (auth → digest parse →
      `Service.Referrers` → header → `writeJSONAs`).
- [x] 7.8 Confirm Phase 7 GREEN (Unit 4 focused test command, partial).

## Phase 8: Regression (PR 4)

- [x] 8.1 RED `internal/protocol/http/router_test.go` or existing suites:
      push, pull, tag listing, catalog, delete, scan queueing, and
      signature verification are byte-identical before and after (threat
      matrix / spec: no push-path or scan behavior change).
- [x] 8.2 Confirm full `go test ./...` zero regressions; `gofmt -l .` clean;
      `go vet ./...` clean.
- [x] 8.3 Docs: `README.md` — document the Referrers endpoint; retain the
      `provenance: false` workaround note verbatim.
