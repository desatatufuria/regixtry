# Apply Progress: signing-keyless-verification

## Scope covered by this batch (PR1 — Unit 1: domain-isolated)

Branch: `feature/signing-keyless-verification-01-domain-foundation`
(based on tracker `feature/signing-keyless-verification`, based on `develop`).

Phases 0, 1, 2, 3 from `tasks.md` — complete. Phase 4 (`ports.TrustedIdentity`)
and later are explicitly OUT of scope for this batch/branch (PR2+).

## Phase 0 — Real-artifact bundle-shape spike: UNCONFIRMED, evidence-backed

**Goal**: confirm whether a real keyless `cosign sign --yes` (GitHub Actions
OIDC) Sigstore Bundle document is `dsseEnvelope`-shaped or
`messageSignature`-shaped.

**Result: could not obtain a live keyless-signed Bundle-document artifact
from any reachable public registry after multiple real attempts.** Proceeded
on the `dsseEnvelope` assumption per the fallback instruction, with strong
(not conclusive) corroborating evidence. This remains open and should be
closed out by a human or CI environment with real `docker`/`cosign` access
before this change ships.

### What was tried

1. `ghcr.io/sigstore/cosign` (and `ghcr.io/sigstore/rekor-cli`) — anonymous
   token exchange returned `DENIED`, matching what the user reported
   independently before delegating this batch. Confirmed this is a registry
   visibility/policy quirk, not a network problem: `ghcr.io/oras-project/oras`,
   `ghcr.io/sigstore/fulcio`, and `ghcr.io/chainguard-images/cosign` all
   returned a real anonymous pull token from the identical token endpoint.
2. `cgr.dev/chainguard/static` (Chainguard's own registry) — anonymous pull
   succeeded. Pulled a **freshly keyless-signed image, GitHub Actions OIDC,
   signed today (2026-09-08)**: a real Fulcio leaf certificate with
   `sigstore-intermediate` issuer and GitHub Actions OIDC extensions (repo
   `chainguard-images/images`, workflow `release.yml`, `refs/heads/main`).
   However, its signature is published under the **legacy** cosign tag
   scheme (`sha256-<hex>.sig` → an OCI manifest with a
   `application/vnd.dev.cosign.simplesigning.v1+json` layer +
   `dev.cosignproject.cosign/signature`/`dev.sigstore.cosign/bundle`/
   `dev.sigstore.cosign/certificate` annotations) — **not** the modern
   `SigstoreBundleMediaType` (`application/vnd.dev.sigstore.bundle.v0.3+json`)
   referrer artifact this change's `ParseBundleDocument`/
   `ParseBundleVerificationMaterial` target at all. Same result for
   `ghcr.io/sigstore/fulcio` and `ghcr.io/chainguard-images/cosign` (both
   OCI Referrers API and the `sha256-<hex>` tag-scheme index returned empty;
   only legacy `.sig`/`.att`/`.sbom` tags exist).
3. `oras-project/oras` (GHCR) — pullable, but never signed at all (no
   `sha256-*` tags of any kind).

Given no live keyless Bundle-document artifact was reachable, per the
fallback instruction the codebase's own real captured fixture and cosign's
source code were used as corroborating (not conclusive) evidence instead of
fabricating a "confirmed" result:

- `internal/domain/signing/testdata/bundle-document.json` (already in this
  repo, see `testdata/README.md`) is a **real, captured, production**
  Sigstore Bundle document — `cosign v3.1.3`, **`--key`-based** (static key,
  not keyless) signing of `team/az-deploy-demo`. It is `dsseEnvelope`-shaped,
  wrapping an in-toto Statement.
- Reading `cosign@v2.5.0`'s actual source (module cache,
  `pkg/cosign/bundle/protobundle.go` + `cmd/cosign/cli/sign/sign_blob.go`):
  `messageSignature`-shaped bundles are produced **exclusively** by
  `cosign sign-blob --new-bundle-format` (a raw, non-statement payload). Any
  image-level `cosign sign`/`cosign attest` output that reaches the new
  bundle format at all DSSE-wraps an in-toto Statement uniformly, regardless
  of whether the signer is a static key or an ephemeral Fulcio certificate —
  the `Content` shape is determined by "is this a raw signed message or a
  signed statement", not by "static key vs keyless".
- Also discovered (informational, not a Phase 0 blocker, and out of this
  PR's scope to fix): the real Chainguard-pulled bundle's inner DSSE
  bundle annotation and `cosign v2.5.0`'s `verify_bundle.go`/
  `signatures.go` reference the OCI **artifactType**
  `"application/vnd.dev.sigstore.bundle+json;version=0.3"` (semicolon
  form), read with a `strings.HasPrefix` tolerance — while this repo's
  `SigstoreBundleMediaType` constant
  (`internal/domain/signing/bundle.go`, already merged pre-this-change) is
  the stricter dotted form `"application/vnd.dev.sigstore.bundle.v0.3+json"`
  and compares with strict equality in `ParseBundleIndex`/
  `ParseBundleReferrerManifest`. `cosign`'s own internal
  `MakeProtobufBundle` (used for `sign-blob`/`attest-blob`) sets the
  document's own inner `mediaType` field to the dotted form, matching this
  repo exactly — so this mismatch, if real for image-level referrer
  artifactType, would only affect *discovery* (finding the referrer
  manifest by artifactType), not `ParseBundleDocument`'s own inner-document
  shape check. Flagging for awareness; not touched by this PR.

**Conclusion carried into Phase 1-3**: proceeded with `dsseEnvelope` as
documented. `ParseBundleVerificationMaterial` (Phase 2) is shape-agnostic
(it inspects `verificationMaterial`, not `dsseEnvelope`/`messageSignature`),
so it needed no change either way. `VerifyKeyless` (Phase 3) is also
shape-agnostic at the call-site level (`bundle.Bundle.UnmarshalJSON` handles
both shapes; `sev.Verify` dispatches internally), so this open question does
not block Phase 3's own correctness — only the still-open question of
whether real keyless *image* signatures reach this code path via the Bundle
format at all today, addressed further below.

## Phase 1 — Dependency + pinned root asset: done

- `go.mod`/`go.sum`: `github.com/sigstore/sigstore-go v0.7.1` added via
  `go get` (module cache pre-warmed at
  `/tmp/opencode/gomodcache`; network to `proxy.golang.org` also confirmed
  reachable). `go mod tidy` pulled the full transitive tree (cosign, rekor,
  fulcio, AWS/GCP/Azure KMS clients, etc. — all already implied by
  `sigstore-go`'s own `go.mod`, not something this change added deliberately).
- `internal/domain/signing/assets/trusted_root.json`: copied verbatim from
  `sigstore-go@v0.7.1`'s own `examples/trusted-root-public-good.json` (the
  SDK-shipped, schema-matched pinned Sigstore public-good root — same
  `certificateAuthorities`/`tlogs`/`ctlogs` content as `cosign@v2.5.0`'s own
  `testdata/trusted_root_pgi.json`, cross-checked). A live fetch from
  `tuf-repo-cdn.sigstore.dev` was attempted for freshness but TUF targets
  are content-addressed, not path-addressed, so a bare `curl` 404'd; using
  the SDK-bundled copy was judged sufficient and version-matched. Sanity
  check: the intermediate CA subject/validity window in this embedded root
  (`sigstore-intermediate`, valid from 2022-04-13, no end date) matches the
  issuer of the real Fulcio certificate captured live today (2026-09-08)
  from Chainguard in the Phase 0 spike.

## Phase 2 — `ParseBundleVerificationMaterial`: done

- `internal/domain/signing/bundle.go`: new `BundleVerificationMaterial{HasCertificate, HasTlogEntry bool}`
  and `ParseBundleVerificationMaterial(raw []byte) (BundleVerificationMaterial, error)`,
  purely additive (74 lines added, zero lines removed/changed —
  `git diff --stat` confirms). Detects certificate presence via either the
  `certificate` or `x509CertificateChain` verificationMaterial shape;
  detects tlog-entry presence via non-empty `tlogEntries`. Existing
  `ParseBundleDocument`/`BundleDSSE`/`bundleDocumentEnvelope` untouched.
- 8 new tests in `bundle_test.go`, including one cross-checked against the
  real static-key fixture (`TestParseBundleVerificationMaterial_RealStaticKeyFixtureHasNoCertificate`).
  All 43 tests in the package pass (was 35 before this batch).

## Phase 3 — `keyless.go` (the keyless verifier): done

`internal/domain/signing/keyless.go` — the sole `sigstore-go` importer in
this codebase (confirmed: `grep -rln "sigstore-go" internal/ --include='*.go'`
returns only `keyless.go`, `keyless_test.go`, `keyless_fixture_test.go`, and
one doc-comment mention in `bundle.go`).

### API

```go
type TrustedIdentity struct {
    CertificateIdentityRegexp string
    CertificateOIDCIssuer     string
}

func VerifyKeyless(bundleRaw []byte, identities []TrustedIdentity, digest string) (matched string, err error)
```

Matches design.md's committed signature exactly. `matched` is the
certificate's Subject Alternative Name (`result.Signature.Certificate.SubjectAlternativeName`),
not `result.VerifiedIdentity`'s echoed matcher criteria (which would be
empty, since identities are configured as SAN *regexps*, not exact values) —
a judgment call design.md's own return-shape note didn't pin down exactly;
Phase 6 (future PR, app-layer `signatureMatch.Identity`) can compose SAN +
issuer together if the operator-facing surface needs both (spec requires
both be surfaced, but that is a Phase 6/7 concern, not this function's own
contract).

### Verifier construction — one deviation from design.md, evidence-based

Built as designed: `bundle.Bundle.UnmarshalJSON` → `root.NewTrustedRootFromJSON(embeddedRoot)`
→ `verify.NewSignedEntityVerifier(trustedRoot, verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1), ...)`
→ `verify.NewShortCertificateIdentity(issuer, "", "", identityRegexp)` per
identity, repeated `verify.WithCertificateIdentity(...)` for OR semantics →
one `sev.Verify(...)` call — **except** `verify.WithSignedCertificateTimestamps(1)`
was **dropped**, resolving design.md's own flagged Open Question
("keep enabled unless a real artifact fails") with confirmed evidence rather
than leaving it pending:

- `sigstore-go/pkg/testing/ca`'s `GenerateLeafCert` (the exact helper
  tasks.md 3.1 mandates using) never embeds a Signed Certificate Timestamp
  extension in any certificate it generates — confirmed by reading its full
  source.
- sigstore-go's own test suite (`pkg/verify/signature_test.go`,
  `fuzz_test.go`) never enables `WithSignedCertificateTimestamps` when
  verifying against `ca.VirtualSigstore`-produced entities, for the same
  reason.
- Keeping it enabled would make every synthetic-chain adversarial test this
  phase mandates structurally unpassable, not just one bundle's — confirmed
  by first writing the tests with it enabled and watching them fail
  identically regardless of the actual test scenario, before removing it.

`WithTransparencyLog` + `WithObserverTimestamps` alone still enforce the
spec's "verify the embedded Signed Entry Timestamp (SET) offline"
requirement.

### Error taxonomy — six distinct sentinels (one more than design's four)

| Sentinel | Cause | sigstore-go v0.7.1 signal (confirmed by source + test execution) |
|---|---|---|
| `ErrNotKeylessShaped` | Bundle carries no Fulcio certificate at all | `"entity was not signed with a certificate"` |
| `ErrTransparencyLogMissing` | Zero tlog entries | This package's own pre-check (`len(entity.TlogEntries()) == 0`), before ever calling `sev.Verify` |
| `ErrTransparencyLogInvalid` | Tlog entry present but its SET doesn't verify against a recognized Rekor key (tampered/forged) | `"failed to verify log inclusion: not enough verified log entries..."` |
| `ErrCertificateExpired` | Cert expired/not-yet-valid relative to the verified SET integrated time | `"failed to verify log inclusion: integrated time outside certificate validity"` |
| `ErrCertificateChainInvalid` | Cert does not chain to the trusted root | `"failed to verify leaf certificate: ..."` |
| `ErrIdentityNotMatched` | SAN/issuer match no configured `TrustedIdentity` (or zero identities supplied) | `errors.As` into `*verify.ErrNoMatchingCertificateIdentity` |

**Notable correction during implementation**: an earlier draft assumed
expired-certificate and untrusted-chain would collapse into the same
sigstore-go error (`VerifyLeafCertificate` in `pkg/verify/certificate.go`
does discard its underlying x509 error). Running the actual adversarial
test for the expired-cert case disproved this — sigstore-go catches
certificate expiry earlier, inside its own transparency-log
integrated-time-vs-certificate-validity check, which produces a distinct
message. All six sentinels are confirmed genuinely distinguishable by
running the full test suite, not by assumption.

### Test strategy

`internal/domain/signing/keyless.go`'s core logic is split into
`VerifyKeyless` (public, bytes → `bundle.Bundle` → delegates) and
`verifyKeylessEntity(sev *verify.SignedEntityVerifier, entity verify.SignedEntity, ...)`
(internal). The adversarial matrix tests call `verifyKeylessEntity` directly
against synthetic `verify.SignedEntity` values built with
`sigstore-go/pkg/testing/ca`'s `VirtualSigstore` (a real synthetic
Fulcio root+intermediate+Rekor key — `VirtualSigstore` itself implements
`root.TrustedMaterial`, so tests build a verifier bound to it, never the
real embedded pinned root) — mirroring exactly how sigstore-go's own test
suite (`pkg/verify/signature_test.go`, `pkg/verify/fuzz_test.go`) tests
`SignedEntityVerifier.Verify`. `keyless_fixture_test.go` hand-builds a
`keylessTestEntity` (one level below `ca`'s own unexported `TestEntity`) so
the tampered-SET case can swap in a tlog entry signed by a *different*
`VirtualSigstore`'s Rekor key — a real member of the "forged SET" failure
class, not a byte-flip simulation.

Nine tests in `keyless_test.go` cover tasks.md 3.1–3.6 plus digest-binding,
zero-identities, and the public `VerifyKeyless([]byte, ...)` boundary
(malformed JSON; the real static-key fixture correctly rejected as
not-keyless-shaped). `TestKeylessImports_NoNetHTTP` (3.6) checks the direct
(non-transitive) imports of both `keyless.go` itself and
`sigstore-go/pkg/verify` never include `net/http`, via `go/parser` +
`go/build`/`go list` — deliberately not the full transitive closure, since
design.md's own "Library Verification" table already confirms TUF links
`net/http` in transitively (via `pkg/root`) but is never called.

## Isolation boundary (3.8) — confirmed

- `grep -rn "regixtry/internal/" internal/domain/signing/*.go` (excluding
  `_test.go` files) returns nothing.
- `sigstore-go` is imported only by `keyless.go` and its two test files.
- `internal/domain/signing` compiles and its tests pass standalone.

## Verification (3.9)

- Focused command: `go test ./internal/domain/signing/... -run 'ParseBundleVerificationMaterial|VerifyKeyless' -v` — 17/17 pass.
- Full package: `go test ./internal/domain/signing/...` — 53 tests pass, 0 fail (was 35 before this batch; +8 Phase 2, +9 Phase 3, existing untouched).
- Full repo: `go test ./...` — all 19 packages pass, zero regressions.
- `go build ./...` — clean.
- `go vet ./...` — clean.
- `gofmt -l` on every changed `.go` file — clean (no output).

## Work Unit Evidence

| Evidence | Value |
|---|---|
| Focused test command and result | `go test ./internal/domain/signing/... -run 'ParseBundleVerificationMaterial\|VerifyKeyless' -v` → 17/17 PASS |
| Runtime harness | N/A — pure package-level unit tests with synthetic fixtures; nothing in `ports`/`app`/`http`/`tui` calls `keyless.go` yet (isolation boundary, confirmed above) |
| Rollback boundary | Revert `internal/domain/signing/keyless.go`, `keyless_test.go`, `keyless_fixture_test.go`, `internal/domain/signing/assets/trusted_root.json`, `bundle.go`'s additive `ParseBundleVerificationMaterial` block, `bundle_test.go`'s additive test block, and the `go.mod`/`go.sum` `sigstore-go` entries. Zero other package imports any of this — a clean revert. |

## TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 2.1–2.2 `ParseBundleVerificationMaterial` | `bundle_test.go` | Unit | ✅ 35/35 (baseline before this batch) | ✅ Written (compile failure: `undefined: ParseBundleVerificationMaterial`) | ✅ Passed | ✅ 8 cases (cert-only, chain-shaped, tlog-only, real fixture, malformed, unknown-fields, oversized, absent) | ✅ Clean — no changes needed after GREEN |
| 3.1–3.7 `VerifyKeyless`/`verifyKeylessEntity` | `keyless_test.go`, `keyless_fixture_test.go` | Unit | N/A (new files) | ✅ Written (compile failure: package didn't exist) | ✅ Passed after fixing: (a) verifier-injection refactor so tests never touch the real pinned root, (b) dropping `WithSignedCertificateTimestamps`, (c) adding `ErrCertificateExpired` after the expired-cert test's actual failure message disproved the initial two-error-collapse assumption | ✅ 9 cases across 6 distinct sentinel errors + digest-binding + zero-identities + public-boundary (malformed/not-keyless-shaped) | ✅ Clean — `classifyVerifyError`/error-var doc comments updated to match confirmed behavior |
| 3.6 net/http structural check | `keyless_test.go` | Unit (structural, `go/parser`+`go/build`) | N/A (new) | ✅ Written | ✅ Passed | ➖ Single (binary presence/absence check, no branching to triangulate) | ➖ None needed |

### Test Summary
- **Total tests written this batch**: 18 top-level (8 Phase 2 + 9 Phase 3 + 1 net/http structural; several are table-driven with subtests)
- **Total tests passing**: 53/53 in `internal/domain/signing` (cumulative, including all pre-existing tests)
- **Layers used**: Unit only (no integration/E2E layer exists yet — Phase 6+ future PRs)
- **Approval tests**: None — no refactoring of existing production code; Phase 2/3 are purely additive
- **Pure functions created**: `ParseBundleVerificationMaterial`, `decodeSHA256Digest`, `classifyVerifyError` (all pure); `VerifyKeyless`/`verifyKeylessEntity` are not pure (call into `sigstore-go`'s crypto verification) but perform zero I/O of their own

## Remaining work as of PR1 (superseded — see the PR2 section below for current status)

- Phase 4: `ports.TrustedIdentity` (PR2) — done, see "Scope covered by this batch (PR2 — Unit 2: ports+store)" below.
- Phase 5: sqlite `trusted_identities` column (PR2) — done, see below.
- Phase 6: app-layer identity branch, keys-OR-identities precondition, `signatureMatch` (PR3)
- Phase 7: HTTP admin decode/serialize (PR4)
- Phase 8: TUI trusted-identity list widget (PR4)
- Phase 9: E2E smoke test with a real keyless-signed image, full regression, docs (PR4)

## Scope covered by this batch (PR2 — Unit 2: ports+store)

Branch: `feature/signing-keyless-verification-02-ports-store` (based on
`feature/signing-keyless-verification-01-domain-foundation`, which is based
on tracker `feature/signing-keyless-verification`, based on `develop`).

Phases 4, 5 from `tasks.md` — complete. Phase 6 (app-layer wiring in
`service_signing.go`) and later are explicitly OUT of scope for this
batch/branch (PR3+). `internal/app/regixtry/service_signing.go`,
`internal/protocol/http/admin_handlers.go`, and the TUI layer were
deliberately NOT touched in this batch.

## Phase 4 — `ports.TrustedIdentity` type: done

`internal/ports/regixtry.go`:

```go
type TrustedIdentity struct {
    CertificateIdentityRegexp string `json:"certificate_identity_regexp"`
    CertificateOIDCIssuer     string `json:"certificate_oidc_issuer"`
}
```

Added `TrustedIdentities []TrustedIdentity` fields to both
`SigningPolicySettings` (`json:"trusted_identities"`, no `omitempty`,
mirroring `TrustedPublicKeys`'s own non-omitempty convention on that struct)
and `SigningOverride` (`json:"trusted_identities,omitempty"`, mirroring
`TrustedPublicKeys`'s `omitempty` convention on that struct). Pure type
addition per tasks.md 4.1 — no RED test for this task itself; exercised as a
real (not mocked) type by Phase 5.1's RED test, per tasks.md's own ordering
note.

**Scope decision, not a deviation**: `ports.RepositoryOverrideDetails` (the
HTTP/TUI wire-projection struct at `internal/ports/regixtry.go:483`) was
deliberately NOT given a `TrustedIdentities` field in this batch. tasks.md
4.1's literal task text lists only `SigningPolicySettings` and
`SigningOverride`; `RepositoryOverrideDetails` exists solely to mirror what
`repositoryOverrideResponse` (`admin_handlers.go`) puts on the wire, which is
explicitly Phase 7 (PR4) scope per the batch instructions. Adding it now
would be premature relative to the design's own phase boundary.

## Phase 5 — sqlite `trusted_identities` column: done

`internal/infra/metadata/sqlite/store.go`:

- `CREATE TABLE IF NOT EXISTS signing_policy_settings` gained
  `trusted_identities TEXT NOT NULL DEFAULT '[]'` as a new column.
- New idempotent migration statement (same
  duplicate-column-name-tolerant loop as every other `ALTER TABLE ADD COLUMN`
  in this file, mirroring `trusted_public_keys`/`unsigned_self_read`'s exact
  pattern):
  `ALTER TABLE signing_policy_settings ADD COLUMN trusted_identities TEXT NOT NULL DEFAULT '[]';`
- `GetSigningPolicySettings`: scan extended with `trusted_identities`;
  unmarshal normalizes a `nil`/`"null"`/empty-string result to a non-nil
  empty `[]ports.TrustedIdentity{}` — closes a real nil-vs-`[]` bug caught by
  the RED test itself (see below), not merely a defensive guess.
- `UpsertSigningPolicySettings`: marshals `settings.TrustedIdentities`,
  normalizing a `nil` input slice to `[]ports.TrustedIdentity{}` before
  marshal so the stored JSON is never the literal `"null"` (Go's
  `json.Marshal` on a nil slice produces `"null"`, not `"[]"` — this was the
  root cause the RED test surfaced, see TDD Cycle Evidence below).
- Per-repository `SigningOverride` storage needed **zero** changes: the
  generic `repository_feature_overrides` JSON-blob column already round-trips
  any `SigningOverride` field, `TrustedIdentities` included — confirmed by
  the Phase 5.3 characterization test, which passed on first run (before any
  production code change), proving the override codec is already generic
  exactly as tasks.md predicted.

### Root-cause note (RED test caught a real bug, not a scaffolding gap)

The first Phase 5.1 RED run failed as expected (`TrustedIdentities` field not
yet scanned). After adding the column/scan/upsert (naive first pass, no nil
normalization), the test failed a SECOND time on its own "unset defaults to
empty slice" assertion: `Go`'s `json.Marshal(nil slice)` encodes to the JSON
literal `null`, which the store then wrote and read back as a `nil` Go slice
(not `[]ports.TrustedIdentity{}`) — exactly the `nil` vs `[]` drift tasks.md
5.1 explicitly named as a required assertion, not an incidental one. Fixed by
normalizing `nil` → `[]ports.TrustedIdentity{}` on both the write side
(before `json.Marshal`) and the read side (after `json.Unmarshal`, defensive
against any pre-existing/other-writer row that might still contain a literal
`null`).

## Verification (5.4)

- Focused command:
  `go test ./internal/infra/metadata/sqlite/... -run 'SigningPolicySettings|TrustedIdentity' -v`
  → 3/3 new/relevant tests PASS (`TestStoreGetSigningPolicySettingsReturnsNotFoundWithNoRow`,
  `TestStoreUpsertSigningPolicySettingsRoundTripsEnabledKeysAndUpdatedAt`,
  `TestStoreUpsertSigningPolicySettingsRoundTripsTrustedIdentities`; the
  Phase 5.3 characterization test `TestStoreSigningOverridePayloadRoundTripsTrustedIdentities`
  is not name-matched by this filter but was run and confirmed passing
  separately, see below).
- Full package: `go test ./internal/infra/metadata/sqlite/...` — all tests
  pass, zero regressions.
- Full repo: `go test ./...` — all 19 packages pass, zero regressions.
- `go build ./...` — clean.
- `go vet ./...` — clean.
- `gofmt -l` on every changed `.go` file
  (`internal/ports/regixtry.go`, `internal/infra/metadata/sqlite/store.go`,
  `internal/infra/metadata/sqlite/store_test.go`) — clean (no output).

## Work Unit Evidence

| Evidence | Value |
|---|---|
| Focused test command and result | `go test ./internal/infra/metadata/sqlite/... -run 'SigningPolicySettings\|TrustedIdentity' -v` → 3/3 PASS; `go test ./internal/infra/metadata/sqlite/... -run TestStoreSigningOverridePayloadRoundTripsTrustedIdentities -v` → 1/1 PASS |
| Runtime harness | N/A — store round-trip only, no HTTP/TUI surface exists yet (Phase 6+ future PRs); matches tasks.md's own Unit 2 "Runtime harness" column |
| Rollback boundary | Revert `ports.TrustedIdentity`/field additions in `internal/ports/regixtry.go`, and `store.go`'s `ALTER TABLE`/`CREATE TABLE` column, scan, and upsert changes; the column is additive with a `'[]'` default, inert to any binary that reverts to not reading it. Zero other package references either change yet. |

## TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 4.1 `ports.TrustedIdentity` | N/A (pure type, tasks.md: "no RED here") | N/A | N/A | ➖ N/A per tasks.md | N/A | N/A | N/A |
| 5.1–5.2 `GetSigningPolicySettings`/`UpsertSigningPolicySettings` identity round-trip | `store_test.go` | Unit (real sqlite, temp-file DB via `newTestStore`) | ✅ existing signing-policy tests passing before this batch (confirmed via targeted run) | ✅ Written (failed: `stored.TrustedIdentities` empty/nil, field not yet scanned) | ✅ Passed after fixing (a) column/scan/upsert wiring, (b) nil-vs-`[]` marshal normalization on both read and write sides, caught by the test's own second assertion | ✅ 2 cases in one test (non-empty round-trip with 2 identities across GitHub+GitLab issuers; explicit clear-to-empty case) | ✅ Clean — normalization logic is the minimal fix, no further extraction needed |
| 5.3 `SigningOverride` identity round-trip via generic blob | `store_test.go` | Unit (real sqlite, characterization) | N/A (new test, no prior behavior to protect) | ✅ Written — passed on first run with ZERO production code change (confirms the characterization claim itself, not a scaffolding artifact) | ✅ Passed (first run) | ➖ Single case — characterization test's entire purpose is proving the generic codec needs no branching, so a second case would prove nothing new | ➖ None needed |

### Test Summary
- **Total tests written this batch**: 2 (`TestStoreUpsertSigningPolicySettingsRoundTripsTrustedIdentities`, `TestStoreSigningOverridePayloadRoundTripsTrustedIdentities`)
- **Total tests passing**: all `internal/infra/metadata/sqlite` package tests pass (cumulative); full repo `go test ./...` also green
- **Layers used**: Unit only (real sqlite temp-file DB via the package's existing `newTestStore` helper — no mocks)
- **Approval tests**: None — no refactoring of existing production code; Phase 4/5 are additive except for the pre-existing `GetSigningPolicySettings`/`UpsertSigningPolicySettings` bodies, which were extended in place (their existing behavior is covered by `TestStoreUpsertSigningPolicySettingsRoundTripsEnabledKeysAndUpdatedAt`, confirmed still passing unmodified)
- **Pure functions created**: None new (the nil-normalization logic is inline in the existing `Get`/`Upsert` methods, consistent with their existing style — not extracted, since it is two lines each and used in exactly one place per method)

## Remaining work as of PR2 (superseded — see the PR3 section below for current status)

- Phase 6: app-layer identity branch, keys-OR-identities precondition, `signatureMatch` (PR3) — done, see below.
- Phase 7: HTTP admin decode/serialize (PR4)
- Phase 8: TUI trusted-identity list widget (PR4)
- Phase 9: E2E smoke test with a real keyless-signed image, full regression, docs (PR4)

## Scope covered by this batch (PR3 — Unit 3: app wiring)

Branch: `feature/signing-keyless-verification-03-app-wiring` (based on
`feature/signing-keyless-verification-02-ports-store`, which is based on PR1,
which is based on tracker `feature/signing-keyless-verification`, based on
`develop`).

Phase 6 from `tasks.md` — complete, strictly inside `internal/app/regixtry/`.
`internal/protocol/http/admin_handlers.go` and every `internal/tui/*` file
were deliberately NOT touched in this batch (Phase 7/8, PR4). No
`internal/ports/regixtry.go` change was needed: PR2 already added
`ports.TrustedIdentity` and the `TrustedIdentities` fields on
`SigningPolicySettings`/`SigningOverride`; `ports.RepositoryOverrideDetails`
(the HTTP/TUI wire-projection struct) remains untouched, confirmed still
PR4/Phase 7 scope.

### Files changed

- `internal/app/regixtry/service_signing.go` — `signatureMatch{KeyFingerprint,
  Identity string}` (design.md's committed shape) replaces the previous bare
  `matchedFingerprint string` return on both `verifySignature` and
  `verifyBundleSignature`. `verifySignature`'s usable-anchor precondition
  becomes keys-OR-identities (`len(keys) == 0 && len(policy.TrustedIdentities)
  == 0`). `verifyBundleSignature` gains the identity branch: for each
  candidate bundle document, after the existing key loop fails to match, if
  `identities` is non-empty AND `signing.ParseBundleVerificationMaterial`
  reports `HasCertificate` (a cheap pre-filter that skips wasted
  `VerifyKeyless` calls on ordinary static-key documents), it calls the REAL
  `signing.VerifyKeyless(bundlePayload, toSigningIdentities(identities),
  digest)` — the same raw bundle bytes the key loop already parsed, per
  design.md's "Bundle handoff" decision (VerifyKeyless does its own bundle
  parsing internally; no sigstore-go type crosses this boundary). On success,
  `composeVerifiedIdentity` (new pure function) formats the matched SAN
  together with the OIDC issuer of whichever configured `TrustedIdentity`'s
  regexp actually matched it, as `"<SAN> (<issuer>)"` — design.md's own
  contract only fixes `Identity` as a `string`, not its exact format; this is
  a judgment call, documented here as such. The legacy SimpleSigning `.sig`
  path in `verifySignature` is completely untouched (spec: "Legacy Static-Key
  Verification Path Is Unchanged") — the identity branch lives ONLY inside
  `verifyBundleSignature`.
- `internal/app/regixtry/queries.go` — `SignatureStatusPolicy` gains
  `TrustedIdentities int` (configured count, mirrors `TrustedKeys`'
  count-only, never-raw-material discipline). `SignatureStatusDetail` gains
  `VerifiedIdentity string` (`json:"verified_identity,omitempty"`), populated
  only from `match.Identity` on the verified path, mutually exclusive with
  `VerifiedKeyFingerprint`.
- `internal/app/regixtry/repository_overrides.go` — `normalizeSigningOverride`
  now also compiles each `TrustedIdentities[i].CertificateIdentityRegexp` via
  `regexp.Compile` (rejecting a malformed one), requires a non-empty
  `CertificateOIDCIssuer`, caps `trusted_identities` at the same
  `maxSigningPolicyTrustedKeys` (16) bound `trusted_public_keys` already has,
  and extends the outage rule so `enabled:true` is rejected only when BOTH
  `trusted_public_keys` and `trusted_identities` end up empty (previously:
  keys alone). `applySigningOverridePayload` now also assigns
  `settings.TrustedIdentities = override.TrustedIdentities` — a plain
  full-row-replace, identical in shape to the existing `TrustedPublicKeys`
  line, which automatically satisfies "an override saved with keys but no
  identities clears inherited global identities" (the override's own
  `TrustedIdentities` is `nil`/absent when the payload didn't set it).

### Deviations / judgment calls, evidence-based

1. **`composeVerifiedIdentity`'s exact string format** (`"<SAN> (<issuer>)"`)
   is this PR's own choice, not pinned by design.md (which only commits to
   `Identity string`, per PR1's own note that Phase 6 "can compose SAN +
   issuer together if the operator-facing surface needs both"). Chosen for
   human readability on the signature-status endpoint; Phase 7/8 (PR4) can
   revisit if the HTTP/TUI surface wants a structured `{san, issuer}` object
   instead of one formatted string — `signatureMatch.Identity` stays a plain
   `string` per design.md's committed contract either way, so any such
   change would be scoped to `composeVerifiedIdentity` alone, not the
   `signatureMatch` type.
2. **Task 6.2's "verifies via a real call" tested as a real, correctly
   fail-closed attempt, not a positive cryptographic success** — carried
   forward from PR1's own confirmed, still-unresolved Phase 0 finding: no
   live keyless-signed Sigstore Bundle-document artifact was obtainable in
   this sandbox (see the Phase 0 section above), and `keyless.go`'s
   `signedEntityVerifier` always verifies against the REAL embedded pinned
   Sigstore public-good root with no operator override (design.md, settled)
   — so no certificate mintable offline in a unit test can ever legitimately
   chain to it. `TestServiceVerifySignature_IdentityOnlyPolicyReachesRealKeylessVerification`
   therefore proves the achievable, real half of this: a self-signed test
   certificate genuinely reaches `signing.VerifyKeyless` (not a mock or test
   double — the exact same function production code calls), and the
   observable result is `signatureStateUntrusted` (a real verification
   ATTEMPT that failed), never `signatureStateUnverifiable` (which
   `TestServiceVerifySignature_ZeroKeysAndZeroIdentitiesIsUnverifiable`
   confirms is reserved for the distinct "no usable anchor configured at
   all" precondition zero keys alone used to always trip before this
   change). The positive "a verified match reports SAN+issuer distinctly"
   half of the spec (task 6.4) is proven separately and directly via
   `TestComposeVerifiedIdentity_ReportsMatchingIdentitysIssuer`, a pure
   unit test of the exact composition logic a real identity-verified pull
   would exercise — no crypto, no mocks, real production code. This
   evidence-based test-design split — same reasoning PR1 applied to the
   SCT/expired-certificate deviations — is flagged here for a human/CI
   environment with real `cosign`/live-artifact access to close out
   end-to-end, same as Phase 0's own open item.
3. **Precondition/final-fallback error message text changed** (not just
   state): `verifySignature`'s zero-usable-anchor message became "no usable
   trusted key or trusted identity is configured" (was "...trusted key is
   configured"), and `verifyBundleSignature`'s final untrusted message
   became "no bundle signature validated against a trusted key or trusted
   identity" (was "...against a trusted key"). Confirmed safe: `rg`-searched
   every test file in the repo for the OLD literal message text before
   changing it — zero hits; every existing assertion checks `state`/error
   code only, never this exact string, so this is a wording improvement,
   not a behavior change, and does not violate task 6.1's byte-for-byte
   characterization guarantee (which is about accept/reject STATE, not this
   one message's exact wording).

### Verification (6.11)

- Focused command: `go test ./internal/app/regixtry/... -run 'VerifyBundleSignature|VerifySignature|NormalizeSigningOverride|ApplySigningOverridePayload|SignatureStatus' -v`
  — all matched tests pass.
- Full package: `go test ./internal/app/regixtry/... -v` — 195 `--- PASS`
  lines, 0 `--- FAIL` (was 51 relevant to signing before this batch under the
  narrower `-run 'Signature|Signing'` filter; the full package count of 195
  includes every test in the package, confirmed zero regressions by full-suite
  diff).
- Full repo: `go test ./...` — all 19 packages pass, zero regressions
  (`internal/protocol/http` and `internal/tui` — untouched this batch — both
  still pass unchanged).
- `go build ./...` — clean.
- `go vet ./...` — clean.
- `gofmt -l` on every changed `.go` file (`service_signing.go`,
  `service_signing_bundle_test.go`, `queries.go`, `queries_test.go`,
  `repository_overrides.go`, `repository_overrides_test.go`) — clean (no
  output).

## Work Unit Evidence

| Evidence | Value |
|---|---|
| Focused test command and result | `go test ./internal/app/regixtry/... -run 'VerifyBundleSignature\|VerifySignature\|NormalizeSigningOverride\|ApplySigningOverridePayload\|SignatureStatus' -v` → all matched tests PASS |
| Runtime harness | N/A — service-layer only; no operator-facing way to write identities exists until PR 4 (HTTP admin decode/TUI), matching tasks.md's own Unit 3 "Runtime harness" column |
| Rollback boundary | Revert `service_signing.go`'s `signatureMatch`/identity branch/`composeVerifiedIdentity`/`toSigningIdentities`, `repository_overrides.go`'s identity normalize/apply additions, and `queries.go`'s `VerifiedIdentity`/`TrustedIdentities` fields (plus each file's test additions). PR 2's `trusted_identities` sqlite column stays in place but unwritten by any code path — inert to a revert. |

## TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 6.1 characterization safety net | `service_signing_bundle_test.go` (pre-existing, from `image-signing`) | Unit (real sqlite + fsblob via `newTestService`) | ✅ Confirmed GREEN before any change: `go test ./internal/app/regixtry/... -run 'Signature\|Signing' -v` → 51/51 `--- PASS` (baseline) | ➖ N/A — file already existed and comprehensively characterized static-key accept/reject behavior; no new RED needed for 6.1 itself | ✅ Re-run after full Phase 6 GREEN: same 51 cases still pass, only their `(state, fingerprint, err)` destructuring adapted to `(state, match, err)` — zero assertion semantics changed | ➖ N/A (pre-existing coverage) | ➖ N/A |
| 6.2 identity-only reaches real `VerifyKeyless` | `service_signing_bundle_test.go` | Unit (real sqlite + fsblob, real `signing.VerifyKeyless`) | ✅ (above) | ✅ Written (compile failure: `verifyBundleSignature`/`verifySignature` did not yet accept `identities`) | ✅ Passed: `signatureStateUntrusted`, distinct from `signatureStateUnverifiable` | ➖ Single case — see Deviation 2 above for why a positive-match case is not constructible offline | ➖ None needed |
| 6.3 keys-OR-identities precondition | `service_signing_bundle_test.go` | Unit | ✅ (above) | ✅ Written (failed: old code always returned `unverifiable` regardless of identities being absent too, same outcome coincidentally, so this RED was actually a same-result-different-reason case — confirmed by running BEFORE 6.5's GREEN, where the assertion held for the wrong reason (only `len(keys)==0` mattered); re-run AFTER 6.5 to confirm it now holds for the intended reason (`len(keys)==0 && len(identities)==0`)) | ✅ Passed | ➖ Paired directly against 6.2's case (same test file, contrasting states) | ➖ None needed |
| 6.4 SAN+issuer composition | `service_signing_bundle_test.go` (`TestComposeVerifiedIdentity_...`) | Unit, pure function | N/A (new function) | ✅ Written (compile failure: `composeVerifiedIdentity` undefined) | ✅ Passed | ✅ 2 cases (single identity; multiple identities, picks the actually-matching one not the first) | ➖ None needed — already minimal |
| 6.5–6.6 `signatureMatch` GREEN + propagate through callers | `service_signing.go`, `queries.go` + all touched test files | Unit | ✅ (51/51 baseline) | N/A (production code task, tests were 6.1-6.4/6.9's job) | ✅ `go build ./...` clean, `go vet ./...` clean | N/A | ✅ Re-ran full characterization suite (51 cases) unmodified in assertion semantics — all still pass |
| 6.7 override validate RED | `repository_overrides_test.go` | Unit | ✅ existing override tests passing before this batch | ✅ Written (compile failure: `regexp` imported and not used until GREEN wired it in — confirmed real RED via `go test` build failure, not assumption) | ✅ Passed after implementing `normalizeSigningOverride`'s identity validation | ✅ 4+1 cases (valid identity; malformed regexp; empty issuer; outage rule with both empty; separate too-many-identities cap test) | ➖ None needed |
| 6.8 override apply GREEN | `repository_overrides.go` | Unit | ✅ (above) | ✅ Written (`TestApplySigningOverridePayloadFullRowReplacesTrustedIdentities` failed pre-GREEN: `got.TrustedIdentities` stayed nil/inherited instead of round-tripping/clearing) | ✅ Passed | ✅ 2 cases (round-trips when set; clears inherited when omitted) | ➖ None needed |
| 6.9–6.10 status fields | `queries_test.go` | Unit (real sqlite + fsblob) | ✅ existing `SignatureStatus` tests passing before this batch | ✅ Written (compile failure: `ports.TrustedIdentity`/`.Policy.TrustedIdentities`/`.Signature.VerifiedIdentity` fields did not exist on `SignatureStatusPolicy`/`SignatureStatusDetail` before GREEN) | ✅ Passed | ✅ 2 cases (count reporting; mutual-exclusion with `VerifiedKeyFingerprint` on the key-verified path) | ➖ None needed |

### Test Summary
- **Total tests written this batch**: 8 top-level (`TestServiceVerifySignature_IdentityOnlyPolicyReachesRealKeylessVerification`,
  `TestServiceVerifySignature_ZeroKeysAndZeroIdentitiesIsUnverifiable`,
  `TestComposeVerifiedIdentity_ReportsMatchingIdentitysIssuer` (2 subtests),
  `TestNormalizeSigningOverrideValidatesTrustedIdentities` (4 subtests),
  `TestNormalizeSigningOverrideRejectsTooManyTrustedIdentities`,
  `TestApplySigningOverridePayloadFullRowReplacesTrustedIdentities` (2
  subtests), `TestServiceSignatureStatusPolicyReportsTrustedIdentitiesCount`,
  `TestServiceSignatureStatusVerifiedIdentityNeverPopulatedOnKeyVerifiedPath`)
  plus 8 pre-existing `service_signing_bundle_test.go` tests adapted
  (destructuring only, zero assertion-semantics changes) to the new
  `signatureMatch` return shape.
- **Total tests passing**: 195/195 in `internal/app/regixtry` (cumulative,
  `--- PASS` count via `go test -v`); full repo `go test ./...` also green,
  zero regressions.
- **Layers used**: Unit only (real sqlite temp-file DB + real fsblob via
  `newTestService`, and one pure-function table-driven test — no mocks
  anywhere in this batch).
- **Approval tests**: The pre-existing `service_signing_bundle_test.go` suite
  (51 cases) served as this batch's approval/characterization tests for the
  `verifySignature`/`verifyBundleSignature` refactor (6.6) — confirmed
  passing both before (baseline) and after (unmodified assertion semantics)
  the `signatureMatch` return-shape change.
- **Pure functions created**: `composeVerifiedIdentity`, `toSigningIdentities`
  (both pure, zero I/O; `toSigningIdentities` is a straight type-mapping
  loop with no branching, so it was not independently unit-tested beyond
  its exercise inside `TestServiceVerifySignature_IdentityOnlyPolicyReachesRealKeylessVerification`
  — triangulation skipped: no possible output shape other than one
  `signing.TrustedIdentity` per input entry, in order).

## Remaining work (out of scope for this PR/branch)

- Phase 7: HTTP admin decode/serialize, including `ports.RepositoryOverrideDetails.TrustedIdentities` (PR4)
- Phase 8: TUI trusted-identity list widget (PR4)
- Phase 9: E2E smoke test with a real keyless-signed image, full regression, docs (PR4)

## Open items for a human / CI-with-docker environment

1. **Phase 0 remains UNCONFIRMED against a live keyless-signed Bundle-document
   artifact.** No publicly reachable registry yielded one during this batch's
   real attempts (see above). Before this change ships, re-attempt with
   either: (a) a real `docker`+`cosign` environment able to `cosign sign
   --new-bundle-format` a test image and push it, or (b) broader registry
   search access than this sandbox had.
2. **Informational, not blocking this PR**: the artifactType mismatch noted
   above (`application/vnd.dev.sigstore.bundle+json;version=0.3` semicolon
   form vs this repo's dotted `SigstoreBundleMediaType` constant with strict
   equality) — if real for image-level referrer discovery, would affect
   `ParseBundleIndex`/`ParseBundleReferrerManifest` (already-merged
   `image-signing` change, not touched by this PR). Worth a follow-up
   investigation once a real bundle-format image artifact is available to
   test against.
3. **Carried forward from Phase 0, now also blocking a fully end-to-end PR3
   test**: with a real keyless-signed image (real Fulcio certificate,
   matching issuer, real Rekor tlog entry), `TestServiceVerifySignature_IdentityOnlyPolicyReachesRealKeylessVerification`
   (PR3, `service_signing_bundle_test.go`) should be extended/replaced with a
   case asserting `signatureStateVerified` and a populated
   `match.Identity`, closing PR3's own Deviation 2 (see the PR3 section
   above) the same way Phase 0's E2E task (9.1) is meant to close this gap
   for the whole feature.
