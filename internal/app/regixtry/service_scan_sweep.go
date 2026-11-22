package regixtry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

const (
	// sweepCatalogPageSize and sweepTagPageSize bound each metadata read of
	// the sweep's enumeration (real pagination: the loops run until the
	// catalog / tag list is exhausted).
	sweepCatalogPageSize = 200
	sweepTagPageSize     = 500
	// sweepTouchBatch is the most last_checked_at rows one write
	// transaction updates: the sqlite write queue is a FIFO lock shared with
	// pushes, so each transaction stays short.
	sweepTouchBatch = 200
	// sweepRecordTimeout bounds the final summary write, which runs even
	// when the sweep's own context was cancelled.
	sweepRecordTimeout = 5 * time.Second
	sweepErrorMaxLen   = 500
)

// sweepJob is one digest that needs a scan: its effective settings are the
// repository's (global plus Trivy override) resolved once by the enumerator.
type sweepJob struct {
	repository string
	reference  string
	digest     string
	settings   ports.ScanSettings
}

// sweepCounters are the sweep summary counters; the enumerator and the
// scan workers update them concurrently.
type sweepCounters struct {
	visited atomic.Int64
	skipped atomic.Int64
	queued  atomic.Int64
	errors  atomic.Int64
}

// sweep holds the state of one scheduled sweep.
type sweep struct {
	s        *Service
	tenant   string
	settings ports.ScanSettings
	// dbFingerprint is the vulnerability DB part of every digest's current
	// fingerprint (read once per sweep, after the optional refresh).
	dbVersion   int
	dbUpdatedAt *time.Time
	hashCache   map[string]settingsHashResult
	touch       []string
	counters    sweepCounters
	jobs        chan sweepJob
	// refreshFailed is true when this sweep attempted the DB refresh and it
	// failed.
	refreshFailed bool
}

type settingsHashResult struct {
	hash string
	ok   bool
}

// RunScheduledScans is the periodic sweep. It refreshes the Trivy
// vulnerability DB once (unless turned off), then visits EVERY tagged digest
// of every repository (catalog and tags paginated): a digest whose latest
// completed result already carries the current fingerprint is not scanned
// (only its last_checked_at is refreshed, in batches); a new or changed one
// is scanned by a worker pool bounded by MaxConcurrency. Untagged manifests
// are not scanned. The sweep returns once its scans have finished.
func (s *Service) RunScheduledScans(ctx context.Context) error {
	settings, err := s.resolveManagedScanSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.Enabled || !settings.ScheduleEnabled {
		return nil
	}
	if _, err := s.scanTarget(settings, "sweep/placeholder", "sha256:placeholder"); err != nil {
		return err
	}
	tenant := s.tenant(ctx)
	started := s.now()
	summary := ports.ScanSweepSummary{StartedAt: &started}
	var previous ports.ScanSweepSummary
	if state, err := s.metadata.GetScanSchedulerState(ctx, tenant); err == nil {
		previous = state.LastSweep
	} else if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		slog.Warn("scan sweep could not read the previous sweep state", "error", err.Error())
	}
	// A sweep that does not refresh keeps reporting the last refresh outcome.
	summary.LastDBRefreshAt = previous.LastDBRefreshAt
	summary.LastDBRefreshError = previous.LastDBRefreshError

	sw := &sweep{s: s, tenant: tenant, settings: settings, hashCache: map[string]settingsHashResult{}}
	if err := sw.refreshDB(ctx, &summary); err != nil {
		return err
	}
	sw.readDBMetadata(ctx, &summary, previous.DBFingerprint)

	runErr := sw.run(ctx)

	finished := s.now()
	summary.FinishedAt = &finished
	summary.Visited = int(sw.counters.visited.Load())
	summary.SkippedUnchanged = int(sw.counters.skipped.Load())
	summary.Queued = int(sw.counters.queued.Load())
	summary.Errors = int(sw.counters.errors.Load())
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sweepRecordTimeout)
	defer cancel()
	if err := s.metadata.RecordScanSweep(recordCtx, tenant, summary); err != nil {
		slog.Warn("scan sweep summary not recorded", "error", err.Error())
	}
	slog.Info("scheduled scan sweep finished",
		"visited", summary.Visited,
		"skipped_unchanged", summary.SkippedUnchanged,
		"queued", summary.Queued,
		"errors", summary.Errors,
		"db_refresh_failed", sw.refreshFailed,
		"db_changed", summary.DBChanged,
		"cancelled", runErr != nil,
	)
	return runErr
}

// refreshDB downloads the vulnerability DB once, honoring the settings
// timeout under the sweep context. A failure is recorded and never aborts the
// sweep: it continues with the DB already in the cache.
func (sw *sweep) refreshDB(ctx context.Context, summary *ports.ScanSweepSummary) error {
	if sw.s.skipSweepDBRefresh.Load() {
		return nil
	}
	refresher, ok := sw.s.getScanRunner().(ports.ScanDBRefresher)
	if !ok {
		return nil
	}
	timeout := sw.settings.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	refreshCtx, cancel := context.WithTimeout(ctx, timeout)
	err := refresher.RefreshDB(refreshCtx, sw.settings)
	cancel()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	at := sw.s.now()
	summary.LastDBRefreshAt = &at
	summary.LastDBRefreshError = ""
	if err != nil {
		message := strings.TrimSpace(err.Error())
		if len(message) > sweepErrorMaxLen {
			message = message[:sweepErrorMaxLen]
		}
		summary.LastDBRefreshError = message
		sw.refreshFailed = true
		slog.Warn("scan sweep vulnerability DB refresh failed, continuing with the existing DB", "error", message)
	}
	return nil
}

// readDBMetadata reads the DB metadata once per sweep and compares it with
// the previous sweep's fingerprint.
func (sw *sweep) readDBMetadata(ctx context.Context, summary *ports.ScanSweepSummary, previousFingerprint string) {
	base := sw.s.currentScanFingerprint(ctx, sw.settings)
	if base.DBUpdatedAt != nil {
		sw.dbVersion = base.DBVersion
		sw.dbUpdatedAt = base.DBUpdatedAt
		summary.DBFingerprint = fmt.Sprintf("v%d@%s", base.DBVersion, base.DBUpdatedAt.UTC().Format(time.RFC3339))
	}
	summary.DBChanged = summary.DBFingerprint != previousFingerprint
}

// fingerprintFor is the current fingerprint of a scan with the given
// EFFECTIVE settings (a per-repository override changes the settings hash).
func (sw *sweep) fingerprintFor(settings ports.ScanSettings) scanFingerprint {
	key := strings.TrimSpace(settings.IgnoreFilePath) + "\x00" + strings.TrimSpace(settings.IgnorePolicyPath)
	cached, found := sw.hashCache[key]
	if !found {
		cached.hash, cached.ok = scanSettingsHash(settings)
		sw.hashCache[key] = cached
	}
	return scanFingerprint{
		TrivyVersion: strings.TrimSpace(settings.TrivyVersion),
		DBVersion:    sw.dbVersion,
		DBUpdatedAt:  sw.dbUpdatedAt,
		SettingsHash: cached.hash,
		Unknown:      !cached.ok || sw.dbUpdatedAt == nil,
	}
}

// run enumerates digests on the calling goroutine and feeds the ones that
// need a scan to a worker pool of MaxConcurrency workers.
func (sw *sweep) run(ctx context.Context) error {
	workers := sw.settings.MaxConcurrency
	if workers <= 0 {
		workers = 1
	}
	sw.jobs = make(chan sweepJob, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range sw.jobs {
				if ctx.Err() != nil {
					continue
				}
				sw.scan(ctx, job)
			}
		}()
	}
	enumerateErr := sw.enumerate(ctx)
	close(sw.jobs)
	wg.Wait()
	sw.flushTouches(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return enumerateErr
}

func (sw *sweep) enumerate(ctx context.Context) error {
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		repositories, err := sw.s.metadata.Catalog(ctx, sw.tenant, sweepCatalogPageSize, after)
		if err != nil {
			return err
		}
		for _, repository := range repositories {
			if err := ctx.Err(); err != nil {
				return err
			}
			sw.visitRepository(ctx, repository)
		}
		if len(repositories) < sweepCatalogPageSize {
			return nil
		}
		after = repositories[len(repositories)-1].String()
	}
}

func (sw *sweep) visitRepository(ctx context.Context, repository domain.RepositoryRef) {
	name := repository.String()
	settings, err := sw.s.applyRepositoryOverride(ctx, sw.tenant, name, trivyFeatureName, sw.settings)
	if err != nil {
		sw.fail("repository override", name, err)
		return
	}
	if !settings.Enabled {
		// A disabling override is a normal skip, not an error.
		return
	}
	fingerprint := sw.fingerprintFor(settings)
	seen := map[string]struct{}{}
	after := ""
	for {
		page, err := sw.s.metadata.ListTaggedDigests(ctx, sw.tenant, repository, sweepTagPageSize, after)
		if err != nil {
			sw.fail("tag listing", name, err)
			return
		}
		for _, tagged := range page {
			if ctx.Err() != nil {
				return
			}
			if _, duplicate := seen[tagged.Digest]; duplicate {
				continue
			}
			seen[tagged.Digest] = struct{}{}
			sw.counters.visited.Add(1)
			sw.visitDigest(ctx, name, tagged, settings, fingerprint)
		}
		if len(page) < sweepTagPageSize {
			return
		}
		after = page[len(page)-1].Tag
	}
}

func (sw *sweep) visitDigest(ctx context.Context, repository string, tagged ports.TaggedDigest, settings ports.ScanSettings, fingerprint scanFingerprint) {
	var latest *ports.ScanRun
	run, err := sw.s.metadata.GetLatestCompletedScanRunByDigest(ctx, sw.tenant, repository, tagged.Digest)
	switch {
	case err == nil:
		latest = &run
	case domain.IsCode(err, domain.ErrorCodeNotFound):
	default:
		sw.fail("latest result lookup", repository+"@"+tagged.Digest, err)
		return
	}
	if scan, _ := needsScan(latest, fingerprint, false); !scan {
		sw.counters.skipped.Add(1)
		sw.touch = append(sw.touch, latest.ID)
		if len(sw.touch) >= sweepTouchBatch {
			sw.flushTouches(ctx)
		}
		return
	}
	select {
	case sw.jobs <- sweepJob{repository: repository, reference: tagged.Tag, digest: tagged.Digest, settings: settings}:
	case <-ctx.Done():
	}
}

// flushTouches writes the pending last_checked_at updates in one short
// transaction (the batch is capped at sweepTouchBatch by the caller).
func (sw *sweep) flushTouches(ctx context.Context) {
	if len(sw.touch) == 0 || ctx.Err() != nil {
		return
	}
	ids := sw.touch
	sw.touch = nil
	if err := sw.s.metadata.TouchScanRunsChecked(ctx, sw.tenant, ids, sw.s.now()); err != nil {
		sw.fail("last_checked_at update", fmt.Sprintf("%d runs", len(ids)), err)
	}
}

func (sw *sweep) fail(what string, subject string, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	sw.counters.errors.Add(1)
	slog.Warn("scan sweep step failed", "step", what, "subject", subject, "error", err.Error())
}

// scan queues and runs one scan synchronously inside a worker, so the pool
// size bounds the sweep's concurrency and the sweep outlives its scans. A
// cancelled sweep never leaves its run in queued/running state.
func (sw *sweep) scan(ctx context.Context, job sweepJob) {
	run, existed, err := sw.s.dedupAndQueueScanRun(ctx, sw.tenant, job.repository, job.reference, job.digest, ports.ScanTriggerScheduled, job.settings.Timeout)
	if err != nil {
		sw.fail("queue scan", job.repository+"@"+job.digest, err)
		return
	}
	if existed {
		return
	}
	sw.counters.queued.Add(1)
	sw.s.executeScanRun(ctx, sw.tenant, run, job.settings)
	if ctx.Err() != nil {
		sw.failCancelledRun(ctx, run.ID)
	}
}

// failCancelledRun closes a run the cancelled sweep left outstanding, using a
// context that survives the cancellation.
func (sw *sweep) failCancelledRun(ctx context.Context, runID string) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sweepRecordTimeout)
	defer cancel()
	run, err := sw.s.metadata.GetScanRun(closeCtx, sw.tenant, runID)
	if err != nil || (run.Status != ports.ScanRunStatusQueued && run.Status != ports.ScanRunStatusRunning) {
		return
	}
	finished := sw.s.now()
	run.Status = ports.ScanRunStatusFailed
	run.FinishedAt = &finished
	run.UpdatedAt = finished
	run.Error = "scan sweep cancelled"
	if err := sw.s.metadata.UpsertScanRun(closeCtx, sw.tenant, run); err != nil {
		slog.Warn("cancelled scan run not closed", "run", runID, "error", err.Error())
	}
}
