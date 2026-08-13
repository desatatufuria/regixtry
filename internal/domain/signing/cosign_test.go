package signing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignatureTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		digest  string
		want    string
		wantErr bool
	}{
		{
			name:   "valid sha256 digest maps to the legacy .sig tag",
			digest: "sha256:d1f1ed21dad86b499c874cef225b6e61c4cf1a8684363810e99d723a306a75b4",
			want:   "sha256-d1f1ed21dad86b499c874cef225b6e61c4cf1a8684363810e99d723a306a75b4.sig",
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

			got, err := SignatureTag(tt.digest)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("SignatureTag(%q) error = nil, want error", tt.digest)
				}
				return
			}
			if err != nil {
				t.Fatalf("SignatureTag(%q) unexpected error = %v", tt.digest, err)
			}
			if got != tt.want {
				t.Fatalf("SignatureTag(%q) = %q, want %q", tt.digest, got, tt.want)
			}
		})
	}
}

// synthetic-fixture note: this manifest was hand-constructed offline (no
// cosign binary was available in this apply environment) to match
// design.md Decision 1b's documented .sig manifest shape byte-for-byte, and
// is explicitly marked synthetic in testdata/README.md. It is NOT captured
// from a real `cosign sign` invocation.
func readTestdataFixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading synthetic fixture testdata/%s: %v", name, err)
	}
	return data
}

func TestParseSignatureManifest_SyntheticFixture(t *testing.T) {
	t.Parallel()

	raw := readTestdataFixture(t, "signature-manifest.json")

	entries, err := ParseSignatureManifest(raw)
	if err != nil {
		t.Fatalf("ParseSignatureManifest() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}

	entry := entries[0]
	if !strings.HasPrefix(entry.PayloadDigest, "sha256:") {
		t.Fatalf("entry.PayloadDigest = %q, want a sha256: digest", entry.PayloadDigest)
	}
	if entry.Signature == "" {
		t.Fatalf("entry.Signature is empty, want the base64 ASN.1 DER signature")
	}
}

func TestParseSignatureManifest_IgnoresNonSimplesigningAndUnannotatedLayers(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": {"mediaType": "application/vnd.oci.image.config.v1+json", "digest": "sha256:aa00000000000000000000000000000000000000000000000000000000000000", "size": 2},
		"layers": [
			{
				"mediaType": "application/vnd.oci.image.layer.v1.tar",
				"digest": "sha256:bb00000000000000000000000000000000000000000000000000000000000000",
				"size": 10
			},
			{
				"mediaType": "application/vnd.dev.cosign.simplesigning.v1+json",
				"digest": "sha256:cc00000000000000000000000000000000000000000000000000000000000000",
				"size": 20,
				"annotations": {"some.other/annotation": "irrelevant"}
			},
			{
				"mediaType": "application/vnd.dev.cosign.simplesigning.v1+json",
				"digest": "sha256:dd00000000000000000000000000000000000000000000000000000000000000",
				"size": 30,
				"annotations": {"dev.cosignproject.cosign/signature": "c2lnbmF0dXJl"}
			}
		]
	}`)

	entries, err := ParseSignatureManifest(raw)
	if err != nil {
		t.Fatalf("ParseSignatureManifest() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1 (only the annotated simplesigning layer)", len(entries))
	}
	if entries[0].PayloadDigest != "sha256:dd00000000000000000000000000000000000000000000000000000000000000" {
		t.Fatalf("entries[0].PayloadDigest = %q, want the annotated layer's digest", entries[0].PayloadDigest)
	}
	if entries[0].Signature != "c2lnbmF0dXJl" {
		t.Fatalf("entries[0].Signature = %q, want the annotation value", entries[0].Signature)
	}
}

func TestParseSignatureManifest_EnforcesMaxSignatureEntries(t *testing.T) {
	t.Parallel()

	var builder strings.Builder
	builder.WriteString(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","layers":[`)
	for i := 0; i < MaxSignatureEntries+1; i++ {
		if i > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(`{"mediaType":"application/vnd.dev.cosign.simplesigning.v1+json","digest":"sha256:ee00000000000000000000000000000000000000000000000000000000000000","size":1,"annotations":{"dev.cosignproject.cosign/signature":"c2ln"}}`)
	}
	builder.WriteString(`]}`)

	_, err := ParseSignatureManifest([]byte(builder.String()))
	if err == nil {
		t.Fatalf("ParseSignatureManifest() with %d layers error = nil, want error (MaxSignatureEntries=%d)", MaxSignatureEntries+1, MaxSignatureEntries)
	}
}

func TestParseSignatureManifest_MalformedJSONIsRejected(t *testing.T) {
	t.Parallel()

	_, err := ParseSignatureManifest([]byte(`not json`))
	if err == nil {
		t.Fatalf("ParseSignatureManifest(malformed) error = nil, want error")
	}
}

func TestParseSignatureManifest_ExceedsMaxSignatureManifestBytesIsRejected(t *testing.T) {
	t.Parallel()

	oversized := make([]byte, MaxSignatureManifestBytes+1)
	for i := range oversized {
		oversized[i] = ' '
	}

	_, err := ParseSignatureManifest(oversized)
	if err == nil {
		t.Fatalf("ParseSignatureManifest(oversized) error = nil, want error")
	}
}
