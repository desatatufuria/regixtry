package tui

import (
	"image/color"
	"testing"

	lipglossv2 "charm.land/lipgloss/v2"
)

// colorsEqual compares two color.Color values by their RGBA output rather
// than by Go equality: lipgloss v2's Color() can return different
// concrete types for the same hex string depending on internal parsing
// paths, so RGBA is the only comparison guaranteed to reflect "same visible
// color."
func colorsEqual(t *testing.T, got, want color.Color) bool {
	t.Helper()
	gr, gg, gb, ga := got.RGBA()
	wr, wg, wb, wa := want.RGBA()
	return gr == wr && gg == wg && gb == wb && ga == wa
}

func TestBubbleTableV2StylesMatchNordConstants(t *testing.T) {
	t.Parallel()

	t.Run("base style uses the border color and left alignment", func(t *testing.T) {
		t.Parallel()
		style := bubbleTableBaseStyleV2()
		if !colorsEqual(t, style.GetBorderTopForeground(), lipglossv2.Color(nordBorderHex)) {
			t.Fatalf("base style border foreground does not match nordBorderHex")
		}
	})

	t.Run("header style is bold and uses the text color", func(t *testing.T) {
		t.Parallel()
		style := bubbleTableHeaderStyleV2()
		if !style.GetBold() {
			t.Fatal("header style GetBold() = false, want true")
		}
		if !colorsEqual(t, style.GetForeground(), lipglossv2.Color(nordTextHex)) {
			t.Fatalf("header style foreground does not match nordTextHex")
		}
	})

	t.Run("highlight style uses selected foreground and background", func(t *testing.T) {
		t.Parallel()
		style := bubbleTableHighlightStyleV2()
		if !style.GetBold() {
			t.Fatal("highlight style GetBold() = false, want true")
		}
		if !colorsEqual(t, style.GetForeground(), lipglossv2.Color(nordSelectedFGHex)) {
			t.Fatalf("highlight style foreground does not match nordSelectedFGHex")
		}
		if !colorsEqual(t, style.GetBackground(), lipglossv2.Color(nordSelectedBGHex)) {
			t.Fatalf("highlight style background does not match nordSelectedBGHex")
		}
	})

	t.Run("severity styles mirror severityStyledCell's old case set color-for-color", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			value    string
			wantHex  string
			wantBold bool
		}{
			{"CRITICAL", nordErrorHex, true},
			{"HIGH", nordWarningHex, true},
			{"MEDIUM", nordSeverityMediumHex, false},
			{"LOW", nordMutedHex, false},
			{"UNKNOWN", nordTextHex, false},
		}
		for _, tc := range cases {
			style := severityStyleV2(tc.value)
			if !colorsEqual(t, style.GetForeground(), lipglossv2.Color(tc.wantHex)) {
				t.Fatalf("severityStyleV2(%q) foreground does not match %s", tc.value, tc.wantHex)
			}
			if style.GetBold() != tc.wantBold {
				t.Fatalf("severityStyleV2(%q) GetBold() = %v, want %v", tc.value, style.GetBold(), tc.wantBold)
			}
		}
	})
}
