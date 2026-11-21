package sqlite

import (
	"context"
	"fmt"
	"sync"
	"testing"

	domain "regixtry/internal/domain/regixtry"
)

// TestStoreConcurrentPublishAndDeleteNeverReturnsDatabaseLocked pins that
// write transactions take the SQLite write lock up front: DeleteManifestByDigest
// reads (tag names) before it writes, and a deferred transaction that has to
// upgrade from read to write after another writer committed fails at once with
// SQLITE_BUSY in WAL mode -- busy_timeout cannot retry that upgrade. Every call
// below must succeed; a NotFound or lock error is a real failure.
func TestStoreConcurrentPublishAndDeleteNeverReturnsDatabaseLocked(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	const (
		workers    = 8
		iterations = 25
	)

	repo := domain.MustParseRepositoryRef("library/alpine")
	ctx := context.Background()

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		failures []string
	)
	fail := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		failures = append(failures, fmt.Sprintf(format, args...))
	}

	for worker := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for iteration := range iterations {
				payload := fmt.Sprintf(`{"schemaVersion":2,"worker":%d,"iteration":%d}`, worker, iteration)
				manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(payload), nil, nil, nil, nil)
				if err != nil {
					fail("worker %d iteration %d NewManifest() error = %v", worker, iteration, err)
					return
				}

				tag := fmt.Sprintf("w%d-i%d", worker, iteration)
				if err := store.PublishManifest(ctx, "tenant-a", repo, tag, manifest, nil); err != nil {
					fail("worker %d iteration %d PublishManifest() error = %v", worker, iteration, err)
					return
				}

				if _, err := store.DeleteManifestByDigest(ctx, "tenant-a", repo, manifest.Digest); err != nil {
					fail("worker %d iteration %d DeleteManifestByDigest() error = %v", worker, iteration, err)
					return
				}
			}
		}()
	}

	wg.Wait()

	if len(failures) > 0 {
		shown := failures
		if len(shown) > 3 {
			shown = shown[:3]
		}
		t.Fatalf("%d of %d workers failed, first errors: %q", len(failures), workers, shown)
	}
}
