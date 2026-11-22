package tui

import (
	"context"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

type delegateFuzzClient struct {
	*fakeAdminClient
	mu       sync.Mutex
	rng      *rand.Rand
	grants   []ports.AdminRepositoryGrant
	puts     []ports.AdminPutRepositoryGrantInput
	deletes  [][2]string
	failList bool
}

func (c *delegateFuzzClient) ListRepositoryGrants(_ context.Context, _ AdminSession, repo string) ([]ports.AdminRepositoryGrant, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failList {
		return nil, context.DeadlineExceeded
	}
	out := append([]ports.AdminRepositoryGrant(nil), c.grants...)
	c.rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out, nil
}
func (c *delegateFuzzClient) PutRepositoryGrant(_ context.Context, _ AdminSession, in ports.AdminPutRepositoryGrantInput) (ports.AdminRepositoryGrant, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.puts = append(c.puts, in)
	return ports.AdminRepositoryGrant{}, nil
}
func (c *delegateFuzzClient) DeleteRepositoryGrant(_ context.Context, _ AdminSession, repo, user string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deletes = append(c.deletes, [2]string{repo, user})
	return nil
}

func TestDelegateEverySentRequestIsWhatTheFormOrDialogShowed(t *testing.T) {
	for seed := int64(1); seed <= 12; seed++ {
		delegateFuzzOne(t, seed)
	}
	// The walk must actually reach the requests it checks.
	if delegateFuzzPuts == 0 || delegateFuzzDels == 0 || delegateFuzzEdits == 0 {
		t.Fatalf("the walk sent puts=%d deletes=%d edits=%d, want all three", delegateFuzzPuts, delegateFuzzDels, delegateFuzzEdits)
	}
}

var delegateFuzzPuts, delegateFuzzDels, delegateFuzzEdits int

func delegateFuzzOne(t *testing.T, seed int64) {
	rng := rand.New(rand.NewSource(seed))
	cl := &delegateFuzzClient{fakeAdminClient: &fakeAdminClient{}, rng: rng}
	names := []string{"alice", "Alice", "ALICE", "bob", "Bob", "carol", "dave", "zed"}
	for _, n := range names {
		role := domainauth.RepoRoleReader
		if rng.Intn(2) == 0 {
			role = domainauth.RepoRoleWriter
		}
		cl.grants = append(cl.grants, ports.AdminRepositoryGrant{Username: n, Role: role})
	}
	cl.grants = append(cl.grants, ports.AdminRepositoryGrant{Username: "boss", Role: domainauth.RepoRoleAdmin}, ports.AdminRepositoryGrant{Username: "Peer", Role: domainauth.RepoRoleAdmin})
	for i := 0; i < rng.Intn(150); i++ {
		cl.grants = append(cl.grants, ports.AdminRepositoryGrant{Username: strings.Repeat("u", 1+i%9) + string(rune('a'+i%26)), Role: domainauth.RepoRoleReader})
	}
	m := delegateConsole(t, cl, []string{"team/api"}, 150, 24)
	var pending []tea.Cmd
	deliver := func(cmd tea.Cmd) {
		var walk func(c tea.Cmd)
		walk = func(c tea.Cmd) {
			if c == nil {
				return
			}
			msg := c()
			if b, ok := msg.(tea.BatchMsg); ok {
				for _, s := range b {
					pending = append(pending, s)
				}
				return
			}
			u, next := m.Update(msg)
			m = u.(Model)
			if next != nil {
				pending = append(pending, next)
			}
		}
		walk(cmd)
	}
	runSome := func() {
		if len(pending) == 0 {
			return
		}
		i := rng.Intn(len(pending))
		c := pending[i]
		pending = append(pending[:i], pending[i+1:]...)
		deliver(c)
	}
	type put struct {
		repo, user string
		role       domainauth.RepoRole
	}
	var wantPuts []put
	var wantDels [][2]string
	var xTarget [2]string
	var editUser string
	press := func(k keyMsg) {
		u, cmd := m.Update(k)
		m = u.(Model)
		if cmd != nil {
			pending = append(pending, cmd)
		}
	}
	keys := []func() keyMsg{testKeyUp, testKeyDown, testKeyPgUp, testKeyPgDown, testKeyTab, testKeySpace, testKeyBackspace, testKeyEnter,
		func() keyMsg { return testKeyRunes("n") }, func() keyMsg { return testKeyRunes("e") }, func() keyMsg { return testKeyRunes("x") },
		func() keyMsg { return testKeyRunes("y") }, func() keyMsg { return testKeyRunes("r") }, func() keyMsg { return testKeyEsc() },
		func() keyMsg { return testKeyRunes("j") }, func() keyMsg { return testKeyRunes("k") },
		func() keyMsg { return testKeyRunes("a") }, func() keyMsg { return testKeyRunes("B") }, func() keyMsg { return testKeyRunes("o") },
	}
	press(testKeyRunes("g"))
	for step := 0; step < 400; step++ {
		if rng.Intn(3) == 0 {
			runSome()
		}
		if rng.Intn(40) == 0 {
			cl.mu.Lock()
			cl.failList = !cl.failList
			cl.mu.Unlock()
		}
		if m.screen != screenRepoAdminGrants {
			press(testKeyRunes("g"))
			continue
		}
		s, ok := m.adminScreens[slotDelegate].(delegateScreen)
		if !ok {
			t.Fatalf("seed %d: no screen", seed)
		}
		var k keyMsg
		if rng.Intn(5) == 0 && s.form.active {
			// type a username fragment
			frag := []string{"alice", "ALICE", "Bob", "carol", "boss", "peer", "newuser", "x y"}[rng.Intn(8)]
			for _, r := range frag {
				press(testKeyRunes(string(r)))
			}
			continue
		}
		k = keys[rng.Intn(len(keys))]()
		isEnter := isEnterKey(k)
		isY := isPlainYKey(k)
		sel, hasSel := s.selected()
		switch {
		case s.confirm.Active() && (isEnter || isY):
			if !s.confirm.inFlight && s.confirm.onConfirm != nil {
				wantDels = append(wantDels, xTarget)
			}
		case !s.confirm.Active() && s.form.active && isEnter:
			uname := strings.TrimSpace(s.form.username)
			conflict := false
			for _, r := range s.rows {
				if strings.EqualFold(r.Username, uname) && r.Role == domainauth.RepoRoleAdmin {
					conflict = true
				}
			}
			if s.form.editing && s.form.username != editUser {
				t.Fatalf("seed %d: edit form username %q != selected-at-e %q", seed, s.form.username, editUser)
			}
			if s.form.role != domainauth.RepoRoleReader && s.form.role != domainauth.RepoRoleWriter {
				t.Fatalf("seed %d: form role %q", seed, s.form.role)
			}
			if uname != "" && !s.saving && !conflict {
				wantPuts = append(wantPuts, put{"team/api", uname, s.form.role})
				if s.form.editing {
					delegateFuzzEdits++
				}
			}
		case !s.confirm.Active() && !s.form.active && isRuneKey(k, 'x') && hasSel && !s.removing:
			xTarget = [2]string{"team/api", sel.Username}
		case !s.confirm.Active() && !s.form.active && isRuneKey(k, 'e') && hasSel && sel.Role != domainauth.RepoRoleAdmin && !s.saving && !s.removing:
			editUser = sel.Username
		}
		press(k)
		if isRuneKey(k, 'x') && !s.confirm.Active() && !s.form.active && hasSel && !s.removing {
			s2 := m.adminScreens[slotDelegate].(delegateScreen)
			if s2.confirm.Active() {
				out := ansi.Strip(m.View().Content)
				if !strings.Contains(out, sel.Username) {
					t.Fatalf("seed %d: dialog does not show %q", seed, sel.Username)
				}
			}
		}
		if step%50 != 0 {
			continue
		}
		out := ansi.Strip(m.View().Content)
		lines := strings.Split(out, "\n")
		if len(lines) != 24 {
			t.Fatalf("seed %d: height %d", seed, len(lines))
		}
		for _, l := range lines {
			if ansi.StringWidth(l) > 150 {
				t.Fatalf("seed %d: overflow %d", seed, ansi.StringWidth(l))
			}
		}
	}
	for i := 0; i < 5000 && len(pending) > 0; i++ {
		runSome()
	}
	cl.mu.Lock()
	defer cl.mu.Unlock()
	delegateFuzzPuts += len(cl.puts)
	delegateFuzzDels += len(cl.deletes)
	var gotPuts []put
	for _, p := range cl.puts {
		gotPuts = append(gotPuts, put{p.Repository, p.Username, p.Role})
	}
	less := func(a, b string) bool { return a < b }
	_ = less
	key := func(p put) string { return p.repo + "|" + p.user + "|" + string(p.role) }
	sk := func(ps []put) []string {
		var o []string
		for _, p := range ps {
			o = append(o, key(p))
		}
		sort.Strings(o)
		return o
	}
	if !reflect.DeepEqual(sk(gotPuts), sk(wantPuts)) {
		t.Fatalf("seed %d: puts got %v want %v", seed, sk(gotPuts), sk(wantPuts))
	}
	sd := func(d [][2]string) []string {
		var o []string
		for _, x := range d {
			o = append(o, x[0]+"|"+x[1])
		}
		sort.Strings(o)
		return o
	}
	if !reflect.DeepEqual(sd(cl.deletes), sd(wantDels)) {
		t.Fatalf("seed %d: deletes got %v want %v", seed, sd(cl.deletes), sd(wantDels))
	}
}
