package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	membersColGap  = 2
	membersColType = 6
	membersColRole = 14
	membersColDate = 11
	// membersPanelWidth is the repositories panel on the right.
	membersPanelWidth = 40
	// membersRowsAbove is the body rows above the two panels: the title, the
	// subtitle and a blank; membersRowsBelow the status line under them.
	membersRowsAbove = 3
	membersRowsBelow = 1
	// membersTableAbove is the panel rows above the first member row: the
	// border, the label and the column header.
	membersTableAbove = 3
)

// membersLayout is the geometry both the view and the mouse use: the panel
// widths and how many member rows and help lines fit.
func (s adminProjectMembersScreen) membersLayout(bodyW, bodyH int) (leftW, panelW, panelH, helpLines int) {
	panelW = min(membersPanelWidth, max(bodyW/3, 1))
	leftW = max(bodyW-panelW-2, 1)
	helpLines = len(grantWrap(s.helpText(), bodyW))
	panelH = max(bodyH-membersRowsAbove-membersRowsBelow-helpLines, 5)
	return
}

func (s adminProjectMembersScreen) helpText() string {
	return fmt.Sprintf("A project role covers every repository inside %s, now and later. A repository grant can only add access.", s.project)
}

func (s adminProjectMembersScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH := shellContentSize(width, height)
	leftW, panelW, panelH, _ := s.membersLayout(bodyW, bodyH)
	repos := s.repositories(env)

	columns := lipgloss.JoinHorizontal(lipgloss.Top,
		s.renderMembers(theme, env, leftW, panelH),
		strings.Repeat(" ", 2),
		s.renderRepositories(theme, repos, panelW, panelH),
	)
	lines := []string{
		theme.subheading.Render(fitText(s.project, bodyW)),
		theme.muted.Render(fitText(s.subtitle(len(repos)), bodyW)),
		"",
		columns,
		s.statusLine(theme, bodyW),
	}
	if !s.notFound {
		lines = append(lines, wrapStyled(theme.muted, s.helpText(), bodyW)...)
	}
	if s.confirm.Active() {
		// The dialog sits on a blank panel: no panel edge is cut by it.
		lines = lines[:3]
	}
	frame := screenFrame{
		Context: adminContext("Access", "Projects", s.project, "Members"),
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: shellCrumb("Access", "Projects", s.project, "Members"),
			Operator:   env.Session.Username,
			Active:     shellNavProjects,
			Body:       strings.Join(lines, "\n"),
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

// subtitle is "project, created 2026-11-02, 3 repositories, 5 members"; the
// created date only when the project record could be read.
func (s adminProjectMembersScreen) subtitle(repositories int) string {
	if s.notFound {
		return "project not found"
	}
	parts := []string{"project"}
	if s.hasInfo && !s.info.CreatedAt.IsZero() {
		parts = append(parts, "created "+s.info.CreatedAt.Format("2006-01-02"))
	}
	parts = append(parts, plural(repositories, "repository"))
	if s.loaded && s.loadErr == "" {
		members := plural(len(s.members), "member")
		if s.truncated {
			members = fmt.Sprintf("first %s", members)
		}
		parts = append(parts, members)
	}
	return strings.Join(parts, ", ")
}

func (s adminProjectMembersScreen) statusLine(theme adminTheme, width int) string {
	if s.status == "" {
		return ""
	}
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
}

func (s adminProjectMembersScreen) stateLines(theme adminTheme, inner int) []string {
	switch {
	case s.notFound:
		return []string{theme.error.Render(grantTruncate(fmt.Sprintf("Project %s does not exist.", s.project), inner)), theme.muted.Render("Esc goes back to the Projects list.")}
	case !s.loaded:
		return []string{theme.muted.Render("Loading members…")}
	case s.loadErr != "":
		return []string{theme.error.Render(grantTruncate("Could not load members: "+s.loadErr+".", inner)), theme.muted.Render("Press r to retry.")}
	}
	return wrapStyled(theme.muted, "No members yet. Press g to give a user or robot a project role. Global administrators always have access.", inner)
}

func (s adminProjectMembersScreen) renderMembers(theme adminTheme, env screenEnv, width, height int) string {
	inner := max(width-4, 1)
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.borderColor).Padding(0, 1).Width(width).Height(height)
	label := theme.muted.Bold(true).Render("MEMBERS")
	lines := []string{label}
	if len(s.members) == 0 {
		return box.Render(strings.Join(append(lines, s.stateLines(theme, inner)...), "\n"))
	}

	roleW, typeW, dateW := membersColRole, membersColType, membersColDate
	nameW := max(inner-2-typeW-roleW-dateW-3*membersColGap, 8)
	gap := strings.Repeat(" ", membersColGap)
	row := func(marker, name, kind, role, since string) string {
		return marker + padText(grantTruncate(name, nameW), nameW) + gap + padText(kind, typeW) + gap + padText(role, roleW) + gap + padText(since, dateW)
	}
	lines = append(lines, theme.muted.Bold(true).Render(fitText(row("  ", "MEMBER", "TYPE", "PROJECT ROLE", "SINCE"), inner)))

	visible := max(height-2-len(lines), 1)
	cursor := s.index()
	start, end := grantWindow(cursor, len(s.members), visible)
	now := env.now()
	for i := start; i < end; i++ {
		member := s.members[i]
		kind := "user"
		if member.IsRobot {
			kind = "robot"
		}
		text := row("  ", member.Username, kind, projectRoleLabel(member.Role), repoAccessSince(member.CreatedAt, now))
		switch {
		case i == cursor && !s.focusRepos:
			text = row("▸ ", member.Username, kind, projectRoleLabel(member.Role), repoAccessSince(member.CreatedAt, now))
			lines = append(lines, theme.selected.Padding(0).Render(fitText(text, inner)))
		default:
			lines = append(lines, theme.text.Render(fitText(text, inner)))
		}
	}
	return box.Render(strings.Join(lines, "\n"))
}

func (s adminProjectMembersScreen) renderRepositories(theme adminTheme, repos []string, width, height int) string {
	inner := max(width-4, 1)
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.borderColor).Padding(0, 1).Width(width).Height(height)
	footer := wrapStyled(theme.muted, fmt.Sprintf("%s. Browse them in Console › Projects › %s.", plural(len(repos), "repository"), s.project), inner)
	lines := []string{theme.muted.Bold(true).Render(grantTruncate("REPOSITORIES IN THIS PROJECT", inner))}
	rows := max(height-2-len(lines)-len(footer), 1)
	if len(repos) == 0 {
		lines = append(lines, theme.muted.Render("No repositories yet."))
	}
	cursor := boundedIndex(s.repoCursor, len(repos))
	start, end := grantWindow(cursor, len(repos), rows)
	for i := start; i < end; i++ {
		name := grantTruncate(repos[i], inner-2)
		if s.focusRepos && i == cursor {
			lines = append(lines, theme.selected.Padding(0).Render(fitText("▸ "+name, inner)))
			continue
		}
		lines = append(lines, theme.text.Render("  "+name))
	}
	for len(lines) < height-2-len(footer) {
		lines = append(lines, "")
	}
	return box.Render(strings.Join(append(lines, footer...), "\n"))
}
