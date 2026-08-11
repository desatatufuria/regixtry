package gitleaks

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveReleaseRejectsAssetBelowMinimumVersion(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tags/v8.18.0" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, `{"tag_name":"v8.18.0","assets":[{"name":"gitleaks_8.18.0_linux_x64.tar.gz","browser_download_url":"https://example.invalid/gitleaks_8.18.0_linux_x64.tar.gz"},{"name":"gitleaks_8.18.0_checksums.txt","browser_download_url":"https://example.invalid/gitleaks_8.18.0_checksums.txt"}]}`)
	}))
	defer server.Close()

	_, err := githubReleaseClient{baseAPI: server.URL, client: server.Client()}.ResolveRelease(context.Background(), "8.18.0")
	if err == nil {
		t.Fatalf("ResolveRelease() error = nil, want rejection below minimum version %s", minimumGitleaksVersion)
	}
	if !strings.Contains(err.Error(), minimumGitleaksVersion) {
		t.Fatalf("ResolveRelease() error = %v, want it to name the minimum version %s", err, minimumGitleaksVersion)
	}
}

func TestResolveReleaseAcceptsAssetAtOrAboveMinimumVersion(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tags/v8.27.0" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, `{"tag_name":"v8.27.0","assets":[{"name":"gitleaks_8.27.0_linux_x64.tar.gz","browser_download_url":"https://example.invalid/gitleaks_8.27.0_linux_x64.tar.gz"},{"name":"gitleaks_8.27.0_checksums.txt","browser_download_url":"https://example.invalid/gitleaks_8.27.0_checksums.txt"}]}`)
	}))
	defer server.Close()

	asset, err := githubReleaseClient{baseAPI: server.URL, client: server.Client()}.ResolveRelease(context.Background(), "8.27.0")
	if err != nil {
		t.Fatalf("ResolveRelease() error = %v", err)
	}
	if asset.ArchiveName != "gitleaks_8.27.0_linux_x64.tar.gz" {
		t.Fatalf("ArchiveName = %q, want linux x64 archive asset", asset.ArchiveName)
	}
}

func TestCompareVersionsOrdersDottedNumericVersions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		a, b string
		want int
	}{
		{"8.18.0", "8.27.0", -1},
		{"8.27.0", "8.18.0", 1},
		{"8.27.0", "8.27.0", 0},
		{"8.27.1", "8.27.0", 1},
		{"v8.27.0", "8.27.0", 0},
	}
	for _, tc := range cases {
		got, err := compareVersions(tc.a, tc.b)
		if err != nil {
			t.Fatalf("compareVersions(%q, %q) error = %v", tc.a, tc.b, err)
		}
		if got != tc.want {
			t.Fatalf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
