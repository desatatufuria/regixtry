package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// A v1 paste stringified as "[text]", so pasted text never matched a key name.
// A single pasted rune such as "q" still acts as a key helper, as in v1, so
// "q" is only checked on the console list (which matches it by name).
var pastedKeyNames = []string{"enter", "esc", "tab", "shift+tab", "up", "down", "ctrl+c"}

func didQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, quit := cmd().(tea.QuitMsg)
	return quit
}

func TestPastedKeyNamesAreTextNotKeys(t *testing.T) {
	t.Parallel()

	fields := []struct {
		name   string
		screen screen
		setup  func(m *Model)
		got    func(m Model) string
	}{
		{"create user username", screenAdminCreateUser, func(m *Model) { mountCreateUser(m, createUserFieldUsername) }, func(m Model) string { return createUserOf(m).username }},
		{"create user password", screenAdminCreateUser, func(m *Model) { mountCreateUser(m, createUserFieldPassword) },
			func(m Model) string { return createUserOf(m).password }},
		{"reset password", screenAdminChangePassword, mountResetPassword, func(m Model) string { return resetPasswordOf(m).password }},
		{"token name", screenAdminEditUserTokens, mountTokensForTest, tokenNameOf},
		{"robot name", screenAdminCreateRobot, mountCreateRobotForTest, createRobotNameOf},
	}
	for _, f := range fields {
		for _, p := range pastedKeyNames {
			t.Run(f.name+"/"+p, func(t *testing.T) {
				t.Parallel()
				m := charAdminModel(t, 160, 30)
				m.screen = f.screen
				if f.setup != nil {
					f.setup(&m)
				}
				u, cmd := m.Update(tea.PasteMsg{Content: p})
				if didQuit(cmd) {
					t.Fatalf("paste %q quit the program", p)
				}
				got := u.(Model)
				if f.got(got) != p || got.screen != f.screen {
					t.Fatalf("paste %q: value=%q screen=%s, want typed text on %s", p, f.got(got), got.screen, f.screen)
				}
			})
		}
	}
}

func TestPastedKeyNamesDoNotActOnLists(t *testing.T) {
	t.Parallel()

	for _, p := range pastedKeyNames {
		t.Run("users list/"+p, func(t *testing.T) {
			t.Parallel()
			m := charAdminModel(t, 160, 30)
			m.screen = screenAdminUsers
			m.adminScreens[slotUsers] = usersAt(usersTestClient(), "")
			u, cmd := m.Update(tea.PasteMsg{Content: p})
			if didQuit(cmd) || u.(Model).screen != screenAdminUsers {
				t.Fatalf("paste %q acted as a key on the users list (screen=%s)", p, u.(Model).screen)
			}
		})
	}
	for _, p := range append([]string{"q"}, pastedKeyNames...) {
		t.Run("console list/"+p, func(t *testing.T) {
			t.Parallel()
			m := newRepositoriesReadyModel(t, repositoriesReadyFakeService())
			u, cmd := m.Update(tea.PasteMsg{Content: p})
			if didQuit(cmd) || u.(Model).screen != screenRepositories {
				t.Fatalf("paste %q acted as a key on the console list (screen=%s)", p, u.(Model).screen)
			}
		})
	}
}

func TestPastedKeyNameIsNotMatchedByScreenKeys(t *testing.T) {
	t.Parallel()

	paste := keyMsg{Text: "enter"}
	if trivyReposKeys.matches(paste, "enter") {
		t.Fatal("pasted \"enter\" matched the Enter binding")
	}
	if !trivyReposKeys.matches(testKeyEnter(), "enter") {
		t.Fatal("a real Enter no longer matches the Enter binding")
	}
}
