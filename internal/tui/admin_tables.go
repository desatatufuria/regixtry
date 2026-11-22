package tui

import (
	"fmt"

	"charm.land/lipgloss/v2"
	"regixtry/internal/ports"
)

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

// Table styles are built from admin_theme.go's palette role constants so they can
// never drift from the theme palette.

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
