package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	createRobotLabelWidth  = 12
	createRobotInputWidth  = 44
	createRobotMaxBoxWidth = 120
	// createRobotSuggestionWindow is how many repository suggestions show.
	createRobotSuggestionWindow = 5
)

func (s createRobotScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH := shellContentSize(width, height)

	var lines []string
	if s.done && s.reveal {
		return screenFrame{
			Context: adminContext("Access", "Robots", "New robot"),
			Full:    true,
			Body:    renderReveal(theme, s.revealSpec(width, height)),
		}
	}
	if s.done {
		lines = s.doneLines(theme, bodyW, bodyH)
	} else {
		lines = s.formLines(theme, env, bodyW, bodyH)
	}
	return screenFrame{
		Context: adminContext("Access", "Robots", "New robot"),
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: shellCrumb("Access", "Robots", "New robot"),
			Operator:   env.Session.Username,
			Active:     shellNavRobots,
			Body:       strings.Join(lines, "\n"),
			Hints:      s.Keys().short,
			Width:      width,
			Height:     height,
		}),
	}
}

// formLines lays the form out in at most rows rows. The fields, the Lifetime
// rows and the buttons are never cut: only the suggestion list shrinks
// (the highlighted row stays visible, then a muted "+N more" when rows allow),
// and the blank separators go first when even that is not enough.
func (s createRobotScreen) formLines(theme adminTheme, env screenEnv, width, rows int) []string {
	indent := strings.Repeat(" ", createRobotLabelWidth)
	inputW := min(createRobotInputWidth, max(width-createRobotLabelWidth, 8))
	label := func(text string) string { return padRight(theme.muted.Render(text), createRobotLabelWidth) }

	suggestions := grantRepoRows(env.KnownRepositories, s.repository)
	selected := boundedIndex(s.suggestion, len(suggestions))

	build := func(window int, blanks bool) []string {
		blank := func() []string {
			if blanks {
				return []string{""}
			}
			return nil
		}
		lines := []string{theme.subheading.Render("New robot")}
		lines = append(lines, blank()...)
		lines = append(lines,
			label("Name")+renderEntryField(theme, s.name, "e.g. ci-publisher", s.focus == robotFieldName, inputW),
			label("Repository")+renderEntryField(theme, s.repository, "e.g. team/api", s.focus == robotFieldRepository, inputW),
		)
		lines = append(lines, s.suggestionLines(theme, suggestions, selected, window, indent, width)...)
		lines = append(lines, blank()...)
		lines = append(lines, label("Role")+s.roleOptions(theme))
		lines = append(lines, s.lifetimeLines(theme, env, indent, width)...)
		lines = append(lines, blank()...)
		lines = append(lines, s.statusLine(theme, width), s.buttons(theme))
		return lines
	}

	window := min(createRobotSuggestionWindow, len(suggestions))
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

func clipRows(lines []string, rows int) []string {
	if len(lines) > rows {
		return lines[:max(rows, 0)]
	}
	return lines
}

func (s createRobotScreen) suggestionLines(theme adminTheme, suggestions []grantRepoRow, selected, window int, indent string, width int) []string {
	if len(suggestions) == 0 {
		if strings.TrimSpace(s.repository) == "" {
			return []string{indent + theme.muted.Render("No repositories known yet. Type a name.")}
		}
		return []string{indent + theme.muted.Render("No match. Names are lowercase, like team/app.")}
	}
	start := 0
	if selected >= window {
		start = selected - window + 1
	}
	end := min(start+window, len(suggestions))
	textW := max(width-createRobotLabelWidth-2, 4)
	var lines []string
	for i := start; i < end; i++ {
		row := suggestions[i]
		name := row.Name
		if row.New {
			name += " (new name)"
		}
		name = grantTruncate(name, textW-2)
		switch {
		case i == selected && row.New:
			lines = append(lines, indent+theme.selected.Render("+ "+name))
		case i == selected:
			lines = append(lines, indent+theme.selected.Render("▸ "+name))
		case row.New:
			lines = append(lines, indent+" "+theme.success.Render("+")+" "+theme.muted.Render(name))
		default:
			lines = append(lines, indent+"   "+name)
		}
	}
	if more := len(suggestions) - window; more > 0 && window > 0 {
		lines = append(lines, indent+"   "+theme.muted.Render(fmt.Sprintf("+%d more", more)))
	}
	return lines
}

func (s createRobotScreen) roleOptions(theme adminTheme) string {
	var marks []string
	for _, option := range grantRoleOptions {
		mark := "( ) " + option.Label
		if option.Role == s.role {
			mark = "(*) " + option.Label
			if s.focus == robotFieldRole {
				mark = theme.selected.Render(mark)
			}
		}
		marks = append(marks, mark)
	}
	return strings.Join(marks, "  ")
}

// lifetimeLines is the shared selector's options, its exact expiry preview (or
// the validation message) and the leak warning.
func (s createRobotScreen) lifetimeLines(theme adminTheme, env screenEnv, indent string, width int) []string {
	focused := s.focus == robotFieldLifetime
	ceiling := lifetimeCeiling()
	options := strings.Join(lifetimeMarks(theme, s.lifetime, focused), "  ")
	head := padRight(theme.muted.Render("Lifetime"), createRobotLabelWidth)
	lines := []string{head + options}
	if s.lifetime.isCustom(ceiling) {
		custom := renderCustomInput(theme, s.lifetime.Custom, focused)
		if lipgloss.Width(lines[0])+2+lipgloss.Width(custom) <= width {
			lines[0] += "  " + custom
		} else {
			lines = append(lines, indent+custom)
		}
	}
	if duration, ok := s.lifetime.duration(ceiling); ok {
		lines = append(lines, indent+expiryPreview(env.now(), duration))
	} else {
		lines = append(lines, indent+theme.error.Render(grantTruncate(lifetimeInvalidText(), max(width-createRobotLabelWidth, 8))))
	}
	return append(lines, indent+theme.warning.Render(grantTruncate("A leaked token works until it expires or you revoke it. Pick the shortest lifetime CI can live with.", max(width-createRobotLabelWidth, 8))))
}

func (s createRobotScreen) statusLine(theme adminTheme, width int) string {
	switch {
	case s.creating:
		return theme.muted.Render("Creating robot...")
	case s.err != "":
		return theme.error.Render(grantTruncate(s.err, width))
	}
	return ""
}

func (s createRobotScreen) buttons(theme adminTheme) string {
	create := theme.pill.Render("Create robot  enter")
	if s.creating {
		create = theme.muted.Render("Create robot  enter")
	}
	return create + "  " + theme.muted.Render("Cancel  esc")
}

// doneLines is the screen after creation: what was made and the one-time
// secret, whole. The secret block and the warning are laid out first and never
// dropped; the details and the button give way when rows are short.
func (s createRobotScreen) doneLines(theme adminTheme, width, rows int) []string {
	created := s.created
	boxW := min(width, createRobotMaxBoxWidth)
	summary := theme.success.Render("✓") + " Robot " + theme.subheading.Render(created.name) + " created"
	if created.repository != "" {
		summary += fmt.Sprintf(", %s on %s", grantRoleLabel(created.role), created.repository)
	}

	secret := append([]string{theme.subheading.Render("New robot"), summary}, secretBox(theme, created.accessor, created.secret, boxW)...)
	secret = append(secret, theme.warning.Render(grantTruncate(oneTimeSecretWarning, width)))
	if s.copied {
		secret = append(secret, theme.success.Render(grantTruncate(copiedNotice, width)))
	}

	detail := func(name, value string) string {
		return padRight(theme.muted.Render(name), createRobotLabelWidth) + value
	}
	var details []string
	if !created.expires.IsZero() {
		details = append(details, detail("Expires", exactExpiry(created.expires)))
	}
	details = append(details, detail("Repository", created.repository), detail("Role", grantRoleLabel(created.role)))

	button := []string{theme.pill.Render("Done  enter")}
	// Whole when it fits (details above the button); otherwise the details go.
	if len(secret)+len(details)+len(button)+2 <= rows {
		return fitSections([][]string{secret, details, button}, rows)
	}
	return fitSections([][]string{secret, button}, rows)
}

// revealSpec is the bare screen of the one-time secret.
func (s createRobotScreen) revealSpec(width, height int) revealSpec {
	created := s.created
	facts := []string{"Robot  " + created.name, "Accessor  " + created.accessor}
	if !created.expires.IsZero() {
		facts = append(facts, "Expires  "+exactExpiry(created.expires))
	}
	if created.repository != "" {
		facts = append(facts, "Repository  "+created.repository, "Role  "+grantRoleLabel(created.role))
	}
	return revealSpec{
		Title: "Robot secret, shown once", Secret: created.secret, Facts: facts,
		Copied: s.copied, Width: width, Height: height,
	}
}
