# Proposal: Blob Garbage Collection — Persisted Report + Gated Delete v1

## Intent

Blob files are never removed. `DeleteManifestByDigest` / `DeleteTag` delete metadata rows only, so every deleted tag or overwritten manifest leaves its layers on disk permanently. `blobsRoot` grows without bound with no way to see, or reclaim, the garbage. v1 makes unreferenced storage **visible**, **auditable**, and — behind an explicit opt-in — **reclaimable**.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | **Report and delete both ship in v1.** Deletion is gated behind a server-level opt-in flag `REGISTRY_GC_DELETE_ENABLED` / `-gc-delete-enabled` (default `false`), mirroring `REGISTRY_DELETE_ENABLED`. The report path is a first-class read path, always reachable regardless of the flag. | Operators need bytes back, not a document. The flag makes the destructive half opt-in per deployment while the read path stays free to run. Mark-set computation is **one** function shared by report and delete: the report is a faithful preview by construction, and there is a single correctness-critical code path, not two that can drift. |
| D1a | **A distinct flag, not a reuse of `REGISTRY_DELETE_ENABLED`.** Authorization is checked *before* the flag, per the `DeleteManifest` precedent (`service.go:352-358`), so flag state is never disclosed to a caller who could not delete anyway. (Flag-off wire behaviour: see D9 — the original "⇒ `UNSUPPORTED`" claim was wrong for admin routes.) | `REGISTRY_DELETE_ENABLED` is documented as metadata-only and "never touches blob files". Reusing it would silently widen it to disk bytes for every operator who already enabled it. |
| D2 | **Manual-only trigger** (admin endpoint, like `QueueManualScan`). No scheduler. | Stronger now that delete exists: no unattended deletion, every reclaimed byte traces to a human request. Avoids `scanning.Scheduler` and its per-tenant wiring. Forward constraint: GC scheduling MUST later be one global `ports.JobRunner` job, never per tenant. |
| D3 | **Fixed 24h grace constant**, not configurable. | Excludes blobs whose manifest may still be in flight (blobs-first / manifest-last push). The grace window is the only protection against deleting an in-flight push, so it must not be settable to `0`. Harbor uses ~2h; 24h is deliberately conservative. Single named constant so v2 can promote it to config deliberately. |
| D4 | **Abandoned uploads excluded.** | Different lifecycle (`uploads/<id>/`, age-only, no mark set). Also a hard safety boundary: the sweep MUST NOT walk or unlink anything under `uploads/`. Track as its own change. |
| D5 | **Delete is two-step: report, then delete referencing that report.** No blind delete. The mark set is recomputed at delete time and only the **intersection** of the referenced report and the fresh recomputation is unlinked. | The TUI `ConfirmTitle`/`ConfirmMessage` pattern (`ports.FeatureAction`) is UI-layer and the TUI is out of scope, so confirmation must live in the request. Requiring the report identity is a real confirm *and* closes the TOCTOU window where a push lands between preview and delete. |
| D6 | **The report is PERSISTED server-side and is GLOBAL, not tenant-scoped.** Two tables mirroring the `scan_runs` / `scan_run_findings` precedent: `gc_reports` (id, status, computed_at, expires_at, grace_cutoff, candidate_count, candidate_bytes, duration_ms, requested_by, triggered_in_tenant, audit columns) and `gc_report_candidates` (report_id, position, digest, size, mtime, + per-digest delete outcome), `ON DELETE CASCADE`. | A client-echoed report is not a confirm — the server would be trusting the caller's own candidate list. Persisting it makes the report the server's own record. **Global, because the thing it describes is global**: the mark set is `SELECT DISTINCT digest FROM manifest_blobs` across all tenants and a blob file is one shared content-addressed file. A tenant-scoped report would be a semantic lie (the candidate set is the *deployment's* garbage, not a tenant's), would leak other tenants' unreferenced digests to whoever read it, and would raise an unanswerable question: may tenant A delete a report created under tenant B? The audit requirement is satisfied by *identity* (`requested_by`), not by tenancy. `triggered_in_tenant` is recorded as provenance ONLY and is deliberately **not** named `tenant` — every other table uses `tenant` as a query predicate, and the copy-paste habit of adding `WHERE tenant = ?` here is the same class of bug as the Severe "mark query accidentally tenant-scoped" risk below. |
| D7 | **The audit trail is the report row transitioning to a terminal `deleted` state** (`deleted_at`, `deleted_by`, `deleted_count`, `bytes_reclaimed`, `error`), plus the per-digest outcome on each candidate row — not a separate audit table. The row is frozen once terminal. | D5 guarantees exactly one report per delete, so report and audit record are 1:1; a separate table would duplicate the candidate list and force a join to answer any audit question. Per-digest outcome columns answer "what was *actually* reclaimed" including partial failures, which a summary-only audit row cannot. The state machine buys a second property for free: a report already `deleted` cannot be replayed into another delete, making the two-step flow **single-use**. A separate table would only be right if many actions targeted one report, or if audit needed its own retention/permission boundary — neither holds. `requested_by` / `deleted_by` capture `principal.UserID` via `ports.PrincipalFromContext`, exactly the `pushed_by` precedent. |
| D8 | **Reports expire 24h after `computed_at`**; a delete referencing an expired report is rejected. Expired reports still in `reported` state are pruned when a new report is computed; `deleted`-state reports are retained indefinitely as the audit trail. | **This is hygiene, not safety** — and must be documented as such, or a future reader will mistake it for the correctness mechanism. Safety is D5: recompute + intersect can only ever *under*-delete. Expiry exists so a months-old report cannot be submitted to produce a confusing "0 bytes reclaimed", and so preview rows (one per candidate digest — potentially 100k) do not accumulate forever. Same constant family as D3, so the two windows stay explainable together. |
| D9 | **Flag-off returns `501 Not Implemented` with a message naming the exact flag** (`blob garbage collection delete is disabled (REGISTRY_GC_DELETE_ENABLED)`), via a new `ErrorCodeUnsupported` mapped in `writeAdminError`. | Correction of fact: admin routes use `writeAdminError`, which emits `{"error": message}` with **no code field at all** — `UNSUPPORTED` is an OCI code belonging to `writeError` on the registry routes and is simply not reachable here. So the original D1a wire claim was wrong, and since there is no code vocabulary, the *message* is the only disambiguator available — hence it must name the env var. `501` is unambiguous: the endpoint exists but the capability is off in this deployment. Rejected alternatives: `422` and `409` misreport a server configuration state as a client error; `403` is this codebase's authorization denial and would conflate "not allowed" with "not enabled", the exact opposite of D1a's intent. Exact JSON body and route paths are left to sdd-design. |

## Scope

### In Scope
- `ports.BlobStore`: blob enumeration (digest, size, mtime) **and** blob delete by digest.
- `fsblob`: walk `blobsRoot/<algo>/<hex>`; unlink by digest; never touch `uploads/`.
- `MetadataStore`: global mark query `SELECT DISTINCT digest FROM manifest_blobs` — intentionally **not** tenant-scoped.
- **New metadata tables** `gc_reports` + `gc_report_candidates` (global; additive `CREATE TABLE IF NOT EXISTS` migration in `store.init()`), with store methods to create a report with its candidates in one transaction, fetch by id, transition to `deleted` with per-digest outcomes, and prune expired preview reports.
- App service: shared mark → sweep-candidate diff → grace filter, consumed by both report and delete. Single in-flight run.
- **Two-call admin flow**: `POST` compute+persist a report (always available) → `GET` report by id → `POST` delete-by-report-id (flag-gated, expiry-checked, single-use).
- Audit capture: `requested_by` / `deleted_by` from `ports.PrincipalFromContext`, `bytes_reclaimed`, per-digest outcome.
- New `ErrorCodeUnsupported` + `writeAdminError` 501 mapping (D9).
- Config wiring: `-gc-delete-enabled` / `REGISTRY_GC_DELETE_ENABLED` in `cmd/regixtry/main.go`, default `false`.
- Docs: configuration, registry, roadmap; operator guidance on the gate, the grace window, report expiry, and the audit trail.

### Out of Scope
- Scheduled/background GC; configurable grace or expiry.
- TUI surface; report history list/pagination endpoint.
- Retention/pruning policy for completed (`deleted`-state) audit reports.
- Abandoned upload cleanup.
- Manifest payload GC (payloads live in SQLite, not `fsblob`).
- Quarantine/soft-delete tier — delete is an unlink.

## Capabilities

### New Capabilities
- `blob-garbage-collection`: identifying, measuring, reporting, auditing, and (opt-in) reclaiming unreferenced blob storage.

### Modified Capabilities
- None. `manifest-deletion` requirements are untouched; its flag and semantics are unchanged.

## Approach

Classic mark-and-sweep. Mark = every digest in `manifest_blobs` across all tenants. Sweep = every file under `blobsRoot`. Candidates = sweep − mark − anything younger than the grace threshold.

**Report call** persists the candidate set plus `computed_at`, `expires_at`, the grace cutoff actually used, totals, duration, and `requested_by`, returning the report id. **Delete call** loads that report, rejects it if expired or already terminal, re-runs the identical computation, intersects, unlinks only the intersection, then transitions the row to `deleted` recording who/when/actual bytes reclaimed and each digest's outcome — not all-or-nothing.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/ports/regixtry.go` | Modified | Blob enumeration + delete on `BlobStore`; global mark + report persistence methods on `MetadataStore`; `GCReport` / `GCReportCandidate` types |
| `internal/infra/storage/fsblob/store.go` | Modified | Walk `blobsRoot`; unlink by digest; `uploads/` excluded |
| `internal/infra/metadata/sqlite/store.go` | Modified | Non-tenant-scoped mark query; two new tables in `init()`; report create/fetch/transition/prune |
| `internal/domain/...` | Modified | New `ErrorCodeUnsupported` |
| `internal/app/regixtry/` | New | GC service: shared mark set, report persistence, gated delete, `SetGCDeleteEnabled` setter (mirrors `SetDeleteEnabled`) |
| `internal/protocol/http/admin_handlers.go` | Modified | Report create/fetch endpoints; flag-gated delete endpoint; 501 mapping in `writeAdminError` |
| `cmd/regixtry/main.go` | Modified | Flag/env parse + `SetGCDeleteEnabled` wiring |
| `docs/` | Modified | `configuration.md`, `registry.md`, `roadmap.md` |

## Risks

**This change ships an irreversible destructive path.** Blobs are content-addressed files; a wrongly deleted blob is unrecoverable without a re-push, and it silently breaks every manifest referencing it.

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Mark query accidentally tenant-scoped → live blobs deleted | Low | **Severe** | Explicit spec requirement; test with two tenants sharing a digest; flag off by default |
| Report table copy-pastes the `tenant` predicate habit → report scoped, delete under-deletes or leaks | Med | Med | D6: column named `triggered_in_tenant`, never `tenant`; spec requirement + test that a report is fetchable and usable from any tenant context |
| Any mark-set bug at all → silent registry corruption | Low | **Severe** | Strict TDD with extra care on the delete path: a RED test proving a *referenced* blob is NOT unlinked must exist and fail before any unlink code is written. Negative tests are the primary artifact, not the happy path |
| Push lands between report and delete (TOCTOU) | Med | High | D5 recompute + intersection; 24h grace |
| Expiry mistaken for the safety mechanism, then loosened | Med | Med | D8 states it is hygiene; safety is D5. Docs and spec must repeat this |
| Candidate rows accumulate (one row per unreferenced digest, per report) | Med | Low | D8 prune of expired preview reports; completed reports retained deliberately |
| `mtime` is write time, not commit time — grace under-protects | Low | High | 24h absorbs it; documented |
| Operator with `REGISTRY_DELETE_ENABLED` on assumes blob GC follows | Med | High | Separate flag (D1a); 501 message names the exact flag (D9); docs state the distinction |
| Crash mid-sweep leaves partial deletion | Med | Low | Content-addressed store — partial deletion is not corrupting; per-digest outcomes persisted, response reports actual reclaimed bytes |
| Full `blobsRoot` walk is I/O heavy | Med | Low | Manual trigger; single in-flight run; duration reported |
| **Persisted reports + audit push the single PR well past the 800-line review budget** | **High** | Med | **See below — recommend chained slices** |

### Review budget

The added persistence and audit surface (migration, two tables, store CRUD in a transaction, report types, expiry/prune, a new error code and its mapping, plus Strict-TDD negative-heavy tests) realistically lands this at **900–1400 changed lines**. The prior revision rated this Med; with this scope it is **High**. `sdd-tasks` should forecast against a chained split, natural boundary:

1. **Slice 1 (non-destructive)**: enumeration, mark query, candidate computation, report persistence, report create/fetch endpoints. Ships operator value, fully testable, contains zero unlink code.
2. **Slice 2 (destructive)**: delete-by-report-id, the flag, expiry/single-use enforcement, audit transition, 501 mapping.

This split is worth it beyond size: it isolates the irreversible path in its own PR, which is what the adversarial review this change warrants should focus on.

## Rollback Plan

Not symmetric. Two layers:
1. **Operational, immediate**: set `REGISTRY_GC_DELETE_ENABLED=false` and restart. The report path survives; no further deletion is possible. This is the real rollback.
2. **Code**: revert the commit — endpoints, flag, and error code disappear. The migration is additive `CREATE TABLE IF NOT EXISTS`, so the tables simply go inert; **do NOT drop them**, they hold the audit record of irreversible deletions. Dropping them destroys the only evidence of what was reclaimed.

Neither restores already-deleted blobs. Recovery from a bad delete is re-push from the source, or restore `blobsRoot` from backup. Docs must say this plainly.

## Dependencies

- None.

## Success Criteria

- [ ] With the flag **off**, the report endpoint returns and persists candidates and the delete endpoint refuses with `501` naming `REGISTRY_GC_DELETE_ENABLED`; zero writes to `blobsRoot` (proven by test).
- [ ] With the flag **off**, an *unauthorized* caller is rejected before the flag is consulted (flag state not disclosed).
- [ ] With the flag **on**, delete removes only unreferenced blobs and reports actual bytes reclaimed.
- [ ] A blob referenced by tenant B is never reported and never deleted while tenant A holds no reference.
- [ ] A blob written within the grace window is never reported and never deleted.
- [ ] A delete request that does not reference a persisted report is rejected; so is one referencing an unknown, expired, or already-`deleted` report.
- [ ] A report created under one tenant context is fetchable and usable from another (reports are global).
- [ ] After a delete, the report row records `deleted_by`, `deleted_at`, `bytes_reclaimed`, and per-digest outcomes; a digest that became referenced between report and delete is recorded as not deleted.
- [ ] Nothing under `uploads/` is ever enumerated or unlinked.

## Note

Session ran in automatic mode; the interactive proposal question round was suppressed. This revision resolves the two open product questions from the prior revision (report persistence → D6, audit trail → D7) and adds D8 (expiry) and D9 (flag-off wire behaviour) as executor-level calls. **D9 corrects a factual error in the prior D1a**: admin routes carry no OCI error code. Open for correction before spec/design.
