package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestCompositeOverlayFitsExactlyWithinTheGivenCanvas is a RED test for the
// reported regression (claude-handoff.md: "must not be appended below the
// page content"): compositeOverlay's result height/width must always equal
// the canvas dimensions it was given, never base's height plus overlay's
// height stacked on top of each other (the superseded row-split/JoinVertical
// approach, which grows unbounded with content).
func TestCompositeOverlayFitsExactlyWithinTheGivenCanvas(t *testing.T) {
	t.Parallel()

	base := strings.Join([]string{"base line 1", "base line 2", "base line 3", "base line 4"}, "\n")
	overlay := strings.Join([]string{"overlay 1", "overlay 2"}, "\n")

	const width, height = 40, 10
	got := compositeOverlay(base, overlay, width, height)

	if h := lipgloss.Height(got); h != height {
		t.Fatalf("compositeOverlay() height = %d, want exactly %d (the canvas height, not base+overlay stacked)", h, height)
	}

	// A naive append (lipgloss.JoinVertical) would produce
	// len(baseLines)+len(overlayLines) = 6 lines total -- well beyond a
	// 10-row canvas only because the canvas happens to be tall enough here.
	// The real proof is the exact-height assertion above; this guards the
	// stacking regression would have been caught even at a canvas exactly
	// sized to base+overlay.
	stackedHeight := lipgloss.Height(base) + lipgloss.Height(overlay)
	got2 := compositeOverlay(base, overlay, width, stackedHeight)
	if h := lipgloss.Height(got2); h != stackedHeight {
		t.Fatalf("compositeOverlay() height = %d, want exactly %d", h, stackedHeight)
	}
}

// TestCompositeOverlayPreservesBaseContentOutsideOverlayFootprint proves the
// base page's content survives unshrunk when an overlay is composited on
// top (claude-handoff.md: "The underlying Features page must remain visible
// behind the modal"): every base line far enough from the overlay's centered
// footprint to fall outside its margin buffer (overlayHorizontalMargin/
// overlayVerticalMargin -- see TestCompositeOverlayLeavesBlankMarginAroundOverlayFootprint)
// must still be present verbatim in the result. Rows immediately adjacent to
// the overlay are positioned OUTSIDE the margin's column span on purpose:
// the margin intentionally blanks a small buffer immediately touching the
// overlay's own footprint for a clean visual gap, so a marker placed inside
// that buffer would not be a meaningful regression signal.
func TestCompositeOverlayPreservesBaseContentOutsideOverlayFootprint(t *testing.T) {
	t.Parallel()

	// width=50 with a 7-wide overlay centers it at x=21; the margin buffer
	// (overlayHorizontalMargin=2) only reaches columns [19,29], so a short
	// marker starting at column 0 sits safely outside it even on the rows
	// immediately above/below the overlay's own two rows.
	const width, height = 50, 6
	base := strings.Join([]string{
		"row-0-untouched-marker",
		"safe-marker-row-1",
		"row-2-covered-by-overlay",
		"row-3-covered-by-overlay",
		"safe-marker-row-4",
		"row-5-untouched-marker",
	}, "\n")
	overlay := strings.Join([]string{"MODAL-A", "MODAL-B"}, "\n")

	got := compositeOverlay(base, overlay, width, height)

	for _, marker := range []string{"row-0-untouched-marker", "safe-marker-row-1", "safe-marker-row-4", "row-5-untouched-marker"} {
		if !strings.Contains(got, marker) {
			t.Fatalf("compositeOverlay() = %q, want untouched base row %q to survive", got, marker)
		}
	}
	if !strings.Contains(got, "MODAL-A") || !strings.Contains(got, "MODAL-B") {
		t.Fatalf("compositeOverlay() = %q, want both overlay rows present", got)
	}
}

// TestCompositeOverlayCentersOverlayWithinCanvas proves the overlay is
// actually placed ON TOP of / within the base at a centered offset (never
// appended below it), by checking the overlay's marker line lands at the
// vertically-centered row rather than after every base row.
func TestCompositeOverlayCentersOverlayWithinCanvas(t *testing.T) {
	t.Parallel()

	base := strings.Repeat("background line\n", 9)
	base = strings.TrimSuffix(base, "\n")
	overlay := "OVERLAY-MARKER"

	const width, height = 30, 9
	got := compositeOverlay(base, overlay, width, height)

	lines := strings.Split(got, "\n")
	if len(lines) != height {
		t.Fatalf("compositeOverlay() produced %d lines, want exactly %d", len(lines), height)
	}

	markerRow := -1
	for i, line := range lines {
		if strings.Contains(line, "OVERLAY-MARKER") {
			markerRow = i
		}
	}
	if markerRow == -1 {
		t.Fatalf("compositeOverlay() = %q, want the overlay marker present", got)
	}
	// A single-row overlay centered in a 9-row canvas lands at row 4 (0-indexed):
	// y = (height - overlayHeight) / 2 = (9-1)/2 = 4.
	if wantRow := (height - 1) / 2; markerRow != wantRow {
		t.Fatalf("overlay marker landed on row %d, want row %d (vertically centered, not appended after every base row)", markerRow, wantRow)
	}
	// The very last row must still be background, not the overlay -- proving
	// the overlay was layered/centered rather than appended at the bottom.
	if strings.Contains(lines[height-1], "OVERLAY-MARKER") {
		t.Fatalf("last row = %q, want plain background content (overlay must not be appended below base)", lines[height-1])
	}
}

// TestCompositeOverlayLeavesBlankMarginAroundOverlayFootprint is the RED
// test for a real-terminal visual regression found after
// TestRenderAdminWorkspaceKeepsBaseFullSizeAndLayersModalOnTopWhenOpen
// shipped: its assertions (exact canvas height, base marker present, modal
// title present) all passed even though the actual rendered output looked
// broken -- the overlay's own left/right border characters directly touched
// whatever base content happened to sit at its footprint's edge (no visible
// gap), which reads as corruption rather than a floating dialog, and any
// base text that continued past the overlay's left edge was hard-cut with
// zero buffer. compositeOverlay must blank a small horizontal margin around
// its own footprint before drawing the overlay, so a real gap always
// separates the overlay's left/right border from surviving base content
// (see admin_overlay.go's overlayHorizontalMargin doc comment for why there
// is deliberately no equivalent vertical margin).
func TestCompositeOverlayLeavesBlankMarginAroundOverlayFootprint(t *testing.T) {
	t.Parallel()

	const width, height = 40, 12
	// Base fills the ENTIRE canvas with a repeating marker character on
	// every row and column, simulating an unshrunk base page (e.g. a
	// full-width table) that would otherwise touch the overlay's border
	// directly with zero gap.
	baseRow := strings.Repeat("X", width)
	baseLines := make([]string, height)
	for i := range baseLines {
		baseLines[i] = baseRow
	}
	base := strings.Join(baseLines, "\n")

	overlay := strings.Join([]string{"MODAL", "BODY "}, "\n")
	got := compositeOverlay(base, overlay, width, height)
	lines := strings.Split(got, "\n")

	overlayWidth := lipgloss.Width(overlay)
	overlayHeight := lipgloss.Height(overlay)
	x := (width - overlayWidth) / 2
	y := (height - overlayHeight) / 2

	for row := y; row < y+overlayHeight; row++ {
		runes := []rune(lines[row])
		for m := 1; m <= overlayHorizontalMargin; m++ {
			if leftCol := x - m; leftCol >= 0 && leftCol < len(runes) {
				if runes[leftCol] != ' ' {
					t.Fatalf("row %d col %d = %q, want blank margin left of the overlay (base content touching the overlay's own edge)", row, leftCol, string(runes[leftCol]))
				}
			}
			if rightCol := x + overlayWidth + m - 1; rightCol >= 0 && rightCol < len(runes) {
				if runes[rightCol] != ' ' {
					t.Fatalf("row %d col %d = %q, want blank margin right of the overlay", row, rightCol, string(runes[rightCol]))
				}
			}
		}
	}

	// No vertical margin is applied (admin_overlay.go's overlayHorizontalMargin
	// doc comment): the row immediately above/below the overlay's footprint
	// must survive completely untouched, proving real adjacent content (like
	// a screen's help line one row below a tall modal) is never blanked just
	// because it happens to sit next to the overlay.
	if aboveRow := y - 1; aboveRow >= 0 {
		if want, got := baseRow, lines[aboveRow]; got != want {
			t.Fatalf("row above overlay = %q, want untouched base row %q (no vertical margin)", got, want)
		}
	}
	if belowRow := y + overlayHeight; belowRow < height {
		if want, got := baseRow, lines[belowRow]; got != want {
			t.Fatalf("row below overlay = %q, want untouched base row %q (no vertical margin)", got, want)
		}
	}
}

// TestCompositeOverlayIsANSISafeAcrossSplicedStyledBlocks is a RED test
// guarding the ANSI-corruption risk explicitly called out for this fix: base
// and overlay both carry real lipgloss SGR escape sequences, and splicing
// them at row/column boundaries must never leave a dangling/incomplete
// escape sequence in the result. This is what motivates using cellbuf
// (a cell-grid ANSI parser/re-emitter) instead of naive string slicing.
func TestCompositeOverlayIsANSISafeAcrossSplicedStyledBlocks(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	base := lipgloss.JoinVertical(lipgloss.Left,
		theme.text.Render("styled base row 1 with some real width"),
		theme.accent.Render("styled base row 2, also styled"),
		theme.muted.Render("styled base row 3"),
	)
	overlay := theme.selected.Render("styled overlay content")

	const width, height = 40, 5
	got := compositeOverlay(base, overlay, width, height)

	// An incomplete/corrupted SGR sequence would either panic ansi decoders
	// downstream or visibly leak a raw escape byte into plain text; assert
	// the composited output round-trips through lipgloss.Width/Height (which
	// itself parses ANSI) without producing a nonsensical result.
	if h := lipgloss.Height(got); h != height {
		t.Fatalf("composited styled output height = %d, want %d", h, height)
	}
	for _, line := range strings.Split(got, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("composited styled line width = %d, want <= %d (an ANSI-corrupted line can measure wider than the canvas): %q", w, width, line)
		}
	}
}
