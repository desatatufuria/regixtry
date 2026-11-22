package scanning

import (
	"context"
	"sync"
	"testing"
	"time"

	"regixtry/internal/ports"
)

func TestSchedulerPreventsOverlapAndReclaimsStaleLease(t *testing.T) {
	t.Parallel()

	store := &fakeLeaseStore{}
	service := &fakeSchedulerService{settings: ports.ScanSettings{Enabled: true, ScheduleEnabled: true, Interval: time.Millisecond, Timeout: time.Minute, MaxConcurrency: 2, CacheDir: "/tmp/trivy-cache", BinaryPath: "trivy"}}
	scheduler := NewScheduler(store, service, "tenant-a", "node-a", time.Millisecond, 5*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := scheduler.Run(ctx); err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		t.Fatalf("Run() error = %v", err)
	}
	if service.maxConcurrent > 1 {
		t.Fatalf("maxConcurrent = %d, want non-overlapping batches", service.maxConcurrent)
	}

	store.forceStale()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel2()
	if err := scheduler.Run(ctx2); err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		t.Fatalf("Run(reclaim) error = %v", err)
	}
	if service.calls == 0 {
		t.Fatal("calls = 0, want scheduled batch executions")
	}
}

func TestSchedulerRespectsConfiguredInterval(t *testing.T) {
	t.Parallel()

	store := &fakeLeaseStore{}
	service := &fakeSchedulerService{settings: ports.ScanSettings{Enabled: true, ScheduleEnabled: true, Interval: 25 * time.Millisecond, Timeout: time.Minute, MaxConcurrency: 1, CacheDir: "/tmp/trivy-cache", BinaryPath: "trivy"}}
	scheduler := NewScheduler(store, service, "tenant-a", "node-a", time.Millisecond, 5*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Millisecond)
	defer cancel()
	if err := scheduler.Run(ctx); err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		t.Fatalf("Run() error = %v", err)
	}
	if service.calls != 1 {
		t.Fatalf("calls = %d, want exactly 1 scheduled batch before configured interval elapses", service.calls)
	}
}

func TestSchedulerTriggersManagedRuntimeBatchesThroughServiceContract(t *testing.T) {
	t.Parallel()

	store := &fakeLeaseStore{}
	service := &fakeSchedulerService{settings: ports.ScanSettings{Enabled: true, ScheduleEnabled: true, Interval: 10 * time.Millisecond, Timeout: time.Minute, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}}
	scheduler := NewScheduler(store, service, "tenant-a", "node-a", time.Millisecond, 5*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := scheduler.Run(ctx); err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		t.Fatalf("Run() error = %v", err)
	}
	if service.calls != 1 {
		t.Fatalf("calls = %d, want one managed batch trigger", service.calls)
	}
}

type fakeLeaseStore struct {
	mu    sync.Mutex
	state ports.ScanSchedulerState
}

func (f *fakeLeaseStore) TryAcquireScanSchedulerLease(_ context.Context, _ string, owner string, now time.Time, leaseTTL time.Duration) (bool, ports.ScanSchedulerState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state.OwnerID != "" && f.state.LeaseExpiresAt.After(now) && f.state.OwnerID != owner {
		return false, f.state, nil
	}
	f.state.OwnerID = owner
	f.state.LeaseExpiresAt = now.Add(leaseTTL)
	f.state.LastHeartbeatAt = now
	if f.state.BatchStartedAt == nil {
		startedAt := now
		f.state.BatchStartedAt = &startedAt
	}
	return true, f.state, nil
}

func (f *fakeLeaseStore) HeartbeatScanScheduler(_ context.Context, _ string, owner string, now time.Time, leaseTTL time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state.OwnerID = owner
	f.state.LeaseExpiresAt = now.Add(leaseTTL)
	f.state.LastHeartbeatAt = now
	return nil
}

func (f *fakeLeaseStore) forceStale() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state.LeaseExpiresAt = time.Now().Add(-time.Second)
	f.state.OwnerID = "node-z"
}

type fakeSchedulerService struct {
	mu            sync.Mutex
	settings      ports.ScanSettings
	calls         int
	inFlight      int
	maxConcurrent int
	runDuration   time.Duration
}

func (f *fakeSchedulerService) GetScanSettings(context.Context) (ports.ScanSettings, error) {
	return f.settings, nil
}

func (f *fakeSchedulerService) RunScheduledScans(ctx context.Context) error {
	f.mu.Lock()
	f.calls++
	f.inFlight++
	if f.inFlight > f.maxConcurrent {
		f.maxConcurrent = f.inFlight
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()
	runDuration := f.runDuration
	if runDuration <= 0 {
		runDuration = 2 * time.Millisecond
	}
	select {
	case <-time.After(runDuration):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// fakeClock is a manually advanced clock with a manually fired ticker.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	ticks chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC), ticks: make(chan time.Time)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *fakeClock) ticker(time.Duration) (<-chan time.Time, func()) { return c.ticks, func() {} }

// recordingLeaseStore records every heartbeat and can refuse them.
type recordingLeaseStore struct {
	fakeLeaseStore
	hbMu       sync.Mutex
	heartbeats []heartbeat
	err        error
}

type heartbeat struct {
	at     time.Time
	expiry time.Time
}

func (r *recordingLeaseStore) HeartbeatScanScheduler(ctx context.Context, tenant string, owner string, now time.Time, leaseTTL time.Duration) error {
	r.hbMu.Lock()
	defer r.hbMu.Unlock()
	r.heartbeats = append(r.heartbeats, heartbeat{at: now, expiry: now.Add(leaseTTL)})
	if r.err != nil {
		return r.err
	}
	return r.fakeLeaseStore.HeartbeatScanScheduler(ctx, tenant, owner, now, leaseTTL)
}

func (r *recordingLeaseStore) heartbeatCount() int {
	r.hbMu.Lock()
	defer r.hbMu.Unlock()
	return len(r.heartbeats)
}

// blockingSweepService blocks RunScheduledScans until released or cancelled.
type blockingSweepService struct {
	settings  ports.ScanSettings
	started   chan struct{}
	release   chan struct{}
	cancelled chan struct{}
}

func (b *blockingSweepService) GetScanSettings(context.Context) (ports.ScanSettings, error) {
	return b.settings, nil
}

func (b *blockingSweepService) RunScheduledScans(ctx context.Context) error {
	close(b.started)
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		close(b.cancelled)
		return ctx.Err()
	}
}

func newBlockingSweepService() *blockingSweepService {
	return &blockingSweepService{
		settings:  ports.ScanSettings{Enabled: true, ScheduleEnabled: true, Interval: time.Hour},
		started:   make(chan struct{}),
		release:   make(chan struct{}),
		cancelled: make(chan struct{}),
	}
}

func fireTick(t *testing.T, clock *fakeClock, wantHeartbeats int, store *recordingLeaseStore) {
	t.Helper()
	select {
	case clock.ticks <- clock.Now():
	case <-time.After(2 * time.Second):
		t.Fatal("renewal loop did not take the tick")
	}
	deadline := time.Now().Add(2 * time.Second)
	for store.heartbeatCount() < wantHeartbeats {
		if time.Now().After(deadline) {
			t.Fatalf("heartbeats = %d, want %d", store.heartbeatCount(), wantHeartbeats)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSchedulerRenewsTheLeaseWhileASweepOutlastsTheTTL(t *testing.T) {
	t.Parallel()
	const ttl = 30 * time.Second
	clock := newFakeClock()
	store := &recordingLeaseStore{}
	service := newBlockingSweepService()
	scheduler := NewScheduler(store, service, "tenant-a", "node-a", time.Minute, ttl)
	scheduler.now, scheduler.newTicker = clock.Now, clock.ticker

	done := make(chan error, 1)
	go func() {
		_, err := scheduler.tick(context.Background())
		done <- err
	}()
	<-service.started
	if got := store.heartbeatCount(); got != 1 {
		t.Fatalf("heartbeats at sweep start = %d, want 1 (the acquire heartbeat)", got)
	}

	// The sweep runs for 5x the TTL: every renewal moves the lease expiry
	// forward, so it never lapses while the sweep is alive.
	for i := 1; i <= 5; i++ {
		clock.advance(ttl)
		fireTick(t, clock, 1+i, store)
		store.hbMu.Lock()
		last := store.heartbeats[len(store.heartbeats)-1]
		store.hbMu.Unlock()
		if !last.expiry.After(clock.Now()) {
			t.Fatalf("renewal %d: lease expiry %v is not after now %v (the lease would have lapsed)", i, last.expiry, clock.Now())
		}
	}
	close(service.release)
	if err := <-done; err != nil {
		t.Fatalf("tick() error = %v", err)
	}
}

func TestSchedulerCancelsTheSweepWhenTheLeaseIsLost(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	store := &recordingLeaseStore{}
	service := newBlockingSweepService()
	scheduler := NewScheduler(store, service, "tenant-a", "node-a", time.Minute, 30*time.Second)
	scheduler.now, scheduler.newTicker = clock.Now, clock.ticker

	done := make(chan error, 1)
	go func() {
		_, err := scheduler.tick(context.Background())
		done <- err
	}()
	<-service.started
	store.hbMu.Lock()
	store.err = ports.ErrScanLeaseLost
	store.hbMu.Unlock()
	clock.advance(10 * time.Second)
	fireTick(t, clock, 2, store)

	select {
	case <-service.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the sweep was not cancelled after the lease was lost")
	}
	if err := <-done; err != nil {
		t.Fatalf("tick() error = %v, want nil so the scheduler keeps running after losing a lease", err)
	}
}

func TestSchedulerCancelsTheSweepWhenRenewalFailsForAFullTTL(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	store := &recordingLeaseStore{}
	service := newBlockingSweepService()
	scheduler := NewScheduler(store, service, "tenant-a", "node-a", time.Minute, 30*time.Second)
	scheduler.now, scheduler.newTicker = clock.Now, clock.ticker

	done := make(chan error, 1)
	go func() {
		_, err := scheduler.tick(context.Background())
		done <- err
	}()
	<-service.started
	store.hbMu.Lock()
	store.err = context.DeadlineExceeded
	store.hbMu.Unlock()

	// A transient failure shorter than the TTL keeps the sweep alive.
	clock.advance(10 * time.Second)
	fireTick(t, clock, 2, store)
	select {
	case <-service.cancelled:
		t.Fatal("the sweep was cancelled after a transient renewal failure inside the TTL")
	case <-time.After(20 * time.Millisecond):
	}
	// Past a full TTL without a successful renewal the lease has lapsed.
	clock.advance(25 * time.Second)
	fireTick(t, clock, 3, store)
	select {
	case <-service.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the sweep kept running after the lease lapsed")
	}
	<-done
}
