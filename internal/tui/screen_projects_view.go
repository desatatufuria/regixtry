package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

const (
	projectsColGap   = 2
	projectsColRepos = 14
	projectsColTags  = 6
	projectsColPush  = 11
	// projectsRowsAbove is the body rows above the first table row: the title,
	// a blank, the table border and its header.
	projectsRowsAbove = 4
	// projectsRowsBelow is the table's bottom border and the status line.
	projectsRowsBelow = 2

	projectsHelp = "A project is the first path segment of a repository name. enter opens its members. d deletes a project only when it has no repositories. s cycles the sort: name, last push, repositories."
)

func (s adminProjectsScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH := shellContentSize(width, height)

	help := wrapStyled(theme.muted, projectsHelp, bodyW)
	if len(s.projects) == 0 || s.forbidden {
		help = nil
	}
	visible := max(bodyH-projectsRowsAbove-projectsRowsBelow-len(help), 1)

	lines := []string{s.titleLine(theme, bodyW), "", s.renderTable(theme, env, bodyW, visible), s.statusLine(theme, bodyW)}
	lines = append(lines, help...)
	if s.capturesInput() {
		// A dialog sits on a blank panel: no table edge is cut by it.
		lines = lines[:2]
	}

	frame := screenFrame{
		Context: adminContext("Access", "Projects"),
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: shellCrumb("Access", "Projects"),
			Operator:   env.Session.Username,
			Active:     shellNavProjects,
			Body:       strings.Join(lines, "\n"),
			Hints:      s.Keys().short,
			Width:      width,
			Height:     height,
		}),
	}
	switch {
	case s.dialog.open:
		frame.Overlay = s.dialog.view(theme, s.saving)
	case s.confirm.Active():
		frame.Overlay = s.confirm.view(theme)
	case s.refusal.active():
		frame.Overlay = s.refusal.view(theme)
	}
	return frame
}

func (s adminProjectsScreen) titleLine(theme adminTheme, width int) string {
	right := ""
	switch {
	case s.loaded && !s.forbidden && (s.loadErr == "" || len(s.projects) > 0):
		right = fmt.Sprintf("%s, sorted by %s", plural(len(s.projects), "project"), s.sortBy.label())
		if len(s.projects) == 0 {
			right = "0 projects"
		}
	case s.forbidden:
		right = "Not allowed"
	}
	rightStyled := theme.muted.Render(right)
	left := theme.subheading.MaxWidth(max(width-lipgloss.Width(rightStyled)-2, 1)).Render("Projects")
	return left + strings.Repeat(" ", max(width-lipgloss.Width(left)-lipgloss.Width(rightStyled), 1)) + rightStyled
}

func (s adminProjectsScreen) statusLine(theme adminTheme, width int) string {
	if s.status != "" {
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
	if s.loadErr != "" && len(s.projects) > 0 {
		return theme.error.Render(grantTruncate("Could not load projects: "+s.loadErr+". Press r to retry.", width))
	}
	return ""
}

// stateLines is what the table box holds instead of rows: loading, the load
// error, the not-allowed notice or the empty card.
func (s adminProjectsScreen) stateLines(theme adminTheme, inner int) []string {
	switch {
	case s.forbidden:
		return []string{theme.warning.Render("Only global administrators manage projects."), theme.muted.Render("Esc goes back.")}
	case !s.loaded:
		return []string{theme.muted.Render("Loading projects…")}
	case s.loadErr != "":
		return []string{theme.error.Render(grantTruncate("Could not load projects: "+s.loadErr+".", inner)), theme.muted.Render("Press r to retry.")}
	}
	lines := []string{theme.text.Bold(true).Render("No projects yet")}
	lines = append(lines, wrapStyled(theme.muted, "A project must exist before anyone can push to it. Press n to create the first one, for example odabank.", inner)...)
	return append(lines, wrapStyled(theme.muted, "Repositories pushed as odabank/api then belong to project odabank.", inner)...)
}

// projectColumns splits the table's inner width: the marker, the flexible
// name and the three fixed columns.
func projectColumns(inner int) (nameW int) {
	return max(inner-2-projectsColRepos-projectsColTags-projectsColPush-3*projectsColGap, 8)
}

func (s adminProjectsScreen) renderTable(theme adminTheme, env screenEnv, width, visible int) string {
	inner := max(width-4, 1)
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.borderColor).Padding(0, 1).Width(width)
	rows := s.sorted()
	if len(rows) == 0 || (!s.loaded && len(rows) == 0) {
		lines := s.stateLines(theme, inner)
		for len(lines) < visible+1 {
			lines = append(lines, "")
		}
		return box.Render(strings.Join(lines[:visible+1], "\n"))
	}

	nameW := projectColumns(inner)
	gap := strings.Repeat(" ", projectsColGap)
	arrow := func(mode projectSort) string {
		if s.sortBy != mode {
			return ""
		}
		if mode == projectSortName {
			return " ▲"
		}
		return " ▼"
	}
	header := "  " + padText("PROJECT"+arrow(projectSortName), nameW) + gap +
		rightAlignText("REPOSITORIES"+arrow(projectSortRepositories), projectsColRepos) + gap +
		rightAlignText("TAGS", projectsColTags) + gap +
		rightAlignText("LAST PUSH"+arrow(projectSortLastPush), projectsColPush)
	lines := []string{theme.muted.Bold(true).Render(fitText(header, inner))}

	cursor := s.index(rows)
	start, end := grantWindow(cursor, len(rows), visible)
	now := env.now()
	for i := start; i < end; i++ {
		project := rows[i]
		cells := padText(grantTruncate(project.Name, nameW), nameW) + gap +
			rightAlignText(fmt.Sprint(project.Repositories), projectsColRepos) + gap +
			rightAlignText(fmt.Sprint(project.Tags), projectsColTags) + gap +
			rightAlignText(projectLastPush(project.LastPushed, now), projectsColPush)
		if i == cursor {
			lines = append(lines, theme.selected.Padding(0).Render("▸ "+cells))
			continue
		}
		lines = append(lines, theme.text.Render("  "+cells))
	}
	for len(lines) < visible+1 {
		lines = append(lines, "")
	}
	return box.Render(strings.Join(lines, "\n"))
}

func rightAlignText(text string, width int) string {
	text = fitText(text, width)
	return strings.Repeat(" ", max(width-lipgloss.Width(text), 0)) + text
}

// projectLastPush is the last push as "19 Nov" (with the year once it is over
// a year old), or "never" when nothing was pushed.
func projectLastPush(at time.Time, now time.Time) string {
	if at.IsZero() {
		return "never"
	}
	if now.Sub(at) > 365*24*time.Hour {
		return at.Format("2 Jan 2006")
	}
	return at.Format("2 Jan")
}
