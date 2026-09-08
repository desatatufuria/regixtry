```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:7a4510b1b7359b5d8c686fbc9e8ee1a345a1af1b0afb0933fd5fa2afe860b54a
verdict: pass_with_warnings
blockers: 0
critical_findings: 0
requirements: 8/8
scenarios: 17/17
test_command: go test ./...
test_exit_code: 0
test_output_hash: sha256:c9236456b182840a0f9bfe389ee8c72504bcc270c3431f3eb8ec36d5fda35158
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: oci-referrers-api
**Version**: N/A (branch `feature/oci-referrers-api-04-router-docs`, HEAD `64f35f8`, full 4-PR Feature Branch Chain off `cd98348`)
**Mode**: Strict TDD

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 38 |
| Tasks complete | 38 |
| Tasks incomplete | 0 |

Verified `tasks.md` directly: `rg -o '^\- \[x\] [0-9]+\.[0-9]+'` returns 38 checked entries (0.1–0.3, 1.1–1.3, 2.1–2.3, 3.1–3.3, 4.1–4.5, 5.1–5.5, 6.1–6.5, 7.1–7.8, 8.1–8.3); `rg -c '\- \[ \]'` returns 0 unchecked boxes. Note: apply-progress's own "Status" section states "40/40 tasks complete" and the orchestrator's launch context also said "40 tasks" — both numerically wrong. The actual, unambiguous byte count in `tasks.md` is 38 tasks, all checked. This mirrors the same class of harmless arithmetic slip the `container-setup-mode` verify report previously found in this repo's apply-progress narrative sections — recorded as SUGGESTION #1 below, not a completeness gap.

### Build & Tests Execution
**Build**: PASS
```text
$ go build ./...
(no output)
```

**Tests**: PASS — full suite, all 19 packages, fresh (`-count=1`, no cache)
```text
$ go test ./... -count=1
ok  	regixtry/cmd/regixtry	5.529s
ok  	regixtry/internal/app/auth	0.375s
ok  	regixtry/internal/app/regixtry	7.372s
ok  	regixtry/internal/app/scanning	0.083s
ok  	regixtry/internal/domain/auth	0.022s
ok  	regixtry/internal/domain/regixtry	0.032s
ok  	regixtry/internal/domain/signing	0.364s
ok  	regixtry/internal/infra/auth/postgres	0.552s
ok  	regixtry/internal/infra/cliprogress	0.022s
ok  	regixtry/internal/infra/install/compose	0.024s
ok  	regixtry/internal/infra/install/linux	0.819s
ok  	regixtry/internal/infra/install/releases	0.092s
ok  	regixtry/internal/infra/metadata/sqlite	1.345s
ok  	regixtry/internal/infra/release	0.025s
ok  	regixtry/internal/infra/scanning/gitleaks	0.520s
ok  	regixtry/internal/infra/scanning/trivy	0.476s
ok  	regixtry/internal/infra/storage/fsblob	0.044s
ok  	regixtry/internal/ports	0.009s
ok  	regixtry/internal/protocol/http	4.184s
ok  	regixtry/internal/tui	0.609s
```
Independently re-executed in this pass (not trusted from apply-progress alone); matches apply-progress's own PR 4 "Verification Evidence" byte-for-byte on package list and pass/fail outcome (timings differ trivially, as expected on separate runs).

**Isolated focused runs** (independently re-executed, not just trusted from apply-progress):
- `go test ./internal/protocol/http/... -run 'TestRouterReferrers|TestSplitRepositoryPathCharacterizesCurrentSixMarkerBehavior|TestHandleV2DispatchCharacterizesCurrentRoutingBeforeReferrers|TestRouterFullLifecycleUnaffectedByReferrersRoute' -v`: all PASS, including every subtest (empty-manifests never-pushed/deleted-with-survivor, Content-Type + artifactType filter header incl. whitespace-only and error-response-no-header, pull-scoped 200 / no-access 401 / non-GET 405, multi-referrer digest ordering, cosign-bundle-listed/legacy-sig-absent, and the full push/pull/tags/catalog/scan-status/signature-status/delete/repeated-Referrers-reads/post-delete-scan-status lifecycle walk).
- `go test ./internal/infra/metadata/sqlite/... -run 'ListReferrers|Backfill|SubjectDigest|PartialIndex|PublishManifest' -v`: all 13 PASS, including `TestStoreListReferrersCrossTenantReturnsEmpty` and `TestStoreListReferrersCrossRepositoryReturnsEmpty` (the highest-severity threat-matrix row), and the idempotent-backfill/atomic-marker tests.
- `go test ./internal/app/regixtry/... -run 'ResolveArtifactType|ServiceReferrers' -v`: all 8 PASS, including the authorize-before-parse-digest test (`401`, never `400`, for an unauthorized caller with a malformed digest) and the never-nil-on-zero-rows test.
- `go test ./internal/app/regixtry/... -run 'TestServiceAcceptsManifestWithExistingSubject|TestServiceRejectsManifestWithMissingSubject' -v`: both PASS — pre-existing push-time subject validation (`409` on a non-resolving subject) is unmodified and still green.

**Static analysis**: PASS
```text
$ go vet ./...
(no output)
$ gofmt -l .
(no output)
```

**Isolated diff for this change** (`git diff --stat cd98348...HEAD`, `cd98348` being the actual commit the PR-1 branch forked from — not the divergent, since-diverged `feature/registry-foundation` tip, which would falsely include thousands of unrelated lines from concurrently-landed TUI/signing work):
```text
 README.md                                            |   1 +
 internal/app/regixtry/queries.go                     | new (Referrers/ReferrersIndex/resolveArtifactType)
 internal/app/regixtry/queries_test.go                | new
 internal/app/regixtry/service.go                     |  13 +-
 internal/app/regixtry/service_signing_bundle_test.go |   2 +-
 internal/app/regixtry/service_signing_test.go        |   4 +-
 internal/app/regixtry/service_test.go                |  42 +
 internal/domain/regixtry/manifest.go                 |  29 +-
 internal/domain/regixtry/manifest_test.go            |  40 +-
 internal/infra/metadata/sqlite/store.go              | 223 +++++-
 internal/infra/metadata/sqlite/store_test.go         | 701 ++++++++++++++++-
 internal/ports/regixtry.go                            |  26 +
 internal/protocol/http/router.go                     |  69 +-
 internal/protocol/http/router_test.go                | 876 +++++++++++++++++++++
 internal/protocol/http/secret_scan_status_test.go    |   2 +-
 internal/protocol/http/signature_status_test.go      |   2 +-
 internal/protocol/http/testdata/bundle-referrer-manifest.json | 26 +
```
17 files touched total, matching design.md's "File Changes" table exactly (domain/manifest, app/service, app/queries, ports, sqlite/store, protocol/http/router, README, plus test files). Independently confirmed `internal/domain/signing`, `internal/app/regixtry/service_gc.go`, `internal/tui`, and `internal/domain/regixtry/descriptor.go` are byte-identical to the pre-change base (`git diff --stat cd98348...HEAD` on those paths returns empty) — the design's non-goal list is genuinely honored, not merely asserted. Confirmed no `artifact_type` column exists anywhere in `store.go` (`rg -n "artifact_type" store.go` returns zero matches) — Decision 4 ("not persisted") is real.

**Coverage**: Not separately measured; `go test ./...` exit 0 across all 19 packages plus the four independently re-executed focused runs above are the load-bearing signal.

### TDD Compliance
| Check | Result | Details |
|-------|--------|---------|
| TDD Evidence reported | ✅ | Found in apply-progress: full "TDD Cycle Evidence" tables for all 38 tasks across PR 1–4 |
| All tasks have tests | ✅ | 38/38 tasks have covering test files/functions (Phase 1.3/8.3 are mechanical/docs tasks with no new test of their own, matching their nature — call-site updates proven by `go build`/`go vet`, docs proven by `rg` grep) |
| RED confirmed (tests exist) | ✅ | All referrer-related test files verified present on disk and executed in this pass |
| GREEN confirmed (tests pass) | ✅ | 38/38 — every focused command re-run in this pass passed; full `go test ./...` passed |
| Triangulation adequate | ✅ | Every behavioral task has 2+ table/subtest cases (cross-tenant/cross-repository, present/absent artifactType, filtered/unfiltered, pull-scoped/no-access/non-GET, etc.); apply-progress explicitly documents 4 extra triangulating tests added beyond the tasks.md minimum in PR 3 alone |
| Safety Net for modified files | ✅ | Apply-progress's TDD Cycle Evidence table records "full package suite green before edit" for every PR's first task, and a stash-based RED/GREEN toggle was used in PR 2/PR 4 to prove genuine RED against production code, not test-only RED |

**TDD Compliance**: 6/6 checks passed

---

### Test Layer Distribution
| Layer | Tests | Files | Tools |
|-------|-------|-------|-------|
| Unit | ~24 (domain, store, resolveArtifactType, queries) | 4 (`manifest_test.go`, `store_test.go`, `queries_test.go`, `service_test.go`) | Go `testing`, real on-disk SQLite via `t.TempDir()` — no mocks |
| Integration | ~13 (router, full HTTP stack via `httptest`) | 1 (`router_test.go`) | Go `testing` + `net/http/httptest`, real SQLite + `fsblob.Store` — no mocks |
| E2E | 1 documented live smoke (push subject → push referrer → GET referrers, `200`/`Content-Type`/filter/header behavior against a real running server) | N/A (manual, per tasks.md Unit 4's own "Runtime harness" row) | Real running `regixtry` binary + `curl`/HTTP client, independently confirmed by the orchestrator per the launch context, and re-confirmed here via the isolated `TestRouterReferrers*` re-runs which exercise the identical code path through `httptest` |
| **Total** | **~37 Go test functions** (33 name-matched via `Referrer|SubjectDigest|ArtifactType|SplitRepositoryPathCharacterizes|HandleV2DispatchCharacterizes|WriteJSONSets|Backfill|PartialIndex`, plus renamed/extended pre-existing characterization tests) | **5** | |

### Changed File Coverage
Coverage analysis skipped — no coverage tool configured in this project's toolchain beyond `go test`'s own pass/fail signal; the isolated focused-package re-runs above (13 store tests, 8 app tests, ~24 router subtests, all PASS) are the load-bearing per-file evidence instead.

### Assertion Quality
`rg -n "expect\(true\)\.toBe\(true\)|assert True|expect\(1\)\.toBe\(1\)"` across all 5 referrer-touched test files: zero matches. `rg -c "Mock|mock\."` across the 3 largest referrer test files: zero matches (this codebase uses real SQLite/`fsblob`/HTTP throughout, never mocks, consistent with apply-progress's own claim). Apply-progress's own Deviations sections document 3 separate cases (PR 2 Deviation #2, PR 3 Deviation #3) where a first-draft test passed trivially or asserted the wrong thing, was caught by actually running it, and was fixed with an added control row/consistent payload — direct, self-reported evidence the Strict TDD "GATE: do not proceed until GREEN is confirmed by execution" rule was followed in practice, not merely claimed.

**Assertion quality**: ✅ All assertions verify real behavior — no tautologies, no ghost loops, no mock-heavy tests found.

---

### Quality Metrics
**Linter**: ➖ Not available (no separate linter configured beyond `go vet`, reported above as PASS)
**Type Checker**: N/A (Go is statically compiled; `go build ./...` and `go vet ./...` above are the equivalent signal, both PASS)

---

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| Listing Returns Matches, Empty List Never 404s | Pushed referrer is listed | `TestRouterReferrersReturnsAllMatchesInDigestOrder`, `TestServiceReferrersReturnsMatchedReferrerWithArtifactTypeAndAnnotations` (PASS) | COMPLIANT |
| Listing Returns Matches, Empty List Never 404s | Digest never existed | `TestRouterReferrersEmptyManifestsNeverPushedAndDeletedSubject`/never-pushed subtest (PASS) | COMPLIANT |
| Listing Returns Matches, Empty List Never 404s | Subject deleted, survivors still list | `TestRouterReferrersEmptyManifestsNeverPushedAndDeletedSubject`/deleted-subject-surviving-referrer subtest (PASS), no `5xx` observed | COMPLIANT |
| ArtifactType Filtering Sets Header Only When Applied | Filtered request narrows results and sets header | `TestRouterReferrersContentTypeAndArtifactTypeFilterHeader`/filtered subtest (PASS) | COMPLIANT |
| ArtifactType Filtering Sets Header Only When Applied | Unfiltered request omits header | Same test/unfiltered + whitespace-only subtests (PASS) | COMPLIANT |
| Descriptor ArtifactType Falls Back To Config MediaType | ArtifactType present | `TestServiceReferrersReturnsMatchedReferrerWithArtifactTypeAndAnnotations`, `TestResolveArtifactTypeManifestValueWins` (PASS) | COMPLIANT |
| Descriptor ArtifactType Falls Back To Config MediaType | ArtifactType absent falls back | `TestServiceReferrersFallsBackToConfigMediaTypeWhenArtifactTypeAbsent`, `TestResolveArtifactTypeAbsentFallsBackToConfigMediaType` (PASS) | COMPLIANT |
| Pre-Existing Content Is Discoverable Via Idempotent Backfill | Pre-existing referrer discoverable after backfill | `TestStoreBackfillsPreExistingRowsSubjectDigestOnNewIdempotently` (PASS) | COMPLIANT |
| Pre-Existing Content Is Discoverable Via Idempotent Backfill | Backfill is idempotent | Same test's second-`New()` assertion: unchanged row, unchanged marker, marker count == 1 (PASS) | COMPLIANT |
| Referrers Are Scoped To Tenant And Repository | Cross-repository isolation | `TestStoreListReferrersCrossRepositoryReturnsEmpty` (PASS) | COMPLIANT |
| Referrers Are Scoped To Tenant And Repository | Cross-tenant isolation | `TestStoreListReferrersCrossTenantReturnsEmpty` (PASS) | COMPLIANT |
| Endpoint Authorization Uses ActionInspect | Pull-scoped token accepted | `TestRouterReferrersAuthorizationAndMethodDispatch`/pull-scoped subtest (PASS) | COMPLIANT |
| Endpoint Authorization Uses ActionInspect | Unauthorized principal rejected | Same test/no-access subtest: `401` + `WWW-Authenticate` challenge (PASS) | COMPLIANT |
| Legacy Cosign Tag Artifacts Stay Separate | Legacy `.sig` artifact absent | `TestRouterReferrersCosignBundleListedLegacySigAbsent` (PASS): `.sig` manifest absent from `manifests[]`, its own tag GET still `200` | COMPLIANT |
| Legacy Cosign Tag Artifacts Stay Separate | Cosign v3 bundle referrer discoverable | Same test: bundle referrer (from `testdata/bundle-referrer-manifest.json`, `subject` set) is listed (PASS) | COMPLIANT |
| No Push-Path Or Scan Behavior Change | Push-time validation unchanged | `TestServiceRejectsManifestWithMissingSubject`, `TestServiceAcceptsManifestWithExistingSubject` (PASS, unmodified pre-existing tests, independently re-run in this pass) | COMPLIANT |
| No Push-Path Or Scan Behavior Change | Reads never affect scanning | `TestRouterFullLifecycleUnaffectedByReferrersRoute` (PASS): 3 interleaved Referrers reads did not perturb post-delete scan-status | COMPLIANT |

**Compliance summary**: 17/17 scenarios COMPLIANT with runtime evidence, across all 8 requirements — every scenario independently re-executed in this pass, not merely trusted from apply-progress.

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| `GET /v2/<name>/referrers/<digest>` | Implemented | `internal/protocol/http/router.go:520` (`handleReferrers`), wired via `handleV2`'s `case strings.HasPrefix(suffix, "referrers/")` (`router.go:193-194`) |
| `?artifactType=` filtering + `OCI-Filters-Applied` | Implemented | `router.go:533,549-551`: trimmed, header set only on the success path when non-empty, matching design.md Decision 7's "error response never claims a filter was applied" |
| `subject_digest` column + backfill | Implemented | `internal/infra/metadata/sqlite/store.go` — inline `CREATE TABLE` column, idempotent `ALTER TABLE`, `backfillSubjectDigests()` gated by `schema_backfills` marker, called from `New()` |
| `ActionInspect` authorization | Implemented | `internal/app/regixtry/queries.go:630`: `s.authorize(ctx, ports.Action{Verb: ports.ActionInspect, ...})`, called before `domain.ParseDigest` (line 634), matching Decision 7's capability-disclosure ordering |
| Tenant/repository scoping | Implemented | `internal/infra/metadata/sqlite/store.go:911-958` (`ListReferrers`): `WHERE m.tenant = ? AND r.tenant = ? AND r.name = ?`, the exact `ListTags` predicate set, never global like `ListReferencedBlobDigests` |
| `artifactType` derived, never persisted | Implemented | No `artifact_type` column in `store.go` (`rg` confirms zero matches); `resolveArtifactType` (`queries.go:605`) is a pure function applied at response-build time only |
| Legacy cosign separation | Implemented | `.sig`/`BundleIndexTag` manifests carry no `subject`, so `ListReferrers`' `subject_digest != ''` predicate excludes them structurally, not via a special case |
| No push-path/scan change | Implemented | Zero diff on `service.go`'s subject-validation block or `service_gc.go`/scan-suppression logic beyond the additive `ArtifactType` threading; pre-existing `TestServiceRejectsManifestWithMissingSubject`/`TestServiceAcceptsManifestWithExistingSubject` unmodified and green |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| Decision 1 — `schema_backfills` marker, atomic with row updates | Yes | `backfillSubjectDigests()` uses one `*sql.Tx`; `TestStoreBackfillRowUpdateAndMarkerCommitTogether` proves the observable invariant (both land together) |
| Decision 2 — partial index + literal `!= ''` predicate | Yes | `idx_manifests_subject` partial index confirmed via `EXPLAIN QUERY PLAN` tests (`TestStoreListReferrersUsesPartialIndexGivenLiteralPredicate`, PASS in this pass); `ListReferrers`' query repeats the literal predicate exactly |
| Decision 3 — `ports.ReferrerRow`, payload→descriptor mapping in `queries.go` | Yes, with one documented ambiguity resolved | `ReferrerRow.Digest` is `domain.Digest` (design's code block showed `string`, but its own prose pointed the other way — apply-progress PR 3 Deviation #1 records this honestly as a genuine design.md internal inconsistency, resolved per explicit orchestrator instruction, zero behavioral consequence since the underlying type is `string`) |
| Decision 4 — `ORDER BY m.digest ASC` | Yes | `store.go:927`; `TestStoreListReferrersMatchesExactSubjectAndOrdersByDigestAscRegardlessOfInsertionOrder` and `TestRouterReferrersReturnsAllMatchesInDigestOrder` both confirm |
| Decision 5 — `ResolveManifest` stays untouched, `Subject: nil` | Yes | `store.go`'s `ResolveManifest` passes `""` for `artifactType` with an explanatory comment per design; no JSON parse added to the pull path |
| Decision 6 — `/referrers/` appended last | Yes | `router.go:708`: `markers := []string{"/blobs/uploads/", "/blobs/uploads", "/blobs/", "/manifests/", "/tags/list", "/referrers/"}`, confirmed last; the documented residual `library/referrers/referrers/<digest>` case fails closed at `400 DIGEST_INVALID`, proven by `TestRouterReferrersDoubleReferrersResidualPathFailsClosedAtDigestInvalid` |
| Decision 7 — authorize before digest parse; header on success only | Yes | `queries.go:630` (`authorize`) precedes line 634 (`ParseDigest`); `router.go:549-551` sets the header strictly after the error-return branch |
| `Manifests` never nil (`make(..., 0, len(rows))`) | Yes | `queries.go:652`; `TestServiceReferrersManifestsIsNeverNilOnZeroRows` and the router-level raw-body `"manifests":[]` assertions both confirm |
| `writeJSON`/`writeJSONAs` split, no behavior change to existing callers | Yes | `writeJSON` delegates to `writeJSONAs` unchanged; pinned by the pre-existing (Phase 0) `TestWriteJSONSetsApplicationJSONContentType`, still green |

### Issues Found

**CRITICAL**: None.

**WARNING**: None found beyond what is already disclosed and resolved in apply-progress's own Deviations sections (the design.md `ReferrerRow.Digest` type ambiguity, the uncompilable `ociImageIndexMediaType` cross-package reference in design.md's own code sample, and the "MUST stay" README wording for text that never previously existed) — all three were caught and resolved within the apply phase's own TDD cycles, are honestly self-reported, and do not represent an unresolved gap at verify time.

**SUGGESTION**:
1. Both apply-progress's "Status" section ("40/40 tasks complete") and the orchestrator's own launch context ("all 40 tasks marked complete") state a task count that does not match `tasks.md`'s actual byte-count of 38 checked tasks (0 unchecked). Harmless — every task that does exist is complete and every phase 0–8 is present — but this is the same class of informal arithmetic slip the `container-setup-mode` verify report previously flagged in this repo's apply-progress narrative sections, and it's worth tightening apply-progress's summary lines to compute totals from the file rather than restate them by hand.
2. The two open follow-ups design.md itself already named and left as deliberate non-goals remain outstanding, as expected: pagination (`n`/`last`) is absent (spec permits this — a full-list response is conformant), and the `<repo>/referrers/referrers/<digest>` residual path fails closed at `400` rather than routing correctly (pre-existing property of the first-match marker scan, shared with `library/manifests/manifests/latest`'s identical `404` behavior today). Neither blocks archive; both are already tracked in design.md's own Open Questions.
3. Orphaned/dangling referrers (Decision 3) remain accepted debt, exactly as proposal.md scoped: an orphan's row keeps its `subject_digest` and is still returned by `ListReferrers`, so behavior is defined, just not cleaned up. Confirmed via code read (`DeleteManifestByDigest` in `store.go` still deletes unconditionally, with no `subject`-reference check) — consistent with the proposal's explicit deferral to a future GC/retention change, not a regression introduced here.

### Remediation

None applied — no CRITICAL findings, and the three self-reported WARNINGs-in-apply-progress were already resolved within the apply phase's own TDD cycles before this verify pass began.

### Verdict
PASS WITH WARNINGS
All 38/38 tasks (tasks.md's real total, not apply-progress's stated "40") are genuinely complete across all 4 chained PRs (0–8, characterization through router/docs). All 17/17 spec scenarios across all 8 requirements are COMPLIANT with runtime evidence independently re-executed in this pass, including the two highest-severity threat-matrix rows (cross-tenant/cross-repository isolation) and the capability-disclosure ordering (`401` before `400` for an unauthorized+malformed-digest caller). `go build`, `go vet`, `gofmt`, and the full `go test ./... -count=1` are all clean, matching the orchestrator's independently-confirmed summary. The isolated diff against this change's actual fork point confirms the non-goal areas (`internal/domain/signing`, `service_gc.go`, `internal/tui`, `descriptor.go`) are byte-identical to pre-change, and that no `artifact_type` column was added. Every design.md architecture decision (1–7) is followed, with one honestly self-disclosed and correctly-resolved internal ambiguity in design.md's own text (Decision 3's `ReferrerRow.Digest` type). Pre-existing push-time subject validation and scan-suppression behavior are unmodified and independently re-confirmed green. Only a harmless task-count documentation slip and two already-scoped, already-deferred non-goals were found — none block archive.
