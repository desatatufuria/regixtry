package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"regixtry/internal/ports"
)

// ovStates are the states of the three screens drawn at every size and, for
// the ones with a golden name, written as goldens at 150x24 and 200x40.
type ovState struct {
	name   string
	golden string
	build  func(t *testing.T, w, h int) Model
}

func ovLongNames() []string {
	return []string{
		"équipe-très-longue/service-de-facturación-中文-émoji-🚀-with-an-extremely-long-name-that-cannot-fit-in-the-column",
		"team/api",
		"组织/仓库-名称-很长很长很长很长很长很长很长很长很长很长很长很长很长",
		"team/web",
	}
}

func ovListStates(prefix, slug string, to screen, feature string) []ovState {
	return []ovState{
		{prefix + " rows", slug + "_rows", func(t *testing.T, w, h int) Model {
			return ovModelAt(t, ovClient(), securityKnown, to, w, h)
		}},
		{prefix + " second row selected", "", func(t *testing.T, w, h int) Model {
			return securityPress(t, ovModelAt(t, ovClient(), securityKnown, to, w, h), testKeyDown())
		}},
		{prefix + " empty", slug + "_empty", func(t *testing.T, w, h int) Model {
			c := ovClient()
			c.repositoryOverrideList = map[string][]ports.RepositoryOverrideDetails{}
			c.repositoryScanSummaries = []ports.RepositoryScanSummary{}
			return ovModelAt(t, c, nil, to, w, h)
		}},
		{prefix + " error", slug + "_error", func(t *testing.T, w, h int) Model {
			c := ovClient()
			c.listRepositoryOverridesErr = fmt.Errorf("the admin API is unreachable")
			return ovModelAt(t, c, securityKnown, to, w, h)
		}},
		{prefix + " refresh failed", "", func(t *testing.T, w, h int) Model {
			c := ovClient()
			m := ovModelAt(t, c, securityKnown, to, w, h)
			c.listRepositoryOverridesErr = fmt.Errorf("flaky network")
			return securityPress(t, m, testKeyRunes("r"))
		}},
		{prefix + " loading", "", func(t *testing.T, w, h int) Model {
			m := newAdminReadyModelWithCatalog(t, securityKnown, ovClient())
			m = resizeModel(t, m, w, h)
			m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedTestTime.Add(24 * 3600e9)}
			m.adminAuth = adminAuthStateAuthenticated
			updated, _ := m.Update(navigateMsg{To: to})
			return updated.(Model)
		}},
		{prefix + " 200 repositories at the end", slug + "_many", func(t *testing.T, w, h int) Model {
			m := ovModelAt(t, ovClient(), ovManyRepositories(200), to, w, h)
			return securityPress(t, m, testKeyEnd())
		}},
		{prefix + " long and multibyte names", "", func(t *testing.T, w, h int) Model {
			c := ovClient()
			names := ovLongNames()
			c.repositoryOverrideList[feature] = append(c.repositoryOverrideList[feature], ports.RepositoryOverrideDetails{Repository: names[0], Feature: feature, Enabled: true, TrustedPublicKeys: ovKeys(t, 3), IgnoreFilePath: "/very/long/path/" + strings.Repeat("segment/", 12) + "ignore", ConfigPath: "/etc/" + strings.Repeat("deep/", 20) + "gitleaks.toml"})
			c.repositoryScanSummaries = append(c.repositoryScanSummaries, ovClient().repositoryScanSummaries[0])
			c.repositoryScanSummaries[len(c.repositoryScanSummaries)-1].Run.Repository = names[2]
			m := ovModelAt(t, c, names, to, w, h)
			return ovSelect(t, m, to, names[0])
		}},
	}
}

func ovEditorState(prefix, golden string, to screen, repository string, prepare func(t *testing.T, c *fakeAdminClient), then func(t *testing.T, m Model) Model) ovState {
	return ovState{prefix, golden, func(t *testing.T, w, h int) Model {
		c := ovClient()
		if prepare != nil {
			prepare(t, c)
		}
		m := ovModelAt(t, c, securityKnown, to, w, h)
		m = ovSelect(t, m, to, repository)
		m = securityPress(t, m, testKeyRunes("o"))
		if then != nil {
			m = then(t, m)
		}
		return m
	}}
}

func ovToClear(t *testing.T, m Model, to screen) Model {
	t.Helper()
	return ovFocus(t, m, to, overrideFieldClear)
}

func ovEditorStates() []ovState {
	trivy, gitleaks, signing := screenSecurityTrivyRepos, screenSecurityGitleaksRepos, screenSecuritySigningRepos
	rejectSave := func(t *testing.T, c *fakeAdminClient) {
		c.setRepositoryOverrideErr = fmt.Errorf("ignore_file_path must be an absolute path")
	}
	manyKeys := func(t *testing.T, c *fakeAdminClient) {
		stored := c.repositoryOverrides["signing/team/api"]
		stored.TrustedPublicKeys = ovKeys(t, 100)
		c.repositoryOverrides["signing/team/api"] = stored
	}
	return []ovState{
		ovEditorState("image scanning editor", "trivy_editor", trivy, "team/api", nil, nil),
		ovEditorState("image scanning editor new override", "", trivy, "team/web", nil, nil),
		ovEditorState("image scanning editor typing a long path", "", trivy, "team/api", nil, func(t *testing.T, m Model) Model {
			m = securityPress(t, m, testKeyTab())
			return securityPress(t, m, securityType(strings.Repeat("/long-directory-name", 10)+"/é中文")...)
		}),
		ovEditorState("image scanning editor save rejected", "trivy_editor_error", trivy, "team/api", rejectSave, func(t *testing.T, m Model) Model {
			return securityPress(t, m, testKeyEnter())
		}),
		ovEditorState("image scanning editor saved", "", trivy, "team/web", nil, func(t *testing.T, m Model) Model {
			return securityPress(t, m, testKeySpace(), testKeyEnter())
		}),
		ovEditorState("image scanning editor read failed", "", trivy, "team/api", func(t *testing.T, c *fakeAdminClient) {
			c.getRepositoryOverrideErr = fmt.Errorf("cannot read the override")
		}, nil),
		ovEditorState("image scanning editor clear confirmation", "trivy_editor_clear", trivy, "team/api", nil, func(t *testing.T, m Model) Model {
			return securityPress(t, ovToClear(t, m, trivy), testKeyEnter())
		}),
		ovEditorState("image scanning editor on Clear", "", trivy, "team/api", nil, func(t *testing.T, m Model) Model { return ovToClear(t, m, trivy) }),
		ovEditorState("secret scanning editor", "gitleaks_editor", gitleaks, "team/api", nil, nil),
		ovEditorState("secret scanning editor clear confirmation", "", gitleaks, "team/api", nil, func(t *testing.T, m Model) Model {
			return securityPress(t, ovToClear(t, m, gitleaks), testKeyEnter())
		}),
		ovEditorState("signing editor", "signing_editor", signing, "team/api", nil, nil),
		ovEditorState("signing editor new override seeded with global keys", "", signing, "team/web", nil, nil),
		ovEditorState("signing editor with 100 keys", "signing_editor_many_keys", signing, "team/api", manyKeys, nil),
		ovEditorState("signing editor with 100 keys on the last field", "", signing, "team/api", manyKeys, func(t *testing.T, m Model) Model {
			return ovToClear(t, m, signing)
		}),
		ovEditorState("signing editor adding a key", "signing_editor_adding_key", signing, "team/api", nil, func(t *testing.T, m Model) Model {
			m = securityPress(t, m, testKeyRunes("n"))
			return pasteInto(t, m, trustedKeyListTestPEM2)
		}),
		ovEditorState("signing editor deleting a key", "", signing, "team/api", func(t *testing.T, c *fakeAdminClient) { c.signingKeyUsageCount = 12 }, func(t *testing.T, m Model) Model {
			return securityPress(t, m, testKeyRunes("x"))
		}),
		ovEditorState("signing editor adding an identity", "", signing, "team/api", nil, func(t *testing.T, m Model) Model {
			m = ovFocus(t, m, signing, overrideFieldIdentities)
			m = securityPress(t, m, testKeyRunes("n"))
			return securityPress(t, m, securityType("^dev@example.com$")...)
		}),
		ovEditorState("signing editor clear confirmation", "signing_editor_clear", signing, "team/api", nil, func(t *testing.T, m Model) Model {
			return securityPress(t, ovToClear(t, m, signing), testKeyEnter())
		}),
		ovEditorState("signing editor save rejected", "signing_editor_error", signing, "team/api", func(t *testing.T, c *fakeAdminClient) {
			c.setRepositoryOverrideErr = fmt.Errorf("enabled requires at least one usable entry in trusted_public_keys or trusted_identities")
		}, func(t *testing.T, m Model) Model { return securityPress(t, m, testKeyEnter()) }),
	}
}

func ovAllStates() []ovState {
	var all []ovState
	all = append(all, ovListStates("image scanning", "trivy", screenSecurityTrivyRepos, trivyFeatureName)...)
	all = append(all, ovListStates("secret scanning", "gitleaks", screenSecurityGitleaksRepos, gitleaksFeatureName)...)
	all = append(all, ovListStates("signing policy", "signing", screenSecuritySigningRepos, signingFeatureName)...)
	all = append(all, ovEditorStates()...)
	return all
}

// Every state fits every supported terminal size: no line wider than the
// terminal, no more rows than its height, exactly the shell's height.
func TestOverridesViewsFitEverySizeInEveryState(t *testing.T) {
	t.Parallel()
	for _, state := range ovAllStates() {
		for _, size := range securityConfigSizes {
			label := fmt.Sprintf("%s at %dx%d", state.name, size.w, size.h)
			m := state.build(t, size.w, size.h)
			view := m.View().Content
			securityNoOverflow(t, label, view, size.w, size.h)
			if rows := len(strings.Split(strings.TrimRight(view, "\n"), "\n")); rows != size.h {
				t.Errorf("%s: %d rows, want exactly %d", label, rows, size.h)
			}
		}
	}
}

// The selected row stays on screen however long the list is.
func TestOverridesSelectionStaysVisibleInAllSizes(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		for _, size := range securityConfigSizes {
			m := ovModelAt(t, ovClient(), ovManyRepositories(200), sc.to, size.w, size.h)
			for step := 0; step < 200; step += 7 {
				row, _ := ovListOf(t, m, sc.to).selectedRow()
				if !strings.Contains(securityRender(m), "▸ "+row.Repository) {
					t.Fatalf("%s at %dx%d: the selected row %q is not on screen:\n%s", sc.name, size.w, size.h, row.Repository, securityRender(m))
				}
				m = securityPress(t, m, testKeyRunes("j"), testKeyRunes("j"), testKeyRunes("j"), testKeyRunes("j"), testKeyRunes("j"), testKeyRunes("j"), testKeyRunes("j"))
			}
		}
	}
}

// The focused line of a form taller than the terminal stays on screen.
func TestOverridesEditorKeepsTheFocusedFieldVisibleWithManyKeys(t *testing.T) {
	t.Parallel()
	to := screenSecuritySigningRepos
	for _, size := range securityConfigSizes {
		c := ovClient()
		stored := c.repositoryOverrides["signing/team/api"]
		stored.TrustedPublicKeys = ovKeys(t, 100)
		c.repositoryOverrides["signing/team/api"] = stored
		m := ovModelAt(t, c, securityKnown, to, size.w, size.h)
		m = securityPress(t, m, testKeyRunes("o"))
		for range 90 {
			m = securityPress(t, m, testKeyDown())
		}
		if e := ovEditorOf(t, m, to); e.keys.selected != 90 {
			t.Fatalf("%dx%d: selected key = %d, want 90", size.w, size.h, e.keys.selected)
		}
		if !regexp.MustCompile(`▸\s+Key: `).MatchString(securityRender(m)) {
			t.Errorf("%dx%d: the selected key is not on screen:\n%s", size.w, size.h, securityRender(m))
		}
		m = ovToClear(t, m, to)
		if !strings.Contains(securityRender(m), "Clear override, use the global settings") {
			t.Errorf("%dx%d: the Clear row is not on screen when focused:\n%s", size.w, size.h, securityRender(m))
		}
	}
}

func TestOverridesGoldens(t *testing.T) {
	t.Parallel()
	for _, state := range ovAllStates() {
		if state.golden == "" {
			continue
		}
		for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
			m := state.build(t, size.w, size.h)
			assertOverridesGolden(t, fmt.Sprintf("%s_%dx%d", state.golden, size.w, size.h), securityRender(m)+"\n")
		}
	}
}
