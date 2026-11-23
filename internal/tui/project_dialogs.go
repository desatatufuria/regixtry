package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// noticeDialog is a refusal the operator can only close: a warning-colored
// dialog with the same parts as a confirm (title, text, target box,
// consequence) and one Close button. Nothing was changed when it shows.
type noticeDialog struct {
	title, text, target, detail, consequence string
}

func (n noticeDialog) active() bool { return n.title != "" }

func (n noticeDialog) view(theme adminTheme) string {
	inner := confirmDialogWidth - confirmDialogChrome
	tone := theme.warning.GetForeground()
	lines := []string{lipgloss.NewStyle().Foreground(tone).Bold(true).Render(n.title)}
	if n.text != "" {
		lines = append(lines, "", strings.Join(grantWrap(n.text, inner), "\n"))
	}
	if n.target != "" {
		box := theme.text.Bold(true).Render(n.target)
		if n.detail != "" {
			box += "\n" + theme.muted.Render(n.detail)
		}
		lines = append(lines, "", lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(theme.borderColor).
			Padding(0, 1).Width(inner).Render(box))
	}
	if n.consequence != "" {
		lines = append(lines, "", theme.warning.Render(strings.Join(grantWrap(n.consequence, inner), "\n")))
	}
	closeButton := lipgloss.NewStyle().Width(inner).Padding(0, 1).Bold(true).
		Foreground(theme.pill.GetForeground()).Background(tone).
		Render(confirmButtonLine("Close", "esc / enter", inner-2))
	lines = append(lines, "", closeButton)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(tone).
		Padding(0, 2).Width(confirmDialogWidth).
		Render(strings.Join(lines, "\n"))
}

// view renders the New project dialog: the name field, the live validation
// message, the target box, the consequence and the buttons. The border is the
// accent while the name is valid (or still empty) and the error color while it
// is wrong; Create is dimmed until the name is valid.
func (d projectCreateDialog) view(theme adminTheme, saving bool) string {
	inner := confirmDialogWidth - confirmDialogChrome
	issue := checkProjectName(d.name)
	valid := issue == projectNameValid
	tone := theme.accent.GetForeground()
	if !valid && issue != projectNameEmpty {
		tone = theme.error.GetForeground()
	}

	status := theme.success.Render("✓ Valid name.")
	switch {
	case saving:
		status = theme.muted.Render("Creating project…")
	case d.err != "":
		status = theme.error.Render(strings.Join(grantWrap(d.err, inner), "\n"))
	case issue == projectNameEmpty:
		status = theme.muted.Render(projectNameMessage(d.name))
	case !valid:
		status = theme.error.Render(strings.Join(grantWrap(projectNameMessage(d.name), inner), "\n"))
	}

	targetName, targetSub := "(no valid name yet)", "type a lowercase name"
	consequence := "Nothing is created until the name is valid."
	if valid {
		targetName, targetSub = d.name, "new project, 0 repositories"
		consequence = fmt.Sprintf("Creates project %s. Repositories pushed as %s/... belong to it.", d.name, d.name)
	}
	box := theme.text.Bold(true).Render(grantTruncate(targetName, inner-4)) + "\n" + theme.muted.Render(targetSub)

	create := confirmButtonLine("Create project", "enter", inner-2)
	createButton := lipgloss.NewStyle().Width(inner).Padding(0, 1).Bold(true).
		Foreground(theme.pill.GetForeground()).Background(tone).Render(create)
	if !valid || saving {
		createButton = lipgloss.NewStyle().Width(inner).Padding(0, 1).Faint(true).Render(create)
	}
	cancel := lipgloss.NewStyle().Width(inner).Padding(0, 1).
		Render("Cancel" + strings.Repeat(" ", max(inner-2-len("Cancel")-len("esc"), 1)) + theme.muted.Render("esc"))

	lines := []string{
		lipgloss.NewStyle().Foreground(tone).Bold(true).Render("New project"),
		"",
		theme.muted.Render("Project name"),
		renderEntryField(theme, d.name, "e.g. odabank", true, inner),
		status,
		"",
		lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.borderColor).Padding(0, 1).Width(inner).Render(box),
		"",
		theme.warning.Render(strings.Join(grantWrap(consequence, inner), "\n")),
		"",
		createButton,
		cancel,
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(tone).
		Padding(0, 2).Width(confirmDialogWidth).
		Render(strings.Join(lines, "\n"))
}
