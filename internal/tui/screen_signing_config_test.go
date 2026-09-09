package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"regixtry/internal/ports"
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

// TestSigningConfigScreenKeyListInteractionSnapsFocusToAddKey is the
// Judgment Day fix-round RED test for the "invisible focus drift" finding
// (both judges): TestSigningConfigScreenKeyListReachableWithoutTabbingToIt
// already proves 'n' reaches the embedded trustedKeyList even while
// s.cfg.Focus visually sits elsewhere (still signingPolicyFieldEnabled), but
// never asserted the modal's OWN focus indicator moves to match -- so the
// rendered highlight (renderSigningPolicyModal's modal.Focus ==
// signingPolicyFieldAddKey check) silently disagreed with what was actually
// responding to input. The moment s.cfg.Keys.update consumes a key,
// s.cfg.Focus must snap to signingPolicyFieldAddKey so the visible indicator
// and the actual interaction target stay in sync.
func TestSigningConfigScreenKeyListInteractionSnapsFocusToAddKey(t *testing.T) {
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
	if nextScreen.cfg.Focus != signingPolicyFieldAddKey {
		t.Fatalf("cfg.Focus = %v after 'n' actually moved focus to the key list, want signingPolicyFieldAddKey (the visible focus indicator must match what actually responded to the keystroke)", nextScreen.cfg.Focus)
	}
}

// TestSigningConfigScreenOpenModalSeedsIdentitiesFromPolicy is the Phase 8.3
// RED test (tasks.md 8.3): opening the global signing modal ('p') seeds
// cfg.Identities from the currently loaded policy's TrustedIdentities,
// mirroring cfg.Keys' own seeding from TrustedPublicKeys.
func TestSigningConfigScreenOpenModalSeedsIdentitiesFromPolicy(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	s := signingConfigScreen{policy: ports.SigningPolicySettings{
		TrustedIdentities: []ports.TrustedIdentity{{CertificateIdentityRegexp: "^valid$", CertificateOIDCIssuer: "https://token.actions.githubusercontent.com"}},
	}}

	next, _, consumed := s.updateKey(env, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if !consumed {
		t.Fatal("'p': consumed = false, want true")
	}
	nextScreen, ok := next.(signingConfigScreen)
	if !ok {
		t.Fatalf("next = %T, want signingConfigScreen", next)
	}
	if len(nextScreen.cfg.Identities.Identities()) != 1 || nextScreen.cfg.Identities.Identities()[0].CertificateOIDCIssuer != "https://token.actions.githubusercontent.com" {
		t.Fatalf("cfg.Identities = %#v, want it seeded from the loaded policy", nextScreen.cfg.Identities.Identities())
	}
}

// TestSigningConfigScreenIdentityListReachableOnceTabbedTo is the Phase 8.3
// RED test: once focus is on signingPolicyFieldIdentities, 'n' reaches the
// embedded trustedIdentityList (design decision: Identities claims its keys
// only once explicitly focused, unlike Keys' always-reachable shortcut --
// see updateConfigKey's own doc comment for why).
func TestSigningConfigScreenIdentityListReachableOnceTabbedTo(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	s := signingConfigScreen{cfg: signingPolicyModal{
		Open:       true,
		Focus:      signingPolicyFieldIdentities,
		Keys:       newTrustedKeyList("", nil),
		Identities: newTrustedIdentityList(nil),
	}}

	next, cmd, consumed := s.updateConfigKey(env, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !consumed || cmd != nil {
		t.Fatalf("'n' with identities focused: consumed = %v, cmd = %v, want consumed=true, cmd=nil", consumed, cmd)
	}
	nextScreen, ok := next.(signingConfigScreen)
	if !ok {
		t.Fatalf("next = %T, want signingConfigScreen", next)
	}
	if !nextScreen.cfg.Identities.adding {
		t.Fatal("'n' did not reach the embedded trustedIdentityList: adding = false, want true")
	}
}

// TestSigningConfigScreenSaveIncludesConfiguredIdentities is the Phase 8.3
// RED test: submitting the modal (Enter) builds a
// ports.SigningPolicySettings carrying the currently configured
// TrustedIdentities, round-tripping through the admin API alongside
// TrustedPublicKeys (spec: "Operator saves a policy change").
func TestSigningConfigScreenSaveIncludesConfiguredIdentities(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	s := signingConfigScreen{cfg: signingPolicyModal{
		Open:       true,
		Focus:      signingPolicyFieldClearKeys,
		Keys:       newTrustedKeyList("", nil),
		Identities: newTrustedIdentityList([]ports.TrustedIdentity{{CertificateIdentityRegexp: "^valid$", CertificateOIDCIssuer: "https://token.actions.githubusercontent.com"}}),
	}}

	_, cmd, consumed := s.updateConfigKey(env, tea.KeyMsg{Type: tea.KeyEnter})
	if !consumed || cmd == nil {
		t.Fatalf("Enter (save): consumed = %v, cmd = %v, want consumed=true, cmd != nil", consumed, cmd)
	}
}

// TestRenderSigningPolicyModalShowsConfiguredIdentities is the Phase 8.3 RED
// test: the rendered modal includes the configured trusted identity's
// regexp and issuer (spec: "Modal shows current global signing policy").
func TestRenderSigningPolicyModalShowsConfiguredIdentities(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	modal := signingPolicyModal{
		Open:       true,
		Keys:       newTrustedKeyList("", nil),
		Identities: newTrustedIdentityList([]ports.TrustedIdentity{{CertificateIdentityRegexp: "^valid$", CertificateOIDCIssuer: "https://token.actions.githubusercontent.com"}}),
	}

	rendered := renderSigningPolicyModal(theme, modal)
	if !strings.Contains(rendered, "^valid$") || !strings.Contains(rendered, "https://token.actions.githubusercontent.com") {
		t.Fatalf("renderSigningPolicyModal() = %q, want the configured identity's regexp and issuer shown", rendered)
	}
}
