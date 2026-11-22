package regixtryhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	appregixtry "regixtry/internal/app/regixtry"
	"regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

type countingFeatureManager struct {
	calls   atomic.Int32
	version atomic.Value
}

func (c *countingFeatureManager) record(v string) (ports.FeatureRuntimeState, error) {
	c.calls.Add(1)
	c.version.Store(v)
	return ports.FeatureRuntimeState{Status: ports.FeatureRuntimeStatusReady}, nil
}
func (c *countingFeatureManager) Install(_ context.Context, v string, _ func(ports.FeatureRuntimeProgress)) (ports.FeatureRuntimeState, error) {
	return c.record(v)
}
func (c *countingFeatureManager) Upgrade(_ context.Context, v string, _ func(ports.FeatureRuntimeProgress)) (ports.FeatureRuntimeState, error) {
	return c.record(v)
}
func (c *countingFeatureManager) Rollback(context.Context) (ports.FeatureRuntimeState, error) {
	return ports.FeatureRuntimeState{}, nil
}
func (c *countingFeatureManager) Status(context.Context) (ports.FeatureRuntimeState, error) {
	return ports.FeatureRuntimeState{}, nil
}
func (c *countingFeatureManager) LatestVersion(context.Context) (string, error) { return "", nil }

var _ appregixtry.FeatureRuntimeManager = (*countingFeatureManager)(nil)

func TestAdminFeatureInstallUpgradeRejectsHostileVersions(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	service := appregixtry.NewService(blobStore, store, allowAllAccessController{}, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	manager := &countingFeatureManager{}
	service.SetFeatureRuntimeManager("trivy", manager)
	handler := NewRouter(service, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	post := func(action string, version string) int {
		body := `{"version":"` + version + `"}`
		req := httptest.NewRequest(http.MethodPost, "/admin/v1/features/trivy:"+action, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	for _, action := range []string{"install", "upgrade"} {
		for _, version := range []string{"../../evil", "1.2.3/../x", "1.2.3?a=b", "1.2.3%2fx", "https://evil.example/x", "1.2", "latest", strings.Repeat("1", 5000) + ".0.0", `1.2.3\\n`} {
			if code := post(action, version); code != http.StatusUnprocessableEntity {
				t.Errorf("%s version %.30q status = %d, want 422", action, version, code)
			}
		}
	}
	if manager.calls.Load() != 0 {
		t.Fatalf("manager called %d times for hostile versions, want 0", manager.calls.Load())
	}

	for _, action := range []string{"install", "upgrade"} {
		if code := post(action, "v0.58.0"); code != http.StatusOK {
			t.Errorf("%s valid version status = %d, want 200", action, code)
		}
		if got := manager.version.Load(); got != "v0.58.0" {
			t.Errorf("%s manager version = %v, want v0.58.0", action, got)
		}
		if code := post(action, ""); code != http.StatusOK {
			t.Errorf("%s empty version status = %d, want 200 (latest)", action, code)
		}
	}
}
