package tui

import (
	lipglossv2 "charm.land/lipgloss/v2"
)

// bubble-table v0.22.3 migrated its own styling API from
// github.com/charmbracelet/lipgloss (v1, used by adminTheme and every other
// styled element in this package) onto charm.land/lipgloss/v2 -- a
// genuinely different Go package, not just a version bump, so a v1
// lipgloss.Style cannot be passed where bubble-table now expects a v2
// lipgloss.Style. Confirmed narrow in scope before this file was written:
// this codebase never calls a bubbletable.Model's own Update method (grep
// across internal/tui/*.go), only WithRows/WithHighlightedRow-style
// builders and View -- bubble-table's parallel dependency on
// charm.land/bubbletea/v2 is therefore never exercised here, and nothing
// outside these four styling call sites needs to change.
//
// These four functions are the ONLY v2-lipgloss styles this package
// builds, each sourced from admin_theme.go's own named Nord hex constants
// (nordBorderHex etc.) so they can never drift onto a different color than
// their v1 counterparts describe the exact same palette in.

func bubbleTableBaseStyleV2() lipglossv2.Style {
	return lipglossv2.NewStyle().Align(lipglossv2.Left).BorderForeground(lipglossv2.Color(nordBorderHex))
}

func bubbleTableHeaderStyleV2() lipglossv2.Style {
	return lipglossv2.NewStyle().Foreground(lipglossv2.Color(nordTextHex)).Bold(true)
}

// bubbleTableHighlightStyleV2 deliberately carries NO Padding, unlike
// adminTheme.selected (v1)'s own Padding(0, 1): bubble-table only reduces
// its wrap-width calculation by a COLUMN's own style padding
// (evertras/bubble-table@v0.22.3 row.go's "Reduce the available text width
// by any horizontal column padding" comment), never by the separate
// row-level HighlightStyle's padding -- so padding added here is invisible
// to bubble-table's own width math but still consumes real character
// columns once rendered, silently wrapping a cell's content that would
// otherwise fit exactly (found live: a 28-char repository name wrapped
// mid-word once this highlight style carried the same Padding(0, 1) as
// adminTheme.selected, in a 30-wide column). The highlight is a
// foreground/background recolor of an existing cell, not a
// pill/badge-shaped UI element the way adminTheme.selected's other,
// non-table callers use it -- it does not need its own padding.
func bubbleTableHighlightStyleV2() lipglossv2.Style {
	return lipglossv2.NewStyle().
		Foreground(lipglossv2.Color(nordSelectedFGHex)).
		Background(lipglossv2.Color(nordSelectedBGHex)).
		Bold(true)
}

// severityStyleV2 maps an uppercased severity value onto its v2 cell
// style, mirroring adminTheme's own severityCritical/severityHigh/
// severityMedium/severityLow/text styles (v1) color-for-color -- the exact
// same case set and fallback as the old severityStyledCell(theme, ...)
// switch, before this function replaced it.
func severityStyleV2(value string) lipglossv2.Style {
	switch value {
	case "CRITICAL":
		return lipglossv2.NewStyle().Foreground(lipglossv2.Color(nordErrorHex)).Bold(true)
	case "HIGH":
		return lipglossv2.NewStyle().Foreground(lipglossv2.Color(nordWarningHex)).Bold(true)
	case "MEDIUM":
		return lipglossv2.NewStyle().Foreground(lipglossv2.Color(nordSeverityMediumHex))
	case "LOW":
		return lipglossv2.NewStyle().Foreground(lipglossv2.Color(nordMutedHex))
	default:
		return lipglossv2.NewStyle().Foreground(lipglossv2.Color(nordTextHex))
	}
}
