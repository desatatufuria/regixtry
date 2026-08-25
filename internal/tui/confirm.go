package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// confirmPrompt is THE confirm-before-destructive-action primitive
// (design.md Decision E, D7). The payload adminConfirmModal carried as
// sibling fields (UserID/Username/FeatureName/Repository/Accessor) is
// captured by onConfirm's closure instead, so adding a confirm never widens
// a shared struct. It is a plain composable value: a migrated sub-model and
// a legacy model (TagsModel) embed it identically.
type confirmPrompt struct {
	title       string
	message     string
	confirmText string
	// submitting is the status text shown the instant Enter dispatches
	// onConfirm's command (e.g. "Submitting enable for alice..."), captured
	// at open time. This is a deliberate, documented extension beyond
	// design.md's minimal newConfirmPrompt(title, message, confirmText,
	// onConfirm) signature: retiring adminConfirmModal's 10 Kind-specific
	// status strings requires SOMEWHERE to hold each one, and confirmPrompt
	// is the only composable value that travels with the right one to each
	// call site — without it, T1.1's exact-status-text characterization
	// could not survive the retirement.
	submitting string
	// onConfirm receives env at CONFIRM time, never at OPEN time: capturing
	// AdminSession in the closure would fire a command with a token that
	// expired or was replaced while the prompt was on screen.
	onConfirm func(env screenEnv) tea.Cmd
}

// newConfirmPrompt constructs an active confirmPrompt. See the submitting
// field's doc comment for why this takes one more parameter than design.md's
// literal signature.
func newConfirmPrompt(title, message, confirmText, submitting string, onConfirm func(screenEnv) tea.Cmd) confirmPrompt {
	return confirmPrompt{title: title, message: message, confirmText: confirmText, submitting: submitting, onConfirm: onConfirm}
}

// Active reports whether a confirm is currently open.
func (c confirmPrompt) Active() bool {
	return c.onConfirm != nil
}

// update mirrors updateAdminConfirmKey's exact behavior: Esc clears the
// prompt and dispatches nothing; Enter dispatches onConfirm(env) and leaves
// the prompt open (the real close happens once the async result msg
// arrives, exactly like adminConfirmModal today); every other key is
// swallowed as a no-op. consumed is true for every key while active,
// reproducing updateAdminConfirmKey's existing swallow-all behavior.
func (c confirmPrompt) update(env screenEnv, msg tea.KeyMsg) (confirmPrompt, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return confirmPrompt{}, nil, true
	case isEnterKey(msg):
		if c.onConfirm == nil {
			return c, nil, true
		}
		return c, c.onConfirm(env), true
	}
	return c, nil, true
}

// view renders the confirm as a floating modal box, byte-identical to the
// retired renderAdminModal.
func (c confirmPrompt) view(theme adminTheme) string {
	return theme.section.Render(strings.Join([]string{
		theme.subheading.Render(c.title),
		c.message,
		"",
		theme.muted.Render(fmt.Sprintf("Enter: %s | Esc: cancel", c.confirmText)),
	}, "\n"))
}
