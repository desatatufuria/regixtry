package regixtry

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"reflect"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// TestNormalizeTrivyOverrideRejectsUnsafeInput is the Phase 3 RED test
// (tasks.md 3.1) backing design.md Decision 3's argv-safety validation:
// unknown fields, relative paths, and a leading "-" (subprocess argv threat
// matrix row) must all be rejected at the normalize boundary, before a
// payload is ever stored.
func TestNormalizeTrivyOverrideRejectsUnsafeInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{
			name:    "valid absolute paths normalize cleanly",
			raw:     `{"enabled":true,"ignore_file_path":"/etc/regixtry/ignore/alpine.trivyignore","ignore_policy_path":"/etc/regixtry/policy/alpine.rego"}`,
			wantErr: false,
		},
		{
			name:    "enabled only, no paths",
			raw:     `{"enabled":false}`,
			wantErr: false,
		},
		{
			name:    "unknown field is rejected",
			raw:     `{"enabled":true,"config_path":"/etc/regixtry/gitleaks.toml"}`,
			wantErr: true,
		},
		{
			name:    "relative ignore file path is rejected",
			raw:     `{"enabled":true,"ignore_file_path":"relative/alpine.trivyignore"}`,
			wantErr: true,
		},
		{
			name:    "ignore file path starting with dash is rejected",
			raw:     `{"enabled":true,"ignore_file_path":"-oJSON"}`,
			wantErr: true,
		},
		{
			name:    "relative ignore policy path is rejected",
			raw:     `{"enabled":true,"ignore_policy_path":"relative/alpine.rego"}`,
			wantErr: true,
		},
		{
			name:    "ignore policy path starting with dash is rejected",
			raw:     `{"enabled":true,"ignore_policy_path":"-oJSON"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := normalizeTrivyOverride([]byte(tt.raw))
			if tt.wantErr && err == nil {
				t.Fatalf("normalizeTrivyOverride(%s) error = nil, want error", tt.raw)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("normalizeTrivyOverride(%s) error = %v, want nil", tt.raw, err)
			}
			if tt.wantErr && !domain.IsCode(err, domain.ErrorCodeValidation) {
				t.Fatalf("normalizeTrivyOverride(%s) error = %v, want ErrorCodeValidation", tt.raw, err)
			}
		})
	}
}

// TestNormalizeGitleaksOverrideRejectsUnsafeInput is the Phase 3 RED test
// (tasks.md 3.2), the gitleaks mirror of
// TestNormalizeTrivyOverrideRejectsUnsafeInput for ConfigPath.
func TestNormalizeGitleaksOverrideRejectsUnsafeInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{
			name:    "valid absolute config path normalizes cleanly",
			raw:     `{"enabled":true,"config_path":"/etc/regixtry/gitleaks/alpine.toml"}`,
			wantErr: false,
		},
		{
			name:    "enabled only, no path",
			raw:     `{"enabled":false}`,
			wantErr: false,
		},
		{
			name:    "unknown field is rejected",
			raw:     `{"enabled":true,"ignore_file_path":"/etc/regixtry/ignore/alpine.trivyignore"}`,
			wantErr: true,
		},
		{
			name:    "relative config path is rejected",
			raw:     `{"enabled":true,"config_path":"relative/alpine.toml"}`,
			wantErr: true,
		},
		{
			name:    "config path starting with dash is rejected",
			raw:     `{"enabled":true,"config_path":"-oJSON"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := normalizeGitleaksOverride([]byte(tt.raw))
			if tt.wantErr && err == nil {
				t.Fatalf("normalizeGitleaksOverride(%s) error = nil, want error", tt.raw)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("normalizeGitleaksOverride(%s) error = %v, want nil", tt.raw, err)
			}
			if tt.wantErr && !domain.IsCode(err, domain.ErrorCodeValidation) {
				t.Fatalf("normalizeGitleaksOverride(%s) error = %v, want ErrorCodeValidation", tt.raw, err)
			}
		})
	}
}

// TestApplyTrivyOverrideAppliesRoundTripAndTolerance is the Phase 3 RED test
// (tasks.md 3.3): Apply must round-trip a payload into ScanSettings, and a
// payload with an unknown extra field (as a newer binary might write) must
// still apply rather than fail — design.md Decision 3's documented
// strict-in/lenient-out asymmetry, which the proposal's rollback plan
// depends on.
func TestApplyTrivyOverrideAppliesRoundTripAndTolerance(t *testing.T) {
	t.Parallel()

	base := ports.ScanSettings{Enabled: true, MaxConcurrency: 3}

	tests := []struct {
		name string
		raw  string
		want ports.ScanSettings
	}{
		{
			name: "round trips enabled and paths",
			raw:  `{"enabled":false,"ignore_file_path":"/etc/regixtry/ignore/alpine.trivyignore","ignore_policy_path":"/etc/regixtry/policy/alpine.rego"}`,
			want: ports.ScanSettings{Enabled: false, MaxConcurrency: 3, IgnoreFilePath: "/etc/regixtry/ignore/alpine.trivyignore", IgnorePolicyPath: "/etc/regixtry/policy/alpine.rego"},
		},
		{
			name: "unknown extra field still applies",
			raw:  `{"enabled":true,"ignore_file_path":"/etc/regixtry/ignore/alpine.trivyignore","future_field":"unused-by-this-binary"}`,
			want: ports.ScanSettings{Enabled: true, MaxConcurrency: 3, IgnoreFilePath: "/etc/regixtry/ignore/alpine.trivyignore"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := applyTrivyOverride([]byte(tt.raw), base)
			if err != nil {
				t.Fatalf("applyTrivyOverride() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("applyTrivyOverride() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestApplyGitleaksOverrideAppliesRoundTripAndTolerance mirrors
// TestApplyTrivyOverrideAppliesRoundTripAndTolerance for the gitleaks codec.
func TestApplyGitleaksOverrideAppliesRoundTripAndTolerance(t *testing.T) {
	t.Parallel()

	base := ports.ScanSettings{Enabled: true, MaxConcurrency: 2}

	tests := []struct {
		name string
		raw  string
		want ports.ScanSettings
	}{
		{
			name: "round trips enabled and config path",
			raw:  `{"enabled":false,"config_path":"/etc/regixtry/gitleaks/alpine.toml"}`,
			want: ports.ScanSettings{Enabled: false, MaxConcurrency: 2, ConfigPath: "/etc/regixtry/gitleaks/alpine.toml"},
		},
		{
			name: "unknown extra field still applies",
			raw:  `{"enabled":true,"config_path":"/etc/regixtry/gitleaks/alpine.toml","future_field":"unused-by-this-binary"}`,
			want: ports.ScanSettings{Enabled: true, MaxConcurrency: 2, ConfigPath: "/etc/regixtry/gitleaks/alpine.toml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := applyGitleaksOverride([]byte(tt.raw), base)
			if err != nil {
				t.Fatalf("applyGitleaksOverride() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("applyGitleaksOverride() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestRepositoryOverrideCodecsRegistryHasBothFeatures pins the registry's
// key set so a future feature addition (or accidental removal) is caught
// here rather than only downstream in applyRepositoryOverride.
func TestRepositoryOverrideCodecsRegistryHasBothFeatures(t *testing.T) {
	t.Parallel()

	for _, feature := range []string{trivyFeatureName, gitleaksFeatureName} {
		codec, ok := repositoryOverrideCodecs[feature]
		if !ok {
			t.Fatalf("repositoryOverrideCodecs[%q] missing", feature)
		}
		if codec.Normalize == nil || codec.Apply == nil {
			t.Fatalf("repositoryOverrideCodecs[%q] = %#v, want both Normalize and Apply set", feature, codec)
		}
	}
	if _, ok := repositoryOverrideCodecs["image-signing"]; ok {
		t.Fatalf("repositoryOverrideCodecs contains an unexpected feature entry")
	}
}

// TestResolveRepositoryOverrideNotFoundReturnsSettingsUnchanged is the Phase
// 3 RED test (tasks.md 3.1): the generic resolveRepositoryOverride[T] must
// return the passed-in settings unchanged when
// GetRepositoryFeatureOverride resolves NotFound, exercised for both type
// instantiations design.md Decision 5 requires: T=ports.ScanSettings
// (trivy/gitleaks's target type) and T=ports.SigningPolicySettings
// (signing's own target type).
func TestResolveRepositoryOverrideNotFoundReturnsSettingsUnchanged(t *testing.T) {
	t.Parallel()

	t.Run("T=ports.ScanSettings", func(t *testing.T) {
		t.Parallel()

		service, cleanup := newTestService(t, allowAllAccessController{})
		defer cleanup()

		settings := ports.ScanSettings{Enabled: true, MaxConcurrency: 4}
		got, err := resolveRepositoryOverride(context.Background(), service, "tenant-a", "library/alpine", trivyFeatureName, settings, applyTrivyOverride)
		if err != nil {
			t.Fatalf("resolveRepositoryOverride() error = %v", err)
		}
		if got != settings {
			t.Fatalf("resolveRepositoryOverride() = %#v, want unchanged %#v", got, settings)
		}
	})

	t.Run("T=ports.SigningPolicySettings", func(t *testing.T) {
		t.Parallel()

		service, cleanup := newTestService(t, allowAllAccessController{})
		defer cleanup()

		settings := ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: []string{"pem-1"}}
		got, err := resolveRepositoryOverride(context.Background(), service, "tenant-a", "library/alpine", "signing", settings, applySigningOverridePayload)
		if err != nil {
			t.Fatalf("resolveRepositoryOverride() error = %v", err)
		}
		if !reflect.DeepEqual(got, settings) {
			t.Fatalf("resolveRepositoryOverride() = %#v, want unchanged %#v", got, settings)
		}
	})
}

// TestResolveRepositoryOverrideNilApplyReturnsSettingsUnchanged is the Phase
// 3 RED test (tasks.md 3.2): when apply is nil, resolveRepositoryOverride
// returns settings unchanged even though a row exists — reproducing today's
// `if !ok { return settings, nil }` branch (repository_overrides.go:140-143)
// for a codec that registers Normalize only (design.md Decision 5).
func TestResolveRepositoryOverrideNilApplyReturnsSettingsUnchanged(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	payload := marshalOverride(t, ports.TrivyOverride{Enabled: false})
	if err := service.metadata.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", trivyFeatureName, payload); err != nil {
		t.Fatalf("UpsertRepositoryFeatureOverride() error = %v", err)
	}

	settings := ports.ScanSettings{Enabled: true, MaxConcurrency: 4}
	got, err := resolveRepositoryOverride(context.Background(), service, "tenant-a", "library/alpine", trivyFeatureName, settings, nil)
	if err != nil {
		t.Fatalf("resolveRepositoryOverride() error = %v", err)
	}
	if got != settings {
		t.Fatalf("resolveRepositoryOverride() = %#v, want unchanged %#v (apply==nil, row present)", got, settings)
	}
}

// generateTestECDSAP256PublicKeyPEM produces a fresh, real-newline PEM
// public key that signing.NormalizePublicKeyPEM (called internally by
// normalizeSigningOverride) accepts — a distinct key per call, this
// package's own equivalent of internal/domain/signing's test fixture.
func generateTestECDSAP256PublicKeyPEM(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey() error = %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("x509.MarshalPKIXPublicKey() error = %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// TestNormalizeSigningOverrideRejectsUnsafeInput is the Phase 3 RED test
// (tasks.md 3.6): unknown fields, a key that fails
// signing.NormalizePublicKeyPEM, and enabled:true with zero usable keys (the
// outage rule, Decision 7's mitigation applied at write time) must all be
// rejected before a payload is ever stored — the same argv-safety posture
// normalizeTrivyOverride/normalizeGitleaksOverride already enforce for their
// own fields.
func TestNormalizeSigningOverrideRejectsUnsafeInput(t *testing.T) {
	t.Parallel()

	validKey := generateTestECDSAP256PublicKeyPEM(t)

	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{
			name:    "enabled with one valid key normalizes cleanly",
			raw:     `{"enabled":true,"trusted_public_keys":["` + escapeJSONString(validKey) + `"]}`,
			wantErr: false,
		},
		{
			name:    "disabled with no keys normalizes cleanly",
			raw:     `{"enabled":false}`,
			wantErr: false,
		},
		{
			name:    "unknown field is rejected",
			raw:     `{"enabled":true,"config_path":"/etc/regixtry/gitleaks.toml"}`,
			wantErr: true,
		},
		{
			name:    "unparseable key is rejected",
			raw:     `{"enabled":false,"trusted_public_keys":["not a pem at all"]}`,
			wantErr: true,
		},
		{
			name:    "enabled true with zero keys is rejected (outage rule)",
			raw:     `{"enabled":true}`,
			wantErr: true,
		},
		{
			name:    "enabled true with an empty keys list is rejected (outage rule)",
			raw:     `{"enabled":true,"trusted_public_keys":[]}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := normalizeSigningOverride([]byte(tt.raw))
			if tt.wantErr && err == nil {
				t.Fatalf("normalizeSigningOverride(%s) error = nil, want error", tt.raw)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("normalizeSigningOverride(%s) error = %v, want nil", tt.raw, err)
			}
			if tt.wantErr && !domain.IsCode(err, domain.ErrorCodeValidation) {
				t.Fatalf("normalizeSigningOverride(%s) error = %v, want ErrorCodeValidation", tt.raw, err)
			}
		})
	}
}

// escapeJSONString is a tiny test helper: a generated PEM contains real
// newlines, which must be escaped to embed it inside a JSON string literal
// built by hand in this file's table-driven raw payloads.
func escapeJSONString(s string) string {
	escaped, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	// json.Marshal(string) already produces a quoted JSON string literal;
	// strip the surrounding quotes so callers can splice it inside their own
	// hand-written `"..."` literal.
	return string(escaped[1 : len(escaped)-1])
}

// TestApplySigningOverridePayloadAppliesRoundTripAndTolerance is the Phase 3
// RED test (tasks.md 3.7): applySigningOverridePayload must round-trip a
// payload into ports.SigningPolicySettings via plain json.Unmarshal,
// preserving the base settings' UpdatedAt (untouched by the override, mirrors
// applyTrivyOverride leaving MaxConcurrency alone) and tolerating an unknown
// extra field — the same strict-in/lenient-out asymmetry documented at
// repository_overrides.go:101-106.
func TestApplySigningOverridePayloadAppliesRoundTripAndTolerance(t *testing.T) {
	t.Parallel()

	fixedUpdatedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := ports.SigningPolicySettings{Enabled: false, UpdatedAt: fixedUpdatedAt}

	tests := []struct {
		name string
		raw  string
		want ports.SigningPolicySettings
	}{
		{
			name: "round trips enabled and keys",
			raw:  `{"enabled":true,"trusted_public_keys":["pem-1","pem-2"]}`,
			want: ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: []string{"pem-1", "pem-2"}, UpdatedAt: fixedUpdatedAt},
		},
		{
			name: "unknown extra field still applies",
			raw:  `{"enabled":true,"trusted_public_keys":["pem-1"],"future_field":"unused-by-this-binary"}`,
			want: ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: []string{"pem-1"}, UpdatedAt: fixedUpdatedAt},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := applySigningOverridePayload([]byte(tt.raw), base)
			if err != nil {
				t.Fatalf("applySigningOverridePayload() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("applySigningOverridePayload() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// marshalOverride is a small test helper to avoid repeating json.Marshal
// error-handling boilerplate across the RED tests in this file.
func marshalOverride(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return raw
}

// TestServiceGetSetClearRepositoryOverrideLifecycle is a Phase 7 RED test
// (tasks.md 7.7) for the feature-agnostic Service methods the admin HTTP
// resource wires onto (design.md Decision 7): Get on an absent row is
// NotFound, Set normalizes through the codec registry before persisting and
// round-trips through Get, and Clear removes the row so a later Get is
// NotFound again.
func TestServiceGetSetClearRepositoryOverrideLifecycle(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	ctx := context.Background()

	if _, err := service.GetRepositoryOverride(ctx, "library/alpine", trivyFeatureName); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetRepositoryOverride() error = %v, want ErrorCodeNotFound before any override is stored", err)
	}

	raw := marshalOverride(t, ports.TrivyOverride{Enabled: true, IgnoreFilePath: "/etc/regixtry/ignore/alpine.trivyignore"})
	set, err := service.SetRepositoryOverride(ctx, "library/alpine", trivyFeatureName, raw)
	if err != nil {
		t.Fatalf("SetRepositoryOverride() error = %v", err)
	}
	if set.Repository != "library/alpine" || set.Feature != trivyFeatureName {
		t.Fatalf("SetRepositoryOverride() = %#v, want repository/feature echoed", set)
	}
	var setPayload ports.TrivyOverride
	if err := json.Unmarshal(set.Payload, &setPayload); err != nil {
		t.Fatalf("json.Unmarshal(set.Payload) error = %v", err)
	}
	if !setPayload.Enabled || setPayload.IgnoreFilePath != "/etc/regixtry/ignore/alpine.trivyignore" {
		t.Fatalf("SetRepositoryOverride() payload = %#v, want the normalized override", setPayload)
	}

	got, err := service.GetRepositoryOverride(ctx, "library/alpine", trivyFeatureName)
	if err != nil {
		t.Fatalf("GetRepositoryOverride() error = %v", err)
	}
	var gotPayload ports.TrivyOverride
	if err := json.Unmarshal(got.Payload, &gotPayload); err != nil {
		t.Fatalf("json.Unmarshal(got.Payload) error = %v", err)
	}
	if gotPayload != setPayload {
		t.Fatalf("GetRepositoryOverride() payload = %#v, want %#v (round trip)", gotPayload, setPayload)
	}

	if err := service.ClearRepositoryOverride(ctx, "library/alpine", trivyFeatureName); err != nil {
		t.Fatalf("ClearRepositoryOverride() error = %v", err)
	}
	if _, err := service.GetRepositoryOverride(ctx, "library/alpine", trivyFeatureName); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetRepositoryOverride() error = %v, want ErrorCodeNotFound after Clear", err)
	}
}

// TestServiceSetRepositoryOverrideAppliesCodecNormalizeBeforeStoring is a
// Phase 7 RED test (tasks.md 7.7): SetRepositoryOverride MUST run the
// payload through the registered codec's Normalize before persisting, so an
// argv-unsafe path (design.md Decision 3's threat matrix) is rejected and
// never reaches storage — the HTTP resource must not bypass the registry.
func TestServiceSetRepositoryOverrideAppliesCodecNormalizeBeforeStoring(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	ctx := context.Background()

	raw := marshalOverride(t, ports.TrivyOverride{Enabled: true, IgnoreFilePath: "relative/alpine.trivyignore"})
	if _, err := service.SetRepositoryOverride(ctx, "library/alpine", trivyFeatureName, raw); !domain.IsCode(err, domain.ErrorCodeValidation) {
		t.Fatalf("SetRepositoryOverride() error = %v, want ErrorCodeValidation for a relative ignore_file_path", err)
	}
	if _, err := service.GetRepositoryOverride(ctx, "library/alpine", trivyFeatureName); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetRepositoryOverride() error = %v, want ErrorCodeNotFound, rejected payload must never be stored", err)
	}
}

// TestServiceSetGetClearRepositoryOverrideUnknownFeatureIsNotFound is a
// Phase 7 RED test (tasks.md 7.5, 7.7): an unrecognized feature name must be
// rejected via the codec-registry lookup with a typed NotFound, not a panic
// or a silent no-op, across all three mutating/reading entry points.
func TestServiceSetGetClearRepositoryOverrideUnknownFeatureIsNotFound(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	ctx := context.Background()

	if _, err := service.GetRepositoryOverride(ctx, "library/alpine", "image-signing"); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetRepositoryOverride() error = %v, want ErrorCodeNotFound for an unknown feature", err)
	}
	if _, err := service.SetRepositoryOverride(ctx, "library/alpine", "image-signing", marshalOverride(t, map[string]any{"enabled": true})); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("SetRepositoryOverride() error = %v, want ErrorCodeNotFound for an unknown feature", err)
	}
	if err := service.ClearRepositoryOverride(ctx, "library/alpine", "image-signing"); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("ClearRepositoryOverride() error = %v, want ErrorCodeNotFound for an unknown feature", err)
	}
}

// TestServiceClearRepositoryOverrideRevertsResolutionToGlobalImmediately is
// the Phase 7 RED test the tasks.md 7.4 evidence note calls for: proving the
// DELETE effect through the actual resolution call path
// (applyRepositoryOverride), not merely asserting the row is gone. It seeds
// a disabling override while the global row stays enabled, confirms
// resolution honors the override, clears it through the same
// ClearRepositoryOverride the HTTP DELETE handler calls, and confirms the
// very next resolution call reverts to the global row in full.
func TestServiceClearRepositoryOverrideRevertsResolutionToGlobalImmediately(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	ctx := context.Background()
	global := ports.ScanSettings{Enabled: true, MaxConcurrency: 4}

	raw := marshalOverride(t, ports.TrivyOverride{Enabled: false})
	if _, err := service.SetRepositoryOverride(ctx, "library/alpine", trivyFeatureName, raw); err != nil {
		t.Fatalf("SetRepositoryOverride() error = %v", err)
	}

	overridden, err := service.applyRepositoryOverride(ctx, "tenant-a", "library/alpine", trivyFeatureName, global)
	if err != nil {
		t.Fatalf("applyRepositoryOverride() error = %v", err)
	}
	if overridden.Enabled {
		t.Fatalf("applyRepositoryOverride() = %#v, want the override's Enabled=false to apply", overridden)
	}

	if err := service.ClearRepositoryOverride(ctx, "library/alpine", trivyFeatureName); err != nil {
		t.Fatalf("ClearRepositoryOverride() error = %v", err)
	}

	reverted, err := service.applyRepositoryOverride(ctx, "tenant-a", "library/alpine", trivyFeatureName, global)
	if err != nil {
		t.Fatalf("applyRepositoryOverride() error = %v", err)
	}
	if reverted != global {
		t.Fatalf("applyRepositoryOverride() after Clear = %#v, want unchanged global %#v", reverted, global)
	}
}
