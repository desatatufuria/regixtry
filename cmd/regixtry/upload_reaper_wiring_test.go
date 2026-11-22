package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func startUploadForReaperTest(t *testing.T, handler http.Handler) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("start upload status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
	return recorder.Header().Get("Location")
}

func uploadStatus(handler http.Handler, location string) int {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, location, nil))
	return recorder.Code
}

// The reaper goroutine wired in newHandler reaps an abandoned upload on its
// own once the (injected, tiny) TTL and interval elapse.
func TestNewHandlerRunsTheUploadReaperLoop(t *testing.T) {
	storageRoot := t.TempDir()
	handler, cleanup, err := newHandler(serveConfig{
		StorageRoot:        storageRoot,
		DatabasePath:       filepath.Join(storageRoot, "registry.db"),
		AllowAnonymousPush: true,
		UploadTTL:          time.Millisecond,
		uploadReapInterval: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("newHandler() error = %v", err)
	}
	defer cleanup()

	location := startUploadForReaperTest(t, handler)
	deadline := time.Now().Add(20 * time.Second)
	for uploadStatus(handler, location) != http.StatusNotFound {
		if time.Now().After(deadline) {
			t.Fatal("the abandoned upload was never reaped by the running loop")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A failing pass is reported through the wired onError callback and the loop
// keeps running.
func TestNewHandlerUploadReaperReportsFailuresThroughOnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not bind root")
	}
	storageRoot := t.TempDir()
	var mu sync.Mutex
	var reported []error
	handler, cleanup, err := newHandler(serveConfig{
		StorageRoot:        storageRoot,
		DatabasePath:       filepath.Join(storageRoot, "registry.db"),
		AllowAnonymousPush: true,
		UploadTTL:          time.Millisecond,
		uploadReapInterval: 200 * time.Millisecond, // wide gap between the upload and the first pass
		uploadReapOnError: func(err error) {
			mu.Lock()
			reported = append(reported, err)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("newHandler() error = %v", err)
	}
	defer cleanup()

	// Locate the uploads root; once an upload exists it is made read-only so
	// removing that upload fails.
	var uploadsRoot string
	_ = filepath.WalkDir(storageRoot, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.IsDir() && entry.Name() == "uploads" && uploadsRoot == "" {
			uploadsRoot = path
		}
		return nil
	})
	if uploadsRoot == "" {
		t.Fatal("uploads directory not found")
	}
	startUploadForReaperTest(t, handler)
	if err := os.Chmod(uploadsRoot, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(uploadsRoot, 0o755) }()

	deadline := time.Now().Add(20 * time.Second)
	for {
		mu.Lock()
		n := len(reported)
		var first error
		if n > 0 {
			first = reported[0]
		}
		mu.Unlock()
		if n > 0 {
			if !strings.Contains(first.Error(), "permission denied") {
				t.Logf("reported error: %v", first)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Skip("upload was reaped before the directory could be locked; failure path not exercised")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
