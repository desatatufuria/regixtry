package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/cellbuf"
)

// overlayHorizontalMargin blanks a small buffer to the immediate left/right
// of the overlay's own footprint before its content is drawn
// (claude-handoff.md follow-up: a real terminal run showed the overlay's
// left/right border touching whatever base content happened to sit at that
// column -- e.g. a base table's own border character fused directly against
// the overlay's corner with zero gap, and base text hard-cut with nothing
// separating it from the overlay -- which reads as corruption rather than a
// floating dialog). This guarantees a visible horizontal gap survives
// between the overlay's border and any surviving base content, independent
// of how wide the base's own unshrunk content happens to be.
//
// There is deliberately no equivalent vertical margin: unlike width (fixed
// per terminal), the row directly above/below the overlay's footprint can
// legitimately hold meaningful base content right up to the overlay's own
// edge (e.g. the screen's help line, one row below a tall modal on a short
// base render) -- blanking it unconditionally risked destroying real
// content instead of empty canvas. The vertical gap the modal already gets
// in practice comes from adminScanHistoryModalRows leaving real margin
// below the floor most terminal heights (adminScanHistoryModalVerticalMargin),
// not from this compositor blanking rows it cannot tell are safe to clear.
const overlayHorizontalMargin = 2

// compositeOverlay draws overlay on top of base within a width x height
// canvas, centering overlay horizontally and vertically. It never appends
// overlay below base in the vertical flow (the superseded "Nested budget by
// row split, not overlay, not stacking" approach) -- it overwrites exactly
// overlay's own footprint on top of base's already-rendered content, leaving
// every other cell of base untouched (claude-handoff.md: "The underlying
// Features page must remain visible behind the modal and must not reflow
// when it opens").
//
// Vertical centering is anchored to base's own ACTUAL rendered height
// (lipgloss.Height(base), clamped to the canvas), not the raw canvas height.
// base is frequently shorter than height -- a content-driven screen body
// does not pad/stretch to fill a tall terminal (renderSection/fitLines only
// ever trim, never pad) -- so centering within the full canvas instead of
// base's own footprint would drift the overlay further down/away from base's
// visible content as height grows, independent of overlay's own size: for
// any base footprint of B rows, centering-in-full-canvas mathematically
// cannot keep even a 1-row overlay within B once height exceeds roughly 2B,
// no matter how small overlay is capped to be (a real regression found by
// real-render inspection at generously tall terminals, tracked alongside
// adminScanHistoryModalRows' own base-height-aware budget cap -- that cap
// bounds overlay's SIZE, this bounds its POSITION; both are required, this
// one categorically cannot be worked around by tightening the budget alone).
// The returned canvas is still always exactly width x height regardless.
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

	baseHeight := lipgloss.Height(base)
	if baseHeight > height {
		baseHeight = height
	}

	x := (width - overlayWidth) / 2
	if x < 0 {
		x = 0
	}
	y := (baseHeight - overlayHeight) / 2
	if y < 0 {
		y = 0
	}

	marginRect := cellbuf.Rect(
		x-overlayHorizontalMargin,
		y,
		overlayWidth+2*overlayHorizontalMargin,
		overlayHeight,
	).Intersect(buf.Bounds())
	cellbuf.ClearRect(buf, marginRect)

	cellbuf.SetContentRect(buf, overlay, cellbuf.Rect(x, y, overlayWidth, overlayHeight))

	// cellbuf.Render joins rows with "\r\n"; the rest of this package treats
	// "\n" as the row separator (fitLines, lipgloss.Height/Width, strings.Split).
	return strings.ReplaceAll(cellbuf.Render(buf), "\r\n", "\n")
}
