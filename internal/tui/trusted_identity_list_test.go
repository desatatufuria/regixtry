package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"regixtry/internal/ports"
)

func trustedIdentityListTestKey(runes string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(runes)}
}

var trustedIdentityListTestGitHub = ports.TrustedIdentity{
	CertificateIdentityRegexp: "^https://github.com/acme/.+$",
	CertificateOIDCIssuer:     "https://token.actions.githubusercontent.com",
}

var trustedIdentityListTestGitLab = ports.TrustedIdentity{
	CertificateIdentityRegexp: "^https://gitlab.com/acme/.+$",
	CertificateOIDCIssuer:     "https://gitlab.com",
}

func TestTrustedIdentityListUpDownNavigationIsBounded(t *testing.T) {
	t.Parallel()

	list := newTrustedIdentityList([]ports.TrustedIdentity{trustedIdentityListTestGitHub, trustedIdentityListTestGitLab})

	next, consumed := list.update(tea.KeyMsg{Type: tea.KeyUp})
	if !consumed {
		t.Fatalf("Up: consumed = %v, want true", consumed)
	}
	if next.selected != 0 {
		t.Fatalf("selected = %d, want 0 (bounded at zero)", next.selected)
	}

	next, _ = next.update(tea.KeyMsg{Type: tea.KeyDown})
	if next.selected != 1 {
		t.Fatalf("selected = %d, want 1", next.selected)
	}
	next, _ = next.update(tea.KeyMsg{Type: tea.KeyDown})
	if next.selected != 1 {
		t.Fatalf("selected = %d, want 1 (bounded at len-1)", next.selected)
	}
}

func TestTrustedIdentityListUnhandledKeyIsNotConsumed(t *testing.T) {
	t.Parallel()

	list := newTrustedIdentityList(nil)
	next, consumed := list.update(tea.KeyMsg{Type: tea.KeyTab})
	if consumed {
		t.Fatal("Tab: consumed = true, want false (falls through to the host screen)")
	}
	if next.adding {
		t.Fatal("Tab must not enter adding mode")
	}
}

func TestTrustedIdentityListAddFlowCommitsAValidIdentity(t *testing.T) {
	t.Parallel()

	list := newTrustedIdentityList(nil)

	list, consumed := list.update(trustedIdentityListTestKey("n"))
	if !consumed || !list.adding {
		t.Fatalf("'n': consumed = %v, adding = %v, want consumed=true, adding=true", consumed, list.adding)
	}
	if list.addFocus != identityAddFieldRegexp {
		t.Fatalf("addFocus = %v, want identityAddFieldRegexp", list.addFocus)
	}

	for _, r := range trustedIdentityListTestGitHub.CertificateIdentityRegexp {
		list, _ = list.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	list, consumed = list.update(tea.KeyMsg{Type: tea.KeyTab})
	if !consumed || list.addFocus != identityAddFieldIssuer {
		t.Fatalf("Tab (switch to issuer field): consumed = %v, addFocus = %v, want consumed=true, addFocus=identityAddFieldIssuer", consumed, list.addFocus)
	}
	for _, r := range trustedIdentityListTestGitHub.CertificateOIDCIssuer {
		list, _ = list.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	list, consumed = list.update(tea.KeyMsg{Type: tea.KeyEnter})
	if !consumed {
		t.Fatalf("Enter (commit add): consumed = %v, want true", consumed)
	}
	if list.adding {
		t.Fatal("adding = true after a successful commit, want false")
	}
	if len(list.identities) != 1 {
		t.Fatalf("len(identities) = %d, want 1", len(list.identities))
	}
	if list.identities[0] != trustedIdentityListTestGitHub {
		t.Fatalf("identities[0] = %#v, want %#v", list.identities[0], trustedIdentityListTestGitHub)
	}
	if list.err != "" {
		t.Fatalf("err = %q, want empty after a successful commit", list.err)
	}
}

func TestTrustedIdentityListAddFlowRejectsInvalidRegexp(t *testing.T) {
	t.Parallel()

	list := newTrustedIdentityList(nil)
	list, _ = list.update(trustedIdentityListTestKey("n"))
	list, _ = list.update(trustedIdentityListTestKey("(unterminated"))

	list, consumed := list.update(tea.KeyMsg{Type: tea.KeyEnter})
	if !consumed {
		t.Fatal("Enter (rejected commit): consumed = false, want true")
	}
	if !list.adding {
		t.Fatal("adding = false after a rejected commit, want true (stays in adding mode)")
	}
	if list.err == "" {
		t.Fatal("err = \"\", want a validation error surfaced for an invalid regexp")
	}
	if len(list.identities) != 0 {
		t.Fatalf("len(identities) = %d, want 0 (nothing appended on a rejected commit)", len(list.identities))
	}
}

func TestTrustedIdentityListAddFlowRejectsMissingIssuer(t *testing.T) {
	t.Parallel()

	list := newTrustedIdentityList(nil)
	list, _ = list.update(trustedIdentityListTestKey("n"))
	list, _ = list.update(trustedIdentityListTestKey("^valid$"))

	list, consumed := list.update(tea.KeyMsg{Type: tea.KeyEnter})
	if !consumed {
		t.Fatal("Enter (rejected commit, empty issuer): consumed = false, want true")
	}
	if !list.adding {
		t.Fatal("adding = false after a rejected commit, want true (stays in adding mode)")
	}
	if list.err == "" {
		t.Fatal("err = \"\", want a validation error surfaced for a missing issuer")
	}
	if len(list.identities) != 0 {
		t.Fatalf("len(identities) = %d, want 0 (nothing appended on a rejected commit)", len(list.identities))
	}
}

func TestTrustedIdentityListEscWhileAddingCancelsWithoutAdding(t *testing.T) {
	t.Parallel()

	list := newTrustedIdentityList(nil)
	list, _ = list.update(trustedIdentityListTestKey("n"))
	list, _ = list.update(trustedIdentityListTestKey("partial input"))

	list, consumed := list.update(tea.KeyMsg{Type: tea.KeyEsc})
	if !consumed {
		t.Fatal("Esc (cancel add): consumed = false, want true")
	}
	if list.adding {
		t.Fatal("adding = true after Esc, want false")
	}
	if list.inputRegexp != "" {
		t.Fatalf("inputRegexp = %q after Esc, want empty", list.inputRegexp)
	}
	if len(list.identities) != 0 {
		t.Fatalf("len(identities) = %d, want 0", len(list.identities))
	}
}

func TestTrustedIdentityListDeleteRemovesExactlyTheSelectedIdentity(t *testing.T) {
	t.Parallel()

	list := newTrustedIdentityList([]ports.TrustedIdentity{trustedIdentityListTestGitHub, trustedIdentityListTestGitLab})
	list.selected = 0

	list, consumed := list.update(trustedIdentityListTestKey("x"))
	if !consumed {
		t.Fatal("'x': consumed = false, want true")
	}
	if len(list.identities) != 1 {
		t.Fatalf("len(identities) = %d, want 1 (exactly one entry removed)", len(list.identities))
	}
	if list.identities[0] != trustedIdentityListTestGitLab {
		t.Fatal("the wrong identity was removed")
	}
}

func TestTrustedIdentityListDeleteWithNoIdentitiesIsNoOp(t *testing.T) {
	t.Parallel()

	list := newTrustedIdentityList(nil)
	next, consumed := list.update(trustedIdentityListTestKey("x"))
	if !consumed {
		t.Fatal("'x' on empty list: consumed = false, want true (no-op)")
	}
	if len(next.identities) != 0 {
		t.Fatalf("len(identities) = %d, want 0", len(next.identities))
	}
}

func TestTrustedIdentityListSetIdentitiesReplacesWholesale(t *testing.T) {
	t.Parallel()

	list := newTrustedIdentityList([]ports.TrustedIdentity{trustedIdentityListTestGitHub})
	list = list.SetIdentities([]ports.TrustedIdentity{trustedIdentityListTestGitLab})
	if len(list.Identities()) != 1 || list.Identities()[0] != trustedIdentityListTestGitLab {
		t.Fatalf("Identities() = %#v, want [%#v]", list.Identities(), trustedIdentityListTestGitLab)
	}
}
