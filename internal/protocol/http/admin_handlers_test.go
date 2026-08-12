package regixtryhttp

import (
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
