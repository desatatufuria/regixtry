package regixtry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"regixtry/internal/ports"
)

// sweepRunner is a scan runner for the scheduled-sweep tests: it exposes the
// DB metadata (ports.ScanDBMetadataReader) and the DB refresh capability
// (ports.ScanDBRefresher), counts scans and refreshes, tracks in-flight
// concurrency, and can block scans until released or cancelled.
type sweepRunner struct {
	mu          sync.Mutex
	scans       int
	refreshes   int
	refreshErr  error
	refreshHook func(r *sweepRunner)
	dbUpdated   time.Time
	inFlight    int
	maxInFlight int
	started     chan struct{}
	block       bool
	delay       time.Duration
	// gather makes each scan wait (bounded) until that many scans are in
	// flight, so a test can prove the pool really runs in parallel.
	gather int
}

func newSweepRunner() *sweepRunner {
	return &sweepRunner{dbUpdated: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
}

func (r *sweepRunner) Run(ctx context.Context, _ string, _ ports.ScanSettings) (ports.ScanResult, error) {
	r.mu.Lock()
	r.scans++
	r.inFlight++
	if r.inFlight > r.maxInFlight {
		r.maxInFlight = r.inFlight
	}
	updated, block, delay, started, gather := r.dbUpdated, r.block, r.delay, r.started, r.gather
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.inFlight--
		r.mu.Unlock()
	}()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if block {
		<-ctx.Done()
		return ports.ScanResult{}, ctx.Err()
	}
	if gather > 0 {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && ctx.Err() == nil {
			r.mu.Lock()
			reached := r.inFlight >= gather
			r.mu.Unlock()
			if reached {
				break
			}
			time.Sleep(time.Millisecond)
		}
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ports.ScanResult{}, ctx.Err()
		}
	}
	return ports.ScanResult{
		TrivyVersion: "0.57.1",
		DBUpdatedAt:  &updated,
		Critical:     1,
		DBFreshness:  ports.ScanRunDBFreshness{TrivyVersion: "0.57.1", DBVersion: 2, DBUpdatedAt: &updated},
	}, nil
}

func (r *sweepRunner) ReadDBMetadata(context.Context, ports.ScanSettings) (ports.ScanDBMetadata, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	updated := r.dbUpdated
	return ports.ScanDBMetadata{Version: 2, UpdatedAt: &updated}, nil
}

func (r *sweepRunner) RefreshDB(context.Context, ports.ScanSettings) error {
	r.mu.Lock()
	r.refreshes++
	hook, err := r.refreshHook, r.refreshErr
	r.mu.Unlock()
	if hook != nil {
		hook(r)
	}
	return err
}

func (r *sweepRunner) counts() (scans int, refreshes int, maxInFlight int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scans, r.refreshes, r.maxInFlight
}

func (r *sweepRunner) setDBUpdated(updated time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dbUpdated = updated
}

// publishTaggedManifests publishes one manifest per tag in a repository, each
// with a distinct digest (an annotation differs), sharing one uploaded layer.
func publishTaggedManifests(t *testing.T, service *Service, repository string, tags []string) {
	t.Helper()
	ctx := context.Background()
	upload, err := service.BeginUpload(ctx, repository)
	if err != nil {
		t.Fatalf("BeginUpload(%q) error = %v", repository, err)
	}
	if _, err := service.AppendUpload(ctx, repository, upload.ID, strings.NewReader("layer-one")); err != nil {
		t.Fatalf("AppendUpload(%q) error = %v", repository, err)
	}
	blob, err := service.CompleteUpload(ctx, repository, upload.ID, digestForTest([]byte("layer-one")), nil)
	if err != nil {
		t.Fatalf("CompleteUpload(%q) error = %v", repository, err)
	}
	for _, tag := range tags {
		payload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + blob.Digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + blob.Digest + `","size":9}],"annotations":{"tag":"` + tag + `"}}`)
		if _, err := service.PublishManifest(ctx, repository, tag, "application/vnd.oci.image.manifest.v1+json", payload); err != nil {
			t.Fatalf("PublishManifest(%q:%s) error = %v", repository, tag, err)
		}
	}
	service.WaitForBackgroundWork()
}

// newSweepService builds a service with `repos` repositories of three tags
// each, published BEFORE the scan settings exist so no push scan fires. The
// scheduled sweep is enabled with the given concurrency.
func newSweepService(t *testing.T, repos int, concurrency int) (*Service, *sweepRunner, func()) {
	t.Helper()
	service, cleanup := newTestService(t, allowAllAccessController{})
	for i := 0; i < repos; i++ {
		publishTaggedManifests(t, service, fmt.Sprintf("team/repo-%03d", i), []string{"latest", "v1", "v2"})
	}
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, ScheduleEnabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: concurrency}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
	runner := newSweepRunner()
	service.SetScanRunner(runner)
	return service, runner, cleanup
}

func sweepSummary(t *testing.T, service *Service) ports.ScanSweepSummary {
	t.Helper()
	state, err := service.metadata.GetScanSchedulerState(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("GetScanSchedulerState() error = %v", err)
	}
	return state.LastSweep
}

func activeRunCount(t *testing.T, service *Service, repos int) int {
	t.Helper()
	active := 0
	for i := 0; i < repos; i++ {
		runs, err := service.metadata.ListScanRuns(context.Background(), "tenant-a", fmt.Sprintf("team/repo-%03d", i), 100)
		if err != nil {
			t.Fatalf("ListScanRuns() error = %v", err)
		}
		for _, run := range runs {
			if run.Status == ports.ScanRunStatusQueued || run.Status == ports.ScanRunStatusRunning {
				active++
			}
		}
	}
	return active
}

func TestSweepCoversEveryTaggedDigestPastTheFirstCatalogPage(t *testing.T) {
	service, runner, cleanup := newSweepService(t, 250, 8)
	defer cleanup()

	if err := service.RunScheduledScans(context.Background()); err != nil {
		t.Fatalf("RunScheduledScans() error = %v", err)
	}
	scans, refreshes, _ := runner.counts()
	if scans != 750 {
		t.Fatalf("scans = %d, want 750 (250 repositories x 3 tagged digests, beyond the old first-100 page and latest-only reference)", scans)
	}
	if refreshes != 1 {
		t.Fatalf("db refreshes = %d, want exactly 1 per sweep", refreshes)
	}
	for _, repository := range []string{"team/repo-000", "team/repo-099", "team/repo-100", "team/repo-249"} {
		completed := 0
		for _, run := range scanRunsOf(t, service, repository) {
			if run.Status == ports.ScanRunStatusCompleted {
				completed++
			}
			if run.Trigger != ports.ScanTriggerScheduled {
				t.Fatalf("%s run trigger = %q, want scheduled", repository, run.Trigger)
			}
		}
		if completed != 3 {
			t.Fatalf("%s completed runs = %d, want one per tagged digest (3)", repository, completed)
		}
	}
	summary := sweepSummary(t, service)
	if summary.Visited != 750 || summary.Queued != 750 || summary.SkippedUnchanged != 0 || summary.FinishedAt == nil {
		t.Fatalf("summary = %#v, want visited=750 queued=750 skipped=0 and a finish time", summary)
	}
}

func TestSweepSkipsUnchangedDigestsWithoutNewRunsAndTouchesLastChecked(t *testing.T) {
	service, runner, cleanup := newSweepService(t, 20, 4)
	defer cleanup()
	if err := service.RunScheduledScans(context.Background()); err != nil {
		t.Fatalf("first sweep error = %v", err)
	}
	before := scanRunsOf(t, service, "team/repo-007")
	firstChecked := before[0].LastCheckedAt
	if firstChecked == nil {
		t.Fatal("completed run has no last_checked_at")
	}
	time.Sleep(5 * time.Millisecond)

	if err := service.RunScheduledScans(context.Background()); err != nil {
		t.Fatalf("second sweep error = %v", err)
	}
	scans, refreshes, _ := runner.counts()
	if scans != 60 {
		t.Fatalf("scans = %d, want 60 (second sweep must queue nothing for unchanged digests)", scans)
	}
	if refreshes != 2 {
		t.Fatalf("db refreshes = %d, want one per sweep (2)", refreshes)
	}
	after := scanRunsOf(t, service, "team/repo-007")
	if len(after) != len(before) {
		t.Fatalf("runs for repo-007 = %d, want %d (no new run for unchanged digests)", len(after), len(before))
	}
	for _, run := range after {
		if run.LastCheckedAt == nil || !run.LastCheckedAt.After(*firstChecked) {
			t.Fatalf("run %s last_checked_at = %v, want advanced past %v", run.ID, run.LastCheckedAt, firstChecked)
		}
	}
	summary := sweepSummary(t, service)
	if summary.Visited != 60 || summary.SkippedUnchanged != 60 || summary.Queued != 0 || summary.DBChanged {
		t.Fatalf("summary = %#v, want visited=60 skipped=60 queued=0 db_changed=false", summary)
	}
}

func TestSweepRescansEveryDigestWhenTheDBChanged(t *testing.T) {
	service, runner, cleanup := newSweepService(t, 10, 4)
	defer cleanup()
	if err := service.RunScheduledScans(context.Background()); err != nil {
		t.Fatalf("first sweep error = %v", err)
	}
	runner.setDBUpdated(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC))
	if err := service.RunScheduledScans(context.Background()); err != nil {
		t.Fatalf("second sweep error = %v", err)
	}
	scans, _, _ := runner.counts()
	if scans != 60 {
		t.Fatalf("scans = %d, want 60 (30 initial + 30 after the DB metadata changed)", scans)
	}
	summary := sweepSummary(t, service)
	if summary.Queued != 30 || summary.SkippedUnchanged != 0 || !summary.DBChanged {
		t.Fatalf("summary = %#v, want queued=30 skipped=0 db_changed=true", summary)
	}
	// Latest only: still exactly one completed result per digest.
	completed := 0
	for _, run := range scanRunsOf(t, service, "team/repo-003") {
		if run.Status == ports.ScanRunStatusCompleted {
			completed++
		}
	}
	if completed != 3 {
		t.Fatalf("completed runs for repo-003 = %d, want 3 (replaced, not accumulated)", completed)
	}
}

func TestSweepScansADigestPublishedAfterTheLastSweep(t *testing.T) {
	service, runner, cleanup := newSweepService(t, 3, 2)
	defer cleanup()
	if err := service.RunScheduledScans(context.Background()); err != nil {
		t.Fatalf("first sweep error = %v", err)
	}
	// Publish while scanning is disabled so the push path does not scan it:
	// only the sweep can find the new digest.
	current, err := service.GetScanSettings(context.Background())
	if err != nil {
		t.Fatalf("GetScanSettings() error = %v", err)
	}
	disabled := current
	disabled.Enabled = false
	if _, err := service.UpdateScanSettings(context.Background(), disabled); err != nil {
		t.Fatalf("UpdateScanSettings(disable) error = %v", err)
	}
	publishTaggedManifests(t, service, "team/repo-001", []string{"v3"})
	if _, err := service.UpdateScanSettings(context.Background(), current); err != nil {
		t.Fatalf("UpdateScanSettings(enable) error = %v", err)
	}

	if err := service.RunScheduledScans(context.Background()); err != nil {
		t.Fatalf("second sweep error = %v", err)
	}
	scans, _, _ := runner.counts()
	if scans != 10 {
		t.Fatalf("scans = %d, want 10 (9 initial + the one new digest)", scans)
	}
	if summary := sweepSummary(t, service); summary.Queued != 1 || summary.SkippedUnchanged != 9 || summary.Visited != 10 {
		t.Fatalf("summary = %#v, want visited=10 skipped=9 queued=1", summary)
	}
}

func TestSweepSkipsRepositoriesWhoseOverrideDisablesTrivy(t *testing.T) {
	service, runner, cleanup := newSweepService(t, 3, 2)
	defer cleanup()
	seedTrivyOverride(t, service, "team/repo-001", ports.TrivyOverride{Enabled: false})

	if err := service.RunScheduledScans(context.Background()); err != nil {
		t.Fatalf("RunScheduledScans() error = %v", err)
	}
	scans, _, _ := runner.counts()
	if scans != 6 {
		t.Fatalf("scans = %d, want 6 (the disabled repository's 3 digests are skipped)", scans)
	}
	if runs := scanRunsOf(t, service, "team/repo-001"); len(runs) != 0 {
		t.Fatalf("disabled repository runs = %#v, want none", runs)
	}
}

func TestSweepUsesEffectiveOverrideSettingsInTheFingerprint(t *testing.T) {
	service, runner, cleanup := newSweepService(t, 2, 2)
	defer cleanup()
	if err := service.RunScheduledScans(context.Background()); err != nil {
		t.Fatalf("first sweep error = %v", err)
	}
	ignore := t.TempDir() + "/ignore.trivyignore"
	if err := os.WriteFile(ignore, []byte("CVE-1\n"), 0o600); err != nil {
		t.Fatalf("write ignore file: %v", err)
	}
	seedTrivyOverride(t, service, "team/repo-001", ports.TrivyOverride{Enabled: true, IgnoreFilePath: ignore})

	if err := service.RunScheduledScans(context.Background()); err != nil {
		t.Fatalf("second sweep error = %v", err)
	}
	scans, _, _ := runner.counts()
	if scans != 9 {
		t.Fatalf("scans = %d, want 9 (only the overridden repository's 3 digests rescan: its settings hash differs)", scans)
	}
}

func TestSweepContinuesWithTheExistingDBWhenTheRefreshFails(t *testing.T) {
	service, runner, cleanup := newSweepService(t, 4, 2)
	defer cleanup()
	runner.refreshErr = errors.New("dial tcp: network is unreachable")

	if err := service.RunScheduledScans(context.Background()); err != nil {
		t.Fatalf("RunScheduledScans() error = %v, want the sweep to continue past a failed refresh", err)
	}
	scans, refreshes, _ := runner.counts()
	if scans != 12 || refreshes != 1 {
		t.Fatalf("scans = %d refreshes = %d, want 12 scans and 1 refresh attempt", scans, refreshes)
	}
	summary := sweepSummary(t, service)
	if !strings.Contains(summary.LastDBRefreshError, "network is unreachable") || summary.LastDBRefreshAt == nil {
		t.Fatalf("summary = %#v, want the refresh failure and its time recorded", summary)
	}

	// A later successful refresh clears the recorded error.
	runner.mu.Lock()
	runner.refreshErr = nil
	runner.mu.Unlock()
	if err := service.RunScheduledScans(context.Background()); err != nil {
		t.Fatalf("second sweep error = %v", err)
	}
	if summary := sweepSummary(t, service); summary.LastDBRefreshError != "" {
		t.Fatalf("LastDBRefreshError = %q, want cleared after a successful refresh", summary.LastDBRefreshError)
	}
}

func TestSweepDoesNotDownloadWhenRefreshOnSweepIsOff(t *testing.T) {
	service, runner, cleanup := newSweepService(t, 2, 2)
	defer cleanup()
	service.SetScanDBRefreshOnSweep(false)

	if err := service.RunScheduledScans(context.Background()); err != nil {
		t.Fatalf("RunScheduledScans() error = %v", err)
	}
	scans, refreshes, _ := runner.counts()
	if refreshes != 0 || scans != 6 {
		t.Fatalf("refreshes = %d scans = %d, want no download and 6 scans against the cached DB", refreshes, scans)
	}
}

func TestSweepBoundsConcurrencyToMaxConcurrency(t *testing.T) {
	service, runner, cleanup := newSweepService(t, 10, 3)
	defer cleanup()
	runner.gather = 3
	runner.delay = 5 * time.Millisecond

	if err := service.RunScheduledScans(context.Background()); err != nil {
		t.Fatalf("RunScheduledScans() error = %v", err)
	}
	scans, _, maxInFlight := runner.counts()
	if scans != 30 {
		t.Fatalf("scans = %d, want 30", scans)
	}
	if maxInFlight != 3 {
		t.Fatalf("max scans in flight = %d, want exactly 3 (MaxConcurrency=3 bounds the sweep, which still runs in parallel)", maxInFlight)
	}
}

func TestSweepStopsPromptlyOnCancelAndLeavesNoRunStuck(t *testing.T) {
	service, runner, cleanup := newSweepService(t, 10, 2)
	defer cleanup()
	runner.block = true
	runner.started = make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.RunScheduledScans(ctx) }()
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("no scan started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RunScheduledScans() error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("sweep did not stop promptly after cancel")
	}
	if active := activeRunCount(t, service, 10); active != 0 {
		t.Fatalf("%d runs left queued or running after cancel, want none", active)
	}
	scans, _, _ := runner.counts()
	if scans > 4 {
		t.Fatalf("scans = %d, want the sweep to stop starting scans after cancel", scans)
	}
}
