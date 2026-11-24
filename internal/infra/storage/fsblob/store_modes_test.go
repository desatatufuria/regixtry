package fsblob

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	domain "regixtry/internal/domain/regixtry"
)

const umaskChildEnv = "REGIXTRY_FSBLOB_MODES_UMASK"

// TestStoreCreationModesAreNotAccessibleToOthers pins the modes of everything
// the store creates. The umask is process-wide state, so each umask runs in a
// re-executed child test binary: nothing leaks into other tests.
func TestStoreCreationModesAreNotAccessibleToOthers(t *testing.T) {
	if raw := os.Getenv(umaskChildEnv); raw != "" {
		mask, err := strconv.ParseUint(raw, 8, 32)
		if err != nil {
			t.Fatalf("bad %s=%q: %v", umaskChildEnv, raw, err)
		}
		syscall.Umask(int(mask))
		assertStoreCreationModes(t)
		return
	}

	for _, mask := range []string{"022", "0"} {
		t.Run("umask "+mask, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestStoreCreationModesAreNotAccessibleToOthers$", "-test.count=1")
			cmd.Env = append(os.Environ(), umaskChildEnv+"="+mask)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("child under umask %s failed: %v\n%s", mask, err, out)
			}
		})
	}
}

func assertStoreCreationModes(t *testing.T) {
	t.Helper()

	// A not-yet-existing root: New creates it and every intermediate directory.
	root := filepath.Join(t.TempDir(), "state", "content")
	store, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	upload, err := store.BeginUpload(context.Background(), domain.MustParseRepositoryRef("library/alpine"))
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	payload := []byte("modes-payload")
	digest := domain.DigestFromBytes(payload)
	if _, err := store.PutUploadChunk(context.Background(), upload.ID, strings.NewReader(string(payload))); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}

	assertMode(t, filepath.Dir(root), 0o750)
	assertMode(t, root, 0o750)
	assertMode(t, store.blobsRoot(), 0o750)
	assertMode(t, store.uploadsRoot(), 0o750)
	assertMode(t, store.uploadDir(upload.ID), 0o750)
	assertMode(t, store.uploadFile(upload.ID), 0o640)
	assertMode(t, store.uploadStateFile(upload.ID), 0o640)

	if _, err := store.CommitUpload(context.Background(), upload.ID, digest); err != nil {
		t.Fatalf("CommitUpload() error = %v", err)
	}
	assertMode(t, filepath.Dir(store.blobPath(digest)), 0o750)
	assertMode(t, store.blobPath(digest), 0o440)

	// The copy fallback (rename refused) must yield the same blob mode.
	store.renameFile = func(string, string) error { return syscall.EXDEV }
	second := beginUpload(t, store)
	other := []byte("modes-payload-copy")
	otherDigest := domain.DigestFromBytes(other)
	if _, err := store.PutUploadChunk(context.Background(), second.ID, strings.NewReader(string(other))); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}
	if _, err := store.CommitUpload(context.Background(), second.ID, otherDigest); err != nil {
		t.Fatalf("CommitUpload() via copy fallback error = %v", err)
	}
	assertMode(t, filepath.Dir(store.blobPath(otherDigest)), 0o750)
	assertMode(t, store.blobPath(otherDigest), 0o440)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode of %s = %#o, want %#o", path, got, want)
	}
}

// An existing tree keeps whatever modes it has: New must never chmod it.
func TestNewDoesNotChangeExistingTreeModes(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "content")
	for _, dir := range []string{root, filepath.Join(root, "blobs"), filepath.Join(root, "blobs", "sha256"), filepath.Join(root, "uploads")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := New(root); err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, dir := range []string{root, filepath.Join(root, "blobs"), filepath.Join(root, "uploads")} {
		assertMode(t, dir, 0o755)
	}
}
