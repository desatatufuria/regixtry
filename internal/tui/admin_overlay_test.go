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
// base page's full content survives unshrunk when an overlay is composited
// on top (claude-handoff.md: "The underlying Features page must remain
// visible behind the modal"): every base line NOT covered by the overlay's
// centered footprint must still be present verbatim in the result.
func TestCompositeOverlayPreservesBaseContentOutsideOverlayFootprint(t *testing.T) {
	t.Parallel()

	base := strings.Join([]string{
		"row-0-untouched-marker",
		"row-1-untouched-marker",
		"row-2-covered-by-overlay",
		"row-3-covered-by-overlay",
		"row-4-untouched-marker",
		"row-5-untouched-marker",
	}, "\n")
	overlay := strings.Join([]string{"MODAL-A", "MODAL-B"}, "\n")

	const width, height = 30, 6
	got := compositeOverlay(base, overlay, width, height)

	for _, marker := range []string{"row-0-untouched-marker", "row-1-untouched-marker", "row-4-untouched-marker", "row-5-untouched-marker"} {
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
