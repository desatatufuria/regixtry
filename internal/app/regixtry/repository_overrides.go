package regixtry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
	"regixtry/internal/ports"
)

// signingFeatureName is the feature-name constant image signing's override
// codec and pull-time gate key against, the same identity space as
// trivyFeatureName/gitleaksFeatureName (feature_registry.go). Declared here
// because this file is where it is first needed (repositoryOverrideCodecs);
// Phase 4's service_signing.go reconciles against this declaration rather
// than redeclaring it.
const signingFeatureName = "signing"

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
	// signing's override targets ports.SigningPolicySettings, not
	// ScanSettings, so it has no ScanSettings-shaped Apply. Its Apply is
	// passed explicitly to resolveRepositoryOverride by
	// applySigningRepositoryOverride (design.md Decision 5).
	signingFeatureName: {Normalize: normalizeSigningOverride},
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

// normalizeSigningOverride runs every key through
// signing.NormalizePublicKeyPEM and rejects enabled:true with zero usable
// keys — Decision 7's outage rule applied at write time, so the
// guaranteed-total-outage configuration is unrepresentable rather than
// merely discouraged. Errors never echo key bytes, only the offending index.
func normalizeSigningOverride(raw []byte) ([]byte, error) {
	var override ports.SigningOverride
	if err := strictDecodeOverride(raw, &override); err != nil {
		return nil, err
	}
	normalizedKeys := make([]string, 0, len(override.TrustedPublicKeys))
	for index, key := range override.TrustedPublicKeys {
		normalized, err := signing.NormalizePublicKeyPEM(key)
		if err != nil {
			return nil, domain.NewValidationError(fmt.Sprintf("trusted_public_keys[%d] is invalid: %s", index, err.Error()))
		}
		normalizedKeys = append(normalizedKeys, normalized)
	}
	if override.Enabled && len(normalizedKeys) == 0 {
		return nil, domain.NewValidationError("enabled requires at least one usable entry in trusted_public_keys")
	}
	override.TrustedPublicKeys = normalizedKeys
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

// applySigningOverridePayload is signing's own Apply, targeting
// ports.SigningPolicySettings rather than ports.ScanSettings — it cannot be
// registered as repositoryOverrideCodec.Apply (design.md Decision 5) and is
// instead passed explicitly to resolveRepositoryOverride by
// applySigningRepositoryOverride. Like applyTrivyOverride/
// applyGitleaksOverride, it deliberately uses plain json.Unmarshal, not
// strictDecodeOverride's DisallowUnknownFields: a payload written by a newer
// binary carrying a field this binary does not know must still resolve
// instead of hard-failing every pull after a rollback.
func applySigningOverridePayload(raw []byte, settings ports.SigningPolicySettings) (ports.SigningPolicySettings, error) {
	var override ports.SigningOverride
	if err := json.Unmarshal(raw, &override); err != nil {
		return ports.SigningPolicySettings{}, domain.NewValidationError("stored signing override is unreadable")
	}
	settings.Enabled = override.Enabled
	settings.TrustedPublicKeys = override.TrustedPublicKeys
	return settings, nil
}

// resolveRepositoryOverride is the row-presence boundary, generic over the
// settings type a feature's override targets (design.md Decision 5). A row
// replaces the feature's global settings in full; NotFound means "use the
// global row", never an error. Type parameters are illegal on methods, so
// this is a free function over *Service, and applyRepositoryOverride stays a
// non-generic method that delegates to it.
func resolveRepositoryOverride[T any](
	ctx context.Context, s *Service,
	tenant, repository, feature string,
	settings T, apply func([]byte, T) (T, error),
) (T, error) {
	raw, err := s.metadata.GetRepositoryFeatureOverride(ctx, tenant, repository, feature)
	if err != nil {
		if domain.IsCode(err, domain.ErrorCodeNotFound) {
			return settings, nil
		}
		var zero T
		return zero, err
	}
	if apply == nil {
		return settings, nil
	}
	return apply(raw, settings)
}

// applyRepositoryOverride is the row-presence boundary for the Trivy/gitleaks
// target type: a row for (tenant, repository, feature) replaces the
// feature's global settings in full; NotFound means "use the global row",
// never an error (design.md Decision 4). UNCHANGED SIGNATURE — all 6 call
// sites in service_scanning.go stay untouched (design.md Decision 5).
func (s *Service) applyRepositoryOverride(ctx context.Context, tenant string, repository string, feature string, settings ports.ScanSettings) (ports.ScanSettings, error) {
	return resolveRepositoryOverride(ctx, s, tenant, repository, feature, settings, repositoryOverrideCodecs[feature].Apply)
}

// applySigningRepositoryOverride is resolveRepositoryOverride's sibling for
// signing's own target type, ports.SigningPolicySettings — not
// ports.ScanSettings, so it cannot share applyRepositoryOverride's signature
// (design.md Decision 5).
func (s *Service) applySigningRepositoryOverride(ctx context.Context, tenant string, repository string, policy ports.SigningPolicySettings) (ports.SigningPolicySettings, error) {
	return resolveRepositoryOverride(ctx, s, tenant, repository, signingFeatureName, policy, applySigningOverridePayload)
}

// GetRepositoryOverride, ListRepositoryOverrides, SetRepositoryOverride, and
// ClearRepositoryOverride are the feature-agnostic Service methods the admin
// HTTP resource wires onto (design.md Decision 7). Feature knowledge lives
// only in repositoryOverrideCodecs — an unrecognized feature name is a
// typed NotFound at every one of these entry points, the same
// codec-registry lookup applyRepositoryOverride uses defensively.

// GetRepositoryOverride returns one repository's stored override for a
// feature, or a typed NotFound when no row exists — the row-presence
// boundary made explicit on the wire (spec.md "Reading indicates no
// override is set").
func (s *Service) GetRepositoryOverride(ctx context.Context, repository string, feature string) (ports.RepositoryFeatureOverride, error) {
	ref, err := parseRepository(repository)
	if err != nil {
		return ports.RepositoryFeatureOverride{}, err
	}
	if _, ok := repositoryOverrideCodecs[feature]; !ok {
		return ports.RepositoryFeatureOverride{}, domain.NewNotFoundError("feature", feature)
	}
	overrides, err := s.metadata.ListRepositoryFeatureOverrides(ctx, s.tenant(ctx), feature)
	if err != nil {
		return ports.RepositoryFeatureOverride{}, err
	}
	for _, override := range overrides {
		if override.Repository == ref.Name {
			return override, nil
		}
	}
	return ports.RepositoryFeatureOverride{}, domain.NewNotFoundError("repository_feature_override", ref.Name+"/"+feature)
}

// ListRepositoryOverrides returns every stored override row for a feature —
// the list/API projection backing the admin collection resource and Phase
// 8's TUI row annotation.
func (s *Service) ListRepositoryOverrides(ctx context.Context, feature string) ([]ports.RepositoryFeatureOverride, error) {
	if _, ok := repositoryOverrideCodecs[feature]; !ok {
		return nil, domain.NewNotFoundError("feature", feature)
	}
	return s.metadata.ListRepositoryFeatureOverrides(ctx, s.tenant(ctx), feature)
}

// SetRepositoryOverride replaces one repository's override for a feature in
// full. It normalizes the inbound raw payload through the registered
// codec's Normalize before persisting — the HTTP resource must never bypass
// this validation boundary (design.md Decision 3's argv-safety rules).
func (s *Service) SetRepositoryOverride(ctx context.Context, repository string, feature string, raw []byte) (ports.RepositoryFeatureOverride, error) {
	ref, err := parseRepository(repository)
	if err != nil {
		return ports.RepositoryFeatureOverride{}, err
	}
	codec, ok := repositoryOverrideCodecs[feature]
	if !ok {
		return ports.RepositoryFeatureOverride{}, domain.NewNotFoundError("feature", feature)
	}
	normalized, err := codec.Normalize(raw)
	if err != nil {
		return ports.RepositoryFeatureOverride{}, err
	}
	if err := s.metadata.UpsertRepositoryFeatureOverride(ctx, s.tenant(ctx), ref.Name, feature, normalized); err != nil {
		return ports.RepositoryFeatureOverride{}, err
	}
	return s.GetRepositoryOverride(ctx, ref.Name, feature)
}

// ClearRepositoryOverride removes one repository's override for a feature,
// reverting it to the global settings row immediately: the very next
// applyRepositoryOverride resolution call for this (repository, feature)
// pair sees the NotFound row-presence boundary and falls back to the global
// row in full (spec.md "Deleting reverts to global settings").
func (s *Service) ClearRepositoryOverride(ctx context.Context, repository string, feature string) error {
	ref, err := parseRepository(repository)
	if err != nil {
		return err
	}
	if _, ok := repositoryOverrideCodecs[feature]; !ok {
		return domain.NewNotFoundError("feature", feature)
	}
	return s.metadata.DeleteRepositoryFeatureOverride(ctx, s.tenant(ctx), ref.Name, feature)
}
