package regixtry

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// repositoryOverrideCodec is the generic-to-typed boundary for one feature's
// stored override payload (design.md Decision 3). Normalize is the only
// place an inbound request body becomes the exact bytes persisted; Apply is
// the only place a stored []byte becomes a typed struct.
type repositoryOverrideCodec struct {
	// Normalize strictly decodes, validates, and re-marshals an inbound
	// payload into the exact bytes stored. Unknown fields are rejected, so a
	// gitleaks body PUT at the trivy resource is a 400, not a silent no-op.
	Normalize func(raw []byte) ([]byte, error)
	// Apply is the ONLY place a stored []byte becomes a typed struct. It
	// layers the override onto the feature's resolved global ScanSettings.
	Apply func(raw []byte, settings ports.ScanSettings) (ports.ScanSettings, error)
}

// repositoryOverrideCodecs keys a codec per feature by the same feature-name
// constants that already key scan_settings/feature_runtime_state. Adding a
// future feature (e.g. image signing) is exactly one map entry plus one
// struct in ports — nothing in the store, resolution, HTTP dispatch, or TUI
// plumbing is feature-aware.
var repositoryOverrideCodecs = map[string]repositoryOverrideCodec{
	trivyFeatureName:    {Normalize: normalizeTrivyOverride, Apply: applyTrivyOverride},
	gitleaksFeatureName: {Normalize: normalizeGitleaksOverride, Apply: applyGitleaksOverride},
}

// strictDecodeOverride mirrors decodeAdminJSON's DisallowUnknownFields
// posture (admin_handlers.go), so a payload written for one feature can
// never be silently accepted at another feature's resource.
func strictDecodeOverride(raw []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return domain.NewValidationError("override payload is invalid: " + err.Error())
	}
	return nil
}

// normalizeOverridePath enforces the argv-safety rules from design.md's
// threat matrix: an empty path means "not set" and is left alone; every
// non-empty path must be absolute and is filepath.Clean-ed, and a path
// beginning with "-" is rejected so a stored value can never be reparsed as
// a flag once it reaches a runner's argv. This is validation, not existence
// checking — existence is enforced by each runner's pre-flight (Decision 6).
func normalizeOverridePath(label string, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if strings.HasPrefix(trimmed, "-") {
		return "", domain.NewValidationError(label + " must not begin with \"-\"")
	}
	if !filepath.IsAbs(trimmed) {
		return "", domain.NewValidationError(label + " must be an absolute path")
	}
	return filepath.Clean(trimmed), nil
}

func normalizeTrivyOverride(raw []byte) ([]byte, error) {
	var override ports.TrivyOverride
	if err := strictDecodeOverride(raw, &override); err != nil {
		return nil, err
	}
	ignoreFilePath, err := normalizeOverridePath("ignore_file_path", override.IgnoreFilePath)
	if err != nil {
		return nil, err
	}
	ignorePolicyPath, err := normalizeOverridePath("ignore_policy_path", override.IgnorePolicyPath)
	if err != nil {
		return nil, err
	}
	override.IgnoreFilePath = ignoreFilePath
	override.IgnorePolicyPath = ignorePolicyPath
	return json.Marshal(override)
}

func normalizeGitleaksOverride(raw []byte) ([]byte, error) {
	var override ports.GitleaksOverride
	if err := strictDecodeOverride(raw, &override); err != nil {
		return nil, err
	}
	configPath, err := normalizeOverridePath("config_path", override.ConfigPath)
	if err != nil {
		return nil, err
	}
	override.ConfigPath = configPath
	return json.Marshal(override)
}

// applyTrivyOverride and applyGitleaksOverride deliberately use plain
// json.Unmarshal, not strictDecodeOverride's DisallowUnknownFields: a
// payload written by a newer binary carrying a field this binary does not
// know must still resolve instead of hard-failing every scan after a
// rollback (design.md Decision 3, the proposal's rollback plan depends on
// this asymmetry).
func applyTrivyOverride(raw []byte, settings ports.ScanSettings) (ports.ScanSettings, error) {
	var override ports.TrivyOverride
	if err := json.Unmarshal(raw, &override); err != nil {
		return ports.ScanSettings{}, domain.NewValidationError("stored trivy override is unreadable")
	}
	settings.Enabled = override.Enabled
	settings.IgnoreFilePath = override.IgnoreFilePath
	settings.IgnorePolicyPath = override.IgnorePolicyPath
	return settings, nil
}

func applyGitleaksOverride(raw []byte, settings ports.ScanSettings) (ports.ScanSettings, error) {
	var override ports.GitleaksOverride
	if err := json.Unmarshal(raw, &override); err != nil {
		return ports.ScanSettings{}, domain.NewValidationError("stored gitleaks override is unreadable")
	}
	settings.Enabled = override.Enabled
	settings.ConfigPath = override.ConfigPath
	return settings, nil
}

// applyRepositoryOverride is the row-presence boundary: a row for (tenant,
// repository, feature) replaces the feature's global settings in full;
// NotFound means "use the global row", never an error (design.md
// Decision 4).
func (s *Service) applyRepositoryOverride(ctx context.Context, tenant string, repository string, feature string, settings ports.ScanSettings) (ports.ScanSettings, error) {
	raw, err := s.metadata.GetRepositoryFeatureOverride(ctx, tenant, repository, feature)
	if err != nil {
		if domain.IsCode(err, domain.ErrorCodeNotFound) {
			return settings, nil
		}
		return ports.ScanSettings{}, err
	}
	codec, ok := repositoryOverrideCodecs[feature]
	if !ok {
		return settings, nil
	}
	return codec.Apply(raw, settings)
}
