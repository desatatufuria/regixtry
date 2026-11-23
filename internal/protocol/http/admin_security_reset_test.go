package regixtryhttp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	appauth "regixtry/internal/app/auth"
)

const resetPath = "/admin/v1/security/login-backoff/reset"

func (e *secHTTP) failN(user string, n int) {
	e.t.Helper()
	for i := 0; i < n; i++ {
		e.exchange(user, "wrong-password", "203.0.113.7")
	}
}

func (e *secHTTP) waitingAccounts() map[string]int {
	e.t.Helper()
	rec := e.do(http.MethodGet, "/admin/v1/security/login-backoff", e.adminToken, "")
	var got struct {
		Accounts []struct {
			Account  string `json:"account"`
			Failures int    `json:"failures"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		e.t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	out := map[string]int{}
	for _, a := range got.Accounts {
		out[a.Account] = a.Failures
	}
	return out
}

func (e *secHTTP) resetEvents() []map[string]any {
	e.t.Helper()
	rec := e.do(http.MethodGet, "/admin/v1/access-events?limit=200", e.adminToken, "")
	var all []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		e.t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	var out []map[string]any
	for _, event := range all {
		if event["action"] == "login.backoff.reset" {
			out = append(out, event)
		}
	}
	return out
}

func TestResetClearsOnlyThatAccountNormalizesTheNameAndIsAudited(t *testing.T) {
	t.Parallel()
	e := newSecHTTP(t, appauth.BackoffEnforce)
	e.newUser("alice")
	e.newUser("bob")
	e.failN("alice", 5)
	e.failN("bob", 5)
	if rec := e.exchange("alice", secPassword, "203.0.113.7"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("alice before reset = %d, want 429", rec.Code)
	}

	for _, spelled := range []string{"Alice", " alice ", "ALICE"} {
		e.failN("alice", 5) // back into backoff for the next spelling
		if _, ok := e.waitingAccounts()["alice"]; !ok {
			t.Fatalf("alice should be waiting before the reset as %q", spelled)
		}
		rec := e.do(http.MethodPost, resetPath, e.adminToken, `{"account":"`+spelled+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("reset %q = %d body=%s", spelled, rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["account"] != "alice" || body["reset"] != true || len(body) != 2 {
			t.Fatalf("reset %q body = %v", spelled, body)
		}
		if _, still := e.waitingAccounts()["alice"]; still {
			t.Fatalf("alice still waiting after reset as %q", spelled)
		}
	}
	if _, ok := e.waitingAccounts()["bob"]; !ok {
		t.Fatal("resetting alice must not clear bob")
	}
	if rec := e.exchange("alice", secPassword, "203.0.113.7"); rec.Code != http.StatusOK {
		t.Fatalf("alice after reset = %d, want 200", rec.Code)
	}
	if rec := e.exchange("bob", secPassword, "203.0.113.7"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("bob after alice's reset = %d, want 429", rec.Code)
	}

	events := e.resetEvents()
	if len(events) != 3 {
		t.Fatalf("audit events = %d, want one per reset: %v", len(events), events)
	}
	if events[0]["actor"] != "admin" || events[0]["subject"] != "alice" || events[0]["detail"] == nil {
		t.Fatalf("audit event = %v", events[0])
	}
}

func TestResetOfAnAccountWithoutBackoffIs404AndNotAudited(t *testing.T) {
	t.Parallel()
	e := newSecHTTP(t, appauth.BackoffEnforce)
	e.newUser("alice")
	e.failN("alice", 5)
	if rec := e.do(http.MethodPost, resetPath, e.adminToken, `{"account":"alice"}`); rec.Code != http.StatusOK {
		t.Fatalf("first reset = %d", rec.Code)
	}
	again := e.do(http.MethodPost, resetPath, e.adminToken, `{"account":"alice"}`)
	existing := again.Body.String()
	missing := e.do(http.MethodPost, resetPath, e.adminToken, `{"account":"nobody-at-all"}`)
	if again.Code != http.StatusNotFound || missing.Code != http.StatusNotFound {
		t.Fatalf("second reset = %d, unknown account = %d; want 404 for both", again.Code, missing.Code)
	}
	// The same message for an existing account with no state and a missing one.
	if strings.ReplaceAll(existing, "alice", "X") != strings.ReplaceAll(missing.Body.String(), "nobody-at-all", "X") {
		t.Fatalf("bodies differ beyond the echoed name: %q vs %q", existing, missing.Body.String())
	}
	if len(e.resetEvents()) != 1 {
		t.Fatalf("audit events = %d, want only the first reset", len(e.resetEvents()))
	}
}

func TestResetOfAnUnknownUsernameInBackoffWorksByNameOrHashAndNeverStoresTheRawName(t *testing.T) {
	t.Parallel()
	e := newSecHTTP(t, appauth.BackoffEnforce)
	e.failN("Ghost-Raw", 5)
	e.failN("other-ghost", 5)
	hash := "hash:" + sha8("ghost-raw")
	if _, ok := e.waitingAccounts()[hash]; !ok {
		t.Fatalf("waiting = %v, want %s", e.waitingAccounts(), hash)
	}

	byName := e.do(http.MethodPost, resetPath, e.adminToken, `{"account":"Ghost-Raw"}`)
	if byName.Code != http.StatusOK || strings.Contains(strings.ToLower(byName.Body.String()), "ghost-raw") || !strings.Contains(byName.Body.String(), hash) {
		t.Fatalf("reset by name = %d %s", byName.Code, byName.Body.String())
	}
	byHash := e.do(http.MethodPost, resetPath, e.adminToken, `{"account":"hash:`+sha8("other-ghost")+`"}`)
	if byHash.Code != http.StatusOK {
		t.Fatalf("reset by hash = %d %s", byHash.Code, byHash.Body.String())
	}
	if len(e.waitingAccounts()) != 0 {
		t.Fatalf("waiting = %v, want none", e.waitingAccounts())
	}
	for _, event := range e.resetEvents() {
		raw, _ := json.Marshal(event)
		if strings.Contains(strings.ToLower(string(raw)), "ghost") && !strings.Contains(string(raw), "hash:") {
			t.Fatalf("audit event keeps a raw unknown name: %s", raw)
		}
		if strings.Contains(strings.ToLower(string(raw)), "ghost-raw") || strings.Contains(strings.ToLower(string(raw)), "other-ghost") {
			t.Fatalf("audit event keeps a raw unknown name: %s", raw)
		}
	}
	if strings.Contains(strings.ToLower(e.logs.String()), "ghost-raw") {
		t.Fatalf("log keeps a raw unknown name:\n%s", e.logs.String())
	}
}

func TestResetRejectsBadBodies(t *testing.T) {
	t.Parallel()
	e := newSecHTTP(t, appauth.BackoffEnforce)
	for name, body := range map[string]string{
		"empty body":    ``,
		"no account":    `{}`,
		"blank account": `{"account":"   "}`,
		"unknown field": `{"account":"alice","force":true}`,
		"not JSON":      `account=alice`,
		"two objects":   `{"account":"a"}{"account":"b"}`,
		"NUL":           `{"account":"a\u0000b"}`,
		"too long":      `{"account":"` + strings.Repeat("a", 300) + `"}`,
		"wrong type":    `{"account":7}`,
	} {
		if rec := e.do(http.MethodPost, resetPath, e.adminToken, body); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422 (%s)", name, rec.Code, rec.Body.String())
		}
	}
	if rec := e.do(http.MethodGet, resetPath, e.adminToken, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET reset = %d, want 405", rec.Code)
	}
}

func TestRealWireFailSixTimesSeeTheAccountResetAndLogInWithNoResidue(t *testing.T) {
	t.Parallel()
	e := newSecHTTP(t, appauth.BackoffEnforce)
	e.newUser("alice")
	e.failN("alice", 6)
	if got := e.waitingAccounts(); got["alice"] != 5 {
		// Six attempts: five count, the sixth is refused before verification.
		t.Fatalf("waiting = %v, want alice with 5 counted failures", got)
	}
	if rec := e.do(http.MethodPost, resetPath, e.adminToken, `{"account":"alice"}`); rec.Code != http.StatusOK {
		t.Fatalf("reset = %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.exchange("alice", secPassword, "203.0.113.7"); rec.Code != http.StatusOK {
		t.Fatalf("login after reset = %d, want 200", rec.Code)
	}
	if got := e.waitingAccounts(); len(got) != 0 {
		t.Fatalf("residual backoff state: %v", got)
	}
	// The counter really restarted: five more failures are free again.
	for i := 0; i < 5; i++ {
		if rec := e.exchange("alice", "wrong-password", "203.0.113.7"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d after reset = %d, want 401 (not throttled)", i, rec.Code)
		}
	}
}
