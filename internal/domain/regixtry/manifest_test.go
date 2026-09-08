package regixtry

import "testing"

func TestNewManifestComputesDigestAndReferences(t *testing.T) {
	t.Parallel()

	config := Descriptor{MediaType: "application/vnd.oci.image.config.v1+json", Digest: DigestFromBytes([]byte("cfg")), Size: 3}
	layer := Descriptor{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: DigestFromBytes([]byte("layer")), Size: 5}

	manifest, err := NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2}`), &config, []Descriptor{layer}, nil, map[string]string{"org.opencontainers.image.ref.name": "latest"})
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}

	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	refs := manifest.BlobReferences()
	if len(refs) != 2 {
		t.Fatalf("len(BlobReferences()) = %d, want 2", len(refs))
	}

	if refs[0].Digest != config.Digest || refs[1].Digest != layer.Digest {
		t.Fatalf("unexpected references order: %#v", refs)
	}
}

// TestManifestBlobReferencesExcludesSubject ensures Subject is never folded
// into BlobReferences: it is a manifest-to-manifest pointer (OCI 1.1), never
// a blob, so it must not be validated or persisted as one.
func TestManifestBlobReferencesExcludesSubject(t *testing.T) {
	t.Parallel()

	config := Descriptor{MediaType: "application/vnd.oci.image.config.v1+json", Digest: DigestFromBytes([]byte("cfg")), Size: 3}
	layer := Descriptor{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: DigestFromBytes([]byte("layer")), Size: 5}
	subject := Descriptor{MediaType: "application/vnd.oci.image.manifest.v1+json", Digest: DigestFromBytes([]byte("subject-manifest")), Size: 7}

	manifest, err := NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2}`), &config, []Descriptor{layer}, &subject, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}

	if manifest.Subject == nil || manifest.Subject.Digest != subject.Digest {
		t.Fatalf("manifest.Subject = %#v, want %#v", manifest.Subject, subject)
	}

	refs := manifest.BlobReferences()
	if len(refs) != 2 {
		t.Fatalf("len(BlobReferences()) = %d, want 2 (config + layer only)", len(refs))
	}

	for _, ref := range refs {
		if ref.Digest == subject.Digest {
			t.Fatalf("BlobReferences() = %#v, must not contain subject digest %s", refs, subject.Digest)
		}
	}
}

// TestNewManifestCarriesArtifactType is the oci-referrers-api Phase 1 RED
// test (design.md Decision 3, tasks.md 1.1): NewManifest must carry a raw
// artifactType value through to the constructed Manifest verbatim -- no
// fallback logic belongs at this layer (that lives in the app-layer
// resolveArtifactType, design.md Decision 3).
func TestNewManifestCarriesArtifactType(t *testing.T) {
	t.Parallel()

	manifest, err := NewManifest("application/vnd.oci.image.manifest.v1+json", "application/vnd.example.sbom.v1+json", []byte(`{"schemaVersion":2}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}

	if manifest.ArtifactType != "application/vnd.example.sbom.v1+json" {
		t.Fatalf("manifest.ArtifactType = %q, want %q", manifest.ArtifactType, "application/vnd.example.sbom.v1+json")
	}
}

// TestNewManifestArtifactTypeAbsentIsEmptyNeverAFallback pins tasks.md 1.1's
// second half: an absent artifactType input must store "", never silently
// fall back to config.MediaType or anything else at the domain layer.
func TestNewManifestArtifactTypeAbsentIsEmptyNeverAFallback(t *testing.T) {
	t.Parallel()

	config := Descriptor{MediaType: "application/vnd.oci.image.config.v1+json", Digest: DigestFromBytes([]byte("cfg")), Size: 3}

	manifest, err := NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2}`), &config, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}

	if manifest.ArtifactType != "" {
		t.Fatalf("manifest.ArtifactType = %q, want empty -- absent artifactType must never fall back to config.MediaType at this layer", manifest.ArtifactType)
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
