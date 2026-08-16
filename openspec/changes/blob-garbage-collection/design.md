# Design: Blob Garbage Collection — Persisted Report + Gated Delete v1

## Technical Approach

Mark-and-sweep, split across the existing hexagonal layers with **one** candidate
computation shared by report and delete (proposal D1):

- **Sweep** — a new enumeration primitive on `ports.BlobStore`, implemented by
  `fsblob.Store` walking its own `blobsRoot` (`blobsRoot/<algorithm>/<hex>`, zero
  tenant/repo namespacing). `uploads/` is a different tree and is never walked.
- **Mark** — a new **global, tenant-less** query on `ports.MetadataStore`:
  `SELECT DISTINCT digest FROM manifest_blobs` (that table has no `tenant` column).
- **Diff + grace** — `internal/app/regixtry.Service` computes
  `sweep − mark − (mtime newer than now-24h)`.
- **Persist** — two new global tables (`gc_reports`, `gc_report_candidates`).
- **Delete** — recompute the same function, intersect with the stored report,
  unlink only the intersection, transition the report row to terminal `deleted`.

All nine proposal decisions (D1–D9) are binding inputs here, not open questions.

## Architecture Decisions

### Decision A: Enumeration lives on `ports.BlobStore`, not behind a type assertion

| Option | Tradeoff | Decision |
|---|---|---|
| Add `ListBlobs`/`DeleteBlob` to `ports.BlobStore` | Every future store (S3) must implement them; one existing test fake (`internal/infra/scanning/gitleaks/stage_test.go:15 fakeBlobStore`) needs two stub methods | **Chosen** |
| Separate optional `BlobEnumerator` interface + runtime type assertion | GC silently degrades to "0 candidates" against a store that doesn't implement it — a safe-but-invisible failure, and untestable through the port | Rejected |
| fsblob-only method, service depends on the concrete type | Breaks the app→ports dependency direction the whole package follows | Rejected |

**Rationale**: this is the first enumeration primitive `BlobStore` has ever needed,
and it is genuinely a blob-store capability, not a GC-specific side channel. A
compile-time contract is what makes the negative tests (a blob under `uploads/` is
never enumerated) expressible against the port rather than against one struct.
GC is meaningless for a store that cannot enumerate, so "every implementation must
provide it" is the correct constraint, not a burden.

### Decision B: `ListBlobs` returns a slice, not a walk callback

Consistent with every other list method in this codebase. The mark set is already
loaded whole (`[]string` of digests), so the memory profile is O(all blobs) either
way; a `WalkBlobs(ctx, func(...) error)` callback would introduce a new pattern for
no asymptotic win. Documented bound: ~100 bytes/entry, so 100k blobs ≈ 10 MB.

### Decision C: the mark query deliberately breaks the store's own tenant-first convention

`ListReferencedBlobDigests(ctx)` is the **only** `MetadataStore` method with no
`tenant` argument. That asymmetry is the safety property, and the interface doc
comment says so in blocking language (see Interfaces below). A tenant predicate
here — added directly, or smuggled in by joining `manifests` to reach one — shrinks
the mark set and unlinks blobs another tenant still references. That is silent,
irreversible, cross-tenant registry corruption. The signature having no `tenant`
parameter makes the bug *unrepresentable* rather than merely discouraged.

### Decision D: reports are global rows with no `tenant` column at all

`gc_reports` has `triggered_in_tenant` (provenance, never a predicate) and no
`tenant` column, so no `WHERE tenant = ?` can be copy-pasted onto it (D6). A report
describes the *deployment's* garbage, because a blob file is one shared
content-addressed file. `GetGCReport`/`MarkGCReportDeleted` take a report ID only.
**A future maintainer "fixing" this into tenant scoping would corrupt the mark set
and delete blobs another tenant still references.**

### Decision E: single-use is enforced in SQL, not only in Go

`MarkGCReportDeleted` issues `UPDATE gc_reports SET ... WHERE id = ? AND status =
'reported'`; zero rows affected is a typed `domain.ErrorCodeConflict`. The Go-level
status check in the service is a good error message; the `WHERE` clause is the
actual guard, and it also settles the two-concurrent-deletes race.

### Decision F: GC authorization is the admin route gate, not `s.authorize`

`ports.Action` requires a repository; GC has none. `handleAdmin`'s
`requireAdminPrincipal` (`admin_handlers.go:48`) already gates every admin route and
runs strictly **before** the service and therefore before the flag — which preserves
D1a's ordering (an unauthorized caller gets 401/403 and never learns the flag state).
Documented as the reason there is no `s.authorize` call on this path, so a later
reader does not read its absence as an oversight. Inside the service the flag is
checked **first**, before the report row is even read, so a flag-off deployment
performs zero reads and zero writes on the destructive path.

### Decision G: 24h grace is the real in-flight protection; 24h report TTL is hygiene

Two constants in the same block, each with a comment saying which is which (D3, D8).
Expiry is **not** the safety mechanism — D5's recompute-and-intersect is, and it can
only ever *under*-delete. Both docs and the constant comments must repeat this.

## Data Flow

    POST /admin/v1/gc/reports
      handleAdminGCReports ─→ Service.ComputeGCReport
                                 ├─ PruneExpiredGCReports(now)        [D8 hygiene]
                                 ├─ gcCandidates(now) ────┬─ BlobStore.ListBlobs        (sweep)
                                 │                        └─ MetadataStore.ListReferencedBlobDigests (mark, GLOBAL)
                                 └─ MetadataStore.CreateGCReport(report, candidates)   [one tx]
                                        └─→ 201 {report:{id,...}, candidates:[...]}

    POST /admin/v1/gc/reports/{id}/delete
      handleAdminGCReportResource ─→ Service.DeleteByGCReport(id)
                                 ├─ gcDeleteEnabled? ──no──→ ErrorCodeUnsupported → 501
                                 ├─ GetGCReport(id)  → status must be "reported", not expired
                                 ├─ gcCandidates(now)  ← SAME function, recomputed  [D5]
                                 ├─ intersect(report.candidates, fresh)             [D5]
                                 │     ├─ in both  → BlobStore.DeleteBlob → outcome deleted|missing|failed
                                 │     └─ report only → outcome "retained", file untouched
                                 └─ MarkGCReportDeleted(id, outcome)  WHERE status='reported'
                                        └─→ 200 terminal report + per-digest outcomes

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/ports/regixtry.go` | Modify | `BlobStore.ListBlobs`/`DeleteBlob`; `BlobFileInfo`; `MetadataStore.ListReferencedBlobDigests` + 4 report methods; `GCReport`, `GCReportCandidate`, `GCReportDetail`, `GCDeleteOutcome`, `GCCandidateOutcome`, status/outcome constants |
| `internal/infra/storage/fsblob/store.go` | Modify | `ListBlobs` (walks `s.blobsRoot()` only), `DeleteBlob` |
| `internal/infra/metadata/sqlite/store.go` | Modify | Two `CREATE TABLE IF NOT EXISTS` in `init()` (insert after the `repository_feature_overrides` statement at :1596-1604, before the trailing `ALTER TABLE` lines at :1605-1606); mark query; report CRUD |
| `internal/domain/regixtry/errors.go` | Modify | `ErrorCodeUnsupported ErrorCode = "UNSUPPORTED"` + `NewUnsupportedError(message string) error` |
| `internal/app/regixtry/service.go` | Modify | `gcDeleteEnabled bool` field + `gcGate *scanGate` (init in `NewService`), `SetGCDeleteEnabled` next to `SetDeleteEnabled` (:167) |
| `internal/app/regixtry/service_gc.go` | Create | Constants, `gcCandidates`, `ComputeGCReport`, `GetGCReport`, `DeleteByGCReport` |
| `internal/protocol/http/admin_handlers.go` | Modify | 2 switch cases in `handleAdmin` (:58-87), `handleAdminGCReports`, `handleAdminGCReportResource`, `ErrorCodeUnsupported → 501` in `writeAdminError` (:1284-1293) |
| `cmd/regixtry/main.go` | Modify | `serveConfig.GCDeleteEnabled` (after :265), `flags.BoolVar` (after :367), `service.SetGCDeleteEnabled` (after :2145) |
| `internal/infra/scanning/gitleaks/stage_test.go` | Modify | Two stub methods on `fakeBlobStore` (the only other `ports.BlobStore` implementation) |
| `internal/app/regixtry/service_gc_test.go`, `internal/infra/storage/fsblob/store_test.go`, `internal/infra/metadata/sqlite/store_test.go`, `internal/protocol/http/admin_gc_test.go` | Create/Modify | RED tests below |
| `docs/configuration.md`, `docs/registry.md`, `docs/roadmap.md` | Modify | Flag distinction, grace window, expiry-is-not-safety, audit trail, no-recovery warning |

## Interfaces / Contracts

### `internal/ports/regixtry.go`

```go
type BlobStore interface {
	// ... existing 7 methods unchanged ...

	// ListBlobs returns one entry per committed blob file in the store.
	// It is GLOBAL by construction: blob files are content-addressed with
	// zero tenant or repository namespacing (fsblob stores them at
	// blobsRoot/<algorithm>/<hex>), so one file is shared by every tenant
	// that pushed that content and there is nothing to scope by.
	//
	// Implementations MUST NOT enumerate in-flight uploads. Those live in a
	// separate tree (uploads/<id>/) and are not blobs until CommitUpload
	// renames one into blobsRoot; enumerating them would expose a
	// half-written push to the garbage collector.
	ListBlobs(ctx context.Context) ([]BlobFileInfo, error)

	// DeleteBlob unlinks exactly one committed blob file. It is idempotent:
	// an already-absent file returns (false, nil), never an error, because a
	// concurrent GC run or an operator having removed it is not a failure.
	// It MUST refuse to touch anything outside blobsRoot.
	DeleteBlob(ctx context.Context, digest domain.Digest) (removed bool, err error)
}

// BlobFileInfo is one blob file as it exists on disk right now. ModTime is
// the file's mtime, which is write time and not commit time -- the 24h grace
// window (gcGraceWindow) is sized to absorb that difference.
type BlobFileInfo struct {
	Digest  domain.Digest
	Size    int64
	ModTime time.Time
}
```

```go
type MetadataStore interface {
	// ... existing methods, all of which take tenant first ...

	// ListReferencedBlobDigests returns every distinct digest referenced by
	// ANY manifest in the deployment: SELECT DISTINCT digest FROM
	// manifest_blobs, with NO tenant predicate and no join that could
	// introduce one.
	//
	// !!! THIS METHOD TAKES NO tenant ARGUMENT, AND THAT IS DELIBERATE !!!
	// Every other method on this interface is tenant-scoped. This one must
	// never be. It is the mark set of a mark-and-sweep over the blob store,
	// and blob files carry no tenant namespacing at all: one file is shared
	// by every tenant that pushed that content. The manifest_blobs table has
	// no tenant column. Scoping this query -- directly, or by joining
	// manifests to reach a tenant -- shrinks the mark set, so blobs that
	// another tenant still references are reported as garbage and unlinked.
	// The result is silent, irreversible, cross-tenant registry corruption
	// with no recovery short of a re-push or a blobsRoot restore.
	ListReferencedBlobDigests(ctx context.Context) ([]string, error)

	// CreateGCReport inserts one gc_reports row plus its
	// gc_report_candidates children in a single transaction; a partially
	// written report is never observable. GLOBAL: no tenant argument.
	// report.TriggeredInTenant is provenance only and is never a predicate.
	CreateGCReport(ctx context.Context, report GCReport, candidates []GCReportCandidate) error

	// GetGCReport returns one report with its candidates in stored position
	// order. GLOBAL: a report created under one tenant's request context is
	// readable and usable from any other. An absent row is a typed
	// domain.ErrorCodeNotFound, mirroring DeleteUpload.
	GetGCReport(ctx context.Context, reportID string) (GCReportDetail, error)

	// MarkGCReportDeleted transitions one report from "reported" to the
	// terminal "deleted" state and writes each candidate's outcome, in one
	// transaction. The UPDATE carries WHERE id = ? AND status = 'reported':
	// zero rows affected is a typed domain.ErrorCodeConflict, which is what
	// makes a report single-use (D7) even under two concurrent deletes.
	MarkGCReportDeleted(ctx context.Context, reportID string, outcome GCDeleteOutcome) error

	// PruneExpiredGCReports deletes gc_reports rows still in "reported"
	// state whose expires_at <= now; children go with the ON DELETE CASCADE.
	// "deleted"-state rows are the audit trail of irreversible deletions and
	// are NEVER pruned. This is hygiene, not safety (D8) -- correctness is
	// the recompute-and-intersect in DeleteByGCReport.
	PruneExpiredGCReports(ctx context.Context, now time.Time) (int, error)
}

type GCReportStatus string

const (
	GCReportStatusReported GCReportStatus = "reported" // preview; usable once
	GCReportStatusDeleted  GCReportStatus = "deleted"  // terminal, frozen, never replayable
)

const (
	GCCandidateOutcomePending  = ""         // preview row, no delete attempted
	GCCandidateOutcomeDeleted  = "deleted"  // unlinked; counted in BytesReclaimed
	GCCandidateOutcomeRetained = "retained" // dropped by the D5 intersection: referenced again, or now inside the grace window
	GCCandidateOutcomeMissing  = "missing"  // file already gone; unlink was a no-op
	GCCandidateOutcomeFailed   = "failed"   // unlink errored; Error carries it
)

type GCReport struct {
	ID                string         `json:"id"`
	Status            GCReportStatus `json:"status"`
	ComputedAt        time.Time      `json:"computed_at"`
	ExpiresAt         time.Time      `json:"expires_at"`
	GraceCutoff       time.Time      `json:"grace_cutoff"`
	CandidateCount    int            `json:"candidate_count"`
	CandidateBytes    int64          `json:"candidate_bytes"`
	DurationMillis    int64          `json:"duration_ms"`
	RequestedBy       string         `json:"requested_by,omitempty"`
	// TriggeredInTenant is PROVENANCE ONLY. It is deliberately not named
	// "tenant": reports are global, and no query may ever filter on it.
	TriggeredInTenant string     `json:"triggered_in_tenant,omitempty"`
	DeletedAt         *time.Time `json:"deleted_at,omitempty"`
	DeletedBy         string     `json:"deleted_by,omitempty"`
	DeletedCount      int        `json:"deleted_count"`
	BytesReclaimed    int64      `json:"bytes_reclaimed"`
	Error             string     `json:"error,omitempty"`
}

type GCReportCandidate struct {
	Digest  string    `json:"digest"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mtime"`
	Outcome string    `json:"outcome,omitempty"`
	Error   string    `json:"error,omitempty"`
}

type GCReportDetail struct {
	Report     GCReport            `json:"report"`
	Candidates []GCReportCandidate `json:"candidates"`
}

type GCDeleteOutcome struct {
	DeletedAt      time.Time
	DeletedBy      string
	DeletedCount   int
	BytesReclaimed int64
	Error          string
	Candidates     []GCCandidateOutcome
}

type GCCandidateOutcome struct {
	Digest  string
	Outcome string
	Error   string
}
```

### SQLite schema — `store.init()` statements

Both are brand-new tables, so they follow the `CREATE TABLE IF NOT EXISTS`
pattern used by `scan_runs`/`scan_run_findings` (`store.go:1454-1495`), not the
`ALTER TABLE ... ADD COLUMN` migration pattern used for `pushed_by` (:1605).
Timestamps are `TEXT` in `time.RFC3339Nano` UTC, matching every other table.
`foreign_keys(1)` is already set in `sqliteDSN` (:42), so the cascade is live.

```sql
CREATE TABLE IF NOT EXISTS gc_reports (
	id TEXT PRIMARY KEY,
	status TEXT NOT NULL,
	computed_at TEXT NOT NULL,
	expires_at TEXT NOT NULL,
	grace_cutoff TEXT NOT NULL,
	candidate_count INTEGER NOT NULL DEFAULT 0,
	candidate_bytes INTEGER NOT NULL DEFAULT 0,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	requested_by TEXT NOT NULL DEFAULT '',
	triggered_in_tenant TEXT NOT NULL DEFAULT '',
	deleted_at TEXT,
	deleted_by TEXT NOT NULL DEFAULT '',
	deleted_count INTEGER NOT NULL DEFAULT 0,
	bytes_reclaimed INTEGER NOT NULL DEFAULT 0,
	error TEXT NOT NULL DEFAULT ''
);
```

```sql
CREATE TABLE IF NOT EXISTS gc_report_candidates (
	report_id TEXT NOT NULL,
	position INTEGER NOT NULL,
	digest TEXT NOT NULL,
	size INTEGER NOT NULL DEFAULT 0,
	mtime TEXT NOT NULL,
	outcome TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	PRIMARY KEY(report_id, position),
	FOREIGN KEY(report_id) REFERENCES gc_reports(id) ON DELETE CASCADE
);
```

`gc_reports` has **no `tenant` column** — this is load-bearing (Decision D). No
index on `gc_report_candidates(digest)`: every read is by `report_id`, which is the
primary-key prefix.

Statement sketches:

```sql
-- mark
SELECT DISTINCT digest FROM manifest_blobs;
-- prune (hygiene; audit rows survive)
DELETE FROM gc_reports WHERE status = 'reported' AND expires_at <= ?;
-- terminal transition, single-use guard
UPDATE gc_reports SET status='deleted', deleted_at=?, deleted_by=?, deleted_count=?,
       bytes_reclaimed=?, error=? WHERE id = ? AND status = 'reported';
UPDATE gc_report_candidates SET outcome=?, error=? WHERE report_id=? AND digest=?;
```

### `internal/app/regixtry/service_gc.go`

```go
// gcGraceWindow (D3) is the ONLY thing standing between a blob written by an
// in-flight push and an irreversible unlink: a push writes blobs first and
// publishes the manifest last, so between CommitUpload and PublishManifest a
// perfectly live blob is unreferenced. It is a fixed constant on purpose --
// it must never become settable to 0.
const gcGraceWindow = 24 * time.Hour

// gcReportTTL (D8) is HYGIENE, NOT SAFETY. It stops months-old reports from
// being submitted and stops preview rows accumulating. The safety mechanism
// is DeleteByGCReport's recompute-and-intersect, which can only under-delete.
const gcReportTTL = 24 * time.Hour

// gcCandidates is THE mark-and-sweep computation. Report and delete both call
// it; there is deliberately no second implementation that could drift, which
// is what makes the report a faithful preview by construction (D1).
func (s *Service) gcCandidates(ctx context.Context, now time.Time) ([]ports.GCReportCandidate, time.Time, error)

func (s *Service) ComputeGCReport(ctx context.Context) (ports.GCReportDetail, error)
func (s *Service) GetGCReport(ctx context.Context, reportID string) (ports.GCReportDetail, error)
func (s *Service) DeleteByGCReport(ctx context.Context, reportID string) (ports.GCReportDetail, error)

// SetGCDeleteEnabled mirrors SetDeleteEnabled (service.go:167) exactly: a
// low-churn setter rather than a constructor argument. It gates ONLY
// DeleteByGCReport and is a DIFFERENT flag from deleteEnabled --
// REGISTRY_DELETE_ENABLED is metadata-only and never touches blob files.
func (s *Service) SetGCDeleteEnabled(enabled bool)
```

`gcCandidates`: `cutoff := now.Add(-gcGraceWindow)`; `ListBlobs`; `ListReferencedBlobDigests`
into a `map[string]struct{}`; keep a blob iff it is unmarked **and**
`info.ModTime.Before(cutoff)`; sort by digest for deterministic `position`.

`ComputeGCReport`: acquire `s.gcGate` (max 1, else `domain.NewConflictError`) →
`PruneExpiredGCReports(now)` → `gcCandidates` → build `GCReport{ID: uuid.NewString(),
Status: reported, ComputedAt: now, ExpiresAt: now.Add(gcReportTTL), GraceCutoff,
DurationMillis, RequestedBy: principal.UserID via ports.PrincipalFromContext (the
`pushed_by` precedent, `service.go:277`), TriggeredInTenant: s.tenant(ctx)}` →
`CreateGCReport`.

`DeleteByGCReport` order: **flag first** (Decision F) → `GetGCReport` → status must
be `reported` (else conflict) → `now.Before(ExpiresAt)` (else validation) → gate →
recompute → intersect → per-digest `DeleteBlob` → `MarkGCReportDeleted`. Unlink
failures are recorded per digest and do not abort the run; the report still reaches
`deleted` with `Error` summarising (`"3 of 120 unlinks failed"`).

### HTTP — `internal/protocol/http/admin_handlers.go`

Dispatch, added to the `handleAdmin` switch (exact match before prefix, per the
file's documented ordering convention):

```go
case subpath == "gc/reports":
	r.handleAdminGCReports(w, req)
case strings.HasPrefix(subpath, "gc/reports/"):
	r.handleAdminGCReportResource(w, req, strings.TrimPrefix(subpath, "gc/reports/"))
```

`handleAdminGCReportResource` splits with `id, action, hasAction := strings.Cut(resource, "/")`:
`!hasAction` → `GET` → `GetGCReport`; `action == "delete"` → `POST` → `DeleteByGCReport`;
anything else → 404. The repository-name ordering hazard documented at
`handleAdminFeatureResource` does **not** apply: report IDs are opaque UUIDs and
cannot contain a slash.

| Route | Method | Request body | Success |
|---|---|---|---|
| `/admin/v1/gc/reports` | POST | none (grace and TTL are constants; nothing is caller-tunable) | `201` + `GCReportDetail` |
| `/admin/v1/gc/reports/{id}` | GET | — | `200` + `GCReportDetail` |
| `/admin/v1/gc/reports/{id}/delete` | POST | none — the report ID in the path **is** the confirmation (D5) | `200` + terminal `GCReportDetail` |

Rejected: a `{"report_id": "..."}` body on delete (redundant with the path, invites
a mismatch) and a client-echoed candidate list (the server would be trusting the
caller's own list — exactly what D6 exists to prevent).

Response (report state):

```json
{
  "report": {"id":"5f3c...","status":"reported","computed_at":"2026-08-16T09:00:00Z",
             "expires_at":"2026-08-17T09:00:00Z","grace_cutoff":"2026-08-15T09:00:00Z",
             "candidate_count":2,"candidate_bytes":8192,"duration_ms":37,
             "requested_by":"usr_01","triggered_in_tenant":"default",
             "deleted_count":0,"bytes_reclaimed":0},
  "candidates":[{"digest":"sha256:aa..","size":4096,"mtime":"2026-08-01T10:00:00Z"}]
}
```

After delete the report carries `"status":"deleted"`, `deleted_at`, `deleted_by`,
`deleted_count`, `bytes_reclaimed`, and each candidate carries
`"outcome":"deleted"|"retained"|"missing"|"failed"` (+ `error`).

`writeAdminError` (:1284) gains one case in the `domainregistry.Error` switch:

```go
case domainregistry.ErrorCodeUnsupported:
	status = stdhttp.StatusNotImplemented
```

Body stays `{"error": message}` with **no code field** — admin routes have no OCI
code vocabulary (D9), so the message is the only disambiguator and must read
`blob garbage collection delete is disabled (REGISTRY_GC_DELETE_ENABLED)`.
Constraint: `ErrorCodeUnsupported` is admin-only; no registry (`/v2/`) route may
return it, and `writeError`'s OCI mapping is not touched.

### `cmd/regixtry/main.go`

```go
// serveConfig, after DeleteEnabled (:265)
GCDeleteEnabled bool

// after :367, mirroring the -delete-enabled line exactly
flags.BoolVar(&cfg.GCDeleteEnabled, "gc-delete-enabled",
	parseBoolEnv("REGISTRY_GC_DELETE_ENABLED", false),
	"enable POST /admin/v1/gc/reports/{id}/delete (irreversibly unlinks unreferenced blob files; distinct from -delete-enabled, which is metadata-only)")

// after :2145
service.SetGCDeleteEnabled(cfg.GCDeleteEnabled)
```

The two flags must never read each other's env var — pinned by a test.

## Testing Strategy

Strict TDD. The negative tests are the primary artifact; **T1, T2 and T6 must be
written and RED before a single line of unlink code exists.**

| # | Test | File | Proves |
|---|---|---|---|
| T1 | `TestDeleteByGCReportNeverUnlinksBlobReferencedByAManifest` | `internal/app/regixtry/service_gc_test.go` | Report a candidate `X` while unreferenced, then publish a manifest referencing `X`, then delete: `BlobExists(X)` still true, candidate outcome `retained`, `bytes_reclaimed` excludes it. Structurally impossible under D5 — proven, not assumed |
| T2 | `TestGCRespectsBlobsReferencedOnlyByAnotherTenant` | same | Tenant B publishes a manifest referencing `X`; compute + delete under tenant A's context: `X` never appears as a candidate and the file survives. Fails the instant anyone tenant-scopes the mark query |
| T3 | `TestComputeGCReportExcludesBlobInsideGraceWindow` + fsblob `TestListBlobsReportsMtime` | service + `fsblob/store_test.go` | `CommitUpload` with no manifest → 0 candidates; advance `s.now` past `gcGraceWindow` (and separately `os.Chtimes` backdating at the fsblob layer) → 1 candidate |
| T4 | `TestDeleteByGCReportRefusesWhenFlagOff` + `TestAdminGCDeleteReturns501NamingEnvVar` | service + `internal/protocol/http/admin_gc_test.go` | Flag off: `501`, body message contains `REGISTRY_GC_DELETE_ENABLED`, report still `reported`, **every blob still on disk**; the report endpoint still works |
| T5 | `TestDeleteByGCReportRejectsAlreadyUsedReport` + `TestMarkGCReportDeletedRejectsNonReportedRow` | service + `sqlite/store_test.go` | Second delete → conflict; the store-level test proves the guard is the SQL `WHERE status='reported'`, not just the Go check |
| T6 | `TestListBlobsNeverEnumeratesUploads` / `TestDeleteBlobNeverTouchesUploads` | `fsblob/store_test.go` | An in-flight `BeginUpload`+`PutUploadChunk` is invisible to `ListBlobs`, and no `DeleteBlob` call can remove a path under `uploads/` |
| T7 | `TestListReferencedBlobDigestsIsGlobalAcrossTenants` | `sqlite/store_test.go` | Two tenants sharing a digest → exactly one row; signature has no `tenant` parameter (compile-time) |
| T8 | `TestGCReportIsReadableAndUsableFromAnotherTenantContext` | service | Reports are global (D6) |
| T9 | `TestPruneRemovesExpiredReportedReportsButKeepsDeletedOnes` + `TestDeleteByGCReportRejectsExpiredReport` | sqlite + service | D8 hygiene; audit rows retained forever |
| T10 | `TestDeleteByGCReportRecordsPerDigestFailureAndPartialReclaim` | service, fake `ports.BlobStore` injected via `NewService` | One unlink errors → outcomes mix `deleted`/`failed`, `bytes_reclaimed` counts only successes, report still reaches terminal state |
| T11 | `TestGCDeleteFlagIsIndependentOfDeleteEnabled` | `cmd/regixtry` | `REGISTRY_DELETE_ENABLED=true` alone leaves `gcDeleteEnabled` false |
| T12 | `TestAdminGCRoutesRequireAdminPrincipal` | `internal/protocol/http` | Unauthenticated/non-admin → 401/403 before the flag is consulted (D1a; no flag-state disclosure) |

Existing `newTestService` (real `fsblob` + real sqlite in `t.TempDir()`) is the right
harness for T1–T5, T8, T9. T10 needs a fake `ports.BlobStore`.

## Threat Matrix

The change adds admin HTTP routes and an irreversible filesystem unlink, so the
routing boundary applies; the git/PR rows do not.

| Boundary | Applicability | Design response | Planned RED tests |
|---|---|---|---|
| Documentation-like paths / executable classification | N/A — no file is classified or executed; blobs are opaque content-addressed bytes | — | — |
| Git repository selection | N/A — no VCS interaction | — | — |
| Commit state | N/A — no VCS interaction | — | — |
| Push state | N/A — no VCS interaction | — | — |
| PR commands | N/A — no subprocess or PR automation | — | — |
| **Admin path dispatch** (`gc/reports`, `gc/reports/{id}`, `gc/reports/{id}/delete`) | Applicable | Exact match before prefix; opaque UUID IDs cannot contain a slash; unknown sub-action → 404; wrong method → 405 with `Allow` | T12 + a dispatch test for `gc/reports/{id}/bogus` → 404 and `GET .../delete` → 405 |
| **Filesystem unlink scope** | Applicable | `DeleteBlob` resolves only through `blobPath` under `blobsRoot`; `uploads/` is never walked or unlinked; digest is validated before any path is built | T6 + a `DeleteBlob` test with a traversal-shaped digest string rejected by `domain.ParseDigest`/`Validate` before touching the FS |
| **Destructive capability gate** | Applicable | Admin gate → flag → report state → expiry → recompute+intersect, in that order; flag default false | T4, T12 |

## Migration / Rollout

Additive `CREATE TABLE IF NOT EXISTS` only; no data migration, no backfill, no
existing column touched. Rollout: ship with `REGISTRY_GC_DELETE_ENABLED` unset
(false) — the report path alone gives operators visibility. Rollback: unset the flag
and restart. **Never drop `gc_reports`/`gc_report_candidates`** — they hold the only
record of irreversible deletions.

## Review Budget

**Confirmed High.** This design's concrete surface (2 port methods + 5 store methods
+ 6 types, 2 tables, transactional CRUD, 4 service methods, 2 handlers + a
`writeAdminError` case, a domain error code, main.go wiring, docs, and 12
negative-heavy test groups) lands at an estimated **1,150–1,500 changed lines**,
above the proposal's 900–1,400 and well past the 800-line budget.

Natural chained split (also the right adversarial-review boundary):

- **Slice 1 — non-destructive, ~600–750 lines.** `BlobFileInfo` + `ListBlobs`
  (fsblob), `ListReferencedBlobDigests`, GC types, both tables, `CreateGCReport` /
  `GetGCReport` / `PruneExpiredGCReports`, `gcCandidates` + `ComputeGCReport` +
  `GetGCReport`, `POST /gc/reports` + `GET /gc/reports/{id}`, tests T2, T3, T6, T7,
  T8, T9(prune half), T12. **`DeleteBlob` is not on the interface yet — this slice
  contains zero unlink code.**
- **Slice 2 — destructive, ~450–650 lines.** `DeleteBlob`, `MarkGCReportDeleted`,
  `ErrorCodeUnsupported` + 501 mapping, `gcDeleteEnabled` + `SetGCDeleteEnabled`,
  main.go flag, `DeleteByGCReport`, delete route, tests T1, T4, T5, T9(expiry half),
  T10, T11.

`Decision needed before apply: Yes` · `Chained PRs recommended: Yes` ·
`800-line budget risk: High`

## Open Questions

- None blocking. Two executor-level calls made here and flagged for correction:
  `POST /gc/reports` answers `201` (a durable resource now exists) rather than `202`;
  the flag is checked before the report row is read, so a flag-off deployment
  performs no reads or writes at all on the destructive path.
