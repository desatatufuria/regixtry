package regixtry

import (
	"context"
	"testing"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
)

// tagManifestForTest publishes an additional tag pointing at digest's
// already-seeded manifest content -- CountManifestsSignedByKey enumerates
// TAGS, not bare digests, so every fixture below needs at least one real
// tag (mirroring how an operator's registry actually looks: tagged images,
// not orphaned digests).
func tagManifestForTest(t *testing.T, service *Service, repository, tag, digest string) {
	t.Helper()

	repo := mustParseRepository(t, repository)
	manifest, err := service.metadata.ResolveManifest(context.Background(), "tenant-a", repo, digest)
	if err != nil {
		t.Fatalf("metadata.ResolveManifest(%q, %q) error = %v", repository, digest, err)
	}
	if err := service.metadata.PublishManifest(context.Background(), "tenant-a", repo, tag, manifest, manifest.BlobReferences()); err != nil {
		t.Fatalf("metadata.PublishManifest(tag=%q) error = %v", tag, err)
	}
}

func mustParseRepository(t *testing.T, repository string) domain.RepositoryRef {
	t.Helper()
	ref, err := parseRepository(repository)
	if err != nil {
		t.Fatalf("parseRepository(%q) error = %v", repository, err)
	}
	return ref
}

// TestCountManifestsSignedByKeyRejectsUnusableKey pins the validation
// boundary: a keyPEM that ParseTrustedKey cannot parse is a
// domain.NewValidationError, never a silent 0 count.
func TestCountManifestsSignedByKeyRejectsUnusableKey(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	_, _, err := service.CountManifestsSignedByKey(context.Background(), "", "not a pem at all")
	if err == nil {
		t.Fatal("CountManifestsSignedByKey() error = nil, want a validation error for an unusable key")
	}
	if !domain.IsCode(err, domain.ErrorCodeValidation) {
		t.Fatalf("CountManifestsSignedByKey() error = %v, want ErrorCodeValidation", err)
	}
}

// TestCountManifestsSignedByKeyCountsLegacyFormatMatch is the legacy `.sig`
// happy path: a currently-tagged digest whose legacy signature verifies
// against keyPEM counts once.
func TestCountManifestsSignedByKeyCountsLegacyFormatMatch(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedFixtureImageManifest(t, service, repository)
	tagManifestForTest(t, service, repository, "latest", digest)
	seedFixtureSignatureArtifact(t, service, repository)

	count, capped, err := service.CountManifestsSignedByKey(context.Background(), repository, fixtureTrustedKeyPEM(t))
	if err != nil {
		t.Fatalf("CountManifestsSignedByKey() error = %v, want nil", err)
	}
	if capped {
		t.Fatal("CountManifestsSignedByKey() capped = true, want false (well under the scan cap)")
	}
	if count != 1 {
		t.Fatalf("CountManifestsSignedByKey() count = %d, want 1", count)
	}
}

// TestCountManifestsSignedByKeyCountsBundleFormatMatch mirrors the legacy
// test above for the modern cosign v3 bundle format, reusing
// seedBundleSignatureArtifact from service_signing_bundle_test.go.
func TestCountManifestsSignedByKeyCountsBundleFormatMatch(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	imageDigest := seedArbitraryImageManifest(t, service, repository, " - key usage bundle match")
	tagManifestForTest(t, service, repository, "latest", imageDigest)
	key, keyPEM := generateTestECDSAP256KeyPair(t)
	seedBundleSignatureArtifact(t, service, repository, imageDigest, imageDigest, bareHex(imageDigest), key)

	count, capped, err := service.CountManifestsSignedByKey(context.Background(), repository, keyPEM)
	if err != nil {
		t.Fatalf("CountManifestsSignedByKey() error = %v, want nil", err)
	}
	if capped {
		t.Fatal("CountManifestsSignedByKey() capped = true, want false")
	}
	if count != 1 {
		t.Fatalf("CountManifestsSignedByKey() count = %d, want 1", count)
	}
}

// TestCountManifestsSignedByKeyDoesNotCountUnrelatedKeyOrUnsignedTag is the
// negative case: a tagged image with a signature that exists but is from a
// DIFFERENT key never counts, and never errors -- this is advisory, not a
// fail-closed gate.
func TestCountManifestsSignedByKeyDoesNotCountUnrelatedKeyOrUnsignedTag(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedFixtureImageManifest(t, service, repository)
	tagManifestForTest(t, service, repository, "latest", digest)
	seedFixtureSignatureArtifact(t, service, repository)

	_, unrelatedKeyPEM := generateTestECDSAP256KeyPair(t)

	count, capped, err := service.CountManifestsSignedByKey(context.Background(), repository, unrelatedKeyPEM)
	if err != nil {
		t.Fatalf("CountManifestsSignedByKey() error = %v, want nil", err)
	}
	if capped {
		t.Fatal("CountManifestsSignedByKey() capped = true, want false")
	}
	if count != 0 {
		t.Fatalf("CountManifestsSignedByKey() count = %d, want 0 (signature exists but not from this key)", count)
	}
}

// TestCountManifestsSignedByKeyScopesToOneRepository confirms a non-empty
// repository argument counts only that repository's tags, and that ""
// counts across every repository -- the two scopes the endpoint supports.
func TestCountManifestsSignedByKeyScopesToOneRepository(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	key, keyPEM := generateTestECDSAP256KeyPair(t)

	signedRepo := "library/signed"
	signedDigest := seedArbitraryImageManifest(t, service, signedRepo, " - scoped signed")
	tagManifestForTest(t, service, signedRepo, "latest", signedDigest)
	seedBundleSignatureArtifact(t, service, signedRepo, signedDigest, signedDigest, bareHex(signedDigest), key)

	otherRepo := "library/other"
	otherDigest := seedArbitraryImageManifest(t, service, otherRepo, " - scoped other")
	tagManifestForTest(t, service, otherRepo, "latest", otherDigest)
	seedBundleSignatureArtifact(t, service, otherRepo, otherDigest, otherDigest, bareHex(otherDigest), key)

	scopedCount, _, err := service.CountManifestsSignedByKey(context.Background(), otherRepo, keyPEM)
	if err != nil {
		t.Fatalf("CountManifestsSignedByKey(otherRepo) error = %v, want nil", err)
	}
	if scopedCount != 1 {
		t.Fatalf("CountManifestsSignedByKey(otherRepo) count = %d, want 1 (must not see signedRepo's match)", scopedCount)
	}

	globalCount, _, err := service.CountManifestsSignedByKey(context.Background(), "", keyPEM)
	if err != nil {
		t.Fatalf("CountManifestsSignedByKey(\"\") error = %v, want nil", err)
	}
	if globalCount != 2 {
		t.Fatalf("CountManifestsSignedByKey(\"\") count = %d, want 2 (both repositories)", globalCount)
	}
}

// TestCountManifestsSignedByKeyStopsAtScanCapAndReportsCapped exercises the
// internal countManifestsSignedByKey helper with a tiny scanCap (the public
// CountManifestsSignedByKey always uses the real maxKeyUsageScanTags=500,
// which would be impractically slow to actually exceed in a unit test) to
// pin the "stop at the cap, report capped: true" contract deterministically
// and fast.
func TestCountManifestsSignedByKeyStopsAtScanCapAndReportsCapped(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	tags := []string{"v1", "v2", "v3"}
	for i, tag := range tags {
		digest := seedArbitraryImageManifest(t, service, repository, tag)
		_ = i
		tagManifestForTest(t, service, repository, tag, digest)
	}
	_, keyPEM := generateTestECDSAP256KeyPair(t)
	key, err := signing.ParseTrustedKey(keyPEM)
	if err != nil {
		t.Fatalf("signing.ParseTrustedKey() error = %v", err)
	}

	count, capped, err := service.countManifestsSignedByKey(context.Background(), repository, key, 2)
	if err != nil {
		t.Fatalf("countManifestsSignedByKey() error = %v, want nil", err)
	}
	if !capped {
		t.Fatal("countManifestsSignedByKey() capped = false, want true (3 tags > scanCap of 2)")
	}
	if count != 0 {
		t.Fatalf("countManifestsSignedByKey() count = %d, want 0 (none of the 3 tags are signed)", count)
	}
}

// TestCountManifestsSignedByKeyUnderTheCapIsNotReportedCapped is the
// boundary companion: a scope at or under scanCap must report capped: false.
func TestCountManifestsSignedByKeyUnderTheCapIsNotReportedCapped(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - under cap")
	tagManifestForTest(t, service, repository, "latest", digest)
	_, keyPEM := generateTestECDSAP256KeyPair(t)
	key, err := signing.ParseTrustedKey(keyPEM)
	if err != nil {
		t.Fatalf("signing.ParseTrustedKey() error = %v", err)
	}

	_, capped, err := service.countManifestsSignedByKey(context.Background(), repository, key, 2)
	if err != nil {
		t.Fatalf("countManifestsSignedByKey() error = %v, want nil", err)
	}
	if capped {
		t.Fatal("countManifestsSignedByKey() capped = true, want false (1 tag <= scanCap of 2)")
	}
}
