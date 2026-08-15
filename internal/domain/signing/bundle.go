package signing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// SigstoreBundleMediaType is the artifactType/layer mediaType modern
	// cosign (v3+, --key-based signing) assigns to a Sigstore Bundle
	// referrer manifest and its single layer.
	SigstoreBundleMediaType = "application/vnd.dev.sigstore.bundle.v0.3+json"

	// MaxBundleManifestBytes bounds how large a pusher-controlled OCI Image
	// Index (BundleIndexTag) or referrer manifest may be before parsing is
	// rejected outright. Both are parsed on the pull-time verification hot
	// path, so this mirrors MaxSignatureManifestBytes's reasoning and bound.
	MaxBundleManifestBytes = 256 << 10

	// MaxBundleIndexEntries bounds the number of manifests a bundle index
	// may declare, capping the per-pull discovery loop. Mirrors
	// MaxSignatureEntries's reasoning and bound.
	MaxBundleIndexEntries = 64

	// MaxBundleDocumentBytes bounds how large a pusher-controlled bundle
	// document (the referrer manifest's layer blob) may be. Enforced by the
	// caller when reading the blob (this package performs no I/O). Defined
	// as MaxPayloadBytes, not merely equal to it: openSignaturePayload
	// (service_signing.go) is one shared helper enforcing that same bound
	// for both the legacy SimpleSigning payload blob and this bundle
	// document blob, so the two constants must never be able to drift
	// apart (adversarial review finding on
	// feat/signing-sigstore-bundle-support).
	MaxBundleDocumentBytes = MaxPayloadBytes

	// dsseVersion is the DSSE Pre-Authentication Encoding version string
	// (https://github.com/secure-systems-lab/dsse/blob/master/protocol.md).
	dsseVersion = "DSSEv1"
)

// BundleIndexEntry is one candidate referrer entry read out of a bundle
// index (BundleIndexTag's manifest): the digest of the referrer manifest to
// resolve next, and the descriptor fields ParseBundleIndex filtered on.
type BundleIndexEntry struct {
	Digest       string
	MediaType    string
	Size         int64
	ArtifactType string
}

// BundleDSSE is the minimal DSSE envelope shape read out of a Sigstore
// Bundle document: the raw (still base64) payload and payloadType, and every
// candidate signature's raw (still base64) signature bytes.
type BundleDSSE struct {
	PayloadType string
	Payload     string
	Signatures  []string
}

// bundleIndexEnvelope is the minimal OCI Image Index shape needed to
// discover bundle referrer entries.
type bundleIndexEnvelope struct {
	SchemaVersion int                        `json:"schemaVersion"`
	MediaType     string                     `json:"mediaType"`
	Manifests     []bundleIndexManifestEntry `json:"manifests"`
}

type bundleIndexManifestEntry struct {
	MediaType    string `json:"mediaType"`
	Digest       string `json:"digest"`
	Size         int64  `json:"size"`
	ArtifactType string `json:"artifactType"`
}

// BundleIndexTag maps a manifest digest ("sha256:<hex>") to the modern
// bundle-index tag ("sha256-<hex>", no ".sig" suffix) in the same
// repository. Mirrors SignatureTag exactly, minus the legacy suffix.
func BundleIndexTag(digest string) (string, error) {
	encoded, err := validatedDigestHex(digest)
	if err != nil {
		return "", err
	}

	return "sha256-" + encoded, nil
}

// ParseBundleIndex reads the verbatim bundle-index manifest bytes and
// returns every referrer entry whose artifactType is SigstoreBundleMediaType,
// in order. Entries with any other artifactType are silently filtered out --
// they are not this feature's concern.
func ParseBundleIndex(raw []byte) ([]BundleIndexEntry, error) {
	if len(raw) > MaxBundleManifestBytes {
		return nil, fmt.Errorf("signing: bundle index exceeds %d bytes", MaxBundleManifestBytes)
	}

	var envelope bundleIndexEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("signing: bundle index is not valid JSON: %w", err)
	}

	if len(envelope.Manifests) > MaxBundleIndexEntries {
		return nil, fmt.Errorf("signing: bundle index declares more than %d manifests", MaxBundleIndexEntries)
	}

	entries := make([]BundleIndexEntry, 0, len(envelope.Manifests))
	for _, manifest := range envelope.Manifests {
		if manifest.ArtifactType != SigstoreBundleMediaType {
			continue
		}

		entries = append(entries, BundleIndexEntry{
			Digest:       manifest.Digest,
			MediaType:    manifest.MediaType,
			Size:         manifest.Size,
			ArtifactType: manifest.ArtifactType,
		})
	}

	return entries, nil
}

// bundleReferrerManifestEnvelope is the minimal OCI image manifest shape
// needed to read a bundle referrer manifest's subject binding and bundle
// layer.
type bundleReferrerManifestEnvelope struct {
	SchemaVersion int                    `json:"schemaVersion"`
	MediaType     string                 `json:"mediaType"`
	ArtifactType  string                 `json:"artifactType"`
	Layers        []bundleReferrerLayer  `json:"layers"`
	Subject       *bundleReferrerSubject `json:"subject"`
}

type bundleReferrerLayer struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

type bundleReferrerSubject struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

// ParseBundleReferrerManifest reads the verbatim referrer manifest bytes and
// returns its subject digest and its single bundle layer's digest. An entry
// that has no subject, or whose layers carry no SigstoreBundleMediaType
// layer, is not a usable bundle referrer -- it is silently skipped (both
// return values empty, nil error) rather than hard-erroring, mirroring
// ParseSignatureManifest's permissive skip-unknown-shapes posture. Malformed
// JSON or an oversized manifest is still a hard error.
func ParseBundleReferrerManifest(raw []byte) (subjectDigest string, layerDigest string, err error) {
	if len(raw) > MaxBundleManifestBytes {
		return "", "", fmt.Errorf("signing: bundle referrer manifest exceeds %d bytes", MaxBundleManifestBytes)
	}

	var envelope bundleReferrerManifestEnvelope
	if unmarshalErr := json.Unmarshal(raw, &envelope); unmarshalErr != nil {
		return "", "", fmt.Errorf("signing: bundle referrer manifest is not valid JSON: %w", unmarshalErr)
	}

	if envelope.Subject == nil || envelope.Subject.Digest == "" {
		return "", "", nil
	}

	for _, layer := range envelope.Layers {
		if layer.MediaType == SigstoreBundleMediaType && layer.Digest != "" {
			return envelope.Subject.Digest, layer.Digest, nil
		}
	}

	return "", "", nil
}

// bundleDocumentEnvelope is the minimal Sigstore Bundle document shape
// needed to read its DSSE envelope. verificationMaterial (tlogEntries,
// timestampVerificationData) is deliberately never modeled here -- this
// package never reads or validates it (package doc, cosign.go).
type bundleDocumentEnvelope struct {
	MediaType    string             `json:"mediaType"`
	DSSEEnvelope bundleDSSEEnvelope `json:"dsseEnvelope"`
}

type bundleDSSEEnvelope struct {
	Payload     string                `json:"payload"`
	PayloadType string                `json:"payloadType"`
	Signatures  []bundleDSSESignature `json:"signatures"`
}

type bundleDSSESignature struct {
	Sig string `json:"sig"`
}

// ParseBundleDocument reads the verbatim bundle document bytes (the referrer
// manifest's bundle-layer blob) and returns its DSSE envelope. A document
// whose top-level mediaType is present but is not SigstoreBundleMediaType,
// or that carries no usable dsseEnvelope payload, is rejected as an error:
// unlike ParseBundleIndex/ParseBundleReferrerManifest (which filter a list
// of candidates), this function is called on one already-selected blob, so
// a shape mismatch here is a hard error rather than something to skip.
func ParseBundleDocument(raw []byte) (BundleDSSE, error) {
	if len(raw) > MaxBundleDocumentBytes {
		return BundleDSSE{}, fmt.Errorf("signing: bundle document exceeds %d bytes", MaxBundleDocumentBytes)
	}

	var envelope bundleDocumentEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return BundleDSSE{}, fmt.Errorf("signing: bundle document is not valid JSON: %w", err)
	}

	if envelope.MediaType != "" && envelope.MediaType != SigstoreBundleMediaType {
		return BundleDSSE{}, fmt.Errorf("signing: bundle document mediaType %q is not %q", envelope.MediaType, SigstoreBundleMediaType)
	}

	if envelope.DSSEEnvelope.Payload == "" || envelope.DSSEEnvelope.PayloadType == "" {
		return BundleDSSE{}, errors.New("signing: bundle document has no usable dsseEnvelope payload")
	}

	signatures := make([]string, 0, len(envelope.DSSEEnvelope.Signatures))
	for _, signature := range envelope.DSSEEnvelope.Signatures {
		if signature.Sig != "" {
			signatures = append(signatures, signature.Sig)
		}
	}

	return BundleDSSE{
		PayloadType: envelope.DSSEEnvelope.PayloadType,
		Payload:     envelope.DSSEEnvelope.Payload,
		Signatures:  signatures,
	}, nil
}

// PAE computes the DSSE Pre-Authentication Encoding of a (payloadType,
// payload) pair, exactly as defined by the DSSE spec:
//
//	PAE(type, body) = "DSSEv1" + SP + LEN(type) + SP + type + SP + LEN(body) + SP + body
//
// where SP is a single ASCII space, LEN(s) is the ASCII decimal string of
// len(s) in bytes, payloadType is used verbatim as raw bytes (it is already
// plain text, never base64), and payload is the caller-supplied raw bytes
// (the caller is responsible for base64-decoding the DSSE envelope's
// `payload` field before calling this function). This is a pure, I/O-free
// function: it performs no hashing or signing itself. See
// https://github.com/secure-systems-lab/dsse/blob/master/protocol.md.
func PAE(payloadType string, payload []byte) []byte {
	var buf bytes.Buffer
	buf.Grow(len(dsseVersion) + 1 + 20 + 1 + len(payloadType) + 1 + 20 + 1 + len(payload))

	buf.WriteString(dsseVersion)
	buf.WriteByte(' ')
	buf.WriteString(strconv.Itoa(len(payloadType)))
	buf.WriteByte(' ')
	buf.WriteString(payloadType)
	buf.WriteByte(' ')
	buf.WriteString(strconv.Itoa(len(payload)))
	buf.WriteByte(' ')
	buf.Write(payload)

	return buf.Bytes()
}

// inTotoStatement is the minimal in-toto Statement shape CheckBundleClaims
// reads the subject digest from.
type inTotoStatement struct {
	Subject []struct {
		Digest struct {
			SHA256 string `json:"sha256"`
		} `json:"digest"`
	} `json:"subject"`
}

// CheckBundleClaims asserts the signed in-toto statement payload actually
// binds the digest being pulled -- the bundle-format equivalent of
// CheckClaims. It deliberately does not enforce anything beyond digest
// binding (predicateType, annotations, or any other in-toto field): the
// digest already binds the content, so a signature copied between
// repositories still attests byte-identical bytes (mirrors CheckClaims'
// posture, cosign.go).
func CheckBundleClaims(payload []byte, digest string) error {
	var parsed inTotoStatement
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return fmt.Errorf("signing: bundle payload is not valid JSON: %w", err)
	}

	if len(parsed.Subject) == 0 || parsed.Subject[0].Digest.SHA256 == "" {
		return errors.New("signing: bundle payload has no subject digest")
	}

	want := strings.TrimPrefix(digest, "sha256:")
	if parsed.Subject[0].Digest.SHA256 != want {
		return fmt.Errorf("signing: bundle payload does not bind digest %q", digest)
	}

	return nil
}
