package sqlite

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

func publishTagForSweepTest(t *testing.T, store *Store, repo domain.RepositoryRef, tag string, payload string) domain.Digest {
	t.Helper()
	blobs := []domain.Descriptor{{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: domain.DigestFromBytes([]byte("layer")), Size: 5}}
	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(payload), nil, blobs, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	if err := store.PublishManifest(context.Background(), latestOnlyTenant, repo, tag, manifest, blobs); err != nil {
		t.Fatalf("PublishManifest(%s) error = %v", tag, err)
	}
	return manifest.Digest
}

func TestStoreListTaggedDigestsPaginatesByTagAndSharesDigests(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	repo := domain.MustParseRepositoryRef("library/alpine")
	want := map[string]domain.Digest{}
	for _, tag := range []string{"a", "b", "c", "d", "e"} {
		want[tag] = publishTagForSweepTest(t, store, repo, tag, fmt.Sprintf(`{"schemaVersion":2,"annotations":{"t":%q}}`, tag))
	}
	// Two tags on the same manifest: both are listed, with the same digest.
	shared := publishTagForSweepTest(t, store, repo, "f", `{"schemaVersion":2,"annotations":{"t":"a"}}`)
	if shared != want["a"] {
		t.Fatalf("shared digest = %s, want %s", shared, want["a"])
	}

	var got []ports.TaggedDigest
	after := ""
	for pages := 0; ; pages++ {
		page, err := store.ListTaggedDigests(context.Background(), latestOnlyTenant, repo, 2, after)
		if err != nil {
			t.Fatalf("ListTaggedDigests() error = %v", err)
		}
		got = append(got, page...)
		if len(page) < 2 {
			break
		}
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
		after = page[len(page)-1].Tag
	}
	if len(got) != 6 {
		t.Fatalf("tagged digests = %#v, want 6 tags across pages of 2", got)
	}
	for i, tag := range []string{"a", "b", "c", "d", "e", "f"} {
		if got[i].Tag != tag {
			t.Fatalf("tagged[%d].Tag = %q, want %q (ordered by tag)", i, got[i].Tag, tag)
		}
	}
	if got[0].Digest != want["a"].String() || got[5].Digest != want["a"].String() {
		t.Fatalf("digests for tags a and f = %s, %s, want the shared %s", got[0].Digest, got[5].Digest, want["a"])
	}
	other, err := store.ListTaggedDigests(context.Background(), "tenant-b", repo, 10, "")
	if err != nil || len(other) != 0 {
		t.Fatalf("other tenant tagged digests = %#v, %v, want none", other, err)
	}
}

func TestStoreTouchScanRunsCheckedBatchesAndOnlyTouchesCompletedRuns(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	var ids []string
	for i := 0; i < 450; i++ {
		id := fmt.Sprintf("run-%03d", i)
		ids = append(ids, id)
		if err := store.UpsertScanRunDetail(ctx, latestOnlyTenant, completedDetail(id, "library/alpine", fmt.Sprintf("sha256:d%03d", i), base, "CVE-1")); err != nil {
			t.Fatalf("UpsertScanRunDetail(%s) error = %v", id, err)
		}
	}
	queued := latestOnlyRun("queued-run", "library/alpine", "sha256:queued", ports.ScanRunStatusQueued, base)
	if err := store.UpsertScanRun(ctx, latestOnlyTenant, queued); err != nil {
		t.Fatalf("UpsertScanRun() error = %v", err)
	}
	checked := time.Now().UTC().Truncate(time.Millisecond)

	if err := store.TouchScanRunsChecked(ctx, latestOnlyTenant, append(ids, "queued-run", "missing"), checked); err != nil {
		t.Fatalf("TouchScanRunsChecked() error = %v", err)
	}
	for _, id := range []string{ids[0], ids[199], ids[200], ids[449]} {
		run, err := store.GetScanRun(ctx, latestOnlyTenant, id)
		if err != nil || run.LastCheckedAt == nil || !run.LastCheckedAt.Equal(checked) {
			t.Fatalf("run %s last_checked_at = %v, err = %v, want %v", id, run.LastCheckedAt, err, checked)
		}
	}
	untouched, err := store.GetScanRun(ctx, latestOnlyTenant, "queued-run")
	if err != nil || (untouched.LastCheckedAt != nil && untouched.LastCheckedAt.Equal(checked)) {
		t.Fatalf("queued run last_checked_at = %v, err = %v, want untouched", untouched.LastCheckedAt, err)
	}
	if err := store.TouchScanRunsChecked(ctx, latestOnlyTenant, nil, checked); err != nil {
		t.Fatalf("TouchScanRunsChecked(empty) error = %v", err)
	}
}

func TestStoreRecordScanSweepRoundTripsAndNeverTouchesTheLease(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()
	started := time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC)
	finished := started.Add(time.Minute)
	refreshed := started.Add(time.Second)
	sweep := ports.ScanSweepSummary{StartedAt: &started, FinishedAt: &finished, Visited: 10, SkippedUnchanged: 7, Queued: 2, Errors: 1, LastDBRefreshAt: &refreshed, LastDBRefreshError: "offline", DBFingerprint: "v2@2026-11-01T00:00:00Z", DBChanged: true}

	// Recorded before any lease row exists: the row is created, unowned.
	if err := store.RecordScanSweep(ctx, latestOnlyTenant, sweep); err != nil {
		t.Fatalf("RecordScanSweep() error = %v", err)
	}
	state, err := store.GetScanSchedulerState(ctx, latestOnlyTenant)
	if err != nil {
		t.Fatalf("GetScanSchedulerState() error = %v", err)
	}
	if state.OwnerID != "" || state.LastSweep.Visited != 10 || state.LastSweep.SkippedUnchanged != 7 || state.LastSweep.Queued != 2 || state.LastSweep.Errors != 1 ||
		state.LastSweep.LastDBRefreshError != "offline" || !state.LastSweep.DBChanged || state.LastSweep.DBFingerprint != sweep.DBFingerprint ||
		state.LastSweep.StartedAt == nil || !state.LastSweep.StartedAt.Equal(started) || state.LastSweep.LastDBRefreshAt == nil || !state.LastSweep.LastDBRefreshAt.Equal(refreshed) {
		t.Fatalf("state = %#v, want the recorded sweep and no owner", state)
	}

	// A lease acquisition keeps the sweep columns; a later record keeps the lease.
	now := time.Now().UTC()
	if acquired, _, err := store.TryAcquireScanSchedulerLease(ctx, latestOnlyTenant, "node-a", now, time.Minute); err != nil || !acquired {
		t.Fatalf("TryAcquireScanSchedulerLease() = %v, %v", acquired, err)
	}
	if err := store.HeartbeatScanScheduler(ctx, latestOnlyTenant, "node-a", now, time.Minute); err != nil {
		t.Fatalf("HeartbeatScanScheduler() error = %v", err)
	}
	next := sweep
	next.Visited = 11
	if err := store.RecordScanSweep(ctx, latestOnlyTenant, next); err != nil {
		t.Fatalf("RecordScanSweep(second) error = %v", err)
	}
	state, err = store.GetScanSchedulerState(ctx, latestOnlyTenant)
	if err != nil || state.OwnerID != "node-a" || !state.LeaseExpiresAt.After(now) || state.LastSweep.Visited != 11 {
		t.Fatalf("state = %#v, err = %v, want owner node-a, a live lease and the second sweep", state, err)
	}
}

func TestStoreHeartbeatRefusesToStealALiveLease(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	if acquired, _, err := store.TryAcquireScanSchedulerLease(ctx, latestOnlyTenant, "node-b", now, time.Minute); err != nil || !acquired {
		t.Fatalf("TryAcquireScanSchedulerLease() = %v, %v", acquired, err)
	}
	if err := store.HeartbeatScanScheduler(ctx, latestOnlyTenant, "node-a", now, time.Minute); !errors.Is(err, ports.ErrScanLeaseLost) {
		t.Fatalf("HeartbeatScanScheduler(other owner) error = %v, want ErrScanLeaseLost", err)
	}
	state, _ := store.GetScanSchedulerState(ctx, latestOnlyTenant)
	if state.OwnerID != "node-b" {
		t.Fatalf("owner = %q, want node-b untouched", state.OwnerID)
	}
	// Once that lease expired the other node may heartbeat (reclaim) again.
	if err := store.HeartbeatScanScheduler(ctx, latestOnlyTenant, "node-a", now.Add(2*time.Minute), time.Minute); err != nil {
		t.Fatalf("HeartbeatScanScheduler(after expiry) error = %v", err)
	}
}
