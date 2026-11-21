package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Shared by the Robots and Tokens screens: how much life a token has left,
// how that is worded and colored, and the one-time-secret layout.

// expiringSoon is the time left under which a token is flagged amber.
const expiringSoon = 7 * lifetimeDay

type expiryTone int

const (
	expiryHealthy expiryTone = iota
	expirySoon
	expiryExpired
)

// expiryToneOf is red once nothing is left, amber under a week, green above.
func expiryToneOf(left time.Duration) expiryTone {
	switch {
	case left <= 0:
		return expiryExpired
	case left < expiringSoon:
		return expirySoon
	}
	return expiryHealthy
}

func (t expiryTone) style(theme adminTheme) lipgloss.Style {
	switch t {
	case expiryExpired:
		return theme.error
	case expirySoon:
		return theme.warning
	}
	return theme.success
}

// timeLeftText words the time left in days, or hours below one day. Never
// seconds. Whole days are floored, so "6 days" means more than 6 and under 7.
func timeLeftText(left time.Duration) string {
	switch {
	case left <= 0:
		return "expired"
	case left >= lifetimeDay:
		return pluralUnit(int64(left/lifetimeDay), "day")
	case left >= lifetimeHour:
		return pluralUnit(int64(left/lifetimeHour), "hour")
	}
	return "<1 hour"
}

// shortAccessor shortens a long accessor to its first 8 and last 4 characters
// ("act_9c41…e0d2"); a short one is shown whole.
func shortAccessor(accessor string) string {
	runes := []rune(accessor)
	if len(runes) <= 13 {
		return accessor
	}
	return string(runes[:8]) + "…" + string(runes[len(runes)-4:])
}

// timeLeftBar draws the time left out of the token's full lifetime, width
// cells wide: filled in the tone's color, the rest as a dim track. An expired
// token has an empty bar.
func timeLeftBar(theme adminTheme, left, total time.Duration, width int) string {
	if width < 1 {
		return ""
	}
	filled := 0
	if left > 0 {
		filled = width
		if total > left {
			filled = int(float64(width)*float64(left)/float64(total) + 0.5)
			filled = max(filled, 1)
		}
	}
	track := lipgloss.NewStyle().Foreground(lipgloss.Color(paletteBorderHex))
	return expiryToneOf(left).style(theme).Render(strings.Repeat("█", filled)) + track.Render(strings.Repeat("░", width-filled))
}

// dayMonth formats a date as "28 Oct"; withYear adds the year.
func dayMonth(t time.Time, withYear bool) string {
	if withYear {
		return t.UTC().Format("2 Jan 2006")
	}
	return t.UTC().Format("2 Jan")
}

// exactExpiry is the exact moment a credential stops working.
func exactExpiry(t time.Time) string { return t.UTC().Format("Mon 2 Jan 2006 15:04") + " UTC" }

// fromToday words a lifetime counted from the clock: "30 days from today",
// or "12 hours from now" below one day.
func fromToday(d time.Duration) string {
	if d < lifetimeDay {
		return humanLifetime(d) + " from now"
	}
	return humanLifetime(d) + " from today"
}

// isCommandRune reports a typed command letter. A pasted letter never counts,
// so pasting "n" into a list cannot open a form.
func isCommandRune(msg keyMsg, candidates ...rune) bool {
	return !isPasteKey(msg) && isRuneKey(msg, candidates...)
}

// sanitizeFieldText drops control characters (and, when spaces is false,
// whitespace) from typed or pasted text.
func sanitizeFieldText(text string, spaces bool) string {
	var b strings.Builder
	for _, r := range text {
		switch {
		case unicode.IsControl(r):
		case !spaces && unicode.IsSpace(r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// hardWrap breaks text into lines of at most width cells, cutting words if it
// must: a secret is never truncated, only wrapped.
func hardWrap(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	return strings.Split(ansi.Hardwrap(text, width, true), "\n")
}

// fitSections joins blocks of lines into at most rows rows. Blocks are
// separated by a blank line; when that does not fit the blanks go first, then
// the last blocks, then whatever still overflows.
func fitSections(blocks [][]string, rows int) []string {
	join := func(sep bool) []string {
		var out []string
		for i, block := range blocks {
			if i > 0 && sep {
				out = append(out, "")
			}
			out = append(out, block...)
		}
		return out
	}
	if lines := join(true); len(lines) <= rows {
		return lines
	}
	for len(blocks) > 0 {
		if lines := join(false); len(lines) <= rows {
			return lines
		}
		if len(blocks) == 1 {
			break
		}
		blocks = blocks[:len(blocks)-1]
	}
	lines := join(false)
	return lines[:max(min(rows, len(lines)), 0)]
}

// padRight pads styled text with spaces up to width cells.
func padRight(text string, width int) string {
	return text + strings.Repeat(" ", max(width-lipgloss.Width(text), 0))
}

// spread puts left and right at the two ends of a width-cell line.
func spread(left, right string, width int) string {
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

// renderEntryField draws a one-line text input of width cells. A focused empty
// field shows only the cursor; an unfocused empty one shows the placeholder.
// Text longer than the field keeps its tail visible.
func renderEntryField(theme adminTheme, value, placeholder string, focused bool, width int) string {
	style := theme.input
	if focused {
		style = theme.inputFocus
	}
	style = style.Width(max(width, 4))
	budget := max(width, 4) - 2
	text := value
	switch {
	case focused:
		text += "▏"
	case value == "":
		style = style.Foreground(theme.muted.GetForeground())
		text = placeholder
	}
	if runes := []rune(text); lipgloss.Width(text) > budget {
		for lipgloss.Width("…"+string(runes)) > budget && len(runes) > 0 {
			runes = runes[1:]
		}
		text = "…" + string(runes)
	}
	return style.Render(text)
}

// secretBox draws the one-time secret block: the accessor and the whole
// secret, wrapped (never cut) to the box width, in a warning-colored border.
func secretBox(theme adminTheme, accessor, secret string, width int) []string {
	const labelWidth = 10
	inner := max(width-4, labelWidth+1)
	value := inner - labelWidth
	lines := []string{padRight(theme.muted.Render("Accessor"), labelWidth) + theme.text.Bold(true).Render(accessor)}
	for i, part := range hardWrap(secret, value) {
		label := ""
		if i == 0 {
			label = theme.muted.Render("Secret")
		}
		lines = append(lines, padRight(label, labelWidth)+theme.text.Bold(true).Render(part))
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(paletteWarningHex)).
		Padding(0, 1).
		Width(width).
		Render(strings.Join(lines, "\n"))
	return strings.Split(box, "\n")
}

// oneTimeSecretWarning is the line under a secret.
const oneTimeSecretWarning = "Shown once. Copy it before leaving: it cannot be displayed again."
