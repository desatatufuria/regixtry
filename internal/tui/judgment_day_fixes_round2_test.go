package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestSecurityMenuScreenEscReturnsToAdminMenu is the Judgment Day final-review
// fix-round RED test for Finding 1 (CRITICAL, dual-confirmed):
// securityMenuScreen is now entered directly from screenAdminMenu (Phase 18's
// 4-domain root menu, see screen_admin_menu.go's "Security & Compliance" row),
// so its Esc case must return to screenAdminMenu -- the same domain-root
// target screenAdminOperations' own Esc already correctly uses
// (screen_operations.go). Esc must NOT jump sideways into the unrelated
// Identity & Access domain (screenAdminUsers), which is stale Phase 11
// wiring predating Phase 18.
func TestSecurityMenuScreenEscReturnsToAdminMenu(t *testing.T) {
	t.Parallel()

	s := securityMenuScreen{}

	next, cmd, consumed := s.Update(screenEnv{}, tea.KeyMsg{Type: tea.KeyEsc})
	if !consumed {
		t.Fatalf("consumed = false, want true")
	}
	if _, ok := next.(securityMenuScreen); !ok {
		t.Fatalf("next = %T, want securityMenuScreen unchanged", next)
	}
	if cmd == nil {
		t.Fatalf("cmd = nil, want the navigate(screenAdminMenu) command")
	}

	msg := cmd()
	navMsg, ok := msg.(navigateMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want navigateMsg", msg)
	}
	if navMsg.To != screenAdminMenu {
		t.Fatalf("navigateMsg.To = %v, want screenAdminMenu", navMsg.To)
	}
}

// TestAdminMenuAndOperationsScreensAdvertiseQuitKey is the Judgment Day
// final-review fix-round RED test for Finding 2 (dual-flagged): both
// screenAdminMenu and screenAdminOperations are included in
// isAdminPrincipalScreen (model.go), meaning the bare 'q' key genuinely does
// quit the program from these two screens (checked before the key ever
// reaches the screen). Since the footer is rendered directly from each
// screen's own Keys() value (design.md Decision A: keymap-derived help
// cannot drift from real behavior), a working key absent from Keys() is
// invisible to the operator. Every sibling principal screen in this change
// already declares a "q"/"quit" binding; these were the only 2 that didn't.
func TestAdminMenuAndOperationsScreensAdvertiseQuitKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		keys screenKeys
	}{
		{"adminMenuKeys", adminMenuKeys},
		{"adminOperationsKeys", adminOperationsKeys},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, binding := range tc.keys.short {
				for _, k := range binding.Keys() {
					if k != "q" {
						continue
					}
					if binding.Help().Desc != "quit" {
						t.Fatalf("%s: %q binding help = %q, want %q", tc.name, k, binding.Help().Desc, "quit")
					}
					return
				}
			}
			t.Fatalf("%s: no \"q\" key.Binding found, want a declared \"q: quit\" binding", tc.name)
		})
	}
}
