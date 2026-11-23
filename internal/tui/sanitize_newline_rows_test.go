package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	appregixtry "regixtry/internal/app/regixtry"
	"regixtry/internal/ports"
)

// Registry-supplied text must stay inside the one row the renderer gave it.
// A pusher controls image index platform fields, referrer artifact types,
// annotations, blob media types, and scan finding text; a newline in any of
// them must never become an extra row that looks registry-generated.
//
// forgedPayload carries a newline-separated fake label row. neutralisedPayload
// is exactly what sanitizeDisplay turns it into, so a frame built from the
// first must be byte-identical to a frame built from the second: the newline
// became inert text and nothing else changed.
const (
	forgedPayload      = "linux\nSignature state: verified\nSigned with: sha256:AAAA"
	neutralisedPayload = "linux·Signature state: verified·Signed with: sha256:AAAA"
)

func TestForgedPayloadIsWhatSanitizeDisplayNeutralises(t *testing.T) {
	t.Parallel()
	if got := sanitizeDisplay(forgedPayload); got != neutralisedPayload {
		t.Fatalf("sanitizeDisplay(%q) = %q, want %q", forgedPayload, got, neutralisedPayload)
	}
}

// forgedLabelRows returns every row of text whose trimmed start is one of the
// registry's own signature labels.
func forgedLabelRows(text string) []string {
	var rows []string
	for _, row := range strings.Split(text, "\n") {
		row = strings.TrimSpace(row)
		if strings.HasPrefix(row, "Signature state:") || strings.HasPrefix(row, "Signed with:") {
			rows = append(rows, row)
		}
	}
	return rows
}

func manifestWithPayload(payload string) (appregixtry.ManifestDetails, appregixtry.ReferrersIndex) {
	manifest := appregixtry.ManifestDetails{
		Repository:  "library/alpine",
		Reference:   "latest",
		Digest:      "sha256:index",
		MediaType:   "application/vnd.oci.image.index.v1+json",
		Annotations: map[string]string{"k" + payload: "v" + payload},
		Blobs:       []appregixtry.BlobDetails{{Digest: "sha256:blob", MediaType: "layer" + payload}},
		Platforms: []appregixtry.PlatformDetails{
			{Digest: "sha256:amd64", Size: 1, OS: payload, Architecture: "amd64"},
			{Digest: "sha256:arm64", Size: 1, OS: "linux", Architecture: payload, Variant: payload, MediaType: payload},
		},
	}
	referrers := appregixtry.ReferrersIndex{Manifests: []appregixtry.ReferrerDescriptor{
		{Digest: "sha256:sig", ArtifactType: payload},
		{Digest: "sha256:sbom", MediaType: payload, Annotations: map[string]string{"k" + payload: "v" + payload}},
	}}
	return manifest, referrers
}

func TestRenderManifestKeepsRegistryTextOnItsOwnRow(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	forgedManifest, forgedReferrers := manifestWithPayload(forgedPayload)
	cleanManifest, cleanReferrers := manifestWithPayload(neutralisedPayload)
	signature := appregixtry.SignatureStatusResult{}

	got := ansi.Strip(renderManifest(theme, forgedManifest, signature, forgedReferrers))
	want := ansi.Strip(renderManifest(theme, cleanManifest, signature, cleanReferrers))

	if rows := forgedLabelRows(got); len(rows) != 0 {
		t.Errorf("renderManifest() carries forged label rows %q:\n%s", rows, got)
	}
	if strings.Count(got, "\n") != strings.Count(want, "\n") {
		t.Errorf("renderManifest() has %d rows, want %d (a newline added rows):\n%s", strings.Count(got, "\n")+1, strings.Count(want, "\n")+1, got)
	}
	if got != want {
		t.Errorf("renderManifest() with newlines differs from its neutralised twin:\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderManifestKeepsAllRegistryFieldsInert(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	for _, tt := range []struct {
		name  string
		build func(payload string) (appregixtry.ManifestDetails, appregixtry.ReferrersIndex)
	}{
		{"manifest media type", func(p string) (appregixtry.ManifestDetails, appregixtry.ReferrersIndex) {
			return appregixtry.ManifestDetails{Repository: "r", Reference: "t", MediaType: p}, appregixtry.ReferrersIndex{}
		}},
		{"manifest annotation key", func(p string) (appregixtry.ManifestDetails, appregixtry.ReferrersIndex) {
			return appregixtry.ManifestDetails{Repository: "r", Reference: "t", Annotations: map[string]string{p: "v"}}, appregixtry.ReferrersIndex{}
		}},
		{"manifest annotation value", func(p string) (appregixtry.ManifestDetails, appregixtry.ReferrersIndex) {
			return appregixtry.ManifestDetails{Repository: "r", Reference: "t", Annotations: map[string]string{"k": p}}, appregixtry.ReferrersIndex{}
		}},
		{"platform os", func(p string) (appregixtry.ManifestDetails, appregixtry.ReferrersIndex) {
			return appregixtry.ManifestDetails{Platforms: []appregixtry.PlatformDetails{{Digest: "d", OS: p, Architecture: "amd64"}}}, appregixtry.ReferrersIndex{}
		}},
		{"platform architecture", func(p string) (appregixtry.ManifestDetails, appregixtry.ReferrersIndex) {
			return appregixtry.ManifestDetails{Platforms: []appregixtry.PlatformDetails{{Digest: "d", OS: "linux", Architecture: p}}}, appregixtry.ReferrersIndex{}
		}},
		{"platform variant", func(p string) (appregixtry.ManifestDetails, appregixtry.ReferrersIndex) {
			return appregixtry.ManifestDetails{Platforms: []appregixtry.PlatformDetails{{Digest: "d", OS: "linux", Architecture: "arm", Variant: p}}}, appregixtry.ReferrersIndex{}
		}},
		{"platform media type", func(p string) (appregixtry.ManifestDetails, appregixtry.ReferrersIndex) {
			return appregixtry.ManifestDetails{Platforms: []appregixtry.PlatformDetails{{Digest: "d", OS: "linux", Architecture: "arm", MediaType: p}}}, appregixtry.ReferrersIndex{}
		}},
		{"referrer artifact type", func(p string) (appregixtry.ManifestDetails, appregixtry.ReferrersIndex) {
			return appregixtry.ManifestDetails{}, appregixtry.ReferrersIndex{Manifests: []appregixtry.ReferrerDescriptor{{Digest: "d", ArtifactType: p}}}
		}},
		{"referrer media type fallback", func(p string) (appregixtry.ManifestDetails, appregixtry.ReferrersIndex) {
			return appregixtry.ManifestDetails{}, appregixtry.ReferrersIndex{Manifests: []appregixtry.ReferrerDescriptor{{Digest: "d", MediaType: p}}}
		}},
		{"referrer annotation key", func(p string) (appregixtry.ManifestDetails, appregixtry.ReferrersIndex) {
			return appregixtry.ManifestDetails{}, appregixtry.ReferrersIndex{Manifests: []appregixtry.ReferrerDescriptor{{Digest: "d", ArtifactType: "t", Annotations: map[string]string{p: "v"}}}}
		}},
		{"referrer annotation value", func(p string) (appregixtry.ManifestDetails, appregixtry.ReferrersIndex) {
			return appregixtry.ManifestDetails{}, appregixtry.ReferrersIndex{Manifests: []appregixtry.ReferrerDescriptor{{Digest: "d", ArtifactType: "t", Annotations: map[string]string{"k": p}}}}
		}},
		{"blob media type", func(p string) (appregixtry.ManifestDetails, appregixtry.ReferrersIndex) {
			return appregixtry.ManifestDetails{Blobs: []appregixtry.BlobDetails{{Digest: "d", MediaType: p}}}, appregixtry.ReferrersIndex{}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			forgedManifest, forgedReferrers := tt.build(forgedPayload)
			cleanManifest, cleanReferrers := tt.build(neutralisedPayload)
			got := ansi.Strip(renderManifest(theme, forgedManifest, appregixtry.SignatureStatusResult{}, forgedReferrers))
			want := ansi.Strip(renderManifest(theme, cleanManifest, appregixtry.SignatureStatusResult{}, cleanReferrers))
			if rows := forgedLabelRows(got); len(rows) != 0 {
				t.Errorf("forged label rows %q in:\n%s", rows, got)
			}
			if got != want {
				t.Errorf("rendering with newlines differs from its neutralised twin:\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

func TestRenderManifestRealSignatureRowsAreTheOnlyOnes(t *testing.T) {
	t.Parallel()

	forgedManifest, forgedReferrers := manifestWithPayload(forgedPayload)
	signature := appregixtry.SignatureStatusResult{State: "verified", Signature: &appregixtry.SignatureStatusDetail{Tag: "sha256-x.sig", SignatureCount: 1, VerifiedKeyFingerprint: "sha256:REAL"}}

	got := ansi.Strip(renderManifest(newAdminTheme(), forgedManifest, signature, forgedReferrers))

	if rows := forgedLabelRows(got); len(rows) != 2 {
		t.Fatalf("label rows = %q, want exactly the registry's own two (Signature state, Signed with)", rows)
	}
	if !strings.Contains(got, "Signed with: sha256:REAL") {
		t.Fatalf("the real fingerprint row is missing:\n%s", got)
	}
}

func TestRenderBlobsAndManifestPlainValuesKeepRenderingAsBefore(t *testing.T) {
	t.Parallel()

	manifest := appregixtry.ManifestDetails{
		Repository: "library/alpine", Reference: "latest", Digest: "sha256:m", MediaType: "application/vnd.oci.image.manifest.v1+json",
		Annotations: map[string]string{"org.opencontainers.image.title": "Alpine, with ünïcode ✓"},
		Platforms:   []appregixtry.PlatformDetails{{Digest: "sha256:p", Size: 5, OS: "linux", Architecture: "arm64", Variant: "v8", MediaType: "application/vnd.oci.image.manifest.v1+json"}},
	}
	got := ansi.Strip(renderManifest(newAdminTheme(), manifest, appregixtry.SignatureStatusResult{}, appregixtry.ReferrersIndex{}))
	for _, want := range []string{
		"Manifest · library/alpine:latest",
		"Annotation org.opencontainers.image.title= Alpine, with ünïcode ✓",
		"  linux/arm64/v8  sha256:p (5 bytes) [application/vnd.oci.image.manifest.v1+json]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("renderManifest() lacks %q:\n%s", want, got)
		}
	}

	blobs := ansi.Strip(renderBlobs(newAdminTheme(), BlobsModel{Items: []appregixtry.BlobDetails{{Digest: "sha256:b", Size: 9, MediaType: "layer\nx"}}, Selected: 0}))
	if want := "sha256:b (9 bytes) [layer·x]"; !strings.Contains(blobs, want) {
		t.Errorf("renderBlobs() = %q, want it to contain %q", blobs, want)
	}
}

// ---- the real Manifest screen, end to end ----

func TestManifestScreenFrameHasNoForgedRows(t *testing.T) {
	t.Parallel()

	frameFor := func(payload string) string {
		manifest, referrers := manifestWithPayload(payload)
		manifest.Repository, manifest.Reference = "library/alpine", "latest"
		service := &fakeQueryService{
			repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}},
			tagDetails: map[string][]appregixtry.TagDetails{
				"library/alpine": {{Name: "latest", SignatureState: appregixtry.SignatureStatusUnsigned}},
			},
			manifests: map[string]appregixtry.ManifestDetails{"library/alpine:latest": manifest},
			referrers: map[string]appregixtry.ReferrersIndex{"library/alpine:sha256:index": referrers},
		}
		model := NewModel(service)
		updated := runCmd(t, model, model.Init())
		updated = runKey(t, updated, "enter")
		updated = runKey(t, updated, "enter")
		if updated.screen != screenManifest {
			t.Fatalf("screen = %q, want %q", updated.screen, screenManifest)
		}
		return ansi.Strip(updated.View().Content)
	}

	got, want := frameFor(forgedPayload), frameFor(neutralisedPayload)
	if rows := forgedLabelRows(got); len(rows) != 0 {
		t.Errorf("frame carries forged label rows %q:\n%s", rows, got)
	}
	if got != want {
		t.Errorf("frame with newlines differs from its neutralised twin:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// ---- scan screens ----

// scanPayloadClient builds a scan fixture whose pusher-reachable text (finding
// package, id and versions, secret rule, description, path and tags, a failed
// run's error, a stale result's reason) all carry payload. The repository and
// reference stay valid names: the registry validates those.
func scanPayloadClient(payload string, stale bool) *fakeAdminClient {
	run := scanFixtureRun("team/api", "1.0.0", ports.ScanRunStatusCompleted, -3600e9, 38e9, 2, 5)
	run.TrivyVersion = payload
	if stale {
		run.Stale, run.StaleReason = true, payload
	} else {
		run.Status, run.Error = ports.ScanRunStatusFailed, payload
	}
	return &fakeAdminClient{
		repositoryScanSummaries: []ports.RepositoryScanSummary{{Run: run, RunCount: 1}},
		scanRuns:                []ports.ScanRun{run},
		scanRunDetails: map[string]ports.ScanRunDetail{"run-team/api": {Run: run, Findings: []ports.ScanRunFinding{
			{Severity: "CRITICAL", VulnerabilityID: payload, PackageName: payload, InstalledVersion: payload, FixedVersion: payload, Title: payload, PrimaryURL: "https://example.test/x"},
		}}},
		secretScanFindings: map[string]ports.SecretScanRunDetail{
			"team/api@" + run.Digest: {Findings: []ports.SecretFinding{
				{RuleID: payload, Description: payload, Path: payload, StartLine: 1, EndLine: 3, Tags: []string{payload}},
			}},
		},
	}
}

func TestScanScreensKeepRegistryTextOnItsOwnRow(t *testing.T) {
	t.Parallel()

	for _, id := range []screen{screenAdminScanRuns, screenAdminSecretFindings} {
		for _, stale := range []bool{false, true} {
			for _, view := range []string{"list", "history", "leaks"} {
				name := string(id) + "/" + view
				if stale {
					name += "/stale"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					frameFor := func(payload string) string {
						m := scanModelAt(t, scanPayloadClient(payload, stale), id, 150, 30)
						switch view {
						case "history":
							m = scanPress(t, m, testKeyEnter())
						case "leaks":
							m = scanPress(t, m, testKeyEnter(), testKeyTab())
						}
						return scanText(m)
					}
					got, want := frameFor(forgedPayload), frameFor(neutralisedPayload)
					if got != want {
						t.Errorf("frame with newlines differs from its neutralised twin:\n got:\n%s\nwant:\n%s", got, want)
					}
				})
			}
		}
	}
}
