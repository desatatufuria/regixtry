package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	overviewSubtitle = "The three things you do most are one key away from anywhere."
	overviewCardGap  = 2
)

// overviewCard is one quick-action card of the board.
type overviewCard struct {
	title string
	key   string
	desc  string
}

var overviewCards = []overviewCard{
	{"Grant repository access", "g", "Pick a user or robot, pick repositories, pick a role. One screen."},
	{"Create robot", "r", "A CI identity with its first repository grant and token."},
	{"Issue token", "t", "Choose a lifetime and see the exact expiry date before creating."},
}

func (s overviewScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH := shellContentSize(width, height)

	cards := s.renderCards(theme, bodyW)
	lines := []string{theme.subheading.Render("Overview"), theme.muted.Render(overviewSubtitle), "", cards, ""}
	used := 4 + lipgloss.Height(cards)
	lines = append(lines, s.renderAttention(theme, bodyW, bodyH-used))

	return screenFrame{
		Context: "Overview",
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: shellCrumb("Overview"),
			Operator:   env.Session.Username,
			Active:     s.selectedID(),
			Body:       strings.Join(lines, "\n"),
			Hints:      overviewKeys.short,
			Width:      width,
			Height:     height,
		}),
	}
}

// renderCards lays the three quick-action cards side by side across width.
func (s overviewScreen) renderCards(theme adminTheme, width int) string {
	count := len(overviewCards)
	available := width - overviewCardGap*(count-1)
	rendered := make([]string, 0, 2*count)
	tallest := 0
	widths := make([]int, count)
	for i := range overviewCards {
		widths[i] = available / count
		if i < available%count {
			widths[i]++
		}
	}
	for i, card := range overviewCards {
		out := renderOverviewCard(theme, card, widths[i], 0)
		tallest = max(tallest, lipgloss.Height(out))
	}
	for i, card := range overviewCards {
		if i > 0 {
			rendered = append(rendered, strings.Repeat(" ", overviewCardGap))
		}
		rendered = append(rendered, renderOverviewCard(theme, card, widths[i], tallest))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

func renderOverviewCard(theme adminTheme, card overviewCard, width, height int) string {
	inner := max(width-4, 1) // border and padding on both sides
	keycap := lipgloss.NewStyle().
		Foreground(theme.muted.GetForeground()).
		Background(lipgloss.Color(paletteBorderStrongHex)).
		Padding(0, 1).
		Render(card.key)
	title := theme.subheading.MaxWidth(max(inner-lipgloss.Width(keycap)-1, 1)).Render(card.title)
	gap := max(inner-lipgloss.Width(title)-lipgloss.Width(keycap), 1)
	content := title + strings.Repeat(" ", gap) + keycap + "\n" + theme.muted.Width(inner).Render(card.desc)

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.borderColor).
		Padding(0, 1).
		Width(width)
	if height > 0 {
		style = style.Height(height)
	}
	return style.Render(content)
}

// renderAttention renders the NEEDS ATTENTION panel in at most rows rows.
func (s overviewScreen) renderAttention(theme adminTheme, width, rows int) string {
	inner := max(width-4, 1)
	lines := []string{theme.muted.Bold(true).Render("NEEDS ATTENTION")}

	switch {
	case !s.loaded:
		lines = append(lines, theme.muted.Render("Loading..."))
	default:
		items := s.attentionItems()
		capacity := max(rows-2-1, 1) // border rows and the title
		extra := s.loadErrorLines(theme, inner)
		switch {
		case len(items) == 0 && len(extra) == 0:
			lines = append(lines, theme.muted.Render("Nothing needs attention."))
		default:
			room := max(capacity-len(extra), 0)
			shown := items
			if len(items) > room {
				shown = mostUrgentAttention(items, max(room-1, 0))
				extra = append([]string{theme.muted.Render(fmt.Sprintf("+%d more", len(items)-len(shown)))}, extra...)
			}
			for _, item := range shown {
				lines = append(lines, renderAttentionItem(theme, item, inner))
			}
			lines = append(lines, extra...)
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.borderColor).
		Padding(0, 1).
		Width(width).
		MaxHeight(max(rows, 3)).
		Render(strings.Join(lines, "\n"))
}

// loadErrorLines reports a failed read in the panel instead of silently
// showing an empty "all clear".
func (s overviewScreen) loadErrorLines(theme adminTheme, width int) []string {
	var lines []string
	if s.robotsErr != "" {
		lines = append(lines, theme.error.MaxWidth(width).Render("Could not check robots: "+s.robotsErr))
	}
	if s.imagesErr != "" {
		lines = append(lines, theme.error.MaxWidth(width).Render("Could not check image scans: "+s.imagesErr))
	}
	return lines
}

func renderAttentionItem(theme adminTheme, item attentionItem, width int) string {
	style := theme.warning
	if item.tone == attentionCritical {
		style = theme.error
	}
	if item.hint == "" {
		return style.MaxWidth(width).Render(item.text)
	}
	hint := theme.muted.Render(item.hint)
	text := style.MaxWidth(max(width-lipgloss.Width(hint)-2, 1)).Render(item.text)
	gap := max(width-lipgloss.Width(text)-lipgloss.Width(hint), 1)
	return text + strings.Repeat(" ", gap) + hint
}

// mostUrgentAttention keeps at most keep items when the panel is too short
// for all of them: critical ones first, then warnings, each kept in their
// original order.
func mostUrgentAttention(items []attentionItem, keep int) []attentionItem {
	chosen := make([]bool, len(items))
	for _, tone := range []attentionTone{attentionCritical, attentionWarning} {
		for i, item := range items {
			if item.tone == tone && keep > 0 {
				chosen[i] = true
				keep--
			}
		}
	}
	var kept []attentionItem
	for i, item := range items {
		if chosen[i] {
			kept = append(kept, item)
		}
	}
	return kept
}
