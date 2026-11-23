package fsblob

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
)

// gatedReader parks inside Read until released, and signals that it is parked.
// It models an HTTP body whose client stopped sending without closing.
type gatedReader struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newGatedReader() *gatedReader {
	return &gatedReader{entered: make(chan struct{}), release: make(chan struct{})}
}

func (g *gatedReader) Read(p []byte) (int, error) {
	g.once.Do(func() { close(g.entered) })
	<-g.release
	return 0, os.ErrClosed
}

// TestCancelUploadDoesNotWaitForAStalledBody pins the deterministic form of
// the race behind TestCancelUploadStopsAStreamingWriter: the writer is already
// parked INSIDE the body Read when the cancel arrives, so only waking that
// read (not checking a flag before it) lets the cancel finish.
func TestCancelUploadDoesNotWaitForAStalledBody(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	upload := beginUpload(t, store)
	body := newGatedReader()
	t.Cleanup(func() { close(body.release) })

	writerDone := make(chan error, 1)
	go func() {
		_, err := store.PutUploadChunk(context.Background(), upload.ID, body)
		writerDone <- err
	}()
	select {
	case <-body.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("writer never reached the body read")
	}

	cancelDone := make(chan error, 1)
	go func() { cancelDone <- store.CancelUpload(context.Background(), upload.ID) }()
	select {
	case err := <-cancelDone:
		if err != nil {
			t.Fatalf("CancelUpload() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CancelUpload blocked behind a writer stalled in a body read")
	}

	select {
	case err := <-writerDone:
		if !domain.IsCode(err, domain.ErrorCodeNotFound) {
			t.Fatalf("writer error after cancel = %v, want not found", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("writer did not return after the cancel")
	}
	if _, statErr := os.Stat(store.uploadDir(upload.ID)); !os.IsNotExist(statErr) {
		t.Fatalf("upload dir still present after cancel: %v", statErr)
	}
}

// stallingBody feeds a few bytes and then goes silent until closed, like a
// slow HTTP client that keeps the connection open.
func stallingBody(payload []byte) (*io.PipeReader, func()) {
	reader, writer := io.Pipe()
	go func() { _, _ = writer.Write(payload) }()
	return reader, func() { _ = writer.Close() }
}

// TestUploadLifecycleStress hammers writers, cancels, commits and reaps on a
// shared store. Invariants: nothing deadlocks, no goroutine outlives its body
// Read, a committed blob hashes to its digest, a cancelled upload leaves no
// directory. Run it under -race.
//
// It runs 3 s by default and 1 s under -short. REGIXTRY_STRESS_SECONDS (integer
// seconds) overrides both, e.g. REGIXTRY_STRESS_SECONDS=30 for a long soak.
func TestUploadLifecycleStress(t *testing.T) {
	duration := 3 * time.Second
	if testing.Short() {
		duration = time.Second
	}
	if raw := os.Getenv("REGIXTRY_STRESS_SECONDS"); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds <= 0 {
			t.Fatalf("REGIXTRY_STRESS_SECONDS = %q, want a positive integer", raw)
		}
		duration = time.Duration(seconds) * time.Second
	}
	store := newTestStore(t)
	// settle waits for the goroutine count to stop changing (or to reach want
	// when want > 0) and returns it.
	settle := func(want int) int {
		last := -1
		for i := 0; i < 100; i++ {
			runtime.GC()
			time.Sleep(20 * time.Millisecond)
			n := runtime.NumGoroutine()
			if (want > 0 && n <= want) || (want == 0 && n == last) {
				return n
			}
			last = n
		}
		return runtime.NumGoroutine()
	}
	baseline := settle(0)

	deadline := time.Now().Add(duration)
	var counter atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			for time.Now().Before(deadline) {
				n := counter.Add(1)
				upload, err := store.BeginUpload(ctx, domain.MustParseRepositoryRef("library/alpine"))
				if err != nil {
					t.Errorf("BeginUpload: %v", err)
					return
				}
				payload := []byte(fmt.Sprintf("payload-%d-%s", n, strings.Repeat("x", int(n%97))))
				switch n % 4 {
				case 0: // cancel a stalled writer
					body, release := stallingBody(payload)
					werr := make(chan error, 1)
					go func() { _, err := store.PutUploadChunk(ctx, upload.ID, body); werr <- err }()
					waitForSize(store, upload.ID, int64(len(payload)))
					if err := store.CancelUpload(ctx, upload.ID); err != nil {
						t.Errorf("CancelUpload: %v", err)
					}
					if err := <-werr; !domain.IsCode(err, domain.ErrorCodeNotFound) {
						t.Errorf("writer after cancel = %v, want not found", err)
					}
					release()
				case 1: // reap must refuse while a stalled writer holds the upload, then cancel
					body, release := stallingBody(payload)
					werr := make(chan error, 1)
					go func() { _, err := store.PutUploadChunk(ctx, upload.ID, body); werr <- err }()
					waitForSize(store, upload.ID, int64(len(payload)))
					if reaped, err := store.ReapUpload(ctx, upload.ID, time.Now().Add(time.Hour)); reaped || err != nil {
						t.Errorf("ReapUpload during write = %v, %v, want false, nil", reaped, err)
					}
					_ = store.CancelUpload(ctx, upload.ID)
					<-werr
					release()
				case 2: // full upload then commit
					if _, err := store.PutUploadChunk(ctx, upload.ID, bytes.NewReader(payload)); err != nil {
						t.Errorf("PutUploadChunk: %v", err)
						return
					}
					digest := domain.DigestFromBytes(payload)
					if _, err := store.CommitUpload(ctx, upload.ID, digest); err != nil {
						t.Errorf("CommitUpload: %v", err)
						return
					}
					if got := readBlob(t, store, digest); !bytes.Equal(got, payload) {
						t.Errorf("committed blob corrupted")
					}
				default: // commit racing a cancel
					if _, err := store.PutUploadChunk(ctx, upload.ID, bytes.NewReader(payload)); err != nil {
						t.Errorf("PutUploadChunk: %v", err)
						return
					}
					digest := domain.DigestFromBytes(payload)
					var inner sync.WaitGroup
					var commitErr error
					inner.Add(2)
					go func() { defer inner.Done(); _, commitErr = store.CommitUpload(ctx, upload.ID, digest) }()
					go func() { defer inner.Done(); _ = store.CancelUpload(ctx, upload.ID) }()
					inner.Wait()
					if commitErr == nil {
						if got := readBlob(t, store, digest); !bytes.Equal(got, payload) {
							t.Errorf("committed blob corrupted")
						}
					}
					_ = store.CancelUpload(ctx, upload.ID)
				}
				if n%4 != 2 {
					if _, statErr := os.Stat(store.uploadDir(upload.ID)); !os.IsNotExist(statErr) {
						t.Errorf("upload dir present after cancel: %v", statErr)
					}
				}
			}
		}()
	}
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(duration + 30*time.Second):
		buf := make([]byte, 1<<20)
		t.Fatalf("stress workers deadlocked:\n%s", buf[:runtime.Stack(buf, true)])
	}
	if after := settle(baseline + 1); after > baseline+1 {
		buf := make([]byte, 1<<20)
		t.Fatalf("goroutines = %d, baseline %d (leak):\n%s", after, baseline, buf[:runtime.Stack(buf, true)])
	}
	t.Logf("iterations: %d", counter.Load())
}

func waitForSize(store *Store, uploadID string, size int64) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(store.uploadFile(uploadID)); err == nil && info.Size() >= size {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func BenchmarkPutUploadChunk256MB(b *testing.B) {
	store, err := New(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	const size = 256 << 20
	b.SetBytes(size)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		upload, err := store.BeginUpload(context.Background(), domain.MustParseRepositoryRef("library/alpine"))
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := store.PutUploadChunk(context.Background(), upload.ID, io.LimitReader(endlessReader{}, size)); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		_ = store.CancelUpload(context.Background(), upload.ID)
		b.StartTimer()
	}
}
