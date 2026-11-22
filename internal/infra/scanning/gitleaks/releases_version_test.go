package gitleaks

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// hostileVersions are values an admin (or a compromised admin token) could
// put in the features API "version" field to steer the release lookup.
var hostileVersions = []string{
	"0.73.0/../../../evil/releases/tags/v1",
	"../../../../evil",
	"0.73.0?x=1",
	"0.73.0#frag",
	"0.73.0%2f..%2fevil",
	"..%2f..%2fevil",
	"https://evil.example/x",
	"//evil.example/x",
	"0.73.\n0",
	"0.73.0\x00",
	"0.73",
	"latest",
	"1.2.3-",
	"v1.2.3/../x",
	strings.Repeat("1", 5000) + ".0.0",
}

func TestResolveReleaseNeverContactsTheFeedForHostileVersions(t *testing.T) {
	t.Parallel()

	for i, version := range hostileVersions {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			t.Parallel()
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				http.NotFound(w, r)
			}))
			defer server.Close()

			_, err := githubReleaseClient{baseAPI: server.URL, client: server.Client()}.ResolveRelease(context.Background(), version)
			if err == nil {
				t.Fatalf("ResolveRelease(%q) error = nil, want a version validation error", version)
			}
			if hits.Load() != 0 {
				t.Fatalf("ResolveRelease(%q) contacted the release feed %d time(s), want 0", version, hits.Load())
			}
		})
	}
}

func TestDownloadsAreSizeLimitedAndFeedURLsMustBeTrusted(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 2<<20))) // 2 MiB
	}))
	defer server.Close()
	client := githubReleaseClient{baseAPI: server.URL, client: server.Client()}
	if _, err := client.DownloadChecksums(context.Background(), server.URL); err == nil {
		t.Fatal("DownloadChecksums() error = nil, want a size-limit error for a 2 MiB checksums body")
	}
	if body, err := client.DownloadReleaseAsset(context.Background(), server.URL); err != nil || len(body) != 2<<20 {
		t.Fatalf("DownloadReleaseAsset() len=%d err=%v, want the archive within its larger limit", len(body), err)
	}

	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v8.30.0","assets":[{"name":"ARCHIVE","browser_download_url":"https://evil.example/a"},{"name":"CHECKSUMS","browser_download_url":"https://github.com/o/r/c"}]}`))
	}))
	defer feed.Close()
	if _, err := (githubReleaseClient{baseAPI: feed.URL, client: feed.Client()}).ResolveRelease(context.Background(), "8.30.0"); err == nil {
		t.Fatal("ResolveRelease() error = nil, want an error (names do not match or URL untrusted)")
	}
}
