package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"regixtry/internal/ports"
)

const (
	robotsPanelWidth = 40
	robotsPanelGap   = 2
	// Table columns: the name takes what the others leave.
	robotsRepoWidth    = 16
	robotsRoleWidth    = 6
	robotsStateWidth   = 8
	robotsExpiresWidth = 10
	robotsColumnGap    = 2
	robotsMarkerWidth  = 2
)

func (s robotsScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH := shellContentSize(width, height)
	now := env.now()

	leftW, panelW := robotsWidths(bodyW)

	left := strings.Join(s.renderList(theme, leftW, bodyH, now), "\n")
	panel := s.renderPanel(theme, panelW, bodyH, now)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", robotsPanelGap), panel)

	frame := screenFrame{
		Context: adminContext("Access", "Robots"),
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: shellCrumb("Access", "Robots"),
			Operator:   env.Session.Username,
			Active:     shellNavRobots,
			Body:       body,
			Hints:      robotsKeys.short,
			Width:      width,
			Height:     height,
		}),
	}
	if s.confirm.Active() {
		frame.Overlay = s.confirm.view(theme)
	}
	return frame
}

// robotsWidths splits the body into the list column and the detail panel.
func robotsWidths(bodyW int) (leftW, panelW int) {
	panelW = robotsPanelWidth
	if bodyW < 2*robotsPanelWidth+robotsPanelGap {
		panelW = max(bodyW*2/5, 12)
	}
	return max(bodyW-panelW-robotsPanelGap, 12), panelW
}

// robotsTableChromeRows is the list's title row, then the table's top border,
// column header and rule above the first robot row.
const robotsTableChromeRows = 4

// listWindow is the robots drawn in a list column of the given width and rows:
// the cursor, the first and one past the last visible robot.
func (s robotsScreen) listWindow(theme adminTheme, width, rows int) (cursor, start, end, visible int) {
	status := s.statusLines(theme, width)
	visible = max(rows-1-4-len(status)-1, 1)
	cursor = boundedIndex(s.selected, len(s.robots))
	start, end = grantWindow(cursor, len(s.robots), visible)
	return cursor, start, end, visible
}

// summary is "5 robots, 1 without repository access".
func (s robotsScreen) summary() string {
	if !s.loaded || s.err != "" {
		return ""
	}
	without := 0
	for _, robot := range s.robots {
		if withoutAccess(robot) {
			without++
		}
	}
	text := pluralUnit(int64(len(s.robots)), "robot")
	if without > 0 {
		text += fmt.Sprintf(", %d without repository access", without)
	}
	return text
}

// renderList is the left column: title, table, then the status and footnote
// lines, in at most rows rows.
func (s robotsScreen) renderList(theme adminTheme, width, rows int, now time.Time) []string {
	lines := []string{spread(theme.subheading.Render("Robots"), theme.muted.Render(s.summary()), width)}
	inner := max(width-4, 1)

	status := s.statusLines(theme, width)
	cursor, start, end, visible := s.listWindow(theme, width, rows)

	box := []string{s.headerRow(theme, inner)}
	box = append(box, lipgloss.NewStyle().Foreground(theme.borderColor).Render(strings.Repeat("─", inner)))
	switch {
	case !s.loaded:
		box = append(box, theme.muted.Render("Loading robots..."))
	case s.err != "":
		box = append(box, theme.error.Render(grantTruncate("Could not load robots: "+s.err, inner)), theme.muted.Render("Press r to try again."))
	case len(s.robots) == 0:
		box = append(box, theme.muted.Render("No robots yet. Press n to create one."))
	}
	if s.loaded && s.err == "" {
		for i := start; i < end; i++ {
			box = append(box, s.renderRow(theme, s.robots[i], i == cursor, inner, now))
		}
	}
	lines = append(lines, strings.Split(lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.borderColor).
		Padding(0, 1).
		Width(width).
		Render(strings.Join(box, "\n")), "\n")...)

	lines = append(lines, status...)
	return append(lines, s.footnote(theme, width, cursor, len(s.robots), visible))
}

// statusLines is what sits under the table: the last action's outcome, or the
// selected robot's missing access.
func (s robotsScreen) statusLines(theme adminTheme, width int) []string {
	switch {
	case s.confirm.inFlight && s.confirm.submitting != "":
		return []string{theme.muted.Render(grantTruncate(s.confirm.submitting, width))}
	case s.notice != "" && s.noticeFail:
		return []string{theme.error.Render(grantTruncate(s.notice, width))}
	case s.notice != "":
		return []string{theme.success.Render(grantTruncate(s.notice, width))}
	}
	if robot, ok := s.selectedRobot(); ok && withoutAccess(robot) {
		return []string{
			theme.warning.Render(grantTruncate(robot.Username+" can authenticate but cannot pull or push.", width)),
			theme.warning.Render("Grant it a repository with g."),
		}
	}
	return []string{""}
}

func (s robotsScreen) footnote(theme adminTheme, width, cursor, total, visible int) string {
	text := "EXPIRES is the token that ends first."
	if s.tokensErr != "" {
		return theme.warning.Render(grantTruncate("Some token expiries could not be read: "+s.tokensErr, width))
	}
	if total > visible {
		text = fmt.Sprintf("%d of %d. %s", cursor+1, total, text)
	}
	return theme.muted.Render(grantTruncate(text, width))
}

// columns returns the widths of name, repository, role, state and expires for
// a row inner cells wide.
func robotColumns(inner int) [5]int {
	repo := robotsRepoWidth
	fixed := robotsMarkerWidth + 4*robotsColumnGap + repo + robotsRoleWidth + robotsStateWidth + robotsExpiresWidth
	name := inner - fixed
	if name < 10 {
		repo = max(repo-(10-name), 8)
		name = max(inner-(fixed-robotsRepoWidth+repo), 1)
	}
	return [5]int{name, repo, robotsRoleWidth, robotsStateWidth, robotsExpiresWidth}
}

func (s robotsScreen) headerRow(theme adminTheme, inner int) string {
	widths := robotColumns(inner)
	titles := [5]string{"NAME", "REPOSITORY", "ROLE", "STATE", "EXPIRES"}
	gap := strings.Repeat(" ", robotsColumnGap)
	parts := []string{strings.Repeat(" ", robotsMarkerWidth)}
	for i, title := range titles {
		parts = append(parts, padRight(grantTruncate(title, widths[i]), widths[i]))
	}
	return theme.muted.Bold(true).Render(parts[0] + strings.Join(parts[1:], gap))
}

type robotCell struct {
	text  string
	style lipgloss.Style
}

func (s robotsScreen) renderRow(theme adminTheme, robot ports.AdminRobot, selected bool, inner int, now time.Time) string {
	widths := robotColumns(inner)

	nameStyle, repoStyle, roleStyle := theme.text, theme.text, theme.text
	repo, role := robot.Repository, grantRoleLabel(robot.Role)
	switch {
	case withoutAccess(robot):
		nameStyle, repoStyle, roleStyle = theme.warning, theme.warning, theme.warning
		repo, role = "none", "none"
	case robot.Repository == "":
		nameStyle, repoStyle, roleStyle = theme.muted, theme.muted, theme.muted
		repo, role = "none", "none"
	case !robot.Enabled:
		nameStyle, repoStyle, roleStyle = theme.muted, theme.muted, theme.muted
	}
	state, stateStyle := "enabled", theme.success
	if !robot.Enabled {
		state, stateStyle = "disabled", theme.error
	}
	expires, expiresStyle := s.expiresCell(theme, robot, now)
	if selected {
		nameStyle = theme.accent
	}

	cells := [5]robotCell{
		{robot.Username, nameStyle},
		{repo, repoStyle},
		{role, roleStyle},
		{state, stateStyle},
		{expires, expiresStyle},
	}
	background := lipgloss.NewStyle()
	marker := "  "
	if selected {
		background = background.Background(lipgloss.Color(paletteSelectedBGHex))
		marker = "▸ "
	}
	var b strings.Builder
	b.WriteString(background.Inherit(theme.accent).Render(marker))
	for i, c := range cells {
		if i > 0 {
			b.WriteString(background.Render(strings.Repeat(" ", robotsColumnGap)))
		}
		b.WriteString(c.style.Background(background.GetBackground()).Render(padRight(grantTruncate(c.text, widths[i]), widths[i])))
	}
	return b.String()
}

// expiresCell is the EXPIRES column: the token that ends first.
func (s robotsScreen) expiresCell(theme adminTheme, robot ports.AdminRobot, now time.Time) (string, lipgloss.Style) {
	tokens, known := s.tokens[robot.ID]
	switch {
	case !known && !s.tokensLoaded:
		return "...", theme.muted
	case !known:
		return "-", theme.muted
	}
	expires, ok := firstExpiry(tokens, now)
	if !ok {
		return "no token", theme.muted
	}
	left := expires.Sub(now)
	return timeLeftText(left), expiryToneOf(left).style(theme)
}

// ---- detail panel ----

func (s robotsScreen) renderPanel(theme adminTheme, width, rows int, now time.Time) string {
	inner := max(width-4, 1)
	content := fitSections(s.panelBlocks(theme, inner, now), max(rows-2, 1))
	for len(content) < rows-2 {
		content = append(content, "")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.borderColor).
		Padding(0, 1).
		Width(width).
		Render(strings.Join(content, "\n"))
}

func (s robotsScreen) panelBlocks(theme adminTheme, inner int, now time.Time) [][]string {
	label := func(text string) string { return theme.muted.Bold(true).Render(text) }
	action := func(name, keyName string, style lipgloss.Style) string {
		return spread(style.Render(name), theme.muted.Render(keyName), inner)
	}
	robot, ok := s.selectedRobot()
	if !ok {
		return [][]string{{theme.muted.Render("No robot selected.")}, {label("ACTIONS"), action("New robot", "n", theme.text)}}
	}

	state := "disabled"
	if robot.Enabled {
		state = "enabled"
	}
	subtitle := "robot, " + state
	if !robot.CreatedAt.IsZero() {
		subtitle += ", created " + dayMonth(robot.CreatedAt, true)
	}
	head := []string{theme.subheading.Render(grantTruncate(robot.Username, inner)), theme.muted.Render(grantTruncate(subtitle, inner))}

	access := []string{label("ACCESS")}
	if robot.Repository == "" {
		access = append(access, theme.warning.Render("No repository access"))
	} else {
		chip := lipgloss.NewStyle().Foreground(theme.accent.GetForeground()).Render("[" + grantRoleLabel(robot.Role) + "]")
		access = append(access, spread(grantTruncate(robot.Repository, max(inner-lipgloss.Width(chip)-1, 1)), chip, inner))
	}
	counts := pluralUnit(boolInt(robot.Repository != ""), "repository")
	if robot.Repository == "" {
		counts = "0 repositories"
	}
	if tokens, known := s.tokens[robot.ID]; known {
		counts += ", " + pluralUnit(int64(len(liveTokens(tokens))), "token")
	}
	access = append(access, theme.muted.Render(grantTruncate(counts, inner)))

	toggle := action("Disable robot", "d", theme.error)
	if !robot.Enabled {
		toggle = action("Enable robot", "e", theme.text)
	}
	actions := []string{
		label("ACTIONS"),
		action("New robot", "n", theme.text),
		action("Tokens", "t", theme.text),
		action("Grant access", "g", theme.text),
		toggle,
		action("Delete robot", "x", theme.error),
	}

	blocks := [][]string{head, access, actions}
	if attention := s.attention(theme, robot, inner, now); len(attention) > 0 {
		blocks = append(blocks, append([]string{label("NEEDS ATTENTION")}, attention...))
	}
	return blocks
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// attention lists what is wrong with the selected robot: no repository access
// and tokens ending within a week. Each item wraps, the key hint ends its last
// line, and at most two items show.
func (s robotsScreen) attention(theme adminTheme, robot ports.AdminRobot, inner int, now time.Time) []string {
	type item struct {
		text, hint string
		style      lipgloss.Style
	}
	var items []item
	if withoutAccess(robot) {
		items = append(items, item{"No repository access", "g grant", theme.warning})
	}
	for _, token := range liveTokens(s.tokens[robot.ID]) {
		left := token.ExpiresAt.Sub(now)
		switch expiryToneOf(left) {
		case expiryExpired:
			items = append(items, item{fmt.Sprintf("Token %s expired", tokenLabel(token)), "t renew", theme.error})
		case expirySoon:
			items = append(items, item{fmt.Sprintf("Token %s expires in %s", tokenLabel(token), timeLeftText(left)), "t renew", theme.warning})
		}
	}
	var lines []string
	for i, it := range items {
		if i == 2 {
			lines = append(lines, theme.muted.Render(fmt.Sprintf("+%d more", len(items)-2)))
			break
		}
		wrapped := grantWrap(it.text, inner)
		for j, line := range wrapped {
			if j < len(wrapped)-1 {
				lines = append(lines, it.style.Render(line))
				continue
			}
			hint := theme.muted.Render(it.hint)
			if lipgloss.Width(line)+1+lipgloss.Width(hint) <= inner {
				lines = append(lines, spread(it.style.Render(line), hint, inner))
			} else {
				lines = append(lines, it.style.Render(line), spread("", hint, inner))
			}
		}
	}
	return lines
}

// tokenLabel is the token's name, or its short accessor when it has none.
func tokenLabel(token ports.AdminToken) string {
	if token.Name != "" {
		return token.Name
	}
	return shortAccessor(token.Accessor)
}
