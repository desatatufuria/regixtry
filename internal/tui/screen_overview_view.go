package tui

import (
	"fmt"
	"strings"
	"time"

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
	used := overviewHeadRows(lipgloss.Height(cards))
	lines = append(lines, s.renderPanels(theme, env, bodyW, bodyH-used))

	return screenFrame{
		Context: "Overview",
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: shellCrumb("Overview"),
			Operator:   env.Session.Username,
			Active:     s.selectedID(),
			Body:       strings.Join(lines, "\n"),
			Hints:      s.Keys().short,
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
	title := theme.subheading.Render(grantTruncate(card.title, max(inner-lipgloss.Width(keycap)-1, 1)))
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

// renderPanels lays NEEDS ATTENTION and RECENT CHANGES side by side, both rows
// tall.
func (s overviewScreen) renderPanels(theme adminTheme, env screenEnv, width, rows int) string {
	left := (width - overviewCardGap) / 2
	right := width - overviewCardGap - left
	return lipgloss.JoinHorizontal(lipgloss.Top,
		s.renderAttention(theme, left, rows, env.now()),
		strings.Repeat(" ", overviewCardGap),
		s.renderRecentChanges(theme, right, rows, env.now()),
	)
}

// overviewPanel frames a panel's lines in a border exactly rows tall.
func overviewPanel(theme adminTheme, width, rows int, lines []string) string {
	rows = max(rows, 3)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.borderColor).
		Padding(0, 1).
		Width(max(width, 3)).
		Height(rows).
		MaxHeight(rows).
		Render(strings.Join(lines, "\n"))
}

// renderAttention renders the NEEDS ATTENTION panel in at most rows rows. The
// row Tab selected is always shown.
func (s overviewScreen) renderAttention(theme adminTheme, width, rows int, now time.Time) string {
	inner := max(width-4, 1)
	lines := []string{theme.muted.Bold(true).Render("NEEDS ATTENTION")}

	if !s.loaded {
		return overviewPanel(theme, width, rows, append(lines, theme.muted.Render("Loading...")))
	}
	items := s.attentionItems(now)
	capacity := max(rows-2-1, 1) // border rows and the title
	notes := s.loadErrorLines(theme, inner)
	if len(items) == 0 && len(notes) == 0 {
		return overviewPanel(theme, width, rows, append(lines, theme.muted.Render("Nothing needs attention.")))
	}
	room := max(capacity-len(notes), 0)
	selected := s.attentionFocus - 1
	shown := attentionShown(items, room, selected)
	var more []string
	if hidden := len(items) - len(shown); hidden > 0 && len(shown) < room {
		more = []string{theme.muted.Render(fmt.Sprintf("+%d more", hidden))}
	}
	for _, i := range shown {
		lines = append(lines, renderAttentionItem(theme, items[i], inner, i == selected))
	}
	lines = append(lines, more...)
	lines = append(lines, notes...)
	return overviewPanel(theme, width, rows, lines)
}

// attentionShown is the items a panel with room rows draws, by index, in order:
// all of them, or the most urgent when they do not fit, always with the row
// Tab selected.
func attentionShown(items []attentionItem, room, selected int) []int {
	if len(items) <= room {
		shown := make([]int, len(items))
		for i := range items {
			shown[i] = i
		}
		return shown
	}
	keep := max(room-1, 0)
	if selected >= 0 {
		keep = max(keep, 1)
	}
	return urgentAttentionRows(items, keep, selected)
}

// attentionPanelChromeRows is the panel's top border and its title line above
// the first attention row.
const attentionPanelChromeRows = 2

// attentionLayout is the NEEDS ATTENTION panel's geometry for a terminal
// size: its width and the room the rows have, from the same arithmetic View
// uses. headRows is how many body lines come before the panels.
func (s overviewScreen) attentionLayout(theme adminTheme, env screenEnv) (width, headRows, room int) {
	bodyW, bodyH := shellContentSize(env.Layout.Width, env.Layout.Height)
	headRows = overviewHeadRows(lipgloss.Height(s.renderCards(theme, bodyW)))
	width = (bodyW - overviewCardGap) / 2
	capacity := max(bodyH-headRows-2-1, 1)
	room = max(capacity-len(s.loadErrorLines(theme, max(width-4, 1))), 0)
	return width, headRows, room
}

// overviewHeadRows is the body lines above the panels: the title, the
// subtitle, a blank, the cards and a blank.
func overviewHeadRows(cardsHeight int) int { return 4 + cardsHeight }

// loadErrorLines reports a failed read in the panel instead of silently
// showing an empty "all clear".
func (s overviewScreen) loadErrorLines(theme adminTheme, width int) []string {
	var lines []string
	for _, note := range []struct{ what, err string }{
		{"robots", s.robotsErr},
		{"image scans", s.imagesErr},
		{"expiring tokens", s.expiringErr},
	} {
		if note.err != "" {
			lines = append(lines, theme.error.Render(fitText("Could not check "+note.what+": "+cleanEventText(note.err), width)))
		}
	}
	return lines
}

// attentionMarkerWidth is the room the selection marker takes in every row.
const attentionMarkerWidth = 2

func renderAttentionItem(theme adminTheme, item attentionItem, width int, selected bool) string {
	style := theme.warning
	switch item.tone {
	case attentionCritical:
		style = theme.error
	case attentionMuted:
		style = theme.muted
	}
	marker, hintText := "  ", item.hint
	if selected {
		marker, hintText = theme.text.Bold(true).Render("▸ "), "enter open"
		style = style.Bold(true).Underline(true)
	}
	width = max(width-attentionMarkerWidth, 1)
	if hintText == "" {
		return marker + style.Render(attentionFit(item, width))
	}
	hint := theme.muted.Render(hintText)
	text := style.Render(attentionFit(item, max(width-lipgloss.Width(hint)-2, 1)))
	gap := max(width-lipgloss.Width(text)-lipgloss.Width(hint), 1)
	return marker + text + strings.Repeat(" ", gap) + hint
}

// attentionFit is a row's text in width columns, ending with an ellipsis when
// it cannot be shortened any other way, never clipped silently.
func attentionFit(item attentionItem, width int) string {
	if item.fit != nil {
		return fitText(item.fit(width), width)
	}
	return grantTruncate(item.text, width)
}

// mostUrgentAttention keeps at most keep items when the panel is too short
// for all of them: critical ones first, then warnings, each kept in their
// original order.
func mostUrgentAttention(items []attentionItem, keep int) []attentionItem {
	var kept []attentionItem
	for _, i := range urgentAttentionRows(items, keep, -1) {
		kept = append(kept, items[i])
	}
	return kept
}

// urgentAttentionRows is mostUrgentAttention by index. The row at index must
// (when not negative) is always kept, at the expense of the least urgent one.
func urgentAttentionRows(items []attentionItem, keep, must int) []int {
	chosen := make([]bool, len(items))
	var order []int
	for _, tone := range []attentionTone{attentionCritical, attentionWarning, attentionMuted} {
		for i, item := range items {
			if item.tone == tone && keep > 0 {
				chosen[i] = true
				order = append(order, i)
				keep--
			}
		}
	}
	if must >= 0 && must < len(items) && !chosen[must] {
		if len(order) > 0 {
			chosen[order[len(order)-1]] = false
		}
		chosen[must] = true
	}
	var kept []int
	for i := range items {
		if chosen[i] {
			kept = append(kept, i)
		}
	}
	return kept
}
