package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
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

func TestStorePersistsDefaultDisabledScanSettings(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	settings := ports.ScanSettings{
		Enabled:         false,
		ScheduleEnabled: false,
		Interval:        24 * time.Hour,
		Timeout:         15 * time.Minute,
		CacheDir:        "/var/lib/regixtry/trivy-cache",
		BinaryPath:      "trivy",
		MaxConcurrency:  1,
	}
	if err := store.UpsertScanSettings(context.Background(), "tenant-a", settings); err != nil {
		t.Fatalf("UpsertScanSettings() error = %v", err)
	}

	stored, err := store.GetScanSettings(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("GetScanSettings() error = %v", err)
	}
	if stored.Enabled || stored.ScheduleEnabled {
		t.Fatalf("stored = %#v, want disabled defaults", stored)
	}
	if stored.CacheDir != settings.CacheDir || stored.BinaryPath != settings.BinaryPath || stored.MaxConcurrency != settings.MaxConcurrency {
		t.Fatalf("stored = %#v, want %#v", stored, settings)
	}
}

func TestStorePersistsScanRunsAndSchedulerStateAcrossReopen(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "registry.db")
	store, err := New(databasePath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	finishedAt := time.Now().UTC()
	dbUpdatedAt := finishedAt.Add(-time.Hour)
	runs := []ports.ScanRun{
		{ID: "run-completed", Repository: "library/alpine", RequestedRef: "latest", Digest: domain.DigestFromBytes([]byte("manifest-1")).String(), Status: ports.ScanRunStatusCompleted, Trigger: ports.ScanTriggerManual, StartedAt: &finishedAt, FinishedAt: &finishedAt, Critical: 1, High: 2, Medium: 3, Low: 4, TrivyVersion: "0.54.0", DBUpdatedAt: &dbUpdatedAt},
		{ID: "run-failed", Repository: "library/alpine", RequestedRef: "1.0", Digest: domain.DigestFromBytes([]byte("manifest-2")).String(), Status: ports.ScanRunStatusFailed, Trigger: ports.ScanTriggerManual, FinishedAt: &finishedAt, Error: "boom"},
	}
	for _, run := range runs {
		if err := store.UpsertScanRun(context.Background(), "tenant-a", run); err != nil {
			t.Fatalf("UpsertScanRun(%s) error = %v", run.ID, err)
		}
	}

	state := ports.ScanSchedulerState{OwnerID: "node-a", LeaseExpiresAt: finishedAt.Add(time.Minute), LastHeartbeatAt: finishedAt, BatchStartedAt: &finishedAt}
	if err := store.UpsertScanSchedulerState(context.Background(), "tenant-a", state); err != nil {
		t.Fatalf("UpsertScanSchedulerState() error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := New(databasePath)
	if err != nil {
		t.Fatalf("New(reopen) error = %v", err)
	}
	defer reopened.Close()

	storedRuns, err := reopened.ListScanRuns(context.Background(), "tenant-a", "library/alpine", 10)
	if err != nil {
		t.Fatalf("ListScanRuns() error = %v", err)
	}
	if len(storedRuns) != 2 {
		t.Fatalf("len(storedRuns) = %d, want 2", len(storedRuns))
	}
	if storedRuns[0].Status != ports.ScanRunStatusFailed || storedRuns[1].Status != ports.ScanRunStatusCompleted {
		t.Fatalf("storedRuns = %#v, want failed then completed ordering", storedRuns)
	}

	storedState, err := reopened.GetScanSchedulerState(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("GetScanSchedulerState() error = %v", err)
	}
	if storedState.OwnerID != state.OwnerID || storedState.BatchStartedAt == nil {
		t.Fatalf("storedState = %#v, want %#v", storedState, state)
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
