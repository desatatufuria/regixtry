package regixtry

import (
	"context"
	"errors"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/domain/signing"
	"regixtry/internal/ports"
)

// TestServiceTagDetailsReturnsCreatedAtAndSignatureStatePerTag is the RED
// test for the console-tags-table change (spec: Tags screen table needs Tag,
// Created, Signed columns): TagDetails returns one entry per tag with its
// manifest's created_at and the exact SignatureStatus state for that tag's
// digest -- reusing Service.SignatureStatus rather than reimplementing
// verification, exactly as the change's scope requires.
//
// It also seeds the fixture's own cosign-style "sha256-<hex>.sig"
// signature-artifact tag alongside the two real tags, and asserts it is
// EXCLUDED from the result (the Console-tags-.sig-filter follow-up): unlike
// the OCI Distribution API's `_tags/list` endpoint (Tags()/ListTags), which
// must keep returning it unfiltered for docker/cosign/skopeo clients, the
// human-facing Console Tags table must not show a signature's own accessory
// artifact as a browsable sibling row.
func TestServiceTagDetailsReturnsCreatedAtAndSignatureStatePerTag(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	signedDigest := seedFixtureImageManifest(t, service, repository)
	tagManifestAtDigest(t, service, repository, "signed-tag", signedDigest)
	seedFixtureSignatureArtifact(t, service, repository) // also mints its own "sha256-....sig" tag row, expected to be filtered out below

	unsignedDigest := seedArbitraryImageManifest(t, service, repository, " - unsigned")
	tagManifestAtDigest(t, service, repository, "unsigned-tag", unsignedDigest)

	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	details, err := service.TagDetails(context.Background(), repository, 10, "")
	if err != nil {
		t.Fatalf("TagDetails() error = %v", err)
	}
	if len(details) != 2 {
		t.Fatalf("len(details) = %d, want 2 (signed-tag and unsigned-tag; the .sig artifact's own tag row must be excluded): %#v", len(details), details)
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

// TestServiceTagDetailsExcludesCosignSignatureArtifactTags is the RED test
// for the Console Tags screen's `.sig`-tag filter: a tag matching cosign's
// legacy signature-tag format ("sha256-<hex>.sig", produced by
// signing.SignatureTag) is an accessory artifact of the image it signs, not
// a version a Console user browses/pulls -- its own "Signed" status is
// nonsensical, since a signature is not itself signed. TagDetails must
// exclude it from the human-facing Console Tags table it powers. This is
// scoped to TagDetails only -- the OCI Distribution API's `_tags/list`
// endpoint (Tags()/ListTags) is untouched and must keep returning it.
func TestServiceTagDetailsExcludesCosignSignatureArtifactTags(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	signedDigest := seedFixtureImageManifest(t, service, repository)
	tagManifestAtDigest(t, service, repository, "signed-tag", signedDigest)
	seedFixtureSignatureArtifact(t, service, repository) // mints its own "sha256-<hex>.sig" tag row
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	sigTag, err := signing.SignatureTag(signedDigest)
	if err != nil {
		t.Fatalf("signing.SignatureTag(%q) error = %v", signedDigest, err)
	}

	details, err := service.TagDetails(context.Background(), repository, 10, "")
	if err != nil {
		t.Fatalf("TagDetails() error = %v", err)
	}

	for _, detail := range details {
		if detail.Name == sigTag {
			t.Fatalf("details = %#v, want no entry for the cosign signature-artifact tag %q", details, sigTag)
		}
	}
	if len(details) != 1 {
		t.Fatalf("len(details) = %d, want 1 (only signed-tag, the .sig artifact tag excluded): %#v", len(details), details)
	}
	if details[0].Name != "signed-tag" {
		t.Fatalf("details[0].Name = %q, want %q", details[0].Name, "signed-tag")
	}
}

// TestServiceTagDetailsExcludesCosignSignatureArtifactTagsEvenWhenNoRealTagExists
// proves the filter operates on tag-name shape alone, not "there happens to
// be another real tag in the result" -- a repository containing only a
// `.sig` artifact tag (no image tag ever pointed at it in this test) must
// come back empty, not with the `.sig` tag itself as a lone row.
func TestServiceTagDetailsExcludesCosignSignatureArtifactTagsEvenWhenNoRealTagExists(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	seedFixtureImageManifest(t, service, repository)
	seedFixtureSignatureArtifact(t, service, repository) // mints only "sha256-<hex>.sig", no other tag
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	details, err := service.TagDetails(context.Background(), repository, 10, "")
	if err != nil {
		t.Fatalf("TagDetails() error = %v", err)
	}
	if len(details) != 0 {
		t.Fatalf("len(details) = %d, want 0 (the only tag present is the .sig artifact, which must be excluded): %#v", len(details), details)
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

// fakeUsernameResolver is a minimal UsernameResolver test double: known maps
// a UserID to the username it should resolve to; a UserID absent from known
// mirrors the real adapter's "not found" contract -- ("", nil), never an
// error -- unless forceErr is set, simulating a genuine infra failure.
type fakeUsernameResolver struct {
	known    map[string]string
	forceErr error
	calls    []string
}

func (f *fakeUsernameResolver) ResolveUsername(_ context.Context, userID string) (string, error) {
	f.calls = append(f.calls, userID)
	if f.forceErr != nil {
		return "", f.forceErr
	}
	return f.known[userID], nil
}

// TestServiceTagDetailsResolvesPushedByUsername is the RED test for the
// console-tags-pushed-by change: TagDetails.PushedBy carries the resolved
// username (via the optional UsernameResolver), not the raw UserID stored
// on the manifest.
func TestServiceTagDetailsResolvesPushedByUsername(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	ctx := ports.ContextWithPrincipal(context.Background(), domainauth.Principal{UserID: "user-abc-123"})
	if _, err := service.PublishManifest(ctx, repository, "known-pusher", "application/vnd.oci.image.manifest.v1+json", []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","layers":[]}`)); err != nil {
		t.Fatalf("PublishManifest(known-pusher) error = %v", err)
	}

	resolver := &fakeUsernameResolver{known: map[string]string{"user-abc-123": "operator"}}
	service.SetUsernameResolver(resolver)

	details, err := service.TagDetails(context.Background(), repository, 10, "")
	if err != nil {
		t.Fatalf("TagDetails() error = %v", err)
	}
	if len(details) != 1 {
		t.Fatalf("len(details) = %d, want 1: %#v", len(details), details)
	}
	if got, want := details[0].PushedBy, "operator"; got != want {
		t.Fatalf("PushedBy = %q, want %q (resolved username, not the raw UserID)", got, want)
	}
	if len(resolver.calls) != 1 || resolver.calls[0] != "user-abc-123" {
		t.Fatalf("resolver.calls = %#v, want one call with the raw UserID", resolver.calls)
	}
}

// TestServiceTagDetailsPushedByEmptyWithoutResolverOrUnknownUser covers two
// degrade-gracefully cases: no resolver configured at all (embedded/no-auth
// mode), and a resolver that legitimately has no username for this UserID
// (e.g. the user was since deleted) -- neither is an error, both leave
// PushedBy "".
func TestServiceTagDetailsPushedByEmptyWithoutResolverOrUnknownUser(t *testing.T) {
	t.Parallel()

	repository := "library/alpine"
	publish := func(t *testing.T, service *Service) {
		t.Helper()
		ctx := ports.ContextWithPrincipal(context.Background(), domainauth.Principal{UserID: "user-ghost"})
		if _, err := service.PublishManifest(ctx, repository, "tag", "application/vnd.oci.image.manifest.v1+json", []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","layers":[]}`)); err != nil {
			t.Fatalf("PublishManifest() error = %v", err)
		}
	}

	t.Run("no resolver configured", func(t *testing.T) {
		t.Parallel()
		service, cleanup := newTestService(t, allowAllAccessController{})
		defer cleanup()
		publish(t, service)

		details, err := service.TagDetails(context.Background(), repository, 10, "")
		if err != nil {
			t.Fatalf("TagDetails() error = %v", err)
		}
		if len(details) != 1 || details[0].PushedBy != "" {
			t.Fatalf("details = %#v, want PushedBy \"\" with no resolver configured", details)
		}
	})

	t.Run("resolver has no username for this user", func(t *testing.T) {
		t.Parallel()
		service, cleanup := newTestService(t, allowAllAccessController{})
		defer cleanup()
		publish(t, service)
		service.SetUsernameResolver(&fakeUsernameResolver{known: map[string]string{}})

		details, err := service.TagDetails(context.Background(), repository, 10, "")
		if err != nil {
			t.Fatalf("TagDetails() error = %v", err)
		}
		if len(details) != 1 || details[0].PushedBy != "" {
			t.Fatalf("details = %#v, want PushedBy \"\" when the resolver has no username for this user", details)
		}
	})
}

// TestServiceTagDetailsPropagatesUsernameResolverError proves a genuine
// resolver failure (e.g. the auth store is unreachable) fails the whole
// TagDetails call, mirroring this method's existing SignatureStatus
// hard-fail policy -- not silently degraded.
func TestServiceTagDetailsPropagatesUsernameResolverError(t *testing.T) {
	t.Parallel()

	repository := "library/alpine"
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	ctx := ports.ContextWithPrincipal(context.Background(), domainauth.Principal{UserID: "user-abc-123"})
	if _, err := service.PublishManifest(ctx, repository, "tag", "application/vnd.oci.image.manifest.v1+json", []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","layers":[]}`)); err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}
	service.SetUsernameResolver(&fakeUsernameResolver{forceErr: errors.New("auth store unreachable")})

	if _, err := service.TagDetails(context.Background(), repository, 10, ""); err == nil {
		t.Fatal("TagDetails() error = nil, want the resolver's infra error propagated")
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

	if err := service.metadata.PublishManifest(context.Background(), "tenant-a", repo, tag, resolved, resolved.BlobReferences()); err != nil {
		t.Fatalf("metadata.PublishManifest(tag=%q) error = %v", tag, err)
	}
}
