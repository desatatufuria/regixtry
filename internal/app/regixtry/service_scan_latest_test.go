package regixtry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// fingerprintRunner is a scan runner that also exposes the vulnerability DB
// metadata (ports.ScanDBMetadataReader), so tests control the fingerprint.
type fingerprintRunner struct {
	mu         sync.Mutex
	calls      int
	dbUpdated  time.Time
	dbVersion  int
	version    string
	metaErr    error
	nextUpdate *time.Time
	scanErr    error
	release    chan struct{}
}

func newFingerprintRunner() *fingerprintRunner {
	return &fingerprintRunner{dbUpdated: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), dbVersion: 2, version: "0.57.1"}
}

func (f *fingerprintRunner) Run(ctx context.Context, _ string, _ ports.ScanSettings) (ports.ScanResult, error) {
	f.mu.Lock()
	f.calls++
	updated, dbVersion, version, scanErr, release := f.dbUpdated, f.dbVersion, f.version, f.scanErr, f.release
	f.mu.Unlock()
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return ports.ScanResult{}, ctx.Err()
		}
	}
	if scanErr != nil {
		return ports.ScanResult{}, scanErr
	}
	return ports.ScanResult{
		TrivyVersion: version,
		DBUpdatedAt:  &updated,
		Critical:     1,
		Findings:     []ports.ScanRunFinding{{Severity: "CRITICAL", VulnerabilityID: "CVE-1", PackageName: "openssl"}},
		DBFreshness:  ports.ScanRunDBFreshness{TrivyVersion: version, DBVersion: dbVersion, DBUpdatedAt: &updated},
	}, nil
}

func (f *fingerprintRunner) ReadDBMetadata(context.Context, ports.ScanSettings) (ports.ScanDBMetadata, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.metaErr != nil {
		return ports.ScanDBMetadata{}, f.metaErr
	}
	updated := f.dbUpdated
	return ports.ScanDBMetadata{Version: f.dbVersion, UpdatedAt: &updated, NextUpdate: f.nextUpdate}, nil
}

func (f *fingerprintRunner) set(mutate func(*fingerprintRunner)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	mutate(f)
}

func (f *fingerprintRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newLatestOnlyService(t *testing.T) (*Service, *fingerprintRunner, func()) {
	t.Helper()
	service, cleanup := newTestService(t, allowAllAccessController{})
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 2}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
	runner := newFingerprintRunner()
	service.SetScanRunner(runner)
	return service, runner, cleanup
}

func scanRunsOf(t *testing.T, service *Service, repository string) []ports.ScanRun {
	t.Helper()
	runs, err := service.metadata.ListScanRuns(context.Background(), "tenant-a", repository, 100)
	if err != nil {
		t.Fatalf("ListScanRuns() error = %v", err)
	}
	return runs
}

func waitForCompletedRuns(t *testing.T, service *Service, repository string, wantCalls int, runner *fingerprintRunner) ports.ScanRun {
	t.Helper()
	var latest ports.ScanRun
	waitUntil(t, "a completed scan run", func() bool {
		if runner.callCount() < wantCalls {
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

func publishAndWait(t *testing.T, service *Service, repository string, tag string, layer string) {
	t.Helper()
	publishRepositoryTag(t, service, context.Background(), repository, tag, layer)
}

func TestNeedsScanDecisionTable(t *testing.T) {
	t.Parallel()
	db := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	current := scanFingerprint{TrivyVersion: "0.57.1", DBVersion: 2, DBUpdatedAt: &db, SettingsHash: "h"}
	matching := ports.ScanRun{Status: ports.ScanRunStatusCompleted, TrivyVersion: "0.57.1", DBVersion: 2, DBUpdatedAt: &db, SettingsHash: "h"}
	stale := matching
	stale.Stale = true
	oldRow := matching
	oldRow.SettingsHash = ""
	newerDB := db.Add(time.Hour)
	unknownDB := current
	unknownDB.Unknown, unknownDB.DBUpdatedAt = true, nil

	cases := []struct {
		name   string
		latest *ports.ScanRun
		cur    scanFingerprint
		force  bool
		want   bool
	}{
		{"no completed result scans", nil, current, false, true},
		{"unchanged fingerprint skips", &matching, current, false, false},
		{"force scans even when unchanged", &matching, current, true, true},
		{"stale result rescans", &stale, current, false, true},
		{"old row with empty hash rescans once", &oldRow, current, false, true},
		{"unknown DB metadata scans", &matching, unknownDB, false, true},
		{"db updated_at change scans", &matching, scanFingerprint{TrivyVersion: "0.57.1", DBVersion: 2, DBUpdatedAt: &newerDB, SettingsHash: "h"}, false, true},
		{"db version change scans", &matching, scanFingerprint{TrivyVersion: "0.57.1", DBVersion: 3, DBUpdatedAt: &db, SettingsHash: "h"}, false, true},
		{"trivy version change scans", &matching, scanFingerprint{TrivyVersion: "0.58.0", DBVersion: 2, DBUpdatedAt: &db, SettingsHash: "h"}, false, true},
		{"settings hash change scans", &matching, scanFingerprint{TrivyVersion: "0.57.1", DBVersion: 2, DBUpdatedAt: &db, SettingsHash: "other"}, false, true},
	}
	for _, tc := range cases {
		got, reason := needsScan(tc.latest, tc.cur, tc.force)
		if got != tc.want || reason == "" {
			t.Errorf("%s: needsScan = (%v, %q), want %v with a reason", tc.name, got, reason, tc.want)
		}
	}
}

func TestScanSettingsHashCoversIgnoreFileAndPolicyPathAndContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name string, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		return path
	}
	ignoreA := write("a.ignore", "CVE-1\n")
	ignoreB := write("b.ignore", "CVE-1\n")
	policy := write("policy.rego", "package trivy\n")

	base, ok := scanSettingsHash(ports.ScanSettings{IgnoreFilePath: ignoreA, IgnorePolicyPath: policy})
	if !ok {
		t.Fatal("scanSettingsHash() not ok for readable files")
	}
	same, _ := scanSettingsHash(ports.ScanSettings{IgnoreFilePath: ignoreA, IgnorePolicyPath: policy})
	if same != base {
		t.Fatal("hash must be deterministic")
	}
	if none, _ := scanSettingsHash(ports.ScanSettings{}); none == base {
		t.Error("configured ignore inputs must change the hash")
	}
	if other, _ := scanSettingsHash(ports.ScanSettings{IgnoreFilePath: ignoreB, IgnorePolicyPath: policy}); other == base {
		t.Error("ignore file PATH change (same content) must change the hash")
	}
	if err := os.WriteFile(ignoreA, []byte("CVE-1\nCVE-2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, _ := scanSettingsHash(ports.ScanSettings{IgnoreFilePath: ignoreA, IgnorePolicyPath: policy}); changed == base {
		t.Error("ignore file CONTENT change must change the hash")
	}
	if err := os.WriteFile(policy, []byte("package trivy\ndefault ignore = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	afterIgnore, _ := scanSettingsHash(ports.ScanSettings{IgnoreFilePath: ignoreA, IgnorePolicyPath: policy})
	if err := os.WriteFile(policy, []byte("package trivy\ndefault ignore = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if again, _ := scanSettingsHash(ports.ScanSettings{IgnoreFilePath: ignoreA, IgnorePolicyPath: policy}); again == afterIgnore {
		t.Error("ignore policy CONTENT change must change the hash")
	}
	if _, ok := scanSettingsHash(ports.ScanSettings{IgnoreFilePath: filepath.Join(dir, "missing")}); ok {
		t.Error("an unreadable configured file must make the hash unknown")
	}
}

func TestPushOfScannedDigestWithSameFingerprintCreatesNoNewRunAndTouchesLastChecked(t *testing.T) {
	service, runner, cleanup := newLatestOnlyService(t)
	defer cleanup()

	publishAndWait(t, service, "library/alpine", "v1", "layer-same")
	first := waitForCompletedRuns(t, service, "library/alpine", 1, runner)
	if first.SettingsHash == "" || first.DBVersion != 2 || first.DBUpdatedAt == nil || first.LastCheckedAt == nil {
		t.Fatalf("completed run = %#v, want the fingerprint inputs and last_checked_at recorded", first)
	}

	time.Sleep(20 * time.Millisecond)
	publishAndWait(t, service, "library/alpine", "v1-again", "layer-same") // same manifest, same digest
	runs := scanRunsOf(t, service, "library/alpine")
	if len(runs) != 1 || runs[0].ID != first.ID || runner.callCount() != 1 {
		t.Fatalf("runs = %#v calls = %d, want no new run and no scan for an unchanged fingerprint", runs, runner.callCount())
	}
	if runs[0].LastCheckedAt == nil || !runs[0].LastCheckedAt.After(*first.LastCheckedAt) {
		t.Fatalf("last_checked_at = %v, want it advanced past %v", runs[0].LastCheckedAt, first.LastCheckedAt)
	}
}

func TestPushOfSameDigestToAnotherRepositoryStillScansThatRepository(t *testing.T) {
	service, runner, cleanup := newLatestOnlyService(t)
	defer cleanup()

	publishAndWait(t, service, "library/alpine", "v1", "layer-shared")
	waitForCompletedRuns(t, service, "library/alpine", 1, runner)
	publishAndWait(t, service, "library/busybox", "v1", "layer-shared")
	waitForCompletedRuns(t, service, "library/busybox", 2, runner)
	if runner.callCount() != 2 {
		t.Fatalf("scans = %d, want one per repository (the key includes the repository)", runner.callCount())
	}
}

func TestChangedFingerprintComponentsTriggerRescanAndKeepASingleRun(t *testing.T) {
	cases := []struct {
		name   string
		change func(t *testing.T, service *Service, runner *fingerprintRunner)
	}{
		{"db updated_at", func(_ *testing.T, _ *Service, r *fingerprintRunner) {
			r.set(func(f *fingerprintRunner) { f.dbUpdated = f.dbUpdated.Add(24 * time.Hour) })
		}},
		{"db version", func(_ *testing.T, _ *Service, r *fingerprintRunner) {
			r.set(func(f *fingerprintRunner) { f.dbVersion = 3 })
		}},
		{"trivy version", func(t *testing.T, s *Service, r *fingerprintRunner) {
			seedManagedRuntimeState(t, s, "0.58.0")
			r.set(func(f *fingerprintRunner) { f.version = "0.58.0" })
		}},
		{"ignore file content", func(t *testing.T, s *Service, _ *fingerprintRunner) {
			path := filepath.Join(t.TempDir(), "ignore")
			if err := os.WriteFile(path, []byte("CVE-1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			seedTrivyOverride(t, s, "library/alpine", ports.TrivyOverride{Enabled: true, IgnoreFilePath: path})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, runner, cleanup := newLatestOnlyService(t)
			defer cleanup()
			publishAndWait(t, service, "library/alpine", "v1", "layer-change")
			first := waitForCompletedRuns(t, service, "library/alpine", 1, runner)

			tc.change(t, service, runner)
			publishAndWait(t, service, "library/alpine", "v1-again", "layer-change")
			second := waitForCompletedRuns(t, service, "library/alpine", 2, runner)
			if second.ID == first.ID {
				t.Fatalf("run id unchanged, want a new run for a changed fingerprint")
			}
			waitUntil(t, "latest-only replacement", func() bool { return len(scanRunsOf(t, service, "library/alpine")) == 1 })
		})
	}
}

func TestUnknownDBMetadataNeverSkipsAScan(t *testing.T) {
	service, runner, cleanup := newLatestOnlyService(t)
	defer cleanup()
	publishAndWait(t, service, "library/alpine", "v1", "layer-unknown")
	waitForCompletedRuns(t, service, "library/alpine", 1, runner)

	runner.set(func(f *fingerprintRunner) { f.metaErr = errors.New("no db yet") })
	publishAndWait(t, service, "library/alpine", "v1-again", "layer-unknown")
	waitForCompletedRuns(t, service, "library/alpine", 2, runner)
	if runner.callCount() != 2 {
		t.Fatalf("scans = %d, want a rescan when the DB metadata is unavailable", runner.callCount())
	}
}

func TestScheduledScanSkipsUnchangedDigestAndManualScanForces(t *testing.T) {
	service, runner, cleanup := newLatestOnlyService(t)
	defer cleanup()
	ctx := context.Background()
	publishAndWait(t, service, "library/alpine", "latest", "layer-sweep")
	first := waitForCompletedRuns(t, service, "library/alpine", 1, runner)
	settings, err := service.resolveManagedScanSettings(ctx)
	if err != nil {
		t.Fatalf("resolveManagedScanSettings() error = %v", err)
	}

	run, err := service.queueScheduledScan(ctx, "library/alpine", "latest", settings)
	if err != nil || run != (ports.ScanRun{}) || runner.callCount() != 1 {
		t.Fatalf("queueScheduledScan() = %#v, %v calls=%d, want a skip (no run) for an unchanged digest", run, err, runner.callCount())
	}

	manual, err := service.QueueManualScan(ctx, "library/alpine", "latest")
	if err != nil {
		t.Fatalf("QueueManualScan() error = %v", err)
	}
	second := waitForCompletedRuns(t, service, "library/alpine", 2, runner)
	if manual.ID == "" || second.ID != manual.ID || second.ID == first.ID {
		t.Fatalf("manual scan = %#v completed = %#v, want a forced scan that replaces the previous result", manual, second)
	}
	waitUntil(t, "single run after manual replacement", func() bool { return len(scanRunsOf(t, service, "library/alpine")) == 1 })
}

func TestFailedRescanKeepsPreviousResultAndPullPolicyStillBlocks(t *testing.T) {
	service, runner, cleanup := newLatestOnlyService(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := service.UpdateScanPolicySettings(ctx, ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical}); err != nil {
		t.Fatalf("UpdateScanPolicySettings() error = %v", err)
	}
	publishAndWait(t, service, "library/alpine", "latest", "layer-policy")
	first := waitForCompletedRuns(t, service, "library/alpine", 1, runner)
	digest := first.Digest
	blocked := func(when string) {
		t.Helper()
		if err := service.enforceScanPolicy(ctx, "library/alpine", digest); !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
			t.Fatalf("%s: enforceScanPolicy() = %v, want the known-critical digest blocked", when, err)
		}
	}
	blocked("before rescan")

	// While the rescan is in flight the queued/running row is the newest
	// run; the gate must still enforce the completed result.
	release := make(chan struct{})
	runner.set(func(f *fingerprintRunner) { f.release = release; f.scanErr = errors.New("scanner crashed") })
	if _, err := service.QueueManualScan(ctx, "library/alpine", "latest"); err != nil {
		t.Fatalf("QueueManualScan() error = %v", err)
	}
	waitUntil(t, "rescan in flight", func() bool { return runner.callCount() == 2 })
	blocked("while the rescan is in flight")

	close(release)
	waitUntil(t, "failed rescan folded into the previous result", func() bool {
		runs := scanRunsOf(t, service, "library/alpine")
		return len(runs) == 1 && runs[0].Stale
	})
	blocked("after the failed rescan")
	kept := scanRunsOf(t, service, "library/alpine")[0]
	if kept.ID != first.ID || kept.Status != ports.ScanRunStatusCompleted || kept.StaleReason == "" {
		t.Fatalf("kept = %#v, want the previous completed result marked stale with a reason", kept)
	}

	// A stale result is never skipped by push or the sweep: it is retried.
	runner.set(func(f *fingerprintRunner) { f.release = nil; f.scanErr = nil })
	publishAndWait(t, service, "library/alpine", "latest-again", "layer-policy")
	waitUntil(t, "stale result rescanned", func() bool {
		runs := scanRunsOf(t, service, "library/alpine")
		return len(runs) == 1 && !runs[0].Stale && runs[0].ID != first.ID && runs[0].Status == ports.ScanRunStatusCompleted
	})
}

func TestPullPolicyNeverSeesAMissingCompletedResultDuringReplacement(t *testing.T) {
	service, runner, cleanup := newLatestOnlyService(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := service.UpdateScanPolicySettings(ctx, ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical}); err != nil {
		t.Fatalf("UpdateScanPolicySettings() error = %v", err)
	}
	publishAndWait(t, service, "library/alpine", "latest", "layer-race")
	first := waitForCompletedRuns(t, service, "library/alpine", 1, runner)
	digest := first.Digest

	stop := make(chan struct{})
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			run := first
			run.ID = fmt.Sprintf("replacement-%d", i)
			run.CreatedAt = first.CreatedAt.Add(time.Duration(i+1) * time.Second)
			run.UpdatedAt = run.CreatedAt
			if err := service.metadata.UpsertScanRunDetail(ctx, "tenant-a", ports.ScanRunDetail{Run: run, Findings: []ports.ScanRunFinding{{Severity: "CRITICAL", VulnerabilityID: "CVE-R"}}}); err != nil {
				t.Errorf("replacement error = %v", err)
				return
			}
		}
	}()
	for i := 0; i < 1000; i++ {
		if _, err := service.metadata.GetLatestCompletedScanRunByDigest(ctx, "tenant-a", "library/alpine", digest); err != nil {
			t.Fatalf("iteration %d: no completed run during replacement: %v", i, err)
		}
		if err := service.enforceScanPolicy(ctx, "library/alpine", digest); !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
			t.Fatalf("iteration %d: enforceScanPolicy() = %v, want the digest blocked (fail-open window)", i, err)
		}
	}
	close(stop)
	writer.Wait()
}

// Nothing refreshes the cached DB metadata when no scan or sweep runs, so a
// push long after the DB's NextUpdate must not trust it: the digest is
// rescanned (the scan refreshes the DB); before NextUpdate it is skipped.
func TestPushAfterTheDBNextUpdateElapsedRescansInsteadOfTrustingCachedMetadata(t *testing.T) {
	service, runner, cleanup := newLatestOnlyService(t)
	defer cleanup()
	start := time.Now().UTC()
	next := start.Add(24 * time.Hour)
	runner.set(func(f *fingerprintRunner) { f.nextUpdate = &next })
	clock := start
	service.now = func() time.Time { return clock }

	publishAndWait(t, service, "library/alpine", "v1", "layer-stale")
	waitForCompletedRuns(t, service, "library/alpine", 1, runner)

	clock = start.Add(12 * time.Hour) // before NextUpdate
	publishAndWait(t, service, "library/alpine", "v1-again", "layer-stale")
	if got := runner.callCount(); got != 1 {
		t.Fatalf("scans before NextUpdate = %d, want 1 (skipped)", got)
	}

	clock = start.Add(60 * 24 * time.Hour) // far past NextUpdate
	publishAndWait(t, service, "library/alpine", "v1-third", "layer-stale")
	waitUntil(t, "rescan after NextUpdate elapsed", func() bool { return runner.callCount() == 2 })
}
