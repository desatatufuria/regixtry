package tui

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// openURLInBrowser is a package-level swappable func var -- the same
// testability pattern cmd/regixtry's openAuthStore uses -- wrapping the real
// OS-dispatching implementation. Tests substitute it so opening a finding's
// link never execs a real browser process.
var openURLInBrowser = func(rawURL string) error {
	// Defense in depth: validated again here (not just by the caller),
	// independent of whatever URL a future call site might pass, so this
	// var can never be asked to exec an arbitrary non-http(s) command line.
	if !isHTTPURL(rawURL) {
		return fmt.Errorf("refusing to open non-http(s) URL: %q", rawURL)
	}

	// exec.Command with an explicit argv (never a shell string) so the URL
	// can never be interpreted as shell syntax.
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", rawURL).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	default:
		return exec.Command("xdg-open", rawURL).Start()
	}
}

// isHTTPURL reports whether rawURL parses with an http or https scheme.
func isHTTPURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

// adminOpenURLCompletedMsg is the result of openAdminURLCmd: err is nil on a
// successful browser launch, non-nil (including the non-http(s) rejection)
// otherwise -- surfaced as a transient status message by Update, following
// this package's existing async Cmd+Msg convention (e.g.
// loadAdminScanRunDetailCmd/adminScanRunDetailLoadedMsg).
type adminOpenURLCompletedMsg struct {
	url string
	err error
}

// openAdminURLCmd returns a tea.Cmd that opens rawURL via the swappable
// openURLInBrowser. Bubble Tea convention: the side effect belongs in the
// Cmd, never directly inside a key handler in Update.
func openAdminURLCmd(rawURL string) tea.Cmd {
	return func() tea.Msg {
		return adminOpenURLCompletedMsg{url: rawURL, err: openURLInBrowser(rawURL)}
	}
}

// nvdVulnerabilityURL constructs the fallback advisory link for a finding
// whose ports.ScanRunFinding.PrimaryURL is empty, from its VulnerabilityID.
func nvdVulnerabilityURL(vulnerabilityID string) string {
	id := strings.TrimSpace(vulnerabilityID)
	if id == "" {
		return ""
	}
	return "https://nvd.nist.gov/vuln/detail/" + id
}
