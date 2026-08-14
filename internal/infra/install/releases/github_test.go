package releases

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultReleasesAPIURLPointsToRegixtryRepository guards against a
// pre-rename leftover: this repository's actual GitHub remote is
// desatatufuria/regixtry (the project used to be called "workspace"), so the
// installer's default release resolver must resolve releases from
// desatatufuria/regixtry, not the old desatatufuria/workspace path.
func TestDefaultReleasesAPIURLPointsToRegixtryRepository(t *testing.T) {
	t.Parallel()

	want := "https://api.github.com/repos/desatatufuria/regixtry/releases"
	if defaultReleasesAPIURL != want {
		t.Fatalf("defaultReleasesAPIURL = %q, want %q", defaultReleasesAPIURL, want)
	}
}

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

func TestGitHubReleaseDownloadVerifiedBinaryReportsByteProgress(t *testing.T) {
	t.Parallel()

	server, asset := testLargeReleaseServer(t)
	defer server.Close()

	client := NewGitHubClient(server.URL)
	dir := t.TempDir()

	var events []DownloadProgress
	if _, err := client.DownloadVerifiedBinary(context.Background(), asset, dir, func(p DownloadProgress) {
		events = append(events, p)
	}); err != nil {
		t.Fatalf("DownloadVerifiedBinary() error = %v", err)
	}

	var byteEvents []DownloadProgress
	for _, event := range events {
		if event.Stage == "download" && event.TotalBytes > 0 {
			byteEvents = append(byteEvents, event)
		}
	}
	if len(byteEvents) < 2 {
		t.Fatalf("byteEvents = %#v, want at least 2 byte-level download progress events for a large archive", byteEvents)
	}
	for i := 1; i < len(byteEvents); i++ {
		if byteEvents[i].BytesRead < byteEvents[i-1].BytesRead {
			t.Fatalf("byteEvents = %#v, want non-decreasing BytesRead", byteEvents)
		}
		if byteEvents[i].TotalBytes != byteEvents[0].TotalBytes {
			t.Fatalf("byteEvents = %#v, want stable TotalBytes across the transfer", byteEvents)
		}
	}
	last := byteEvents[len(byteEvents)-1]
	if last.BytesRead != last.TotalBytes {
		t.Fatalf("last byte event = %#v, want BytesRead == TotalBytes at completion", last)
	}
}

func testLargeReleaseServer(t *testing.T) (*httptest.Server, ReleaseAsset) {
	t.Helper()

	root := t.TempDir()
	archiveName := "regixtry_1.2.3_linux_amd64.tar.gz"
	checksumsName := "regixtry_1.2.3_checksums.txt"
	archivePath := filepath.Join(root, archiveName)
	checksumsPath := filepath.Join(root, checksumsName)
	writeLargeArchive(t, archivePath)
	body, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("ReadFile(archive) error = %v", err)
	}
	sum := sha256.Sum256(body)
	checksum := hex.EncodeToString(sum[:])
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

func writeLargeArchive(t *testing.T, path string) {
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
	// High-entropy body so gzip cannot shrink it away: the transferred
	// archive must stay large enough on the wire to force multiple
	// io.Copy Read() calls during the test.
	body := make([]byte, 512*1024)
	seededRand := rand.New(rand.NewSource(1))
	if _, err := seededRand.Read(body); err != nil {
		t.Fatalf("rand.Read(body) error = %v", err)
	}
	header := &tar.Header{Name: "regixtry", Mode: 0o755, Size: int64(len(body))}
	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatalf("WriteHeader(regixtry) error = %v", err)
	}
	if _, err := tarWriter.Write(body); err != nil {
		t.Fatalf("Write(regixtry) error = %v", err)
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
