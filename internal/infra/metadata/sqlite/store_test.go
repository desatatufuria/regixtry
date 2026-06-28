package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	domain "registry/internal/domain/registry"
)

func TestStorePublishResolveCatalogAndTags(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	repo := domain.MustParseRepositoryRef("library/alpine")
	blobs := []domain.Descriptor{
		{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: domain.DigestFromBytes([]byte("layer-1")), Size: int64(len("layer-1"))},
		{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: domain.DigestFromBytes([]byte("layer-2")), Size: int64(len("layer-2"))},
	}

	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", []byte(`{"schemaVersion":2}`), nil, blobs, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}

	if err := store.PublishManifest(context.Background(), "tenant-a", repo, "latest", manifest, blobs); err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}

	resolvedByTag, err := store.ResolveManifest(context.Background(), "tenant-a", repo, "latest")
	if err != nil {
		t.Fatalf("ResolveManifest(tag) error = %v", err)
	}

	if resolvedByTag.Digest != manifest.Digest {
		t.Fatalf("resolvedByTag.Digest = %s, want %s", resolvedByTag.Digest, manifest.Digest)
	}

	resolvedByDigest, err := store.ResolveManifest(context.Background(), "tenant-a", repo, manifest.Digest.String())
	if err != nil {
		t.Fatalf("ResolveManifest(digest) error = %v", err)
	}

	if resolvedByDigest.Digest != manifest.Digest {
		t.Fatalf("resolvedByDigest.Digest = %s, want %s", resolvedByDigest.Digest, manifest.Digest)
	}

	catalog, err := store.Catalog(context.Background(), "tenant-a", 10, "")
	if err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}

	if len(catalog) != 1 || catalog[0].String() != repo.String() {
		t.Fatalf("catalog = %#v, want [%s]", catalog, repo)
	}

	tags, err := store.ListTags(context.Background(), "tenant-a", repo, 10, "")
	if err != nil {
		t.Fatalf("ListTags() error = %v", err)
	}

	if len(tags) != 1 || tags[0] != "latest" {
		t.Fatalf("tags = %#v, want [latest]", tags)
	}

	linkedBlobs, err := store.ListManifestBlobs(context.Background(), "tenant-a", repo, manifest.Digest)
	if err != nil {
		t.Fatalf("ListManifestBlobs() error = %v", err)
	}

	if len(linkedBlobs) != len(blobs) {
		t.Fatalf("len(linkedBlobs) = %d, want %d", len(linkedBlobs), len(blobs))
	}
}

func TestStorePersistsUploadMetadataAcrossReopen(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "registry.db")
	store, err := New(databasePath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	upload := domain.UploadState{
		ID:         "upload-1",
		Repository: domain.MustParseRepositoryRef("library/alpine"),
		Status:     domain.UploadStatusActive,
		Size:       42,
		StartedAt:  time.Now().UTC().Add(-time.Minute),
		UpdatedAt:  time.Now().UTC(),
		Location:   "/tmp/uploads/upload-1/data",
	}

	if err := store.SaveUpload(context.Background(), "tenant-a", upload); err != nil {
		t.Fatalf("SaveUpload() error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := New(databasePath)
	if err != nil {
		t.Fatalf("New(reopen) error = %v", err)
	}
	defer reopened.Close()

	storedUpload, err := reopened.GetUpload(context.Background(), "tenant-a", upload.ID)
	if err != nil {
		t.Fatalf("GetUpload() error = %v", err)
	}

	if storedUpload.Repository.String() != upload.Repository.String() || storedUpload.Size != upload.Size || storedUpload.Location != upload.Location {
		t.Fatalf("stored upload = %#v, want %#v", storedUpload, upload)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := New(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return store
}
