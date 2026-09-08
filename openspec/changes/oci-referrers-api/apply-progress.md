# Apply Progress: OCI 1.1 Referrers API (oci-referrers-api)

## Scope of this run

PR #1 of 4 in the `feature/oci-referrers-api` Feature Branch Chain — **Phases
0–2 only** (tasks.md), on branch
`feature/oci-referrers-api-01-characterization-domain`, branched from the
tracker branch `feature/oci-referrers-api`. Phases 3–8 (store schema/backfill,
`ListReferrers` query/service, router/HTTP handler, regression) are explicitly
out of scope for this run and are deferred to PR #2–#4 per tasks.md's
"Suggested Work Units" table.

This is the first apply run for this change — no prior apply-progress existed
to merge.

## Completed Tasks

### Phase 0: Characterization (PR 1, no behavior change)

- [x] 0.1 `internal/protocol/http/router_test.go`: table-driven
      `TestSplitRepositoryPathCharacterizesCurrentFiveMarkerBehavior` pinning
      today's five-marker outcomes, including `library/referrers/manifests/latest`
      and `library/manifests/manifests/latest`.
- [x] 0.2 `internal/protocol/http/router_test.go`:
      `TestHandleV2DispatchCharacterizesCurrentRoutingBeforeReferrers` pinning
      today's routing for `blobs/uploads`, `blobs/`, `manifests/<ref>`,
      `/scan-status`, `/signature-status`, `/secret-scan-status`, `tags/list`,
      `_catalog`, and `referrers/<digest>` → `404 NAME_UNKNOWN` (proves no
      referrers route is dispatched anywhere yet).
- [x] 0.3 `internal/protocol/http/router_test.go`:
      `TestWriteJSONSetsApplicationJSONContentType` pinning `writeJSON`'s
      `Content-Type: application/json` before the future `writeJSONAs` split.

### Phase 1: Domain — `NewManifest` ArtifactType (PR 1)

- [x] 1.1 RED `internal/domain/regixtry/manifest_test.go`:
      `TestNewManifestCarriesArtifactType` and
      `TestNewManifestArtifactTypeAbsentIsEmptyNeverAFallback`.
- [x] 1.2 GREEN `internal/domain/regixtry/manifest.go`: added `ArtifactType
      string` field to `Manifest`; added `artifactType string` parameter to
      `NewManifest` (placed as the 2nd parameter, right after `mediaType`).
- [x] 1.3 GREEN: updated all 21 real `NewManifest` call sites (not 41 — see
      Deviations) so the codebase compiles: prod
      `internal/infra/metadata/sqlite/store.go` (`ResolveManifest` passes
      `""`), `internal/app/regixtry/service.go` (Phase 1 passed `""` as a
      placeholder, threaded for real in Phase 2); tests: `manifest_test.go`
      (2), `store_test.go` (12), `service_signing_test.go` (2),
      `service_signing_bundle_test.go` (1), `signature_status_test.go` (1),
      `secret_scan_status_test.go` (1).

### Phase 2: App Parse — `parseManifestPayload` Threading (PR 1)

- [x] 2.1 RED `internal/app/regixtry/service_test.go`:
      `TestParseManifestPayloadThreadsArtifactType` and
      `TestParseManifestPayloadArtifactTypeAbsentStaysEmpty`.
- [x] 2.2 GREEN `internal/app/regixtry/service.go`: added `ArtifactType
      string` (`json:"artifactType"`) to `manifestEnvelope`; threaded
      `strings.TrimSpace(envelope.ArtifactType)` through `parseManifestPayload`
      into the `domain.NewManifest` call, replacing Phase 1's `""` placeholder.
- [x] 2.3 Confirmed Phase 0–2 GREEN via the Unit 1 focused test command; full
      `go build ./...` clean.

## TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| 0.1 | `router_test.go` (`TestSplitRepositoryPathCharacterizes...`) | Unit | N/A (approval test, pre-existing behavior, no prior test) | ✅ Written | ✅ Passed immediately (approval test — see Deviations) | ✅ 13 table cases | ➖ None needed |
| 0.2 | `router_test.go` (`TestHandleV2DispatchCharacterizes...`) | Integration | N/A (approval test, no prior test) | ✅ Written | ✅ Passed after 2 fixture fixes (see Deviations) | ✅ 9 subtests | ➖ None needed |
| 0.3 | `router_test.go` (`TestWriteJSONSetsApplicationJSONContentType`) | Unit | N/A (approval test, no prior test) | ✅ Written | ✅ Passed immediately | ➖ Single (writeJSON has one behavior to pin) | ➖ None needed |
| 1.1/1.2 | `manifest_test.go` (`TestNewManifestCarriesArtifactType`, `...AbsentIsEmptyNeverAFallback`) | Unit | ✅ 3/3 pre-existing manifest tests passing before edit | ✅ Written — confirmed compile failure (`too many arguments in call to NewManifest`) | ✅ Passed after adding `ArtifactType` field + parameter | ✅ 2 cases (present value / absent → "") | ✅ Clean — no further extraction needed |
| 1.3 | 21 call sites across 8 files | N/A (mechanical) | ✅ full suite green before edit | N/A — compile-fix only, no new test | ✅ `go build`/`go vet` clean after all 21 updated | ➖ N/A | ➖ N/A |
| 2.1/2.2 | `service_test.go` (`TestParseManifestPayloadThreadsArtifactType`, `...ArtifactTypeAbsentStaysEmpty`) | Unit | ✅ full suite green before edit | ✅ Written — confirmed RED (`manifest.ArtifactType = "", want "application/vnd.example.sbom.v1+json"`); absent-case test passed trivially against the Phase-1 placeholder (documented, not treated as a hidden gap) | ✅ Passed after threading `envelope.ArtifactType` | ✅ 2 cases (present / absent) | ➖ None needed |

### Test Summary
- **Total tests written**: 9 new test functions (`TestSplitRepositoryPathCharacterizesCurrentFiveMarkerBehavior` with 13 subtests, `TestHandleV2DispatchCharacterizesCurrentRoutingBeforeReferrers` with 9 subtests, `TestWriteJSONSetsApplicationJSONContentType`, `TestNewManifestCarriesArtifactType`, `TestNewManifestArtifactTypeAbsentIsEmptyNeverAFallback`, `TestParseManifestPayloadThreadsArtifactType`, `TestParseManifestPayloadArtifactTypeAbsentStaysEmpty`)
- **Total tests passing**: all of the above, plus the full pre-existing suite (`go test ./...` — see Verification Evidence)
- **Layers used**: Unit (7 test functions), Integration (1 test function, 9 subtests, exercised through the full HTTP router)
- **Approval tests** (characterization, Phase 0): 3 test functions, 23 total sub-assertions, capturing pre-existing zero-coverage behavior before any production change
- **Pure functions created**: 0 new — `NewManifest` and `parseManifestPayload` already existed; this PR only widened their signatures

## Files Changed

| File | Action | What Was Done |
|------|--------|---------------|
| `internal/protocol/http/router_test.go` | Modified | Added Phase 0 characterization tests (+286 lines): `splitRepositoryPath` table test, `handleV2` dispatch table test, `writeJSON` Content-Type test |
| `internal/domain/regixtry/manifest.go` | Modified | Added `Manifest.ArtifactType` field; added `artifactType` parameter to `NewManifest` (2nd position) |
| `internal/domain/regixtry/manifest_test.go` | Modified | Added 2 new tests; updated 2 pre-existing call sites to the new signature |
| `internal/app/regixtry/service.go` | Modified | Added `manifestEnvelope.ArtifactType`; threaded it through `parseManifestPayload` into `domain.NewManifest` |
| `internal/app/regixtry/service_test.go` | Modified | Added 2 new tests for `parseManifestPayload` artifactType threading |
| `internal/app/regixtry/service_signing_test.go` | Modified | Updated 2 call sites to the new `NewManifest` signature (pass `""`) |
| `internal/app/regixtry/service_signing_bundle_test.go` | Modified | Updated 1 call site to the new `NewManifest` signature (pass `""`) |
| `internal/infra/metadata/sqlite/store.go` | Modified | Updated `ResolveManifest`'s `NewManifest` call to pass `""` with an explanatory comment (Decision 5's "no JSON parse on the hot pull path" reasoning) |
| `internal/infra/metadata/sqlite/store_test.go` | Modified | Updated 12 call sites to the new `NewManifest` signature (pass `""`) |
| `internal/protocol/http/signature_status_test.go` | Modified | Updated 1 call site to the new `NewManifest` signature (pass `""`) |
| `internal/protocol/http/secret_scan_status_test.go` | Modified | Updated 1 call site to the new `NewManifest` signature (pass `""`) |
| `openspec/changes/oci-referrers-api/tasks.md` | Modified | Marked tasks 0.1–2.3 `[x]` |

## Deviations from Design

1. **Call-site count: 21 real call sites, not 41.** tasks.md's Review Workload
   Forecast cited "41 grep-confirmed call sites of `NewManifest(`". A literal
   `rg -n "NewManifest\("` does return 41 matches, but that count includes
   `t.Fatalf("... NewManifest() error = %v", err)` string-literal lines (one
   per real call in most test files) and the function definition itself. The
   actual set of expressions that construct-call `NewManifest` and needed a
   signature update is **21** (1 in `service.go`, 1 in `store.go`, 12 in
   `store_test.go`, 2 in `service_signing_test.go`, 1 in
   `service_signing_bundle_test.go`, 1 in `signature_status_test.go`, 1 in
   `secret_scan_status_test.go`, 2 in `manifest_test.go`), verified via
   `go build ./...` and `go vet ./...` both reporting zero remaining call-site
   errors after the edit. This does not change scope or design — every call
   site design.md and tasks.md care about was updated — it only corrects an
   inflated raw-grep estimate that was already flagged as an overcount in
   tasks.md's own Rationale ("undercounts test call sites that invoke it
   multiple times per table-driven case" — the actual issue was the reverse:
   the naive count included non-call lines).

2. **`NewManifest` parameter position.** design.md's interface reasoning
   ("`ResolveManifest` passes `""`, alongside the `nil` it already passes for
   subject") does not pin an exact parameter index. I placed `artifactType`
   as the constructor's 2nd parameter (`mediaType string, artifactType
   string, payload []byte, ...`), immediately after `mediaType`, mirroring
   the OCI convention of `mediaType`/`artifactType` as paired type
   classifiers. This is a naming/ordering choice with zero behavioral
   consequence; the compile-error-at-every-call-site enforcement design.md
   calls out holds regardless of position.

3. **Phase 0.2's dispatch test needed two implementation fixes discovered
   only by running it** (documented here per the Rules: "if a task is
   blocked by something unexpected, STOP and report back" — these were
   resolved within scope, not deferred, since they are test-file-only fixes
   with no production-code impact):
   - `t.Cleanup(cleanup)` instead of `defer cleanup()`: the parallel
     subtests (`t.Parallel()`) resume after the parent test function body
     returns, so a `defer`red store-close ran before the subtests executed
     against it (`sql: database is closed`). Switched to `t.Cleanup`, which
     Go guarantees runs only after the parent and all its subtests finish.
   - The blob-read subtest's first repository name (`dispatch/blobs`)
     collided with the `/blobs/` marker inside `splitRepositoryPath` itself
     (the same route-shadowing class of bug design.md Decision 6 discusses
     for `/referrers/`), misrouting the digest into the reference. Renamed
     the fixture repository to `team/blob-read-dispatch` to avoid the
     collision. This is itself a small, useful confirmation of Decision 6's
     reasoning, not a design gap.
   - The secret-scan-status subtest's original assertion (`finding_count`
     present) is wrong for the *unscanned* state, which is byte-identical in
     shape between `scan-status` and `secret-scan-status` responses (both
     lack a `scan` object when unscanned). Fixed the assertion to check for
     the absence of `would_block_pull` (present only in `ScanStatusResult`)
     and `signature` (present only in `SignatureStatusResult`) instead —
     confirmed against `queries.go`'s actual struct definitions
     (`ScanStatusResult`, `SecretScanStatusResult`, `SignatureStatusResult`).

No other deviations. Every production-code decision in this PR (field name,
no fallback at the domain layer, `manifestEnvelope.ArtifactType` JSON tag,
threading location) matches design.md Decision 3/4 and tasks.md 1.1–2.3
exactly.

## Issues Found

None in scope. Two follow-ups are explicitly out of scope for PR #1 and
already tracked by design.md/tasks.md for later phases:
- Phases 3–8 (store schema, backfill, `ListReferrers`, `Service.Referrers`,
  router wiring, regression suite) — PR #2, #3, #4.
- Everything else in design.md's own Open Questions list is unaffected by
  this PR's scope.

## Remaining Tasks

- [ ] Phase 3: Store — `PublishManifest` writes `subject_digest` (PR 2)
- [ ] Phase 4: Store — Idempotent Backfill (PR 2)
- [ ] Phase 5: Store Query — `ListReferrers` (PR 3)
- [ ] Phase 6: App Query — `Service.Referrers` (PR 3)
- [ ] Phase 7: Router / HTTP (PR 4)
- [ ] Phase 8: Regression (PR 4)

## Workload / PR Boundary

- Mode: chained PR slice (Feature Branch Chain, per session preflight —
  tasks.md's `Chain strategy: pending` field is superseded by the
  orchestrator-resolved `feature-branch-chain` passed into this run)
- Current work unit: Unit 1 — "Characterization safety net + `ArtifactType`
  threading (Phases 0–2)"
- Boundary: starts from a clean `develop`-derived tracker branch state (no
  prior commits on this leaf branch before this run) and ends with Phases
  0–2 fully green, no router/store/HTTP surface touched
- Estimated review budget impact: **468 changed lines** (`git diff --stat
  feature/oci-referrers-api...HEAD`: 424 insertions + 44 deletions across 12
  files, including the 18-line `tasks.md` checkbox delta) — well under the
  800-line session-cached PR budget for this chain, and under tasks.md's own
  pessimistic ~1250–1750 total-change estimate divided across 4 PRs

## Work Unit Evidence

| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test ./internal/protocol/http/... ./internal/domain/regixtry/... ./internal/app/regixtry/... -run 'SplitRepositoryPath\|HandleV2Dispatch\|WriteJSON\|NewManifest\|ParseManifestPayload' -v` → **all PASS** (13 + 9 + 1 + 3 + 2 = 28 test functions/subtests, zero failures) |
| Runtime harness command/scenario and exact result | N/A — no new HTTP endpoint exists yet in this slice (per tasks.md's own Unit 1 row: "no new endpoint yet; proven by unit/characterization suite only"). The Phase 0.2 dispatch test does exercise the real HTTP router end-to-end (`httptest` + `ServeHTTP`, not mocks), which is the closest runtime proof available at this boundary. |
| Rollback boundary | Revert `internal/domain/regixtry/manifest.go`, `internal/app/regixtry/service.go`'s parse threading, and the 21 call-site diffs (8 files); `internal/protocol/http/router_test.go`'s new characterization tests are additive-only and can be reverted independently. Router dispatch logic and schema are completely untouched by this PR. |

## Verification Evidence

```
$ go build ./...
(clean, exit 0)

$ go vet ./...
(clean, exit 0)

$ gofmt -l .
(clean, no output)

$ go test ./... 
ok  	regixtry/cmd/regixtry	4.357s
ok  	regixtry/internal/app/auth	(cached)
ok  	regixtry/internal/app/regixtry	6.020s
ok  	regixtry/internal/app/scanning	(cached)
ok  	regixtry/internal/domain/auth	(cached)
ok  	regixtry/internal/domain/regixtry	(cached)
ok  	regixtry/internal/domain/signing	(cached)
ok  	regixtry/internal/infra/auth/postgres	(cached)
ok  	regixtry/internal/infra/cliprogress	(cached)
ok  	regixtry/internal/infra/install/compose	(cached)
ok  	regixtry/internal/infra/install/linux	(cached)
ok  	regixtry/internal/infra/install/releases	(cached)
ok  	regixtry/internal/infra/metadata/sqlite	(cached)
ok  	regixtry/internal/infra/release	(cached)
ok  	regixtry/internal/infra/scanning/gitleaks	(cached)
ok  	regixtry/internal/infra/scanning/trivy	(cached)
ok  	regixtry/internal/infra/storage/fsblob	(cached)
ok  	regixtry/internal/ports	(cached)
ok  	regixtry/internal/protocol/http	3.398s
ok  	regixtry/internal/tui	(cached)
```

Line-count evidence (`git diff --stat feature/oci-referrers-api...HEAD`):

```
 internal/app/regixtry/service.go                   |  13 +-
 internal/app/regixtry/service_signing_bundle_test.go |   2 +-
 internal/app/regixtry/service_signing_test.go      |   4 +-
 internal/app/regixtry/service_test.go              |  42 +++
 internal/domain/regixtry/manifest.go               |  29 ++-
 internal/domain/regixtry/manifest_test.go          |  40 ++-
 internal/infra/metadata/sqlite/store.go            |   6 +-
 internal/infra/metadata/sqlite/store_test.go       |  24 +-
 internal/protocol/http/router_test.go              | 286 +++++++++++++++++++++
 internal/protocol/http/secret_scan_status_test.go  |   2 +-
 internal/protocol/http/signature_status_test.go    |   2 +-
 openspec/changes/oci-referrers-api/tasks.md        |  18 +-
 12 files changed, 424 insertions(+), 44 deletions(-)
```

Commits on this branch (3, each a coherent unit):

```
c38d404 test(referrers): characterize router dispatch before referrers route
354200d feat(referrers): thread ArtifactType through domain.Manifest
dc45787 feat(referrers): thread artifactType through parseManifestPayload
```

## Status

9/9 tasks in scope (0.1–2.3) complete. Ready for `sdd-verify`, and ready for
PR #2 (Phases 3–4, store schema/backfill) to branch from this leaf once
merged/reviewed.
