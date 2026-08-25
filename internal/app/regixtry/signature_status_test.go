package regixtry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
	"regixtry/internal/ports"
)

// fixtureSignatureBase64 is the exact base64 ASN.1 DER signature bytes
// carried by internal/domain/signing/testdata/signature-manifest.json's
// single layer annotation -- used to prove no response ever echoes it.
const fixtureSignatureBase64 = "MEUCIF4bpdl7sYB6SPgwtWWupCuZOjoq1kv9dVdoD/RlamJaAiEA/AwiTgxicjSnCJSzOWMc+5YjmHM2US5WUFFy97oiTmY="

// TestServiceSignatureStatusComputesAllFiveStatesIndependentOfPolicyEnabled
// is the Phase 7 RED test (tasks.md 7.1 and 7.2, design.md Decision 9):
// SignatureStatus computes each of the five states for corresponding
// fixture/double setups independent of whether policy.Enabled is true or
// false -- the state is always computed, never short-circuited by a
// disabled policy -- and WouldBlockPull == policy.Enabled && State !=
// verified, table-driven across all five states x both policy-enabled
// values.
func TestServiceSignatureStatusComputesAllFiveStatesIndependentOfPolicyEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		seed  func(t *testing.T, service *Service, repository string) string
		trust func(t *testing.T) []string
		want  string
	}{
		{
			name: "unsigned -- no .sig tag resolves",
			seed: func(t *testing.T, service *Service, repository string) string {
				return seedFixtureImageManifest(t, service, repository)
			},
			trust: func(t *testing.T) []string { return []string{fixtureTrustedKeyPEM(t)} },
			want:  SignatureStatusUnsigned,
		},
		{
			name: "unverifiable -- zero usable trusted keys",
			seed: func(t *testing.T, service *Service, repository string) string {
				digest := seedFixtureImageManifest(t, service, repository)
				seedFixtureSignatureArtifact(t, service, repository)
				return digest
			},
			trust: func(t *testing.T) []string { return nil },
			want:  SignatureStatusUnverifiable,
		},
		{
			name: "untrusted -- signature present but no configured key validates it",
			seed: func(t *testing.T, service *Service, repository string) string {
				digest := seedFixtureImageManifest(t, service, repository)
				seedFixtureSignatureArtifact(t, service, repository)
				return digest
			},
			trust: func(t *testing.T) []string { return []string{generateTestECDSAP256PublicKeyPEM(t)} },
			want:  SignatureStatusUntrusted,
		},
		{
			name: "mismatched -- signature verifies but binds a different digest",
			seed: func(t *testing.T, service *Service, repository string) string {
				transplantTarget := seedArbitraryImageManifest(t, service, repository, " - status transplant target")
				publishFixtureSignatureManifestAt(t, service, repository, transplantTarget)
				return transplantTarget
			},
			trust: func(t *testing.T) []string { return []string{fixtureTrustedKeyPEM(t)} },
			want:  SignatureStatusMismatched,
		},
		{
			name: "verified",
			seed: func(t *testing.T, service *Service, repository string) string {
				digest := seedFixtureImageManifest(t, service, repository)
				seedFixtureSignatureArtifact(t, service, repository)
				return digest
			},
			trust: func(t *testing.T) []string { return []string{fixtureTrustedKeyPEM(t)} },
			want:  SignatureStatusVerified,
		},
	}

	for _, tt := range tests {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s (policy enabled=%v)", tt.name, enabled), func(t *testing.T) {
				t.Parallel()

				service, cleanup := newTestService(t, allowAllAccessController{})
				defer cleanup()

				repository := "library/alpine"
				digest := tt.seed(t, service, repository)
				seedSigningPolicy(t, service, enabled, tt.trust(t))

				result, err := service.SignatureStatus(context.Background(), repository, digest)
				if err != nil {
					t.Fatalf("SignatureStatus() error = %v", err)
				}
				if result.State != tt.want {
					t.Fatalf("SignatureStatus().State = %q, want %q (state must be computed regardless of policy.Enabled)", result.State, tt.want)
				}
				wantBlocked := enabled && tt.want != SignatureStatusVerified
				if result.WouldBlockPull != wantBlocked {
					t.Fatalf("SignatureStatus().WouldBlockPull = %v, want %v (policy.Enabled=%v, state=%q)", result.WouldBlockPull, wantBlocked, enabled, tt.want)
				}
				if result.Repository != repository || result.Digest != digest {
					t.Fatalf("SignatureStatus() repository/digest = %q/%q, want %q/%q", result.Repository, result.Digest, repository, digest)
				}
			})
		}
	}
}

// TestServiceSignatureStatusReportsVerifiedKeyFingerprintOnlyWhenVerified is
// the RED test for SignatureStatusDetail.VerifiedKeyFingerprint: a verified
// signature reports the short fingerprint (signing.Fingerprint) of the exact
// trusted key that matched it, while every other computed state -- unsigned,
// unverifiable, untrusted, mismatched -- leaves it empty. Never a raw PEM.
func TestServiceSignatureStatusReportsVerifiedKeyFingerprintOnlyWhenVerified(t *testing.T) {
	t.Parallel()

	t.Run("unsigned -- Signature is nil, nothing to leak", func(t *testing.T) {
		t.Parallel()

		service, cleanup := newTestService(t, allowAllAccessController{})
		defer cleanup()

		repository := "library/alpine"
		digest := seedFixtureImageManifest(t, service, repository)
		seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

		result, err := service.SignatureStatus(context.Background(), repository, digest)
		if err != nil {
			t.Fatalf("SignatureStatus() error = %v", err)
		}
		if result.State != SignatureStatusUnsigned {
			t.Fatalf("SignatureStatus().State = %q, want %q (test setup sanity check)", result.State, SignatureStatusUnsigned)
		}
		if result.Signature != nil {
			t.Fatalf("SignatureStatus().Signature = %#v, want nil for an unsigned digest", result.Signature)
		}
	})

	tests := []struct {
		name  string
		seed  func(t *testing.T, service *Service, repository string) string
		trust func(t *testing.T) []string
		want  string
	}{
		{
			name: "unverifiable -- zero usable trusted keys",
			seed: func(t *testing.T, service *Service, repository string) string {
				digest := seedFixtureImageManifest(t, service, repository)
				seedFixtureSignatureArtifact(t, service, repository)
				return digest
			},
			trust: func(t *testing.T) []string { return nil },
			want:  SignatureStatusUnverifiable,
		},
		{
			name: "untrusted",
			seed: func(t *testing.T, service *Service, repository string) string {
				digest := seedFixtureImageManifest(t, service, repository)
				seedFixtureSignatureArtifact(t, service, repository)
				return digest
			},
			trust: func(t *testing.T) []string { return []string{generateTestECDSAP256PublicKeyPEM(t)} },
			want:  SignatureStatusUntrusted,
		},
		{
			name: "mismatched",
			seed: func(t *testing.T, service *Service, repository string) string {
				transplantTarget := seedArbitraryImageManifest(t, service, repository, " - fingerprint transplant target")
				publishFixtureSignatureManifestAt(t, service, repository, transplantTarget)
				return transplantTarget
			},
			trust: func(t *testing.T) []string { return []string{fixtureTrustedKeyPEM(t)} },
			want:  SignatureStatusMismatched,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service, cleanup := newTestService(t, allowAllAccessController{})
			defer cleanup()

			repository := "library/alpine"
			digest := tt.seed(t, service, repository)
			seedSigningPolicy(t, service, true, tt.trust(t))

			result, err := service.SignatureStatus(context.Background(), repository, digest)
			if err != nil {
				t.Fatalf("SignatureStatus() error = %v", err)
			}
			if result.State != tt.want {
				t.Fatalf("SignatureStatus().State = %q, want %q (test setup sanity check)", result.State, tt.want)
			}
			if result.Signature == nil {
				t.Fatal("SignatureStatus().Signature = nil, want a populated detail")
			}
			if result.Signature.VerifiedKeyFingerprint != "" {
				t.Fatalf("SignatureStatus().Signature.VerifiedKeyFingerprint = %q, want empty for state %q", result.Signature.VerifiedKeyFingerprint, tt.want)
			}
		})
	}

	t.Run("verified", func(t *testing.T) {
		t.Parallel()

		service, cleanup := newTestService(t, allowAllAccessController{})
		defer cleanup()

		repository := "library/alpine"
		digest := seedFixtureImageManifest(t, service, repository)
		seedFixtureSignatureArtifact(t, service, repository)
		keyPEM := fixtureTrustedKeyPEM(t)
		seedSigningPolicy(t, service, true, []string{keyPEM})

		result, err := service.SignatureStatus(context.Background(), repository, digest)
		if err != nil {
			t.Fatalf("SignatureStatus() error = %v", err)
		}
		if result.State != SignatureStatusVerified {
			t.Fatalf("SignatureStatus().State = %q, want %q (test setup sanity check)", result.State, SignatureStatusVerified)
		}
		if result.Signature == nil {
			t.Fatal("SignatureStatus().Signature = nil, want a populated detail for a verified signature")
		}
		want := signing.Fingerprint(keyPEM)
		if result.Signature.VerifiedKeyFingerprint != want {
			t.Fatalf("SignatureStatus().Signature.VerifiedKeyFingerprint = %q, want %q", result.Signature.VerifiedKeyFingerprint, want)
		}

		raw, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			t.Fatalf("json.Marshal() error = %v", marshalErr)
		}
		if strings.Contains(string(raw), "BEGIN PUBLIC KEY") {
			t.Fatalf("SignatureStatus JSON = %s, must never contain PEM key material even alongside the fingerprint", raw)
		}
	})
}

// TestServiceSignatureStatusPolicyTrustedKeysIsCountOnlyNeverPEM is the
// Phase 7 RED test (tasks.md 7.3): SignatureStatusPolicy.TrustedKeys is a
// count only -- the serialized JSON response never contains PEM bytes.
func TestServiceSignatureStatusPolicyTrustedKeysIsCountOnlyNeverPEM(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedFixtureImageManifest(t, service, repository)
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	result, err := service.SignatureStatus(context.Background(), repository, digest)
	if err != nil {
		t.Fatalf("SignatureStatus() error = %v", err)
	}
	if result.Policy.TrustedKeys != 1 {
		t.Fatalf("result.Policy.TrustedKeys = %d, want 1", result.Policy.TrustedKeys)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(raw), "BEGIN PUBLIC KEY") {
		t.Fatalf("SignatureStatus JSON = %s, must never contain PEM key material", raw)
	}
}

// TestServiceSignatureStatusIsNeverGatedByThePolicyItReports is the Phase 7
// RED test (tasks.md 7.6, spec.md "Status endpoint is never itself gated"):
// even when the resolved policy would 403 an actual pull of the same
// digest, SignatureStatus still succeeds and reports the blocking verdict --
// it never itself calls enforceSigningPolicy.
func TestServiceSignatureStatusIsNeverGatedByThePolicyItReports(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedFixtureImageManifest(t, service, repository)
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)}) // enabled, no .sig published -> would 403 a pull

	if err := service.enforceSigningPolicy(context.Background(), repository, digest, ""); err == nil || !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("enforceSigningPolicy() error = %v, want a policy violation (test setup sanity check)", err)
	}

	result, err := service.SignatureStatus(context.Background(), repository, digest)
	if err != nil {
		t.Fatalf("SignatureStatus() error = %v, want it to succeed even though a pull of this digest would be blocked", err)
	}
	if result.State != SignatureStatusUnsigned || !result.WouldBlockPull {
		t.Fatalf("SignatureStatus() = %#v, want state=%q would_block_pull=true", result, SignatureStatusUnsigned)
	}
}

// TestSigningPolicyViolationAndSignatureStatusNeverLeakKeyOrSignatureBytes
// is the Phase 7 RED test (tasks.md 7.7, the threat-matrix key-material-
// disclosure case): neither enforceSigningPolicy's 403 PolicyViolation
// message (Phase 5's pull gate) nor SignatureStatus's serialized response
// ever contains PEM key material or the raw base64 signature -- asserted
// across both endpoints, as the threat matrix requires.
func TestSigningPolicyViolationAndSignatureStatusNeverLeakKeyOrSignatureBytes(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedFixtureImageManifest(t, service, repository)
	seedFixtureSignatureArtifact(t, service, repository)
	seedSigningPolicy(t, service, true, []string{generateTestECDSAP256PublicKeyPEM(t)}) // untrusted: unrelated key

	err := service.enforceSigningPolicy(context.Background(), repository, digest, "")
	if err == nil {
		t.Fatal("enforceSigningPolicy() error = nil, want a policy violation")
	}
	if strings.Contains(err.Error(), "BEGIN PUBLIC KEY") || strings.Contains(err.Error(), fixtureSignatureBase64) {
		t.Fatalf("enforceSigningPolicy() error = %q, must never echo key or signature bytes", err.Error())
	}

	result, statusErr := service.SignatureStatus(context.Background(), repository, digest)
	if statusErr != nil {
		t.Fatalf("SignatureStatus() error = %v", statusErr)
	}
	if result.State != SignatureStatusUntrusted {
		t.Fatalf("SignatureStatus().State = %q, want %q (test setup sanity check)", result.State, SignatureStatusUntrusted)
	}
	raw, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		t.Fatalf("json.Marshal() error = %v", marshalErr)
	}
	if strings.Contains(string(raw), "BEGIN PUBLIC KEY") || strings.Contains(string(raw), fixtureSignatureBase64) {
		t.Fatalf("SignatureStatus JSON = %s, must never echo key or signature bytes", raw)
	}
}

// TestServiceSignatureStatusRequiresPullAuthorization proves SignatureStatus
// authorizes with ports.ActionPull, exactly like ScanStatus -- not an admin
// action (design.md Decision 9, spec.md "Caller with pull credentials reads
// status").
func TestServiceSignatureStatusRequiresPullAuthorization(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, ports.NewConfigurableAccessController(ports.AccessConfig{}))
	defer cleanup()

	repository := "library/alpine"
	_, err := service.SignatureStatus(context.Background(), repository, "latest")
	if err == nil {
		t.Fatal("SignatureStatus() error = nil, want an authorization error when access is denied")
	}
}
