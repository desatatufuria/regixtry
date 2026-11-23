package regixtry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

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
	// principal across all repositories (an eighth of the global default).
	DefaultPushScanMaxPerPusher = DefaultPushScanMaxPending / 8
	// pushScanNewcomerReserveDivisor reserves 1/N of the global cap for
	// identities that hold no slot yet: once the pool is within that
	// reserve of full, only an identity with nothing in flight is admitted.
	// Several identities that each stay inside their own per-pusher cap
	// therefore cannot fill the pool and starve every other writer.
	pushScanNewcomerReserveDivisor = 4

	// maxDeferredPushScans bounds the in-memory retry queue of pushes whose
	// admission was refused; maxDeferredPerPusher bounds one identity's share
	// of it. Beyond them a refused push keeps only its skipped notice (and
	// the scheduled sweep, when enabled).
	maxDeferredPushScans = 256
	maxDeferredPerPusher = 32

	// anonymousPushScanMaxPerPusher is the baseline per-identity cap kept for
	// pushes without a principal: they all share one bucket, so the smaller
	// default would throttle legitimate anonymous registries.
	anonymousPushScanMaxPerPusher = 16
	// deferredDropLogInterval rate-limits the "retry list full" log line.
	deferredDropLogInterval = time.Minute

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

	// deferred holds pushes refused at admission, retried (in order) as
	// slots free up. drainRunning is true while one drainer goroutine runs.
	deferred     []deferredPushScan
	drainRunning bool
	// deferredDropped counts pushes not deferred because the list was full;
	// droppedSinceLog and lastDropLog drive the rate-limited log line.
	deferredDropped int
	droppedSinceLog int
	lastDropLog     time.Time
}

// noteDeferDropped counts one push that could not be deferred and reports
// whether a log line is due (at most one per deferredDropLogInterval),
// returning how many drops it covers.
func (l *pushScanLimiter) noteDeferDropped(now time.Time) (int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.deferredDropped++
	l.droppedSinceLog++
	if !l.lastDropLog.IsZero() && now.Sub(l.lastDropLog) < deferredDropLogInterval {
		return 0, false
	}
	n := l.droppedSinceLog
	l.droppedSinceLog = 0
	l.lastDropLog = now
	return n, true
}

type deferredPushScan struct {
	tenant, repository, reference, digest, pusher string
}

func (d deferredPushScan) pusherKey() string { return d.tenant + "\x00" + d.pusher }

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
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.acquireLocked(tenant, repository, pusher)
}

func (l *pushScanLimiter) acquireLocked(tenant string, repository string, pusher string) (func(), bool) {
	key := tenant + "\x00" + repository
	pusherKey := tenant + "\x00" + pusher
	maxPerPusher := l.maxPerPusher
	if pusher == "" && maxPerPusher == DefaultPushScanMaxPerPusher {
		maxPerPusher = anonymousPushScanMaxPerPusher
	}
	if l.total >= l.maxPending || l.perRepo[key] >= l.maxPerRepo || l.perPusher[pusherKey] >= maxPerPusher {
		return nil, false
	}
	// Reserve the last slice of the pool for identities with no slot yet.
	// Anonymous pushes share one bucket: nobody to protect, so no reserve.
	if pusher != "" && l.perPusher[pusherKey] > 0 && l.total >= l.maxPending-l.maxPending/pushScanNewcomerReserveDivisor {
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
	release, ok := s.pushScans.acquire(tenant, repository, pusher)
	if !ok {
		return nil, false
	}
	return s.releaseThenDrain(release), true
}

// releaseThenDrain wraps a slot release so that freeing the slot also wakes
// the deferred-scan drainer. No polling: retries are event-driven.
func (s *Service) releaseThenDrain(release func()) func() {
	return func() {
		release()
		s.kickDeferredPushScans()
	}
}

// deferPushScan remembers a push refused at admission so it is scanned as
// soon as the caps allow, even when the scheduled sweep is disabled. The
// queue is bounded (maxDeferredPushScans, maxDeferredPerPusher) and holds
// one entry per digest, so it cannot grow without limit.
func (l *pushScanLimiter) deferPushScan(d deferredPushScan) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.deferred) >= maxDeferredPushScans {
		return false
	}
	perPusher := 0
	for _, existing := range l.deferred {
		if existing.tenant == d.tenant && existing.repository == d.repository && existing.digest == d.digest {
			return true
		}
		if existing.pusherKey() == d.pusherKey() {
			perPusher++
		}
	}
	if perPusher >= maxDeferredPerPusher {
		return false
	}
	l.deferred = append(l.deferred, d)
	return true
}

// nextDeferred pops the oldest deferred push the caps admit now. When none
// is admissible it marks the drainer stopped, atomically with that check, so
// a release that follows always starts a new one.
func (l *pushScanLimiter) nextDeferred(stuck bool) (deferredPushScan, func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !stuck {
		for i, d := range l.deferred {
			if release, ok := l.acquireLocked(d.tenant, d.repository, d.pusher); ok {
				l.deferred = append(l.deferred[:i], l.deferred[i+1:]...)
				return d, release, true
			}
		}
	}
	l.drainRunning = false
	return deferredPushScan{}, nil, false
}

// deferredManifestExists reports whether the deferred digest is still
// published. A deleted manifest is dropped without a scan or a result row;
// any other lookup error keeps the entry (scanning is harmless).
func (s *Service) deferredManifestExists(d deferredPushScan) bool {
	repository, err := parseRepository(d.repository)
	if err != nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), skippedRecordTimeout)
	defer cancel()
	_, err = s.metadata.ResolveManifest(ctx, d.tenant, repository, d.digest)
	return err == nil || !domain.IsCode(err, domain.ErrorCodeNotFound)
}

// kickDeferredPushScans starts the single drainer when work is queued.
func (s *Service) kickDeferredPushScans() {
	l := s.pushScans
	l.mu.Lock()
	if len(l.deferred) == 0 || l.drainRunning {
		l.mu.Unlock()
		return
	}
	l.drainRunning = true
	l.mu.Unlock()
	s.backgroundWork.Add(1)
	go func() {
		defer s.backgroundWork.Done()
		for {
			d, release, ok := s.pushScans.nextDeferred(s.stuckSecretLegs.Load() >= maxStuckSecretLegs)
			if !ok {
				return
			}
			s.backgroundWork.Add(1)
			go func() {
				defer s.backgroundWork.Done()
				if !s.deferredManifestExists(d) {
					release() // manifest deleted while deferred: nothing to scan
					return
				}
				s.runPushScan(context.Background(), d.tenant, d.repository, d.reference, d.digest, s.releaseThenDrain(release))
			}()
		}
	}()
}

// startPushScan is PublishManifest's fire-and-forget entry: it admits the
// push against the caps before spawning anything. A push over the cap never
// fails; it is recorded as a visible skipped scan and queued for a bounded
// retry as soon as slots free up.
func (s *Service) startPushScan(tenant string, repository string, reference string, digest string, pusher string) {
	release, ok := s.admitPushScan(tenant, repository, pusher)
	if !ok {
		// In-memory only: the store work happens on the recorder's worker.
		s.noteSkippedPushScan(tenant, repository, reference, digest)
		if s.pushScans.deferPushScan(deferredPushScan{tenant, repository, reference, digest, pusher}) {
			s.kickDeferredPushScans()
		} else if n, due := s.pushScans.noteDeferDropped(s.now()); due {
			slog.Warn(fmt.Sprintf("push scan admission: deferred list full, dropped %d pushes in the last minute (sweep recovers them only when enabled)", n))
		}
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
