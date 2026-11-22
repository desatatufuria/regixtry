package tui

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// The per-repository override screens (Image scanning, Secret scanning and
// Signing policy repositories) and their editor have their own golden flag so
// regenerating their goldens can never touch another screen's.
var updateOverridesGoldens = flag.Bool("update-overrides", false, "rewrite the per-repository override golden files")

func assertOverridesGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "overrides", name+".golden")
	if *updateOverridesGoldens {
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
		t.Fatalf("read golden %s (run with -update-overrides): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("rendered view differs from %s (run with -update-overrides to inspect)\n--- want\n%s\n--- got\n%s", path, strings.TrimRight(string(want), "\n"), strings.TrimRight(got, "\n"))
	}
}

// ovClient is the Security fixture with what the override screens read: the
// stored overrides are also what a single-repository read returns, and every
// repository has a latest scan.
func ovClient() *fakeAdminClient {
	c := securityClient()
	c.repositoryOverrideList[signingFeatureName][0].TrustedIdentities = []ports.TrustedIdentity{{CertificateIdentityRegexp: "https://github.com/team/.*", CertificateOIDCIssuer: "https://token.actions.githubusercontent.com"}}
	c.repositoryOverrideList[signingFeatureName][0].UnsignedSelfRead = "pusher"
	c.repositoryOverrides = map[string]ports.RepositoryOverrideDetails{}
	for feature, list := range c.repositoryOverrideList {
		for _, stored := range list {
			c.repositoryOverrides[feature+"/"+stored.Repository] = stored
		}
	}
	scan := func(repo, ref, status string, critical, high, runs int, ago time.Duration) ports.RepositoryScanSummary {
		finished := fixedTestTime.Add(-ago)
		run := ports.ScanRun{ID: "run-" + repo, Repository: repo, RequestedRef: ref, Status: status, Critical: critical, High: high, FinishedAt: &finished, CreatedAt: finished}
		if status == ports.ScanRunStatusFailed {
			run.Error = "trivy: could not download the vulnerability database"
		}
		return ports.RepositoryScanSummary{RunCount: runs, Run: run}
	}
	done, failed := ports.ScanRunStatusCompleted, ports.ScanRunStatusFailed
	c.repositoryScanSummaries = []ports.RepositoryScanSummary{
		scan("team/api", "1.4.2", done, 2, 5, 7, 3*time.Hour), scan("team/web", "stable", failed, 0, 3, 2, 50*time.Hour),
		scan("team/docs", "latest", done, 0, 0, 1, 45*24*time.Hour), scan("team/infra", "v3", done, 1, 0, 4, 400*24*time.Hour),
	}
	return c
}

// ovModelAt opens screen to on a model whose catalog is known.
func ovModelAt(t *testing.T, client AdminClient, known []string, to screen, w, h int) Model {
	t.Helper()
	m := newAdminReadyModelWithCatalog(t, known, client)
	m.now = func() time.Time { return fixedTestTime }
	m = resizeModel(t, m, w, h)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedTestTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	updated, cmd := m.Update(navigateMsg{To: to})
	return securityRun(t, updated.(Model), cmd)
}

func ovTrivyOf(t *testing.T, m Model) trivyReposScreen {
	t.Helper()
	s, ok := m.adminScreens[slotTrivyRepos].(trivyReposScreen)
	if !ok {
		t.Fatalf("slot holds %T, want trivyReposScreen", m.adminScreens[slotTrivyRepos])
	}
	return s
}

func ovFeatureOf(t *testing.T, m Model, slot screenSlot) featureOverridesScreen {
	t.Helper()
	s, ok := m.adminScreens[slot].(featureOverridesScreen)
	if !ok {
		t.Fatalf("slot %d holds %T, want featureOverridesScreen", slot, m.adminScreens[slot])
	}
	return s
}

// ovListOf is the shared list of whichever override screen is mounted.
func ovListOf(t *testing.T, m Model, to screen) overridesList {
	t.Helper()
	switch to {
	case screenSecurityTrivyRepos:
		return ovTrivyOf(t, m).overridesList
	case screenSecurityGitleaksRepos:
		return ovFeatureOf(t, m, slotGitleaksRepos).overridesList
	}
	return ovFeatureOf(t, m, slotSigningRepos).overridesList
}

var ovScreens = []struct {
	name    string
	to      screen
	feature string
	config  screen
}{
	{"image scanning", screenSecurityTrivyRepos, trivyFeatureName, screenSecurityTrivy},
	{"secret scanning", screenSecurityGitleaksRepos, gitleaksFeatureName, screenSecurityGitleaksConfig},
	{"signing policy", screenSecuritySigningRepos, signingFeatureName, screenSecuritySigningConfig},
}

// ovSelect moves the selection to repository with the keys an operator presses.
func ovSelect(t *testing.T, m Model, to screen, repository string) Model {
	t.Helper()
	for range 400 {
		if row, ok := ovListOf(t, m, to).selectedRow(); ok && row.Repository == repository {
			return m
		}
		m = securityPress(t, m, testKeyDown())
	}
	t.Fatalf("could not select %q", repository)
	return m
}

func ovManyRepositories(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("team/service-%03d", i)
	}
	return names
}

// ovKeys are n distinct trusted public keys, the same ones on every run so
// the fingerprints in the goldens never change: each is the P-256 key of a
// private scalar derived from its index.
func ovKeys(t *testing.T, n int) []string {
	t.Helper()
	keys := make([]string, n)
	for i := range keys {
		scalar := sha256.Sum256([]byte(fmt.Sprintf("override-test-key-%d", i)))
		private, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), scalar[:])
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		keys[i] = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	}
	return keys
}

// ---- lists ----

func TestOverridesImageScanningListsEachRepositoryWithItsScanAndOverride(t *testing.T) {
	t.Parallel()
	client := ovClient()
	m := ovModelAt(t, client, securityKnown, screenSecurityTrivyRepos, 150, 24)
	if client.listRepositoryScanSummariesCalls != 1 || client.listRepositoryOverridesCalls != 1 || client.getScanPolicyCalls != 0 {
		t.Fatalf("calls: summaries %d, overrides %d, policy %d, want 1, 1, 0", client.listRepositoryScanSummariesCalls, client.listRepositoryOverridesCalls, client.getScanPolicyCalls)
	}
	out := securityRender(m)
	for _, want := range []string{
		"Admin › Security › Image scanning › Repositories", "Image scanning", "Trivy, vulnerability scanner", "Config", "Repositories",
		"REPOSITORY", "CRIT", "HIGH", "LAST SCAN", "STATUS", "ENABLED", "4 repositories, 2 with an override",
		"team/api", "override", "ok 3 h ago", "team/docs", "no", "team/web", "inherited",
		"OVERRIDE", "Ignore file", "/etc/trivy/ignore", "ACTIONS", "Edit override", "Scan history",
		"tab switch tabs", "↑↓ move", "o override", "enter scan history", "r refresh", "esc back",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	// The server's severity-first order is kept: api (2 critical) before web.
	if strings.Index(out, "team/api") > strings.Index(out, "team/web") || strings.Index(out, "team/web") > strings.Index(out, "team/docs") {
		t.Errorf("rows are not in the scan order:\n%s", out)
	}
	if row, _ := ovTrivyOf(t, m).selectedRow(); row.Repository != "team/api" {
		t.Errorf("selected = %q, want the first row", row.Repository)
	}
	// With room, the panel adds the repository's latest scan.
	tall := securityRender(ovModelAt(t, ovClient(), securityKnown, screenSecurityTrivyRepos, 200, 40))
	for _, want := range []string{"LATEST SCAN", "Reference", "1.4.2", "completed", "14 Mar 06:30"} {
		if !strings.Contains(tall, want) {
			t.Errorf("200x40 view lacks %q:\n%s", want, tall)
		}
	}
}

func TestOverridesFeatureScreensShowEachRepositorysOverrideState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		to    screen
		wants []string
	}{
		{screenSecurityGitleaksRepos, []string{
			"Admin › Security › Secret scanning › Repositories", "Gitleaks, leaked-secret scanner", "STATUS", "DETAIL",
			"team/api", "config path set", "12 Nov", "team/docs", "scanning off", "team/web", "inherited", "global settings",
			"Config path", "/etc/gitleaks.toml", "↑↓ move", "o override", "r refresh", "esc back",
		}},
		{screenSecuritySigningRepos, []string{
			"Admin › Security › Signing policy › Repositories", "image signature verification", "team/api", "1 key, 1 id", "9 Nov",
			"team/docs", "not enforced", "team/web", "global policy", "Pull-time gate", "enforced", "Unsigned self-read", "pusher", "Trusted keys", "Identities",
			"An override replaces the global policy for that repository.",
		}},
	}
	for _, tc := range cases {
		out := securityRender(ovModelAt(t, ovClient(), securityKnown, tc.to, 150, 24))
		for _, want := range tc.wants {
			if !strings.Contains(out, want) {
				t.Errorf("%s: view lacks %q:\n%s", tc.to, want, out)
			}
		}
	}
}

func TestOverridesSelectionMovesAndFollowsItsRepositoryAcrossARefresh(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		known := ovManyRepositories(200)
		client := ovClient()
		m := ovModelAt(t, client, known, sc.to, 150, 24)
		sel := func() string {
			row, _ := ovListOf(t, m, sc.to).selectedRow()
			return row.Repository
		}
		first := sel()
		m = securityPress(t, m, testKeyDown(), testKeyDown(), testKeyRunes("j"), testKeyRunes("k"))
		if got := ovListOf(t, m, sc.to).cursor; got != 2 {
			t.Fatalf("%s: cursor = %d after down, down, j, k, want 2", sc.name, got)
		}
		m = securityPress(t, m, testKeyPgDown())
		if got := ovListOf(t, m, sc.to).cursor; got != 2+overridesPageRows {
			t.Fatalf("%s: cursor = %d after pgdown, want %d", sc.name, got, 2+overridesPageRows)
		}
		m = securityPress(t, m, testKeyEnd())
		last := sel()
		if !strings.Contains(securityRender(m), last) {
			t.Fatalf("%s: the last row %q is not on screen at the end of the list:\n%s", sc.name, last, securityRender(m))
		}
		m = securityPress(t, m, testKeyHome(), testKeyUp())
		if sel() != first {
			t.Fatalf("%s: Home then up left %q selected, want %q", sc.name, sel(), first)
		}
		m = ovSelect(t, m, sc.to, "team/service-150")
		before := client.listRepositoryOverridesCalls
		m = securityPress(t, m, testKeyRunes("r"))
		if client.listRepositoryOverridesCalls != before+1 {
			t.Fatalf("%s: r did not reload the overrides", sc.name)
		}
		if got := sel(); got != "team/service-150" {
			t.Fatalf("%s: selection after refresh = %q, want it to stay on team/service-150", sc.name, got)
		}
	}
}

func TestOverridesOEditsTheSelectedRepositoryOnly(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		client := ovClient()
		m := ovModelAt(t, client, securityKnown, sc.to, 150, 24)
		m = ovSelect(t, m, sc.to, "team/docs")
		m = securityPress(t, m, testKeyRunes("o"))
		l := ovListOf(t, m, sc.to)
		if !l.editor.Active() || l.editor.repository != "team/docs" || l.editor.Feature() != sc.feature {
			t.Fatalf("%s: editor = %+v, want open for team/docs / %s", sc.name, l.editor, sc.feature)
		}
		if client.lastRepositoryOverrideRepo != "team/docs" || client.lastRepositoryOverrideFeature != sc.feature {
			t.Fatalf("%s: the editor read %q/%q, want team/docs/%s", sc.name, client.lastRepositoryOverrideRepo, client.lastRepositoryOverrideFeature, sc.feature)
		}
		if !strings.Contains(securityRender(m), "team/docs") {
			t.Fatalf("%s: the editor does not name the repository:\n%s", sc.name, securityRender(m))
		}
		m = securityPress(t, m, testKeyEsc())
		if ovListOf(t, m, sc.to).editor.Active() || m.screen != sc.to {
			t.Fatalf("%s: Esc must close the editor and stay on the list (screen %q)", sc.name, m.screen)
		}
	}
}

func TestOverridesOIsInertWithoutRepositories(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		client := ovClient()
		client.repositoryOverrideList = map[string][]ports.RepositoryOverrideDetails{}
		client.repositoryScanSummaries = []ports.RepositoryScanSummary{}
		m := ovModelAt(t, client, nil, sc.to, 150, 24)
		if out := securityRender(m); !strings.Contains(out, "No repositories yet.") {
			t.Errorf("%s: empty list does not say so:\n%s", sc.name, out)
		}
		m = securityPress(t, m, testKeyRunes("o"), testKeyEnter(), testKeyDown(), testKeyEnd())
		if ovListOf(t, m, sc.to).editor.Active() || client.getRepositoryOverrideCalls != 0 {
			t.Errorf("%s: o opened an editor with nothing selected", sc.name)
		}
	}
}

func TestOverridesImageScanningEnterOpensTheScanHistoryAndEscComesBackHere(t *testing.T) {
	t.Parallel()
	client := ovClient()
	client.scanRuns = []ports.ScanRun{{ID: "run-web", Repository: "team/web", RequestedRef: "stable", Status: ports.ScanRunStatusCompleted, Critical: 0, High: 3, CreatedAt: securityDay(time.November, 20)}}
	m := ovModelAt(t, client, securityKnown, screenSecurityTrivyRepos, 150, 24)
	m = ovSelect(t, m, screenSecurityTrivyRepos, "team/web")
	m = securityPress(t, m, testKeyEnter())
	scan, ok := m.adminScreens[slotScanHistory].(scanHistoryScreen)
	if !ok || scan.modal.Repository != "team/web" || scan.returnTo != screenSecurityTrivyRepos {
		t.Fatalf("scan history = %+v (%v), want one for team/web returning to the repositories", scan.modal, ok)
	}
	if out := securityRender(m); !strings.Contains(out, "Admin › Security › Image scanning › team/web") {
		t.Fatalf("scan history breadcrumb wrong:\n%s", out)
	}
	m = securityPress(t, m, testKeyEsc())
	if m.adminScreens[slotScanHistory] != nil || m.screen != screenSecurityTrivyRepos {
		t.Fatalf("Esc left screen %q with history %v, want the repositories", m.screen, m.adminScreens[slotScanHistory])
	}
	if row, _ := ovTrivyOf(t, m).selectedRow(); row.Repository != "team/web" {
		t.Fatalf("selected after the history = %q, want team/web kept", row.Repository)
	}

	// The other two screens have no scan history: Enter does nothing.
	for _, to := range []screen{screenSecurityGitleaksRepos, screenSecuritySigningRepos} {
		other := securityPress(t, ovModelAt(t, ovClient(), securityKnown, to, 150, 24), testKeyEnter())
		if other.adminScreens[slotScanHistory] != nil {
			t.Errorf("%s: Enter opened a scan history", to)
		}
	}
}

func TestOverridesNavigationBetweenConfigAndRepositories(t *testing.T) {
	t.Parallel()
	m := securityModelAt(t, ovClient(), screenSecurityTrivy, 150, 24)
	m = securityPress(t, m, testKeyTab())
	if m.screen != screenSecurityTrivyRepos {
		t.Fatalf("Tab on Image scanning reached %q, want the repositories", m.screen)
	}
	m = securityPress(t, m, testKeyTab())
	if m.screen != screenSecurityTrivy {
		t.Fatalf("Tab on the repositories reached %q, want Image scanning", m.screen)
	}
	for _, sc := range ovScreens {
		m = securityModelAt(t, ovClient(), sc.config, 150, 24)
		m = securityPress(t, m, testKeyRunes("o"))
		if m.screen != sc.to {
			t.Fatalf("%s: o on the config reached %q, want %q", sc.name, m.screen, sc.to)
		}
		m = securityPress(t, m, testKeyEsc())
		if m.screen != screenAdminOverview {
			t.Fatalf("%s: Esc on the repositories reached %q, want the Overview", sc.name, m.screen)
		}
	}
}

func TestOverridesLoadingErrorAndRefreshStates(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		// Before the result arrives the list says it is reading.
		m := newAdminReadyModelWithCatalog(t, securityKnown, ovClient())
		m = resizeModel(t, m, 150, 24)
		m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedTestTime.Add(time.Hour)}
		m.adminAuth = adminAuthStateAuthenticated
		updated, _ := m.Update(navigateMsg{To: sc.to})
		if out := securityRender(updated.(Model)); !strings.Contains(out, "Reading repositories…") {
			t.Errorf("%s: loading state missing:\n%s", sc.name, out)
		}

		// A first read that fails shows the error and r tries again.
		client := ovClient()
		client.listRepositoryOverridesErr = errors.New("boom")
		m = ovModelAt(t, client, securityKnown, sc.to, 150, 24)
		out := securityRender(m)
		if !strings.Contains(out, "Could not read the overrides: boom") || !strings.Contains(out, "Press r to try again.") {
			t.Errorf("%s: error state wrong:\n%s", sc.name, out)
		}
		client.listRepositoryOverridesErr = nil
		m = securityPress(t, m, testKeyRunes("r"))
		if out := securityRender(m); strings.Contains(out, "boom") || !strings.Contains(out, "team/api") {
			t.Errorf("%s: r did not recover:\n%s", sc.name, out)
		}

		// A refresh that fails keeps the rows and says so.
		client.listRepositoryOverridesErr = errors.New("flaky")
		m = securityPress(t, m, testKeyRunes("r"))
		out = securityRender(m)
		if !strings.Contains(out, "Could not refresh: flaky") || !strings.Contains(out, "team/api") {
			t.Errorf("%s: a failed refresh must keep the rows and show the failure:\n%s", sc.name, out)
		}
	}
}

func TestOverridesImageScanningKeepsRowsWhenOnlyTheScansFail(t *testing.T) {
	t.Parallel()
	client := ovClient()
	client.featureErr = errors.New("scans unavailable") // ListRepositoryScanSummaries fails
	m := ovModelAt(t, client, securityKnown, screenSecurityTrivyRepos, 150, 24)
	out := securityRender(m)
	for _, want := range []string{"team/api", "team/docs", "Could not read the scans: scans unavailable"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
}

func TestOverridesRefreshIsIgnoredWhileReading(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		m := ovModelAt(t, ovClient(), securityKnown, sc.to, 150, 24)
		updated, first := m.Update(testKeyRunes("r"))
		if first == nil {
			t.Fatalf("%s: r returned no command", sc.name)
		}
		_, second := updated.(Model).Update(testKeyRunes("r"))
		if second != nil {
			t.Errorf("%s: r while a read is in flight started another", sc.name)
		}
	}
}

func TestOverridesExpiredSessionRedirectsToLogin(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		client := ovClient()
		client.listRepositoryOverridesErr = securityExpired()
		if m := ovModelAt(t, client, securityKnown, sc.to, 150, 24); m.screen != screenAdminLogin {
			t.Errorf("%s: list load with an expired session left the screen at %q, want login", sc.name, m.screen)
		}
		client = ovClient()
		client.getRepositoryOverrideErr = securityExpired()
		m := ovModelAt(t, client, securityKnown, sc.to, 150, 24)
		if m = securityPress(t, m, testKeyRunes("o")); m.screen != screenAdminLogin {
			t.Errorf("%s: editor read with an expired session left the screen at %q, want login", sc.name, m.screen)
		}
		client = ovClient()
		m = ovModelAt(t, client, securityKnown, sc.to, 150, 24)
		m = securityPress(t, m, testKeyRunes("o"))
		client.setRepositoryOverrideErr = securityExpired()
		if m = securityPress(t, m, testKeyEnter()); m.screen != screenAdminLogin {
			t.Errorf("%s: save with an expired session left the screen at %q, want login", sc.name, m.screen)
		}
		client = ovClient()
		m = ovModelAt(t, client, securityKnown, sc.to, 150, 24)
		m = securityPress(t, m, testKeyRunes("o"))
		client.clearRepositoryOverrideErr = securityExpired()
		for ovListOf(t, m, sc.to).editor.currentField() != overrideFieldClear {
			m = securityPress(t, m, testKeyTab())
		}
		if m = securityPress(t, m, testKeyEnter(), testKeyRunes("y")); m.screen != screenAdminLogin {
			t.Errorf("%s: clear with an expired session left the screen at %q, want login", sc.name, m.screen)
		}
	}
}

// A result that carries another screen's generation is dropped before it can
// land on the screen or log the operator out.
func TestOverridesDropResultsOfAnotherScreenInstance(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		m := ovModelAt(t, ovClient(), securityKnown, sc.to, 150, 24)
		l := ovListOf(t, m, sc.to)
		foreign := securityResult{gen: l.gen + 1000, err: securityExpired()}
		for _, msg := range []tea.Msg{
			overrideListMsg{securityResult: foreign},
			overrideSummariesMsg{securityResult: foreign},
			overrideLoadedMsg{securityResult: foreign, repository: "team/api", feature: sc.feature},
			overrideSavedMsg{securityResult: foreign, repository: "team/api", feature: sc.feature},
		} {
			updated, _ := m.Update(msg)
			if got := updated.(Model); got.screen != sc.to {
				t.Fatalf("%s: a stale %T logged the operator out (screen %q)", sc.name, msg, got.screen)
			}
		}
		updated, _ := m.Update(overrideListMsg{securityResult: securityResult{gen: l.gen + 1000}, overrides: nil})
		if got := ovListOf(t, updated.(Model), sc.to); len(got.overrides) != len(l.overrides) {
			t.Fatalf("%s: a foreign overrides list replaced the rows", sc.name)
		}

		// A screen replaced by a new one of the same kind ignores the old
		// one's late answers.
		reopened := securityPress(t, m, testKeyEsc())
		reopened = ovModelAtNavigate(t, reopened, sc.to)
		updated, _ = reopened.Update(overrideListMsg{securityResult: securityResult{gen: l.gen}, seq: 0})
		if got := ovListOf(t, updated.(Model), sc.to); len(got.rows) == 0 {
			t.Fatalf("%s: the old screen's late answer emptied the new screen", sc.name)
		}
	}
}

func ovModelAtNavigate(t *testing.T, m Model, to screen) Model {
	t.Helper()
	updated, cmd := m.Update(navigateMsg{To: to})
	return securityRun(t, updated.(Model), cmd)
}

// ---- keys ----

func TestOverridesQuitOnlyWithoutAnEditorAndCtrlCAlways(t *testing.T) {
	t.Parallel()
	isQuit := func(cmd tea.Cmd) bool {
		if cmd == nil {
			return false
		}
		_, ok := cmd().(tea.QuitMsg)
		return ok
	}
	for _, sc := range ovScreens {
		m := ovModelAt(t, ovClient(), securityKnown, sc.to, 150, 24)
		if _, cmd := m.Update(testKeyRunes("q")); !isQuit(cmd) {
			t.Errorf("%s: q on the list must quit", sc.name)
		}
		opened := securityPress(t, m, testKeyRunes("o"))
		if _, cmd := opened.Update(testKeyRunes("q")); isQuit(cmd) {
			t.Errorf("%s: q quit while the editor was open", sc.name)
		}
		if _, cmd := opened.Update(testKeyCtrl('c')); !isQuit(cmd) {
			t.Errorf("%s: ctrl+c must quit with the editor open", sc.name)
		}
		// l logs out only without an editor.
		if _, cmd := opened.Update(testKeyRunes("l")); isQuit(cmd) || opened.screen != sc.to {
			t.Errorf("%s: l acted while the editor was open", sc.name)
		}
		confirm := opened
		for ovListOf(t, confirm, sc.to).editor.currentField() != overrideFieldClear {
			confirm = securityPress(t, confirm, testKeyTab())
		}
		confirm = securityPress(t, confirm, testKeyEnter())
		if !ovListOf(t, confirm, sc.to).editor.confirm.Active() {
			t.Fatalf("%s: the clear confirmation did not open", sc.name)
		}
		if _, cmd := confirm.Update(testKeyRunes("q")); isQuit(cmd) {
			t.Errorf("%s: q quit while the clear confirmation was open", sc.name)
		}
		if _, cmd := confirm.Update(testKeyCtrl('c')); !isQuit(cmd) {
			t.Errorf("%s: ctrl+c must quit with the confirmation open", sc.name)
		}
	}
}

func TestOverridesTextFieldsTakeEveryLetterAndPaste(t *testing.T) {
	t.Parallel()
	m := ovModelAt(t, ovClient(), securityKnown, screenSecurityTrivyRepos, 150, 24)
	m = ovSelect(t, m, screenSecurityTrivyRepos, "team/web")
	m = securityPress(t, m, testKeyRunes("o"), testKeyTab())
	m = securityPress(t, m, securityType("jkql /x")...)
	if got := ovTrivyOf(t, m).editor.pathPrimary; got != "jkql /x" {
		t.Fatalf("ignore file path = %q, want every typed key including j, k, q, l and a space", got)
	}
	m = pasteInto(t, m, "/etc/trivy/ignore\nsecond-line")
	if got := ovTrivyOf(t, m).editor.pathPrimary; got != "jkql /x/etc/trivy/ignoresecond-line" {
		t.Fatalf("ignore file path after a paste = %q, want the text appended without its newline", got)
	}
	if client := m; client.screen != screenSecurityTrivyRepos {
		t.Fatalf("typing or pasting changed the screen to %q", client.screen)
	}
}

func TestOverridesPastedTextIsNeverACommand(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		client := ovClient()
		m := ovModelAt(t, client, securityKnown, sc.to, 150, 24)
		for _, pasted := range []string{"o", "r", "enter", "esc", "tab"} {
			m = pasteInto(t, m, pasted)
		}
		if ovListOf(t, m, sc.to).editor.Active() || client.getRepositoryOverrideCalls != 0 || m.screen != sc.to {
			t.Errorf("%s: pasted text acted as a key on the list (screen %q)", sc.name, m.screen)
		}
		opened := securityPress(t, m, testKeyRunes("o"))
		before := client.setRepositoryOverrideCalls
		for _, pasted := range []string{"x", "n", "enter", "esc", " "} {
			opened = pasteInto(t, opened, pasted)
		}
		l := ovListOf(t, opened, sc.to)
		if !l.editor.Active() || client.setRepositoryOverrideCalls != before || l.editor.confirm.Active() || l.editor.keys.adding || l.editor.keys.usageLoading {
			t.Errorf("%s: pasted text acted as a key in the editor: %+v", sc.name, l.editor)
		}
	}
}

// ---- the latest scan stays in the Trivy list ----

func TestOverridesImageScanningListShowsTheLatestScanResultAtEverySize(t *testing.T) {
	t.Parallel()
	for _, size := range securityConfigSizes {
		out := securityRender(ovModelAt(t, ovClient(), securityKnown, screenSecurityTrivyRepos, size.w, size.h))
		for _, want := range []string{"LAST SCAN", "ok 3 h ago", "failed 2 d ago", "ok 45 d ago", "ok 1 y ago"} {
			if !strings.Contains(out, want) {
				t.Errorf("%dx%d: the list lacks %q:\n%s", size.w, size.h, want, out)
			}
		}
		if strings.Contains(out, " s ago") || strings.Contains(out, "sec") {
			t.Errorf("%dx%d: a scan age in seconds:\n%s", size.w, size.h, out)
		}
	}
	// A repository that was never scanned says so, and so does a running scan.
	c := ovClient()
	c.repositoryScanSummaries = c.repositoryScanSummaries[:2]
	run := c.repositoryScanSummaries[0]
	run.Run.Status, run.Run.FinishedAt = ports.ScanRunStatusRunning, nil
	c.repositoryScanSummaries[0] = run
	out := securityRender(ovModelAt(t, c, securityKnown, screenSecurityTrivyRepos, 150, 24))
	if !strings.Contains(out, "never") || !strings.Contains(out, "running") {
		t.Errorf("never scanned or running not shown:\n%s", out)
	}
}

func TestOverridesImageScanningFailedScanIsDistinctAndPanelKeepsReferenceAndRuns(t *testing.T) {
	t.Parallel()
	m := ovModelAt(t, ovClient(), securityKnown, screenSecurityTrivyRepos, 150, 24)
	m = ovSelect(t, m, screenSecurityTrivyRepos, "team/web")
	// The failed row is drawn in the error colour, not like the others.
	view := m.View().Content
	var failedLine, okLine string
	for _, line := range strings.Split(view, "\n") {
		switch {
		case strings.Contains(line, "failed 2 d ago"):
			failedLine = line
		case strings.Contains(line, "ok 45 d ago"):
			okLine = line
		}
	}
	if failedLine == "" || okLine == "" {
		t.Fatalf("rows not found:\n%s", securityRender(m))
	}
	// Compact panel at 150x24: reference and runs stay.
	out := securityRender(m)
	for _, want := range []string{"LATEST SCAN", "Reference", "stable", "Runs"} {
		if !strings.Contains(out, want) {
			t.Errorf("150x24 panel lacks %q:\n%s", want, out)
		}
	}
	// With room the panel shows the failure itself.
	tall := securityRender(ovSelect(t, ovModelAt(t, ovClient(), securityKnown, screenSecurityTrivyRepos, 200, 40), screenSecurityTrivyRepos, "team/web"))
	for _, want := range []string{"Reference", "Runs", "Error", "could not download"} {
		if !strings.Contains(tall, want) {
			t.Errorf("200x40 panel lacks %q:\n%s", want, tall)
		}
	}
	// A row of the selected repository is not marked by colour alone: another
	// repository's failure is red even when it is not selected.
	other := ovModelAt(t, ovClient(), securityKnown, screenSecurityTrivyRepos, 150, 24)
	var styled string
	for _, line := range strings.Split(other.View().Content, "\n") {
		if strings.Contains(line, "failed 2 d ago") {
			styled = line
		}
	}
	if !strings.Contains(styled, "\x1b[") || styled == "" {
		t.Fatalf("the failed scan is not styled: %q", styled)
	}
	if strings.Contains(styled, "ok 45") {
		t.Fatal("rows share a line")
	}
}

func TestScanAgeIsHoursDaysOrYearsNeverSeconds(t *testing.T) {
	t.Parallel()
	now := fixedTestTime
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{10 * time.Second, "<1 h ago"}, {59 * time.Minute, "<1 h ago"}, {time.Hour, "1 h ago"}, {23 * time.Hour, "23 h ago"},
		{24 * time.Hour, "1 d ago"}, {47 * time.Hour, "1 d ago"}, {364 * 24 * time.Hour, "364 d ago"}, {365 * 24 * time.Hour, "1 y ago"}, {800 * 24 * time.Hour, "2 y ago"},
		{-time.Hour, "<1 h ago"},
	}
	for _, tc := range cases {
		at := now.Add(-tc.ago)
		if got := scanAge(at, now); got != tc.want {
			t.Errorf("scanAge(now-%v) = %q, want %q", tc.ago, got, tc.want)
		}
	}
}

// ---- a list read before a save must not overwrite it ----

// ovListRead returns the command that reads the overrides list after the
// screen was asked to refresh, already holding the answer the server gave
// at the time it ran.
func ovListRead(t *testing.T, m Model, to screen) (Model, tea.Msg) {
	t.Helper()
	updated, cmd := m.Update(testKeyRunes("r"))
	m = updated.(Model)
	for range 3 {
		msg := cmd()
		if _, isSummaries := msg.(overrideSummariesMsg); isSummaries {
			updated, next := m.Update(msg)
			m, cmd = updated.(Model), next
			continue
		}
		return m, msg
	}
	t.Fatal("no overrides read")
	return m, nil
}

func TestOverridesStaleListReadDoesNotOverwriteASavedOrClearedRow(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		// A read that ran before a save, delivered after it.
		client := ovClient()
		m := ovModelAt(t, client, securityKnown, sc.to, 160, 30)
		m = ovSelect(t, m, sc.to, "team/web")
		m, stale := ovListRead(t, m, sc.to)
		m = securityPress(t, m, testKeyRunes("o"))
		m = securityPress(t, m, testKeySpace(), testKeyEnter()) // a new override on team/web
		if !hasOverrideRow(ovListOf(t, m, sc.to), "team/web") {
			t.Fatalf("%s: the save did not show on the row", sc.name)
		}
		updated, _ := m.Update(stale)
		m = updated.(Model)
		if !hasOverrideRow(ovListOf(t, m, sc.to), "team/web") {
			t.Errorf("%s: a list read from before the save showed the saved override as inherited", sc.name)
		}

		// ... and before a clear.
		client = ovClient()
		m = ovModelAt(t, client, securityKnown, sc.to, 160, 30)
		m = ovSelect(t, m, sc.to, "team/api")
		m, stale = ovListRead(t, m, sc.to)
		m = securityPress(t, m, testKeyRunes("o"))
		m = ovFocus(t, m, sc.to, overrideFieldClear)
		m = securityPress(t, m, testKeyEnter(), testKeyRunes("y"))
		if hasOverrideRow(ovListOf(t, m, sc.to), "team/api") {
			t.Fatalf("%s: the clear did not show on the row", sc.name)
		}
		updated, _ = m.Update(stale)
		m = updated.(Model)
		if hasOverrideRow(ovListOf(t, m, sc.to), "team/api") {
			t.Errorf("%s: a list read from before the clear showed the cleared override as overridden", sc.name)
		}

		// A read that started after the change is taken.
		client = ovClient()
		m = ovModelAt(t, client, securityKnown, sc.to, 160, 30)
		m = ovSelect(t, m, sc.to, "team/api")
		m = securityPress(t, m, testKeyRunes("o"))
		m = ovFocus(t, m, sc.to, overrideFieldClear)
		m = securityPress(t, m, testKeyEnter(), testKeyRunes("y"), testKeyEsc())
		client.repositoryOverrideList[sc.feature] = nil
		m = securityPress(t, m, testKeyRunes("r"))
		if hasOverrideRow(ovListOf(t, m, sc.to), "team/docs") {
			t.Errorf("%s: a fresh read was not taken", sc.name)
		}
	}
}
