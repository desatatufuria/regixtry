package regixtry

import (
	"context"
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
