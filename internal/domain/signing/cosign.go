// Package signing holds every stdlib crypto and cosign-format primitive
// needed to verify cosign static-key image signatures. It is deliberately
// I/O-free: every function is a pure projection over bytes the caller
// already has in hand (a manifest's verbatim stored payload, a blob's
// verbatim stored bytes, a PEM string). All registry reads (resolving the
// `.sig` tag, opening the payload blob) stay in the app layer.
//
// See openspec/changes/image-signing/design.md Decision 1 and Decision 2.
package signing

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// SimpleSigningMediaType is the media type cosign assigns to the layer
	// carrying a SimpleSigning payload's signature.
	SimpleSigningMediaType = "application/vnd.dev.cosign.simplesigning.v1+json"

	// SignatureAnnotationKey is the layer-level (not manifest-level)
	// annotation cosign attaches the base64 ASN.1 DER signature under.
	SignatureAnnotationKey = "dev.cosignproject.cosign/signature"

	// SimpleSigningType is the required literal value of a SimpleSigning
	// payload's critical.type field.
	SimpleSigningType = "cosign container image signature"

	// MaxSignatureManifestBytes bounds how large a pusher-controlled `.sig`
	// manifest may be before parsing is rejected outright. This manifest is
	// parsed on every gated pull, so it is a hot-path bound.
	MaxSignatureManifestBytes = 256 << 10

	// MaxPayloadBytes bounds how large a pusher-controlled SimpleSigning
	// payload blob may be. Enforced by the caller when reading the blob
	// (this package performs no I/O), reusing the same bound here so the
	// hot-path budget is defined in one place.
	MaxPayloadBytes = 1 << 20

	// MaxSignatureEntries bounds the number of layers a `.sig` manifest may
	// declare, capping the per-pull verification loop.
	MaxSignatureEntries = 64

	sha256HexLength = 64
)

// SignatureEntry is one candidate signature read out of a `.sig` manifest:
// the digest of the payload blob to fetch, and the base64 ASN.1 DER
// signature annotated on that layer.
type SignatureEntry struct {
	PayloadDigest string
	Signature     string
}

// signatureManifestEnvelope is the minimal OCI image manifest shape needed
// to read cosign's `.sig` layers. domain.Descriptor
// (internal/domain/regixtry/descriptor.go) has no Annotations field, so
// this package re-parses the manifest's verbatim stored bytes directly
// rather than reusing domain.Manifest.Layers.
type signatureManifestEnvelope struct {
	SchemaVersion int                      `json:"schemaVersion"`
	MediaType     string                   `json:"mediaType"`
	Layers        []signatureManifestLayer `json:"layers"`
}

type signatureManifestLayer struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations"`
}

// SignatureTag maps a manifest digest ("sha256:<hex>") to cosign's legacy
// signature tag ("sha256-<hex>.sig") in the same repository.
func SignatureTag(digest string) (string, error) {
	const prefix = "sha256:"

	if !strings.HasPrefix(digest, prefix) {
		return "", fmt.Errorf("signing: digest %q must have the %q prefix", digest, prefix)
	}

	encoded := strings.TrimPrefix(digest, prefix)
	if len(encoded) != sha256HexLength {
		return "", fmt.Errorf("signing: digest %q does not have a 64-character sha256 hex value", digest)
	}

	if _, err := hex.DecodeString(encoded); err != nil {
		return "", fmt.Errorf("signing: digest %q is not valid hex", digest)
	}

	return "sha256-" + encoded + ".sig", nil
}

// ParseSignatureManifest reads the verbatim `.sig` manifest bytes and
// returns every simplesigning layer carrying a signature annotation, in
// order. Layers with a different media type, or with no signature
// annotation, are silently skipped — they are not this feature's concern
// (e.g. keyless-only annotations per design.md Decision 1b).
func ParseSignatureManifest(raw []byte) ([]SignatureEntry, error) {
	if len(raw) > MaxSignatureManifestBytes {
		return nil, fmt.Errorf("signing: signature manifest exceeds %d bytes", MaxSignatureManifestBytes)
	}

	var envelope signatureManifestEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("signing: signature manifest is not valid JSON: %w", err)
	}

	if len(envelope.Layers) > MaxSignatureEntries {
		return nil, fmt.Errorf("signing: signature manifest declares more than %d layers", MaxSignatureEntries)
	}

	entries := make([]SignatureEntry, 0, len(envelope.Layers))
	for _, layer := range envelope.Layers {
		if layer.MediaType != SimpleSigningMediaType {
			continue
		}

		signature, ok := layer.Annotations[SignatureAnnotationKey]
		if !ok || signature == "" {
			continue
		}

		entries = append(entries, SignatureEntry{
			PayloadDigest: layer.Digest,
			Signature:     signature,
		})
	}

	return entries, nil
}

// CheckClaims asserts the signed payload actually binds the digest being
// pulled. It deliberately does not enforce critical.identity.docker-reference
// (design.md Decision 2): the digest already binds the content, so a
// signature copied between repositories still attests byte-identical bytes.
func CheckClaims(payload []byte, digest string) error {
	var parsed simpleSigningPayload
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return fmt.Errorf("signing: payload is not valid JSON: %w", err)
	}

	if parsed.Critical.Type != SimpleSigningType {
		return fmt.Errorf("signing: payload critical.type is not %q", SimpleSigningType)
	}

	if parsed.Critical.Image.DockerManifestDigest != digest {
		return fmt.Errorf("signing: payload does not bind digest %q", digest)
	}

	return nil
}

// simpleSigningPayload is the minimal SimpleSigning JSON shape this package
// reads claims from. It is used only for claim extraction — it is never
// re-marshalled and hashed; verification always hashes the caller-supplied
// payload bytes verbatim (design.md Decision 1a's load-bearing consequence).
type simpleSigningPayload struct {
	Critical struct {
		Identity struct {
			DockerReference string `json:"docker-reference"`
		} `json:"identity"`
		Image struct {
			DockerManifestDigest string `json:"docker-manifest-digest"`
		} `json:"image"`
		Type string `json:"type"`
	} `json:"critical"`
	Optional json.RawMessage `json:"optional"`
}
