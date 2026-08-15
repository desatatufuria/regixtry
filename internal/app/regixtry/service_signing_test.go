package regixtry

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
)

// fixtureImageManifestPayload is a literal chosen so its raw bytes hash to
// fixtureImageDigest — the exact digest internal/domain/signing/testdata's
// synthetic fixture's payload.json claims in critical.image.docker-manifest-
// digest (testdata/README.md's "Reproducing" section). Publishing a manifest
// with this exact payload lets a test pull a digest the fixture's own
// signature actually binds, without needing a SHA-256 preimage search.
const fixtureImageManifestPayload = "image-signing synthetic fixture image manifest v1"

const fixtureImageDigest = "sha256:d1f1ed21dad86b499c874cef225b6e61c4cf1a8684363810e99d723a306a75b4"

// readSigningFixture reads one of internal/domain/signing/testdata's Phase 0
// synthetic fixture files (README.md there documents their provenance: hand-
// constructed offline, never captured from a real `cosign sign` invocation,
// because no cosign binary was available in the apply environment).
func readSigningFixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "domain", "signing", "testdata", name))
	if err != nil {
		t.Fatalf("reading synthetic fixture testdata/%s: %v", name, err)
	}
	return data
}

// seedArbitraryImageManifest publishes a manifest directly through the
// metadata store (bypassing Service.PublishManifest's JSON-envelope parsing
// and BlobExists checks, neither of which this package's own image-manifest
// stand-in payload needs to satisfy) and returns its computed digest. The
// suffix lets a test mint a second, unrelated digest in the same repository.
func seedArbitraryImageManifest(t *testing.T, service *Service, repository string, suffix string) string {
	t.Helper()

	payload := []byte(fixtureImageManifestPayload + suffix)
	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", payload, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("domain.NewManifest() error = %v", err)
	}

	repo, err := parseRepository(repository)
	if err != nil {
		t.Fatalf("parseRepository(%q) error = %v", repository, err)
	}

	if err := service.metadata.PublishManifest(context.Background(), "tenant-a", repo, "", manifest, manifest.BlobReferences()); err != nil {
		t.Fatalf("metadata.PublishManifest() error = %v", err)
	}

	return manifest.Digest.String()
}

// seedFixtureImageManifest publishes the exact payload the synthetic
// fixture's signature was produced against, and fails fast if the computed
// digest ever drifts from fixtureImageDigest — a canary against a fixture
// change silently invalidating every test in this file.
func seedFixtureImageManifest(t *testing.T, service *Service, repository string) string {
	t.Helper()

	digest := seedArbitraryImageManifest(t, service, repository, "")
	if digest != fixtureImageDigest {
		t.Fatalf("seeded fixture image manifest digest = %s, want %s (fixture provenance drifted)", digest, fixtureImageDigest)
	}
	return digest
}

// uploadBlobForTest is BeginUpload/AppendUpload/CompleteUpload, condensed
// for tests that only care about the resulting blob existing.
func uploadBlobForTest(t *testing.T, service *Service, repository string, content []byte) {
	t.Helper()

	ctx := context.Background()
	upload, err := service.BeginUpload(ctx, repository)
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if _, err := service.AppendUpload(ctx, repository, upload.ID, bytes.NewReader(content)); err != nil {
		t.Fatalf("AppendUpload() error = %v", err)
	}
	if _, err := service.CompleteUpload(ctx, repository, upload.ID, digestForTest(content), nil); err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
}

// publishFixtureSignatureManifestAt uploads the synthetic fixture's config
// and payload blobs (idempotent: re-uploading the same content-addressed
// blob is a no-op), then publishes the fixture's `.sig` manifest bytes
// verbatim at the legacy tag for the given digest. Publishing the same
// fixture `.sig` manifest at a digest other than fixtureImageDigest is
// exactly the signature-transplant shape CheckClaims defends against.
func publishFixtureSignatureManifestAt(t *testing.T, service *Service, repository string, digest string) {
	t.Helper()

	uploadBlobForTest(t, service, repository, []byte("{}"))
	uploadBlobForTest(t, service, repository, readSigningFixture(t, "payload.json"))

	tag, err := signing.SignatureTag(digest)
	if err != nil {
		t.Fatalf("signing.SignatureTag(%q) error = %v", digest, err)
	}

	sigManifestPayload := readSigningFixture(t, "signature-manifest.json")
	if _, err := service.PublishManifest(context.Background(), repository, tag, "application/vnd.oci.image.manifest.v1+json", sigManifestPayload); err != nil {
		t.Fatalf("PublishManifest(.sig at %s) error = %v", tag, err)
	}
	service.WaitForBackgroundWork()
}

// seedFixtureSignatureArtifact is publishFixtureSignatureManifestAt for the
// common case: the fixture's `.sig` manifest published at its own digest's
// tag, an untampered signature artifact.
func seedFixtureSignatureArtifact(t *testing.T, service *Service, repository string) {
	t.Helper()
	publishFixtureSignatureManifestAt(t, service, repository, fixtureImageDigest)
}

func seedSigningPolicy(t *testing.T, service *Service, enabled bool, keys []string) {
	t.Helper()

	if err := service.metadata.UpsertSigningPolicySettings(context.Background(), "tenant-a", ports.SigningPolicySettings{Enabled: enabled, TrustedPublicKeys: keys, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertSigningPolicySettings() error = %v", err)
	}
}

func fixtureTrustedKeyPEM(t *testing.T) string {
	t.Helper()
	return string(readSigningFixture(t, "cosign.pub"))
}

// TestServiceGetSigningPolicySettingsDefaultsToDisabledWithNoRowWritten is
// the Phase 4 RED test (tasks.md 4.1): a fresh install with zero rows
// written reports {Enabled: false} — the inverted default vs.
// GetScanPolicySettings' {Enabled: true} default (design.md Decision 4).
func TestServiceGetSigningPolicySettingsDefaultsToDisabledWithNoRowWritten(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	settings, err := service.GetSigningPolicySettings(context.Background())
	if err != nil {
		t.Fatalf("GetSigningPolicySettings() error = %v", err)
	}
	if settings.Enabled {
		t.Fatal("GetSigningPolicySettings().Enabled = true, want false (inverted default vs. GetScanPolicySettings)")
	}
	if len(settings.TrustedPublicKeys) != 0 {
		t.Fatalf("GetSigningPolicySettings().TrustedPublicKeys = %v, want empty", settings.TrustedPublicKeys)
	}
}

// TestServiceEnforceSigningPolicyAllowsPullWithoutVerificationWhenDisabled is
// the Phase 4 RED test (tasks.md 4.2): with the resolved policy disabled,
// enforceSigningPolicy must allow the pull without attempting verification —
// the only allow-without-verify path — proven for both a signed and an
// unsigned digest.
func TestServiceEnforceSigningPolicyAllowsPullWithoutVerificationWhenDisabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		seed func(t *testing.T, service *Service, repository string)
	}{
		{
			name: "a digest with a valid signature artifact",
			seed: func(t *testing.T, service *Service, repository string) {
				seedFixtureImageManifest(t, service, repository)
				seedFixtureSignatureArtifact(t, service, repository)
			},
		},
		{
			name: "a digest with no signature artifact at all",
			seed: func(t *testing.T, service *Service, repository string) {
				seedFixtureImageManifest(t, service, repository)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service, cleanup := newTestService(t, allowAllAccessController{})
			defer cleanup()

			repository := "library/alpine"
			tt.seed(t, service, repository)
			// The global signing policy is left at its zero value:
			// {Enabled: false} — the default this package resolves with no
			// row written (task 4.1).

			if err := service.enforceSigningPolicy(context.Background(), repository, fixtureImageDigest); err != nil {
				t.Fatalf("enforceSigningPolicy() error = %v, want nil (a disabled policy must never attempt verification)", err)
			}
		})
	}
}

// TestServiceEnforceSigningPolicyBlocksWhenNoSignatureTagResolves is the
// Phase 4 RED test (tasks.md 4.3): policy enabled, no `sha256-<hex>.sig` tag
// resolvable for the digest -> domain.ErrorCodePolicyViolation.
func TestServiceEnforceSigningPolicyBlocksWhenNoSignatureTagResolves(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	seedFixtureImageManifest(t, service, repository)
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	err := service.enforceSigningPolicy(context.Background(), repository, fixtureImageDigest)
	if err == nil {
		t.Fatal("enforceSigningPolicy() error = nil, want a policy violation")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("enforceSigningPolicy() error = %v, want ErrorCodePolicyViolation", err)
	}
}

// TestServiceEnforceSigningPolicyBlocksWhenSignatureManifestHasNoUsableEntry
// is the Phase 4 RED test (tasks.md 4.4): a `.sig` manifest resolves but
// carries no simplesigning layer with a signature annotation ->
// PolicyViolation. (ParseSignatureManifest's own malformed-JSON error path
// is already unit-tested in internal/domain/signing/cosign_test.go; this
// integration test covers the "zero usable entries" shape only, since a
// manifest referencing a missing blob cannot reach the store through the
// ordinary publish path used here.)
func TestServiceEnforceSigningPolicyBlocksWhenSignatureManifestHasNoUsableEntry(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	seedFixtureImageManifest(t, service, repository)
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	tag, err := signing.SignatureTag(fixtureImageDigest)
	if err != nil {
		t.Fatalf("signing.SignatureTag() error = %v", err)
	}
	emptyLayersManifest := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","layers":[]}`)
	if _, err := service.PublishManifest(context.Background(), repository, tag, "application/vnd.oci.image.manifest.v1+json", emptyLayersManifest); err != nil {
		t.Fatalf("PublishManifest(.sig) error = %v", err)
	}

	err = service.enforceSigningPolicy(context.Background(), repository, fixtureImageDigest)
	if err == nil {
		t.Fatal("enforceSigningPolicy() error = nil, want a policy violation")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("enforceSigningPolicy() error = %v, want ErrorCodePolicyViolation", err)
	}
}

// TestServiceEnforceSigningPolicyBlocksWhenPayloadBlobIsAbsent is the Phase
// 4 RED test (tasks.md 4.5): the `.sig` manifest is present and parseable,
// but its payload blob is absent from the blob store -> PolicyViolation.
// Publishing this shape requires bypassing Service.PublishManifest's
// BlobExists check (the ordinary push path refuses a manifest referencing a
// blob that does not exist yet) by writing through the metadata store
// directly, standing in for a blob later becoming unavailable.
func TestServiceEnforceSigningPolicyBlocksWhenPayloadBlobIsAbsent(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	seedFixtureImageManifest(t, service, repository)
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	sigManifestPayload := readSigningFixture(t, "signature-manifest.json")
	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", sigManifestPayload, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("domain.NewManifest(.sig) error = %v", err)
	}
	tag, err := signing.SignatureTag(fixtureImageDigest)
	if err != nil {
		t.Fatalf("signing.SignatureTag() error = %v", err)
	}
	repo, err := parseRepository(repository)
	if err != nil {
		t.Fatalf("parseRepository(%q) error = %v", repository, err)
	}
	if err := service.metadata.PublishManifest(context.Background(), "tenant-a", repo, tag, manifest, manifest.BlobReferences()); err != nil {
		t.Fatalf("metadata.PublishManifest(.sig) error = %v", err)
	}

	err = service.enforceSigningPolicy(context.Background(), repository, fixtureImageDigest)
	if err == nil {
		t.Fatal("enforceSigningPolicy() error = nil, want a policy violation")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("enforceSigningPolicy() error = %v, want ErrorCodePolicyViolation", err)
	}
}

// TestServiceEnforceSigningPolicyBlocksWhenNoTrustedKeyParses is the Phase 4
// RED test (tasks.md 4.6): zero configured keys parse as ECDSA P-256 ->
// PolicyViolation. The unparsable row is hand-seeded directly on the store,
// bypassing the admin-time 400 normalizeSigningOverride/
// UpdateSigningPolicySettings would otherwise raise.
func TestServiceEnforceSigningPolicyBlocksWhenNoTrustedKeyParses(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	seedFixtureImageManifest(t, service, repository)
	seedSigningPolicy(t, service, true, []string{"not a valid PEM key"})

	err := service.enforceSigningPolicy(context.Background(), repository, fixtureImageDigest)
	if err == nil {
		t.Fatal("enforceSigningPolicy() error = nil, want a policy violation")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("enforceSigningPolicy() error = %v, want ErrorCodePolicyViolation", err)
	}
}

// TestServiceEnforceSigningPolicyBlocksWhenNoSignatureValidatesAgainstTrustedKeys
// is the Phase 4 RED test (tasks.md 4.7): every (key, entry) pair fails
// Verify -> PolicyViolation. The trusted key configured here is a freshly
// generated, unrelated ECDSA P-256 key — the fixture's real signature was
// produced by a different (discarded) private key.
func TestServiceEnforceSigningPolicyBlocksWhenNoSignatureValidatesAgainstTrustedKeys(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	seedFixtureImageManifest(t, service, repository)
	seedFixtureSignatureArtifact(t, service, repository)
	seedSigningPolicy(t, service, true, []string{generateTestECDSAP256PublicKeyPEM(t)})

	err := service.enforceSigningPolicy(context.Background(), repository, fixtureImageDigest)
	if err == nil {
		t.Fatal("enforceSigningPolicy() error = nil, want a policy violation")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("enforceSigningPolicy() error = %v, want ErrorCodePolicyViolation", err)
	}
}

// TestServiceEnforceSigningPolicyBlocksTransplantedSignatureBindingADifferentDigest
// is the Phase 4 RED test (tasks.md 4.8): a signature verifies
// cryptographically but CheckClaims binds a different digest ->
// PolicyViolation (the gate's outcome is the same 403-shaped error as every
// other blocked case; Phase 7 distinguishes "mismatched" from "untrusted" at
// the status layer). The exact same fixture `.sig` manifest bytes are
// published at a second, unrelated digest's tag — a signature transplant.
func TestServiceEnforceSigningPolicyBlocksTransplantedSignatureBindingADifferentDigest(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	transplantTargetDigest := seedArbitraryImageManifest(t, service, repository, " - transplant target")
	publishFixtureSignatureManifestAt(t, service, repository, transplantTargetDigest)
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	err := service.enforceSigningPolicy(context.Background(), repository, transplantTargetDigest)
	if err == nil {
		t.Fatal("enforceSigningPolicy() error = nil, want a policy violation")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("enforceSigningPolicy() error = %v, want ErrorCodePolicyViolation", err)
	}
}

// TestServiceEnforceSigningPolicyAllowsPullWhenFixtureSignatureVerifies is
// the Phase 4 RED test (tasks.md 4.9): the Phase 0/1 captured (synthetic,
// explicitly marked per testdata/README.md) fixture's valid signature by a
// trusted key -> nil error, pull allowed, exercised through the service
// layer with the real sqlite store and fsblob store seeded from the fixture
// bytes.
func TestServiceEnforceSigningPolicyAllowsPullWhenFixtureSignatureVerifies(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	seedFixtureImageManifest(t, service, repository)
	seedFixtureSignatureArtifact(t, service, repository)
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	if err := service.enforceSigningPolicy(context.Background(), repository, fixtureImageDigest); err != nil {
		t.Fatalf("enforceSigningPolicy() error = %v, want nil (the fixture signature must verify)", err)
	}
}

// failingResolveManifestStore wraps a real *metadata.Store, failing
// ResolveManifest with a fixed, non-NotFound infrastructure error for one
// specific reference — every other call delegates to the real store
// unchanged.
type failingResolveManifestStore struct {
	*metadata.Store
	failReference string
	err           error
}

func (f *failingResolveManifestStore) ResolveManifest(ctx context.Context, tenant string, repository domain.RepositoryRef, reference string) (domain.Manifest, error) {
	if reference == f.failReference {
		return domain.Manifest{}, f.err
	}
	return f.Store.ResolveManifest(ctx, tenant, repository, reference)
}

// TestServiceEnforceSigningPolicyPropagatesStoreInfrastructureErrorUnchanged
// is the Phase 4 RED test (tasks.md 4.10): a store infrastructure error (not
// NotFound) during `.sig` resolution propagates unchanged, not wrapped as
// PolicyViolation (design.md Decision 6's truth table, last row) — an
// infrastructure fault must never be mislabeled a missing signature.
func TestServiceEnforceSigningPolicyPropagatesStoreInfrastructureErrorUnchanged(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	blobs, err := fsblob.New(filepath.Join(rootDir, "blobs"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}
	realStore, err := metadata.New(filepath.Join(rootDir, "registry.db"))
	if err != nil {
		t.Fatalf("metadata.New() error = %v", err)
	}

	repository := "library/alpine"
	tag, err := signing.SignatureTag(fixtureImageDigest)
	if err != nil {
		t.Fatalf("signing.SignatureTag() error = %v", err)
	}

	infraErr := errors.New("boom: connection reset")
	store := &failingResolveManifestStore{Store: realStore, failReference: tag, err: infraErr}

	service := NewService(blobs, store, allowAllAccessController{}, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	t.Cleanup(func() {
		service.WaitForBackgroundWork()
		_ = realStore.Close()
	})

	seedFixtureImageManifest(t, service, repository)
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	err = service.enforceSigningPolicy(context.Background(), repository, fixtureImageDigest)
	if err == nil {
		t.Fatal("enforceSigningPolicy() error = nil, want the infrastructure error propagated")
	}
	if !errors.Is(err, infraErr) {
		t.Fatalf("enforceSigningPolicy() error = %v, want it to propagate %v unchanged", err, infraErr)
	}
	if domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("enforceSigningPolicy() error = %v, must NOT be reported as a policy violation (design.md Decision 6's last row)", err)
	}
}
