package tui

import (
	"errors"
	"flag"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"regixtry/internal/ports"
)

// The Security configuration screens have their own golden flag so
// regenerating their goldens can never touch another screen's.
var updateSecurityConfigGoldens = flag.Bool("update-security-config", false, "rewrite the security configuration golden files")

var securityConfigSizes = []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}}

var securityKnown = []string{"team/api", "team/docs", "team/web", "team/infra"}

func securityDay(month time.Month, day int) time.Time {
	return time.Date(2026, month, day, 10, 14, 0, 0, time.UTC)
}

func securityRuntimeSection() ports.FeatureSection {
	return ports.FeatureSection{ID: "runtime", Title: "Runtime", Kind: "fields", Fields: []ports.FeatureField{
		{Label: "Status", Value: "ready"}, {Label: "Health", Value: "healthy"}, {Label: "Version", Value: "0.57.1"},
		{Label: "Latest Version", Value: "0.58.0"}, {Label: "Update Status", Value: "available"}, {Label: "Rollback Available", Value: "true"},
	}}
}

func securityActions(enabled bool) []ports.FeatureAction {
	actions := []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}}
	if enabled {
		actions = append(actions, ports.FeatureAction{ID: "disable", Label: "Disable", ConfirmTitle: "Confirm Disable", ConfirmMessage: "Confirm disable feature?"})
	} else {
		actions = append(actions, ports.FeatureAction{ID: "enable", Label: "Enable", ConfirmTitle: "Confirm Enable", ConfirmMessage: "Confirm enable feature?"})
	}
	return append(actions, ports.FeatureAction{ID: "upgrade-runtime", Label: "Upgrade Runtime"})
}

func securityTrivyPage() ports.FeaturePage {
	return ports.FeaturePage{
		Summary: ports.FeatureSummary{Name: trivyFeatureName, Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
		Sections: []ports.FeatureSection{{ID: "config", Title: "Configuration", Kind: "fields", Fields: []ports.FeatureField{
			{Label: "Schedule Enabled", Value: "true"}, {Label: "Interval", Value: "24h0m0s"}, {Label: "Timeout", Value: "5m0s"},
			{Label: "Registry Reachable URL", Value: "http://regixtry:5000"}, {Label: "Max Concurrency", Value: "2"},
		}}, securityRuntimeSection()},
		Actions: securityActions(true),
	}
}

func securityGitleaksPage() ports.FeaturePage {
	return ports.FeaturePage{
		Summary: ports.FeatureSummary{Name: gitleaksFeatureName, Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
		Header:  []ports.FeatureField{{Label: "Enabled", Value: "true"}},
		Sections: []ports.FeatureSection{{ID: "config", Title: "Configuration", Kind: "fields", Fields: []ports.FeatureField{
			{Label: "Timeout", Value: "2m0s"}, {Label: "Max Concurrency", Value: "2"},
		}}, securityRuntimeSection()},
		Actions: securityActions(true),
	}
}

func securitySigningPage() ports.FeaturePage {
	return ports.FeaturePage{
		Summary:  ports.FeatureSummary{Name: signingFeatureName, Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
		Sections: []ports.FeatureSection{{ID: "policy", Title: "Policy", Kind: "fields", Fields: []ports.FeatureField{{Label: "Enabled", Value: "true"}}}},
		Actions:  securityActions(true)[:2],
	}
}

func securityClient() *fakeAdminClient {
	return &fakeAdminClient{
		featurePages: map[string]ports.FeaturePage{
			trivyFeatureName:    securityTrivyPage(),
			gitleaksFeatureName: securityGitleaksPage(),
			signingFeatureName:  securitySigningPage(),
		},
		scanPolicy:    ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCriticalHigh, UpdatedAt: securityDay(time.November, 2)},
		signingPolicy: ports.SigningPolicySettings{Enabled: true, UnsignedSelfRead: "pusher", TrustedPublicKeys: []string{trustedKeyListTestPEM1, trustedKeyListTestPEM2}, UpdatedAt: securityDay(time.November, 9), TrustedIdentities: []ports.TrustedIdentity{{CertificateIdentityRegexp: "https://github.com/team/.*", CertificateOIDCIssuer: "https://token.actions.githubusercontent.com"}}},
		repositoryOverrideList: map[string][]ports.RepositoryOverrideDetails{
			trivyFeatureName: {
				{Repository: "team/docs", Feature: trivyFeatureName, Enabled: false, UpdatedAt: securityDay(time.November, 2)},
				{Repository: "team/api", Feature: trivyFeatureName, Enabled: true, IgnoreFilePath: "/etc/trivy/ignore", UpdatedAt: securityDay(time.November, 14)},
			},
			gitleaksFeatureName: {
				{Repository: "team/api", Feature: gitleaksFeatureName, Enabled: true, ConfigPath: "/etc/gitleaks.toml", UpdatedAt: securityDay(time.November, 12)},
				{Repository: "team/docs", Feature: gitleaksFeatureName, Enabled: false, UpdatedAt: securityDay(time.November, 2)},
			},
			signingFeatureName: {
				{Repository: "team/api", Feature: signingFeatureName, Enabled: true, TrustedPublicKeys: []string{trustedKeyListTestPEM1}, UpdatedAt: securityDay(time.November, 9)},
				{Repository: "team/docs", Feature: signingFeatureName, Enabled: false, UpdatedAt: securityDay(time.November, 2)},
			},
		},
	}
}

// securityRun runs cmd and everything it triggers, flattening batches the way
// the Bubble Tea runtime does.
func securityRun(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			m = securityRun(t, m, sub)
		}
		return m
	}
	updated, next := m.Update(msg)
	return securityRun(t, updated.(Model), next)
}

func securityModelAt(t *testing.T, client AdminClient, to screen, w, h int) Model {
	t.Helper()
	m := newAdminReadyModelWithCatalog(t, securityKnown, client)
	m.now = func() time.Time { return charFixedTime }
	m = charResize(t, m, w, h)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: charFixedTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	updated, cmd := m.Update(navigateMsg{To: to})
	return securityRun(t, updated.(Model), cmd)
}

func securityPress(t *testing.T, m Model, keys ...keyMsg) Model {
	t.Helper()
	for _, k := range keys {
		updated, cmd := m.Update(k)
		m = securityRun(t, updated.(Model), cmd)
	}
	return m
}

func securityType(text string) []keyMsg {
	keys := make([]keyMsg, 0, len(text))
	for _, r := range text {
		keys = append(keys, testKeyRunes(string(r)))
	}
	return keys
}

func securityRender(m Model) string { return ansi.Strip(m.View().Content) }

func securityNoOverflow(t *testing.T, label, view string, w, h int) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if len(lines) > h {
		t.Errorf("%s: %d rows, want at most %d\n%s", label, len(lines), h, ansi.Strip(view))
	}
	for i, line := range lines {
		if width := ansi.StringWidth(line); width > w {
			t.Errorf("%s: row %d is %d columns, want at most %d: %q", label, i, width, w, ansi.Strip(line))
		}
	}
}

func securityTrivyOf(t *testing.T, m Model) trivyConfigScreen {
	t.Helper()
	s, ok := m.adminScreens[slotTrivyConfig].(trivyConfigScreen)
	if !ok {
		t.Fatalf("slot holds %T, want trivyConfigScreen", m.adminScreens[slotTrivyConfig])
	}
	return s
}

func securityGitleaksOf(t *testing.T, m Model) gitleaksConfigScreen {
	t.Helper()
	s, ok := m.adminScreens[slotGitleaksConfig].(gitleaksConfigScreen)
	if !ok {
		t.Fatalf("slot holds %T, want gitleaksConfigScreen", m.adminScreens[slotGitleaksConfig])
	}
	return s
}

func securitySigningOf(t *testing.T, m Model) signingConfigScreen {
	t.Helper()
	s, ok := m.adminScreens[slotSigningConfig].(signingConfigScreen)
	if !ok {
		t.Fatalf("slot holds %T, want signingConfigScreen", m.adminScreens[slotSigningConfig])
	}
	return s
}

// securityBackspaces deletes n characters of the focused field.
func securityBackspaces(n int) []keyMsg {
	keys := make([]keyMsg, n)
	for i := range keys {
		keys[i] = testKeyBackspace()
	}
	return keys
}

func securityExpired() error {
	return NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
}

// ---- Image scanning ----

func TestSecurityTrivyScreenLoadsThePageThePolicyAndTheOverrides(t *testing.T) {
	t.Parallel()
	client := securityClient()
	m := securityModelAt(t, client, screenSecurityTrivy, 150, 24)
	if client.getFeaturePageCalls != 1 || client.getScanPolicyCalls != 1 || client.listRepositoryOverridesCalls != 1 {
		t.Fatalf("calls: page %d, policy %d, overrides %d, want one each", client.getFeaturePageCalls, client.getScanPolicyCalls, client.listRepositoryOverridesCalls)
	}
	out := securityRender(m)
	for _, want := range []string{
		"Admin › Security › Image scanning", "Image scanning", "Trivy, vulnerability scanner", "Config", "Repositories",
		"TRIVY CONFIGURATION", "[x] enabled", "24h", "5m", "http://regixtry:5000", "SCAN POLICY", "CRITICAL+HIGH",
		"PER-REPOSITORY OVERRIDES", "team/api", "ignore file set", "team/docs", "scanning off", "14 Nov",
		"tab switch tabs", "c configure", "p policy", "o overrides", "r refresh", "esc back",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "24h0m0s") || strings.Contains(out, "Feature Page") {
		t.Errorf("view shows the old raw duration or page dump:\n%s", out)
	}
	if rows := strings.Index(out, "team/api"); rows > strings.Index(out, "team/web") && strings.Contains(out, "team/web") {
		t.Errorf("override rows must come before inherited ones:\n%s", out)
	}
}

func TestSecurityTrivyConfigModalEditsAndSavesOnce(t *testing.T) {
	t.Parallel()
	client := securityClient()
	m := securityModelAt(t, client, screenSecurityTrivy, 150, 24)
	m = securityPress(t, m, testKeyRunes("c"))
	s := securityTrivyOf(t, m)
	if want := (trivyConfigModal{Open: true, Focus: trivyConfigFieldScheduleEnabled, ScheduleEnabled: true, Interval: "24h", Timeout: "5m", RegistryReachableURL: "http://regixtry:5000", MaxConcurrency: "2"}); s.cfg != want {
		t.Fatalf("cfg = %#v, want %#v", s.cfg, want)
	}

	// Space toggles the switch; the text fields take j, k, q and every letter.
	m = securityPress(t, m, testKeySpace(), testKeyTab())
	m = securityPress(t, m, securityType("jkq")...)
	if got := securityTrivyOf(t, m).cfg.Interval; got != "24hjkq" {
		t.Fatalf("Interval = %q, want j, k and q typed", got)
	}
	m = securityPress(t, m, securityBackspaces(6)...)
	m = securityPress(t, m, securityType("12h")...)
	if got := securityTrivyOf(t, m).cfg; got.Interval != "12h" || got.ScheduleEnabled {
		t.Fatalf("cfg = %#v, want Interval 12h and the schedule off", got)
	}

	// Enter saves once: a second Enter while the first is in flight does nothing.
	updated, cmd := m.Update(testKeyEnter())
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("Enter returned no command")
	}
	updated, again := m.Update(testKeyEnter())
	m = updated.(Model)
	if again != nil {
		t.Fatal("Enter while the save is in flight returned a command")
	}
	if !strings.Contains(securityRender(m), "Saving…") {
		t.Fatalf("modal does not show it is saving:\n%s", securityRender(m))
	}
	m = securityRun(t, m, cmd)

	if client.configureFeatureCalls != 1 || client.lastConfiguredFeature != trivyFeatureName {
		t.Fatalf("ConfigureFeature calls = %d for %q, want one for trivy", client.configureFeatureCalls, client.lastConfiguredFeature)
	}
	in := client.lastConfigureInput
	if in.Interval == nil || *in.Interval != 12*time.Hour || in.ScheduleEnabled == nil || *in.ScheduleEnabled || in.Timeout == nil || *in.Timeout != 5*time.Minute ||
		in.MaxConcurrency == nil || *in.MaxConcurrency != 2 || in.RegistryReachableURL == nil || *in.RegistryReachableURL != "http://regixtry:5000" {
		t.Fatalf("lastConfigureInput = %#v", in)
	}
	s = securityTrivyOf(t, m)
	if s.cfg.Active() || s.saving {
		t.Fatalf("modal open = %v, saving = %v after the save, want both false", s.cfg.Active(), s.saving)
	}
	if out := securityRender(m); !strings.Contains(out, "Configuration saved.") {
		t.Fatalf("view lacks the saved note:\n%s", out)
	}
	if client.getFeaturePageCalls != 2 {
		t.Fatalf("page reads = %d, want a reload after the save", client.getFeaturePageCalls)
	}
}

func TestSecurityTrivyConfigModalValidationAndFailureStayInTheModal(t *testing.T) {
	t.Parallel()
	client := securityClient()
	m := securityModelAt(t, client, screenSecurityTrivy, 150, 24)
	m = securityPress(t, m, testKeyRunes("c"), testKeyTab(), testKeyRunes("x"), testKeyEnter())
	if client.configureFeatureCalls != 0 || !strings.Contains(securityRender(m), "invalid interval") {
		t.Fatalf("calls = %d, view:\n%s\nwant an invalid interval error and no call", client.configureFeatureCalls, securityRender(m))
	}

	m = securityPress(t, m, testKeyBackspace(), testKeyEnter())
	if client.configureFeatureCalls != 1 {
		t.Fatalf("calls = %d, want the corrected value saved", client.configureFeatureCalls)
	}

	client.featureErr = errors.New("boom")
	m = securityPress(t, m, testKeyRunes("c"), testKeyEnter())
	if !securityTrivyOf(t, m).cfg.Active() || !strings.Contains(securityRender(m), "boom") {
		t.Fatalf("a failed save must keep the modal open and show the error:\n%s", securityRender(m))
	}
	m = securityPress(t, m, testKeyEnter())
	if client.configureFeatureCalls != 3 {
		t.Fatalf("calls = %d, want Enter to work again once the failure arrived", client.configureFeatureCalls)
	}
}

func TestSecurityTrivyConfigModalPasteAndEscape(t *testing.T) {
	t.Parallel()
	client := securityClient()
	m := securityModelAt(t, client, screenSecurityTrivy, 150, 24)
	m = securityPress(t, m, testKeyRunes("c"), testKeyTab(), testKeyTab(), testKeyTab())
	updated, _ := m.Update(tea.PasteMsg{Content: "https://x.example\n/path"})
	m = updated.(Model)
	if got := securityTrivyOf(t, m).cfg.RegistryReachableURL; got != "http://regixtry:5000https://x.example/path" {
		t.Fatalf("pasted URL = %q, want the text appended without the newline", got)
	}
	m = securityPress(t, m, testKeyEsc())
	if securityTrivyOf(t, m).cfg.Active() || client.configureFeatureCalls != 0 {
		t.Fatal("Esc must close the modal without saving")
	}
}

func TestSecurityTrivyPolicyModalNeedsTheLoadedPolicyAndSavesOnce(t *testing.T) {
	t.Parallel()
	client := securityClient()
	client.scanPolicyErr = errors.New("policy down")
	m := securityModelAt(t, client, screenSecurityTrivy, 150, 24)
	m = securityPress(t, m, testKeyRunes("p"))
	if securityTrivyOf(t, m).policyModal.Active() || !strings.Contains(securityRender(m), "has not loaded") {
		t.Fatalf("p must not open the policy editor on a policy that never loaded:\n%s", securityRender(m))
	}
	if !strings.Contains(securityRender(m), "Could not read the policy: policy down") {
		t.Fatalf("the policy card must say why it is empty:\n%s", securityRender(m))
	}

	client.scanPolicyErr = nil
	m = securityPress(t, m, testKeyRunes("r"), testKeyRunes("p"))
	if !securityTrivyOf(t, m).policyModal.Active() {
		t.Fatal("p did not open the policy modal once the policy loaded")
	}
	m = securityPress(t, m, testKeySpace(), testKeyTab(), testKeySpace())
	updated, cmd := m.Update(testKeyEnter())
	m = updated.(Model)
	if _, again := m.Update(testKeyEnter()); again != nil {
		t.Fatal("Enter while the policy save is in flight returned a command")
	}
	m = securityRun(t, m, cmd)
	if client.updateScanPolicyCalls != 1 || client.lastScanPolicyInput.Enabled || client.lastScanPolicyInput.SeverityThreshold != ports.ScanPolicyThresholdCritical {
		t.Fatalf("UpdateScanPolicy calls = %d, input %#v, want one with the gate off and the threshold cycled", client.updateScanPolicyCalls, client.lastScanPolicyInput)
	}
	if securityTrivyOf(t, m).policyModal.Active() || !strings.Contains(securityRender(m), "Vulnerability policy saved.") {
		t.Fatalf("view after the save:\n%s", securityRender(m))
	}

	client.scanPolicyUpdateErr = errors.New("denied")
	m = securityPress(t, m, testKeyRunes("p"), testKeyEnter())
	if !securityTrivyOf(t, m).policyModal.Active() || !strings.Contains(securityRender(m), "denied") {
		t.Fatalf("a failed policy save must stay in the modal:\n%s", securityRender(m))
	}
}

func TestSecurityTrivyActionsConfirmAndIgnoreRepeats(t *testing.T) {
	t.Parallel()
	client := securityClient()
	m := securityModelAt(t, client, screenSecurityTrivy, 150, 24)

	// x opens the confirm; Enter confirms once, a second Enter is ignored.
	m = securityPress(t, m, testKeyRunes("x"))
	if !securityTrivyOf(t, m).confirm.Active() || !strings.Contains(securityRender(m), "Confirm disable feature?") {
		t.Fatalf("x did not open the disable confirm:\n%s", securityRender(m))
	}
	updated, cmd := m.Update(testKeyEnter())
	m = updated.(Model)
	if _, again := m.Update(testKeyEnter()); again != nil {
		t.Fatal("Enter while the action is in flight returned a command")
	}
	m = securityRun(t, m, cmd)
	if client.executeFeatureActionCalls != 1 || client.lastFeatureAction != "disable" {
		t.Fatalf("actions = %d (%q), want one disable", client.executeFeatureActionCalls, client.lastFeatureAction)
	}
	if s := securityTrivyOf(t, m); s.confirm.Active() || s.acting {
		t.Fatal("the confirm and the in-flight state must clear when the result arrives")
	}

	// u upgrades without a confirm; a second u before the result is ignored.
	updated, cmd = m.Update(testKeyRunes("u"))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("u returned no command")
	}
	if _, again := m.Update(testKeyRunes("u")); again != nil {
		t.Fatal("a second u while the action is in flight returned a command")
	}
	m = securityRun(t, m, cmd)
	if client.executeFeatureActionCalls != 2 || client.lastFeatureAction != "upgrade-runtime" {
		t.Fatalf("actions = %d (%q), want the upgrade as the second", client.executeFeatureActionCalls, client.lastFeatureAction)
	}

	// Esc closes the confirm without running anything.
	m = securityPress(t, m, testKeyRunes("x"), testKeyEsc())
	if securityTrivyOf(t, m).confirm.Active() || client.executeFeatureActionCalls != 2 {
		t.Fatal("Esc must close the confirm and run nothing")
	}

	client.featureErr = errors.New("nope")
	m = securityPress(t, m, testKeyRunes("u"))
	if !strings.Contains(securityRender(m), "nope") || securityTrivyOf(t, m).acting {
		t.Fatalf("a failed action must show its error and unlock:\n%s", securityRender(m))
	}
}

func TestSecurityTrivyNavigation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		key  keyMsg
		want screen
	}{
		{testKeyTab(), screenSecurityTrivyRepos},
		{testKeyRunes("o"), screenSecurityTrivyRepos},
		{testKeyEsc(), screenAdminOverview},
	} {
		m := securityModelAt(t, securityClient(), screenSecurityTrivy, 150, 24)
		m = securityPress(t, m, tc.key)
		if m.screen != tc.want {
			t.Errorf("after %v the screen is %q, want %q", tc.key, m.screen, tc.want)
		}
	}
}

func TestSecurityTrivyQuitsOnlyWithoutAnOpenModalAndCtrlCAlways(t *testing.T) {
	t.Parallel()
	isQuit := func(cmd tea.Cmd) bool {
		if cmd == nil {
			return false
		}
		_, ok := cmd().(tea.QuitMsg)
		return ok
	}
	m := securityModelAt(t, securityClient(), screenSecurityTrivy, 150, 24)
	if _, cmd := m.Update(testKeyRunes("q")); !isQuit(cmd) {
		t.Fatal("q on the plain screen must quit")
	}
	for _, open := range [][]keyMsg{{testKeyRunes("c")}, {testKeyRunes("p")}, {testKeyRunes("x")}, {testKeyRunes("c"), testKeyTab()}} {
		opened := securityPress(t, m, open...)
		if _, cmd := opened.Update(testKeyRunes("q")); isQuit(cmd) {
			t.Fatalf("q quit while a modal or confirm was open (keys %v)", open)
		}
		if _, cmd := opened.Update(testKeyCtrl('c')); !isQuit(cmd) {
			t.Fatalf("ctrl+c must quit even with a modal open (keys %v)", open)
		}
	}
}

func TestSecurityScreensRedirectToLoginOnAnExpiredSession(t *testing.T) {
	t.Parallel()
	for _, to := range []screen{screenSecurityTrivy, screenSecurityGitleaksConfig, screenSecuritySigningConfig} {
		client := securityClient()
		client.featureErr = securityExpired()
		m := securityModelAt(t, client, to, 150, 24)
		if m.screen != screenAdminLogin {
			t.Errorf("%s: page load with an expired session left the screen at %q, want login", to, m.screen)
		}
	}

	client := securityClient()
	m := securityModelAt(t, client, screenSecurityTrivy, 150, 24)
	client.featureErr = securityExpired()
	m = securityPress(t, m, testKeyRunes("c"), testKeyEnter())
	if m.screen != screenAdminLogin {
		t.Errorf("save with an expired session left the screen at %q, want login", m.screen)
	}
}

func securityScreenGenOf(t *testing.T, m Model, to screen) uint64 {
	t.Helper()
	slot, _ := slotFor(to)
	gen, ok := securityScreenGen(m.adminScreens, slot)
	if !ok {
		t.Fatalf("no security screen mounted for %s", to)
	}
	return gen
}

// securityForeign returns one result of every kind, stamped with gen.
func securityForeign(gen uint64, err error) []tea.Msg {
	res := securityResult{gen: gen, err: err}
	return []tea.Msg{
		securityPageMsg{securityResult: res, page: securityGitleaksPage()},
		securityOverridesMsg{securityResult: res, overrides: []ports.RepositoryOverrideDetails{{Repository: "x/y"}}},
		securityActionMsg{securityResult: res},
		securityConfigureMsg{res},
		securityScanPolicyLoadedMsg{securityResult: res},
		securityScanPolicySavedMsg{securityResult: res},
		securitySigningPolicyLoadedMsg{securityResult: res},
		securitySigningPolicySavedMsg{securityResult: res},
		securityKeyUsageMsg{securityResult: res, count: 9},
	}
}

func TestSecurityScreensDropResultsOfAnotherGeneration(t *testing.T) {
	t.Parallel()
	for _, to := range []screen{screenSecurityTrivy, screenSecurityGitleaksConfig, screenSecuritySigningConfig} {
		m := securityModelAt(t, securityClient(), to, 150, 24)
		before := securityRender(m)
		slot, _ := slotFor(to)
		own := securityScreenGenOf(t, m, to)
		for _, msg := range securityForeign(own+1000, errors.New("other")) {
			// Straight to the screen: it drops a foreign generation itself.
			next, cmd, _ := m.adminScreens[slot].Update(m.screenEnv(), msg)
			if cmd != nil || !reflect.DeepEqual(next, m.adminScreens[slot]) {
				t.Fatalf("%s: %T of another generation changed the screen or started work", to, msg)
			}
			// Through the Model: dropped before anything else happens.
			updated, cmd := m.Update(msg)
			if cmd != nil || securityRender(updated.(Model)) != before {
				t.Fatalf("%s: %T of another generation changed the view", to, msg)
			}
		}
	}
}

func TestSecurityStaleExpiredResultDoesNotLogTheOperatorOut(t *testing.T) {
	t.Parallel()
	for _, to := range []screen{screenSecurityTrivy, screenSecurityGitleaksConfig, screenSecuritySigningConfig} {
		m := securityModelAt(t, securityClient(), to, 150, 24)
		for _, msg := range securityForeign(securityScreenGenOf(t, m, to)+1000, securityExpired()) {
			updated, _ := m.Update(msg)
			got := updated.(Model)
			if got.screen != to || got.adminAuth != adminAuthStateAuthenticated {
				t.Fatalf("%s: a stale expired %T logged the operator out (screen %q, auth %q)", to, msg, got.screen, got.adminAuth)
			}
		}
		// The screen's own expired result still does.
		for _, msg := range securityForeign(securityScreenGenOf(t, m, to), securityExpired())[:1] {
			updated, _ := m.Update(msg)
			if updated.(Model).screen != screenAdminLogin {
				t.Fatalf("%s: an expired result of the mounted screen must redirect to login", to)
			}
		}
	}
}

func TestSecurityLateSaveAfterLeavingAndReopeningNeverTouchesTheNewScreen(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		to   screen
		open keyMsg
		note string
		read func(*testing.T, Model) (saving, modal bool, note string)
	}{
		{screenSecurityTrivy, testKeyRunes("c"), "Configuration saved.", func(t *testing.T, m Model) (bool, bool, string) {
			s := securityTrivyOf(t, m)
			return s.saving, s.cfg.Active(), s.note
		}},
		{screenSecurityGitleaksConfig, testKeyRunes("c"), "Configuration saved.", func(t *testing.T, m Model) (bool, bool, string) {
			s := securityGitleaksOf(t, m)
			return s.saving, s.cfg.Active(), s.note
		}},
		{screenSecuritySigningConfig, testKeyRunes("p"), "Signing policy saved.", func(t *testing.T, m Model) (bool, bool, string) {
			s := securitySigningOf(t, m)
			return s.saving, s.cfg.Active(), s.note
		}},
	} {
		for _, expired := range []bool{false, true} {
			client := securityClient()
			m := securityModelAt(t, client, tc.to, 150, 24)
			m = securityPress(t, m, tc.open)
			updated, saveCmd := m.Update(testKeyEnter()) // the save is in flight
			m = updated.(Model)
			if saveCmd == nil {
				t.Fatalf("%s: Enter returned no command", tc.to)
			}
			// Leave (Esc closes the modal, Esc leaves) and come back.
			m = securityPress(t, m, testKeyEsc(), testKeyEsc())
			if m.screen != screenAdminOverview {
				t.Fatalf("%s: could not leave the screen", tc.to)
			}
			updated, reopenCmd := m.Update(navigateMsg{To: tc.to})
			m = securityRun(t, updated.(Model), reopenCmd)
			if saving, modal, note := tc.read(t, m); saving || modal || note != "" {
				t.Fatalf("%s: the reopened screen starts with saving=%v modal=%v note=%q", tc.to, saving, modal, note)
			}
			before := securityRender(m)

			// The old save finally answers.
			if expired {
				client.featureErr, client.scanPolicyUpdateErr, client.signingPolicyUpdateErr = securityExpired(), securityExpired(), securityExpired()
			}
			late := saveCmd()
			updated, _ = m.Update(late)
			m = updated.(Model)
			if m.screen != tc.to || m.adminAuth != adminAuthStateAuthenticated {
				t.Fatalf("%s (expired=%v): the late result moved the operator to %q (auth %q)", tc.to, expired, m.screen, m.adminAuth)
			}
			if saving, modal, note := tc.read(t, m); saving || modal || note != "" || securityRender(m) != before {
				t.Fatalf("%s (expired=%v): the late result marked the new screen (saving=%v modal=%v note=%q):\n%s", tc.to, expired, saving, modal, note, securityRender(m))
			}
		}
	}
}

func TestSecurityScreensGetAFreshGenerationOnEveryVisit(t *testing.T) {
	t.Parallel()
	for _, to := range []screen{screenSecurityTrivy, screenSecurityGitleaksConfig, screenSecuritySigningConfig} {
		m := securityModelAt(t, securityClient(), to, 150, 24)
		first := securityScreenGenOf(t, m, to)
		updated, cmd := m.Update(navigateMsg{To: to})
		m = securityRun(t, updated.(Model), cmd)
		if second := securityScreenGenOf(t, m, to); second == first {
			t.Fatalf("%s: reopening kept generation %d", to, first)
		}
	}
}

// ---- Secret scanning ----

func TestSecurityGitleaksScreenAndConfigModal(t *testing.T) {
	t.Parallel()
	client := securityClient()
	m := securityModelAt(t, client, screenSecurityGitleaksConfig, 150, 24)
	out := securityRender(m)
	for _, want := range []string{
		"Admin › Security › Secret scanning", "Gitleaks, leaked-secret scanner", "GITLEAKS CONFIGURATION", "[x] enabled", "2m",
		"PER-REPOSITORY OVERRIDES", "config path set", "team/web", "inherited", "global settings",
		"c configure", "o overrides", "r refresh", "esc back",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}

	// The old settings key no longer opens anything.
	if m = securityPress(t, m, testKeyRunes("s")); securityGitleaksOf(t, m).cfg.Active() {
		t.Fatal("s opened the modal, the board's key is c")
	}

	m = securityPress(t, m, testKeyRunes("c"))
	if want := (gitleaksConfigModal{Open: true, Focus: gitleaksConfigFieldEnabled, Enabled: true, Timeout: "2m", MaxConcurrency: "2"}); securityGitleaksOf(t, m).cfg != want {
		t.Fatalf("cfg = %#v, want %#v", securityGitleaksOf(t, m).cfg, want)
	}
	m = securityPress(t, m, testKeySpace(), testKeyTab())
	m = securityPress(t, m, securityBackspaces(2)...)
	m = securityPress(t, m, securityType("30sq")...)
	if got := securityGitleaksOf(t, m).cfg.Timeout; got != "30sq" {
		t.Fatalf("Timeout = %q, want q typed", got)
	}
	m = securityPress(t, m, testKeyEnter())
	if client.configureFeatureCalls != 0 || !strings.Contains(securityRender(m), "invalid timeout") {
		t.Fatalf("calls = %d, view:\n%s\nwant an invalid timeout error and no call", client.configureFeatureCalls, securityRender(m))
	}
	m = securityPress(t, m, testKeyBackspace())
	updated, cmd := m.Update(testKeyEnter())
	m = updated.(Model)
	if _, again := m.Update(testKeyEnter()); again != nil {
		t.Fatal("Enter while the save is in flight returned a command")
	}
	m = securityRun(t, m, cmd)
	in := client.lastConfigureInput
	if client.configureFeatureCalls != 1 || client.lastConfiguredFeature != gitleaksFeatureName || in.Enabled == nil || *in.Enabled ||
		in.Timeout == nil || *in.Timeout != 30*time.Second || in.MaxConcurrency == nil || *in.MaxConcurrency != 2 {
		t.Fatalf("ConfigureFeature calls = %d for %q, input %#v", client.configureFeatureCalls, client.lastConfiguredFeature, in)
	}
	if securityGitleaksOf(t, m).cfg.Active() || !strings.Contains(securityRender(m), "Configuration saved.") {
		t.Fatalf("view after the save:\n%s", securityRender(m))
	}
}

func TestSecurityGitleaksNavigationAndActions(t *testing.T) {
	t.Parallel()
	m := securityModelAt(t, securityClient(), screenSecurityGitleaksConfig, 150, 24)
	if got := securityPress(t, m, testKeyRunes("o")); got.screen != screenSecurityGitleaksRepos {
		t.Fatalf("o opened %q, want the Gitleaks overrides screen", got.screen)
	}
	if got := securityPress(t, m, testKeyEsc()); got.screen != screenAdminOverview {
		t.Fatalf("esc opened %q, want the Overview", got.screen)
	}
	m = securityPress(t, m, testKeyRunes("x"))
	if !securityGitleaksOf(t, m).confirm.Active() {
		t.Fatal("x did not open the disable confirm")
	}
}

// ---- Signing policy ----

func TestSecuritySigningScreen(t *testing.T) {
	t.Parallel()
	client := securityClient()
	m := securityModelAt(t, client, screenSecuritySigningConfig, 200, 40)
	if client.getFeaturePageCalls != 1 || client.getSigningPolicyCalls != 1 || client.listRepositoryOverridesCalls != 1 {
		t.Fatalf("calls: page %d, policy %d, overrides %d, want one each", client.getFeaturePageCalls, client.getSigningPolicyCalls, client.listRepositoryOverridesCalls)
	}
	out := securityRender(m)
	for _, want := range []string{
		"Admin › Security › Signing policy", "image signature verification", "SIGNING POLICY", "[x] enforced", "pusher",
		"TRUSTED PUBLIC KEYS", "FINGERPRINT", "ECDSA P-256", "TRUSTED IDENTITIES (KEYLESS)", "https://github.com/team/.*",
		"token.actions.githubusercontent.com", "PER-REPOSITORY OVERRIDES", "1 key, 0 identities", "not enforced",
		"p policy", "o overrides", "r refresh", "esc back",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "BEGIN PUBLIC KEY") || strings.Contains(out, "MFkwEwYHKoZIzj0C") {
		t.Fatalf("a trusted key must show only as a fingerprint:\n%s", out)
	}
	if got := securityPress(t, m, testKeyRunes("o")); got.screen != screenSecuritySigningRepos {
		t.Fatalf("o opened %q, want the Signing overrides screen", got.screen)
	}
}

func TestSecuritySigningPolicyModalSavesKeysAndIdentities(t *testing.T) {
	t.Parallel()
	client := securityClient()
	m := securityModelAt(t, client, screenSecuritySigningConfig, 150, 24)
	m = securityPress(t, m, testKeyRunes("p"))
	s := securitySigningOf(t, m)
	if !s.cfg.Enabled || s.cfg.UnsignedSelfRead != "pusher" || len(s.cfg.Keys.Keys()) != 2 || len(s.cfg.Identities.Identities()) != 1 {
		t.Fatalf("cfg = %#v, want it seeded from the loaded policy", s.cfg)
	}

	// Space cycles self-read; the key list answers n without tabbing to it.
	m = securityPress(t, m, testKeyTab(), testKeySpace(), testKeySpace())
	if got := securitySigningOf(t, m).cfg.UnsignedSelfRead; got != "off" {
		t.Fatalf("UnsignedSelfRead = %q, want pusher cycled to repo_push to off", got)
	}
	m = securityPress(t, m, testKeyRunes("x"))
	if !strings.Contains(securityRender(m), "Checking key usage") && !securitySigningOf(t, m).cfg.Keys.confirm.Active() {
		t.Fatal("x did not start the delete of the selected key")
	}
	if !securitySigningOf(t, m).cfg.Keys.confirm.Active() || !strings.Contains(securityRender(m), "Delete it anyway?") {
		t.Fatalf("the delete confirm must be visible in the modal:\n%s", securityRender(m))
	}
	m = securityPress(t, m, testKeyEnter())
	if got := len(securitySigningOf(t, m).cfg.Keys.Keys()); got != 1 {
		t.Fatalf("keys = %d after the confirmed delete, want 1", got)
	}

	// Save: Enter on the list's idle navigation saves the whole policy, once.
	updated, cmd := m.Update(testKeyEnter())
	m = updated.(Model)
	if _, again := m.Update(testKeyEnter()); again != nil {
		t.Fatal("Enter while the policy save is in flight returned a command")
	}
	m = securityRun(t, m, cmd)
	got := client.lastSigningPolicyInput
	if client.updateSigningPolicyCalls != 1 || !got.Enabled || got.UnsignedSelfRead != "off" || len(got.TrustedPublicKeys) != 1 || len(got.TrustedIdentities) != 1 {
		t.Fatalf("UpdateSigningPolicy calls = %d, input %#v", client.updateSigningPolicyCalls, got)
	}
	if !securitySigningOf(t, m).cfg.Active() || !strings.Contains(securityRender(m), "Signing policy saved.") {
		t.Fatalf("the modal stays open after a save and says so:\n%s", securityRender(m))
	}

	// Esc closes the modal.
	m = securityPress(t, m, testKeyEsc())
	if securitySigningOf(t, m).cfg.Active() {
		t.Fatal("Esc did not close the policy modal")
	}
}

func TestSecuritySigningPolicyModalNeedsTheLoadedPolicy(t *testing.T) {
	t.Parallel()
	client := securityClient()
	client.signingPolicyErr = errors.New("policy down")
	m := securityModelAt(t, client, screenSecuritySigningConfig, 150, 24)
	m = securityPress(t, m, testKeyRunes("p"))
	if securitySigningOf(t, m).cfg.Active() || !strings.Contains(securityRender(m), "has not loaded") {
		t.Fatalf("p must not open the editor on a policy that never loaded (saving would wipe the trusted keys):\n%s", securityRender(m))
	}
	if !strings.Contains(securityRender(m), "Could not read the policy: policy down") {
		t.Fatalf("the policy card must say why it is empty:\n%s", securityRender(m))
	}
}

func TestSecuritySigningModalEscapeLeavesOneLevelAtATime(t *testing.T) {
	t.Parallel()
	client := securityClient()
	m := securityModelAt(t, client, screenSecuritySigningConfig, 150, 24)
	m = securityPress(t, m, testKeyRunes("p"), testKeyRunes("n"), testKeyRunes("a"))
	if s := securitySigningOf(t, m); !s.cfg.Keys.adding || s.cfg.Keys.input != "a" {
		t.Fatalf("Keys = %#v, want an add in progress", s.cfg.Keys)
	}
	m = securityPress(t, m, testKeyEsc())
	if s := securitySigningOf(t, m); s.cfg.Keys.adding || !s.cfg.Active() {
		t.Fatal("the first Esc must cancel the add and keep the modal")
	}
	m = securityPress(t, m, testKeyEsc())
	if securitySigningOf(t, m).cfg.Active() {
		t.Fatal("the second Esc must close the modal")
	}

	// Esc stops waiting for a slow key-usage lookup instead of locking the modal.
	m = securityPress(t, m, testKeyRunes("p"))
	updated, _ := m.Update(testKeyRunes("x"))
	m = updated.(Model)
	if !securitySigningOf(t, m).cfg.Keys.usageLoading {
		t.Fatal("x did not start the usage lookup")
	}
	m = securityPress(t, m, testKeyEsc())
	if s := securitySigningOf(t, m); s.cfg.Keys.usageLoading || !s.cfg.Active() {
		t.Fatal("Esc must stop waiting for the lookup and keep the modal")
	}
	late, _ := m.Update(adminSigningKeyUsageLoadedMsg{key: trustedKeyListTestPEM1, count: 3})
	if securitySigningOf(t, late.(Model)).cfg.Keys.confirm.Active() {
		t.Fatal("a late usage answer must not open a confirm nobody asked for")
	}
}

func TestSecuritySigningIdentityListAndSaveFailure(t *testing.T) {
	t.Parallel()
	client := securityClient()
	m := securityModelAt(t, client, screenSecuritySigningConfig, 150, 24)
	m = securityPress(t, m, testKeyRunes("p"), testKeyTab(), testKeyTab(), testKeyTab(), testKeyRunes("n"))
	m = securityPress(t, m, securityType("^jkq$")...)
	m = securityPress(t, m, testKeyTab())
	m = securityPress(t, m, securityType("https://issuer.example")...)
	m = securityPress(t, m, testKeyEnter())
	ids := securitySigningOf(t, m).cfg.Identities.Identities()
	if len(ids) != 2 || ids[1].CertificateIdentityRegexp != "^jkq$" {
		t.Fatalf("identities = %#v, want the typed identity added (j, k and q typed)", ids)
	}

	client.signingPolicyUpdateErr = errors.New("rejected")
	m = securityPress(t, m, testKeyEnter())
	if !securitySigningOf(t, m).cfg.Active() || !strings.Contains(securityRender(m), "rejected") {
		t.Fatalf("a failed save keeps the modal and shows the error:\n%s", securityRender(m))
	}

	// The clear row saves without any trusted key.
	client.signingPolicyUpdateErr = nil
	calls := client.updateSigningPolicyCalls
	m = securityPress(t, m, testKeyTab(), testKeyEnter())
	if client.updateSigningPolicyCalls != calls || !strings.Contains(securityRender(m), "Press enter again to confirm") {
		t.Fatalf("the clear row must ask before it sends anything:\n%s", securityRender(m))
	}
	m = securityPress(t, m, testKeyEnter())
	if got := client.lastSigningPolicyInput; client.updateSigningPolicyCalls != calls+1 || len(got.TrustedPublicKeys) != 0 || len(got.TrustedIdentities) != 2 {
		t.Fatalf("clear-keys save sent %#v after the confirmation, want no keys and both identities", got)
	}
}

func TestSecuritySigningExpiredSessionRedirects(t *testing.T) {
	t.Parallel()
	client := securityClient()
	m := securityModelAt(t, client, screenSecuritySigningConfig, 150, 24)
	client.signingPolicyUpdateErr = securityExpired()
	m = securityPress(t, m, testKeyRunes("p"), testKeyEnter())
	if m.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want login after an expired save", m.screen)
	}
}

// ---- pure helpers ----

func TestSecurityDurationShowsHoursAndMinutesWithoutZeroSuffixes(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"24h0m0s": "24h", "5m0s": "5m", "1h30m0s": "1h30m", "2m0s": "2m", "30s": "30s", "0s": "0s", "": "", "soon": "soon", "6h0m0s": "6h",
	} {
		if got := securityDuration(in); got != want {
			t.Errorf("securityDuration(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSecurityOverrideRowsDescribeWhatTheOverrideSets(t *testing.T) {
	t.Parallel()
	now := securityDay(time.December, 1)
	for _, tc := range []struct {
		feature string
		detail  ports.RepositoryOverrideDetails
		want    string
	}{
		{trivyFeatureName, ports.RepositoryOverrideDetails{Enabled: false}, "scanning off"},
		{trivyFeatureName, ports.RepositoryOverrideDetails{Enabled: true, IgnoreFilePath: "/i"}, "ignore file set"},
		{trivyFeatureName, ports.RepositoryOverrideDetails{Enabled: true, IgnorePolicyPath: "/p"}, "ignore policy set"},
		{trivyFeatureName, ports.RepositoryOverrideDetails{Enabled: true, IgnoreFilePath: "/i", IgnorePolicyPath: "/p"}, "ignore file and policy"},
		{trivyFeatureName, ports.RepositoryOverrideDetails{Enabled: true}, "scanning on"},
		{gitleaksFeatureName, ports.RepositoryOverrideDetails{Enabled: true, ConfigPath: "/c"}, "config path set"},
		{gitleaksFeatureName, ports.RepositoryOverrideDetails{Enabled: true}, "default config"},
		{gitleaksFeatureName, ports.RepositoryOverrideDetails{Enabled: false}, "scanning off"},
		{signingFeatureName, ports.RepositoryOverrideDetails{Enabled: false}, "not enforced"},
		{signingFeatureName, ports.RepositoryOverrideDetails{Enabled: true, TrustedPublicKeys: []string{"k"}, TrustedIdentities: []ports.TrustedIdentity{{}, {}}}, "1 key, 2 identities"},
	} {
		if got := securityOverrideDetail(tc.feature, tc.detail); got != tc.want {
			t.Errorf("securityOverrideDetail(%s, %#v) = %q, want %q", tc.feature, tc.detail, got, tc.want)
		}
	}
	for _, tc := range []struct {
		at   time.Time
		want string
	}{
		{securityDay(time.November, 14), "14 Nov"},
		{time.Date(2025, time.March, 2, 0, 0, 0, 0, time.UTC), "2 Mar 2025"},
		{time.Time{}, "-"},
	} {
		if got := securityOverrideDate(tc.at, now); got != tc.want {
			t.Errorf("securityOverrideDate(%v) = %q, want %q", tc.at, got, tc.want)
		}
	}
}

func TestSecurityOverridesListShowsErrorsAndOverflow(t *testing.T) {
	t.Parallel()
	client := securityClient()
	client.listRepositoryOverridesErr = errors.New("overrides down")
	m := securityModelAt(t, client, screenSecurityTrivy, 150, 24)
	if out := securityRender(m); !strings.Contains(out, "Could not read the overrides: overrides down") {
		t.Fatalf("the overrides list must say why it is empty:\n%s", out)
	}

	client = securityClient()
	known := make([]string, 0, 60)
	for i := range 60 {
		known = append(known, fmt.Sprintf("team/service-%02d", i))
	}
	m = newAdminReadyModelWithCatalog(t, known, client)
	m.now = func() time.Time { return charFixedTime }
	m = charResize(t, m, 150, 24)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: charFixedTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	updated, cmd := m.Update(navigateMsg{To: screenSecurityTrivy})
	m = securityRun(t, updated.(Model), cmd)
	out := securityRender(m)
	if !strings.Contains(out, "+") || !strings.Contains(out, "more, o opens the list") {
		t.Fatalf("a long list must say how many rows it hides:\n%s", out)
	}
	if strings.Index(out, "team/docs") < 0 || strings.Index(out, "team/api") < 0 {
		t.Fatalf("the overrides must come first even in a long catalog:\n%s", out)
	}
}

// ---- the policy editors cannot send a blank or foreign policy ----

func TestSecurityPolicyEditorsRefuseToOpenOnAPolicyThatNeverLoaded(t *testing.T) {
	t.Parallel()
	client := securityClient()
	client.signingPolicyErr = errors.New("policy down")
	client.scanPolicyErr = errors.New("policy down")

	m := securityModelAt(t, client, screenSecuritySigningConfig, 150, 24)
	m = securityPress(t, m, testKeyRunes("p"), testKeyEnter(), testKeyEnter())
	if securitySigningOf(t, m).cfg.Active() || client.updateSigningPolicyCalls != 0 {
		t.Fatalf("signing: modal open = %v, saves = %d, want neither: a blank policy must never go over the real keys", securitySigningOf(t, m).cfg.Active(), client.updateSigningPolicyCalls)
	}
	m = securityModelAt(t, client, screenSecurityTrivy, 150, 24)
	m = securityPress(t, m, testKeyRunes("p"), testKeyEnter(), testKeyEnter())
	if securityTrivyOf(t, m).policyModal.Active() || client.updateScanPolicyCalls != 0 {
		t.Fatalf("trivy: modal open = %v, saves = %d, want neither", securityTrivyOf(t, m).policyModal.Active(), client.updateScanPolicyCalls)
	}
}

func TestSecuritySigningSaveSendsExactlyTheLoadedTrustUnlessTheOperatorRemovedIt(t *testing.T) {
	t.Parallel()
	client := securityClient()
	loaded := client.signingPolicy
	m := securityModelAt(t, client, screenSecuritySigningConfig, 150, 24)

	// Opening and saving without touching the lists sends them as they are.
	m = securityPress(t, m, testKeyRunes("p"), testKeyEnter())
	got := client.lastSigningPolicyInput
	if client.updateSigningPolicyCalls != 1 || !reflect.DeepEqual(got.TrustedPublicKeys, loaded.TrustedPublicKeys) || !reflect.DeepEqual(got.TrustedIdentities, loaded.TrustedIdentities) {
		t.Fatalf("a plain save sent keys %d and identities %d, want the loaded %d and %d", len(got.TrustedPublicKeys), len(got.TrustedIdentities), len(loaded.TrustedPublicKeys), len(loaded.TrustedIdentities))
	}

	// A refresh that fails does not blank what the editor will send.
	client.featureErr = errors.New("down")
	client.signingPolicyErr = errors.New("down")
	m = securityPress(t, m, testKeyEsc(), testKeyRunes("r"), testKeyRunes("p"), testKeyEnter())
	got = client.lastSigningPolicyInput
	if len(got.TrustedPublicKeys) != len(loaded.TrustedPublicKeys) || len(got.TrustedIdentities) != len(loaded.TrustedIdentities) {
		t.Fatalf("after a failed refresh a save sent keys %d and identities %d, want the last loaded", len(got.TrustedPublicKeys), len(got.TrustedIdentities))
	}
}

func TestSecuritySigningClearAllKeysAsksBeforeSendingAnything(t *testing.T) {
	t.Parallel()
	client := securityClient()
	m := securityModelAt(t, client, screenSecuritySigningConfig, 150, 24)
	m = securityPress(t, m, testKeyRunes("p"), testKeyTab(), testKeyTab(), testKeyTab(), testKeyTab())
	if securitySigningOf(t, m).cfg.Focus != signingPolicyFieldClearKeys {
		t.Fatalf("focus = %v, want the clear row", securitySigningOf(t, m).cfg.Focus)
	}

	m = securityPress(t, m, testKeyEnter())
	if client.updateSigningPolicyCalls != 0 || !strings.Contains(securityRender(m), "Remove all 2 trusted keys?") {
		t.Fatalf("saves = %d, view:\n%s\nwant a question and no save", client.updateSigningPolicyCalls, securityRender(m))
	}
	// Any other key cancels the question: the next Enter asks again.
	m = securityPress(t, m, testKeyTab(), testKeyTab(), testKeyTab(), testKeyTab(), testKeyTab(), testKeyEnter())
	if client.updateSigningPolicyCalls != 0 {
		t.Fatal("a cancelled clear question still saved")
	}
	// Esc leaves with the question open and nothing sent.
	m = securityPress(t, m, testKeyEsc())
	if client.updateSigningPolicyCalls != 0 || securitySigningOf(t, m).clearArmed {
		t.Fatal("Esc must drop the clear question without saving")
	}
}

func TestSecuritySigningFailedSaveKeepsTheModalAndWhatWasTyped(t *testing.T) {
	t.Parallel()
	client := securityClient()
	m := securityModelAt(t, client, screenSecuritySigningConfig, 150, 24)
	m = securityPress(t, m, testKeyRunes("p"), testKeyRunes("n"))
	updated, _ := m.Update(tea.PasteMsg{Content: strings.TrimSpace(trustedKeyListTestPEM1) + "\n"})
	m = updated.(Model)
	m = securityPress(t, m, testKeyEnter()) // commits the pasted key into the list
	keysBefore := securitySigningOf(t, m).cfg.Keys.Keys()
	m = securityPress(t, m, testKeySpace()) // self-read untouched; focus is on the key list
	m = securityPress(t, m, testKeyTab(), testKeyTab())

	client.signingPolicyUpdateErr = errors.New("rejected")
	m = securityPress(t, m, testKeyTab(), testKeyTab(), testKeyTab(), testKeyTab(), testKeyEnter())
	s := securitySigningOf(t, m)
	if !s.cfg.Active() || s.saving || !strings.Contains(securityRender(m), "rejected") {
		t.Fatalf("a failed save must keep the modal open, unlocked and showing the error:\n%s", securityRender(m))
	}
	if !reflect.DeepEqual(s.cfg.Keys.Keys(), keysBefore) || len(keysBefore) != 3 {
		t.Fatalf("keys after the failure = %d, want the %d typed ones kept", len(s.cfg.Keys.Keys()), len(keysBefore))
	}

	// Trying again sends what was typed.
	client.signingPolicyUpdateErr = nil
	m = securityPress(t, m, testKeyShiftTab(), testKeyShiftTab(), testKeyShiftTab(), testKeyEnter())
	if got := client.lastSigningPolicyInput.TrustedPublicKeys; len(got) != 3 {
		t.Fatalf("the retry sent %d keys, want the 3 typed ones", len(got))
	}
}

func TestSecurityTrivyFailedSaveKeepsTheTypedValues(t *testing.T) {
	t.Parallel()
	client := securityClient()
	m := securityModelAt(t, client, screenSecurityTrivy, 150, 24)
	m = securityPress(t, m, testKeyRunes("c"), testKeyTab(), testKeyTab(), testKeyTab(), testKeyTab())
	m = securityPress(t, m, securityBackspaces(1)...)
	m = securityPress(t, m, testKeyRunes("7"))
	client.featureErr = errors.New("rejected")
	m = securityPress(t, m, testKeyEnter())
	if s := securityTrivyOf(t, m); !s.cfg.Active() || s.cfg.MaxConcurrency != "7" || !strings.Contains(securityRender(m), "rejected") {
		t.Fatalf("cfg = %#v, want the modal kept with the typed value", s.cfg)
	}
	client.featureErr = nil
	securityPress(t, m, testKeyEnter())
	if got := client.lastConfigureInput.MaxConcurrency; got == nil || *got != 7 {
		t.Fatalf("the retry sent %v, want 7", got)
	}
}

func TestSecurityEachScreenSendsOnlyItsOwnFeature(t *testing.T) {
	t.Parallel()
	type counts struct{ configure, scan, signing int }
	read := func(c *fakeAdminClient) counts {
		return counts{c.configureFeatureCalls, c.updateScanPolicyCalls, c.updateSigningPolicyCalls}
	}
	for _, tc := range []struct {
		to      screen
		keys    []keyMsg
		want    counts
		feature string
	}{
		{screenSecurityTrivy, []keyMsg{testKeyRunes("c"), testKeyEnter()}, counts{1, 0, 0}, trivyFeatureName},
		{screenSecurityTrivy, []keyMsg{testKeyRunes("p"), testKeyEnter()}, counts{0, 1, 0}, ""},
		{screenSecurityGitleaksConfig, []keyMsg{testKeyRunes("c"), testKeyEnter()}, counts{1, 0, 0}, gitleaksFeatureName},
		{screenSecuritySigningConfig, []keyMsg{testKeyRunes("p"), testKeyEnter()}, counts{0, 0, 1}, ""},
	} {
		client := securityClient()
		m := securityModelAt(t, client, tc.to, 150, 24)
		securityPress(t, m, tc.keys...)
		if got := read(client); got != tc.want {
			t.Errorf("%s %v: calls %+v, want %+v", tc.to, tc.keys, got, tc.want)
		}
		if tc.feature != "" && client.lastConfiguredFeature != tc.feature {
			t.Errorf("%s configured %q, want %q", tc.to, client.lastConfiguredFeature, tc.feature)
		}
	}

	// A result of another screen's save is never applied here.
	client := securityClient()
	m := securityModelAt(t, client, screenSecuritySigningConfig, 150, 24)
	m = securityPress(t, m, testKeyRunes("p"))
	updated, _ := m.Update(securityScanPolicySavedMsg{securityResult: securityResult{gen: securityScreenGenOf(t, m, screenSecuritySigningConfig)}, settings: ports.ScanPolicySettings{Enabled: true}})
	if s := securitySigningOf(t, updated.(Model)); s.saving || s.note != "" || !s.cfg.Active() {
		t.Fatal("a scan policy result changed the signing screen")
	}
}

func TestSecurityScreensIgnorePastedTextOnTheMainScreen(t *testing.T) {
	t.Parallel()
	// A paste is not a key press: the first character of pasted text must not
	// run a runtime action or open an editor.
	for _, to := range []screen{screenSecurityTrivy, screenSecurityGitleaksConfig, screenSecuritySigningConfig} {
		for _, text := range []string{"i", "u", "b", "e", "x", "c", "p", "o", "r", "upgrade"} {
			client := securityClient()
			m := securityModelAt(t, client, to, 150, 24)
			before, reads := securityRender(m), client.getFeaturePageCalls
			updated, cmd := m.Update(tea.PasteMsg{Content: text})
			got := updated.(Model)
			if cmd != nil || client.executeFeatureActionCalls != 0 || got.screen != to || securityRender(got) != before || client.getFeaturePageCalls != reads {
				t.Errorf("%s: pasting %q did something (cmd %v, actions %d)", to, text, cmd != nil, client.executeFeatureActionCalls)
			}
		}
	}
}

func TestSigningTrustedKeysCardSaysWhyItIsEmptyAndHowToRetry(t *testing.T) {
	t.Parallel()
	client := securityClient()
	client.signingPolicyErr = errors.New("policy down")
	out := securityRender(securityModelAt(t, client, screenSecuritySigningConfig, 150, 24))
	if strings.Count(out, "Could not read the policy: policy down") < 2 || !strings.Contains(out, "r to retry") || strings.Contains(out, "Loading…") {
		t.Fatalf("the trusted keys card must show the error and how to retry, never Loading… forever:\n%s", out)
	}
}
