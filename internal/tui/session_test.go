package tui

import (
	"errors"
	"testing"
	"time"
)

func TestAdminSessionIsExpired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 4, 22, 0, 0, 0, time.UTC)
	session := AdminSession{
		Username:    "operator",
		BearerToken: "token",
		ExpiresAt:   now.Add(30 * time.Second),
	}

	if session.IsExpired(now) {
		t.Fatal("IsExpired() = true, want false before expiry")
	}
	if !session.IsExpired(now.Add(30 * time.Second)) {
		t.Fatal("IsExpired() = false, want true at expiry")
	}
	if got := session.Remaining(now); got != 30*time.Second {
		t.Fatalf("Remaining() = %s, want %s", got, 30*time.Second)
	}
	if got := session.Remaining(now.Add(31 * time.Second)); got != 0 {
		t.Fatalf("Remaining() after expiry = %s, want 0", got)
	}
}

func TestLogoutAdminStateClearsSessionAndViewData(t *testing.T) {
	t.Parallel()

	session, view := LogoutAdminState()
	if session.IsAuthenticated() {
		t.Fatal("LogoutAdminState() returned authenticated session")
	}
	if session.ExpiredReason != "" {
		t.Fatalf("ExpiredReason = %q, want empty", session.ExpiredReason)
	}
	if len(view.Users) != 0 || view.SelectedUserID != "" || len(view.Grants) != 0 || len(view.AdminTokens) != 0 {
		t.Fatalf("LogoutAdminState() returned populated view state: %#v", view)
	}
}

func TestExpireAdminStateClearsViewDataAndKeepsReason(t *testing.T) {
	t.Parallel()

	session, view := ExpireAdminState("Session expired. Log in again.")
	if session.IsAuthenticated() {
		t.Fatal("ExpireAdminState() returned authenticated session")
	}
	if session.ExpiredReason != "Session expired. Log in again." {
		t.Fatalf("ExpiredReason = %q, want session expiry message", session.ExpiredReason)
	}
	if len(view.Users) != 0 || len(view.Grants) != 0 || len(view.AdminTokens) != 0 {
		t.Fatalf("ExpireAdminState() returned populated view state: %#v", view)
	}
}

func TestIsAdminSessionExpired(t *testing.T) {
	t.Parallel()

	err := NewAdminSessionExpiredError("")
	if !IsAdminSessionExpired(err) {
		t.Fatal("IsAdminSessionExpired() = false, want true")
	}
	if IsAdminSessionExpired(errors.New("different error")) {
		t.Fatal("IsAdminSessionExpired() = true, want false for unrelated error")
	}
}
