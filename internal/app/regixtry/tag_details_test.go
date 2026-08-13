package regixtry

import (
	"context"
	"testing"

	"regixtry/internal/ports"
)

// TestServiceTagDetailsReturnsCreatedAtAndSignatureStatePerTag is the RED
// test for the console-tags-table change (spec: Tags screen table needs Tag,
// Created, Signed columns): TagDetails returns one entry per tag with its
// manifest's created_at and the exact SignatureStatus state for that tag's
// digest -- reusing Service.SignatureStatus rather than reimplementing
// verification, exactly as the change's scope requires.
//
// This also proves (unchanged, pre-existing behavior, not introduced by this
// change) that a cosign-style "sha256-<hex>.sig" signature-artifact tag is
// itself a real row in the tags table -- exactly like the existing OCI
// `_tags/list` endpoint (Tags()/ListTags), which already returns it
// unfiltered today -- so TagDetails must account for it too rather than
// silently dropping a row ListTags itself does not drop.
func TestServiceTagDetailsReturnsCreatedAtAndSignatureStatePerTag(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	signedDigest := seedFixtureImageManifest(t, service, repository)
	tagManifestAtDigest(t, service, repository, "signed-tag", signedDigest)
	seedFixtureSignatureArtifact(t, service, repository) // also mints its own "sha256-....sig" tag row

	unsignedDigest := seedArbitraryImageManifest(t, service, repository, " - unsigned")
	tagManifestAtDigest(t, service, repository, "unsigned-tag", unsignedDigest)

	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	details, err := service.TagDetails(context.Background(), repository, 10, "")
	if err != nil {
		t.Fatalf("TagDetails() error = %v", err)
	}
	if len(details) != 3 {
		t.Fatalf("len(details) = %d, want 3 (signed-tag, unsigned-tag, and the .sig artifact's own tag row): %#v", len(details), details)
	}

	byName := make(map[string]TagDetails, len(details))
	for _, detail := range details {
		byName[detail.Name] = detail
	}

	signed, ok := byName["signed-tag"]
	if !ok {
		t.Fatalf("details = %#v, want an entry for signed-tag", details)
	}
	if signed.SignatureState != SignatureStatusVerified {
		t.Fatalf("signed-tag.SignatureState = %q, want %q", signed.SignatureState, SignatureStatusVerified)
	}
	if !signed.SigningEnabled {
		t.Fatal("signed-tag.SigningEnabled = false, want true (policy enabled)")
	}
	if signed.CreatedAt.IsZero() {
		t.Fatal("signed-tag.CreatedAt is zero, want the manifest's push time")
	}

	unsigned, ok := byName["unsigned-tag"]
	if !ok {
		t.Fatalf("details = %#v, want an entry for unsigned-tag", details)
	}
	if unsigned.SignatureState != SignatureStatusUnsigned {
		t.Fatalf("unsigned-tag.SignatureState = %q, want %q", unsigned.SignatureState, SignatureStatusUnsigned)
	}
	if !unsigned.SigningEnabled {
		t.Fatal("unsigned-tag.SigningEnabled = false, want true (policy enabled)")
	}
}

// TestServiceTagDetailsSigningEnabledMirrorsPolicyEnabledBit proves
// SigningEnabled is exactly SignatureStatus's own Policy.Enabled bit (design
// decision: the TUI Signed column collapses to "n/a" when the whole signing
// policy is disabled) -- computed even though no signature was ever
// published for this tag.
func TestServiceTagDetailsSigningEnabledMirrorsPolicyEnabledBit(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedFixtureImageManifest(t, service, repository)
	tagManifestAtDigest(t, service, repository, "latest", digest)
	seedSigningPolicy(t, service, false, nil)

	details, err := service.TagDetails(context.Background(), repository, 10, "")
	if err != nil {
		t.Fatalf("TagDetails() error = %v", err)
	}
	if len(details) != 1 {
		t.Fatalf("len(details) = %d, want 1: %#v", len(details), details)
	}
	if details[0].SigningEnabled {
		t.Fatal("details[0].SigningEnabled = true, want false (policy disabled)")
	}
}

// TestServiceTagDetailsRequiresAuthorization mirrors
// TestServiceSignatureStatusRequiresPullAuthorization: TagDetails must
// authorize before returning any tag data.
func TestServiceTagDetailsRequiresAuthorization(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, ports.NewConfigurableAccessController(ports.AccessConfig{}))
	defer cleanup()

	_, err := service.TagDetails(context.Background(), "library/alpine", 10, "")
	if err == nil {
		t.Fatal("TagDetails() error = nil, want an authorization error when access is denied")
	}
}

// tagManifestAtDigest publishes an already-seeded digest under an explicit
// tag directly through the metadata store, mirroring
// seedArbitraryImageManifest's own bypass of Service.PublishManifest's JSON-
// envelope parsing -- seedArbitraryImageManifest/seedFixtureImageManifest
// publish with an empty tag, so TagDetails (which starts from the tags
// table) needs a real tag row to find.
func tagManifestAtDigest(t *testing.T, service *Service, repository string, tag string, digest string) {
	t.Helper()

	repo, err := parseRepository(repository)
	if err != nil {
		t.Fatalf("parseRepository(%q) error = %v", repository, err)
	}

	resolved, err := service.metadata.ResolveManifest(context.Background(), "tenant-a", repo, digest)
	if err != nil {
		t.Fatalf("metadata.ResolveManifest(%q) error = %v", digest, err)
	}

	if err := service.metadata.PublishManifest(context.Background(), "tenant-a", repo, tag, resolved, resolved.References()); err != nil {
		t.Fatalf("metadata.PublishManifest(tag=%q) error = %v", tag, err)
	}
}
