package fsblob

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
)

func newTestStore(t *testing.T, options ...Option) *Store {
	t.Helper()
	store, err := New(t.TempDir(), options...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return store
}

func beginUpload(t *testing.T, store *Store) domain.UploadState {
	t.Helper()
	upload, err := store.BeginUpload(context.Background(), domain.MustParseRepositoryRef("library/alpine"))
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	return upload
}

func readBlob(t *testing.T, store *Store, digest domain.Digest) []byte {
	t.Helper()
	reader, _, err := store.OpenBlob(context.Background(), digest)
	if err != nil {
		t.Fatalf("OpenBlob(%s) error = %v", digest, err)
	}
	defer reader.Close()
	payload, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	return payload
}

// TestCommittedBlobCannotBeModifiedThroughAStaleWriter reproduces the
// audit's blob-poisoning claim: a PATCH body is still streaming (so its
// write descriptor is open) when the upload is committed. Whatever the commit
// outcome, once a blob exists under a digest its bytes must hash to that
// digest, no matter what the stale writer sends afterwards.
func TestCommittedBlobCannotBeModifiedThroughAStaleWriter(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	upload := beginUpload(t, store)

	good := []byte("good-content")
	pipeReader, pipeWriter := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := store.PutUploadChunk(context.Background(), upload.ID, pipeReader)
		done <- err
	}()

	if _, err := pipeWriter.Write(good); err != nil {
		t.Fatalf("pipe write: %v", err)
	}
	// Wait until the writer has flushed the first bytes to the file, which
	// proves its descriptor is open and in the middle of the body.
	deadline := time.Now().Add(5 * time.Second)
	for {
		info, err := os.Stat(store.uploadFile(upload.ID))
		if err == nil && info.Size() == int64(len(good)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writer never flushed the first chunk")
		}
		time.Sleep(5 * time.Millisecond)
	}

	digest := domain.DigestFromBytes(good)
	_, commitErr := store.CommitUpload(context.Background(), upload.ID, digest)

	// The stale writer keeps going after the commit attempt.
	_, _ = pipeWriter.Write([]byte("-POISON"))
	_ = pipeWriter.Close()
	<-done

	if exists, _ := store.BlobExists(context.Background(), digest); exists {
		actual, _, err := domain.DigestFromReader(strings.NewReader(string(readBlob(t, store, digest))))
		if err != nil {
			t.Fatalf("DigestFromReader() error = %v", err)
		}
		if actual != digest {
			t.Fatalf("committed blob %s now hashes to %s: it was modified after commit (commit error = %v)", digest, actual, commitErr)
		}
	}
}

func TestCommitIsRefusedWhileAWriteIsInFlight(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	upload := beginUpload(t, store)

	pipeReader, pipeWriter := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := store.PutUploadChunk(context.Background(), upload.ID, pipeReader)
		done <- err
	}()
	if _, err := pipeWriter.Write([]byte("abc")); err != nil {
		t.Fatalf("pipe write: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if info, err := os.Stat(store.uploadFile(upload.ID)); err == nil && info.Size() == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writer never flushed")
		}
		time.Sleep(5 * time.Millisecond)
	}

	_, err := store.CommitUpload(context.Background(), upload.ID, domain.DigestFromBytes([]byte("abc")))
	if !domain.IsCode(err, domain.ErrorCodeConflict) {
		t.Fatalf("CommitUpload() during a write error = %v, want conflict", err)
	}

	_ = pipeWriter.Close()
	if err := <-done; err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}

	// Once the writer is done the same commit succeeds.
	if _, err := store.CommitUpload(context.Background(), upload.ID, domain.DigestFromBytes([]byte("abc"))); err != nil {
		t.Fatalf("CommitUpload() after the write error = %v", err)
	}
}

func TestWritesAfterCommitAreRejected(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	upload := beginUpload(t, store)
	payload := []byte("payload")
	if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader(string(payload))); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}
	digest := domain.DigestFromBytes(payload)
	if _, err := store.CommitUpload(context.Background(), upload.ID, digest); err != nil {
		t.Fatalf("CommitUpload() error = %v", err)
	}

	_, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader("more"))
	if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("PutUploadChunk() after commit error = %v, want not found", err)
	}
	if got := string(readBlob(t, store, digest)); got != string(payload) {
		t.Fatalf("blob = %q, want %q", got, payload)
	}
}

func TestCommittedBlobIsReadOnlyAndNeverReplaced(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	payload := []byte("same-bytes")
	digest := domain.DigestFromBytes(payload)

	first := beginUpload(t, store)
	if _, err := store.PutUploadChunk(context.Background(), first.ID, strings.NewReader(string(payload))); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}
	if _, err := store.CommitUpload(context.Background(), first.ID, digest); err != nil {
		t.Fatalf("CommitUpload() error = %v", err)
	}
	before, err := os.Stat(store.blobPath(digest))
	if err != nil {
		t.Fatalf("stat blob: %v", err)
	}
	if before.Mode().Perm() != 0o444 {
		t.Fatalf("blob mode = %v, want 0444", before.Mode().Perm())
	}

	second := beginUpload(t, store)
	if _, err := store.PutUploadChunk(context.Background(), second.ID, strings.NewReader(string(payload))); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}
	if _, err := store.CommitUpload(context.Background(), second.ID, digest); err != nil {
		t.Fatalf("second CommitUpload() error = %v", err)
	}
	after, err := os.Stat(store.blobPath(digest))
	if err != nil {
		t.Fatalf("stat blob: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("recommitting identical content replaced the existing blob inode")
	}
	if _, err := os.Stat(store.uploadDir(second.ID)); !os.IsNotExist(err) {
		t.Fatalf("upload dir still present after commit: %v", err)
	}
}

func TestDigestMismatchLeavesUploadOpenForMoreData(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	upload := beginUpload(t, store)
	if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader("part")); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}
	full := []byte("part-two")
	digest := domain.DigestFromBytes(full)
	if _, err := store.CommitUpload(context.Background(), upload.ID, digest); !domain.IsCode(err, domain.ErrorCodeDigestMismatch) {
		t.Fatalf("CommitUpload() error = %v, want digest mismatch", err)
	}
	if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader("-two")); err != nil {
		t.Fatalf("PutUploadChunk() after mismatch error = %v", err)
	}
	if _, err := store.CommitUpload(context.Background(), upload.ID, digest); err != nil {
		t.Fatalf("CommitUpload() error = %v", err)
	}
}

func TestMaxUploadSizeIsEnforcedAcrossChunks(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, WithMaxUploadSize(10))
	upload := beginUpload(t, store)

	if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader("123456")); err != nil {
		t.Fatalf("first chunk error = %v", err)
	}
	_, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader("78901"))
	if !domain.IsCode(err, domain.ErrorCodePayloadTooLarge) {
		t.Fatalf("second chunk error = %v, want payload too large", err)
	}
	state, err := store.GetUpload(context.Background(), upload.ID)
	if err != nil {
		t.Fatalf("GetUpload() error = %v", err)
	}
	if state.Size != 6 {
		t.Fatalf("size after rejected chunk = %d, want 6 (rolled back)", state.Size)
	}
	// Exactly reaching the cap is allowed.
	if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader("7890")); err != nil {
		t.Fatalf("chunk reaching the cap error = %v", err)
	}
}

func TestMaxUploadSizeStopsAnEndlessBody(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, WithMaxUploadSize(1024))
	upload := beginUpload(t, store)

	_, err := store.PutUploadChunk(context.Background(), upload.ID, endlessReader{})
	if !domain.IsCode(err, domain.ErrorCodePayloadTooLarge) {
		t.Fatalf("endless body error = %v, want payload too large", err)
	}
	info, statErr := os.Stat(store.uploadFile(upload.ID))
	if statErr != nil {
		t.Fatalf("stat: %v", statErr)
	}
	if info.Size() != 0 {
		t.Fatalf("file size after rejected body = %d, want 0", info.Size())
	}
}

type endlessReader struct{}

func (endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestCancelUploadStopsAStreamingWriter(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	upload := beginUpload(t, store)

	pipeReader, pipeWriter := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := store.PutUploadChunk(context.Background(), upload.ID, pipeReader)
		done <- err
	}()
	_, _ = pipeWriter.Write([]byte("abc"))

	if err := store.CancelUpload(context.Background(), upload.ID); err != nil {
		t.Fatalf("CancelUpload() error = %v", err)
	}
	// The writer is parked in a body read; one more chunk wakes it, and it
	// must then stop instead of waiting for the body to finish.
	go func() { _, _ = pipeWriter.Write([]byte("def")) }()
	err := <-done
	_ = pipeWriter.Close()
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("writer error after cancel = %v, want not found", err)
	}
	if _, statErr := os.Stat(store.uploadDir(upload.ID)); !os.IsNotExist(statErr) {
		t.Fatalf("upload dir still present after cancel: %v", statErr)
	}
}

func TestReapUploadHonorsCutoffAndInFlightWrites(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	upload := beginUpload(t, store)
	if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader("data")); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}

	reaped, err := store.ReapUpload(context.Background(), upload.ID, time.Now().Add(-time.Hour))
	if err != nil || reaped {
		t.Fatalf("ReapUpload(fresh) = %v, %v, want false, nil", reaped, err)
	}

	// A writer holding the upload blocks the reaper even for an old cutoff.
	pipeReader, pipeWriter := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := store.PutUploadChunk(context.Background(), upload.ID, pipeReader)
		done <- err
	}()
	_, _ = pipeWriter.Write([]byte("x"))
	deadline := time.Now().Add(5 * time.Second)
	for {
		if info, err := os.Stat(store.uploadFile(upload.ID)); err == nil && info.Size() == 5 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writer never flushed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	reaped, err = store.ReapUpload(context.Background(), upload.ID, time.Now().Add(time.Hour))
	if err != nil || reaped {
		t.Fatalf("ReapUpload(in flight) = %v, %v, want false, nil", reaped, err)
	}
	_ = pipeWriter.Close()
	<-done

	reaped, err = store.ReapUpload(context.Background(), upload.ID, time.Now().Add(time.Hour))
	if err != nil || !reaped {
		t.Fatalf("ReapUpload(stale) = %v, %v, want true, nil", reaped, err)
	}
	if _, statErr := os.Stat(store.uploadDir(upload.ID)); !os.IsNotExist(statErr) {
		t.Fatalf("upload dir still present after reap: %v", statErr)
	}
}

func TestCommitFallsBackToVerifiedCopyWhenLinkFails(t *testing.T) {
	t.Parallel()

	for _, linkErr := range []error{syscall.EXDEV, syscall.EPERM, syscall.ENOTSUP} {
		t.Run(linkErr.Error(), func(t *testing.T) {
			t.Parallel()
			store := newTestStore(t)
			store.linkFile = func(string, string) error { return &os.LinkError{Op: "link", Err: linkErr} }

			upload := beginUpload(t, store)
			payload := []byte("copied-into-the-store")
			if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader(string(payload))); err != nil {
				t.Fatalf("PutUploadChunk() error = %v", err)
			}
			digest := domain.DigestFromBytes(payload)
			if _, err := store.CommitUpload(context.Background(), upload.ID, digest); err != nil {
				t.Fatalf("CommitUpload() with failing link error = %v", err)
			}

			if got := string(readBlob(t, store, digest)); got != string(payload) {
				t.Fatalf("blob = %q, want %q", got, payload)
			}
			info, err := os.Stat(store.blobPath(digest))
			if err != nil {
				t.Fatalf("stat blob: %v", err)
			}
			if info.Mode().Perm() != 0o444 {
				t.Fatalf("blob mode = %v, want 0444", info.Mode().Perm())
			}
			assertNoTempFiles(t, store, digest)
			if _, err := os.Stat(store.uploadDir(upload.ID)); !os.IsNotExist(err) {
				t.Fatalf("upload dir still present: %v", err)
			}
		})
	}
}

func TestCommitFallbackNeverReplacesAnExistingBlobAndLeavesNoTempFile(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	payload := []byte("already-there")
	digest := domain.DigestFromBytes(payload)

	first := beginUpload(t, store)
	if _, err := store.PutUploadChunk(context.Background(), first.ID, strings.NewReader(string(payload))); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}
	if _, err := store.CommitUpload(context.Background(), first.ID, digest); err != nil {
		t.Fatalf("CommitUpload() error = %v", err)
	}
	before, err := os.Stat(store.blobPath(digest))
	if err != nil {
		t.Fatalf("stat blob: %v", err)
	}

	// The link now fails with something other than EEXIST: the copy path must
	// notice the occupied final path and keep the existing blob.
	store.linkFile = func(string, string) error { return syscall.EXDEV }
	second := beginUpload(t, store)
	if _, err := store.PutUploadChunk(context.Background(), second.ID, strings.NewReader(string(payload))); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}
	if _, err := store.CommitUpload(context.Background(), second.ID, digest); err != nil {
		t.Fatalf("second CommitUpload() error = %v", err)
	}
	after, err := os.Stat(store.blobPath(digest))
	if err != nil {
		t.Fatalf("stat blob: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("fallback replaced an existing blob")
	}
	assertNoTempFiles(t, store, digest)
}

func TestCopyIntoStoreLeavesNoPartialFileOnFailure(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	payload := []byte("will-not-match")
	upload := beginUpload(t, store)
	if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader(string(payload))); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}
	// Asking for a different digest makes the post-copy verification fail.
	wrong := domain.DigestFromBytes([]byte("something else"))
	finalPath := store.blobPath(wrong)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := store.copyIntoStore(store.uploadFile(upload.ID), finalPath, wrong); !domain.IsCode(err, domain.ErrorCodeDigestMismatch) {
		t.Fatalf("copyIntoStore() error = %v, want digest mismatch", err)
	}
	entries, err := os.ReadDir(filepath.Dir(finalPath))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("store dir has %d leftover entries after a failed copy, want 0", len(entries))
	}
}

func assertNoTempFiles(t *testing.T, store *Store, digest domain.Digest) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(store.blobPath(digest)))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 {
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("store dir entries = %v, want exactly the blob", names)
	}
}

func TestReapOrphanUploadsOnlyRemovesStaleRowlessUploadDirs(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	withRow := beginUpload(t, store)
	orphan := beginUpload(t, store)
	freshOrphan := beginUpload(t, store)

	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	linkName := strings.Repeat("a", 32)
	if err := os.Symlink(outside, store.uploadDir(linkName)); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	notAnUpload := filepath.Join(store.uploadsRoot(), "not-an-upload-id")
	if err := os.MkdirAll(notAnUpload, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	old := time.Now().Add(-48 * time.Hour)
	for _, id := range []string{withRow.ID, orphan.ID} {
		for _, path := range []string{store.uploadFile(id), store.uploadStateFile(id), store.uploadDir(id)} {
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatalf("chtimes: %v", err)
			}
		}
	}
	if err := os.Chtimes(notAnUpload, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := os.Lchown(store.uploadDir(linkName), os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("lchown: %v", err)
	}

	rows := map[string]bool{withRow.ID: true}
	hasRow := func(_ context.Context, id string) (bool, error) { return rows[id], nil }
	cutoff := time.Now().Add(-24 * time.Hour)

	reaped, err := store.ReapOrphanUploads(ctx, cutoff, 100, hasRow)
	if err != nil {
		t.Fatalf("ReapOrphanUploads() error = %v", err)
	}
	if reaped != 1 {
		t.Fatalf("reaped = %d, want 1 (only the stale rowless dir)", reaped)
	}
	if _, err := os.Stat(store.uploadDir(orphan.ID)); !os.IsNotExist(err) {
		t.Fatalf("orphan dir still present: %v", err)
	}
	for name, path := range map[string]string{"dir with a row": store.uploadDir(withRow.ID), "fresh orphan": store.uploadDir(freshOrphan.ID), "non-upload name": notAnUpload} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s must be kept: %v", name, err)
		}
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("a symlinked directory outside the root was touched: %v", err)
	}
}

func TestReapOrphanUploadsIsBoundedPerCall(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	old := time.Now().Add(-48 * time.Hour)
	for i := 0; i < 5; i++ {
		upload := beginUpload(t, store)
		for _, path := range []string{store.uploadFile(upload.ID), store.uploadStateFile(upload.ID), store.uploadDir(upload.ID)} {
			_ = os.Chtimes(path, old, old)
		}
	}
	noRow := func(context.Context, string) (bool, error) { return false, nil }
	reaped, err := store.ReapOrphanUploads(context.Background(), time.Now().Add(-24*time.Hour), 2, noRow)
	if err != nil || reaped != 2 {
		t.Fatalf("ReapOrphanUploads(limit 2) = %d, %v, want 2, nil", reaped, err)
	}
}
