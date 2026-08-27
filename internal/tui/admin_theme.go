package tui

import "github.com/charmbracelet/lipgloss"

// The Nord hex values bubble-table's v2-lipgloss styling needs
// (admin_tables_lipgloss_v2.go) are pulled from these named constants
// rather than duplicated as separate literals, so the two style
// definitions -- v1 for everything else in this package, v2 for the four
// bubble-table integration points that require it -- can never drift onto
// different colors for what is meant to be the exact same palette.
const (
	nordBorderHex         = "#4C566A"
	nordTextHex           = "#ECEFF4"
	nordMutedHex          = "#A7B1C2"
	nordSelectedFGHex     = "#2E3440"
	nordSelectedBGHex     = "#D8DEE9"
	nordErrorHex          = "#BF616A"
	nordWarningHex        = "#EBCB8B"
	nordSeverityMediumHex = "#C0A16B"
)

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
	border := lipgloss.Color(nordBorderHex)
	text := lipgloss.Color(nordTextHex)
	muted := lipgloss.Color(nordMutedHex)
	// accent moved off gold ("#D4AF37") onto Nord's own Snow Storm nord4
	// ("#D8DEE9"): the bright gold fill read poorly as a focus/selection
	// background against this theme's dark palette, and this cool light
	// gray already belongs to the same Nord family as border/muted/text
	// instead of introducing an unrelated hue.
	accent := lipgloss.Color(nordSelectedBGHex)
	selected := lipgloss.Color(nordSelectedFGHex)
	selectedBG := lipgloss.Color(nordSelectedBGHex)
	// fieldBG is a subtle Nord "panel" background (nord1) for unfocused
	// input/toggle fields: a filled box gives real visual delimitation for
	// the field without the row cost of a top/bottom border (design.md
	// Decision 5 removed that border specifically to compact forms; this
	// restores delimitation through fill, not border, so the row budget
	// that let the Trivy Config modal fit the 24-row floor stays intact).
	fieldBG := lipgloss.Color("#3B4252")
	success := lipgloss.Color("#A3BE8C")
	warning := lipgloss.Color(nordWarningHex)
	errorColor := lipgloss.Color(nordErrorHex)

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
		// ignored (design.md Decision 3). "#C0A16B" stays distinct from both
		// severityHigh ("#EBCB8B") and accent, and the two never share a role.
		severityMedium: lipgloss.NewStyle().Foreground(lipgloss.Color(nordSeverityMediumHex)),
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
		inputFocus:  lipgloss.NewStyle().Foreground(selected).Background(selectedBG).Bold(true).Padding(0, 1).Width(30),
		pill:        lipgloss.NewStyle().Foreground(selected).Background(accent).Bold(true).Padding(0, 1),
		help:        lipgloss.NewStyle().Foreground(muted),
		inputWidth:  30,
		secretWidth: 54,
	}
}
