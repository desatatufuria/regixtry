package releases

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHubReleaseResolveLatestAndTag(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_, _ = fmt.Fprint(w, `{"tag_name":"v1.2.3","assets":[{"browser_download_url":"https://example.invalid/regixtry_1.2.3_linux_amd64.tar.gz"},{"browser_download_url":"https://example.invalid/regixtry_1.2.3_checksums.txt"}]}`)
		case "/tags/v1.2.2":
			_, _ = fmt.Fprint(w, `{"tag_name":"v1.2.2","assets":[{"browser_download_url":"https://example.invalid/regixtry_1.2.2_linux_amd64.tar.gz"},{"browser_download_url":"https://example.invalid/regixtry_1.2.2_checksums.txt"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewGitHubClient(server.URL)
	latest, err := client.Resolve(context.Background(), "", "linux", "amd64")
	if err != nil {
		t.Fatalf("Resolve(latest) error = %v", err)
	}
	if latest.Tag != "v1.2.3" || latest.Version != "1.2.3" {
		t.Fatalf("Resolve(latest) = %#v, want v1.2.3 / 1.2.3", latest)
	}

	resolved, err := client.Resolve(context.Background(), "v1.2.2", "linux", "amd64")
	if err != nil {
		t.Fatalf("Resolve(tag) error = %v", err)
	}
	if resolved.Tag != "v1.2.2" || !strings.HasSuffix(resolved.ArchiveURL, "1.2.2_linux_amd64.tar.gz") {
		t.Fatalf("Resolve(tag) = %#v, want explicit tag asset", resolved)
	}
}

func TestGitHubReleaseDownloadVerifiedBinaryRejectsWrongChecksum(t *testing.T) {
	t.Parallel()

	server, asset := testReleaseServer(t, true, false)
	defer server.Close()

	client := NewGitHubClient(server.URL)
	dir := t.TempDir()
	_, err := client.DownloadVerifiedBinary(context.Background(), asset, dir, nil)
	if err == nil || !strings.Contains(err.Error(), "checksum verification failed") {
		t.Fatalf("DownloadVerifiedBinary() error = %v, want checksum failure", err)
	}
}

func TestGitHubReleaseDownloadVerifiedBinaryRejectsMultiEntryArchive(t *testing.T) {
	t.Parallel()

	server, asset := testReleaseServer(t, false, true)
	defer server.Close()

	client := NewGitHubClient(server.URL)
	dir := t.TempDir()
	_, err := client.DownloadVerifiedBinary(context.Background(), asset, dir, nil)
	if err == nil || !strings.Contains(err.Error(), "archive must contain exactly one regixtry entry") {
		t.Fatalf("DownloadVerifiedBinary() error = %v, want archive validation failure", err)
	}
}

func testReleaseServer(t *testing.T, wrongChecksum bool, multiEntry bool) (*httptest.Server, ReleaseAsset) {
	t.Helper()

	root := t.TempDir()
	archiveName := "regixtry_1.2.3_linux_amd64.tar.gz"
	checksumsName := "regixtry_1.2.3_checksums.txt"
	archivePath := filepath.Join(root, archiveName)
	checksumsPath := filepath.Join(root, checksumsName)
	writeArchive(t, archivePath, multiEntry)
	body, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("ReadFile(archive) error = %v", err)
	}
	sum := sha256.Sum256(body)
	checksum := hex.EncodeToString(sum[:])
	if wrongChecksum {
		checksum = strings.Repeat("0", 64)
	}
	if err := os.WriteFile(checksumsPath, []byte(checksum+"  "+archiveName+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(checksums) error = %v", err)
	}

	server := httptest.NewServer(http.FileServer(http.Dir(root)))
	asset := ReleaseAsset{
		Tag:          "v1.2.3",
		Version:      "1.2.3",
		ArchiveURL:   server.URL + "/" + archiveName,
		ChecksumsURL: server.URL + "/" + checksumsName,
		ArchiveName:  archiveName,
	}
	return server, asset
}

func writeArchive(t *testing.T, path string, multiEntry bool) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(archive) error = %v", err)
	}
	defer file.Close()
	gzWriter := gzip.NewWriter(file)
	defer gzWriter.Close()
	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()
	entries := map[string]string{"regixtry": "binary"}
	if multiEntry {
		entries["README.txt"] = "extra"
	}
	for name, body := range entries {
		header := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("WriteHeader(%s) error = %v", name, err)
		}
		if _, err := tarWriter.Write([]byte(body)); err != nil {
			t.Fatalf("Write(%s) error = %v", name, err)
		}
	}
}
