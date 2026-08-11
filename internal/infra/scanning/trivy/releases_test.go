package trivy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// Checksum verification (mismatch, sidecar-checksum tolerance) and binary
// extraction (filepath.Base matching, executable permissions, not-found
// sentinel) now live in internal/infra/release — see client_test.go,
// checksum_test.go, and extract_test.go there. RuntimeManager here only
// wires those shared primitives with the "trivy" binary name; the
// runtime_manager_test.go install/upgrade flow exercises that wiring
// end-to-end via fakeReleaseClient.withArchive, which still needs a real
// trivy archive fixture.

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
