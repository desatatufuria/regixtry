package release

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// maxTagLength bounds a release tag used as a URL path segment.
const maxTagLength = 128

// tagPattern is the conservative tag shape the shared resolver will put in a
// URL path: one segment of letters, digits, '.', '_', '+' and '-', starting
// with an alphanumeric. Slashes, percent escapes, queries, fragments,
// whitespace and control characters never match.
var tagPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]*$`)

func validateTag(tag string) error {
	if len(tag) > maxTagLength || !tagPattern.MatchString(tag) || strings.Contains(tag, "..") {
		return fmt.Errorf("invalid release tag %q", truncateForError(tag))
	}
	return nil
}

func truncateForError(value string) string {
	if len(value) > 40 {
		return value[:40] + "..."
	}
	return value
}

// RestrictRedirects is an http.Client.CheckRedirect policy for release
// downloads: HTTPS only, at most 5 hops, and every hop must stay on the
// original request host or on GitHub's own release hosts (github.com and
// *.githubusercontent.com, where release assets are served from). A
// redirect anywhere else aborts the request.
func RestrictRedirects(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("stopped after 5 redirects")
	}
	if req.URL.Scheme != "https" {
		return fmt.Errorf("refusing redirect to non-https URL %q", req.URL.Redacted())
	}
	host := strings.ToLower(req.URL.Hostname())
	if len(via) > 0 && host == strings.ToLower(via[0].URL.Hostname()) {
		return nil
	}
	if trustedReleaseHost(host) {
		return nil
	}
	return fmt.Errorf("refusing redirect to untrusted host %q", host)
}

const (
	// MaxMetadataBytes bounds a release metadata or checksums body.
	MaxMetadataBytes int64 = 1 << 20
	// MaxArchiveBytes bounds a release archive download.
	MaxArchiveBytes int64 = 512 << 20
)

// ReadLimited reads at most max bytes from r and fails, rather than
// truncating, when the body is larger.
func ReadLimited(r io.Reader, max int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("response body exceeds %d bytes", max)
	}
	return body, nil
}

func trustedReleaseHost(host string) bool {
	host = strings.ToLower(host)
	return host == "github.com" || host == "api.github.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

// CheckDownloadURL requires a release asset or checksums URL taken from
// release metadata to be https on GitHub's own hosts, the same hosts
// RestrictRedirects allows.
func CheckDownloadURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || !trustedReleaseHost(u.Hostname()) {
		return fmt.Errorf("refusing release download URL %q", truncateForError(raw))
	}
	return nil
}
