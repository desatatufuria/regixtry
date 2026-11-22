package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// footerRow is the last rendered row: the shell's hint line.
func footerRow(m Model) string {
	rows := strings.Split(ansi.Strip(m.View().Content), "\n")
	return rows[len(rows)-1]
}

func quitsOnQ(m Model) bool {
	_, cmd := m.Update(testKeyRunes("q"))
	return didQuit(cmd)
}

// qScreens are the shell screens opened through navigation; the footer shows
// "q quit" exactly when q quits.
var qScreens = []struct {
	name string
	id   screen
	want bool
}{
	{"overview", screenAdminOverview, true},
	{"users", screenAdminUsers, true},
	{"robots", screenAdminRobots, true},
	{"scan runs", screenAdminScanRuns, true},
	{"secret findings", screenAdminSecretFindings, true},
	{"image scanning", screenSecurityTrivy, true},
	{"image scanning repos", screenSecurityTrivyRepos, true},
	{"secret scanning", screenSecurityGitleaksConfig, true},
	{"secret scanning repos", screenSecurityGitleaksRepos, true},
	{"signing", screenSecuritySigningConfig, true},
	{"signing repos", screenSecuritySigningRepos, true},
	{"update channel", screenAdminUpdateChannel, true},
	{"repository access", screenAdminRepositoryAccess, true},
	{"grant access", screenAdminGrantAccess, false},
}

func TestQQuitsExactlyWhereTheFooterSaysSo(t *testing.T) {
	t.Parallel()
	for _, size := range [][2]int{{150, 24}, {200, 40}} {
		for _, tc := range qScreens {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				m := securityModelAt(t, securityClient(), tc.id, size[0], size[1])
				if m.screen != tc.id {
					t.Fatalf("opened %q, want %q", m.screen, tc.id)
				}
				if got := quitsOnQ(m); got != tc.want {
					t.Errorf("q quits = %v, want %v", got, tc.want)
				}
				if got := strings.Contains(footerRow(m), "q quit"); got != tc.want {
					t.Errorf("footer shows q quit = %v, want %v: %q", got, tc.want, footerRow(m))
				}
				if !strings.Contains(footerRow(m), "F2") {
					t.Errorf("the F2 hint was dropped: %q", footerRow(m))
				}
			})
		}
	}
}

func TestQHeldBackWhileSomethingHoldsTheKeys(t *testing.T) {
	t.Parallel()
	for _, size := range [][2]int{{150, 24}, {200, 40}} {
		t.Run("users search", func(t *testing.T) {
			t.Parallel()
			m := modelPress(t, usersModelAt(t, usersTestClient(), size[0], size[1]), testKeyRunes("/"))
			assertQHeldBack(t, m)
			m = modelPress(t, m, testKeyRunes("q"))
			if got := usersScreenOf(t, m).query; got != "q" {
				t.Errorf("typed q: query = %q, want q", got)
			}
		})
		t.Run("users confirm", func(t *testing.T) {
			t.Parallel()
			assertQHeldBack(t, modelPress(t, usersModelAt(t, usersTestClient(), size[0], size[1]), testKeyRunes("d")))
		})
		t.Run("image scanning modal", func(t *testing.T) {
			t.Parallel()
			m := securityModelAt(t, securityClient(), screenSecurityTrivy, size[0], size[1])
			assertQHeldBack(t, securityPress(t, m, testKeyRunes("c")))
		})
		t.Run("secret scanning modal", func(t *testing.T) {
			t.Parallel()
			m := securityModelAt(t, securityClient(), screenSecurityGitleaksConfig, size[0], size[1])
			assertQHeldBack(t, securityPress(t, m, testKeyRunes("c")))
		})
		t.Run("repository access filter", func(t *testing.T) {
			t.Parallel()
			m := securityModelAt(t, securityClient(), screenAdminRepositoryAccess, size[0], size[1])
			assertQHeldBack(t, securityPress(t, m, testKeyRunes("/")))
		})
		t.Run("grant access types q", func(t *testing.T) {
			t.Parallel()
			m := grantModel(t, newRecordingGrantClient(), size[0], size[1])
			updated, cmd := m.Update(openGrantAccessMsg{returnTo: screenAdminOverview})
			assertQHeldBack(t, runCmd(t, updated.(Model), cmd))
		})
	}
}

func assertQHeldBack(t *testing.T, m Model) {
	t.Helper()
	if quitsOnQ(m) {
		t.Error("q quit while something holds the keys")
	}
	if strings.Contains(footerRow(m), "q quit") {
		t.Errorf("footer promises q quit while it is held back: %q", footerRow(m))
	}
	if _, cmd := m.Update(testKeyCtrl('c')); !didQuit(cmd) {
		t.Error("ctrl+c must always quit")
	}
}

func TestQQuitsOnTheScanHistoryAndItsFooterSaysSo(t *testing.T) {
	t.Parallel()
	for _, size := range [][2]int{{150, 24}, {200, 40}} {
		for _, id := range []screen{screenAdminScanRuns, screenAdminSecretFindings} {
			m := scanModelAt(t, scanFixtureClient(), id, size[0], size[1])
			m = scanPress(t, m, testKeyEnter())
			if m.adminScreens[slotScanHistory] == nil {
				t.Fatalf("%s: enter did not open the history", id)
			}
			if !quitsOnQ(m) {
				t.Errorf("%s: q did not quit the history", id)
			}
			if !strings.Contains(footerRow(m), "q quit") || !strings.Contains(footerRow(m), "F2") {
				t.Errorf("%s: footer = %q", id, footerRow(m))
			}
		}
	}
}
