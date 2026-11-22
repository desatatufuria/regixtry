package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// A one-time secret can be copied two ways, because a terminal selection that
// crosses rows also copies the sidebar and the borders drawn on those rows:
// c puts the whole secret on the clipboard (an OSC 52 command of Bubble Tea,
// never an escape sequence written into a frame), and v shows a bare screen
// where the secret sits alone on its own row for a triple-click.

const (
	// copiedNotice is the one-line confirmation. Whether the terminal honored
	// the request cannot be known, so it says what to do when nothing pastes.
	copiedNotice = "Copied. If nothing pastes, your terminal may not support OSC 52: press v to select it by hand."
	// revealHint is the one plain row at the bottom of the bare view.
	revealHint = "triple-click the secret row to select it, or press c to copy · v back · esc done"
	// revealSplitNote is said when the terminal is narrower than the secret.
	revealSplitNote = "Split over rows: c copies it whole."
	// secretCardMinList is the narrowest the token list may get when the
	// panel is widened so the secret fits on one row.
	secretCardMinList = 28
)

// copySecretCmd asks the terminal to put secret on the clipboard. The closure
// holds the secret only until the runtime executes the command; nothing about
// it is stored in the model.
func copySecretCmd(secret string) tea.Cmd {
	if secret == "" {
		return nil
	}
	return tea.SetClipboard(secret)
}

// revealRows splits secret into rows of exactly width cells (the last may be
// shorter), with no padding and no prefix: one row when it fits.
func revealRows(secret string, width int) []string { return hardWrap(secret, width) }

// revealSpec is everything the bare reveal view draws.
type revealSpec struct {
	Title  string
	Secret string
	// Facts are one row each, below the secret, never beside it.
	Facts         []string
	Copied        bool
	Width, Height int
}

// renderReveal draws the bare view: a title row, the secret alone from column
// 0, the facts on their own rows below it and one hint row at the bottom:
// exactly Height rows, none wider than Width.
func renderReveal(theme adminTheme, spec revealSpec) string {
	width, height := max(spec.Width, 1), max(spec.Height, 3)
	rows := revealRows(spec.Secret, width)

	body := []string{theme.subheading.Render(grantTruncate(spec.Title, width)), ""}
	for _, row := range rows {
		body = append(body, theme.text.Bold(true).Render(row))
	}
	if len(rows) > 1 {
		body = append(body, theme.muted.Render(grantTruncate(revealSplitNote, width)))
	}
	body = append(body, "")
	for _, fact := range spec.Facts {
		body = append(body, theme.muted.Render(grantTruncate(fact, width)))
	}
	if spec.Copied {
		body = append(body, "", theme.success.Render(grantTruncate(copiedNotice, width)))
	}
	if len(body) > height-1 {
		kept := body[:0:0]
		for _, line := range body {
			if line != "" {
				kept = append(kept, line)
			}
		}
		body = kept
	}
	body = clipRows(body, height-1)
	for len(body) < height-1 {
		body = append(body, "")
	}
	hint := padRight(theme.muted.Render(grantTruncate(revealHint, width)), width)
	return strings.Join(append(body, hint), "\n")
}

// secretFooterKeys are the footer hints of a secret screen.
var secretFooterKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy")),
	key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "show alone")),
	key.NewBinding(key.WithKeys("enter", "esc"), key.WithHelp("enter", "done")),
}}
