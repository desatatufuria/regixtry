package regixtry

import (
	"encoding/json"
	"testing"

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
