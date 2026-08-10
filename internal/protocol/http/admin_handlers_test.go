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

	if err := store.UpsertScanSettings(context.Background(), "tenant-a", ports.ScanSettings{Enabled: true, ScheduleEnabled: true, Interval: 6 * time.Hour, Timeout: 10 * time.Minute, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 2, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertScanSettings() error = %v", err)
	}
	if err := store.UpsertTrivyRuntimeState(context.Background(), "tenant-a", ports.TrivyRuntimeState{Status: ports.TrivyRuntimeStatusMigrationRequired, MigrationHint: `legacy binary_path "/tmp/README.sh" requires managed reinstall and will never be executed`, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertTrivyRuntimeState() error = %v", err)
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
