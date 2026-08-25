# Design: Image Signature Verification Gate (image-signing)

## Technical Approach

One new global settings table + two `MetadataStore` methods, one new I/O-free
domain package (`internal/domain/signing`) holding every stdlib crypto and
cosign-format primitive, one generic resolution helper that unblocks a second
override target type without touching a single shipped call site, one
fail-closed gate beside `enforceScanPolicy` in `OpenManifest`, one registry
route, one admin resource, and three TUI touch points. Every line reference
below was read from the current tree, not estimated.

The verification path is a pure projection over data regixtry already stores:
the `.sig` tag is an ordinary manifest (`manifests` table), and the signed
payload is an ordinary blob (`fsblob`). Nothing on the push path changes.

**One thing in this design is not fully verifiable in this phase.** The exact
byte layout of cosign's signature artifact is stated below from documented
upstream behavior, but this phase has **no shell, no network, and no `cosign`
binary**, so it could not be confirmed against a real `cosign sign` output.
See "Decision 1" and "Open Questions" — this is a hard apply-time checkpoint,
not a caveat to skim.

## Architecture Decisions

### Decision 1: cosign's static-key signature format, pinned as precisely as this phase can

This is the crux the proposal flagged (open question 4). The claims below are
from upstream cosign/sigstore documented behavior. They are stated at the
byte level so that apply-time verification is a diff, not a re-derivation.

**1a — What is signed.** `cosign sign --key cosign.key <repo>@<digest>` builds a
SimpleSigning JSON document (sigstore's `payload.SimpleContainerImage`), uploads
it as a blob, and signs **that blob's exact bytes**:

```json
{"critical":{"identity":{"docker-reference":"<repo ref>"},"image":{"docker-manifest-digest":"sha256:<hex>"},"type":"cosign container image signature"},"optional":null}
```

The signature is over `SHA-256(payload_bytes)`, where `payload_bytes` are the
blob's bytes verbatim.

> **Load-bearing consequence**: verification MUST hash the blob bytes as read
> from the store. Decoding the JSON into a Go struct and re-marshalling it
> produces different bytes (key order, whitespace, `omitempty`) and every
> signature would fail. `internal/domain/signing.Verify` therefore takes
> `payload []byte` and never a struct.

**1b — How it is attached.** cosign pushes an OCI image manifest at the tag
`sha256-<hex>.sig` in the *same repository* (digest `sha256:<hex>` with the
`:` replaced by `-`). Its layers carry the signatures:

```jsonc
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "config": { "mediaType": "application/vnd.oci.image.config.v1+json", ... },
  "layers": [{
    "mediaType": "application/vnd.dev.cosign.simplesigning.v1+json",
    "digest": "sha256:<payload blob digest>",
    "size": 250,
    "annotations": {
      "dev.cosignproject.cosign/signature": "<base64(ASN.1 DER ECDSA sig)>"
    }
  }]
}
```

The signature annotation is **layer-level**, not manifest-level. Multiple
signatures over the same digest are multiple layers. Keyless-only annotations
(`dev.sigstore.cosign/certificate`, `/chain`, `/bundle`,
`/rfc3161timestamp`) are ignored entirely by this slice.

> **Load-bearing consequence**: `domain.Descriptor`
> (`internal/domain/regixtry/descriptor.go:3-7`) has **only** `MediaType`,
> `Digest`, `Size` — **no `Annotations` field**. regixtry's parsed
> `domain.Manifest.Layers` therefore *cannot* carry the signature. The
> verifier must re-parse `manifest.Payload`, the verbatim stored bytes
> (`NewManifest`, `manifest.go:41-52`, keeps a copy; `Validate`,
> `manifest.go:68-70`, proves it is byte-exact). Adding `Annotations` to
> `Descriptor` is explicitly **rejected**: it would change a domain type on
> the push path and the `manifest_blobs` persistence shape
> (`store.go:1121-1129`) for a read-only need that raw bytes already serve.

**1c — Key types.** `cosign generate-key-pair` produces an **ECDSA P-256**
key pair, signed with SHA-256, signature encoded as ASN.1 DER (`r`,`s`).

| Key type | v1 support | Reason |
|---|---|---|
| **ECDSA P-256 / SHA-256 / ASN.1 DER** | **Yes** | What `cosign generate-key-pair` produces by default; `ecdsa.VerifyASN1` is one stdlib call |
| Ed25519 | No — documented future | sigstore's Ed25519 signer signs the *raw message*, not a pre-hash, and an `ed25519ph` variant also exists; a second, differently-shaped code path in a fail-closed gate is not worth it for a key type `generate-key-pair` never emits |
| RSA (PKCS#1 v1.5 vs PSS) | No — documented future | The scheme is not recoverable from the public key alone; guessing wrong yields a silent verification failure that reads as "untrusted image" |

**A key that parses but is not ECDSA P-256 is rejected at configuration
time**, not at pull time — so an operator learns at `PUT` that their Ed25519
key is unsupported, instead of discovering it as a registry-wide outage.

**1d — Public key format.** `cosign.pub` is PEM, block type `PUBLIC KEY`,
body = PKIX/SPKI DER. Parsing is `pem.Decode` → `x509.ParsePKIXPublicKey` →
type-assert `*ecdsa.PublicKey` → assert `Curve == elliptic.P256()`. This is
exactly what an admin pastes from `cat cosign.pub`.

**Verification status of 1a–1d: DOCUMENTED, NOT EXECUTED, now cross-checked
against the authoritative spec.** The orchestrator fetched
`github.com/sigstore/cosign/specs/SIGNATURE_SPEC.md` directly (this phase had
no `cosign` binary, no shell, no network) and confirmed: the SimpleSigning
envelope shape, the `dev.cosignproject.cosign/signature` annotation at
**layer** level (not manifest level), the
`application/vnd.dev.cosign.simplesigning.v1+json` media type, and the
`sha256-<hex>.sig` tag convention all match 1a–1c above exactly. One
discrepancy worth flagging precisely: the spec's own example capitalizes the
digest field as `"Docker-manifest-digest"` (capital D), not
`"docker-manifest-digest"` — since the payload is hashed **verbatim, never
re-marshalled** (this is the entire point of Decision 1a's negative test),
this casing must be read from a real captured payload, not assumed from
either this document or the spec's prose example. The spec also shows
`"optional"` carrying `creator`/`timestamp` fields in its example, not `null`
— confirming the payload's exact byte content varies per invocation and MUST
come from a real captured fixture, never a hand-typed literal. This does not
change the required fixture-capture task below; it only raises confidence
that the overall shape it will confirm is correct. `sdd-tasks` MUST still
schedule a fixture-capture task **before** the parser task, not after:

> **Required task**: if a `cosign` binary is available in the apply
> environment, generate a key pair, sign a real test image pushed to a running
> regixtry, and capture (i) the raw `.sig` manifest bytes, (ii) the raw
> payload blob bytes, (iii) `cosign.pub`, as testdata. Use those exact bytes
> as the parser fixture. **Do not hand-construct a synthetic fixture from
> this document.** If no `cosign` binary is available, the task MUST record
> that fact in the verify report and the fixture stays explicitly marked
> `synthetic, unconfirmed against a real cosign output` — the same posture
> `repository-scan-config-overrides/design.md:630-644` took for the
> unavailable Trivy binary.

### Decision 2: Verification lives in `internal/domain/signing`, not `internal/infra/verification/cosignsig/`

**Deliberate divergence from the proposal's Affected Areas table.**

| Option | Tradeoff | Decision |
|---|---|---|
| `internal/domain/signing` — pure functions, zero I/O | Diverges from the proposal's stated path | **Chosen** |
| `internal/infra/verification/cosignsig/` + a `ports.SignatureVerifier` port | Matches the proposal's table, but invents a port + a fake for a deterministic pure function, and every test must wire a double | Rejected |
| `internal/infra/verification/cosignsig/`, imported directly by the app layer | Violates the layering `repository-scan-config-overrides/design.md:106-114` pins (the app package imports only stdlib + `domain` + `ports`) | Rejected |

The package is I/O-free by construction — all registry reads stay in the app
layer, which already owns `s.metadata.ResolveManifest` and `s.blobs.OpenBlob`
(`queries.go:259`). An adapter exists to hide I/O; there is none to hide.

```go
package signing // internal/domain/signing

const (
    SimpleSigningMediaType = "application/vnd.dev.cosign.simplesigning.v1+json"
    SignatureAnnotationKey = "dev.cosignproject.cosign/signature"
    SimpleSigningType      = "cosign container image signature"

    MaxSignatureManifestBytes = 256 << 10 // hot-path bound, Decision 8
    MaxPayloadBytes           = 1 << 20
    MaxSignatureEntries       = 64
)

// SignatureTag maps "sha256:<hex>" to cosign's legacy tag "sha256-<hex>.sig".
func SignatureTag(digest string) (string, error)

// SignatureEntry is one candidate signature read out of a .sig manifest.
type SignatureEntry struct {
    PayloadDigest string // layers[i].digest — the blob to fetch
    Signature     string // layers[i].annotations[SignatureAnnotationKey]
}

// ParseSignatureManifest reads the verbatim .sig manifest bytes and returns
// every simplesigning layer carrying a signature annotation, in order.
func ParseSignatureManifest(raw []byte) ([]SignatureEntry, error)

// NormalizePublicKeyPEM accepts a PEM public key in any whitespace
// arrangement (real newlines from an API body, spaces from the TUI's
// single-line field), rejects anything that is not ECDSA P-256, and returns
// the canonical PEM that gets stored. Configuration-time, never pull-time.
func NormalizePublicKeyPEM(raw string) (string, error)

// ParseTrustedKey decodes one canonical stored PEM.
func ParseTrustedKey(pemText string) (*ecdsa.PublicKey, error)

// Verify returns nil only on a valid signature. It never returns a bool: a
// caller under a fail-closed gate must not be able to read "error" as
// "allowed". Errors carry a fixed vocabulary and never echo key or payload
// bytes.
func Verify(key *ecdsa.PublicKey, payload []byte, signatureBase64 string) error

// CheckClaims asserts the signed payload actually binds the digest being
// pulled. Without it, a valid signature over ANY other image passes.
func CheckClaims(payload []byte, digest string) error
```

`CheckClaims` requires `critical.type == SimpleSigningType` **and**
`critical.image.docker-manifest-digest == <the resolved digest>`, exact match.
It deliberately does **not** enforce `critical.identity.docker-reference`:
the digest already binds the content, so a signature copied between
repositories still attests byte-identical bytes, while enforcing the
reference would break legitimate `cosign copy` and registry-migration flows.
Rejecting the reference check is a decision, not an omission.

### Decision 3: Trusted keys are inline PEM in the settings row, not server-local file paths

The proposal's open question 1 leaned toward paths on the
`TLSCACertPath`/`IgnoreFilePath` precedent. **Not adopted.**

| Option | Tradeoff | Decision |
|---|---|---|
| Inline PEM strings in the row | Second JSON column in this store | **Chosen** |
| Server-local paths (`TLSCACertPath` precedent) | Adds a filesystem read to the **pull hot path** under a **fail-closed** gate: a lost volume mount or rebuilt container turns into "every pull 403s" — an outage-shaped failure mode | Rejected |
| Separate `signing_trusted_keys(tenant, position, pem)` child table | Matches the store's typed-column idiom, but full-row-replace across two tables needs a transaction the store has no precedent for, for zero query benefit (no field is ever filtered or joined) | Rejected |

The `TLSCACertPath`/`IgnoreFilePath` precedent exists because those files are
consumed by an **external subprocess** that can only receive a path in argv.
There is no subprocess here — verification is in-process — so the precedent's
motivation does not transfer. Two further wins: public keys are not secret
(unlike `ScanSettings.AuthToken`, which is `json:"-"`, `ports/regixtry.go:104`),
so nothing new is exposed by storing them; and the entire "operator-supplied
string reaches argv" threat-matrix row from
`repository-scan-config-overrides/design.md:610` disappears, because no path
exists to inject.

### Decision 4: `SigningPolicySettings` mirrors `ScanPolicySettings`'s shape and inverts its default

```go
// internal/ports/regixtry.go, beside ScanPolicySettings (:91-95)

// SigningPolicySettings is the global image-signature verification policy:
// whether the pull-time content-trust gate is enforced, and the set of
// public keys a signature may verify against (any one is sufficient).
type SigningPolicySettings struct {
    Enabled           bool      `json:"enabled"`
    TrustedPublicKeys []string  `json:"trusted_public_keys"` // canonical PEM, ECDSA P-256
    UpdatedAt         time.Time `json:"updated_at"`
}

// SigningOverride is one repository's full replacement of the global signing
// policy (full-row-replace: present -> all fields apply).
type SigningOverride struct {
    Enabled           bool     `json:"enabled"`
    TrustedPublicKeys []string `json:"trusted_public_keys,omitempty"`
}
```

Storage mirrors `scan_policy_settings` (`store.go:1273-1279`) exactly — one
`CREATE TABLE IF NOT EXISTS` appended to `Store.init()`'s statement slice:

```sql
CREATE TABLE IF NOT EXISTS signing_policy_settings (
    tenant TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0,
    trusted_public_keys TEXT NOT NULL DEFAULT '[]',
    updated_at TEXT NOT NULL,
    PRIMARY KEY(tenant)
);
```

Two `MetadataStore` methods beside `Get/UpsertScanPolicySettings`
(`ports/regixtry.go:35-36`, `store.go:500-534`), row absence a typed
`domain.NewNotFoundError`, `updated_at` as `time.RFC3339Nano`:

```go
GetSigningPolicySettings(ctx context.Context, tenant string) (SigningPolicySettings, error)
UpsertSigningPolicySettings(ctx context.Context, tenant string, settings SigningPolicySettings) error
```

**`enabled … DEFAULT 0` deliberately inverts `scan_policy_settings`'
`DEFAULT 1` (`store.go:1275`), and the code-level default inverts
`GetScanPolicySettings`' (`service_scanning.go:56-62`).** This is the single
most important safety property in this change: a fail-closed gate defaulting
to ON with zero trusted keys would 403 every pull on every existing
deployment the moment the new binary boots. The missing-row default is
`{Enabled: false, TrustedPublicKeys: nil}` — the same "a missed boot path must
never silently produce the wrong answer" reasoning as the scan gate's comment,
pointing the opposite way because the posture is opposite.

`trusted_public_keys` is the store's second JSON column, and it is justified
by the same bounded-blast-radius argument as the first
(`repository-scan-config-overrides/design.md:44-57`): no query ever filters,
sorts, or joins on a key; every read is by the full `(tenant)` key.

### Decision 5: Generalize the codec with a generic *function*, not a generic *struct* — zero shipped call sites change

The proposal's open question 3. The constraint is hard: the shipped
Trivy/gitleaks codecs and `repository_overrides_test.go` must pass
**unchanged**.

| Option | Tradeoff | Decision |
|---|---|---|
| Generic free function `resolveRepositoryOverride[T]`, non-generic method wrapper | Go forbids type parameters on methods, so the generic must be a free function taking `*Service` — an accepted, idiomatic shape | **Chosen** |
| `repositoryOverrideCodec[T any]` struct | `map[string]repositoryOverrideCodec[T]` fixes one `T` for the whole map, so a heterogeneous registry is impossible; two maps defeat the point | Rejected |
| `Apply func(raw []byte, settings any) (any, error)` | One map, but every call site type-asserts, a mismatch is a runtime error instead of a compile error, and **`applyTrivyOverride`/`applyGitleaksOverride`'s signatures change**, breaking the tests the proposal requires stay unchanged | Rejected |
| Parallel `signingOverrideCodecs` map | Duplicates the row-presence boundary; two places to get NotFound-means-inherit wrong | Rejected |

Concretely, in `internal/app/regixtry/repository_overrides.go`:

```go
// resolveRepositoryOverride is the row-presence boundary, generic over the
// settings type a feature's override targets. A row replaces the feature's
// global settings in full; NotFound means "use the global row", never an
// error. Type parameters are illegal on methods, so this is a free function
// over *Service, and applyRepositoryOverride stays a non-generic method.
func resolveRepositoryOverride[T any](
    ctx context.Context, s *Service,
    tenant, repository, feature string,
    settings T, apply func([]byte, T) (T, error),
) (T, error) {
    raw, err := s.metadata.GetRepositoryFeatureOverride(ctx, tenant, repository, feature)
    if err != nil {
        if domain.IsCode(err, domain.ErrorCodeNotFound) {
            return settings, nil
        }
        var zero T
        return zero, err
    }
    if apply == nil {
        return settings, nil
    }
    return apply(raw, settings)
}

// UNCHANGED SIGNATURE — all 6 call sites in service_scanning.go untouched.
func (s *Service) applyRepositoryOverride(ctx context.Context, tenant, repository, feature string, settings ports.ScanSettings) (ports.ScanSettings, error) {
    return resolveRepositoryOverride(ctx, s, tenant, repository, feature, settings, repositoryOverrideCodecs[feature].Apply)
}

// New sibling for the signing policy's own target type.
func (s *Service) applySigningRepositoryOverride(ctx context.Context, tenant, repository string, policy ports.SigningPolicySettings) (ports.SigningPolicySettings, error) {
    return resolveRepositoryOverride(ctx, s, tenant, repository, signingFeatureName, policy, applySigningOverridePayload)
}
```

`repositoryOverrideCodecs[feature].Apply` on a missing key yields the zero
codec whose `Apply` is `nil`, and the `apply == nil` guard reproduces today's
`if !ok { return settings, nil }` (`repository_overrides.go:140-143`)
**exactly** — the behavior is preserved, only relocated.

The registry gains a third entry that registers `Normalize` only:

```go
var repositoryOverrideCodecs = map[string]repositoryOverrideCodec{
    trivyFeatureName:    {Normalize: normalizeTrivyOverride, Apply: applyTrivyOverride},
    gitleaksFeatureName: {Normalize: normalizeGitleaksOverride, Apply: applyGitleaksOverride},
    // signing's override targets ports.SigningPolicySettings, not
    // ScanSettings, so it has no ScanSettings-shaped Apply. Its Apply is
    // passed explicitly to resolveRepositoryOverride by the signing gate.
    signingFeatureName: {Normalize: normalizeSigningOverride},
}
```

That one map entry is also what makes `GetRepositoryOverride`,
`ListRepositoryOverrides`, `SetRepositoryOverride`, and
`ClearRepositoryOverride` (`repository_overrides.go:158-225`) accept
`signing` — they consult only the map's *presence* and `Normalize`, never
`Apply` (verified by reading all four). **Zero changes to those four
methods.**

`normalizeSigningOverride` runs every key through
`signing.NormalizePublicKeyPEM` and rejects `enabled:true` with zero keys
(Decision 7's outage rule). `applySigningOverridePayload` uses plain
`json.Unmarshal`, preserving the deliberate strict-in / lenient-out asymmetry
documented at `repository_overrides.go:101-106`.

### Decision 6: `enforceSigningPolicy` — what "fail-closed" means, hop by hop

New `internal/app/regixtry/service_signing.go`, mirroring
`service_scanning.go:88-110`'s shape and inverting its posture. Inserted in
`OpenManifest` (`queries.go:71-91`) immediately after the existing scan gate
at `:86-88`:

```go
    if err := s.enforceScanPolicy(ctx, repository.String(), manifest.Digest.String()); err != nil {
        return domain.Manifest{}, err
    }
    if err := s.enforceSigningPolicy(ctx, repository.String(), manifest.Digest.String()); err != nil {
        return domain.Manifest{}, err
    }
    return manifest, nil
```

```go
const signingFeatureName = "signing"

// enforceSigningPolicy is the pull-time content-trust gate. Unlike
// enforceScanPolicy, which is deliberately fail-OPEN on uncertainty, this
// gate is fail-CLOSED: "cannot verify" and "not trustworthy" are the same
// answer. The two sit side by side with opposite defaults, on purpose.
func (s *Service) enforceSigningPolicy(ctx context.Context, repository, digest string) error {
    policy, err := s.GetSigningPolicySettings(ctx)
    if err != nil {
        return err
    }
    policy, err = s.applySigningRepositoryOverride(ctx, s.tenant(ctx), repository, policy)
    if err != nil {
        return err
    }
    if !policy.Enabled {
        return nil // the ONLY allow-without-verify path in this function
    }
    if _, err := s.verifySignature(ctx, repository, digest, policy); err != nil {
        return err
    }
    return nil
}
```

`OpenManifest` stays the only gated entry point. `ResolveManifest`
(`queries.go:48-69`) is deliberately **not** gated — the browse path, exactly
as the scan gate left it (`service_test.go:467-469`).

**"Closed" traced concretely.** `verifySignature` returns the same
`(state, error)` pair the status endpoint consumes (Decision 9), so the gate
and the report can never disagree:

| Situation | Result | Status code |
|---|---|---|
| Resolved policy disabled | allow | 200 |
| No `sha256-<hex>.sig` tag (`ResolveManifest` → NotFound) | `domain.NewPolicyViolationError` | **403** |
| `.sig` manifest malformed, or no simplesigning layer with a signature annotation | `NewPolicyViolationError` | **403** |
| Payload blob absent from the blob store | `NewPolicyViolationError` | **403** |
| Zero configured keys parse as ECDSA P-256 | `NewPolicyViolationError` | **403** |
| Every (key, entry) pair fails `Verify` | `NewPolicyViolationError` | **403** |
| A signature verifies but `CheckClaims` binds a different digest | `NewPolicyViolationError` | **403** |
| Store/blob **infrastructure** error (not NotFound) | propagated unchanged | 500 |

The last row is the one subtle choice. A 500 also blocks the pull, so
fail-closed holds either way; reporting an infrastructure fault as a *policy
violation* would send the operator hunting for a missing signature that
exists. Only verification outcomes become 403 — matching how
`enforceScanPolicy` propagates non-NotFound store errors unchanged
(`service_scanning.go:100-105`). 403 uses `domain.NewPolicyViolationError`,
the identical `domain.ErrorCodePolicyViolation` path the scan gate already
proves end to end (`service_test.go:399-400, 463-464`) — **no new HTTP error
mapping is introduced**.

The 403 message names the repository, the digest, and a fixed-vocabulary
reason. It never contains PEM bytes, payload bytes, or a base64 signature.

**Multi-arch note (documented limitation, not a bug to fix here):**
`cosign sign` on an image index signs the *index* digest. Pulling a child
manifest by its own digest therefore resolves no `.sig` tag and is blocked.
Operators enabling the gate on multi-arch repositories must sign what their
clients pull.

### Decision 7: No verification cache in this slice

The proposal's open question 2.

| Option | Tradeoff | Decision |
|---|---|---|
| No cache | One extra `ResolveManifest` + one `OpenBlob` + one SHA-256 + one P-256 verify per gated pull | **Chosen** |
| Digest-keyed in-memory cache | Under fail-closed semantics, a stale *positive* entry allows a pull whose trust was revoked — the invalidation bug fails in the unsafe direction. A re-pushed `.sig` tag and a rotated key both need invalidation hooks that do not exist | Rejected |

Both extra reads are the same shape the pull path already performs
(`ResolveManifest` is already called once at `queries.go:81`; `OpenBlob` is a
filesystem open, `fsblob`), and the crypto is a single P-256 verification.
Revisit only on a measured hot-path regression, and only with an explicit
invalidation on `UpsertSigningPolicySettings`, override writes, and any
manifest publish whose tag ends in `.sig`.

Cost is also bounded by policy: the loop is at most
`len(TrustedPublicKeys) × len(entries)`, capped at 16 keys (Decision 8) and
`MaxSignatureEntries = 64`.

### Decision 8: Admin resource, validation, and hot-path bounds

One new `case` in `handleAdmin`'s dispatch, beside `scan-policy`
(`admin_handlers.go:41-42`):

```go
case subpath == "signing-policy":
    r.handleAdminSigningPolicy(w, req)
```

`handleAdminSigningPolicy` is modeled line-for-line on
`handleAdminScanPolicy` (`admin_handlers.go:354-379`): `GET` returns the
current settings including the code-level default; `PUT` fully replaces them.
Authorization is inherited from `handleAdmin`'s `requireAdminPrincipal`
(`admin_handlers.go:23-26`) — no new permission surface. Public keys are not
secret, so the **admin** response echoes the canonical PEM; the
registry-scoped status endpoint (Decision 9) exposes only a count.

`decodeSigningPolicySettings` mirrors `decodeScanPolicySettings`
(`:381-394`) and enforces three rules:

1. Every key goes through `signing.NormalizePublicKeyPEM`. A parse failure or
   a non-ECDSA-P256 key is a `400` naming the **index**, never echoing bytes.
2. **`enabled: true` with zero usable keys is a `400`**, not a stored row.
   This is the outage rule: it makes the guaranteed-total-outage configuration
   unrepresentable rather than merely discouraged.
3. At most 16 keys, bounding the per-pull verification loop.

Hot-path parsing bounds (`internal/domain/signing`, Decision 2):
`MaxSignatureManifestBytes`, `MaxPayloadBytes`, `MaxSignatureEntries`. These
matter because a `.sig` manifest is **pusher-controlled** and is parsed on
every gated pull; without them a malicious pusher makes every pull read a
large blob. Request-body size keeps `decodeAdminJSON`'s inherited posture
(same open question as `repository-scan-config-overrides/design.md:661-664`).

### Decision 9: `signature-status` — five states, computed independently of the toggle

One suffix check inside `handleV2`'s existing `manifests/` branch
(`router.go:153-165`), tested against the **trimmed reference**, not the
suffix, for the exact reason documented at `router.go:155-160`:

```go
    if strings.HasSuffix(reference, "/signature-status") {
        r.handleManifestSignatureStatus(w, req, repository, strings.TrimSuffix(reference, "/signature-status"))
        return
    }
```

`handleManifestSignatureStatus` is a copy of `handleManifestScanStatus`
(`router.go:362-382`): `GET` only, `ports.ActionPull` via `withPrincipal`,
always `200` with a verdict. **It reports a verdict; it is never subject to
one** — it does not call `enforceSigningPolicy`.

```go
// internal/app/regixtry/queries.go, beside ScanStatusResult (:98-128)
type SignatureStatusResult struct {
    Repository     string                 `json:"repository"`
    Reference      string                 `json:"reference"`
    Digest         string                 `json:"digest"`
    State          string                 `json:"state"`
    WouldBlockPull bool                   `json:"would_block_pull"`
    Policy         SignatureStatusPolicy  `json:"policy"`
    Signature      *SignatureStatusDetail `json:"signature,omitempty"`
}

type SignatureStatusPolicy struct {
    Enabled     bool `json:"enabled"`
    TrustedKeys int  `json:"trusted_keys"` // COUNT ONLY — never the PEM
}

type SignatureStatusDetail struct {
    Tag            string `json:"tag"`             // sha256-<hex>.sig
    SignatureCount int    `json:"signature_count"` // layers carrying the annotation
    Reason         string `json:"reason,omitempty"`// fixed vocabulary
}

const (
    SignatureStatusUnsigned     = "unsigned"
    SignatureStatusUnverifiable = "unverifiable"
    SignatureStatusUntrusted    = "untrusted"
    SignatureStatusMismatched   = "mismatched"
    SignatureStatusVerified     = "verified"
)
```

Five states, mapped against `ScanStatus`'s five (`queries.go:122-128`):

| State | Meaning | Who fixes it |
|---|---|---|
| `unsigned` | No `.sig` tag resolves for this digest (≙ `unscanned`) | CI: sign the image |
| `unverifiable` | A `.sig` exists but no verification could run: malformed manifest, no annotated simplesigning layer, missing payload blob, or zero usable trusted keys. `Reason` says which (≙ `failed`) | Registry operator or the push |
| `untrusted` | Verification ran and no signature validated against any trusted key (≙ `blocked`) | CI: signed with the wrong key |
| `mismatched` | A signature verified cryptographically but its payload binds a **different** digest — kept distinct from `untrusted` because it is a transplanted-signature signal, not a wrong-key one | Security investigation |
| `verified` | ≥1 signature verified and its claims bind this digest (≙ `clean`) | — |

There is no `in_progress` analogue: signing has no async producer inside
regixtry. `WouldBlockPull = policy.Enabled && State != verified`.

**The state is always computed, even when the policy is disabled.** That is
deliberate: the proposal's own rollout mitigation is "`signature-status` lets
CI check before rollout", which is worthless if a disabled policy short-
circuits the verdict. The toggle affects `WouldBlockPull` only.

### Decision 10: `signing` is a builtin feature with **no** runtime — the existing conditionals already do the right thing once `projectFeatureRuntime` stops lying

The proposal requires Enable/Disable/Configure and **no**
Install/Upgrade/Rollback. Here is exactly what breaks today and the minimal
guard for each, read from `feature_registry.go`:

| Symptom if `signing` is simply appended to `builtInFeatures` | Cause | Guard |
|---|---|---|
| Signing reports a **managed runtime that is uninstalled** | `projectFeatureRuntime` (`:136-153`): `manager` is nil, so it falls to `GetFeatureRuntimeState` → NotFound → fabricates `{Mode: FeatureRuntimeModeManaged, Status: "uninstalled"}` | Early-return `ports.FeatureRuntime{}` for a feature with no managed runtime |
| **"Install Runtime" is offered** in the admin API and TUI | `buildFeatureActions` (`:433`) appends it whenever `Mode == Managed && status == uninstalled` | **None needed.** All three runtime actions at `:433, :436, :439` are already gated on `Mode == FeatureRuntimeModeManaged`; with an empty `FeatureRuntime{}`, `Mode == ""` and all three vanish. `featureActionForKey` (`model.go:2948-2969`) only fires for actions present in `page.Actions`, so `i`/`u`/`b` become inert with **zero TUI change** |
| `install-runtime` executed anyway returns an untyped 500 | `featureRuntimeManager` (`feature_runtime.go:51-54`) returns a plain `fmt.Errorf` | Reject the three runtime actions in `ExecuteFeatureAction` (`:113-130`) with `domain.NewValidationError` — defense in depth |
| Feature page shows a meaningless Config section (Schedule Enabled / Interval / Timeout / Max Concurrency) and an empty Runtime section | `buildFeaturePage` (`:340-367`) builds both for any `FeatureKindBuiltin` | Gate the Runtime section on `details.Runtime.Mode == FeatureRuntimeModeManaged`; give signing its own Policy fields section |
| Two contradictory "enabled" bits | `loadFeatureSettings` (`:242`) reads `scan_settings(feature="signing")` while the gate reads `signing_policy_settings` — the TUI could read "disabled" while pulls 403 | Make them **one bit** (below) |

Data-driven, not name-driven:

```go
type featureDescriptor struct {
    name string
    kind ports.FeatureKind
    // managedRuntime reports whether this feature has a FeatureRuntimeManager
    // and therefore an install/upgrade/rollback lifecycle. False for signing:
    // verification is in-process stdlib crypto — there is no binary to manage.
    managedRuntime bool
}

var builtInFeatures = []featureDescriptor{
    {name: trivyFeatureName, kind: ports.FeatureKindBuiltin, managedRuntime: true},
    {name: gitleaksFeatureName, kind: ports.FeatureKindBuiltin, managedRuntime: true},
    {name: signingFeatureName, kind: ports.FeatureKindBuiltin, managedRuntime: false},
}
```

**One bit, one source of truth.** `signing` never writes a `scan_settings`
row. `loadFeatureSettings` projects the policy row into the `ScanSettings`
shell the generic path expects (`ScanSettings{Enabled: policy.Enabled}`, with
`configured` = "a `signing_policy_settings` row exists"); `ExecuteFeatureAction`'s
`enable`/`disable` cases route to `UpdateSigningPolicySettings` instead of
`SetFeatureEnabled`; and `ConfigureFeature` rejects `signing` outright with
`domain.NewValidationError("signing is configured through the signing policy
endpoint")` so nothing can create a stray row behind the projection. Rejected
alternative: two independent flags — it produces a UI that says "disabled"
while the gate 403s, which is the worst possible failure mode for a
security control.

### Decision 11: TUI — one badge line, one modal, one cycle entry

> **Superseded by `tui-menu-architecture`** (its third piece, "Signing Is A
> Third Feature Cycle Option In The Override Modal", is REMOVED — not
> amended). Cycling `repositoryOverrideModal`'s `Feature` field with Space to
> reach `signing` is explicitly reversed: the cycle mechanism
> (`repositoryOverrideFeatureCycle`/`nextRepositoryOverrideFeatureName`) is
> deleted entirely, and Signing gains its own dedicated, discoverable
> per-repository override entry point (`overrideEditor`, design.md Decision F
> of the superseding change), reachable without ever selecting Trivy's
> screen or cycling through it. The badge (piece 1) and `signingPolicyModal`
> (piece 2) below are unaffected by this reversal. The original decision
> text is preserved for history, not deleted.

**1. Badge, zero rows.** `renderTrivyTabs` (`admin_views.go:240-249`) is
Trivy-specific and is only rendered when the selected feature is trivy
(`admin_views.go:183-185`), so it is the wrong host. There is no
tab strip on the signing page — it renders through `renderGenericFeaturePage`
(`admin_views.go:197-205`). The zero-row host is the **already-existing
"Feature Page" heading line** at `admin_views.go:179`:

```go
featurePageHeading := theme.subheading.Render("Feature Page")
if view.FeaturePage.Summary.Name == signingFeatureName {
    featurePageHeading += "  " + signingPolicyBadge(theme, view.SigningPolicy)
}
lines = append(lines, "", featurePageHeading)
```

`signingPolicyBadge` sits beside `scanPolicyBadge` (`admin_views.go:251-258`)
and is **text-only, no icon or glyph**, matching
`TestRenderTrivyTabsPolicyBadgeTextReflectsStateAndUsesNoIconOrGlyph`:
`Signing: OFF` (muted) or `Signing: REQUIRED (2 keys)` (selected). Row
arithmetic elsewhere is untouched — the badge is appended to a line that
already exists.

**2. `signingPolicyModal`**, mirroring `scanPolicyModal`'s exact 3-piece
shape (`session.go:156-187`, `model.go:1292-1296`, `admin_views.go:634-651`).

*Piece 1 — `session.go`*, beside `scanPolicyModal`:

```go
type signingPolicyField int

const (
    signingPolicyFieldEnabled signingPolicyField = iota
    signingPolicyFieldAddKey
    signingPolicyFieldClearKeys // action row, not an input
)

type signingPolicyModal struct {
    Open         bool
    Focus        signingPolicyField
    Enabled      bool
    AddKey       string   // one PEM, single line — see below
    Fingerprints []string // read-only SHA-256/12 of each stored key
    Loading      bool
    Error        string
}

func (m signingPolicyModal) Active() bool { return m.Open }
func nextSigningPolicyField(field signingPolicyField) signingPolicyField
```

`AdminViewState` gains `SigningPolicy ports.SigningPolicySettings` and
`SigningPolicyModal signingPolicyModal`, beside `ScanPolicy`/`ScanPolicyModal`
(`session.go:348-349`), zeroed in `clearSelectedAdminDetails` and
`applyFeaturePage` alongside the other modals.

**The multi-line PEM problem, solved once.** A PEM block is multi-line, but
`renderTextField` is single-line and a newline arrives as `KeyEnter`, which
*submits*. Rather than invent an escape syntax, `signing.NormalizePublicKeyPEM`
(Decision 2) accepts **any whitespace arrangement** and reconstructs canonical
PEM. A `cat cosign.pub` paste through the admin API (real newlines in a JSON
string) and a space-separated single-line paste in the modal both normalize to
the same stored bytes. Stored keys render read-only as truncated fingerprints,
never as PEM, so the modal never has to *display* multi-line text either.

*Piece 2 — `model.go`*: one `Active()` branch in the chain (beside
`ScanPolicyModal`), one `updateSigningPolicyModalKey` modeled on
`updateScanPolicyModalKey`, and an opener `case m.isSelectedSigningFeature()
&& isRuneKey(msg, 'p')` mirroring `model.go:1292` — **no key collision**,
because the existing `p` case is guarded by `m.isSelectedTrivyFeature()`.
Two msg types and three commands follow `adminScanPolicyLoadedMsg` /
`loadScanPolicyCmd` / `updateScanPolicyCmd`. `adminFeatureHelp`
(`admin_views.go:729-748`) gains a `signingFeatureName` branch with
`p: policy`.

*Piece 3 — `admin_views.go`*: `renderSigningPolicyModal` beside
`renderScanPolicyModal`, plus one more branch on `renderAdminWorkspace`'s
`compositeOverlay` tail. Row arithmetic, same 2-rows-per-field cost and 4-row
`theme.section` chrome the two prior modals use:

| Modal state | Heading | Status | Fields (2×2) | Key list | Clear row | Blank+help | Inner | +chrome | Total |
|---|---|---|---|---|---|---|---|---|---|
| No error, 0 keys | 1 | 1 | 4 | 1 | 1 | 2 | 10 | 4 | **14** |
| No error, 3 keys | 1 | 1 | 4 | 3 | 1 | 2 | 12 | 4 | **16** |
| Error, ≥4 keys (list capped at 4 + "+N more") | 1 | 1 | 4 | 5 | 1 | 4 | 16 | 4 | **20** |

Worst case 20 rows against the 24-row `minViewportHeight` floor — the same
class as the shipped 19-row `repositoryOverrideModal`, with 4 rows of margin.
The fingerprint list is capped at 4 rows precisely so the worst case stays
bounded regardless of how many keys are configured.

**3. Third cycle value.** `nextRepositoryOverrideFeatureName`
(`model.go:1618-1626`) is today a hardcoded 2-value flip
(`gitleaks → trivy`, else `→ gitleaks`). A third `if` would be the wrong
shape. Replace with an ordered slice, so a fourth feature is one entry:

```go
// repositoryOverrideFeatureCycle is the modal's Feature field cycle order.
// A fourth feature is one more entry — no restructuring.
var repositoryOverrideFeatureCycle = []string{trivyFeatureName, gitleaksFeatureName, signingFeatureName}

func nextRepositoryOverrideFeatureName(feature string) string {
    for index, candidate := range repositoryOverrideFeatureCycle {
        if candidate == feature {
            return repositoryOverrideFeatureCycle[(index+1)%len(repositoryOverrideFeatureCycle)]
        }
    }
    return repositoryOverrideFeatureCycle[0]
}
```

The cycle changes from `trivy → gitleaks → trivy` to
`trivy → gitleaks → signing → trivy`. **This is the one shipped TUI behavior
this change deliberately alters**, and the codec-generalization
"tests-pass-unchanged" constraint does not cover it: any test asserting the
two-value cycle must be updated, and `sdd-tasks` must schedule that
explicitly rather than let it surface as a surprise failure.

`nextRepositoryOverrideField` (`session.go:230-239`) skips
`…PathSecondary` for gitleaks; signing likewise has no second path field, so
the condition generalizes from `feature == gitleaksFeatureName` to
`feature != trivyFeatureName` — one token. For signing, `PathPrimary` is
relabelled "Trusted Key (PEM)". `ports.RepositoryOverrideDetails`
(`ports/regixtry.go:156-164`) gains `TrustedPublicKeys []string
\`json:"trusted_public_keys,omitempty"\``;
`applyRepositoryOverrideToModal` (`model.go:1543-1558`) and the client body
builder (`admin_client.go:396-409`) each gain one `signing` branch beside
their existing `gitleaksFeatureName` branch.

## Data Flow

    CONFIG  TUI 'p' on the Signing feature page
              -> PUT /admin/v1/signing-policy  {enabled, trusted_public_keys[]}
                   decodeSigningPolicySettings
                     -> signing.NormalizePublicKeyPEM (per key, ECDSA P-256 only)
                     -> reject enabled+0 keys (outage rule)
                   -> UpsertSigningPolicySettings(tenant, settings)

    OVERRIDE  repositoryOverrideModal, Feature cycled to "signing"
              -> PUT /admin/v1/features/signing/repository-overrides/<repo>
                   codec.Normalize (normalizeSigningOverride)
                     -> UpsertRepositoryFeatureOverride(tenant, repo, "signing", payload)

    PULL    GET /v2/<repo>/manifests/<ref>
              OpenManifest -> authorize(ActionPull) -> ResolveManifest -> digest
                -> enforceScanPolicy      [unchanged, fail-OPEN]
                -> enforceSigningPolicy   [new,       fail-CLOSED]
                     GetSigningPolicySettings   (missing row => {Enabled:false})
                       -> resolveRepositoryOverride[SigningPolicySettings]
                            --NotFound--> global unchanged
                            --found-----> applySigningOverridePayload (full replace)
                       -> !Enabled -> allow (the only allow-without-verify path)
                       -> signing.SignatureTag(digest) => "sha256-<hex>.sig"
                       -> ResolveManifest(repo, tag)   --NotFound--> 403 unsigned
                       -> signing.ParseSignatureManifest(manifest.Payload)
                            (raw bytes: Descriptor has no Annotations field)
                       -> for each entry:
                            blobs.OpenBlob(entry.PayloadDigest)  -> payload bytes
                            for each trusted key:
                              signing.Verify(key, payload, entry.Signature)
                              signing.CheckClaims(payload, digest)
                       -> any pair verified+claims-bound -> allow
                       -> otherwise                      -> 403 PolicyViolation

    STATUS  GET /v2/<repo>/manifests/<ref>/signature-status   [ActionPull]
              same resolution + verification, never gated
                -> {state, would_block_pull, policy{enabled, trusted_keys:N}}

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/domain/signing/cosign.go` | Create | `SignatureTag`, `ParseSignatureManifest`, `CheckClaims`, media-type/annotation constants, hot-path bounds |
| `internal/domain/signing/keys.go` | Create | `NormalizePublicKeyPEM`, `ParseTrustedKey`, `Verify` (stdlib `crypto/ecdsa`, `crypto/sha256`, `crypto/x509`, `encoding/pem`, `encoding/base64`) |
| `internal/ports/regixtry.go` | Modify | `SigningPolicySettings`, `SigningOverride`, 2 `MetadataStore` methods, `RepositoryOverrideDetails.TrustedPublicKeys` |
| `internal/infra/metadata/sqlite/store.go` | Modify | `signing_policy_settings` DDL in `init()`; `Get`/`UpsertSigningPolicySettings` |
| `internal/app/regixtry/service_signing.go` | Create | Policy resolution, `enforceSigningPolicy`, `verifySignature`, `Get`/`UpdateSigningPolicySettings` |
| `internal/app/regixtry/repository_overrides.go` | Modify | `resolveRepositoryOverride[T]`, `applySigningRepositoryOverride`, `signing` codec entry, normalize/apply |
| `internal/app/regixtry/queries.go` | Modify | `enforceSigningPolicy` call in `OpenManifest`; `SignatureStatus` + DTOs + 5 state constants |
| `internal/app/regixtry/feature_registry.go` | Modify | `signingFeatureName`, `featureDescriptor.managedRuntime`, guards in `projectFeatureRuntime` / `ExecuteFeatureAction` / `ConfigureFeature` / `loadFeatureSettings` / `buildFeaturePage` |
| `internal/protocol/http/router.go` | Modify | `/signature-status` suffix case + `handleManifestSignatureStatus` |
| `internal/protocol/http/admin_handlers.go` | Modify | `signing-policy` case, handler, decoder, response |
| `internal/tui/session.go` | Modify | `signingPolicyField`, `signingPolicyModal`, `Active()`, 2 `AdminViewState` fields, `nextRepositoryOverrideField` condition |
| `internal/tui/model.go` | Modify | `p` opener, `Active()` branch, modal key handler, 2 msgs, 3 cmds, `repositoryOverrideFeatureCycle`, `applyRepositoryOverrideToModal` signing branch |
| `internal/tui/admin_client.go` | Modify | `Get`/`UpdateSigningPolicy`; `SetRepositoryOverride` signing body branch |
| `internal/tui/admin_views.go` | Modify | `signingPolicyBadge`, badge on the Feature Page heading, `renderSigningPolicyModal`, overlay branch, `adminFeatureHelp` |
| `go.mod` / `go.sum` | **Unchanged** | Standard library only |

`internal/app/regixtry/service.go` (`PublishManifest`) is **not** in this
list: the push path is untouched, and the `BlobExists`/`subject` defect
(`explore.md:53-65`) stays explicitly unfixed per the proposal's Out of Scope.

## Interfaces / Contracts

Wire shapes (admin, admin-only auth):

```
GET  /admin/v1/signing-policy
200  {"enabled":true,"trusted_public_keys":["-----BEGIN PUBLIC KEY-----\n…"],"updated_at":"…"}

PUT  /admin/v1/signing-policy
     {"enabled":true,"trusted_public_keys":["-----BEGIN PUBLIC KEY-----\n…"]}
400  when a key is unparseable / not ECDSA P-256 / >16 keys / enabled with 0 keys

PUT  /admin/v1/features/signing/repository-overrides/library/alpine
     {"enabled":true,"trusted_public_keys":["-----BEGIN PUBLIC KEY-----\n…"]}
```

Registry-scoped (ordinary pull credentials, never gated, no key material):

```
GET  /v2/library/alpine/manifests/1.0.0/signature-status
200  {"repository":"library/alpine","reference":"1.0.0","digest":"sha256:…",
      "state":"untrusted","would_block_pull":true,
      "policy":{"enabled":true,"trusted_keys":2},
      "signature":{"tag":"sha256-….sig","signature_count":1,
                   "reason":"no signature verified against a trusted key"}}
```

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | `SignatureTag` maps `sha256:<hex>` → `sha256-<hex>.sig`; rejects a malformed digest | Table-driven |
| Unit | `ParseSignatureManifest` on **real captured cosign bytes** yields the expected `(PayloadDigest, Signature)`; ignores non-simplesigning layers and layers with no annotation; enforces `MaxSignatureEntries` | Golden fixture (Decision 1's capture task) |
| Unit | `NormalizePublicKeyPEM` accepts real-newline and space-separated forms and yields identical canonical bytes; rejects Ed25519, RSA, P-384, garbage | Table-driven |
| Unit | `Verify` accepts a real cosign signature over the real payload bytes; rejects a one-byte-mutated payload, a mutated signature, a wrong key, non-base64 | Golden fixture |
| Unit | **Re-marshalling the payload before hashing breaks verification** — the pinning test for Decision 1a | Deliberate negative test |
| Unit | `CheckClaims` rejects a payload binding a different digest and a wrong `critical.type`; accepts a differing `docker-reference` (documented, intentional) | Table-driven |
| Unit | `Get`/`UpsertSigningPolicySettings` round-trip; absent row → typed `ErrorCodeNotFound`; key list order preserved | `t.TempDir()` store |
| Unit | `GetSigningPolicySettings` on an empty store returns `{Enabled:false}` — the inverted default | Service test |
| Unit | `resolveRepositoryOverride` returns settings unchanged on NotFound and on a nil `apply` | Table-driven, both `T` instantiations |
| Unit | **Existing `repository_overrides_test.go` passes byte-unmodified** after the generalization | Run as-is (proposal success criterion) |
| Unit | `normalizeSigningOverride` rejects unknown fields, bad PEM, enabled-with-zero-keys | Table-driven |
| Unit | `renderSigningPolicyModal` ≤ 20 rows worst case; fingerprint list capped at 4 + "+N more"; never renders PEM | `lipgloss.Height` + substring assertions |
| Unit | `signingPolicyBadge` is text-only, no icon/glyph, and adds 0 rows to the Feature Page heading | Mirrors the shipped badge test |
| Unit | `nextRepositoryOverrideFeatureName` cycles trivy → gitleaks → signing → trivy | Table-driven |
| Integration | Policy **disabled** → `OpenManifest` byte-identical to today for signed and unsigned digests | Service test |
| Integration | Policy enabled + valid signature by a trusted key → pull succeeds | Service test |
| Integration | Each fail-closed case asserted **separately** → `ErrorCodePolicyViolation`: no `.sig` tag; malformed `.sig`; missing payload blob; unparseable key; wrong key; claims mismatch | 6 service tests |
| Integration | A store **infrastructure** error surfaces unchanged (not as a policy violation) and still blocks | Service test with a failing store double |
| Integration | Override requires signing where global does not; override exempts where global does; clearing reverts to global | 3 service tests (both directions) |
| Integration | `ResolveManifest` (browse) is never gated | Service test, mirrors `service_test.go:467-469` |
| Integration | `signature-status` returns each of the 5 states; is reachable with pull-only credentials; is never blocked by the gate; response contains no PEM | Handler tests |
| Integration | Feature registry: `signing` exposes Enable/Disable/Configure and **no** install/upgrade/rollback action; `install-runtime` is a typed validation error | Service + handler test |
| Integration | Enable/Disable via the feature action and via `PUT /admin/v1/signing-policy` move the **same** bit | Service test (one-bit invariant) |
| Integration | **`cosign sign --key` against a running regixtry round-trips with zero push-path changes** — the proposal's first success criterion | End-to-end test, skipped with a recorded reason if no `cosign` binary exists |

## Threat Matrix

Included because this design adds HTTP path dispatch and parses
pusher-controlled bytes on the pull hot path. No subprocess, no argv, no VCS,
no PR automation, no executable-file classification.

| Boundary | Applicability | Design response | Planned RED test |
|---|---|---|---|
| Documentation-like paths | N/A — no file-classification or execution decision from a path name | — | — |
| Git repository / commit / push state | N/A — no VCS invocation | — | — |
| PR commands | N/A — no PR automation | — | — |
| Subprocess argv | **N/A — deliberately eliminated by Decision 3.** Trusted keys are inline PEM, so no operator-supplied string reaches any argv or filesystem path | — | — |
| **HTTP path dispatch (added)** | **Applicable** — new `/v2/…/manifests/<ref>/signature-status` suffix and new `/admin/v1/signing-policy` subpath | Suffix matched against the **trimmed reference**, not the raw suffix (`router.go:155-160`), so a tag literally named `signature-status` still routes to `handleManifest`; the admin subpath is an exact `==` match | A tag named `signature-status` resolves as a manifest; `signature-status` requires only `ActionPull` and is never gated |
| **Untrusted artifact parsing on the hot path (added)** | **Applicable** — the `.sig` manifest and payload blob are pusher-controlled and are parsed on every gated pull | `MaxSignatureManifestBytes`, `MaxPayloadBytes` (`io.LimitReader`), `MaxSignatureEntries`; key list capped at 16, bounding the loop at 16×64; JSON decoded into fixed structs, never `any` | An oversized `.sig` manifest, an oversized payload blob, and a 10 000-layer manifest are all rejected without unbounded work |
| **Signature transplant (added)** | **Applicable** — a valid signature over a *different* image would otherwise pass | `CheckClaims` binds `critical.image.docker-manifest-digest` to the resolved digest, and the `mismatched` state makes it separately observable | A signature valid for digest A, pushed at digest B's `.sig` tag, is rejected as `mismatched` |
| **Fail-closed gate availability (added)** | **Applicable** — a misconfiguration can 403 every pull registry-wide | Default OFF (`DEFAULT 0` + inverted code default); `enabled` with zero keys is unrepresentable (400); per-repository exemption override; `signature-status` for pre-rollout CI checks; TUI modal states the consequence | Enabling with zero keys is a 400; a fresh store with no row allows every pull |
| **Key material disclosure (added)** | **Applicable** — a registry-scoped endpoint reports on trust configuration | `SignatureStatusPolicy` carries a **count**, never PEM; `Reason` is a fixed vocabulary; 403 messages and run errors never echo key, payload, or signature bytes | Every `signature-status` and 403 body is asserted to contain no `BEGIN PUBLIC KEY` and no base64 signature |

## Migration / Rollout

Additive only: one `CREATE TABLE IF NOT EXISTS` appended to `Store.init()`'s
statement slice (`store.go:1002-1190`), whose loop already tolerates re-runs.
No `ALTER TABLE`, no existing-column change, no manifest, blob, or
`repository_feature_overrides` schema change.

Behavioral rollout is inert by construction. With no `signing_policy_settings`
row, `GetSigningPolicySettings` returns `{Enabled:false}` and
`enforceSigningPolicy` returns at its first branch, so `OpenManifest` behaves
byte-identically to today. Enabling is a two-step act the API forces into a
safe order: keys first (or in the same body), never enablement alone.

Rollback: `git revert`. A reverted binary ignores `signing_policy_settings`,
and `feature_name="signing"` override rows resolve to "no override" through
the same unknown-feature fallback the shipped codec map already has. No
`go.mod` change to revert. Operationally, `PUT {"enabled":false}` restores
pre-change pull behavior without a deploy.

## Open Questions

- [ ] **cosign's exact byte layout is documented, not executed (Decision 1).**
      No `cosign` binary, no shell, and no network were available in this
      phase. The annotation key `dev.cosignproject.cosign/signature`, its
      **layer-level** placement, the simplesigning media type, the
      `"cosign container image signature"` critical type, and ECDSA-P256 /
      SHA-256 / ASN.1-DER / base64 encoding are all stated from documented
      upstream behavior. `sdd-tasks` MUST schedule the fixture-capture task
      **before** the parser task and MUST NOT let a hand-constructed fixture
      stand in silently — mirroring how
      `repository-scan-config-overrides/design.md:630-644` recorded its
      unverifiable Trivy claims rather than presenting them as confirmed.
- [ ] **Does adding a third `builtInFeatures` entry break a shipped count
      assertion?** `ListFeatures` (`feature_registry.go:51-62`) iterates the
      slice; any test asserting exactly two features, or asserting that the
      feature name `signing` is rejected as unsupported, needs updating. No
      such test was found by inspection (`"signing"` appears in no `_test.go`
      file today), but this must be confirmed by running the suite, not by
      grep.
- [ ] **Live-render confirmation of `signingPolicyModal`** at 150×24
      (`minViewportWidth`×`minViewportHeight`) for the 20-row worst case,
      following the throwaway-debug-test method used for
      `repositoryOverrideModal` (`repository-scan-config-overrides/design.md:645-651`).
- [ ] **Hot-path cost is reasoned, not measured** (Decision 7). If a gated
      pull's added latency is ever measured as material, the cache decision
      reopens — but only together with an explicit invalidation on policy
      writes, override writes, and `.sig` tag publishes.
- [ ] **Request-body size bound** for `PUT /admin/v1/signing-policy` inherits
      `decodeAdminJSON`'s current unbounded posture. Same open question the
      overrides endpoint left standing; a 16-key cap bounds the *stored* size
      but not the *submitted* one.
