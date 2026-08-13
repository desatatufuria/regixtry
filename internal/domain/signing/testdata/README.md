# Signing testdata — provenance

**Status: SYNTHETIC, UNCONFIRMED AGAINST REAL COSIGN OUTPUT.**

No `cosign` binary was available in the `sdd-apply` environment for this
work unit (`which cosign` exits non-zero, confirmed 2026-08-13). Per
`openspec/changes/image-signing/design.md` Decision 1 and
`openspec/changes/image-signing/tasks.md` task 0.2b, these files were
hand-constructed (via a throwaway, offline Go generator, not run as part of
the shipped codebase or any test) to match the documented SimpleSigning JSON
and cosign `.sig` manifest shapes byte-for-byte, cross-checked against
`github.com/sigstore/cosign/specs/SIGNATURE_SPEC.md`. They were **never**
produced by an actual `cosign sign` invocation.

Real-capture (task 0.2a) was **not run** and stays unchecked/N/A in
`tasks.md`. A future environment with a real `cosign` binary MUST re-capture
these fixtures from an actual `cosign generate-key-pair` + `cosign sign`
round trip before this posture can be upgraded from "synthetic" to
"confirmed".

## Files

| File | Contents |
|---|---|
| `payload.json` | The verbatim SimpleSigning payload bytes — the exact bytes that are SHA-256-hashed and signed. Never re-marshal these bytes; `internal/domain/signing/keys_test.go`'s negative test proves why. |
| `signature-manifest.json` | The `.sig` OCI image manifest bytes: one `application/vnd.dev.cosign.simplesigning.v1+json` layer referencing `payload.json`'s digest, carrying the `dev.cosignproject.cosign/signature` annotation (base64 ASN.1 DER ECDSA signature). |
| `cosign.pub` | The PEM (PKIX/SPKI, `PUBLIC KEY` block) public half of the throwaway ECDSA P-256 key pair used to sign `payload.json`. The private key was discarded after generation and is not checked in. |

## Casing choice: `docker-manifest-digest` (lowercase)

`design.md` Decision 1a's own worked JSON literal uses lowercase
`"docker-manifest-digest"`. The upstream `SIGNATURE_SPEC.md` example the
orchestrator cross-checked capitalizes it as `"Docker-manifest-digest"`.
`design.md` explicitly flags this as unresolved without a real capture. This
fixture uses **lowercase**, matching `design.md`'s own pinned literal. This
is a fixture-provenance choice, not a parser behavior choice: the payload is
always hashed and read verbatim, so the parser never inspects this casing —
only a real captured payload can settle which casing cosign actually emits.

## Whitespace choice: spaces after `:`/`,`, not compact

`payload.json` uses a space after every `:` and `,` (e.g.
`{"critical": {"identity": {...`), not the fully compact form design.md's
literal shows. This is deliberate: Go's `encoding/json` marshals a decoded
`map[string]any` both compactly *and* with keys sorted alphabetically, and
this fixture's field order (`identity`, `image`, `type` inside `critical`;
`critical`, `optional` at top level) already happens to be alphabetical. A
fully compact original would therefore round-trip through
decode-into-map-then-remarshal *byte-identical to itself*, silently
defeating `TestVerify_RemarshallingPayloadBreaksVerification` — the single
most important test in this package. The added spacing keeps the field
order and content exactly as design.md Decision 1a documents while ensuring
a naive remarshal actually produces different bytes, which is what makes
that pinning test meaningful. This is a fixture-construction choice to make
the negative test exercise something real, not a claim about what a real
captured cosign payload's whitespace looks like.

## Reproducing

The generator that produced these bytes is not part of this repository (it
was a throwaway program run once, offline). It: generates an ECDSA P-256 key
pair (`crypto/ecdsa`, `crypto/elliptic`), builds the SimpleSigning JSON
literal above (spaced, per the note above) with a deterministic fake image
digest (`sha256("image-signing synthetic fixture image manifest v1")`),
signs `SHA-256(payload.json)` with `ecdsa.SignASN1`, base64-encodes the DER
signature into the manifest's annotation, and writes all three files.
