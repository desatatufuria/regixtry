package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureLog redirects the standard logger for one test. Tests using it must
// not run in parallel.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &buf
}

func permissionWarnings(logs string) []string {
	var lines []string
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "other local users") {
			lines = append(lines, line)
		}
	}
	return lines
}

func startHandlerForPermissionTest(t *testing.T, storageRoot string, databasePath string) {
	t.Helper()
	_, cleanup, err := newHandler(serveConfig{
		StorageRoot:  storageRoot,
		DatabasePath: databasePath,
	})
	if err != nil {
		t.Fatalf("newHandler() error = %v", err)
	}
	cleanup()
}

func TestServeWarnsOnceAboutWorldAccessibleExistingStorage(t *testing.T) {
	storageRoot := filepath.Join(t.TempDir(), "data")
	databasePath := filepath.Join(storageRoot, "metadata.db")
	if err := os.MkdirAll(storageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(storageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(databasePath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(databasePath, 0o644); err != nil {
		t.Fatal(err)
	}

	logs := captureLog(t)
	startHandlerForPermissionTest(t, storageRoot, databasePath)

	warnings := permissionWarnings(logs.String())
	if len(warnings) != 1 {
		t.Fatalf("got %d permission warnings, want exactly 1:\n%s", len(warnings), logs)
	}
	for _, want := range []string{storageRoot, databasePath, "chmod o-rwx " + storageRoot + " " + databasePath} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("warning %q does not contain %q", warnings[0], want)
		}
	}
	// The startup check only reports: it never changes an existing tree.
	for path, want := range map[string]os.FileMode{storageRoot: 0o755, databasePath: 0o644} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("mode of %s = %#o, want unchanged %#o", path, got, want)
		}
	}
}

func TestServeDoesNotWarnForTightOrNewStorage(t *testing.T) {
	t.Run("new storage root is created without other access and no warning", func(t *testing.T) {
		storageRoot := filepath.Join(t.TempDir(), "fresh")
		databasePath := filepath.Join(storageRoot, "metadata.db")
		logs := captureLog(t)
		startHandlerForPermissionTest(t, storageRoot, databasePath)
		if got := permissionWarnings(logs.String()); len(got) != 0 {
			t.Fatalf("unexpected warnings for a fresh tree: %v", got)
		}
		for _, path := range []string{storageRoot, databasePath, filepath.Join(storageRoot, "content")} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm()&0o007 != 0 {
				t.Errorf("%s created accessible to other users: %#o", path, info.Mode().Perm())
			}
		}
	})

	t.Run("already tight existing tree", func(t *testing.T) {
		storageRoot := filepath.Join(t.TempDir(), "tight")
		databasePath := filepath.Join(storageRoot, "metadata.db")
		if err := os.MkdirAll(storageRoot, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(databasePath, nil, 0o640); err != nil {
			t.Fatal(err)
		}
		logs := captureLog(t)
		startHandlerForPermissionTest(t, storageRoot, databasePath)
		if got := permissionWarnings(logs.String()); len(got) != 0 {
			t.Fatalf("unexpected warnings for a tight tree: %v", got)
		}
	})
}
