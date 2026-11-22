package regixtry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
)

// TestCommitsRacingGCNeverLoseAPublishedBlob composes the storage-limits
// commit (rename / dedup into the content store) with the GC age check: fresh
// commits, dedup commits onto an OLD existing blob, and commits of an upload
// that sat idle past the grace window race a looping GC while manifests
// referencing those blobs are published. Invariant: every manifest that was
// published has its blob on disk and linked in its repository. A bounded run
// stays under 3 s; REGIXTRY_COMPOSITION_STRESS_SECONDS=N runs it for N seconds.
func TestCommitsRacingGCNeverLoseAPublishedBlob(t *testing.T) {
	t.Parallel()

	duration := time.Duration(0)
	if raw := os.Getenv("REGIXTRY_COMPOSITION_STRESS_SECONDS"); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("bad REGIXTRY_COMPOSITION_STRESS_SECONDS %q", raw)
		}
		duration = time.Duration(seconds) * time.Second
	}
	const iterations = 3

	ctx := context.Background()
	rootDir := t.TempDir()
	blobsDir := filepath.Join(rootDir, "store")
	blobs, err := fsblob.New(blobsDir)
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}
	store, err := metadata.New(filepath.Join(rootDir, "registry.db"))
	if err != nil {
		t.Fatalf("sqlite.New() error = %v", err)
	}
	service := NewService(blobs, store, allowAllAccessController{}, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	service.SetGCDeleteEnabled(true)
	t.Cleanup(func() {
		service.WaitForBackgroundWork()
		_ = store.Close()
	})

	age := func(path string) {
		old := time.Now().Add(-48 * time.Hour)
		_ = os.Chtimes(path, old, old)
	}
	blobFile := func(d domain.Digest) string { return filepath.Join(blobsDir, "blobs", d.Algorithm(), d.Encoded()) }

	type published struct {
		repo   string
		digest domain.Digest
	}
	var (
		mu        sync.Mutex
		results   []published
		stop      atomic.Bool
		gcWG      sync.WaitGroup
		workersWG sync.WaitGroup
	)

	gcWG.Add(1)
	go func() {
		defer gcWG.Done()
		for !stop.Load() {
			report, err := service.ComputeGCReport(ctx)
			if err != nil {
				t.Errorf("ComputeGCReport() error = %v", err)
				return
			}
			if _, err := service.DeleteByGCReport(ctx, report.Report.ID); err != nil {
				t.Errorf("DeleteByGCReport() error = %v", err)
				return
			}
		}
	}()

	deadline := time.Now().Add(duration)
	const workers = 6
	for w := 0; w < workers; w++ {
		workersWG.Add(1)
		go func(w int) {
			defer workersWG.Done()
			repo := fmt.Sprintf("library/repo%d", w%3)
			shared := []byte(fmt.Sprintf("shared-layer-%d", w%2))
			for i := 0; duration > 0 && time.Now().Before(deadline) || duration == 0 && i < iterations; i++ {
				var payload []byte
				idle := false
				switch i % 3 {
				case 0: // fresh content
					payload = []byte(fmt.Sprintf("fresh-%d-%d", w, i))
				case 1: // dedup onto an OLD blob that already exists
					payload = shared
					age(blobFile(domain.MustParseDigest(digestForTest(payload))))
				default: // upload idle past the grace window before commit
					payload = []byte(fmt.Sprintf("idle-%d-%d", w, i))
					idle = true
				}
				started, err := service.BeginUpload(ctx, repo)
				if err != nil {
					t.Errorf("BeginUpload() error = %v", err)
					return
				}
				if _, err := service.AppendUpload(ctx, repo, started.ID, strings.NewReader(string(payload))); err != nil {
					t.Errorf("AppendUpload() error = %v", err)
					return
				}
				if idle {
					age(filepath.Join(blobsDir, "uploads", started.ID, "data"))
				}
				blob, err := service.CompleteUpload(ctx, repo, started.ID, digestForTest(payload), nil)
				if err != nil {
					t.Errorf("CompleteUpload() error = %v", err)
					return
				}
				digest := domain.MustParseDigest(blob.Digest)
				// A committed blob is fresh (inside the 24h grace window) no
				// matter how old its upload or the deduplicated blob was, so
				// a concurrent GC must not have reclaimed it.
				if exists, err := blobs.BlobExists(ctx, digest); err != nil || !exists {
					t.Errorf("CompleteUpload succeeded in %s but blob %s is gone (exists=%v err=%v): lost update", repo, digest, exists, err)
					return
				}
				manifest := []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"%s","size":%d},"layers":[]}`, digest, len(payload)))
				// A publish refused because GC reclaimed the unreferenced blob
				// first is allowed; a publish that succeeds must be intact.
				if _, err := service.PublishManifest(ctx, repo, fmt.Sprintf("t%d-%d", w, i), "application/vnd.oci.image.manifest.v1+json", manifest); err != nil {
					continue
				}
				mu.Lock()
				results = append(results, published{repo: repo, digest: digest})
				mu.Unlock()
			}
		}(w)
	}
	workersWG.Wait()
	stop.Store(true)
	gcWG.Wait()

	if len(results) == 0 {
		t.Fatal("no manifest was published; the test proves nothing")
	}
	for _, item := range results {
		if exists, err := blobs.BlobExists(ctx, item.digest); err != nil || !exists {
			t.Fatalf("published manifest in %s references %s but the blob is gone (exists=%v err=%v): lost update", item.repo, item.digest, exists, err)
		}
		repository, err := domain.ParseRepositoryRef(item.repo)
		if err != nil {
			t.Fatal(err)
		}
		linked, err := store.IsBlobLinked(ctx, "tenant-a", repository, item.digest)
		if err != nil || !linked {
			t.Fatalf("published manifest in %s references %s but the repository link is missing (linked=%v err=%v)", item.repo, item.digest, linked, err)
		}
	}
	t.Logf("published %d manifests, all blobs intact", len(results))
}

// A dedup commit (the blob file already exists, the upload is discarded) must
// still link the blob into the pushing repository, and only that one.
func TestDedupCommitLinksBlobIntoPushingRepository(t *testing.T) {
	t.Parallel()

	env := newGCRaceEnv(t)
	ctx := context.Background()
	layer := []byte("dedup-link-layer")
	digest := env.upload(t, "library/alpine", layer)

	other := domain.MustParseRepositoryRef("library/busybox")
	if linked, err := env.store.IsBlobLinked(ctx, "tenant-a", other, digest); err != nil || linked {
		t.Fatalf("before push: linked=%v err=%v, want unlinked", linked, err)
	}
	if _, _, err := env.service.OpenBlob(ctx, "library/busybox", digest.String()); err == nil {
		t.Fatal("OpenBlob() served a blob another repository holds")
	}

	if got := env.upload(t, "library/busybox", layer); got != digest {
		t.Fatalf("digest = %s, want %s", got, digest)
	}
	if linked, err := env.store.IsBlobLinked(ctx, "tenant-a", other, digest); err != nil || !linked {
		t.Fatalf("after dedup push: linked=%v err=%v, want linked", linked, err)
	}
	if _, _, err := env.service.OpenBlob(ctx, "library/busybox", digest.String()); err != nil {
		t.Fatalf("OpenBlob() after dedup push error = %v", err)
	}
}
