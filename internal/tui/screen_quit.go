package tui

import "charm.land/bubbles/v2/key"

// quitKey is the footer hint of a screen where a bare q quits: it is shown
// exactly while no text input, form, confirm, modal or editor holds the keys.
func quitKey() key.Binding {
	return key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))
}
