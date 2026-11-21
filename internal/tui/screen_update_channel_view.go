package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"regixtry/internal/infra/release"
)

const updateChannelSubtitle = "which released tags the update check considers"

// updateChannelOption is one selectable channel of the board's CHANNEL panel.
type updateChannelOption struct {
	name string
	desc string
}

var updateChannelOptions = []updateChannelOption{
	{string(release.ChannelStable), "Default channel."},
	{string(release.ChannelInsider), "Alternative channel for the update check."},
}

func (s updateChannelScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, _ := shellContentSize(width, height)

	title := "Update channel"
	subtitle := fitText(updateChannelSubtitle, max(bodyW-lipgloss.Width(title)-2, 0))
	gap := max(bodyW-lipgloss.Width(title)-lipgloss.Width(subtitle), 1)
	lines := []string{
		theme.subheading.Render(title) + strings.Repeat(" ", gap) + theme.muted.Render(subtitle),
		"",
	}
	lines = append(lines, userPanel(theme, bodyW, 0, s.panelLines(theme, max(bodyW-4, 1))))

	return screenFrame{
		Context: "Update channel",
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: shellCrumb("Operations", "Update channel"),
			Operator:   env.Session.Username,
			Active:     shellNavUpdateChannel,
			Body:       strings.Join(lines, "\n"),
			Hints:      updateChannelKeys.short,
			Width:      width,
			Height:     height,
		}),
	}
}

func (s updateChannelScreen) panelLines(theme adminTheme, inner int) []string {
	lines := []string{userSectionTitle(theme, "CHANNEL")}
	if !s.loaded {
		if s.err != "" {
			return append(lines, theme.error.Render(s.err), theme.muted.Render("Press r to try again."))
		}
		return append(lines, theme.muted.Render("Loading update channel..."))
	}
	for _, option := range updateChannelOptions {
		lines = append(lines, s.optionLines(theme, option, inner)...)
	}
	return append(lines, s.statusLine(theme))
}

// optionLines draws one channel: its name with its state on the right and
// its description below. The selected channel carries the theme's selected
// style and the "▸" marker.
func (s updateChannelScreen) optionLines(theme adminTheme, option updateChannelOption, inner int) []string {
	selected := option.name == s.selected
	state := ""
	switch {
	case selected && s.saving:
		state = "saving"
	case selected && option.name != s.current:
		state = "selected, unsaved"
	case option.name == s.current:
		state = "current"
	}
	name := "  " + option.name
	if selected {
		name = "▸ " + option.name
	}
	// The selected style pads one column on each side, so its text gets two
	// fewer columns than the panel offers.
	textW := max(inner-2, 1)
	head := spreadText(name, state, textW)
	desc := "  " + option.desc
	if selected {
		return []string{
			theme.selected.Width(inner).Render(head),
			theme.muted.Render(fitText(desc, inner)),
		}
	}
	return []string{theme.text.Render(fitText(head, inner)), theme.muted.Render(fitText(desc, inner))}
}

func (s updateChannelScreen) statusLine(theme adminTheme) string {
	switch {
	case s.err != "":
		return theme.error.Render(s.err)
	case s.saving:
		return theme.muted.Render("Saving...")
	case s.selected != s.current:
		return theme.warning.Render("Selected: " + s.selected + " (unsaved, press enter to save)")
	case s.saved:
		return theme.success.Render("Saved: the update check now uses " + s.current)
	}
	return theme.muted.Render("Unchanged: " + s.current + " (press space to pick the other channel)")
}
