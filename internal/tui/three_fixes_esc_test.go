package tui

import "testing"

// --- Fix 1: Esc ends the user search ---

func pressEsc(t *testing.T, m Model) Model {
	t.Helper()
	updated, cmd := m.Update(testKeyEsc())
	m = updated.(Model)
	if cmd != nil {
		if msg, ok := cmd().(navigateMsg); ok {
			updated, _ = m.Update(msg)
			m = updated.(Model)
		}
	}
	return m
}

func TestEscEndsTheUserSearchAndStaysOnUsers(t *testing.T) {
	t.Parallel()
	m := kjSearchModel(t, false)
	m = testSendKey(t, m, testKeyRunes("/"))
	m = typeRunes(t, m, "kube")
	if !m.adminView.UserSearchActive || m.adminView.UserSearchQuery != "kube" {
		t.Fatalf("setup: active = %v query = %q", m.adminView.UserSearchActive, m.adminView.UserSearchQuery)
	}

	m = pressEsc(t, m)
	if m.screen != screenAdminUsers {
		t.Fatalf("screen after Esc = %v, want Users", m.screen)
	}
	if m.adminView.UserSearchActive {
		t.Fatal("the search is still active after Esc")
	}

	_, cmd := sendKeyCmd(m, testKeyRunes("q"))
	if !didQuit(cmd) {
		t.Fatal("q after the search ended did not quit (it was typed into a stale search)")
	}

	m = pressEsc(t, m)
	if m.screen != screenAdminOverview {
		t.Fatalf("second Esc screen = %v, want the Overview", m.screen)
	}
}

func TestEscWithoutSearchStillGoesBack(t *testing.T) {
	t.Parallel()
	m := pressEsc(t, kjSearchModel(t, false))
	if m.screen != screenAdminOverview {
		t.Fatalf("screen = %v, want the Overview", m.screen)
	}
}

func TestActiveSearchFooterSaysEscEndsTheSearch(t *testing.T) {
	t.Parallel()
	if got := adminScreenHelp(screenAdminUsers, kjSearchModel(t, true).adminView); got != "Type to search | Up/Down: pick user | Esc: end search" {
		t.Fatalf("footer = %q", got)
	}
}
