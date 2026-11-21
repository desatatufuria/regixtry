package tui

import (
	"testing"
	"time"

	"regixtry/internal/ports"
)

// TestEveryBaselineFunctionIsReachableFromTheOverview is the reachability
// table of the redesign: each function the admin TUI offered before the
// Overview and the sidebar still has a path from the post-login Overview,
// by the sidebar or by a key, to the screen that serves it.
func TestEveryBaselineFunctionIsReachableFromTheOverview(t *testing.T) {
	t.Parallel()

	key := func(k string) func(*testing.T, Model) Model {
		return func(t *testing.T, m Model) Model { return runKey(t, m, k) }
	}
	sidebar := func(id shellNavID) func(*testing.T, Model) Model {
		return func(t *testing.T, m Model) Model { return openSidebarItem(t, m, id) }
	}
	then := func(steps ...func(*testing.T, Model) Model) func(*testing.T, Model) Model {
		return func(t *testing.T, m Model) Model {
			for _, step := range steps {
				m = step(t, m)
			}
			return m
		}
	}

	cases := []struct {
		name string
		path func(*testing.T, Model) Model
		want screen
	}{
		{"users", sidebar(shellNavUsers), screenAdminUsers},
		{"robots", sidebar(shellNavRobots), screenAdminRobots},
		{"grant repository access", sidebar(shellNavRepositoryAccess), screenAdminGrantAccess},
		{"create user", then(sidebar(shellNavUsers), key("n")), screenAdminCreateUser},
		{"edit user", then(sidebar(shellNavUsers), key("enter")), screenAdminEditUser},
		{"user tokens", then(sidebar(shellNavUsers), key("t")), screenAdminEditUserTokens},
		{"create robot", key("r"), screenAdminCreateRobot},
		{"image scanning", sidebar(shellNavImageScanning), screenSecurityTrivy},
		{"image scanning repositories", then(sidebar(shellNavImageScanning), key("tab")), screenSecurityTrivyRepos},
		{"secret scanning", sidebar(shellNavSecretScanning), screenSecurityGitleaksConfig},
		{"secret scanning repositories", then(sidebar(shellNavSecretScanning), key("o")), screenSecurityGitleaksRepos},
		{"signing policy", sidebar(shellNavSigningPolicy), screenSecuritySigningConfig},
		{"signing policy repositories", then(sidebar(shellNavSigningPolicy), key("o")), screenSecuritySigningRepos},
		{"scan runs", sidebar(shellNavScanRuns), screenAdminScanRuns},
		{"secret findings", sidebar(shellNavSecretFindings), screenAdminSecretFindings},
		{"update channel", sidebar(shellNavUpdateChannel), screenAdminUpdateChannel},
		{"console browse", key("esc"), screenRepositories},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := overviewClient()
			client.loginSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Now().Add(time.Hour)}
			client.featurePage = ports.FeaturePage{Summary: ports.FeatureSummary{Name: "feature", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}}
			m := runAdminLogin(t, newAdminReadyModel(t, client), "operator", "secret-pass")
			if m.screen != screenAdminOverview {
				t.Fatalf("login landed on %q, want the Overview", m.screen)
			}
			m = tc.path(t, m)
			if m.screen != tc.want {
				t.Fatalf("reached %q, want %q", m.screen, tc.want)
			}
		})
	}
}

// TestEscFromRepositoryScreensEndsOnTheOverview pins the back path of the
// repository screens reached from a feature screen: Tab on Image scanning,
// 'o' on Secret scanning and Signing policy.
func TestEscFromRepositoryScreensEndsOnTheOverview(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		item  shellNavID
		key   string
		repos screen
	}{
		{"image scanning", shellNavImageScanning, "tab", screenSecurityTrivyRepos},
		{"secret scanning", shellNavSecretScanning, "o", screenSecurityGitleaksRepos},
		{"signing policy", shellNavSigningPolicy, "o", screenSecuritySigningRepos},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := overviewClient()
			client.loginSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Now().Add(time.Hour)}
			m := runAdminLogin(t, newAdminReadyModel(t, client), "operator", "secret-pass")
			m = openSidebarItem(t, m, tc.item)
			m = runKey(t, m, tc.key)
			if m.screen != tc.repos {
				t.Fatalf("setup: reached %q, want %q", m.screen, tc.repos)
			}
			m = runKey(t, m, "esc")
			if m.screen != screenAdminOverview {
				t.Fatalf("first Esc from the repositories screen reached %q, want the Overview", m.screen)
			}
		})
	}
}

func TestEscFromCreateRobotGoesThroughRobotsToTheOverview(t *testing.T) {
	t.Parallel()
	client := overviewClient()
	client.loginSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Now().Add(time.Hour)}
	m := runAdminLogin(t, newAdminReadyModel(t, client), "operator", "secret-pass")
	m = runKey(t, m, "r")
	m = runKey(t, m, "esc")
	if m.screen != screenAdminRobots {
		t.Fatalf("first Esc from Create robot reached %q, want the Robots list", m.screen)
	}
	m = runKey(t, m, "esc")
	if m.screen != screenAdminOverview {
		t.Fatalf("second Esc reached %q, want the Overview", m.screen)
	}
}
