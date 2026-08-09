package tui

import "github.com/charmbracelet/lipgloss"

type adminTheme struct {
	app         lipgloss.Style
	section     lipgloss.Style
	title       lipgloss.Style
	context     lipgloss.Style
	subheading  lipgloss.Style
	muted       lipgloss.Style
	text        lipgloss.Style
	accent      lipgloss.Style
	selected    lipgloss.Style
	success     lipgloss.Style
	warning     lipgloss.Style
	error       lipgloss.Style
	input       lipgloss.Style
	inputFocus  lipgloss.Style
	pill        lipgloss.Style
	help        lipgloss.Style
	inputWidth  int
	secretWidth int
}

func newAdminTheme() adminTheme {
	border := lipgloss.Color("#4C566A")
	text := lipgloss.Color("#ECEFF4")
	muted := lipgloss.Color("#A7B1C2")
	accent := lipgloss.Color("#88C0D0")
	selected := lipgloss.Color("#2E3440")
	selectedBG := lipgloss.Color("#81A1C1")
	success := lipgloss.Color("#A3BE8C")
	warning := lipgloss.Color("#EBCB8B")
	errorColor := lipgloss.Color("#BF616A")

	return adminTheme{
		app:         lipgloss.NewStyle().Padding(0, 1),
		section:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(1).Width(88),
		title:       lipgloss.NewStyle().Foreground(text).Bold(true),
		context:     lipgloss.NewStyle().Foreground(accent),
		subheading:  lipgloss.NewStyle().Foreground(accent).Bold(true),
		muted:       lipgloss.NewStyle().Foreground(muted),
		text:        lipgloss.NewStyle().Foreground(text),
		accent:      lipgloss.NewStyle().Foreground(accent).Bold(true),
		selected:    lipgloss.NewStyle().Foreground(selected).Background(selectedBG).Bold(true).Padding(0, 1),
		success:     lipgloss.NewStyle().Foreground(success).Bold(true),
		warning:     lipgloss.NewStyle().Foreground(warning).Bold(true),
		error:       lipgloss.NewStyle().Foreground(errorColor).Bold(true),
		input:       lipgloss.NewStyle().Foreground(text).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1).Width(30),
		inputFocus:  lipgloss.NewStyle().Foreground(text).Border(lipgloss.NormalBorder()).BorderForeground(accent).Padding(0, 1).Width(30),
		pill:        lipgloss.NewStyle().Foreground(selected).Background(accent).Bold(true).Padding(0, 1),
		help:        lipgloss.NewStyle().Foreground(muted),
		inputWidth:  30,
		secretWidth: 54,
	}
}
