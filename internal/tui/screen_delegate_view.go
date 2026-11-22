package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	domainauth "regixtry/internal/domain/auth"
)

// Layout of the Delegate repository access board, in terminal cells: the
// grants table on the left, the repository panel on the right.
const (
	delegatePanelWidth = 38
	delegateColGap     = 2
	delegateColRole    = 7
	// delegateTableChromeRows is every body row that is not a table row: the
	// title, the blank after it, the table heading, the border (2), the
	// header (1) and the status line.
	delegateTableChromeRows = 7
)

func (s delegateScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH := shellContentSize(width, height)

	panelW := min(delegatePanelWidth, max(bodyW/3, 1))
	leftW := max(bodyW-panelW-2, 1)
	tableRows := max(bodyH-delegateTableChromeRows, 1)

	left := strings.Join([]string{
		theme.muted.Bold(true).Render(grantTruncate("GRANTS ON "+strings.ToUpper(s.repo), leftW)),
		s.renderTable(theme, leftW, tableRows),
		s.statusLine(theme, leftW),
	}, "\n")
	panel := s.renderPanel(theme, env.Session.Username, panelW, tableRows+5)
	columns := lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", 2), panel)
	body := s.titleLine(theme, bodyW) + "\n\n" + columns

	frame := screenFrame{
		Context: "Repository access",
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: []string{"regixtry", "Repository access", s.repo},
			Operator:   env.Session.Username,
			Active:     shellNavRepositoryAccess,
			RepoAdmin:  s.repo,
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

func (s delegateScreen) titleLine(theme adminTheme, width int) string {
	right := theme.muted.Render(grantTruncate(s.repo, max(width/2, 1)))
	left := theme.subheading.Render("Repository access")
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

// renderTable renders the grants box: exactly rows+3 lines (border, header
// and rows), the selected row always inside the window.
func (s delegateScreen) renderTable(theme adminTheme, width, visible int) string {
	inner := max(width-4, 1)
	roleW := delegateColRole
	userW := max(inner-roleW-delegateColGap-2, 1)
	cell := func(text string, w int) string {
		text = grantTruncate(text, w)
		return text + strings.Repeat(" ", max(w-lipgloss.Width(text), 0))
	}
	gap := strings.Repeat(" ", delegateColGap)

	lines := []string{theme.muted.Bold(true).Render("  " + cell("USERNAME", userW) + gap + cell("ROLE", roleW))}
	if len(s.rows) == 0 {
		lines = append(lines, theme.muted.Render(grantTruncate(s.emptyText(), inner)))
	}
	cursor := boundedIndex(s.cursor, len(s.rows))
	start, end := grantWindow(cursor, len(s.rows), visible)
	for i := start; i < end; i++ {
		row := s.rows[i]
		role := grantRoleLabel(row.Role)
		if i == cursor {
			lines = append(lines, theme.selected.Padding(0).Render("▸ "+cell(row.Username, userW)+gap+cell(role, roleW)))
			continue
		}
		roleStyle := theme.text
		switch row.Role {
		case domainauth.RepoRoleReader:
			roleStyle = theme.muted
		case domainauth.RepoRoleWriter:
			roleStyle = theme.accent
		}
		lines = append(lines, theme.text.Render("  "+cell(row.Username, userW)+gap)+roleStyle.Render(cell(role, roleW)))
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

func (s delegateScreen) emptyText() string {
	switch {
	case s.loading:
		return "Reading grants…"
	case s.loadErr != "":
		return "Could not read the grants."
	}
	return "No grants for this repository."
}

func (s delegateScreen) statusLine(theme adminTheme, width int) string {
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
		return theme.error.Render(grantTruncate("Could not read the grants (press r to retry): "+s.loadErr, width))
	}
	return theme.muted.Render(grantTruncate("You can give Reader or Writer. Admin grants: ask a global admin.", width))
}

// ---- repository panel ----

// renderPanel renders the repository panel in exactly height rows: the
// repository, the operator's own role when known, then the actions or, with
// the form open, the form.
func (s delegateScreen) renderPanel(theme adminTheme, operator string, width, height int) string {
	inner := max(width-4, 1)
	innerH := max(height-2, 1)
	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.borderColor).Padding(0, 1).Width(width).Height(height)

	lines := []string{theme.subheading.Render(grantTruncate(s.repo, inner))}
	if role, ok := s.ownRole(operator); ok {
		lines = append(lines, theme.muted.Render(grantTruncate("you are "+grantRoleLabel(role)+" on this repository", inner)))
	}
	if s.form.active {
		lines = append(lines, s.formLines(theme, inner)...)
	} else {
		lines = append(lines, "",
			theme.muted.Bold(true).Render("ACTIONS"),
			repoAccessAction(theme, "New grant", "n", inner),
			repoAccessAction(theme, "Edit role", "e", inner),
			repoAccessRevokeAction(theme, "Revoke grant", inner),
			"",
			theme.muted.Bold(true).Render("ADD GRANT"))
		lines = append(lines, wrapStyled(theme.muted, "Username and role (Reader or Writer). The repository is fixed to "+s.repo+".", inner)...)
	}
	if len(lines) > innerH {
		lines = lines[:innerH]
	}
	return style.Render(strings.Join(lines, "\n"))
}

// formLines is the new grant / edit role form: the username, the role, then
// the error and the keys. The error comes before the keys so a short panel
// clips the keys first.
func (s delegateScreen) formLines(theme adminTheme, inner int) []string {
	title := "NEW GRANT"
	if s.form.editing {
		title = "EDIT ROLE"
	}
	lines := []string{"", theme.muted.Bold(true).Render(title),
		theme.muted.Render("Username"),
		s.usernameField(theme, inner),
		theme.muted.Render("Role"),
		s.roleField(theme, inner),
	}
	if s.form.err != "" {
		lines = append(lines, wrapStyled(theme.error, s.form.err, inner)...)
	}
	lines = append(lines, "")
	lines = append(lines, wrapStyled(theme.muted, "Reader or Writer. The repository is fixed to "+s.repo+".", inner)...)
	return lines
}

func (s delegateScreen) usernameField(theme adminTheme, inner int) string {
	style := theme.input
	focused := s.form.focus == delegateFieldUsername && !s.form.editing
	if focused {
		style = theme.inputFocus
	}
	style = style.Width(inner)
	text := s.form.username
	switch {
	case s.form.editing:
		style = style.Foreground(theme.muted.GetForeground())
	case focused:
		text += "▏"
	case text == "":
		style = style.Foreground(theme.muted.GetForeground())
		text = "username"
	}
	if runes := []rune(text); lipgloss.Width(text) > inner-2 {
		for lipgloss.Width("…"+string(runes)) > inner-2 && len(runes) > 0 {
			runes = runes[1:]
		}
		text = "…" + string(runes)
	}
	return style.Render(text)
}

// roleField shows both roles a delegate can give, the current one marked.
func (s delegateScreen) roleField(theme adminTheme, inner int) string {
	style := theme.input
	if s.form.focus == delegateFieldRole {
		style = theme.inputFocus
	}
	chip := func(role domainauth.RepoRole) string {
		label := grantRoleLabel(role)
		if delegateClampRole(s.form.role) == role {
			return "[" + label + "]"
		}
		return " " + label + " "
	}
	text := chip(domainauth.RepoRoleReader) + " " + chip(domainauth.RepoRoleWriter)
	return style.Width(inner).Render(grantTruncate(text, inner-2))
}
