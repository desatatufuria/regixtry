package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	domainauth "regixtry/internal/domain/auth"
)

// The lifetime selector shared by the Create Token and Create Robot forms
// (both issue the same kind of credential). Everything about what the
// operator may choose lives here: the preset list, the ceiling that filters
// it, the days-to-duration conversion and the expiry preview.

// lifetimePreset is one ready-made lifetime. Presets are the ONLY place the
// list of lifetimes is spelled; options longer than the ceiling are filtered
// out by lifetimeOptions, never shown.
type lifetimePreset struct {
	Days  int
	Label string
}

var lifetimePresets = []lifetimePreset{
	{Days: 7, Label: "7 days"},
	{Days: 30, Label: "30 days"},
}

// lifetimeCeiling is the longest lifetime the server accepts. It is the
// server default today; when the backend grows a separate maximum only this
// reference (and lifetimePresets) change.
func lifetimeCeiling() time.Duration { return domainauth.DefaultAdminTokenTTL }

// maxCustomDigits bounds the Custom days input; anything longer is far above
// any ceiling and would only risk overflow while parsing.
const maxCustomDigits = 6

const customLifetimeLabel = "Custom"

type lifetimeOption struct {
	Days   int
	Label  string
	Custom bool
}

// lifetimeCeilingDays is the ceiling in whole days.
func lifetimeCeilingDays(ceiling time.Duration) int { return int(ceiling / (24 * time.Hour)) }

// lifetimeOptions returns the presets that fit under the ceiling followed by
// the Custom option, which is always last.
func lifetimeOptions(ceiling time.Duration) []lifetimeOption {
	var options []lifetimeOption
	for _, preset := range lifetimePresets {
		if lifetimeDuration(preset.Days) > ceiling {
			continue
		}
		options = append(options, lifetimeOption{Days: preset.Days, Label: preset.Label})
	}
	return append(options, lifetimeOption{Label: customLifetimeLabel, Custom: true})
}

// lifetimeDuration is the one place days become a duration, so a
// days/seconds mix-up cannot happen elsewhere.
func lifetimeDuration(days int) time.Duration { return time.Duration(days) * 24 * time.Hour }

// parseCustomDays accepts a whole number of days from 1 to the ceiling.
func parseCustomDays(text string, ceiling time.Duration) (int, bool) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > maxCustomDigits {
		return 0, false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	days, err := strconv.Atoi(text)
	if err != nil || days < 1 || days > lifetimeCeilingDays(ceiling) {
		return 0, false
	}
	return days, true
}

// expiryPreview states the exact moment a credential created at now with
// lifetime d stops working. Plain time arithmetic on the clock value, in UTC.
func expiryPreview(now time.Time, d time.Duration) string {
	expires := now.UTC().Add(d)
	days := int(d / (24 * time.Hour))
	unit := "days"
	if days == 1 {
		unit = "day"
	}
	return fmt.Sprintf("Expires %s UTC (in %d %s)", expires.Format("Mon 2 Jan 2006 15:04"), days, unit)
}

// tokenLifetime is the selector state held by both forms. Its zero value
// means "untouched", which selects the default option; Chosen records that
// the operator moved the selection.
type tokenLifetime struct {
	Index      int
	Chosen     bool
	CustomDays string
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
		if lifetimeDuration(option.Days) <= domainauth.DefaultAdminTokenTTL {
			best = i
		}
	}
	return best
}

func (l tokenLifetime) isCustom(ceiling time.Duration) bool {
	return lifetimeOptions(ceiling)[l.selected(ceiling)].Custom
}

// duration is the lifetime to send; ok is false when Custom holds no valid
// number of days.
func (l tokenLifetime) duration(ceiling time.Duration) (time.Duration, bool) {
	option := lifetimeOptions(ceiling)[l.selected(ceiling)]
	if !option.Custom {
		return lifetimeDuration(option.Days), true
	}
	days, ok := parseCustomDays(l.CustomDays, ceiling)
	if !ok {
		return 0, false
	}
	return lifetimeDuration(days), true
}

// handleKey applies a key to the selector when it has focus and reports
// whether the selector consumed it. Left/Right (arrow keys only) move
// between options; with Custom selected, digits (and pasted text filtered to
// digits) fill the days input and Backspace deletes one. No letter moves
// anything, and presets have no digit shortcuts.
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
		l.CustomDays = trimLastRune(l.CustomDays)
		return l, true
	case isTextKey(msg):
		var digits strings.Builder
		for _, r := range keyText(msg) {
			if r >= '0' && r <= '9' {
				digits.WriteRune(r)
			}
		}
		l.CustomDays = truncateRunes(l.CustomDays+digits.String(), maxCustomDigits)
		return l, true
	}
	return l, false
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
