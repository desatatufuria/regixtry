package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

var overflowSizes = []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}}

const overflowSecret = "rgx_0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-_0123456789"

var overflowStates = []struct {
	name string
	keys []keyMsg
}{
	{"default", nil},
	{"custom_empty", toCustom()},
	{"custom_valid", withCustom("1", "2", "h")},
	{"custom_invalid", withCustom("3", "y")},
	{"repository_focus", []keyMsg{testKeyTab(), testKeyTab()}},
}

// robotFormForOverflow builds the Create Robot form with `suggestions`
// matching repositories, the focus on Lifetime, and optionally a revealed
// one-time secret.
func robotFormForOverflow(t *testing.T, w, h, suggestions int, secret bool, keys []keyMsg) Model {
	t.Helper()
	repos := []string{}
	for i := 0; i < suggestions; i++ {
		repos = append(repos, fmt.Sprintf("team/app-%d", i+1))
	}
	m := newAdminReadyModelWithCatalog(t, repos, newTTLClient())
	m.now = func() time.Time { return charFixedTime }
	m = charResize(t, m, w, h)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: charFixedTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	m.screen = screenAdminCreateRobot
	if secret {
		m.adminView.RevealedTokenSecret = overflowSecret
		m.adminView.RevealedTokenAccessor = "acc-123"
		m.status = `Robot "ci-bot" created.`
	}
	m = testSendKeys(t, m, testKeyRunes("ci-bot"), testKeyTab(), testKeyRunes("team/app"), testKeyTab(), testKeyTab())
	return testSendKeys(t, m, keys...)
}

func TestCreateRobotFormNeverExceedsTheTerminal(t *testing.T) {
	for _, size := range overflowSizes {
		for _, secret := range []bool{false, true} {
			for suggestions := 0; suggestions <= 5; suggestions++ {
				for _, state := range overflowStates {
					name := fmt.Sprintf("%s secret=%v n=%d %s", sizeLabel(size.w, size.h), secret, suggestions, state.name)
					t.Run(name, func(t *testing.T) {
						m := robotFormForOverflow(t, size.w, size.h, suggestions, secret, state.keys)
						rendered := ansi.Strip(m.View().Content)
						lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
						if len(lines) > size.h {
							t.Fatalf("%d rows, terminal has %d", len(lines), size.h)
						}
						if secret && !strings.Contains(rendered, overflowSecret) {
							t.Fatal("the one-time secret is not fully visible")
						}
						if suggestions > 0 && !strings.Contains(rendered, "team/app-1") {
							t.Fatal("the selected suggestion is not visible")
						}
						for _, want := range []string{"Lifetime", "Esc: "} {
							if !strings.Contains(rendered, want) {
								t.Fatalf("%q is not visible", want)
							}
						}
					})
				}
			}
		}
	}
}

var updateThreeFixesGoldens = flag.Bool("update-three-fixes", false, "rewrite the three-fixes golden files")

func TestCreateRobotFormWithSecretAndFiveSuggestionsGolden(t *testing.T) {
	for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
		name := "robot_secret_five_suggestions_" + sizeLabel(size.w, size.h)
		t.Run(name, func(t *testing.T) {
			m := robotFormForOverflow(t, size.w, size.h, 5, true, nil)
			got := ansi.Strip(m.View().Content)
			path := filepath.Join("testdata", "three_fixes", name+".golden")
			if *updateThreeFixesGoldens {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run with -update-three-fixes): %v", err)
			}
			if string(want) != got {
				t.Fatalf("view differs from %s\n--- want\n%s\n--- got\n%s", path, want, got)
			}
		})
	}
}
