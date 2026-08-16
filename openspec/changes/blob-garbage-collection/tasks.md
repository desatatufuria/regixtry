# Tasks: Blob Garbage Collection — Persisted Report + Gated Delete v1 (blob-garbage-collection)

## Mandatory Ordering Constraint (design.md Testing Strategy — safety-critical tests)

T1 (`TestDeleteByGCReportNeverUnlinksBlobReferencedByAManifest`), T2's delete-survival
half (`TestGCRespectsBlobsReferencedOnlyByAnotherTenantAtDeleteTime`), and T6's delete
half (`TestDeleteBlobNeverTouchesUploads`) **MUST be written and confirmed RED before
any unlink-capable code exists anywhere in the tree.** This is enforced structurally,
not by intention, via a hard phase split:

- **Phase 7 (RED gate)** adds `ports.BlobStore.DeleteBlob`'s signature and a
  `fsblob.Store.DeleteBlob` **stub** that validates the digest and then
  unconditionally returns `(false, errors.New("gc: blob unlink not yet implemented"))`
  — no `os.Remove`/`os.RemoveAll` call exists in the file at this point. The full
  `DeleteByGCReport` orchestration (recompute + intersect + outcome bookkeeping) is
  wired against this stub. T1, T2's delete half, and T6's delete half are written here
  and confirmed RED for the honest reason that the stub errors on every unlink
  attempt — never a compile error, never a naive-but-dangerous placeholder.
  Task 7.8 is a literal grep gate: `rg 'os\.Remove' internal/infra/storage/fsblob/store.go`
  MUST return zero matches before Phase 7 is considered done.
- **Phase 8 (GREEN)** is the only phase permitted to replace that stub body with a
  real `os.Remove`. It cannot start until Phase 7's grep gate and RED confirmations
  are recorded in this file.

No other task in Phases 1–6 introduces, references, or requires an unlink-capable
code path — Phases 1–5 (Slice 1) contain zero `DeleteBlob` code at all.

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~1,100–1,500 (Slice 1 ~600–750, Slice 2 ~450–650) |
| 400-line budget risk | High (session-cached review budget is **800**, not the skill default 400) |
| Chained PRs recommended | Yes |
| Suggested split | 2 units — Unit 1 = Phases 1–5 (report/enumeration/persistence, zero unlink code); Unit 2 = Phases 6–11 (delete/flag/audit/expiry) |
| Delivery strategy | single-pr |
| Chain strategy | pending |

**Rationale**: 2 port methods + 5 store methods + 6 new types, 2 new tables with
transactional CRUD, 4 service methods, 2 HTTP handlers + a `writeAdminError` case, a
new domain error code, `main.go` flag wiring, docs, and 12+ negative-heavy test groups
(design's own T1–T12 plus the two deliberate additions below) land consistently in the
1,100–1,500 range both proposal.md and design.md independently estimated — well above
this session's 800-line budget even before the mandatory RED-gate split is counted as
extra structural overhead. Unit 1 has **zero unlink code**, matching design's own
slice boundary and making it the safe, independently-mergeable, non-destructive half.

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Report/enumeration/persistence, zero unlink code (Phases 1–5) | PR 1 | `go test ./internal/infra/storage/fsblob/... ./internal/infra/metadata/sqlite/... ./internal/app/regixtry/... ./internal/protocol/http/... -run 'ListBlobs\|ListReferencedBlobDigests\|GCReport\|ComputeGCReport' -v` | Manual: `curl -u admin -X POST https://<host>/admin/v1/gc/reports` against a running instance seeded with an unreferenced grace-expired blob; confirm `201` lists it as a candidate and the file is still on disk | Revert `ports.BlobStore.ListBlobs`/`MetadataStore` GC additions, `fsblob.Store.ListBlobs`, both `CREATE TABLE IF NOT EXISTS`, `service_gc.go`'s report half, the 2 admin GC routes; additive-only, nothing destructive to unwind |
| 2 | Delete/flag/audit/expiry (Phases 6–11) | PR 2 | `go test ./internal/infra/storage/fsblob/... ./internal/infra/metadata/sqlite/... ./internal/app/regixtry/... ./internal/protocol/http/... ./cmd/regixtry/... -run 'DeleteBlob\|DeleteByGCReport\|MarkGCReportDeleted\|GCDelete' -v` | Manual: `REGISTRY_GC_DELETE_ENABLED=true` then `curl -X POST https://<host>/admin/v1/gc/reports/{id}/delete` against a running instance with a known grace-expired unreferenced blob; confirm `200`, terminal report, and the blob file actually removed from disk | Revert `fsblob.DeleteBlob`, `MarkGCReportDeleted`'s outcome UPDATE + index decision, `DeleteByGCReport`'s real branch, the delete route, `ErrorCodeUnsupported`+501 mapping, `main.go` flag wiring; flag defaults `false` so a reverted binary answers `501` again; Slice 1's report path is untouched |

## Phase 1: Sweep — BlobStore Enumeration (`fsblob`) — Slice 1

- [x] 1.1 GREEN (compile prerequisite) `internal/ports/regixtry.go`: add `BlobFileInfo`
      struct + `ListBlobs(ctx) ([]BlobFileInfo, error)` to `BlobStore`, exact doc
      comment from design (blocking-language uploads/ exclusion note).
- [x] 1.2 GREEN (compile prerequisite) `internal/infra/scanning/gitleaks/stage_test.go`:
      add a `ListBlobs` stub returning `(nil, nil)` to `fakeBlobStore`.
- [x] 1.3 RED `fsblob/store_test.go`: `TestListBlobsReportsMtime` (T3, fsblob half) —
      enumerates committed blobs under `blobsRoot/<alg>/<hex>` with digest/size/mtime;
      empty store → empty slice.
- [x] 1.4 RED `fsblob/store_test.go`: `TestListBlobsNeverEnumeratesUploads` (T6,
      enumeration half) — an in-flight `BeginUpload`+`PutUploadChunk` is invisible to
      `ListBlobs`.
- [x] 1.5 GREEN `fsblob/store.go`: implement `ListBlobs` walking only `blobsRoot`
      (`uploads/` never walked).
- [x] 1.6 Confirm 1.3–1.4 GREEN: `go test ./internal/infra/storage/fsblob/... -run ListBlobs -v`.

## Phase 2: Mark — Global Tenant-less Digest Query (`sqlite`) — Slice 1

- [x] 2.1 GREEN (compile prerequisite) `internal/ports/regixtry.go`: add
      `ListReferencedBlobDigests(ctx) ([]string, error)` to `MetadataStore` — no
      `tenant` argument, blocking-language doc comment from design Decision C.
- [x] 2.2 RED `sqlite/store_test.go`: `TestListReferencedBlobDigestsIsGlobalAcrossTenants`
      (T7) — two tenants sharing a digest → exactly one row; signature has no
      `tenant` parameter (compile-time proof).
- [x] 2.3 GREEN `sqlite/store.go`: implement `SELECT DISTINCT digest FROM manifest_blobs`.
- [x] 2.4 Confirm 2.2 GREEN.

## Phase 3: Persist — `gc_reports`/`gc_report_candidates` Tables + CRUD (`sqlite`) — Slice 1

- [x] 3.1 GREEN `sqlite/store.go`: add both `CREATE TABLE IF NOT EXISTS` statements to
      `init()` (after `repository_feature_overrides`, before the trailing `ALTER TABLE`
      lines).
- [x] 3.2 GREEN (compile prerequisite) `internal/ports/regixtry.go`: add `GCReport`,
      `GCReportCandidate`, `GCReportDetail`, `GCReportStatus` + status consts;
      `CreateGCReport`/`GetGCReport`/`PruneExpiredGCReports` signatures on
      `MetadataStore`. `MarkGCReportDeleted` and the outcome types are **deliberately
      deferred to Phase 7** — see Mandatory Ordering Constraint.
- [x] 3.3 RED `sqlite/store_test.go`: `TestCreateGCReportPersistsReportAndCandidatesInOneTransaction`
      — report + candidates round-trip via `GetGCReport`, position order preserved.
- [x] 3.4 RED: `TestGetGCReportReturnsNotFoundForUnknownID`.
- [x] 3.5 GREEN `sqlite/store.go`: implement `CreateGCReport` (single transaction),
      `GetGCReport`.
- [x] 3.6 RED: `TestPruneExpiredGCReportsRemovesExpiredReportedButKeepsDeleted` (T9,
      prune half) — `reported`-state expired rows pruned; `deleted`-state rows kept
      regardless of age.
- [x] 3.7 GREEN `sqlite/store.go`: implement `PruneExpiredGCReports`
      (`DELETE ... WHERE status='reported' AND expires_at<=?`).
- [x] 3.8 Confirm 3.3–3.4, 3.6 GREEN.

## Phase 4: Diff+Grace — `gcCandidates` + `ComputeGCReport`/`GetGCReport` (service) — Slice 1

- [x] 4.1 RED `service_gc_test.go`: `TestComputeGCReportExcludesBlobInsideGraceWindow`
      (T3, service half) — `CommitUpload` with no manifest → 0 candidates; advance
      `s.now` past `gcGraceWindow` → 1 candidate.
- [x] 4.2 RED: `TestGCRespectsBlobsReferencedOnlyByAnotherTenant` (T2, report-time
      half) — tenant B publishes a manifest referencing `X`; `ComputeGCReport` under
      tenant A's context → `X` never appears as a candidate. (Delete-survival half
      deferred to Phase 7 — see Mandatory Ordering Constraint.)
- [x] 4.3 RED: `TestGCReportIsReadableAndUsableFromAnotherTenantContext` (T8) — a
      report computed under tenant A's context is fetchable under tenant B's.
- [x] 4.4 GREEN `service_gc.go` (new): `gcGraceWindow`/`gcReportTTL` consts,
      `gcCandidates(ctx, now)`, `ComputeGCReport` (gate → prune → `gcCandidates` →
      `CreateGCReport`), `GetGCReport`.
- [x] 4.5 GREEN `service.go`: `gcGate *scanGate` field, initialized in `NewService`.
- [x] 4.6 Confirm 4.1–4.3 GREEN.

## Phase 5: Report Endpoints (HTTP) — Slice 1 close-out

- [x] 5.1 RED `admin_gc_test.go`: `TestAdminGCRoutesRequireAdminPrincipal` (T12,
      report-endpoint half) — unauthenticated/non-admin → 401/403 on
      `POST /admin/v1/gc/reports` and `GET /admin/v1/gc/reports/{id}`.
- [x] 5.2 RED: `TestAdminPostGCReportsReturns201WithDetail`.
- [x] 5.3 RED: `TestAdminGetGCReportResourceReturnsDetailOrNotFound` — unknown id →
      404.
- [x] 5.4 RED: `TestAdminGCReportDispatchRejectsUnknownSubaction` (threat matrix:
      admin path dispatch) — `gc/reports/{id}/bogus` → 404.
- [x] 5.5 GREEN `admin_handlers.go`: `gc/reports` + `gc/reports/` cases in
      `handleAdmin`; `handleAdminGCReports` (POST); `handleAdminGCReportResource`
      (GET by id only — the delete branch is added in Phase 6).
- [x] 5.6 Confirm 5.1–5.4 GREEN: `go test ./internal/protocol/http/... -run GC -v`.
- [x] 5.7 Confirm Slice 1 close-out (Unit 1 focused test command): `go test ./... -count=1` green, `gofmt -l .` clean.

## Phase 6: Delete Flag, Unsupported Error, Config Wiring — Slice 2 start

- [x] 6.1 RED `domain/regixtry/errors_test.go`: `NewUnsupportedError` produces
      `ErrorCodeUnsupported == "UNSUPPORTED"`.
- [x] 6.2 GREEN `domain/regixtry/errors.go`: add `ErrorCodeUnsupported` +
      `NewUnsupportedError`.
- [x] 6.3 RED `internal/protocol/http` admin error test: `writeAdminError` maps
      `ErrorCodeUnsupported` → `501`.
- [x] 6.4 GREEN `admin_handlers.go`: add the switch case in `writeAdminError`.
- [x] 6.5 RED `service_gc_test.go`: `TestDeleteByGCReportRefusesWhenFlagOff` (T4,
      service half) — flag off (default) → `ErrorCodeUnsupported`; a store double
      that fails the test if `GetGCReport`/`MarkGCReportDeleted` is ever called
      confirms **zero reads/writes** (Decision F).
- [x] 6.6 GREEN `service.go`: `gcDeleteEnabled bool` field + `SetGCDeleteEnabled`
      (mirrors `SetDeleteEnabled`); `service_gc.go`: `DeleteByGCReport` — **only** the
      flag-check-first branch is real; the flag-on branch is a temporary
      `errors.New("gc delete: awaiting Phase 7 safety gate")` placeholder that calls
      no store or blob method, preserving Decision F's zero-reads/writes invariant.
- [x] 6.7 RED `cmd/regixtry/main_test.go`: `TestGCDeleteFlagIsIndependentOfDeleteEnabled`
      (T11) — `REGISTRY_DELETE_ENABLED=true` alone leaves `gcDeleteEnabled` false.
- [x] 6.8 GREEN `main.go`: `GCDeleteEnabled` field, `flags.BoolVar` reading
      `REGISTRY_GC_DELETE_ENABLED`, `service.SetGCDeleteEnabled` wiring.
- [x] 6.9 RED `admin_gc_test.go`: `TestAdminGCDeleteReturns501NamingEnvVar` (T4, HTTP
      half) — flag off, authorized caller → `501` naming `REGISTRY_GC_DELETE_ENABLED`;
      report row still `reported`; every blob still on disk.
- [x] 6.10 RED `admin_gc_test.go`: `TestAdminGCDeleteUnauthorizedRejectedBeforeFlagCheck`
      (T12, delete-endpoint half) — flag false + unauthorized caller → 401/403, not a
      flag-disabled error (`requireAdminPrincipal` runs first, Decision F).
- [x] 6.11 GREEN `admin_handlers.go`: add the `action=="delete"` (POST) branch to
      `handleAdminGCReportResource` calling `DeleteByGCReport`; anything else → 404;
      still safe — `DeleteByGCReport`'s on-branch is only 6.6's placeholder.
- [x] 6.12 RED (extends 5.4): `GET .../delete` → 405 with `Allow: POST` now that the
      route exists.
- [x] 6.13 Confirm 6.1, 6.3, 6.5, 6.7, 6.9–6.10, 6.12 GREEN.

## Phase 7: Safety-Critical Delete RED Gate (T1, T2 delete half, T6 delete half) — zero unlink capability

See Mandatory Ordering Constraint above. Nothing in this phase may call `os.Remove`.

- [x] 7.1 GREEN (compile prerequisite, zero unlink capability) `ports/regixtry.go`:
      add `DeleteBlob(ctx, digest) (bool, error)` to `BlobStore`; add
      `GCDeleteOutcome`/`GCCandidateOutcome` types + outcome consts
      (`pending`/`deleted`/`retained`/`missing`/`failed`); add `MarkGCReportDeleted`
      signature to `MetadataStore`.
- [x] 7.2 GREEN (compile prerequisite, zero unlink capability) `fsblob/store.go`: add
      `DeleteBlob` — validates the digest via `domain.ParseDigest`/`.Validate()`, then
      **unconditionally returns** `(false, errors.New("gc: blob unlink not yet implemented"))`.
      No `os.Remove` call exists in the file. `gitleaks/stage_test.go`'s
      `fakeBlobStore` gets a matching stub.
- [x] 7.3 GREEN (compile prerequisite) `sqlite/store.go`: implement
      `MarkGCReportDeleted` for real — SQL `UPDATE gc_reports ... WHERE id=? AND
      status='reported'` + per-candidate outcome `UPDATE`. A DB status write, not a
      filesystem unlink — safe to implement now.
- [x] 7.4 GREEN `service_gc.go`: replace 6.6's placeholder flag-on branch with the
      full real orchestration — `GetGCReport` → status must be `reported` (else
      conflict) → `now.Before(ExpiresAt)` (else validation) → `gcGate` → recompute
      `gcCandidates` → `intersect(report.candidates, fresh)` → per-digest
      `store.DeleteBlob` → build `GCDeleteOutcome` → `MarkGCReportDeleted`. Because
      7.2's `DeleteBlob` unconditionally errors, every intersection member currently
      resolves to outcome `failed`; nothing can transition to `deleted` yet.
- [x] 7.5 RED `service_gc_test.go`: `TestDeleteByGCReportNeverUnlinksBlobReferencedByAManifest`
      (T1) — report a candidate `X` while unreferenced, publish a manifest
      referencing `X`, then delete: `BlobExists(X)` still true; `X`'s outcome
      recorded `retained`; `bytes_reclaimed` excludes `X`. **Confirmed RED today**:
      the currently-unreferenced sibling candidate that should reach `deleted`
      instead surfaces `failed` via 7.2's stub, so the test's full expected terminal
      shape does not hold yet.
- [x] 7.6 RED `service_gc_test.go`: `TestGCRespectsBlobsReferencedOnlyByAnotherTenantAtDeleteTime`
      (T2, delete-survival half) — under tenant A's context, `X` (referenced only by
      tenant B) is excluded from candidates and never attempted for delete, proven
      end-to-end through `DeleteByGCReport`, not just `gcCandidates`.
- [x] 7.7 RED `fsblob/store_test.go`: `TestDeleteBlobNeverTouchesUploads` (T6, delete
      half) — no `DeleteBlob` call, however constructed, can resolve or touch a path
      under `uploads/`. RED today because `DeleteBlob` unconditionally errors before
      any path is ever resolved — proven safe by construction, not by accident.
- [x] 7.8 Confirm 7.5–7.7 fail for the stated reasons (not a compile error); record
      each failure message in this file. **Grep gate**: `rg 'os\.Remove'
      internal/infra/storage/fsblob/store.go` returns zero matches at this commit —
      literal proof no unlink-capable code exists anywhere in the tree yet.

      **RESOLVED (this apply-batch)**: confirmed the discrepancy flagged by the
      Phase 1-6 executor is real — the literal `rg 'os\.Remove'` pattern also
      matches the 2 pre-existing, GC-unrelated `os.RemoveAll(s.uploadDir(uploadID))`
      calls in `CommitUpload`/`CancelUpload` (uploads/ cleanup, predates this
      change). Resolved by scoping the gate to the singular, word-bounded pattern
      `rg 'os\.Remove\(' internal/infra/storage/fsblob/store.go` (excludes
      `RemoveAll` because of the trailing `(`), which correctly returns **zero
      matches** at this commit — the intended literal proof that no unlink-capable
      code exists anywhere in the tree yet. Applying the same scoping to 11.10's
      final gate.

      **Recorded RED failures (all three confirmed failing for the honest reason —
      the stub unconditionally errors before any path is built, never a compile
      failure):**
      - `TestDeleteByGCReportNeverUnlinksBlobReferencedByAManifest` (T1):
        `service_gc_test.go:269: sibling candidate outcome = "failed", want
        "deleted" (still genuinely unreferenced, past grace)`
      - `TestGCRespectsBlobsReferencedOnlyByAnotherTenantAtDeleteTime` (T2,
        delete-survival half): `service_gc_test.go:355: unreferenced candidate
        outcome = "failed", want "deleted" (genuinely garbage, past grace, must
        actually be deleted at delete time)`
      - `TestDeleteBlobNeverTouchesUploads` (T6, delete half):
        `store_test.go:132: DeleteBlob() error = gc: blob unlink not yet
        implemented, want nil once a real unlink runs against a committed blob`

## Phase 8: Real Unlink Implementation (GREEN) — the only phase permitted to add `os.Remove`

- [x] 8.1 GREEN `fsblob/store.go`: replace 7.2's stub body — resolve the path via the
      existing `blobPath` helper (scoped under `blobsRoot` only), call `os.Remove`;
      `os.IsNotExist` → `(false, nil)` idempotent no-op.
- [x] 8.2 RED `fsblob/store_test.go`: `TestDeleteBlobRejectsTraversalShapedDigestBeforeTouchingFS`
      (threat matrix: filesystem unlink scope) — a traversal-shaped digest string is
      rejected by `domain.ParseDigest`/`Validate` before any path is built.
      **Note**: digest validation already existed in 7.2's stub body, so this test
      passes immediately once written (against both the stub and the real 8.1 body)
      rather than needing a separate RED-then-GREEN transition — the negative
      behavior was never unimplemented, only the positive unlink was.
- [x] 8.3 RED `fsblob/store_test.go`: `TestDeleteBlobIsIdempotentOnAlreadyAbsentFile`
      — `(false, nil)`, never an error. Genuinely RED against 7.2's stub (which
      always returns a non-nil error), GREEN after 8.1's real body.
- [x] 8.4 GREEN: wire 8.2/8.3's negative-path assertions (validation already present
      from 8.1's real body).
- [x] 8.5 Confirm 7.5–7.7 (T1, T2 delete half, T6 delete half) now GREEN, plus
      8.2–8.3 GREEN. All confirmed via `go test ./internal/app/regixtry/...
      ./internal/infra/storage/fsblob/... -run '...' -v` — PASS.
- [x] 8.6 RED+GREEN `service_gc_test.go`: `TestStillUnreferencedDigestIsDeletedAtDeleteTime`
      (spec's dedicated "Still-unreferenced digest is deleted" scenario) — blob file
      genuinely absent from disk after delete, outcome `deleted`, `bytes_reclaimed`
      includes it. Confirmed GREEN against the real 8.1 implementation.

## Phase 9: Delete Endpoint Confirmation (HTTP)

- [x] 9.1 `admin_gc_test.go`: `TestAdminGCDeleteEndpointDeletesAndReturnsTerminalDetail`
      — `POST /admin/v1/gc/reports/{id}/delete` → `200` + terminal `GCReportDetail`
      with per-candidate outcomes. Written after Phase 8 landed (this apply batch
      completed Phases 7-11 in one continuous session), so it exercises the route
      against the real `DeleteBlob` immediately rather than needing a separate RED
      pass against the stub — the route itself was already wired at 6.11 and proven
      against the flag-off 501 path by `TestAdminGCDeleteReturns501NamingEnvVar`.
      Uses a new `backdateBlob` test helper (os.Chtimes on the blob file, mirroring
      T3's fsblob-layer technique) since the HTTP test package cannot reach
      `Service`'s unexported `now` field to simulate grace-window expiry.
- [x] 9.2 Confirm 9.1 GREEN (route already wired at 6.11; first pair to exercise it
      against real deletion) — `go test ./internal/protocol/http/... -run
      TestAdminGCDeleteEndpointDeletesAndReturnsTerminalDetail -v` → PASS.
- [x] 9.3 **Post-verify continuation task (added after sdd-verify flagged the gap)**:
      spec.md's "Delete without a report reference is rejected" scenario (empty or
      unknown report id, flag ON) had no covering test anywhere in the tree — the
      router's `id == ""` 404 branch and `GetGCReport`'s NotFound-for-unknown-id
      behavior were each tested independently elsewhere but never proven to compose
      through the delete endpoint. Closed with two tests, flag deliberately ON in
      both to isolate the ID-validation path from the flag-off 501 path:
      `TestDeleteByGCReportRejectsEmptyReportID` (`service_gc_test.go`) covers the
      omitted-id half at the service layer — `service.DeleteByGCReport(ctx, "")` →
      `ErrorCodeNotFound`. HTTP-level empty-id coverage was investigated but the only
      way to reach `handleAdminGCReportResource`'s `id == ""` branch is a
      double-slash URL (`.../gc/reports//delete`), which `net/http`'s own `ServeMux`
      307-redirects before this application's routing code ever runs — that HTTP
      branch is unreachable through the real routing surface, so the service-layer
      test is the precise one. `TestAdminGCDeleteRejectsUnknownReportID`
      (`admin_gc_test.go`) covers the well-formed-but-nonexistent-id half at the HTTP
      layer — `POST /admin/v1/gc/reports/{fabricated-uuid}/delete` → `404`. Both
      tests **passed immediately on first run** (proof by construction, same pattern
      as 8.2/11.1-11.2 — no production code change was needed). Confirmed via
      `go test ./internal/app/regixtry/... -run TestDeleteByGCReportRejectsEmptyReportID -v`
      and `go test ./internal/protocol/http/... -run TestAdminGCDeleteRejectsUnknownReportID -v`,
      both PASS; full-repo gate re-run clean (`go build ./...`, `go vet ./...`,
      `gofmt -l .` empty, `go test -count=1 ./...` 18/18 packages green).

## Phase 10: Single-Use, Expiry, Partial Failure, Full Audit Trail, Index Decision

- [x] 10.1 RED `sqlite/store_test.go`: `TestMarkGCReportDeletedRejectsNonReportedRow`
      (T5, store half) — a second `UPDATE ... WHERE status='reported'` affects 0 rows
      → `domain.ErrorCodeConflict`; proves the SQL `WHERE` clause is the guard.
      **Continuation-batch note**: found already written and passing in the tree at
      this batch's start (uncommitted, pre-existing from the connection-interrupted
      prior batch). Re-confirmed GREEN this batch via
      `go test ./internal/infra/metadata/sqlite/... -run TestMarkGCReportDeletedRejectsNonReportedRow -v`
      → PASS. No code changes made; checkbox was simply never ticked before the drop.
- [x] 10.2 RED `service_gc_test.go`: `TestDeleteByGCReportRejectsAlreadyUsedReport`
      (T5, service half) — a second delete against the same report id → conflict, no
      blob unlinked. **Continuation-batch note**: found already written and passing;
      re-confirmed GREEN this batch.
- [x] 10.3 RED `service_gc_test.go`: `TestDeleteByGCReportRejectsExpiredReport` (T9,
      expiry half) — a report past `expires_at` → validation error, rejected without
      unlinking. **Continuation-batch note**: found already written and passing;
      re-confirmed GREEN this batch.
- [x] 10.4 RED `service_gc_test.go`: `TestDeleteByGCReportRecordsPerDigestFailureAndPartialReclaim`
      (T10) — a fake `ports.BlobStore` injected via `NewService`; one unlink errors →
      outcomes mix `deleted`/`failed`, `bytes_reclaimed` counts only successes, report
      still reaches `deleted`. **Continuation-batch note**: found already written and
      passing; re-confirmed GREEN this batch.
- [x] 10.5 RED `service_gc_test.go`: `TestDeleteByGCReportRecordsFullAuditFieldSetOnCompletion`
      — **dedicated test (reviewer note: spec's "terminal row records audit fields"
      scenario has no dedicated coverage in design's 12 test groups)**. Asserts the
      complete audit field set on the terminal report row — `deleted_at`,
      `deleted_by`, `deleted_count`, `bytes_reclaimed`, **and** `error` — for both an
      all-success case and 10.4's partial-failure case, not just `bytes_reclaimed`
      (which every other GC test only asserts incidentally). **Continuation-batch
      note**: found already written (as two subtests, `all_success`/`partial_failure`)
      and passing; re-confirmed GREEN this batch.
- [x] 10.6 **Index decision (reviewer's call, deliberate — not silently skipped)**:
      `UPDATE gc_report_candidates SET outcome=?, error=? WHERE report_id=? AND
      digest=?` filters by `digest` within a `report_id` partition with no index on
      that column — a potential O(n) scan per digest for reports up to ~100k
      candidates. Choose one: **(a)** add
      `CREATE INDEX IF NOT EXISTS idx_gc_report_candidates_report_digest ON
      gc_report_candidates(report_id, digest)` to `store.init()` and confirm via
      `EXPLAIN QUERY PLAN` that the outcome `UPDATE` uses it; or **(b)** document in a
      `store.go` comment above `MarkGCReportDeleted` why the O(n) scan is acceptable
      at expected candidate volumes and not worth an extra index. Record the chosen
      option and rationale in this file before Phase 10 is confirmed done.
      **Decision recorded**: option **(a)** was chosen and already implemented in the
      tree at this batch's start —
      `idx_gc_report_candidates_report_digest ON gc_report_candidates(report_id, digest)`
      added to `store.init()`'s `CREATE INDEX IF NOT EXISTS` statement list, with a
      comment explaining the O(n)-per-digest / O(n²)-per-delete rationale. Rationale:
      the design explicitly cites reports of up to ~100k candidates, and
      `MarkGCReportDeleted` issues one `UPDATE ... WHERE report_id=? AND digest=?` per
      candidate inside a single delete transaction — without the index that is a
      linear scan of the report's candidate rows per digest, i.e. quadratic across
      the whole delete. A composite `(report_id, digest)` index turns each lookup into
      a direct point query. This is a pure additive index on a brand-new table with no
      existing traffic pattern to disrupt, so option (a) was preferred over
      documenting-and-accepting the O(n) scan.
- [x] 10.7 GREEN `sqlite/store.go`: implement whichever of 10.6(a)/(b) was chosen.
      **Continuation-batch note**: found already implemented (the `CREATE INDEX`
      statement above) plus a dedicated
      `TestMarkGCReportDeletedCandidateUpdateUsesTheReportDigestIndex` test asserting
      via `EXPLAIN QUERY PLAN` that the per-candidate `UPDATE` uses the index (not a
      table/`SCAN`). Re-confirmed GREEN this batch:
      `go test ./internal/infra/metadata/sqlite/... -run TestMarkGCReportDeletedCandidateUpdateUsesTheReportDigestIndex -v`
      → PASS.
- [x] 10.8 Confirm 10.1–10.5 GREEN. **Re-confirmed this batch** (all found already
      implemented from the interrupted prior batch, independently re-run, not
      re-implemented):
      `go test ./internal/infra/metadata/sqlite/... -run 'TestMarkGCReportDeletedRejectsNonReportedRow|TestMarkGCReportDeletedCandidateUpdateUsesTheReportDigestIndex' -v`
      → both PASS;
      `go test ./internal/app/regixtry/... -run 'TestDeleteByGCReportRejectsAlreadyUsedReport|TestDeleteByGCReportRejectsExpiredReport|TestDeleteByGCReportRecordsPerDigestFailureAndPartialReclaim|TestDeleteByGCReportRecordsFullAuditFieldSetOnCompletion' -v`
      → all PASS (including both audit-field subtests). No production or test code
      was modified for 10.1–10.8 in this batch — this was a verification pass over
      pre-existing, uncommitted work from the connection-interrupted prior batch, per
      the orchestrator's explicit cross-check instruction.

## Phase 11: Cross-Cutting Safety Proofs, Threat Matrix, Docs, Full Regression

- [x] 11.1 RED `service_gc_test.go`: `TestNoUnattendedDeletionOccursWithoutExplicitDeleteRequest`
      — a grace-expired unreferenced blob + repeated `ComputeGCReport` calls over
      time, `DeleteByGCReport` never called → blob file still present. **Written this
      batch.** Safety net run first (all pre-existing `service_gc_test.go` GC tests,
      full package `go test ./internal/app/regixtry/... -count=1`) — green baseline
      before adding the new test. Like 8.2, this is a proof-by-construction test: it
      references only existing production code (`ComputeGCReport`, `DeleteByGCReport`,
      `BlobExists`) and requires no implementation change, so it is expected to pass
      immediately rather than needing a RED-then-GREEN transition — the invariant it
      proves ("no unattended deletion") was already true by construction because
      `ComputeGCReport` never calls `DeleteBlob` anywhere in its body.
- [x] 11.2 Confirm 11.1 GREEN (trivially — `ComputeGCReport` never calls `DeleteBlob`;
      grep confirms). Confirmed both ways: `rg -n "DeleteBlob" internal/app/regixtry/service_gc.go`
      shows `DeleteBlob` called only inside `DeleteByGCReport`, never inside
      `ComputeGCReport`/`gcCandidates`; and
      `go test ./internal/app/regixtry/... -run TestNoUnattendedDeletionOccursWithoutExplicitDeleteRequest -v`
      → PASS (3 repeated `ComputeGCReport` calls across advancing time, blob file
      confirmed present via `BlobExists` after each).
- [x] 11.3 Cross-reference design's Threat Matrix table row by row against tests
      written (5.4/6.12 dispatch, 8.2 unlink scope, 6.5/6.9/6.10 destructive gate) —
      confirm no gap. **Confirmed, no gap**:
      - Admin path dispatch → `TestAdminGCReportDispatchRejectsUnknownSubaction` (5.4,
        unknown sub-action → 404) + `TestAdminGCDeleteRouteRejectsWrongMethod` (6.12,
        `GET .../delete` → 405) both present and passing in `admin_gc_test.go`.
      - Filesystem unlink scope → `TestDeleteBlobRejectsTraversalShapedDigestBeforeTouchingFS`
        (8.2) + `TestDeleteBlobNeverTouchesUploads` (7.7/T6) both present and passing
        in `fsblob/store_test.go`.
      - Destructive capability gate (admin → flag → report state → expiry →
        recompute+intersect, flag default false) → `TestAdminGCRoutesRequireAdminPrincipal`
        (5.1/T12), `TestAdminGCDeleteReturns501NamingEnvVar` (6.9/T4),
        `TestAdminGCDeleteUnauthorizedRejectedBeforeFlagCheck` (6.10) all present and
        passing in `admin_gc_test.go`.
      - Git-repository-selection / commit-state / push-state / PR-command /
        documentation-path threat rows: confirmed N/A per design (no VCS, no
        subprocess, no file classification/execution in this change).
- [x] 11.4 `docs/configuration.md`: document `REGISTRY_GC_DELETE_ENABLED` (default
      `false`), distinct from `REGISTRY_DELETE_ENABLED`. Done this batch — added a row
      to the `serve` flags table immediately after `-delete-enabled`.
- [x] 11.5 `docs/registry.md`: document the grace window, expiry-is-hygiene-not-safety,
      the audit trail, and the no-recovery warning. Done this batch — added a new
      "Blob garbage collection" section (routes table + all four required points) and
      updated the "Blob deletes"/"Garbage collection" rows in the compatibility table
      to reflect the shipped implementation.
- [x] 11.6 `docs/roadmap.md`: mark blob garbage collection delivered. Done this batch —
      added a `blob-garbage-collection is complete` checkpoint bullet, a
      "Delivery sequence for `blob-garbage-collection`" table (PR 1/PR 2, both
      `Completed`), corrected the stale "operator-triggered garbage collection
      controls" non-goal line, and corrected the stale "Garbage collection" Post-v1
      candidate row to point at the now-shipped workstream.
- [x] 11.7 `go build ./...`, `go vet ./...`, `gofmt -l .` clean. **Confirmed this
      batch**: `go build ./...` clean; `go vet ./...` clean; `gofmt -l .` empty output
      (one file, `internal/app/regixtry/service_gc_test.go`, needed a `gofmt -w` pass
      after adding 11.1's test — applied, then re-confirmed empty).
- [x] 11.8 `go test -count=1 ./...` green (full repo, not just touched packages).
      **Confirmed this batch**: all 18 packages pass,
      `go test -count=1 ./...` exit 0, zero failures.
- [x] 11.9 Confirm `git diff go.mod go.sum` is empty — zero new dependency. **Confirmed
      this batch**: `git diff --stat go.mod go.sum` produces no output — both files
      are byte-identical to the base branch; no new dependency was introduced
      anywhere in this change.
- [x] 11.10 Final grep gate: exactly one `os.Remove`/`os.RemoveAll` call site exists in
      `internal/infra/storage/fsblob/store.go` (inside `DeleteBlob`, added Phase 8),
      proving no other unlink path was introduced anywhere else in the change.
      **Apply-batch note (same correction as 7.8)**: `store.go` already has 2
      pre-existing `os.RemoveAll` call sites in `CommitUpload`/`CancelUpload`
      (uploads/ cleanup, unrelated to GC, predating this change), so the literal
      "exactly one" count will be 3, not 1, once `DeleteBlob` is real. Scope this
      gate to `DeleteBlob`'s body specifically (e.g. confirm exactly one
      `os.Remove(` — singular, not `RemoveAll` — appears, and that it is inside
      `DeleteBlob`), or restate the expected count as 3 with the other two
      identified as pre-existing/unrelated.
      **Re-confirmed this batch (final gate)**: `rg -n "os\.Remove" internal/infra/storage/fsblob/store.go`
      returns 3 matches total — lines 158 and 166 are the pre-existing, GC-unrelated
      `os.RemoveAll(s.uploadDir(uploadID))` calls inside `CommitUpload`/`CancelUpload`
      (uploads/ cleanup, predates this change); line 279 is the single
      `os.Remove(s.blobPath(digest))` call, and it is inside `DeleteBlob` (confirmed
      by reading the enclosing function). The scoped, singular-only gate —
      `rg -n 'os\.Remove\(' internal/infra/storage/fsblob/store.go` (excludes
      `RemoveAll` via the trailing `(`) — returns exactly **one** match: line 279
      inside `DeleteBlob`. A repo-wide sweep (`rg -n "os\.Remove\(" --type go | grep -v _test.go`)
      confirms the only other non-test `os.Remove(` call sites in the entire tree are
      in `internal/infra/scanning/{trivy,gitleaks}/runtime_manager.go` (temp symlink
      cleanup for scanner binaries) — pre-existing and unrelated to blob storage or
      this change. No other unlink-capable path was introduced anywhere.
