package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

// The operator login screen (tui-v2-redesign, the approved "Operator login"
// board): no sidebar, a header bar, one centered panel and a footer of key
// hints. The same chrome frames the "signing in" wait.
const (
	// loginPanelWidth is the board's 460px panel at about 8.5px a column.
	loginPanelWidth = 72
	// loginPanelChrome is the panel's border and horizontal padding: 2 + 2*2.
	loginPanelChrome = 6
)

var loginKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "sign in")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}}

var authenticatingKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
}}

// loginView is what the login screen shows besides the form.
type loginView struct {
	form adminLoginForm
	// note is the session-expired note: wrapped in an amber box at the top of
	// the panel. status is any other message (a failed sign-in, a missing
	// field), shown in the error color.
	note   string
	status string
}

// renderAdminLogin renders the login screen as exactly height rows by width
// columns.
func renderAdminLogin(theme adminTheme, view loginView, width, height int) string {
	inner := loginPanelWidth - loginPanelChrome
	build := func(spacers bool) string {
		gap := func() []string {
			if spacers {
				return []string{""}
			}
			return nil
		}
		var lines []string
		lines = append(lines, theme.subheading.Render("regixtry")+theme.muted.Render(" operator sign in"))
		if view.note != "" {
			lines = append(lines, gap()...)
			lines = append(lines, loginNoteBox(theme, view.note, inner))
		} else if view.status != "" {
			lines = append(lines, gap()...)
			lines = append(lines, theme.error.Render(strings.Join(loginCapLines(view.status, inner), "\n")))
		}
		lines = append(lines, gap()...)
		lines = append(lines, renderLoginField(theme, "Username", maskedOrPlain(view.form.Username, false), inner, view.form.Focus == loginFieldUsername))
		lines = append(lines, renderLoginField(theme, "Password", maskedOrPlain(view.form.Password, true), inner, view.form.Focus == loginFieldPassword))
		lines = append(lines, gap()...)
		lines = append(lines, loginButton(theme, "Sign in", "enter", inner, true), loginButton(theme, "Back", "esc", inner, false))
		return loginPanelStyle(theme).Render(strings.Join(lines, "\n"))
	}

	rows := max(height-shellChromeRows, 1)
	panel := build(true)
	if lipgloss.Height(panel) > rows {
		panel = build(false)
	}
	return renderLoginChrome(theme, "Sign in", "not signed in", panel, loginKeys, width, height)
}

// renderAdminAuthenticating renders the wait while the sign-in request runs.
func renderAdminAuthenticating(theme adminTheme, text string, width, height int) string {
	inner := loginPanelWidth - loginPanelChrome
	panel := loginPanelStyle(theme).Render(strings.Join([]string{
		theme.subheading.Render("regixtry") + theme.muted.Render(" operator sign in"),
		"",
		theme.muted.Render(strings.Join(grantWrap(text, inner), "\n")),
	}, "\n"))
	return renderLoginChrome(theme, "Sign in", "not signed in", panel, authenticatingKeys, width, height)
}

// renderLoginChrome frames panel with the board's header and footer bars,
// centered in the area between them, as exactly height rows by width columns.
func renderLoginChrome(theme adminTheme, crumb, right, panel string, keys screenKeys, width, height int) string {
	width, height = max(width, 1), max(height, shellChromeRows+1)
	rows := height - shellChromeRows
	bar := shellBarStyle()
	muted := theme.muted.Inherit(bar)
	text := theme.text.Inherit(bar)

	left := bar.Render("  ") + muted.Render("regixtry"+" › ") + text.Render(crumb)
	tail := muted.Render(right) + bar.Render("  ")
	header := shellFitBar(left, width)
	if gap := width - lipgloss.Width(left) - lipgloss.Width(tail); gap >= 1 {
		header = left + bar.Render(strings.Repeat(" ", gap)) + tail
	}

	rule := lipgloss.NewStyle().Foreground(theme.borderColor).Render(strings.Repeat("─", width))
	body := lipgloss.Place(width, rows, lipgloss.Center, lipgloss.Center, panel)
	body = lipgloss.NewStyle().MaxWidth(width).MaxHeight(rows).Render(body)

	return strings.Join([]string{
		header,
		rule,
		body,
		rule,
		shellFooter(theme, shellSpec{Hints: keys.short}, width),
	}, "\n")
}

func loginPanelStyle(theme adminTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.borderColor).
		Padding(0, 2).
		Width(loginPanelWidth)
}

// loginNoteBox is the amber session-expired note, wrapped to width so its
// tail ("Not applied ... Sign in ...") is never cut off.
func loginNoteBox(theme adminTheme, note string, width int) string {
	warn := theme.warning.GetForeground()
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(warn).
		Foreground(warn).
		Padding(0, 1).
		Render(strings.Join(loginCapLines(note, max(width-4, 1)), "\n"))
}

// maskedOrPlain is what a field shows: the password becomes one "*" per rune.
func maskedOrPlain(value string, secret bool) string {
	if secret {
		return strings.Repeat("*", len([]rune(value)))
	}
	return value
}

// renderLoginField renders a label above a full-width input; a value longer than
// the input shows its tail, where the typing happens.
func renderLoginField(theme adminTheme, label, shown string, width int, focused bool) string {
	style := theme.input
	if focused {
		style = theme.inputFocus
	}
	if room := width - 2; len([]rune(shown)) > room {
		shown = string([]rune(shown)[len([]rune(shown))-room:])
	}
	return theme.muted.Render(label) + "\n" + style.Width(width).Render(shown)
}

// loginButton renders a full-width button: its label on the left, its key on
// the right. The primary one is the pill (page color on accent).
func loginButton(theme adminTheme, label, keycap string, width int, primary bool) string {
	style := lipgloss.NewStyle().Width(width).Padding(0, 1)
	gap := max(width-2-lipgloss.Width(label)-lipgloss.Width(keycap), 1)
	line := label + strings.Repeat(" ", gap) + keycap
	if primary {
		return style.Foreground(theme.pill.GetForeground()).Background(theme.pill.GetBackground()).Bold(true).Render(line)
	}
	return style.Foreground(theme.text.GetForeground()).Render(label + strings.Repeat(" ", gap) + theme.muted.Render(keycap))
}

// loginMaxMessageLines caps the message block so the fields, buttons and
// footer stay visible however long the message is.
const loginMaxMessageLines = 4

// loginCapLines wraps text to width and keeps at most loginMaxMessageLines
// lines; when text is cut, the last line ends in an ellipsis (cut on a rune
// boundary, so a multibyte rune is never split).
func loginCapLines(text string, width int) []string {
	lines := grantWrap(text, width)
	if len(lines) <= loginMaxMessageLines {
		return lines
	}
	lines = lines[:loginMaxMessageLines]
	lines[loginMaxMessageLines-1] = grantTruncate(lines[loginMaxMessageLines-1]+"…", width)
	return lines
}
