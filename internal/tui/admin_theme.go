package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// The palette of the approved design (screens Main, GrantAccess, Tokens).
// This is the ONE place colors are defined; every other file in this package
// reaches them through adminTheme or the role constants below
// (palette_test.go fails on any other color literal).
const (
	paletteBgHex           = "#0f1117" // page background (the terminal's own is kept: nothing paints it)
	palettePanelHex        = "#161a23" // panel/card, unfocused input fill
	paletteBarHex          = "#12151d" // header, sidebar and footer bars
	paletteBorderHex       = "#2a3042"
	paletteBorderStrongHex = "#3a4258"
	paletteTextHex         = "#d7dce8"
	paletteMutedHex        = "#8a92a8"
	paletteAccentHex       = "#7aa2f7"
	paletteSelectedBGHex   = "#1c2540" // selected/active row, accent text on it
	paletteWarningHex      = "#e0af68"
	paletteSuccessHex      = "#9ece6a"
	paletteErrorHex        = "#f7768e"
)

type adminTheme struct {
	app              lipgloss.Style
	section          lipgloss.Style
	tableHeader      lipgloss.Style
	borderColor      color.Color
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
	border := lipgloss.Color(paletteBorderHex)
	text := lipgloss.Color(paletteTextHex)
	muted := lipgloss.Color(paletteMutedHex)
	accent := lipgloss.Color(paletteAccentHex)
	// Selected rows keep their role: accent text on the design's selected
	// background. The primary button (pill) is page-colored text on accent.
	selectedBG := lipgloss.Color(paletteSelectedBGHex)
	pillText := lipgloss.Color(paletteBgHex)
	// fieldBG fills unfocused inputs with the panel color: delimitation by
	// fill, not border, so the 24-row budget is unchanged.
	fieldBG := lipgloss.Color(palettePanelHex)
	success := lipgloss.Color(paletteSuccessHex)
	warning := lipgloss.Color(paletteWarningHex)
	errorColor := lipgloss.Color(paletteErrorHex)

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
		selected:         lipgloss.NewStyle().Foreground(accent).Background(selectedBG).Bold(true).Padding(0, 1),
		success:          lipgloss.NewStyle().Foreground(success).Bold(true),
		warning:          lipgloss.NewStyle().Foreground(warning).Bold(true),
		error:            lipgloss.NewStyle().Foreground(errorColor).Bold(true),
		severityCritical: lipgloss.NewStyle().Foreground(errorColor).Bold(true),
		severityHigh:     lipgloss.NewStyle().Foreground(warning).Bold(true),
		// severityMedium: the design has no hue for it, so it uses the accent
		// blue (paletteAccentHex); distinct from critical, high and low.
		severityMedium: lipgloss.NewStyle().Foreground(lipgloss.Color(paletteAccentHex)),
		severityLow:    lipgloss.NewStyle().Foreground(muted),
		// input/inputFocus stay borderless (design.md Decision 5: a
		// top/bottom border only ever carried a focus color, so 2 rows per
		// field were spent to communicate one bit already expressible in
		// color -- removing it is what let the Trivy Config modal fit the
		// 24-row floor). Delimitation instead comes from a filled
		// background on BOTH states, not just focus: unfocused fields get
		// fieldBG (a visibly boxed, slightly raised panel) so an empty or
		// unfocused field still reads as "this is a text box" even with no
		// border rune anywhere -- addressing that gap without spending any
		// extra rows. Width(30) is kept on both so values stay
		// column-aligned and the modal's width cannot jitter as focus
		// moves. renderTextField/renderSecretField/renderToggleField are
		// not edited -- they already resolve input vs inputFocus and emit
		// "label\nvalue", so this stays a pure theme-token change.
		input:       lipgloss.NewStyle().Foreground(text).Background(fieldBG).Padding(0, 1).Width(30),
		inputFocus:  lipgloss.NewStyle().Foreground(accent).Background(selectedBG).Bold(true).Padding(0, 1).Width(30),
		pill:        lipgloss.NewStyle().Foreground(pillText).Background(accent).Bold(true).Padding(0, 1),
		help:        lipgloss.NewStyle().Foreground(muted),
		inputWidth:  30,
		secretWidth: 54,
	}
}
