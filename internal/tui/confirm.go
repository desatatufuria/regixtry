package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	// target and targetDetail name what the action applies to, shown in a box
	// under the message; consequence is the amber note about what follows.
	// All three are optional (set with withTarget and withConsequence): a
	// prompt without them shows only its title and message.
	target, targetDetail, consequence string
}

// newConfirmPrompt constructs an active confirmPrompt. See the submitting
// field's doc comment for why this takes one more parameter than design.md's
// literal signature.
func newConfirmPrompt(title, message, confirmText, submitting string, onConfirm func(screenEnv) tea.Cmd) confirmPrompt {
	return confirmPrompt{title: title, message: message, confirmText: confirmText, submitting: submitting, onConfirm: onConfirm}
}

// withTarget names the exact target of the action (and an optional detail
// line under it) for the dialog's target box.
func (c confirmPrompt) withTarget(target, detail string) confirmPrompt {
	c.target, c.targetDetail = target, detail
	return c
}

// withConsequence sets the amber note about what the action leads to.
func (c confirmPrompt) withConsequence(text string) confirmPrompt {
	c.consequence = text
	return c
}

// Active reports whether a confirm is currently open.
func (c confirmPrompt) Active() bool {
	return c.onConfirm != nil
}

// update mirrors updateAdminConfirmKey's exact behavior: Esc clears the
// prompt and dispatches nothing; Enter, or y as the dialog's button says,
// dispatches onConfirm(env) and leaves
// the prompt open (the real close happens once the async result msg
// arrives, exactly like adminConfirmModal today); every other key is
// swallowed as a no-op. consumed is true for every key while active,
// reproducing updateAdminConfirmKey's existing swallow-all behavior.
func (c confirmPrompt) update(env screenEnv, msg keyMsg) (confirmPrompt, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return confirmPrompt{}, nil, true
	case isEnterKey(msg), isRuneKey(msg, 'y'):
		if c.onConfirm == nil {
			return c, nil, true
		}
		return c, c.onConfirm(env), true
	}
	return c, nil, true
}

const (
	// confirmDialogWidth is the board's 360px dialog at about 8.5px a column,
	// widened for the longest messages.
	confirmDialogWidth  = 64
	confirmDialogChrome = 6 // border plus two columns of padding each side
)

// confirmDestructiveVerbs are the confirmText values of actions that remove
// or switch off something: their dialog is drawn in the error color.
var confirmDestructiveVerbs = map[string]bool{"delete": true, "remove": true, "revoke": true, "disable": true}

// destructive reports whether the action is one the dialog must warn about.
func (c confirmPrompt) destructive() bool {
	return confirmDestructiveVerbs[strings.ToLower(strings.TrimSpace(c.confirmText))]
}

// view renders the confirm as a bordered dialog (the approved "Confirm
// dialogs" board): the title, what will happen, the exact target, the
// consequence, then the Confirm and Cancel buttons. A destructive action is
// drawn in the error color; any other in the accent.
func (c confirmPrompt) view(theme adminTheme) string {
	inner := confirmDialogWidth - confirmDialogChrome
	tone := theme.accent.GetForeground()
	if c.destructive() {
		tone = theme.error.GetForeground()
	}

	lines := []string{lipgloss.NewStyle().Foreground(tone).Bold(true).Render(c.title)}
	if c.message != "" {
		lines = append(lines, "", strings.Join(grantWrap(c.message, inner), "\n"))
	}
	if c.target != "" {
		box := theme.text.Bold(true).Render(c.target)
		if c.targetDetail != "" {
			box += "\n" + theme.muted.Render(c.targetDetail)
		}
		lines = append(lines, "", lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(theme.borderColor).
			Padding(0, 1).Width(inner).Render(box))
	}
	if c.consequence != "" {
		lines = append(lines, "", theme.warning.Render(strings.Join(grantWrap(c.consequence, inner), "\n")))
	}
	confirm := lipgloss.NewStyle().Width(inner).Padding(0, 1).Bold(true).
		Foreground(theme.pill.GetForeground()).Background(tone).
		Render(confirmButtonLine("Confirm", "y / enter", inner-2))
	cancel := lipgloss.NewStyle().Width(inner).Padding(0, 1).
		Render("Cancel" + strings.Repeat(" ", max(inner-2-len("Cancel")-len("esc"), 1)) + theme.muted.Render("esc"))
	lines = append(lines, "", confirm, cancel)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(tone).
		Padding(0, 2).Width(confirmDialogWidth).
		Render(strings.Join(lines, "\n"))
}

func confirmButtonLine(label, keycap string, width int) string {
	return label + strings.Repeat(" ", max(width-len(label)-len(keycap), 1)) + keycap
}
