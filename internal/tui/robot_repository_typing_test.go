package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// robotRepositoryFocusedModel returns the Create Robot form with the
// Repository field focused (Name filled) and a catalog whose names contain
// the letters j and k.
func robotRepositoryFocusedModel(t *testing.T) Model {
	t.Helper()
	m := newAdminReadyModelWithCatalog(t, []string{"kube/jobs", "kube/jobs-extra", "kube/kafka"}, newTTLClient())
	m = charResize(t, m, 100, 40)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token"}
	m.adminAuth = adminAuthStateAuthenticated
	m.screen = screenAdminCreateRobot
	m = testSendKeys(t, m, testKeyRunes("ci-bot"), testKeyTab())
	if m.adminView.CreateRobotForm.Focus != adminCreateRobotFieldRepository {
		t.Fatalf("focus = %v, want Repository", m.adminView.CreateRobotForm.Focus)
	}
	return m
}

func typeRunes(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m = testSendKey(t, m, testKeyRunes(string(r)))
	}
	return m
}

func TestRobotRepositoryFieldTypesJAndK(t *testing.T) {
	m := typeRunes(t, robotRepositoryFocusedModel(t), "kube/jobs")
	if got := m.adminView.CreateRobotForm.Repository; got != "kube/jobs" {
		t.Fatalf("Repository = %q, want %q", got, "kube/jobs")
	}
}

func TestRobotRepositoryFieldArrowsMoveSuggestionButJKDoNot(t *testing.T) {
	m := typeRunes(t, robotRepositoryFocusedModel(t), "kube/jobs")
	if got := m.adminView.CreateRobotForm.RepositorySuggestion; got != 0 {
		t.Fatalf("suggestion = %d, want 0", got)
	}
	m = testSendKey(t, m, testKeyDown())
	if got := m.adminView.CreateRobotForm.RepositorySuggestion; got != 1 {
		t.Fatalf("after Down suggestion = %d, want 1", got)
	}
	m = testSendKey(t, m, testKeyUp())
	if got := m.adminView.CreateRobotForm.RepositorySuggestion; got != 0 {
		t.Fatalf("after Up suggestion = %d, want 0", got)
	}
	m = testSendKey(t, m, testKeyDown())
	for _, letter := range []string{"j", "k"} {
		before := m.adminView.CreateRobotForm.RepositorySuggestion
		m = testSendKey(t, m, testKeyRunes(letter))
		if got := m.adminView.CreateRobotForm.Repository; got != "kube/jobs"+letter {
			t.Fatalf("after %q Repository = %q", letter, got)
		}
		// Typing resets the suggestion exactly as any other letter does.
		if got := m.adminView.CreateRobotForm.RepositorySuggestion; got != 0 {
			t.Fatalf("after %q suggestion = %d (was %d), want 0 as for any typed letter", letter, got, before)
		}
		m = testSendKey(t, m, testKeyBackspace())
		m = testSendKey(t, m, testKeyDown())
	}
}

func TestRobotRepositoryFieldEnterAndTabAsBefore(t *testing.T) {
	m := typeRunes(t, robotRepositoryFocusedModel(t), "kube/jobs")
	m = testSendKey(t, m, testKeyDown())
	m = testSendKey(t, m, testKeyEnter())
	if got := m.adminView.CreateRobotForm.Repository; got != "kube/jobs-extra" {
		t.Fatalf("Enter accepted %q, want kube/jobs-extra", got)
	}
	if m.adminView.CreateRobotForm.Focus != adminCreateRobotFieldRole {
		t.Fatalf("focus after Enter = %v, want Role", m.adminView.CreateRobotForm.Focus)
	}

	m = typeRunes(t, robotRepositoryFocusedModel(t), "kube/jobs")
	m = testSendKey(t, m, testKeyTab())
	if got := m.adminView.CreateRobotForm.Repository; got != "kube/jobs" {
		t.Fatalf("Tab changed Repository to %q", got)
	}
	if m.adminView.CreateRobotForm.Focus == adminCreateRobotFieldRepository {
		t.Fatal("Tab did not leave the Repository field")
	}
}

func TestRobotRepositoryFieldBackspaceDeletesOneCharacter(t *testing.T) {
	m := typeRunes(t, robotRepositoryFocusedModel(t), "kube/jobs")
	m = testSendKey(t, m, testKeyBackspace())
	if got := m.adminView.CreateRobotForm.Repository; got != "kube/job" {
		t.Fatalf("Repository = %q, want kube/job", got)
	}
}

func TestRobotRepositoryFieldPasteStaysText(t *testing.T) {
	m := robotRepositoryFocusedModel(t)
	updated, _ := m.Update(tea.PasteMsg{Content: "kube/jobs"})
	m = updated.(Model)
	if got := m.adminView.CreateRobotForm.Repository; got != "kube/jobs" {
		t.Fatalf("Repository after paste = %q, want kube/jobs", got)
	}
}
