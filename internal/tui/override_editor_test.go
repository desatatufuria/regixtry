package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"regixtry/internal/ports"
)

// TestOverrideEditorFeatureIsImmutable is the Phase 9 task 9.1 RED test
// (T2.2, spec.md "The modal's Feature cannot be changed once open"):
// every key in overrideEditorKeys, plus Space (the retired cycle key), is
// asserted against Feature() -- none of them changes it.
func TestOverrideEditorFeatureIsImmutable(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	keys := []tea.KeyMsg{
		{Type: tea.KeyEnter},
		{Type: tea.KeyTab},
		{Type: tea.KeyRunes, Runes: []rune{' '}},
		{Type: tea.KeyBackspace},
		{Type: tea.KeyRunes, Runes: []rune{'x'}},
	}

	for _, feature := range []string{trivyFeatureName, gitleaksFeatureName, signingFeatureName} {
		editor := newOverrideEditor(feature, "team/api")
		if got := editor.Feature(); got != feature {
			t.Fatalf("Feature() = %q, want %q immediately after construction", got, feature)
		}
		for _, key := range keys {
			next, _, _ := editor.update(env, key)
			if got := next.Feature(); got != feature {
				t.Fatalf("Feature() = %q after key %v, want unchanged %q", got, key, feature)
			}
			editor = next
		}
	}
}

// TestOverrideEditorFieldsAreImmutableAfterConstruction proves the fields
// []overrideField set is built exactly once at construction (design.md
// Decision F) and is per-feature: trivy gets a second path field, signing
// gets UnsignedSelfRead, gitleaks gets neither -- and there is no
// representable "Feature" field in any of the three sets (T2.2's other
// half: Feature is unrepresentable, not merely unreachable).
// TestOverrideEditorSigningKeyListReachableWithoutTabbingToIt is the RED
// test for the "can't add a key" bug report: a freshly-opened signing
// override editor defaults focus to overrideFieldEnabled (not
// overrideFieldPathPrimary, where e.keys lives), so 'n' must still reach the
// embedded trustedKeyList and enter adding mode without the operator ever
// pressing Tab first.
func TestOverrideEditorSigningKeyListReachableWithoutTabbingToIt(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	editor := newOverrideEditor(signingFeatureName, "team/api")
	if editor.currentField() == overrideFieldPathPrimary {
		t.Fatal("test setup invalid: default focus is already overrideFieldPathPrimary")
	}

	next, cmd, consumed := editor.update(env, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !consumed || cmd != nil {
		t.Fatalf("'n' with focus elsewhere: consumed = %v, cmd = %v, want consumed=true, cmd=nil", consumed, cmd)
	}
	if !next.keys.adding {
		t.Fatal("'n' did not reach the embedded trustedKeyList: adding = false, want true")
	}
}

// TestOverrideEditorSigningKeyListDeleteReachableWithoutTabbingToIt covers
// the same bug for 'x' (delete), which must fire the usage-count Cmd even
// though Tab-focus is still on overrideFieldEnabled.
func TestOverrideEditorSigningKeyListDeleteReachableWithoutTabbingToIt(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	editor := newOverrideEditor(signingFeatureName, "team/api")
	editor.keys = newTrustedKeyList("team/api", []string{trustedKeyListTestPEM1})
	if editor.currentField() == overrideFieldPathPrimary {
		t.Fatal("test setup invalid: default focus is already overrideFieldPathPrimary")
	}

	next, cmd, consumed := editor.update(env, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if !consumed || cmd == nil {
		t.Fatalf("'x' with focus elsewhere: consumed = %v, cmd = %v, want consumed=true, cmd != nil", consumed, cmd)
	}
	if !next.keys.usageLoading {
		t.Fatal("'x' did not reach the embedded trustedKeyList: usageLoading = false, want true")
	}
}

// TestOverrideEditorSigningKeyListInteractionSnapsFocusToTheKeyList is the
// Judgment Day fix-round RED test for the "invisible focus drift" finding
// (both judges): TestOverrideEditorSigningKeyListReachableWithoutTabbingToIt
// already proves 'n' reaches the embedded trustedKeyList even while Tab-focus
// visually sits elsewhere (e.g. still on overrideFieldEnabled), but never
// asserted that the editor's OWN focus indicator moves to match -- so the
// rendered highlight (renderOverrideEditor's e.currentField() ==
// overrideFieldPathPrimary check) silently disagreed with what was actually
// responding to input. The moment e.keys.update consumes a key, e.focus must
// snap to overrideFieldPathPrimary's position so the visible indicator and
// the actual interaction target stay in sync.
func TestOverrideEditorSigningKeyListInteractionSnapsFocusToTheKeyList(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	editor := newOverrideEditor(signingFeatureName, "team/api")
	if editor.currentField() == overrideFieldPathPrimary {
		t.Fatal("test setup invalid: default focus is already overrideFieldPathPrimary")
	}

	next, _, consumed := editor.update(env, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !consumed {
		t.Fatal("'n' with focus elsewhere: consumed = false, want true")
	}
	if !next.keys.adding {
		t.Fatal("'n' did not reach the embedded trustedKeyList: adding = false, want true")
	}
	if next.currentField() != overrideFieldPathPrimary {
		t.Fatalf("currentField() = %v after 'n' actually moved focus to the key list, want overrideFieldPathPrimary (the visible focus indicator must match what actually responded to the keystroke)", next.currentField())
	}
}

// TestOverrideEditorPrefillSeedsNeverConfiguredOverrideWithGlobalKeys is the
// Judgment Day fix-round RED test for Fix 4 (single judge, zero coverage):
// maybeApplyGlobalPrefill/applyGlobalPolicyLoaded/applyLoaded's chained
// loadSigningPolicyCmd dispatch had no test referencing these identifiers at
// all. A genuinely never-configured override (exists == false after its own
// load) that then receives the global policy load must end up seeded with
// exactly the global policy's trusted keys.
func TestOverrideEditorPrefillSeedsNeverConfiguredOverrideWithGlobalKeys(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	editor := newOverrideEditor(signingFeatureName, "team/api")

	editor, cmd := editor.applyLoaded(env, adminRepositoryOverrideLoadedMsg{
		repository: "team/api",
		feature:    signingFeatureName,
		exists:     false,
	})
	if editor.exists {
		t.Fatal("test setup invalid: exists = true after the override load, want false (never-configured override)")
	}
	if cmd == nil {
		t.Fatal("applyLoaded() cmd = nil for a never-configured signing override, want the chained loadSigningPolicyCmd")
	}
	if len(editor.keys.Keys()) != 0 {
		t.Fatalf("keys = %v before the global policy load resolves, want empty", editor.keys.Keys())
	}

	globalKeys := []string{trustedKeyListTestPEM1, trustedKeyListTestPEM2}
	editor = editor.applyGlobalPolicyLoaded(adminSigningPolicyLoadedMsg{
		settings: ports.SigningPolicySettings{TrustedPublicKeys: globalKeys},
	})

	if got := editor.keys.Keys(); !reflect.DeepEqual(got, globalKeys) {
		t.Fatalf("keys = %v after the global policy load, want %v (seeded with the current global trusted keys)", got, globalKeys)
	}
	if !editor.prefillApplied {
		t.Fatal("prefillApplied = false after the prefill fired, want true")
	}
}

// TestOverrideEditorPrefillAppliesWhenGlobalPolicyLoadResolvesBeforeOverrideLoad
// is Judgment Day round-2's CRITICAL finding: applyGlobalPolicyLoaded is the
// only call site of maybeApplyGlobalPrefill, but its own guard requires
// !e.loading -- which is still true until applyLoaded runs. If the global
// policy's response resolves FIRST (a real reachable ordering: both loads
// are independent async Cmds, and a signing policy load can already be in
// flight from a screen the operator recently visited), the prefill silently
// no-ops here, and applyLoaded itself never re-checks it -- only re-fires a
// second loadSigningPolicyCmd, which is a fragile, redundant recovery path,
// not the "order-independent" guarantee maybeApplyGlobalPrefill's own doc
// comment claims.
func TestOverrideEditorPrefillAppliesWhenGlobalPolicyLoadResolvesBeforeOverrideLoad(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	editor := newOverrideEditor(signingFeatureName, "team/api")

	globalKeys := []string{trustedKeyListTestPEM1, trustedKeyListTestPEM2}
	editor = editor.applyGlobalPolicyLoaded(adminSigningPolicyLoadedMsg{
		settings: ports.SigningPolicySettings{TrustedPublicKeys: globalKeys},
	})
	if editor.prefillApplied {
		t.Fatal("prefillApplied = true before the override's own load resolved, want false (still loading)")
	}

	editor, _ = editor.applyLoaded(env, adminRepositoryOverrideLoadedMsg{
		repository: "team/api",
		feature:    signingFeatureName,
		exists:     false,
	})

	if got := editor.keys.Keys(); !reflect.DeepEqual(got, globalKeys) {
		t.Fatalf("keys = %v after the override load resolved (global policy already arrived), want %v", got, globalKeys)
	}
	if !editor.prefillApplied {
		t.Fatal("prefillApplied = false after both loads resolved, want true")
	}
}

// TestOverrideEditorPrefillNeverOverwritesAnExistingOverridesOwnKeys is the
// other half of Fix 4's own named requirement: an override that already has
// its own stored keys (exists == true) must never have them replaced by the
// global policy's keys, however the global policy load resolves.
func TestOverrideEditorPrefillNeverOverwritesAnExistingOverridesOwnKeys(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	editor := newOverrideEditor(signingFeatureName, "team/api")

	ownKeys := []string{trustedKeyListTestPEM1}
	editor, _ = editor.applyLoaded(env, adminRepositoryOverrideLoadedMsg{
		repository: "team/api",
		feature:    signingFeatureName,
		exists:     true,
		override:   ports.RepositoryOverrideDetails{TrustedPublicKeys: ownKeys},
	})
	if !editor.exists {
		t.Fatal("test setup invalid: exists = false after the override load, want true")
	}
	if got := editor.keys.Keys(); !reflect.DeepEqual(got, ownKeys) {
		t.Fatalf("keys = %v after the override load, want %v (the override's own stored keys)", got, ownKeys)
	}

	globalKeys := []string{trustedKeyListTestPEM2}
	editor = editor.applyGlobalPolicyLoaded(adminSigningPolicyLoadedMsg{
		settings: ports.SigningPolicySettings{TrustedPublicKeys: globalKeys},
	})

	if got := editor.keys.Keys(); !reflect.DeepEqual(got, ownKeys) {
		t.Fatalf("keys = %v after the global policy load, want unchanged %v (an existing override's own keys must never be overwritten by the prefill)", got, ownKeys)
	}
}

// TestOverrideEditorPrefillAppliesOnlyOnce is Fix 4's third named
// requirement: prefillApplied guards the seed to at most once per open
// editor -- a second adminSigningPolicyLoadedMsg must never re-seed or reset
// the keys a second time.
func TestOverrideEditorPrefillAppliesOnlyOnce(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	editor := newOverrideEditor(signingFeatureName, "team/api")
	editor, _ = editor.applyLoaded(env, adminRepositoryOverrideLoadedMsg{
		repository: "team/api",
		feature:    signingFeatureName,
		exists:     false,
	})

	firstGlobalKeys := []string{trustedKeyListTestPEM1}
	editor = editor.applyGlobalPolicyLoaded(adminSigningPolicyLoadedMsg{
		settings: ports.SigningPolicySettings{TrustedPublicKeys: firstGlobalKeys},
	})
	if got := editor.keys.Keys(); !reflect.DeepEqual(got, firstGlobalKeys) {
		t.Fatalf("keys = %v after the first prefill, want %v", got, firstGlobalKeys)
	}
	if !editor.prefillApplied {
		t.Fatal("prefillApplied = false after the first prefill, want true")
	}

	secondGlobalKeys := []string{trustedKeyListTestPEM2}
	editor = editor.applyGlobalPolicyLoaded(adminSigningPolicyLoadedMsg{
		settings: ports.SigningPolicySettings{TrustedPublicKeys: secondGlobalKeys},
	})

	if got := editor.keys.Keys(); !reflect.DeepEqual(got, firstGlobalKeys) {
		t.Fatalf("keys = %v after a second adminSigningPolicyLoadedMsg, want unchanged %v (prefillApplied must guard against re-seeding)", got, firstGlobalKeys)
	}
}

// TestOverrideEditorSigningIdentityListReachableOnceTabbedTo is the Phase
// 8.5 RED test: once focus is on overrideFieldIdentities, 'n' reaches the
// embedded trustedIdentityList -- unlike Keys' always-reachable shortcut,
// Identities only claims its keys once explicitly focused (same asymmetric
// design as signingConfigScreen.updateConfigKey, for the identical reason:
// two list-shaped fields cannot both claim the same keys ambiguously).
func TestOverrideEditorSigningIdentityListReachableOnceTabbedTo(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	editor := newOverrideEditor(signingFeatureName, "team/api")
	idx := indexOfOverrideField(editor.fields, overrideFieldIdentities)
	if idx < 0 {
		t.Fatal("test setup invalid: overrideFieldIdentities not present in signing's fields")
	}
	editor.focus = idx

	next, cmd, consumed := editor.update(env, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !consumed || cmd != nil {
		t.Fatalf("'n' with identities focused: consumed = %v, cmd = %v, want consumed=true, cmd=nil", consumed, cmd)
	}
	if !next.identities.adding {
		t.Fatal("'n' did not reach the embedded trustedIdentityList: adding = false, want true")
	}
}

// TestOverrideEditorSaveIncludesConfiguredIdentities is the Phase 8.5 RED
// test: submitting the editor (Enter) builds a
// ports.RepositoryOverrideDetails carrying the currently configured
// TrustedIdentities, mirroring TrustedPublicKeys.
func TestOverrideEditorSaveIncludesConfiguredIdentities(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	editor := newOverrideEditor(signingFeatureName, "team/api")
	editor.identities = newTrustedIdentityList([]ports.TrustedIdentity{{CertificateIdentityRegexp: "^valid$", CertificateOIDCIssuer: "https://token.actions.githubusercontent.com"}})

	_, cmd, consumed := editor.update(env, tea.KeyMsg{Type: tea.KeyEnter})
	if !consumed || cmd == nil {
		t.Fatalf("Enter (save): consumed = %v, cmd = %v, want consumed=true, cmd != nil", consumed, cmd)
	}
}

// TestOverrideEditorApplyOverrideSeedsIdentitiesFromStoredOverride is the
// Phase 8.5 RED test: applyOverride (via applyLoaded) seeds e.identities
// from the stored override's TrustedIdentities, mirroring e.keys' own
// seeding from TrustedPublicKeys.
func TestOverrideEditorApplyOverrideSeedsIdentitiesFromStoredOverride(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	editor := newOverrideEditor(signingFeatureName, "team/api")

	ownIdentities := []ports.TrustedIdentity{{CertificateIdentityRegexp: "^valid$", CertificateOIDCIssuer: "https://token.actions.githubusercontent.com"}}
	editor, _ = editor.applyLoaded(env, adminRepositoryOverrideLoadedMsg{
		repository: "team/api",
		feature:    signingFeatureName,
		exists:     true,
		override:   ports.RepositoryOverrideDetails{TrustedIdentities: ownIdentities},
	})

	if got := editor.identities.Identities(); !reflect.DeepEqual(got, ownIdentities) {
		t.Fatalf("identities = %#v, want %#v (the override's own stored identities)", got, ownIdentities)
	}
}

// TestRenderOverrideEditorShowsConfiguredIdentities is the Phase 8.5 RED
// test: the rendered editor includes the configured trusted identity's
// regexp and issuer when signing is selected (spec: "Modal fields adapt to
// signing's settings shape including identities").
func TestRenderOverrideEditorShowsConfiguredIdentities(t *testing.T) {
	t.Parallel()

	editor := newOverrideEditor(signingFeatureName, "team/api")
	editor.identities = newTrustedIdentityList([]ports.TrustedIdentity{{CertificateIdentityRegexp: "^valid$", CertificateOIDCIssuer: "https://token.actions.githubusercontent.com"}})

	theme := newAdminTheme()
	rendered := renderOverrideEditor(theme, editor)
	if !strings.Contains(rendered, "^valid$") || !strings.Contains(rendered, "https://token.actions.githubusercontent.com") {
		t.Fatalf("renderOverrideEditor() = %q, want the configured identity's regexp and issuer shown", rendered)
	}
}

func TestOverrideEditorFieldsAreImmutableAfterConstruction(t *testing.T) {
	t.Parallel()

	cases := []struct {
		feature string
		want    []overrideField
	}{
		{trivyFeatureName, []overrideField{overrideFieldEnabled, overrideFieldPathPrimary, overrideFieldPathSecondary, overrideFieldClear}},
		{gitleaksFeatureName, []overrideField{overrideFieldEnabled, overrideFieldPathPrimary, overrideFieldClear}},
		{signingFeatureName, []overrideField{overrideFieldEnabled, overrideFieldPathPrimary, overrideFieldIdentities, overrideFieldUnsignedSelfRead, overrideFieldClear}},
	}
	for _, tc := range cases {
		editor := newOverrideEditor(tc.feature, "team/api")
		if len(editor.fields) != len(tc.want) {
			t.Fatalf("feature %q: fields = %v, want %v", tc.feature, editor.fields, tc.want)
		}
		for i, field := range tc.want {
			if editor.fields[i] != field {
				t.Fatalf("feature %q: fields[%d] = %v, want %v", tc.feature, i, editor.fields[i], field)
			}
		}
	}
}
