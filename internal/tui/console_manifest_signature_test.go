package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	appregixtry "regixtry/internal/app/regixtry"
)

// TestRenderManifestSignatureSectionStates is the RED test for the manifest
// inspection view's Signature section: it renders the exact fixed-vocabulary
// State plus the `.sig` tag/signature count when a signature was resolved,
// and a minimal "not signed" line when it was not -- reusing the SAME
// SignatureStatusResult/SignatureStatusDetail types Service.SignatureStatus
// already returns (no new fields).
func TestRenderManifestSignatureSectionStates(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	manifest := appregixtry.ManifestDetails{Repository: "library/alpine", Reference: "latest", Digest: "sha256:manifest"}

	tests := []struct {
		name      string
		signature appregixtry.SignatureStatusResult
		wantAll   []string
		wantNone  []string
	}{
		{
			name: "verified",
			signature: appregixtry.SignatureStatusResult{
				State: appregixtry.SignatureStatusVerified,
				Signature: &appregixtry.SignatureStatusDetail{
					Tag:                    "sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.sig",
					SignatureCount:         1,
					VerifiedKeyFingerprint: "abcdef012345",
				},
			},
			wantAll: []string{
				appregixtry.SignatureStatusVerified,
				"sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.sig",
				"1",
				"Signed with:",
				"abcdef012345",
			},
			wantNone: []string{"not signed"},
		},
		{
			name:      "unsigned",
			signature: appregixtry.SignatureStatusResult{State: appregixtry.SignatureStatusUnsigned},
			wantAll:   []string{"not signed"},
			wantNone: []string{
				appregixtry.SignatureStatusVerified,
				appregixtry.SignatureStatusUntrusted,
				appregixtry.SignatureStatusMismatched,
				appregixtry.SignatureStatusUnverifiable,
			},
		},
		{
			name: "untrusted",
			signature: appregixtry.SignatureStatusResult{
				State: appregixtry.SignatureStatusUntrusted,
				Signature: &appregixtry.SignatureStatusDetail{
					Tag:            "sha256-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.sig",
					SignatureCount: 1,
				},
			},
			wantAll: []string{
				appregixtry.SignatureStatusUntrusted,
				"sha256-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.sig",
			},
			wantNone: []string{"not signed", "Signed with:"},
		},
		{
			name: "mismatched",
			signature: appregixtry.SignatureStatusResult{
				State: appregixtry.SignatureStatusMismatched,
				Signature: &appregixtry.SignatureStatusDetail{
					Tag:            "sha256-cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc.sig",
					SignatureCount: 2,
				},
			},
			wantAll: []string{
				appregixtry.SignatureStatusMismatched,
				"sha256-cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc.sig",
				"2",
			},
			wantNone: []string{"not signed", "Signed with:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rendered := ansi.Strip(renderManifest(theme, manifest, tt.signature))
			for _, want := range tt.wantAll {
				if !strings.Contains(rendered, want) {
					t.Fatalf("renderManifest() = %q, want it to contain %q", rendered, want)
				}
			}
			for _, notWant := range tt.wantNone {
				if strings.Contains(rendered, notWant) {
					t.Fatalf("renderManifest() = %q, want it NOT to contain %q", rendered, notWant)
				}
			}
		})
	}
}

// TestRenderManifestSignatureSectionNeverLeaksKeyMaterial mirrors this
// codebase's existing no-key-leakage assertion style (see
// internal/protocol/http/signature_status_test.go, which asserts a response
// body never contains "BEGIN PUBLIC KEY"): even if SignatureStatusDetail's
// Reason field ever carried adversarial content, the Signature section must
// never surface it -- the spec explicitly excludes Reason (and any key
// material, raw signature bytes, or trust configuration detail) from this
// section's rendered content.
func TestRenderManifestSignatureSectionNeverLeaksKeyMaterial(t *testing.T) {
	t.Parallel()

	const leakSentinelPEM = "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEtestsentinelkeymaterial\n-----END PUBLIC KEY-----"

	theme := newAdminTheme()
	manifest := appregixtry.ManifestDetails{Repository: "library/alpine", Reference: "latest", Digest: "sha256:manifest"}
	signature := appregixtry.SignatureStatusResult{
		State:  appregixtry.SignatureStatusVerified,
		Policy: appregixtry.SignatureStatusPolicy{Enabled: true, TrustedKeys: 1},
		Signature: &appregixtry.SignatureStatusDetail{
			Tag:            "sha256-dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd.sig",
			SignatureCount: 1,
			Reason:         leakSentinelPEM,
		},
	}

	rendered := renderManifest(theme, manifest, signature)

	if strings.Contains(rendered, "BEGIN PUBLIC KEY") {
		t.Fatalf("renderManifest() = %q, must never contain key material", rendered)
	}
	if strings.Contains(rendered, leakSentinelPEM) {
		t.Fatalf("renderManifest() = %q, must never surface SignatureStatusDetail.Reason", rendered)
	}
}
