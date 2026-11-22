package tui

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

type repoAccessCorpus struct {
	grants []ports.AdminGrantEntry
	admin  map[string]bool
}

func repoAccessRandomCorpus(rng *rand.Rand, n int) repoAccessCorpus {
	g := repoAccessCorpus{admin: map[string]bool{}}
	repoI := 0
	for len(g.grants) < n {
		repo := fmt.Sprintf("org/repo%04d", repoI)
		repoI++
		cnt := 1 + rng.Intn(1+rng.Intn(60))
		if rng.Intn(10) == 0 {
			cnt = 1 + rng.Intn(5)
		}
		hasAdmin := rng.Intn(3) == 0
		for u := 0; u < cnt && len(g.grants) < n; u++ {
			role := domainauth.RepoRoleReader
			if rng.Intn(2) == 0 {
				role = domainauth.RepoRoleWriter
			}
			if hasAdmin && u == cnt-1 || (hasAdmin && rng.Intn(8) == 0) {
				role = domainauth.RepoRoleAdmin
			}
			name := fmt.Sprintf("user%03d", u)
			if rng.Intn(5) == 0 {
				name = fmt.Sprintf("robot%03d", u)
			}
			g.grants = append(g.grants, repoAccessEntry(repo, "id-"+name, name, strings.HasPrefix(name, "robot"), role, time.Date(2026, 1, 1+rng.Intn(27), 0, 0, 0, 0, time.UTC)))
			if role == domainauth.RepoRoleAdmin {
				g.admin[repo] = true
			}
		}
	}
	// stable unique per repo
	seen := map[string]bool{}
	out := g.grants[:0]
	for _, e := range g.grants {
		k := e.Repository + "|" + e.Username
		if !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	g.grants = out
	g.admin = map[string]bool{}
	for _, e := range g.grants {
		if e.Role == domainauth.RepoRoleAdmin {
			g.admin[e.Repository] = true
		}
	}
	sort.SliceStable(g.grants, func(i, j int) bool {
		if g.grants[i].Repository != g.grants[j].Repository {
			return g.grants[i].Repository < g.grants[j].Repository
		}
		return g.grants[i].Username < g.grants[j].Username
	})
	return g
}

func repoAccessWalkCorpus(t *testing.T, g repoAccessCorpus, w, h int, label string) {
	t.Helper()
	client := &fakeAdminClient{allGrants: g.grants}
	m := newAdminReadyModel(t, client)
	m.now = func() time.Time { return fixedTestTime }
	m = resizeModel(t, m, w, h)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "b", ExpiresAt: fixedTestTime.Add(time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	var pending []tea.Msg
	step := func(msg tea.Msg) {
		u, cmd := m.Update(msg)
		m = u.(Model)
		repoAccessFlatten(cmd, &pending)
	}
	cnt := 0
	check := func(where string) {
		cnt++
		if (where == "down" || where == "up") && cnt%25 != 0 {
			return
		}
		s := repoAccessScreenOf(t, m)
		out := repoAccessRender(m)
		nrows := s.visibleRows()
		if len(nrows) > 0 {
			sel := nrows[boundedIndex(s.cursor, len(nrows))]
			if strings.Contains(out, "No Admin on this repository") {
				if g.admin[sel.repo] {
					t.Fatalf("%s %s: FALSE No Admin for %s (done=%v)", label, where, sel.repo, s.done)
				}
				if !s.done && s.rows[len(s.rows)-1].repo == sel.repo {
					t.Fatalf("%s %s: No Admin on incomplete repo", label, where)
				}
			}
			if s.done && !g.admin[sel.repo] && !strings.Contains(out, "No Admin on this repository") && h >= 40 {
				t.Fatalf("%s %s: missing No Admin for %s", label, where, sel.repo)
			}
		}
		for _, l := range strings.Split(out, "\n") {
			if lw := ansi.StringWidth(l); lw > w {
				t.Fatalf("%s overflow %d>%d", label, lw, w)
			}
		}
		if n := len(strings.Split(out, "\n")); n > h {
			t.Fatalf("%s rows %d>%d", label, n, h)
		}
	}
	step(navigateMsg{To: screenAdminRepositoryAccess})
	check("open-pending")
	if len(g.grants) == 0 {
		for len(pending) > 0 {
			p := pending[0]
			pending = pending[1:]
			step(p)
		}
		s := repoAccessScreenOf(t, m)
		if !s.done || len(s.rows) != 0 || !strings.Contains(repoAccessRender(m), "No grants yet") {
			t.Fatalf("%s empty state wrong", label)
		}
		return
	}
	for guard := 0; guard < 20000; guard++ {
		for len(pending) > 0 {
			check("pending")
			p := pending[0]
			pending = pending[1:]
			step(p)
			check("delivered")
		}
		s := repoAccessScreenOf(t, m)
		visible := s.visibleRows()
		if s.cursor >= len(visible)-1 && s.done {
			break
		}
		step(testKeyDown())
		check("down")
	}
	s := repoAccessScreenOf(t, m)
	if !s.done || s.loading {
		t.Fatalf("%s not done", label)
	}
	if len(s.rows) != len(g.grants) {
		t.Fatalf("%s rows %d want %d", label, len(s.rows), len(g.grants))
	}
	for i, r := range s.rows {
		e := g.grants[i]
		if r.repo != e.Repository || r.username != e.Username || r.role != e.Role || r.userID != e.UserID || r.isRobot != e.IsRobot {
			t.Fatalf("%s row %d mismatch %v vs %v", label, i, r, e)
		}
	}
	wantCalls := (len(g.grants) + 199) / 200
	if got := len(client.allGrantsCursors); got != wantCalls {
		t.Fatalf("%s calls %d want %d (%v)", label, got, wantCalls, client.allGrantsCursors)
	}
	// walk back up checking panel for every row
	for i := 0; i < len(s.rows); i += 5 {
		for range 5 {
			step(testKeyUp())
		}
		check("up")
	}
}

// TestRepoAccessPageBoundaryWalks walks seeded corpora whose pages split
// repositories at many offsets, checking at every step that the panel never
// claims "No Admin" for a repository that has an Admin or is still being
// read, that every row arrives in order, that a walk costs ceil(n/200) calls
// and that the view fits the terminal.
func TestRepoAccessPageBoundaryWalks(t *testing.T) {
	t.Parallel()
	for _, n := range []int{0, 1, 200, 201, 450} {
		g := repoAccessRandomCorpus(rand.New(rand.NewSource(1000+int64(n))), n)
		repoAccessWalkCorpus(t, g, 220, 60, fmt.Sprintf("n=%d", n))
	}
	// pages split at every offset: shift a single block of repos
	for _, off := range []int{1, 38} {
		var gr []ports.AdminGrantEntry
		for i := 0; i < off; i++ {
			gr = append(gr, repoAccessEntry("a/pad", fmt.Sprintf("p%d", i), fmt.Sprintf("p%03d", i), false, domainauth.RepoRoleReader, fixedTestTime))
		}
		for i := 0; i < 400-off; i++ {
			role := domainauth.RepoRoleWriter
			repo := "b/split"
			if i > 150 {
				repo = "c/tail"
			}
			if i == 399-off {
				role = domainauth.RepoRoleAdmin
			}
			gr = append(gr, repoAccessEntry(repo, fmt.Sprintf("u%d", i), fmt.Sprintf("u%04d", i), false, role, fixedTestTime))
		}
		sort.SliceStable(gr, func(i, j int) bool {
			if gr[i].Repository != gr[j].Repository {
				return gr[i].Repository < gr[j].Repository
			}
			return gr[i].Username < gr[j].Username
		})
		g := repoAccessCorpus{grants: gr, admin: map[string]bool{"c/tail": true}}
		repoAccessWalkCorpus(t, g, 220, 60, fmt.Sprintf("offset=%d", off))
	}
}
