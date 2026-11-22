package regixtry

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"regixtry/internal/infra/scanning/trivy"
	"regixtry/internal/ports"
)

// realShapedTrivy builds a real trivy.Runner over a fake exec that returns
// Trivy-shaped JSON WITHOUT any vulnerability DB fields (neither in the
// version response nor in the image report): the DB identity only exists in
// <cache>/db/metadata.json, like a real image scan.
type realShapedTrivy struct {
	mu         sync.Mutex
	imageCalls int
	onImage    func()
}

func (r *realShapedTrivy) exec(_ context.Context, _ string, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "version" {
		return []byte(`{"Version":"0.57.1"}`), nil
	}
	r.mu.Lock()
	r.imageCalls++
	hook := r.onImage
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	return []byte(`{"SchemaVersion":2,"Results":[{"Target":"alpine","Class":"os-pkgs","Type":"alpine","Vulnerabilities":[{"VulnerabilityID":"CVE-1","PkgName":"openssl","InstalledVersion":"1","FixedVersion":"2","Severity":"CRITICAL"}]}]}`), nil
}

func (r *realShapedTrivy) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.imageCalls
}

func newRealShapedService(t *testing.T) (*Service, *realShapedTrivy, string, func()) {
	t.Helper()
	service, cleanup := newTestService(t, allowAllAccessController{})
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 2}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	cacheDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cacheDir, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "db", "metadata.json"), []byte(`{"Version":2,"NextUpdate":"`+time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339)+`","UpdatedAt":"2026-11-01T00:00:00Z","DownloadedAt":"2026-11-01T06:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.metadata.UpsertFeatureRuntimeState(context.Background(), "tenant-a", "trivy", ports.FeatureRuntimeState{
		Status: ports.FeatureRuntimeStatusReady, ActiveVersion: "0.57.1",
		ActiveBinaryPath: "/var/lib/regixtry/features/trivy/bin/active/trivy", CacheDir: cacheDir, UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertFeatureRuntimeState() error = %v", err)
	}
	fake := &realShapedTrivy{}
	service.SetScanRunner(trivy.New(trivy.RunnerConfig{Exec: fake.exec}))
	return service, fake, cacheDir, cleanup
}

func waitForSettledCompleted(t *testing.T, service *Service, repository string, wantCalls int, fake *realShapedTrivy) ports.ScanRun {
	t.Helper()
	var latest ports.ScanRun
	waitUntil(t, "completed run", func() bool {
		if fake.calls() < wantCalls {
			return false
		}
		runs := scanRunsOf(t, service, repository)
		for _, run := range runs {
			if run.Status == ports.ScanRunStatusQueued || run.Status == ports.ScanRunStatusRunning {
				return false
			}
		}
		for _, run := range runs {
			if run.Status == ports.ScanRunStatusCompleted {
				latest = run
				return true
			}
		}
		return false
	})
	return latest
}

func TestRealShapedTrivyReportWithoutDBFieldsStillSkipsUnchangedRescan(t *testing.T) {
	service, fake, _, cleanup := newRealShapedService(t)
	defer cleanup()

	publishRepositoryTag(t, service, context.Background(), "library/alpine", "v1", "layer-real")
	first := waitForSettledCompleted(t, service, "library/alpine", 1, fake)
	if first.DBUpdatedAt == nil || first.DBVersion != 2 || first.DBUpdatedAt.Format(time.RFC3339) != "2026-11-01T00:00:00Z" {
		t.Fatalf("first run = %#v, want db_version and db_updated_at taken from the cache metadata", first)
	}
	detail, err := service.metadata.GetScanRunDetail(context.Background(), "tenant-a", first.ID)
	if err != nil || detail.DBFreshness.DBVersion != 2 || detail.DBFreshness.DBUpdatedAt == nil {
		t.Fatalf("detail freshness = %#v err = %v, want the cache metadata persisted too", detail.DBFreshness, err)
	}

	time.Sleep(20 * time.Millisecond)
	publishRepositoryTag(t, service, context.Background(), "library/alpine", "v1-again", "layer-real")
	runs := scanRunsOf(t, service, "library/alpine")
	if len(runs) != 1 || runs[0].ID != first.ID || fake.calls() != 1 {
		t.Fatalf("runs = %#v calls = %d, want the second push skipped (no new run)", runs, fake.calls())
	}
	if runs[0].LastCheckedAt == nil || !runs[0].LastCheckedAt.After(*first.LastCheckedAt) {
		t.Fatalf("last_checked_at = %v, want it advanced past %v", runs[0].LastCheckedAt, first.LastCheckedAt)
	}
}

func TestSettingsHashStoredIsThePreScanOneWhenIgnoreFileChangesMidScan(t *testing.T) {
	service, fake, _, cleanup := newRealShapedService(t)
	defer cleanup()
	ignore := filepath.Join(t.TempDir(), "ignore")
	if err := os.WriteFile(ignore, []byte("CVE-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	seedTrivyOverride(t, service, "library/alpine", ports.TrivyOverride{Enabled: true, IgnoreFilePath: ignore})
	preHash, ok := scanSettingsHash(ports.ScanSettings{IgnoreFilePath: ignore})
	if !ok {
		t.Fatal("pre-scan hash unavailable")
	}
	var once sync.Once
	fake.mu.Lock()
	fake.onImage = func() {
		once.Do(func() {
			if err := os.WriteFile(ignore, []byte("CVE-1\nCVE-2\n"), 0o600); err != nil {
				t.Errorf("rewrite ignore file: %v", err)
			}
		})
	}
	fake.mu.Unlock()

	publishRepositoryTag(t, service, context.Background(), "library/alpine", "v1", "layer-mid")
	first := waitForSettledCompleted(t, service, "library/alpine", 1, fake)
	if first.SettingsHash != preHash {
		t.Fatalf("stored settings hash = %q, want the pre-scan hash %q", first.SettingsHash, preHash)
	}

	publishRepositoryTag(t, service, context.Background(), "library/alpine", "v1-again", "layer-mid")
	waitForSettledCompleted(t, service, "library/alpine", 2, fake)
	if fake.calls() != 2 {
		t.Fatalf("scans = %d, want the next push to rescan because the ignore file changed", fake.calls())
	}
}
