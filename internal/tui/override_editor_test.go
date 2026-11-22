package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"regixtry/internal/ports"
)

// ovOpenEditor opens the editor of repository on screen to.
func ovOpenEditor(t *testing.T, client AdminClient, to screen, repository string) Model {
	t.Helper()
	m := ovModelAt(t, client, securityKnown, to, 160, 30)
	m = ovSelect(t, m, to, repository)
	return securityPress(t, m, testKeyRunes("o"))
}

func ovEditorOf(t *testing.T, m Model, to screen) overrideEditor {
	t.Helper()
	return ovListOf(t, m, to).editor
}

// ovFocus tabs to the field.
func ovFocus(t *testing.T, m Model, to screen, field overrideField) Model {
	t.Helper()
	for range 8 {
		if ovEditorOf(t, m, to).currentField() == field {
			return m
		}
		m = securityPress(t, m, testKeyTab())
	}
	t.Fatalf("field %v is not reachable with Tab", field)
	return m
}

func TestOverrideEditorFieldsAreFixedPerFeatureAndTabCyclesThem(t *testing.T) {
	t.Parallel()
	want := map[string][]overrideField{
		trivyFeatureName:    {overrideFieldEnabled, overrideFieldPathPrimary, overrideFieldPathSecondary, overrideFieldClear},
		gitleaksFeatureName: {overrideFieldEnabled, overrideFieldPathPrimary, overrideFieldClear},
		signingFeatureName:  {overrideFieldEnabled, overrideFieldPathPrimary, overrideFieldIdentities, overrideFieldUnsignedSelfRead, overrideFieldClear},
	}
	for _, sc := range ovScreens {
		fields := want[sc.feature]
		m := ovOpenEditor(t, ovClient(), sc.to, "team/api")
		for i := 0; i <= len(fields); i++ {
			if got, wantField := ovEditorOf(t, m, sc.to).currentField(), fields[i%len(fields)]; got != wantField {
				t.Fatalf("%s: field %d = %v, want %v", sc.name, i, got, wantField)
			}
			if i < len(fields) {
				m = securityPress(t, m, testKeyTab())
			}
		}
		m = securityPress(t, m, testKeyShiftTab())
		if got := ovEditorOf(t, m, sc.to).currentField(); got != fields[len(fields)-1] {
			t.Fatalf("%s: shift+tab did not go back to the last field (%v)", sc.name, got)
		}
		if got := ovEditorOf(t, m, sc.to).Feature(); got != sc.feature {
			t.Fatalf("%s: Feature() = %q after tabbing, want %q", sc.name, got, sc.feature)
		}
	}
}

// What the editor sends is for the repository and feature it was opened for,
// whatever else happens on the screen under it.
func TestOverrideEditorStaysBoundToItsRepositoryAndFeature(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		client := ovClient()
		m := ovOpenEditor(t, client, sc.to, "team/docs")
		e := ovEditorOf(t, m, sc.to)
		wantEnabled, wantExists := e.enabled, e.exists

		// A reload landing while the editor is open replaces the rows (and may
		// reorder them) but cannot change what the editor edits.
		reordered := []ports.RepositoryOverrideDetails{{Repository: "team/web", Feature: sc.feature, Enabled: true}}
		updated, _ := m.Update(overrideListMsg{securityResult: securityResult{gen: e.gen}, overrides: reordered})
		m = updated.(Model)
		if got := ovEditorOf(t, m, sc.to); got.repository != "team/docs" || got.Feature() != sc.feature || got.id != e.id {
			t.Fatalf("%s: a reload changed the editor to %q/%q", sc.name, got.repository, got.Feature())
		}

		// Results for another editor, repository or feature are dropped.
		other := "trivy"
		if sc.feature == trivyFeatureName {
			other = "gitleaks"
		}
		for _, msg := range []any{
			overrideLoadedMsg{securityResult: securityResult{gen: e.gen}, editor: e.id + 1, repository: "team/docs", feature: sc.feature, exists: true, override: ports.RepositoryOverrideDetails{Enabled: true, ConfigPath: "/x"}},
			overrideLoadedMsg{securityResult: securityResult{gen: e.gen}, editor: e.id, repository: "team/web", feature: sc.feature, exists: true, override: ports.RepositoryOverrideDetails{Enabled: true}},
			overrideLoadedMsg{securityResult: securityResult{gen: e.gen}, editor: e.id, repository: "team/docs", feature: other, exists: true, override: ports.RepositoryOverrideDetails{Enabled: true}},
		} {
			updated, _ = m.Update(msg)
			if got := ovEditorOf(t, updated.(Model), sc.to); got.exists != wantExists || got.enabled != wantEnabled || got.pathPrimary != e.pathPrimary {
				t.Fatalf("%s: a result meant for another editor was applied: %+v", sc.name, msg)
			}
		}

		// Saving goes to team/docs with this feature, never to a repository the
		// list moved to.
		securityPress(t, m, testKeyEnter())
		if client.lastRepositoryOverrideRepo != "team/docs" || client.lastRepositoryOverrideFeature != sc.feature || client.setRepositoryOverrideCalls != 1 {
			t.Fatalf("%s: saved %q/%q (%d calls), want team/docs/%s once", sc.name, client.lastRepositoryOverrideRepo, client.lastRepositoryOverrideFeature, client.setRepositoryOverrideCalls, sc.feature)
		}
	}
}

func TestOverrideEditorIgnoresTheLateAnswerOfAnEditorThatWasClosed(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		client := ovClient()
		m := ovModelAt(t, client, securityKnown, sc.to, 160, 30)
		m = ovSelect(t, m, sc.to, "team/api")
		updated, firstRead := m.Update(testKeyRunes("o")) // the read is in flight
		m = updated.(Model)
		first := ovEditorOf(t, m, sc.to)
		m = securityPress(t, m, testKeyEsc())
		m = ovSelect(t, m, sc.to, "team/docs")
		m = securityPress(t, m, testKeyRunes("o"))
		second := ovEditorOf(t, m, sc.to)
		if second.id == first.id || second.repository != "team/docs" {
			t.Fatalf("%s: the second editor = %+v, want a new one for team/docs", sc.name, second)
		}
		// The first read finally answers: it must not land on the second editor.
		answer := firstRead()
		if answer == nil {
			t.Fatalf("%s: the first read produced no message", sc.name)
		}
		before := ovEditorOf(t, m, sc.to)
		updated, _ = m.Update(answer)
		if got := ovEditorOf(t, updated.(Model), sc.to); !reflect.DeepEqual(got.enabled, before.enabled) || got.repository != "team/docs" || got.exists != before.exists {
			t.Fatalf("%s: the closed editor's answer changed the open one: %+v", sc.name, got)
		}
	}
}

// A PUT replaces the override in full: a save that changes nothing must send
// back every field the stored override carries.
func TestOverrideEditorSavesEveryFieldOfTheOverride(t *testing.T) {
	t.Parallel()
	identities := []ports.TrustedIdentity{
		{CertificateIdentityRegexp: "https://github.com/b/.*", CertificateOIDCIssuer: "https://issuer.b"},
		{CertificateIdentityRegexp: "https://github.com/a/.*", CertificateOIDCIssuer: "https://issuer.a"},
	}
	cases := []struct {
		name   string
		to     screen
		stored ports.RepositoryOverrideDetails
	}{
		{"trivy", screenSecurityTrivyRepos, ports.RepositoryOverrideDetails{Feature: trivyFeatureName, Enabled: true, IgnoreFilePath: "/etc/trivy/ignore", IgnorePolicyPath: "/etc/trivy/policy.rego"}},
		{"trivy disabled with one path", screenSecurityTrivyRepos, ports.RepositoryOverrideDetails{Feature: trivyFeatureName, Enabled: false, IgnorePolicyPath: "/etc/trivy/policy.rego"}},
		{"gitleaks", screenSecurityGitleaksRepos, ports.RepositoryOverrideDetails{Feature: gitleaksFeatureName, Enabled: true, ConfigPath: "/etc/gitleaks.toml"}},
		{"signing", screenSecuritySigningRepos, ports.RepositoryOverrideDetails{Feature: signingFeatureName, Enabled: true, TrustedPublicKeys: []string{trustedKeyListTestPEM2, trustedKeyListTestPEM1}, TrustedIdentities: identities, UnsignedSelfRead: "repo_push"}},
		{"signing without a self-read mode", screenSecuritySigningRepos, ports.RepositoryOverrideDetails{Feature: signingFeatureName, Enabled: true, TrustedPublicKeys: []string{trustedKeyListTestPEM1}}},
		{"signing off mode written explicitly", screenSecuritySigningRepos, ports.RepositoryOverrideDetails{Feature: signingFeatureName, Enabled: true, TrustedIdentities: identities, UnsignedSelfRead: "off"}},
		{"signing identities only, gate off", screenSecuritySigningRepos, ports.RepositoryOverrideDetails{Feature: signingFeatureName, Enabled: false, TrustedIdentities: identities, UnsignedSelfRead: "pusher"}},
	}
	for _, tc := range cases {
		client := ovClient()
		stored := tc.stored
		stored.Repository = "team/web"
		client.repositoryOverrides[stored.Feature+"/team/web"] = stored
		m := ovOpenEditor(t, client, tc.to, "team/web")
		securityPress(t, m, testKeyEnter())
		if client.setRepositoryOverrideCalls != 1 {
			t.Fatalf("%s: Enter sent %d saves, want 1", tc.name, client.setRepositoryOverrideCalls)
		}
		want := tc.stored
		got := client.lastSetRepositoryOverride
		got.Feature = want.Feature // the client fills these from the request path
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: a no-op save sent\n  %#v\nwant the stored override\n  %#v", tc.name, got, want)
		}
	}
}

func TestOverrideEditorChangesOnlyWhatTheOperatorEdited(t *testing.T) {
	t.Parallel()
	client := ovClient()
	m := ovOpenEditor(t, client, screenSecuritySigningRepos, "team/api")
	m = ovFocus(t, m, screenSecuritySigningRepos, overrideFieldUnsignedSelfRead)
	securityPress(t, m, testKeySpace(), testKeyEnter()) // pusher -> repo_push
	got := client.lastSetRepositoryOverride
	stored := ovClient().repositoryOverrides["signing/team/api"]
	if got.UnsignedSelfRead != "repo_push" || got.Enabled != stored.Enabled ||
		!reflect.DeepEqual(got.TrustedPublicKeys, stored.TrustedPublicKeys) || !reflect.DeepEqual(got.TrustedIdentities, stored.TrustedIdentities) {
		t.Fatalf("saved %#v, want only the mode changed from %#v", got, stored)
	}
}

// Nothing is saved before the stored override was read: that would replace it
// with an empty one.
func TestOverrideEditorCannotSaveBeforeTheOverrideLoaded(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		client := ovClient()
		m := ovModelAt(t, client, securityKnown, sc.to, 160, 30)
		// The read is still in flight: Enter does nothing.
		updated, _ := m.Update(testKeyRunes("o"))
		m = updated.(Model)
		for _, k := range []keyMsg{testKeyEnter(), testKeyTab(), testKeySpace(), testKeyEnter()} {
			var cmd any
			updated, c := m.Update(k)
			m = updated.(Model)
			if c != nil {
				cmd = c
			}
			if cmd != nil {
				t.Fatalf("%s: a key while the override was loading returned a command", sc.name)
			}
		}
		if client.setRepositoryOverrideCalls != 0 || client.clearRepositoryOverrideCalls != 0 {
			t.Fatalf("%s: saved or cleared before the override loaded", sc.name)
		}
		if out := securityRender(m); !strings.Contains(out, "Reading the override…") {
			t.Fatalf("%s: the editor does not say it is reading:\n%s", sc.name, out)
		}

		// A read that fails leaves nothing to save over.
		failing := ovClient()
		failing.getRepositoryOverrideErr = errors.New("cannot read")
		m = ovOpenEditor(t, failing, sc.to, "team/api")
		out := securityRender(m)
		if !strings.Contains(out, "cannot read") || strings.Contains(out, "Save  enter") {
			t.Fatalf("%s: a failed read must show the error and offer no Save:\n%s", sc.name, out)
		}
		m = securityPress(t, m, testKeyEnter(), testKeyTab(), testKeyEnter(), testKeyRunes("y"))
		if failing.setRepositoryOverrideCalls != 0 || failing.clearRepositoryOverrideCalls != 0 {
			t.Fatalf("%s: saved or cleared over an override that could not be read", sc.name)
		}
		m = securityPress(t, m, testKeyEsc())
		if ovEditorOf(t, m, sc.to).Active() {
			t.Fatalf("%s: Esc must close an editor whose read failed", sc.name)
		}
	}
}

func TestOverrideEditorSaveIsOnceInFlightAndAFailureKeepsTheValues(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		client := ovClient()
		m := ovOpenEditor(t, client, sc.to, "team/web")
		if sc.feature == signingFeatureName {
			m = securityPress(t, m, testKeySpace()) // gate on
		} else {
			m = securityPress(t, m, testKeySpace(), testKeyTab())
			m = securityPress(t, m, securityType("/etc/p")...)
		}
		before := ovEditorOf(t, m, sc.to)

		client.setRepositoryOverrideErr = errors.New("rejected: enabled requires a key")
		updated, cmd := m.Update(testKeyEnter())
		m = updated.(Model)
		if cmd == nil {
			t.Fatalf("%s: Enter returned no command", sc.name)
		}
		if _, again := m.Update(testKeyEnter()); again != nil {
			t.Fatalf("%s: Enter while the save is in flight returned a command", sc.name)
		}
		if out := securityRender(m); !strings.Contains(out, "Saving…") {
			t.Fatalf("%s: the editor does not show it is saving:\n%s", sc.name, out)
		}
		m = securityRun(t, m, cmd)

		after := ovEditorOf(t, m, sc.to)
		if !after.Active() || after.busy || after.exists || after.enabled != before.enabled || after.pathPrimary != before.pathPrimary {
			t.Fatalf("%s: after a failed save the editor = %+v, want it open with the typed values", sc.name, after)
		}
		if out := securityRender(m); !strings.Contains(out, "rejected: enabled requires a key") {
			t.Fatalf("%s: the failure is not shown:\n%s", sc.name, out)
		}
		if client.setRepositoryOverrideCalls != 1 {
			t.Fatalf("%s: saves = %d, want 1", sc.name, client.setRepositoryOverrideCalls)
		}

		// Enter works again once the result arrived.
		client.setRepositoryOverrideErr = nil
		m = securityPress(t, m, testKeyEnter())
		if client.setRepositoryOverrideCalls != 2 || !ovEditorOf(t, m, sc.to).exists {
			t.Fatalf("%s: the retry did not save (%d calls)", sc.name, client.setRepositoryOverrideCalls)
		}
		if row, _ := ovListOf(t, m, sc.to).selectedRow(); row.Repository != "team/web" || !row.HasOverride {
			t.Fatalf("%s: the row did not reflect the saved override: %+v", sc.name, row)
		}
	}
}

func TestOverrideEditorClearAsksFirstAndClearsOnlyThatRepository(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		client := ovClient()
		m := ovOpenEditor(t, client, sc.to, "team/api")
		m = ovFocus(t, m, sc.to, overrideFieldClear)

		m = securityPress(t, m, testKeyEnter())
		out := securityRender(m)
		for _, want := range []string{"Clear override", "team/api", "goes back to the global settings", "Only this repository changes", "y / enter", "esc"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%s: the confirmation lacks %q:\n%s", sc.name, want, out)
			}
		}
		if client.clearRepositoryOverrideCalls != 0 {
			t.Fatalf("%s: cleared before the confirmation", sc.name)
		}

		// Esc cancels and keeps the editor.
		m = securityPress(t, m, testKeyEsc())
		if client.clearRepositoryOverrideCalls != 0 || !ovEditorOf(t, m, sc.to).Active() || ovEditorOf(t, m, sc.to).confirm.Active() {
			t.Fatalf("%s: Esc must cancel the confirmation only", sc.name)
		}

		// Any other key does not confirm; y does, once.
		m = securityPress(t, m, testKeyEnter(), testKeyRunes("n"), testKeyTab(), testKeyRunes("Y"))
		if client.clearRepositoryOverrideCalls != 0 {
			t.Fatalf("%s: a key other than y or Enter confirmed the clear", sc.name)
		}
		updated, cmd := m.Update(testKeyRunes("y"))
		m = updated.(Model)
		if _, again := m.Update(testKeyRunes("y")); again != nil {
			t.Fatalf("%s: a second y while the clear is in flight returned a command", sc.name)
		}
		if _, again := m.Update(testKeyEnter()); again != nil {
			t.Fatalf("%s: Enter while the clear is in flight returned a command", sc.name)
		}
		m = securityRun(t, m, cmd)

		if client.clearRepositoryOverrideCalls != 1 || client.lastRepositoryOverrideRepo != "team/api" || client.lastRepositoryOverrideFeature != sc.feature {
			t.Fatalf("%s: cleared %q/%q (%d calls), want team/api/%s once", sc.name, client.lastRepositoryOverrideRepo, client.lastRepositoryOverrideFeature, client.clearRepositoryOverrideCalls, sc.feature)
		}
		if _, still := client.repositoryOverrides[sc.feature+"/team/docs"]; !still {
			t.Fatalf("%s: clearing team/api removed the override of team/docs", sc.name)
		}
		e := ovEditorOf(t, m, sc.to)
		if !e.Active() || e.exists || e.confirm.Active() || e.busy || e.note != "Override cleared." {
			t.Fatalf("%s: after the clear the editor = %+v, want it open and inheriting", sc.name, e)
		}
		row, _ := ovListOf(t, m, sc.to).selectedRow()
		if row.Repository != "team/api" || row.HasOverride {
			t.Fatalf("%s: the row of team/api still has an override: %+v", sc.name, row)
		}
		if other := ovListOf(t, m, sc.to); !hasOverrideRow(other, "team/docs") {
			t.Fatalf("%s: the row of team/docs lost its override", sc.name)
		}

		// With nothing to clear Enter on Clear explains instead of asking.
		m = securityPress(t, m, testKeyEnter())
		if ovEditorOf(t, m, sc.to).confirm.Active() || !strings.Contains(ovEditorOf(t, m, sc.to).err, "Already inheriting") {
			t.Fatalf("%s: clearing an override that does not exist must not ask", sc.name)
		}
	}
}

func hasOverrideRow(l overridesList, repository string) bool {
	for _, row := range l.rows {
		if row.Repository == repository {
			return row.HasOverride
		}
	}
	return false
}

func TestOverrideEditorFailedClearKeepsTheOverrideAndTheEditor(t *testing.T) {
	t.Parallel()
	client := ovClient()
	m := ovOpenEditor(t, client, screenSecurityGitleaksRepos, "team/api")
	m = ovFocus(t, m, screenSecurityGitleaksRepos, overrideFieldClear)
	client.clearRepositoryOverrideErr = errors.New("database is locked")
	m = securityPress(t, m, testKeyEnter(), testKeyRunes("y"))
	e := ovEditorOf(t, m, screenSecurityGitleaksRepos)
	if !e.Active() || !e.exists || e.confirm.Active() || e.busy || !strings.Contains(e.err, "database is locked") {
		t.Fatalf("editor after a failed clear = %+v, want open, still overriding, with the error", e)
	}
	if !hasOverrideRow(ovListOf(t, m, screenSecurityGitleaksRepos), "team/api") {
		t.Fatal("a failed clear removed the row's override")
	}
}

// Esc closes the editor even while a save is running; its answer still updates
// the row and cannot reopen anything.
func TestOverrideEditorEscWhileSavingClosesItAndTheRowStillUpdates(t *testing.T) {
	t.Parallel()
	client := ovClient()
	m := ovOpenEditor(t, client, screenSecurityGitleaksRepos, "team/web")
	m = securityPress(t, m, testKeySpace())
	updated, cmd := m.Update(testKeyEnter())
	m = updated.(Model)
	m = securityPress(t, m, testKeyEsc())
	if ovEditorOf(t, m, screenSecurityGitleaksRepos).Active() {
		t.Fatal("Esc did not close the editor while it was saving")
	}
	m = securityRun(t, m, cmd)
	if ovEditorOf(t, m, screenSecurityGitleaksRepos).Active() {
		t.Fatal("the late save result reopened the editor")
	}
	if !hasOverrideRow(ovListOf(t, m, screenSecurityGitleaksRepos), "team/web") {
		t.Fatal("the saved override is not on the row of team/web")
	}
}

// ---- signing: trusted lists ----

func TestOverrideEditorSigningKeyListIsReachableWithoutTabbingToIt(t *testing.T) {
	t.Parallel()
	to := screenSecuritySigningRepos
	client := ovClient()
	client.signingKeyUsageCount = 7
	m := ovOpenEditor(t, client, to, "team/api")
	if ovEditorOf(t, m, to).currentField() != overrideFieldEnabled {
		t.Fatal("a fresh editor must start on Enabled")
	}

	// n adds a key from the first field; the focus snaps to the list.
	m = securityPress(t, m, testKeyRunes("n"))
	e := ovEditorOf(t, m, to)
	if !e.keys.adding || e.currentField() != overrideFieldPathPrimary {
		t.Fatalf("n did not start adding on the key list: %+v", e.keys)
	}
	m = pasteInto(t, m, trustedKeyListTestPEM2)
	m = securityPress(t, m, testKeyEnter())
	if got := len(ovEditorOf(t, m, to).keys.Keys()); got != 2 {
		t.Fatalf("keys = %d after adding one, want 2", got)
	}

	// Esc cancels an add without closing the editor; the next one closes it.
	m = securityPress(t, m, testKeyRunes("n"), testKeyEsc())
	if e := ovEditorOf(t, m, to); !e.Active() || e.keys.adding {
		t.Fatal("Esc must only cancel the add")
	}

	// x asks how many images the key verifies, in THIS repository, then confirms.
	m = securityPress(t, m, testKeyRunes("x"))
	if client.signingKeyUsageCalls != 1 || client.lastSigningKeyUsageRepo != "team/api" {
		t.Fatalf("usage lookups = %d for %q, want one for team/api", client.signingKeyUsageCalls, client.lastSigningKeyUsageRepo)
	}
	if out := securityRender(m); !strings.Contains(out, "This key currently verifies 7") {
		t.Fatalf("the delete confirmation lacks the usage count:\n%s", out)
	}
	m = securityPress(t, m, testKeyEnter())
	if got := len(ovEditorOf(t, m, to).keys.Keys()); got != 1 {
		t.Fatalf("keys = %d after deleting one, want 1", got)
	}
	if client.setRepositoryOverrideCalls != 0 {
		t.Fatal("deleting a key from the list saved the override")
	}
}

func TestOverrideEditorSigningKeyUsageAnswerOfAnotherEditorIsDropped(t *testing.T) {
	t.Parallel()
	to := screenSecuritySigningRepos
	client := ovClient()
	m := ovOpenEditor(t, client, to, "team/api")
	updated, cmd := m.Update(testKeyRunes("x"))
	m = updated.(Model)
	e := ovEditorOf(t, m, to)
	if !e.keys.usageLoading {
		t.Fatal("x did not start the usage lookup")
	}
	msg := cmd().(overrideKeyUsageMsg)
	msg.editor = e.id + 1
	updated, _ = m.Update(msg)
	if got := ovEditorOf(t, updated.(Model), to); !got.keys.usageLoading || got.keys.confirm.Active() {
		t.Fatal("the answer of another editor opened the confirmation")
	}
	msg.editor = e.id
	updated, _ = m.Update(msg)
	if got := ovEditorOf(t, updated.(Model), to); got.keys.usageLoading || !got.keys.confirm.Active() {
		t.Fatal("the editor's own answer did not open the confirmation")
	}
}

func TestOverrideEditorSigningIdentityListIsReachableOnceTabbedTo(t *testing.T) {
	t.Parallel()
	to := screenSecuritySigningRepos
	client := ovClient()
	m := ovOpenEditor(t, client, to, "team/docs")
	m = ovFocus(t, m, to, overrideFieldIdentities)
	m = securityPress(t, m, testKeyRunes("n"))
	m = securityPress(t, m, securityType("^dev@example.com$")...)
	m = securityPress(t, m, testKeyTab())
	m = securityPress(t, m, securityType("https://issuer.example")...)
	m = securityPress(t, m, testKeyEnter())
	if ids := ovEditorOf(t, m, to).identities.Identities(); len(ids) != 1 || ids[0].CertificateIdentityRegexp != "^dev@example.com$" {
		t.Fatalf("identities = %+v, want the new one", ids)
	}
	m = ovFocus(t, m, to, overrideFieldEnabled)
	securityPress(t, m, testKeySpace(), testKeyEnter())
	if got := client.lastSetRepositoryOverride; len(got.TrustedIdentities) != 1 || !got.Enabled {
		t.Fatalf("saved %#v, want the new identity and the gate on", got)
	}
}

// ---- signing: seeding a new override with the global keys ----

func TestOverrideEditorSeedsAnOverrideThatDoesNotExistYetWithTheGlobalKeys(t *testing.T) {
	t.Parallel()
	to := screenSecuritySigningRepos
	client := ovClient()
	m := ovOpenEditor(t, client, to, "team/web") // no override stored
	e := ovEditorOf(t, m, to)
	if e.exists || len(e.keys.Keys()) != 2 || !e.prefillApplied {
		t.Fatalf("editor = exists %v, keys %d, prefill %v, want a new override seeded with the 2 global keys", e.exists, len(e.keys.Keys()), e.prefillApplied)
	}

	// An override that exists keeps its own keys.
	m = ovOpenEditor(t, ovClient(), to, "team/api")
	if got := ovEditorOf(t, m, to).keys.Keys(); len(got) != 1 {
		t.Fatalf("keys of an existing override = %d, want its own 1", len(got))
	}
}

func TestOverrideEditorPrefillIsOrderIndependentAndRunsOnce(t *testing.T) {
	t.Parallel()
	env := screenEnv{}
	policy := func(e overrideEditor, keys ...string) overrideGlobalMsg {
		return overrideGlobalMsg{securityResult: securityResult{gen: e.gen}, editor: e.id, enabled: true, keys: keys}
	}
	loaded := func(e overrideEditor, exists bool, keys ...string) overrideLoadedMsg {
		return overrideLoadedMsg{securityResult: securityResult{gen: e.gen}, editor: e.id, repository: e.repository, feature: e.feature, exists: exists,
			override: ports.RepositoryOverrideDetails{Enabled: true, TrustedPublicKeys: keys}}
	}

	// The global settings answer first.
	e := newOverrideEditor(1, signingFeatureName, "team/web")
	e = e.applyGlobal(policy(e, trustedKeyListTestPEM1))
	if len(e.keys.Keys()) != 0 || e.prefillApplied {
		t.Fatal("the prefill ran before the override was read")
	}
	e, cmd := e.applyLoaded(env, loaded(e, false))
	if len(e.keys.Keys()) != 1 || !e.enabled || cmd != nil {
		t.Fatalf("keys = %d, enabled = %v, cmd = %v, want the global key and switch seeded and no second read", len(e.keys.Keys()), e.enabled, cmd != nil)
	}

	// The override answers first and asks for the global settings.
	e = newOverrideEditor(1, signingFeatureName, "team/web")
	e, cmd = e.applyLoaded(env, loaded(e, false))
	if cmd == nil {
		t.Fatal("an override that does not exist must read the global settings")
	}
	e = e.applyGlobal(policy(e, trustedKeyListTestPEM1, trustedKeyListTestPEM2))
	if len(e.keys.Keys()) != 2 || !e.ready() {
		t.Fatalf("keys = %d, want 2 seeded and the form ready", len(e.keys.Keys()))
	}
	// Only once: a second or late answer never overwrites what the operator
	// edited, never resets the switch and never clears dirty.
	e.keys = e.keys.SetKeys([]string{trustedKeyListTestPEM2})
	e.enabled, e.dirty = false, true
	late := policy(e, trustedKeyListTestPEM1)
	late.enabled = true
	e = e.applyGlobal(late)
	if got := e.keys.Keys(); len(got) != 1 || got[0] != trustedKeyListTestPEM2 {
		t.Fatalf("a late global answer replaced the operator's key: %v", got)
	}
	if e.enabled || !e.dirty {
		t.Fatalf("a late global answer reset enabled=%v dirty=%v, want the operator's false and true", e.enabled, e.dirty)
	}

	// An override that exists is never overwritten, whatever the order.
	e = newOverrideEditor(1, signingFeatureName, "team/api")
	e = e.applyGlobal(policy(e, trustedKeyListTestPEM1, trustedKeyListTestPEM2))
	e, _ = e.applyLoaded(env, loaded(e, true, trustedKeyListTestPEM2))
	if got := e.keys.Keys(); len(got) != 1 || got[0] != trustedKeyListTestPEM2 {
		t.Fatalf("keys = %v, want the stored override's own key", got)
	}

	// An answer for another editor is dropped.
	e = newOverrideEditor(1, signingFeatureName, "team/web")
	other := policy(e, trustedKeyListTestPEM1)
	other.editor++
	e = e.applyGlobal(other)
	if e.globalLoaded {
		t.Fatal("the global answer of another editor was taken")
	}
}

// ---- a new override is never created by an untouched editor ----

func ovSetGlobalEnabled(c *fakeAdminClient, enabled bool) {
	for _, feature := range []string{trivyFeatureName, gitleaksFeatureName} {
		page := c.featurePages[feature]
		page.Summary.Enabled = enabled
		c.featurePages[feature] = page
	}
	c.signingPolicy.Enabled = enabled
}

func TestOverrideEditorUntouchedEnterDoesNotCreateAnOverride(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		for _, global := range []bool{true, false} {
			client := ovClient()
			ovSetGlobalEnabled(client, global)
			m := ovOpenEditor(t, client, sc.to, "team/web") // no override stored
			m = securityPress(t, m, testKeyEnter())
			if client.setRepositoryOverrideCalls != 0 {
				t.Fatalf("%s (global %v): Enter on an untouched editor created an override: %#v", sc.name, global, client.lastSetRepositoryOverride)
			}
			if e := ovEditorOf(t, m, sc.to); !strings.Contains(e.err, "No changes") || !e.Active() || e.exists {
				t.Fatalf("%s: editor = err %q, want it open with a no-changes message", sc.name, e.err)
			}
			if out := securityRender(m); !strings.Contains(out, "No changes") {
				t.Fatalf("%s: the message is not shown:\n%s", sc.name, out)
			}
			// Focus elsewhere and Enter again: still nothing.
			m = securityPress(t, m, testKeyTab(), testKeyEnter())
			if client.setRepositoryOverrideCalls != 0 {
				t.Fatalf("%s: Enter after Tab created an override", sc.name)
			}
		}
	}
}

// The first thing an operator changes starts from the GLOBAL settings: a new
// override is never silently off.
func TestOverrideEditorNewOverrideStartsFromTheGlobalEnabledValue(t *testing.T) {
	t.Parallel()
	for _, sc := range ovScreens {
		for _, global := range []bool{true, false} {
			client := ovClient()
			ovSetGlobalEnabled(client, global)
			m := ovOpenEditor(t, client, sc.to, "team/web")
			if e := ovEditorOf(t, m, sc.to); e.enabled != global || e.exists {
				t.Fatalf("%s: a new override starts enabled=%v, want the global %v", sc.name, e.enabled, global)
			}
			// Change something that is not the switch.
			if sc.feature == signingFeatureName {
				m = ovFocus(t, m, sc.to, overrideFieldUnsignedSelfRead)
				m = securityPress(t, m, testKeySpace(), testKeyEnter())
			} else {
				m = securityPress(t, m, testKeyTab())
				m = securityPress(t, m, securityType("/etc/x")...)
				m = securityPress(t, m, testKeyEnter())
			}
			if client.setRepositoryOverrideCalls != 1 || client.lastSetRepositoryOverride.Enabled != global {
				t.Fatalf("%s (global %v): saved %d times with enabled=%v", sc.name, global, client.setRepositoryOverrideCalls, client.lastSetRepositoryOverride.Enabled)
			}
			if sc.feature == signingFeatureName && len(client.lastSetRepositoryOverride.TrustedPublicKeys) != 2 {
				t.Fatalf("signing: the new override did not start from the global keys: %#v", client.lastSetRepositoryOverride)
			}
		}
	}
}

// Until the global settings were read a new override cannot be edited or
// saved, and if they cannot be read it never can.
func TestOverrideEditorNewOverrideWaitsForTheGlobalSettings(t *testing.T) {
	t.Parallel()
	for _, feature := range []string{trivyFeatureName, gitleaksFeatureName, signingFeatureName} {
		env := screenEnv{}
		e := newOverrideEditor(1, feature, "team/web")
		e, cmd := e.applyLoaded(env, overrideLoadedMsg{securityResult: securityResult{gen: 1}, editor: e.id, repository: "team/web", feature: feature})
		if cmd == nil {
			t.Fatalf("%s: a new override must read the global settings", feature)
		}
		for _, k := range []keyMsg{testKeySpace(), testKeyTab(), testKeyRunes("x"), testKeyEnter()} {
			next, c, _ := e.update(env, k)
			if c != nil || next.dirty || next.enabled != e.enabled || next.focus != e.focus {
				t.Fatalf("%s: a key before the global settings arrived changed the editor", feature)
			}
		}
		failed := e.applyGlobalFailure(overrideGlobalMsg{securityResult: securityResult{gen: 1, err: errors.New("boom")}, editor: e.id})
		if _, c, _ := failed.update(env, testKeyEnter()); c != nil || !strings.Contains(failed.err, "boom") {
			t.Fatalf("%s: with the global settings unreadable the editor must show why and not save (err %q)", feature, failed.err)
		}
	}
}

func TestOverrideEditorAfterAClearTheNextSaveNeedsAChange(t *testing.T) {
	t.Parallel()
	client := ovClient()
	m := ovOpenEditor(t, client, screenSecurityGitleaksRepos, "team/api")
	m = ovFocus(t, m, screenSecurityGitleaksRepos, overrideFieldClear)
	m = securityPress(t, m, testKeyEnter(), testKeyRunes("y"))
	e := ovEditorOf(t, m, screenSecurityGitleaksRepos)
	if e.exists || e.dirty || !e.prefillApplied {
		t.Fatalf("editor after a clear = %+v, want inheriting, untouched and holding the global settings", e)
	}
	m = ovFocus(t, m, screenSecurityGitleaksRepos, overrideFieldEnabled)
	m = securityPress(t, m, testKeyEnter())
	if client.setRepositoryOverrideCalls != 0 {
		t.Fatal("Enter right after a clear re-created the override")
	}
}
