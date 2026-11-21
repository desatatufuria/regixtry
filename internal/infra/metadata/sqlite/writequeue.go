package sqlite

import (
	"container/list"
	"context"
	"database/sql"
	"sync"
)

// fifoLock is a mutual-exclusion lock that grants ownership strictly in
// arrival order and lets a waiter give up when its context is cancelled.
// sync.Mutex is neither FIFO nor cancellable, and SQLite's busy handler polls
// with sleeps and has no queue, so writers must queue here instead.
type fifoLock struct {
	mu      sync.Mutex
	held    bool
	waiters list.List // of chan struct{}
}

func (l *fifoLock) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	l.mu.Lock()
	if !l.held && l.waiters.Len() == 0 {
		l.held = true
		l.mu.Unlock()
		return nil
	}
	granted := make(chan struct{})
	element := l.waiters.PushBack(granted)
	l.mu.Unlock()

	select {
	case <-granted:
		return nil
	case <-ctx.Done():
		l.abandon(element, granted)
		return ctx.Err()
	}
}

// abandon withdraws a waiter whose context ended. Ownership is decided by the
// granted channel, never by list.Remove (which returns the value either way).
func (l *fifoLock) abandon(element *list.Element, granted chan struct{}) {
	l.mu.Lock()
	select {
	case <-granted:
		l.mu.Unlock()
		// Ownership was handed to us just before cancellation: pass it on.
		l.release()
	default:
		l.waiters.Remove(element)
		l.mu.Unlock()
	}
}

func (l *fifoLock) release() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if front := l.waiters.Front(); front != nil {
		// Ownership transfers directly to the oldest waiter; held stays true.
		close(l.waiters.Remove(front).(chan struct{}))
		return
	}
	l.held = false
}

// writeTx is a write transaction that owns the store's write lock until its
// Commit or Rollback runs. Both release the lock exactly once, so a deferred
// Rollback after Commit is safe.
type writeTx struct {
	*sql.Tx
	release func()
	once    sync.Once
}

func (t *writeTx) Commit() error {
	defer t.once.Do(t.release)
	return t.Tx.Commit()
}

func (t *writeTx) Rollback() error {
	defer t.once.Do(t.release)
	return t.Tx.Rollback()
}

// beginWrite serializes write transactions in arrival order. Never call it (or
// touch s.db for a write) while another writeTx is open in the same goroutine:
// that would deadlock on the write lock.
//
// Single-statement writes on s.db (SaveUpload, DeleteUpload,
// PruneExpiredGCReports, DeleteTag, the settings/override/runtime-state
// upserts, UpsertScanRun, UpsertScanSchedulerState) deliberately skip the
// queue: one statement holds the SQLite lock only briefly. The queue is for
// multi-statement transactions.
func (s *Store) beginWrite(ctx context.Context) (*writeTx, error) {
	if err := s.writeLock.acquire(ctx); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.writeLock.release()
		return nil, err
	}

	return &writeTx{Tx: tx, release: s.writeLock.release}, nil
}
