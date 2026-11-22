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
		{"create token name", mountTokensForTest, tokenNameOf, pasteSample},
		{"create robot name", mountCreateRobotForTest, createRobotNameOf, pasteSample},
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
		{"token name", mountTokensForTest, tokenNameOf},
		{"robot name", mountCreateRobotForTest, createRobotNameOf},
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
