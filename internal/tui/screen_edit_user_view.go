package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"regixtry/internal/ports"
)

const (
	editUserColumnGap = 2
	// editUserBarWidth is the width of a token's remaining-lifetime bar.
	editUserBarWidth = 10
	// editUserTallHeight is the column height from which the Password panel
	// keeps its rule line.
	editUserTallHeight = 18
)

func (s editUserScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH := shellContentSize(width, height)

	title := adminFirstNonEmpty(s.username, s.userID)
	subtitle := ""
	if s.found {
		subtitle = userSubtitle(s.user, false)
	}
	lines := []string{theme.subheading.Render(fitText(title, bodyW)), theme.muted.Render(fitText(subtitle, bodyW))}
	if s.notice != "" {
		lines = append(lines, renderUserNotice(theme, s.noticeTone, s.notice, bodyW))
	}
	lines = append(lines, "")

	columnsHeight := max(bodyH-len(lines), 3)
	leftW := (bodyW - editUserColumnGap) / 2
	rightW := bodyW - editUserColumnGap - leftW

	var columns string
	if s.loaded && !s.found {
		columns = userPanel(theme, bodyW, columnsHeight, s.missingLines(theme))
	} else {
		columns = lipgloss.JoinHorizontal(lipgloss.Top,
			s.renderLeft(theme, leftW, columnsHeight),
			strings.Repeat(" ", editUserColumnGap),
			s.renderRight(theme, env, rightW, columnsHeight),
		)
	}

	frame := screenFrame{
		Context: title,
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: s.breadcrumb(),
			Operator:   env.Session.Username,
			Active:     shellNavUsers,
			Body:       strings.Join(lines, "\n") + "\n" + columns,
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

// missingLines is what the panel says when the user could not be loaded.
func (s editUserScreen) missingLines(theme adminTheme) []string {
	if s.userErr != "" {
		return []string{theme.error.Render("Could not load the user: " + s.userErr), theme.muted.Render("Esc goes back to the Users list.")}
	}
	return []string{theme.error.Render("This user was not found."), theme.muted.Render("Esc goes back to the Users list.")}
}

// renderLeft stacks Profile, Password and Repository grants in height rows.
func (s editUserScreen) renderLeft(theme adminTheme, width, height int) string {
	inner := max(width-4, 1)

	profile := userPanel(theme, width, 6, s.profileLines(theme, inner))
	name := adminFirstNonEmpty(s.username, "this user")
	passwordLines := []string{
		userSectionTitle(theme, "PASSWORD"),
		spreadText("Set a new password for "+name, theme.muted.Render("p reset"), inner),
	}
	// The rule is told again on the reset form; it only gets a row here when
	// the terminal is tall enough to spare one.
	if height >= editUserTallHeight {
		passwordLines = append(passwordLines, theme.muted.Render("At least 8 characters. You type it twice."))
	}
	password := userPanel(theme, width, len(passwordLines)+2, passwordLines)
	grantsHeight := max(height-lipgloss.Height(profile)-lipgloss.Height(password), 3)
	grants := userPanel(theme, width, grantsHeight, s.grantLines(theme, inner, grantsHeight-2))
	return lipgloss.JoinVertical(lipgloss.Left, profile, password, grants)
}

func (s editUserScreen) profileLines(theme adminTheme, width int) []string {
	row := func(label, value string) string {
		return spreadText(theme.muted.Render(label), value, width)
	}
	if !s.loaded {
		return []string{userSectionTitle(theme, "PROFILE"), theme.muted.Render("Loading…")}
	}
	box, state := "[x]", theme.success.UnsetBold().Render("enabled")
	if !s.user.Enabled {
		box, state = "[ ]", theme.error.UnsetBold().Render("disabled")
	}
	return []string{
		userSectionTitle(theme, "PROFILE"),
		row("Username", s.user.Username),
		row("Role", theme.accent.UnsetBold().Render(userRoleFlags(s.user))),
		row("Enabled", theme.accent.UnsetBold().Render(box)+" "+state),
	}
}

func (s editUserScreen) grantLines(theme adminTheme, width, rows int) []string {
	lines := []string{spreadText(userSectionTitle(theme, "REPOSITORY GRANTS"), theme.muted.Render("g grant access"), width)}
	switch {
	case !s.loaded:
		return append(lines, theme.muted.Render("Loading…"))
	case s.grantsErr != "":
		return append(lines, theme.error.Render(fitText("Could not load grants: "+s.grantsErr, width)))
	case len(s.grants) == 0:
		return append(lines, theme.muted.Render(grantsSummary(0)))
	}
	room := max(rows-len(lines)-1, 0) // the summary line below the list
	shown := s.grants
	if len(shown) > room {
		shown = shown[:max(room-1, 0)]
	}
	for _, grant := range shown {
		role := theme.muted.Render(grantRoleLabel(grant.Role))
		lines = append(lines, spreadText(grant.Repository.String(), role, width))
	}
	summary := grantsSummary(len(s.grants))
	if len(shown) < len(s.grants) {
		summary = fmt.Sprintf("+%d more. %s", len(s.grants)-len(shown), summary)
	}
	return append(lines, theme.muted.Render(summary))
}

// renderRight is the token summary above the danger area.
func (s editUserScreen) renderRight(theme adminTheme, env screenEnv, width, height int) string {
	inner := max(width-4, 1)
	danger := userPanelDanger(theme, width, s.dangerLines(theme, inner))
	tokensHeight := max(height-lipgloss.Height(danger), 3)
	tokens := userPanel(theme, width, tokensHeight, s.tokenLines(theme, env, inner, tokensHeight-2))
	return lipgloss.JoinVertical(lipgloss.Left, tokens, danger)
}

func (s editUserScreen) tokenLines(theme adminTheme, env screenEnv, width, rows int) []string {
	lines := []string{spreadText(userSectionTitle(theme, "TOKENS"), theme.muted.Render("t manage"), width)}
	switch {
	case !s.loaded:
		return append(lines, theme.muted.Render("Loading…"))
	case s.tokensErr != "":
		return append(lines, theme.error.Render(fitText("Could not load tokens: "+s.tokensErr, width)))
	case len(s.tokens) == 0:
		return append(lines, theme.muted.Render("No tokens."))
	}
	room := max(rows-len(lines), 0)
	shown := s.tokens
	if len(shown) > room {
		shown = shown[:max(room-1, 0)]
	}
	for _, token := range shown {
		lines = append(lines, s.tokenRow(theme, env, token, width))
	}
	if len(shown) < len(s.tokens) {
		lines = append(lines, theme.muted.Render(fmt.Sprintf("+%d more", len(s.tokens)-len(shown))))
	}
	return lines
}

// tokenRow is one token: its name, a bar of the lifetime left and the time
// left in hours, days or years.
func (s editUserScreen) tokenRow(theme adminTheme, env screenEnv, token ports.AdminToken, width int) string {
	text, tone, warn := tokenLeftText(token, env)
	style := theme.success
	switch {
	case tone == attentionCritical:
		style = theme.error
	case warn:
		style = theme.warning
	}
	name := s.tokenRowName(token)
	if token.RevokedAt != nil || !token.ExpiresAt.After(env.now()) {
		return spreadText(theme.muted.Render(name), style.UnsetBold().Render(text), width)
	}
	bar := tokenBar(theme, style, token, env)
	right := bar + " " + style.UnsetBold().Render(text)
	return spreadText(name, right, width)
}

// tokenBar draws the share of a token's lifetime that is left.
func tokenBar(theme adminTheme, style lipgloss.Style, token ports.AdminToken, env screenEnv) string {
	total := token.ExpiresAt.Sub(token.CreatedAt)
	left := token.ExpiresAt.Sub(env.now())
	filled := 0
	if total > 0 {
		filled = int(int64(editUserBarWidth) * int64(left) / int64(total))
	}
	filled = min(max(filled, 1), editUserBarWidth)
	return style.UnsetBold().Render(strings.Repeat("█", filled)) + theme.muted.Render(strings.Repeat("░", editUserBarWidth-filled))
}

func (s editUserScreen) dangerLines(theme adminTheme, width int) []string {
	name := adminFirstNonEmpty(s.username, "this user")
	verb, text := "Disable user", fmt.Sprintf("Disable %s. They are signed out and their tokens stop working. Their grants stay, so enabling them again restores access.", name)
	if s.found && !s.user.Enabled {
		verb, text = "Enable user", fmt.Sprintf("%s is disabled and cannot sign in. Enabling restores their access; their grants were kept.", name)
	}
	lines := []string{theme.error.Render("DANGER AREA")}
	for _, line := range wrapText(text, width) {
		lines = append(lines, theme.text.Render(line))
	}
	return append(lines, theme.error.UnsetBold().Render(verb+"  d"))
}

// userPanelDanger is userPanel with the danger border, sized to its content.
func userPanelDanger(theme adminTheme, width int, lines []string) string {
	inner := max(width-4, 1)
	fitted := make([]string, 0, len(lines))
	for _, line := range lines {
		fitted = append(fitted, fitText(line, inner))
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.error.GetForeground()).
		Padding(0, 1).
		Width(width).
		Render(strings.Join(fitted, "\n"))
}

// wrapText breaks text at spaces into lines of at most width columns.
func wrapText(text string, width int) []string {
	var lines []string
	current := ""
	for _, word := range strings.Fields(text) {
		switch {
		case current == "":
			current = word
		case lipgloss.Width(current)+1+lipgloss.Width(word) <= width:
			current += " " + word
		default:
			lines = append(lines, current)
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
