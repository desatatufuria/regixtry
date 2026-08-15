package signing

import (
	"encoding/base64"
	"strings"
	"testing"
)

// realBundleImageDigest, realBundleReferrerDigest, and realBundleLayerDigest
// are the exact digests captured live from team/az-deploy-demo (see
// testdata/README.md's "Sigstore bundle fixtures" section) -- NOT synthetic.
const (
	realBundleImageDigest    = "sha256:4a668fd22601acf91adb14ae57c2fe45a61010c709b3c48ff12b0b9187147640"
	realBundleReferrerDigest = "sha256:8678618790027dc3da5c3716d29379d7a2fb4f37b227985d5520bf183508efec"
	realBundleLayerDigest    = "sha256:c887d7a10ad092f21778dca18ac355a346028c8e0e65cb8e31199104e0f0521d"
)

// TestPAE_GoldenRealSignatureVerifies is the single most important test in
// this package (testdata/README.md). It decodes the real captured DSSE
// envelope from bundle-document.json, computes its PAE bytes with this
// package's PAE function, and asserts the real captured ASN.1 DER signature
// verifies against the real trusted public key that actually produced it --
// all real production data, none of it synthetic. This is the golden test
// required before any of the rest of this change is trusted: if PAE or
// Verify is wrong, this is the test that catches it.
func TestPAE_GoldenRealSignatureVerifies(t *testing.T) {
	t.Parallel()

	raw := readTestdataFixture(t, "bundle-document.json")
	bundle, err := ParseBundleDocument(raw)
	if err != nil {
		t.Fatalf("ParseBundleDocument() error = %v", err)
	}

	if bundle.PayloadType != "application/vnd.in-toto+json" {
		t.Fatalf("bundle.PayloadType = %q, want application/vnd.in-toto+json", bundle.PayloadType)
	}
	if len(bundle.Signatures) != 1 {
		t.Fatalf("len(bundle.Signatures) = %d, want 1", len(bundle.Signatures))
	}

	payload, err := base64.StdEncoding.DecodeString(bundle.Payload)
	if err != nil {
		t.Fatalf("base64-decoding bundle.Payload: %v", err)
	}

	// Independently re-verify (as the task description instructs) that
	// decoding the real captured payload yields the exact in-toto Statement
	// JSON documented in the work-unit description, before trusting it.
	const wantStatement = `{"_type":"https://in-toto.io/Statement/v1","subject":[{"digest":{"sha256":"4a668fd22601acf91adb14ae57c2fe45a61010c709b3c48ff12b0b9187147640"},"annotations":{}}],"predicateType":"https://sigstore.dev/cosign/sign/v1","predicate":{}}`
	if string(payload) != wantStatement {
		t.Fatalf("decoded payload = %s, want %s", payload, wantStatement)
	}

	paeBytes := PAE(bundle.PayloadType, payload)

	keyPEM := readTestdataFixture(t, "bundle-trusted-key.pub")
	key, err := ParseTrustedKey(string(keyPEM))
	if err != nil {
		t.Fatalf("ParseTrustedKey() error = %v", err)
	}

	if err := Verify(key, paeBytes, bundle.Signatures[0]); err != nil {
		t.Fatalf("Verify(realBundleTrustedKey, PAE(...), realSignature) error = %v, want nil (the real captured signature must verify against the real captured key)", err)
	}

	if err := CheckBundleClaims(payload, realBundleImageDigest); err != nil {
		t.Fatalf("CheckBundleClaims() error = %v, want nil (the real payload binds the real image digest)", err)
	}
}

// TestPAE_HandComputedExample is PAE()'s own dedicated unit test, computed
// by hand from the DSSE spec's formula
// (https://github.com/secure-systems-lab/dsse/blob/master/protocol.md),
// independent of the golden fixture above.
//
// PAE("application/vnd.in-toto+json", "test") =
//
//	"DSSEv1" + " " + "28" + " " + "application/vnd.in-toto+json" + " " + "4" + " " + "test"
//
// len("application/vnd.in-toto+json") = 28, len("test") = 4.
func TestPAE_HandComputedExample(t *testing.T) {
	t.Parallel()

	got := PAE("application/vnd.in-toto+json", []byte("test"))
	want := "DSSEv1 28 application/vnd.in-toto+json 4 test"

	if string(got) != want {
		t.Fatalf("PAE() = %q, want %q", got, want)
	}
}

func TestPAE_EmptyTypeAndPayload(t *testing.T) {
	t.Parallel()

	got := PAE("", nil)
	want := "DSSEv1 0  0 "

	if string(got) != want {
		t.Fatalf("PAE(\"\", nil) = %q, want %q", got, want)
	}
}

func TestBundleIndexTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		digest  string
		want    string
		wantErr bool
	}{
		{
			name:   "valid sha256 digest maps to the bundle index tag, no .sig suffix",
			digest: "sha256:d1f1ed21dad86b499c874cef225b6e61c4cf1a8684363810e99d723a306a75b4",
			want:   "sha256-d1f1ed21dad86b499c874cef225b6e61c4cf1a8684363810e99d723a306a75b4",
		},
		{
			name:    "missing sha256 prefix is rejected",
			digest:  "d1f1ed21dad86b499c874cef225b6e61c4cf1a8684363810e99d723a306a75b4",
			wantErr: true,
		},
		{
			name:    "empty hex is rejected",
			digest:  "sha256:",
			wantErr: true,
		},
		{
			name:    "non-hex characters are rejected",
			digest:  "sha256:zzzzed21dad86b499c874cef225b6e61c4cf1a8684363810e99d723a306a75zz",
			wantErr: true,
		},
		{
			name:    "short hex is rejected",
			digest:  "sha256:d1f1ed21",
			wantErr: true,
		},
		{
			name:    "empty digest is rejected",
			digest:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := BundleIndexTag(tt.digest)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("BundleIndexTag(%q) error = nil, want error", tt.digest)
				}
				return
			}
			if err != nil {
				t.Fatalf("BundleIndexTag(%q) unexpected error = %v", tt.digest, err)
			}
			if got != tt.want {
				t.Fatalf("BundleIndexTag(%q) = %q, want %q", tt.digest, got, tt.want)
			}
			if strings.HasSuffix(got, ".sig") {
				t.Fatalf("BundleIndexTag(%q) = %q, must NOT carry the legacy .sig suffix", tt.digest, got)
			}
		})
	}
}

func TestParseBundleIndex_RealFixture(t *testing.T) {
	t.Parallel()

	raw := readTestdataFixture(t, "bundle-index.json")

	entries, err := ParseBundleIndex(raw)
	if err != nil {
		t.Fatalf("ParseBundleIndex() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Digest != realBundleReferrerDigest {
		t.Fatalf("entries[0].Digest = %q, want %q", entries[0].Digest, realBundleReferrerDigest)
	}
	if entries[0].ArtifactType != SigstoreBundleMediaType {
		t.Fatalf("entries[0].ArtifactType = %q, want %q", entries[0].ArtifactType, SigstoreBundleMediaType)
	}
}

func TestParseBundleIndex_FiltersNonMatchingArtifactType(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.oci.image.index.v1+json",
		"manifests": [
			{
				"mediaType": "application/vnd.oci.image.manifest.v1+json",
				"size": 100,
				"digest": "sha256:aa00000000000000000000000000000000000000000000000000000000000000",
				"artifactType": "application/vnd.example.other+json"
			},
			{
				"mediaType": "application/vnd.oci.image.manifest.v1+json",
				"size": 200,
				"digest": "sha256:bb00000000000000000000000000000000000000000000000000000000000000",
				"artifactType": "application/vnd.dev.sigstore.bundle.v0.3+json"
			}
		]
	}`)

	entries, err := ParseBundleIndex(raw)
	if err != nil {
		t.Fatalf("ParseBundleIndex() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1 (only the sigstore-bundle artifactType entry)", len(entries))
	}
	if entries[0].Digest != "sha256:bb00000000000000000000000000000000000000000000000000000000000000" {
		t.Fatalf("entries[0].Digest = %q, want the matching entry's digest", entries[0].Digest)
	}
}

func TestParseBundleIndex_MalformedJSONIsRejected(t *testing.T) {
	t.Parallel()

	_, err := ParseBundleIndex([]byte(`not json`))
	if err == nil {
		t.Fatal("ParseBundleIndex(malformed) error = nil, want error")
	}
}

func TestParseBundleIndex_ExceedsMaxBundleManifestBytesIsRejected(t *testing.T) {
	t.Parallel()

	oversized := make([]byte, MaxBundleManifestBytes+1)
	for i := range oversized {
		oversized[i] = ' '
	}

	_, err := ParseBundleIndex(oversized)
	if err == nil {
		t.Fatal("ParseBundleIndex(oversized) error = nil, want error")
	}
}

func TestParseBundleIndex_EnforcesMaxBundleIndexEntries(t *testing.T) {
	t.Parallel()

	var builder strings.Builder
	builder.WriteString(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[`)
	for i := 0; i < MaxBundleIndexEntries+1; i++ {
		if i > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(`{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:ee00000000000000000000000000000000000000000000000000000000000000","size":1,"artifactType":"application/vnd.dev.sigstore.bundle.v0.3+json"}`)
	}
	builder.WriteString(`]}`)

	_, err := ParseBundleIndex([]byte(builder.String()))
	if err == nil {
		t.Fatalf("ParseBundleIndex() with %d manifests error = nil, want error (MaxBundleIndexEntries=%d)", MaxBundleIndexEntries+1, MaxBundleIndexEntries)
	}
}

func TestParseBundleReferrerManifest_RealFixture(t *testing.T) {
	t.Parallel()

	raw := readTestdataFixture(t, "bundle-referrer-manifest.json")

	subjectDigest, layerDigest, err := ParseBundleReferrerManifest(raw)
	if err != nil {
		t.Fatalf("ParseBundleReferrerManifest() error = %v", err)
	}
	if subjectDigest != realBundleImageDigest {
		t.Fatalf("subjectDigest = %q, want %q", subjectDigest, realBundleImageDigest)
	}
	if layerDigest != realBundleLayerDigest {
		t.Fatalf("layerDigest = %q, want %q", layerDigest, realBundleLayerDigest)
	}
}

func TestParseBundleReferrerManifest_SkipsNonMatchingLayerMediaType(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.oci.image.manifest.v1+json",
		"layers": [
			{"mediaType": "application/vnd.oci.image.layer.v1.tar", "digest": "sha256:cc00000000000000000000000000000000000000000000000000000000000000", "size": 10}
		],
		"subject": {
			"mediaType": "application/vnd.docker.distribution.manifest.v2+json",
			"size": 949,
			"digest": "sha256:4a668fd22601acf91adb14ae57c2fe45a61010c709b3c48ff12b0b9187147640"
		}
	}`)

	subjectDigest, layerDigest, err := ParseBundleReferrerManifest(raw)
	if err != nil {
		t.Fatalf("ParseBundleReferrerManifest() error = %v, want nil (skip, not a hard error)", err)
	}
	if subjectDigest != "" || layerDigest != "" {
		t.Fatalf("ParseBundleReferrerManifest() = (%q, %q), want (\"\", \"\") when no layer matches the bundle media type", subjectDigest, layerDigest)
	}
}

func TestParseBundleReferrerManifest_SkipsMissingSubject(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.oci.image.manifest.v1+json",
		"layers": [
			{"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json", "digest": "sha256:cc00000000000000000000000000000000000000000000000000000000000000", "size": 10}
		]
	}`)

	subjectDigest, layerDigest, err := ParseBundleReferrerManifest(raw)
	if err != nil {
		t.Fatalf("ParseBundleReferrerManifest() error = %v, want nil (skip, not a hard error)", err)
	}
	if subjectDigest != "" || layerDigest != "" {
		t.Fatalf("ParseBundleReferrerManifest() = (%q, %q), want (\"\", \"\") when there is no subject", subjectDigest, layerDigest)
	}
}

func TestParseBundleReferrerManifest_MalformedJSONIsRejected(t *testing.T) {
	t.Parallel()

	_, _, err := ParseBundleReferrerManifest([]byte(`not json`))
	if err == nil {
		t.Fatal("ParseBundleReferrerManifest(malformed) error = nil, want error")
	}
}

func TestParseBundleReferrerManifest_ExceedsMaxBundleManifestBytesIsRejected(t *testing.T) {
	t.Parallel()

	oversized := make([]byte, MaxBundleManifestBytes+1)
	for i := range oversized {
		oversized[i] = ' '
	}

	_, _, err := ParseBundleReferrerManifest(oversized)
	if err == nil {
		t.Fatal("ParseBundleReferrerManifest(oversized) error = nil, want error")
	}
}

func TestParseBundleDocument_RealFixture(t *testing.T) {
	t.Parallel()

	raw := readTestdataFixture(t, "bundle-document.json")

	bundle, err := ParseBundleDocument(raw)
	if err != nil {
		t.Fatalf("ParseBundleDocument() error = %v", err)
	}
	if bundle.PayloadType != "application/vnd.in-toto+json" {
		t.Fatalf("bundle.PayloadType = %q, want application/vnd.in-toto+json", bundle.PayloadType)
	}
	if bundle.Payload == "" {
		t.Fatal("bundle.Payload is empty, want the base64 DSSE payload")
	}
	if len(bundle.Signatures) != 1 {
		t.Fatalf("len(bundle.Signatures) = %d, want 1", len(bundle.Signatures))
	}
	if bundle.Signatures[0] == "" {
		t.Fatal("bundle.Signatures[0] is empty, want the base64 ASN.1 DER signature")
	}
}

func TestParseBundleDocument_RejectsNonMatchingMediaType(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"mediaType": "application/vnd.example.other+json",
		"dsseEnvelope": {
			"payload": "eyJhIjoxfQ==",
			"payloadType": "application/vnd.in-toto+json",
			"signatures": [{"sig": "c2ln"}]
		}
	}`)

	_, err := ParseBundleDocument(raw)
	if err == nil {
		t.Fatal("ParseBundleDocument() with a non-bundle mediaType error = nil, want error")
	}
}

func TestParseBundleDocument_MalformedJSONIsRejected(t *testing.T) {
	t.Parallel()

	_, err := ParseBundleDocument([]byte(`not json`))
	if err == nil {
		t.Fatal("ParseBundleDocument(malformed) error = nil, want error")
	}
}

func TestParseBundleDocument_MissingDSSEEnvelopeIsRejected(t *testing.T) {
	t.Parallel()

	_, err := ParseBundleDocument([]byte(`{"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json"}`))
	if err == nil {
		t.Fatal("ParseBundleDocument(no dsseEnvelope) error = nil, want error")
	}
}

func TestParseBundleDocument_ExceedsMaxBundleDocumentBytesIsRejected(t *testing.T) {
	t.Parallel()

	oversized := make([]byte, MaxBundleDocumentBytes+1)
	for i := range oversized {
		oversized[i] = ' '
	}

	_, err := ParseBundleDocument(oversized)
	if err == nil {
		t.Fatal("ParseBundleDocument(oversized) error = nil, want error")
	}
}

func TestCheckBundleClaims_MatchingDigestPasses(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"_type":"https://in-toto.io/Statement/v1","subject":[{"digest":{"sha256":"4a668fd22601acf91adb14ae57c2fe45a61010c709b3c48ff12b0b9187147640"},"annotations":{}}],"predicateType":"https://sigstore.dev/cosign/sign/v1","predicate":{}}`)

	if err := CheckBundleClaims(payload, realBundleImageDigest); err != nil {
		t.Fatalf("CheckBundleClaims() error = %v, want nil", err)
	}
}

func TestCheckBundleClaims_MismatchedDigestFails(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"_type":"https://in-toto.io/Statement/v1","subject":[{"digest":{"sha256":"4a668fd22601acf91adb14ae57c2fe45a61010c709b3c48ff12b0b9187147640"},"annotations":{}}],"predicateType":"https://sigstore.dev/cosign/sign/v1","predicate":{}}`)

	err := CheckBundleClaims(payload, "sha256:0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("CheckBundleClaims() with a mismatched digest error = nil, want error")
	}
}

func TestCheckBundleClaims_MalformedPayloadFails(t *testing.T) {
	t.Parallel()

	err := CheckBundleClaims([]byte(`not json`), realBundleImageDigest)
	if err == nil {
		t.Fatal("CheckBundleClaims(malformed payload) error = nil, want error")
	}
}

func TestCheckBundleClaims_EmptySubjectFails(t *testing.T) {
	t.Parallel()

	err := CheckBundleClaims([]byte(`{"_type":"https://in-toto.io/Statement/v1","subject":[],"predicateType":"x","predicate":{}}`), realBundleImageDigest)
	if err == nil {
		t.Fatal("CheckBundleClaims(empty subject) error = nil, want error")
	}
}
