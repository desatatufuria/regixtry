package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
)

// Layout budget (see viewport.go): the section body gets env.Layout.SectionRows
// rows and sectionWidth(l)-2 columns. The panes take every row but one, which
// is the status line under them; each pane spends 2 rows on its border.
const (
	grantStatusRows   = 1
	grantPaneBorder   = 2
	grantPaneChromeW  = 4 // border (2) + horizontal padding (2)
	grantMinSectionRs = 9
)

func (s grantAccessScreen) View(theme adminTheme, env screenEnv) screenFrame {
	layout := env.Layout
	contentW := sectionWidth(layout) - 2
	rows := layout.SectionRows
	if rows < grantMinSectionRs {
		rows = grantMinSectionRs
	}
	innerH := rows - grantStatusRows - grantPaneBorder

	const gap = 1
	avail := contentW - 2*gap
	w1 := avail * 30 / 100
	w3 := avail * 32 / 100
	w2 := avail - w1 - w3

	panes := lipgloss.JoinHorizontal(lipgloss.Top,
		renderGrantPane(theme, s.whoPane(theme, env, w1-grantPaneChromeW, innerH), w1, innerH, s.focus == grantPaneWho),
		strings.Repeat(" ", gap),
		renderGrantPane(theme, s.reposPane(theme, env, w2-grantPaneChromeW, innerH), w2, innerH, s.focus == grantPaneRepos),
		strings.Repeat(" ", gap),
		renderGrantPane(theme, s.rolePane(theme, env, w3-grantPaneChromeW, innerH), w3, innerH, s.focus == grantPaneRole),
	)
	body := renderSection(theme, panes+"\n"+s.statusLine(theme, contentW), layout)
	return screenFrame{Context: "Identity & Access / Grant repository access", Body: body}
}

func renderGrantPane(theme adminTheme, lines []string, outerW, innerH int, focused bool) string {
	if outerW < grantPaneChromeW+1 {
		outerW = grantPaneChromeW + 1
	}
	lines = slices.Clone(lines)
	if len(lines) > innerH {
		lines = lines[:innerH]
	}
	for len(lines) < innerH {
		lines = append(lines, "")
	}
	borderColor := lipgloss.Color(paletteBorderHex)
	if focused {
		borderColor = lipgloss.Color(paletteAccentHex)
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(outerW).
		Render(strings.Join(lines, "\n"))
}

func grantPaneTitle(theme adminTheme, title string, focused bool) string {
	if focused {
		return theme.accent.Render(title)
	}
	return theme.subheading.Render(title)
}

func (s grantAccessScreen) statusLine(theme adminTheme, width int) string {
	text := grantTruncate(s.status, width)
	switch s.statusTone {
	case grantToneError:
		return theme.error.Render(text)
	case grantToneWarning:
		return theme.warning.Render(text)
	case grantToneSuccess:
		return theme.success.Render(text)
	case grantToneInfo:
		return theme.muted.Render(text)
	}
	return ""
}

func renderGrantFilter(theme adminTheme, value string, focused bool, width int) string {
	style := theme.input
	if focused {
		style = theme.inputFocus
	}
	style = style.Width(width)
	budget := width - 2
	text := value
	switch {
	case focused:
		text += "▏"
	case value == "":
		style = style.Foreground(theme.muted.GetForeground())
		text = "Filter…"
	}
	if runes := []rune(text); lipgloss.Width(text) > budget {
		for lipgloss.Width("…"+string(runes)) > budget && len(runes) > 0 {
			runes = runes[1:]
		}
		text = "…" + string(runes)
	}
	return style.Render(text)
}

// ---- Who pane ----

func (s grantAccessScreen) whoPane(theme adminTheme, env screenEnv, iw, h int) []string {
	lines := []string{
		grantPaneTitle(theme, "Who", s.focus == grantPaneWho),
		renderGrantFilter(theme, s.whoFilter, s.focus == grantPaneWho, iw),
	}
	already := min(max(h/4, 2), 6)
	listRows := max(h-2-1-already, 1)

	var listLines []string
	if !s.principalsLoaded {
		listLines = append(listLines, theme.muted.Render(grantTruncate("Loading users and robots...", iw)))
	}
	if s.usersErr != "" {
		listLines = append(listLines, theme.error.Render(grantTruncate("Users: "+s.usersErr, iw)))
	}
	if s.robotsErr != "" {
		listLines = append(listLines, theme.error.Render(grantTruncate("Robots: "+s.robotsErr, iw)))
	}
	filtered := filterGrantPrincipals(s.principals, s.whoFilter)
	if s.principalsLoaded && len(filtered) == 0 {
		text := "No users or robots yet."
		if strings.TrimSpace(s.whoFilter) != "" {
			text = "No users or robots match."
		}
		listLines = append(listLines, theme.muted.Render(grantTruncate(text, iw)))
	}
	itemRows := max(listRows-len(listLines), 1)
	cursor := boundedIndex(s.whoCursor, len(filtered))
	visible := itemRows
	if len(filtered) > itemRows {
		visible = max(itemRows-1, 1)
	}
	start, end := grantWindow(cursor, len(filtered), visible)
	textBudget := iw - 4
	for i := start; i < end; i++ {
		p := filtered[i]
		plain := grantTruncate(p.Name, textBudget-6)
		plain += strings.Repeat(" ", max(textBudget-6-lipgloss.Width(plain), 0)) + " " + fmt.Sprintf("%-5s", p.Kind)
		switch {
		case s.cursorShown && i == cursor:
			listLines = append(listLines, "▸ "+theme.selected.Render(plain))
		case p.ID == s.chosenID:
			listLines = append(listLines, "✓  "+plain+" ")
		default:
			listLines = append(listLines, "   "+plain+" ")
		}
	}
	if len(filtered) > visible {
		listLines = append(listLines, theme.muted.Render(grantTruncate(fmt.Sprintf("  %d of %d", cursor+1, len(filtered)), iw)))
	}
	listLines = grantClipLines(listLines, listRows)
	for len(listLines) < listRows {
		listLines = append(listLines, "")
	}
	lines = append(lines, listLines...)
	lines = append(lines, s.alreadyHasLines(theme, iw, already)...)
	return lines
}

func (s grantAccessScreen) alreadyHasLines(theme adminTheme, iw, rows int) []string {
	principal, ok := s.chosen()
	header := "Already has"
	if ok {
		header += " (" + principal.Name + ")"
	}
	lines := []string{theme.muted.Render(grantTruncate(header, iw))}
	if !ok {
		return lines
	}
	entry := s.grants[principal.ID]
	switch {
	case entry.err != "":
		return append(lines, theme.error.Render(grantTruncate(entry.err, iw)))
	case !entry.loaded:
		return append(lines, theme.muted.Render("Loading grants..."))
	case len(entry.items) == 0:
		return append(lines, theme.muted.Render("No grants yet."))
	}
	shown := entry.items
	more := 0
	if len(shown) > rows {
		shown, more = shown[:max(rows-1, 0)], len(entry.items)-max(rows-1, 0)
	}
	const roleW = 6
	for _, grant := range shown {
		name := grantTruncate(grant.Repository.Name, iw-roleW-1)
		pad := strings.Repeat(" ", max(iw-roleW-1-lipgloss.Width(name), 0))
		lines = append(lines, name+pad+" "+theme.muted.Render(grantTruncate(grantRoleLabel(grant.Role), roleW)))
	}
	if more > 0 {
		lines = append(lines, theme.muted.Render(fmt.Sprintf("+%d more", more)))
	}
	return lines
}

// ---- Repositories pane ----

func (s grantAccessScreen) reposPane(theme adminTheme, env screenEnv, iw, h int) []string {
	focused := s.focus == grantPaneRepos
	lines := []string{
		grantPaneTitle(theme, "Which repositories", focused),
		renderGrantFilter(theme, s.repoFilter, focused, iw),
		theme.muted.Render(grantTruncate("Exact names only, no wildcards.", iw)),
	}
	listRows := max(h-4, 1)
	rows := grantRepoRows(env.KnownRepositories, s.repoFilter)
	principal, hasPrincipal := s.chosen()
	var current []string
	if hasPrincipal {
		for _, grant := range s.grants[principal.ID].items {
			current = append(current, grant.Repository.Name)
		}
	}

	var listLines []string
	if len(rows) == 0 {
		text := "No repositories known yet. Type a name to add one."
		if strings.TrimSpace(s.repoFilter) != "" {
			text = "No match. Names are lowercase, like team/app."
		}
		listLines = append(listLines, theme.muted.Render(grantTruncate(text, iw)))
	}
	cursor := boundedIndex(s.repoCursor, len(rows))
	visible := listRows
	if len(rows) > listRows {
		visible = max(listRows-1, 1)
	}
	start, end := grantWindow(cursor, len(rows), visible)
	textBudget := iw - 4
	for i := start; i < end; i++ {
		row := rows[i]
		box := "[ ] "
		if s.selection.has(row.Name) {
			box = "[x] "
		}
		name, annotation := row.Name, ""
		switch {
		case row.New:
			name, annotation = "+ "+row.Name, "not pushed yet"
		case hasPrincipal:
			if role, ok := grantRoleOn(s.grants[principal.ID].items, row.Name); ok {
				annotation = principal.Name + " is " + grantRoleLabel(role)
			}
		}
		listLines = append(listLines, renderGrantRepoRow(theme, box, name, annotation, textBudget, i == cursor))
	}
	if len(rows) > visible {
		listLines = append(listLines, theme.muted.Render(grantTruncate(fmt.Sprintf("  %d of %d", cursor+1, len(rows)), iw)))
	}
	listLines = grantClipLines(listLines, listRows)
	for len(listLines) < listRows {
		listLines = append(listLines, "")
	}
	lines = append(lines, listLines...)
	selected := len(s.selection.ordered(env.KnownRepositories))
	lines = append(lines, theme.muted.Render(grantTruncate(fmt.Sprintf("%d selected, %d shown", selected, len(rows)), iw)))
	return lines
}

func renderGrantRepoRow(theme adminTheme, box, name, annotation string, budget int, highlighted bool) string {
	head := grantTruncate(box+name, budget)
	rest := budget - lipgloss.Width(head) - 2
	if rest < 3 || annotation == "" || lipgloss.Width(box+name) >= budget {
		annotation = ""
	} else {
		annotation = grantTruncate(annotation, rest)
	}
	if highlighted {
		plain := head
		if annotation != "" {
			plain += "  " + annotation
		}
		plain += strings.Repeat(" ", max(budget-lipgloss.Width(plain), 0))
		return "▸ " + theme.selected.Render(plain)
	}
	line := "   " + head
	if annotation != "" {
		line += "  " + theme.muted.Render(annotation)
	}
	return line
}

// ---- Role and review pane ----

func (s grantAccessScreen) rolePane(theme adminTheme, env screenEnv, iw, h int) []string {
	focused := s.focus == grantPaneRole
	lines := []string{grantPaneTitle(theme, "Role and review", focused)}
	for i, option := range grantRoleOptions {
		radio := fmt.Sprintf("( ) %d %s", i+1, option.Label)
		if option.Role == s.role {
			radio = theme.accent.Render(fmt.Sprintf("(•) %d %s", i+1, option.Label))
		}
		lines = append(lines, radio, "    "+theme.muted.Render(grantTruncate(option.Description, iw-4)))
	}
	boxRows := h - len(lines)
	if boxRows < 3 {
		return append(lines, s.reviewContent(theme, env, iw, h-len(lines))...)
	}
	content := s.reviewContent(theme, env, iw-2, boxRows-2)
	for len(content) < boxRows-2 {
		content = append(content, "")
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(paletteBorderHex)).
		Width(iw).
		Render(strings.Join(content, "\n"))
	return append(lines, strings.Split(box, "\n")...)
}

func (s grantAccessScreen) reviewContent(theme adminTheme, env screenEnv, width, budget int) []string {
	if budget < 1 {
		return nil
	}
	if len(s.results) > 0 {
		return grantResultLines(theme, s.results, width, budget)
	}
	principal, ok := s.chosen()
	name := "Nobody"
	if ok {
		name = grantTruncate(principal.Name, max(width/2, 8))
	}
	var plain []string
	if ok {
		plain = grantReviewLines(name, s.role, s.selection.ordered(env.KnownRepositories), width, budget)
	} else {
		plain = grantReviewLines(name, s.role, nil, width, budget)
	}
	lines := make([]string, len(plain))
	for i, line := range plain {
		lines[i] = line
		if strings.HasPrefix(line, "+") && strings.HasSuffix(line, " more") {
			lines[i] = theme.muted.Render(line)
		}
	}
	return lines
}

// grantResultLines shows one line per repository: "granted" or the error text.
// When they do not all fit, failures come first so they are never hidden.
func grantResultLines(theme adminTheme, results []grantOutcome, width, budget int) []string {
	ordered := slices.Clone(results)
	more := 0
	if len(ordered) > budget {
		slices.SortStableFunc(ordered, func(a, b grantOutcome) int {
			switch {
			case a.err != nil && b.err == nil:
				return -1
			case a.err == nil && b.err != nil:
				return 1
			}
			return 0
		})
		keep := max(budget-1, 0)
		ordered, more = ordered[:keep], len(results)-keep
	}
	lines := make([]string, 0, budget)
	for _, result := range ordered {
		tail := "granted"
		if result.err != nil {
			tail = strings.Join(strings.Fields(result.err.Error()), " ")
		}
		tail = grantTruncate(tail, max(width*2/3, 7))
		name := grantTruncate(result.repo, max(width-lipgloss.Width(tail)-2, 1))
		styled := theme.success.Render(tail)
		if result.err != nil {
			styled = theme.error.Render(tail)
		}
		lines = append(lines, name+"  "+styled)
	}
	if more > 0 {
		lines = append(lines, theme.muted.Render(fmt.Sprintf("+%d more", more)))
	}
	return lines
}
