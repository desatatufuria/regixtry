package fsblob

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
)

// TestCommitVersusConcurrentPatchNeverPoisonsTheBlob races writers against
// CommitUpload for the same upload id and checks, uid-independently, that a
// committed blob always hashes to its digest and that no path to its inode
// survives under the upload directory.
//
// Read-only file modes do not stop a root server, so a helper keeps flipping
// the committed blob back to 0644 the whole time (root emulation): the only
// thing left protecting the blob is that no writer can ever reach its inode.
func TestCommitVersusConcurrentPatchNeverPoisonsTheBlob(t *testing.T) {
	t.Parallel()

	deadline := time.Now().Add(2 * time.Second)
	for iteration := 0; iteration < 400 && time.Now().Before(deadline); iteration++ {
		store := newTestStore(t)
		upload := beginUpload(t, store)
		good := []byte("good-content-" + strings.Repeat("g", iteration%7))
		if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader(string(good))); err != nil {
			t.Fatalf("PutUploadChunk() error = %v", err)
		}
		digest := domain.DigestFromBytes(good)

		var stop atomic.Bool
		var wg sync.WaitGroup
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for !stop.Load() {
					_, _ = store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader("POISON"))
				}
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				_ = os.Chmod(store.blobPath(digest), 0o644) // emulate root
			}
		}()

		var commitErr error
		for attempt := 0; attempt < 50; attempt++ {
			// Writers legitimately make the digest mismatch or hold the lock;
			// what matters is what happens to a blob that does get committed.
			_, commitErr = store.CommitUpload(context.Background(), upload.ID, digest)
			if commitErr == nil {
				break
			}
		}
		stop.Store(true)
		wg.Wait()

		if commitErr != nil {
			continue
		}
		if actual, _, err := domain.DigestFromReader(strings.NewReader(string(readBlob(t, store, digest)))); err != nil || actual != digest {
			t.Fatalf("iteration %d: committed blob hashes to %s, want %s (err %v)", iteration, actual, digest, err)
		}
		if _, err := os.Lstat(store.uploadFile(upload.ID)); !os.IsNotExist(err) {
			t.Fatalf("iteration %d: upload data path still exists after commit (%v): a path to the blob inode survives", iteration, err)
		}
		if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader("late")); !domain.IsCode(err, domain.ErrorCodeNotFound) {
			t.Fatalf("iteration %d: PutUploadChunk() after commit error = %v, want not found", iteration, err)
		}
	}
}

func TestFailedCommitLeavesTheUploadWritable(t *testing.T) {
	t.Parallel()

	if os.Getuid() == 0 {
		t.Skip("directory permissions do not stop root")
	}
	store := newTestStore(t)
	upload := beginUpload(t, store)
	if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader("abc")); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}
	digest := domain.DigestFromBytes([]byte("abc"))
	dir := store.blobPath(digest)
	dir = dir[:strings.LastIndex(dir, "/")]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() { _ = os.Chmod(dir, 0o755) }()

	if _, err := store.CommitUpload(context.Background(), upload.ID, digest); err == nil {
		t.Fatal("CommitUpload() into an unwritable store succeeded")
	}
	if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader("def")); err != nil {
		t.Fatalf("PutUploadChunk() after a failed commit error = %v, want the upload still writable", err)
	}
	_ = os.Chmod(dir, 0o755)
	if _, err := store.CommitUpload(context.Background(), upload.ID, domain.DigestFromBytes([]byte("abcdef"))); err != nil {
		t.Fatalf("CommitUpload() after the store recovered error = %v", err)
	}
}

func TestCancelRacingCommitNeverYieldsAnInternalError(t *testing.T) {
	t.Parallel()

	for iteration := 0; iteration < 200; iteration++ {
		store := newTestStore(t)
		upload := beginUpload(t, store)
		if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader("racing")); err != nil {
			t.Fatalf("PutUploadChunk() error = %v", err)
		}
		digest := domain.DigestFromBytes([]byte("racing"))

		var wg sync.WaitGroup
		var commitErr error
		wg.Add(2)
		go func() { defer wg.Done(); _, commitErr = store.CommitUpload(context.Background(), upload.ID, digest) }()
		go func() { defer wg.Done(); _ = store.CancelUpload(context.Background(), upload.ID) }()
		wg.Wait()

		if commitErr != nil && !domain.IsCode(commitErr, domain.ErrorCodeNotFound) && !domain.IsCode(commitErr, domain.ErrorCodeConflict) {
			t.Fatalf("iteration %d: CommitUpload() racing a cancel error = %v, want nil, not found or conflict", iteration, commitErr)
		}
	}
}

func TestCancelRacingFinishingPatchesNeverFails(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	deadline := time.Now().Add(2500 * time.Millisecond)
	for iteration := 0; iteration < 1500 && time.Now().Before(deadline); iteration++ {
		upload := beginUpload(t, store)

		var stop atomic.Bool
		var wg sync.WaitGroup
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for !stop.Load() {
					_, _ = store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader("x"))
				}
			}()
		}
		time.Sleep(time.Duration(iteration%3) * 100 * time.Microsecond)
		err := store.CancelUpload(context.Background(), upload.ID)
		stop.Store(true)
		wg.Wait()

		if err != nil && !domain.IsCode(err, domain.ErrorCodeNotFound) && !domain.IsCode(err, domain.ErrorCodeConflict) {
			t.Fatalf("iteration %d: CancelUpload() racing PATCHes error = %v, want nil, not found or conflict", iteration, err)
		}
		if _, statErr := os.Stat(store.uploadDir(upload.ID)); !os.IsNotExist(statErr) {
			t.Fatalf("iteration %d: upload dir still present after cancel: %v", iteration, statErr)
		}
	}
}
