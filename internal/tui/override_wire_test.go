package tui

import (
	"bytes"
	"encoding/json"
	"testing"

	"regixtry/internal/ports"
)

// The override editor against the REAL admin client, router, service and
// SQLite store: a save must send every field the PUT replaces, so a save that
// changes nothing leaves the stored override byte for byte the same.

func ovWireSeed(t *testing.T, e signingWireEnv, repository, feature string, body any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.service.SetRepositoryOverride(e.ctx, repository, feature, raw); err != nil {
		t.Fatalf("seed %s/%s: %v", feature, repository, err)
	}
}

// ovWirePayloads is every stored override of feature, by repository.
func ovWirePayloads(t *testing.T, e signingWireEnv, feature string) map[string]string {
	t.Helper()
	stored, err := e.service.ListRepositoryOverrides(e.ctx, feature)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, row := range stored {
		out[row.Repository] = string(row.Payload)
	}
	return out
}

func ovWireOpen(t *testing.T, e signingWireEnv, to screen, repository string) Model {
	t.Helper()
	m, cmd := signingWireModel(t, e, to, 160, 30)
	m = repoAccessRun(t, m, cmd)
	m = ovSelect(t, m, to, repository)
	return repoAccessPress(t, m, testKeyRunes("o"))
}

func TestOverrideEditorNoOpSaveLeavesEveryStoredOverrideUnchangedOverTheRealServer(t *testing.T) {
	t.Parallel()
	k1, k2 := signingWireKey(t), signingWireKey(t)
	identities := []ports.TrustedIdentity{
		{CertificateIdentityRegexp: "^b@example.com$", CertificateOIDCIssuer: "https://issuer.b"},
		{CertificateIdentityRegexp: "^a@example.com$", CertificateOIDCIssuer: "https://issuer.a"},
	}
	cases := []struct {
		name    string
		to      screen
		feature string
		body    map[string]any
	}{
		{"trivy with both paths", screenSecurityTrivyRepos, trivyFeatureName, map[string]any{"enabled": true, "ignore_file_path": "/etc/trivy/ignore", "ignore_policy_path": "/etc/trivy/policy.rego"}},
		{"trivy disabled with one path", screenSecurityTrivyRepos, trivyFeatureName, map[string]any{"enabled": false, "ignore_policy_path": "/etc/trivy/policy.rego"}},
		{"trivy enabled without paths", screenSecurityTrivyRepos, trivyFeatureName, map[string]any{"enabled": true}},
		{"gitleaks", screenSecurityGitleaksRepos, gitleaksFeatureName, map[string]any{"enabled": true, "config_path": "/etc/gitleaks.toml"}},
		{"gitleaks disabled", screenSecurityGitleaksRepos, gitleaksFeatureName, map[string]any{"enabled": false}},
		{"signing with keys, identities and a mode", screenSecuritySigningRepos, signingFeatureName, map[string]any{"enabled": true, "trusted_public_keys": []string{k2, k1}, "trusted_identities": identities, "unsigned_self_read": "repo_push"}},
		{"signing without a mode", screenSecuritySigningRepos, signingFeatureName, map[string]any{"enabled": true, "trusted_public_keys": []string{k1}}},
		{"signing with the off mode written out", screenSecuritySigningRepos, signingFeatureName, map[string]any{"enabled": true, "trusted_identities": identities, "unsigned_self_read": "off"}},
		{"signing gate off with identities and pusher mode", screenSecuritySigningRepos, signingFeatureName, map[string]any{"enabled": false, "trusted_identities": identities, "unsigned_self_read": "pusher"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := signingWireSetup(t)
			ovWireSeed(t, e, "team/api", tc.feature, tc.body)
			ovWireSeed(t, e, "team/web", tc.feature, map[string]any{"enabled": false})
			before := ovWirePayloads(t, e, tc.feature)

			m := ovWireOpen(t, e, tc.to, "team/api")
			if ed := ovEditorOf(t, m, tc.to); !ed.exists || !ed.loaded {
				t.Fatalf("the editor did not read the stored override: %+v", ed)
			}
			m = repoAccessPress(t, m, testKeyEnter())
			if ed := ovEditorOf(t, m, tc.to); ed.err != "" || ed.note != "Override saved." {
				t.Fatalf("the save failed: err %q note %q", ed.err, ed.note)
			}

			after := ovWirePayloads(t, e, tc.feature)
			if len(after) != len(before) {
				t.Fatalf("overrides before %d, after %d", len(before), len(after))
			}
			for repository, payload := range before {
				if !bytes.Equal([]byte(after[repository]), []byte(payload)) {
					t.Errorf("%s: a no-op save changed the stored override\n before %s\n after  %s", repository, payload, after[repository])
				}
			}
		})
	}
}

func TestOverrideEditorEditsAndClearsOverTheRealServer(t *testing.T) {
	t.Parallel()
	e := signingWireSetup(t)
	ovWireSeed(t, e, "team/api", trivyFeatureName, map[string]any{"enabled": true, "ignore_file_path": "/etc/trivy/ignore"})
	ovWireSeed(t, e, "team/web", trivyFeatureName, map[string]any{"enabled": true, "ignore_file_path": "/etc/trivy/web"})
	ovWireSeed(t, e, "team/api", gitleaksFeatureName, map[string]any{"enabled": true, "config_path": "/etc/gitleaks.toml"})

	to := screenSecurityTrivyRepos
	m := ovWireOpen(t, e, to, "team/api")
	// Focus is on Enabled: Tab to the ignore file, add to it, then to the policy.
	m = repoAccessPress(t, m, testKeyTab())
	m = repoAccessPress(t, m, repoAccessType("2")...)
	m = repoAccessPress(t, m, testKeyTab())
	m = repoAccessPress(t, m, repoAccessType("/etc/trivy/policy.rego")...)
	m = repoAccessPress(t, m, testKeyEnter())
	if ed := ovEditorOf(t, m, to); ed.err != "" {
		t.Fatalf("save failed: %s", ed.err)
	}
	stored := ovWirePayloads(t, e, trivyFeatureName)
	want := `{"enabled":true,"ignore_file_path":"/etc/trivy/ignore2","ignore_policy_path":"/etc/trivy/policy.rego"}`
	if stored["team/api"] != want {
		t.Fatalf("team/api = %s, want %s", stored["team/api"], want)
	}
	if stored["team/web"] != `{"enabled":true,"ignore_file_path":"/etc/trivy/web"}` {
		t.Fatalf("editing team/api changed team/web: %s", stored["team/web"])
	}

	// A path the server rejects is shown and keeps the editor and the typed text.
	m = repoAccessPress(t, m, testKeyTab()) // policy field is focused: go on to Clear and back to the ignore file
	m = ovFocus(t, m, to, overrideFieldPathPrimary)
	for range len(ovEditorOf(t, m, to).pathPrimary) {
		m = repoAccessPress(t, m, testKeyBackspace())
	}
	m = repoAccessPress(t, m, repoAccessType("relative/path")...)
	m = repoAccessPress(t, m, testKeyEnter())
	ed := ovEditorOf(t, m, to)
	if ed.err == "" || !ed.Active() || ed.pathPrimary != "relative/path" {
		t.Fatalf("after a rejected path: err %q, pathPrimary %q", ed.err, ed.pathPrimary)
	}
	if got := ovWirePayloads(t, e, trivyFeatureName)["team/api"]; got != want {
		t.Fatalf("a rejected save changed the stored override: %s", got)
	}

	// Clear: only team/api's trivy override goes.
	m = ovFocus(t, m, to, overrideFieldClear)
	repoAccessPress(t, m, testKeyEnter(), testKeyRunes("y"))
	trivy := ovWirePayloads(t, e, trivyFeatureName)
	if _, still := trivy["team/api"]; still || trivy["team/web"] == "" {
		t.Fatalf("after clearing team/api the trivy overrides are %v, want only team/web's", trivy)
	}
	if gitleaks := ovWirePayloads(t, e, gitleaksFeatureName); gitleaks["team/api"] == "" {
		t.Fatalf("clearing the trivy override removed the gitleaks one of the same repository: %v", gitleaks)
	}
}

// An editor that was only opened must not create an override: for signing that
// would have turned pull-time enforcement off for the repository.
func TestOverrideEditorUntouchedOpenCreatesNothingOverTheRealServer(t *testing.T) {
	t.Parallel()
	k1 := signingWireKey(t)
	cases := []struct {
		name    string
		to      screen
		feature string
		change  func(t *testing.T, m Model) Model
	}{
		{"trivy", screenSecurityTrivyRepos, trivyFeatureName, func(t *testing.T, m Model) Model {
			m = repoAccessPress(t, m, testKeyTab())
			return repoAccessPress(t, m, repoAccessType("/etc/trivy/ignore")...)
		}},
		{"gitleaks", screenSecurityGitleaksRepos, gitleaksFeatureName, func(t *testing.T, m Model) Model {
			m = repoAccessPress(t, m, testKeyTab())
			return repoAccessPress(t, m, repoAccessType("/etc/gitleaks.toml")...)
		}},
		{"signing", screenSecuritySigningRepos, signingFeatureName, func(t *testing.T, m Model) Model {
			m = ovFocus(t, m, screenSecuritySigningRepos, overrideFieldUnsignedSelfRead)
			return repoAccessPress(t, m, testKeySpace())
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := signingWireSetup(t)
			if _, err := e.service.UpdateSigningPolicySettings(e.ctx, ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: []string{k1}}); err != nil {
				t.Fatal(err)
			}
			for _, feature := range []string{trivyFeatureName, gitleaksFeatureName} {
				if _, err := e.service.SetFeatureEnabled(e.ctx, feature, true); err != nil {
					t.Logf("could not enable %s globally: %v", feature, err)
				}
			}
			page, err := e.client.GetFeaturePage(e.ctx, e.session, tc.feature)
			if err != nil {
				t.Fatal(err)
			}
			globalEnabled := page.Summary.Enabled
			if tc.feature == signingFeatureName {
				globalEnabled = true
			}

			m := ovWireOpen(t, e, tc.to, "library/alpine")
			m = repoAccessPress(t, m, testKeyEnter(), testKeyEnter())
			if got := ovWirePayloads(t, e, tc.feature); len(got) != 0 {
				t.Fatalf("an untouched editor created an override: %v", got)
			}

			m = tc.change(t, m)
			m = ovFocus(t, m, tc.to, overrideFieldPathPrimary)
			m = repoAccessPress(t, m, testKeyEnter())
			got := ovWirePayloads(t, e, tc.feature)["library/alpine"]
			if got == "" {
				t.Fatalf("a changed editor saved nothing (err %q)", ovEditorOf(t, m, tc.to).err)
			}
			var stored struct {
				Enabled bool `json:"enabled"`
			}
			if err := json.Unmarshal([]byte(got), &stored); err != nil {
				t.Fatal(err)
			}
			if stored.Enabled != globalEnabled {
				t.Fatalf("the new override has enabled=%v, want the global %v: %s", stored.Enabled, globalEnabled, got)
			}
			if tc.feature == signingFeatureName && !bytes.Contains([]byte(got), []byte("trusted_public_keys")) {
				t.Fatalf("the new signing override lacks the global keys: %s", got)
			}
		})
	}
}
