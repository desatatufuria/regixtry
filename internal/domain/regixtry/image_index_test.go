package regixtry

import "testing"

// TestParseImageIndexEntriesReadsPlatformPerManifest is the RED test for
// image-index-platform-breakdown: an OCI Image Index's manifests[] array
// must parse into one ImageIndexEntry per child manifest, carrying its own
// descriptor (digest/mediaType/size) and Platform (architecture/os/variant),
// in payload order -- the shape the Console manifest inspection screen needs
// to render a multi-arch platform breakdown.
func TestParseImageIndexEntriesReadsPlatformPerManifest(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.oci.image.index.v1+json",
		"manifests": [
			{
				"mediaType": "application/vnd.oci.image.manifest.v1+json",
				"digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"size": 100,
				"platform": {"architecture": "amd64", "os": "linux"}
			},
			{
				"mediaType": "application/vnd.oci.image.manifest.v1+json",
				"digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"size": 200,
				"platform": {"architecture": "arm64", "os": "linux", "variant": "v8"}
			}
		]
	}`)

	entries, err := ParseImageIndexEntries(payload)
	if err != nil {
		t.Fatalf("ParseImageIndexEntries() error = %v", err)
	}
	if got, want := len(entries), 2; got != want {
		t.Fatalf("len(entries) = %d, want %d", got, want)
	}

	first := entries[0]
	if got, want := first.Digest, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; got != want {
		t.Fatalf("entries[0].Digest = %q, want %q", got, want)
	}
	if got, want := first.MediaType, "application/vnd.oci.image.manifest.v1+json"; got != want {
		t.Fatalf("entries[0].MediaType = %q, want %q", got, want)
	}
	if got, want := first.Size, int64(100); got != want {
		t.Fatalf("entries[0].Size = %d, want %d", got, want)
	}
	if first.Platform == nil {
		t.Fatalf("entries[0].Platform = nil, want a populated Platform")
	}
	if got, want := first.Platform.Architecture, "amd64"; got != want {
		t.Fatalf("entries[0].Platform.Architecture = %q, want %q", got, want)
	}
	if got, want := first.Platform.OS, "linux"; got != want {
		t.Fatalf("entries[0].Platform.OS = %q, want %q", got, want)
	}
	if first.Platform.Variant != "" {
		t.Fatalf("entries[0].Platform.Variant = %q, want empty (amd64 has no variant)", first.Platform.Variant)
	}

	second := entries[1]
	if got, want := second.Platform.Architecture, "arm64"; got != want {
		t.Fatalf("entries[1].Platform.Architecture = %q, want %q", got, want)
	}
	if got, want := second.Platform.Variant, "v8"; got != want {
		t.Fatalf("entries[1].Platform.Variant = %q, want %q", got, want)
	}
}

// TestParseImageIndexEntriesEntryWithoutPlatformIsNilNotZeroValue proves a
// manifests[] entry with no "platform" object at all (legal per OCI 1.1 --
// e.g. some attestation-only index entries) parses to a nil Platform, never
// a zero-value *Platform{} that would render as an empty "/" platform
// string.
func TestParseImageIndexEntriesEntryWithoutPlatformIsNilNotZeroValue(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.oci.image.index.v1+json",
		"manifests": [
			{"mediaType": "application/vnd.oci.image.manifest.v1+json", "digest": "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "size": 50}
		]
	}`)

	entries, err := ParseImageIndexEntries(payload)
	if err != nil {
		t.Fatalf("ParseImageIndexEntries() error = %v", err)
	}
	if got, want := len(entries), 1; got != want {
		t.Fatalf("len(entries) = %d, want %d", got, want)
	}
	if entries[0].Platform != nil {
		t.Fatalf("entries[0].Platform = %#v, want nil", entries[0].Platform)
	}
}

// TestParseImageIndexEntriesEmptyManifestsIsEmptyNotNil mirrors
// ReferrersIndex.Manifests' own never-nil discipline (oci-referrers-api):
// callers range over the result unconditionally.
func TestParseImageIndexEntriesEmptyManifestsIsEmptyNotNil(t *testing.T) {
	t.Parallel()

	entries, err := ParseImageIndexEntries([]byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}`))
	if err != nil {
		t.Fatalf("ParseImageIndexEntries() error = %v", err)
	}
	if entries == nil {
		t.Fatalf("entries = nil, want a non-nil empty slice")
	}
	if len(entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0", len(entries))
	}
}

func TestParseImageIndexEntriesRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	_, err := ParseImageIndexEntries([]byte(`{not json`))
	if err == nil {
		t.Fatalf("ParseImageIndexEntries() error = nil, want a parse error")
	}
	if !IsCode(err, ErrorCodeInvalidManifest) {
		t.Fatalf("ParseImageIndexEntries() error = %v, want ErrorCodeInvalidManifest", err)
	}
}

func TestParseImageIndexEntriesExceedsMaxImageIndexBytesIsRejected(t *testing.T) {
	t.Parallel()

	oversized := make([]byte, MaxImageIndexBytes+1)
	_, err := ParseImageIndexEntries(oversized)
	if err == nil {
		t.Fatalf("ParseImageIndexEntries() error = nil, want rejection of an oversized payload")
	}
	if !IsCode(err, ErrorCodeInvalidManifest) {
		t.Fatalf("ParseImageIndexEntries() error = %v, want ErrorCodeInvalidManifest", err)
	}
}

func TestParseImageIndexEntriesExceedsMaxImageIndexEntriesIsRejected(t *testing.T) {
	t.Parallel()

	entry := `{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","size":1}`
	manifests := "["
	for i := 0; i <= MaxImageIndexEntries; i++ {
		if i > 0 {
			manifests += ","
		}
		manifests += entry
	}
	manifests += "]"
	payload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":` + manifests + `}`)

	_, err := ParseImageIndexEntries(payload)
	if err == nil {
		t.Fatalf("ParseImageIndexEntries() error = nil, want rejection of more than %d entries", MaxImageIndexEntries)
	}
	if !IsCode(err, ErrorCodeInvalidManifest) {
		t.Fatalf("ParseImageIndexEntries() error = %v, want ErrorCodeInvalidManifest", err)
	}
}

func TestIsImageIndexMediaTypeRecognizesBothIndexTypesAndRejectsOthers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mediaType string
		want      bool
	}{
		{OCIImageIndexMediaType, true},
		{DockerManifestListMediaType, true},
		{"application/vnd.oci.image.manifest.v1+json", false},
		{"application/vnd.docker.distribution.manifest.v2+json", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := IsImageIndexMediaType(tt.mediaType); got != tt.want {
			t.Fatalf("IsImageIndexMediaType(%q) = %v, want %v", tt.mediaType, got, tt.want)
		}
	}
}
