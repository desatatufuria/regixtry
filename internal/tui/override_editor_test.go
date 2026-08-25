package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
func TestOverrideEditorFieldsAreImmutableAfterConstruction(t *testing.T) {
	t.Parallel()

	cases := []struct {
		feature string
		want    []overrideField
	}{
		{trivyFeatureName, []overrideField{overrideFieldEnabled, overrideFieldPathPrimary, overrideFieldPathSecondary, overrideFieldClear}},
		{gitleaksFeatureName, []overrideField{overrideFieldEnabled, overrideFieldPathPrimary, overrideFieldClear}},
		{signingFeatureName, []overrideField{overrideFieldEnabled, overrideFieldPathPrimary, overrideFieldUnsignedSelfRead, overrideFieldClear}},
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
