# Design: OCI 1.1 Referrers API (oci-referrers-api)

## Technical Approach

One new read route, threaded through the four layers push already uses, plus one indexed column and one
migration. The proposal's four resolved decisions (backfill yes; legacy cosign stays separate; orphaned
referrers are accepted debt; `artifactType` derived at query time) are inputs here, not revisited.

1. **Domain** — `Manifest.ArtifactType`, populated by `NewManifest`. Raw value only; no fallback.
2. **App parse** — `manifestEnvelope.ArtifactType`, threaded by `parseManifestPayload` (`service.go:392-439`).
3. **Store** — `manifests.subject_digest`, written by `PublishManifest`, read by `ListReferrers`; one
   marker-gated backfill.
4. **App query** — `Service.Referrers` authorizes `ActionInspect`, maps rows to descriptors, applies the
   `config.mediaType` fallback and the `artifactType` filter.
5. **Router** — `handleReferrers`, parallel to `handleTags`.

Every line reference below was read from the working tree.

## Architecture Decisions

### Decision 1 — Backfill marker: a narrow `schema_backfills` table, written in the backfill's own transaction

| Option | Tradeoff | Decision |
|---|---|---|
| `schema_backfills(name TEXT PRIMARY KEY, completed_at TEXT NOT NULL)`; guard is a PK point lookup | One new 2-column table | **Chosen** |
| Nullable `subject_digest`, backfill `WHERE subject_digest IS NULL`, data is its own marker | Needs no table, but makes the column tri-state for every reader **and** invalidates the proposal's rollback argument, which rests on `NOT NULL DEFAULT ''` being inert to reverted binaries | Rejected |
| No marker; re-run `WHERE subject_digest = ''` each boot | `''` is the legitimate value for every non-referrer manifest, so this re-parses almost the whole table on every boot — exactly the proposal's Med risk | Rejected |
| General `schema_meta(key,value)` bag | Invites reuse as a config store and implies a version counter this repo does not have | Rejected |

```sql
CREATE TABLE IF NOT EXISTS schema_backfills (
    name TEXT PRIMARY KEY,
    completed_at TEXT NOT NULL
);
```

Appended to `init()`'s flat statement list (`store.go:1674-1959`) — `CREATE TABLE IF NOT EXISTS` needs no new
error tolerance beyond the existing `"duplicate column name"` arm. Marker name: `manifests.subject_digest.v1`.

The marker **cannot drift** because the row updates and the `INSERT OR IGNORE` into `schema_backfills` commit
in **one transaction**: either the whole backfill landed or none of it did. That atomicity, not the absence of
a migration framework, is the reason to prefer a marker row over a stateless guard.

Runs from `New()` (`store.go:25-38`) after `init()`, before the store is returned, so no request can observe a
half-migrated table. Concurrent openers (`serve` and `tui` both call `New`) are serialised by that write
transaction; the loser either blocks or recomputes identical, deterministic values.

### Decision 2 — Yes to an index, but a **partial** one, and the query must name the partial predicate

```sql
CREATE INDEX IF NOT EXISTS idx_manifests_subject
    ON manifests(tenant, repository_id, subject_digest, digest)
    WHERE subject_digest != '';
```

| Option | Tradeoff | Decision |
|---|---|---|
| Partial index on `(tenant, repository_id, subject_digest, digest)` | Indexes only referrer rows — a small minority — so ordinary image pushes pay ~zero write amplification, and `digest` as the 4th column makes Decision 4's `ORDER BY` index-ordered | **Chosen** |
| Full index on the same columns | Every image push writes an entry whose `subject_digest` is `''`; the index is mostly dead weight for a value never queried | Rejected |
| No index (premature optimization) | Defensible today: `UNIQUE(tenant, repository_id, digest)` already narrows to one repository, so the residual filter is O(manifests in that repo). But a partial index removes the usual reason to defer — write cost — and adding it later costs the same one statement | Rejected |

**Load-bearing detail:** SQLite uses a partial index only when the query's `WHERE` clause *provably implies*
the index's. `subject_digest = ?` with a bound parameter proves nothing. `ListReferrers` therefore repeats the
predicate literally — `AND m.subject_digest != ''` — which is semantically free (we never query for `''`) and
is what makes the index reachable at all.

### Decision 3 — `ports.ReferrerRow`, and the payload→descriptor mapping lives in `queries.go`

| Option | Tradeoff | Decision |
|---|---|---|
| `[]ports.ReferrerRow{Digest, MediaType, Size, Payload}` | Explicit, honest, matches the existing `ports.TagSummary`/`ports.RepositorySummary` DTO precedent | **Chosen** |
| `[]domain.Manifest` | The store would have to hand back Manifests with `nil` Config/Annotations/ArtifactType — a lying domain object — or parse OCI JSON in the adapter | Rejected |
| `[][]byte` payloads only | `Digest`/`Size` are derivable, but `media_type` is a **stored column** set from the push `Content-Type` and may differ from the payload's own `mediaType` | Rejected |

```go
// internal/ports/regixtry.go
// ReferrerRow is one manifests row matched by subject_digest. It is a row,
// not a domain object: the SQLite adapter never parses OCI JSON, so Payload
// is returned verbatim and the app layer derives artifactType/annotations.
type ReferrerRow struct {
    Digest    string
    MediaType string
    Size      int64
    Payload   []byte
}

// ListReferrers returns every manifest in ONE tenant's ONE repository whose
// subject.digest equals subjectDigest, ordered by digest ASC. Scoped exactly
// like ResolveManifest/ListTags (m.tenant AND r.tenant AND r.name) and
// explicitly NOT like ListReferencedBlobDigests, which is global by design.
// An absent subject digest is an empty slice, never a not-found error.
ListReferrers(ctx context.Context, tenant string, repository domain.RepositoryRef, subjectDigest domain.Digest) ([]ReferrerRow, error)
```

Taking `domain.Digest` (not `string`) makes an unvalidated digest unrepresentable at the port boundary,
matching `DeleteManifestByDigest`.

The mapping is one unexported function in `internal/app/regixtry/queries.go`, which is in the **same package**
as `manifestEnvelope`/`parseManifestPayload` — so no type is exported and no parser is duplicated. It reuses
`parseManifestPayload(row.Digest, row.MediaType, row.Payload)` verbatim, passing the stored digest as the
reference: the digest-mismatch check is then free, and a payload that fails to parse or mismatches is storage
corruption that surfaces as `500`, never a silently dropped referrer.

The `config.mediaType` fallback is applied **here, when building the descriptor** — not in `NewManifest`, not
at write time (proposal Decision 4).

```go
// internal/app/regixtry/queries.go
const ociImageIndexMediaType = "application/vnd.oci.image.index.v1+json"

type ReferrersIndex struct {
    SchemaVersion int                  `json:"schemaVersion"` // always 2
    MediaType     string               `json:"mediaType"`     // ociImageIndexMediaType
    Manifests     []ReferrerDescriptor `json:"manifests"`     // NEVER nil — see below
}

type ReferrerDescriptor struct {
    MediaType    string            `json:"mediaType"`
    Digest       string            `json:"digest"`
    Size         int64             `json:"size"`
    ArtifactType string            `json:"artifactType,omitempty"`
    Annotations  map[string]string `json:"annotations,omitempty"`
}

// resolveArtifactType is the OCI fallback, isolated as a pure function so it
// is unit-testable without a store: the manifest's own artifactType wins;
// otherwise config.mediaType; otherwise "".
func resolveArtifactType(manifest domain.Manifest) string {
    if manifest.ArtifactType != "" {
        return manifest.ArtifactType
    }
    if manifest.Config != nil {
        return manifest.Config.MediaType
    }
    return ""
}
```

`Manifests` MUST be built with `make([]ReferrerDescriptor, 0, len(rows))`. A `nil` slice encodes as
`"manifests": null`, which fails the spec's empty-list requirement — this is the single most likely way to
ship a green unit test and a non-conformant wire response.

`NewManifest` gains an `artifactType` parameter rather than a post-construction assignment like `PushedBy`:
`artifactType` is content (it lives inside the digested payload), and the compile error at all 18 call sites is
the enforcement that no construction path silently drops it. `ResolveManifest` (`store.go:319`) passes `""`,
alongside the `nil` it already passes for subject.

### Decision 4 — `ORDER BY m.digest ASC`

The spec mandates no order. Digest ordering is total, content-addressed, stable under repush (`ON CONFLICT DO
UPDATE` does not touch `digest`), identical for backfilled and freshly pushed rows, and covered by the
Decision 2 index's 4th column. Rejected: `created_at DESC` (more human-friendly, but the backfill and repush
make "newest" a weaker guarantee than it looks, and ties are asserted awkwardly) and insertion order (`id`, an
implementation detail leaking onto the wire).

### Decision 5 — `ResolveManifest` stays untouched; `Subject` keeps returning `nil`

| Option | Tradeoff | Decision |
|---|---|---|
| Leave `nil`; add a comment recording *why*, pointing at `subject_digest` | Zero blast radius, zero pull-path cost | **Chosen** |
| Parse the payload in `ResolveManifest` to populate `Subject` | `ResolveManifest` is the hot pull path (`OpenManifest`, 6 port callers, 11 service callers) — this adds a JSON parse to **every manifest GET/HEAD**, for a field nothing in this change reads | Rejected |
| Reconstruct `Subject` from the new column | **Impossible, and that is the real answer:** the column stores only the digest, while `Descriptor.Validate()` requires `mediaType` and `size`. The index is deliberately not a descriptor | Rejected |

`ListReferrers` does its own parse over a small matched set; it never routes through `ResolveManifest`. The
comment is the deliverable, so the next reader does not conclude subject data is unavailable.

### Decision 6 — `/referrers/` is appended **last** to `splitRepositoryPath`'s markers

```go
markers := []string{"/blobs/uploads/", "/blobs/uploads", "/blobs/", "/manifests/", "/tags/list", "/referrers/"}
```

Position is load-bearing, and this is the `/scan-status` reasoning (`router.go:163-186`) applied to the
repository half of the path instead of the reference half. `splitRepositoryPath` returns on the **first**
marker found, so if `/referrers/` were listed before `/manifests/`, the path `library/referrers/manifests/latest`
(repository literally named `library/referrers`) would split as repository `library`, suffix
`referrers/manifests/latest` — a misroute. Listed last, `/manifests/` matches first and a repository named
`referrers` keeps working for manifests, tags, and blobs.

The one residual case — `library/referrers/referrers/sha256:…` — hits `strings.Index`'s *first* occurrence and
splits as repository `library`, subject digest `referrers/sha256:…`. That **fails closed** with
`400 DIGEST_INVALID`; no cross-repository data is returned. It is a pre-existing property of the first-match
marker scan, not new: `library/manifests/manifests/latest` misroutes identically today and fails closed with
`404`. Fixing it would mean switching every marker to `strings.LastIndex`, changing existing blob/manifest
routing — out of scope, recorded as a follow-up.

`GET /v2/<name>/referrers` with no digest matches no marker and stays `404 NAME_UNKNOWN` (today's default).
`GET /v2/<name>/referrers/` yields an empty digest → `400 DIGEST_INVALID`.

### Decision 7 — authorization before digest parsing; the filter header follows "actually applied"

`Referrers` orders exactly like `Tags`/`DeleteManifest`: `parseRepository` → `authorize(ActionInspect)` →
`ParseDigest` → store. An unauthorized caller therefore cannot probe digest validation, matching
`manifest-blob-delete` Decision 2's capability-disclosure rule.

`?artifactType=` is trimmed; **an empty or whitespace-only value is no filter and sets no header**. The
proposal's wording is "set only when a filter was actually applied", and clients that append `?artifactType=`
unconditionally must not silently receive an empty list.

Pagination (`n`/`last`) is deliberately **not** implemented — the spec delta requires none, and a registry that
returns the full list is conformant. `handleReferrers` therefore does not call `parsePagination`. Recorded as a
follow-up.

## Interfaces / Contracts

```
GET  /v2/<name>/referrers/<digest>                  200  Content-Type: application/vnd.oci.image.index.v1+json
                                                         {"schemaVersion":2,"mediaType":"…image.index.v1+json","manifests":[…]}
GET  /v2/<name>/referrers/<digest>?artifactType=X   200  + OCI-Filters-Applied: artifactType
     (unknown/deleted/never-pushed digest)          200  {"…","manifests":[]}          never 404
     (unparseable digest)                           400  DIGEST_INVALID
     (unauthorized / anonymous where required)      401  UNAUTHORIZED + WWW-Authenticate
<any other method>                                  405  Allow: GET
```

```go
// internal/protocol/http/router.go — handleV2, appended after `case suffix == "tags/list":`
case strings.HasPrefix(suffix, "referrers/"):
    r.handleReferrers(w, req, repository, strings.TrimPrefix(suffix, "referrers/"))

// handleReferrers is the OCI 1.1 Referrers read, parallel to handleTags: a
// list-shaped view of already-stored content, so ActionInspect, not ActionPull.
func (r *Router) handleReferrers(w stdhttp.ResponseWriter, req *stdhttp.Request, repository string, subjectDigest string) {
    action := ports.Action{Verb: ports.ActionInspect, Repository: repository}
    if req.Method != stdhttp.MethodGet {
        w.Header().Set("Allow", stdhttp.MethodGet)
        w.WriteHeader(stdhttp.StatusMethodNotAllowed)
        return
    }

    req, ok := r.withPrincipal(w, req, action)
    if !ok {
        return
    }

    artifactType := strings.TrimSpace(req.URL.Query().Get("artifactType"))

    index, err := r.service.Referrers(req.Context(), repository, subjectDigest, artifactType)
    if err != nil {
        // Mirrors handleManifest's DELETE arm: an unparseable digest is
        // DIGEST_INVALID/400, an unknown repository stays NAME_UNKNOWN.
        defaultCode := "NAME_UNKNOWN"
        if domain.IsCode(err, domain.ErrorCodeInvalidDigest) {
            defaultCode = "DIGEST_INVALID"
        }
        writeError(w, req, err, r.challengeForError(action, err), defaultCode)
        return
    }

    // Set only on the success path: an error response must not claim a filter
    // was applied.
    if artifactType != "" {
        w.Header().Set("OCI-Filters-Applied", "artifactType")
    }

    writeJSONAs(w, stdhttp.StatusOK, ociImageIndexMediaType, index)
}
```

`writeError` already maps `domain.ErrorCodeInvalidDigest` to `400` (`router.go:708-712`), so no new error path
is added. `writeJSON` (`router.go:747-751`) hardcodes `application/json` and cannot be reused as-is, because
Content-Type must be set before `WriteHeader`. Split it, with no behavior change to existing callers:

```go
func writeJSON(w stdhttp.ResponseWriter, status int, payload any) {
    writeJSONAs(w, status, "application/json", payload)
}

func writeJSONAs(w stdhttp.ResponseWriter, status int, contentType string, payload any) {
    w.Header().Set("Content-Type", contentType)
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(payload)
}
```

```go
// internal/app/regixtry/queries.go — shape follows Tags (queries.go:556-572)
func (s *Service) Referrers(ctx context.Context, repositoryName string, subjectDigest string, artifactType string) (ReferrersIndex, error)
```

```sql
-- internal/infra/metadata/sqlite/store.go — ListReferrers
SELECT m.digest, m.media_type, m.size, m.payload
FROM manifests m
JOIN repositories r ON r.id = m.repository_id
WHERE m.tenant = ? AND r.tenant = ? AND r.name = ?     -- ListTags' exact three predicates
  AND m.subject_digest = ?
  AND m.subject_digest != ''                            -- makes the partial index reachable (Decision 2)
ORDER BY m.digest ASC;
```

```sql
-- init() additions
ALTER TABLE manifests ADD COLUMN subject_digest TEXT NOT NULL DEFAULT '';
-- plus the matching column inside the CREATE TABLE manifests block, and the
-- Decision 2 partial index and Decision 1 schema_backfills table.
```

`PublishManifest` (`store.go:234-241`) adds `subject_digest` to the column list **and** to
`ON CONFLICT DO UPDATE SET`, beside `payload` — unlike `pushed_by`, which is deliberately insert-only. A repush
that changes the payload changes the subject, so the two must move together or the index lies.

## Data Flow

    GET /v2/library/app/referrers/sha256:abc…?artifactType=application/spdx+json
      splitRepositoryPath  ["/manifests/" tried before "/referrers/"]  -> repo "library/app", suffix "referrers/sha256:abc…"
        handleReferrers   action.Verb = ActionInspect
          -> withPrincipal                          [401 on a bad token]
          -> Service.Referrers(repo, "sha256:abc…", "application/spdx+json")
               parseRepository
               authorize(ActionInspect)             [401 + challenge; BEFORE digest parsing]
               domain.ParseDigest                   [400 DIGEST_INVALID on failure]
               store.ListReferrers(tenant, repo, digest)
                     WHERE m.tenant AND r.tenant AND r.name AND subject_digest  ORDER BY digest ASC
                     -> []ports.ReferrerRow{digest, mediaType, size, payload}   [payload NOT parsed here]
               for each row: parseManifestPayload -> domain.Manifest
                             resolveArtifactType(manifest)   [manifest.ArtifactType else config.mediaType]
                             ReferrerDescriptor{…, Annotations: manifest.Annotations}
               filter on the resolved artifactType
          -> OCI-Filters-Applied: artifactType      [success path only]
          -> 200 application/vnd.oci.image.index.v1+json  {"schemaVersion":2,…,"manifests":[…]}

    Boot:  New() -> init()  [CREATE/ALTER/INDEX, "duplicate column name" tolerated]
                 -> backfillSubjectDigests()
                      SELECT 1 FROM schema_backfills WHERE name = 'manifests.subject_digest.v1'  -> present? return
                      BEGIN
                        SELECT id, payload FROM manifests WHERE subject_digest = ''
                          per row: probe {"subject":{"digest":…}} -> ParseDigest -> keep (id, digest)
                          [payloads are parsed in-loop; only (id, digest) pairs are retained, and the
                           cursor is closed BEFORE any UPDATE — never UPDATE while iterating the same table]
                        UPDATE manifests SET subject_digest = ? WHERE id = ?   [non-empty results only]
                        INSERT OR IGNORE INTO schema_backfills VALUES ('manifests.subject_digest.v1', now)
                      COMMIT                      [rows + marker atomic — the marker cannot drift]

The backfill's extraction is a **migration-local field probe** (`struct{ Subject *struct{ Digest string } }`),
not an OCI manifest parser: its only contract is the JSON path `subject.digest`, fixed by the OCI spec. This is
the one place OCI bytes are read inside the adapter, and it is justified because a migration is about the
schema's own history, not a query path — the proposal's "adapter stays dumb" constraint binds `ListReferrers`,
which honours it exactly. A row whose payload fails to unmarshal, or whose subject digest fails
`domain.ParseDigest`, is left at `''` and the boot continues: an unstartable registry is a worse failure than
one unindexed row, and `''` is exactly the pre-change behavior. Push-time validation means such a row can only
exist through storage corruption.

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/domain/regixtry/manifest.go` | Modify | `Manifest.ArtifactType`; `NewManifest` parameter (18 call sites) |
| `internal/domain/regixtry/descriptor.go` | Unchanged | Deliberately not widened — proposal Decision 4 |
| `internal/app/regixtry/service.go` | Modify | `manifestEnvelope.ArtifactType`; `parseManifestPayload` threads it |
| `internal/app/regixtry/queries.go` | Modify | `ReferrersIndex`, `ReferrerDescriptor`, `resolveArtifactType`, `Service.Referrers` |
| `internal/ports/regixtry.go` | Modify | `ReferrerRow`; `MetadataStore.ListReferrers` |
| `internal/infra/metadata/sqlite/store.go` | Modify | Column + `ALTER TABLE`; partial index; `schema_backfills`; `backfillSubjectDigests`; `PublishManifest` insert/update; `ListReferrers`; `ResolveManifest` comment + `""` artifactType |
| `internal/protocol/http/router.go` | Modify | `/referrers/` marker (last); `handleV2` case; `handleReferrers`; `writeJSONAs` split |
| `README.md` | Modify | Referrers endpoint; `provenance: false` note retained verbatim |
| `internal/domain/signing`, `service_gc.go`, `internal/tui` | Unchanged | Proposal non-goals |

## Testing Strategy

Strict TDD. `handleV2`'s dispatch switch and `splitRepositoryPath` have **zero** covering tests (confirmed via
codegraph blast radius). **Sequencing requirement for `sdd-tasks`, mirroring `manifest-blob-delete/design.md`:**
the characterization phase MUST land and pass before any behavior change.

| Phase | Layer | What to Test |
|---|---|---|
| **0 (characterization, no behavior change)** | Unit | `splitRepositoryPath` table test pinning today's five-marker outcomes, including `library/referrers/manifests/latest` and `library/manifests/manifests/latest` |
| **0** | Integration | `handleV2` dispatch table pinning today's routing: `blobs/uploads`, `blobs/`, `manifests/<ref>`, `/scan-status`, `/signature-status`, `/secret-scan-status`, `tags/list`, `_catalog`, and `referrers/<digest>` → **404 NAME_UNKNOWN** |
| **0** | Unit | `writeJSON` sets `Content-Type: application/json` (pinned before the `writeJSONAs` split) |
| 1 | Unit | `NewManifest` carries `artifactType`; absent → `""`, never a fallback at this layer |
| 2 | Unit | `parseManifestPayload` threads `artifactType`; unchanged for payloads without it |
| 3 | Unit | `PublishManifest` writes `subject_digest`; **repush with a different subject updates it** (proposal's Low risk); a manifest with no subject stores `''` |
| 4 | Unit | Pre-existing rows are backfilled; **idempotency** — a second `New()` changes no row and writes no second marker; unparseable payload → `''` and `New()` still succeeds; marker + rows commit atomically |
| 5 | Unit | **`ListReferrers` cross-tenant returns empty; cross-repository returns empty** (proposal's High risk — mandatory, asserted directly against seeded rows in both, never inferred from the HTTP layer) |
| 5 | Unit | Ordering is `digest ASC` regardless of insertion order; `''` never matches; unknown digest → empty slice, not an error |
| 6 | Unit | `resolveArtifactType` table test: manifest value wins; absent → `config.mediaType`; absent + no config → `""` |
| 6 | Unit | `Referrers` authorizes **before** parsing the digest: an unauthorized caller sending a malformed digest gets `401`, not `400` |
| 7 | Integration | `200` + empty `manifests[]` for never-pushed and for deleted subjects; JSON encodes `[]`, **not `null`** |
| 7 | Integration | `Content-Type: application/vnd.oci.image.index.v1+json`; `?artifactType=` filters and sets `OCI-Filters-Applied`; unfiltered and `?artifactType=` (empty) set no header |
| 7 | Integration | Pull-scoped token → `200`; unauthorized → `401` + challenge; non-GET → `405 Allow: GET` |
| 7 | Integration | Cosign v3 bundle referrer (`testdata/bundle-referrer-manifest.json`) is listed; a legacy `.sig` is not, and its tag path still resolves |
| 8 | Integration | Push, pull, tag listing, catalog, delete, scan queueing and signature verification are byte-identical before and after |

## Threat Matrix

Applicable: this change adds an HTTP route, a path-splitting marker, and a boot-time data migration. No
subprocess, no shell, no argv, no VCS/PR automation, no executable-file classification.

| Boundary | Applicability | Design response | Planned RED test |
|---|---|---|---|
| Documentation-like paths | N/A — no path drives an execution decision | — | — |
| Git / PR automation | N/A — none invoked | — | — |
| Subprocess argv | N/A — no subprocess on this path | — | — |
| **Route shadowing** | **Applicable** — a new marker joins a first-match scan | `/referrers/` appended last (Decision 6); residual `<repo>/referrers/referrers/<d>` fails closed at `400` | `splitRepositoryPath` table test incl. a repository named `referrers`; Phase-0 dispatch characterization |
| **Cross-tenant / cross-repository leakage** | **Applicable — highest severity** | `ListReferrers` copies `ListTags`' three predicates; the port takes `tenant` + `RepositoryRef`; the interface doc names `ListReferencedBlobDigests` as the anti-pattern | Seeded two-tenant and two-repository store tests asserting empty (Phase 5) |
| **Capability/validation disclosure to unauthorized callers** | **Applicable** | `authorize(ActionInspect)` runs before `ParseDigest` (Decision 7) | Unauthorized + malformed digest → `401`, never `400` |
| **Privilege reuse** | **Applicable** — a read endpoint over signed content | `ActionInspect` only; no new scope action; `Action.Scope()` untouched, so a plain pull token already satisfies it | Pull-scoped token `200`; no-access principal `401` |
| **Boot-time migration availability** | **Applicable** — the backfill runs inside `New()` | Marker-gated, one pass, one transaction; a corrupt row is skipped rather than failing `New()` | Unparseable payload → `New()` succeeds, row stays `''` |
| **Unbounded response** | **Applicable** — no pagination in this slice | Bounded by manifests in one repository sharing one subject digest; only the matched set is parsed, never the whole repository | Seeded multi-referrer test asserting all are returned in digest order |
| **Silent wire non-conformance (`null` vs `[]`)** | **Applicable** | `Manifests` always built with `make(..., 0, n)` | Assert the **raw response body** contains `"manifests":[]`, not merely a zero-length Go slice |

## Migration / Rollout

Additive and inert to reverted binaries. `subject_digest` is `NOT NULL DEFAULT ''` and unread by old code;
`schema_backfills` and `idx_manifests_subject` are unreferenced by old code. No down-migration is written and
none is needed. The backfill writes only the new column — never `payload`, `digest`, `pushed_by`, tags, or blob
files — so it is non-destructive; re-applying after a revert re-runs the same idempotent statements, and the
marker makes the second run a single PK lookup. No feature flag: a read-only listing of already-stored content
adds no new authority (proposal, stated assumption).

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Partial index silently unused because the bound-parameter predicate does not imply it | Med | The literal `AND m.subject_digest != ''` is part of the query, not an optimization note; assert the plan with `EXPLAIN QUERY PLAN` in the store test |
| `manifests[]` encodes as `null` on the empty path | Med | `make(..., 0, n)`; the test asserts raw response bytes |
| `NewManifest`'s signature change (18 call sites) inflates one PR past the 800-line review budget | Med | Flagged for `sdd-tasks`: Phases 0–2 are a natural first slice, deliverable and independently green |
| Backfill transaction blocks a concurrent `New()` past the 100 ms `busy_timeout` | Low | Bounded by manifest count at this project's scale; `New()` runs before serving; the loser recomputes identical deterministic values |
| Router sets `OCI-Filters-Applied` on an error response | Low | Header set strictly after the error branch; asserted on a `400` response |

## Open Questions

- [ ] Pagination (`n`/`last`) is intentionally absent; a registry returning the full list is conformant. Worth
      confirming no target client requires the `Link` header before this ships.
- [ ] `<repo>/referrers/referrers/<digest>` fails closed at `400` rather than routing correctly. Fixing it means
      moving every marker to `strings.LastIndex`, changing existing blob/manifest routing — deferred, not
      designed here.
- [ ] `ResolveManifest` still returns `Subject: nil` (Decision 5). Deliberate and now commented; a future change
      that genuinely needs subject on the read path should widen the column into a stored descriptor rather than
      parse per pull.
