package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
	if m.adminView.UserSearchQuery != "quq" || !m.adminView.UserSearchActive {
		t.Fatalf("query = %q active = %v, want quq and active", m.adminView.UserSearchQuery, m.adminView.UserSearchActive)
	}
}

func TestQStillQuitsOutsideTextFields(t *testing.T) {
	screens := map[string]screen{
		"repositories": screenRepositories,
		"tags":         screenTags,
		"users":        screenAdminUsers,
		"features":     screenAdminFeatures,
		"edit user":    screenAdminEditUser,
		"grants":       screenAdminEditUserGrants,
		"tokens":       screenAdminEditUserTokens,
		"repo grants":  screenRepoAdminGrants,
		"robots":       screenAdminRobots,
		"menu":         screenAdminMenu,
		"operations":   screenAdminOperations,
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

func TestOldGrantFormStillTypesQ(t *testing.T) {
	m := kjSearchModel(t, false)
	m.screen = screenAdminAddGrant
	_, cmd := sendKeyCmd(m, testKeyRunes("q"))
	if didQuit(cmd) {
		t.Fatal("q on the old grant form quit")
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

func TestLoginAndActiveSearchFootersSayEscBack(t *testing.T) {
	login := kjLoginModel(t).View().Content
	if strings.Contains(login, "q: quit") || !strings.Contains(login, "Esc: back") {
		t.Fatalf("login footer wrong:\n%s", login)
	}
	m := charResize(t, kjSearchModel(t, true), 200, 40)
	search := m.View().Content
	if strings.Contains(search, "q: quit") || !strings.Contains(search, "Esc: back") {
		t.Fatalf("active search footer wrong:\n%s", search)
	}
	m = charResize(t, kjSearchModel(t, false), 200, 40)
	if !strings.Contains(m.View().Content, "q: quit") {
		t.Fatal("inactive search footer must still advertise q: quit")
	}
}

func createUserQModel(t *testing.T, focus adminCreateUserField) Model {
	t.Helper()
	m := kjSearchModel(t, false)
	m.screen = screenAdminCreateUser
	m.adminView.CreateUserForm = adminCreateUserForm{Focus: focus}
	return m
}

func TestCreateUserTypesQInTextFieldsOnly(t *testing.T) {
	m := createUserQModel(t, adminCreateUserFieldUsername)
	m, cmd := sendKeyCmd(m, testKeyRunes("q"))
	if didQuit(cmd) {
		t.Fatal("q in Create User username quit")
	}
	m = typeRunes(t, m, "Q")
	if m.adminView.CreateUserForm.Username != "qQ" || m.adminView.CreateUserForm.Focus != adminCreateUserFieldUsername {
		t.Fatalf("username = %q focus = %v", m.adminView.CreateUserForm.Username, m.adminView.CreateUserForm.Focus)
	}
	m = testSendKey(t, m, testKeyTab())
	m, cmd = sendKeyCmd(m, testKeyRunes("q"))
	if didQuit(cmd) {
		t.Fatal("q in Create User password quit")
	}
	if m.adminView.CreateUserForm.Password != "q" || m.adminView.CreateUserForm.Username != "qQ" {
		t.Fatalf("password = %q username = %q", m.adminView.CreateUserForm.Password, m.adminView.CreateUserForm.Username)
	}
	for _, focus := range []adminCreateUserField{adminCreateUserFieldIsAdmin, adminCreateUserFieldIsReadOnly, adminCreateUserFieldEnabled} {
		_, cmd = sendKeyCmd(createUserQModel(t, focus), testKeyRunes("q"))
		if !didQuit(cmd) {
			t.Fatalf("q on Create User toggle %v did not quit", focus)
		}
	}
	for _, focus := range []adminCreateUserField{adminCreateUserFieldUsername, adminCreateUserFieldPassword} {
		_, cmd = sendKeyCmd(createUserQModel(t, focus), testKeyCtrl('c'))
		if !didQuit(cmd) {
			t.Fatalf("ctrl+c in Create User field %v did not quit", focus)
		}
	}
}
