package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// tooSmallSpec is everything the too-small notice shows: the terminal size to
// fill (also the "current" size it reports) and the required minimum.
type tooSmallSpec struct {
	Width, Height       int
	MinWidth, MinHeight int
}

// tooSmall reports whether the terminal is under the required minimum.
func (m Model) tooSmall() bool {
	return m.viewport.Width < minViewportWidth || m.viewport.Height < minViewportHeight
}

// renderTooSmall draws the centered notice shown instead of any screen while
// the terminal is under the minimum. As space shrinks it drops decoration
// first (header and footer bars, then border and padding), then text, down to
// a clipped single line. It never emits a line wider than spec.Width nor more
// rows than spec.Height.
func renderTooSmall(theme adminTheme, spec tooSmallSpec) string {
	if spec.Width < 1 || spec.Height < 1 {
		return ""
	}
	need := fmt.Sprintf("%dx%d", spec.MinWidth, spec.MinHeight)
	cur := fmt.Sprintf("%dx%d", spec.Width, spec.Height)

	title := theme.warning.Render("Terminal too small")
	detail := theme.text.Render("Regixtry needs at least ") + theme.text.Bold(true).Render(need) +
		theme.text.Render(". Current: ") + theme.error.Render(cur) + theme.text.Render(".")
	hint := theme.muted.Render("Resize, or press ") + theme.text.Render("q") + theme.muted.Render(" to quit.")
	text := []string{title, detail, hint}
	boxed := func(padY, padX int) []string {
		box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(paletteWarningHex)).
			Padding(padY, padX).Render(strings.Join(text, "\n"))
		return strings.Split(box, "\n")
	}

	if box := boxed(1, 3); fits(box, spec.Width, spec.Height-2) {
		bar := shellBarStyle()
		muted, bright := theme.muted.Inherit(bar), theme.text.Inherit(bar)
		right := muted.Render(cur + " ")
		left := muted.Render(" regixtry › ") + bright.Render("Resize terminal")
		header := shellFitBar(ansi.Truncate(left, max(spec.Width-lipgloss.Width(right), 0), ""), max(spec.Width-lipgloss.Width(right), 0)) + right
		footer := shellFitBar(bright.Render(" q")+muted.Render(" quit"), spec.Width)
		rows := append([]string{header}, centerRows(box, spec.Width, spec.Height-2)...)
		return strings.Join(append(rows, footer), "\n")
	}

	layouts := [][]string{
		boxed(1, 3),
		boxed(0, 1),
		text,
		{title, theme.text.Render("Need "+need+", have ") + theme.error.Render(cur)},
		{theme.error.Render(cur) + theme.muted.Render(" < ") + theme.text.Render(need)},
		{theme.error.Render(cur)},
	}
	pick := layouts[len(layouts)-1]
	for _, l := range layouts {
		if fits(l, spec.Width, spec.Height) {
			pick = l
			break
		}
	}
	// Clip what still does not fit (the last layout at tiny sizes).
	pick = pick[:min(len(pick), spec.Height)]
	clipped := make([]string, len(pick))
	for i, line := range pick {
		clipped[i] = ansi.Truncate(line, spec.Width, "")
	}
	return strings.Join(centerRows(clipped, spec.Width, spec.Height), "\n")
}

func fits(block []string, width, height int) bool {
	if width < 1 || height < 1 || len(block) > height {
		return false
	}
	for _, line := range block {
		if lipgloss.Width(line) > width {
			return false
		}
	}
	return true
}

// centerRows centers block inside width x rows, blank-padding the rows.
func centerRows(block []string, width, rows int) []string {
	out := make([]string, 0, rows)
	for range max((rows-len(block))/2, 0) {
		out = append(out, "")
	}
	for _, line := range block {
		out = append(out, strings.Repeat(" ", max((width-lipgloss.Width(line))/2, 0))+line)
	}
	for len(out) < rows {
		out = append(out, "")
	}
	return out
}

// updateTooSmallKey is the only key handling while the notice is up: Ctrl+C
// quits, q quits unless a text input has focus, nothing else is acted on.
func (m Model) updateTooSmallKey(msg keyMsg) (tea.Model, tea.Cmd) {
	switch keyString(msg) {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		if !m.migratedScreenCapturesTextInput() && !m.adminTextInputFocused() {
			return m, tea.Quit
		}
	}
	return m, nil
}
