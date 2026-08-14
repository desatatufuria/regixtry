# Design: Manifest and Tag Deletion (manifest-blob-delete)

## Technical Approach

One new verb on an existing route, threaded through the four layers that already exist for push.
No new route, no schema migration, no blob-store call anywhere on the path.

1. **Router** — `handleManifest` (`router.go:314-364`) gains `case stdhttp.MethodDelete`; the
   `default:` 405 `Allow` list grows to `PUT, GET, HEAD, DELETE`.
2. **Service** — `DeleteManifest(ctx, repositoryName, reference)` authorizes `ActionDelete`,
   *then* checks the opt-in flag, *then* disambiguates digest-vs-tag and calls one of two store
   methods.
3. **Store** — two transactional deletes; the existing `ON DELETE CASCADE` FKs
   (`store.go:1251-1290`) remove `tags` and `manifest_blobs` rows. Zero rows affected →
   `domain.NewNotFoundError`.
4. **Auth** — `delete` becomes a real scope action across `scope.go`, `intersectRequestedActions`,
   `Action.Scope()`, and a new `Principal.HasDeleteAccess`.

Every line reference below was read from the working tree, not estimated.

## Architecture Decisions

### Decision 1: `202 Accepted` carries a JSON body naming what was removed

| Option | Tradeoff | Decision |
|---|---|---|
| `202` + `{"digest","tagsRemoved","manifestRemoved"}` | One extra `SELECT` inside the same transaction | **Chosen** |
| Bodyless `202` (bare OCI minimum) | Makes the highest-likelihood risk in the proposal — a cascade silently dropping tags the caller never named — invisible on the wire | Rejected |
| Bodyless `202` + `Docker-Content-Digest` header only | Carries the digest but still cannot express *which* tags died | Rejected |

The OCI spec mandates the status code, not an empty body, and `handleTags`/`handleCatalog`
already answer `/v2` requests with JSON via `writeJSON`. The tag names are genuinely cheap: the
store selects them **inside the same transaction, immediately before** the `DELETE`, so the body
is an exact record of what the cascade removed rather than a second, racy query. Clients that
ignore 2xx bodies lose nothing; operators and CI logs gain the audit line.

```go
// internal/app/regixtry/queries.go
type DeletionDetails struct {
    Repository      string   `json:"repository"`
    Reference       string   `json:"reference"`
    Digest          string   `json:"digest,omitempty"`     // digest path only
    ManifestRemoved bool     `json:"manifestRemoved"`
    TagsRemoved     []string `json:"tagsRemoved"`
}
```

`digest` is omitted on the tag path deliberately: resolving it would add a lookup that buys the
caller nothing it did not already send. `manifestRemoved` is the field that distinguishes the two
semantics without the caller having to re-parse its own reference.

### Decision 2: the flag lives on the Service and is checked **after** authorization

`withPrincipal` (`router.go:493-508`) only *authenticates*; every authorization decision happens in
the service via `s.authorize` → `AccessController.Authorize`. So "flag after auth" necessarily
means the flag is a Service field, not a Router field.

| Option | Tradeoff | Decision |
|---|---|---|
| Service field, checked after `s.authorize` | Only a caller who would otherwise have succeeded learns the capability exists but is off | **Chosen** |
| Router field (`RouterOption`), checked before dispatch | Sits above the service, so it necessarily fires *before* authorization — every anonymous prober learns delete is implemented-but-disabled | Rejected |
| Sixth positional arg on `NewService` | `NewService` has 7 call sites in `main.go`; a setter mirroring the existing `scanHost` setter (`service.go:131-134`) is the established low-churn shape | Rejected |

**Deliberate, reasoned deviation from the `handleUploadState` precedent.** That branch
(`router.go:276-277`) refuses *before* any authorization — but it has no authorization step to be
consistent with: upload cancellation is a permanent stub with no operation behind it, so the
router answers directly and never calls the service. Delete is a real, authorized operation that
is merely switched off, so its refusal is runtime-configuration information only an authorized
caller is entitled to. What we **do** reuse verbatim is the precedent's wire shape:
`domain.NewValidationError` + OCI code `UNSUPPORTED`, which `writeError` (`router.go:644-648`)
maps to **`400`**, not `405`. Docker's registry answers `405` here; the proposal binds us to the
in-repo precedent, and the deviation is recorded rather than silently inherited.

Ordering inside `DeleteManifest`: `parseRepository` → `authorize(ActionDelete)` → flag →
reference disambiguation → store.

### Decision 3: two store methods, because delete is a write path

| Option | Tradeoff | Decision |
|---|---|---|
| `DeleteManifestByDigest` + `DeleteTag` | Two methods; each has one unambiguous SQL statement and one unambiguous typed not-found | **Chosen** |
| One `DeleteManifest(..., reference)` branching internally | Reads like `ResolveManifest`, but hides two radically different effects (cascade vs. untag) behind one signature | Rejected |

The convention here is not "one method per route", it is **read paths take a raw `reference`;
write paths disambiguate at the service layer and hand the store an unambiguous argument**.
`ResolveManifest(ctx, tenant, repository, reference)` is the read side; `PublishManifest(ctx,
tenant, repository, tag string, ...)` is the write side, and `parseManifestPayload`
(`service.go:264`) is where the service already resolves digest-vs-tag for it. Delete is a write
path, so it follows `PublishManifest`, not `ResolveManifest`.

Naming honours the exploration's future-proofing note: both methods say *delete manifest* / *delete
tag*, never *delete and GC*, so a later `SweepUnreferencedBlobs` attaches without renaming either.

```go
// internal/ports/regixtry.go — MetadataStore
// DeleteManifestByDigest removes one manifests row; the ON DELETE CASCADE FKs
// remove its tags and manifest_blobs rows in the same transaction. It returns
// the names of the tags that pointed at that digest, selected inside that
// transaction before the delete, so the caller can report exactly what the
// cascade removed. Zero rows affected is a typed domain.ErrorCodeNotFound,
// mirroring DeleteUpload. It never touches blob files on disk.
DeleteManifestByDigest(ctx context.Context, tenant string, repository domain.RepositoryRef, digest domain.Digest) ([]string, error)
// DeleteTag removes one tags row, leaving the manifest and every other tag on
// it intact. Zero rows affected is a typed domain.ErrorCodeNotFound.
DeleteTag(ctx context.Context, tenant string, repository domain.RepositoryRef, tag string) error
```

Service-side disambiguation reuses the existing idiom — `domain.ParseDigest` first, plain tag on
error — the same test `parseManifestPayload` already applies to the same argument.

### Decision 4: `delete` is added to the scope vocabulary — otherwise the token endpoint rejects it

**Load-bearing and easy to miss.** `normalizeScopeActions` (`scope.go:160-163`) *rejects* any
repository action that is not `pull` or `push` with `"repository scope actions must be pull and/or
push"`. Today, `ParseScope("repository:app:pull,push,delete")` fails, so `/auth/token` would answer
`400` to any client honouring a `delete` challenge. Four coordinated edits, all in
`internal/domain/auth/scope.go`:

```go
actionDelete = "delete"                                  // const block, scope.go:10-17

func (s Scope) AllowsDelete() bool { return s.hasAction(actionDelete) }

// normalizeScopeActions validation
if action != actionPull && action != actionPush && action != actionDelete {
    return nil, NewValidationError("repository scope actions must be pull, push, and/or delete")
}

// normalizeScopeActions canonical ordering weight
case actionPull:   return 0
case actionPush:   return 1
case actionDelete: return 2
default:           return 3
```

And in `internal/ports/regixtry.go`: `ActionDelete ActionVerb = "delete"`, plus an
`Action.Scope()` arm returning `"repository:" + a.Repository + ":delete"`. Deliberately **not**
`pull,delete` (contrast `ActionPush`'s `pull,push`): deleting requires no read, so the challenge
asks for the minimum. That challenge string is also the compatibility mitigation for the
proposal's client-tooling risk — `go-containerregistry`/`oras` re-request a token using the scope
the `WWW-Authenticate` header names, so a client that guessed `pull,push` recovers on the retry.

### Decision 5: `ActionDelete` derivation and the authorization predicate

`intersectRequestedActions` (`service.go:992-1030`), exact addition:

```go
func intersectRequestedActions(isAdmin bool, isReadOnly bool, grants []domainauth.RepoGrant, requested domainauth.Scope) []string {
	allowPull := false
	allowPush := false
	allowDelete := false

	if isAdmin {
		allowPull = requested.AllowsPull()
		allowPush = requested.AllowsPush()
		allowDelete = requested.AllowsDelete()
	} else if isReadOnly {
		// Unchanged. Never grants push, and now never grants delete either:
		// the absence of a line here is the guarantee, not a check.
		allowPull = requested.AllowsPull()
	} else {
		for _, grant := range grants {
			if grant.Repository.String() != requested.Repository().String() {
				continue
			}
			if grant.Role.AllowsRead() && requested.AllowsPull() {
				allowPull = true
			}
			if grant.Role.AllowsWrite() && requested.AllowsPush() {
				allowPush = true
			}
			// Own statement, own requested.AllowsDelete() guard — never folded
			// into the push branch as `AllowsPush() || AllowsDelete()`. A fifth
			// verb must likewise add its own line, so no future action can
			// inherit writer derivation by being forgotten (proposal Q4).
			if grant.Role.AllowsWrite() && requested.AllowsDelete() {
				allowDelete = true
			}
			break
		}
	}

	actions := make([]string, 0, 3)   // was 2
	if allowPull { actions = append(actions, "pull") }
	if allowPush { actions = append(actions, "push") }
	if allowDelete { actions = append(actions, "delete") }
	return actions
}
```

This stays an **intersection**: a `repo-writer` who requests `pull,push` receives `pull,push`.
`delete` is never added unrequested.

**`principal.go` changes after all** — the proposal listed it `Unchanged`, but reusing
`HasWriteAccess` would break the bound decision that a `pull,push` token must not delete:
`HasWriteAccess` (`principal.go:20-22`) checks `Scope.AllowsPush`, which such a token satisfies.
The role half stays `RepoRole.AllowsWrite` (the bound `repo-writer` decision); only the scope half
changes:

```go
func (p Principal) HasDeleteAccess(repository string) bool {
	return p.hasGrantedRepositoryAccess(repository, RepoRole.AllowsWrite) && p.scopeAllowsRepository(repository, Scope.AllowsDelete)
}
```

Wired in `principalAccessController.Authorize` (`defaults.go:107-120`) as a new
`case ActionDelete:` arm. `configurableAccessController.Authorize` (`defaults.go:62-76`) is left
**untouched**: `ActionDelete` matches neither its anonymous-pull nor its anonymous-push arm, so
anonymous delete falls through to `NewUnauthorizedError` by construction, even on an
anonymous-pull-enabled instance. The failure direction is closed-by-default.

### Decision 6: rejection reuses the existing path exactly — no bespoke error

Traced from today's PUT: `handleManifest` sets `ActionPush` → `withPrincipal` (authenticate only;
a *missing* header is not an error, it simply injects no principal) → `PublishManifest` →
`s.authorize` → `principalAccessController.Authorize` → `domain.NewUnauthorizedError` →
`writeError` → **`401` + `WWW-Authenticate`** (`router.go:632-635`).

DELETE takes the identical path with `Verb: ports.ActionDelete`. Worth stating precisely, because
it corrects a common assumption: this codebase answers **`401` for both** the unauthenticated
caller *and* the authenticated-but-under-scoped caller — `Authorize` returns
`NewUnauthorizedError("authorization required")` for the latter, and there is no `403` scope-denial
branch anywhere on `/v2`. `403` is reserved for `ErrorCodePolicyViolation` (scan/signing gates).
We add no new error path; the two cases differ only in the challenge's `scope` string.

## Data Flow

    DELETE /v2/library/app/manifests/sha256:abc…        [3 tags point at sha256:abc…]
      handleManifest  action.Verb = ActionDelete
        -> withPrincipal                 [authenticate only; 401 on a bad token]
        -> Service.DeleteManifest(repo, ref)
             authorize(ActionDelete) -> HasDeleteAccess  [401 + challenge if denied]
             deleteEnabled? no -> ValidationError/"UNSUPPORTED" -> 400
             domain.ParseDigest(ref) ok  -> store.DeleteManifestByDigest
                   BEGIN
                     SELECT name FROM tags WHERE manifest_id = (…digest…)   -> [v1 v2 latest]
                     DELETE FROM manifests WHERE tenant/repository_id/digest
                       └─ CASCADE: tags(3 rows), manifest_blobs(n rows)
                     RowsAffected == 0 -> NewNotFoundError -> 404 MANIFEST_UNKNOWN
                   COMMIT
        -> 202 {"digest":"sha256:abc…","manifestRemoved":true,"tagsRemoved":["latest","v1","v2"]}

    DELETE /v2/library/app/manifests/v1
             domain.ParseDigest(ref) fails -> store.DeleteTag
                   DELETE FROM tags WHERE tenant/repository_id/name = 'v1'
        -> 202 {"manifestRemoved":false,"tagsRemoved":["v1"]}   [manifest + other tags survive]

    Blob files on disk: never opened, never stat-ed, never unlinked on either path.

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/domain/auth/scope.go` | Modify | `actionDelete`, `AllowsDelete`, `normalizeScopeActions` allow-list + sort weight (Decision 4) |
| `internal/domain/auth/principal.go` | Modify | `HasDeleteAccess` (deviates from the proposal's "Unchanged", Decision 5) |
| `internal/app/auth/service.go` | Modify | `intersectRequestedActions` delete derivation |
| `internal/ports/regixtry.go` | Modify | `ActionDelete`, `Action.Scope()` arm, `DeleteManifestByDigest`/`DeleteTag` on `MetadataStore` |
| `internal/ports/defaults.go` | Modify | `case ActionDelete` in `principalAccessController.Authorize`; configurable controller untouched |
| `internal/infra/metadata/sqlite/store.go` | Modify | Both transactional deletes; typed not-found on zero rows |
| `internal/app/regixtry/service.go` | Modify | `DeleteManifest`; `deleteEnabled` field + setter |
| `internal/app/regixtry/queries.go` | Modify | `DeletionDetails` |
| `internal/protocol/http/router.go` | Modify | `case MethodDelete` in `handleManifest`; `Allow` list gains `DELETE` |
| `cmd/regixtry/main.go` | Modify | `DeleteEnabled` field, `flags.BoolVar(..., parseBoolEnv("REGISTRY_DELETE_ENABLED", false), ...)`, threaded into the service |
| `internal/infra/blob/**` | Unchanged | Explicit non-goal: no delete path touches the blob store |
| Schema / migrations | Unchanged | Existing cascading FKs suffice |

## Interfaces / Contracts

```
DELETE /v2/<name>/manifests/<digest>   202  {"repository","reference","digest","manifestRemoved":true,"tagsRemoved":[…]}
DELETE /v2/<name>/manifests/<tag>      202  {"repository","reference","manifestRemoved":false,"tagsRemoved":["<tag>"]}
                                       400  UNSUPPORTED           flag off, caller was authorized
                                       401  UNAUTHORIZED + WWW-Authenticate: …scope="repository:<name>:delete"
                                       404  MANIFEST_UNKNOWN      absent digest or tag
<any other method>                     405  Allow: PUT, GET, HEAD, DELETE
```

## Testing Strategy

Strict TDD, and `handleManifest`, `PublishManifest`, and `ListManifestBlobs` have **zero or only
indirect** coverage today. **Sequencing implication for `sdd-tasks`**: a characterization task
pinning today's behaviour — `DELETE` on `manifests/<ref>` answering `405` with
`Allow: PUT, GET, HEAD`, and `PUT`/`GET`/`HEAD` answering as they do now — MUST land and pass
*before* any task that adds the DELETE branch. Otherwise the RED test for delete is written
against a handler whose current behaviour was never captured, and the `Allow`-list edit silently
changes an unasserted response.

| Layer | What to Test | Approach |
|---|---|---|
| Unit | Characterization: `handleManifest` DELETE → 405 + today's `Allow`; PUT/GET/HEAD unchanged | Table-driven, before any change |
| Unit | `PRAGMA foreign_keys` is `1`, so the cascade premise holds | Mirrors the existing `busy_timeout` assertion (`store_test.go:139-145`) |
| Unit | Digest delete removes the manifest, all 3 of its tags, and its `manifest_blobs` rows; returns those 3 names | Store test, `t.TempDir()` |
| Unit | Tag delete removes one `tags` row; manifest and sibling tags still resolve | Store test |
| Unit | Absent digest and absent tag each return `domain.ErrorCodeNotFound` | Table-driven |
| Unit | `ParseScope("repository:x:pull,push,delete")` succeeds and canonicalises in `pull,push,delete` order; an unknown action still fails | Table-driven, `scope.go` |
| Unit | `HasDeleteAccess`: writer+`delete` scope passes; writer+`pull,push` only fails; reader fails; admin passes; read-only fails | Table-driven |
| Unit | `intersectRequestedActions`: writer requesting `delete` gets it; requesting `pull,push` never gets it; read-only never gets it; admin honours the request | Table-driven |
| Unit | Service `DeleteManifest` refuses with `UNSUPPORTED` only *after* authorization — an unauthorized caller gets unauthorized even with the flag off | Two service tests (Decision 2 ordering) |
| Unit | Config parse defaults `DeleteEnabled` to `false` with the env var unset and unparseable | `main_test.go` |
| Integration | Flag off → `400`/`UNSUPPORTED`; flag on → `202` + body; anonymous → `401` + `scope="…:delete"` on an anonymous-pull instance | Router tests |
| Integration | Reader `401`, writer `202`, repo-admin `202`; `pull,push` token `401`, `pull,push,delete` token `202` | Router table test |
| Integration | **No blob file is removed by any delete path**, asserted by listing the blob directory before and after | Router/store test (non-goal guard) |
| Integration | Push, pull, tag listing and catalog byte-identical with the flag off and on | Existing suite unmodified |

## Threat Matrix

Applicable: this change adds an HTTP method to an existing route and a new authorization verb.
No subprocess, no shell, no argv, no VCS/PR automation, no executable-file classification.

| Boundary | Applicability | Design response | Planned RED test |
|---|---|---|---|
| Documentation-like paths | N/A — no path drives an execution decision | — | — |
| Git / PR automation | N/A — none invoked | — | — |
| Subprocess argv | N/A — no subprocess on the delete path | — | — |
| **HTTP method dispatch** | **Applicable** — a destructive verb joins a route that was read/write only | The verb is added inside the existing handler's `switch`; the route, path parsing and `splitRepositoryPath` are untouched, so no new path-shadowing surface appears | DELETE on `tags/list`, `blobs/<digest>`, and `blobs/uploads/<id>` still behave as today |
| **Privilege reuse (push token silently gains delete)** | **Applicable** — the highest-severity failure mode | Distinct `delete` scope action plus `HasDeleteAccess`, which checks `Scope.AllowsDelete`, not `AllowsPush` | A `pull,push` token is rejected on DELETE while succeeding on PUT, in one test |
| **Anonymous destructive access** | **Applicable** — instances may enable anonymous pull | `configurableAccessController` gets no `ActionDelete` arm, so anonymous delete fails by construction rather than by a check that could be deleted | Anonymous DELETE on an `AllowAnonymousPull` instance → `401` |
| **Capability disclosure to unauthorized callers** | **Applicable** — `UNSUPPORTED` reveals delete is implemented but off | Flag checked after authorization (Decision 2) | Unauthenticated DELETE with the flag off → `401`, never `400`/`UNSUPPORTED` |
| **Unintended cascade (data loss)** | **Applicable** — one digest delete drops every tag on it | Cascade is intended and atomic; the `202` body names every removed tag (Decision 1) | Three tags on one digest → all three removed and all three named in the body |
| **Blob-store scope creep** | **Applicable** — an eager unlink would corrupt shared layers | No delete path imports or calls the blob store | Blob directory byte-identical before and after both delete paths |

## Migration / Rollout

No migration. No schema change, no data rewrite — the change relies on FK cascades already present
at `store.go:1251-1290`, with `PRAGMA foreign_keys(1)` already set in the DSN (`store.go:42`).

Inert on boot: `REGISTRY_DELETE_ENABLED` defaults to `false`, so every deployment that does not opt
in answers `400`/`UNSUPPORTED` and behaves identically to today apart from the `Allow` header
listing `DELETE`. Rollback is `git revert`; a reverted binary is byte-compatible with the database
and answers `405` again. Rows already deleted are not restored, but their blob files were never
touched, so re-pushing the same content restores the manifest and its tags.

## Open Questions

- [ ] The disabled response is `400`/`UNSUPPORTED` (the in-repo `handleUploadState` precedent the
      proposal binds us to); Docker's registry answers `405`/`UNSUPPORTED` for the same condition.
      Worth confirming no target client treats `400` as fatal where it would retry on `405`.
- [ ] `Allow: PUT, GET, HEAD, DELETE` is advertised even when the flag is off, matching
      `handleUploadState:279`, which lists `DELETE` for a verb it always refuses. Accepted for
      consistency; a flag-dependent `Allow` list is the alternative if this is judged misleading.
- [ ] Untagged-but-stored manifests (last tag deleted by tag, digest still present) stay addressable
      by digest until the deferred GC change. Documented and accepted, not designed here.
