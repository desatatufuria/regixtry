package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"regixtry/internal/ports"
)

// screenKeys's struct declaration lives in screen.go (adminScreen's Keys()
// method needs the type before this file's wiring exists — see screen.go's
// comment). This file adds its behavior: ShortHelp/FullHelp (the key.Binding
// accessors), matches (the ONLY key-matching path a migrated screen may use),
// and shortHelpView (the generated-footer renderer).

func (k screenKeys) ShortHelp() []key.Binding  { return k.short }
func (k screenKeys) FullHelp() [][]key.Binding { return k.full }

// shortHelpView is the ONLY footer source a migrated screen's View() may
// use (design.md Decision A: screenFrame has no Help field). It renders each
// enabled binding as "<key>: <desc>" joined by " | ", unstyled — the
// surrounding theme.muted/renderConsoleWorkspace keeps doing the actual
// styling. The colon is added here, centrally, rather than baked into every
// screen's own key.Map, so "Enter: save" is produced without any screen
// needing to know the formatting trick.
func shortHelpView(theme adminTheme, keys screenKeys) string {
	parts := make([]string, 0, len(keys.short))
	for _, b := range keys.short {
		if !b.Enabled() {
			continue
		}
		parts = append(parts, b.Help().Key+": "+b.Help().Desc)
	}
	return strings.Join(parts, " | ")
}

// featureActionKeyBindings returns dynamic key.Binding values for whichever
// of enable/disable/install-runtime/upgrade-runtime/rollback-runtime
// page.Actions declares -- mirrors the retired featureActionHelp's switch
// (model.go) as bindings instead of a formatted string, so a migrated
// screen's Keys() composes them with shortHelpView and help can never drift
// from what a key actually does (design.md Decision A). Shared by
// trivyConfigScreen/gitleaksConfigScreen/signingConfigScreen (the Phase 11
// resolved-gap addendum: each independently owns this dispatch, but the
// pure mapping itself is the same for all three).
func featureActionKeyBindings(page ports.FeaturePage) []key.Binding {
	var bindings []key.Binding
	for _, action := range page.Actions {
		switch action.ID {
		case "enable":
			bindings = append(bindings, key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "enable")))
		case "disable":
			bindings = append(bindings, key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "disable")))
		case "install-runtime":
			bindings = append(bindings, key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "install runtime")))
		case "upgrade-runtime":
			bindings = append(bindings, key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "upgrade runtime")))
		case "rollback-runtime":
			bindings = append(bindings, key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "rollback runtime")))
		}
	}
	return bindings
}
