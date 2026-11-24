package sqlite

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

const umaskChildEnv = "REGIXTRY_SQLITE_MODES_UMASK"

// TestNewCreatesDatabaseFilesWithoutOtherAccess pins the modes of metadata.db
// and its WAL sidecars. The umask is process-wide, so each value runs in a
// re-executed child test binary and nothing leaks into other tests.
func TestNewCreatesDatabaseFilesWithoutOtherAccess(t *testing.T) {
	if raw := os.Getenv(umaskChildEnv); raw != "" {
		mask, err := strconv.ParseUint(raw, 8, 32)
		if err != nil {
			t.Fatalf("bad %s=%q: %v", umaskChildEnv, raw, err)
		}
		syscall.Umask(int(mask))

		path := filepath.Join(t.TempDir(), "metadata.db")
		store, err := New(path)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer store.Close()
		for _, name := range []string{path, path + "-wal", path + "-shm"} {
			info, err := os.Stat(name)
			if err != nil {
				t.Fatalf("stat %s: %v", name, err)
			}
			if got := info.Mode().Perm(); got != 0o640 {
				t.Errorf("mode of %s = %#o, want 0640", filepath.Base(name), got)
			}
		}
		return
	}

	for _, mask := range []string{"022", "0"} {
		t.Run("umask "+mask, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestNewCreatesDatabaseFilesWithoutOtherAccess$", "-test.count=1")
			cmd.Env = append(os.Environ(), umaskChildEnv+"="+mask)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("child under umask %s failed: %v\n%s", mask, err, out)
			}
		})
	}
}

// An existing database keeps its mode: New never chmods an existing file.
func TestNewKeepsExistingDatabaseMode(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "metadata.db")
	first, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	second, err := New(path)
	if err != nil {
		t.Fatalf("New() on existing database error = %v", err)
	}
	defer second.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("existing database mode = %#o, want 0644 (unchanged)", got)
	}
}
