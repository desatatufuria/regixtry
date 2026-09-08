package regixtry

import (
	"context"
	"encoding/json"
	"testing"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// testManifestResource/testManifestEnvelope mirror manifestEnvelope's JSON
// shape (service.go) so a test can build a raw manifest payload whose bytes
// actually carry artifactType/config/subject/annotations -- required because
// Service.Referrers re-derives everything from the stored payload's own JSON
// via parseManifestPayload, never from a domain.Manifest value the test
// happened to construct in memory.
type testManifestResource struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

type testManifestEnvelope struct {
	SchemaVersion int                   `json:"schemaVersion"`
	MediaType     string                `json:"mediaType"`
	ArtifactType  string                `json:"artifactType,omitempty"`
	Config        *testManifestResource `json:"config,omitempty"`
	Subject       *testManifestResource `json:"subject,omitempty"`
	Annotations   map[string]string     `json:"annotations,omitempty"`
}

func marshalManifestPayload(t *testing.T, envelope testManifestEnvelope) []byte {
	t.Helper()

	payload, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	return payload
}

// denyAccessController rejects every action, unconditionally -- used to
// prove Referrers authorizes BEFORE parsing the requested digest (design.md
// Decision 7): an unauthorized caller sending a malformed digest must get
// ErrorCodeUnauthorized, never ErrorCodeInvalidDigest (threat matrix:
// capability/validation disclosure).
type denyAccessController struct{}

func (denyAccessController) Authorize(context.Context, ports.Action) error {
	return domain.NewUnauthorizedError("access denied")
}

func (denyAccessController) Challenge(ports.Action) ports.Challenge {
	return ports.Challenge{Scheme: "Bearer", Realm: "regixtry", Service: "regixtry"}
}

// --- resolveArtifactType: pure function, table test (tasks.md 6.1) --------

func TestResolveArtifactTypeManifestValueWins(t *testing.T) {
	t.Parallel()

	manifest, err := domain.NewManifest(
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.example.sbom.v1+json",
		[]byte(`{"schemaVersion":2}`),
		&domain.Descriptor{MediaType: "application/vnd.myapp.config.v1+json", Digest: domain.DigestFromBytes([]byte("config")), Size: 6},
		nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}

	if got := resolveArtifactType(manifest); got != "application/vnd.example.sbom.v1+json" {
		t.Fatalf("resolveArtifactType() = %q, want the manifest's own artifactType even though Config is also set", got)
	}
}

func TestResolveArtifactTypeAbsentFallsBackToConfigMediaType(t *testing.T) {
	t.Parallel()

	manifest, err := domain.NewManifest(
		"application/vnd.oci.image.manifest.v1+json",
		"",
		[]byte(`{"schemaVersion":2}`),
		&domain.Descriptor{MediaType: "application/vnd.myapp.config.v1+json", Digest: domain.DigestFromBytes([]byte("config")), Size: 6},
		nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}

	if got := resolveArtifactType(manifest); got != "application/vnd.myapp.config.v1+json" {
		t.Fatalf("resolveArtifactType() = %q, want config.mediaType fallback", got)
	}
}

func TestResolveArtifactTypeAbsentAndNoConfigIsEmpty(t *testing.T) {
	t.Parallel()

	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}

	if got := resolveArtifactType(manifest); got != "" {
		t.Fatalf(`resolveArtifactType() = %q, want "" when both artifactType and Config are absent`, got)
	}
}

// --- Service.Referrers: authorization ordering (tasks.md 6.2) -------------

func TestServiceReferrersAuthorizesBeforeParsingDigestUnauthorizedGetsUnauthorizedNotInvalidDigest(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, denyAccessController{})
	defer cleanup()

	_, err := service.Referrers(context.Background(), "library/alpine", "not-a-valid-digest-at-all", "")
	if err == nil {
		t.Fatalf("Referrers() error = nil, want ErrorCodeUnauthorized")
	}
	if !domain.IsCode(err, domain.ErrorCodeUnauthorized) {
		t.Fatalf("Referrers() error = %v, want ErrorCodeUnauthorized -- authorization must run BEFORE digest parsing (design.md Decision 7)", err)
	}
	if domain.IsCode(err, domain.ErrorCodeInvalidDigest) {
		t.Fatalf("Referrers() error = %v, must NOT be ErrorCodeInvalidDigest for an unauthorized caller (capability/validation disclosure)", err)
	}
}

// --- Service.Referrers: never-nil Manifests slice (tasks.md 6.3) ----------

func TestServiceReferrersManifestsIsNeverNilOnZeroRows(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	neverPushed := domain.DigestFromBytes([]byte("never-pushed-subject")).String()

	result, err := service.Referrers(context.Background(), "library/alpine", neverPushed, "")
	if err != nil {
		t.Fatalf("Referrers() error = %v", err)
	}

	if result.Manifests == nil {
		t.Fatalf("Referrers().Manifests = nil, want a non-nil empty slice -- a nil slice encodes as JSON null, breaking the empty-list-never-404 wire contract")
	}
	if len(result.Manifests) != 0 {
		t.Fatalf("Referrers().Manifests = %#v, want empty", result.Manifests)
	}
	if result.SchemaVersion != 2 || result.MediaType != ociImageIndexMediaType {
		t.Fatalf("Referrers() index = %+v, want schemaVersion 2 and mediaType %q even on the empty path", result, ociImageIndexMediaType)
	}
}

// --- Service.Referrers: mapping, fallback, and filtering -------------------

// TestServiceReferrersReturnsMatchedReferrerWithArtifactTypeAndAnnotations
// covers the spec's "Pushed referrer is listed" and "ArtifactType present"
// scenarios: a referrer's own artifactType and annotations flow through
// unchanged.
func TestServiceReferrersReturnsMatchedReferrerWithArtifactTypeAndAnnotations(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	ctx := context.Background()
	repo := domain.MustParseRepositoryRef("library/alpine")

	target, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2,"target":true}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest(target) error = %v", err)
	}
	if err := service.metadata.PublishManifest(ctx, "tenant-a", repo, "target", target, nil); err != nil {
		t.Fatalf("PublishManifest(target) error = %v", err)
	}

	subject := &domain.Descriptor{MediaType: target.MediaType, Digest: target.Digest, Size: target.Size}
	annotations := map[string]string{"org.opencontainers.image.created": "2026-01-01T00:00:00Z"}
	referrerPayload := marshalManifestPayload(t, testManifestEnvelope{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		ArtifactType:  "application/vnd.example.sbom.v1+json",
		Subject:       &testManifestResource{MediaType: subject.MediaType, Digest: subject.Digest.String(), Size: subject.Size},
		Annotations:   annotations,
	})
	referrer, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "application/vnd.example.sbom.v1+json", referrerPayload, nil, nil, subject, annotations)
	if err != nil {
		t.Fatalf("NewManifest(referrer) error = %v", err)
	}
	if err := service.metadata.PublishManifest(ctx, "tenant-a", repo, "", referrer, nil); err != nil {
		t.Fatalf("PublishManifest(referrer) error = %v", err)
	}

	result, err := service.Referrers(ctx, "library/alpine", target.Digest.String(), "")
	if err != nil {
		t.Fatalf("Referrers() error = %v", err)
	}

	if len(result.Manifests) != 1 {
		t.Fatalf("Referrers().Manifests = %d entries, want 1", len(result.Manifests))
	}

	got := result.Manifests[0]
	if got.Digest != referrer.Digest.String() {
		t.Fatalf("Manifests[0].Digest = %q, want %q", got.Digest, referrer.Digest.String())
	}
	if got.ArtifactType != "application/vnd.example.sbom.v1+json" {
		t.Fatalf("Manifests[0].ArtifactType = %q, want the manifest's own artifactType", got.ArtifactType)
	}
	if got.Annotations["org.opencontainers.image.created"] != "2026-01-01T00:00:00Z" {
		t.Fatalf("Manifests[0].Annotations = %#v, want the manifest's own annotations", got.Annotations)
	}
}

// TestServiceReferrersFallsBackToConfigMediaTypeWhenArtifactTypeAbsent covers
// the spec's "ArtifactType absent falls back" scenario end-to-end (not just
// the pure resolveArtifactType unit above): a referrer with no top-level
// artifactType, but a Config descriptor, must report Config.MediaType.
func TestServiceReferrersFallsBackToConfigMediaTypeWhenArtifactTypeAbsent(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	ctx := context.Background()
	repo := domain.MustParseRepositoryRef("library/alpine")

	target, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2,"target":true}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest(target) error = %v", err)
	}
	if err := service.metadata.PublishManifest(ctx, "tenant-a", repo, "target", target, nil); err != nil {
		t.Fatalf("PublishManifest(target) error = %v", err)
	}

	subject := &domain.Descriptor{MediaType: target.MediaType, Digest: target.Digest, Size: target.Size}
	config := &domain.Descriptor{MediaType: "application/vnd.myapp.config.v1+json", Digest: domain.DigestFromBytes([]byte("config-bytes")), Size: int64(len("config-bytes"))}
	referrerPayload := marshalManifestPayload(t, testManifestEnvelope{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		Config:        &testManifestResource{MediaType: config.MediaType, Digest: config.Digest.String(), Size: config.Size},
		Subject:       &testManifestResource{MediaType: subject.MediaType, Digest: subject.Digest.String(), Size: subject.Size},
	})
	referrer, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", referrerPayload, config, nil, subject, nil)
	if err != nil {
		t.Fatalf("NewManifest(referrer) error = %v", err)
	}
	if err := service.metadata.PublishManifest(ctx, "tenant-a", repo, "", referrer, nil); err != nil {
		t.Fatalf("PublishManifest(referrer) error = %v", err)
	}

	result, err := service.Referrers(ctx, "library/alpine", target.Digest.String(), "")
	if err != nil {
		t.Fatalf("Referrers() error = %v", err)
	}
	if len(result.Manifests) != 1 {
		t.Fatalf("Referrers().Manifests = %d entries, want 1", len(result.Manifests))
	}
	if got := result.Manifests[0].ArtifactType; got != config.MediaType {
		t.Fatalf("Manifests[0].ArtifactType = %q, want config.mediaType fallback %q", got, config.MediaType)
	}
}

// TestServiceReferrersArtifactTypeFilterNarrowsResults covers the spec's
// "Filtered request narrows results" and "Unfiltered request omits header"
// scenarios at the service layer: the header itself is Phase 7 (router),
// but the narrowing MUST happen here, over the already-fetched matched set,
// and a whitespace-only filter value MUST behave as no filter at all
// (design.md Decision 7's "set only when a filter was actually applied").
func TestServiceReferrersArtifactTypeFilterNarrowsResults(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	ctx := context.Background()
	repo := domain.MustParseRepositoryRef("library/alpine")

	target, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2,"target":true}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest(target) error = %v", err)
	}
	if err := service.metadata.PublishManifest(ctx, "tenant-a", repo, "target", target, nil); err != nil {
		t.Fatalf("PublishManifest(target) error = %v", err)
	}
	subject := &domain.Descriptor{MediaType: target.MediaType, Digest: target.Digest, Size: target.Size}
	subjectResource := &testManifestResource{MediaType: subject.MediaType, Digest: subject.Digest.String(), Size: subject.Size}

	sbomPayload := marshalManifestPayload(t, testManifestEnvelope{SchemaVersion: 2, MediaType: "application/vnd.oci.image.manifest.v1+json", ArtifactType: "application/vnd.example.sbom.v1+json", Subject: subjectResource})
	sbom, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "application/vnd.example.sbom.v1+json", sbomPayload, nil, nil, subject, nil)
	if err != nil {
		t.Fatalf("NewManifest(sbom) error = %v", err)
	}
	if err := service.metadata.PublishManifest(ctx, "tenant-a", repo, "", sbom, nil); err != nil {
		t.Fatalf("PublishManifest(sbom) error = %v", err)
	}

	signaturePayload := marshalManifestPayload(t, testManifestEnvelope{SchemaVersion: 2, MediaType: "application/vnd.oci.image.manifest.v1+json", ArtifactType: "application/vnd.example.signature.v1+json", Subject: subjectResource})
	signature, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "application/vnd.example.signature.v1+json", signaturePayload, nil, nil, subject, nil)
	if err != nil {
		t.Fatalf("NewManifest(signature) error = %v", err)
	}
	if err := service.metadata.PublishManifest(ctx, "tenant-a", repo, "", signature, nil); err != nil {
		t.Fatalf("PublishManifest(signature) error = %v", err)
	}

	unfiltered, err := service.Referrers(ctx, "library/alpine", target.Digest.String(), "")
	if err != nil {
		t.Fatalf("Referrers(unfiltered) error = %v", err)
	}
	if len(unfiltered.Manifests) != 2 {
		t.Fatalf("Referrers(unfiltered).Manifests = %d, want 2", len(unfiltered.Manifests))
	}

	filtered, err := service.Referrers(ctx, "library/alpine", target.Digest.String(), "application/vnd.example.sbom.v1+json")
	if err != nil {
		t.Fatalf("Referrers(filtered) error = %v", err)
	}
	if len(filtered.Manifests) != 1 || filtered.Manifests[0].Digest != sbom.Digest.String() {
		t.Fatalf("Referrers(filtered).Manifests = %#v, want exactly the sbom referrer", filtered.Manifests)
	}

	whitespaceFiltered, err := service.Referrers(ctx, "library/alpine", target.Digest.String(), "   ")
	if err != nil {
		t.Fatalf("Referrers(whitespace filter) error = %v", err)
	}
	if len(whitespaceFiltered.Manifests) != 2 {
		t.Fatalf("Referrers(whitespace-only artifactType) = %d entries, want 2 -- an empty/whitespace-only filter value is no filter at all", len(whitespaceFiltered.Manifests))
	}
}
