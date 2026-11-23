package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	appauth "regixtry/internal/app/auth"
)

// TestLoginClientThroughTheRealStack generates failures through the real login
// path and reads them back through the real HTTPAdminClient, router and
// service: an unknown username is only ever a hash label, an enforce-mode
// lockout shows up as an account waiting, a reset clears it and a second one is
// a 404.
func TestLoginClientThroughTheRealStack(t *testing.T) {
	w := newLoginWire(t, appauth.BackoffEnforce)
	ctx, c, s := context.Background(), w.client, w.session
	w.user("alice")

	// Five wrong passwords for alice without time passing in between: the
	// threshold is reached and the next attempt is refused with a 429.
	for range 5 {
		if status := w.exchange("alice", "wrong-password"); status != 401 {
			t.Fatalf("wrong password status = %d, want 401", status)
		}
	}
	if status := w.exchange("alice", loginWirePassword); status != 429 {
		t.Fatalf("correct password while waiting: status = %d, want 429", status)
	}
	w.exchange("Ghost-Name", "bad")

	state, err := c.GetLoginBackoff(ctx, s)
	if err != nil || state.Mode != "enforce" || state.Threshold != 5 || state.MaxSeconds != 300 || state.TrustedProxiesConfigured {
		t.Fatalf("GetLoginBackoff = %#v, %v", state, err)
	}
	if len(state.Accounts) != 1 || state.Accounts[0].Account != "alice" || state.Accounts[0].Failures < 5 || state.Accounts[0].RemainingSeconds < 1 {
		t.Fatalf("accounts waiting = %#v, want alice with 5 failures and time left", state.Accounts)
	}
	summary, err := c.GetLoginSummary(ctx, s)
	if err != nil || summary.Failures < 6 || summary.AccountsInBackoff != 1 || summary.WindowMinutes != 15 {
		t.Fatalf("GetLoginSummary = %#v, %v", summary, err)
	}
	page, err := c.ListLoginFailures(ctx, s, AdminLoginFailuresQuery{From: time.Now().Add(-24 * time.Hour), Limit: 50})
	if err != nil || len(page.Failures) == 0 {
		t.Fatalf("ListLoginFailures = %#v, %v", page, err)
	}
	sawHash := false
	for _, row := range page.Failures {
		if strings.Contains(strings.ToLower(row.Account), "ghost") {
			t.Fatalf("a raw unknown username came back: %#v", row)
		}
		if strings.HasPrefix(row.Account, "hash:") && row.Reason == "unknown user" {
			sawHash = true
		}
	}
	if !sawHash {
		t.Fatalf("no hash-labelled unknown-user row in %#v", page.Failures)
	}

	label, err := c.ResetLoginBackoff(ctx, s, "Alice")
	if err != nil || label != "alice" {
		t.Fatalf("ResetLoginBackoff = %q, %v", label, err)
	}
	if status := w.exchange("alice", loginWirePassword); status != 200 {
		t.Fatalf("login after the reset: status = %d, want 200", status)
	}
	if _, err := c.ResetLoginBackoff(ctx, s, "alice"); !IsAdminNotFound(err) {
		t.Fatalf("second reset error = %v, want a 404", err)
	}
	if _, err := c.ResetLoginBackoff(ctx, s, "  "); !IsAdminUnprocessable(err) {
		t.Fatalf("blank account error = %v, want a 422", err)
	}
}

// TestLoginClientRefusesNonAdmins pins the 403 the screens turn into the
// "global administrators only" state.
func TestLoginClientRefusesNonAdmins(t *testing.T) {
	w := newLoginWire(t, appauth.BackoffLog)
	ctx := context.Background()
	w.user("plain")
	login, err := w.auth.LoginWithPassword(ctx, "plain", loginWirePassword, nil)
	if err != nil {
		t.Fatal(err)
	}
	session := w.session
	session.BearerToken = login.BearerToken
	if _, err := w.client.GetLoginSummary(ctx, session); !IsAdminForbidden(err) {
		t.Fatalf("summary error = %v, want a 403", err)
	}
	if _, err := w.client.ListLoginFailures(ctx, session, AdminLoginFailuresQuery{}); !IsAdminForbidden(err) {
		t.Fatalf("failures error = %v, want a 403", err)
	}
	if _, err := w.client.ResetLoginBackoff(ctx, session, "alice"); !IsAdminForbidden(err) {
		t.Fatalf("reset error = %v, want a 403", err)
	}
}
