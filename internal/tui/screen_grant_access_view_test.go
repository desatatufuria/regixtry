package tui

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"regixtry/internal/ports"

	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
)

// The grant screen has its own flag so regenerating its goldens never touches
// another screen's.
var updateGrantGoldens = flag.Bool("update-grant-screen", false, "rewrite the grant screen golden files")

var grantViewSizes = []struct{ w, h int }{{150, 24}, {200, 40}}

var grantViewKnown = []string{
	"library/alpine", "library/nginx", "ops/console", "team/backend",
	"team/demo", "team/frontend", "team/infra", "team/web",
}

func grantViewClient() *recordingGrantClient {
	c := newRecordingGrantClient()
	c.users = []ports.AdminUser{
		{ID: "u-1", Username: "alice", Enabled: true},
		{ID: "u-2", Username: "bob", Enabled: true},
		{ID: "u-3", Username: "carol", Enabled: true},
	}
	c.robots = []ports.AdminRobot{{ID: "r-1", Username: "ci-bot", Enabled: true}}
	c.grantsFor["u-1"] = []ports.AdminRepoGrant{
		grantTestGrant("u-1", "team/demo", domainauth.RepoRoleReader),
		grantTestGrant("u-1", "ops/console", domainauth.RepoRoleWriter),
	}
	return c
}

// grantViewModelAt opens the screen through the Model (the real path) and
// returns the model with an authenticated admin session at the given size.
func grantViewModelAt(t *testing.T, client AdminClient, known []string, w, h int) Model {
	t.Helper()
	m := newAdminReadyModelWithCatalog(t, known, client)
	m.now = func() time.Time { return fixedTestTime }
	m = resizeModel(t, m, w, h)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedTestTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	updated, cmd := m.Update(openGrantAccessMsg{returnTo: screenAdminOverview})
	return runCmd(t, updated.(Model), cmd)
}

func grantRender(m Model) string { return ansi.Strip(m.View().Content) }

func assertGrantGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "grant_screen", name+".golden")
	if *updateGrantGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update-grant-screen): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("rendered view differs from %s (run with -update-grant-screen to inspect)\n--- want\n%s\n--- got\n%s", path, strings.TrimRight(string(want), "\n"), strings.TrimRight(got, "\n"))
	}
}

func assertGrantNoOverflow(t *testing.T, label, rendered string, w, h int) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	if len(lines) > h {
		t.Fatalf("%s: %d rows, terminal has %d\n%s", label, len(lines), h, rendered)
	}
	for i, line := range lines {
		if width := ansi.StringWidth(line); width > w {
			t.Fatalf("%s: line %d is %d cells wide, terminal has %d: %q", label, i+1, width, w, line)
		}
	}
}

func grantStateInitial(t *testing.T, w, h int) Model {
	return grantViewModelAt(t, grantViewClient(), grantViewKnown, w, h)
}

// grantStateFiltered: two repositories ticked, then the filter narrowed to a
// name that is not pushed yet, with that row ticked too.
func grantStateFiltered(t *testing.T, w, h int) Model {
	m := grantStateInitial(t, w, h)
	m = grantModelPress(t, m, testKeyDown(), testKeySpace())
	m = grantModelPress(t, m, testKeyTab(), testKeyDown(), testKeySpace(), testKeyDown(), testKeyDown(), testKeySpace())
	m = grantModelPress(t, m, grantType("team/newrepo")...)
	return grantModelPress(t, m, testKeySpace())
}

// grantStatePartial: three repositories applied as Writer, the second fails.
func grantStatePartial(t *testing.T, w, h int) Model {
	client := grantViewClient()
	client.putErrors["library/nginx"] = errors.New("repository is locked by a retention policy")
	m := grantViewModelAt(t, client, grantViewKnown, w, h)
	m = grantModelPress(t, m, testKeyDown(), testKeySpace())
	m = grantModelPress(t, m, testKeyTab(), testKeySpace(), testKeyDown(), testKeySpace(), testKeyDown(), testKeySpace())
	m = grantModelPress(t, m, testKeyTab(), testKeyRunes("2"))
	return grantModelPress(t, m, testKeyEnter())
}

// grantStateStress is worst-case content: far more principals, repositories,
// grants and selections than any pane can show, with very long names.
func grantStateStress(t *testing.T, w, h int) Model {
	client := newRecordingGrantClient()
	client.users, client.robots = nil, nil
	for i := 0; i < 60; i++ {
		client.users = append(client.users, ports.AdminUser{ID: fmt.Sprintf("u-%d", i), Username: fmt.Sprintf("a-really-long-username-for-person-number-%02d", i)})
	}
	client.robots = []ports.AdminRobot{{ID: "r-1", Username: "robot-with-an-extremely-long-name-that-keeps-going-and-going"}}
	var known []string
	for i := 0; i < 70; i++ {
		name := fmt.Sprintf("organization-%02d/a-deeply-nested/repository-with-a-very-long-name-%02d", i, i)
		known = append(known, name)
		if i < 30 {
			client.grantsFor["u-0"] = append(client.grantsFor["u-0"], ports.AdminRepoGrant{
				UserID: "u-0", Repository: regixtrydomain.MustParseRepositoryRef(name), Role: domainauth.RepoRoleAdmin,
			})
		}
	}
	m := grantViewModelAt(t, client, known, w, h)
	m = grantModelPress(t, m, testKeyTab(), testKeyCtrl('a'))
	m = grantModelPress(t, m, grantType("organization-0")...)
	m = grantModelPress(t, m, testKeyCtrl('a'))
	m = grantModelPress(t, m, grantType("1/brand-new-repository-with-another-extremely-long-name-for-sure")...)
	m = grantModelPress(t, m, testKeySpace(), testKeyTab(), testKeyRunes("3"))
	return m
}

func TestGrantScreenGoldens(t *testing.T) {
	states := []struct {
		name  string
		build func(t *testing.T, w, h int) Model
	}{
		{"grant_initial", grantStateInitial},
		{"grant_filtered_new_name", grantStateFiltered},
		{"grant_partial_failure", grantStatePartial},
	}
	for _, state := range states {
		for _, size := range grantViewSizes {
			name := fmt.Sprintf("%s_%dx%d", state.name, size.w, size.h)
			t.Run(name, func(t *testing.T) {
				assertGrantGolden(t, name, grantRender(state.build(t, size.w, size.h)))
			})
		}
	}
}

func TestGrantScreenNeverOverflowsTheTerminal(t *testing.T) {
	states := []struct {
		name  string
		build func(t *testing.T, w, h int) Model
	}{
		{"initial", grantStateInitial},
		{"filtered", grantStateFiltered},
		{"partial", grantStatePartial},
		{"stress", grantStateStress},
	}
	for _, state := range states {
		for _, size := range grantViewSizes {
			t.Run(fmt.Sprintf("%s_%dx%d", state.name, size.w, size.h), func(t *testing.T) {
				rendered := grantRender(state.build(t, size.w, size.h))
				assertGrantNoOverflow(t, state.name, rendered, size.w, size.h)
				if !strings.Contains(rendered, "Who") || !strings.Contains(rendered, "Which repositories") || !strings.Contains(rendered, "Role and review") {
					t.Fatalf("a pane title is missing:\n%s", rendered)
				}
			})
		}
	}
}

func TestGrantScreenAlsoFitsOtherSizesAtOrAboveTheFloor(t *testing.T) {
	for _, size := range []struct{ w, h int }{{151, 25}, {160, 30}, {173, 33}, {220, 60}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			assertGrantNoOverflow(t, "stress", grantRender(grantStateStress(t, size.w, size.h)), size.w, size.h)
			assertGrantNoOverflow(t, "partial", grantRender(grantStatePartial(t, size.w, size.h)), size.w, size.h)
		})
	}
}

func TestGrantScreenShowsTheExactMatchHintAndTheReview(t *testing.T) {
	rendered := grantRender(grantStateFiltered(t, 150, 24))
	for _, want := range []string{
		"no wildcards",
		"+ team/newrepo",
		"not pushed yet",
		"alice becomes Reader on 3 repositories:",
		"Already has",
		"Reader",
		"pull, push, delete tags",
		"Writer plus manage who has access",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("missing %q in:\n%s", want, rendered)
		}
	}
}
