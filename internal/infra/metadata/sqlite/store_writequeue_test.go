package sqlite

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// TestStoreWriteQueueSurvivesShortBusyTimeoutUnderContention pins that write
// transactions queue in Go instead of polling SQLite's busy handler: with a
// 100 ms busy timeout, 12 writers would starve each other into SQLITE_BUSY.
func TestStoreWriteQueueSurvivesShortBusyTimeoutUnderContention(t *testing.T) {
	t.Parallel()

	store, err := newWithBusyTimeout(filepath.Join(t.TempDir(), "registry.db"), 100)
	if err != nil {
		t.Fatalf("newWithBusyTimeout() error = %v", err)
	}
	defer store.Close()

	const (
		workers    = 12
		iterations = 50
	)

	repo := domain.MustParseRepositoryRef("library/alpine")
	ctx := context.Background()

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		failures []string
	)
	fail := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		failures = append(failures, fmt.Sprintf(format, args...))
	}

	for worker := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for iteration := range iterations {
				payload := fmt.Sprintf(`{"schemaVersion":2,"worker":%d,"iteration":%d}`, worker, iteration)
				manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(payload), nil, nil, nil, nil)
				if err != nil {
					fail("worker %d iteration %d NewManifest() error = %v", worker, iteration, err)
					return
				}

				tag := fmt.Sprintf("w%d-i%d", worker, iteration)
				if err := store.PublishManifest(ctx, "tenant-a", repo, tag, manifest, nil); err != nil {
					fail("worker %d iteration %d PublishManifest() error = %v", worker, iteration, err)
					return
				}

				if _, err := store.DeleteManifestByDigest(ctx, "tenant-a", repo, manifest.Digest); err != nil {
					fail("worker %d iteration %d DeleteManifestByDigest() error = %v", worker, iteration, err)
					return
				}
			}
		}()
	}

	wg.Wait()

	if len(failures) > 0 {
		shown := failures
		if len(shown) > 3 {
			shown = shown[:3]
		}
		t.Fatalf("%d writers failed, first errors: %q", len(failures), shown)
	}
}

func TestFIFOLockCancelledWaiterReturnsContextErrorAndKeepsOwnerIntact(t *testing.T) {
	t.Parallel()

	var lock fifoLock
	if err := lock.acquire(context.Background()); err != nil {
		t.Fatalf("acquire() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- lock.acquire(ctx) }()

	waitForWaiters(t, &lock, 1)
	cancel()

	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire() error = %v, want context.Canceled", err)
	}

	// The cancelled waiter must not have released the owner's lock.
	probe, probeCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer probeCancel()
	if err := lock.acquire(probe); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acquire() while held error = %v, want context.DeadlineExceeded", err)
	}

	lock.release()
	if err := lock.acquire(context.Background()); err != nil {
		t.Fatalf("acquire() after release error = %v", err)
	}
	lock.release()
}

func TestFIFOLockAlreadyCancelledContextDoesNotAcquire(t *testing.T) {
	t.Parallel()

	var lock fifoLock
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := lock.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire() error = %v, want context.Canceled", err)
	}
	if err := lock.acquire(context.Background()); err != nil {
		t.Fatalf("acquire() after cancelled attempt error = %v", err)
	}
	lock.release()
}

func TestFIFOLockServesWaitersInArrivalOrder(t *testing.T) {
	t.Parallel()

	const waiters = 20

	var lock fifoLock
	if err := lock.acquire(context.Background()); err != nil {
		t.Fatalf("acquire() error = %v", err)
	}

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		order []int
	)
	for i := range waiters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := lock.acquire(context.Background()); err != nil {
				t.Errorf("waiter %d acquire() error = %v", i, err)
				return
			}
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
			lock.release()
		}()
		waitForWaiters(t, &lock, i+1)
	}

	lock.release()
	wg.Wait()

	for i, got := range order {
		if got != i {
			t.Fatalf("service order = %v, want arrival order", order)
		}
	}
}

func TestWriteTxReleasesLockExactlyOnce(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	tx, err := store.beginWrite(ctx)
	if err != nil {
		t.Fatalf("beginWrite() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	// A second writer owns the lock now; the first tx's extra Rollbacks must
	// not release it.
	second, err := store.beginWrite(ctx)
	if err != nil {
		t.Fatalf("second beginWrite() error = %v", err)
	}
	_ = tx.Rollback()
	_ = tx.Rollback()

	probe, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if third, err := store.beginWrite(probe); err == nil {
		_ = third.Rollback()
		t.Fatal("beginWrite() acquired the lock while a second writer holds it")
	}

	if err := second.Rollback(); err != nil {
		t.Fatalf("second Rollback() error = %v", err)
	}
	third, err := store.beginWrite(ctx)
	if err != nil {
		t.Fatalf("beginWrite() after release error = %v", err)
	}
	_ = third.Rollback()
}

func TestBeginWriteCancelledWhileWaitingReturnsContextError(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	held, err := store.beginWrite(context.Background())
	if err != nil {
		t.Fatalf("beginWrite() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := store.beginWrite(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("beginWrite() error = %v, want context.DeadlineExceeded", err)
	}

	if err := held.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	next, err := store.beginWrite(context.Background())
	if err != nil {
		t.Fatalf("beginWrite() after cancelled waiter error = %v", err)
	}
	_ = next.Rollback()
}

func TestWritePanicReleasesTheWriteLock(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	func() {
		defer func() { _ = recover() }()
		tx, err := store.beginWrite(context.Background())
		if err != nil {
			t.Errorf("beginWrite() error = %v", err)
			return
		}
		defer func() { _ = tx.Rollback() }()
		panic("boom")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tx, err := store.beginWrite(ctx)
	if err != nil {
		t.Fatalf("beginWrite() after panic error = %v", err)
	}
	_ = tx.Rollback()
}

func waitForWaiters(t *testing.T, lock *fifoLock, want int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		lock.mu.Lock()
		n := lock.waiters.Len()
		lock.mu.Unlock()
		if n == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d queued waiters", want)
}

func TestReadersAreNotBlockedByTheWriteQueue(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})

	held, err := store.beginWrite(ctx)
	if err != nil {
		t.Fatalf("beginWrite() error = %v", err)
	}
	defer func() { _ = held.Rollback() }()

	const queued = 8
	for range queued {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tx, err := store.beginWrite(ctx); err == nil {
				_ = tx.Rollback()
			}
		}()
	}
	waitForWaiters(t, &store.writeLock, queued)

	reads := map[string]func() error{
		"ListRepositoriesWithSummary": func() error {
			_, err := store.ListRepositoriesWithSummary(ctx, "tenant-a", 10, "")
			return err
		},
		"ListScanRuns": func() error {
			_, err := store.ListScanRuns(ctx, "tenant-a", "", 10)
			return err
		},
	}
	for name, read := range reads {
		start := time.Now()
		if err := read(); err != nil {
			t.Fatalf("%s error = %v", name, err)
		}
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Fatalf("%s took %s while writers were queued, want < 500ms", name, elapsed)
		}
	}
}

// TestFIFOLockAbandonAfterGrantPassesOwnershipOn forces the race between
// release() granting a waiter and that waiter's context ending: the grant has
// already happened (element removed, channel closed, held still true) when the
// cancelled waiter runs its cancel branch. The lock must not leak.
func TestFIFOLockAbandonAfterGrantPassesOwnershipOn(t *testing.T) {
	t.Parallel()

	var lock fifoLock
	if err := lock.acquire(context.Background()); err != nil {
		t.Fatalf("acquire() error = %v", err)
	}

	granted := make(chan struct{})
	lock.mu.Lock()
	element := lock.waiters.PushBack(granted)
	lock.mu.Unlock()

	lock.release() // grants the queued waiter: held stays true, granted closed
	lock.abandon(element, granted)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := lock.acquire(ctx); err != nil {
		t.Fatalf("acquire() after a cancelled grant error = %v, want the lock to be free", err)
	}
	lock.release()
}

func TestFIFOLockSurvivesRandomCancellationsWhileReleasing(t *testing.T) {
	t.Parallel()

	const (
		goroutines = 64
		rounds     = 200
	)

	var lock fifoLock
	rng := rand.New(rand.NewSource(42))
	var rngMu sync.Mutex
	pick := func(n int) int {
		rngMu.Lock()
		defer rngMu.Unlock()
		return rng.Intn(n)
	}

	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range rounds {
				ctx, cancel := context.WithCancel(context.Background())
				go func(delay int) {
					for range delay {
						runtime.Gosched()
					}
					cancel()
				}(pick(20))
				if err := lock.acquire(ctx); err == nil {
					runtime.Gosched()
					lock.release()
				}
				cancel()
			}
		}()
	}
	wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := lock.acquire(ctx); err != nil {
		t.Fatalf("final acquire() error = %v, want the lock to be free", err)
	}
	lock.release()
}

// singleStatementWrites lists every store method that issues one write
// statement (no explicit transaction). Each must go through the write queue.
func singleStatementWrites(store *Store) map[string]func(ctx context.Context) error {
	repo := domain.MustParseRepositoryRef("library/alpine")
	upload := domain.UploadState{
		ID:         "upload-queue",
		Repository: repo,
		Status:     domain.UploadStatusActive,
		StartedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		Location:   "/tmp/uploads/upload-queue/data",
	}
	return map[string]func(ctx context.Context) error{
		"SaveUpload":   func(ctx context.Context) error { return store.SaveUpload(ctx, "t", upload) },
		"DeleteUpload": func(ctx context.Context) error { return store.DeleteUpload(ctx, "t", "missing") },
		"PruneExpiredGCReports": func(ctx context.Context) error {
			_, err := store.PruneExpiredGCReports(ctx, time.Now())
			return err
		},
		"DeleteTag": func(ctx context.Context) error { return store.DeleteTag(ctx, "t", repo, "missing") },
		"UpsertScanSettings": func(ctx context.Context) error {
			return store.UpsertScanSettings(ctx, "t", "scanning", ports.ScanSettings{})
		},
		"UpsertScanPolicySettings": func(ctx context.Context) error {
			return store.UpsertScanPolicySettings(ctx, "t", ports.ScanPolicySettings{})
		},
		"UpsertUpdateChannel": func(ctx context.Context) error {
			return store.UpsertUpdateChannel(ctx, "t", ports.UpdateChannelSettings{})
		},
		"UpsertSigningPolicySettings": func(ctx context.Context) error {
			return store.UpsertSigningPolicySettings(ctx, "t", ports.SigningPolicySettings{})
		},
		"UpsertRepositoryFeatureOverride": func(ctx context.Context) error {
			return store.UpsertRepositoryFeatureOverride(ctx, "t", "library/alpine", "scanning", []byte(`{}`))
		},
		"DeleteRepositoryFeatureOverride": func(ctx context.Context) error {
			return store.DeleteRepositoryFeatureOverride(ctx, "t", "library/alpine", "scanning")
		},
		"UpsertFeatureRuntimeState": func(ctx context.Context) error {
			return store.UpsertFeatureRuntimeState(ctx, "t", "scanning", ports.FeatureRuntimeState{})
		},
		"UpsertScanRun": func(ctx context.Context) error {
			return store.UpsertScanRun(ctx, "t", ports.ScanRun{ID: "run-queue", Repository: "library/alpine", Digest: "sha256:" + strings.Repeat("a", 64), Status: ports.ScanRunStatusQueued})
		},
		"RecordScanSweep": func(ctx context.Context) error {
			return store.RecordScanSweep(ctx, "t", ports.ScanSweepSummary{})
		},
		"UpsertScanSchedulerState": func(ctx context.Context) error {
			return store.UpsertScanSchedulerState(ctx, "t", ports.ScanSchedulerState{})
		},
	}
}

// A queued write transaction that outlives the busy timeout must not make a
// single-statement write fail with SQLITE_BUSY: the statement waits in the
// queue, like every other writer, and runs once the holder is done.
func TestSingleStatementWritesQueueBehindALongWriteTransaction(t *testing.T) {
	t.Parallel()

	for name := range singleStatementWrites(nil) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := mustTestStore(t, 50)
			ctx := context.Background()
			holder, err := store.beginWrite(ctx)
			if err != nil {
				t.Fatalf("beginWrite() error = %v", err)
			}
			if _, err := holder.ExecContext(ctx, `INSERT OR IGNORE INTO repository_blobs (tenant, repository, digest, created_at) VALUES ('t','r','d','x')`); err != nil {
				t.Fatalf("holder write error = %v", err)
			}

			done := make(chan error, 1)
			go func() { done <- singleStatementWrites(store)[name](ctx) }()

			time.Sleep(300 * time.Millisecond) // 6x the busy timeout
			if err := holder.Commit(); err != nil {
				t.Fatalf("holder Commit() error = %v", err)
			}
			select {
			case err := <-done:
				if err != nil && isBusy(err) {
					t.Fatalf("%s error = %v, want it queued behind the holder", name, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("%s never returned", name)
			}
		})
	}
}

// A single-statement write that is waiting in the queue gives up with the
// context's error as soon as its context ends, and never touches SQLite.
func TestSingleStatementWritesHonourCancellationWhileQueued(t *testing.T) {
	t.Parallel()

	store := mustTestStore(t, 5000)
	holder, err := store.beginWrite(context.Background())
	if err != nil {
		t.Fatalf("beginWrite() error = %v", err)
	}
	defer holder.Rollback()

	for name, write := range singleStatementWrites(store) {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			start := time.Now()
			err := write(ctx)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("%s error = %v, want context.DeadlineExceeded", name, err)
			}
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Fatalf("%s took %s to give up", name, elapsed)
			}
		})
	}
}

func mustTestStore(t *testing.T, busyMillis int) *Store {
	t.Helper()
	store, err := newWithBusyTimeout(filepath.Join(t.TempDir(), "registry.db"), busyMillis)
	if err != nil {
		t.Fatalf("newWithBusyTimeout() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func isBusy(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "SQLITE_BUSY") || strings.Contains(err.Error(), "database is locked"))
}
