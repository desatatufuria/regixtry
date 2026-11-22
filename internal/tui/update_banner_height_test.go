package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// The update banner takes its row from the screen: with a version set, every
// shell screen still renders exactly the terminal's height and never overflows.
func TestUpdateBannerNeverMakesAShellScreenTallerThanTheTerminal(t *testing.T) {
	t.Parallel()
	ids := []screen{
		screenAdminOverview, screenAdminUsers, screenAdminRobots, screenAdminRepositoryAccess,
		screenAdminUpdateChannel, screenAdminScanRuns, screenAdminSecretFindings,
		screenSecurityTrivy, screenSecurityTrivyRepos, screenSecurityGitleaksConfig, screenSecurityGitleaksRepos,
		screenSecuritySigningConfig, screenSecuritySigningRepos, screenAdminGrantAccess, screenRepoAdminGrants,
		screenRepositories, screenProjects, screenTags,
	}
	for _, id := range ids {
		for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
			for _, version := range []string{"", "v1.2.3"} {
				m := NewModel(newConsoleService(consoleRepos("team/api")...), WithAdminClient(&fakeAdminClient{}))
				m = charResize(t, m, size.w, size.h)
				m.currentVersion = version
				m.adminSession = AdminSession{Username: "operator", BearerToken: "t", ExpiresAt: consoleTime.Add(24 * 3600e9)}
				m.now = func() time.Time { return consoleTime }
				slot, ok := slotFor(id)
				if !ok {
					t.Fatalf("no slot for %s", id)
				}
				switch id {
				case screenAdminUsers:
					m.adminScreens[slot] = newUsersScreen("")
				case screenRepoAdminGrants:
					m.adminScreens[slot] = newDelegateScreen("team/api")
				case screenRepositories:
					m.adminScreens[slot] = newConsoleRepositoriesScreen(consoleRepos("team/api"))
				case screenProjects:
					m.adminScreens[slot] = newConsoleProjectsScreen(consoleRepos("team/api"))
				case screenTags:
					m.adminScreens[slot] = newConsoleTagsScreen("team/api", sortByNameAsc)
				default:
					m.adminScreens[slot] = newAdminScreenFor(id)
				}
				if m.adminScreens[slot] == nil {
					t.Fatalf("%s: no screen built", id)
				}
				m.screen = id
				lines := strings.Split(ansi.Strip(m.View().Content), "\n")
				// A screen still on the old workspace draws what it needs; a shell
				// screen fills the terminal exactly, with or without the banner.
				shell := id != screenSecurityTrivyRepos && id != screenSecurityGitleaksRepos && id != screenSecuritySigningRepos
				if len(lines) > size.h || (shell && len(lines) != size.h) {
					t.Fatalf("%s at %dx%d with version %q: %d rows, want %d", id, size.w, size.h, version, len(lines), size.h)
				}
				for i, line := range lines {
					if ansi.StringWidth(line) > size.w {
						t.Fatalf("%s at %dx%d: row %d overflows", id, size.w, size.h, i)
					}
				}
				if version != "" && shell && !strings.Contains(lines[len(lines)-1], version) {
					t.Fatalf("%s at %dx%d: the version line is not the last row", id, size.w, size.h)
				}
			}
		}
	}
}
