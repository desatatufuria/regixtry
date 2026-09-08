# Design: Keyless (Fulcio/OIDC) Signature Verification

## Technical Approach

`internal/domain/signing/keyless.go` is the sole importer of `sigstore-go`. It takes regixtry-owned inputs (raw bundle bytes, `[]TrustedIdentity`, digest) and returns a regixtry-owned `(matchedIdentity string, error)`. `verifyBundleSignature` gains an identity branch after the key loop fails; every other layer mirrors `TrustedPublicKeys` exactly.

## Library Verification (done against source, not docs)

Verified against `github.com/sigstore/sigstore-go@v0.7.1` in the local module cache. **The proposal's "pinned root, no TUF" non-goal holds.**

| Need | Verified API |
|------|--------------|
| Pinned root, no TUF | `root.NewTrustedRootFromJSON(rootJSON []byte) (*root.TrustedRoot, error)` — pure `protojson` unmarshal, zero TUF/network (`pkg/root/trusted_root.go:348`) |
| Bundle from bytes | `var b bundle.Bundle; b.UnmarshalJSON(raw)` — no file path required |
| Verifier | `verify.NewSignedEntityVerifier(trustedMaterial, verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1), verify.WithSignedCertificateTimestamps(1))` |
| Identity anchor | `verify.NewShortCertificateIdentity(issuer, "", "", sanRegexp)` + `verify.WithCertificateIdentity(ci)`; repeating the option is **OR** (`signed_entity.go:384-395`) |
| Claim binding | `verify.WithArtifactDigest("sha256", digestBytes)` |
| Call | `sev.Verify(&b, verify.NewPolicy(artifactPolicy, identityPolicies...)) (*VerificationResult, error)` |
| Matched identity | `result.VerifiedIdentity *verify.CertificateIdentity`; SAN/issuer also in `result.Signature.Certificate` (`certificate.Summary`) |

`pkg/verify` contains **no `net/http` import** — offline verification is structural, not configured. TUF (`pkg/tuf`) is reachable from `pkg/root` and so links into the binary, but is never called.

## Architecture Decisions

| Decision | Choice | Rejected | Rationale |
|---|---|---|---|
| Bundle handoff | Pass **raw bundle bytes** into `keyless.go`; it calls `bundle.UnmarshalJSON` internally | Extracting cert DER + tlog entries into regixtry types and rebuilding a `SignedEntity` | Rebuilding a protobuf `SignedEntity` by hand is the same security-critical work the dependency exists to avoid; raw bytes still leak no Sigstore type |
| Parser extension | New `signing.ParseBundleVerificationMaterial(raw) (BundleVerificationMaterial, error)` alongside untouched `ParseBundleDocument` | Adding fields to `BundleDSSE` | Zero-risk: existing `bundle_test.go` struct comparisons and the static-key path are byte-for-byte unchanged |
| Root storage | `//go:embed assets/trusted_root.json` inside `keyless.go` | Separate `assets.go`; runtime file path | Mirrors `internal/infra/install/compose/assets.go`; keeps the pinned root beside its only consumer, no operator override (settled) |
| Parse cost | `sync.OnceValues` for root + verifier at package level | Per-pull construction | `protojson` root parse is milliseconds; the pull gate is hot-path. Identities are policy-scoped so they live in the `Policy`, not the verifier |
| Result shape | `verifySignature` returns `signatureMatch{KeyFingerprint, Identity string}` | A fourth return value | Settled decision 8: identity is its own operator value; a 4-tuple is unreadable |

## Data Flow

    OpenManifest → enforceSigningPolicy → verifySignature
                                              │  .sig miss
                                              ▼
                                    verifyBundleSignature
                        ┌───────────────┴───────────────┐
                   key loop (stdlib)            identity branch
                   signing.Verify               signing.VerifyKeyless(raw, ids, digest)
                        │                              │ (keyless.go only)
                        └──────→ signatureMatch ←──────┘  embedded root → sigstore verify

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/domain/signing/keyless.go` | Create | `VerifyKeyless`; `//go:embed`; only `sigstore-go` importer |
| `internal/domain/signing/assets/trusted_root.json` | Create | Pinned Sigstore public-good root |
| `internal/domain/signing/bundle.go` | Modify | Add `ParseBundleVerificationMaterial` + envelope structs (additive) |
| `internal/app/regixtry/service_signing.go` | Modify | Identity branch; anchor precondition becomes keys-OR-identities; `signatureMatch` |
| `internal/app/regixtry/queries.go` | Modify | `SignatureStatusDetail.VerifiedIdentity`; `SignatureStatusPolicy.TrustedIdentities` count |
| `internal/app/regixtry/repository_overrides.go` | Modify | Normalize/apply identities (regex compiled at write time, cap, enabled-requires-anchor) |
| `internal/ports/regixtry.go` | Modify | `TrustedIdentity`; field on both settings types + `RepositoryOverrideDetails` |
| `internal/infra/metadata/sqlite/store.go` | Modify | `ALTER TABLE signing_policy_settings ADD COLUMN trusted_identities TEXT NOT NULL DEFAULT '[]';` + scan/upsert |
| `internal/protocol/http/admin_handlers.go` | Modify | Decode/validate/serialize `trusted_identities` |
| `internal/tui/trusted_identity_list.go` | Create | Widget mirroring `trusted_key_list.go`, wired into both modals |
| `go.mod` / `go.sum` | Modify | `sigstore-go v0.7.1` pinned |

## Interfaces / Contracts

```go
type TrustedIdentity struct {
    CertificateIdentityRegexp string `json:"certificate_identity_regexp"`
    CertificateOIDCIssuer     string `json:"certificate_oidc_issuer"`
}

// keyless.go — no sigstore-go type crosses this boundary.
func VerifyKeyless(bundleRaw []byte, identities []TrustedIdentity, digest string) (matched string, err error)
```

Errors are typed and distinct: untrusted chain, expired cert, tampered SET, no matching identity.

## Testing Strategy

| Layer | What | Approach |
|---|---|---|
| Unit | `ParseBundleVerificationMaterial`; existing `bundle_test.go` unmodified | Table-driven, RED first |
| Unit | `VerifyKeyless` fail-closed cases | Synthetic Fulcio-shaped chain via `sigstore-go/pkg/testing/ca` + its own test root |
| Unit | Identity normalize/validate; sqlite round-trip; TUI snapshots | Existing patterns |
| Integration | Admin decode/serialize; override full-row-replace | `httptest` |
| E2E | Real keyless `cosign` image: match allows, wrong issuer blocks | `docker-push-pull-smoke.sh` |

## Threat Matrix

N/A — no routing, shell, subprocess, VCS/PR automation, executable-file classification, or process-integration boundary. Untrusted pusher-controlled bytes are already bounded by `MaxBundleDocumentBytes`, and every new parse path is fail-closed.

## Migration / Rollout

Additive column with `'[]'` default; older binaries ignore it. No data migration.

## Open Questions

- [ ] Keyless bundles carrying `messageSignature` instead of `dsseEnvelope` are rejected by today's `ParseBundleDocument` before the identity branch is reached. v1 treats them as `unverifiable`; confirm against a real `cosign sign` keyless artifact whether this is the common shape.
- [ ] `WithSignedCertificateTimestamps(1)` requires CT-log keys in the pinned root and an embedded SCT. Keep enabled (defense in depth) unless a real artifact fails.
