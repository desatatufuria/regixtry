package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
)

var latestOnlyColumns = []string{"settings_hash", "db_version", "last_checked_at", "stale", "stale_reason"}

// openOldSchemaDB creates a database and rewinds it to the pre-latest-only
// state: the T1 columns and the digest index are dropped and the purge marker
// is absent. Rows are then inserted with the 18 original columns only.
func openOldSchemaDB(t *testing.T) (string, *Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.db")
	store, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	rewindToOldSchema(t, store)
	return path, store
}

func rewindToOldSchema(t *testing.T, store *Store) {
	t.Helper()
	if _, err := store.db.Exec(`DROP INDEX IF EXISTS idx_scan_runs_digest`); err != nil {
		t.Fatal(err)
	}
	for _, column := range latestOnlyColumns {
		if _, err := store.db.Exec(`ALTER TABLE scan_runs DROP COLUMN ` + column); err != nil {
			t.Fatalf("drop %s: %v", column, err)
		}
	}
	if _, err := store.db.Exec(`DELETE FROM schema_backfills WHERE name = ?`, scanHistoryPurgeMarker); err != nil {
		t.Fatal(err)
	}
}

type oldRun struct {
	tenant, repo, digest, id, status string
	created, updated                 string
	critical, high                   int
	findings                         int
}

func insertOldRuns(t *testing.T, store *Store, runs []oldRun) {
	t.Helper()
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	run, err := tx.Prepare(`INSERT INTO scan_runs (id, tenant, repository, requested_ref, digest, status, trigger, created_at, updated_at, critical, high, medium, low, trivy_version, error) VALUES (?, ?, ?, 'latest', ?, ?, 'manual', ?, ?, ?, ?, 0, 0, '0.57.1', '')`)
	if err != nil {
		t.Fatal(err)
	}
	finding, err := tx.Prepare(`INSERT INTO scan_run_findings (run_id, position, severity, vulnerability_id) VALUES (?, ?, 'HIGH', ?)`)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := tx.Prepare(`INSERT INTO scan_run_db_freshness (run_id, db_version) VALUES (?, 2)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		updated := r.updated
		if updated == "" {
			updated = r.created
		}
		if _, err := run.Exec(r.id, r.tenant, r.repo, r.digest, r.status, r.created, updated, r.critical, r.high); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < r.findings; i++ {
			if _, err := finding.Exec(r.id, i, fmt.Sprintf("CVE-%s-%d", r.id, i)); err != nil {
				t.Fatal(err)
			}
		}
		if r.status == "completed" {
			if _, err := fresh.Exec(r.id); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func ts(base time.Time, seconds int) string {
	return base.Add(time.Duration(seconds) * time.Second).UTC().Format(time.RFC3339Nano)
}

func runIDs(t *testing.T, store *Store) []string {
	t.Helper()
	rows, err := store.db.Query(`SELECT id FROM scan_runs ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func scalar(t *testing.T, store *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := store.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// baselineLatestCompleted is the baseline GetLatestCompletedScanRunByDigest
// ordering over the old columns only (the store method selects T1 columns an
// old schema lacks).
func baselineLatestCompleted(t *testing.T, store *Store, tenant, repo, digest string) (string, int, int, bool) {
	t.Helper()
	var id string
	var critical, high int
	err := store.db.QueryRow(`SELECT id, critical, high FROM scan_runs WHERE tenant = ? AND repository = ? AND digest = ? AND status = 'completed' ORDER BY created_at DESC, rowid DESC LIMIT 1`, tenant, repo, digest).Scan(&id, &critical, &high)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, 0, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return id, critical, high, true
}

// policyOutcome mirrors the pull policy: the latest completed run decides, no
// completed run fails open.
func policyOutcome(threshold string, critical, high int, found bool) bool {
	if !found {
		return false
	}
	if threshold == "critical_high" {
		return critical > 0 || high > 0
	}
	return critical > 0
}

func TestPurgeKeepsOnlyTheIntendedRunsOnAnOldSchemaFixture(t *testing.T) {
	t.Parallel()
	path, store := openOldSchemaDB(t)
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	const t1, t2 = "tenant-1", "tenant-2"
	runs := []oldRun{
		// completed with different findings, failed newer, skipped newest, in-flight
		{t1, "r/a", "sha256:multi", "multi-c1", "completed", ts(base, 1), "", 0, 0, 3},
		{t1, "r/a", "sha256:multi", "multi-c2", "completed", ts(base, 2), "", 2, 1, 2},
		{t1, "r/a", "sha256:multi", "multi-f1", "failed", ts(base, 3), "", 0, 0, 0},
		{t1, "r/a", "sha256:multi", "multi-s1", "skipped", ts(base, 4), "", 0, 0, 0},
		{t1, "r/a", "sha256:multi", "multi-q", "queued", ts(base, 5), "", 0, 0, 0},
		{t1, "r/a", "sha256:multi", "multi-run", "running", ts(base, 6), "", 0, 0, 0},
		// only failed runs, plus a skipped row (a real result exists: skipped goes)
		{t1, "r/a", "sha256:failed", "failed-1", "failed", ts(base, 1), "", 0, 0, 0},
		{t1, "r/a", "sha256:failed", "failed-2", "failed", ts(base, 2), "", 0, 0, 0},
		{t1, "r/a", "sha256:failed", "failed-s", "skipped", ts(base, 3), "", 0, 0, 0},
		// only a skipped row: kept
		{t1, "r/a", "sha256:skipped", "skipped-only", "skipped", ts(base, 1), "", 0, 0, 0},
		// only skipped duplicates: newest kept
		{t1, "r/a", "sha256:skipped2", "skipped2-1", "skipped", ts(base, 1), "", 0, 0, 0},
		{t1, "r/a", "sha256:skipped2", "skipped2-2", "skipped", ts(base, 2), "", 0, 0, 0},
		// completed only: untouched
		{t1, "r/a", "sha256:single", "single-c", "completed", ts(base, 1), "", 1, 0, 1},
		// no completed, in-flight with an old failed: failed and in-flight stay
		{t1, "r/a", "sha256:inflight", "inflight-f", "failed", ts(base, 1), "", 0, 0, 0},
		{t1, "r/a", "sha256:inflight", "inflight-q", "queued", ts(base, 2), "", 0, 0, 0},
		// same digest in another repository and another tenant is independent
		{t1, "r/b", "sha256:multi", "b-c1", "completed", ts(base, 1), "", 1, 0, 1},
		{t1, "r/b", "sha256:multi", "b-c2", "completed", ts(base, 2), "", 0, 0, 1},
		{t2, "r/a", "sha256:multi", "t2-c1", "completed", ts(base, 9), "", 0, 1, 1},
		{t2, "r/a", "sha256:multi", "t2-c2", "completed", ts(base, 3), "", 0, 0, 1},
	}
	insertOldRuns(t, store, runs)

	type key struct{ tenant, repo, digest string }
	keys := map[key]bool{}
	for _, r := range runs {
		keys[key{r.tenant, r.repo, r.digest}] = true
	}
	type before struct {
		id           string
		critical     int
		high         int
		found        bool
		findingCount int
	}
	beforeState := map[key]before{}
	for k := range keys {
		id, c, h, found := baselineLatestCompleted(t, store, k.tenant, k.repo, k.digest)
		beforeState[k] = before{id, c, h, found, scalar(t, store, `SELECT COUNT(*) FROM scan_run_findings WHERE run_id = ?`, id)}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// Migration runs on open; the T1 additive columns must exist first.
	store, err := New(path)
	if err != nil {
		t.Fatalf("New() on old schema error = %v", err)
	}
	defer store.Close()

	want := []string{
		"b-c2", "failed-2", "inflight-f", "inflight-q", "multi-c2", "multi-q", "multi-run",
		"single-c", "skipped-only", "skipped2-2", "t2-c1",
	}
	sort.Strings(want)
	if got := runIDs(t, store); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("remaining runs = %v; want %v", got, want)
	}
	if n := scalar(t, store, `SELECT COUNT(*) FROM scan_run_findings WHERE run_id IN ('multi-c1','b-c1','t2-c2')`); n != 0 {
		t.Fatalf("findings of deleted runs remain: %d", n)
	}
	if n := scalar(t, store, `SELECT COUNT(*) FROM scan_run_db_freshness WHERE run_id IN ('multi-c1','b-c1','t2-c2')`); n != 0 {
		t.Fatalf("db_freshness of deleted runs remain: %d", n)
	}
	if n := scalar(t, store, `SELECT COUNT(*) FROM scan_run_findings WHERE run_id = 'multi-c2'`); n != 2 {
		t.Fatalf("kept completed run findings = %d; want 2", n)
	}

	ctx := context.Background()
	for k, b := range beforeState {
		got, err := store.GetLatestCompletedScanRunByDigest(ctx, k.tenant, k.repo, k.digest)
		if !b.found {
			if !domain.IsCode(err, domain.ErrorCodeNotFound) {
				t.Fatalf("%v: expected not found, got %v", k, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%v: %v", k, err)
		}
		if got.ID != b.id || got.Critical != b.critical || got.High != b.high {
			t.Fatalf("%v: latest completed changed: %s (%d/%d), want %s (%d/%d)", k, got.ID, got.Critical, got.High, b.id, b.critical, b.high)
		}
		if n := scalar(t, store, `SELECT COUNT(*) FROM scan_run_findings WHERE run_id = ?`, got.ID); n != b.findingCount {
			t.Fatalf("%v: findings = %d; want %d", k, n, b.findingCount)
		}
		for _, threshold := range []string{"critical", "critical_high"} {
			if policyOutcome(threshold, b.critical, b.high, b.found) != policyOutcome(threshold, got.Critical, got.High, true) {
				t.Fatalf("%v: pull policy outcome changed at %s", k, threshold)
			}
		}
	}
	// The failed-only digest keeps its newest failed run.
	if _, err := store.GetLatestCompletedScanRunByDigest(ctx, t1, "r/a", "sha256:failed"); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("failed-only digest must still fail open, got %v", err)
	}
	if scalar(t, store, `SELECT COUNT(*) FROM schema_backfills WHERE name = ?`, scanHistoryPurgeMarker) != 1 {
		t.Fatal("purge marker missing")
	}
}

func TestPurgeIsIdempotentAndCheapOnSecondOpen(t *testing.T) {
	t.Parallel()
	path, store := openOldSchemaDB(t)
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	var runs []oldRun
	for i := 0; i < 5; i++ {
		runs = append(runs, oldRun{"t", "r", "sha256:x", fmt.Sprintf("c%d", i), "completed", ts(base, i), "", 0, 0, 1})
	}
	insertOldRuns(t, store, runs)
	store.Close()
	for pass := 1; pass <= 2; pass++ {
		s, err := New(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := runIDs(t, s); len(got) != 1 || got[0] != "c4" {
			t.Fatalf("pass %d: runs = %v", pass, got)
		}
		if pass == 2 {
			// A marker-gated second run touches nothing: add a duplicate,
			// reopen, and it must survive (no re-purge).
			insertOldRunsCurrent(t, s, "c-extra")
		}
		s.Close()
	}
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := runIDs(t, s); len(got) != 2 {
		t.Fatalf("marker must make the purge a no-op, runs = %v", got)
	}
}

func insertOldRunsCurrent(t *testing.T, store *Store, id string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.db.Exec(`INSERT INTO scan_runs (id, tenant, repository, requested_ref, digest, status, trigger, created_at, updated_at, critical, high, medium, low, trivy_version, error) VALUES (?, 't', 'r', 'latest', 'sha256:x', 'completed', 'manual', ?, ?, 0, 0, 0, 0, '', '')`, id, now, now); err != nil {
		t.Fatal(err)
	}
}

func TestPurgeIsSafeOnAFreshDatabase(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	if scalar(t, store, `SELECT COUNT(*) FROM schema_backfills WHERE name = ?`, scanHistoryPurgeMarker) != 1 {
		t.Fatal("fresh database must record the marker")
	}
}

func TestPurgePicksTheNewestInstantNotTheLargestString(t *testing.T) {
	t.Parallel()
	path, store := openOldSchemaDB(t)
	// "..:05Z" sorts above "..:05.5Z" as a string but is the OLDER instant.
	// The +02:00 value is 10:00:04Z, older than both.
	insertOldRuns(t, store, []oldRun{
		{"t", "r", "sha256:mixed", "mixed-a", "completed", "2026-01-01T10:00:05Z", "", 0, 0, 1},
		{"t", "r", "sha256:mixed", "mixed-b", "completed", "2026-01-01T10:00:05.5Z", "", 0, 0, 1},
		{"t", "r", "sha256:mixed", "mixed-c", "completed", "2026-01-01T12:00:04+02:00", "", 0, 0, 1},
		// tie on created_at: updated_at decides
		{"t", "r", "sha256:tie-updated", "tu-a", "completed", "2026-01-01T10:00:00Z", "2026-01-01T10:00:09Z", 0, 0, 1},
		{"t", "r", "sha256:tie-updated", "tu-b", "completed", "2026-01-01T10:00:00Z", "2026-01-01T10:00:01Z", 0, 0, 1},
		// full tie: larger id decides
		{"t", "r", "sha256:tie-id", "ti-a", "completed", "2026-01-01T10:00:00Z", "", 0, 0, 1},
		{"t", "r", "sha256:tie-id", "ti-b", "completed", "2026-01-01T10:00:00Z", "", 0, 0, 1},
	})
	store.Close()
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got := strings.Join(runIDs(t, store), ","); got != "mixed-b,ti-b,tu-a" {
		t.Fatalf("remaining = %s; want mixed-b,ti-b,tu-a", got)
	}
}

func TestPurgeResumesAfterAnInterruption(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "registry.db")
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	var runs []oldRun
	for d := 0; d < 40; d++ {
		for i := 0; i < 30; i++ {
			status := "completed"
			if i%5 == 0 {
				status = "failed"
			}
			runs = append(runs, oldRun{"t", "r", fmt.Sprintf("sha256:d%02d", d), fmt.Sprintf("d%02d-%02d", d, i), status, ts(base, i), "", 0, 0, 1})
		}
	}
	insertOldRuns(t, store, runs)
	if _, err := store.db.Exec(`DELETE FROM schema_backfills WHERE name = ?`, scanHistoryPurgeMarker); err != nil {
		t.Fatal(err)
	}
	total := len(runs)

	last := total
	boom := errors.New("killed")
	err = store.purgeDuplicateScanHistory(func(batch int) error {
		now := scalar(t, store, `SELECT COUNT(*) FROM scan_runs`)
		if last-now > scanHistoryPurgeBatchSize {
			t.Errorf("batch %d deleted %d rows; max %d", batch, last-now, scanHistoryPurgeBatchSize)
		}
		last = now
		if batch == 2 {
			return boom
		}
		return nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("purge error = %v; want injected", err)
	}
	if scalar(t, store, `SELECT COUNT(*) FROM schema_backfills WHERE name = ?`, scanHistoryPurgeMarker) != 0 {
		t.Fatal("marker must not exist after an interrupted purge")
	}
	if now := scalar(t, store, `SELECT COUNT(*) FROM scan_runs`); now >= total || now <= 40 {
		t.Fatalf("expected a partial purge, rows = %d of %d", now, total)
	}
	store.Close()

	store, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got := scalar(t, store, `SELECT COUNT(*) FROM scan_runs`); got != 40 {
		t.Fatalf("rows after resume = %d; want 40", got)
	}
	if scalar(t, store, `SELECT COUNT(*) FROM schema_backfills WHERE name = ?`, scanHistoryPurgeMarker) != 1 {
		t.Fatal("marker missing after resume")
	}
	if n := scalar(t, store, `SELECT COUNT(*) FROM scan_runs WHERE id LIKE '%-29' AND status = 'completed'`); n != 40 {
		t.Fatalf("newest completed kept for %d of 40 digests", n)
	}
}

func TestPurgeOfFiftyThousandRunsLetsPublishesInterleave(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("large fixture")
	}
	_, store := openOldSchemaDB(t)
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	const digests, perDigest = 1000, 50
	runs := make([]oldRun, 0, digests*perDigest)
	for d := 0; d < digests; d++ {
		for i := 0; i < perDigest; i++ {
			status := []string{"completed", "failed", "skipped", "completed"}[i%4]
			runs = append(runs, oldRun{"t", fmt.Sprintf("r/%d", d%7), fmt.Sprintf("sha256:d%04d", d), fmt.Sprintf("d%04d-%02d", d, i), status, ts(base, i), "", 0, 0, 0})
		}
	}
	insertOldRuns(t, store, runs)
	defer store.Close()
	// init() creates this index before the purge runs in production.
	if _, err := store.db.Exec(`CREATE INDEX IF NOT EXISTS idx_scan_runs_digest ON scan_runs (tenant, repository, digest)`); err != nil {
		t.Fatal(err)
	}

	repo := domain.MustParseRepositoryRef("team/pushes")
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		maxPush  time.Duration
		pushes   int
		pushErr  error
		stop     = make(chan struct{})
		purgeErr error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(fmt.Sprintf(`{"schemaVersion":2,"i":%d}`, i)), nil, nil, nil, nil)
			if err != nil {
				pushErr = err
				return
			}
			started := time.Now()
			if err := store.PublishManifest(context.Background(), "t", repo, fmt.Sprintf("v%d", i), manifest, nil); err != nil {
				pushErr = err
				return
			}
			d := time.Since(started)
			mu.Lock()
			pushes++
			if d > maxPush {
				maxPush = d
			}
			mu.Unlock()
		}
	}()

	started := time.Now()
	purgeErr = store.purgeDuplicateScanHistory(nil)
	duration := time.Since(started)
	close(stop)
	wg.Wait()
	if purgeErr != nil || pushErr != nil {
		t.Fatalf("purge error = %v, push error = %v", purgeErr, pushErr)
	}
	t.Logf("purged %d runs in %s; %d concurrent publishes, slowest %s", len(runs), duration, pushes, maxPush)
	bound, pushBound := 30*time.Second, 2*time.Second
	if raceEnabled {
		// The race detector slows sqlite roughly tenfold; the structure
		// under test (batches, interleaving) is what matters there.
		bound, pushBound = 5*time.Minute, 10*time.Second
	}
	if duration > bound {
		t.Fatalf("purge took %s; bound %s", duration, bound)
	}
	if pushes == 0 || maxPush > pushBound {
		t.Fatalf("publishes = %d, slowest = %s; the purge must not starve the write queue", pushes, maxPush)
	}
	if got := scalar(t, store, `SELECT COUNT(*) FROM scan_runs`); got != digests {
		t.Fatalf("rows = %d; want %d", got, digests)
	}
}
