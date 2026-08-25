package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestConfirmPromptActiveReflectsOnConfirmPresence is the tui-menu-architecture
// change's Phase 5 task 5.1 (part of design.md Decision E) RED test:
// Active() is false when onConfirm is nil (the zero value), true once
// newConfirmPrompt has set it.
func TestConfirmPromptActiveReflectsOnConfirmPresence(t *testing.T) {
	t.Parallel()

	var zero confirmPrompt
	if zero.Active() {
		t.Fatalf("zero-value confirmPrompt.Active() = true, want false")
	}

	c := newConfirmPrompt("Title", "Message", "confirm", "Submitting...", func(screenEnv) tea.Cmd { return nil })
	if !c.Active() {
		t.Fatalf("newConfirmPrompt(...).Active() = false, want true")
	}
}

// TestConfirmPromptSwallowsEveryKeyWhileActive is the tui-menu-architecture
// change's Phase 5 task 5.2 RED test: it reproduces
// updateAdminConfirmKey's existing swallow-all behavior — while Active(),
// every key is consumed, whether or not it is Esc/Enter.
func TestConfirmPromptSwallowsEveryKeyWhileActive(t *testing.T) {
	t.Parallel()

	c := newConfirmPrompt("Title", "Message", "confirm", "Submitting...", func(screenEnv) tea.Cmd { return nil })

	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyEsc},
		{Type: tea.KeyEnter},
		{Type: tea.KeyRunes, Runes: []rune{'z'}},
		{Type: tea.KeyTab},
	} {
		_, _, consumed := c.update(screenEnv{}, msg)
		if !consumed {
			t.Fatalf("update(%v) consumed = false, want true (swallow-all while active)", msg)
		}
	}
}

// TestConfirmPromptEnterDispatchesOnConfirmAndKeepsPromptOpen mirrors
// updateAdminConfirmKey's Enter branch: onConfirm fires (receiving env at
// confirm time, never captured at open time — design.md Decision E), and
// the prompt itself is NOT cleared yet (the real close happens once the
// async result msg arrives, exactly like today's adminConfirmModal).
func TestConfirmPromptEnterDispatchesOnConfirmAndKeepsPromptOpen(t *testing.T) {
	t.Parallel()

	called := false
	c := newConfirmPrompt("Title", "Message", "confirm", "Submitting...", func(env screenEnv) tea.Cmd {
		called = true
		return func() tea.Msg { return nil }
	})

	next, cmd, consumed := c.update(screenEnv{}, tea.KeyMsg{Type: tea.KeyEnter})
	if !consumed {
		t.Fatalf("consumed = false, want true")
	}
	if cmd == nil {
		t.Fatalf("cmd = nil, want the onConfirm-dispatched command")
	}
	if !called {
		t.Fatalf("onConfirm was not invoked")
	}
	if !next.Active() {
		t.Fatalf("next.Active() = false, want the prompt to stay open until the async result arrives")
	}
}

// TestConfirmPromptEscClosesWithoutDispatching mirrors
// updateAdminConfirmKey's Esc branch: the prompt clears and no command is
// returned.
func TestConfirmPromptEscClosesWithoutDispatching(t *testing.T) {
	t.Parallel()

	called := false
	c := newConfirmPrompt("Title", "Message", "confirm", "Submitting...", func(screenEnv) tea.Cmd {
		called = true
		return nil
	})

	next, cmd, consumed := c.update(screenEnv{}, tea.KeyMsg{Type: tea.KeyEsc})
	if !consumed {
		t.Fatalf("consumed = false, want true")
	}
	if cmd != nil {
		t.Fatalf("cmd = non-nil, want nil on cancel")
	}
	if next.Active() {
		t.Fatalf("next.Active() = true, want the prompt closed after esc")
	}
	if called {
		t.Fatalf("onConfirm was invoked on esc, want it never called")
	}
}
