package auth

import (
	"context"
	"log"
	"sync"
	"time"

	"regixtry/internal/ports"
)

const (
	// LoginFailureWindow is the width of one aggregation bucket.
	LoginFailureWindow = 5 * time.Minute
	// LoginFailureFlushInterval is how often the aggregator writes to the
	// store. It bounds the write rate to Postgres whatever the failure rate.
	LoginFailureFlushInterval = 60 * time.Second
	// LoginFailureRetention is how long aggregated rows are kept.
	LoginFailureRetention = 30 * 24 * time.Hour
	// LoginFailureShutdownTimeout bounds the final flush at shutdown.
	LoginFailureShutdownTimeout = 5 * time.Second
	// OverflowAccount labels the single key per window that absorbs failures
	// once the live key cap is reached. It is also used for the endpoint,
	// reason and ip of that key, so it can never collide with a real key
	// (usernames cannot start with an underscore).
	OverflowAccount = "_overflow"

	defaultLoginFailureMaxKeys    = 10_000
	defaultLoginFailureMaxPending = 10_000
	defaultLoginFailureChunk      = 500
	loginFailurePurgeBatch        = 1_000
	loginFailureFlushTimeout      = 10 * time.Second
	loginFailureLogEvery          = time.Minute

	loginFailureMaxFieldBytes = 100
)

// LoginFailureOptions tunes a LoginFailureAggregator; zero values take the
// defaults above.
type LoginFailureOptions struct {
	// MaxKeys caps the live (not yet flushed) keys.
	MaxKeys int
	// MaxPending caps the records kept for retry after a failed flush; the
	// oldest are dropped (and counted) beyond it.
	MaxPending int
	// FlushChunk is how many records one store call writes.
	FlushChunk    int
	FlushInterval time.Duration
	Logger        *log.Logger
	// Now is a clock seam (retention cutoff and log rate limit).
	Now func() time.Time
}

// LoginFailureStats reports the aggregator's bounded state.
type LoginFailureStats struct {
	Live       int
	Pending    int
	Dropped    uint64
	Overflowed uint64
}

type loginFailureKey struct {
	window                        time.Time
	endpoint, reason, account, ip string
}

type loginFailureCell struct {
	count               int64
	firstSeen, lastSeen time.Time
}

// LoginFailureAggregator folds every rejected credential into a bounded
// in-memory counter keyed by (5-minute window, endpoint, reason, account, ip)
// and flushes it to a store on an interval. Recording never does I/O, never
// blocks on the store and never fails: a store outage only fills a bounded
// retry buffer.
type LoginFailureAggregator struct {
	store      ports.LoginFailureStore
	maxKeys    int
	maxPending int
	chunk      int
	interval   time.Duration
	logger     *log.Logger
	now        func() time.Time

	mu         sync.Mutex
	live       map[loginFailureKey]*loginFailureCell
	pending    []ports.LoginFailureRecord
	dropped    uint64
	overflowed uint64

	flushMu sync.Mutex
	lastLog time.Time
}

// NewLoginFailureAggregator builds an aggregator that flushes to store.
func NewLoginFailureAggregator(store ports.LoginFailureStore, options LoginFailureOptions) *LoginFailureAggregator {
	a := &LoginFailureAggregator{
		store:      store,
		maxKeys:    options.MaxKeys,
		maxPending: options.MaxPending,
		chunk:      options.FlushChunk,
		interval:   options.FlushInterval,
		logger:     options.Logger,
		now:        options.Now,
		live:       make(map[loginFailureKey]*loginFailureCell),
	}
	if a.maxKeys <= 0 {
		a.maxKeys = defaultLoginFailureMaxKeys
	}
	if a.maxPending <= 0 {
		a.maxPending = defaultLoginFailureMaxPending
	}
	if a.chunk <= 0 {
		a.chunk = defaultLoginFailureChunk
	}
	if a.interval <= 0 {
		a.interval = LoginFailureFlushInterval
	}
	if a.logger == nil {
		a.logger = log.Default()
	}
	if a.now == nil {
		a.now = func() time.Time { return time.Now().UTC() }
	}
	return a
}

// RecordLoginFailure adds one failure. It holds a mutex for a map update only.
func (a *LoginFailureAggregator) RecordLoginFailure(o ports.LoginFailureObservation) {
	at := o.At.UTC()
	key := loginFailureKey{
		window:   at.Truncate(LoginFailureWindow),
		endpoint: capBytes(o.Endpoint),
		reason:   capBytes(o.Reason),
		account:  capBytes(o.Account),
		ip:       capBytes(o.IP),
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	cell, ok := a.live[key]
	if !ok && len(a.live) >= a.maxKeys {
		overflow := loginFailureKey{window: key.window, endpoint: OverflowAccount, reason: OverflowAccount, account: OverflowAccount, ip: OverflowAccount}
		a.overflowed++
		if cell, ok = a.live[overflow]; !ok {
			cell = &loginFailureCell{firstSeen: at, lastSeen: at}
			a.live[overflow] = cell
		}
		cell.add(at)
		return
	}
	if !ok {
		cell = &loginFailureCell{firstSeen: at, lastSeen: at}
		a.live[key] = cell
	}
	cell.add(at)
}

func (c *loginFailureCell) add(at time.Time) {
	c.count++
	if at.Before(c.firstSeen) {
		c.firstSeen = at
	}
	if at.After(c.lastSeen) {
		c.lastSeen = at
	}
}

// capBytes bounds an attacker-influenced field, cutting on a rune boundary.
func capBytes(value string) string {
	if len(value) <= loginFailureMaxFieldBytes {
		return value
	}
	cut := loginFailureMaxFieldBytes
	for cut > 0 && value[cut]&0xC0 == 0x80 {
		cut--
	}
	return value[:cut]
}

// Stats returns the current bounded state.
func (a *LoginFailureAggregator) Stats() LoginFailureStats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return LoginFailureStats{Live: len(a.live), Pending: len(a.pending), Dropped: a.dropped, Overflowed: a.overflowed}
}

// Flush writes the live counters (and any records left over from a failed
// flush) to the store, then purges rows past the retention. On a failure the
// records not yet written stay pending, bounded by MaxPending. Concurrent
// flushes are serialized; recording is never held up by one.
func (a *LoginFailureAggregator) Flush(ctx context.Context) error {
	a.flushMu.Lock()
	defer a.flushMu.Unlock()

	batch := a.take()
	if len(batch) == 0 {
		return nil
	}
	for start := 0; start < len(batch); start += a.chunk {
		end := min(start+a.chunk, len(batch))
		if err := a.store.AddLoginFailures(ctx, batch[start:end]); err != nil {
			a.requeue(batch[start:])
			a.logFlushFailure(err, len(batch)-start)
			return err
		}
	}

	if _, err := a.store.PurgeLoginFailures(ctx, a.now().Add(-LoginFailureRetention), loginFailurePurgeBatch); err != nil {
		a.logFlushFailure(err, 0)
	}
	return nil
}

// take moves the pending records and the live counters into one batch.
func (a *LoginFailureAggregator) take() []ports.LoginFailureRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	batch := a.pending
	a.pending = nil
	for key, cell := range a.live {
		batch = append(batch, ports.LoginFailureRecord{
			WindowStart: key.window, Endpoint: key.endpoint, Reason: key.reason, Account: key.account, IP: key.ip,
			Count: cell.count, FirstSeen: cell.firstSeen, LastSeen: cell.lastSeen,
		})
	}
	a.live = make(map[loginFailureKey]*loginFailureCell)
	return batch
}

// requeue keeps unwritten records for the next flush, dropping the oldest
// beyond the cap.
func (a *LoginFailureAggregator) requeue(records []ports.LoginFailureRecord) {
	a.mu.Lock()
	defer a.mu.Unlock()
	merged := append(append([]ports.LoginFailureRecord(nil), records...), a.pending...)
	if over := len(merged) - a.maxPending; over > 0 {
		a.dropped += uint64(over)
		merged = merged[over:]
	}
	a.pending = merged
}

// logFlushFailure writes at most one line per minute and never a name, IP or
// any other field of a record.
func (a *LoginFailureAggregator) logFlushFailure(err error, unwritten int) {
	now := a.now()
	if !a.lastLog.IsZero() && now.Sub(a.lastLog) < loginFailureLogEvery {
		return
	}
	a.lastLog = now
	stats := a.Stats()
	a.logger.Printf("login failures not flushed: %v unwritten=%d pending=%d dropped=%d", err, unwritten, stats.Pending, stats.Dropped)
}

// Run flushes every interval until ctx is done. It does not flush at the end:
// call Shutdown after Run returns.
func (a *LoginFailureAggregator) Run(ctx context.Context) {
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			flushCtx, cancel := context.WithTimeout(ctx, loginFailureFlushTimeout)
			_ = a.Flush(flushCtx)
			cancel()
		}
	}
}

// Shutdown performs the final flush, bounded by timeout.
func (a *LoginFailureAggregator) Shutdown(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = LoginFailureShutdownTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return a.Flush(ctx)
}
