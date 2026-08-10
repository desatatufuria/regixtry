package trivy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveReleasePrefersPrimaryArchiveOverSigstoreSidecar(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tags/v0.73.0" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, `{"tag_name":"v0.73.0","assets":[{"name":"trivy_0.73.0_Linux-64bit.tar.gz.sigstore.json","browser_download_url":"https://example.invalid/trivy_0.73.0_Linux-64bit.tar.gz.sigstore.json"},{"name":"trivy_0.73.0_Linux-64bit.tar.gz","browser_download_url":"https://example.invalid/trivy_0.73.0_Linux-64bit.tar.gz"},{"name":"trivy_0.73.0_checksums.txt","browser_download_url":"https://example.invalid/trivy_0.73.0_checksums.txt"}]}`)
	}))
	defer server.Close()

	asset, err := githubReleaseClient{baseAPI: server.URL, client: server.Client()}.ResolveRelease(context.Background(), "0.73.0")
	if err != nil {
		t.Fatalf("ResolveRelease() error = %v", err)
	}
	if asset.ArchiveName != "trivy_0.73.0_Linux-64bit.tar.gz" {
		t.Fatalf("ArchiveName = %q, want main archive asset", asset.ArchiveName)
	}
	if strings.HasSuffix(asset.ArchiveURL, ".sigstore.json") {
		t.Fatalf("ArchiveURL = %q, want primary archive instead of sidecar", asset.ArchiveURL)
	}
}

func TestVerifyArchiveChecksumRejectsMismatchesAndExtractsManagedBinary(t *testing.T) {
	t.Parallel()

	archiveBody := makeTrivyArchive(t, "trivy-binary")
	archivePath := filepath.Join(t.TempDir(), "trivy_0.57.1_Linux-64bit.tar.gz")
	if err := os.WriteFile(archivePath, archiveBody, 0o600); err != nil {
		t.Fatalf("WriteFile(archive) error = %v", err)
	}
	checksum := sha256.Sum256(archiveBody)
	validChecksums := []byte(hex.EncodeToString(checksum[:]) + "  " + filepath.Base(archivePath) + "\n")
	if err := verifyArchiveChecksum(archivePath, filepath.Base(archivePath), validChecksums); err != nil {
		t.Fatalf("verifyArchiveChecksum() error = %v", err)
	}
	withSidecarEntry := []byte(strings.Join([]string{
		strings.Repeat("f", 64) + "  " + filepath.Base(archivePath) + ".sigstore.json",
		hex.EncodeToString(checksum[:]) + "  " + filepath.Base(archivePath),
	}, "\n") + "\n")
	if err := verifyArchiveChecksum(archivePath, filepath.Base(archivePath), withSidecarEntry); err != nil {
		t.Fatalf("verifyArchiveChecksum() with sidecar entry error = %v", err)
	}

	if err := verifyArchiveChecksum(archivePath, filepath.Base(archivePath), []byte(strings.Repeat("0", 64)+"  "+filepath.Base(archivePath)+"\n")); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("verifyArchiveChecksum() error = %v, want checksum mismatch", err)
	}

	binaryPath, err := extractTrivyBinary(context.Background(), archivePath, filepath.Join(t.TempDir(), "extract"))
	if err != nil {
		t.Fatalf("extractTrivyBinary() error = %v", err)
	}
	body, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("ReadFile(binary) error = %v", err)
	}
	if string(body) != "trivy-binary" {
		t.Fatalf("binary body = %q, want extracted trivy payload", string(body))
	}
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatalf("Stat(binary) error = %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("binary mode = %v, want executable permissions", info.Mode())
	}

	missingPath := filepath.Join(t.TempDir(), "missing.tar.gz")
	if err := os.WriteFile(missingPath, makeArchiveWithoutTrivy(t), 0o600); err != nil {
		t.Fatalf("WriteFile(missing archive) error = %v", err)
	}
	if _, err := extractTrivyBinary(context.Background(), missingPath, filepath.Join(t.TempDir(), "missing-extract")); err == nil || !errors.Is(err, ErrTrivyBinaryNotFound) {
		t.Fatalf("extractTrivyBinary() error = %v, want ErrTrivyBinaryNotFound", err)
	}
}

func makeArchiveWithoutTrivy(t *testing.T) []byte {
	t.Helper()
	var raw bytes.Buffer
	gzw := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gzw)
	body := []byte("not-trivy")
	if err := tw.WriteHeader(&tar.Header{Name: "README.md", Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatalf("WriteHeader(README) error = %v", err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatalf("Write(README) error = %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close(tar missing) error = %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("Close(gzip missing) error = %v", err)
	}
	return raw.Bytes()
}

func makeTrivyArchive(t *testing.T, binaryBody string) []byte {
	t.Helper()
	var raw bytes.Buffer
	gzw := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gzw)
	if err := tw.WriteHeader(&tar.Header{Name: "trivy", Mode: 0o755, Size: int64(len(binaryBody))}); err != nil {
		t.Fatalf("WriteHeader(trivy) error = %v", err)
	}
	if _, err := tw.Write([]byte(binaryBody)); err != nil {
		t.Fatalf("Write(trivy) error = %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close(tar) error = %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("Close(gzip) error = %v", err)
	}
	return raw.Bytes()
}
