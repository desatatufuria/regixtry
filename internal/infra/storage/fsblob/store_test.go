package fsblob

import (
	"context"
	"io"
	"strings"
	"testing"

	domain "regixtry/internal/domain/regixtry"
)

// TestListBlobsReportsMtime is T3's fsblob half (design.md Testing
// Strategy): an empty store enumerates nothing, and a committed blob is
// reported with its digest, size, and mtime -- ListBlobs' only enumeration
// primitive on ports.BlobStore.
func TestListBlobsReportsMtime(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	empty, err := store.ListBlobs(context.Background())
	if err != nil {
		t.Fatalf("ListBlobs() error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("ListBlobs() on empty store = %d entries, want 0", len(empty))
	}

	payload := []byte("gc-candidate-body")
	upload, err := store.BeginUpload(context.Background(), domain.MustParseRepositoryRef("library/alpine"))
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader(string(payload))); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}
	expected := domain.DigestFromBytes(payload)
	if _, err := store.CommitUpload(context.Background(), upload.ID, expected); err != nil {
		t.Fatalf("CommitUpload() error = %v", err)
	}

	blobs, err := store.ListBlobs(context.Background())
	if err != nil {
		t.Fatalf("ListBlobs() error = %v", err)
	}
	if len(blobs) != 1 {
		t.Fatalf("ListBlobs() = %d entries, want 1", len(blobs))
	}
	if blobs[0].Digest != expected {
		t.Fatalf("blobs[0].Digest = %s, want %s", blobs[0].Digest, expected)
	}
	if blobs[0].Size != int64(len(payload)) {
		t.Fatalf("blobs[0].Size = %d, want %d", blobs[0].Size, len(payload))
	}
	if blobs[0].ModTime.IsZero() {
		t.Fatal("blobs[0].ModTime is zero, want the committed file's mtime")
	}
}

// TestListBlobsNeverEnumeratesUploads is T6's enumeration half: an
// in-flight upload (BeginUpload+PutUploadChunk, never committed) lives
// under uploads/<id>/ and MUST be invisible to ListBlobs -- the sweep must
// never touch or expose a half-written push (design's blocking-language
// doc comment on ports.BlobStore.ListBlobs).
func TestListBlobsNeverEnumeratesUploads(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	upload, err := store.BeginUpload(context.Background(), domain.MustParseRepositoryRef("library/alpine"))
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader("in-flight-upload-body")); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}

	blobs, err := store.ListBlobs(context.Background())
	if err != nil {
		t.Fatalf("ListBlobs() error = %v", err)
	}
	if len(blobs) != 0 {
		t.Fatalf("ListBlobs() reported %d entries while an upload is in flight, want 0 (uploads/ must never be enumerated)", len(blobs))
	}
}

// TestDeleteBlobNeverTouchesUploads is T6's delete half (design.md Testing
// Strategy): an in-flight upload under uploads/<id>/ must survive
// DeleteBlob completely untouched, proven by construction (DeleteBlob only
// ever resolves a path under blobsRoot, never uploads/) rather than by
// accident. RED today because Phase 7's stub unconditionally errors before
// touching the filesystem at all, so the "a committed blob is actually
// removed" assertion below fails for the honest reason -- Phase 8 makes it
// pass without ever touching this test's uploads/-survival assertion.
func TestDeleteBlobNeverTouchesUploads(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	inFlight, err := store.BeginUpload(context.Background(), domain.MustParseRepositoryRef("library/alpine"))
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if _, err := store.PutUploadChunk(context.Background(), inFlight.ID, strings.NewReader("in-flight-upload-body")); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}

	committedPayload := []byte("delete-target-body")
	committedDigest := domain.DigestFromBytes(committedPayload)
	committedUpload, err := store.BeginUpload(context.Background(), domain.MustParseRepositoryRef("library/alpine"))
	if err != nil {
		t.Fatalf("BeginUpload(committed) error = %v", err)
	}
	if _, err := store.PutUploadChunk(context.Background(), committedUpload.ID, strings.NewReader(string(committedPayload))); err != nil {
		t.Fatalf("PutUploadChunk(committed) error = %v", err)
	}
	if _, err := store.CommitUpload(context.Background(), committedUpload.ID, committedDigest); err != nil {
		t.Fatalf("CommitUpload(committed) error = %v", err)
	}

	removed, err := store.DeleteBlob(context.Background(), committedDigest)
	if err != nil {
		t.Fatalf("DeleteBlob() error = %v, want nil once a real unlink runs against a committed blob", err)
	}
	if !removed {
		t.Fatal("DeleteBlob() removed = false, want true for an existing committed blob")
	}

	// The in-flight upload must be completely untouched: same directory,
	// same file, same content -- proving DeleteBlob never resolved or
	// touched any path under uploads/.
	stillThere, err := store.GetUpload(context.Background(), inFlight.ID)
	if err != nil {
		t.Fatalf("GetUpload() error = %v, in-flight upload must survive DeleteBlob untouched", err)
	}
	if stillThere.ID != inFlight.ID {
		t.Fatalf("GetUpload().ID = %s, want %s", stillThere.ID, inFlight.ID)
	}
	if stillThere.Size != int64(len("in-flight-upload-body")) {
		t.Fatalf("GetUpload().Size = %d, want %d (upload content must be unchanged)", stillThere.Size, len("in-flight-upload-body"))
	}
}

// TestDeleteBlobRejectsTraversalShapedDigestBeforeTouchingFS is the
// filesystem-unlink-scope threat-matrix RED test (design.md Threat Matrix):
// a traversal-shaped digest string is rejected by domain.ParseDigest's own
// Validate() -- algorithm must be exactly "sha256" and the encoded part
// exactly 64 hex chars -- before DeleteBlob ever builds a path, so a value
// like "sha256:../../../../etc/passwd" can never reach os.Remove.
func TestDeleteBlobRejectsTraversalShapedDigestBeforeTouchingFS(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	traversal := domain.Digest("sha256:../../../../etc/passwd")
	removed, err := store.DeleteBlob(context.Background(), traversal)
	if err == nil {
		t.Fatal("DeleteBlob() error = nil, want a validation error for a traversal-shaped digest")
	}
	if !domain.IsCode(err, domain.ErrorCodeInvalidDigest) {
		t.Fatalf("DeleteBlob() error = %v, want ErrorCodeInvalidDigest", err)
	}
	if removed {
		t.Fatal("DeleteBlob() removed = true for a rejected digest, want false")
	}
}

// TestDeleteBlobIsIdempotentOnAlreadyAbsentFile pins DeleteBlob's documented
// idempotency contract: an already-absent blob is (false, nil), never an
// error.
func TestDeleteBlobIsIdempotentOnAlreadyAbsentFile(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	absent := domain.DigestFromBytes([]byte("never-committed-payload"))
	removed, err := store.DeleteBlob(context.Background(), absent)
	if err != nil {
		t.Fatalf("DeleteBlob() error = %v, want nil for an already-absent blob", err)
	}
	if removed {
		t.Fatal("DeleteBlob() removed = true for a blob that was never committed, want false")
	}
}

func TestStoreUploadCommitAndRead(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	repo := domain.MustParseRepositoryRef("library/alpine")
	upload, err := store.BeginUpload(context.Background(), repo)
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}

	updated, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader("hello blob"))
	if err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}

	if updated.Size != int64(len("hello blob")) {
		t.Fatalf("updated.Size = %d, want %d", updated.Size, len("hello blob"))
	}

	expected := domain.DigestFromBytes([]byte("hello blob"))
	descriptor, err := store.CommitUpload(context.Background(), upload.ID, expected)
	if err != nil {
		t.Fatalf("CommitUpload() error = %v", err)
	}

	if descriptor.Digest != expected {
		t.Fatalf("descriptor.Digest = %s, want %s", descriptor.Digest, expected)
	}

	exists, err := store.BlobExists(context.Background(), expected)
	if err != nil {
		t.Fatalf("BlobExists() error = %v", err)
	}
	if !exists {
		t.Fatal("expected blob to exist after commit")
	}

	reader, openedDescriptor, err := store.OpenBlob(context.Background(), expected)
	if err != nil {
		t.Fatalf("OpenBlob() error = %v", err)
	}
	defer reader.Close()

	payload, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if string(payload) != "hello blob" {
		t.Fatalf("blob payload = %q, want %q", payload, "hello blob")
	}

	if openedDescriptor.Size != descriptor.Size {
		t.Fatalf("openedDescriptor.Size = %d, want %d", openedDescriptor.Size, descriptor.Size)
	}
}

func TestStoreRejectsDigestMismatch(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	upload, err := store.BeginUpload(context.Background(), domain.MustParseRepositoryRef("library/alpine"))
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}

	if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader("wrong")); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}

	_, err = store.CommitUpload(context.Background(), upload.ID, domain.DigestFromBytes([]byte("expected")))
	if err == nil {
		t.Fatal("expected digest mismatch error")
	}

	if !domain.IsCode(err, domain.ErrorCodeDigestMismatch) {
		t.Fatalf("expected digest mismatch error, got %v", err)
	}
}
