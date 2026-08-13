package regixtry

import (
	"context"
	"testing"

	domain "regixtry/internal/domain/regixtry"
)

// This file is the Phase 10 cross-cutting proof (tasks.md 10.2/10.3) that a
// repository-level signing override actually changes the OpenManifest
// pull-gate outcome in BOTH directions, exercised through the real service
// (SetRepositoryOverride -> enforceSigningPolicy -> OpenManifest), not just
// resolveRepositoryOverride/applySigningOverridePayload in isolation
// (already covered at the resolution layer by
// TestApplySigningOverridePayloadAppliesRoundTripAndTolerance and
// TestResolveRepositoryOverride*ReturnsSettingsUnchanged in
// repository_overrides_test.go). Neither direction is exercised end-to-end
// at the OpenManifest level anywhere else in the suite -- confirmed by
// grepping every SetRepositoryOverride call site before writing these.

// TestServiceOpenManifestSigningOverrideDirectionRequiresSigningWhenGlobalDoesNot
// is the Phase 10 RED/GREEN test (tasks.md 10.2, direction 1): the global
// signing policy is left at its default {Enabled: false} (so an unsigned
// pull would ordinarily succeed, proven here as a same-test sanity check on
// a sibling repository with no override), but a repository override sets
// {Enabled: true} with a usable trusted key -- the override-scoped
// repository must now block an unsigned pull of the very same digest.
func TestServiceOpenManifestSigningOverrideDirectionRequiresSigningWhenGlobalDoesNot(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	overriddenRepository := "library/alpine"
	controlRepository := "library/busybox"

	seedFixtureImageManifest(t, service, overriddenRepository)
	seedFixtureImageManifest(t, service, controlRepository)
	// Global signing policy left at its zero-value default: {Enabled: false}.

	// Sanity check: with no override and the global policy disabled, the
	// same unsigned digest pulls cleanly on the control repository -- the
	// baseline this test's override must visibly change.
	if _, err := service.OpenManifest(context.Background(), controlRepository, fixtureImageDigest); err != nil {
		t.Fatalf("OpenManifest(control) error = %v, want nil (global signing policy is disabled)", err)
	}

	overrideBody := marshalOverride(t, map[string]any{
		"enabled":             true,
		"trusted_public_keys": []string{fixtureTrustedKeyPEM(t)},
	})
	if _, err := service.SetRepositoryOverride(context.Background(), overriddenRepository, signingFeatureName, overrideBody); err != nil {
		t.Fatalf("SetRepositoryOverride() error = %v", err)
	}

	_, err := service.OpenManifest(context.Background(), overriddenRepository, fixtureImageDigest)
	if err == nil {
		t.Fatal("OpenManifest(overridden) error = nil, want a policy violation: the repository override requires signing even though the global policy does not")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("OpenManifest(overridden) error = %v, want ErrorCodePolicyViolation", err)
	}
}

// TestServiceOpenManifestSigningOverrideDirectionExemptsWhenGlobalRequiresSigning
// is the Phase 10 RED/GREEN test (tasks.md 10.3, direction 2): the global
// signing policy is enabled with a trusted key (so an unsigned pull would
// ordinarily be blocked, proven here as a same-test sanity check on a
// sibling repository with no override), but a repository override sets
// {Enabled: false} -- the override-scoped repository must allow the very
// same unsigned digest to pull.
func TestServiceOpenManifestSigningOverrideDirectionExemptsWhenGlobalRequiresSigning(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	exemptedRepository := "library/alpine"
	controlRepository := "library/busybox"

	seedFixtureImageManifest(t, service, exemptedRepository)
	seedFixtureImageManifest(t, service, controlRepository)
	// Global signing policy enabled with a usable trusted key; neither
	// repository has published a signature artifact for this digest.
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	// Sanity check: with no override, the global policy blocks the same
	// unsigned digest on the control repository -- the baseline this test's
	// override must visibly change.
	if _, err := service.OpenManifest(context.Background(), controlRepository, fixtureImageDigest); !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("OpenManifest(control) error = %v, want ErrorCodePolicyViolation (global signing policy is enabled)", err)
	}

	overrideBody := marshalOverride(t, map[string]any{"enabled": false})
	if _, err := service.SetRepositoryOverride(context.Background(), exemptedRepository, signingFeatureName, overrideBody); err != nil {
		t.Fatalf("SetRepositoryOverride() error = %v", err)
	}

	if _, err := service.OpenManifest(context.Background(), exemptedRepository, fixtureImageDigest); err != nil {
		t.Fatalf("OpenManifest(exempted) error = %v, want nil: the repository override exempts this repository even though the global policy requires signing", err)
	}
}

// TestServiceOpenManifestSigningOverrideClearRevertsToGlobalPolicy is the
// end-to-end OpenManifest-level proof of
// image-signature-verification/spec.md's "Clearing the override reverts to
// global policy" scenario, for the signing feature specifically. Found
// missing while building the verify-report's compliance matrix: the only
// existing Clear coverage
// (TestServiceClearRepositoryOverrideRevertsResolutionToGlobalImmediately,
// repository_overrides_test.go) exercises Trivy only, at the
// applyRepositoryOverride resolution layer, never OpenManifest; and
// TestResolveRepositoryOverrideNotFoundReturnsSettingsUnchanged's
// T=ports.SigningPolicySettings subtest proves the NotFound branch for
// signing's type but never through a genuine Set-then-Clear sequence. This
// test closes that gap directly: a repository override requires signing
// while the global policy does not (mirroring the direction-1 test above),
// then ClearRepositoryOverride is called, and the very next OpenManifest
// pull of the same unsigned digest must revert to the global (disabled)
// policy's outcome.
func TestServiceOpenManifestSigningOverrideClearRevertsToGlobalPolicy(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	seedFixtureImageManifest(t, service, repository)
	// Global signing policy left at its zero-value default: {Enabled: false}.

	overrideBody := marshalOverride(t, map[string]any{
		"enabled":             true,
		"trusted_public_keys": []string{fixtureTrustedKeyPEM(t)},
	})
	if _, err := service.SetRepositoryOverride(context.Background(), repository, signingFeatureName, overrideBody); err != nil {
		t.Fatalf("SetRepositoryOverride() error = %v", err)
	}

	if _, err := service.OpenManifest(context.Background(), repository, fixtureImageDigest); !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("OpenManifest() before Clear error = %v, want ErrorCodePolicyViolation (override requires signing, sanity check the override took effect)", err)
	}

	if err := service.ClearRepositoryOverride(context.Background(), repository, signingFeatureName); err != nil {
		t.Fatalf("ClearRepositoryOverride() error = %v", err)
	}

	if _, err := service.OpenManifest(context.Background(), repository, fixtureImageDigest); err != nil {
		t.Fatalf("OpenManifest() after Clear error = %v, want nil: clearing the override must revert this repository to the disabled global policy", err)
	}
}

// TestServicePublishManifestSucceedsForUnsignedImageEvenWithSigningPolicyEnabled
// is the end-to-end proof of image-signature-verification/spec.md's
// "Pushing an unsigned image remains legal" scenario. Found missing while
// building the verify-report's compliance matrix: enforceSigningPolicy is
// called from exactly one call site (OpenManifest, confirmed by reading
// service.go's PublishManifest, which has zero references to signing at
// all), but no existing test actually pushes while the signing policy is
// enabled to prove this behaviorally rather than only by code inspection.
func TestServicePublishManifestSucceedsForUnsignedImageEvenWithSigningPolicyEnabled(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	// PublishManifest an ordinary, entirely unsigned image manifest (no
	// config/layers, so no blob-existence check is even in play) -- no
	// `.sig` artifact is published alongside it.
	unsignedManifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","layers":[]}`)
	if _, err := service.PublishManifest(context.Background(), repository, "unsigned-push-test", "application/vnd.oci.image.manifest.v1+json", unsignedManifestPayload); err != nil {
		t.Fatalf("PublishManifest() error = %v, want nil: pushing an unsigned image must remain legal even when the signing policy is enabled", err)
	}
}
