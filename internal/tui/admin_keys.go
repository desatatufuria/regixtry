package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// noOpStyle is lipgloss.Style's zero value: Render(s) returns s unchanged.
// shortHelpView uses it for every help.Styles field so bubbles/help does no
// styling of its own — the caller's theme.muted (or equivalent) wraps the
// whole generated string instead, matching the pre-change hand-written
// footer's own single-style treatment.
var noOpStyle = lipgloss.NewStyle()

// screenKeys's struct declaration lives in screen.go (adminScreen's Keys()
// method needs the type before this file's bubbles/help wiring exists — see
// screen.go's comment). This file adds its behavior: ShortHelp/FullHelp
// (satisfying help.KeyMap), matches (the ONLY key-matching path a migrated
// screen may use), and shortHelpView (the generated-footer renderer).

func (k screenKeys) ShortHelp() []key.Binding  { return k.short }
func (k screenKeys) FullHelp() [][]key.Binding { return k.full }

// matches reports whether msg matches the binding in k.short whose display
// label (Help().Key) equals id. Using the SAME label for both display and
// lookup is deliberate: a screen's Update can only dispatch a key that is
// also in its rendered help, and a key present in help cannot silently stop
// being handled — the drift class this change exists to kill.
func (k screenKeys) matches(msg tea.KeyMsg, id string) bool {
	for _, b := range k.short {
		if b.Help().Key == id && key.Matches(msg, b) {
			return true
		}
	}
	return false
}

// shortHelpView is the ONLY footer source a migrated screen's View() may
// use (design.md Decision A: screenFrame has no Help field). It uses
// bubbles/help for rendering (design.md Decision D), with no-op styles —
// the surrounding theme.muted/renderConsoleWorkspace keeps doing the actual
// styling — and ShortSeparator=" | " to match the pre-change hand-written
// format exactly (T1.3). bubbles/help renders "<key> <desc>" with no colon;
// the colon is appended to each binding's displayed key here, centrally,
// rather than baked into every screen's own key.Map, so "Enter: save" is
// produced without any screen needing to know the formatting trick.
func shortHelpView(theme adminTheme, keys screenKeys) string {
	h := help.New()
	h.ShortSeparator = " | "
	h.Styles.ShortKey = noOpStyle
	h.Styles.ShortDesc = noOpStyle
	h.Styles.ShortSeparator = noOpStyle
	h.Styles.Ellipsis = noOpStyle

	bindings := make([]key.Binding, 0, len(keys.short))
	for _, b := range keys.short {
		if !b.Enabled() {
			continue
		}
		bindings = append(bindings, key.NewBinding(
			key.WithKeys(b.Keys()...),
			key.WithHelp(b.Help().Key+":", b.Help().Desc),
		))
	}
	return h.ShortHelpView(bindings)
}

// gitleaksConfigKeys is the Slice 1 proof screen's key.Map (design.md
// Interfaces section), matching admin_views.go:789's 4 pre-change
// hand-written bindings exactly.
var gitleaksConfigKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "save")),
	key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "next field")),
	key.NewBinding(key.WithKeys(" "), key.WithHelp("Space", "toggle")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "cancel")),
}}

// signingConfigKeys is signingConfigScreen's key.Map (Phase 12.3), matching
// admin_views.go's pre-move hand-written signing policy modal footer
// exactly: "Enter: save/add key | Tab: next field | Space: toggle/cycle |
// Esc: cancel".
var signingConfigKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "save/add key")),
	key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "next field")),
	key.NewBinding(key.WithKeys(" "), key.WithHelp("Space", "toggle/cycle")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "cancel")),
}}
