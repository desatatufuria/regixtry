package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	appregixtry "regixtry/internal/app/regixtry"
)

// TestRenderManifestShowsManifestSizeAndTotalImageSizeDistinctly is the RED
// test for console-manifest-enrichment's total-image-size gap: the manifest
// inspection screen must show BOTH the manifest JSON document's own byte
// size (relabeled "Manifest Size" so it is never confused with the other
// figure) and the actual image content size -- the sum of every blob's Size
// (config + all layers), via ManifestDetails.TotalBlobSize().
func TestRenderManifestShowsManifestSizeAndTotalImageSizeDistinctly(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	manifest := appregixtry.ManifestDetails{
		Repository: "library/alpine",
		Reference:  "latest",
		Digest:     "sha256:manifest",
		Size:       512,
		Blobs: []appregixtry.BlobDetails{
			{Digest: "sha256:config", Size: 100},
			{Digest: "sha256:layer", Size: 3000000},
		},
	}

	rendered := ansi.Strip(renderManifest(theme, manifest, appregixtry.SignatureStatusResult{}, appregixtry.ReferrersIndex{}))

	if !strings.Contains(rendered, "Manifest Size:") {
		t.Fatalf("renderManifest() = %q, want a \"Manifest Size:\" label", rendered)
	}
	if !strings.Contains(rendered, "512 bytes") {
		t.Fatalf("renderManifest() = %q, want the manifest JSON's own size (512 bytes)", rendered)
	}
	if !strings.Contains(rendered, "Total Image Size:") {
		t.Fatalf("renderManifest() = %q, want a \"Total Image Size:\" label", rendered)
	}
	if !strings.Contains(rendered, "3000100 bytes") {
		t.Fatalf("renderManifest() = %q, want the summed blob size (3000100 bytes)", rendered)
	}
}

// TestRenderManifestListsReferrers is the RED test for the Referrers
// section wired through Service.Referrers: each referrer renders its
// digest, its artifactType (falling back to mediaType when artifactType is
// empty), and a one-line annotation summary when annotations are present.
func TestRenderManifestListsReferrers(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	manifest := appregixtry.ManifestDetails{Repository: "library/alpine", Reference: "latest", Digest: "sha256:manifest"}
	referrers := appregixtry.ReferrersIndex{
		Manifests: []appregixtry.ReferrerDescriptor{
			{
				Digest:       "sha256:sig111",
				MediaType:    "application/vnd.oci.image.manifest.v1+json",
				ArtifactType: "application/vnd.dev.sigstore.bundle.v0.3+json",
			},
			{
				Digest:      "sha256:sbom222",
				MediaType:   "application/vnd.oci.image.manifest.v1+json",
				Annotations: map[string]string{"predicateType": "https://spdx.dev/Document"},
			},
		},
	}

	rendered := ansi.Strip(renderManifest(theme, manifest, appregixtry.SignatureStatusResult{}, referrers))

	if !strings.Contains(rendered, "sha256:sig111") {
		t.Fatalf("renderManifest() = %q, want the first referrer's digest", rendered)
	}
	if !strings.Contains(rendered, "application/vnd.dev.sigstore.bundle.v0.3+json") {
		t.Fatalf("renderManifest() = %q, want the first referrer's artifactType", rendered)
	}
	if !strings.Contains(rendered, "sha256:sbom222") {
		t.Fatalf("renderManifest() = %q, want the second referrer's digest", rendered)
	}
	// The second referrer has no ArtifactType, so it must fall back to
	// MediaType rather than rendering an empty kind.
	if !strings.Contains(rendered, "application/vnd.oci.image.manifest.v1+json") {
		t.Fatalf("renderManifest() = %q, want the second referrer's mediaType fallback", rendered)
	}
	if !strings.Contains(rendered, "predicateType=https://spdx.dev/Document") {
		t.Fatalf("renderManifest() = %q, want a one-line annotation summary for the second referrer", rendered)
	}
}

// TestRenderManifestOmitsReferrersSectionWhenEmpty mirrors the existing
// Annotations gating (renderManifest omits that block entirely when there
// are none): an image with no referrers must not render an empty
// "Referrers:" header.
func TestRenderManifestOmitsReferrersSectionWhenEmpty(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	manifest := appregixtry.ManifestDetails{Repository: "library/alpine", Reference: "latest", Digest: "sha256:manifest"}

	rendered := renderManifest(theme, manifest, appregixtry.SignatureStatusResult{}, appregixtry.ReferrersIndex{})

	if strings.Contains(rendered, "Referrers:") {
		t.Fatalf("renderManifest() = %q, want no Referrers section when there are none", rendered)
	}
}

// TestRenderManifestShowsPlatformBreakdownForImageIndex is the RED test for
// image-index-platform-breakdown's TUI rendering: a multi-arch manifest
// (ManifestDetails.Platforms populated) renders each child manifest's
// platform (os/architecture[/variant]) alongside its digest.
func TestRenderManifestShowsPlatformBreakdownForImageIndex(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	manifest := appregixtry.ManifestDetails{
		Repository: "library/alpine",
		Reference:  "latest",
		Digest:     "sha256:index",
		MediaType:  "application/vnd.oci.image.index.v1+json",
		Platforms: []appregixtry.PlatformDetails{
			{Digest: "sha256:amd64manifest", Architecture: "amd64", OS: "linux", Size: 100},
			{Digest: "sha256:arm64manifest", Architecture: "arm64", OS: "linux", Variant: "v8", Size: 200},
		},
	}

	rendered := ansi.Strip(renderManifest(theme, manifest, appregixtry.SignatureStatusResult{}, appregixtry.ReferrersIndex{}))

	if !strings.Contains(rendered, "linux/amd64") {
		t.Fatalf("renderManifest() = %q, want the amd64 platform rendered", rendered)
	}
	if !strings.Contains(rendered, "sha256:amd64manifest") {
		t.Fatalf("renderManifest() = %q, want the amd64 child manifest's digest", rendered)
	}
	if !strings.Contains(rendered, "linux/arm64/v8") {
		t.Fatalf("renderManifest() = %q, want the arm64/v8 platform rendered", rendered)
	}
	if !strings.Contains(rendered, "sha256:arm64manifest") {
		t.Fatalf("renderManifest() = %q, want the arm64 child manifest's digest", rendered)
	}
}

// TestRenderManifestOmitsPlatformsSectionForOrdinaryManifest proves a
// single-arch manifest (empty Platforms) renders no platform breakdown at
// all.
func TestRenderManifestOmitsPlatformsSectionForOrdinaryManifest(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	manifest := appregixtry.ManifestDetails{Repository: "library/alpine", Reference: "latest", Digest: "sha256:manifest"}

	rendered := renderManifest(theme, manifest, appregixtry.SignatureStatusResult{}, appregixtry.ReferrersIndex{})

	if strings.Contains(rendered, "Platforms:") {
		t.Fatalf("renderManifest() = %q, want no Platforms section for an ordinary manifest", rendered)
	}
}

// TestModelLoadManifestFetchesAndStoresReferrers is the RED test for the
// loadManifestCmd wiring: opening a tag's manifest must also call
// QueryService.Referrers (subject digest = the resolved manifest's own
// Digest) and store the result on ManifestModel.Referrers, rendered into
// the manifest screen's view.
func TestModelLoadManifestFetchesAndStoresReferrers(t *testing.T) {
	t.Parallel()

	service := &fakeQueryService{
		repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}},
		tagDetails: map[string][]appregixtry.TagDetails{
			"library/alpine": {{Name: "latest", SignatureState: appregixtry.SignatureStatusUnsigned}},
		},
		manifests: map[string]appregixtry.ManifestDetails{
			"library/alpine:latest": {
				Repository: "library/alpine",
				Reference:  "latest",
				Digest:     "sha256:manifest",
			},
		},
		referrers: map[string]appregixtry.ReferrersIndex{
			"library/alpine:sha256:manifest": {
				Manifests: []appregixtry.ReferrerDescriptor{
					{Digest: "sha256:sig111", ArtifactType: "application/vnd.dev.sigstore.bundle.v0.3+json"},
				},
			},
		},
	}

	model := NewModel(service)
	updated := runCmd(t, model, model.Init())
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "enter")

	if updated.screen != screenManifest {
		t.Fatalf("screen = %q, want %q", updated.screen, screenManifest)
	}
	if service.calls.referrers != 1 {
		t.Fatalf("service.calls.referrers = %d, want 1", service.calls.referrers)
	}
	if got, want := len(updated.manifest.Referrers.Manifests), 1; got != want {
		t.Fatalf("len(updated.manifest.Referrers.Manifests) = %d, want %d", got, want)
	}
	if !strings.Contains(updated.View(), "sha256:sig111") {
		t.Fatalf("view = %q, want the referrer's digest rendered", updated.View())
	}
}
