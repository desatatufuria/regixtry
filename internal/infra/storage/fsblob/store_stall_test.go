package fsblob

import (
	"context"
	"os"
	"sync"
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
