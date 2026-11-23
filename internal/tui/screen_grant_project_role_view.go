package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	domainauth "regixtry/internal/domain/auth"
)

const (
	grantProjectLabelWidth = 14
	grantProjectInputWidth = 44
	grantProjectWindow     = 5
)

func (s grantProjectRoleScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH := shellContentSize(width, height)
	return screenFrame{
		Context: adminContext("Access", "Projects", s.project, "Grant role"),
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: shellCrumb("Access", "Projects", s.project, "Grant role"),
			Operator:   env.Session.Username,
			Active:     shellNavProjects,
			Body:       strings.Join(s.formLines(theme, bodyW, bodyH), "\n"),
			Hints:      s.Keys().short,
			Width:      width,
			Height:     height,
		}),
	}
}

// formLines lays the form out in at most rows rows: the fields, the preview
// and the buttons are never cut; the suggestion list shrinks first, then the
// blank separators go.
func (s grantProjectRoleScreen) formLines(theme adminTheme, width, rows int) []string {
	indent := strings.Repeat(" ", grantProjectLabelWidth)
	inputW := min(grantProjectInputWidth, max(width-grantProjectLabelWidth, 8))
	label := func(text string) string { return padRight(theme.muted.Render(text), grantProjectLabelWidth) }
	suggestions := s.rows()
	selected := boundedIndex(s.suggestion, len(suggestions))

	build := func(window int, blanks bool) []string {
		blank := func() []string {
			if blanks {
				return []string{""}
			}
			return nil
		}
		right := theme.muted.Render("project " + s.project)
		left := theme.subheading.Render("Grant project role")
		lines := []string{left + strings.Repeat(" ", max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)) + right}
		lines = append(lines, blank()...)
		lines = append(lines,
			label("Member type")+s.kindOptions(theme),
			label("Member")+renderEntryField(theme, s.member, s.memberPlaceholder(), s.focus == grantProjectFieldMember, inputW),
		)
		lines = append(lines, s.suggestionLines(theme, suggestions, selected, window, indent, width)...)
		lines = append(lines, label("Project role")+s.roleOptions(theme))
		lines = append(lines, blank()...)
		lines = append(lines, s.previewLines(theme, width)...)
		lines = append(lines, s.statusLine(theme, width), s.buttons(theme))
		return lines
	}
	window := min(grantProjectWindow, len(suggestions))
	lines := build(window, true)
	if len(lines) > rows {
		lines = build(window, false)
	}
	for len(lines) > rows && window > 1 {
		window--
		lines = build(window, false)
	}
	return clipRows(lines, rows)
}

func (s grantProjectRoleScreen) memberPlaceholder() string {
	if s.principalsLoaded && s.principalsErr != "" {
		return "type the exact username"
	}
	return "type a name"
}

func (s grantProjectRoleScreen) kindOptions(theme adminTheme) string {
	mark := func(kind, text string) string {
		if s.kind != kind {
			return "( ) " + text
		}
		out := "(*) " + text
		if s.focus == grantProjectFieldKind {
			out = theme.selected.Render(out)
		}
		return out
	}
	return mark(grantKindUser, "User") + "  " + mark(grantKindRobot, "Robot")
}

func (s grantProjectRoleScreen) roleOptions(theme adminTheme) string {
	var marks []string
	for _, option := range grantRoleOptions {
		text := projectRoleLabel(option.Role)
		switch {
		case option.Role == domainauth.RepoRoleAdmin && s.kind == grantKindRobot:
			// Not available for robots: struck out and dimmed, still
			// selectable so the refusal can say why.
			text = theme.muted.Strikethrough(true).Faint(true).Render(text)
			if s.role == option.Role {
				text = "(*) " + text
			} else {
				text = "( ) " + text
			}
		case option.Role == s.role:
			text = "(*) " + text
			if s.focus == grantProjectFieldRole {
				text = theme.selected.Render(text)
			}
		default:
			text = "( ) " + text
		}
		marks = append(marks, text)
	}
	return strings.Join(marks, "  ")
}

func (s grantProjectRoleScreen) suggestionLines(theme adminTheme, rows []grantPrincipal, selected, window int, indent string, width int) []string {
	if len(rows) == 0 {
		switch {
		case !s.principalsLoaded:
			return []string{indent + theme.muted.Render("Loading…")}
		case s.principalsErr != "":
			return []string{indent + theme.muted.Render(grantTruncate("The list is not available to this session. Type the exact username.", max(width-grantProjectLabelWidth, 8)))}
		}
		return []string{indent + theme.muted.Render("No match.")}
	}
	start := 0
	if selected >= window {
		start = selected - window + 1
	}
	end := min(start+window, len(rows))
	textW := max(width-grantProjectLabelWidth-2, 4)
	var lines []string
	for i := start; i < end; i++ {
		text := grantTruncate(rows[i].Name, textW-8) + "  " + rows[i].Kind
		if i == selected {
			lines = append(lines, indent+theme.selected.Render("▸ "+text))
			continue
		}
		lines = append(lines, indent+"  "+theme.text.Render(grantTruncate(rows[i].Name, textW-8))+theme.muted.Render("  "+rows[i].Kind))
	}
	if more := len(rows) - window; more > 0 && window > 0 {
		lines = append(lines, indent+"  "+theme.muted.Render(fmt.Sprintf("+%d more", more)))
	}
	return lines
}

// previewLines is the box under the fields: what the grant does, or why a
// robot cannot be Project admin. It always holds a title and two text rows, so
// the form does not jump while the role changes.
func (s grantProjectRoleScreen) previewLines(theme adminTheme, width int) []string {
	inner := max(width-4, 1)
	wrap := func(text string) []string {
		lines := wrapStyled(theme.muted, text, inner)
		if len(lines) > 2 {
			lines = append(lines[:1], wrapStyled(theme.muted, grantTruncate(strings.Join(grantWrap(text, inner)[1:], " "), inner), inner)...)
		}
		for len(lines) < 2 {
			lines = append(lines, "")
		}
		return lines
	}
	var box []string
	tone := theme.borderColor
	switch {
	case s.robotAdmin():
		box = append([]string{theme.error.Render("Project admin is not available for robots")},
			wrap("Robots can be Project reader or Project writer only. Project admin is for users.")...)
		tone = theme.error.GetForeground()
	case strings.TrimSpace(s.member) == "":
		box = append([]string{theme.muted.Render("Pick a member to see what changes.")}, wrap("")...)
	default:
		name := strings.TrimSpace(s.member)
		box = append([]string{theme.text.Bold(true).Render(grantTruncate(fmt.Sprintf("%s becomes %s of %s", name, projectRoleLabel(s.role), s.project), inner))},
			wrap(s.roleEffect())...)
	}
	return strings.Split(lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(tone).Padding(0, 1).Width(width).Render(strings.Join(box, "\n")), "\n")
}

// roleEffect says what the chosen role gives, for every repository of the
// project, existing and future.
func (s grantProjectRoleScreen) roleEffect() string {
	switch s.role {
	case domainauth.RepoRoleWriter:
		return fmt.Sprintf("Read and write access to every repository in %s, existing and future. Applies from the next request.", s.project)
	case domainauth.RepoRoleAdmin:
		return fmt.Sprintf("Write access to every repository in %s, existing and future, and the right to give others Reader or Writer access to it. Applies from the next request.", s.project)
	}
	return fmt.Sprintf("Read access to every repository in %s, existing and future. Applies from the next request.", s.project)
}

func (s grantProjectRoleScreen) statusLine(theme adminTheme, width int) string {
	switch {
	case s.applying:
		return theme.muted.Render("Granting role…")
	case s.err != "":
		return theme.error.Render(grantTruncate(s.err, width))
	}
	return ""
}

func (s grantProjectRoleScreen) buttons(theme adminTheme) string {
	grant := theme.pill.Render("Grant role  enter")
	if s.applying || s.robotAdmin() {
		grant = theme.muted.Render("Grant role  enter")
	}
	return grant + "  " + theme.muted.Render("Cancel  esc")
}
