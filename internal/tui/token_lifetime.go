package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	domainauth "regixtry/internal/domain/auth"
)

// The lifetime selector shared by the Create Token and Create Robot forms
// (both issue the same kind of credential). Everything about what the
// operator may choose lives here: the preset list, the ceiling that filters
// it, the days-to-duration conversion and the expiry preview.

// lifetimePreset is one ready-made lifetime. Presets are the ONLY place the
// list of lifetimes is spelled; options longer than the ceiling are filtered
// out by lifetimeOptions, never shown. Labels are in days and years, never
// seconds.
type lifetimePreset struct {
	Duration time.Duration
	Label    string
}

const (
	lifetimeHour = time.Hour
	lifetimeDay  = 24 * time.Hour
	lifetimeYear = 365 * lifetimeDay
)

var lifetimePresets = []lifetimePreset{
	{Duration: 7 * lifetimeDay, Label: "7 days"},
	{Duration: 30 * lifetimeDay, Label: "30 days"},
	{Duration: 90 * lifetimeDay, Label: "90 days"},
	{Duration: lifetimeYear, Label: "1 year"},
	{Duration: 2 * lifetimeYear, Label: "2 years"},
}

// lifetimeCeiling is the longest lifetime the server accepts.
func lifetimeCeiling() time.Duration { return domainauth.MaxAdminTokenTTL }

// maxCustomLifetimeChars bounds the Custom input (digits plus one unit
// letter); it makes overflow while parsing impossible.
const maxCustomLifetimeChars = 7

const customLifetimeLabel = "Custom"

type lifetimeOption struct {
	Duration time.Duration
	Label    string
	Custom   bool
}

// lifetimeOptions returns the presets that fit under the ceiling followed by
// the Custom option, which is always last.
func lifetimeOptions(ceiling time.Duration) []lifetimeOption {
	var options []lifetimeOption
	for _, preset := range lifetimePresets {
		if preset.Duration > ceiling {
			continue
		}
		options = append(options, lifetimeOption{Duration: preset.Duration, Label: preset.Label})
	}
	return append(options, lifetimeOption{Label: customLifetimeLabel, Custom: true})
}

// lifetimeUnit maps a unit letter (any case) to its length.
func lifetimeUnit(r rune) (time.Duration, bool) {
	switch r {
	case 'h', 'H':
		return lifetimeHour, true
	case 'd', 'D':
		return lifetimeDay, true
	case 'y', 'Y':
		return lifetimeYear, true
	}
	return 0, false
}

// parseCustomLifetime is the one place the Custom text becomes a duration. The
// grammar is the API's ttl field: a positive whole number followed by ONE
// unit letter h (hours), d (days) or y (years, 365 days); no unit means days.
// The minimum is 1 hour and the maximum is the ceiling.
func parseCustomLifetime(text string, ceiling time.Duration) (time.Duration, bool) {
	unit := lifetimeDay
	digits := text
	if n := len(text); n > 0 {
		if u, ok := lifetimeUnit(rune(text[n-1])); ok {
			unit, digits = u, text[:n-1]
		}
	}
	if digits == "" || len(digits) > maxCustomLifetimeChars {
		return 0, false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	count, err := strconv.Atoi(digits)
	if err != nil || count < 1 {
		return 0, false
	}
	// At most 7 digits times 8760 hours cannot overflow an int64 of hours.
	hours := int64(count) * int64(unit/lifetimeHour)
	if hours > int64(ceiling/lifetimeHour) {
		return 0, false
	}
	return time.Duration(hours) * lifetimeHour, true
}

// humanLifetime words a duration in its natural unit: years when it is a
// multiple of 365 days, else days when a multiple of 24 hours, else hours.
func humanLifetime(d time.Duration) string {
	count, unit := int64(d/lifetimeHour), "hour"
	switch {
	case d > 0 && d%lifetimeYear == 0:
		count, unit = int64(d/lifetimeYear), "year"
	case d > 0 && d%lifetimeDay == 0:
		count, unit = int64(d/lifetimeDay), "day"
	}
	if count != 1 {
		unit += "s"
	}
	return fmt.Sprintf("%d %s", count, unit)
}

// expiryPreview states the exact moment a credential created at now with
// lifetime d stops working. Plain time arithmetic on the clock value, in UTC.
func expiryPreview(now time.Time, d time.Duration) string {
	expires := now.UTC().Add(d)
	return fmt.Sprintf("Expires %s UTC (in %s)", expires.Format("Mon 2 Jan 2006 15:04"), humanLifetime(d))
}

// tokenLifetime is the selector state held by both forms. Its zero value
// means "untouched", which selects the default option; Chosen records that
// the operator moved the selection.
type tokenLifetime struct {
	Index  int
	Chosen bool
	Custom string
}

// selected is the index into lifetimeOptions(ceiling). Untouched, it is the
// preset equal to the server default, or the longest preset below it.
func (l tokenLifetime) selected(ceiling time.Duration) int {
	options := lifetimeOptions(ceiling)
	if l.Chosen {
		return max(0, min(l.Index, len(options)-1))
	}
	best := len(options) - 1 // Custom, when no preset fits
	for i, option := range options {
		if option.Custom {
			continue
		}
		if option.Duration <= domainauth.DefaultAdminTokenTTL {
			best = i
		}
	}
	return best
}

func (l tokenLifetime) isCustom(ceiling time.Duration) bool {
	return lifetimeOptions(ceiling)[l.selected(ceiling)].Custom
}

// duration is the lifetime to send; ok is false when Custom holds no valid
// value.
func (l tokenLifetime) duration(ceiling time.Duration) (time.Duration, bool) {
	option := lifetimeOptions(ceiling)[l.selected(ceiling)]
	if !option.Custom {
		return option.Duration, true
	}
	return parseCustomLifetime(l.Custom, ceiling)
}

// typeCustom applies one typed character to the Custom text: digits extend
// the number (never after the unit), a unit letter h, d or y (any case, stored
// lowercase) sets or replaces the trailing unit once a number exists, and
// anything else is ignored. The text never exceeds maxCustomLifetimeChars.
func typeCustom(text string, r rune) string {
	if _, ok := lifetimeUnit(r); ok {
		r = unicode.ToLower(r)
		if n := len(text); n > 0 && text[n-1] >= '0' && text[n-1] <= '9' {
			if n < maxCustomLifetimeChars {
				return text + string(r)
			}
			return text
		} else if n > 0 { // a unit is already there: replace it
			return text[:n-1] + string(r)
		}
		return text
	}
	if r < '0' || r > '9' || len(text) >= maxCustomLifetimeChars {
		return text
	}
	if n := len(text); n > 0 && (text[n-1] < '0' || text[n-1] > '9') {
		return text
	}
	return text + string(r)
}

// pasteCustom handles pasted text, which is all or nothing: after trimming
// whitespace it must be a complete valid Custom lifetime (digits and an
// optional h, d or y, any case) and then it REPLACES the current input, unit
// lowercased. Anything else (prose such as "1 day") leaves the input as it was,
// so a paste can never pick a different unit than the one written.
func pasteCustom(current, pasted string) string {
	pasted = strings.ToLower(strings.TrimSpace(pasted))
	if len(pasted) > maxCustomLifetimeChars {
		return current
	}
	if _, ok := parseCustomLifetime(pasted, time.Duration(1<<62)); !ok {
		return current
	}
	return pasted
}

// handleKey applies a key to the selector when it has focus and reports
// whether the selector consumed it. Left/Right (arrow keys only) move between
// options; with Custom selected, digits and one trailing unit letter (and
// pasted text filtered the same way) fill the input and Backspace deletes one
// character. No other letter moves anything, and presets have no shortcuts.
func (l tokenLifetime) handleKey(msg keyMsg, ceiling time.Duration) (tokenLifetime, bool) {
	options := lifetimeOptions(ceiling)
	current := l.selected(ceiling)
	switch {
	case isMoveLeftKey(msg):
		if current > 0 {
			l.Index, l.Chosen = current-1, true
		}
		return l, true
	case isMoveRightKey(msg):
		if current < len(options)-1 {
			l.Index, l.Chosen = current+1, true
		}
		return l, true
	}
	if !options[current].Custom {
		return l, isBackspaceKey(msg) || isTextKey(msg)
	}
	switch {
	case isBackspaceKey(msg):
		l.Custom = trimLastRune(l.Custom)
		return l, true
	case isTextKey(msg):
		text := keyText(msg)
		if utf8.RuneCountInString(text) > 1 {
			l.Custom = pasteCustom(l.Custom, text)
			return l, true
		}
		for _, r := range text {
			l.Custom = typeCustom(l.Custom, r)
		}
		return l, true
	}
	return l, false
}

// renderLifetimeField draws the selector for either form on three rows: the
// label with the option row (and the days input when Custom is selected), then
// either the exact expiry preview or the validation message, then the leak
// warning. One row for label and options keeps the Create Robot form, the
// tallest one, inside the 24 row minimum with a full suggestion list.
func renderLifetimeField(theme adminTheme, l tokenLifetime, now time.Time, focused bool) string {
	ceiling := lifetimeCeiling()
	selected := l.selected(ceiling)
	var options []string
	for i, option := range lifetimeOptions(ceiling) {
		mark := "( ) " + option.Label
		if i == selected {
			mark = "(*) " + option.Label
			if focused {
				mark = theme.selected.Render(mark)
			}
		}
		options = append(options, mark)
	}
	row := "Lifetime  " + strings.Join(options, "  ")
	if l.isCustom(ceiling) {
		row += "  " + renderCustomInput(theme, l.Custom, focused)
	}
	lines := []string{row}
	if duration, ok := l.duration(ceiling); ok {
		lines = append(lines, expiryPreview(now, duration))
	} else {
		lines = append(lines, theme.error.Render(fmt.Sprintf("Enter a whole number followed by h, d or y, up to %s (for example 12h, 30d, 2y)", humanLifetime(ceiling))))
	}
	lines = append(lines, theme.muted.Render("A leaked token stays valid until it expires or you revoke it."))
	return strings.Join(lines, "\n")
}

func renderCustomInput(theme adminTheme, text string, focused bool) string {
	style := theme.input
	if focused {
		style = theme.inputFocus
	}
	if text == "" {
		return style.Render(theme.muted.Render("e.g. 12h, 30d, 2y"))
	}
	return style.Render(text)
}
