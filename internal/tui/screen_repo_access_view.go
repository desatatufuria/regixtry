package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	domainauth "regixtry/internal/domain/auth"
)

// Layout of the Repository access board, in terminal cells: the table on the
// left, the repository summary panel on the right.
const (
	repoAccessPanelWidth = 38
	repoAccessColGap     = 2
	repoAccessColPrincip = 16
	repoAccessColRole    = 7
	repoAccessColSince   = 11
	// repoAccessTableChromeRows is every body row that is not a table row:
	// the title, the filter, the blank after it, the table's border (2) and
	// header (1), and the status line.
	repoAccessTableChromeRows = 7
)

func (s repoAccessScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH := shellContentSize(width, height)

	panelW := min(repoAccessPanelWidth, max(bodyW/3, 1))
	leftW := max(bodyW-panelW-2, 1)
	tableRows := max(bodyH-repoAccessTableChromeRows, 1)

	rows := s.visibleRows()
	selected, hasSelected := s.selected(rows)

	left := []string{
		s.titleLine(theme, bodyW),
		renderGrantFilter(theme, s.filter, s.filtering, leftW),
		"",
		s.renderTable(theme, env, rows, leftW, tableRows),
		s.statusLine(theme, leftW),
	}
	panel := s.renderPanel(theme, selected, hasSelected, panelW, tableRows+6)
	// The title spans both columns, so it sits above the join.
	columns := lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(left[1:], "\n"), strings.Repeat(" ", 2), panel)
	body := left[0] + "\n" + columns

	frame := screenFrame{
		Context: "Repository access",
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: shellCrumb("Access", "Repository access"),
			Operator:   env.Session.Username,
			Active:     shellNavRepositoryAccess,
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

func (s repoAccessScreen) titleLine(theme adminTheme, width int) string {
	right := theme.muted.Render(s.countText())
	left := theme.subheading.MaxWidth(max(width-lipgloss.Width(right)-2, 1)).Render("Repository access")
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

// countText is "11 grants on 4 repositories", flagged when later pages were
// not read, or "Reading grants… N loaded" while a page is on its way.
func (s repoAccessScreen) countText() string {
	switch {
	case s.loading:
		return s.readingText()
	case len(s.rows) == 0 && s.loadErr != "":
		return "Grants unavailable"
	}
	repos := map[string]bool{}
	for _, row := range s.rows {
		repos[row.repo] = true
	}
	text := fmt.Sprintf("%s on %s", plural(len(s.rows), "grant"), plural(len(repos), "repository"))
	if !s.done {
		text += " (not all loaded)"
	}
	return text
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	if strings.HasSuffix(noun, "y") {
		return fmt.Sprintf("%d %sies", n, strings.TrimSuffix(noun, "y"))
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// renderTable renders the grants box: exactly rows+3 lines (border, header
// and rows), the selected row always inside the window.
func (s repoAccessScreen) renderTable(theme adminTheme, env screenEnv, rows []repoAccessRow, width, visible int) string {
	inner := max(width-4, 1)
	principalW, roleW, sinceW := repoAccessColPrincip, repoAccessColRole, repoAccessColSince
	repoW := inner - principalW - roleW - sinceW - 3*repoAccessColGap - 2
	if repoW < 8 {
		// Too narrow for every column: the principal gives way first.
		principalW = max(principalW+repoW-8, 6)
		repoW = max(inner-principalW-roleW-sinceW-3*repoAccessColGap-2, 1)
	}
	cell := func(text string, w int) string {
		text = grantTruncate(text, w)
		return text + strings.Repeat(" ", max(w-lipgloss.Width(text), 0))
	}
	line := func(marker, repo, principal, role, since string) string {
		gap := strings.Repeat(" ", repoAccessColGap)
		return marker + cell(repo, repoW) + gap + cell(principal, principalW) + gap + cell(role, roleW) + gap + cell(since, sinceW)
	}

	lines := []string{theme.muted.Bold(true).Render(line("  ", "REPOSITORY", "PRINCIPAL", "ROLE", "SINCE"))}
	if len(rows) == 0 {
		lines = append(lines, theme.muted.Render(grantTruncate(s.emptyText(), inner)))
	}
	cursor := boundedIndex(s.cursor, len(rows))
	start, end := grantWindow(cursor, len(rows), visible)
	now := env.now()
	for i := start; i < end; i++ {
		row := rows[i]
		role, since := grantRoleLabel(row.role), repoAccessSince(row.since, now)
		switch {
		case i == cursor:
			lines = append(lines, theme.selected.Padding(0).Render(line("▸ ", row.repo, row.username, role, since)))
		default:
			roleStyle := theme.text
			if row.role == domainauth.RepoRoleReader {
				roleStyle = theme.muted
			}
			lines = append(lines, theme.text.Render("  "+cell(row.repo, repoW)+strings.Repeat(" ", repoAccessColGap)+cell(row.username, principalW)+strings.Repeat(" ", repoAccessColGap))+
				roleStyle.Render(cell(role, roleW))+strings.Repeat(" ", repoAccessColGap)+theme.muted.Render(cell(since, sinceW)))
		}
	}
	for len(lines) < visible+1 {
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.borderColor).
		Padding(0, 1).
		Width(width).
		Render(strings.Join(lines[:visible+1], "\n"))
}

func (s repoAccessScreen) emptyText() string {
	switch {
	case s.loading:
		return s.readingText()
	case s.loadErr != "":
		return "Could not read grants."
	case s.filter != "":
		return "No grants match."
	}
	return "No grants yet. Press g to grant access."
}

// readingText is the loading line, with how many grants arrived so far.
func (s repoAccessScreen) readingText() string {
	return fmt.Sprintf("Reading grants… %d loaded", len(s.rows))
}

// repoAccessSince is the grant date as "14 Oct", with the year once it is
// more than a year old.
func repoAccessSince(at time.Time, now time.Time) string {
	if at.IsZero() {
		return "-"
	}
	if now.Sub(at) > 365*24*time.Hour {
		return at.Format("2 Jan 2006")
	}
	return at.Format("2 Jan")
}

func (s repoAccessScreen) statusLine(theme adminTheme, width int) string {
	switch {
	case s.status != "":
		style := theme.muted
		switch s.statusTone {
		case grantToneError:
			style = theme.error
		case grantToneWarning:
			style = theme.warning
		case grantToneSuccess:
			style = theme.success
		}
		return style.Render(grantTruncate(s.status, width))
	case s.loadErr != "":
		return theme.error.Render(grantTruncate("Could not read grants: "+strings.TrimRight(s.loadErr, ".")+". Press r to retry.", width))
	}
	return theme.muted.Render(grantTruncate("Roles: Reader pulls, Writer pushes, Admin manages access.", width))
}

// ---- repository summary panel ----

type repoAccessWho struct {
	name string
	kind string
	role domainauth.RepoRole
}

func (s repoAccessScreen) whoHas(repo string) []repoAccessWho {
	var out []repoAccessWho
	for _, row := range s.rows {
		if row.repo == repo {
			out = append(out, repoAccessWho{name: row.username, kind: row.principal().Kind, role: row.role})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := grantRoleIndex(out[i].role), grantRoleIndex(out[j].role); ri != rj {
			return ri > rj
		}
		return strings.ToLower(out[i].name) < strings.ToLower(out[j].name)
	})
	return out
}

// renderPanel renders the summary of the selected grant's repository in
// exactly height rows.
func (s repoAccessScreen) renderPanel(theme adminTheme, row repoAccessRow, ok bool, width, height int) string {
	inner := max(width-4, 1)
	innerH := max(height-2, 1)
	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.borderColor).Padding(0, 1).Width(width).Height(height)
	if !ok {
		return style.Render(theme.muted.Render(grantTruncate("Select a grant to see who has access to its repository.", inner)))
	}

	who := s.whoHas(row.repo)
	spaced := func(spacer bool) (head, actions, warning []string) {
		blank := func() []string {
			if spacer {
				return []string{""}
			}
			return nil
		}
		head = append([]string{
			theme.subheading.Render(grantTruncate(row.repo, inner)),
			theme.muted.Render(grantTruncate(fmt.Sprintf("%s %s access", plural(len(who), "principal"), haveWord(len(who))), inner)),
		}, blank()...)
		head = append(head, theme.muted.Bold(true).Render("WHO HAS WHAT"))
		actions = append(blank(), theme.muted.Bold(true).Render("ACTIONS ON SELECTED GRANT"),
			repoAccessAction(theme, "Open principal", "enter", inner),
			repoAccessAction(theme, "Grant access", "g", inner),
			repoAccessRevokeAction(theme, fmt.Sprintf("Revoke %s on %s", row.username, row.repo), inner))
		if !hasRepoAdmin(who) {
			warning = append(blank(), wrapStyled(theme.warning, "No Admin on this repository. Only server admins can change access here.", inner)...)
		}
		return head, actions, warning
	}
	// The blank rows between the sections are the first thing to go when the
	// panel is too short to list everyone.
	head, actions, warning := spaced(true)
	if innerH-len(head)-len(actions)-len(warning) < len(who) {
		head, actions, warning = spaced(false)
	}
	capacity := innerH - len(head) - len(actions) - len(warning)
	shown := who
	hidden := 0
	if len(who) > capacity {
		shown = who[:max(capacity-1, 0)]
		hidden = len(who) - len(shown)
	}
	lines := append([]string(nil), head...)
	for _, w := range shown {
		lines = append(lines, repoAccessWhoLine(theme, w, inner))
	}
	if hidden > 0 {
		lines = append(lines, theme.muted.Render(fmt.Sprintf("+%d more", hidden)))
	}
	lines = append(lines, actions...)
	lines = append(lines, warning...)
	if len(lines) > innerH {
		lines = lines[:innerH]
	}
	return style.Render(strings.Join(lines, "\n"))
}

func haveWord(n int) string {
	if n == 1 {
		return "has"
	}
	return "have"
}

func hasRepoAdmin(who []repoAccessWho) bool {
	for _, w := range who {
		if w.role == domainauth.RepoRoleAdmin {
			return true
		}
	}
	return false
}

func wrapStyled(style lipgloss.Style, text string, width int) []string {
	wrapped := grantWrap(text, width)
	out := make([]string, len(wrapped))
	for i, line := range wrapped {
		out[i] = style.Render(line)
	}
	return out
}

func repoAccessWhoLine(theme adminTheme, who repoAccessWho, width int) string {
	chip := "[" + grantRoleLabel(who.role) + "]"
	chipStyle := theme.accent
	if who.role == domainauth.RepoRoleReader {
		chipStyle = theme.muted
	}
	chipText := chipStyle.Render(chip)
	room := max(width-lipgloss.Width(chip)-1, 1)
	name := grantTruncate(who.name, room)
	text := theme.text.Render(name)
	if who.kind != "" && lipgloss.Width(name)+1+len(who.kind) <= room {
		text += theme.muted.Render(" " + who.kind)
	}
	gap := max(width-lipgloss.Width(text)-lipgloss.Width(chipText), 1)
	return text + strings.Repeat(" ", gap) + chipText
}

func repoAccessAction(theme adminTheme, label, keyName string, width int) string {
	hint := theme.muted.Render(keyName)
	text := theme.text.Render(grantTruncate(label, max(width-lipgloss.Width(hint)-1, 1)))
	return text + strings.Repeat(" ", max(width-lipgloss.Width(text)-lipgloss.Width(hint), 1)) + hint
}

func repoAccessRevokeAction(theme adminTheme, label string, width int) string {
	hint := theme.muted.Render("x")
	text := theme.error.Render(grantTruncate(label, max(width-lipgloss.Width(hint)-1, 1)))
	return text + strings.Repeat(" ", max(width-lipgloss.Width(text)-lipgloss.Width(hint), 1)) + hint
}
