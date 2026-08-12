package tui

import "github.com/charmbracelet/lipgloss"

type adminTheme struct {
	app              lipgloss.Style
	section          lipgloss.Style
	tableHeader      lipgloss.Style
	borderColor      lipgloss.Color
	title            lipgloss.Style
	context          lipgloss.Style
	subheading       lipgloss.Style
	muted            lipgloss.Style
	text             lipgloss.Style
	accent           lipgloss.Style
	selected         lipgloss.Style
	success          lipgloss.Style
	warning          lipgloss.Style
	error            lipgloss.Style
	severityCritical lipgloss.Style
	severityHigh     lipgloss.Style
	severityMedium   lipgloss.Style
	severityLow      lipgloss.Style
	input            lipgloss.Style
	inputFocus       lipgloss.Style
	pill             lipgloss.Style
	help             lipgloss.Style
	inputWidth       int
	secretWidth      int
}

func newAdminTheme() adminTheme {
	border := lipgloss.Color("#4C566A")
	text := lipgloss.Color("#ECEFF4")
	muted := lipgloss.Color("#A7B1C2")
	accent := lipgloss.Color("#D4AF37")
	selected := lipgloss.Color("#2E3440")
	selectedBG := lipgloss.Color("#D4AF37")
	success := lipgloss.Color("#A3BE8C")
	warning := lipgloss.Color("#EBCB8B")
	errorColor := lipgloss.Color("#BF616A")

	return adminTheme{
		app: lipgloss.NewStyle().Padding(0, 1),
		// section is deliberately left without a fixed Width here: the
		// historical Width(88) (tui-table-viewport-fixed-size proposal's
		// deferred Q5) was narrower than the tables it wraps at realistic
		// terminal sizes, corrupting borders/alignment. Call sites that carry
		// a consoleLayout apply a real, viewport-derived width via
		// viewport.go's sectionWidth(); call sites without one (forms that
		// are not viewport-driven) fall back to auto-sizing to their content.
		section: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(1),
		// tableHeader/subheading/context deliberately stay off accent: gold is
		// reserved for interactive/selected state (selected, inputFocus, pill)
		// so it reads as "this is active" rather than decorating every static
		// label on screen -- restraint over decoration.
		tableHeader: lipgloss.NewStyle().Foreground(text).Bold(true),
		// borderColor: same palette as section's border, reused by bubble-table styling (Phase 4).
		borderColor:      border,
		title:            lipgloss.NewStyle().Foreground(text).Bold(true),
		context:          lipgloss.NewStyle().Foreground(muted),
		subheading:       lipgloss.NewStyle().Foreground(text).Bold(true),
		muted:            lipgloss.NewStyle().Foreground(muted),
		text:             lipgloss.NewStyle().Foreground(text),
		accent:           lipgloss.NewStyle().Foreground(accent).Bold(true),
		selected:         lipgloss.NewStyle().Foreground(selected).Background(selectedBG).Bold(true).Padding(0, 1),
		success:          lipgloss.NewStyle().Foreground(success).Bold(true),
		warning:          lipgloss.NewStyle().Foreground(warning).Bold(true),
		error:            lipgloss.NewStyle().Foreground(errorColor).Bold(true),
		severityCritical: lipgloss.NewStyle().Foreground(errorColor).Bold(true),
		severityHigh:     lipgloss.NewStyle().Foreground(warning).Bold(true),
		// severityMedium gets its own dedicated bronze hex rather than reusing
		// warning's amber: sharing a hex with severityHigh (differing only by
		// Bold) made the two indistinguishable with color disabled or bold
		// ignored (design.md Decision 3). "#C0A16B" is confirmed distinct from
		// both severityHigh ("#EBCB8B", ~1.6:1 luminance separation) and accent
		// gold ("#D4AF37", ~1.16:1 luminance but sharply different saturation —
		// muted bronze vs bright gold — and the two never share a role.
		severityMedium: lipgloss.NewStyle().Foreground(lipgloss.Color("#C0A16B")),
		severityLow:    lipgloss.NewStyle().Foreground(muted),
		input:          lipgloss.NewStyle().Foreground(text).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1).Width(30),
		inputFocus:     lipgloss.NewStyle().Foreground(text).Border(lipgloss.NormalBorder()).BorderForeground(accent).Padding(0, 1).Width(30),
		pill:           lipgloss.NewStyle().Foreground(selected).Background(accent).Bold(true).Padding(0, 1),
		help:           lipgloss.NewStyle().Foreground(muted),
		inputWidth:     30,
		secretWidth:    54,
	}
}
