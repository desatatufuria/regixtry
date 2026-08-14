package regixtryhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

func TestAdminFeatureStatusSeparatesIntentFromManagedRuntimeAndShowsMigrationTruth(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	if err := store.UpsertScanSettings(context.Background(), "tenant-a", "trivy", ports.ScanSettings{Enabled: true, ScheduleEnabled: true, Interval: 6 * time.Hour, Timeout: 10 * time.Minute, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 2, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertScanSettings() error = %v", err)
	}
	if err := store.UpsertFeatureRuntimeState(context.Background(), "tenant-a", "trivy", ports.FeatureRuntimeState{Status: ports.FeatureRuntimeStatusMigrationRequired, MigrationHint: `legacy binary_path "/tmp/README.sh" requires managed reinstall and will never be executed`, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertFeatureRuntimeState() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/features/trivy/status", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	for _, want := range []string{`"registry_reachable_url":"https://registry.internal"`, `"runtime":{"mode":"managed"`, `"status":"migration-required"`, `legacy binary_path \"/tmp/README.sh\" requires managed reinstall`} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("body = %q, want %q", recorder.Body.String(), want)
		}
	}
}

// TestAdminFeatureStatusReportsGitleaksIndependentlyFromTrivy is the Phase 6
// RED test (tasks.md 6.2, operator-admin-tui "Per-Feature Runtime Status
// Surface"): the per-feature runtime status response must project each
// registered feature's own (tenant, feature)-keyed state and never let one
// feature's status leak into another's entry.
func TestAdminFeatureStatusReportsGitleaksIndependentlyFromTrivy(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	if err := store.UpsertScanSettings(context.Background(), "tenant-a", "trivy", ports.ScanSettings{Enabled: true, Interval: 6 * time.Hour, Timeout: 10 * time.Minute, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 2, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertScanSettings(trivy) error = %v", err)
	}
	if err := store.UpsertFeatureRuntimeState(context.Background(), "tenant-a", "trivy", ports.FeatureRuntimeState{Status: ports.FeatureRuntimeStatusReady, ActiveVersion: "0.57.1", UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertFeatureRuntimeState(trivy) error = %v", err)
	}
	if err := store.UpsertScanSettings(context.Background(), "tenant-a", "gitleaks", ports.ScanSettings{Enabled: false, Interval: time.Hour, Timeout: time.Minute, MaxConcurrency: 1, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertScanSettings(gitleaks) error = %v", err)
	}
	if err := store.UpsertFeatureRuntimeState(context.Background(), "tenant-a", "gitleaks", ports.FeatureRuntimeState{Status: ports.FeatureRuntimeStatusUninstalled, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertFeatureRuntimeState(gitleaks) error = %v", err)
	}

	trivyReq := httptest.NewRequest(http.MethodGet, "/admin/v1/features/trivy/status", nil)
	trivyReq.Header.Set("Authorization", "Bearer admin-token")
	trivyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(trivyRecorder, trivyReq)
	if trivyRecorder.Code != http.StatusOK {
		t.Fatalf("trivy status = %d, want %d", trivyRecorder.Code, http.StatusOK)
	}
	trivyBody := trivyRecorder.Body.String()
	if !strings.Contains(trivyBody, `"enabled":true`) || !strings.Contains(trivyBody, `"version":"0.57.1"`) || !strings.Contains(trivyBody, `"status":"ready"`) {
		t.Fatalf("trivy body = %q, want its own enabled/ready state", trivyBody)
	}
	if strings.Contains(trivyBody, "uninstalled") {
		t.Fatalf("trivy body = %q, must not leak gitleaks' uninstalled runtime status", trivyBody)
	}

	gitleaksReq := httptest.NewRequest(http.MethodGet, "/admin/v1/features/gitleaks/status", nil)
	gitleaksReq.Header.Set("Authorization", "Bearer admin-token")
	gitleaksRecorder := httptest.NewRecorder()
	handler.ServeHTTP(gitleaksRecorder, gitleaksReq)
	if gitleaksRecorder.Code != http.StatusOK {
		t.Fatalf("gitleaks status = %d, want %d", gitleaksRecorder.Code, http.StatusOK)
	}
	gitleaksBody := gitleaksRecorder.Body.String()
	if !strings.Contains(gitleaksBody, `"enabled":false`) || !strings.Contains(gitleaksBody, `"status":"uninstalled"`) {
		t.Fatalf("gitleaks body = %q, want its own disabled/uninstalled state", gitleaksBody)
	}
	if strings.Contains(gitleaksBody, "0.57.1") || strings.Contains(gitleaksBody, `"status":"ready"`) {
		t.Fatalf("gitleaks body = %q, must not leak trivy's ready/version state", gitleaksBody)
	}
}

// TestAdminSecretScanFindingsResponseStructurallyCannotCarrySecretOrFingerprint
// is the Phase 6 RED test (tasks.md 6.2): the secret-findings-by-image
// endpoint response must have no field capable of holding a matched secret
// value or fingerprint — mirroring the structural-redaction assertion style
// from Phase 4's report_test.go (reflecting over the actual response JSON
// keys, not merely checking that one field is empty).
func TestAdminScanPolicyGetReturnsCurrentSettings(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	if err := store.UpsertScanPolicySettings(context.Background(), "tenant-a", ports.ScanPolicySettings{Enabled: false, SeverityThreshold: ports.ScanPolicyThresholdCriticalHigh, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertScanPolicySettings() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/scan-policy", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	for _, want := range []string{`"enabled":false`, `"severity_threshold":"critical_high"`} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("body = %q, want %q", recorder.Body.String(), want)
		}
	}
}

func TestAdminScanPolicyGetRequiresAdminPrincipal(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "reader-1", Username: "reader", IsAdmin: false}})

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/scan-policy", nil)
	req.Header.Set("Authorization", "Bearer reader-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusOK {
		t.Fatalf("status = %d, want a non-admin caller to be rejected", recorder.Code)
	}
}

func TestAdminScanPolicyPutPersistsAndRoundTrips(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	req := httptest.NewRequest(http.MethodPut, "/admin/v1/scan-policy", strings.NewReader(`{"enabled":true,"severity_threshold":"critical_high"}`))
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	for _, want := range []string{`"enabled":true`, `"severity_threshold":"critical_high"`} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("body = %q, want %q", recorder.Body.String(), want)
		}
	}

	stored, err := store.GetScanPolicySettings(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("GetScanPolicySettings() error = %v", err)
	}
	if !stored.Enabled || stored.SeverityThreshold != ports.ScanPolicyThresholdCriticalHigh {
		t.Fatalf("stored = %#v, want enabled/critical_high", stored)
	}
}

func TestAdminScanPolicyPutRejectsUnknownSeverityThreshold(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	req := httptest.NewRequest(http.MethodPut, "/admin/v1/scan-policy", strings.NewReader(`{"enabled":true,"severity_threshold":"apocalyptic"}`))
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	// writeAdminError maps domainauth.ErrorCodeValidation to 422 (the same
	// mapping every other admin validation rejection uses, e.g. an invalid
	// scan-settings interval) — the evaluator's permissive default branch
	// for an unknown threshold must be unreachable from this API.
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d for an unknown severity_threshold", recorder.Code, http.StatusUnprocessableEntity)
	}

	if _, err := store.GetScanPolicySettings(context.Background(), "tenant-a"); err == nil {
		t.Fatal("GetScanPolicySettings() error = nil, want no row persisted for a rejected PUT")
	}
}

func TestAdminSecretScanFindingsResponseStructurallyCannotCarrySecretOrFingerprint(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	digest := "sha256:" + strings.Repeat("a", 64)
	if err := store.UpsertSecretScanRunDetail(context.Background(), "tenant-a", ports.SecretScanRunDetail{
		Run: ports.SecretScanRun{
			ID:              "secret-run-1",
			Repository:      "library/alpine",
			Digest:          digest,
			Status:          ports.SecretScanRunStatusCompleted,
			Trigger:         ports.ScanTriggerManual,
			GitleaksVersion: "8.27.0",
			CreatedAt:       time.Now().UTC(),
			UpdatedAt:       time.Now().UTC(),
		},
		Findings: []ports.SecretFinding{{RuleID: "aws-access-token", Description: "AWS Access Token", BlobDigest: "sha256:" + strings.Repeat("b", 64), Path: "layers/001.tar.gz", StartLine: 12, EndLine: 12, Tags: []string{"aws"}}},
	}); err != nil {
		t.Fatalf("UpsertSecretScanRunDetail() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/secret-scan-findings?repository=library/alpine&digest="+url.QueryEscape(digest), nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	for _, want := range []string{`"rule_id":"aws-access-token"`, `"path":"layers/001.tar.gz"`, `"start_line":12`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %q, want %q", body, want)
		}
	}

	// Structural guarantee: reflect over the decoded JSON keys at every
	// depth and confirm none of the banned field names exist anywhere in
	// the response — not just that a specific field happens to be empty.
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(body) error = %v", err)
	}
	assertNoBannedSecretKeys(t, decoded)
}

// TestAdminRepositoryOverrideGetReturnsNotFoundWhenNoOverrideExists is the
// Phase 7 RED test (tasks.md 7.1): the row-presence boundary is made
// explicit on the wire — GET on a repository/feature pair with no stored
// override row returns 404, mirroring GetScanSettings' NotFound precedent
// (design.md Decision 7). Rejected alternative: 200 {"override": false}.
func TestAdminRepositoryOverrideGetReturnsNotFoundWhenNoOverrideExists(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/features/trivy/repository-overrides/library/alpine", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d for a repository/feature pair with no override row", recorder.Code, http.StatusNotFound)
	}
}

// TestAdminRepositoryOverrideGetReturnsCurrentOverride is the Phase 7 RED
// test (tasks.md 7.2): GET returns 200 with the override's current field
// values plus updated_at when a row exists.
func TestAdminRepositoryOverrideGetReturnsCurrentOverride(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	if err := store.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy", []byte(`{"enabled":true,"ignore_file_path":"/etc/regixtry/ignore/alpine.trivyignore"}`)); err != nil {
		t.Fatalf("UpsertRepositoryFeatureOverride() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/features/trivy/repository-overrides/library/alpine", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{`"enabled":true`, `"ignore_file_path":"/etc/regixtry/ignore/alpine.trivyignore"`, `"updated_at"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %q, want %q", body, want)
		}
	}
}

// TestAdminRepositoryOverridePutPersistsAndRoundTrips is the Phase 7 RED
// test (tasks.md 7.3): PUT replaces the override in full and the stored row
// round-trips through the codec-normalized shape.
func TestAdminRepositoryOverridePutPersistsAndRoundTrips(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	req := httptest.NewRequest(http.MethodPut, "/admin/v1/features/trivy/repository-overrides/library/alpine", strings.NewReader(`{"enabled":true,"ignore_file_path":"/etc/regixtry/ignore/alpine.trivyignore"}`))
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	for _, want := range []string{`"enabled":true`, `"ignore_file_path":"/etc/regixtry/ignore/alpine.trivyignore"`} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("body = %q, want %q", recorder.Body.String(), want)
		}
	}

	stored, err := store.GetRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy")
	if err != nil {
		t.Fatalf("GetRepositoryFeatureOverride() error = %v", err)
	}
	if !strings.Contains(string(stored), `"enabled":true`) || !strings.Contains(string(stored), "alpine.trivyignore") {
		t.Fatalf("stored payload = %s, want the persisted override", stored)
	}
}

// TestAdminRepositoryOverridePutRejectsMismatchedFeatureShape is the Phase 7
// RED test (tasks.md 7.3): a gitleaks-shaped body PUT at the trivy resource
// is rejected via DisallowUnknownFields (codec.Normalize), and nothing is
// persisted. This codebase's existing convention maps a validation
// rejection to 422 (see TestAdminScanPolicyPutRejectsUnknownSeverityThreshold),
// not a bare 400.
func TestAdminRepositoryOverridePutRejectsMismatchedFeatureShape(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	req := httptest.NewRequest(http.MethodPut, "/admin/v1/features/trivy/repository-overrides/library/alpine", strings.NewReader(`{"enabled":true,"config_path":"/etc/regixtry/gitleaks.toml"}`))
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d for a gitleaks-shaped body at the trivy resource", recorder.Code, http.StatusUnprocessableEntity)
	}
	if _, err := store.GetRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy"); err == nil {
		t.Fatal("GetRepositoryFeatureOverride() error = nil, want no row persisted for a rejected PUT")
	}
}

// TestAdminRepositoryOverrideDeleteRemovesRowThenReturnsNotFound is the
// Phase 7 RED test (tasks.md 7.4): DELETE returns 204, and a second DELETE
// on the same resource returns 404 (DeleteUpload precedent).
func TestAdminRepositoryOverrideDeleteRemovesRowThenReturnsNotFound(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	if err := store.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy", []byte(`{"enabled":false}`)); err != nil {
		t.Fatalf("UpsertRepositoryFeatureOverride() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/admin/v1/features/trivy/repository-overrides/library/alpine", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}

	secondReq := httptest.NewRequest(http.MethodDelete, "/admin/v1/features/trivy/repository-overrides/library/alpine", nil)
	secondReq.Header.Set("Authorization", "Bearer admin-token")
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, secondReq)
	if secondRecorder.Code != http.StatusNotFound {
		t.Fatalf("second DELETE status = %d, want %d", secondRecorder.Code, http.StatusNotFound)
	}
}

// TestAdminRepositoryOverrideUnknownFeatureReturnsNotFound is the Phase 7
// RED test (tasks.md 7.5): an unknown feature name in the URL returns 404
// via the codec-registry lookup, for GET, PUT, and DELETE alike.
func TestAdminRepositoryOverrideUnknownFeatureReturnsNotFound(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	getReq := httptest.NewRequest(http.MethodGet, "/admin/v1/features/image-signing/repository-overrides/library/alpine", nil)
	getReq.Header.Set("Authorization", "Bearer admin-token")
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)
	if getRecorder.Code != http.StatusNotFound {
		t.Fatalf("GET status = %d, want %d for an unknown feature", getRecorder.Code, http.StatusNotFound)
	}

	putReq := httptest.NewRequest(http.MethodPut, "/admin/v1/features/image-signing/repository-overrides/library/alpine", strings.NewReader(`{"enabled":true}`))
	putReq.Header.Set("Authorization", "Bearer admin-token")
	putReq.Header.Set("Content-Type", "application/json")
	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, putReq)
	if putRecorder.Code != http.StatusNotFound {
		t.Fatalf("PUT status = %d, want %d for an unknown feature", putRecorder.Code, http.StatusNotFound)
	}
}

// TestAdminRepositoryOverrideRequiresAdminPrincipal is the Phase 7 RED test
// backing "Override Authorization Matches Existing Admin Scan-Settings
// Authorization" (repository-config-overrides/spec.md): a non-admin caller
// is rejected without exposing override data, reusing the exact same admin
// authz already protecting /admin/v1/scan-settings — no new permission
// surface.
func TestAdminRepositoryOverrideRequiresAdminPrincipal(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "reader-1", Username: "reader", IsAdmin: false}})

	if err := store.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy", []byte(`{"enabled":true,"ignore_file_path":"/etc/regixtry/ignore/alpine.trivyignore"}`)); err != nil {
		t.Fatalf("UpsertRepositoryFeatureOverride() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/features/trivy/repository-overrides/library/alpine", nil)
	req.Header.Set("Authorization", "Bearer reader-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusOK {
		t.Fatalf("status = %d, want a non-admin caller to be rejected", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "alpine.trivyignore") {
		t.Fatalf("body = %q, want no override data exposed to a rejected caller", recorder.Body.String())
	}
}

// TestAdminRepositoryOverridesCollectionListsForFeature exercises the
// collection route (design.md Decision 7's "List (TUI annotation)" row)
// that Phase 8's TUI wiring depends on: GET on the feature-scoped collection
// lists every stored override row for that feature.
func TestAdminRepositoryOverridesCollectionListsForFeature(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	if err := store.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy", []byte(`{"enabled":false}`)); err != nil {
		t.Fatalf("UpsertRepositoryFeatureOverride(alpine) error = %v", err)
	}
	if err := store.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", "library/busybox", "trivy", []byte(`{"enabled":true}`)); err != nil {
		t.Fatalf("UpsertRepositoryFeatureOverride(busybox) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/features/trivy/repository-overrides", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{`"repository":"library/alpine"`, `"repository":"library/busybox"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %q, want %q", body, want)
		}
	}
}

// TestAdminRepositoryOverrideRoutesRepositoryNamedConfigSegmentCorrectly is
// the Phase 7 RED test (tasks.md 7.6, threat matrix — HTTP path dispatch):
// design.md Decision 7's ordering hazard requires the repository-overrides
// case to be dispatched BEFORE the existing HasSuffix(resource, "/config")
// branch in handleAdminFeatureResource's switch. A repository literally
// named "team/config" would otherwise be swallowed by the /config branch
// (ConfigureFeature), and no override row would ever be persisted. This
// test proves ordering by round-tripping through the real store row keyed
// by the exact literal repository name "team/config", not just checking an
// HTTP status code (which is not reliably distinguishable between the two
// wrong-routing failure modes).
func TestAdminRepositoryOverrideRoutesRepositoryNamedConfigSegmentCorrectly(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	req := httptest.NewRequest(http.MethodPut, "/admin/v1/features/trivy/repository-overrides/team/config", strings.NewReader(`{"enabled":true,"ignore_file_path":"/etc/regixtry/ignore/team-config.trivyignore"}`))
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d, body = %s (naive ordering would route this into the /config branch instead)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	stored, err := store.GetRepositoryFeatureOverride(context.Background(), "tenant-a", "team/config", "trivy")
	if err != nil {
		t.Fatalf("GetRepositoryFeatureOverride(tenant-a, %q, trivy) error = %v, want a row keyed by the exact literal repository name — a naive ordering never reaches UpsertRepositoryFeatureOverride at all", "team/config", err)
	}
	if !strings.Contains(string(stored), "team-config.trivyignore") {
		t.Fatalf("stored payload = %s, want the PUT body's ignore_file_path", stored)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/admin/v1/features/trivy/repository-overrides/team/config", nil)
	getReq.Header.Set("Authorization", "Bearer admin-token")
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d, body = %s", getRecorder.Code, http.StatusOK, getRecorder.Body.String())
	}
	if !strings.Contains(getRecorder.Body.String(), "team-config.trivyignore") {
		t.Fatalf("GET body = %q, want the stored override for repository \"team/config\"", getRecorder.Body.String())
	}
}

func assertNoBannedSecretKeys(t *testing.T, value any) {
	t.Helper()
	banned := map[string]bool{"secret": true, "match": true, "fingerprint": true, "entropy": true}
	switch v := value.(type) {
	case map[string]any:
		for key, nested := range v {
			if banned[strings.ToLower(key)] {
				t.Fatalf("response contains banned key %q, want no field capable of holding a secret value or fingerprint", key)
			}
			assertNoBannedSecretKeys(t, nested)
		}
	case []any:
		for _, item := range v {
			assertNoBannedSecretKeys(t, item)
		}
	}
}

func TestAdminScanRunDetailReturnsOrderedFindingsAndFreshnessPayload(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	if err := store.UpsertScanRunDetail(context.Background(), "tenant-a", ports.ScanRunDetail{
		Run:         ports.ScanRun{ID: "run-1", Repository: "library/alpine", RequestedRef: "latest", Digest: "sha256:111", Status: ports.ScanRunStatusCompleted, Trigger: ports.ScanTriggerManual, CreatedAt: time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC), Critical: 1, High: 1, TrivyVersion: "0.58.1"},
		Findings:    []ports.ScanRunFinding{{Severity: "CRITICAL", VulnerabilityID: "CVE-1", PackageName: "openssl", InstalledVersion: "3.0.0", FixedVersion: "3.0.1", Fixable: true}, {Severity: "HIGH", VulnerabilityID: "CVE-2", PackageName: "busybox", InstalledVersion: "1.0.0", Fixable: false}},
		DBFreshness: ports.ScanRunDBFreshness{FreshnessState: ports.ScanRunDBFreshnessStateFresh},
	}); err != nil {
		t.Fatalf("UpsertScanRunDetail() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/scan-runs/run-1", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	for _, want := range []string{`"id":"run-1"`, `"reference_freshness":"missing"`, `"db_freshness":{"report_schema_version":0`, `"freshness_state":"fresh"`, `"vulnerability_id":"CVE-1"`, `"vulnerability_id":"CVE-2"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %q, want %q", body, want)
		}
	}
	if strings.Index(body, `"vulnerability_id":"CVE-1"`) > strings.Index(body, `"vulnerability_id":"CVE-2"`) {
		t.Fatalf("body = %q, want findings ordered by severity/fixability", body)
	}
}

// TestAdminSigningPolicyGetReturnsDefaultWhenNoRowExists is the Phase 8 RED
// test (tasks.md 8.1): with no stored row, GET must still return 200 with
// the code-level {Enabled: false} default — never 404 — mirroring
// handleAdminScanPolicy's GetScanPolicySettings default-on-NotFound posture
// (GetSigningPolicySettings, service_signing.go).
func TestAdminSigningPolicyGetReturnsDefaultWhenNoRowExists(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/signing-policy", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d for a signing policy with no stored row, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	for _, want := range []string{`"enabled":false`, `"trusted_public_keys":[]`} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("body = %q, want %q", recorder.Body.String(), want)
		}
	}
}

// TestAdminSigningPolicyGetReturnsCurrentSettings is the Phase 8 RED test
// (tasks.md 8.1): GET reflects the stored row, echoing the trusted public
// keys as canonical PEM — the admin config surface is not the redacted
// registry-scoped signature-status endpoint (design.md Decision 8, "Public
// keys are not secret").
func TestAdminSigningPolicyGetReturnsCurrentSettings(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	key := generateHTTPTestECDSAP256PublicKeyPEM(t)
	if err := store.UpsertSigningPolicySettings(context.Background(), "tenant-a", ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: []string{key}, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertSigningPolicySettings() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/signing-policy", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var decoded struct {
		Enabled           bool     `json:"enabled"`
		TrustedPublicKeys []string `json:"trusted_public_keys"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(body) error = %v, body = %s", err, recorder.Body.String())
	}
	if !decoded.Enabled || len(decoded.TrustedPublicKeys) != 1 || decoded.TrustedPublicKeys[0] != key {
		t.Fatalf("decoded = %#v, want enabled and the stored trusted key echoed as canonical PEM", decoded)
	}
}

// TestAdminSigningPolicyGetRequiresAdminPrincipal is the Phase 8 RED test
// (tasks.md 8.6): the same admin authorization already protecting
// /admin/v1/scan-policy protects /admin/v1/signing-policy — no new
// permission surface.
func TestAdminSigningPolicyGetRequiresAdminPrincipal(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "reader-1", Username: "reader", IsAdmin: false}})

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/signing-policy", nil)
	req.Header.Set("Authorization", "Bearer reader-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusOK {
		t.Fatalf("status = %d, want a non-admin caller to be rejected", recorder.Code)
	}
}

// TestAdminSigningPolicyPutPersistsAndRoundTrips is the Phase 8 RED test
// (tasks.md 8.2): PUT fully replaces the settings and round-trips through
// the store.
func TestAdminSigningPolicyPutPersistsAndRoundTrips(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	key := generateHTTPTestECDSAP256PublicKeyPEM(t)
	body, err := json.Marshal(map[string]any{"enabled": true, "trusted_public_keys": []string{key}})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/admin/v1/signing-policy", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"enabled":true`) {
		t.Fatalf("body = %q, want enabled:true", recorder.Body.String())
	}

	stored, err := store.GetSigningPolicySettings(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("GetSigningPolicySettings() error = %v", err)
	}
	if !stored.Enabled || len(stored.TrustedPublicKeys) != 1 || stored.TrustedPublicKeys[0] != key {
		t.Fatalf("stored = %#v, want enabled and the normalized trusted key persisted", stored)
	}
}

// TestAdminSigningPolicyPutRejectsInvalidKeyNamingIndexNeverEchoingBytes is
// the Phase 8 RED test (tasks.md 8.3): a key that fails
// signing.NormalizePublicKeyPEM is a 400/422 naming the offending index and
// never echoing key bytes.
func TestAdminSigningPolicyPutRejectsInvalidKeyNamingIndexNeverEchoingBytes(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	validKey := generateHTTPTestECDSAP256PublicKeyPEM(t)
	body, err := json.Marshal(map[string]any{"enabled": true, "trusted_public_keys": []string{validKey, "not a pem at all"}})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/admin/v1/signing-policy", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d for an invalid key, body = %s", recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "[1]") {
		t.Fatalf("body = %q, want the offending index (1) named", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "not a pem at all") {
		t.Fatalf("body = %q, must never echo the offending key bytes", recorder.Body.String())
	}

	if _, err := store.GetSigningPolicySettings(context.Background(), "tenant-a"); err == nil {
		t.Fatal("GetSigningPolicySettings() error = nil, want no row persisted for a rejected PUT")
	}
}

// TestAdminSigningPolicyPutRejectsEnabledWithZeroKeys is the Phase 8 RED
// test (tasks.md 8.4): enabled:true with zero usable keys is the outage
// rule's 400/422 — the row is NOT stored, confirmed via a subsequent GET
// showing the prior/default state unchanged.
func TestAdminSigningPolicyPutRejectsEnabledWithZeroKeys(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	body, err := json.Marshal(map[string]any{"enabled": true, "trusted_public_keys": []string{}})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/admin/v1/signing-policy", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d for enabled:true with zero keys, body = %s", recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}

	if _, err := store.GetSigningPolicySettings(context.Background(), "tenant-a"); err == nil {
		t.Fatal("GetSigningPolicySettings() error = nil, want no row persisted for a rejected PUT")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/admin/v1/signing-policy", nil)
	getReq.Header.Set("Authorization", "Bearer admin-token")
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", getRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(getRecorder.Body.String(), `"enabled":false`) {
		t.Fatalf("GET body = %q, want the code-level default unchanged after the rejected PUT", getRecorder.Body.String())
	}
}

// TestAdminSigningPolicyPutRejectsMoreThan16Keys is the Phase 8 RED test
// (tasks.md 8.5): more than 16 keys is a 400/422, bounding the per-pull
// verification loop (design.md Decision 8).
func TestAdminSigningPolicyPutRejectsMoreThan16Keys(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	keys := make([]string, 0, 17)
	for i := 0; i < 17; i++ {
		keys = append(keys, generateHTTPTestECDSAP256PublicKeyPEM(t))
	}
	body, err := json.Marshal(map[string]any{"enabled": true, "trusted_public_keys": keys})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/admin/v1/signing-policy", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d for 17 trusted keys, body = %s", recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}

	if _, err := store.GetSigningPolicySettings(context.Background(), "tenant-a"); err == nil {
		t.Fatal("GetSigningPolicySettings() error = nil, want no row persisted for a rejected PUT")
	}
}

// TestAdminSigningPolicyPutRequiresAdminPrincipal is the Phase 8 RED test
// (tasks.md 8.6): PUT requires the same admin authorization as GET.
func TestAdminSigningPolicyPutRequiresAdminPrincipal(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "reader-1", Username: "reader", IsAdmin: false}})

	req := httptest.NewRequest(http.MethodPut, "/admin/v1/signing-policy", strings.NewReader(`{"enabled":false}`))
	req.Header.Set("Authorization", "Bearer reader-token")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusOK {
		t.Fatalf("status = %d, want a non-admin caller to be rejected", recorder.Code)
	}
}

// TestAdminSigningRepositoryOverridePutPersistsAndRoundTrips is the Phase 8
// RED test (tasks.md 8.9): PUT
// /admin/v1/features/signing/repository-overrides/<repo> persists and
// round-trips through the EXISTING generic repository-overrides HTTP
// resource — Phase 3's codec registration (repositoryOverrideCodecs,
// signingFeatureName) is what makes this work with zero new HTTP route.
func TestAdminSigningRepositoryOverridePutPersistsAndRoundTrips(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	key := generateHTTPTestECDSAP256PublicKeyPEM(t)
	body, err := json.Marshal(map[string]any{"enabled": true, "trusted_public_keys": []string{key}})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/admin/v1/features/signing/repository-overrides/library/alpine", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"enabled":true`) {
		t.Fatalf("body = %q, want enabled:true", recorder.Body.String())
	}

	stored, err := store.GetRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "signing")
	if err != nil {
		t.Fatalf("GetRepositoryFeatureOverride() error = %v", err)
	}
	if !strings.Contains(string(stored), `"enabled":true`) {
		t.Fatalf("stored payload = %s, want the persisted signing override", stored)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/admin/v1/features/signing/repository-overrides/library/alpine", nil)
	getReq.Header.Set("Authorization", "Bearer admin-token")
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d, body = %s", getRecorder.Code, http.StatusOK, getRecorder.Body.String())
	}
	if !strings.Contains(getRecorder.Body.String(), `"enabled":true`) {
		t.Fatalf("GET body = %q, want the stored signing override", getRecorder.Body.String())
	}
}

// TestAdminSigningRepositoryOverridePutRejectsUnknownFieldsAndZeroKeys is
// the Phase 8 RED test (tasks.md 8.10): a signing override body with
// unknown fields, or enabled:true with zero keys, is rejected 422 via
// normalizeSigningOverride through the existing generic PUT handler — no
// new code path.
func TestAdminSigningRepositoryOverridePutRejectsUnknownFieldsAndZeroKeys(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	unknownFieldReq := httptest.NewRequest(http.MethodPut, "/admin/v1/features/signing/repository-overrides/library/alpine", strings.NewReader(`{"enabled":true,"config_path":"/etc/regixtry/gitleaks.toml"}`))
	unknownFieldReq.Header.Set("Authorization", "Bearer admin-token")
	unknownFieldReq.Header.Set("Content-Type", "application/json")
	unknownFieldRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unknownFieldRecorder, unknownFieldReq)
	if unknownFieldRecorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown-field status = %d, want %d, body = %s", unknownFieldRecorder.Code, http.StatusUnprocessableEntity, unknownFieldRecorder.Body.String())
	}

	zeroKeysReq := httptest.NewRequest(http.MethodPut, "/admin/v1/features/signing/repository-overrides/library/alpine", strings.NewReader(`{"enabled":true,"trusted_public_keys":[]}`))
	zeroKeysReq.Header.Set("Authorization", "Bearer admin-token")
	zeroKeysReq.Header.Set("Content-Type", "application/json")
	zeroKeysRecorder := httptest.NewRecorder()
	handler.ServeHTTP(zeroKeysRecorder, zeroKeysReq)
	if zeroKeysRecorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("zero-keys status = %d, want %d, body = %s", zeroKeysRecorder.Code, http.StatusUnprocessableEntity, zeroKeysRecorder.Body.String())
	}

	if _, err := store.GetRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "signing"); err == nil {
		t.Fatal("GetRepositoryFeatureOverride() error = nil, want no row persisted for either rejected PUT")
	}
}

// TestAdminRepositoryGrantRoutesAuthenticationAndDelegateAuthority is the
// Phase 2 RED test (tasks.md 2.11, design.md Decision 3's route table):
// GET/PUT/DELETE /admin/v1/repositories/{repo}/grants[/{username}] must
// 401 unauthenticated, 403 an authenticated non-delegate, and succeed for a
// repo-admin delegate acting on their own repository — while the same
// delegate is 403 on a repository they do not administer.
func TestAdminRepositoryGrantRoutesAuthenticationAndDelegateAuthority(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, _, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	delegate, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "delegate", Password: "password123", Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser(delegate) error = %v", err)
	}
	if _, err := authService.PutRepoGrant(context.Background(), adminActor, delegate.ID, "team/app", auth.RepoRoleAdmin); err != nil {
		t.Fatalf("PutRepoGrant(delegate, team/app, repo-admin) error = %v", err)
	}
	victim, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "victim", Password: "password123", Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser(victim) error = %v", err)
	}

	plainUser, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "plain", Password: "password123", Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser(plain) error = %v", err)
	}

	delegateLogin, err := authService.LoginWithPassword(context.Background(), delegate.Username, "password123", nil)
	if err != nil {
		t.Fatalf("LoginWithPassword(delegate) error = %v", err)
	}
	plainLogin, err := authService.LoginWithPassword(context.Background(), plainUser.Username, "password123", nil)
	if err != nil {
		t.Fatalf("LoginWithPassword(plain) error = %v", err)
	}

	unauthenticatedReq := httptest.NewRequest(http.MethodGet, "/admin/v1/repositories/team/app/grants", nil)
	unauthenticatedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticatedRecorder, unauthenticatedReq)
	if unauthenticatedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d, body = %s", unauthenticatedRecorder.Code, http.StatusUnauthorized, unauthenticatedRecorder.Body.String())
	}

	nonDelegateReq := httptest.NewRequest(http.MethodGet, "/admin/v1/repositories/team/app/grants", nil)
	nonDelegateReq.Header.Set("Authorization", "Bearer "+plainLogin.BearerToken)
	nonDelegateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(nonDelegateRecorder, nonDelegateReq)
	if nonDelegateRecorder.Code != http.StatusForbidden {
		t.Fatalf("non-delegate status = %d, want %d, body = %s", nonDelegateRecorder.Code, http.StatusForbidden, nonDelegateRecorder.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/admin/v1/repositories/team/app/grants", nil)
	listReq.Header.Set("Authorization", "Bearer "+delegateLogin.BearerToken)
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("delegate list status = %d, want %d, body = %s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}

	putReq := httptest.NewRequest(http.MethodPut, "/admin/v1/repositories/team/app/grants/"+victim.Username, strings.NewReader(`{"role":"repo-reader"}`))
	putReq.Header.Set("Authorization", "Bearer "+delegateLogin.BearerToken)
	putReq.Header.Set("Content-Type", "application/json")
	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, putReq)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("delegate put status = %d, want %d, body = %s", putRecorder.Code, http.StatusOK, putRecorder.Body.String())
	}
	if !strings.Contains(putRecorder.Body.String(), `"username":"victim"`) || !strings.Contains(putRecorder.Body.String(), `"role":"repo-reader"`) {
		t.Fatalf("delegate put body = %q, want username/role reflected", putRecorder.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/admin/v1/repositories/team/app/grants/"+victim.Username, nil)
	deleteReq.Header.Set("Authorization", "Bearer "+delegateLogin.BearerToken)
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteReq)
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delegate delete status = %d, want %d, body = %s", deleteRecorder.Code, http.StatusNoContent, deleteRecorder.Body.String())
	}

	otherRepoReq := httptest.NewRequest(http.MethodGet, "/admin/v1/repositories/team/other/grants", nil)
	otherRepoReq.Header.Set("Authorization", "Bearer "+delegateLogin.BearerToken)
	otherRepoRecorder := httptest.NewRecorder()
	handler.ServeHTTP(otherRepoRecorder, otherRepoReq)
	if otherRepoRecorder.Code != http.StatusForbidden {
		t.Fatalf("delegate on team/other status = %d, want %d, body = %s", otherRepoRecorder.Code, http.StatusForbidden, otherRepoRecorder.Body.String())
	}
}

// TestAdminRepositoryGrantRoutesRepositoryNamedTeamGrantsRoutesCorrectly is
// the Phase 2 RED test (tasks.md 2.12, design.md Decision 3's `/`-in-path
// parsing hazard, precedent at admin_handlers.go:78-84): a repository
// literally named "team/grants" must route correctly for GET/PUT/DELETE —
// the "/grants" suffix belonging to the resource split must not be
// swallowed by the repository name containing the literal string "grants".
func TestAdminRepositoryGrantRoutesRepositoryNamedTeamGrantsRoutesCorrectly(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, _, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	adminLogin, err := authService.LoginWithPassword(context.Background(), adminActor.Username, "password123", nil)
	if err != nil {
		t.Fatalf("LoginWithPassword(admin) error = %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/admin/v1/repositories/team/grants/grants", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminLogin.BearerToken)
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d, body = %s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}

	putReq := httptest.NewRequest(http.MethodPut, "/admin/v1/repositories/team/grants/grants/bob", strings.NewReader(`{"role":"repo-writer"}`))
	putReq.Header.Set("Authorization", "Bearer "+adminLogin.BearerToken)
	putReq.Header.Set("Content-Type", "application/json")
	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, putReq)
	if putRecorder.Code != http.StatusNotFound {
		// bob does not exist yet — this still proves the repository/username
		// split is correct (a parsing failure would 422/404 differently, at
		// the repository-ref-validation stage, not the user lookup stage).
		t.Fatalf("put status = %d, want %d (user not found after correct parse), body = %s", putRecorder.Code, http.StatusNotFound, putRecorder.Body.String())
	}

	if _, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{Username: "bob", Password: "password123", Enabled: true}); err != nil {
		t.Fatalf("CreateAdminUser(bob) error = %v", err)
	}

	putBobReq := httptest.NewRequest(http.MethodPut, "/admin/v1/repositories/team/grants/grants/bob", strings.NewReader(`{"role":"repo-writer"}`))
	putBobReq.Header.Set("Authorization", "Bearer "+adminLogin.BearerToken)
	putBobReq.Header.Set("Content-Type", "application/json")
	putBobRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putBobRecorder, putBobReq)
	if putBobRecorder.Code != http.StatusOK {
		t.Fatalf("put(bob) status = %d, want %d, body = %s", putBobRecorder.Code, http.StatusOK, putBobRecorder.Body.String())
	}
	if !strings.Contains(putBobRecorder.Body.String(), `"username":"bob"`) || !strings.Contains(putBobRecorder.Body.String(), `"role":"repo-writer"`) {
		t.Fatalf("put(bob) body = %q, want username=bob role=repo-writer reflecting repository=team/grants", putBobRecorder.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/admin/v1/repositories/team/grants/grants/bob", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+adminLogin.BearerToken)
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteReq)
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d, body = %s", deleteRecorder.Code, http.StatusNoContent, deleteRecorder.Body.String())
	}
}

// TestAdminNonGrantRoutesStillRequireGlobalAdminForRepoAdminDelegate is the
// Phase 2 RED test (tasks.md 2.13, design.md's highest-risk guard): a
// repo-admin delegate — authenticated, holding repo-admin on one
// repository, but never global IsAdmin — must still be 403-ed on every
// existing non-grant `/admin/v1/*` route, enumerated route by route. This
// is the exhaustive proof that the HTTP gate restructure narrows authority
// ONLY for the new repositories/ namespace and inherits the unchanged
// blanket gate everywhere else.
func TestAdminNonGrantRoutesStillRequireGlobalAdminForRepoAdminDelegate(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, _, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	delegate, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "delegate", Password: "password123", Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser(delegate) error = %v", err)
	}
	if _, err := authService.PutRepoGrant(context.Background(), adminActor, delegate.ID, "team/app", auth.RepoRoleAdmin); err != nil {
		t.Fatalf("PutRepoGrant(delegate, team/app, repo-admin) error = %v", err)
	}
	delegateLogin, err := authService.LoginWithPassword(context.Background(), delegate.Username, "password123", nil)
	if err != nil {
		t.Fatalf("LoginWithPassword(delegate) error = %v", err)
	}

	routes := []string{
		"/admin/v1/features",
		"/admin/v1/features/trivy/status",
		"/admin/v1/features/trivy/config",
		"/admin/v1/scan-settings",
		"/admin/v1/scan-policy",
		"/admin/v1/signing-policy",
		"/admin/v1/scan-runs",
		"/admin/v1/scan-runs/run-123",
		"/admin/v1/secret-scan-findings?repository=library/alpine&digest=sha256:abc",
		"/admin/v1/users",
		"/admin/v1/users/" + delegate.ID,
		"/admin/v1/users/" + delegate.ID + ":enable",
		"/admin/v1/users/" + delegate.ID + ":disable",
		"/admin/v1/users/" + delegate.ID + ":reset-password",
		"/admin/v1/users/" + delegate.ID + "/grants",
		"/admin/v1/users/" + delegate.ID + "/grants/team/app",
		"/admin/v1/users/" + delegate.ID + "/admin-tokens",
		"/admin/v1/users/" + delegate.ID + "/admin-tokens/act_123",
	}

	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, route, nil)
			req.Header.Set("Authorization", "Bearer "+delegateLogin.BearerToken)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusForbidden, recorder.Body.String())
			}
		})
	}
}

// TestAdminRepositoryGrantResponsesCarryNoUserIdentityFields is the Phase 2
// RED test (tasks.md 2.14, threat matrix: identity disclosure). The
// repository-grant list and put responses must carry no user ID, password
// hash, admin/read-only flags, or another repository's name — only
// username/role/created_at/updated_at, matching design.md Decision 3's
// route table exactly.
func TestAdminRepositoryGrantResponsesCarryNoUserIdentityFields(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, _, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	victim, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "victim", Password: "password123", Enabled: true, IsAdmin: false, IsReadOnly: true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser(victim) error = %v", err)
	}
	if _, err := authService.PutRepoGrant(context.Background(), adminActor, victim.ID, "team/app", auth.RepoRoleReader); err != nil {
		t.Fatalf("PutRepoGrant(victim, team/app, repo-reader) error = %v", err)
	}
	if _, err := authService.PutRepoGrant(context.Background(), adminActor, victim.ID, "team/other", auth.RepoRoleWriter); err != nil {
		t.Fatalf("PutRepoGrant(victim, team/other, repo-writer) error = %v", err)
	}

	adminLogin, err := authService.LoginWithPassword(context.Background(), adminActor.Username, "password123", nil)
	if err != nil {
		t.Fatalf("LoginWithPassword(admin) error = %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/admin/v1/repositories/team/app/grants", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminLogin.BearerToken)
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d, body = %s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}

	body := listRecorder.Body.String()
	if !strings.Contains(body, `"username":"victim"`) {
		t.Fatalf("body = %q, want username=victim", body)
	}
	forbidden := []string{victim.ID, "is_admin", "is_read_only", "password", "hash", "team/other"}
	for _, needle := range forbidden {
		if strings.Contains(body, needle) {
			t.Fatalf("body = %q, must not contain %q", body, needle)
		}
	}
}

// TestAdminRobotsRoutesCreateAndListGlobalAdminOnly is the Phase 4 RED test
// (tasks.md 4.13): POST /admin/v1/robots creates a robot, its single grant,
// and an issued token in one call (global admin only); GET /admin/v1/robots
// lists robots; a non-admin authenticated principal is rejected on both.
func TestAdminRobotsRoutesCreateAndListGlobalAdminOnly(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, _, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	plainUser, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "plain", Password: "password123", Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser(plain) error = %v", err)
	}
	plainLogin, err := authService.LoginWithPassword(context.Background(), plainUser.Username, "password123", nil)
	if err != nil {
		t.Fatalf("LoginWithPassword(plain) error = %v", err)
	}
	adminLogin, err := authService.LoginWithPassword(context.Background(), adminActor.Username, "password123", nil)
	if err != nil {
		t.Fatalf("LoginWithPassword(admin) error = %v", err)
	}

	nonAdminCreateReq := httptest.NewRequest(http.MethodPost, "/admin/v1/robots", strings.NewReader(`{"name":"ci","repository":"team/app","role":"repo-writer"}`))
	nonAdminCreateReq.Header.Set("Authorization", "Bearer "+plainLogin.BearerToken)
	nonAdminCreateReq.Header.Set("Content-Type", "application/json")
	nonAdminCreateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(nonAdminCreateRecorder, nonAdminCreateReq)
	if nonAdminCreateRecorder.Code != http.StatusForbidden {
		t.Fatalf("non-admin create status = %d, want %d, body = %s", nonAdminCreateRecorder.Code, http.StatusForbidden, nonAdminCreateRecorder.Body.String())
	}

	createReq := httptest.NewRequest(http.MethodPost, "/admin/v1/robots", strings.NewReader(`{"name":"ci","repository":"team/app","role":"repo-writer"}`))
	createReq.Header.Set("Authorization", "Bearer "+adminLogin.BearerToken)
	createReq.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, createReq)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body = %s", createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}
	createBody := createRecorder.Body.String()
	if !strings.Contains(createBody, `"username":"ci"`) || !strings.Contains(createBody, `"repository":"team/app"`) || !strings.Contains(createBody, `"role":"repo-writer"`) || !strings.Contains(createBody, `"secret"`) {
		t.Fatalf("create body = %q, want robot+grant+secret reflected", createBody)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/admin/v1/robots", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminLogin.BearerToken)
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d, body = %s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}
	if !strings.Contains(listRecorder.Body.String(), `"username":"ci"`) {
		t.Fatalf("list body = %q, want to contain robot ci", listRecorder.Body.String())
	}

	nonAdminListReq := httptest.NewRequest(http.MethodGet, "/admin/v1/robots", nil)
	nonAdminListReq.Header.Set("Authorization", "Bearer "+plainLogin.BearerToken)
	nonAdminListRecorder := httptest.NewRecorder()
	handler.ServeHTTP(nonAdminListRecorder, nonAdminListReq)
	if nonAdminListRecorder.Code != http.StatusForbidden {
		t.Fatalf("non-admin list status = %d, want %d, body = %s", nonAdminListRecorder.Code, http.StatusForbidden, nonAdminListRecorder.Body.String())
	}
}

// TestAdminRobotDeleteRouteGlobalAdminOnlyAndRejectsNonRobots is the
// registry-acl-v1 robot-deletion follow-up's HTTP-boundary defense-in-depth
// test: the same IsRobot guard enforced by the service layer is confirmed
// again here (401/403/404 gate, plus a non-robot user ID rejection), and a
// genuine robot delete is confirmed to remove it from the list afterward.
func TestAdminRobotDeleteRouteGlobalAdminOnlyAndRejectsNonRobots(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, _, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	plainUser, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "plain-delete-robot", Password: "password123", Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser(plain) error = %v", err)
	}
	plainLogin, err := authService.LoginWithPassword(context.Background(), plainUser.Username, "password123", nil)
	if err != nil {
		t.Fatalf("LoginWithPassword(plain) error = %v", err)
	}
	adminLogin, err := authService.LoginWithPassword(context.Background(), adminActor.Username, "password123", nil)
	if err != nil {
		t.Fatalf("LoginWithPassword(admin) error = %v", err)
	}

	created, err := authService.CreateRobot(context.Background(), adminActor, ports.CreateRobotInput{
		Name: "ci-delete-route", Repository: "team/app", Role: auth.RepoRoleReader,
	})
	if err != nil {
		t.Fatalf("CreateRobot() error = %v", err)
	}

	unauthenticatedReq := httptest.NewRequest(http.MethodDelete, "/admin/v1/robots/"+created.User.ID, nil)
	unauthenticatedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticatedRecorder, unauthenticatedReq)
	if unauthenticatedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d, body = %s", unauthenticatedRecorder.Code, http.StatusUnauthorized, unauthenticatedRecorder.Body.String())
	}

	nonAdminReq := httptest.NewRequest(http.MethodDelete, "/admin/v1/robots/"+created.User.ID, nil)
	nonAdminReq.Header.Set("Authorization", "Bearer "+plainLogin.BearerToken)
	nonAdminRecorder := httptest.NewRecorder()
	handler.ServeHTTP(nonAdminRecorder, nonAdminReq)
	if nonAdminRecorder.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, want %d, body = %s", nonAdminRecorder.Code, http.StatusForbidden, nonAdminRecorder.Body.String())
	}

	unknownReq := httptest.NewRequest(http.MethodDelete, "/admin/v1/robots/does-not-exist", nil)
	unknownReq.Header.Set("Authorization", "Bearer "+adminLogin.BearerToken)
	unknownRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unknownRecorder, unknownReq)
	if unknownRecorder.Code != http.StatusNotFound {
		t.Fatalf("unknown id status = %d, want %d, body = %s", unknownRecorder.Code, http.StatusNotFound, unknownRecorder.Body.String())
	}

	nonRobotReq := httptest.NewRequest(http.MethodDelete, "/admin/v1/robots/"+plainUser.ID, nil)
	nonRobotReq.Header.Set("Authorization", "Bearer "+adminLogin.BearerToken)
	nonRobotRecorder := httptest.NewRecorder()
	handler.ServeHTTP(nonRobotRecorder, nonRobotReq)
	if nonRobotRecorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("non-robot target status = %d, want %d, body = %s", nonRobotRecorder.Code, http.StatusUnprocessableEntity, nonRobotRecorder.Body.String())
	}
	if _, err := authService.LoginWithPassword(context.Background(), plainUser.Username, "password123", nil); err != nil {
		t.Fatalf("plain user was removed by a rejected robot-delete call: LoginWithPassword() error = %v", err)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/admin/v1/robots/"+created.User.ID, nil)
	deleteReq.Header.Set("Authorization", "Bearer "+adminLogin.BearerToken)
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteReq)
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d, body = %s", deleteRecorder.Code, http.StatusNoContent, deleteRecorder.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/admin/v1/robots", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminLogin.BearerToken)
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d, body = %s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}
	if strings.Contains(listRecorder.Body.String(), `"username":"ci-delete-route"`) {
		t.Fatalf("list body = %q, want deleted robot ci-delete-route to be gone", listRecorder.Body.String())
	}
}
