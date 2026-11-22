package tui

import (
	"fmt"

	"charm.land/lipgloss/v2"
	bubbletable "github.com/evertras/bubble-table/table"
	"regixtry/internal/ports"
)

// newAdminBubbleTable is the sole construction point for every admin table.
// pageSize is the number of data rows shown per page (design.md decision #1:
// we derive this ourselves from the measured terminal budget since
// WithTargetHeight does not exist at the pinned bubble-table version).
// Rendered table height is deterministically pageSize+tableChromeRows once a
// page is filled (viewport.go's tableChromeRows, verified against
// evertras/bubble-table@v0.19.2).
func newAdminBubbleTable(columns []bubbletable.Column, rows []bubbletable.Row, highlighted int, pageSize int) bubbletable.Model {
	keys := bubbletable.DefaultKeyMap()
	keys.RowSelectToggle.SetKeys("ctrl+space")
	keys.FilterBlur.SetKeys("ctrl+g")
	keys.FilterClear.SetKeys("ctrl+shift+g")
	keys.PageDown.SetKeys("pgdown")
	keys.PageUp.SetKeys("pgup")
	keys.PageFirst.SetKeys("home")
	keys.PageLast.SetKeys("end")

	model := bubbletable.New(columns).
		WithRows(rows).
		WithKeyMap(keys).
		WithBaseStyle(bubbleTableBaseStyle()).
		HeaderStyle(bubbleTableHeaderStyle()).
		HighlightStyle(bubbleTableHighlightStyle()).
		Focused(true).
		BorderRounded().
		WithPageSize(pageSize).
		WithFooterVisibility(true)

	if len(rows) > 0 {
		model = model.WithHighlightedRow(boundedIndex(highlighted, len(rows)))
	}

	return model
}

func secretFindingLocation(finding ports.SecretFinding) string {
	location := adminFirstNonEmpty(finding.Path, finding.BlobDigest)
	if location == "" {
		return "unknown"
	}
	if finding.StartLine <= 0 {
		return location
	}
	if finding.EndLine > finding.StartLine {
		return fmt.Sprintf("%s:%d-%d", location, finding.StartLine, finding.EndLine)
	}
	return fmt.Sprintf("%s:%d", location, finding.StartLine)
}

// tableRoles splits a screen's row budget into two admin-table pageSize
// roles (design.md decision #6): primary tables (Features, ScanSummary — the
// operator-navigable top-level lists) get the whole budget available to a
// standalone table so a single-table screen fits exactly without relying on
// the outer clip; compact tables (FeatureRows — always paired with a
// primary selection) are capped at compactTableRows, floored at
// minTableRows. When several tables stack on one screen, their combined
// height can still exceed l.SectionRows — renderSection's outer-pane clip
// (Phase 3) is the structural guarantee against overflow there, not this
// split; this split is ergonomics only.
func tableRoles(l consoleLayout) (primary, compact int) {
	available := l.SectionRows - tableChromeRows
	if available < minTableRows {
		available = minTableRows
	}
	primary = available
	compact = compactTableRows
	if compact > available {
		compact = available
	}
	return primary, compact
}

// rebuildAdminTables keeps AdminViewState.Layout's Primary/Compact split
// fresh (design.md decision #6) -- a handful of tests read it as a proxy
// for "the row budget this screen was sized against". Every admin table
// itself is built by its own owning screen (trivyConfigScreen/
// trivyReposScreen's own fields; scanHistoryScreen's own
// findings/secretFindings fields, design.md's State Migration table) from inside its own Update, broadcast
// the same way every other migrated screen refreshes on tea.WindowSizeMsg
// (design.md Decision H). Still called unconditionally after almost every
// admin action -- harmless/cheap, this method itself now does no table
// construction of its own.
func (m *Model) rebuildAdminTables(layout consoleLayout) {
	layout.Primary, layout.Compact = tableRoles(layout)
	m.adminView.Layout = layout
}

// Table styles are built from admin_theme.go's palette role constants so they can
// never drift from the theme palette.

func bubbleTableBaseStyle() lipgloss.Style {
	return lipgloss.NewStyle().Align(lipgloss.Left).BorderForeground(lipgloss.Color(paletteBorderHex))
}

func bubbleTableHeaderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(paletteTextHex)).Bold(true)
}

// bubbleTableHighlightStyle deliberately carries NO Padding, unlike
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
func bubbleTableHighlightStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(paletteAccentHex)).
		Background(lipgloss.Color(paletteSelectedBGHex)).
		Bold(true)
}

// severityStyle maps an uppercased severity value onto its v2 cell
// style, mirroring adminTheme's own severityCritical/severityHigh/
// severityMedium/severityLow/text styles (v1) color-for-color -- the exact
// same case set and fallback as the old severityStyledCell(theme, ...)
// switch, before this function replaced it.
func severityStyle(value string) lipgloss.Style {
	switch value {
	case "CRITICAL":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(paletteErrorHex)).Bold(true)
	case "HIGH":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(paletteWarningHex)).Bold(true)
	case "MEDIUM":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(paletteAccentHex))
	case "LOW":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(paletteMutedHex))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(paletteTextHex))
	}
}
