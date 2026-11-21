package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func sendKeyCmd(m Model, msg keyMsg) (Model, tea.Cmd) {
	updated, cmd := m.Update(msg)
	return updated.(Model), cmd
}

func TestAdminLoginTypesQInUsernameAndPassword(t *testing.T) {
	m := kjLoginModel(t)
	for _, text := range []string{"q", "Q"} {
		var cmd tea.Cmd
		m, cmd = sendKeyCmd(m, testKeyRunes(text))
		if didQuit(cmd) {
			t.Fatalf("%q on login quit the program", text)
		}
	}
	if m.adminLogin.Username != "qQ" || m.adminLogin.Focus != loginFieldUsername {
		t.Fatalf("username = %q focus = %v, want qQ and Username", m.adminLogin.Username, m.adminLogin.Focus)
	}
	m = testSendKey(t, m, testKeyTab())
	m = typeRunes(t, m, "pqQ")
	if m.adminLogin.Password != "pqQ" || m.adminLogin.Focus != loginFieldPassword || m.adminLogin.Username != "qQ" {
		t.Fatalf("password = %q username = %q focus = %v", m.adminLogin.Password, m.adminLogin.Username, m.adminLogin.Focus)
	}
}

func TestAdminUserSearchTypesQ(t *testing.T) {
	m := kjSearchModel(t, true)
	m, cmd := sendKeyCmd(m, testKeyRunes("q"))
	if didQuit(cmd) {
		t.Fatal("q in active search quit the program")
	}
	m = typeRunes(t, m, "uq")
	if got := usersScreenOf(t, m); got.query != "quq" || !got.searching {
		t.Fatalf("query = %q searching = %v, want quq and searching", got.query, got.searching)
	}
}

func TestQStillQuitsOutsideTextFields(t *testing.T) {
	screens := map[string]screen{
		"repositories": screenRepositories,
		"tags":         screenTags,
		"users":        screenAdminUsers,
		"edit user":    screenAdminEditUser,
		"tokens":       screenAdminEditUserTokens,
		"repo grants":  screenRepoAdminGrants,
		"robots":       screenAdminRobots,
		"overview":     screenAdminOverview,
	}
	for name, sc := range screens {
		t.Run(name, func(t *testing.T) {
			m := kjSearchModel(t, false)
			m.screen = sc
			_, cmd := sendKeyCmd(m, testKeyRunes("q"))
			if !didQuit(cmd) {
				t.Fatalf("q on %s did not quit", name)
			}
		})
	}
}

func TestCtrlCQuitsOnLoginAndActiveSearch(t *testing.T) {
	_, cmd := sendKeyCmd(kjLoginModel(t), testKeyCtrl('c'))
	if !didQuit(cmd) {
		t.Fatal("ctrl+c on login did not quit")
	}
	_, cmd = sendKeyCmd(kjSearchModel(t, true), testKeyCtrl('c'))
	if !didQuit(cmd) {
		t.Fatal("ctrl+c in active search did not quit")
	}
}

func TestLoginSaysEscBackAndActiveSearchSaysEscEndsIt(t *testing.T) {
	login := kjLoginModel(t).View().Content
	if strings.Contains(login, "q: quit") || !strings.Contains(ansi.Strip(login), "esc back") {
		t.Fatalf("login footer wrong:\n%s", login)
	}
	m := charResize(t, kjSearchModel(t, true), 200, 40)
	search := ansi.Strip(m.View().Content)
	if strings.Contains(search, "q quit") || !strings.Contains(search, "esc done") {
		t.Fatalf("active search footer wrong:\n%s", search)
	}
}
