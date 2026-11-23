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
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
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
	r.aggregateLoginFailure(req, endpoint, username, reason)
	if r.logger == nil {
		return
	}
	line := fmt.Sprintf("login failed endpoint=%s ip=%s reason=%q", logEndpoint(endpoint), clientIP(req, r.trustedProxies), reason)
	if username != "" {
		line += " " + accountLogField(username, reason)
	}
	r.logger.Print(line)
}

// recordLoginThrottle counts and logs a login refused before verification.
func (r *Router) recordLoginThrottle(req *stdhttp.Request, endpoint string, username string, throttled *ports.LoginThrottledError) {
	reason := throttled.Reason
	if reason == "" {
		reason = "throttled"
	}
	r.loginFailures.inc(reason)
	r.aggregateLoginFailure(req, endpoint, username, reason)
	if r.logger == nil {
		return
	}
	r.logger.Printf("login failed endpoint=%s ip=%s reason=%q %s", logEndpoint(endpoint), clientIP(req, r.trustedProxies), reason, accountLogField(username, reason))
}

// accountIdentity decides how a failed login's account is written anywhere it
// is recorded: an existing account by its normalized (and bounded) name; an
// unknown username is attacker-chosen and unbounded in variety, so it is only
// ever its short hash (hashed true). A throttled refusal never looked the
// account up, so it is hashed too.
func accountIdentity(username string, reason string) (value string, hashed bool) {
	normalized := strings.ToLower(strings.TrimSpace(username))
	if reason == "unknown user" || strings.HasPrefix(reason, "throttled") {
		sum := sha256.Sum256([]byte(normalized))
		return hex.EncodeToString(sum[:4]), true
	}
	if runes := []rune(normalized); len(runes) > maxLoggedAccountRunes {
		normalized = string(runes[:maxLoggedAccountRunes])
	}
	return normalized, false
}

// accountLogField renders the account for the log line.
func accountLogField(username string, reason string) string {
	value, hashed := accountIdentity(username, reason)
	if hashed {
		return "account_hash=" + value
	}
	return "account=" + logToken(value)
}

// accountAggregateKey is the account as the aggregated record keeps it: the
// name, or "hash:<8 hex>", the same hash the log line carries. Empty for a
// bearer failure.
func accountAggregateKey(username string, reason string) string {
	if username == "" {
		return ""
	}
	value, hashed := accountIdentity(username, reason)
	if hashed {
		return "hash:" + value
	}
	return value
}

// aggregateEndpoint bounds the endpoint label of an aggregated record to the
// credential surfaces; anything else is "other".
func aggregateEndpoint(endpoint string) string {
	switch endpoint {
	case "/auth/token", "/v2", "/admin/v1":
		return endpoint
	}
	return "other"
}

// aggregateLoginFailure hands one rejected credential to the aggregator. It is
// called from the two places that write the "login failed" line, so every
// failure contributes exactly once, whether or not a logger is set.
func (r *Router) aggregateLoginFailure(req *stdhttp.Request, endpoint string, username string, reason string) {
	if r.failureRecorder == nil {
		return
	}
	r.failureRecorder.RecordLoginFailure(ports.LoginFailureObservation{
		At:       time.Now().UTC(),
		Endpoint: aggregateEndpoint(endpoint),
		Reason:   reason,
		Account:  accountAggregateKey(username, reason),
		IP:       clientIP(req, r.trustedProxies),
	})
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

// logEndpoint writes an endpoint label bare when it is only path characters
// (the fixed labels /auth/token, /v2 and /admin/v1 always are, so operators'
// greps keep working) and quotes anything else, because credentialEndpoint
// falls back to the raw request path.
func logEndpoint(value string) string {
	for _, c := range value {
		plain := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '/' || c == '.' || c == '_' || c == '-'
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
