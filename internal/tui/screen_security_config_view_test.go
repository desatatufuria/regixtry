package tui

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"regixtry/internal/ports"
)

// securityModelLoading opens a screen and leaves its first read unanswered.
func securityModelLoading(t *testing.T, to screen, w, h int) Model {
	t.Helper()
	m := newAdminReadyModelWithCatalog(t, securityKnown, securityClient())
	m.now = func() time.Time { return fixedTestTime }
	m = resizeModel(t, m, w, h)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedTestTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	updated, _ := m.Update(navigateMsg{To: to})
	return updated.(Model)
}

// securityManyKeys returns n distinct valid trusted-key PEMs.
func securityManyKeys(t *testing.T, n int) []string {
	t.Helper()
	keys := make([]string, 0, n)
	for range n {
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("generate key: %v", err)
		}
		der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
		if err != nil {
			t.Fatalf("marshal key: %v", err)
		}
		keys = append(keys, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})))
	}
	return keys
}

func securityManyIdentities(n int) []ports.TrustedIdentity {
	identities := make([]ports.TrustedIdentity, 0, n)
	for i := range n {
		identities = append(identities, ports.TrustedIdentity{
			CertificateIdentityRegexp: fmt.Sprintf("https://github.com/日本語-organisation-with-a-very-long-name-%02d/.*/.github/workflows/release.yml@refs/tags/v.*", i),
			CertificateOIDCIssuer:     "https://token.actions.githubusercontent.com/some/very/long/issuer/path",
		})
	}
	return identities
}

// securityStressClient is the sample with long names, many repositories, many
// keys and many identities.
func securityStressClient(t *testing.T) (*fakeAdminClient, []string) {
	t.Helper()
	client := securityClient()
	known := make([]string, 0, 80)
	var overrides []ports.RepositoryOverrideDetails
	for i := range 80 {
		name := fmt.Sprintf("team-日本語/service-with-a-really-long-repository-name-%02d", i)
		known = append(known, name)
		if i%7 == 0 {
			overrides = append(overrides, ports.RepositoryOverrideDetails{Repository: name, Enabled: i%2 == 0, UpdatedAt: securityDay(time.October, i%28+1)})
		}
	}
	for _, feature := range []string{trivyFeatureName, gitleaksFeatureName, signingFeatureName} {
		client.repositoryOverrideList[feature] = overrides
	}
	client.signingPolicy.TrustedPublicKeys = securityManyKeys(t, 14)
	client.signingPolicy.TrustedIdentities = securityManyIdentities(9)
	long := securityTrivyPage()
	long.Sections[0].Fields[3].Value = "https://registry.internal.example.com/with/a/very/long/path/that/never/ends/and/ends/nowhere"
	client.featurePages[trivyFeatureName] = long
	return client, known
}

func securityStressModelAt(t *testing.T, to screen, w, h int) Model {
	t.Helper()
	client, known := securityStressClient(t)
	m := newAdminReadyModelWithCatalog(t, known, client)
	m.now = func() time.Time { return fixedTestTime }
	m = resizeModel(t, m, w, h)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedTestTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	updated, cmd := m.Update(navigateMsg{To: to})
	return securityRun(t, updated.(Model), cmd)
}

type securityViewCase struct {
	name  string
	build func(t *testing.T, w, h int) Model
}

func securityPlain(to screen, keys ...keyMsg) func(t *testing.T, w, h int) Model {
	return func(t *testing.T, w, h int) Model {
		return securityPress(t, securityModelAt(t, securityClient(), to, w, h), keys...)
	}
}

func securityStress(to screen, keys ...keyMsg) func(t *testing.T, w, h int) Model {
	return func(t *testing.T, w, h int) Model {
		return securityPress(t, securityStressModelAt(t, to, w, h), keys...)
	}
}

func securityFailing(to screen, err error) func(t *testing.T, w, h int) Model {
	return func(t *testing.T, w, h int) Model {
		client := securityClient()
		client.featureErr = err
		return securityModelAt(t, client, to, w, h)
	}
}

func securityPolicyUnavailable(to screen) func(t *testing.T, w, h int) Model {
	return func(t *testing.T, w, h int) Model {
		client := securityClient()
		client.scanPolicyErr = errors.New("policy down")
		client.signingPolicyErr = errors.New("policy down")
		client.listRepositoryOverridesErr = errors.New("overrides down")
		return securityModelAt(t, client, to, w, h)
	}
}

// securityCases are the states every size is checked in; the ones with
// golden set are also pinned as goldens.
var securityCases = []struct {
	securityViewCase
	golden bool
}{
	{securityViewCase{"trivy_config", securityPlain(screenSecurityTrivy)}, true},
	{securityViewCase{"trivy_config_modal", securityPlain(screenSecurityTrivy, testKeyRunes("c"))}, true},
	{securityViewCase{"trivy_config_modal_error", securityPlain(screenSecurityTrivy, testKeyRunes("c"), testKeyTab(), testKeyRunes("x"), testKeyEnter())}, true},
	{securityViewCase{"trivy_policy_modal", securityPlain(screenSecurityTrivy, testKeyRunes("p"))}, true},
	{securityViewCase{"trivy_confirm_disable", securityPlain(screenSecurityTrivy, testKeyRunes("x"))}, true},
	{securityViewCase{"trivy_loading", func(t *testing.T, w, h int) Model { return securityModelLoading(t, screenSecurityTrivy, w, h) }}, true},
	{securityViewCase{"trivy_error", securityFailing(screenSecurityTrivy, errors.New("boom"))}, true},
	{securityViewCase{"trivy_policy_unavailable", securityPolicyUnavailable(screenSecurityTrivy)}, false},
	{securityViewCase{"trivy_stress", securityStress(screenSecurityTrivy)}, false},
	{securityViewCase{"trivy_stress_config_modal", securityStress(screenSecurityTrivy, testKeyRunes("c"), testKeyTab(), testKeyTab(), testKeyTab())}, false},
	{securityViewCase{"gitleaks_config", securityPlain(screenSecurityGitleaksConfig)}, true},
	{securityViewCase{"gitleaks_config_modal", securityPlain(screenSecurityGitleaksConfig, testKeyRunes("c"))}, true},
	{securityViewCase{"gitleaks_config_modal_error", securityPlain(screenSecurityGitleaksConfig, testKeyRunes("c"), testKeyTab(), testKeyRunes("x"), testKeyEnter())}, true},
	{securityViewCase{"gitleaks_loading", func(t *testing.T, w, h int) Model { return securityModelLoading(t, screenSecurityGitleaksConfig, w, h) }}, false},
	{securityViewCase{"gitleaks_error", securityFailing(screenSecurityGitleaksConfig, errors.New("boom"))}, false},
	{securityViewCase{"gitleaks_confirm_disable", securityPlain(screenSecurityGitleaksConfig, testKeyRunes("x"))}, false},
	{securityViewCase{"gitleaks_overrides_unavailable", securityPolicyUnavailable(screenSecurityGitleaksConfig)}, false},
	{securityViewCase{"gitleaks_stress", securityStress(screenSecurityGitleaksConfig)}, false},
	{securityViewCase{"signing_config", securityPlain(screenSecuritySigningConfig)}, true},
	{securityViewCase{"signing_policy_modal", securityPlain(screenSecuritySigningConfig, testKeyRunes("p"))}, true},
	{securityViewCase{"signing_policy_modal_adding_key", securityPlain(screenSecuritySigningConfig, testKeyRunes("p"), testKeyRunes("n"), testKeyRunes("-"), testKeyRunes("-"))}, true},
	{securityViewCase{"signing_policy_modal_delete_key", securityPlain(screenSecuritySigningConfig, testKeyRunes("p"), testKeyRunes("x"))}, true},
	{securityViewCase{"signing_policy_modal_adding_identity", securityPlain(screenSecuritySigningConfig, testKeyRunes("p"), testKeyTab(), testKeyTab(), testKeyTab(), testKeyRunes("n"), testKeyRunes("a"), testKeyTab(), testKeyRunes("b"))}, false},
	{securityViewCase{"signing_loading", func(t *testing.T, w, h int) Model { return securityModelLoading(t, screenSecuritySigningConfig, w, h) }}, false},
	{securityViewCase{"signing_error", securityFailing(screenSecuritySigningConfig, errors.New("boom"))}, true},
	{securityViewCase{"signing_policy_unavailable", securityPolicyUnavailable(screenSecuritySigningConfig)}, false},
	{securityViewCase{"signing_stress", securityStress(screenSecuritySigningConfig)}, false},
	{securityViewCase{"signing_stress_modal", securityStress(screenSecuritySigningConfig, testKeyRunes("p"))}, false},
	{securityViewCase{"signing_stress_modal_selected_key", securityStress(screenSecuritySigningConfig, testKeyRunes("p"), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown())}, false},
	{securityViewCase{"signing_stress_modal_adding_key", securityStress(screenSecuritySigningConfig, testKeyRunes("p"), testKeyRunes("n"), testKeyRunes("a"))}, false},
	{securityViewCase{"signing_stress_modal_identities", securityStress(screenSecuritySigningConfig, testKeyRunes("p"), testKeyTab(), testKeyTab(), testKeyTab(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown())}, false},
	{securityViewCase{"signing_stress_modal_adding_identity", securityStress(screenSecuritySigningConfig, testKeyRunes("p"), testKeyTab(), testKeyTab(), testKeyTab(), testKeyRunes("n"), testKeyRunes("a"), testKeyTab(), testKeyRunes("b"))}, false},
}

func TestSecurityScreensNeverOverflowTheTerminalInAnyState(t *testing.T) {
	t.Parallel()
	for _, tc := range securityCases {
		for _, size := range securityConfigSizes {
			label := fmt.Sprintf("%s at %dx%d", tc.name, size.w, size.h)
			securityNoOverflow(t, label, tc.build(t, size.w, size.h).View().Content, size.w, size.h)
		}
	}
}

func TestSecurityModalsFitWithTheirFocusedFieldVisible(t *testing.T) {
	t.Parallel()
	// Typing into the focused field must keep it on screen even when the modal
	// has to drop other rows to fit.
	for _, size := range securityConfigSizes {
		m := securityStressModelAt(t, screenSecuritySigningConfig, size.w, size.h)
		m = securityPress(t, m, testKeyRunes("p"), testKeyRunes("n"), testKeyRunes("a"), testKeyRunes("b"), testKeyRunes("c"))
		if out := securityRender(m); !strings.Contains(out, "abc") {
			t.Errorf("%dx%d: the key being typed is not visible:\n%s", size.w, size.h, out)
		}
		m = securityStressModelAt(t, screenSecuritySigningConfig, size.w, size.h)
		m = securityPress(t, m, testKeyRunes("p"), testKeyTab(), testKeyTab(), testKeyTab(), testKeyRunes("n"), testKeyRunes("a"), testKeyTab(), testKeyRunes("z"))
		if out := securityRender(m); !strings.Contains(out, "z▏") {
			t.Errorf("%dx%d: the issuer being typed is not visible:\n%s", size.w, size.h, out)
		}
	}
}

func TestSecuritySigningModalKeepsTheSelectedKeyVisible(t *testing.T) {
	t.Parallel()
	for _, size := range securityConfigSizes {
		m := securityStressModelAt(t, screenSecuritySigningConfig, size.w, size.h)
		m = securityPress(t, m, testKeyRunes("p"))
		keys := securitySigningOf(t, m).cfg.Keys.Keys()
		fingerprints := signingKeyFingerprints(keys)
		for i := range keys {
			if i > 0 {
				m = securityPress(t, m, testKeyDown())
			}
			if out := securityRender(m); !strings.Contains(out, "▸ ") || !strings.Contains(out, fingerprints[i]) {
				t.Fatalf("%dx%d: key %d (%s) is not visible when selected:\n%s", size.w, size.h, i, fingerprints[i], out)
			}
		}
	}
}

func assertSecurityGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "security_config", name+".golden")
	if *updateSecurityConfigGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update-security-config): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("rendered view differs from %s (run with -update-security-config to inspect)\n--- want\n%s\n--- got\n%s", path, strings.TrimRight(string(want), "\n"), strings.TrimRight(got, "\n"))
	}
}

func TestSecurityViewGoldens(t *testing.T) {
	for _, tc := range securityCases {
		if !tc.golden {
			continue
		}
		for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
			name := fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h)
			t.Run(name, func(t *testing.T) {
				assertSecurityGolden(t, name, securityRender(tc.build(t, size.w, size.h)))
			})
		}
	}
}
