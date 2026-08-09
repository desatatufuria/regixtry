package tui

import "github.com/charmbracelet/lipgloss"

type adminTheme struct {
	app        lipgloss.Style
	sidebar    lipgloss.Style
	panel      lipgloss.Style
	modal      lipgloss.Style
	section    lipgloss.Style
	heading    lipgloss.Style
	muted      lipgloss.Style
	text       lipgloss.Style
	accent     lipgloss.Style
	selected   lipgloss.Style
	success    lipgloss.Style
	warning    lipgloss.Style
	error      lipgloss.Style
	input      lipgloss.Style
	inputFocus lipgloss.Style
	badge      lipgloss.Style
	border     lipgloss.Color
	bg         lipgloss.Color
}

func newAdminTheme() adminTheme {
	bg := lipgloss.Color("#0D0D0F")
	surface := lipgloss.Color("#151518")
	surfaceAlt := lipgloss.Color("#1C1C20")
	border := lipgloss.Color("#29292E")
	text := lipgloss.Color("#F2F0EB")
	muted := lipgloss.Color("#A09DA6")
	accent := lipgloss.Color("#D6B56D")
	secondary := lipgloss.Color("#9D8FC1")
	success := lipgloss.Color("#78A083")
	warning := lipgloss.Color("#D6B56D")
	errorColor := lipgloss.Color("#C87575")

	return adminTheme{
		app:        lipgloss.NewStyle().Background(bg).Foreground(text).Padding(0, 1),
		sidebar:    lipgloss.NewStyle().Background(surface).Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(1).Width(34),
		panel:      lipgloss.NewStyle().Background(surfaceAlt).Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(1).Width(84),
		modal:      lipgloss.NewStyle().Background(surfaceAlt).Border(lipgloss.DoubleBorder()).BorderForeground(accent).Padding(1).Width(54),
		section:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1),
		heading:    lipgloss.NewStyle().Foreground(accent).Bold(true),
		muted:      lipgloss.NewStyle().Foreground(muted),
		text:       lipgloss.NewStyle().Foreground(text),
		accent:     lipgloss.NewStyle().Foreground(accent).Bold(true),
		selected:   lipgloss.NewStyle().Foreground(text).Background(secondary).Bold(true).Padding(0, 1),
		success:    lipgloss.NewStyle().Foreground(success).Bold(true),
		warning:    lipgloss.NewStyle().Foreground(warning).Bold(true),
		error:      lipgloss.NewStyle().Foreground(errorColor).Bold(true),
		input:      lipgloss.NewStyle().Foreground(text).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1),
		inputFocus: lipgloss.NewStyle().Foreground(text).Border(lipgloss.NormalBorder()).BorderForeground(accent).Padding(0, 1),
		badge:      lipgloss.NewStyle().Foreground(bg).Background(accent).Bold(true).Padding(0, 1),
		border:     border,
		bg:         bg,
	}
}
