package scanning

import (
	"context"
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
}

func NewScheduler(store leaseStore, service schedulerService, tenant string, owner string, interval time.Duration, leaseTTL time.Duration) *Scheduler {
	if interval <= 0 {
		interval = time.Second
	}
	if leaseTTL <= 0 {
		leaseTTL = 5 * time.Second
	}
	return &Scheduler{store: store, service: service, tenant: tenant, owner: owner, interval: interval, leaseTTL: leaseTTL}
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
	now := time.Now().UTC()
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
	return interval, s.service.RunScheduledScans(ctx)
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
