package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const pasteSample = "pasted text"

func pasteInto(t *testing.T, m Model, content string) Model {
	t.Helper()
	updated, _ := m.Update(tea.PasteMsg{Content: content})
	return updated.(Model)
}

func TestPasteReachesAdminTextFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(m *Model)
		got   func(m Model) string
		want  string
	}{
		{"create user username", func(m *Model) { mountCreateUser(m, createUserFieldUsername) },
			func(m Model) string { return createUserOf(m).username }, pasteSample},
		{"create user password", func(m *Model) { mountCreateUser(m, createUserFieldPassword) },
			func(m Model) string { return createUserOf(m).password }, pasteSample},
		{"reset password", mountResetPassword,
			func(m Model) string { return resetPasswordOf(m).password }, pasteSample},
		{"create token name", func(m *Model) { m.screen = screenAdminCreateToken },
			func(m Model) string { return m.adminView.TokenForm.Name }, pasteSample},
		{"create robot name", func(m *Model) { m.screen = screenAdminCreateRobot },
			func(m Model) string { return m.adminView.CreateRobotForm.Name }, pasteSample},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := charAdminModel(t, 160, 30)
			tt.setup(&m)
			m = pasteInto(t, m, pasteSample)
			if got := tt.got(m); got != tt.want {
				t.Fatalf("value after paste = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPastedPasswordIsNeverRenderedInClear(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		setup func(m *Model)
	}{
		{"create user", func(m *Model) { mountCreateUser(m, createUserFieldPassword) }},
		{"reset password", mountResetPassword},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := charAdminModel(t, 160, 30)
			tt.setup(&m)
			m = pasteInto(t, m, "s3cr3t-pasted")
			if view := ansi.Strip(m.View().Content); strings.Contains(view, "s3cr3t-pasted") {
				t.Fatalf("pasted password rendered in clear:\n%s", view)
			}
		})
	}
}

func TestPasteReachesOverrideEditorFields(t *testing.T) {
	t.Parallel()

	open := func(t *testing.T, feature string) (Model, *overrideEditor) {
		t.Helper()
		m := charAdminModel(t, 160, 30)
		m.screen = screenSecuritySigningRepos
		editor := newOverrideEditor(feature, "team/api")
		editor.loading = false
		return m, &editor
	}
	install := func(m *Model, e overrideEditor) {
		m.adminScreens[slotSigningRepos] = featureOverridesScreen{id: screenSecuritySigningRepos, feature: e.feature, editor: e}
	}
	editorOf := func(m Model) overrideEditor {
		return m.adminScreens[slotSigningRepos].(featureOverridesScreen).editor
	}

	t.Run("path field", func(t *testing.T) {
		t.Parallel()
		m, e := open(t, trivyFeatureName)
		e.focus = indexOfOverrideField(e.fields, overrideFieldPathPrimary)
		install(&m, *e)
		m = pasteInto(t, m, "/etc/trivy/ignore")
		if got := editorOf(m).pathPrimary; got != "/etc/trivy/ignore" {
			t.Fatalf("pathPrimary = %q, want the pasted path", got)
		}
	})

	t.Run("trusted key list keeps multiline content", func(t *testing.T) {
		t.Parallel()
		m, e := open(t, signingFeatureName)
		install(&m, *e)
		m = testSendKey(t, m, testKeyRunes("n"))
		m = pasteInto(t, m, trustedKeyListTestPEM1)
		got := editorOf(m).keys
		if !got.adding || got.input != trustedKeyListTestPEM1 {
			t.Fatalf("keys adding=%v input=%q, want the whole pasted PEM in the add buffer", got.adding, got.input)
		}
	})

	t.Run("trusted identity list", func(t *testing.T) {
		t.Parallel()
		m, e := open(t, signingFeatureName)
		e.focus = indexOfOverrideField(e.fields, overrideFieldIdentities)
		install(&m, *e)
		m = testSendKey(t, m, testKeyRunes("n"))
		m = pasteInto(t, m, "https://example.com/.*")
		got := editorOf(m).identities
		if !got.adding || got.inputRegexp != "https://example.com/.*" {
			t.Fatalf("identities adding=%v inputRegexp=%q, want the pasted regexp", got.adding, got.inputRegexp)
		}
	})

	t.Run("single pasted rune can match a key helper as in v1", func(t *testing.T) {
		t.Parallel()
		m, e := open(t, signingFeatureName)
		install(&m, *e)
		m = pasteInto(t, m, "n")
		if !editorOf(m).keys.adding {
			t.Fatal("a pasted \"n\" did not open the key add mode, as a typed 'n' does")
		}
	})
}

// A real Space key is typed into text fields; Bubble Tea v1 delivered it as
// KeySpace, which these fields ignored, so v2 intentionally differs here.
func TestRealSpaceKeyTypesIntoNameAndPasswordFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(m *Model)
		got   func(m Model) string
	}{
		{"reset password", mountResetPassword,
			func(m Model) string { return resetPasswordOf(m).password }},
		{"token name", func(m *Model) { m.screen = screenAdminCreateToken },
			func(m Model) string { return m.adminView.TokenForm.Name }},
		{"robot name", func(m *Model) { m.screen = screenAdminCreateRobot },
			func(m Model) string { return m.adminView.CreateRobotForm.Name }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := charAdminModel(t, 160, 30)
			tt.setup(&m)
			m = testSendKey(t, m, testKeyRunes("ab"))
			m = testSendKey(t, m, testKeySpace())
			m = testSendKey(t, m, testKeyRunes("cd"))
			if got := tt.got(m); got != "ab cd" {
				t.Fatalf("value = %q, want %q", got, "ab cd")
			}
		})
	}
}
