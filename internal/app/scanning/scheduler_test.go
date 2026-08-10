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

type fakeLeaseStore struct {
	mu    sync.Mutex
	state ports.ScanSchedulerState
	owner string
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
