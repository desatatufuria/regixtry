package regixtry

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
)

// hookedMetadata lets a test run code at an exact point of the publish path.
type hookedMetadata struct {
	ports.MetadataStore
	beforePublish func()
	once          sync.Once
}

func (h *hookedMetadata) PublishManifestVerified(ctx context.Context, tenant string, repository domain.RepositoryRef, tag string, manifest domain.Manifest, blobs []domain.Descriptor, present ports.BlobPresence) error {
	h.once.Do(func() {
		if h.beforePublish != nil {
			h.beforePublish()
		}
	})
	return h.MetadataStore.PublishManifestVerified(ctx, tenant, repository, tag, manifest, blobs, present)
}

// hookedBlobs lets a test run code at exact points of the blob store: right
// after GC removed a blob file (inside the reclaim transaction), and after an
// upload committed a blob file.
type hookedBlobs struct {
	ports.BlobStore
	afterDelete func()
	committed   chan struct{}
}

func (h *hookedBlobs) DeleteBlobIfOlderThan(ctx context.Context, digest domain.Digest, cutoff time.Time) (ports.BlobReclaimOutcome, error) {
	outcome, err := h.BlobStore.DeleteBlobIfOlderThan(ctx, digest, cutoff)
	if h.afterDelete != nil {
		h.afterDelete()
	}
	return outcome, err
}

func (h *hookedBlobs) CommitUpload(ctx context.Context, uploadID string, expected domain.Digest) (domain.Descriptor, error) {
	descriptor, err := h.BlobStore.CommitUpload(ctx, uploadID, expected)
	if h.committed != nil {
		close(h.committed)
		h.committed = nil
	}
	return descriptor, err
}

type gcRaceEnv struct {
	service *Service
	blobs   ports.BlobStore
	store   *metadata.Store
	hooked  *hookedMetadata
	hblobs  *hookedBlobs
}

func newGCRaceEnv(t *testing.T) *gcRaceEnv {
	t.Helper()

	rootDir := t.TempDir()
	blobs, err := fsblob.New(filepath.Join(rootDir, "blobs"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}
	store, err := metadata.New(filepath.Join(rootDir, "registry.db"))
	if err != nil {
		t.Fatalf("sqlite.New() error = %v", err)
	}
	hooked := &hookedMetadata{MetadataStore: store}
	hblobs := &hookedBlobs{BlobStore: blobs}
	service := NewService(hblobs, hooked, allowAllAccessController{}, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	service.SetGCDeleteEnabled(true)
	t.Cleanup(func() {
		service.WaitForBackgroundWork()
		_ = store.Close()
	})

	return &gcRaceEnv{service: service, blobs: blobs, store: store, hooked: hooked, hblobs: hblobs}
}

func (e *gcRaceEnv) upload(t *testing.T, repo string, payload []byte) domain.Digest {
	t.Helper()
	ctx := context.Background()
	started, err := e.service.BeginUpload(ctx, repo)
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	blob, err := e.service.CompleteUpload(ctx, repo, started.ID, digestForTest(payload), strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	return domain.MustParseDigest(blob.Digest)
}

func (e *gcRaceEnv) manifestFor(digest domain.Digest, size int) []byte {
	return []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"%s","size":%d},"layers":[]}`, digest, size))
}

// reportAfterGrace computes a GC report as of a clock past the grace window.
func (e *gcRaceEnv) reportAfterGrace(t *testing.T) ports.GCReportDetail {
	t.Helper()
	e.service.now = func() time.Time { return time.Now().UTC().Add(gcGraceWindow + time.Hour) }
	detail, err := e.service.ComputeGCReport(context.Background())
	if err != nil {
		t.Fatalf("ComputeGCReport() error = %v", err)
	}
	return detail
}

// TestGCRacingManifestPublishNeverLeavesMissingBlob forces the interleaving
// "publish checked the blob, GC then unlinked it, publish then committed":
// either the publish is refused, or every blob the manifest references exists.
func TestGCRacingManifestPublishNeverLeavesMissingBlob(t *testing.T) {
	t.Parallel()

	env := newGCRaceEnv(t)
	ctx := context.Background()
	layer := []byte("racy-config-blob")
	digest := env.upload(t, "library/alpine", layer)
	report := env.reportAfterGrace(t)
	if report.Report.CandidateCount != 1 {
		t.Fatalf("candidates = %d, want the unreferenced blob", report.Report.CandidateCount)
	}

	env.hooked.beforePublish = func() {
		if _, err := env.service.DeleteByGCReport(ctx, report.Report.ID); err != nil {
			t.Errorf("DeleteByGCReport() error = %v", err)
		}
	}

	_, err := env.service.PublishManifest(ctx, "library/alpine", "latest", "application/vnd.oci.image.manifest.v1+json", env.manifestFor(digest, len(layer)))
	if err == nil {
		if exists, _ := env.blobs.BlobExists(ctx, digest); !exists {
			t.Fatalf("manifest published but its blob %s is gone: data loss", digest)
		}
	}
}

// TestUploadRecreatingBlobDuringGCKeepsItsLink forces "GC removed the file,
// the same digest was uploaded again (file recreated), GC then dropped the
// links": the new upload must stay readable and referenceable. The re-upload
// runs concurrently and its link write queues behind the reclaim transaction,
// so it lands after the stale links were dropped. Covers the same repository
// and a second repository pushing the same layer.
func TestUploadRecreatingBlobDuringGCKeepsItsLink(t *testing.T) {
	t.Parallel()

	for _, repo := range []string{"library/alpine", "library/busybox"} {
		t.Run(repo, func(t *testing.T) {
			t.Parallel()

			env := newGCRaceEnv(t)
			ctx := context.Background()
			layer := []byte("recreated-blob")
			digest := env.upload(t, "library/alpine", layer)
			report := env.reportAfterGrace(t)

			// Prepare the second push up to (not including) its commit.
			started, err := env.service.BeginUpload(ctx, repo)
			if err != nil {
				t.Fatalf("BeginUpload() error = %v", err)
			}
			if _, err := env.service.AppendUpload(ctx, repo, started.ID, strings.NewReader(string(layer))); err != nil {
				t.Fatalf("AppendUpload() error = %v", err)
			}

			env.hblobs.committed = make(chan struct{})
			committed := env.hblobs.committed
			finished := make(chan error, 1)
			env.hblobs.afterDelete = func() {
				env.hblobs.afterDelete = nil
				go func() {
					_, err := env.service.CompleteUpload(ctx, repo, started.ID, digest.String(), nil)
					finished <- err
				}()
				<-committed // the file is recreated before GC drops the links
			}
			if _, err := env.service.DeleteByGCReport(ctx, report.Report.ID); err != nil {
				t.Fatalf("DeleteByGCReport() error = %v", err)
			}
			if err := <-finished; err != nil {
				t.Fatalf("CompleteUpload() error = %v", err)
			}

			if _, _, err := env.service.OpenBlob(ctx, repo, digest.String()); err != nil {
				t.Fatalf("OpenBlob() after re-upload error = %v (new link lost)", err)
			}
			if _, err := env.service.PublishManifest(ctx, repo, "latest", "application/vnd.oci.image.manifest.v1+json", env.manifestFor(digest, len(layer))); err != nil {
				t.Fatalf("PublishManifest() after re-upload error = %v (new link lost)", err)
			}
		})
	}
}

// TestGCRetainsBlobReferencedByManifestPublishedAfterReport pins the existing
// behavior the reclaim transaction must keep: a manifest published between
// the report and the delete retains the blob.
func TestGCRetainsBlobReferencedByManifestPublishedAfterReport(t *testing.T) {
	t.Parallel()

	env := newGCRaceEnv(t)
	ctx := context.Background()
	layer := []byte("published-after-report")
	digest := env.upload(t, "library/alpine", layer)
	report := env.reportAfterGrace(t)

	if _, err := env.service.PublishManifest(ctx, "library/alpine", "latest", "application/vnd.oci.image.manifest.v1+json", env.manifestFor(digest, len(layer))); err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}
	detail, err := env.service.DeleteByGCReport(ctx, report.Report.ID)
	if err != nil {
		t.Fatalf("DeleteByGCReport() error = %v", err)
	}
	if detail.Report.DeletedCount != 0 {
		t.Fatalf("DeletedCount = %d, want 0", detail.Report.DeletedCount)
	}
	if exists, _ := env.blobs.BlobExists(ctx, digest); !exists {
		t.Fatal("referenced blob was deleted")
	}
}
