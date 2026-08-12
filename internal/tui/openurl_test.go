package tui

import (
	"errors"
	"testing"
)

// TestIsHTTPURLAcceptsOnlyHTTPAndHTTPSSchemes is the RED test for the
// defense-in-depth scheme guard: openURLInBrowser (and its default real
// implementation) must never exec anything for a URL that does not parse
// with an http/https scheme, independent of any OS-specific dispatch.
func TestIsHTTPURLAcceptsOnlyHTTPAndHTTPSSchemes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"https accepted", "https://nvd.nist.gov/vuln/detail/CVE-2026-0001", true},
		{"http accepted", "http://example.com", true},
		{"javascript scheme rejected", "javascript:alert(1)", false},
		{"file scheme rejected", "file:///etc/passwd", false},
		{"no scheme rejected", "example.com", false},
		{"empty rejected", "", false},
		{"whitespace-only rejected", "   ", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := isHTTPURL(tc.url); got != tc.want {
				t.Fatalf("isHTTPURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

// TestOpenAdminURLCmdReturnsCompletedMsgUsingConfiguredOpener is the RED test
// for the Bubble Tea side-effect convention: openAdminURLCmd returns a
// tea.Cmd (not a raw side-effecting call inside a key handler) that invokes
// the swappable openURLInBrowser var and turns its result into
// adminOpenURLCompletedMsg -- substituting the var here so this test never
// execs a real browser.
func TestOpenAdminURLCmdReturnsCompletedMsgUsingConfiguredOpener(t *testing.T) {
	original := openURLInBrowser
	defer func() { openURLInBrowser = original }()

	var gotURL string
	openURLInBrowser = func(rawURL string) error {
		gotURL = rawURL
		return nil
	}

	msg := openAdminURLCmd("https://nvd.nist.gov/vuln/detail/CVE-2026-0001")()
	completed, ok := msg.(adminOpenURLCompletedMsg)
	if !ok {
		t.Fatalf("openAdminURLCmd() msg type = %T, want adminOpenURLCompletedMsg", msg)
	}
	if completed.err != nil {
		t.Fatalf("completed.err = %v, want nil", completed.err)
	}
	if gotURL != "https://nvd.nist.gov/vuln/detail/CVE-2026-0001" {
		t.Fatalf("openURLInBrowser called with %q, want the exact URL passed to openAdminURLCmd", gotURL)
	}

	wantErr := errors.New("boom")
	openURLInBrowser = func(rawURL string) error { return wantErr }
	failMsg := openAdminURLCmd("https://example.com")().(adminOpenURLCompletedMsg)
	if !errors.Is(failMsg.err, wantErr) {
		t.Fatalf("completed.err = %v, want %v surfaced from the configured opener", failMsg.err, wantErr)
	}
}
