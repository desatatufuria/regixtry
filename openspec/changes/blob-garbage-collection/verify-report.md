```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:2ae7b9deaa2da4c6c0949a4d56ae8e2367f8e4cb44597666345d5083bcebf783
verdict: pass
blockers: 0
critical_findings: 0
requirements: 11/11
scenarios: 19/19
test_command: go build ./... && go vet ./... && gofmt -l . && go test -count=1 ./...
test_exit_code: 0
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report (RE-VERIFICATION)

**Change**: blob-garbage-collection
**Version**: N/A (v1)
**Mode**: Strict TDD
**Branch**: feature/blob-garbage-collection (uncommitted working tree; HEAD 1630a94)
**Supersedes**: this change's prior FAIL verdict (Engram id 1074, same topic key, upserted in place)

### What changed since the prior verify pass

The prior verify pass found exactly ONE CRITICAL: spec.md's "Delete without a report reference
is rejected" scenario (Requirement 7) had zero covering test anywhere in the tree. A targeted,
test-only continuation batch (apply-progress topic, same key, revision 3) closed this gap by
adding two tests:

- `internal/app/regixtry/service_gc_test.go > TestDeleteByGCReportRejectsEmptyReportID` — calls
  `service.DeleteByGCReport(ctx, "")` with `SetGCDeleteEnabled(true)`, asserts
  `domain.ErrorCodeNotFound`. Read the test body directly (not the apply agent's description):
  it exercises the real service method with a genuinely empty report id and the flag genuinely
  ON, which is the literal GIVEN/WHEN of the spec scenario ("a delete request omits a report
  id"). Traced the call path: `DeleteByGCReport` (service_gc.go:157) checks the flag first, then
  calls `s.metadata.GetGCReport(ctx, "")` (line 162) before any candidate recomputation or
  `DeleteBlob` call is reachable. The sqlite store's `GetGCReport` (store.go:482-507) runs
  `SELECT ... FROM gc_reports WHERE id = ?` with `reportID=""`, which matches zero rows
  (`gc_reports.id` is a UUID primary key, never empty), so `sql.ErrNoRows` maps to
  `domain.NewNotFoundError`. This is a real, non-mocked, end-to-end proof: no blob-deletion code
  is even reachable when this error returns, structurally guaranteeing "rejected without
  deleting anything." Ran this test in isolation: `PASS` (0.11s).
- `internal/protocol/http/admin_gc_test.go > TestAdminGCDeleteRejectsUnknownReportID` — real
  router + real sqlite + real fsblob, `POST /admin/v1/gc/reports/00000000-.../delete` with an
  admin principal and the flag ON, asserts HTTP 404. Read the test body directly: it is a
  genuine end-to-end HTTP request through `handler.ServeHTTP`, not a unit-level shortcut. This
  covers the sibling case of a well-formed but never-persisted report id, reinforcing the same
  requirement from the HTTP layer. Ran this test in isolation: `PASS` (0.10s, confirmed via
  request log `status=404`).

Both tests were re-run individually (not just as part of the full suite) during this
re-verification and both pass. Both exercise the real production code path described above —
neither is a tautology or a mock-only check.

`tasks.md` now shows 79/79 tasks `[x]` (task 9.3 added to document this fix, verified via
`rg -c '^\- \[[ x]\]' tasks.md` = 79, zero `- [ ]` remain). Zero production code changed in this
batch — this is a test-only continuation, matching the prior verify report's own recommended
fix exactly ("add the one missing test ... rather than returning to sdd-apply for a
production-code fix").

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 79 |
| Tasks complete | 79 |
| Tasks incomplete | 0 |

### Build & Tests Execution (re-run this pass)
**Build**: PASSED
```text
go build ./...   → exit 0, empty output
go vet ./...     → exit 0, empty output
gofmt -l .       → exit 0, empty output (no unformatted files)
```

**Tests**: 18/18 packages PASSED, 0 failed, 0 skipped
```text
go test -count=1 ./...
ok  	regixtry/cmd/regixtry	4.915s
ok  	regixtry/internal/app/auth	0.301s
ok  	regixtry/internal/app/regixtry	6.128s
ok  	regixtry/internal/app/scanning	0.059s
ok  	regixtry/internal/domain/auth	0.013s
ok  	regixtry/internal/domain/regixtry	0.019s
ok  	regixtry/internal/domain/signing	0.257s
ok  	regixtry/internal/infra/auth/postgres	0.520s
ok  	regixtry/internal/infra/cliprogress	0.021s
ok  	regixtry/internal/infra/install/linux	0.921s
ok  	regixtry/internal/infra/install/releases	0.097s
ok  	regixtry/internal/infra/metadata/sqlite	1.112s
ok  	regixtry/internal/infra/release	0.146s
ok  	regixtry/internal/infra/scanning/gitleaks	0.495s
ok  	regixtry/internal/infra/scanning/trivy	0.411s
ok  	regixtry/internal/infra/storage/fsblob	0.125s
ok  	regixtry/internal/ports	0.022s
ok  	regixtry/internal/protocol/http	3.316s
ok  	regixtry/internal/tui	0.671s
```

### Spec Compliance Matrix (re-run, all 11 requirements / 19 scenarios)

| # | Requirement | Scenario | Test | Result |
|---|---|---|---|---|
| 1 | Global Non-Tenant-Scoped Mark Set | Blob shared across tenants stays protected | `TestListReferencedBlobDigestsIsGlobalAcrossTenants` + `TestGCRespectsBlobsReferencedOnlyByAnotherTenant` | COMPLIANT |
| 1 | Global Non-Tenant-Scoped Mark Set | Report is usable from a different tenant context | `TestGCReportIsReadableAndUsableFromAnotherTenantContext` | COMPLIANT |
| 2 | Grace-Period Protection | Recently written blob is protected | `TestComputeGCReportExcludesBlobInsideGraceWindow` (t=0 half) | COMPLIANT |
| 2 | Grace-Period Protection | Aged unreferenced blob is a candidate | `TestComputeGCReportExcludesBlobInsideGraceWindow` (t+25h half) | COMPLIANT |
| 3 | Uploads Directory Exclusion | Uploads tree is never touched | `TestListBlobsNeverEnumeratesUploads` + `TestDeleteBlobNeverTouchesUploads` | COMPLIANT |
| 4 | Report Computation and Persistence | Report is persisted before response | `TestCreateGCReportPersistsReportAndCandidatesInOneTransaction` + `TestAdminPostGCReportsReturns201WithDetail` | COMPLIANT |
| 4 | Report Computation and Persistence | Report path ignores the delete flag | Every report test runs and passes with `gcDeleteEnabled` at its default-false zero value (collective proof, re-confirmed unchanged) | COMPLIANT |
| 5 | Report Retrieval | Unknown report id | `TestGetGCReportReturnsNotFoundForUnknownID` + `TestAdminGetGCReportResourceReturnsDetailOrNotFound` | COMPLIANT |
| 6 | Report Expiry and Pruning | Expired preview report is pruned on next report | `TestPruneExpiredGCReportsRemovesExpiredReportedButKeepsDeleted` | COMPLIANT |
| 6 | Report Expiry and Pruning | Deleted-state report survives pruning | same test, `expired-deleted` row assertion | COMPLIANT |
| 7 | Delete Requires a Valid Prior Report | Delete against an expired or already-deleted report is rejected | `TestDeleteByGCReportRejectsAlreadyUsedReport` + `TestDeleteByGCReportRejectsExpiredReport` | COMPLIANT |
| 7 | Delete Requires a Valid Prior Report | Delete without a report reference is rejected | **NEWLY CLOSED**: `service_gc_test.go > TestDeleteByGCReportRejectsEmptyReportID` (`DeleteByGCReport(ctx, "")`, flag ON, asserts `ErrorCodeNotFound`) + `admin_gc_test.go > TestAdminGCDeleteRejectsUnknownReportID` (HTTP 404 for a fabricated report id, flag ON). Both read directly and re-run individually this pass; both PASS. | COMPLIANT |
| 8 | Recompute-and-Intersect at Delete Time | Digest referenced after report is not deleted | `TestDeleteByGCReportNeverUnlinksBlobReferencedByAManifest` | COMPLIANT |
| 8 | Recompute-and-Intersect at Delete Time | Still-unreferenced digest is deleted | `TestStillUnreferencedDigestIsDeletedAtDeleteTime` | COMPLIANT |
| 9 | Delete Gated by a Distinct Flag | Unauthorized caller rejected before flag check | `TestAdminGCDeleteUnauthorizedRejectedBeforeFlagCheck` | COMPLIANT |
| 9 | Delete Gated by a Distinct Flag | Authorized caller blocked by disabled flag | `TestDeleteByGCReportRefusesWhenFlagOff` + `TestAdminGCDeleteReturns501NamingEnvVar` | COMPLIANT |
| 10 | Audit Trail on Report Transition | Terminal row records audit fields | `TestDeleteByGCReportRecordsFullAuditFieldSetOnCompletion` | COMPLIANT |
| 10 | Audit Trail on Report Transition | Partial failure is recorded per digest, not all-or-nothing | `TestDeleteByGCReportRecordsPerDigestFailureAndPartialReclaim` | COMPLIANT |
| 11 | Out of Scope for v1 | No unattended deletion occurs | `TestNoUnattendedDeletionOccursWithoutExplicitDeleteRequest` | COMPLIANT |

**Compliance summary**: 19/19 scenarios compliant. 11/11 requirements fully compliant. No
regression found anywhere else — the 18 previously-compliant scenarios remain compliant and
this was the only previously-open item, now closed.

### Incidental finding — sanity check (dead defensive branch, not a bug)

The gap-closing batch's investigation found that `admin_handlers.go`'s `id == ""` branch inside
`handleAdminGCReportResource` (lines 665-669) is unreachable in practice through real HTTP
traffic. Independently re-verified this claim during this re-verification pass rather than
trusting the description:

- Confirmed the router is built on `stdhttp.NewServeMux()` (`internal/protocol/http/router.go:38`),
  i.e. the standard library's `net/http.ServeMux`.
- Read `net/http`'s own source (`server.go`, Go 1.26.4 toolchain in this environment): path
  cleaning redirects for non-canonical paths (including collapsing `//`) use
  `RedirectHandler(u.String(), StatusTemporaryRedirect)` — a 307, issued entirely inside
  `net/http` before this application's `handleAdmin`/`handleAdminGCReportResource` code ever
  runs.
- Empirically reproduced this with a minimal standalone `http.ServeMux` test
  (`POST /admin/v1/gc/reports//delete` against a mux with an `/admin/v1/` handler): confirmed
  `307` with `Location: /admin/v1/gc/reports/delete`, i.e. the double slash is collapsed and the
  request redirected before any handler logic runs.
- Conclusion matches the batch's own finding exactly: the `id == ""` branch is genuinely
  reachable in source (a request that arrived with an already-empty `id` segment, e.g. via a
  non-mux-normalized path, would hit it and correctly return 404), but no real client — browser,
  `curl`, this project's own `httptest` harness, or any other — can present that exact path to
  the handler through this router's current wiring without manually following/suppressing the
  307 first. This is accurately described as dead code under real traffic, not a bug: it has no
  route-security implication (the path it would have handled is instead redirected to a
  differently-shaped path that itself resolves to a 404 via normal route-miss handling, since
  `/admin/v1/gc/reports/delete` — no id segment — does not match `handleAdminGCReportResource`'s
  expected `gc/reports/{id}[/{action}]` shape either). No further action needed; noting it here
  for the record, matching the apply-progress documentation.

### Deep Verification carried forward (unchanged from prior pass, re-confirmed no regression)

All prior-pass source-level confirmations (D6 global mark query, D5 recompute-and-intersect, D1a/D9
flag-off 501 + report-path independence, D3 non-configurable grace constant, D7 SQL-level
single-use enforcement, design.md drift check) were re-checked for continued validity this pass
via `go build`/`go vet`/full test suite re-run and spot re-reads of `service_gc.go`,
`admin_handlers.go`, and `sqlite/store.go`. No drift, no regression. Full detail preserved in the
superseded prior report (Engram id 1074 / git history of this file).

### Issues Found

**CRITICAL**: None. The prior pass's sole CRITICAL (Requirement 7's "Delete without a report
reference is rejected" scenario) is now closed by two genuine, individually re-run passing
tests as detailed above.

**WARNING**: None.

**SUGGESTION** (carried forward, unchanged, still non-blocking):
1. Consider a dedicated test asserting `ComputeGCReport` runs successfully with
   `SetGCDeleteEnabled(true)` set as well, for symmetry with the flag-off direction already
   covered.
2. `apply-progress`'s TDD Cycle Evidence table still only covers the final continuation batches
   per revision, not the full Phase 1-11 history in one table — same topic-key-upsert limitation
   previously flagged in `manifest-blob-delete`'s verify-report. Non-blocking; tasks.md's inline
   notes remain sufficient.

### TDD Compliance
| Check | Result | Details |
|-------|--------|---------|
| TDD Evidence reported | Yes | Gap-closing batch documents Safety Net (12/12 and 6/6 pre-existing GC tests green before adding), RED discovery (HTTP-level double-slash attempt genuinely 307'd before correct re-scoping to service layer), GREEN (both new tests pass) |
| All tasks have tests | Yes | 79/79 tasks complete |
| RED confirmed (tests exist) | Yes | Carried forward from prior pass; this batch's RED was a genuine layer-choice discovery (net/http mux redirect), documented in the test's own doc comment |
| GREEN confirmed (tests pass) | Yes | Full suite green at this re-verification: 18/18 packages; both new tests independently re-run and pass in isolation |
| Triangulation adequate | Yes | Two tests at two layers (service + HTTP) triangulate the same spec scenario |
| Safety Net for modified files | Yes | Documented pre-existing-test-green checks before each new test addition |

**TDD Compliance**: 6/6 checks passed.

### Assertion Quality
Both new tests assert specific, meaningful outcomes (`domain.ErrorCodeNotFound` via
`domain.IsCode`; HTTP status `404` via `recorder.Code`), not presence-only or tautological
checks. No regressions found in previously-reviewed GC test files.

**Assertion quality**: All assertions verify real behavior.

### Verdict
PASS — 0 CRITICAL, 0 WARNING, 2 SUGGESTION (non-blocking, carried forward). Task completion is
79/79. Build/vet/gofmt/full-suite are all green (18/18 packages, `go test -count=1 ./...` exit
0). All 11 requirements / 19 scenarios are now COMPLIANT with genuine, individually re-run
passing tests — the prior pass's sole CRITICAL (Requirement 7's untested "omits a report id"
scenario) is closed by `TestDeleteByGCReportRejectsEmptyReportID` (service layer) and
`TestAdminGCDeleteRejectsUnknownReportID` (HTTP layer), both read and re-run directly during
this re-verification, not trusted from the apply agent's description. The incidental
`admin_handlers.go` `id == ""` dead-branch finding was independently re-verified (source read +
empirical `net/http.ServeMux` reproduction) and confirmed accurate: not a bug, no
route-security implication. Recommended next step: proceed to `sdd-archive`.
