package regixtryhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
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
