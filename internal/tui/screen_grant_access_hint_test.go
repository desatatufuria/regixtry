package tui

import (
	"strings"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// The per-repository hint may shorten the principal name but never the roles.
func TestGrantRepoHintNeverCutsTheTargetRole(t *testing.T) {
	t.Parallel()
	roles := []domainauth.RepoRole{domainauth.RepoRoleReader, domainauth.RepoRoleWriter, domainauth.RepoRoleAdmin}
	keys := map[domainauth.RepoRole]keyMsg{
		domainauth.RepoRoleReader: testKeyRunes("1"),
		domainauth.RepoRoleWriter: testKeyRunes("2"),
		domainauth.RepoRoleAdmin:  testKeyRunes("3"),
	}
	sizes := []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}}
	for _, size := range sizes {
		for _, n := range []int{5, 20, 40} {
			for _, from := range roles {
				for _, to := range roles {
					name := strings.Repeat("a", n)
					client := grantViewClient()
					client.users = []ports.AdminUser{{ID: "u-1", Username: name, Enabled: true}}
					client.robots = nil
					client.grantsFor = map[string][]ports.AdminRepoGrant{"u-1": {grantTestGrant("u-1", "team/demo", from)}}
					m := grantViewModelAt(t, client, grantViewKnown, size.w, size.h)
					m = grantModelPress(t, m, testKeyDown(), testKeySpace(), testKeyTab(), testKeyTab(), keys[to])
					out := grantRender(m)
					assertGrantNoOverflow(t, "hint", out, size.w, size.h)
					row := ""
					for _, line := range strings.Split(out, "\n") {
						if strings.Contains(line, "team/demo") && strings.Contains(line, "[") {
							row = line
						}
					}
					want := grantRoleLabel(to)
					if !strings.Contains(row, want) {
						t.Errorf("%dx%d name=%d %s->%s: row %q does not show the role %q whole", size.w, size.h, n, from, to, row, want)
					}
				}
			}
		}
	}
}

func TestGrantRepoPreviewFitShortensOnlyTheName(t *testing.T) {
	t.Parallel()
	alice := grantPrincipal{ID: "u-1", Name: "alice-with-a-long-name"}
	grants := []ports.AdminRepoGrant{grantTestGrant("u-1", "team/docs", domainauth.RepoRoleReader)}
	s := grantAccessScreen{role: domainauth.RepoRoleWriter, grants: map[string]grantsEntry{"u-1": {loaded: true, items: grants}}}
	for width, want := range map[int]string{
		100: "alice-with-a-long-name is Reader, will become Writer",
		40:  "alice-with-a-l… is Reader, will become Writer",
		30:  "Reader to Writer",
		16:  "Reader to Writer",
		15:  "to Writer",
		8:   "",
	} {
		got := s.repoPreviewFit(alice, "team/docs", width)
		if width == 40 {
			if !strings.HasSuffix(got, " is Reader, will become Writer") || len([]rune(got)) > 40 {
				t.Errorf("width 40: %q", got)
			}
			continue
		}
		if got != want {
			t.Errorf("width %d: %q, want %q", width, got, want)
		}
	}
	if got := s.repoPreviewFit(alice, "team/none", 6); got != "none" {
		t.Errorf("no access narrow: %q", got)
	}
	s.role = domainauth.RepoRoleReader
	if got := s.repoPreviewFit(alice, "team/docs", 8); got != "Reader" {
		t.Errorf("same role narrow: %q", got)
	}
}
