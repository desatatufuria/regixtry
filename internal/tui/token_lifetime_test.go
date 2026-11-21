package tui

import (
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
)

const day = 24 * time.Hour

func TestLifetimeCeilingIsTheServerDefaultToday(t *testing.T) {
	t.Parallel()
	if got := lifetimeCeiling(); got != domainauth.DefaultAdminTokenTTL {
		t.Fatalf("lifetimeCeiling() = %v, want domainauth.DefaultAdminTokenTTL (%v)", got, domainauth.DefaultAdminTokenTTL)
	}
	if got := lifetimeCeilingDays(lifetimeCeiling()); got != 30 {
		t.Fatalf("lifetimeCeilingDays = %d, want 30", got)
	}
}

func TestLifetimeOptionsAreFilteredByTheCeiling(t *testing.T) {
	t.Parallel()
	labels := func(ceiling time.Duration) string {
		var out []string
		for _, o := range lifetimeOptions(ceiling) {
			out = append(out, o.Label)
		}
		return strings.Join(out, "|")
	}
	tests := []struct {
		name    string
		ceiling time.Duration
		want    string
	}{
		{"today's ceiling of 30 days", 30 * day, "7 days|30 days|Custom"},
		{"a preset longer than the ceiling is never shown", 10 * day, "7 days|Custom"},
		{"a ceiling equal to a preset keeps it", 7 * day, "7 days|Custom"},
		{"a ceiling below every preset leaves Custom only", 3 * day, "Custom"},
		{"a longer ceiling still shows only the listed presets", 90 * day, "7 days|30 days|Custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := labels(tt.ceiling); got != tt.want {
				t.Fatalf("lifetimeOptions(%v) labels = %q, want %q", tt.ceiling, got, tt.want)
			}
		})
	}
	opts := lifetimeOptions(30 * day)
	if !opts[len(opts)-1].Custom || opts[0].Custom {
		t.Fatalf("only the last option is Custom: %#v", opts)
	}
}

func TestLifetimeDefaultSelectionIs30Days(t *testing.T) {
	t.Parallel()
	var l tokenLifetime
	ceiling := lifetimeCeiling()
	opts := lifetimeOptions(ceiling)
	if got := opts[l.selected(ceiling)].Label; got != "30 days" {
		t.Fatalf("default selection = %q, want 30 days", got)
	}
	d, ok := l.duration(ceiling)
	if !ok || d != 30*day {
		t.Fatalf("default duration = %v, %v; want 30 days", d, ok)
	}
	// With a ceiling that hides 30 days the default falls back to the largest preset.
	if got := lifetimeOptions(10 * day)[l.selected(10*day)].Label; got != "7 days" {
		t.Fatalf("default under a 10 day ceiling = %q, want 7 days", got)
	}
}

func TestLifetimeDurationIsExactlyDaysTimes24Hours(t *testing.T) {
	t.Parallel()
	for days, want := range map[int]time.Duration{1: 24 * time.Hour, 7: 168 * time.Hour, 15: 360 * time.Hour, 30: 720 * time.Hour} {
		if got := lifetimeDuration(days); got != want {
			t.Fatalf("lifetimeDuration(%d) = %v, want %v", days, got, want)
		}
		if got := int64(lifetimeDuration(days) / time.Second); got != int64(days)*86400 {
			t.Fatalf("lifetimeDuration(%d) = %d seconds, want %d", days, got, int64(days)*86400)
		}
	}
}

func TestParseCustomDays(t *testing.T) {
	t.Parallel()
	ceiling := 30 * day
	tests := []struct {
		in     string
		want   int
		wantOK bool
	}{
		{"1", 1, true},
		{"15", 15, true},
		{"30", 30, true},
		{"007", 7, true},
		{"0", 0, false},
		{"00", 0, false},
		{"31", 0, false},
		{"", 0, false},
		{"  ", 0, false},
		{"abc", 0, false},
		{"1a", 0, false},
		{"-5", 0, false},
		{"+5", 0, false},
		{"1.5", 0, false},
		{"99999999999999999999", 0, false},
		{"9223372036854775807", 0, false},
		{"٣", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseCustomDays(tt.in, ceiling)
		if ok != tt.wantOK || got != tt.want {
			t.Errorf("parseCustomDays(%q) = %d, %v; want %d, %v", tt.in, got, ok, tt.want, tt.wantOK)
		}
	}
	if _, ok := parseCustomDays("31", 90*day); !ok {
		t.Errorf("parseCustomDays(31) with a 90 day ceiling must be valid")
	}
}

func TestExpiryPreview(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		now  time.Time
		d    time.Duration
		want string
	}{
		{"30 days", time.Date(2026, time.March, 14, 9, 30, 0, 0, time.UTC), 30 * day, "Expires Mon 13 Apr 2026 09:30 UTC (in 30 days)"},
		{"7 days", time.Date(2026, time.March, 14, 9, 30, 0, 0, time.UTC), 7 * day, "Expires Sat 21 Mar 2026 09:30 UTC (in 7 days)"},
		{"crosses a month end", time.Date(2026, time.January, 31, 23, 5, 0, 0, time.UTC), 7 * day, "Expires Sat 7 Feb 2026 23:05 UTC (in 7 days)"},
		{"crosses a leap day", time.Date(2028, time.February, 25, 12, 0, 0, 0, time.UTC), 7 * day, "Expires Fri 3 Mar 2028 12:00 UTC (in 7 days)"},
		{"lands on a leap day", time.Date(2028, time.January, 30, 8, 15, 0, 0, time.UTC), 30 * day, "Expires Tue 29 Feb 2028 08:15 UTC (in 30 days)"},
		{"no leap day in a common year", time.Date(2027, time.February, 25, 12, 0, 0, 0, time.UTC), 7 * day, "Expires Thu 4 Mar 2027 12:00 UTC (in 7 days)"},
		{"crosses a year end", time.Date(2026, time.December, 20, 18, 45, 0, 0, time.UTC), 30 * day, "Expires Tue 19 Jan 2027 18:45 UTC (in 30 days)"},
		{"one day is singular", time.Date(2026, time.December, 31, 23, 59, 0, 0, time.UTC), day, "Expires Fri 1 Jan 2027 23:59 UTC (in 1 day)"},
		{"a non UTC clock is shown in UTC", time.Date(2026, time.March, 14, 23, 30, 0, 0, time.FixedZone("x", -5*3600)), 7 * day, "Expires Sun 22 Mar 2026 04:30 UTC (in 7 days)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := expiryPreview(tt.now, tt.d); got != tt.want {
				t.Fatalf("expiryPreview = %q, want %q", got, tt.want)
			}
		})
	}
}

func lifetimeKeys(t *testing.T, l tokenLifetime, ceiling time.Duration, keys ...keyMsg) tokenLifetime {
	t.Helper()
	for _, k := range keys {
		l, _ = l.handleKey(k, ceiling)
	}
	return l
}

func TestLifetimeLeftRightMoveBetweenOptionsWithoutWrapping(t *testing.T) {
	t.Parallel()
	ceiling := lifetimeCeiling()
	opts := lifetimeOptions(ceiling)
	label := func(l tokenLifetime) string { return opts[l.selected(ceiling)].Label }
	var l tokenLifetime
	l = lifetimeKeys(t, l, ceiling, testKeyLeft())
	if got := label(l); got != "7 days" {
		t.Fatalf("after Left = %q, want 7 days", got)
	}
	l = lifetimeKeys(t, l, ceiling, testKeyLeft())
	if got := label(l); got != "7 days" {
		t.Fatalf("Left at the first option = %q, want it to stay", got)
	}
	l = lifetimeKeys(t, l, ceiling, testKeyRight(), testKeyRight())
	if got := label(l); got != "Custom" || !l.isCustom(ceiling) {
		t.Fatalf("after two Right = %q, want Custom", got)
	}
	l = lifetimeKeys(t, l, ceiling, testKeyRight())
	if got := label(l); got != "Custom" {
		t.Fatalf("Right at the last option = %q, want it to stay", got)
	}
}

func TestLifetimeKeysPresetsIgnoreTextAndDigits(t *testing.T) {
	t.Parallel()
	ceiling := lifetimeCeiling()
	var l tokenLifetime
	for _, k := range []keyMsg{testKeyRunes("7"), testKeyRunes("1"), testKeyRunes("j"), testKeyRunes("k"), testKeySpace(), testKeyBackspace()} {
		got, _ := l.handleKey(k, ceiling)
		if got != l {
			t.Fatalf("key %#v changed a preset selection: %#v -> %#v", k, l, got)
		}
	}
	d, ok := l.duration(ceiling)
	if !ok || d != 30*day {
		t.Fatalf("duration after stray keys = %v, %v; want 30 days", d, ok)
	}
}

func TestLifetimeCustomDigitsOnly(t *testing.T) {
	t.Parallel()
	ceiling := lifetimeCeiling()
	l := lifetimeKeys(t, tokenLifetime{}, ceiling, testKeyRight())
	l = lifetimeKeys(t, l, ceiling, testKeyRight()) // Custom
	if !l.isCustom(ceiling) {
		t.Fatalf("setup: Custom not selected")
	}
	l = lifetimeKeys(t, l, ceiling, testKeyRunes("j"), testKeyRunes("k"), testKeyRunes("x"), testKeySpace(), testKeyRunes("-"), testKeyRunes("."))
	if l.CustomDays != "" {
		t.Fatalf("non digits typed into days: %q", l.CustomDays)
	}
	if !l.isCustom(ceiling) {
		t.Fatalf("j/k must not move the selection")
	}
	l = lifetimeKeys(t, l, ceiling, testKeyRunes("1"), testKeyRunes("5"))
	if l.CustomDays != "15" {
		t.Fatalf("days = %q, want 15", l.CustomDays)
	}
	l = lifetimeKeys(t, l, ceiling, testKeyBackspace())
	if l.CustomDays != "1" {
		t.Fatalf("days after Backspace = %q, want 1", l.CustomDays)
	}
	l = lifetimeKeys(t, l, ceiling, testKeyBackspace(), testKeyBackspace())
	if l.CustomDays != "" {
		t.Fatalf("days after extra Backspace = %q, want empty", l.CustomDays)
	}
	// Paste is filtered to digits.
	l = lifetimeKeys(t, l, ceiling, keyMsg{Text: "1a2 b-3"})
	if l.CustomDays != "123" {
		t.Fatalf("days after paste = %q, want 123", l.CustomDays)
	}
	l = lifetimeKeys(t, tokenLifetime{Index: 2, Chosen: true}, ceiling, keyMsg{Text: "enter"})
	if l.CustomDays != "" {
		t.Fatalf("pasted key name leaked into days: %q", l.CustomDays)
	}
}

func TestLifetimeCustomDurationAndValidation(t *testing.T) {
	t.Parallel()
	ceiling := lifetimeCeiling()
	custom := func(days string) tokenLifetime { return tokenLifetime{Index: 2, Chosen: true, CustomDays: days} }
	for days, want := range map[string]time.Duration{"1": day, "15": 15 * day, "30": 30 * day} {
		d, ok := custom(days).duration(ceiling)
		if !ok || d != want {
			t.Errorf("custom %s days = %v, %v; want %v", days, d, ok, want)
		}
	}
	for _, days := range []string{"", "0", "31", "000"} {
		if d, ok := custom(days).duration(ceiling); ok {
			t.Errorf("custom %q accepted as %v, want invalid", days, d)
		}
	}
}

func TestLifetimeDigitInputIsBounded(t *testing.T) {
	t.Parallel()
	ceiling := lifetimeCeiling()
	l := tokenLifetime{Index: 2, Chosen: true}
	l = lifetimeKeys(t, l, ceiling, keyMsg{Text: strings.Repeat("9", 500)})
	if len(l.CustomDays) > 6 {
		t.Fatalf("days input grew to %d digits", len(l.CustomDays))
	}
	if _, ok := l.duration(ceiling); ok {
		t.Fatalf("a huge value must be invalid")
	}
}
