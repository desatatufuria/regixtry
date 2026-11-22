package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"regixtry/internal/ports"
)

// overviewAgoWidth is the column of the "3 h ago" times in RECENT CHANGES.
const overviewAgoWidth = 9

// renderRecentChanges renders the RECENT CHANGES panel: one row per access
// event, newest first, as many as rows allow. Its failure and empty states
// stay inside the panel; the other panel is never affected.
func (s overviewScreen) renderRecentChanges(theme adminTheme, width, rows int, now time.Time) string {
	inner := max(width-4, 1)
	lines := []string{theme.muted.Bold(true).Render("RECENT CHANGES")}
	switch {
	case !s.loaded:
		lines = append(lines, theme.muted.Render("Loading..."))
	case s.eventsErr != "":
		lines = append(lines, theme.error.Render(fitText("Could not load recent changes: "+cleanEventText(s.eventsErr), inner)))
	case len(s.events) == 0:
		lines = append(lines, theme.muted.Render("No changes recorded yet."))
	default:
		for _, event := range s.events[:min(len(s.events), max(rows-3, 1))] {
			lines = append(lines, renderOverviewEvent(theme, event, inner, now))
		}
	}
	return overviewPanel(theme, width, rows, lines)
}

// renderOverviewEvent is "3 h ago  admin  gave alice Writer on team/api".
func renderOverviewEvent(theme adminTheme, event ports.AdminAccessEvent, width int, now time.Time) string {
	ago := agoText(now.Sub(event.At))
	when := theme.muted.Render(ago + strings.Repeat(" ", max(overviewAgoWidth-lipgloss.Width(ago), 0)))
	actor := cleanEventText(event.Actor)
	if actor == "" {
		actor = "someone"
	}
	rest := max(width-overviewAgoWidth, 1)
	return when + fitText(theme.text.Render(actor)+" "+theme.muted.Render(eventSentence(event)), rest)
}

// agoText words how long ago something happened in hours, days or years,
// never seconds: "3 h ago", "2 d ago", "1 y ago".
func agoText(elapsed time.Duration) string {
	switch {
	case elapsed < lifetimeHour:
		return "<1 h ago"
	case elapsed < 2*lifetimeDay:
		return pluralShort(int64(elapsed/lifetimeHour), "h")
	case elapsed < 2*lifetimeYear:
		return pluralShort(int64(elapsed/lifetimeDay), "d")
	}
	return pluralShort(int64(elapsed/lifetimeYear), "y")
}

func pluralShort(count int64, unit string) string { return fmt.Sprintf("%d %s ago", count, unit) }

// eventSentence words an access event after the actor: "gave alice Writer on
// team/api". An action this screen does not know is shown as its own text, so
// a newer server never crashes or blanks the panel.
func eventSentence(event ports.AdminAccessEvent) string {
	subject := cleanEventText(event.Subject)
	repo := cleanEventText(event.Repository)
	role := capitalizeFirst(cleanEventText(event.Role))
	token := cleanEventText(event.TokenName)
	detail := cleanEventText(event.Detail)

	var words []string
	switch string(event.Action) {
	case "grant.put":
		words = []string{"gave", subject, role, "on", repo}
	case "grant.delete":
		words = []string{"revoked", subject, "on", repo}
	case "token.issued":
		words = []string{"issued token", token, "for", subject, detailIn(detail)}
	case "token.revoked":
		words = []string{"revoked token", token, "of", subject}
	case "robot.created":
		words = []string{"created robot", subject}
	case "robot.deleted":
		words = []string{"deleted robot", subject}
	case "user.created":
		words = []string{"created user", subject, detailIn(detail)}
	case "user.enabled":
		words = []string{"enabled user", subject}
	case "user.disabled":
		words = []string{"disabled user", subject}
	case "user.updated":
		words = []string{"updated user", subject, detailIn(detail)}
	case "user.password_reset":
		words = []string{"reset the password of", subject}
	case "user.deleted":
		words = []string{"deleted user", subject}
	default:
		words = []string{cleanEventText(string(event.Action)), subject, repo, detail}
	}
	return joinWords(words)
}

func detailIn(detail string) string {
	if detail == "" {
		return ""
	}
	return "(" + detail + ")"
}

func joinWords(words []string) string {
	kept := words[:0:0]
	for _, word := range words {
		if word != "" {
			kept = append(kept, word)
		}
	}
	return strings.Join(kept, " ")
}

func capitalizeFirst(text string) string {
	for i, r := range text {
		return string(unicode.ToUpper(r)) + text[i+len(string(r)):]
	}
	return text
}

// cleanEventText drops control characters from server text before it reaches
// the terminal.
func cleanEventText(text string) string { return strings.TrimSpace(sanitizeFieldText(text, true)) }
