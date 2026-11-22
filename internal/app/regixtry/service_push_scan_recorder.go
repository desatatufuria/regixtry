package regixtry

import (
	"context"
	"sync"
	"time"
)

const (
	// maxPendingSkippedRepositories bounds the recorder's memory: beyond it,
	// notices for further repositories are dropped (and counted).
	maxPendingSkippedRepositories = 1024
	// skippedRecordTimeout bounds one notice's store work.
	skippedRecordTimeout = 10 * time.Second
)

type skippedNotice struct {
	tenant, repository, reference, digest string
}

// skippedRecorder persists "skipped" notices for dropped push scans off the
// request path. note() is in-memory only: it coalesces per tenant and
// repository (newest wins), and a single worker goroutine, started on
// demand, performs the store writes. So the number of goroutines and the
// amount of pending state are bounded regardless of how many pushes are
// dropped, and a stalled store can never delay a push response.
type skippedRecorder struct {
	mu      sync.Mutex
	pending map[string]skippedNotice
	running bool
	dropped int
}

func (r *skippedRecorder) stats() (pending int, workers int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		workers = 1
	}
	return len(r.pending), workers
}

// note queues a notice and ensures the worker runs; it never blocks on I/O.
func (s *Service) noteSkippedPushScan(tenant string, repository string, reference string, digest string) {
	r := &s.skippedPushScans
	key := tenant + "\x00" + repository
	r.mu.Lock()
	if r.pending == nil {
		r.pending = make(map[string]skippedNotice)
	}
	if _, exists := r.pending[key]; !exists && len(r.pending) >= maxPendingSkippedRepositories {
		r.dropped++
		r.mu.Unlock()
		return
	}
	r.pending[key] = skippedNotice{tenant, repository, reference, digest}
	start := !r.running
	r.running = true
	r.mu.Unlock()
	if start {
		s.backgroundWork.Add(1)
		go s.runSkippedRecorder()
	}
}

func (s *Service) runSkippedRecorder() {
	defer s.backgroundWork.Done()
	r := &s.skippedPushScans
	for {
		r.mu.Lock()
		if len(r.pending) == 0 {
			r.running = false
			r.mu.Unlock()
			return
		}
		var notice skippedNotice
		for key, n := range r.pending {
			notice = n
			delete(r.pending, key)
			break
		}
		r.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), skippedRecordTimeout)
		s.recordPushScanSkipped(ctx, notice.tenant, notice.repository, notice.reference, notice.digest)
		cancel()
	}
}
