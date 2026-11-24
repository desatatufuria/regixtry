package regixtry

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
	"regixtry/internal/ports"
)

const (
	ociImageManifestType = "application/vnd.oci.image.manifest.v1+json"
	ociImageIndexType    = "application/vnd.oci.image.index.v1+json"
	ociImageConfigType   = "application/vnd.oci.image.config.v1+json"
	ociImageLayerType    = "application/vnd.oci.image.layer.v1.tar+gzip"
	ociEmptyType         = "application/vnd.oci.empty.v1+json"
)

// scanSkipFixture is a repository with one real image, a scan runner that
// records every target it is asked to scan, and one blob the manifests below
// reference.
type scanSkipFixture struct {
	service *Service
	runner  *capturingScanRunner
	image   ManifestDetails
	blob    string
}

func newScanSkipFixture(t *testing.T) *scanSkipFixture {
	t.Helper()

	service, cleanup := newTestService(t, allowAllAccessController{})
	t.Cleanup(cleanup)

	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 4}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
	runner := &capturingScanRunner{result: ports.ScanResult{TrivyVersion: "0.57.1"}}
	service.SetScanRunner(runner)

	upload, err := service.BeginUpload(context.Background(), "library/skip")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if _, err := service.AppendUpload(context.Background(), "library/skip", upload.ID, strings.NewReader("layer-one")); err != nil {
		t.Fatalf("AppendUpload() error = %v", err)
	}
	blob, err := service.CompleteUpload(context.Background(), "library/skip", upload.ID, digestForTest([]byte("layer-one")), nil)
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}

	imagePayload := []byte(`{"schemaVersion":2,"mediaType":"` + ociImageManifestType + `","config":{"mediaType":"` + ociImageConfigType + `","digest":"` + blob.Digest + `","size":9},"layers":[{"mediaType":"` + ociImageLayerType + `","digest":"` + blob.Digest + `","size":9}]}`)
	image, err := service.PublishManifest(context.Background(), "library/skip", "v1", ociImageManifestType, imagePayload)
	if err != nil {
		t.Fatalf("PublishManifest(image) error = %v", err)
	}
	waitForRunnerTargets(t, runner, 1)

	return &scanSkipFixture{service: service, runner: runner, image: image, blob: blob.Digest}
}

// subjectManifest builds a manifest payload that points at the fixture image
// through "subject", with the given artifactType, config media type and layer
// media types (an empty configType omits the config; an empty layer list
// omits the layers).
func (f *scanSkipFixture) subjectManifest(artifactType, configType string, layerTypes ...string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, `{"schemaVersion":2,"mediaType":%q,`, ociImageManifestType)
	if artifactType != "" {
		fmt.Fprintf(&b, `"artifactType":%q,`, artifactType)
	}
	fmt.Fprintf(&b, `"subject":{"mediaType":%q,"digest":%q,"size":%d}`, ociImageManifestType, f.image.Digest, f.image.Size)
	if configType != "" {
		fmt.Fprintf(&b, `,"config":{"mediaType":%q,"digest":%q,"size":9}`, configType, f.blob)
	}
	if len(layerTypes) > 0 {
		b.WriteString(`,"layers":[`)
		for i, layerType := range layerTypes {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"mediaType":%q,"digest":%q,"size":9}`, layerType, f.blob)
		}
		b.WriteString("]")
	}
	b.WriteString("}")
	return []byte(b.String())
}

// scannedTargets reports, once background work and a grace period have passed,
// how many digests reached the scan runner.
func (f *scanSkipFixture) scannedTargets() int {
	f.service.WaitForBackgroundWork()
	time.Sleep(100 * time.Millisecond)
	f.runner.mu.Lock()
	defer f.runner.mu.Unlock()
	return len(f.runner.targets)
}

// TestPublishManifestScansImageShapedManifestDespiteSubjectOrSignatureTag is
// the regression for the push-scan opt-out: a writer chooses a subject, or a
// cosign-looking tag, for a manifest that carries real image layers. The
// skip must follow the manifest content, so such an image is still scanned
// and the vulnerability pull policy can apply to it.
func TestPublishManifestScansImageShapedManifestDespiteSubjectOrSignatureTag(t *testing.T) {
	t.Parallel()

	t.Run("image layers with a subject, pushed by digest", func(t *testing.T) {
		t.Parallel()

		f := newScanSkipFixture(t)
		payload := f.subjectManifest("", ociImageConfigType, ociImageLayerType)
		pushed, err := f.service.PublishManifest(context.Background(), "library/skip", digestForTest(payload), ociImageManifestType, payload)
		if err != nil {
			t.Fatalf("PublishManifest() error = %v", err)
		}

		waitForRunnerTargets(t, f.runner, 2)
		deadline := time.Now().Add(3 * time.Second)
		for {
			_, err := f.service.metadata.GetLatestCompletedScanRunByDigest(context.Background(), "tenant-a", "library/skip", pushed.Digest)
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("GetLatestCompletedScanRunByDigest(%s) error = %v, want a completed push scan", pushed.Digest, err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	})

	t.Run("image layers under a legacy signature tag", func(t *testing.T) {
		t.Parallel()

		f := newScanSkipFixture(t)
		tag, err := signing.SignatureTag(f.image.Digest)
		if err != nil {
			t.Fatalf("signing.SignatureTag() error = %v", err)
		}
		payload := []byte(`{"schemaVersion":2,"mediaType":"` + ociImageManifestType + `","config":{"mediaType":"` + ociImageConfigType + `","digest":"` + f.blob + `","size":9},"layers":[{"mediaType":"` + ociImageLayerType + `","digest":"` + f.blob + `","size":9},{"mediaType":"` + ociImageLayerType + `","digest":"` + f.blob + `","size":9}]}`)
		if _, err := f.service.PublishManifest(context.Background(), "library/skip", tag, ociImageManifestType, payload); err != nil {
			t.Fatalf("PublishManifest() error = %v", err)
		}

		waitForRunnerTargets(t, f.runner, 2)
	})

	t.Run("image layers under a bundle index tag", func(t *testing.T) {
		t.Parallel()

		f := newScanSkipFixture(t)
		tag, err := signing.BundleIndexTag(f.image.Digest)
		if err != nil {
			t.Fatalf("signing.BundleIndexTag() error = %v", err)
		}
		payload := []byte(`{"schemaVersion":2,"mediaType":"` + ociImageManifestType + `","config":{"mediaType":"` + ociImageConfigType + `","digest":"` + f.blob + `","size":9},"layers":[{"mediaType":"` + ociImageLayerType + `","digest":"` + f.blob + `","size":9},{"mediaType":"` + ociImageLayerType + `","digest":"` + f.blob + `","size":9}]}`)
		if _, err := f.service.PublishManifest(context.Background(), "library/skip", tag, ociImageManifestType, payload); err != nil {
			t.Fatalf("PublishManifest() error = %v", err)
		}

		waitForRunnerTargets(t, f.runner, 2)
	})

	// A referrer-looking artifactType or config does not vouch for layers
	// that are not referrer payloads: such a manifest carries real content.
	for _, layerType := range []string{"application/octet-stream", "application/x-tar", "application/gzip"} {
		t.Run("spoofed referrer type with "+layerType+" layer and a subject", func(t *testing.T) {
			t.Parallel()

			f := newScanSkipFixture(t)
			payload := f.subjectManifest(signing.SigstoreBundleMediaType, ociEmptyType, layerType)
			if _, err := f.service.PublishManifest(context.Background(), "library/skip", digestForTest(payload), ociImageManifestType, payload); err != nil {
				t.Fatalf("PublishManifest() error = %v", err)
			}

			waitForRunnerTargets(t, f.runner, 2)
		})

		t.Run("cosign config with "+layerType+" layer under a signature tag", func(t *testing.T) {
			t.Parallel()

			f := newScanSkipFixture(t)
			tag, err := signing.SignatureTag(f.image.Digest)
			if err != nil {
				t.Fatalf("signing.SignatureTag() error = %v", err)
			}
			payload := []byte(`{"schemaVersion":2,"mediaType":"` + ociImageManifestType + `","config":{"mediaType":"application/vnd.dev.cosign.artifact.sig.v1+json","digest":"` + f.blob + `","size":9},"layers":[{"mediaType":"` + layerType + `","digest":"` + f.blob + `","size":9}]}`)
			if _, err := f.service.PublishManifest(context.Background(), "library/skip", tag, ociImageManifestType, payload); err != nil {
				t.Fatalf("PublishManifest() error = %v", err)
			}

			waitForRunnerTargets(t, f.runner, 2)
		})
	}

	t.Run("index without referrer entries under a bundle index tag", func(t *testing.T) {
		t.Parallel()

		f := newScanSkipFixture(t)
		tag, err := signing.BundleIndexTag(f.image.Digest)
		if err != nil {
			t.Fatalf("signing.BundleIndexTag() error = %v", err)
		}
		payload := []byte(`{"schemaVersion":2,"mediaType":"` + ociImageIndexType + `","manifests":[{"mediaType":"` + ociImageManifestType + `","size":` + fmt.Sprint(f.image.Size) + `,"digest":"` + f.image.Digest + `","platform":{"architecture":"amd64","os":"linux"}}]}`)
		if _, err := f.service.PublishManifest(context.Background(), "library/skip", tag, ociImageIndexType, payload); err != nil {
			t.Fatalf("PublishManifest() error = %v", err)
		}

		waitForRunnerTargets(t, f.runner, 2)
	})
}

// TestPublishManifestStillSkipsGenuineReferrerArtifacts pins the behavior the
// fix must keep: real signatures, attestations, SBOMs and bundle indexes are
// never offered to Trivy.
func TestPublishManifestStillSkipsGenuineReferrerArtifacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload func(f *scanSkipFixture) []byte
		tag     func(f *scanSkipFixture) string
	}{
		{
			name: "sigstore bundle referrer",
			payload: func(f *scanSkipFixture) []byte {
				return f.subjectManifest(signing.SigstoreBundleMediaType, ociEmptyType, signing.SigstoreBundleMediaType)
			},
		},
		{
			name: "cosign simple signing referrer with artifactType only",
			payload: func(f *scanSkipFixture) []byte {
				return f.subjectManifest("application/vnd.dev.cosign.artifact.sig.v1+json", "", signing.SimpleSigningMediaType)
			},
		},
		{
			name: "in-toto attestation referrer",
			payload: func(f *scanSkipFixture) []byte {
				return f.subjectManifest("application/vnd.in-toto+json", ociEmptyType, "application/vnd.in-toto+json")
			},
		},
		{
			name: "spdx sbom referrer",
			payload: func(f *scanSkipFixture) []byte {
				return f.subjectManifest("application/spdx+json", ociEmptyType, "application/spdx+json")
			},
		},
		{
			name: "legacy cosign signature manifest under the sig tag",
			payload: func(f *scanSkipFixture) []byte {
				return f.subjectManifest("", ociImageConfigType, signing.SimpleSigningMediaType)
			},
			tag: func(f *scanSkipFixture) string {
				tag, _ := signing.SignatureTag(f.image.Digest)
				return tag
			},
		},
		{
			name: "bundle index under the bundle index tag",
			payload: func(f *scanSkipFixture) []byte {
				return []byte(`{"schemaVersion":2,"mediaType":"` + ociImageIndexType + `","manifests":[{"mediaType":"` + ociImageManifestType + `","size":888,"digest":"` + f.blob + `","artifactType":"` + signing.SigstoreBundleMediaType + `"}]}`)
			},
			tag: func(f *scanSkipFixture) string {
				tag, _ := signing.BundleIndexTag(f.image.Digest)
				return tag
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newScanSkipFixture(t)
			payload := tt.payload(f)
			reference := digestForTest(payload)
			mediaType := ociImageManifestType
			if tt.tag != nil {
				reference = tt.tag(f)
			}
			if strings.Contains(string(payload), `"manifests"`) {
				mediaType = ociImageIndexType
			}
			pushed, err := f.service.PublishManifest(context.Background(), "library/skip", reference, mediaType, payload)
			if err != nil {
				t.Fatalf("PublishManifest() error = %v", err)
			}

			if got := f.scannedTargets(); got != 1 {
				t.Fatalf("scan runner targets = %d, want still 1 (a genuine referrer artifact is never scanned)", got)
			}
			if _, err := f.service.metadata.GetActiveScanRunByDigest(context.Background(), "tenant-a", "library/skip", pushed.Digest); !domain.IsCode(err, domain.ErrorCodeNotFound) {
				t.Fatalf("GetActiveScanRunByDigest() error = %v, want ErrorCodeNotFound", err)
			}
		})
	}
}

// TestIsReferrerArtifactPushDecidesFromContent pins the decision table: the
// manifest content must look like a referrer before a subject or a
// cosign-shaped tag can skip the push scan.
func TestIsReferrerArtifactPushDecidesFromContent(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("a", 64)
	sigTag := "sha256-" + strings.Repeat("a", 64) + ".sig"
	bundleTag := "sha256-" + strings.Repeat("a", 64)
	subject := `"subject":{"mediaType":"` + ociImageManifestType + `","digest":"` + digest + `","size":10}`
	config := func(mediaType string) string {
		return `"config":{"mediaType":"` + mediaType + `","digest":"` + digest + `","size":2}`
	}
	layers := func(mediaTypes ...string) string {
		parts := make([]string, len(mediaTypes))
		for i, mediaType := range mediaTypes {
			parts[i] = `{"mediaType":"` + mediaType + `","digest":"` + digest + `","size":2}`
		}
		return `"layers":[` + strings.Join(parts, ",") + `]`
	}
	manifest := func(fields ...string) string {
		return `{"schemaVersion":2,"mediaType":"` + ociImageManifestType + `",` + strings.Join(fields, ",") + `}`
	}
	index := func(artifactTypes ...string) string {
		entries := make([]string, len(artifactTypes))
		for i, artifactType := range artifactTypes {
			entries[i] = `{"mediaType":"` + ociImageManifestType + `","digest":"` + digest + `","size":2,"artifactType":"` + artifactType + `"}`
		}
		return `{"schemaVersion":2,"mediaType":"` + ociImageIndexType + `","manifests":[` + strings.Join(entries, ",") + `]}`
	}

	tests := []struct {
		name    string
		payload string
		tag     string
		want    bool
	}{
		{"image with subject", manifest(subject, config(ociImageConfigType), layers(ociImageLayerType)), "v1", false},
		{"image with signature tag", manifest(config(ociImageConfigType), layers(ociImageLayerType)), sigTag, false},
		{"image with bundle tag", manifest(config(ociImageConfigType), layers(ociImageLayerType)), bundleTag, false},
		{"docker rootfs layer beside a referrer artifactType", manifest(`"artifactType":"`+signing.SigstoreBundleMediaType+`"`, subject, config(ociEmptyType), layers("application/vnd.docker.image.rootfs.diff.tar.gzip")), "v1", false},
		{"image layer mixed with a signature layer", manifest(subject, config(ociImageConfigType), layers(signing.SimpleSigningMediaType, ociImageLayerType)), "v1", false},
		{"uppercase image layer type", manifest(subject, config(ociEmptyType), layers("Application/VND.OCI.Image.Layer.V1.Tar")), "v1", false},
		{"subject with unknown artifact type and unknown config", manifest(`"artifactType":"application/vnd.example.thing"`, subject, config("application/vnd.example.cfg"), layers("application/octet-stream")), "v1", false},
		{"subject with no config and no layers", manifest(subject), "v1", false},
		{"referrer artifactType with octet-stream layer", manifest(`"artifactType":"`+signing.SigstoreBundleMediaType+`"`, subject, config(ociEmptyType), layers("application/octet-stream")), "v1", false},
		{"referrer config with x-tar layer", manifest(subject, config("application/vnd.dev.cosign.artifact.sig.v1+json"), layers("application/x-tar")), "v1", false},
		{"referrer config with gzip layer under the sig tag", manifest(config("application/vnd.dev.cosign.artifact.sig.v1+json"), layers("application/gzip")), sigTag, false},
		{"referrer layer mixed with an unknown layer", manifest(`"artifactType":"`+signing.SigstoreBundleMediaType+`"`, subject, config(ociEmptyType), layers(signing.SigstoreBundleMediaType, "application/octet-stream")), "v1", false},
		{"notary signature referrer", manifest(`"artifactType":"application/vnd.cncf.notary.signature"`, subject, config(ociEmptyType), layers("application/jose+json")), "v1", true},
		{"sigstore bundle referrer", manifest(`"artifactType":"`+signing.SigstoreBundleMediaType+`"`, subject, config(ociEmptyType), layers(signing.SigstoreBundleMediaType)), "v1", true},
		{"referrer by uppercase artifact type", manifest(`"artifactType":"Application/VND.In-Toto+json"`, subject, config(ociEmptyType)), "v1", true},
		{"cyclonedx sbom referrer", manifest(subject, config(ociEmptyType), layers("application/vnd.cyclonedx+json")), "v1", true},
		{"legacy signature layers with image config", manifest(subject, config(ociImageConfigType), layers(signing.SimpleSigningMediaType)), "v1", true},
		{"legacy signature under the sig tag without subject", manifest(config(ociImageConfigType), layers(signing.SimpleSigningMediaType)), sigTag, true},
		{"referrer content without subject or tag", manifest(`"artifactType":"`+signing.SigstoreBundleMediaType+`"`, config(ociEmptyType), layers(signing.SigstoreBundleMediaType)), "v1", false},
		{"referrer content under a non-matching sha256 tag", manifest(`"artifactType":"`+signing.SigstoreBundleMediaType+`"`, config(ociEmptyType), layers(signing.SigstoreBundleMediaType)), "sha256-abc", false},
		{"bundle index", index(signing.SigstoreBundleMediaType), bundleTag, true},
		{"bundle index with several referrer entries", index(signing.SigstoreBundleMediaType, "application/vnd.in-toto+json"), bundleTag, true},
		{"index with one non-referrer entry", index(signing.SigstoreBundleMediaType, "application/vnd.example.thing"), bundleTag, false},
		{"index with an entry without artifact type", index(""), bundleTag, false},
		{"referrer index under an ordinary tag", index(signing.SigstoreBundleMediaType), "v1", false},
		{"empty index under the bundle tag", `{"schemaVersion":2,"mediaType":"` + ociImageIndexType + `","manifests":[]}`, bundleTag, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mediaType := ociImageManifestType
			if strings.Contains(tt.payload, ociImageIndexType) {
				mediaType = ociImageIndexType
			}
			parsed, parsedTag, err := parseManifestPayload(tt.tag, mediaType, []byte(tt.payload))
			if err != nil {
				t.Fatalf("parseManifestPayload() error = %v", err)
			}
			if got := isReferrerArtifactPush(parsed, parsedTag); got != tt.want {
				t.Fatalf("isReferrerArtifactPush() = %v, want %v", got, tt.want)
			}
		})
	}
}
