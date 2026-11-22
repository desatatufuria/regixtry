package tui

import (
	"regixtry/internal/ports"
	"strings"
	"testing"
)

// A new signing override says what it inherits from the global policy: the
// keys are copied into the form, the identities are not.
func TestSigningOverrideEditorStatesWhatIsInherited(t *testing.T) {
	t.Parallel()
	for _, size := range polishSizes {
		m := ovModelAt(t, ovClient(), securityKnown, screenSecuritySigningRepos, size.w, size.h)
		m = ovSelect(t, m, screenSecuritySigningRepos, "team/web")
		m = securityPress(t, m, testKeyRunes("o"))
		out := securityRender(m)
		securityNoOverflow(t, "override editor", out, size.w, size.h)
		if want := "Inheriting the global policy: 2 keys, 1 identity."; !strings.Contains(out, want) && size.h >= 30 {
			t.Errorf("%dx%d: editor lacks %q:\n%s", size.w, size.h, want, out)
		}
	}
}

// A new override saved after one edit keeps the global identity.
func TestSigningOverrideEditorPrefillsTheGlobalIdentitiesLikeTheKeys(t *testing.T) {
	t.Parallel()
	to := screenSecuritySigningRepos
	client := ovClient()
	m := ovOpenEditor(t, client, to, "team/web") // no override stored
	e := ovEditorOf(t, m, to)
	if len(e.identities.Identities()) != 1 || e.dirty {
		t.Fatalf("identities = %+v dirty = %v, want the global identity and an untouched editor", e.identities.Identities(), e.dirty)
	}
	m = ovFocus(t, m, to, overrideFieldEnabled)
	securityPress(t, m, testKeySpace(), testKeyEnter())
	got := client.lastSetRepositoryOverride
	if len(got.TrustedIdentities) != 1 || got.TrustedIdentities[0].CertificateIdentityRegexp != "https://github.com/team/.*" {
		t.Fatalf("saved %#v, want the global identity kept", got)
	}
	if out := securityRender(ovOpenEditor(t, ovClient(), to, "team/web")); !strings.Contains(out, "Inheriting the global policy: 2 keys, 1 identity.") {
		t.Fatalf("note not truthful:\n%s", out)
	}
}

// An existing override keeps its own identities; a late global read never
// overwrites what the operator typed.
func TestSigningOverrideEditorGlobalIdentitiesNeverOverwrite(t *testing.T) {
	t.Parallel()
	global := []ports.TrustedIdentity{{CertificateIdentityRegexp: "g", CertificateOIDCIssuer: "gi"}}
	own := []ports.TrustedIdentity{{CertificateIdentityRegexp: "own", CertificateOIDCIssuer: "oi"}}
	env := screenEnv{}
	msg := func(e overrideEditor) overrideGlobalMsg {
		return overrideGlobalMsg{securityResult: securityResult{gen: e.gen}, editor: e.id, enabled: true, identities: global}
	}
	loaded := func(e overrideEditor, exists bool) overrideLoadedMsg {
		return overrideLoadedMsg{securityResult: securityResult{gen: e.gen}, editor: e.id, repository: e.repository, feature: e.feature, exists: exists,
			override: ports.RepositoryOverrideDetails{Enabled: true, TrustedIdentities: own}}
	}
	e := newOverrideEditor(1, signingFeatureName, "team/api")
	e = e.applyGlobal(msg(e))
	e, _ = e.applyLoaded(env, loaded(e, true))
	if got := e.identities.Identities(); len(got) != 1 || got[0].CertificateIdentityRegexp != "own" {
		t.Fatalf("existing override identities = %+v", got)
	}

	e = newOverrideEditor(1, signingFeatureName, "team/web")
	e, _ = e.applyLoaded(env, loaded(e, false))
	e = e.applyGlobal(msg(e))
	e.identities = e.identities.SetIdentities(own)
	e = e.applyGlobal(msg(e))
	if got := e.identities.Identities(); got[0].CertificateIdentityRegexp != "own" {
		t.Fatalf("a second global read overwrote the operator's identities: %+v", got)
	}
}
