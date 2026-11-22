package tui

import (
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
