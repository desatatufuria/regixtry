package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"regixtry/internal/ports"
)

type fakeFailureStore struct {
	mu       sync.Mutex
	added    []ports.LoginFailureRecord
	calls    int
	failFrom int // fail every AddLoginFailures call numbered >= failFrom (1-based); 0 never fails
	purges   []time.Time
	block    chan struct{}
}

func (f *fakeFailureStore) AddLoginFailures(ctx context.Context, records []ports.LoginFailureRecord) error {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failFrom > 0 && f.calls >= f.failFrom {
		return errors.New("postgres is down")
	}
	f.added = append(f.added, records...)
	return nil
}

func (f *fakeFailureStore) PurgeLoginFailures(_ context.Context, before time.Time, _ int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purges = append(f.purges, before)
	return 0, nil
}

func (f *fakeFailureStore) ListLoginFailures(context.Context, ports.LoginFailureFilter, *ports.LoginFailureCursor, int) ([]ports.LoginFailureRecord, error) {
	return nil, nil
}

func (f *fakeFailureStore) SummarizeLoginFailures(context.Context, time.Time) (ports.LoginFailureSummary, error) {
	return ports.LoginFailureSummary{}, nil
}

func (f *fakeFailureStore) total() (n int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.added {
		n += r.Count
	}
	return n
}

var aggBase = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func obs(at time.Time, account string) ports.LoginFailureObservation {
	return ports.LoginFailureObservation{At: at, Endpoint: "/auth/token", Reason: "wrong password", Account: account, IP: "203.0.113.7"}
}

func TestAggregatorCountsPerKeyWithFirstAndLastSeen(t *testing.T) {
	t.Parallel()
	store := &fakeFailureStore{}
	agg := NewLoginFailureAggregator(store, LoginFailureOptions{})
	agg.RecordLoginFailure(obs(aggBase.Add(10*time.Second), "alice"))
	agg.RecordLoginFailure(obs(aggBase.Add(40*time.Second), "alice"))
	agg.RecordLoginFailure(obs(aggBase.Add(20*time.Second), "alice"))
	agg.RecordLoginFailure(obs(aggBase.Add(20*time.Second), "bob"))

	if err := agg.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if len(store.added) != 2 {
		t.Fatalf("records = %d, want 2 (one per key): %+v", len(store.added), store.added)
	}
	var alice ports.LoginFailureRecord
	for _, r := range store.added {
		if r.Account == "alice" {
			alice = r
		}
	}
	if alice.Count != 3 || !alice.WindowStart.Equal(aggBase) || !alice.FirstSeen.Equal(aggBase.Add(10*time.Second)) || !alice.LastSeen.Equal(aggBase.Add(40*time.Second)) {
		t.Fatalf("alice = %+v", alice)
	}
}

func TestAggregatorWindowBoundaryIsFiveMinutes(t *testing.T) {
	t.Parallel()
	store := &fakeFailureStore{}
	agg := NewLoginFailureAggregator(store, LoginFailureOptions{})
	agg.RecordLoginFailure(obs(aggBase.Add(5*time.Minute-time.Nanosecond), "alice"))
	agg.RecordLoginFailure(obs(aggBase.Add(5*time.Minute), "alice"))
	_ = agg.Flush(context.Background())
	if len(store.added) != 2 {
		t.Fatalf("records = %+v, want two windows", store.added)
	}
	windows := map[time.Time]bool{}
	for _, r := range store.added {
		windows[r.WindowStart] = true
	}
	if !windows[aggBase] || !windows[aggBase.Add(5*time.Minute)] {
		t.Fatalf("windows = %v", windows)
	}
}

func TestAggregatorFoldsOverflowIntoOneKeyPerWindowAndCountsIt(t *testing.T) {
	t.Parallel()
	store := &fakeFailureStore{}
	agg := NewLoginFailureAggregator(store, LoginFailureOptions{MaxKeys: 3})
	for i := 0; i < 10; i++ {
		agg.RecordLoginFailure(obs(aggBase, fmt.Sprintf("hash:%08d", i)))
	}
	// A key already live keeps counting under its own name, not in overflow.
	agg.RecordLoginFailure(obs(aggBase, "hash:00000000"))
	if stats := agg.Stats(); stats.Live > 4 || stats.Overflowed != 7 {
		t.Fatalf("stats = %+v, want at most 3 keys plus one overflow key and 7 overflowed", stats)
	}
	_ = agg.Flush(context.Background())
	if got := store.total(); got != 11 {
		t.Fatalf("total persisted = %d, want 11 (nothing lost)", got)
	}
	var overflow []ports.LoginFailureRecord
	for _, r := range store.added {
		if r.Account == OverflowAccount {
			overflow = append(overflow, r)
		}
	}
	if len(overflow) != 1 || overflow[0].Count != 7 || overflow[0].Endpoint != OverflowAccount || overflow[0].IP != OverflowAccount {
		t.Fatalf("overflow rows = %+v", overflow)
	}
}

func TestAggregatorMemoryStaysBoundedUnderRandomKeys(t *testing.T) {
	t.Parallel()
	agg := NewLoginFailureAggregator(&fakeFailureStore{failFrom: 1}, LoginFailureOptions{MaxKeys: 100, MaxPending: 250})
	for round := 0; round < 20; round++ {
		for i := 0; i < 5_000; i++ {
			agg.RecordLoginFailure(obs(aggBase.Add(time.Duration(round)*5*time.Minute), fmt.Sprintf("hash:%d-%d", round, i)))
		}
		_ = agg.Flush(context.Background()) // fails: everything goes to the bounded retry buffer
		if stats := agg.Stats(); stats.Live > 101 || stats.Pending > 250 {
			t.Fatalf("round %d: stats = %+v exceed the bounds", round, stats)
		}
	}
	if agg.Stats().Dropped == 0 {
		t.Fatal("expected dropped records to be counted")
	}
}

func TestAggregatorConcurrentAddsLoseNothing(t *testing.T) {
	t.Parallel()
	store := &fakeFailureStore{}
	agg := NewLoginFailureAggregator(store, LoginFailureOptions{})
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 1_000; i++ {
				agg.RecordLoginFailure(obs(aggBase, fmt.Sprintf("user%d", i%7)))
				if i%250 == 0 {
					_ = agg.Flush(context.Background())
				}
			}
		}(g)
	}
	wg.Wait()
	_ = agg.Flush(context.Background())
	if got := store.total(); got != 16_000 {
		t.Fatalf("persisted = %d, want 16000", got)
	}
}

func TestAggregatorFlushFailureKeepsRecordsAndRetriesWithoutDoubleCounting(t *testing.T) {
	t.Parallel()
	store := &fakeFailureStore{failFrom: 1}
	var logs bytes.Buffer
	agg := NewLoginFailureAggregator(store, LoginFailureOptions{Logger: log.New(&logs, "", 0)})
	agg.RecordLoginFailure(obs(aggBase, "alice"))
	if err := agg.Flush(context.Background()); err == nil {
		t.Fatal("Flush() error = nil, want the store failure")
	}
	if agg.Stats().Pending != 1 || store.total() != 0 {
		t.Fatalf("stats = %+v persisted = %d", agg.Stats(), store.total())
	}
	store.mu.Lock()
	store.failFrom = 0
	store.mu.Unlock()
	agg.RecordLoginFailure(obs(aggBase, "alice"))
	if err := agg.Flush(context.Background()); err != nil {
		t.Fatalf("second Flush() error = %v", err)
	}
	if got := store.total(); got != 2 || agg.Stats().Pending != 0 {
		t.Fatalf("persisted = %d pending = %d, want 2 and 0", got, agg.Stats().Pending)
	}
	if !strings.Contains(logs.String(), "login failures not flushed") || strings.Contains(logs.String(), "alice") {
		t.Fatalf("log = %q", logs.String())
	}
}

func TestAggregatorPartialFailureRetriesOnlyTheUnwrittenChunks(t *testing.T) {
	t.Parallel()
	store := &fakeFailureStore{failFrom: 2}
	agg := NewLoginFailureAggregator(store, LoginFailureOptions{FlushChunk: 2})
	for i := 0; i < 5; i++ {
		agg.RecordLoginFailure(obs(aggBase, fmt.Sprintf("user%d", i)))
	}
	if err := agg.Flush(context.Background()); err == nil {
		t.Fatal("want failure on the second chunk")
	}
	if store.total() != 2 || agg.Stats().Pending != 3 {
		t.Fatalf("persisted = %d pending = %d, want 2 and 3", store.total(), agg.Stats().Pending)
	}
	store.mu.Lock()
	store.failFrom = 0
	store.mu.Unlock()
	if err := agg.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.total() != 5 {
		t.Fatalf("persisted = %d, want 5 exactly once each", store.total())
	}
}

func TestAggregatorFlushFailureLogIsRateLimited(t *testing.T) {
	t.Parallel()
	now := aggBase
	var logs bytes.Buffer
	agg := NewLoginFailureAggregator(&fakeFailureStore{failFrom: 1}, LoginFailureOptions{Logger: log.New(&logs, "", 0), Now: func() time.Time { return now }})
	for i := 0; i < 5; i++ {
		agg.RecordLoginFailure(obs(aggBase, "alice"))
		_ = agg.Flush(context.Background())
		now = now.Add(10 * time.Second)
	}
	if lines := strings.Count(logs.String(), "\n"); lines != 1 {
		t.Fatalf("log lines = %d, want 1 within the rate limit window:\n%s", lines, logs.String())
	}
	now = now.Add(time.Minute)
	_ = agg.Flush(context.Background())
	if lines := strings.Count(logs.String(), "\n"); lines != 2 {
		t.Fatalf("log lines after the window = %d, want 2", lines)
	}
}

func TestAggregatorFlushPurgesRowsOlderThanRetention(t *testing.T) {
	t.Parallel()
	store := &fakeFailureStore{}
	agg := NewLoginFailureAggregator(store, LoginFailureOptions{Now: func() time.Time { return aggBase }})
	agg.RecordLoginFailure(obs(aggBase, "alice"))
	_ = agg.Flush(context.Background())
	if len(store.purges) != 1 || !store.purges[0].Equal(aggBase.Add(-LoginFailureRetention)) {
		t.Fatalf("purges = %v, want one with cutoff now-30d", store.purges)
	}
}

func TestAggregatorAddNeverBlocksWhileTheStoreHangs(t *testing.T) {
	t.Parallel()
	store := &fakeFailureStore{block: make(chan struct{})}
	agg := NewLoginFailureAggregator(store, LoginFailureOptions{})
	agg.RecordLoginFailure(obs(aggBase, "alice"))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	flushed := make(chan error, 1)
	go func() { flushed <- agg.Flush(ctx) }()
	time.Sleep(20 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10_000; i++ {
			agg.RecordLoginFailure(obs(aggBase, "bob"))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RecordLoginFailure blocked behind a hanging flush")
	}
	if err := <-flushed; err == nil {
		t.Fatal("Flush() error = nil, want the context deadline")
	}
	if agg.Stats().Pending != 1 {
		t.Fatalf("the unflushed record must stay pending, stats = %+v", agg.Stats())
	}
}

func TestAggregatorRunFlushesOnIntervalAndAtShutdown(t *testing.T) {
	t.Parallel()
	store := &fakeFailureStore{}
	agg := NewLoginFailureAggregator(store, LoginFailureOptions{FlushInterval: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { agg.Run(ctx); close(done) }()

	agg.RecordLoginFailure(obs(aggBase, "alice"))
	deadline := time.Now().Add(2 * time.Second)
	for store.total() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if store.total() != 1 {
		t.Fatal("the interval flush never ran")
	}
	agg.RecordLoginFailure(obs(aggBase, "bob"))
	cancel()
	<-done
	if err := agg.Shutdown(time.Second); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if store.total() != 2 {
		t.Fatalf("persisted = %d, want 2 after the shutdown flush", store.total())
	}
}

func TestAggregatorShutdownIsBoundedWhenPostgresHangs(t *testing.T) {
	t.Parallel()
	agg := NewLoginFailureAggregator(&fakeFailureStore{block: make(chan struct{})}, LoginFailureOptions{})
	agg.RecordLoginFailure(obs(aggBase, "alice"))
	start := time.Now()
	if err := agg.Shutdown(100 * time.Millisecond); err == nil {
		t.Fatal("Shutdown() error = nil, want the deadline")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("Shutdown took %v", time.Since(start))
	}
}

func TestAggregatorBoundsAttackerControlledFieldLengths(t *testing.T) {
	t.Parallel()
	store := &fakeFailureStore{}
	agg := NewLoginFailureAggregator(store, LoginFailureOptions{})
	huge := strings.Repeat("x", 10_000)
	agg.RecordLoginFailure(ports.LoginFailureObservation{At: aggBase, Endpoint: huge, Reason: huge, Account: huge, IP: huge})
	_ = agg.Flush(context.Background())
	r := store.added[0]
	for _, field := range []string{r.Endpoint, r.Reason, r.Account, r.IP} {
		if len(field) > 128 {
			t.Fatalf("field kept %d bytes", len(field))
		}
	}
}

func BenchmarkAggregatorRecordLoginFailure(b *testing.B) {
	agg := NewLoginFailureAggregator(&fakeFailureStore{}, LoginFailureOptions{})
	o := obs(aggBase, "alice")
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			agg.RecordLoginFailure(o)
		}
	})
}

func TestAggregatorSustains100kFailuresPerSecond(t *testing.T) {
	t.Parallel()
	agg := NewLoginFailureAggregator(&fakeFailureStore{}, LoginFailureOptions{})
	start := time.Now()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 25_000; i++ {
				agg.RecordLoginFailure(obs(aggBase, fmt.Sprintf("hash:%d", i%500)))
			}
		}(g)
	}
	wg.Wait()
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("200k adds took %v, want well under 100k/s budget", elapsed)
	}
}

func TestReadTriggeredFlushIsCoalescedAndNeverPurges(t *testing.T) {
	t.Parallel()
	store := &fakeFailureStore{}
	now := aggBase
	agg := NewLoginFailureAggregator(store, LoginFailureOptions{Now: func() time.Time { return now }})
	for i := 0; i < 300; i++ {
		agg.RecordLoginFailure(obs(now, fmt.Sprintf("user%d", i%3)))
		if err := agg.FlushForRead(context.Background()); err != nil {
			t.Fatal(err)
		}
		now = now.Add(10 * time.Millisecond) // 300 pairs span 3 seconds
	}
	store.mu.Lock()
	calls, purges := store.calls, len(store.purges)
	store.mu.Unlock()
	if calls > 2 {
		t.Fatalf("store calls = %d for 300 failure+read pairs over 3 s, want at most 2 (one per 2 s window)", calls)
	}
	if purges != 0 {
		t.Fatalf("purges = %d, want none from read-triggered flushes", purges)
	}
	if store.total() == 0 {
		t.Fatal("the data of the flush must be visible")
	}
	// Counts stay exact: what the reads did not flush the timer flush writes.
	if err := agg.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.total() != 300 || len(store.purges) != 1 {
		t.Fatalf("total = %d purges = %d, want 300 and 1 (the timer flush purges)", store.total(), len(store.purges))
	}
}

func TestConcurrentReadersShareOneFlush(t *testing.T) {
	t.Parallel()
	store := &fakeFailureStore{}
	agg := NewLoginFailureAggregator(store, LoginFailureOptions{})
	agg.RecordLoginFailure(obs(aggBase, "alice"))
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = agg.FlushForRead(context.Background()) }()
	}
	wg.Wait()
	if store.calls != 1 || store.total() != 1 {
		t.Fatalf("calls = %d total = %d, want one shared flush", store.calls, store.total())
	}
}

func TestRetryNeverResendsAcknowledgedChunks(t *testing.T) {
	t.Parallel()
	store := &fakeFailureStore{failFrom: 3}
	agg := NewLoginFailureAggregator(store, LoginFailureOptions{FlushChunk: 2})
	for i := 0; i < 6; i++ {
		agg.RecordLoginFailure(obs(aggBase, fmt.Sprintf("user%d", i)))
	}
	if err := agg.Flush(context.Background()); err == nil {
		t.Fatal("want the failure on the third chunk")
	}
	acknowledged := map[string]int{}
	for _, r := range store.added {
		acknowledged[r.Account]++
	}
	if len(acknowledged) != 4 {
		t.Fatalf("acknowledged = %v, want 4 accounts", acknowledged)
	}
	store.mu.Lock()
	store.failFrom = 0
	store.added = nil
	store.mu.Unlock()
	if err := agg.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.added) != 2 {
		t.Fatalf("resent %d records, want only the 2 of the failed chunk", len(store.added))
	}
	for _, r := range store.added {
		if acknowledged[r.Account] > 0 {
			t.Fatalf("%s was acknowledged before and re-sent", r.Account)
		}
	}
}
