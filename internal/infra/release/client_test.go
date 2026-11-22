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

func TestResolveAssetEscapesTagAndRejectsUnsafeValues(t *testing.T) {
	t.Parallel()

	var gotPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.EscapedPath())
		_, _ = fmt.Fprint(w, `{"tag_name":"v1.0.0+build","assets":[{"name":"x_linux.tar.gz","browser_download_url":"https://example.invalid/a"},{"name":"../evil_linux.tar.gz","browser_download_url":"https://example.invalid/e"},{"name":"x_checksums.txt","browser_download_url":"https://example.invalid/c"}]}`)
	}))
	defer server.Close()

	query := AssetQuery{BaseAPI: server.URL, MatchArchive: func(n string) bool { return strings.HasSuffix(n, "_linux.tar.gz") }, MatchChecksums: func(n string) bool { return strings.HasSuffix(n, "_checksums.txt") }}

	for _, tag := range []string{"../../x", "v1/../x", "v1?x=1", "v1#f", "v1%2fx", "v 1", "v\n1", "https://evil/x", strings.Repeat("a", 500), "v1..2"} {
		query.Tag = tag
		if _, err := ResolveAsset(context.Background(), server.Client(), query); err == nil {
			t.Errorf("ResolveAsset(tag=%.30q) error = nil, want rejection", tag)
		}
	}
	if len(gotPaths) != 0 {
		t.Fatalf("server contacted for hostile tags: %v", gotPaths)
	}

	query.Tag = "v1.0.0+build"
	asset, err := ResolveAsset(context.Background(), server.Client(), query)
	if err != nil {
		t.Fatalf("ResolveAsset(valid) error = %v", err)
	}
	if len(gotPaths) != 1 || gotPaths[0] != "/tags/v1.0.0+build" {
		t.Fatalf("paths = %v, want /tags/v1.0.0+build", gotPaths)
	}
	if asset.ArchiveName != "x_linux.tar.gz" {
		t.Fatalf("ArchiveName = %q, want the asset with a path-free name (a traversal name must be ignored)", asset.ArchiveName)
	}
}

func TestResolveAssetRejectsTraversalTagNameFromMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"tag_name":"../../x","assets":[]}`)
	}))
	defer server.Close()
	if _, err := ResolveAsset(context.Background(), server.Client(), AssetQuery{BaseAPI: server.URL}); err == nil {
		t.Fatal("ResolveAsset() error = nil, want a rejected tag_name")
	}
}

func TestRestrictRedirects(t *testing.T) {
	t.Parallel()

	mk := func(raw string) *http.Request {
		r, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	via := []*http.Request{mk("https://api.github.com/repos/x/y/releases/latest")}
	cases := []struct {
		name string
		to   string
		via  []*http.Request
		ok   bool
	}{
		{"same host", "https://api.github.com/repositories/1/releases/latest", via, true},
		{"github objects", "https://objects.githubusercontent.com/a/b", via, true},
		{"github release host", "https://github.com/x/y/releases/download/v1/a.tgz", via, true},
		{"other host", "https://evil.example/a", via, false},
		{"suffix trick", "https://evilgithubusercontent.com/a", via, false},
		{"downgrade to http", "http://objects.githubusercontent.com/a", via, false},
		{"too many hops", "https://github.com/a", []*http.Request{via[0], via[0], via[0], via[0], via[0]}, false},
	}
	for _, tc := range cases {
		err := RestrictRedirects(mk(tc.to), tc.via)
		if (err == nil) != tc.ok {
			t.Errorf("%s: RestrictRedirects(%s) error = %v, want ok=%v", tc.name, tc.to, err, tc.ok)
		}
	}
}
