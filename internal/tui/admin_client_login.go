package tui

import (
	"context"
	stdhttp "net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"regixtry/internal/ports"
)

// adminLoginFailuresMaxLimit is the most rows GET /admin/v1/security/login-failures
// returns per page; a larger request is a server validation error, so it is
// clamped.
const adminLoginFailuresMaxLimit = 200

// AdminLoginFailuresQuery selects one page of the aggregated login failures
// (docs/api.md). Zero values mean "any" or "the server default"; Cursor is the
// opaque Next of the previous page, "" for the first one.
type AdminLoginFailuresQuery struct {
	From, To time.Time
	Account  string
	IP       string
	Reason   string
	Limit    int
	Cursor   string
}

func (q AdminLoginFailuresQuery) encode() string {
	values := url.Values{}
	if !q.From.IsZero() {
		values.Set("from", q.From.UTC().Format(time.RFC3339))
	}
	if !q.To.IsZero() {
		values.Set("to", q.To.UTC().Format(time.RFC3339))
	}
	for name, value := range map[string]string{"account": q.Account, "ip": q.IP, "reason": q.Reason, "cursor": q.Cursor} {
		if value != "" {
			values.Set(name, value)
		}
	}
	if q.Limit > 0 {
		values.Set("limit", strconv.Itoa(min(q.Limit, adminLoginFailuresMaxLimit)))
	}
	return values.Encode()
}

// ListLoginFailures reads one page of aggregated login failures, newest window
// first (global administrator only).
func (c *HTTPAdminClient) ListLoginFailures(ctx context.Context, session AdminSession, query AdminLoginFailuresQuery) (ports.AdminLoginFailurePage, error) {
	path := "/admin/v1/security/login-failures"
	if encoded := query.encode(); encoded != "" {
		path += "?" + encoded
	}
	var page ports.AdminLoginFailurePage
	if err := c.getJSON(ctx, session, path, &page); err != nil {
		return ports.AdminLoginFailurePage{}, err
	}
	return page, nil
}

// GetLoginSummary reads the last-15-minutes picture of login failures.
func (c *HTTPAdminClient) GetLoginSummary(ctx context.Context, session AdminSession) (ports.AdminLoginSummary, error) {
	var summary ports.AdminLoginSummary
	if err := c.getJSON(ctx, session, "/admin/v1/security/login-summary", &summary); err != nil {
		return ports.AdminLoginSummary{}, err
	}
	return summary, nil
}

// GetLoginBackoff reads the live login backoff state: mode, thresholds and the
// accounts waiting right now.
func (c *HTTPAdminClient) GetLoginBackoff(ctx context.Context, session AdminSession) (ports.AdminLoginBackoff, error) {
	var state ports.AdminLoginBackoff
	if err := c.getJSON(ctx, session, "/admin/v1/security/login-backoff", &state); err != nil {
		return ports.AdminLoginBackoff{}, err
	}
	return state, nil
}

// ResetLoginBackoff clears the failure counter of ONE account, named by its
// login name or by the "hash:xxxxxxxx" label of an unknown username, and
// returns how the server names it. An account with no backoff state is a 404.
func (c *HTTPAdminClient) ResetLoginBackoff(ctx context.Context, session AdminSession, account string) (string, error) {
	var payload struct {
		Account string `json:"account"`
		Reset   bool   `json:"reset"`
	}
	body := map[string]string{"account": strings.TrimSpace(account)}
	if err := c.requestJSON(ctx, stdhttp.MethodPost, session, "/admin/v1/security/login-backoff/reset", body, &payload, stdhttp.StatusOK); err != nil {
		return "", err
	}
	return payload.Account, nil
}
