package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"regixtry/internal/ports"
)

// Shared helpers of the user-management screens (Users, Edit user, New user,
// Reset password).

// userPasswordMinLength is the server's password rule
// (internal/app/auth/service.go validatePassword): the TRIMMED password must
// be at least this long. The client applies the same rule and message, and the
// server stays the authority.
const userPasswordMinLength = 8

// userPasswordTooShort is the server's own message for a short password.
const userPasswordTooShort = "password must be at least 8 characters"

// validateNewPassword applies the server's password rule on the client. It
// returns "" when the password passes.
func validateNewPassword(password string) string {
	if len(strings.TrimSpace(password)) < userPasswordMinLength {
		return userPasswordTooShort
	}
	return ""
}

// userRole names the account's role the way the board does.
func userRole(user ports.AdminUser) string {
	switch {
	case user.IsAdmin:
		return "admin"
	case user.IsReadOnly:
		return "read-only"
	}
	return "user"
}

// userRoleFlags words the role flags of the detail views: both flags are named
// when both are set.
func userRoleFlags(user ports.AdminUser) string {
	switch {
	case user.IsAdmin && user.IsReadOnly:
		return "admin, read-only"
	case user.IsAdmin:
		return "admin"
	case user.IsReadOnly:
		return "read-only"
	}
	return "user"
}

func userStateLabel(user ports.AdminUser) string {
	if user.Enabled {
		return "enabled"
	}
	return "disabled"
}

// userCreatedText is "3 Mar 2026", or "" when the account has no creation time.
func userCreatedText(user ports.AdminUser) string {
	if user.CreatedAt.IsZero() {
		return ""
	}
	return user.CreatedAt.UTC().Format("2 Jan 2006")
}

// userSubtitle is the muted line under a user's name: role flags, state and
// creation date, from the fields the API returns.
func userSubtitle(user ports.AdminUser, withState bool) string {
	parts := []string{userRoleFlags(user)}
	if withState {
		parts = append(parts, userStateLabel(user))
	}
	if created := userCreatedText(user); created != "" {
		parts = append(parts, "created "+created)
	}
	return strings.Join(parts, ", ")
}

// remainingText words a positive time left as hours, days or years, never
// seconds: hours below two days, days below two years, else years.
func remainingText(left time.Duration) string {
	switch {
	case left < lifetimeHour:
		return "less than 1 hour"
	case left < 2*lifetimeDay:
		return pluralUnit(int64(left/lifetimeHour), "hour")
	case left < 2*lifetimeYear:
		return pluralUnit(int64(left/lifetimeDay), "day")
	}
	return pluralUnit(int64(left/lifetimeYear), "year")
}

func pluralUnit(count int64, unit string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %ss", count, unit)
}

// fitText truncates a styled string to width columns with an ellipsis.
func fitText(text string, width int) string {
	if width < 1 {
		return ""
	}
	return ansi.Truncate(text, width, "…")
}

// padText fits text into exactly width columns, padding with spaces.
func padText(text string, width int) string {
	text = fitText(text, width)
	if gap := width - lipgloss.Width(text); gap > 0 {
		text += strings.Repeat(" ", gap)
	}
	return text
}

// spreadText puts left and right at the two ends of a width-column line,
// shortening left when the two do not fit together.
func spreadText(left, right string, width int) string {
	right = fitText(right, width)
	left = fitText(left, max(width-lipgloss.Width(right)-1, 1))
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

// userPanel draws a bordered panel exactly width columns by height rows.
// Lines are fitted to the inner width first, so nothing wraps.
func userPanel(theme adminTheme, width, height int, lines []string) string {
	inner := max(width-4, 1)
	fitted := make([]string, 0, len(lines))
	for _, line := range lines {
		fitted = append(fitted, fitText(line, inner))
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.borderColor).
		Padding(0, 1).
		Width(width).
		Height(height).
		MaxHeight(height).
		Render(strings.Join(fitted, "\n"))
}

// userSectionTitle is the small caps heading of a panel section.
func userSectionTitle(theme adminTheme, text string) string {
	return theme.muted.Bold(true).Render(text)
}

// printableText drops control characters from typed or pasted text, so a
// pasted newline never ends up in a single-line field.
func printableText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
}

// userField draws one labelled single-line input: the value (masked when
// secret) with a trailing cursor when focused, in a box width columns wide.
func userField(theme adminTheme, label, value string, secret, focused bool, width int) []string {
	shown := value
	if secret {
		shown = strings.Repeat("•", utf8.RuneCountInString(value))
	}
	style := theme.input
	cursor := ""
	if focused {
		style = theme.inputFocus
		cursor = "▏"
	}
	inner := max(width-2, 1) // the style's horizontal padding
	box := style.Width(width).Render(fitText(tailText(shown+cursor, inner), inner))
	return []string{theme.muted.Render(label), box}
}

// tailText keeps the END of text when it is wider than width, so the cursor
// stays visible while typing a long value.
func tailText(text string, width int) string {
	runes := []rune(text)
	for lipgloss.Width(string(runes)) > width && len(runes) > 1 {
		runes = runes[1:]
	}
	return string(runes)
}

// userNoticeTone colors a screen's one-line notice.
type userNoticeTone int

const (
	userNoticeNone userNoticeTone = iota
	userNoticeInfo
	userNoticeSuccess
	userNoticeError
)

func renderUserNotice(theme adminTheme, tone userNoticeTone, text string, width int) string {
	style := theme.muted
	switch tone {
	case userNoticeSuccess:
		style = theme.success
	case userNoticeError:
		style = theme.error
	case userNoticeInfo:
		style = theme.warning
	}
	return style.Render(fitText(text, width))
}
