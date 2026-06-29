package fsblob

import (
	"context"
	"io"
	"strings"
	"testing"

	domain "registry/internal/domain/registry"
)

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
