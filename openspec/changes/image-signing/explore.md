# Exploration: Image signing (cosign/Sigstore verification gate)

## Goal

Investigate the design space for an "image signing" feature before any product
decisions are made. No requirements exist yet beyond the user's one-line
intent. Confirmed via live web research (Harbor docs/blog) that the dominant
real-world mechanism is external `cosign`/`notation` CLI tools pushing
signatures as OCI accessory artifacts against the registry, with the registry
itself never holding private keys or doing cryptographic signing — only
storage, linking (OCI 1.1 Referrers API or the older tag convention), and an
optional pull-time enforcement gate. This mirrors `scan-policy-gate`'s exact
shape (async producer, sync pull-time gate) far more than it resembles
`repository-scan-config-overrides`' scanner-runner pattern.

## Current State

**Harbor's model (web-verified, not training-data guess).** Harbor does NOT
sign or verify cryptography itself. Per Harbor's docs
(goharbor.io/docs/2.13.0/.../sign-images/) and the "Introducing Cosign in
Harbor v2.5.0" blog: a user runs `cosign sign` locally with cosign installed
and a private key (or keyless OIDC), and cosign "pushes the generated
signature into Harbor," stored as an "artifact accessory alongside the signed
artifact." Harbor "manages a link between the signed artifact and cosign
signature" so retention/immutability rules can apply to the pair. Enforcement
is a project-level "content trust" toggle: "a project administrator can
configure a project to enforce content trust, making it required for all
artifacts to be signed before they can be pulled." No scanner-adapter-style
plugin protocol exists for signing (unlike Trivy's Scanner Adapter API) —
integration is purely "be OCI-artifact/Referrers compliant storage" plus one
enforcement boolean. A live Harbor issue (#22592) confirms Harbor's own
referrers-based signature *detection* is still imperfect (signatures pushed
via OCI 1.1 referrers mode sometimes show as generic `UNKNOWN` type rather
than being recognized as cosign signatures) — even Harbor's implementation of
this is not fully mature.

**OCI Distribution Spec 1.1 Referrers API** (web-verified against
opencontainers/distribution-spec): `GET /v2/<name>/referrers/<digest>` returns
an OCI image index of manifests whose `subject` field points at `<digest>`. A
registry that supports it MUST return `200` (even an empty index) — never
`404` — because `404` is how clients detect "this registry doesn't implement
the API" and fall back to cosign's older tag-based convention
(`sha256-<digest-hex>.sig` as an ordinary tag on the same repository).

**regixtry already partially parses OCI 1.1 `subject`, but does not persist
or serve it as a graph.** `domain.Manifest` (`internal/domain/regixtry/manifest.go:5-14`)
already has a `Subject *Descriptor` field, populated from the pushed
manifest's JSON `"subject"` key (`internal/app/regixtry/service.go:293`,
`manifestEnvelope.Subject` at `service.go:344-350`) — the parsing exists.
But `Manifest.References()` (`manifest.go:93-106`) folds `Subject` into the
same flat list as `Config`/`Layers`, and `PublishManifest`
(`service.go:222-262`) uses that list only to (a) verify every referenced
digest exists via `s.blobs.BlobExists` (`service.go:237-245`) and (b) persist
them as undifferentiated `manifest_blobs` rows (`store.go:1121-1129`, no
`position`-based type tag). **Concrete bug for real cosign OCI-1.1-mode
pushes**: a cosign signature manifest's `subject` field points at the
*image's manifest digest*, not a blob digest. `fsblob.Store`
(`internal/infra/storage/fsblob/store.go:172` `BlobExists`) is a
content-addressed blob store that manifests are never registered into
(manifest payloads live in the separate `manifests` SQLite table,
`store.go:1097-1108`, keyed by `(tenant, repository_id, digest)`). So
`PublishManifest`'s existing `BlobExists` loop would reject a real cosign
OCI-1.1-referrers-mode push with a false "manifest references missing blob"
conflict (`service.go:243`) today — this is new, unowned scope, not a
theoretical concern.

**No Referrers API route exists at all.** `handleV2`'s switch
(`internal/protocol/http/router.go:139-171`) recognizes only
`blobs/uploads`, `blobs/`, `manifests/`, `manifests/<ref>/scan-status`, and
`tags/list`; there is no `referrers/` case. A cosign client attempting OCI
1.1 mode against regixtry today gets a generic `404 NAME_UNKNOWN`
(`router.go:169`), not the spec-mandated `200` — cosign's own client-side
fallback logic handles that gracefully by using the tag convention instead,
so this is a compliance gap, not an outage, but it means "referrers
discovery" (e.g., for a TUI to list "what signs this image?") has no server
support to build on.

**The tag-based fallback convention likely works today with zero new code.**
`parseManifestPayload` (`service.go:264-268`) accepts any non-empty string as
a manifest `reference` with no OCI tag-format regex — only
`RepositoryRef.Validate` constrains the *repository* name
(`internal/domain/regixtry/repository.go:8,21`). So `cosign sign` in its
legacy (non-referrers) mode, which just PUTs a normal manifest at tag
`sha256-<digest-hex>.sig`, should already round-trip through regixtry's
existing manifest PUT/GET path unmodified — worth confirming with an actual
integration test in `sdd-design`, but no code gap is visible.

**The exact pull-time gate insertion point already exists and is proven.**
`Service.OpenManifest` (`internal/app/regixtry/queries.go:71-91`) already
calls `s.enforceScanPolicy` (`service_scanning.go:94-110`) right after
resolving the digest — a verification gate would sit beside it, same
insertion point, same fail-open-on-uncertainty posture, same 403
(`domain.NewPolicyViolationError`) convention. `ScanPolicySettings`
(`internal/ports/regixtry.go:91-95`) is a deliberately global-only
`{Enabled, SeverityThreshold, UpdatedAt}` row with a code-level default
(`service_scanning.go:56-62`) — the exact shape a global "require signature"
toggle would reuse structurally, though not the same table (signing needs
different fields: trusted identity/key material, not a severity enum).

**The repository-override mechanism is real and reusable, but only for
scan-execution-shaped config, not obviously for signing policy.**
`repository_feature_overrides` (`store.go:1293`, resolved via
`s.applyRepositoryOverride`, `internal/app/regixtry/repository_overrides.go:132-145`)
is a generic `(tenant, repository, feature_name) → JSON payload` table with a
per-feature codec registry (`repositoryOverrideCodecs`,
`repository_overrides.go:33-36`) that currently layers onto
`ports.ScanSettings` specifically (`Apply` signature takes/returns
`ports.ScanSettings`, `repository_overrides.go:25`) — it is NOT a generic
"any settings type" mechanism as shipped; it is coupled to `ScanSettings`'s
fields (`Enabled`, `IgnoreFilePath`, `ConfigPath`, etc.). Reusing the same
*table* for a `feature_name="signing"` row is straightforward (the table and
HTTP/TUI resource shape are feature-name-keyed already per its own doc
comment, `repository_overrides.go:29-32`: "Adding a future feature (e.g.
image signing) is exactly one map entry plus one struct in ports"), but the
codec's `Apply` function would need a different target type than
`ports.ScanSettings` (something like a new `ports.SigningSettings`), which
means `repositoryOverrideCodec.Apply`'s signature is not literally reusable
as-is — it would need to be generalized (e.g., `any`/generics) or duplicated
as a parallel codec map, a real design decision, not a given.

**TUI badge precedent exists and is directly reusable.** The just-shipped
`scanPolicyBadge` (`internal/tui/admin_views.go:251-253`, composed at zero
row cost into `renderTrivyTabs`, `admin_views.go:234-248`) is a **text-only**
persistent status badge (`TestRenderTrivyTabsPolicyBadgeTextReflectsStateAndUsesNoIconOrGlyph`,
`admin_views_test.go:563-568`) — the exact established pattern (not icon
vocabulary) a "Signing: required/off" badge would follow.

## Affected Areas (if pursued)

- `internal/domain/regixtry/manifest.go` — `References()` currently treats
  `Subject` as blob-equivalent; needs to distinguish "subject points at a
  manifest, not a blob" if OCI 1.1 Referrers push support is in scope.
- `internal/app/regixtry/service.go` (`PublishManifest`) — the `BlobExists`
  loop over `manifest.References()` must stop rejecting subject digests that
  point at manifests instead of blobs.
- `internal/infra/metadata/sqlite/store.go` — `manifests`/`manifest_blobs`
  have no subject/referrer reverse index; a Referrers API needs one.
- `internal/protocol/http/router.go` (`handleV2`) — new `referrers/` route,
  only if OCI 1.1 discoverability is in scope (not required for tag-based
  cosign to work).
- `internal/app/regixtry/queries.go` (`OpenManifest`) — verification-gate
  insertion point, mirroring `enforceScanPolicy`.
- `internal/ports/regixtry.go`, `internal/infra/metadata/sqlite/store.go` —
  new settings type (global `SigningPolicySettings`?) and/or new
  `repository_feature_overrides` codec, depending on scope decision below.
- `internal/infra/scanning/` sibling (e.g. `internal/infra/verification/cosign/`)
  — if verification shells out to the `cosign` binary, mirroring the
  managed-runtime pattern already used for Trivy/gitleaks.
- `internal/tui/session.go`, `model.go`, `admin_views.go` — new badge/modal,
  following `scanPolicyBadge`'s exact zero-row-cost, text-only precedent.
- `go.mod` — new dependency if a Go verification library is chosen over
  shelling out to `cosign verify`.

## Approaches Considered

**Scope of "signing" work regixtry should own:**

1. **Verification-only pull-time gate + passive OCI-artifact storage
   compliance (no Referrers API)** — regixtry never signs; it just needs to
   accept cosign's tag-based `.sig` push (already works) and add a gate in
   `OpenManifest` that shells out to `cosign verify` (or a Go verification
   library) against a configured public key/OIDC identity, blocking pull on
   failure. No Referrers API, no new blob/manifest graph.
   - Pros: smallest slice; directly mirrors `scan-policy-gate`'s proven
     shape; zero cryptography reimplemented; no changes to `References()`/
     `BlobExists`.
   - Cons: not spec-compliant with OCI 1.1 Referrers discoverability (fine
     for cosign clients, which fall back automatically; a problem only if a
     Referrers-only tool is used).
   - Effort: Medium.

2. **Full OCI 1.1 Referrers API compliance + verification gate** — fixes the
   `BlobExists`/`References()` gap for subject-bearing pushes, adds
   `referrers/` route and a subject→referrer index, then layers the same
   pull-time gate on top.
   - Pros: fully spec-compliant, matches what a modern registry (Harbor,
     ECR, ACR, GHCR) is expected to support; unblocks `cosign sign
     --new-bundle-format`/attestations generally, not just signatures.
   - Cons: materially larger — new domain modeling (referrer graph is not
     "just another settings row"), new storage index, new route, more
     surface for `sdd-tasks` to size and for reviewers to absorb.
   - Effort: High.

3. **Admin-facing "signed/unsigned" visibility only, no enforcement** — just
   detect and display whether a digest has an associated signature artifact
   (via tag-convention lookup), no pull blocking.
   - Pros: lowest risk, no fail-open/fail-closed policy debate, fast to ship,
     gives immediate value (audit visibility) without a security-critical
     gate.
   - Cons: doesn't satisfy "signing feature" in the sense Harbor's content
     trust or most users would expect; likely still a stepping stone to (1),
     not a substitute.
   - Effort: Low.

**Go tooling for verification** (only relevant once a scope above is picked):

- **Shell out to `cosign verify`** as a managed external binary — matches
  the established `internal/infra/scanning/{trivy,gitleaks}` convention
  exactly (external, independently-updatable, security-critical tool
  invoked as a subprocess, not vendored logic). No `go.mod` cosign
  dependency, no risk of import-graph bloat from cosign's famously large
  dependency tree.
- **Import `sigstore/sigstore-go`** (web-verified: "minimal dependency
  library for signing and verifying," intended as cosign's own eventual
  verification backend, not a CLI replacement) directly into regixtry's Go
  binary — avoids a subprocess/binary-management dependency (no analog to
  Trivy DB downloads needed for signature verification), but is a new
  dependency-management pattern (import vs. manage-a-binary) this codebase
  hasn't used for security tooling before.

## Recommendation

Approach 1 (verification-only gate, no Referrers API) as the near-term slice,
explicitly deferring Referrers-API compliance (Approach 2) as a separate,
larger future change — same "split into cohesive pieces" pattern
`scan-policy-gate`'s own explore recommended for its CI-poll endpoint. Do
NOT attempt cryptographic signing in regixtry itself under any scope; keep it
strictly verification-only, consistent with the explicit constraint that
regixtry must never hold private signing keys. Between the two Go-tooling
options, lean toward **`sigstore-go`** direct import over shelling out to
`cosign verify`, breaking from the Trivy/gitleaks external-binary precedent —
unlike Trivy (which needs a large, independently-updated vulnerability DB)
signature verification is stateless crypto plus a public key/certificate
chain, so the "external managed binary" rationale (DB freshness, install/
upgrade lifecycle) that justifies Trivy/gitleaks's runner pattern doesn't
carry over; but this is a real tradeoff for `sdd-propose`/`sdd-design` to
weigh explicitly, not a settled decision — this exploration surfaces the
option, it does not resolve it.

## Risks

- `PublishManifest`'s `BlobExists` check over `manifest.References()`
  (`service.go:237-245`) will reject any real cosign push that uses OCI 1.1
  referrers mode (`subject` pointing at a manifest digest) today — anyone
  testing against a real cosign client, even under Approach 1's reduced
  scope, may hit this if the cosign version being tested defaults to
  referrers mode over the tag convention (current cosign versions do prefer
  referrers mode when the registry advertises support, but fall back
  gracefully on 404 — needs to be verified experimentally, not assumed).
- `repositoryOverrideCodec.Apply`'s signature is hard-coupled to
  `ports.ScanSettings` today (`repository_overrides.go:25`) — reusing
  `repository_feature_overrides` for a signing policy is not the "zero new
  interface surface" win it initially appears to be; the codec abstraction
  itself needs generalizing first.
- `sigstore-go` vs. `cosign` CLI is a meaningfully different integration
  posture (library dependency vs. managed subprocess) from every existing
  scanning integration in this codebase — whichever is chosen sets a new
  precedent future features will be compared against.
- Harbor's own Referrers-based signature detection is still buggy in
  production (issue #22592, `UNKNOWN` artifact type) — even the reference
  implementation this feature is modeled on has not fully solved this;
  regixtry attempting full Referrers compliance risks the same class of
  edge-case bugs for comparatively little near-term value if verification-
  only is the actual product goal.
- No requirements have been decided yet (unlike the two prior features,
  which entered explore with product decisions already made) — every
  approach above is provisional until `sdd-propose` gets explicit answers to
  the open questions below.

## Open Questions Requiring a Product Decision

1. **Scope**: verification-gate only (Approach 1), full Referrers-API
   compliance (Approach 2), visibility-only (Approach 3), or some
   combination/sequencing of these as separate changes?
2. **Trust model**: static public key(s) configured by an admin, keyless
   OIDC identity matching (e.g., "must be signed by this GitHub Actions
   workflow identity"), or both? This fundamentally changes what settings
   fields exist and whether Rekor transparency-log lookups are needed.
3. **Global vs. per-repository policy scope**: does "require signature"
   belong on a global-only settings row (matching `ScanPolicySettings`'s
   deliberate choice) or does it need per-repository trust configuration
   (matching `repository_feature_overrides`'s existing per-repo pattern,
   since different repositories plausibly trust different signers)? Unlike
   `ScanPolicySettings` vs. `ScanSettings`'s granularity tension (which
   concluded "stay global, it's a materially bigger decision"), signing
   trust genuinely seems more likely to be per-repository in real usage
   (different teams, different signing identities) — this needs an explicit
   decision, not a default carried over from the vulnerability gate.
4. **Fail-open or fail-closed on verification errors** (network failure
   reaching a transparency log, missing signature, expired cert) — the
   vulnerability gate is deliberately fail-open on uncertainty; signing
   policy in the wider industry is more often fail-closed by design (an
   unsigned/unverifiable image is the failure mode content trust exists to
   catch) — this is a meaningfully different default posture to decide
   explicitly, not inherit.
5. **Go library (`sigstore-go`) vs. external `cosign verify` binary** — see
   Recommendation; genuinely open, affects `go.mod`, CI, and ops burden
   differently than any existing dependency in this codebase.
6. **Does Referrers-API compliance matter for this org's actual client
   tooling**, or is tag-based cosign (already working) sufficient? Directly
   determines whether Approach 2's larger scope is ever justified.
7. **Should push-time behavior change at all** (e.g., reject pushes of
   images with no corresponding signature yet), or does signing/verification
   stay strictly pull-time, matching the vulnerability gate's async-scan/
   sync-gate split?

## Ready for Proposal

No — significantly more product decisions are needed here than in either
prior feature before `sdd-propose` can meaningfully scope a change. At
minimum, questions 1-4 above need explicit answers; this exploration
deliberately surfaces the design space rather than guessing at scope, per
the much-less-defined starting point (a one-line "I want to sign images"
versus six pre-decided requirements for `scan-policy-gate`).
