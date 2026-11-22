package tui

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	mrand "math/rand"
	"testing"
	"time"

	"regixtry/internal/ports"
)

type recordingSigningClient struct {
	*fakeAdminClient
	saves []ports.SigningPolicySettings
}

func (c *recordingSigningClient) UpdateSigningPolicy(ctx context.Context, s AdminSession, in ports.SigningPolicySettings) (ports.SigningPolicySettings, error) {
	c.saves = append(c.saves, in)
	return c.fakeAdminClient.UpdateSigningPolicy(ctx, s, in)
}

// TestSigningPolicySaveNeverLosesWhatTheFormDidNotChange throws seeded random
// non-destructive keys at the signing screen (no n / x, so no key or identity
// is added or removed by hand) and checks every save payload: the trusted keys
// and identities are exactly the loaded ones, except for the one Enter pair on
// "Clear all trusted keys" that asks for an empty list.
func TestSigningPolicySaveNeverLosesWhatTheFormDidNotChange(t *testing.T) {
	keys := []string{fuzzKeyPEM(t), fuzzKeyPEM(t), fuzzKeyPEM(t)}
	ids := []ports.TrustedIdentity{{CertificateIdentityRegexp: "^a$", CertificateOIDCIssuer: "https://i"}, {CertificateIdentityRegexp: "^b$", CertificateOIDCIssuer: "https://j"}}
	alphabet := []keyMsg{testKeyRunes("p"), testKeyEsc(), testKeyTab(), testKeyShiftTab(), testKeyDown(), testKeyUp(), testKeyRunes("j"), testKeyRunes("k"), testKeySpace(), testKeyEnter(), testKeyEnter(), testKeyRunes("r")}
	total, clears := 0, 0
	for seed := int64(0); seed < 60; seed++ {
		rng := mrand.New(mrand.NewSource(seed))
		client := &recordingSigningClient{fakeAdminClient: &fakeAdminClient{
			featurePage:   ports.FeaturePage{Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true}},
			signingPolicy: ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: keys, TrustedIdentities: ids, UnsignedSelfRead: "pusher"},
		}}
		m := newAdminReadyModel(t, client)
		m.adminSession, m.adminAuth = AdminSession{Username: "operator", BearerToken: "t", ExpiresAt: charFixedTime.Add(24 * 3600e9)}, adminAuthStateAuthenticated
		m.now = func() time.Time { return charFixedTime }
		u, c := m.Update(navigateMsg{To: screenSecuritySigningConfig})
		m = repoAccessRun(t, u.(Model), c)
		cleared := false
		for step := 0; step < 40; step++ {
			s, onScreen := m.adminScreens[slotSigningConfig].(signingConfigScreen)
			if m.screen != screenSecuritySigningConfig || !onScreen {
				u, c := m.Update(navigateMsg{To: screenSecuritySigningConfig})
				m = repoAccessRun(t, u.(Model), c)
				continue
			}
			armedClear := s.cfg.Active() && s.cfg.Focus == signingPolicyFieldClearKeys && s.clearArmed && !s.saving
			before := len(client.saves)
			m = repoAccessPress(t, m, alphabet[rng.Intn(len(alphabet))])
			for _, save := range client.saves[before:] {
				total++
				switch {
				case len(save.TrustedPublicKeys) == 0 && armedClear:
					cleared = true
					clears++
				case len(save.TrustedPublicKeys) != len(keys):
					t.Fatalf("seed %d step %d: payload keys=%d, want %d (not a confirmed clear)", seed, step, len(save.TrustedPublicKeys), len(keys))
				}
				if !cleared && len(save.TrustedIdentities) != len(ids) {
					t.Fatalf("seed %d step %d: payload identities=%d, want %d", seed, step, len(save.TrustedIdentities), len(ids))
				}
				if cleared {
					step = 1 << 20 // after a confirmed clear the seed is spent
				}
			}
		}
	}
	if total == 0 {
		t.Fatal("the fuzz never reached a save")
	}
	t.Logf("%d saves, %d confirmed clears", total, clears)
}

func fuzzKeyPEM(t *testing.T) string {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}
