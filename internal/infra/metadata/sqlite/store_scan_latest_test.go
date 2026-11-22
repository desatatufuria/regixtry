package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

const latestOnlyTenant = "tenant-a"

func latestOnlyRun(id string, repository string, digest string, status string, createdAt time.Time) ports.ScanRun {
	return ports.ScanRun{ID: id, Repository: repository, RequestedRef: "latest", Digest: digest, Status: status, Trigger: ports.ScanTriggerManual, CreatedAt: createdAt, UpdatedAt: createdAt}
}

func completedDetail(id string, repository string, digest string, createdAt time.Time, vulnerabilityID string) ports.ScanRunDetail {
	run := latestOnlyRun(id, repository, digest, ports.ScanRunStatusCompleted, createdAt)
	run.Critical = 1
	run.SettingsHash = "hash-" + id
	run.DBVersion = 2
	return ports.ScanRunDetail{
		Run:         run,
		Findings:    []ports.ScanRunFinding{{Severity: "CRITICAL", VulnerabilityID: vulnerabilityID, PackageName: "openssl"}},
		DBFreshness: ports.ScanRunDBFreshness{DBVersion: 2, TrivyVersion: "0.57.1"},
	}
}

func countRunsForDigest(t *testing.T, store *Store, repository string, digest string) int {
	t.Helper()
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM scan_runs WHERE tenant = ? AND repository = ? AND digest = ?`, latestOnlyTenant, repository, digest).Scan(&n); err != nil {
		t.Fatalf("count scan_runs error = %v", err)
	}
	return n
}

func TestStoreFailedRescanKeepsCompletedResultAndMarksItStale(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	digest := "sha256:keep"

	if err := store.UpsertScanRunDetail(ctx, latestOnlyTenant, completedDetail("old", "library/alpine", digest, base, "CVE-1")); err != nil {
		t.Fatalf("UpsertScanRunDetail() error = %v", err)
	}
	rescan := latestOnlyRun("rescan", "library/alpine", digest, ports.ScanRunStatusQueued, base.Add(time.Minute))
	if err := store.UpsertScanRun(ctx, latestOnlyTenant, rescan); err != nil {
		t.Fatalf("queue rescan error = %v", err)
	}
	rescan.Status = ports.ScanRunStatusFailed
	rescan.Error = "timed out"
	if err := store.UpsertScanRun(ctx, latestOnlyTenant, rescan); err != nil {
		t.Fatalf("fail rescan error = %v", err)
	}

	got, err := store.GetLatestCompletedScanRunByDigest(ctx, latestOnlyTenant, "library/alpine", digest)
	if err != nil {
		t.Fatalf("GetLatestCompletedScanRunByDigest() error = %v", err)
	}
	if got.ID != "old" || got.Critical != 1 || !got.Stale || got.StaleReason != "timed out" {
		t.Fatalf("completed = %#v, want the previous result kept and marked stale with the failure reason", got)
	}
	if n := countRunsForDigest(t, store, "library/alpine", digest); n != 1 {
		t.Fatalf("runs for digest = %d, want 1 (failed row dropped while a completed result exists)", n)
	}
	findings, err := store.GetScanRunDetail(ctx, latestOnlyTenant, "old")
	if err != nil || len(findings.Findings) != 1 {
		t.Fatalf("detail = %#v err = %v, want the old findings intact", findings, err)
	}
}

func TestStoreFailedRunIsKeptWhenNoCompletedResultExists(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()
	failed := latestOnlyRun("failed", "library/alpine", "sha256:none", ports.ScanRunStatusFailed, time.Now().UTC())
	failed.Error = "boom"
	if err := store.UpsertScanRun(ctx, latestOnlyTenant, failed); err != nil {
		t.Fatalf("UpsertScanRun() error = %v", err)
	}
	got, err := store.GetLatestScanRunByDigest(ctx, latestOnlyTenant, "library/alpine", "sha256:none")
	if err != nil || got.ID != "failed" || got.Stale {
		t.Fatalf("latest = %#v err = %v, want the failed row kept (nothing to protect)", got, err)
	}
	if _, err := store.GetLatestCompletedScanRunByDigest(ctx, latestOnlyTenant, "library/alpine", "sha256:none"); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetLatestCompletedScanRunByDigest() error = %v, want typed NotFound", err)
	}
}

func TestStoreCompletedRunReplacesOlderRunsOfTheSameDigestOnly(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	digest := "sha256:replace"

	// Legacy history: duplicates written before latest-only existed.
	for i, run := range []ports.ScanRun{
		latestOnlyRun("dup-completed-1", "library/alpine", digest, ports.ScanRunStatusCompleted, base),
		latestOnlyRun("dup-completed-2", "library/alpine", digest, ports.ScanRunStatusCompleted, base.Add(time.Minute)),
		latestOnlyRun("dup-failed", "library/alpine", digest, ports.ScanRunStatusFailed, base.Add(2*time.Minute)),
		latestOnlyRun("other-repo", "library/busybox", digest, ports.ScanRunStatusCompleted, base),
		latestOnlyRun("other-digest", "library/alpine", "sha256:other", ports.ScanRunStatusCompleted, base),
		latestOnlyRun("in-flight", "library/alpine", digest, ports.ScanRunStatusRunning, base.Add(3*time.Minute)),
	} {
		if err := store.UpsertScanRun(ctx, latestOnlyTenant, run); err != nil {
			t.Fatalf("seed %d error = %v", i, err)
		}
	}
	if err := store.UpsertScanRunDetail(ctx, latestOnlyTenant, completedDetail("new", "library/alpine", digest, base.Add(10*time.Minute), "CVE-NEW")); err != nil {
		t.Fatalf("UpsertScanRunDetail(new) error = %v", err)
	}

	rows, err := store.db.Query(`SELECT id FROM scan_runs WHERE tenant = ? AND repository = ? AND digest = ? ORDER BY id`, latestOnlyTenant, "library/alpine", digest)
	if err != nil {
		t.Fatalf("query error = %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan error = %v", err)
		}
		ids = append(ids, id)
	}
	if len(ids) != 2 || ids[0] != "in-flight" || ids[1] != "new" {
		t.Fatalf("remaining runs = %v, want only the new result and the in-flight run", ids)
	}
	for _, kept := range []string{"other-repo", "other-digest"} {
		if _, err := store.GetScanRun(ctx, latestOnlyTenant, kept); err != nil {
			t.Fatalf("GetScanRun(%s) error = %v, want runs of another repository or digest untouched", kept, err)
		}
	}
	var orphanFindings int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM scan_run_findings WHERE run_id NOT IN (SELECT id FROM scan_runs)`).Scan(&orphanFindings); err != nil || orphanFindings != 0 {
		t.Fatalf("orphan findings = %d err = %v, want cascade to remove them", orphanFindings, err)
	}
	detail, err := store.GetScanRunDetail(ctx, latestOnlyTenant, "new")
	if err != nil || len(detail.Findings) != 1 || detail.Findings[0].VulnerabilityID != "CVE-NEW" {
		t.Fatalf("detail = %#v err = %v, want only the new run's findings", detail, err)
	}
}

func TestStoreSkippedMarkerNeverDeletesNorOutranksARealResult(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	digest := "sha256:skip"
	if err := store.UpsertScanRunDetail(ctx, latestOnlyTenant, completedDetail("real", "library/alpine", digest, base, "CVE-1")); err != nil {
		t.Fatalf("seed error = %v", err)
	}
	skipped := latestOnlyRun("push-skipped-x", "library/alpine", digest, ports.ScanRunStatusSkipped, base.Add(time.Minute))
	skipped.Error = "skipped: push scan queue full"
	if err := store.UpsertScanRun(ctx, latestOnlyTenant, skipped); err != nil {
		t.Fatalf("skipped upsert error = %v", err)
	}
	got, err := store.GetLatestScanRunByDigest(ctx, latestOnlyTenant, "library/alpine", digest)
	if err != nil || got.ID != "real" {
		t.Fatalf("latest = %#v err = %v, want the real result to outrank skipped", got, err)
	}
	if _, err := store.GetScanRun(ctx, latestOnlyTenant, "real"); err != nil {
		t.Fatalf("real result deleted by a skipped marker: %v", err)
	}
	// A real completion supersedes the now-pointless marker for its digest.
	if err := store.UpsertScanRunDetail(ctx, latestOnlyTenant, completedDetail("real2", "library/alpine", digest, base.Add(2*time.Minute), "CVE-2")); err != nil {
		t.Fatalf("second completion error = %v", err)
	}
	if n := countRunsForDigest(t, store, "library/alpine", digest); n != 1 {
		t.Fatalf("runs = %d, want exactly the newest completed run", n)
	}
}

func TestStoreReplacementNeverExposesADigestWithoutACompletedResult(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	digest := "sha256:atomic"
	if err := store.UpsertScanRunDetail(ctx, latestOnlyTenant, completedDetail("run-0", "library/alpine", digest, base, "CVE-0")); err != nil {
		t.Fatalf("seed error = %v", err)
	}

	stop := make(chan struct{})
	var writers sync.WaitGroup
	writers.Add(1)
	go func() {
		defer writers.Done()
		for i := 1; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			id := fmt.Sprintf("run-%d", i)
			if err := store.UpsertScanRun(ctx, latestOnlyTenant, latestOnlyRun(id+"-q", "library/alpine", digest, ports.ScanRunStatusQueued, base.Add(time.Duration(i)*time.Second))); err != nil {
				t.Errorf("queue error = %v", err)
				return
			}
			if err := store.UpsertScanRunDetail(ctx, latestOnlyTenant, completedDetail(id, "library/alpine", digest, base.Add(time.Duration(i)*time.Second), "CVE-N")); err != nil {
				t.Errorf("replace error = %v", err)
				return
			}
		}
	}()

	var readers sync.WaitGroup
	for r := 0; r < 4; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for i := 0; i < 1000; i++ {
				run, err := store.GetLatestCompletedScanRunByDigest(ctx, latestOnlyTenant, "library/alpine", digest)
				if err != nil || run.Status != ports.ScanRunStatusCompleted {
					t.Errorf("iteration %d: completed lookup = %#v err = %v, want a completed run always present", i, run, err)
					return
				}
			}
		}()
	}
	readers.Wait()
	close(stop)
	writers.Wait()
}

func TestStoreTouchScanRunCheckedOnlyUpdatesLastCheckedAt(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	if err := store.UpsertScanRunDetail(ctx, latestOnlyTenant, completedDetail("run", "library/alpine", "sha256:touch", base, "CVE-1")); err != nil {
		t.Fatalf("seed error = %v", err)
	}
	checked := base.Add(30 * time.Minute)
	if err := store.TouchScanRunChecked(ctx, latestOnlyTenant, "run", checked); err != nil {
		t.Fatalf("TouchScanRunChecked() error = %v", err)
	}
	got, err := store.GetScanRun(ctx, latestOnlyTenant, "run")
	if err != nil {
		t.Fatalf("GetScanRun() error = %v", err)
	}
	if got.LastCheckedAt == nil || !got.LastCheckedAt.Equal(checked) || got.SettingsHash != "hash-run" || got.DBVersion != 2 || got.Critical != 1 {
		t.Fatalf("run = %#v, want last_checked_at set and every other field intact", got)
	}
	if n := countRunsForDigest(t, store, "library/alpine", "sha256:touch"); n != 1 {
		t.Fatalf("runs = %d, want no new run from a check", n)
	}
}

func TestStoreOldSchemaScanRunsGainFingerprintColumnsAndOldRowsReadAsEmptyHash(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, statement := range []string{
		`CREATE TABLE scan_runs (id TEXT PRIMARY KEY, tenant TEXT NOT NULL, repository TEXT NOT NULL, requested_ref TEXT NOT NULL, digest TEXT NOT NULL, status TEXT NOT NULL, trigger TEXT NOT NULL, started_at TEXT, finished_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, critical INTEGER NOT NULL, high INTEGER NOT NULL, medium INTEGER NOT NULL, low INTEGER NOT NULL, trivy_version TEXT NOT NULL, db_updated_at TEXT, error TEXT NOT NULL);`,
		`INSERT INTO scan_runs VALUES ('old-1','tenant-a','library/alpine','latest','sha256:old','completed','manual',NULL,NULL,'` + now + `','` + now + `',3,0,0,0,'0.50.0','` + now + `','');`,
		`INSERT INTO scan_runs VALUES ('old-2','tenant-a','library/alpine','latest','sha256:old','completed','scheduled',NULL,NULL,'` + now + `','` + now + `',3,0,0,0,'0.50.0','` + now + `','');`,
	} {
		if _, err := raw.Exec(statement); err != nil {
			t.Fatalf("old schema statement error = %v", err)
		}
	}
	raw.Close()

	store, err := New(path)
	if err != nil {
		t.Fatalf("New() on an old-schema database error = %v", err)
	}
	defer store.Close()
	// Opening twice must stay idempotent.
	again, err := New(path)
	if err != nil {
		t.Fatalf("second New() error = %v", err)
	}
	again.Close()

	// The two old rows are duplicates of one digest with identical
	// timestamps: the purge migration keeps one (the larger id, old-2) and
	// deletes the other; the survivor keeps its data with empty defaults.
	if _, err := store.GetScanRun(context.Background(), latestOnlyTenant, "old-1"); err == nil {
		t.Fatalf("duplicate old-1 must be purged by the latest-only migration")
	}
	got, err := store.GetScanRun(context.Background(), latestOnlyTenant, "old-2")
	if err != nil {
		t.Fatalf("GetScanRun(old row) error = %v", err)
	}
	if got.SettingsHash != "" || got.DBVersion != 0 || got.LastCheckedAt != nil || got.Stale || got.Critical != 3 {
		t.Fatalf("old row = %#v, want empty fingerprint defaults and data intact", got)
	}
	if n := countRunsForDigest(t, store, "library/alpine", "sha256:old"); n != 1 {
		t.Fatalf("old rows = %d, want 1 after the purge", n)
	}
}

func completedWithDB(id string, digest string, createdAt time.Time, dbUpdated *time.Time) ports.ScanRunDetail {
	detail := completedDetail(id, "library/alpine", digest, createdAt, "CVE-"+id)
	detail.Run.DBUpdatedAt = dbUpdated
	return detail
}

func TestStoreOutOfOrderCompletionKeepsTheResultWithTheNewerDB(t *testing.T) {
	t.Parallel()
	base := time.Now().UTC().Add(-time.Hour)
	older := base.Add(-24 * time.Hour)
	newer := base

	cases := []struct {
		name     string
		first    ports.ScanRunDetail // completes first
		second   ports.ScanRunDetail // completes last (queued earlier in the dangerous case)
		wantKept string
	}{
		{"older-DB run completing last does not replace the newer-DB result", completedWithDB("new-db", "sha256:ooo", base.Add(time.Minute), &newer), completedWithDB("old-db", "sha256:ooo", base, &older), "new-db"},
		{"newer-DB run completing last replaces the older-DB result", completedWithDB("old-db", "sha256:ooo", base, &older), completedWithDB("new-db", "sha256:ooo", base.Add(time.Minute), &newer), "new-db"},
		{"unknown DB: last completion wins", completedWithDB("a", "sha256:ooo", base, nil), completedWithDB("b", "sha256:ooo", base.Add(time.Minute), nil), "b"},
		{"unknown DB incoming over known DB: last completion wins", completedWithDB("known", "sha256:ooo", base, &newer), completedWithDB("unknown", "sha256:ooo", base.Add(time.Minute), nil), "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newTestStore(t)
			defer store.Close()
			ctx := context.Background()
			// The later-completing run was queued (running) before it finished.
			running := tc.second.Run
			running.Status = ports.ScanRunStatusRunning
			if err := store.UpsertScanRun(ctx, latestOnlyTenant, running); err != nil {
				t.Fatalf("running row error = %v", err)
			}
			if err := store.UpsertScanRunDetail(ctx, latestOnlyTenant, tc.first); err != nil {
				t.Fatalf("first completion error = %v", err)
			}
			if err := store.UpsertScanRunDetail(ctx, latestOnlyTenant, tc.second); err != nil {
				t.Fatalf("second completion error = %v", err)
			}
			got, err := store.GetLatestCompletedScanRunByDigest(ctx, latestOnlyTenant, "library/alpine", "sha256:ooo")
			if err != nil || got.ID != tc.wantKept {
				t.Fatalf("kept = %#v err = %v, want %q", got, err, tc.wantKept)
			}
			if n := countRunsForDigest(t, store, "library/alpine", "sha256:ooo"); n != 1 {
				t.Fatalf("runs = %d, want exactly 1", n)
			}
			detail, err := store.GetScanRunDetail(ctx, latestOnlyTenant, tc.wantKept)
			if err != nil || len(detail.Findings) != 1 || detail.Findings[0].VulnerabilityID != "CVE-"+tc.wantKept {
				t.Fatalf("detail = %#v err = %v, want the kept run's own findings", detail, err)
			}
		})
	}
}
