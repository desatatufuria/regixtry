package regixtry

import (
	"context"
	"testing"
	"time"

	"regixtry/internal/ports"
)

// This file covers the stale-scan-run-dedup bugfix: dedupAndQueueScanRun
// (Trivy leg) and executeSecretScanLeg (gitleaks leg) both treat ANY
// queued/running row for a digest as "already in flight" with no staleness
// awareness. If the process that would flip a run's status to a terminal
// state dies mid-scan (crash, restart, panic), the orphaned row blocks every
// future rescan of that digest forever. The fix compares the active row's
// age (now - StartedAt, or CreatedAt if StartedAt is nil) against that
// feature's own configured ScanSettings.Timeout: past timeout, the row is
// force-failed and a genuinely new run is queued instead of silently
// no-opping.

// TestQueueManualScanKeepsFreshActiveRunWithinTimeout proves the existing
// dedup behavior is preserved: an active run younger than its Timeout is
// left untouched and no new run is queued.
func TestQueueManualScanKeepsFreshActiveRunWithinTimeout(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")
	seedManagedRuntimeState(t, service, "0.57.1")
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}

	fixedNow := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }

	digest := seededManifestDigest(t, service, "library/alpine")
	startedAt := fixedNow.Add(-30 * time.Second) // younger than the 1-minute timeout
	active := ports.ScanRun{ID: "run-active", Repository: "library/alpine", RequestedRef: "latest", Digest: digest, Status: ports.ScanRunStatusRunning, Trigger: ports.ScanTriggerManual, StartedAt: &startedAt, CreatedAt: startedAt, UpdatedAt: startedAt}
	if err := service.metadata.UpsertScanRun(context.Background(), "tenant-a", active); err != nil {
		t.Fatalf("UpsertScanRun() error = %v", err)
	}

	run, err := service.QueueManualScan(context.Background(), "library/alpine", "latest")
	if err != nil {
		t.Fatalf("QueueManualScan() error = %v", err)
	}
	if run.ID != "run-active" {
		t.Fatalf("QueueManualScan() run.ID = %q, want the still-fresh active run %q (no requeue)", run.ID, "run-active")
	}

	stored, err := service.metadata.GetScanRun(context.Background(), "tenant-a", "run-active")
	if err != nil {
		t.Fatalf("GetScanRun() error = %v", err)
	}
	if stored.Status != ports.ScanRunStatusRunning {
		t.Fatalf("active run status = %q, want unchanged %q", stored.Status, ports.ScanRunStatusRunning)
	}

	runs, err := service.metadata.ListScanRuns(context.Background(), "tenant-a", "library/alpine", 10)
	if err != nil {
		t.Fatalf("ListScanRuns() error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("scan runs = %d, want exactly 1 (no new run queued while the active run is still fresh)", len(runs))
	}
}

// TestQueueManualScanReplacesStaleActiveRunPastTimeout proves the fix: an
// active run older than its Timeout is force-failed and a genuinely new run
// is queued for the same digest instead of the dedup guard silently
// no-opping forever.
func TestQueueManualScanReplacesStaleActiveRunPastTimeout(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")
	seedManagedRuntimeState(t, service, "0.57.1")
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}

	fixedNow := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }

	digest := seededManifestDigest(t, service, "library/alpine")
	startedAt := fixedNow.Add(-90 * time.Second) // older than the 1-minute timeout
	stale := ports.ScanRun{ID: "run-stale", Repository: "library/alpine", RequestedRef: "latest", Digest: digest, Status: ports.ScanRunStatusRunning, Trigger: ports.ScanTriggerManual, StartedAt: &startedAt, CreatedAt: startedAt, UpdatedAt: startedAt}
	if err := service.metadata.UpsertScanRun(context.Background(), "tenant-a", stale); err != nil {
		t.Fatalf("UpsertScanRun() error = %v", err)
	}

	run, err := service.QueueManualScan(context.Background(), "library/alpine", "latest")
	if err != nil {
		t.Fatalf("QueueManualScan() error = %v", err)
	}
	if run.ID == "run-stale" {
		t.Fatalf("QueueManualScan() reused the stale run id %q, want a genuinely new run", run.ID)
	}
	if run.Status != ports.ScanRunStatusQueued {
		t.Fatalf("new run status = %q, want %q", run.Status, ports.ScanRunStatusQueued)
	}

	orphaned, err := service.metadata.GetScanRun(context.Background(), "tenant-a", "run-stale")
	if err != nil {
		t.Fatalf("GetScanRun(stale) error = %v", err)
	}
	if orphaned.Status != ports.ScanRunStatusFailed {
		t.Fatalf("stale run status = %q, want %q", orphaned.Status, ports.ScanRunStatusFailed)
	}
	if orphaned.Error == "" {
		t.Fatalf("stale run Error is empty, want an orphaned-timeout explanation")
	}
	if orphaned.FinishedAt == nil || !orphaned.FinishedAt.Equal(fixedNow) {
		t.Fatalf("stale run FinishedAt = %v, want %v", orphaned.FinishedAt, fixedNow)
	}

	runs, err := service.metadata.ListScanRuns(context.Background(), "tenant-a", "library/alpine", 10)
	if err != nil {
		t.Fatalf("ListScanRuns() error = %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("scan runs = %d, want 2 (the failed orphan plus the new queued run)", len(runs))
	}
}

// TestQueueManualScanIgnoresCompletedRunRegardlessOfAge proves the
// staleness logic only ever applies to queued/running rows: a completed row
// (however old) is never touched, since GetActiveScanRunByDigest never
// returns it as "active" in the first place.
func TestQueueManualScanIgnoresCompletedRunRegardlessOfAge(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")
	seedManagedRuntimeState(t, service, "0.57.1")
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}

	fixedNow := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }

	digest := seededManifestDigest(t, service, "library/alpine")
	startedAt := fixedNow.Add(-24 * time.Hour) // very old, but terminal
	finishedAt := fixedNow.Add(-23 * time.Hour)
	completed := ports.ScanRun{ID: "run-completed", Repository: "library/alpine", RequestedRef: "latest", Digest: digest, Status: ports.ScanRunStatusCompleted, Trigger: ports.ScanTriggerManual, StartedAt: &startedAt, FinishedAt: &finishedAt, CreatedAt: startedAt, UpdatedAt: finishedAt}
	if err := service.metadata.UpsertScanRun(context.Background(), "tenant-a", completed); err != nil {
		t.Fatalf("UpsertScanRun() error = %v", err)
	}

	run, err := service.QueueManualScan(context.Background(), "library/alpine", "latest")
	if err != nil {
		t.Fatalf("QueueManualScan() error = %v", err)
	}
	if run.ID == "run-completed" {
		t.Fatalf("QueueManualScan() reused the completed run id, want a fresh queued run")
	}
	if run.Status != ports.ScanRunStatusQueued {
		t.Fatalf("new run status = %q, want %q", run.Status, ports.ScanRunStatusQueued)
	}

	unchanged, err := service.metadata.GetScanRun(context.Background(), "tenant-a", "run-completed")
	if err != nil {
		t.Fatalf("GetScanRun(completed) error = %v", err)
	}
	if unchanged.Status != ports.ScanRunStatusCompleted {
		t.Fatalf("completed run status = %q, want unchanged %q", unchanged.Status, ports.ScanRunStatusCompleted)
	}
}

// TestExecuteSecretScanLegKeepsFreshActiveRunWithinTimeout mirrors
// TestQueueManualScanKeepsFreshActiveRunWithinTimeout for the gitleaks leg.
func TestExecuteSecretScanLegKeepsFreshActiveRunWithinTimeout(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")
	seedManagedGitleaksSettings(t, service, true) // Timeout: time.Minute
	secretRunner := &capturingSecretScanRunner{}
	service.SetSecretScanRunner(secretRunner)

	fixedNow := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }

	digest := seededManifestDigest(t, service, "library/alpine")
	startedAt := fixedNow.Add(-30 * time.Second) // younger than the 1-minute timeout
	active := ports.SecretScanRun{ID: "secret-active", Repository: "library/alpine", Digest: digest, Status: ports.SecretScanRunStatusRunning, Trigger: ports.ScanTriggerManual, StartedAt: &startedAt, CreatedAt: startedAt, UpdatedAt: startedAt}
	if err := service.metadata.UpsertSecretScanRun(context.Background(), "tenant-a", active); err != nil {
		t.Fatalf("UpsertSecretScanRun() error = %v", err)
	}

	service.executeSecretScanLeg(context.Background(), "tenant-a", "library/alpine", digest, ports.ScanTriggerManual)

	secretRunner.mu.Lock()
	calls := len(secretRunner.targets)
	secretRunner.mu.Unlock()
	if calls != 0 {
		t.Fatalf("secret runner calls = %d, want 0: an active run within timeout must suppress requeue", calls)
	}

	stored, err := service.metadata.GetSecretScanRun(context.Background(), "tenant-a", "secret-active")
	if err != nil {
		t.Fatalf("GetSecretScanRun() error = %v", err)
	}
	if stored.Status != ports.SecretScanRunStatusRunning {
		t.Fatalf("active secret run status = %q, want unchanged %q", stored.Status, ports.SecretScanRunStatusRunning)
	}
}

// TestExecuteSecretScanLegReplacesStaleActiveRunPastTimeout mirrors
// TestQueueManualScanReplacesStaleActiveRunPastTimeout for the gitleaks leg
// — this is the exact bug confirmed live: a gitleaks-demo repo's secret-scan
// run stuck in "running" for days silently blocked every rescan.
func TestExecuteSecretScanLegReplacesStaleActiveRunPastTimeout(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")
	seedManagedGitleaksSettings(t, service, true) // Timeout: time.Minute
	secretRunner := &capturingSecretScanRunner{}
	service.SetSecretScanRunner(secretRunner)

	fixedNow := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }

	digest := seededManifestDigest(t, service, "library/alpine")
	startedAt := fixedNow.Add(-90 * time.Second) // older than the 1-minute timeout
	stale := ports.SecretScanRun{ID: "secret-stale", Repository: "library/alpine", Digest: digest, Status: ports.SecretScanRunStatusRunning, Trigger: ports.ScanTriggerManual, StartedAt: &startedAt, CreatedAt: startedAt, UpdatedAt: startedAt}
	if err := service.metadata.UpsertSecretScanRun(context.Background(), "tenant-a", stale); err != nil {
		t.Fatalf("UpsertSecretScanRun() error = %v", err)
	}

	service.executeSecretScanLeg(context.Background(), "tenant-a", "library/alpine", digest, ports.ScanTriggerManual)

	secretRunner.mu.Lock()
	calls := len(secretRunner.targets)
	secretRunner.mu.Unlock()
	if calls != 1 {
		t.Fatalf("secret runner calls = %d, want 1: a stale active run must not block a genuinely new scan", calls)
	}

	orphaned, err := service.metadata.GetSecretScanRun(context.Background(), "tenant-a", "secret-stale")
	if err != nil {
		t.Fatalf("GetSecretScanRun(stale) error = %v", err)
	}
	if orphaned.Status != ports.SecretScanRunStatusFailed {
		t.Fatalf("stale secret run status = %q, want %q", orphaned.Status, ports.SecretScanRunStatusFailed)
	}
	if orphaned.Error == "" {
		t.Fatalf("stale secret run Error is empty, want an orphaned-timeout explanation")
	}
	if orphaned.FinishedAt == nil || !orphaned.FinishedAt.Equal(fixedNow) {
		t.Fatalf("stale secret run FinishedAt = %v, want %v", orphaned.FinishedAt, fixedNow)
	}

	runs, err := service.metadata.ListSecretScanRuns(context.Background(), "tenant-a", "library/alpine", 10)
	if err != nil {
		t.Fatalf("ListSecretScanRuns() error = %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("secret scan runs = %d, want 2 (the failed orphan plus the new run)", len(runs))
	}
}

// TestExecuteSecretScanLegIgnoresCompletedRunRegardlessOfAge mirrors
// TestQueueManualScanIgnoresCompletedRunRegardlessOfAge for the gitleaks leg.
func TestExecuteSecretScanLegIgnoresCompletedRunRegardlessOfAge(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")
	seedManagedGitleaksSettings(t, service, true) // Timeout: time.Minute
	secretRunner := &capturingSecretScanRunner{}
	service.SetSecretScanRunner(secretRunner)

	fixedNow := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }

	digest := seededManifestDigest(t, service, "library/alpine")
	startedAt := fixedNow.Add(-24 * time.Hour) // very old, but terminal
	finishedAt := fixedNow.Add(-23 * time.Hour)
	completed := ports.SecretScanRun{ID: "secret-completed", Repository: "library/alpine", Digest: digest, Status: ports.SecretScanRunStatusCompleted, Trigger: ports.ScanTriggerManual, StartedAt: &startedAt, FinishedAt: &finishedAt, CreatedAt: startedAt, UpdatedAt: finishedAt}
	if err := service.metadata.UpsertSecretScanRun(context.Background(), "tenant-a", completed); err != nil {
		t.Fatalf("UpsertSecretScanRun() error = %v", err)
	}

	service.executeSecretScanLeg(context.Background(), "tenant-a", "library/alpine", digest, ports.ScanTriggerManual)

	secretRunner.mu.Lock()
	calls := len(secretRunner.targets)
	secretRunner.mu.Unlock()
	if calls != 1 {
		t.Fatalf("secret runner calls = %d, want 1: a completed row must never suppress a fresh scan", calls)
	}

	unchanged, err := service.metadata.GetSecretScanRun(context.Background(), "tenant-a", "secret-completed")
	if err != nil {
		t.Fatalf("GetSecretScanRun(completed) error = %v", err)
	}
	if unchanged.Status != ports.SecretScanRunStatusCompleted {
		t.Fatalf("completed secret run status = %q, want unchanged %q", unchanged.Status, ports.SecretScanRunStatusCompleted)
	}
}
