package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
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
