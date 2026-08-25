package regixtryhttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	stdhttp "net/http"
	domainauth "regixtry/internal/domain/auth"
	domainregistry "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
	"regixtry/internal/ports"
)

func (r *Router) handleAdmin(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	if r.admin == nil || r.auth == nil {
		writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
		return
	}

	subpath := strings.Trim(strings.TrimPrefix(req.URL.Path, "/admin/v1"), "/")

	// The ONLY delegate-eligible namespace (design.md Decision 3). It is an
	// explicit opt-in prefix that returns early; authority is decided in the
	// service layer, which already has the repository argument. Everything
	// else — including every future case added to the switch below — stays
	// under requireAdminPrincipal by construction. A new route can only
	// become delegate-eligible by being added to this allow-list on
	// purpose. This check deliberately runs BEFORE the subpath == "" 404
	// below, but AFTER it would have run relative to requireAdminPrincipal
	// in the naive ordering: putting the empty-subpath 404 first would
	// return 404 for an unauthenticated call to the bare "/admin/v1" mount,
	// which would silently break the unchanged "Missing or invalid bearer
	// token" scenario (operator-admin-http-api spec) that requires 401.
	if strings.HasPrefix(subpath, "repositories/") {
		principal, ok := r.requireAuthenticatedPrincipal(w, req)
		if !ok {
			return
		}
		r.handleAdminRepositoryResource(w, req, *principal, strings.TrimPrefix(subpath, "repositories/"))
		return
	}

	principal, ok := r.requireAdminPrincipal(w, req)
	if !ok {
		return
	}

	if subpath == "" {
		writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
		return
	}

	switch {
	case subpath == "gc/reports":
		r.handleAdminGCReports(w, req)
	case strings.HasPrefix(subpath, "gc/reports/"):
		r.handleAdminGCReportResource(w, req, strings.TrimPrefix(subpath, "gc/reports/"))
	case subpath == "features":
		r.handleAdminFeaturesCollection(w, req)
	case strings.HasPrefix(subpath, "features/"):
		r.handleAdminFeatureResource(w, req, strings.TrimPrefix(subpath, "features/"))
	case subpath == "scan-settings":
		r.handleAdminScanSettings(w, req)
	case subpath == "scan-policy":
		r.handleAdminScanPolicy(w, req)
	case subpath == "signing-policy":
		r.handleAdminSigningPolicy(w, req)
	case subpath == "signing-policy/key-usage":
		r.handleAdminSigningKeyUsage(w, req)
	case subpath == "scan-runs":
		r.handleAdminScanRuns(w, req)
	case subpath == "repository-scan-summaries":
		r.handleAdminRepositoryScanSummaries(w, req)
	case strings.HasPrefix(subpath, "scan-runs/"):
		r.handleAdminScanRunDetail(w, req, strings.TrimPrefix(subpath, "scan-runs/"))
	case subpath == "secret-scan-findings":
		r.handleAdminSecretScanFindings(w, req)
	case subpath == "users":
		r.handleAdminUsersCollection(w, req, *principal)
	case strings.HasPrefix(subpath, "users/"):
		r.handleAdminUserResource(w, req, *principal, strings.TrimPrefix(subpath, "users/"))
	case subpath == "robots":
		r.handleAdminRobotsCollection(w, req, *principal)
	case strings.HasPrefix(subpath, "robots/"):
		r.handleAdminRobotResource(w, req, *principal, strings.TrimPrefix(subpath, "robots/"))
	default:
		writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
	}
}

func (r *Router) handleAdminFeaturesCollection(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	if req.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}
	features, err := r.service.ListFeatures(req.Context())
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}
	writeJSON(w, stdhttp.StatusOK, features)
}

func (r *Router) handleAdminFeatureResource(w stdhttp.ResponseWriter, req *stdhttp.Request, resource string) {
	switch {
	// The repository-overrides case MUST be dispatched first, ahead of the
	// "/actions/" and HasSuffix families below (design.md Decision 7's
	// ordering hazard). A repository literally named "team/config" (or
	// "team/status", "team:enable", ...) would otherwise be swallowed by
	// one of those branches instead of reaching the override handler;
	// TestAdminRepositoryOverrideRoutesRepositoryNamedConfigSegmentCorrectly
	// pins this against the "/config" branch specifically.
	case strings.HasSuffix(resource, "/repository-overrides"):
		feature := strings.TrimSuffix(resource, "/repository-overrides")
		if feature == "" || strings.Contains(feature, "/") {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminRepositoryOverridesCollection(w, req, feature)
	case strings.Contains(resource, "/repository-overrides/"):
		feature, repository, ok := adminNestedResource(resource, "/repository-overrides/")
		if !ok {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminRepositoryOverrideResource(w, req, feature, repository)
	case strings.Contains(resource, "/actions/"):
		name, actionID, ok := adminNestedResource(resource, "/actions/")
		if !ok || strings.Contains(actionID, "/") {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		if req.Method != stdhttp.MethodPost {
			w.Header().Set("Allow", stdhttp.MethodPost)
			w.WriteHeader(stdhttp.StatusMethodNotAllowed)
			return
		}
		result, err := r.service.ExecuteFeatureAction(req.Context(), name, actionID)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, result)
	case strings.HasSuffix(resource, "/status"):
		name := strings.TrimSuffix(resource, "/status")
		details, err := r.service.GetFeatureStatus(req.Context(), name)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, featureDetailsResponse(details, true))
	case strings.HasSuffix(resource, "/config"):
		name := strings.TrimSuffix(resource, "/config")
		if req.Method != stdhttp.MethodPut {
			w.Header().Set("Allow", stdhttp.MethodPut)
			w.WriteHeader(stdhttp.StatusMethodNotAllowed)
			return
		}
		input, err := decodeFeatureConfigureInput(req)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		details, err := r.service.ConfigureFeature(req.Context(), name, input)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, featureDetailsResponse(details, false))
	case strings.HasSuffix(resource, ":install"):
		name := strings.TrimSuffix(resource, ":install")
		state, err := r.mutateFeatureRuntime(req, name, func(version string) (ports.FeatureRuntimeState, error) {
			return r.service.InstallFeatureRuntime(req.Context(), name, version)
		})
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, state)
	case strings.HasSuffix(resource, ":upgrade"):
		name := strings.TrimSuffix(resource, ":upgrade")
		state, err := r.mutateFeatureRuntime(req, name, func(version string) (ports.FeatureRuntimeState, error) {
			return r.service.UpgradeFeatureRuntime(req.Context(), name, version)
		})
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, state)
	case strings.HasSuffix(resource, ":rollback"):
		name := strings.TrimSuffix(resource, ":rollback")
		if req.Method != stdhttp.MethodPost {
			w.Header().Set("Allow", stdhttp.MethodPost)
			w.WriteHeader(stdhttp.StatusMethodNotAllowed)
			return
		}
		state, err := r.service.RollbackFeatureRuntime(req.Context(), name)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, state)
	case strings.HasSuffix(resource, ":enable"):
		name := strings.TrimSuffix(resource, ":enable")
		if req.Method != stdhttp.MethodPost {
			w.Header().Set("Allow", stdhttp.MethodPost)
			w.WriteHeader(stdhttp.StatusMethodNotAllowed)
			return
		}
		details, err := r.service.SetFeatureEnabled(req.Context(), name, true)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, featureDetailsResponse(details, false))
	case strings.HasSuffix(resource, ":disable"):
		name := strings.TrimSuffix(resource, ":disable")
		if req.Method != stdhttp.MethodPost {
			w.Header().Set("Allow", stdhttp.MethodPost)
			w.WriteHeader(stdhttp.StatusMethodNotAllowed)
			return
		}
		details, err := r.service.SetFeatureEnabled(req.Context(), name, false)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, featureDetailsResponse(details, false))
	default:
		if req.Method != stdhttp.MethodGet {
			w.Header().Set("Allow", stdhttp.MethodGet)
			w.WriteHeader(stdhttp.StatusMethodNotAllowed)
			return
		}
		page, err := r.service.GetFeaturePage(req.Context(), resource)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, page)
	}
}

// handleAdminRepositoryOverridesCollection is the "List (TUI annotation)"
// row of design.md Decision 7's route table: every stored override row for
// one feature, used by Phase 8's Repository Alerts row annotation.
func (r *Router) handleAdminRepositoryOverridesCollection(w stdhttp.ResponseWriter, req *stdhttp.Request, feature string) {
	if req.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}
	overrides, err := r.service.ListRepositoryOverrides(req.Context(), feature)
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}
	responses := make([]map[string]any, 0, len(overrides))
	for _, override := range overrides {
		response, err := repositoryOverrideResponse(override)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		responses = append(responses, response)
	}
	writeJSON(w, stdhttp.StatusOK, responses)
}

// handleAdminRepositoryOverrideResource is the single-repository admin
// resource from design.md Decision 7: GET returns the current override or a
// 404 row-presence boundary when none exists, PUT replaces it in full
// (codec.Normalize rejects a mismatched feature shape), and DELETE removes
// it, reverting that repository and feature to the global settings row
// immediately.
func (r *Router) handleAdminRepositoryOverrideResource(w stdhttp.ResponseWriter, req *stdhttp.Request, feature string, repository string) {
	switch req.Method {
	case stdhttp.MethodGet:
		override, err := r.service.GetRepositoryOverride(req.Context(), repository, feature)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		response, err := repositoryOverrideResponse(override)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, response)
	case stdhttp.MethodPut:
		raw, err := decodeAdminRawJSON(req)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		override, err := r.service.SetRepositoryOverride(req.Context(), repository, feature, raw)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		response, err := repositoryOverrideResponse(override)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, response)
	case stdhttp.MethodDelete:
		if err := r.service.ClearRepositoryOverride(req.Context(), repository, feature); err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		w.WriteHeader(stdhttp.StatusNoContent)
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodPut, stdhttp.MethodDelete}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

// repositoryOverrideResponse projects a stored override row onto the wire:
// the body is the typed feature object's own fields (already normalized by
// the codec at write time) plus updated_at appended.
func repositoryOverrideResponse(override ports.RepositoryFeatureOverride) (map[string]any, error) {
	response := map[string]any{}
	if len(override.Payload) > 0 {
		if err := json.Unmarshal(override.Payload, &response); err != nil {
			return nil, err
		}
	}
	response["repository"] = override.Repository
	response["feature"] = override.Feature
	response["updated_at"] = override.UpdatedAt
	return response, nil
}

// decodeAdminRawJSON reads the request body verbatim so the caller can hand
// it to a repositoryOverrideCodec's Normalize, which performs its own
// strict decode (DisallowUnknownFields) into the exact typed shape for the
// resource's feature — this function does no shape validation itself.
func decodeAdminRawJSON(req *stdhttp.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, domainauth.NewValidationError("request body is required")
	}
	defer req.Body.Close()

	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, domainauth.NewValidationError("request body must be valid JSON")
	}
	return raw, nil
}

func (r *Router) handleAdminScanSettings(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	switch req.Method {
	case stdhttp.MethodGet:
		settings, err := r.service.GetScanSettings(req.Context())
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, scanSettingsResponse(settings))
	case stdhttp.MethodPut:
		settings, err := decodeScanSettings(req)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		updated, err := r.service.UpdateScanSettings(req.Context(), settings)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, scanSettingsResponse(updated))
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodPut}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

// handleAdminScanPolicy is the vulnerability policy gate's admin read/write
// endpoint (design.md Decision 5), modeled line-for-line on
// handleAdminScanSettings: GET returns the current settings (including the
// code-level default when no row has ever been written), PUT fully replaces
// them and requires a known severity_threshold value so the evaluator's
// permissive default branch (scanPolicyViolated's unknown-threshold case)
// can never be reached from this API.
func (r *Router) handleAdminScanPolicy(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	switch req.Method {
	case stdhttp.MethodGet:
		settings, err := r.service.GetScanPolicySettings(req.Context())
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, scanPolicySettingsResponse(settings))
	case stdhttp.MethodPut:
		settings, err := decodeScanPolicySettings(req)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		updated, err := r.service.UpdateScanPolicySettings(req.Context(), settings)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, scanPolicySettingsResponse(updated))
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodPut}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

func decodeScanPolicySettings(req *stdhttp.Request) (ports.ScanPolicySettings, error) {
	var payload struct {
		Enabled           bool   `json:"enabled"`
		SeverityThreshold string `json:"severity_threshold"`
	}
	if err := decodeAdminJSON(req, &payload); err != nil {
		return ports.ScanPolicySettings{}, err
	}
	threshold := strings.TrimSpace(payload.SeverityThreshold)
	if threshold != ports.ScanPolicyThresholdCritical && threshold != ports.ScanPolicyThresholdCriticalHigh {
		return ports.ScanPolicySettings{}, domainauth.NewValidationError("severity_threshold must be critical or critical_high")
	}
	return ports.ScanPolicySettings{Enabled: payload.Enabled, SeverityThreshold: threshold}, nil
}

func scanPolicySettingsResponse(settings ports.ScanPolicySettings) map[string]any {
	return map[string]any{
		"enabled":            settings.Enabled,
		"severity_threshold": settings.SeverityThreshold,
		"updated_at":         settings.UpdatedAt,
	}
}

// maxSigningPolicyTrustedKeys bounds the global signing policy's trusted-key
// list (design.md Decision 8, rule 3): the per-pull verification loop is
// O(len(TrustedPublicKeys) x len(entries)), so an admin-configurable
// unbounded list is a hot-path cost hazard, not just a UX concern.
const maxSigningPolicyTrustedKeys = 16

// handleAdminSigningPolicy is modeled line-for-line on
// handleAdminScanPolicy: GET returns the current global signing policy
// settings, including the code-level {Enabled: false} default when no row
// exists — never 404. PUT fully replaces the settings. Authorization is
// inherited from handleAdmin's requireAdminPrincipal — no new permission
// surface (design.md Decision 8).
func (r *Router) handleAdminSigningPolicy(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	switch req.Method {
	case stdhttp.MethodGet:
		settings, err := r.service.GetSigningPolicySettings(req.Context())
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, signingPolicySettingsResponse(settings))
	case stdhttp.MethodPut:
		settings, err := decodeSigningPolicySettings(req)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		updated, err := r.service.UpdateSigningPolicySettings(req.Context(), settings)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, signingPolicySettingsResponse(updated))
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodPut}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

// handleAdminSigningKeyUsage is CountManifestsSignedByKey's HTTP surface: a
// read-only, best-effort advisory the TUI/console calls right before
// showing a "delete this key?" confirm, never a blocking gate (design
// requirement: deletion of a key must never be blocked by this count, only
// informed by it). key is the URL-encoded PEM to check; repository is
// optional (global scope when absent).
func (r *Router) handleAdminSigningKeyUsage(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	if req.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	// Reuses signing.NormalizePublicKeyPEM, the exact same validation
	// normalizeSigningOverride/decodeSigningPolicySettings already apply to
	// a trusted key -- a malformed key param must be a validation error at
	// the HTTP boundary, not a 500 surfaced from the service layer.
	keyParam := req.URL.Query().Get("key")
	normalizedKey, err := signing.NormalizePublicKeyPEM(keyParam)
	if err != nil {
		writeAdminError(w, domainauth.NewValidationError("key is invalid: "+err.Error()), ports.Challenge{})
		return
	}

	repository := strings.TrimSpace(req.URL.Query().Get("repository"))

	count, capped, err := r.service.CountManifestsSignedByKey(req.Context(), repository, normalizedKey)
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}

	writeJSON(w, stdhttp.StatusOK, map[string]any{"count": count, "capped": capped})
}

// decodeSigningPolicySettings mirrors decodeScanPolicySettings and enforces
// the three rules from design.md Decision 8:
//  1. Every key goes through signing.NormalizePublicKeyPEM; a parse failure
//     or a non-ECDSA-P256 key is a validation error naming the offending
//     index, never echoing key bytes.
//  2. enabled:true with zero usable keys is a validation error (the outage
//     rule) — it makes the guaranteed-total-outage configuration
//     unrepresentable rather than merely discouraged.
//  3. At most maxSigningPolicyTrustedKeys keys, bounding the per-pull
//     verification loop.
func decodeSigningPolicySettings(req *stdhttp.Request) (ports.SigningPolicySettings, error) {
	var payload struct {
		Enabled           bool     `json:"enabled"`
		TrustedPublicKeys []string `json:"trusted_public_keys"`
		UnsignedSelfRead  string   `json:"unsigned_self_read"`
	}
	if err := decodeAdminJSON(req, &payload); err != nil {
		return ports.SigningPolicySettings{}, err
	}
	if len(payload.TrustedPublicKeys) > maxSigningPolicyTrustedKeys {
		return ports.SigningPolicySettings{}, domainauth.NewValidationError(fmt.Sprintf("trusted_public_keys must contain at most %d entries", maxSigningPolicyTrustedKeys))
	}
	normalizedKeys := make([]string, 0, len(payload.TrustedPublicKeys))
	for index, key := range payload.TrustedPublicKeys {
		normalized, err := signing.NormalizePublicKeyPEM(key)
		if err != nil {
			return ports.SigningPolicySettings{}, domainauth.NewValidationError(fmt.Sprintf("trusted_public_keys[%d] is invalid: %s", index, err.Error()))
		}
		normalizedKeys = append(normalizedKeys, normalized)
	}
	if payload.Enabled && len(normalizedKeys) == 0 {
		return ports.SigningPolicySettings{}, domainauth.NewValidationError("enabled requires at least one usable entry in trusted_public_keys")
	}
	if !ports.ValidUnsignedSelfRead(payload.UnsignedSelfRead) {
		return ports.SigningPolicySettings{}, domainauth.NewValidationError(fmt.Sprintf("unsigned_self_read %q is invalid", payload.UnsignedSelfRead))
	}
	return ports.SigningPolicySettings{Enabled: payload.Enabled, TrustedPublicKeys: normalizedKeys, UnsignedSelfRead: payload.UnsignedSelfRead}, nil
}

func signingPolicySettingsResponse(settings ports.SigningPolicySettings) map[string]any {
	trustedKeys := settings.TrustedPublicKeys
	if trustedKeys == nil {
		trustedKeys = []string{}
	}
	return map[string]any{
		"enabled":             settings.Enabled,
		"trusted_public_keys": trustedKeys,
		"unsigned_self_read":  settings.UnsignedSelfRead,
		"updated_at":          settings.UpdatedAt,
	}
}

func (r *Router) handleAdminScanRuns(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	switch req.Method {
	case stdhttp.MethodGet:
		repository := strings.TrimSpace(req.URL.Query().Get("repository"))
		limit := 20
		if raw := strings.TrimSpace(req.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				writeAdminError(w, domainauth.NewValidationError("limit must be a positive integer"), ports.Challenge{})
				return
			}
			limit = parsed
		}
		runs, err := r.service.ListScanRuns(req.Context(), repository, limit)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, runs)
	case stdhttp.MethodPost:
		var payload struct {
			Repository string `json:"repository"`
			Reference  string `json:"reference"`
		}
		if err := decodeAdminJSON(req, &payload); err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		run, err := r.service.QueueManualScan(req.Context(), payload.Repository, payload.Reference)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusAccepted, run)
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodPost}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

// handleAdminRepositoryScanSummaries backs the Repository Alerts table's
// crowd-out fix (ports.RepositoryScanSummary): one row per repository,
// collapsed before limit, mirroring handleAdminScanRuns' GET shape.
func (r *Router) handleAdminRepositoryScanSummaries(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	if req.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}
	limit := 25
	if raw := strings.TrimSpace(req.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeAdminError(w, domainauth.NewValidationError("limit must be a positive integer"), ports.Challenge{})
			return
		}
		limit = parsed
	}
	summaries, err := r.service.ListLatestScanRunPerRepository(req.Context(), limit)
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}
	writeJSON(w, stdhttp.StatusOK, summaries)
}

func (r *Router) handleAdminScanRunDetail(w stdhttp.ResponseWriter, req *stdhttp.Request, runID string) {
	if req.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}
	detail, err := r.service.GetScanRunDetail(req.Context(), strings.TrimSpace(runID))
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}
	writeJSON(w, stdhttp.StatusOK, detail)
}

// handleAdminSecretScanFindings is the secret-findings-by-image endpoint
// (tasks.md 6.1, spec.md "Operator Visibility of Findings"). An image is
// identified by repository+digest (the same pair both the Trivy and secret
// scan legs are triggered with from the same rescan). The response is
// ports.SecretScanRunDetail, which structurally cannot carry a matched
// secret value or fingerprint — ports.SecretFinding only declares
// RuleID/Description/BlobDigest/Path/StartLine/EndLine/Tags.
func (r *Router) handleAdminSecretScanFindings(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	if req.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}
	repository := strings.TrimSpace(req.URL.Query().Get("repository"))
	digest := strings.TrimSpace(req.URL.Query().Get("digest"))
	if repository == "" || digest == "" {
		writeAdminError(w, domainauth.NewValidationError("repository and digest are required"), ports.Challenge{})
		return
	}
	detail, err := r.service.GetSecretScanFindings(req.Context(), repository, digest)
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}
	writeJSON(w, stdhttp.StatusOK, detail)
}

// handleAdminGCReports is the report-create route (design.md Data Flow):
// POST computes and persists a mark-sweep-grace report and returns it with
// 201 (a durable resource now exists, an executor-level correction from an
// earlier 202 draft). This endpoint is reachable regardless of
// REGISTRY_GC_DELETE_ENABLED -- the report path is always available.
func (r *Router) handleAdminGCReports(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	if req.Method != stdhttp.MethodPost {
		w.Header().Set("Allow", stdhttp.MethodPost)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	detail, err := r.service.ComputeGCReport(req.Context())
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}
	writeJSON(w, stdhttp.StatusCreated, detail)
}

// handleAdminGCReportResource dispatches gc/reports/{id}[/{action}]
// (design.md Data Flow): id, action, hasAction := strings.Cut(resource,
// "/"). Report IDs are opaque UUIDs and cannot contain a slash, so the
// repository-name ordering hazard documented at handleAdminFeatureResource
// does NOT apply here. GET-by-id only, for now -- the "delete" action is
// added once Phase 6's flag/error-code plumbing lands.
func (r *Router) handleAdminGCReportResource(w stdhttp.ResponseWriter, req *stdhttp.Request, resource string) {
	id, action, hasAction := strings.Cut(resource, "/")
	if id == "" {
		writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
		return
	}

	if hasAction {
		if action != "delete" {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}

		if req.Method != stdhttp.MethodPost {
			w.Header().Set("Allow", stdhttp.MethodPost)
			w.WriteHeader(stdhttp.StatusMethodNotAllowed)
			return
		}

		detail, err := r.service.DeleteByGCReport(req.Context(), id)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, detail)
		return
	}

	if req.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	detail, err := r.service.GetGCReport(req.Context(), id)
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}
	writeJSON(w, stdhttp.StatusOK, detail)
}

func decodeScanSettings(req *stdhttp.Request) (ports.ScanSettings, error) {
	var payload struct {
		Enabled               bool   `json:"enabled"`
		ScheduleEnabled       bool   `json:"schedule_enabled"`
		Interval              string `json:"interval"`
		Timeout               string `json:"timeout"`
		ServiceURL            string `json:"service_url"`
		RegistryReachableURL  string `json:"registry_reachable_url"`
		AuthToken             string `json:"auth_token"`
		TLSCACertPath         string `json:"tls_ca_cert_path"`
		TLSInsecureSkipVerify bool   `json:"tls_insecure_skip_verify"`
		MaxConcurrency        int    `json:"max_concurrency"`
	}
	if err := decodeAdminJSON(req, &payload); err != nil {
		return ports.ScanSettings{}, err
	}
	interval, err := time.ParseDuration(strings.TrimSpace(payload.Interval))
	if err != nil {
		return ports.ScanSettings{}, domainauth.NewValidationError("interval must be a valid duration")
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(payload.Timeout))
	if err != nil {
		return ports.ScanSettings{}, domainauth.NewValidationError("timeout must be a valid duration")
	}
	return ports.ScanSettings{Enabled: payload.Enabled, ScheduleEnabled: payload.ScheduleEnabled, Interval: interval, Timeout: timeout, ServiceURL: payload.ServiceURL, RegistryReachableURL: payload.RegistryReachableURL, AuthToken: payload.AuthToken, TLSCACertPath: payload.TLSCACertPath, TLSInsecureSkipVerify: payload.TLSInsecureSkipVerify, MaxConcurrency: payload.MaxConcurrency}, nil
}

func scanSettingsResponse(settings ports.ScanSettings) map[string]any {
	return map[string]any{
		"enabled":                  settings.Enabled,
		"schedule_enabled":         settings.ScheduleEnabled,
		"interval":                 settings.Interval.String(),
		"timeout":                  settings.Timeout.String(),
		"service_url":              settings.ServiceURL,
		"registry_reachable_url":   settings.RegistryReachableURL,
		"tls_ca_cert_path":         settings.TLSCACertPath,
		"tls_insecure_skip_verify": settings.TLSInsecureSkipVerify,
		"max_concurrency":          settings.MaxConcurrency,
		"updated_at":               settings.UpdatedAt,
	}
}

func decodeFeatureConfigureInput(req *stdhttp.Request) (ports.FeatureConfigureInput, error) {
	var payload struct {
		Enabled               *bool   `json:"enabled"`
		ScheduleEnabled       *bool   `json:"schedule_enabled"`
		Interval              string  `json:"interval"`
		Timeout               string  `json:"timeout"`
		ServiceURL            *string `json:"service_url"`
		RegistryReachableURL  *string `json:"registry_reachable_url"`
		AuthToken             *string `json:"auth_token"`
		TLSCACertPath         *string `json:"tls_ca_cert_path"`
		TLSInsecureSkipVerify *bool   `json:"tls_insecure_skip_verify"`
		MaxConcurrency        *int    `json:"max_concurrency"`
	}
	if err := decodeAdminJSON(req, &payload); err != nil {
		return ports.FeatureConfigureInput{}, err
	}
	input := ports.FeatureConfigureInput{
		Enabled:               payload.Enabled,
		ScheduleEnabled:       payload.ScheduleEnabled,
		ServiceURL:            payload.ServiceURL,
		RegistryReachableURL:  payload.RegistryReachableURL,
		AuthToken:             payload.AuthToken,
		TLSCACertPath:         payload.TLSCACertPath,
		TLSInsecureSkipVerify: payload.TLSInsecureSkipVerify,
		MaxConcurrency:        payload.MaxConcurrency,
	}
	if strings.TrimSpace(payload.Interval) != "" {
		interval, err := time.ParseDuration(strings.TrimSpace(payload.Interval))
		if err != nil {
			return ports.FeatureConfigureInput{}, domainauth.NewValidationError("interval must be a valid duration")
		}
		input.Interval = &interval
	}
	if strings.TrimSpace(payload.Timeout) != "" {
		timeout, err := time.ParseDuration(strings.TrimSpace(payload.Timeout))
		if err != nil {
			return ports.FeatureConfigureInput{}, domainauth.NewValidationError("timeout must be a valid duration")
		}
		input.Timeout = &timeout
	}
	return input, nil
}

func featureDetailsResponse(details ports.FeatureDetails, includeRuntime bool) map[string]any {
	response := map[string]any{
		"name":                     details.Name,
		"kind":                     details.Kind,
		"enabled":                  details.Enabled,
		"configured":               details.Configured,
		"schedule_enabled":         details.ScheduleEnabled,
		"interval":                 details.Interval.String(),
		"timeout":                  details.Timeout.String(),
		"service_url":              details.ServiceURL,
		"registry_reachable_url":   details.RegistryReachableURL,
		"tls_ca_cert_path":         details.TLSCACertPath,
		"tls_insecure_skip_verify": details.TLSInsecureSkipVerify,
		"max_concurrency":          details.MaxConcurrency,
	}
	if includeRuntime {
		response["runtime"] = details.Runtime
	}
	return response
}

func (r *Router) mutateFeatureRuntime(req *stdhttp.Request, name string, action func(version string) (ports.FeatureRuntimeState, error)) (ports.FeatureRuntimeState, error) {
	if req.Method != stdhttp.MethodPost {
		return ports.FeatureRuntimeState{}, domainauth.NewValidationError("runtime mutation requires POST")
	}
	var payload struct {
		Version string `json:"version"`
	}
	if req.Body != nil && req.ContentLength != 0 {
		if err := decodeAdminJSON(req, &payload); err != nil {
			return ports.FeatureRuntimeState{}, err
		}
	}
	return action(strings.TrimSpace(payload.Version))
}

func (r *Router) handleAdminUsersCollection(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal) {
	switch req.Method {
	case stdhttp.MethodGet:
		users, err := r.admin.ListAdminUsers(req.Context(), principal)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, users)
	case stdhttp.MethodPost:
		var input ports.AdminCreateUserInput
		if err := decodeAdminJSON(req, &input); err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}

		user, err := r.admin.CreateAdminUser(req.Context(), principal, input)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusCreated, user)
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodPost}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

// handleAdminRobotsCollection dispatches the two new robot routes
// (design.md Decision 6): everything else about robot lifecycle (enable/
// disable/delete, token issue/list/revoke) reuses the existing user routes,
// which already accept any user ID. This collection stays under
// handleAdmin's blanket requireAdminPrincipal gate — global admin only.
func (r *Router) handleAdminRobotsCollection(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal) {
	switch req.Method {
	case stdhttp.MethodGet:
		robots, err := r.admin.ListAdminRobots(req.Context(), principal)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, robots)
	case stdhttp.MethodPost:
		input, err := decodeAdminCreateRobotInput(req)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}

		created, err := r.admin.CreateAdminRobot(req.Context(), principal, input)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusCreated, created)
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodPost}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

// handleAdminRobotResource dispatches DELETE /admin/v1/robots/{id}
// (registry-acl-v1 robot-deletion follow-up): the target's IsRobot flag is
// re-checked at the service layer (Service.DeleteRobot), so a non-robot
// user ID is rejected here too, not just for defense-in-depth documentation
// -- the handler surfaces exactly whatever error the service returns.
func (r *Router) handleAdminRobotResource(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, resource string) {
	userID, ok := adminResourceID(resource, "")
	if !ok {
		writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
		return
	}

	switch req.Method {
	case stdhttp.MethodDelete:
		if err := r.admin.DeleteAdminRobot(req.Context(), principal, userID); err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		w.WriteHeader(stdhttp.StatusNoContent)
	default:
		w.Header().Set("Allow", stdhttp.MethodDelete)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

func decodeAdminCreateRobotInput(req *stdhttp.Request) (ports.AdminCreateRobotInput, error) {
	var payload struct {
		Name       string              `json:"name"`
		Repository string              `json:"repository"`
		Role       domainauth.RepoRole `json:"role"`
		TTLSeconds *int64              `json:"ttl_seconds"`
	}
	if err := decodeAdminJSON(req, &payload); err != nil {
		return ports.AdminCreateRobotInput{}, err
	}
	if payload.TTLSeconds != nil && *payload.TTLSeconds < 0 {
		return ports.AdminCreateRobotInput{}, domainauth.NewValidationError("ttl_seconds must be zero or greater")
	}

	input := ports.AdminCreateRobotInput{Name: payload.Name, Repository: payload.Repository, Role: payload.Role}
	if payload.TTLSeconds != nil {
		input.TTL = time.Duration(*payload.TTLSeconds) * time.Second
	}

	return input, nil
}

func (r *Router) handleAdminUserResource(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, resource string) {
	switch {
	case strings.HasSuffix(resource, ":enable"):
		userID, ok := adminResourceID(resource, ":enable")
		if !ok {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminUserEnablement(w, req, principal, userID, true)
	case strings.HasSuffix(resource, ":disable"):
		userID, ok := adminResourceID(resource, ":disable")
		if !ok {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminUserEnablement(w, req, principal, userID, false)
	case strings.HasSuffix(resource, ":reset-password"):
		userID, ok := adminResourceID(resource, ":reset-password")
		if !ok {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminUserPasswordReset(w, req, principal, userID)
	case strings.HasSuffix(resource, "/grants"):
		userID, ok := adminNestedUserID(resource, "/grants")
		if !ok {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminUserGrantsCollection(w, req, principal, userID)
	case strings.Contains(resource, "/grants/"):
		userID, repository, ok := adminNestedResource(resource, "/grants/")
		if !ok {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminUserGrantResource(w, req, principal, userID, repository)
	case strings.HasSuffix(resource, "/admin-tokens"):
		userID, ok := adminNestedUserID(resource, "/admin-tokens")
		if !ok {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminUserTokensCollection(w, req, principal, userID)
	case strings.Contains(resource, "/admin-tokens/"):
		userID, accessor, ok := adminNestedResource(resource, "/admin-tokens/")
		if !ok || strings.Contains(accessor, "/") {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminUserTokenResource(w, req, principal, userID, accessor)
	default:
		writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
	}
}

func (r *Router) handleAdminUserGrantsCollection(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, userID string) {
	if req.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	grants, err := r.admin.ListAdminUserRepoGrants(req.Context(), principal, userID)
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}

	writeJSON(w, stdhttp.StatusOK, grants)
}

func (r *Router) handleAdminUserGrantResource(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, userID string, repository string) {
	switch req.Method {
	case stdhttp.MethodPut:
		var input ports.AdminPutRepoGrantInput
		if err := decodeAdminJSON(req, &input); err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		input.UserID = userID
		input.Repository = repository

		grant, err := r.admin.PutAdminUserRepoGrant(req.Context(), principal, input)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}

		writeJSON(w, stdhttp.StatusOK, grant)
	case stdhttp.MethodDelete:
		if err := r.admin.DeleteAdminUserRepoGrant(req.Context(), principal, userID, repository); err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}

		w.WriteHeader(stdhttp.StatusNoContent)
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodPut, stdhttp.MethodDelete}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

// handleAdminRepositoryResource dispatches the "repositories/" delegate
// namespace (design.md Decision 3). resource has already had the
// "repositories/" prefix trimmed. It splits on strings.LastIndex(resource,
// "/grants/") and strings.HasSuffix(resource, "/grants") — a username can
// never contain "/", but a repository can literally be named "team/grants"
// (the same ordering hazard already documented at handleAdminFeatureResource
// above); TestAdminRepositoryGrantRoutesRepositoryNamedTeamGrantsRoutesCorrectly
// pins this.
func (r *Router) handleAdminRepositoryResource(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, resource string) {
	// The collection suffix MUST be checked first, ahead of the "/grants/"
	// split below (same ordering hazard as handleAdminFeatureResource's
	// repository-overrides case above): a repository literally named
	// "team/grants" produces a resource like "team/grants/grants" for the
	// collection GET, which also happens to CONTAIN the substring
	// "/grants/" earlier in the string. Checking the suffix first routes
	// that case to the collection handler instead of misreading it as a
	// single-user PUT/DELETE on repository "team" username "grants".
	if strings.HasSuffix(resource, "/grants") {
		repository := strings.TrimSuffix(resource, "/grants")
		if repository == "" {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminRepositoryGrantsCollection(w, req, principal, repository)
		return
	}

	if index := strings.LastIndex(resource, "/grants/"); index >= 0 {
		repository := resource[:index]
		username := resource[index+len("/grants/"):]
		if repository == "" || username == "" || strings.Contains(username, "/") {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminRepositoryGrantResource(w, req, principal, repository, username)
		return
	}

	writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
}

func (r *Router) handleAdminRepositoryGrantsCollection(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, repository string) {
	if req.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	grants, err := r.admin.ListAdminRepositoryGrants(req.Context(), principal, repository)
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}

	writeJSON(w, stdhttp.StatusOK, grants)
}

func (r *Router) handleAdminRepositoryGrantResource(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, repository string, username string) {
	switch req.Method {
	case stdhttp.MethodPut:
		var input ports.AdminPutRepositoryGrantInput
		if err := decodeAdminJSON(req, &input); err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		input.Repository = repository
		input.Username = username

		grant, err := r.admin.PutAdminRepositoryGrant(req.Context(), principal, input)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}

		writeJSON(w, stdhttp.StatusOK, grant)
	case stdhttp.MethodDelete:
		if err := r.admin.DeleteAdminRepositoryGrant(req.Context(), principal, repository, username); err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}

		w.WriteHeader(stdhttp.StatusNoContent)
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodPut, stdhttp.MethodDelete}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

func (r *Router) handleAdminUserTokensCollection(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, userID string) {
	switch req.Method {
	case stdhttp.MethodGet:
		tokens, err := r.admin.ListAdminUserTokens(req.Context(), principal, userID)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}

		writeJSON(w, stdhttp.StatusOK, tokens)
	case stdhttp.MethodPost:
		input, err := decodeAdminCreateTokenInput(req)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		input.UserID = userID

		created, err := r.admin.CreateAdminUserToken(req.Context(), principal, input)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}

		writeJSON(w, stdhttp.StatusCreated, created)
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodPost}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

func (r *Router) handleAdminUserTokenResource(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, userID string, accessor string) {
	if req.Method != stdhttp.MethodDelete {
		w.Header().Set("Allow", stdhttp.MethodDelete)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	if err := r.admin.RevokeAdminUserToken(req.Context(), principal, userID, accessor); err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}

	w.WriteHeader(stdhttp.StatusNoContent)
}

func (r *Router) handleAdminUserEnablement(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, userID string, enabled bool) {
	if req.Method != stdhttp.MethodPost {
		w.Header().Set("Allow", stdhttp.MethodPost)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	var (
		user ports.AdminUser
		err  error
	)
	if enabled {
		user, err = r.admin.EnableAdminUser(req.Context(), principal, userID)
	} else {
		user, err = r.admin.DisableAdminUser(req.Context(), principal, userID)
	}
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}

	writeJSON(w, stdhttp.StatusOK, user)
}

func (r *Router) handleAdminUserPasswordReset(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, userID string) {
	if req.Method != stdhttp.MethodPost {
		w.Header().Set("Allow", stdhttp.MethodPost)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	var input ports.AdminResetPasswordInput
	if err := decodeAdminJSON(req, &input); err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}
	input.UserID = userID

	if err := r.admin.ResetAdminUserPassword(req.Context(), principal, input); err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}

	w.WriteHeader(stdhttp.StatusNoContent)
}

func adminResourceID(resource string, suffix string) (string, bool) {
	userID := strings.TrimSpace(strings.TrimSuffix(resource, suffix))
	if userID == "" || strings.Contains(userID, "/") {
		return "", false
	}
	return userID, true
}

func adminNestedUserID(resource string, suffix string) (string, bool) {
	return adminResourceID(resource, suffix)
}

func adminNestedResource(resource string, marker string) (string, string, bool) {
	parts := strings.SplitN(resource, marker, 2)
	if len(parts) != 2 {
		return "", "", false
	}

	userID := strings.TrimSpace(parts[0])
	nested := strings.TrimSpace(parts[1])
	if userID == "" || nested == "" || strings.Contains(userID, "/") {
		return "", "", false
	}

	return userID, nested, true
}

func decodeAdminCreateTokenInput(req *stdhttp.Request) (ports.AdminCreateTokenInput, error) {
	var payload struct {
		Name       string `json:"name"`
		TTLSeconds *int64 `json:"ttl_seconds"`
	}
	if err := decodeAdminJSON(req, &payload); err != nil {
		return ports.AdminCreateTokenInput{}, err
	}
	if payload.TTLSeconds != nil && *payload.TTLSeconds < 0 {
		return ports.AdminCreateTokenInput{}, domainauth.NewValidationError("ttl_seconds must be zero or greater")
	}

	input := ports.AdminCreateTokenInput{Name: payload.Name}
	if payload.TTLSeconds != nil {
		input.TTL = time.Duration(*payload.TTLSeconds) * time.Second
	}

	return input, nil
}

func decodeAdminJSON(req *stdhttp.Request, dst any) error {
	if req.Body == nil {
		return domainauth.NewValidationError("request body is required")
	}
	defer req.Body.Close()

	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return domainauth.NewValidationError("request body is required")
		}
		var unmarshalTypeError *json.UnmarshalTypeError
		if errors.As(err, &unmarshalTypeError) {
			field := strings.TrimSpace(unmarshalTypeError.Field)
			if field == "" {
				field = strings.TrimSpace(unmarshalTypeError.Struct)
			}
			if field != "" {
				return domainauth.NewValidationError(field + " has an invalid type")
			}
		}
		var numErr *strconv.NumError
		if errors.As(err, &numErr) {
			return domainauth.NewValidationError("request body contains an invalid number")
		}
		return domainauth.NewValidationError("request body must be valid JSON")
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return domainauth.NewValidationError("request body must contain a single JSON object")
	}

	return nil
}

// requireAuthenticatedPrincipal is requireAdminPrincipal minus the IsAdmin
// check (design.md Decision 3): it authenticates the caller and reuses the
// same adminChallenge/writeAdminError shapes, but leaves the
// admin-or-repo-admin authority decision to the service layer, which has
// the repository argument requireAdminPrincipal never sees.
func (r *Router) requireAuthenticatedPrincipal(w stdhttp.ResponseWriter, req *stdhttp.Request) (*domainauth.Principal, bool) {
	bearerChallenge := r.adminChallenge(req)
	principal, err := r.authenticate(req)
	if err != nil {
		writeAdminError(w, err, bearerChallenge)
		return nil, false
	}
	if principal == nil {
		writeAdminError(w, domainauth.NewInvalidCredentialsError(), bearerChallenge)
		return nil, false
	}

	return principal, true
}

func (r *Router) requireAdminPrincipal(w stdhttp.ResponseWriter, req *stdhttp.Request) (*domainauth.Principal, bool) {
	bearerChallenge := r.adminChallenge(req)
	principal, err := r.authenticate(req)
	if err != nil {
		writeAdminError(w, err, bearerChallenge)
		return nil, false
	}
	if principal == nil {
		writeAdminError(w, domainauth.NewInvalidCredentialsError(), bearerChallenge)
		return nil, false
	}
	if !principal.IsAdmin {
		writeAdminError(w, domainauth.NewForbiddenError("admin access is required"), ports.Challenge{})
		return nil, false
	}

	return principal, true
}

func (r *Router) adminChallenge(req *stdhttp.Request) ports.Challenge {
	challenge := r.service.Challenge(ports.Action{})
	challenge.Scope = ""
	if req != nil && strings.TrimSpace(req.Header.Get("Authorization")) != "" {
		challenge.Error = "invalid_token"
	}
	return challenge
}

func writeAdminError(w stdhttp.ResponseWriter, err error, challenge ports.Challenge) {
	status := stdhttp.StatusInternalServerError

	message := err.Error()
	if registryErr, ok := err.(*domainregistry.Error); ok {
		message = registryErr.Message
		switch registryErr.Code {
		case domainregistry.ErrorCodeInvalidRepository, domainregistry.ErrorCodeValidation:
			status = stdhttp.StatusUnprocessableEntity
		case domainregistry.ErrorCodeNotFound:
			status = stdhttp.StatusNotFound
		case domainregistry.ErrorCodeConflict:
			status = stdhttp.StatusConflict
		case domainregistry.ErrorCodeUnsupported:
			// design.md D9: the endpoint exists but the capability is off in
			// this deployment. Admin-only -- no registry (/v2/) route may
			// return this code, and writeError's OCI mapping is untouched.
			status = stdhttp.StatusNotImplemented
		default:
			status = stdhttp.StatusInternalServerError
		}
	} else if authErr, ok := err.(*domainauth.Error); ok {
		message = authErr.Message
		switch authErr.Code {
		case domainauth.ErrorCodeInvalidCredentials, domainauth.ErrorCodeExpiredToken, domainauth.ErrorCodeRevokedToken, domainauth.ErrorCodeDisabledUser:
			status = stdhttp.StatusUnauthorized
			w.Header().Set("WWW-Authenticate", challengeHeader(challenge))
		case domainauth.ErrorCodeForbidden:
			status = stdhttp.StatusForbidden
		case domainauth.ErrorCodeValidation:
			status = stdhttp.StatusUnprocessableEntity
		case domainauth.ErrorCodeNotFound:
			status = stdhttp.StatusNotFound
		case domainauth.ErrorCodeConflict:
			status = stdhttp.StatusConflict
		default:
			status = stdhttp.StatusInternalServerError
		}
	}

	writeJSON(w, status, map[string]any{"error": message})
}
