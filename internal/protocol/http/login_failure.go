package regixtryhttp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	stdhttp "net/http"
	"strconv"
	"strings"
	"sync"

	domainauth "regixtry/internal/domain/auth"
)

// maxLoginFailureReasons bounds the counter's label set so a bug that leaks
// attacker-controlled text into a reason cannot grow it without limit.
const maxLoginFailureReasons = 64

// maxLoggedAccountRunes bounds the account name written to the log.
const maxLoggedAccountRunes = 64

// loginFailureStats is the in-process counter of credential failures by reason.
type loginFailureStats struct {
	mu     sync.Mutex
	counts map[string]uint64
}

func (s *loginFailureStats) inc(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counts == nil {
		s.counts = make(map[string]uint64)
	}
	if _, known := s.counts[reason]; !known && len(s.counts) >= maxLoginFailureReasons {
		reason = "other"
	}
	s.counts[reason]++
}

func (s *loginFailureStats) snapshot() map[string]uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]uint64, len(s.counts))
	for reason, n := range s.counts {
		out[reason] = n
	}
	return out
}

// LoginFailureCounts returns a copy of the credential-failure counters keyed
// by the server-side reason ("wrong password", "unknown user", ...).
func (r *Router) LoginFailureCounts() map[string]uint64 {
	return r.loginFailures.snapshot()
}

// isCredentialFailure reports whether err is a rejected credential, as opposed
// to a malformed request or an internal error.
func isCredentialFailure(err error) bool {
	return isInvalidTokenError(err)
}

// loginFailureReason is the server-side reason for a rejected credential: the
// wrapped cause when the service attached one, else the error code.
func loginFailureReason(err error) string {
	if cause := errors.Unwrap(err); cause != nil {
		return cause.Error()
	}
	var authErr *domainauth.Error
	if errors.As(err, &authErr) {
		return strings.ToLower(strings.ReplaceAll(string(authErr.Code), "_", " "))
	}
	return "rejected"
}

// recordLoginFailure counts a rejected credential and writes one structured
// log line with the client IP and the real reason. It never receives or logs a
// password or a token. username is empty for bearer failures.
func (r *Router) recordLoginFailure(req *stdhttp.Request, endpoint string, username string, err error) {
	reason := loginFailureReason(err)
	r.loginFailures.inc(reason)
	if r.logger == nil {
		return
	}
	line := fmt.Sprintf("login failed endpoint=%s ip=%s reason=%q", endpoint, clientIP(req, r.trustedProxies), reason)
	if username != "" {
		line += " " + accountLogField(username, reason)
	}
	r.logger.Print(line)
}

// accountLogField renders the account for the log. An existing account is
// logged by its normalized (and bounded) name; an unknown username is
// attacker-chosen and unbounded in variety, so it is logged as a short hash.
func accountLogField(username string, reason string) string {
	normalized := strings.ToLower(strings.TrimSpace(username))
	if reason == "unknown user" {
		sum := sha256.Sum256([]byte(normalized))
		return "account_hash=" + hex.EncodeToString(sum[:4])
	}
	if runes := []rune(normalized); len(runes) > maxLoggedAccountRunes {
		normalized = string(runes[:maxLoggedAccountRunes])
	}
	return "account=" + logToken(normalized)
}

// logToken leaves plain names bare and quotes anything with characters that
// could forge or split a log line.
func logToken(value string) string {
	for _, c := range value {
		plain := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' || c == '@'
		if !plain {
			return strconv.Quote(value)
		}
	}
	return value
}

// credentialEndpoint labels a bearer failure by the API surface it hit.
func credentialEndpoint(req *stdhttp.Request) string {
	path := ""
	if req != nil && req.URL != nil {
		path = req.URL.Path
	}
	switch {
	case path == "/admin/v1" || strings.HasPrefix(path, "/admin/v1/"):
		return "/admin/v1"
	case path == "/v2" || strings.HasPrefix(path, "/v2/"):
		return "/v2"
	}
	return path
}
