package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestSigningConfigScreenSatisfiesAdminScreenInterface is the Phase 12.3 RED
// test (design.md's State Migration table: SigningPolicy/SigningPolicyModal
// -> signingConfigScreen), mirroring
// TestGitleaksConfigScreenSatisfiesAdminScreenInterface's compile-level
// assertion. signingConfigScreen must satisfy the adminScreen contract
// value-typed (Update returns a new adminScreen, never a pointer receiver --
// design.md Decision A).
func TestSigningConfigScreenSatisfiesAdminScreenInterface(t *testing.T) {
	t.Parallel()

	var _ adminScreen = signingConfigScreen{}
}

// TestSigningConfigScreenKeyListReachableWithoutTabbingToIt is the RED test
// for the "can't add a key" bug report on the global Signing Policy modal:
// the modal opens with Focus == signingPolicyFieldEnabled (updateKey's 'p'
// branch), not signingPolicyFieldAddKey where cfg.Keys lives, so 'n' must
// still reach the embedded trustedKeyList without the operator tabbing to it
// first.
func TestSigningConfigScreenKeyListReachableWithoutTabbingToIt(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	s := signingConfigScreen{cfg: signingPolicyModal{
		Open:  true,
		Focus: signingPolicyFieldEnabled,
		Keys:  newTrustedKeyList("", nil),
	}}
	if s.cfg.Focus == signingPolicyFieldAddKey {
		t.Fatal("test setup invalid: default focus is already signingPolicyFieldAddKey")
	}

	next, cmd, consumed := s.updateConfigKey(env, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !consumed || cmd != nil {
		t.Fatalf("'n' with focus elsewhere: consumed = %v, cmd = %v, want consumed=true, cmd=nil", consumed, cmd)
	}
	nextScreen, ok := next.(signingConfigScreen)
	if !ok {
		t.Fatalf("next = %T, want signingConfigScreen", next)
	}
	if !nextScreen.cfg.Keys.adding {
		t.Fatal("'n' did not reach the embedded trustedKeyList: adding = false, want true")
	}
}
