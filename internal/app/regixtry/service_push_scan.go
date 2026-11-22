package regixtry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

const (
	// DefaultPushScanMaxPending bounds how many push-triggered scans (queued
	// or running, Trivy and secret legs included) may be in flight at once
	// across the whole service.
	DefaultPushScanMaxPending = 64
	// DefaultPushScanMaxPerRepository bounds the same quantity for one
	// repository, so a single noisy writer cannot consume every slot.
	DefaultPushScanMaxPerRepository = 8
	// DefaultPushScanMaxPerPusher bounds the same quantity for one pushing
	// principal across all repositories (a quarter of the global default).
	DefaultPushScanMaxPerPusher = 16

	pushScanSkippedError = "skipped: push scan queue full"

	// maxStuckSecretLegs bounds goroutines leaked by secret-scan runners
	// that ignore context cancellation. At the limit no new push scan is
	// admitted, so leaked goroutines can never exceed it.
	maxStuckSecretLegs = 16
)

// pushScanLimiter admits push-triggered scans up to a global and a
// per-repository cap. A slot is held from admission until the scan (both
// legs) has finished, so goroutines, polling waiters and queued rows are all
// bounded by the caps.
type pushScanLimiter struct {
	mu         sync.Mutex
	maxPending int
	maxPerRepo int
	// maxPerPusher bounds one pushing principal across all of its
	// repositories, so many repositories cannot starve everyone else.
	maxPerPusher int
	total        int
	perRepo      map[string]int
	perPusher    map[string]int
}

func newPushScanLimiter() *pushScanLimiter {
	return &pushScanLimiter{
		maxPending:   DefaultPushScanMaxPending,
		maxPerRepo:   DefaultPushScanMaxPerRepository,
		maxPerPusher: DefaultPushScanMaxPerPusher,
		perRepo:      make(map[string]int),
		perPusher:    make(map[string]int),
	}
}

func (l *pushScanLimiter) setLimits(maxPending int, maxPerRepo int, maxPerPusher int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if maxPending <= 0 {
		maxPending = DefaultPushScanMaxPending
	}
	if maxPerRepo <= 0 {
		maxPerRepo = DefaultPushScanMaxPerRepository
	}
	if maxPerPusher <= 0 {
		maxPerPusher = DefaultPushScanMaxPerPusher
	}
	l.maxPerPusher = maxPerPusher
	l.maxPending = maxPending
	l.maxPerRepo = maxPerRepo
}

// acquire reserves a slot and returns its release func, or false when the
// service-wide or per-repository cap is reached.
func (l *pushScanLimiter) acquire(tenant string, repository string, pusher string) (func(), bool) {
	key := tenant + "\x00" + repository
	pusherKey := tenant + "\x00" + pusher
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total >= l.maxPending || l.perRepo[key] >= l.maxPerRepo || l.perPusher[pusherKey] >= l.maxPerPusher {
		return nil, false
	}
	l.total++
	l.perRepo[key]++
	l.perPusher[pusherKey]++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.total--
			if l.perRepo[key]--; l.perRepo[key] <= 0 {
				delete(l.perRepo, key)
			}
			if l.perPusher[pusherKey]--; l.perPusher[pusherKey] <= 0 {
				delete(l.perPusher, pusherKey)
			}
		})
	}, true
}

// SetPushScanLimits configures the push-triggered scan admission caps. A
// value <= 0 keeps the default (DefaultPushScanMaxPending,
// DefaultPushScanMaxPerRepository and DefaultPushScanMaxPerPusher).
func (s *Service) SetPushScanLimits(maxPending int, maxPerRepository int, maxPerPusher int) {
	s.pushScans.setLimits(maxPending, maxPerRepository, maxPerPusher)
}

// admitPushScan reserves a push-scan slot unless the caps are reached or too
// many secret-scan legs are stuck.
func (s *Service) admitPushScan(tenant string, repository string, pusher string) (func(), bool) {
	if s.stuckSecretLegs.Load() >= maxStuckSecretLegs {
		return nil, false
	}
	return s.pushScans.acquire(tenant, repository, pusher)
}

// startPushScan is PublishManifest's fire-and-forget entry: it admits the
// push against the caps before spawning anything. A push over the cap never
// fails; it is recorded as a visible skipped scan instead.
func (s *Service) startPushScan(tenant string, repository string, reference string, digest string, pusher string) {
	release, ok := s.admitPushScan(tenant, repository, pusher)
	if !ok {
		s.recordPushScanSkipped(context.Background(), tenant, repository, reference, digest)
		return
	}
	s.backgroundWork.Add(1)
	go func() {
		defer s.backgroundWork.Done()
		s.runPushScan(context.Background(), tenant, repository, reference, digest, release)
	}()
}

// recordPushScanSkipped makes a dropped push-triggered scan visible without
// growing storage: one "skipped" row per repository, overwritten by the
// newest dropped digest, and only for a digest with no real run. The store
// ranks skipped rows below every real run, so they never win "latest" and
// never reach the pull policy. It only writes when push scanning would have
// been enabled for the repository.
func (s *Service) recordPushScanSkipped(ctx context.Context, tenant string, repository string, reference string, digest string) {
	settings, err := s.resolveManagedScanSettingsForTenant(ctx, tenant)
	if err != nil {
		return
	}
	settings, err = s.applyRepositoryOverride(ctx, tenant, repository, trivyFeatureName, settings)
	if err != nil || !settings.Enabled {
		return
	}
	// A digest that already has any real run (completed, failed or in
	// flight) needs no notice: its own result is the truth.
	if existing, err := s.metadata.GetLatestScanRunByDigest(ctx, tenant, repository, digest); err == nil && existing.Status != ports.ScanRunStatusSkipped {
		return
	} else if err != nil && !domain.IsCode(err, domain.ErrorCodeNotFound) {
		return
	}
	sum := sha256.Sum256([]byte(tenant + "\x00" + repository))
	now := s.now()
	run := ports.ScanRun{
		ID:           "push-skipped-" + hex.EncodeToString(sum[:8]),
		Repository:   repository,
		RequestedRef: strings.TrimSpace(reference),
		Digest:       digest,
		Status:       ports.ScanRunStatusSkipped,
		Trigger:      ports.ScanTriggerPush,
		Error:        pushScanSkippedError,
		CreatedAt:    now,
		UpdatedAt:    now,
		FinishedAt:   &now,
	}
	s.persistAsyncScanRun(ctx, tenant, run)
}
