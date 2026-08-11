package release

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveAssetSelectsMatchingArchiveAndChecksums(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tags/v0.73.0" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, `{"tag_name":"v0.73.0","assets":[{"name":"trivy_0.73.0_Linux-64bit.tar.gz.sigstore.json","browser_download_url":"https://example.invalid/trivy_0.73.0_Linux-64bit.tar.gz.sigstore.json"},{"name":"trivy_0.73.0_Linux-64bit.tar.gz","browser_download_url":"https://example.invalid/trivy_0.73.0_Linux-64bit.tar.gz"},{"name":"trivy_0.73.0_checksums.txt","browser_download_url":"https://example.invalid/trivy_0.73.0_checksums.txt"}]}`)
	}))
	defer server.Close()

	query := AssetQuery{
		BaseAPI: server.URL,
		Tag:     "v0.73.0",
		MatchArchive: func(name string) bool {
			return strings.HasPrefix(name, "trivy_") && strings.HasSuffix(name, "_Linux-64bit.tar.gz")
		},
		MatchChecksums: func(name string) bool {
			return strings.HasSuffix(name, "checksums.txt")
		},
	}

	asset, err := ResolveAsset(context.Background(), server.Client(), query)
	if err != nil {
		t.Fatalf("ResolveAsset() error = %v", err)
	}
	if asset.ArchiveName != "trivy_0.73.0_Linux-64bit.tar.gz" {
		t.Fatalf("ArchiveName = %q, want main archive asset", asset.ArchiveName)
	}
	if strings.HasSuffix(asset.ArchiveURL, ".sigstore.json") {
		t.Fatalf("ArchiveURL = %q, want primary archive instead of sidecar", asset.ArchiveURL)
	}
	if asset.Version != "0.73.0" {
		t.Fatalf("Version = %q, want 0.73.0", asset.Version)
	}
}

func TestResolveAssetDefaultsToLatestWhenTagEmpty(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, `{"tag_name":"v1.2.3","assets":[{"browser_download_url":"https://example.invalid/regixtry_1.2.3_linux_amd64.tar.gz"},{"browser_download_url":"https://example.invalid/regixtry_1.2.3_checksums.txt"}]}`)
	}))
	defer server.Close()

	query := AssetQuery{
		BaseAPI: server.URL,
		MatchArchive: func(name string) bool {
			return strings.HasSuffix(name, "_linux_amd64.tar.gz")
		},
		MatchChecksums: func(name string) bool {
			return strings.HasSuffix(name, "_checksums.txt")
		},
	}

	asset, err := ResolveAsset(context.Background(), server.Client(), query)
	if err != nil {
		t.Fatalf("ResolveAsset() error = %v", err)
	}
	if asset.Tag != "v1.2.3" || asset.Version != "1.2.3" {
		t.Fatalf("asset = %#v, want v1.2.3 / 1.2.3", asset)
	}
	if !strings.HasSuffix(asset.ArchiveURL, "1.2.3_linux_amd64.tar.gz") {
		t.Fatalf("ArchiveURL = %q, want linux amd64 archive", asset.ArchiveURL)
	}
}

func TestResolveAssetFailsWhenNoAssetMatches(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"tag_name":"v1.0.0","assets":[]}`)
	}))
	defer server.Close()

	_, err := ResolveAsset(context.Background(), server.Client(), AssetQuery{
		BaseAPI:        server.URL,
		MatchArchive:   func(string) bool { return false },
		MatchChecksums: func(string) bool { return false },
	})
	if err == nil {
		t.Fatalf("ResolveAsset() error = nil, want missing asset error")
	}
}
