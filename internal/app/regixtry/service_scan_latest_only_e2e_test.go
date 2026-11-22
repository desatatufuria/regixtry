package regixtry

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
)

// TestLatestOnlyScanningEndToEnd walks the whole latest-only feature through
// the real service and the real sqlite store with a fake scan runner:
// migration purge, unchanged sweep, changed-DB sweep, manual scan, push of an
// already scanned digest.
func TestLatestOnlyScanningEndToEnd(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "registry.db")
	blobs, err := fsblob.New(filepath.Join(root, "blobs"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}
	open := func() *Service {
		store, err := metadata.New(dbPath)
		if err != nil {
			t.Fatalf("metadata.New() error = %v", err)
		}
		return NewService(blobs, store, allowAllAccessController{}, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	}
	repos := []string{"team/alpha", "team/beta", "team/gamma"}

	// Publish with scanning not configured, so no push scan fires.
	service := open()
	for _, repository := range repos {
		publishTaggedManifests(t, service, repository, []string{"latest"})
	}
	digests := map[string]string{}
	for _, repository := range repos {
		for _, d := range taggedDigests(t, service, repository) {
			digests[repository] = d
		}
	}
	service.WaitForBackgroundWork()
	if err := service.metadata.(*metadata.Store).Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// (1) Old-schema database with 40 duplicate runs per digest.
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`DROP INDEX IF EXISTS idx_scan_runs_digest`,
		`ALTER TABLE scan_runs DROP COLUMN settings_hash`,
		`ALTER TABLE scan_runs DROP COLUMN db_version`,
		`ALTER TABLE scan_runs DROP COLUMN last_checked_at`,
		`ALTER TABLE scan_runs DROP COLUMN stale`,
		`ALTER TABLE scan_runs DROP COLUMN stale_reason`,
		`DELETE FROM schema_backfills WHERE name = 'scan_runs_latest_only_purge_v1'`,
	} {
		if _, err := raw.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	// alpha: newest completed run is critical (blocked); beta: newest
	// completed run is clean (allowed) while older ones were critical;
	// gamma: newest run FAILED, newest completed is critical (still blocked).
	newestCompletedCritical := map[string]int{"team/alpha": 1, "team/beta": 0, "team/gamma": 1}
	for _, repository := range repos {
		for i := 0; i < 40; i++ {
			status, critical := "completed", 1-newestCompletedCritical[repository]
			if i == 39 {
				critical = newestCompletedCritical[repository]
			}
			if repository == "team/gamma" && i == 39 {
				status, critical = "failed", 0
			}
			if repository == "team/gamma" && i == 38 {
				critical = 1
			}
			at := base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339Nano)
			if _, err := raw.Exec(`INSERT INTO scan_runs (id, tenant, repository, requested_ref, digest, status, trigger, created_at, updated_at, critical, high, medium, low, trivy_version, error) VALUES (?, 'tenant-a', ?, 'latest', ?, ?, 'manual', ?, ?, ?, 0, 0, 0, '0.57.1', '')`,
				fmt.Sprintf("old-%s-%02d", repository, i), repository, digests[repository], status, at, at, critical); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	service = open() // opening runs the purge
	defer func() {
		service.WaitForBackgroundWork()
		_ = service.metadata.(*metadata.Store).Close()
	}()
	if _, err := service.UpdateScanPolicySettings(ctx, ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical}); err != nil {
		t.Fatalf("UpdateScanPolicySettings() error = %v", err)
	}
	wantBlocked := map[string]bool{"team/alpha": true, "team/beta": false, "team/gamma": true}
	checkPolicy := func(when string) {
		t.Helper()
		for _, repository := range repos {
			err := service.enforceScanPolicy(ctx, repository, digests[repository])
			if blocked := domain.IsCode(err, domain.ErrorCodePolicyViolation); blocked != wantBlocked[repository] || (!blocked && err != nil) {
				t.Fatalf("%s: enforceScanPolicy(%s) = %v, blocked want %v", when, repository, err, wantBlocked[repository])
			}
		}
	}
	for _, repository := range repos {
		runs := scanRunsOf(t, service, repository)
		completed := 0
		for _, run := range runs {
			if run.Status == ports.ScanRunStatusCompleted {
				completed++
			}
		}
		if completed != 1 || len(runs) != 1 {
			t.Fatalf("%s after purge: %d runs (%d completed), want exactly one completed result (a newer failed run is dropped when a completed one exists)", repository, len(runs), completed)
		}
	}
	checkPolicy("after the purge")

	// Scanning goes live with a fake runner whose DB metadata we control.
	if _, err := service.EnsureScanSettings(ctx, ports.ScanSettings{Enabled: true, ScheduleEnabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 2}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
	runner := newSweepRunner()
	service.SetScanRunner(runner)

	// Baseline sweep: legacy rows carry no fingerprint, so each digest is
	// scanned once and becomes a fingerprinted latest result.
	if err := service.RunScheduledScans(ctx); err != nil {
		t.Fatalf("baseline sweep error = %v", err)
	}
	scans, _, _ := runner.counts()
	if scans != 3 {
		t.Fatalf("baseline scans = %d, want 3", scans)
	}
	for _, repository := range repos {
		wantBlocked[repository] = true // the fake scanner reports one critical finding
	}
	checkPolicy("after the baseline sweep")
	checked := func() map[string]time.Time {
		out := map[string]time.Time{}
		for _, repository := range repos {
			runs := scanRunsOf(t, service, repository)
			for _, run := range runs {
				if run.Status == ports.ScanRunStatusCompleted {
					if run.LastCheckedAt == nil {
						t.Fatalf("%s completed run has no last_checked_at", repository)
					}
					out[repository] = *run.LastCheckedAt
				}
			}
		}
		return out
	}
	before := checked()
	time.Sleep(5 * time.Millisecond)

	// (2) Sweep with unchanged DB metadata: nothing queued, last check advances.
	if err := service.RunScheduledScans(ctx); err != nil {
		t.Fatalf("unchanged sweep error = %v", err)
	}
	if scans, _, _ = runner.counts(); scans != 3 {
		t.Fatalf("scans after an unchanged sweep = %d, want still 3", scans)
	}
	after := checked()
	for _, repository := range repos {
		if !after[repository].After(before[repository]) {
			t.Fatalf("%s last_checked_at = %v, want advanced past %v", repository, after[repository], before[repository])
		}
	}
	if summary := sweepSummary(t, service); summary.Queued != 0 || summary.SkippedUnchanged != 3 {
		t.Fatalf("summary = %#v, want queued=0 skipped=3", summary)
	}

	// (3) New DB metadata: every digest is rescanned once, history stays one
	// row per digest, and the pull policy never opens while it happens.
	runner.setDBUpdated(time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC))
	stop := make(chan struct{})
	var reader sync.WaitGroup
	reader.Add(1)
	go func() {
		defer reader.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			for _, repository := range repos {
				if err := service.enforceScanPolicy(ctx, repository, digests[repository]); !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
					t.Errorf("enforceScanPolicy(%s) = %v during a rescan, want the digest blocked", repository, err)
					return
				}
			}
		}
	}()
	err = service.RunScheduledScans(ctx)
	close(stop)
	reader.Wait()
	if err != nil {
		t.Fatalf("changed-DB sweep error = %v", err)
	}
	if scans, _, _ = runner.counts(); scans != 6 {
		t.Fatalf("scans after the DB changed = %d, want 6 (each digest rescanned once)", scans)
	}
	for _, repository := range repos {
		runs := scanRunsOf(t, service, repository)
		completed := 0
		for _, run := range runs {
			if run.Status == ports.ScanRunStatusCompleted {
				completed++
			}
		}
		if completed != 1 {
			t.Fatalf("%s completed runs = %d, want one per digest after a rescan", repository, completed)
		}
	}

	// (4) A manual scan forces a rescan even though nothing changed.
	run, err := service.QueueManualScan(ctx, "team/alpha", "latest")
	if err != nil {
		t.Fatalf("QueueManualScan() error = %v", err)
	}
	waitUntil(t, "manual scan completed and replaced the result", func() bool {
		runs := scanRunsOf(t, service, "team/alpha")
		return len(runs) == 1 && runs[0].ID == run.ID && runs[0].Status == ports.ScanRunStatusCompleted
	})
	if scans, _, _ = runner.counts(); scans != 7 {
		t.Fatalf("scans after the manual scan = %d, want 7", scans)
	}

	// (5) Pushing an already scanned digest (same manifest, new tag) creates no run.
	payload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digestForTest([]byte("layer-one")) + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + digestForTest([]byte("layer-one")) + `","size":9}],"annotations":{"tag":"latest"}}`)
	if _, err := service.PublishManifest(ctx, "team/alpha", "again", "application/vnd.oci.image.manifest.v1+json", payload); err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}
	service.WaitForBackgroundWork()
	if scans, _, _ = runner.counts(); scans != 7 {
		t.Fatalf("scans after pushing an already scanned digest = %d, want still 7", scans)
	}
	if runs := scanRunsOf(t, service, "team/alpha"); len(runs) != 1 {
		t.Fatalf("runs after the push = %d, want 1 (no run for an unchanged digest)", len(runs))
	}
}

func taggedDigests(t *testing.T, service *Service, repository string) []string {
	t.Helper()
	ref, err := domain.ParseRepositoryRef(repository)
	if err != nil {
		t.Fatalf("ParseRepositoryRef(%q) error = %v", repository, err)
	}
	tagged, err := service.metadata.ListTaggedDigests(context.Background(), "tenant-a", ref, 100, "")
	if err != nil {
		t.Fatalf("ListTaggedDigests() error = %v", err)
	}
	var out []string
	for _, entry := range tagged {
		out = append(out, entry.Digest)
	}
	return out
}
