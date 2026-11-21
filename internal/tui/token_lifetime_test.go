package tui

import (
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
)

const day = 24 * time.Hour

const (
	hour = time.Hour
	year = 365 * day
)

func TestLifetimeCeilingIsTheServerMaximum(t *testing.T) {
	t.Parallel()
	if got := lifetimeCeiling(); got != domainauth.MaxAdminTokenTTL {
		t.Fatalf("lifetimeCeiling() = %v, want domainauth.MaxAdminTokenTTL (730 days)", got)
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
		{"the real ceiling shows every preset", lifetimeCeiling(), "7 days|30 days|90 days|1 year|2 years|Custom"},
		{"a ceiling of 30 days", 30 * day, "7 days|30 days|Custom"},
		{"a ceiling equal to a preset keeps it", 365 * day, "7 days|30 days|90 days|1 year|Custom"},
		{"a ceiling below every preset leaves Custom only", 3 * day, "Custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := labels(tt.ceiling); got != tt.want {
				t.Fatalf("lifetimeOptions(%v) labels = %q, want %q", tt.ceiling, got, tt.want)
			}
		})
	}
	opts := lifetimeOptions(lifetimeCeiling())
	want := []time.Duration{7 * day, 30 * day, 90 * day, 365 * day, 730 * day}
	for i, d := range want {
		if opts[i].Duration != d || opts[i].Custom {
			t.Fatalf("option %d = %#v, want %v", i, opts[i], d)
		}
	}
	if !opts[len(opts)-1].Custom {
		t.Fatalf("only the last option is Custom: %#v", opts)
	}
}

func TestLifetimeDefaultSelectionIs30Days(t *testing.T) {
	t.Parallel()
	var l tokenLifetime
	ceiling := lifetimeCeiling()
	if got := lifetimeOptions(ceiling)[l.selected(ceiling)].Label; got != "30 days" {
		t.Fatalf("default selection = %q, want 30 days", got)
	}
	d, ok := l.duration(ceiling)
	if !ok || d != domainauth.DefaultAdminTokenTTL {
		t.Fatalf("default duration = %v, %v; want the 30 day default", d, ok)
	}
	if got := lifetimeOptions(10 * day)[l.selected(10*day)].Label; got != "7 days" {
		t.Fatalf("default under a 10 day ceiling = %q, want 7 days", got)
	}
}

func TestParseCustomLifetime(t *testing.T) {
	t.Parallel()
	ceiling := lifetimeCeiling()
	tests := []struct {
		in     string
		want   time.Duration
		wantOK bool
	}{
		{"12h", 12 * hour, true},
		{"1h", hour, true},
		{"24h", day, true},
		{"30d", 30 * day, true},
		{"730d", 730 * day, true},
		{"17520h", 730 * day, true},
		{"2y", 730 * day, true},
		{"1y", 365 * day, true},
		{"5", 5 * day, true},
		{"5D", 5 * day, true},
		{"12H", 12 * hour, true},
		{"007", 7 * day, true},
		{"3y", 0, false},
		{"731d", 0, false},
		{"17521h", 0, false},
		{"0h", 0, false},
		{"0", 0, false},
		{"", 0, false},
		{"h", 0, false},
		{"y", 0, false},
		{"5 d", 0, false},
		{"5dd", 0, false},
		{"d5", 0, false},
		{"abc", 0, false},
		{"-5", 0, false},
		{"1.5d", 0, false},
		{"99999999d", 0, false},
		{"9999999y", 0, false},
		{"30 days", 0, false},
		{"٣d", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseCustomLifetime(tt.in, ceiling)
		if ok != tt.wantOK || got != tt.want {
			t.Errorf("parseCustomLifetime(%q) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.wantOK)
		}
	}
	if _, ok := parseCustomLifetime("31d", 30*day); ok {
		t.Errorf("31d must exceed a 30 day ceiling")
	}
}

func TestHumanLifetimePicksTheNaturalUnit(t *testing.T) {
	t.Parallel()
	tests := map[time.Duration]string{
		hour:      "1 hour",
		12 * hour: "12 hours",
		36 * hour: "36 hours",
		day:       "1 day",
		45 * day:  "45 days",
		7 * day:   "7 days",
		364 * day: "364 days",
		year:      "1 year",
		366 * day: "366 days",
		2 * year:  "2 years",
	}
	for d, want := range tests {
		if got := humanLifetime(d); got != want {
			t.Errorf("humanLifetime(%v) = %q, want %q", d, got, want)
		}
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
		{"12 hours", time.Date(2026, time.March, 14, 9, 30, 0, 0, time.UTC), 12 * hour, "Expires Sat 14 Mar 2026 21:30 UTC (in 12 hours)"},
		{"1 hour is singular", time.Date(2026, time.March, 14, 23, 30, 0, 0, time.UTC), hour, "Expires Sun 15 Mar 2026 00:30 UTC (in 1 hour)"},
		{"crosses a month end", time.Date(2026, time.January, 31, 23, 5, 0, 0, time.UTC), 7 * day, "Expires Sat 7 Feb 2026 23:05 UTC (in 7 days)"},
		{"crosses a leap day", time.Date(2028, time.February, 25, 12, 0, 0, 0, time.UTC), 7 * day, "Expires Fri 3 Mar 2028 12:00 UTC (in 7 days)"},
		{"lands on a leap day", time.Date(2028, time.January, 30, 8, 15, 0, 0, time.UTC), 30 * day, "Expires Tue 29 Feb 2028 08:15 UTC (in 30 days)"},
		{"crosses a year end", time.Date(2026, time.December, 20, 18, 45, 0, 0, time.UTC), 30 * day, "Expires Tue 19 Jan 2027 18:45 UTC (in 30 days)"},
		{"one day is singular", time.Date(2026, time.December, 31, 23, 59, 0, 0, time.UTC), day, "Expires Fri 1 Jan 2027 23:59 UTC (in 1 day)"},
		{"one year", time.Date(2026, time.March, 14, 9, 30, 0, 0, time.UTC), year, "Expires Sun 14 Mar 2027 09:30 UTC (in 1 year)"},
		{"two years", time.Date(2026, time.March, 14, 9, 30, 0, 0, time.UTC), 2 * year, "Expires Mon 13 Mar 2028 09:30 UTC (in 2 years)"},
		{"two years from a leap day", time.Date(2028, time.February, 29, 0, 0, 0, 0, time.UTC), 2 * year, "Expires Thu 28 Feb 2030 00:00 UTC (in 2 years)"},
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
	l = lifetimeKeys(t, l, ceiling, testKeyRight(), testKeyRight(), testKeyRight(), testKeyRight(), testKeyRight())
	if got := label(l); got != "Custom" || !l.isCustom(ceiling) {
		t.Fatalf("after five Right = %q, want Custom", got)
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
	for _, k := range []keyMsg{testKeyRunes("7"), testKeyRunes("1"), testKeyRunes("j"), testKeyRunes("k"), testKeyRunes("d"), testKeySpace(), testKeyBackspace()} {
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

func customSelected(t *testing.T) (tokenLifetime, time.Duration) {
	t.Helper()
	ceiling := lifetimeCeiling()
	l := lifetimeKeys(t, tokenLifetime{}, ceiling, testKeyRight(), testKeyRight(), testKeyRight(), testKeyRight())
	if !l.isCustom(ceiling) {
		t.Fatalf("setup: Custom not selected")
	}
	return l, ceiling
}

func TestLifetimeCustomTyping(t *testing.T) {
	t.Parallel()
	l, ceiling := customSelected(t)
	l = lifetimeKeys(t, l, ceiling, testKeyRunes("j"), testKeyRunes("k"), testKeyRunes("x"), testKeySpace(), testKeyRunes("-"), testKeyRunes("."), testKeyRunes("h"))
	if l.Custom != "" {
		t.Fatalf("invalid characters typed into the input: %q", l.Custom)
	}
	if !l.isCustom(ceiling) {
		t.Fatalf("j/k must not move the selection")
	}
	l = lifetimeKeys(t, l, ceiling, testKeyRunes("1"), testKeyRunes("2"), testKeyRunes("H"))
	if l.Custom != "12h" {
		t.Fatalf("input = %q, want 12h (unit stored lowercase)", l.Custom)
	}
	l = lifetimeKeys(t, l, ceiling, testKeyRunes("d"))
	if l.Custom != "12d" {
		t.Fatalf("second unit letter must replace the first, got %q", l.Custom)
	}
	l = lifetimeKeys(t, l, ceiling, testKeyRunes("5"), testKeyRunes("k"))
	if l.Custom != "12d" {
		t.Fatalf("digits after the unit must be ignored, got %q", l.Custom)
	}
	l = lifetimeKeys(t, l, ceiling, testKeyBackspace())
	if l.Custom != "12" {
		t.Fatalf("input after Backspace = %q, want 12", l.Custom)
	}
	l = lifetimeKeys(t, l, ceiling, testKeyBackspace(), testKeyBackspace(), testKeyBackspace())
	if l.Custom != "" {
		t.Fatalf("input after extra Backspace = %q, want empty", l.Custom)
	}
}

func TestLifetimeCustomPasteIsFiltered(t *testing.T) {
	t.Parallel()
	l, ceiling := customSelected(t)
	l = lifetimeKeys(t, l, ceiling, keyMsg{Text: "1a2 b-3"})
	if l.Custom != "123" {
		t.Fatalf("input after paste = %q, want 123", l.Custom)
	}
	l = lifetimeKeys(t, tokenLifetime{Index: 5, Chosen: true}, ceiling, keyMsg{Text: "enter"})
	if l.Custom != "" {
		t.Fatalf("pasted key name leaked into the input: %q", l.Custom)
	}
	l = lifetimeKeys(t, tokenLifetime{Index: 5, Chosen: true}, ceiling, keyMsg{Text: "12 H"})
	if l.Custom != "12h" {
		t.Fatalf("paste of '12 H' = %q, want 12h", l.Custom)
	}
	// "30 days" filters to 30 d y: the second unit replaces the first (30y), invalid.
	l = lifetimeKeys(t, tokenLifetime{Index: 5, Chosen: true}, ceiling, keyMsg{Text: "30 days"})
	if _, ok := l.duration(ceiling); ok {
		t.Fatalf("a pasted '30 days' must not become a valid lifetime silently: %q", l.Custom)
	}
}

func TestLifetimeCustomDurationAndValidation(t *testing.T) {
	t.Parallel()
	ceiling := lifetimeCeiling()
	custom := func(text string) tokenLifetime { return tokenLifetime{Index: 5, Chosen: true, Custom: text} }
	for text, want := range map[string]time.Duration{"12h": 12 * hour, "36h": 36 * hour, "45": 45 * day, "2y": 2 * year, "730d": 730 * day} {
		d, ok := custom(text).duration(ceiling)
		if !ok || d != want {
			t.Errorf("custom %q = %v, %v; want %v", text, d, ok, want)
		}
	}
	for _, text := range []string{"", "0", "0h", "731d", "3y", "17521h", "h"} {
		if d, ok := custom(text).duration(ceiling); ok {
			t.Errorf("custom %q accepted as %v, want invalid", text, d)
		}
	}
}

func TestLifetimeCustomInputIsBounded(t *testing.T) {
	t.Parallel()
	ceiling := lifetimeCeiling()
	l := tokenLifetime{Index: 5, Chosen: true}
	l = lifetimeKeys(t, l, ceiling, keyMsg{Text: strings.Repeat("9", 500) + "d"})
	if len(l.Custom) > 7 {
		t.Fatalf("input grew to %d characters", len(l.Custom))
	}
	if _, ok := l.duration(ceiling); ok {
		t.Fatalf("a huge value must be invalid")
	}
}
