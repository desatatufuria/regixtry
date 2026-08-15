package regixtry

import (
	"context"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// This file is the exhaustive scenario matrix for the opt-in
// SigningPolicySettings.UnsignedSelfRead / SigningOverride.UnsignedSelfRead
// knob: a fail-closed-gate exemption letting a principal read back a
// manifest it cannot yet prove is signed, solving the cosign bootstrap
// chicken-and-egg (cosign must GET the manifest to know what to sign, but
// that GET is itself blocked by enforceSigningPolicy before the image is
// signed). Compensates for this being a direct feature-branch change with no
// openspec/ proposal-spec-design-tasks ceremony, per explicit agreement.

// writerPrincipal builds a domainauth.Principal with the given UserID and
// full pull+push grant/scope on repository -- HasWriteAccess(repository)
// true, mirroring principalForGrants (service_test.go) plus the UserID this
// file's tests need for the "pusher" exemption mode.
func writerPrincipal(userID, repository string) domainauth.Principal {
	p := principalForGrants(repository, domainauth.RepoRoleWriter, []domainauth.Scope{
		{Type: "repository", Name: repository, Actions: []string{"pull", "push"}, Canonical: "repository:" + repository + ":pull,push"},
	})
	p.UserID = userID
	return p
}

// readerPrincipal is writerPrincipal's pull-only counterpart --
// HasWriteAccess(repository) false, HasReadAccess(repository) true.
func readerPrincipal(userID, repository string) domainauth.Principal {
	p := principalForGrants(repository, domainauth.RepoRoleReader, []domainauth.Scope{
		{Type: "repository", Name: repository, Actions: []string{"pull"}, Canonical: "repository:" + repository + ":pull"},
	})
	p.UserID = userID
	return p
}

// publishUnsignedManifest publishes a minimal, entirely unsigned OCI image
// manifest (no config/layers, so no blob-existence check is in play, mirrors
// signing_override_direction_test.go's unsignedManifestPayload) through the
// real Service.PublishManifest -- unlike seedArbitraryImageManifest/
// seedFixtureImageManifest, which write through the metadata store directly
// and so never populate PushedBy. tag both names the pushed tag and varies
// the payload bytes (and so the content-addressed digest) between calls.
func publishUnsignedManifest(t *testing.T, service *Service, ctx context.Context, repository, tag string) string {
	t.Helper()

	payload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","layers":[],"annotations":{"unsigned-self-read-test-tag":"` + tag + `"}}`)
	details, err := service.PublishManifest(ctx, repository, tag, "application/vnd.oci.image.manifest.v1+json", payload)
	if err != nil {
		t.Fatalf("PublishManifest(%q) error = %v", tag, err)
	}
	return details.Digest
}

// seedSigningPolicyWithSelfRead is seedSigningPolicy (service_signing_test.go)
// plus a chosen UnsignedSelfRead mode, always enabled with the fixture's
// trusted key so enforceSigningPolicy actually reaches verifySignature (and
// so the exemption branch) rather than short-circuiting on !policy.Enabled.
func seedSigningPolicyWithSelfRead(t *testing.T, service *Service, mode string) {
	t.Helper()

	if err := service.metadata.UpsertSigningPolicySettings(context.Background(), "tenant-a", ports.SigningPolicySettings{
		Enabled:           true,
		TrustedPublicKeys: []string{fixtureTrustedKeyPEM(t)},
		UnsignedSelfRead:  mode,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertSigningPolicySettings() error = %v", err)
	}
}

// TestServiceOpenManifestUnsignedSelfReadOffStillBlocksExactPusher is the
// most important test in this file (scenario 1 of the agreed matrix): with
// UnsignedSelfRead left at its zero value ("" == "off"), the exact principal
// who pushed an unsigned manifest is STILL blocked reading it back --
// proving the default is byte-for-byte unchanged from today's production
// behavior. A bug that let "off" behave like "repo_push" (e.g. an
// accidentally inverted or missing switch case) would make this pull
// succeed instead of returning a PolicyViolation; this test would then fail
// with "OpenManifest() error = nil, want the exact pusher STILL blocked".
func TestServiceOpenManifestUnsignedSelfReadOffStillBlocksExactPusher(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	pusherCtx := ports.ContextWithPrincipal(context.Background(), writerPrincipal("user-pusher", repository))

	digest := publishUnsignedManifest(t, service, pusherCtx, repository, "off-default")
	// UnsignedSelfRead is deliberately left unset ("") on this policy --
	// the default this test locks in.
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	_, err := service.OpenManifest(pusherCtx, repository, digest)
	if err == nil {
		t.Fatal("OpenManifest() error = nil, want the exact pusher STILL blocked when UnsignedSelfRead is \"\" (off) -- the default must never silently become permissive")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("OpenManifest() error = %v, want ErrorCodePolicyViolation", err)
	}
}

// TestServiceOpenManifestUnsignedSelfReadPusherAllowsExactPusher is scenario
// 2: "pusher" mode lets the exact pusher (matching UserID) read back their
// own unsigned push.
func TestServiceOpenManifestUnsignedSelfReadPusherAllowsExactPusher(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	pusherCtx := ports.ContextWithPrincipal(context.Background(), writerPrincipal("user-pusher", repository))

	digest := publishUnsignedManifest(t, service, pusherCtx, repository, "pusher-self")
	seedSigningPolicyWithSelfRead(t, service, "pusher")

	if _, err := service.OpenManifest(pusherCtx, repository, digest); err != nil {
		t.Fatalf("OpenManifest() error = %v, want nil: pusher mode must let the exact pusher read back their own unsigned push", err)
	}
}

// TestServiceOpenManifestUnsignedSelfReadPusherBlocksDifferentPrincipalWithPushAccess
// is scenario 3, the entire reason "pusher" exists over "repo_push": a
// DIFFERENT principal with push access to the SAME repository must NOT be
// able to read back another principal's unsigned push.
func TestServiceOpenManifestUnsignedSelfReadPusherBlocksDifferentPrincipalWithPushAccess(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	pusherCtx := ports.ContextWithPrincipal(context.Background(), writerPrincipal("user-pusher", repository))
	otherWriterCtx := ports.ContextWithPrincipal(context.Background(), writerPrincipal("user-other-writer", repository))

	digest := publishUnsignedManifest(t, service, pusherCtx, repository, "pusher-vs-other-writer")
	seedSigningPolicyWithSelfRead(t, service, "pusher")

	_, err := service.OpenManifest(otherWriterCtx, repository, digest)
	if err == nil {
		t.Fatal("OpenManifest() error = nil, want a policy violation: pusher mode must not exempt a different principal even with push access to the same repository")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("OpenManifest() error = %v, want ErrorCodePolicyViolation", err)
	}
}

// TestServiceOpenManifestUnsignedSelfReadPusherBlocksPullOnlyPrincipal is
// scenario 4: a principal with only pull access (no push at all) is still
// blocked under "pusher" mode.
func TestServiceOpenManifestUnsignedSelfReadPusherBlocksPullOnlyPrincipal(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	pusherCtx := ports.ContextWithPrincipal(context.Background(), writerPrincipal("user-pusher", repository))
	readerCtx := ports.ContextWithPrincipal(context.Background(), readerPrincipal("user-reader", repository))

	digest := publishUnsignedManifest(t, service, pusherCtx, repository, "pusher-vs-reader")
	seedSigningPolicyWithSelfRead(t, service, "pusher")

	_, err := service.OpenManifest(readerCtx, repository, digest)
	if err == nil {
		t.Fatal("OpenManifest() error = nil, want a policy violation: a pull-only principal must remain blocked under pusher mode")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("OpenManifest() error = %v, want ErrorCodePolicyViolation", err)
	}
}

// TestServiceOpenManifestUnsignedSelfReadPusherNeverMatchesEmptyPushedBy is
// scenario 5: a manifest with empty pushed_by (simulating a pre-migration
// legacy row, published directly through the metadata store the way
// seedFixtureImageManifest does, bypassing Service.PublishManifest) is never
// readable by anyone via this exemption -- including the edge case where the
// requesting principal's own UserID also happens to be empty, which must not
// accidentally match empty-to-empty.
func TestServiceOpenManifestUnsignedSelfReadPusherNeverMatchesEmptyPushedBy(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedFixtureImageManifest(t, service, repository)
	seedSigningPolicyWithSelfRead(t, service, "pusher")

	emptyUserIDCtx := ports.ContextWithPrincipal(context.Background(), writerPrincipal("", repository))
	if _, err := service.OpenManifest(emptyUserIDCtx, repository, digest); err == nil {
		t.Fatal("OpenManifest() error = nil, want a policy violation: an empty pushed_by must never be exempt-readable, even by a principal whose own UserID is also empty")
	} else if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("OpenManifest() error = %v, want ErrorCodePolicyViolation", err)
	}

	otherCtx := ports.ContextWithPrincipal(context.Background(), writerPrincipal("user-someone", repository))
	if _, err := service.OpenManifest(otherCtx, repository, digest); err == nil {
		t.Fatal("OpenManifest() error = nil, want a policy violation for a legacy zero-pushed_by manifest")
	} else if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("OpenManifest() error = %v, want ErrorCodePolicyViolation", err)
	}
}

// TestServiceOpenManifestUnsignedSelfReadRepoPushAllowsAnyPrincipalWithPushAccess
// is scenario 6: "repo_push" mode exempts ANY principal with push access to
// the repository, not just the original pusher.
func TestServiceOpenManifestUnsignedSelfReadRepoPushAllowsAnyPrincipalWithPushAccess(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	pusherCtx := ports.ContextWithPrincipal(context.Background(), writerPrincipal("user-pusher", repository))
	otherWriterCtx := ports.ContextWithPrincipal(context.Background(), writerPrincipal("user-other-writer", repository))

	digest := publishUnsignedManifest(t, service, pusherCtx, repository, "repo-push-any-writer")
	seedSigningPolicyWithSelfRead(t, service, "repo_push")

	if _, err := service.OpenManifest(otherWriterCtx, repository, digest); err != nil {
		t.Fatalf("OpenManifest() error = %v, want nil: repo_push mode must exempt any principal with push access, not just the original pusher", err)
	}
}

// TestServiceOpenManifestUnsignedSelfReadRepoPushBlocksPullOnlyPrincipal is
// scenario 7: a principal with only pull access is still blocked under
// "repo_push" mode.
func TestServiceOpenManifestUnsignedSelfReadRepoPushBlocksPullOnlyPrincipal(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	pusherCtx := ports.ContextWithPrincipal(context.Background(), writerPrincipal("user-pusher", repository))
	readerCtx := ports.ContextWithPrincipal(context.Background(), readerPrincipal("user-reader", repository))

	digest := publishUnsignedManifest(t, service, pusherCtx, repository, "repo-push-vs-reader")
	seedSigningPolicyWithSelfRead(t, service, "repo_push")

	_, err := service.OpenManifest(readerCtx, repository, digest)
	if err == nil {
		t.Fatal("OpenManifest() error = nil, want a policy violation: repo_push mode must still block a pull-only principal")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("OpenManifest() error = %v, want ErrorCodePolicyViolation", err)
	}
}

// TestServiceOpenManifestUnsignedSelfReadIrrelevantOnceSigned is scenario 8:
// once a manifest IS successfully signed, normal verified-pull behavior
// applies to any pull-authorized principal regardless of UnsignedSelfRead
// mode -- the exemption branch inside enforceSigningPolicy is simply never
// reached because verifySignature already succeeds.
func TestServiceOpenManifestUnsignedSelfReadIrrelevantOnceSigned(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	seedFixtureImageManifest(t, service, repository)
	seedFixtureSignatureArtifact(t, service, repository)
	// UnsignedSelfRead is set to "pusher", but a signed digest never reaches
	// the exemption branch, so this must have no effect either way.
	seedSigningPolicyWithSelfRead(t, service, "pusher")

	pullCtx := ports.ContextWithPrincipal(context.Background(), readerPrincipal("user-anyone", repository))
	if _, err := service.OpenManifest(pullCtx, repository, fixtureImageDigest); err != nil {
		t.Fatalf("OpenManifest() error = %v, want nil: a properly signed digest pulls normally regardless of UnsignedSelfRead mode", err)
	}
}

// TestServiceOpenManifestUnsignedSelfReadRepositoryOverridePrecedence is
// scenario 9, both directions of full-row-replace precedence, mirroring how
// Enabled/TrustedPublicKeys overrides already behave
// (signing_override_direction_test.go).
func TestServiceOpenManifestUnsignedSelfReadRepositoryOverridePrecedence(t *testing.T) {
	t.Parallel()

	t.Run("repo override pusher applies despite global off", func(t *testing.T) {
		t.Parallel()

		service, cleanup := newTestService(t, allowAllAccessController{})
		defer cleanup()

		repository := "library/alpine"
		pusherCtx := ports.ContextWithPrincipal(context.Background(), writerPrincipal("user-pusher", repository))
		digest := publishUnsignedManifest(t, service, pusherCtx, repository, "override-pusher")

		// Global policy enabled, UnsignedSelfRead left at its "off" default.
		seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

		overrideBody := marshalOverride(t, map[string]any{
			"enabled":             true,
			"trusted_public_keys": []string{fixtureTrustedKeyPEM(t)},
			"unsigned_self_read":  "pusher",
		})
		if _, err := service.SetRepositoryOverride(context.Background(), repository, signingFeatureName, overrideBody); err != nil {
			t.Fatalf("SetRepositoryOverride() error = %v", err)
		}

		if _, err := service.OpenManifest(pusherCtx, repository, digest); err != nil {
			t.Fatalf("OpenManifest() error = %v, want nil: the repository override's pusher mode must apply despite the global policy being off", err)
		}
	})

	t.Run("repo override off forces off despite global pusher", func(t *testing.T) {
		t.Parallel()

		service, cleanup := newTestService(t, allowAllAccessController{})
		defer cleanup()

		repository := "library/alpine"
		pusherCtx := ports.ContextWithPrincipal(context.Background(), writerPrincipal("user-pusher", repository))
		digest := publishUnsignedManifest(t, service, pusherCtx, repository, "override-off")

		// Global policy enabled with UnsignedSelfRead: "pusher".
		seedSigningPolicyWithSelfRead(t, service, "pusher")

		overrideBody := marshalOverride(t, map[string]any{
			"enabled":             true,
			"trusted_public_keys": []string{fixtureTrustedKeyPEM(t)},
			"unsigned_self_read":  "off",
		})
		if _, err := service.SetRepositoryOverride(context.Background(), repository, signingFeatureName, overrideBody); err != nil {
			t.Fatalf("SetRepositoryOverride() error = %v", err)
		}

		_, err := service.OpenManifest(pusherCtx, repository, digest)
		if err == nil {
			t.Fatal("OpenManifest() error = nil, want a policy violation: the repository override forces off despite the global policy allowing pusher self-read")
		}
		if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
			t.Fatalf("OpenManifest() error = %v, want ErrorCodePolicyViolation", err)
		}
	})
}

// TestServicePublishManifestPersistsPushedByAndUpdatesOnRepush is scenario
// 11: Service.PublishManifest persists PushedBy from the pushing principal's
// UserID, and re-pushing the identical digest (content-addressed, so
// re-push means byte-identical content) under a DIFFERENT principal's
// credentials updates pushed_by to the new pusher -- confirming the
// ON CONFLICT ... DO UPDATE SET pushed_by = excluded.pushed_by store
// behavior.
func TestServicePublishManifestPersistsPushedByAndUpdatesOnRepush(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	repo, err := parseRepository(repository)
	if err != nil {
		t.Fatalf("parseRepository(%q) error = %v", repository, err)
	}

	payload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","layers":[]}`)

	firstPusherCtx := ports.ContextWithPrincipal(context.Background(), writerPrincipal("user-first-pusher", repository))
	details, err := service.PublishManifest(firstPusherCtx, repository, "repush-test", "application/vnd.oci.image.manifest.v1+json", payload)
	if err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}

	stored, err := service.metadata.ResolveManifest(context.Background(), "tenant-a", repo, details.Digest)
	if err != nil {
		t.Fatalf("metadata.ResolveManifest() error = %v", err)
	}
	if stored.PushedBy != "user-first-pusher" {
		t.Fatalf("stored.PushedBy = %q, want %q after the first push", stored.PushedBy, "user-first-pusher")
	}

	secondPusherCtx := ports.ContextWithPrincipal(context.Background(), writerPrincipal("user-second-pusher", repository))
	// Re-push the identical bytes (content-addressed -> identical digest)
	// under a different principal's credentials.
	if _, err := service.PublishManifest(secondPusherCtx, repository, "repush-test", "application/vnd.oci.image.manifest.v1+json", payload); err != nil {
		t.Fatalf("PublishManifest() re-push error = %v", err)
	}

	restored, err := service.metadata.ResolveManifest(context.Background(), "tenant-a", repo, details.Digest)
	if err != nil {
		t.Fatalf("metadata.ResolveManifest() error = %v after re-push", err)
	}
	if restored.PushedBy != "user-second-pusher" {
		t.Fatalf("stored.PushedBy = %q, want %q after re-pushing the identical digest under a different principal", restored.PushedBy, "user-second-pusher")
	}
}
