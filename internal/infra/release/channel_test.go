package release

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidChannel(t *testing.T) {
	t.Parallel()

	if !ValidChannel("stable") || !ValidChannel("insider") {
		t.Fatalf("ValidChannel(stable/insider) = false, want true")
	}
	if ValidChannel("") || ValidChannel("beta") || ValidChannel("STABLE") {
		t.Fatalf("ValidChannel accepted a value outside the exact {stable, insider} set")
	}
}

func releasesListHandler(t *testing.T, tags []string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/desatatufuria/regixtry/releases" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(buildReleasesListJSON(tags)))
	}
}

func buildReleasesListJSON(tags []string) string {
	body := "["
	for i, tag := range tags {
		if i > 0 {
			body += ","
		}
		body += fmt.Sprintf(`{"tag_name":%q,"prerelease":false,"draft":false}`, tag)
	}
	return body + "]"
}

func TestLatestForChannel(t *testing.T) {
	t.Parallel()

	// Deliberately out of both creation-date and lexicographic order, so a
	// correct implementation must apply real version ordering rather than
	// trusting API list order or string sorting. All the rc tags here are
	// candidates for a v0.2.0 that has NOT yet stabilized -- deliberately no
	// plain "v0.2.0" tag exists among them, so this data can't collide with
	// compareVersions' own "stable beats any rc of the same version" rule
	// (covered separately by TestCompareVersionsOrdering); this test is only
	// about picking the right candidate BY CHANNEL FILTER and by NUMERIC rc
	// ordering, not about stable-vs-rc precedence.
	tags := []string{"v0.2.0-rc9", "v0.2.0-rc105", "v0.1.0", "v0.2.0-rc99", "v0.2.0-rc10"}

	t.Run("stable channel skips every rc-suffixed tag", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(releasesListHandler(t, tags))
		defer server.Close()

		latest, err := LatestForChannel(context.Background(), nil, server.URL+"/repos/desatatufuria/regixtry/releases", ChannelStable)
		if err != nil {
			t.Fatalf("LatestForChannel(stable) error = %v", err)
		}
		if latest != "v0.1.0" {
			t.Fatalf("LatestForChannel(stable) = %q, want v0.1.0 (the only non-rc tag in the set)", latest)
		}
	})

	t.Run("insider channel picks the newest tag overall, rc numbers compared numerically", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(releasesListHandler(t, tags))
		defer server.Close()

		latest, err := LatestForChannel(context.Background(), nil, server.URL+"/repos/desatatufuria/regixtry/releases", ChannelInsider)
		if err != nil {
			t.Fatalf("LatestForChannel(insider) error = %v", err)
		}
		if latest != "v0.2.0-rc105" {
			t.Fatalf("LatestForChannel(insider) = %q, want v0.2.0-rc105 (the numerically highest rc, not rc10/rc9/rc99 picked by string order)", latest)
		}
	})

	t.Run("an unreachable server is a plain error, never a panic or a fabricated tag", func(t *testing.T) {
		t.Parallel()
		if _, err := LatestForChannel(context.Background(), nil, "http://127.0.0.1:1/releases", ChannelStable); err == nil {
			t.Fatalf("LatestForChannel against an unreachable server returned nil error, want an error")
		}
	})

	t.Run("no releases at all is a plain error, not a fabricated empty-string tag", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(releasesListHandler(t, nil))
		defer server.Close()

		if _, err := LatestForChannel(context.Background(), nil, server.URL+"/repos/desatatufuria/regixtry/releases", ChannelStable); err == nil {
			t.Fatalf("LatestForChannel with zero releases returned nil error, want an error")
		}
	})

	t.Run("unparseable tags are skipped rather than aborting the whole scan", func(t *testing.T) {
		t.Parallel()
		mixed := []string{"not-a-version", "v0.2.0", "also-garbage"}
		server := httptest.NewServer(releasesListHandler(t, mixed))
		defer server.Close()

		latest, err := LatestForChannel(context.Background(), nil, server.URL+"/repos/desatatufuria/regixtry/releases", ChannelStable)
		if err != nil {
			t.Fatalf("LatestForChannel error = %v, want the unparseable tags skipped and v0.2.0 returned", err)
		}
		if latest != "v0.2.0" {
			t.Fatalf("LatestForChannel = %q, want v0.2.0", latest)
		}
	})
}
