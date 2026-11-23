package tui

import (
	"testing"
	"time"
)

func TestLoginDelayText(t *testing.T) {
	t.Parallel()
	for seconds, want := range map[int]string{0: "under 1 min", 2: "under 1 min", 59: "under 1 min", 60: "1 min", 119: "1 min", 240: "4 min", 300: "5 min", 3540: "59 min", 3600: "1 h", 5400: "1 h 30 min", 7260: "2 h 1 min"} {
		if got := loginDelayText(seconds); got != want {
			t.Errorf("loginDelayText(%d) = %q, want %q", seconds, got, want)
		}
	}
}

func TestLoginAccountNeverShowsMoreThanTheChip(t *testing.T) {
	t.Parallel()
	for account, want := range map[string]string{
		"alice":           "alice",
		"hash:ab12cd34":   "unknown #ab12cd",
		"hash:ab":         "unknown #ab",
		"hash:":           "unknown #",
		"bad\x1b[31mname": "bad[31mname",
	} {
		if got := loginAccountName(account); got != want {
			t.Errorf("loginAccountName(%q) = %q, want %q", account, got, want)
		}
	}
}

func TestLoginReasonAndEndpointLabels(t *testing.T) {
	t.Parallel()
	for reason, want := range map[string]string{
		"wrong password or unknown admin credential token": "bad password",
		"wrong password":                      "bad password",
		"unknown user":                        "unknown user",
		"throttled: account backoff":          "throttled",
		"throttled: bcrypt concurrency limit": "throttled",
		"invalid credentials":                 "bad token",
		"something new":                       "something new",
	} {
		if got := loginReasonLabel(reason); got != want {
			t.Errorf("loginReasonLabel(%q) = %q, want %q", reason, got, want)
		}
	}
	for endpoint, want := range map[string]string{"/auth/token": "token", "/v2": "v2", "/admin/v1": "admin", "other": "other"} {
		if got := loginEndpointLabel(endpoint); got != want {
			t.Errorf("loginEndpointLabel(%q) = %q, want %q", endpoint, got, want)
		}
	}
}

func TestLoginFailureTime(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 23, 10, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, time.November, 23, 0, 5, 0, 0, time.UTC), "00:05"},
		{time.Date(2026, time.November, 22, 23, 58, 0, 0, time.UTC), "22 Nov 23:58"},
		{time.Date(2026, time.November, 2, 9, 1, 0, 0, time.UTC), "2 Nov 09:01"},
		{time.Date(2025, time.November, 23, 10, 0, 0, 0, time.UTC), "23 Nov 10:00"},
	} {
		if got := loginFailureTime(tt.at, now); got != tt.want {
			t.Errorf("loginFailureTime(%v) = %q, want %q", tt.at, got, tt.want)
		}
	}
}
