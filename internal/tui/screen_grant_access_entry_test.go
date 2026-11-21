package tui

import (
	"strings"
	"testing"
)

func TestGrantAccessIsReachableFromTheOverviewCard(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	m := grantModel(t, client, 150, 24)
	updated, cmd := m.Update(navigateMsg{To: screenAdminOverview})
	m = runCmd(t, updated.(Model), cmd)
	m = grantModelPress(t, m, testKeyRunes("g"))
	if m.screen != screenAdminGrantAccess {
		t.Fatalf("screen = %q, want %q", m.screen, screenAdminGrantAccess)
	}
	s, ok := m.adminScreens[slotGrantAccess].(grantAccessScreen)
	if !ok {
		t.Fatalf("slot holds %T", m.adminScreens[slotGrantAccess])
	}
	if len(s.principals) != 5 {
		t.Fatalf("principals = %d, want loaded users and robots", len(s.principals))
	}
	m = grantModelPress(t, m, testKeyEsc())
	if m.screen != screenAdminOverview {
		t.Fatalf("Esc went to %q, want the Overview", m.screen)
	}
}

func TestGKeyOnUsersListOpensGrantScreenWithThatUserPreselected(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	m := grantModel(t, client, 150, 24)
	m.screen = screenAdminUsers
	m.adminScreens[slotUsers] = usersAt(client, "u-2")

	m = grantModelPress(t, m, testKeyRunes("g"))
	if m.screen != screenAdminGrantAccess {
		t.Fatalf("screen = %q after g on Users", m.screen)
	}
	s := m.adminScreens[slotGrantAccess].(grantAccessScreen)
	if p, ok := s.highlighted(); !ok || p.ID != "u-2" {
		t.Fatalf("highlighted = %#v, want bob", p)
	}
	m = grantModelPress(t, m, testKeyEsc())
	if m.screen != screenAdminUsers {
		t.Fatalf("Esc went to %q, want the Users list", m.screen)
	}
}

func TestGKeyOnUsersListWithoutSelectionSaysSo(t *testing.T) {
	t.Parallel()
	m := grantModel(t, newRecordingGrantClient(), 150, 24)
	m.screen = screenAdminUsers
	m.adminScreens[slotUsers] = usersAt(&fakeAdminClient{}, "")
	m = grantModelPress(t, m, testKeyRunes("g"))
	if notice := usersScreenOf(t, m).notice; m.screen != screenAdminUsers || !strings.Contains(notice, "Select a user") {
		t.Fatalf("screen=%q notice=%q", m.screen, notice)
	}
}

func TestGKeyIsTextWhileSearchingUsers(t *testing.T) {
	t.Parallel()
	m := grantModel(t, newRecordingGrantClient(), 150, 24)
	m.screen = screenAdminUsers
	searching := usersAt(newRecordingGrantClient(), "")
	searching.searching = true
	m.adminScreens[slotUsers] = searching
	m = grantModelPress(t, m, testKeyRunes("g"))
	if m.screen != screenAdminUsers {
		t.Fatalf("g left the Users list while searching: %q", m.screen)
	}
	if !strings.Contains(usersScreenOf(t, m).query, "g") {
		t.Fatalf("g was not typed into the search: %q", usersScreenOf(t, m).query)
	}
}

func TestGKeyOnRobotsListOpensGrantScreenWithThatRobotPreselected(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	m := grantModel(t, client, 150, 24)
	m.screen = screenAdminRobots
	m.adminView.Robots = client.robots
	m.adminView.SelectedRobot = 1

	m = grantModelPress(t, m, testKeyRunes("g"))
	if m.screen != screenAdminGrantAccess {
		t.Fatalf("screen = %q after g on Robots", m.screen)
	}
	s := m.adminScreens[slotGrantAccess].(grantAccessScreen)
	if p, ok := s.highlighted(); !ok || p.ID != "r-2" || p.Kind != "robot" {
		t.Fatalf("highlighted = %#v, want deploy-bot", p)
	}
	m = grantModelPress(t, m, testKeyEsc())
	if m.screen != screenAdminRobots {
		t.Fatalf("Esc went to %q, want the Robots list", m.screen)
	}
}

func TestGKeyOnRobotsListWithoutRobotsSaysSo(t *testing.T) {
	t.Parallel()
	m := grantModel(t, newRecordingGrantClient(), 150, 24)
	m.screen = screenAdminRobots
	m.adminView.Robots = nil
	m = grantModelPress(t, m, testKeyRunes("g"))
	if m.screen != screenAdminRobots || !strings.Contains(m.status, "No robot selected") {
		t.Fatalf("screen=%q status=%q", m.screen, m.status)
	}
}

func TestRobotsFooterAdvertisesTheGrantKey(t *testing.T) {
	t.Parallel()
	if help := adminScreenHelp(screenAdminRobots, AdminViewState{}); !strings.Contains(help, "g: grant access") {
		t.Fatalf("robots footer = %q, want it to advertise g", help)
	}
}
