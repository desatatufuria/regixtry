package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appauth "regixtry/internal/app/auth"
	appregixtry "regixtry/internal/app/regixtry"
	authpostgres "regixtry/internal/infra/auth/postgres"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
	regixtryhttp "regixtry/internal/protocol/http"
)

// TestAdminClientDecodesTheLatestOnlyScanFieldsFromTheRealRouter reads the
// additive latest-only fields (run freshness and the sweep summary) through the
// real router and sqlite stores, then renders both screens from them.
func TestAdminClientDecodesTheLatestOnlyScanFieldsFromTheRealRouter(t *testing.T) {
	dir := t.TempDir()
	blob, err := fsblob.New(filepath.Join(dir, "c"))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := metadata.New(filepath.Join(dir, "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer meta.Close()
	authStore, err := authpostgres.NewWithDriver("sqlite", filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer authStore.Close()
	access := ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "regixtry"})
	authService := appauth.NewService(authStore)
	service := appregixtry.NewService(blob, meta, access, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	router := regixtryhttp.NewRouter(service, authService)
	ctx := context.Background()
	if _, err := authService.BootstrapAdmin(ctx, ports.BootstrapAdminInput{Username: "admin", Password: "fixture-pass-9x"}); err != nil {
		t.Fatal(err)
	}
	login, err := authService.LoginWithPassword(ctx, "admin", "fixture-pass-9x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.EnsureScanSettings(ctx, ports.ScanSettings{Enabled: true, ScheduleEnabled: true, Timeout: time.Minute, Interval: 24 * time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 2}); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	checked, dbDate := now.Add(-5*time.Hour), now.Add(-72*time.Hour)
	runs := []ports.ScanRun{
		{ID: "run-ok", Repository: "team/api", RequestedRef: "1.0", Digest: "sha256:" + strings.Repeat("a1", 32), Status: ports.ScanRunStatusCompleted, Trigger: "push",
			CreatedAt: now.Add(-6 * time.Hour), UpdatedAt: now.Add(-6 * time.Hour), StartedAt: &checked, FinishedAt: &checked, Critical: 1, DBUpdatedAt: &dbDate, DBVersion: 2, LastCheckedAt: &checked, TrivyVersion: "0.57.1"},
		{ID: "run-stale", Repository: "team/api", RequestedRef: "0.9", Digest: "sha256:" + strings.Repeat("b2", 32), Status: ports.ScanRunStatusCompleted, Trigger: "scheduled",
			CreatedAt: now.Add(-30 * time.Hour), UpdatedAt: now.Add(-30 * time.Hour), StartedAt: &checked, FinishedAt: &checked, High: 2, Stale: true, StaleReason: "trivy timed out", TrivyVersion: "0.57.1"},
	}
	for _, run := range runs {
		if err := meta.UpsertScanRunDetail(ctx, "tenant-a", ports.ScanRunDetail{Run: run}); err != nil {
			t.Fatal(err)
		}
	}
	started, finished := now.Add(-5*time.Hour-time.Minute), now.Add(-5*time.Hour)
	if err := meta.RecordScanSweep(ctx, "tenant-a", ports.ScanSweepSummary{StartedAt: &started, FinishedAt: &finished, Visited: 750, SkippedUnchanged: 742, Queued: 8, Errors: 1,
		LastDBRefreshAt: &finished, LastDBRefreshError: "", DBFingerprint: "2:x", DBChanged: true}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(router)
	defer server.Close()
	client, err := NewHTTPAdminClient(server.URL, &http.Client{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	session := AdminSession{Username: "admin", BearerToken: login.BearerToken, ExpiresAt: now.Add(time.Hour)}

	got, err := client.ListScanRuns(ctx, session, "team/api", 100)
	if err != nil || len(got) != 2 {
		t.Fatalf("ListScanRuns() = %d runs, err %v, want 2", len(got), err)
	}
	byID := map[string]ports.ScanRun{}
	for _, run := range got {
		byID[run.ID] = run
	}
	if r := byID["run-ok"]; r.LastCheckedAt == nil || !r.LastCheckedAt.Equal(checked) || r.DBUpdatedAt == nil || !r.DBUpdatedAt.Equal(dbDate) || r.DBVersion != 2 || r.Stale {
		t.Fatalf("run-ok = %#v, want last_checked_at, db_updated_at and db_version decoded", r)
	}
	if r := byID["run-stale"]; !r.Stale || r.StaleReason != "trivy timed out" {
		t.Fatalf("run-stale = %#v, want stale and its reason decoded", r)
	}

	status, err := client.GetFeatureStatus(ctx, session, trivyFeatureName)
	if err != nil || status.Sweep == nil {
		t.Fatalf("GetFeatureStatus() sweep = %#v, err %v, want the sweep object decoded", status.Sweep, err)
	}
	if s := status.Sweep; s.Visited != 750 || s.SkippedUnchanged != 742 || s.Queued != 8 || s.Errors != 1 || !s.DBChanged || s.LastDBRefreshAt == nil || s.FinishedAt == nil {
		t.Fatalf("sweep = %#v, want visited 750, unchanged 742, queued 8, errors 1, db_changed", s)
	}
	page, err := client.GetFeaturePage(ctx, session, trivyFeatureName)
	if err != nil {
		t.Fatal(err)
	}
	if sweep, ok := trivySweepFromPage(page); !ok || sweep.visited != "750" || sweep.unchanged != "742" || sweep.queued != "8" || sweep.errors != 1 || sweep.finished.IsZero() || !sweep.dbChanged {
		t.Fatalf("trivySweepFromPage() = %#v, %v, want the sweep section read", sweep, ok)
	}

	// Both screens render from the real payloads.
	m := newAdminReadyModelWithCatalog(t, []string{"team/api"}, client)
	m = resizeModel(t, m, 200, 40)
	m.adminSession, m.adminAuth = session, adminAuthStateAuthenticated
	updated, cmd := m.Update(navigateMsg{To: screenAdminScanRuns})
	m = scanRun(t, updated.(Model), cmd)
	out := scanText(m)
	for _, want := range []string{"stale", "3 d ago", "5 h ago", "completed"} {
		if !strings.Contains(out, want) {
			t.Errorf("Scan runs lacks %q:\n%s", want, out)
		}
	}
	updated, cmd = m.Update(navigateMsg{To: screenSecurityTrivy})
	m = securityRun(t, updated.(Model), cmd)
	out = securityRender(m)
	for _, want := range []string{"LAST SWEEP", "visited 750", "unchanged 742", "queued 8", "1 error", "DB refresh ok 5 h ago"} {
		if !strings.Contains(out, want) {
			t.Errorf("Image scanning lacks %q:\n%s", want, out)
		}
	}
}
