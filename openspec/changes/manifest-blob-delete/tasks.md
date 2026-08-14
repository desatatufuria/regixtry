# Tasks: Manifest and Tag Deletion (manifest-blob-delete)

## Mandatory Ordering Constraint (design.md Testing Strategy)

`handleManifest`, `PublishManifest`, and `ListManifestBlobs` have zero or only
indirect coverage today; `scope.go`'s `normalizeScopeActions` has none. Tasks
1.1 (HTTP characterization: DELETE on `manifests/<ref>` → `405`,
`Allow: PUT, GET, HEAD`) and 1.2 (scope characterization: `pull,push,delete`
is rejected today) MUST land and pass **before** any behavior-changing task.
The `scope.go` fix (1.3–1.4) is also a hard prerequisite for `/auth/token`
ever issuing a working `delete` scope — every later phase depends on it.
Every other behavior-changing task follows RED → GREEN, Strict TDD.

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~1100–1500 (prod ~200–260, tests ~870–1220, docs ~15) |
| 400-line budget risk | High (session-cached review budget is **1000**, not the skill default 400) |
| Chained PRs recommended | Yes |
| Suggested split | 4 units, mapped 1:1 to phases below |
| Delivery strategy | single-pr |
| Chain strategy | pending |

**Rationale**: this change touches domain/auth, ports, app/auth, app/regixtry,
sqlite store, HTTP router, and `main.go`, with zero pre-existing coverage on
three of the touched functions and one hard-rejecting parser (`scope.go`)
that must be fixed before token issuance works at all. Table-driven RED
suites (characterization, scope canonicalization, `HasDeleteAccess`,
`intersectRequestedActions`, store cascade/not-found, service ordering,
router integration incl. 4 threat-matrix cases) dominate the diff, similar in
shape to `registry-acl-v1` Phase 2 (~750–950 lines alone). The combined
estimate is at or above the session's 1000-line budget, so `single-pr`
requires an explicit `size:exception` before `sdd-apply`, or the
orchestrator asks the user for a chain strategy.

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Scope/auth foundation (Phase 1) | PR 1 | `go test ./internal/domain/auth/... ./internal/ports/... ./internal/app/auth/... -run 'Scope\|Delete' -v` | N/A — no runnable endpoint changes yet; proven by unit suite only | Revert `scope.go`/`principal.go`/`defaults.go`/`ports/regixtry.go`/`app/auth/service.go` delta; no schema, no route change |
| 2 | Store layer (Phase 2) | PR 2 | `go test ./internal/infra/metadata/sqlite/... -run Delete -v` | N/A — new store methods have no caller yet | Revert `DeleteManifestByDigest`/`DeleteTag`; existing cascade FKs untouched |
| 3 | Service layer (Phase 3) | PR 3 | `go test ./internal/app/regixtry/... -run DeleteManifest -v` | N/A — no HTTP route calls it yet | Revert `Service.DeleteManifest`/`DeletionDetails`; `deleteEnabled` field stays unread |
| 4 | HTTP layer, config, docs (Phase 4) | PR 4 | `go test ./internal/protocol/http/... ./cmd/regixtry/... -run 'Delete\|Manifest' -v` | Manual: `REGISTRY_DELETE_ENABLED=true` then `curl -X DELETE https://<host>/v2/team/app/manifests/sha256:<digest>` against a running instance; confirm `202` body and blob dir unchanged | Revert router `case MethodDelete`, `Allow` list, `main.go` flag wiring, docs; flag defaults `false` so a reverted binary answers `405` again |

## Phase 1: Scope/Auth Foundation (PR 1)

- [x] 1.1 Characterization RED+confirm GREEN `internal/protocol/http/router_test.go`
      (new/extend): table-driven — DELETE on `manifests/<ref>` → `405`,
      `Allow: PUT, GET, HEAD`; PUT/GET/HEAD unchanged. Zero prod change.
- [x] 1.2 Characterization RED+confirm GREEN `internal/domain/auth/scope_test.go`
      (new): `ParseScope("repository:x:pull,push,delete")` fails today with
      the current rejection message. Zero prod change.
- [x] 1.3 RED (same file): target behavior — `pull,push,delete` succeeds and
      canonicalizes in `pull,push,delete` order; an unknown action still
      fails — table-driven.
- [x] 1.4 GREEN `internal/domain/auth/scope.go`: `actionDelete` const,
      `AllowsDelete()`, allow-list + canonical sort-weight edit (Decision 4).
- [x] 1.5 RED `internal/ports/action_scope_test.go`: `Action{Verb:
      ActionDelete}.Scope()` == `"repository:<name>:delete"` (not
      `pull,delete`).
- [x] 1.6 GREEN `internal/ports/regixtry.go`: `ActionDelete ActionVerb`,
      `Action.Scope()` arm.
- [x] 1.7 RED `internal/domain/auth/principal_test.go`: `HasDeleteAccess`
      table-driven — writer+`delete` scope passes; writer+`pull,push` only
      fails; reader fails; admin passes; read-only fails.
- [x] 1.8 GREEN `internal/domain/auth/principal.go`: `HasDeleteAccess`
      (Decision 5 — does not reuse `HasWriteAccess`).
- [x] 1.9 RED `internal/app/auth/service_test.go`: `intersectRequestedActions`
      — writer requesting `delete` gets it; requesting `pull,push` never
      gets it; read-only never gets it; admin honours the request.
- [x] 1.10 GREEN `internal/app/auth/service.go`: add `allowDelete` branch to
      `intersectRequestedActions`, own statement, never folded into push
      (Decision 5).
- [x] 1.11 RED `internal/ports/defaults_test.go`: `principalAccessController
      .Authorize` `case ActionDelete` — writer/admin pass, reader/anonymous
      fail; `configurableAccessController` untouched — anonymous delete
      falls through to `NewUnauthorizedError` even with anonymous-pull
      enabled (threat matrix: anonymous destructive access).
- [x] 1.12 GREEN `internal/ports/defaults.go`: add `case ActionDelete` arm to
      `principalAccessController.Authorize` only.
- [x] 1.13 Confirm Phase 1 GREEN (Unit 1 focused test command).

## Phase 2: Store Layer (PR 2, depends on Phase 1 for `MetadataStore` shape)

- [x] 2.1 Pin `PRAGMA foreign_keys` == `1` in `store_test.go` (mirrors the
      `busy_timeout` assertion, `store_test.go:139-145`) — expected
      already-GREEN cascade-premise guard, not a state that must flip.
- [x] 2.2 GREEN `internal/ports/regixtry.go`: add `DeleteManifestByDigest`/
      `DeleteTag` signatures to `MetadataStore` (compile prerequisite for
      2.3/2.5, interleaved per Decision 3).
- [x] 2.3 RED `store_test.go`: `DeleteManifestByDigest` on a digest with 3
      tags removes the manifest, all 3 tags, and `manifest_blobs` rows,
      returns the 3 names; absent digest returns `domain.ErrorCodeNotFound`
      — table-driven, `t.TempDir()`.
- [x] 2.4 GREEN `store.go`: `DeleteManifestByDigest` — SELECT tag names
      in-transaction before DELETE; rely on `ON DELETE CASCADE`; zero rows
      affected → `NewNotFoundError`.
- [x] 2.5 RED `store_test.go`: `DeleteTag` removes only the named row;
      manifest and sibling tag still resolve; absent tag returns
      `NotFound` — table-driven.
- [x] 2.6 GREEN `store.go`: `DeleteTag` — DELETE by
      tenant/repository_id/name; zero rows affected → `NewNotFoundError`.
- [x] 2.7 Confirm Phase 2 GREEN (Unit 2 focused test command).

## Phase 3: Service Layer (PR 3, depends on Phases 1–2)

- [x] 3.1 RED `internal/app/regixtry/service_test.go`: `DeleteManifest`
      refuses an unauthorized caller with `NewUnauthorizedError` even when
      the flag is off (Decision 2 ordering — auth before flag).
- [x] 3.2 RED (same file): authorized caller with flag off gets
      `domain.NewValidationError`/`UNSUPPORTED`, never reaches the store.
- [x] 3.3 GREEN `service.go`: `DeleteManifest(ctx, repositoryName,
      reference)` — `parseRepository` → `authorize(ActionDelete)` →
      `deleteEnabled` check → digest/tag disambiguation via
      `domain.ParseDigest` (same idiom as `parseManifestPayload`) → store
      call. Add `deleteEnabled bool` field + setter (mirrors `scanHost`
      setter, `service.go:131-134`).
- [x] 3.4 RED: digest path — 3 tags on one digest →
      `DeletionDetails{ManifestRemoved:true, TagsRemoved:[3 names]}`.
- [x] 3.5 RED: tag path —
      `DeletionDetails{ManifestRemoved:false, TagsRemoved:[tag]}`; manifest
      and siblings untouched.
- [x] 3.6 GREEN `internal/app/regixtry/queries.go`: `DeletionDetails`
      struct (Decision 1); wire both paths in `DeleteManifest`. (Interleaved
      into 3.3's commit — Go's whole-function compilation forced
      `DeletionDetails` and both store-call paths to exist before 3.1/3.2
      could even compile, mirroring Phase 2 task 2.2's compile-prerequisite
      interleaving. 3.4/3.5 confirm the already-implemented behavior is
      correct rather than driving new production code.)
- [x] 3.7 RED: absent digest and absent tag each surface
      `domain.ErrorCodeNotFound` through the service. (Already GREEN —
      the store's typed NotFound propagates unchanged, `err != nil` returns
      it verbatim.)
- [x] 3.8 Confirm Phase 3 GREEN (Unit 3 focused test command).

## Phase 4: HTTP Layer, Config, Docs (PR 4, depends on Phases 1–3)

- [x] 4.1 RED `router_test.go` (extends 1.1's file): DELETE on
      `manifests/<digest>` — flag-on/authorized → `202` + JSON body;
      flag-off/authorized → `400 UNSUPPORTED`; unauthenticated → `401` +
      `WWW-Authenticate scope="...:delete"`; unknown ref → `404
      MANIFEST_UNKNOWN` — table-driven.
- [x] 4.2 RED: `Allow` header grows to `PUT, GET, HEAD, DELETE` for any
      other method against `manifests/<ref>`.
- [x] 4.3 GREEN `router.go`: `handleManifest` gains `case
      stdhttp.MethodDelete` calling `Service.DeleteManifest`, writes `202`
      via `writeJSON`; `default:` `Allow` list updated.
- [x] 4.4 RED: DELETE on `tags/list`, `blobs/<digest>`,
      `blobs/uploads/<id>` unchanged (threat matrix: HTTP method dispatch —
      no new path-shadowing).
- [x] 4.5 RED: a `pull,push` token is rejected on DELETE while still
      succeeding on PUT, one test (threat matrix: privilege reuse).
- [x] 4.6 RED: reader `401`, writer `202`, repo-admin `202`; `pull,push`
      token `401`, `pull,push,delete` token `202` — integration table.
- [x] 4.7 RED: blob directory byte-identical before/after both delete paths
      (threat matrix: blob-store scope creep, non-goal guard).
- [x] 4.8 RED `cmd/regixtry/main_test.go`: config parse defaults
      `DeleteEnabled` to `false` with the env var unset and unparseable.
- [x] 4.9 GREEN `cmd/regixtry/main.go`: `DeleteEnabled` field,
      `flags.BoolVar(..., parseBoolEnv("REGISTRY_DELETE_ENABLED", false),
      ...)`, threaded into the service.
- [x] 4.10 GREEN: wire the router's `DELETE` case to the flag/service
      plumbing end-to-end so 4.1/4.2/4.5–4.8 pass.
- [x] 4.11 Confirm Phase 4 GREEN (Unit 4 focused test command); then full
      `go test ./...` (zero regressions) and `gofmt -l .` (clean).
- [x] 4.12 Docs: `docs/configuration.md` — document `REGISTRY_DELETE_ENABLED`
      (default `false`); `docs/roadmap.md` — mark manifest/tag deletion
      delivered.
