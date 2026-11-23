package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHTTPAdminClientLoginReads(t *testing.T) {
	t.Parallel()
	client, session := projectsClientAt(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer bearer-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /admin/v1/security/login-summary":
			writeAPIJSON(w, 200, `{"window_minutes":15,"failures":42,"distinct_ips":5,"distinct_accounts":9,"accounts_in_backoff":3}`)
		case "GET /admin/v1/security/login-backoff":
			writeAPIJSON(w, 200, `{"mode":"enforce","threshold":5,"base_seconds":2,"max_seconds":300,"max_concurrency":8,"trusted_proxies_configured":true,"accounts":[{"account":"alice","failures":6,"remaining_seconds":4},{"account":"hash:ab12cd34","failures":12,"remaining_seconds":300}]}`)
		case "GET /admin/v1/security/login-failures":
			q := r.URL.Query()
			if q.Get("limit") != "200" || q.Get("cursor") != "c+1/=" || q.Get("account") != "alice" || q.Get("ip") != "203.0.113.7" || q.Get("reason") != "unknown user" || q.Get("from") != "2026-11-20T10:00:00Z" || q.Get("to") != "2026-11-21T10:00:00Z" {
				t.Errorf("failures query = %q, want every filter sent and the limit clamped to 200", r.URL.RawQuery)
			}
			writeAPIJSON(w, 200, `{"failures":[{"window_start":"2026-11-21T09:55:00Z","endpoint":"/auth/token","reason":"unknown user","account":"hash:ab12cd34","ip":"203.0.113.7","count":3,"first_seen":"2026-11-21T09:56:00Z","last_seen":"2026-11-21T09:58:00Z"}],"next":"n2"}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	})
	ctx := context.Background()

	summary, err := client.GetLoginSummary(ctx, session)
	if err != nil || summary.Failures != 42 || summary.DistinctIPs != 5 || summary.DistinctAccounts != 9 || summary.AccountsInBackoff != 3 || summary.WindowMinutes != 15 {
		t.Fatalf("GetLoginSummary = %#v, %v", summary, err)
	}
	state, err := client.GetLoginBackoff(ctx, session)
	if err != nil || state.Mode != "enforce" || state.Threshold != 5 || state.BaseSeconds != 2 || state.MaxSeconds != 300 || state.MaxConcurrency != 8 || !state.TrustedProxiesConfigured ||
		len(state.Accounts) != 2 || state.Accounts[1].Account != "hash:ab12cd34" || state.Accounts[0].RemainingSeconds != 4 {
		t.Fatalf("GetLoginBackoff = %#v, %v", state, err)
	}
	from := time.Date(2026, time.November, 20, 10, 0, 0, 0, time.UTC)
	page, err := client.ListLoginFailures(ctx, session, AdminLoginFailuresQuery{From: from, To: from.Add(24 * time.Hour), Account: "alice", IP: "203.0.113.7", Reason: "unknown user", Limit: 500, Cursor: "c+1/="})
	if err != nil || page.Next != "n2" || len(page.Failures) != 1 || page.Failures[0].Account != "hash:ab12cd34" || page.Failures[0].Count != 3 || page.Failures[0].Endpoint != "/auth/token" {
		t.Fatalf("ListLoginFailures = %#v, %v", page, err)
	}
}

func TestHTTPAdminClientLoginFailuresSendsNothingExtraByDefault(t *testing.T) {
	t.Parallel()
	client, session := projectsClientAt(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want none for a zero query", r.URL.RawQuery)
		}
		writeAPIJSON(w, 200, `{"failures":[],"next":""}`)
	})
	if page, err := client.ListLoginFailures(context.Background(), session, AdminLoginFailuresQuery{}); err != nil || len(page.Failures) != 0 || page.Next != "" {
		t.Fatalf("page = %#v, %v", page, err)
	}
}

func TestHTTPAdminClientResetLoginBackoff(t *testing.T) {
	t.Parallel()
	var bodies []string
	client, session := projectsClientAt(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/v1/security/login-backoff/reset" {
			t.Errorf("request = %s %s", r.Method, r.URL)
		}
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		bodies = append(bodies, payload["account"])
		switch payload["account"] {
		case "alice", "hash:ab12cd34":
			writeAPIJSON(w, 200, `{"account":"`+payload["account"]+`","reset":true}`)
		case "gone":
			writeAPIJSON(w, 404, `{"error":"login backoff not found"}`)
		default:
			writeAPIJSON(w, 422, `{"error":"account must be a short valid login name"}`)
		}
	})
	ctx := context.Background()
	for _, account := range []string{" alice ", "hash:ab12cd34"} {
		if label, err := client.ResetLoginBackoff(ctx, session, account); err != nil || label != strings.TrimSpace(account) {
			t.Fatalf("ResetLoginBackoff(%q) = %q, %v", account, label, err)
		}
	}
	if _, err := client.ResetLoginBackoff(ctx, session, "gone"); !IsAdminNotFound(err) {
		t.Fatalf("second reset error = %v, want a 404", err)
	}
	if _, err := client.ResetLoginBackoff(ctx, session, "x y"); !IsAdminUnprocessable(err) {
		t.Fatalf("invalid account error = %v, want a 422", err)
	}
	if strings.Join(bodies, ",") != "alice,hash:ab12cd34,gone,x y" {
		t.Fatalf("accounts sent = %v, want the trimmed name or hash label", bodies)
	}
}

func TestHTTPAdminClientLoginTypedErrorsAndExpiry(t *testing.T) {
	t.Parallel()
	client, session := projectsClientAt(t, func(w http.ResponseWriter, r *http.Request) {
		writeAPIJSON(w, 403, `{"error":"global administrator required"}`)
	})
	ctx := context.Background()
	if _, err := client.GetLoginSummary(ctx, session); !IsAdminForbidden(err) {
		t.Fatalf("summary error = %v, want a 403", err)
	}
	if _, err := client.GetLoginBackoff(ctx, session); !IsAdminForbidden(err) {
		t.Fatalf("backoff error = %v, want a 403", err)
	}
	if _, err := client.ListLoginFailures(ctx, session, AdminLoginFailuresQuery{}); !IsAdminForbidden(err) {
		t.Fatalf("failures error = %v, want a 403", err)
	}
	if _, err := client.ResetLoginBackoff(ctx, session, "alice"); !IsAdminForbidden(err) {
		t.Fatalf("reset error = %v, want a 403", err)
	}

	// An expired session never reaches the server: the same typed error as every
	// other client method.
	expired := session
	expired.ExpiresAt = time.Date(2026, time.November, 21, 9, 0, 0, 0, time.UTC)
	checks := map[string]error{}
	_, checks["summary"] = client.GetLoginSummary(ctx, expired)
	_, checks["backoff"] = client.GetLoginBackoff(ctx, expired)
	_, checks["failures"] = client.ListLoginFailures(ctx, expired, AdminLoginFailuresQuery{})
	_, checks["reset"] = client.ResetLoginBackoff(ctx, expired, "alice")
	for name, err := range checks {
		if !IsAdminSessionExpired(err) {
			t.Errorf("%s with an expired session: error = %v, want session expired", name, err)
		}
	}
}
