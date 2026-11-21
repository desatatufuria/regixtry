package tui

import (
	"strings"
	"testing"

	"regixtry/internal/ports"
)

func kjLoginModel(t *testing.T) Model {
	t.Helper()
	m := newAdminReadyModel(t, &fakeAdminClient{})
	m = charResize(t, m, 150, 40)
	m.screen = screenAdminLogin
	m.adminLogin = adminLoginForm{Focus: loginFieldUsername}
	return m
}

func TestAdminLoginTypesJAndKInUsernameAndPassword(t *testing.T) {
	m := typeRunes(t, kjLoginModel(t), "kjkj")
	if m.adminLogin.Username != "kjkj" || m.adminLogin.Focus != loginFieldUsername {
		t.Fatalf("username = %q focus = %v, want kjkj and Username", m.adminLogin.Username, m.adminLogin.Focus)
	}
	m = testSendKey(t, m, testKeyTab())
	m = typeRunes(t, m, "kjkj")
	if m.adminLogin.Password != "kjkj" || m.adminLogin.Focus != loginFieldPassword {
		t.Fatalf("password = %q focus = %v, want kjkj and Password", m.adminLogin.Password, m.adminLogin.Focus)
	}
	if m.adminLogin.Username != "kjkj" {
		t.Fatalf("username changed to %q", m.adminLogin.Username)
	}
	view := m.View().Content
	if strings.Contains(view, "kjkj") && strings.Count(view, "kjkj") != 1 {
		t.Fatalf("password rendered in clear:\n%s", view)
	}
	if !strings.Contains(view, "****") {
		t.Fatalf("password not masked:\n%s", view)
	}
}

func TestAdminLoginArrowsTabAndShiftTabStillToggleFocus(t *testing.T) {
	for name, key := range map[string]keyMsg{"tab": testKeyTab(), "down": testKeyDown(), "up": testKeyUp()} {
		m := testSendKey(t, kjLoginModel(t), key)
		if m.adminLogin.Focus != loginFieldPassword {
			t.Fatalf("%s: focus = %v, want Password", name, m.adminLogin.Focus)
		}
		m = testSendKey(t, m, key)
		if m.adminLogin.Focus != loginFieldUsername {
			t.Fatalf("%s twice: focus = %v, want Username", name, m.adminLogin.Focus)
		}
	}
}

// Shift+Tab does not toggle the login focus today (isTabKey rejects the Shift
// modifier); the fix keeps that.
func TestAdminLoginShiftTabBehavesAsBefore(t *testing.T) {
	m := testSendKey(t, kjLoginModel(t), testKeyShiftTab())
	if m.adminLogin.Focus != loginFieldUsername {
		t.Fatalf("shift+tab: focus = %v, want unchanged Username", m.adminLogin.Focus)
	}
}

func kjSearchModel(t *testing.T, active bool) Model {
	t.Helper()
	users := []ports.AdminUser{{ID: "u1", Username: "kj-one"}, {ID: "u2", Username: "kj-two"}, {ID: "u3", Username: "zed"}}
	m := newAdminReadyModel(t, &fakeAdminClient{users: users})
	m.screen = screenAdminUsers
	m.adminAuth = adminAuthStateAuthenticated
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token"}
	m.adminView.Users = users
	m.adminView.SelectedUser = 0
	m.adminView.SelectedUserID = "u1"
	m.adminView.SelectedUsername = "kj-one"
	m.adminView.UserSearchActive = active
	return m
}

func TestAdminUserSearchTypesJAndKWithoutMovingSelection(t *testing.T) {
	m := typeRunes(t, kjSearchModel(t, true), "kube/jobs")
	if got := m.adminView.UserSearchQuery; got != "kube/jobs" {
		t.Fatalf("query = %q, want kube/jobs", got)
	}
	if !m.adminView.UserSearchActive {
		t.Fatal("search should stay active")
	}
	m = kjSearchModel(t, true)
	m = typeRunes(t, m, "kj")
	if m.adminView.SelectedUser != 0 || m.adminView.SelectedUserID != "u1" {
		t.Fatalf("selection moved: index %d id %q", m.adminView.SelectedUser, m.adminView.SelectedUserID)
	}
}

func TestAdminUserSearchArrowsStillMoveSelection(t *testing.T) {
	m := testSendKey(t, kjSearchModel(t, true), testKeyDown())
	if m.adminView.SelectedUser != 1 || m.adminView.SelectedUserID != "u2" {
		t.Fatalf("after Down: index %d id %q, want 1 u2", m.adminView.SelectedUser, m.adminView.SelectedUserID)
	}
	m = testSendKey(t, m, testKeyUp())
	if m.adminView.SelectedUser != 0 || m.adminView.SelectedUserID != "u1" {
		t.Fatalf("after Up: index %d id %q, want 0 u1", m.adminView.SelectedUser, m.adminView.SelectedUserID)
	}
}

func TestAdminUsersJAndKStillNavigateWhenSearchInactive(t *testing.T) {
	m := testSendKey(t, kjSearchModel(t, false), testKeyRunes("j"))
	if m.adminView.SelectedUserID != "u2" || m.adminView.UserSearchQuery != "" {
		t.Fatalf("after j: id %q query %q, want u2 and empty", m.adminView.SelectedUserID, m.adminView.UserSearchQuery)
	}
	m = testSendKey(t, m, testKeyRunes("k"))
	if m.adminView.SelectedUserID != "u1" || m.adminView.UserSearchQuery != "" {
		t.Fatalf("after k: id %q query %q, want u1 and empty", m.adminView.SelectedUserID, m.adminView.UserSearchQuery)
	}
}

func kjGrantFormModel(t *testing.T) Model {
	t.Helper()
	m := newAdminReadyModelWithCatalog(t, []string{"kube/jobs", "kube/jobs-extra", "kube/kafka"}, &fakeAdminClient{})
	m.screen = screenAdminAddGrant
	m.adminAuth = adminAuthStateAuthenticated
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token"}
	m.adminView.SelectedUserID = "u1"
	m.adminView.SelectedUsername = "alice"
	m.adminView.GrantForm.Focus = adminGrantFieldRepository
	return m
}

func TestGrantFormRepositoryTypesJAndK(t *testing.T) {
	m := typeRunes(t, kjGrantFormModel(t), "kube/jobs")
	if got := m.adminView.GrantForm.Repository; got != "kube/jobs" {
		t.Fatalf("Repository = %q, want kube/jobs", got)
	}
}

func TestGrantFormRepositoryArrowsMoveSuggestionAndEnterTabAsBefore(t *testing.T) {
	m := typeRunes(t, kjGrantFormModel(t), "kube/jobs")
	m = testSendKey(t, m, testKeyDown())
	if got := m.adminView.GrantForm.RepositorySuggestion; got != 1 {
		t.Fatalf("after Down suggestion = %d, want 1", got)
	}
	m = testSendKey(t, m, testKeyUp())
	if got := m.adminView.GrantForm.RepositorySuggestion; got != 0 {
		t.Fatalf("after Up suggestion = %d, want 0", got)
	}
	m = testSendKey(t, m, testKeyDown())
	m = testSendKey(t, m, testKeyEnter())
	if got := m.adminView.GrantForm.Repository; got != "kube/jobs-extra" {
		t.Fatalf("Enter accepted %q, want kube/jobs-extra", got)
	}
	if m.adminView.GrantForm.Focus != adminGrantFieldRole {
		t.Fatalf("focus after Enter = %v, want Role", m.adminView.GrantForm.Focus)
	}

	m = typeRunes(t, kjGrantFormModel(t), "kube/jobs")
	m = testSendKey(t, m, testKeyTab())
	if m.adminView.GrantForm.Focus != adminGrantFieldRole || m.adminView.GrantForm.Repository != "kube/jobs" {
		t.Fatalf("Tab: focus %v repository %q", m.adminView.GrantForm.Focus, m.adminView.GrantForm.Repository)
	}
}
