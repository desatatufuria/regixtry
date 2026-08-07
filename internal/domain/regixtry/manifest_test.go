package regixtry

import "testing"

func TestNewManifestComputesDigestAndReferences(t *testing.T) {
	t.Parallel()

	config := Descriptor{MediaType: "application/vnd.oci.image.config.v1+json", Digest: DigestFromBytes([]byte("cfg")), Size: 3}
	layer := Descriptor{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: DigestFromBytes([]byte("layer")), Size: 5}

	manifest, err := NewManifest("application/vnd.oci.image.manifest.v1+json", []byte(`{"schemaVersion":2}`), &config, []Descriptor{layer}, nil, map[string]string{"org.opencontainers.image.ref.name": "latest"})
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}

	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	refs := manifest.References()
	if len(refs) != 2 {
		t.Fatalf("len(References()) = %d, want 2", len(refs))
	}

	if refs[0].Digest != config.Digest || refs[1].Digest != layer.Digest {
		t.Fatalf("unexpected references order: %#v", refs)
	}
}

func TestManifestValidateRejectsDigestMismatch(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		MediaType: "application/vnd.oci.image.manifest.v1+json",
		Digest:    MustParseDigest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Size:      int64(len([]byte("payload"))),
		Payload:   []byte("payload"),
	}

	err := manifest.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}

	if !IsCode(err, ErrorCodeDigestMismatch) {
		t.Fatalf("expected digest mismatch error, got %v", err)
	}
}
