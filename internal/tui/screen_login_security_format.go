package tui

import (
	"fmt"
	"strings"
	"time"
)

// loginHashPrefix starts the label the server gives an unknown username: the
// same short hash the "login failed" log line prints. The raw name is never
// stored or returned.
const loginHashPrefix = "hash:"

// loginChipHexDigits is how many of the label's hex digits the chip shows.
const loginChipHexDigits = 6

// loginDelayText words a delay for people, never in seconds: under a minute is
// "under 1 min", then whole minutes, then "1 h 30 min" from an hour.
func loginDelayText(seconds int) string {
	if seconds < 60 {
		return "under 1 min"
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%d min", minutes)
	}
	if rest := minutes % 60; rest != 0 {
		return fmt.Sprintf("%d h %d min", minutes/60, rest)
	}
	return fmt.Sprintf("%d h", minutes/60)
}

// loginIsHashLabel reports whether an account is the label of an unknown
// username.
func loginIsHashLabel(account string) bool {
	return strings.HasPrefix(account, loginHashPrefix)
}

// loginAccountName is how an account is shown: its name, or the chip
// "unknown #" and six hex digits for the label of an unknown username. Never a raw name.
func loginAccountName(account string) string {
	if loginIsHashLabel(account) {
		hex := sanitizeFieldText(strings.TrimPrefix(account, loginHashPrefix), false)
		return "unknown #" + hex[:min(len(hex), loginChipHexDigits)]
	}
	return sanitizeFieldText(account, false)
}

// loginReasonLabel shortens the server-side reason of a failure to the board's
// words; a reason it does not know is shown as the server wrote it.
func loginReasonLabel(reason string) string {
	lower := strings.ToLower(reason)
	switch {
	case strings.Contains(lower, "wrong password"):
		return "bad password"
	case strings.Contains(lower, "unknown user"):
		return "unknown user"
	case strings.HasPrefix(lower, "throttled"):
		return "throttled"
	case strings.Contains(lower, "invalid credentials"):
		// A bearer token the server refused on /v2 or /admin/v1.
		return "bad token"
	}
	return sanitizeFieldText(reason, true)
}

// loginEndpointLabel is the short name of the endpoint a failure came from.
func loginEndpointLabel(endpoint string) string {
	switch endpoint {
	case "/auth/token":
		return "token"
	case "/v2":
		return "v2"
	case "/admin/v1":
		return "admin"
	}
	return sanitizeFieldText(endpoint, true)
}

// loginFailureTime is "23:58" for a time of today and "22 Nov 23:58" for any
// other day, both in UTC like every time on these screens.
func loginFailureTime(at, now time.Time) string {
	at, now = at.UTC(), now.UTC()
	if at.Year() == now.Year() && at.YearDay() == now.YearDay() {
		return at.Format("15:04")
	}
	return at.Format("2 Jan 15:04")
}
