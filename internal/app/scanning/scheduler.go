package scanning

import (
	"context"
	"errors"
	"time"

	"regixtry/internal/ports"
)

type leaseStore interface {
	TryAcquireScanSchedulerLease(ctx context.Context, tenant string, owner string, now time.Time, leaseTTL time.Duration) (bool, ports.ScanSchedulerState, error)
	HeartbeatScanScheduler(ctx context.Context, tenant string, owner string, now time.Time, leaseTTL time.Duration) error
}

type schedulerService interface {
	GetScanSettings(ctx context.Context) (ports.ScanSettings, error)
	RunScheduledScans(ctx context.Context) error
}

type Scheduler struct {
	store    leaseStore
	service  schedulerService
	tenant   string
	owner    string
	interval time.Duration
	leaseTTL time.Duration

	// now and newTicker are the clock the lease logic reads; tests replace
	// them with a fake clock.
	now       func() time.Time
	newTicker func(d time.Duration) (<-chan time.Time, func())
}

func NewScheduler(store leaseStore, service schedulerService, tenant string, owner string, interval time.Duration, leaseTTL time.Duration) *Scheduler {
	if interval <= 0 {
		interval = time.Second
	}
	if leaseTTL <= 0 {
		leaseTTL = 5 * time.Second
	}
	return &Scheduler{store: store, service: service, tenant: tenant, owner: owner, interval: interval, leaseTTL: leaseTTL, now: time.Now, newTicker: realTicker}
}

func realTicker(d time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(d)
	return ticker.C, ticker.Stop
}

func (s *Scheduler) Run(ctx context.Context) error {
	if s == nil || s.store == nil || s.service == nil {
		return nil
	}
	for {
		interval, err := s.tick(ctx)
		if err != nil {
			return err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) (time.Duration, error) {
	settings, err := s.service.GetScanSettings(ctx)
	if err != nil {
		return s.interval, nil
	}
	interval := s.nextInterval(settings.Interval)
	if !settings.Enabled || !settings.ScheduleEnabled {
		return interval, nil
	}
	now := s.now().UTC()
	acquired, _, err := s.store.TryAcquireScanSchedulerLease(ctx, s.tenant, s.owner, now, s.leaseTTL)
	if err != nil {
		return interval, err
	}
	if !acquired {
		return interval, nil
	}
	if err := s.store.HeartbeatScanScheduler(ctx, s.tenant, s.owner, now, s.leaseTTL); err != nil {
		return interval, err
	}
	return interval, s.runWithLease(ctx, now)
}

// runWithLease runs one sweep while renewing the lease in the background: a
// sweep that outlasts the lease TTL would otherwise let another node acquire
// the lease and sweep concurrently. The lease is renewed every leaseTTL/3;
// when it is lost to another owner, or cannot be renewed for a full TTL, the
// sweep is cancelled (another node may already be running) and this tick ends
// without error so the scheduler keeps running.
func (s *Scheduler) runWithLease(ctx context.Context, acquiredAt time.Time) error {
	sweepCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var leaseLost bool
	renewed := make(chan struct{})
	go func() {
		defer close(renewed)
		leaseLost = s.renewLease(sweepCtx, cancel, acquiredAt)
	}()
	err := s.service.RunScheduledScans(sweepCtx)
	cancel()
	<-renewed
	if leaseLost && ctx.Err() == nil {
		return nil
	}
	return err
}

// renewLease heartbeats until ctx ends. It reports true when it cancelled the
// sweep because the lease was lost.
func (s *Scheduler) renewLease(ctx context.Context, cancelSweep context.CancelFunc, acquiredAt time.Time) bool {
	period := s.leaseTTL / 3
	if period <= 0 {
		period = time.Millisecond
	}
	ticks, stop := s.newTicker(period)
	defer stop()
	lastRenewed := acquiredAt
	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticks:
		}
		now := s.now().UTC()
		err := s.store.HeartbeatScanScheduler(ctx, s.tenant, s.owner, now, s.leaseTTL)
		switch {
		case err == nil:
			lastRenewed = now
		case ctx.Err() != nil:
			return false
		case errors.Is(err, ports.ErrScanLeaseLost), now.Sub(lastRenewed) >= s.leaseTTL:
			cancelSweep()
			return true
		}
	}
}

func (s *Scheduler) nextInterval(configured time.Duration) time.Duration {
	if configured > 0 {
		return configured
	}
	if s.interval > 0 {
		return s.interval
	}
	return time.Second
}
