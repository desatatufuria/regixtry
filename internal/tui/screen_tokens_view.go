package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"regixtry/internal/ports"
)

const (
	tokensPanelWidth = 48
	tokensPanelGap   = 2
	// tokensCardRows is a card's three lines; cards are one blank line apart.
	tokensCardRows = 3

	tokensLeakWarning = "A leaked token stays valid until it expires or you revoke it. Shorter for people, longer only for CI robots."
	tokensSecretOnce  = "The secret is shown once, right after creation. Copy it before leaving."
)

func (s tokensScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH := shellContentSize(width, height)
	now := env.now()

	leftW, panelW := tokensWidths(bodyW)

	left := strings.Join(s.renderList(theme, leftW, bodyH, now), "\n")
	panel := s.renderPanel(theme, env, panelW, bodyH)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", tokensPanelGap), panel)

	crumbs, active := s.breadcrumb()
	frame := screenFrame{
		Context: strings.Join(shellCrumb(crumbs...), " › "),
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: shellCrumb(crumbs...),
			Operator:   env.Session.Username,
			Active:     active,
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

// tokensWidths splits the body into the token list and the form panel.
func tokensWidths(bodyW int) (leftW, panelW int) {
	panelW = tokensPanelWidth
	if bodyW < 2*tokensPanelWidth {
		panelW = max(bodyW*2/5, 12)
	}
	return max(bodyW-panelW-tokensPanelGap, 12), panelW
}

// tokensHeadRows is the title, the explanation and a blank above the cards.
const tokensHeadRows = 3

// tokensVisible is how many cards fit in a list column rows tall.
func tokensVisible(rows int) int {
	// One row each for the status line and the "k of n" footer.
	cardRows := max(rows-tokensHeadRows-2, tokensCardRows)
	return max((cardRows+1)/(tokensCardRows+1), 1)
}

// breadcrumb is "Access > Users > alice > Tokens" or "Access > Robots > ci >
// Tokens", with the sidebar item it belongs to.
func (s tokensScreen) breadcrumb() ([]string, shellNavID) {
	if s.robot {
		return []string{"Access", "Robots", s.principalName, "Tokens"}, shellNavRobots
	}
	return []string{"Access", "Users", s.principalName, "Tokens"}, shellNavUsers
}

// ---- left: the tokens ----

func (s tokensScreen) renderList(theme adminTheme, width, rows int, now time.Time) []string {
	lines := []string{
		theme.subheading.Render(grantTruncate("Tokens for "+s.principalName, width)),
		theme.muted.Render(grantTruncate("Each bar is the time left out of the token's full lifetime.", width)),
		"",
	}
	visible := tokensVisible(rows)
	cursor := boundedIndex(s.selected, len(s.tokens))
	start, end := grantWindow(cursor, len(s.tokens), visible)

	switch {
	case !s.loaded:
		lines = append(lines, theme.muted.Render("Loading tokens..."))
	case s.err != "":
		lines = append(lines, theme.error.Render(grantTruncate("Could not load tokens: "+s.err, width)))
	case len(s.tokens) == 0:
		lines = append(lines, theme.muted.Render("No tokens yet. Press n to issue one."))
	default:
		for i := start; i < end; i++ {
			if i > start {
				lines = append(lines, "")
			}
			lines = append(lines, s.renderCard(theme, s.tokens[i], i == cursor, width, now)...)
		}
	}
	lines = append(lines, s.statusLine(theme, width))
	if len(s.tokens) > visible {
		lines = append(lines, theme.muted.Render(fmt.Sprintf("%d of %d", cursor+1, len(s.tokens))))
	}
	return lines
}

func (s tokensScreen) statusLine(theme adminTheme, width int) string {
	switch {
	case s.confirm.inFlight && s.confirm.submitting != "":
		return theme.muted.Render(grantTruncate(s.confirm.submitting, width))
	case s.notice != "" && s.noticeFail:
		return theme.error.Render(grantTruncate(s.notice, width))
	case s.notice != "":
		return theme.success.Render(grantTruncate(s.notice, width))
	}
	return ""
}

// renderCard is one token: its name and the time it has left, the bar, then
// the short accessor and the dates.
func (s tokensScreen) renderCard(theme adminTheme, token ports.AdminToken, selected bool, width int, now time.Time) []string {
	left := token.ExpiresAt.Sub(now)
	state := theme.muted
	stateText := timeLeftText(left) + " left"
	dates := fmt.Sprintf("issued %s, expires %s", dayMonth(token.CreatedAt, token.CreatedAt.Year() != token.ExpiresAt.Year()), dayMonth(token.ExpiresAt, true))
	nameStyle := theme.text
	switch {
	case token.RevokedAt != nil:
		left = 0
		stateText = "revoked"
		dates = "revoked " + dayMonth(*token.RevokedAt, true)
		nameStyle = theme.muted
	case left <= 0:
		stateText = "expired"
		state = theme.error
		dates = "expired " + dayMonth(token.ExpiresAt, true)
		nameStyle = theme.muted
	default:
		state = expiryToneOf(left).style(theme)
	}

	marker := "  "
	if selected {
		marker = "▸ "
		nameStyle = theme.accent
	}
	stateRendered := state.Render(stateText)
	name := nameStyle.Render(marker + grantTruncate(tokenDisplayLabel(token), max(width-lipgloss.Width(stateText)-lipgloss.Width(marker)-2, 1)))
	accessor := theme.muted.Render(shortAccessor(token.Accessor))
	datesRendered := theme.muted.Render(grantTruncate(dates, max(width-lipgloss.Width(accessor)-2, 1)))
	return []string{
		spread(name, stateRendered, width),
		timeLeftBar(theme, left, token.ExpiresAt.Sub(token.CreatedAt), width),
		spread(accessor, datesRendered, width),
	}
}

// tokenDisplayLabel is the name a token shows under, or "(unnamed)".
func tokenDisplayLabel(token ports.AdminToken) string {
	if token.Name != "" {
		return token.Name
	}
	return "(unnamed)"
}

// ---- right: Issue a new token ----

func (s tokensScreen) renderPanel(theme adminTheme, env screenEnv, width, rows int) string {
	inner := max(width-4, 1)
	var blocks [][]string
	border := lipgloss.Color(paletteBorderHex)
	switch {
	case s.done:
		blocks = s.doneBlocks(theme, inner)
		border = lipgloss.Color(paletteWarningHex)
	default:
		blocks = s.formBlocks(theme, env, inner)
		if s.focus != tokensFocusList {
			border = lipgloss.Color(paletteAccentHex)
		}
	}
	content := fitSections(blocks, max(rows-2, 1))
	for len(content) < rows-2 {
		content = append(content, "")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(width).
		Render(strings.Join(content, "\n"))
}

func (s tokensScreen) formBlocks(theme adminTheme, env screenEnv, inner int) [][]string {
	label := func(text string) string { return theme.muted.Render(text) }

	head := []string{
		theme.subheading.Render("Issue a new token"),
		padRight(label("Name"), 6) + renderEntryField(theme, s.name, "optional", s.focus == tokensFocusName, max(inner-6, 8)),
	}

	focused := s.focus == tokensFocusLifetime
	ceiling := lifetimeCeiling()
	lifetime := []string{label("Lifetime")}
	lifetime = append(lifetime, packMarks(lifetimeMarks(theme, s.lifetime, focused), inner)...)
	if s.lifetime.isCustom(ceiling) {
		lifetime = append(lifetime, renderCustomInput(theme, s.lifetime.Custom, focused))
	}

	expires := []string{theme.muted.Bold(true).Render("EXPIRES")}
	if duration, ok := s.lifetime.duration(ceiling); ok {
		expires = append(expires,
			theme.text.Bold(true).Render(exactExpiry(env.now().Add(duration))),
			theme.muted.Render(fromToday(duration)))
	} else {
		for _, line := range grantWrapN(lifetimeInvalidText(), inner, 3) {
			expires = append(expires, theme.error.Render(line))
		}
	}

	var notes []string
	for _, line := range grantWrap(tokensLeakWarning, inner) {
		notes = append(notes, theme.muted.Render(line))
	}
	for _, line := range grantWrap(tokensSecretOnce, inner) {
		notes = append(notes, theme.muted.Render(line))
	}

	create := theme.pill.Render("Create token  enter")
	if s.creating {
		create = theme.muted.Render("Creating token...")
	}
	buttons := []string{create + "  " + theme.muted.Render("Cancel  esc")}
	if s.formErr != "" {
		var errLines []string
		for _, line := range grantWrapN(s.formErr, inner, 2) {
			errLines = append(errLines, theme.error.Render(line))
		}
		buttons = append(errLines, buttons...)
	}
	return [][]string{head, lifetime, expires, notes, buttons}
}

// packMarks lays the selector's options out in rows no wider than width.
func packMarks(marks []string, width int) []string {
	var rows []string
	current := ""
	for _, mark := range marks {
		switch {
		case current == "":
			current = mark
		case lipgloss.Width(current)+2+lipgloss.Width(mark) <= width:
			current += "  " + mark
		default:
			rows = append(rows, current)
			current = mark
		}
	}
	if current != "" {
		rows = append(rows, current)
	}
	return rows
}

// grantWrapN wraps text to width and keeps at most n lines.
func grantWrapN(text string, width, n int) []string {
	lines := grantWrap(text, width)
	if len(lines) > n {
		lines = lines[:n]
	}
	return lines
}

// doneBlocks is the panel after creation: the one-time secret, whole.
func (s tokensScreen) doneBlocks(theme adminTheme, inner int) [][]string {
	created := s.created
	name := created.name
	if name == "" {
		name = "(unnamed)"
	}
	head := []string{
		theme.success.Render("✓") + " Token " + theme.subheading.Render(grantTruncate(name, max(inner-16, 4))) + " created",
	}
	secret := []string{theme.muted.Render("Accessor"), theme.text.Bold(true).Render(grantTruncate(created.accessor, inner)), theme.muted.Render("Secret")}
	for _, part := range hardWrap(created.secret, inner) {
		secret = append(secret, theme.text.Bold(true).Render(part))
	}
	for _, line := range grantWrap(oneTimeSecretWarning, inner) {
		secret = append(secret, theme.warning.Render(line))
	}
	var details []string
	if !created.expires.IsZero() {
		details = append(details, theme.muted.Render("Expires"), theme.text.Render(exactExpiry(created.expires)))
	}
	return [][]string{head, secret, {theme.pill.Render("Done  enter")}, details}
}
