package tui

import (
	"strings"
	"testing"

	"regixtry/internal/ports"
)

var polishSizes = []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}}

// The trusted keys and the trusted identities are two sections: one blank row
// separates them, and a long policy drops that row before it overflows.
func TestSigningPolicySeparatesKeysFromIdentities(t *testing.T) {
	t.Parallel()
	for _, size := range polishSizes {
		out := securityRender(securityModelAt(t, securityClient(), screenSecuritySigningConfig, size.w, size.h))
		securityNoOverflow(t, "signing", out, size.w, size.h)
		lines := strings.Split(out, "\n")
		found := false
		for i, line := range lines {
			if !strings.Contains(line, "TRUSTED IDENTITIES") {
				continue
			}
			found = true
			prev := strings.TrimRight(lines[i-1], " ")
			inside := prev[:strings.LastIndex(prev, "│")]
			if strings.TrimSpace(inside[strings.LastIndex(inside, "│")+len("│"):]) != "" {
				t.Errorf("%dx%d: no blank row before the identities:\n%s", size.w, size.h, out)
			}
		}
		if !found {
			t.Fatalf("%dx%d: no identities section:\n%s", size.w, size.h, out)
		}
	}

	client := securityClient()
	for range 8 {
		client.signingPolicy.TrustedPublicKeys = append(client.signingPolicy.TrustedPublicKeys, trustedKeyListTestPEM1)
		client.signingPolicy.TrustedIdentities = append(client.signingPolicy.TrustedIdentities, ports.TrustedIdentity{CertificateIdentityRegexp: "https://github.com/x/.*", CertificateOIDCIssuer: "https://issuer"})
	}
	for _, size := range polishSizes {
		securityNoOverflow(t, "signing long", securityRender(securityModelAt(t, client, screenSecuritySigningConfig, size.w, size.h)), size.w, size.h)
	}
}
