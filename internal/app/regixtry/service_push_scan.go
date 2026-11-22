package regixtry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"

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

	pushScanSkippedError = "skipped: push scan queue full"
)

// pushScanLimiter admits push-triggered scans up to a global and a
// per-repository cap. A slot is held from admission until the scan (both
// legs) has finished, so goroutines, polling waiters and queued rows are all
// bounded by the caps.
type pushScanLimiter struct {
	mu         sync.Mutex
	maxPending int
	maxPerRepo int
	total      int
	perRepo    map[string]int
}

func newPushScanLimiter() *pushScanLimiter {
	return &pushScanLimiter{
		maxPending: DefaultPushScanMaxPending,
		maxPerRepo: DefaultPushScanMaxPerRepository,
		perRepo:    make(map[string]int),
	}
}

func (l *pushScanLimiter) setLimits(maxPending int, maxPerRepo int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if maxPending <= 0 {
		maxPending = DefaultPushScanMaxPending
	}
	if maxPerRepo <= 0 {
		maxPerRepo = DefaultPushScanMaxPerRepository
	}
	l.maxPending = maxPending
	l.maxPerRepo = maxPerRepo
}

// acquire reserves a slot and returns its release func, or false when the
// service-wide or per-repository cap is reached.
func (l *pushScanLimiter) acquire(tenant string, repository string) (func(), bool) {
	key := tenant + "\x00" + repository
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total >= l.maxPending || l.perRepo[key] >= l.maxPerRepo {
		return nil, false
	}
	l.total++
	l.perRepo[key]++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.total--
			if l.perRepo[key]--; l.perRepo[key] <= 0 {
				delete(l.perRepo, key)
			}
		})
	}, true
}

// SetPushScanLimits configures the push-triggered scan admission caps. A
// value <= 0 keeps the default (DefaultPushScanMaxPending and
// DefaultPushScanMaxPerRepository).
func (s *Service) SetPushScanLimits(maxPending int, maxPerRepository int) {
	s.pushScans.setLimits(maxPending, maxPerRepository)
}

// startPushScan is PublishManifest's fire-and-forget entry: it admits the
// push against the caps before spawning anything. A push over the cap never
// fails; it is recorded as a visible skipped scan instead.
func (s *Service) startPushScan(tenant string, repository string, reference string, digest string) {
	release, ok := s.pushScans.acquire(tenant, repository)
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
// growing storage: one failed "skipped: ..." row per repository, overwritten
// by the newest dropped digest. It only writes when push scanning would have
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
	sum := sha256.Sum256([]byte(repository))
	now := s.now()
	run := ports.ScanRun{
		ID:           "push-skipped-" + hex.EncodeToString(sum[:8]),
		Repository:   repository,
		RequestedRef: strings.TrimSpace(reference),
		Digest:       digest,
		Status:       ports.ScanRunStatusFailed,
		Trigger:      ports.ScanTriggerPush,
		Error:        pushScanSkippedError,
		CreatedAt:    now,
		UpdatedAt:    now,
		FinishedAt:   &now,
	}
	s.persistAsyncScanRun(ctx, tenant, run)
}
