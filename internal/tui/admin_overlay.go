package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/cellbuf"
)

// compositeOverlay draws overlay on top of base within a width x height
// canvas, centering overlay horizontally and vertically. It never appends
// overlay below base in the vertical flow (the superseded "Nested budget by
// row split, not overlay, not stacking" approach) -- it overwrites exactly
// overlay's own footprint on top of base's already-rendered content, leaving
// every other cell of base untouched (claude-handoff.md: "The underlying
// Features page must remain visible behind the modal and must not reflow
// when it opens").
//
// Both base and overlay may contain ANSI escape sequences from lipgloss
// styling. Naive byte/rune slicing to splice one string's lines into
// another's would corrupt those escape sequences (an incomplete SGR
// sequence, a dangling reset). cellbuf parses ANSI into a cell grid and
// re-emits it, so every splice happens at cell boundaries, never mid-escape
// sequence.
func compositeOverlay(base, overlay string, width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	buf := cellbuf.NewBuffer(width, height)
	cellbuf.SetContentRect(buf, base, buf.Bounds())

	overlayWidth := lipgloss.Width(overlay)
	overlayHeight := lipgloss.Height(overlay)
	if overlayWidth > width {
		overlayWidth = width
	}
	if overlayHeight > height {
		overlayHeight = height
	}

	x := (width - overlayWidth) / 2
	if x < 0 {
		x = 0
	}
	y := (height - overlayHeight) / 2
	if y < 0 {
		y = 0
	}

	cellbuf.SetContentRect(buf, overlay, cellbuf.Rect(x, y, overlayWidth, overlayHeight))

	// cellbuf.Render joins rows with "\r\n"; the rest of this package treats
	// "\n" as the row separator (fitLines, lipgloss.Height/Width, strings.Split).
	return strings.ReplaceAll(cellbuf.Render(buf), "\r\n", "\n")
}
