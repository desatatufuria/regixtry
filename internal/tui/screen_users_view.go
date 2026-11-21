package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"regixtry/internal/ports"
)

const (
	// usersDetailWidth is the board's 340px detail panel at about 8.5px a column.
	usersDetailWidth = 38
	usersColumnGap   = 2
	usersRoleWidth   = 9
	usersStateWidth  = 8
	usersExpireWidth = 8
)

// usersTableTitle is the header line of the screen: the title, and on the
// right how many users there are.
func (s usersScreen) summaryText() string {
	if !s.loaded {
		return ""
	}
	disabled := 0
	for _, user := range s.users {
		if !user.Enabled {
			disabled++
		}
	}
	visible := len(s.visible())
	count := pluralUnit(int64(len(s.users)), "user")
	if strings.TrimSpace(s.query) != "" {
		count = fmt.Sprintf("%d of %s", visible, count)
	}
	if disabled > 0 {
		count += fmt.Sprintf(", %d disabled", disabled)
	}
	return count
}

func (s usersScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH := shellContentSize(width, height)

	lines := []string{spreadText(theme.subheading.Render("Users"), theme.muted.Render(s.summaryText()), bodyW), ""}
	columnsHeight := max(bodyH-len(lines), 3)
	detailW := min(usersDetailWidth, max(bodyW/3, 20))
	tableW := max(bodyW-detailW-usersColumnGap, 10)

	left := s.renderLeft(theme, env, tableW, columnsHeight)
	right := userPanel(theme, detailW, columnsHeight, s.detailLines(theme, env, detailW-4, columnsHeight-2))
	body := strings.Join(lines, "\n") + "\n" +
		lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", usersColumnGap), right)

	frame := screenFrame{
		Context: "Users",
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: shellCrumb("Access", "Users"),
			Operator:   env.Session.Username,
			Active:     shellNavUsers,
			Body:       body,
			Hints:      s.Keys().short,
			Width:      width,
			Height:     height,
		}),
	}
	if s.confirm.Active() {
		frame.Overlay = s.confirm.view(theme)
	}
	return frame
}

// renderLeft is the search box, the notice line and the user table, exactly
// width columns by height rows.
func (s usersScreen) renderLeft(theme adminTheme, env screenEnv, width, height int) string {
	search := s.renderSearch(theme, width)
	rows := height - lipgloss.Height(search)
	parts := []string{search}

	if s.notice != "" {
		parts = append(parts, renderUserNotice(theme, s.noticeTone, s.notice, width))
		rows--
	}
	footnote := s.footnote(theme, width)
	rows -= len(footnote)
	parts = append(parts, s.renderTable(theme, width, max(rows, 3)))
	parts = append(parts, footnote...)
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(strings.Join(parts, "\n"))
}

func (s usersScreen) renderSearch(theme adminTheme, width int) string {
	inner := max(width-4, 1)
	text := s.query
	switch {
	case s.searching:
		text += "▏"
	case text == "":
		text = theme.muted.Render("username")
	}
	label := theme.muted.Render("/ search  ")
	border := theme.borderColor
	if s.searching {
		border = theme.accent.GetForeground()
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(width).
		Render(fitText(label+tailText(text, max(inner-lipgloss.Width(label), 1)), inner))
}

// footnote explains the EXPIRING column, and says so when the report is
// missing or may be incomplete.
func (s usersScreen) footnote(theme adminTheme, width int) []string {
	switch {
	case s.expiringErr != "":
		return []string{theme.error.Render(fitText("Could not load expiring tokens: "+s.expiringErr, width))}
	case s.expiringCapped:
		return []string{theme.warning.Render(fitText(fmt.Sprintf("EXPIRING counts only the %d soonest tokens; more may exist.", usersExpiringLimit), width))}
	}
	return []string{theme.muted.Render(fitText("EXPIRING counts tokens that expire within 7 days.", width))}
}

// renderTable draws the user table in a panel of exactly height rows.
func (s usersScreen) renderTable(theme adminTheme, width, height int) string {
	inner := max(width-4, 1)
	nameW := max(inner-2-usersRoleWidth-usersStateWidth-usersExpireWidth-3*usersColumnGap, 4)
	gap := strings.Repeat(" ", usersColumnGap)
	row := func(name, role, state, expiring string) string {
		return padText(name, nameW) + gap + padText(role, usersRoleWidth) + gap + padText(state, usersStateWidth) + gap + padText(expiring, usersExpireWidth)
	}

	lines := []string{theme.muted.Bold(true).Render("  " + row("USERNAME", "ROLE", "STATE", "EXPIRING"))}
	capacity := max(height-2-1, 1) // border rows and the column header

	switch {
	case !s.loaded:
		lines = append(lines, theme.muted.Render("Loading…"))
	case s.loadErr != "":
		lines = append(lines, theme.error.Render(fitText("Could not load users: "+s.loadErr, inner)))
	default:
		visible := s.visible()
		if len(visible) == 0 {
			message := "No users yet. Press n to create one."
			if strings.TrimSpace(s.query) != "" {
				message = "No users match the search."
			}
			lines = append(lines, theme.muted.Render(message))
			break
		}
		selected := s.selectedIndex(visible)
		top := min(max(selected-capacity/2, 0), max(len(visible)-capacity, 0))
		for i := top; i < min(top+capacity, len(visible)); i++ {
			lines = append(lines, s.renderRow(theme, visible[i], i == selected, row, inner))
		}
	}
	return userPanel(theme, width, height, lines)
}

// renderRow draws one user row; the selected row is one solid highlight.
func (s usersScreen) renderRow(theme adminTheme, user ports.AdminUser, selected bool, row func(name, role, state, expiring string) string, inner int) string {
	expiring := s.expiringCell(user)
	plain := row(user.Username, userRole(user), userStateLabel(user), expiring)
	if selected {
		return theme.selected.Padding(0).Width(inner).Render(fitText("▸ "+plain, inner))
	}
	if !user.Enabled {
		return theme.muted.Render("  " + plain)
	}
	// Color the cells that carry meaning, the way the board does.
	roleStyle, stateStyle, expiringStyle := theme.text, theme.success, theme.muted
	if user.IsAdmin {
		roleStyle = theme.accent
	}
	tokens, _ := s.expiringFor(user.ID)
	if len(tokens) > 0 {
		expiringStyle = theme.warning
	}
	nameW := lipgloss.Width(plain) - usersRoleWidth - usersStateWidth - usersExpireWidth - 3*usersColumnGap
	gap := strings.Repeat(" ", usersColumnGap)
	return "  " + theme.text.Render(padText(user.Username, nameW)) + gap +
		roleStyle.UnsetBold().Render(padText(userRole(user), usersRoleWidth)) + gap +
		stateStyle.UnsetBold().Render(padText(userStateLabel(user), usersStateWidth)) + gap +
		expiringStyle.UnsetBold().Render(padText(expiring, usersExpireWidth))
}

// expiringCell is the EXPIRING column: how many of the user's tokens expire
// within a week, "?" when the report failed.
func (s usersScreen) expiringCell(user ports.AdminUser) string {
	tokens, ok := s.expiringFor(user.ID)
	if !ok {
		return "?"
	}
	if s.expiringCapped && len(tokens) > 0 {
		return fmt.Sprintf("%d+", len(tokens))
	}
	return fmt.Sprintf("%d", len(tokens))
}

// detailLines is the detail panel: who is selected, the actions and what needs
// attention, in at most rows rows.
func (s usersScreen) detailLines(theme adminTheme, env screenEnv, width, rows int) []string {
	user, ok := s.current()
	if !ok {
		return []string{theme.muted.Render("No user selected.")}
	}
	toggle := theme.error.UnsetBold().Render("Disable user")
	if !user.Enabled {
		toggle = theme.success.UnsetBold().Render("Enable user")
	}
	action := func(label, hint string) string {
		return spreadText(label, theme.muted.Render(hint), width)
	}
	lines := []string{
		theme.subheading.Render(fitText(user.Username, width)),
		theme.muted.Render(fitText(userSubtitle(user, true), width)),
		"",
		userSectionTitle(theme, "ACTIONS"),
		action("Open profile", "enter"),
		action("Grant access", "g"),
		action("Tokens", "t"),
		action("Reset password", "p"),
		action(toggle, "d"),
		"",
		userSectionTitle(theme, "NEEDS ATTENTION"),
	}
	items := s.attentionItems(user, env.now())
	if len(items) == 0 {
		lines = append(lines, theme.muted.Render("Nothing needs attention."))
		return clipLines(lines, rows)
	}
	room := max(rows-len(lines), 1)
	shown := items
	if len(items) > room {
		shown = mostUrgentAttention(items, max(room-1, 0))
	}
	for _, item := range shown {
		lines = append(lines, userAttentionLines(theme, item, width)...)
	}
	if len(shown) < len(items) {
		lines = append(lines, theme.muted.Render(fmt.Sprintf("+%d more", len(items)-len(shown))))
	}
	return clipLines(lines, rows)
}

// userAttentionLines draws one "Needs attention" row of the narrow detail
// panel: the text wrapped to at most two lines, and the key hint at the end of
// the last line when it still fits.
func userAttentionLines(theme adminTheme, item attentionItem, width int) []string {
	style := theme.warning
	if item.tone == attentionCritical {
		style = theme.error
	}
	wrapped := wrapText(item.text, width)
	if len(wrapped) > 2 {
		wrapped = append(wrapped[:1], fitText(strings.Join(wrapped[1:], " "), width))
	}
	out := make([]string, 0, len(wrapped))
	for i, line := range wrapped {
		rendered := style.Render(line)
		if i == len(wrapped)-1 && item.hint != "" && lipgloss.Width(line)+2+len(item.hint) <= width {
			rendered = spreadText(rendered, theme.muted.Render(item.hint), width)
		}
		out = append(out, rendered)
	}
	return out
}

// clipLines keeps at most rows lines.
func clipLines(lines []string, rows int) []string {
	if rows >= 0 && len(lines) > rows {
		return lines[:rows]
	}
	return lines
}
