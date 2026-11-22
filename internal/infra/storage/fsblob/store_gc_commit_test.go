package fsblob

import (
	"context"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

func ageFile(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("age %s: %v", path, err)
	}
}

func commitPayload(t *testing.T, store *Store, payload []byte, beforeCommit func(uploadID string)) domain.Digest {
	t.Helper()
	upload := beginUpload(t, store)
	if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader(string(payload))); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}
	if beforeCommit != nil {
		beforeCommit(upload.ID)
	}
	digest := domain.DigestFromBytes(payload)
	if _, err := store.CommitUpload(context.Background(), upload.ID, digest); err != nil {
		t.Fatalf("CommitUpload() error = %v", err)
	}
	return digest
}

// A commit of any kind leaves the blob younger than the GC grace window, so a
// GC that raced the commit cannot unlink it right afterwards.
func TestCommitRefreshesBlobAgeForGC(t *testing.T) {
	t.Parallel()
	grace := time.Now().Add(-24 * time.Hour)

	cases := map[string]func(t *testing.T) (*Store, domain.Digest){
		"idle upload": func(t *testing.T) (*Store, domain.Digest) {
			store := newTestStore(t)
			digest := commitPayload(t, store, []byte("idle"), func(id string) { ageFile(t, store.uploadFile(id)) })
			return store, digest
		},
		"dedup onto an old blob": func(t *testing.T) (*Store, domain.Digest) {
			store := newTestStore(t)
			payload := []byte("dedup")
			first := commitPayload(t, store, payload, nil)
			ageFile(t, store.blobPath(first))
			return store, commitPayload(t, store, payload, nil)
		},
		"copy fallback": func(t *testing.T) (*Store, domain.Digest) {
			store := newTestStore(t)
			store.renameFile = func(string, string) error { return &os.LinkError{Op: "rename", Err: syscall.EXDEV} }
			digest := commitPayload(t, store, []byte("copy"), func(id string) { ageFile(t, store.uploadFile(id)) })
			return store, digest
		},
		"copy fallback dedup": func(t *testing.T) (*Store, domain.Digest) {
			store := newTestStore(t)
			store.renameFile = func(string, string) error { return &os.LinkError{Op: "rename", Err: syscall.EXDEV} }
			payload := []byte("copy-dedup")
			first := commitPayload(t, store, payload, nil)
			ageFile(t, store.blobPath(first))
			return store, commitPayload(t, store, payload, nil)
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store, digest := run(t)
			outcome, err := store.DeleteBlobIfOlderThan(context.Background(), digest, grace)
			if err != nil || outcome != ports.BlobReclaimTooRecent {
				t.Fatalf("DeleteBlobIfOlderThan() = %v, %v; want too_recent (blob must stay fresh after a commit)", outcome, err)
			}
			if exists, _ := store.BlobExists(context.Background(), digest); !exists {
				t.Fatal("blob missing after commit + GC attempt")
			}
		})
	}
}

// Commits and GC share one mutex: hammering both never loses a blob that a
// commit just returned.
func TestConcurrentCommitsAndReclaimNeverLoseACommittedBlob(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	grace := func() time.Time { return time.Now().Add(-24 * time.Hour) }
	payload := []byte("hammered")
	digest := domain.DigestFromBytes(payload)

	stop := make(chan struct{})
	var gc sync.WaitGroup
	gc.Add(1)
	go func() {
		defer gc.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = store.DeleteBlobIfOlderThan(context.Background(), digest, grace())
			}
		}
	}()
	for i := 0; i < 40; i++ {
		commitPayload(t, store, payload, func(id string) { ageFile(t, store.uploadFile(id)) })
		if exists, _ := store.BlobExists(context.Background(), digest); !exists {
			t.Fatalf("iteration %d: blob gone right after a successful commit", i)
		}
		ageFile(t, store.blobPath(digest))
	}
	close(stop)
	gc.Wait()
}
