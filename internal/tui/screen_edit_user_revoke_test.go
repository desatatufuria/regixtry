package tui

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"
	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// revokeClient is a fake whose delete really removes the grant from what
// ListUserGrants serves, so a reload after a revoke reads the new truth.
type revokeClient struct {
	*fakeAdminClient
	mu   sync.Mutex
	dels [][2]string
	err  error
}

func (c *revokeClient) DeleteRepositoryGrant(_ context.Context, _ AdminSession, repo, username string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dels = append(c.dels, [2]string{repo, username})
	if c.err != nil {
		return c.err
	}
	c.fakeAdminClient.mu.Lock()
	defer c.fakeAdminClient.mu.Unlock()
	for id, grants := range c.fakeAdminClient.grants {
		kept := grants[:0:0]
		for _, grant := range grants {
			if grant.Repository.String() != repo {
				kept = append(kept, grant)
			}
		}
		c.fakeAdminClient.grants[id] = kept
	}
	return nil
}

func (c *revokeClient) deletes() [][2]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][2]string(nil), c.dels...)
}

func newRevokeClient() *revokeClient { return &revokeClient{fakeAdminClient: editTestClient()} }

// revokeFocus opens carol (u-4: team/docs Reader, team/api Writer) and moves
// focus into the grants list.
func revokeFocus(t *testing.T, client AdminClient) (editUserScreen, screenEnv) {
	t.Helper()
	s, env := editOpen(t, client, "u-4", "carol")
	s, _ = editPress(t, s, env, testKeyTab())
	return s, env
}

func TestEditUserTabFocusesTheGrantsAndMovesTheSelection(t *testing.T) {
	t.Parallel()
	s, env := editOpen(t, newRevokeClient(), "u-4", "carol")
	if strings.Contains(renderEdit(s, env), "▸ team") {
		t.Fatal("a selection marker is shown before the grants have focus")
	}
	s, _ = editPress(t, s, env, testKeyTab())
	if out := renderEdit(s, env); !strings.Contains(out, "▸ team/docs") {
		t.Fatalf("first grant not marked after Tab:\n%s", out)
	}
	s, _ = editPress(t, s, env, testKeyDown())
	if out := renderEdit(s, env); !strings.Contains(out, "▸ team/api") {
		t.Fatalf("down did not select the second grant:\n%s", out)
	}
	s, _ = editPress(t, s, env, testKeyRunes("k"))
	if out := renderEdit(s, env); !strings.Contains(out, "▸ team/docs") {
		t.Fatalf("k did not move up:\n%s", out)
	}
	s, _ = editPress(t, s, env, testKeyTab())
	if strings.Contains(renderEdit(s, env), "▸ team") {
		t.Fatal("second Tab did not give the focus back")
	}
}

func TestEditUserHintsShowRevokeOnlyWithAGrantSelected(t *testing.T) {
	t.Parallel()
	hints := func(s editUserScreen) string {
		var out []string
		for _, b := range s.Keys().short {
			out = append(out, b.Help().Key+" "+b.Help().Desc)
		}
		return strings.Join(out, " | ")
	}
	s, env := editOpen(t, newRevokeClient(), "u-4", "carol")
	if got := hints(s); strings.Contains(got, "revoke") {
		t.Fatalf("revoke hinted without a grant selected: %s", got)
	}
	s, _ = editPress(t, s, env, testKeyTab())
	if got := hints(s); !strings.Contains(got, "x revoke") {
		t.Fatalf("revoke not hinted with a grant selected: %s", got)
	}
	empty := newRevokeClient()
	empty.grants = map[string][]ports.AdminRepoGrant{}
	s, env = editOpen(t, empty, "u-4", "carol")
	s, _ = editPress(t, s, env, testKeyTab(), testKeyRunes("x"))
	if s.confirm.Active() || strings.Contains(hints(s), "revoke") {
		t.Fatalf("a user without grants offers revoke: %s", hints(s))
	}
}

func TestEditUserXWithoutFocusDoesNothing(t *testing.T) {
	t.Parallel()
	s, env := editOpen(t, newRevokeClient(), "u-4", "carol")
	s, _ = editPress(t, s, env, testKeyRunes("x"))
	if s.confirm.Active() {
		t.Fatal("x opened a dialog while the grants had no focus")
	}
}

func TestEditUserRevokeDialogNamesUserRepositoryRoleAndTheTrueConsequence(t *testing.T) {
	t.Parallel()
	s, env := revokeFocus(t, newRevokeClient())
	s, _ = editPress(t, s, env, testKeyDown(), testKeyRunes("x"))
	if !s.confirm.Active() {
		t.Fatal("x opened no dialog")
	}
	frame := ansi.Strip(s.View(newAdminTheme(), env).Overlay)
	for _, want := range []string{"Revoke access", "Revoke carol's Writer access to team/api?", "carol on team/api", "carol loses Writer access to team/api.", "next request", "no token is revoked"} {
		if !strings.Contains(frame, want) {
			t.Errorf("dialog lacks %q:\n%s", want, frame)
		}
	}
}

func TestEditUserRevokeRemovesExactlyTheSelectedGrant(t *testing.T) {
	t.Parallel()
	client := newRevokeClient()
	s, env := revokeFocus(t, client)
	s, _ = editPress(t, s, env, testKeyDown(), testKeyRunes("x"), testKeyRunes("y"))
	if got := client.deletes(); len(got) != 1 || got[0] != [2]string{"team/api", "carol"} {
		t.Fatalf("deletes = %v, want team/api for carol", got)
	}
	if s.confirm.Active() {
		t.Fatal("dialog still open after the result")
	}
	if len(s.grants) != 1 || s.grants[0].Repository.String() != "team/docs" {
		t.Fatalf("grants after revoke = %v", s.grants)
	}
	if out := renderEdit(s, env); !strings.Contains(out, "▸ team/docs") || strings.Contains(out, "team/api") && !strings.Contains(out, "Revoked") {
		t.Fatalf("selection did not move to the remaining row / no notice:\n%s", out)
	}
	if !strings.Contains(s.notice, "Revoked carol on team/api") {
		t.Fatalf("notice = %q", s.notice)
	}
}

func TestEditUserRevokingTheLastGrantReturnsTheFocus(t *testing.T) {
	t.Parallel()
	client := newRevokeClient()
	client.grants["u-4"] = client.grants["u-4"][:1]
	s, env := revokeFocus(t, client)
	s, _ = editPress(t, s, env, testKeyRunes("x"), testKeyEnter())
	if len(s.grants) != 0 || !strings.Contains(renderEdit(s, env), "No repository grants.") {
		t.Fatalf("grants = %v", s.grants)
	}
	if strings.Contains(strings.Join(helpKeys(s), "|"), "revoke") {
		t.Fatal("revoke still hinted with no grants")
	}
}

func helpKeys(s editUserScreen) []string {
	var out []string
	for _, b := range s.Keys().short {
		out = append(out, b.Help().Key+" "+b.Help().Desc)
	}
	return out
}

func TestEditUserRevokeCancelAndDoubleEnter(t *testing.T) {
	t.Parallel()
	client := newRevokeClient()
	s, env := revokeFocus(t, client)
	s, _ = editPress(t, s, env, testKeyRunes("x"), testKeyEsc())
	if s.confirm.Active() || len(client.deletes()) != 0 {
		t.Fatal("Esc did not cancel without a call")
	}
	// Two Enters before the result is applied: one call.
	s, _ = editPress(t, s, env, testKeyRunes("x"))
	next, cmd, _ := s.Update(env, testKeyEnter())
	if cmd == nil {
		t.Fatal("Enter dispatched nothing")
	}
	s2, again, _ := next.Update(env, testKeyEnter())
	if again != nil {
		t.Fatal("second Enter in flight dispatched a second delete")
	}
	settled, _ := settleScreen(s2, env, cmd)
	if n := len(client.deletes()); n != 1 {
		t.Fatalf("%d deletes, want 1", n)
	}
	_ = settled
}

func TestEditUserRevokeIsBlockedWhileOneIsInFlightEvenAfterEsc(t *testing.T) {
	t.Parallel()
	client := newRevokeClient()
	s, env := revokeFocus(t, client)
	next, _, _ := s.Update(env, testKeyRunes("x"))
	next, cmd, _ := next.Update(env, testKeyEnter())
	next, _, _ = next.Update(env, testKeyEsc())
	next, second, _ := next.Update(env, testKeyRunes("x"))
	if next.(editUserScreen).confirm.Active() || second != nil {
		t.Fatal("a second revoke opened while the first was in flight")
	}
	if !strings.Contains(next.(editUserScreen).notice, "please wait") {
		t.Fatalf("notice = %q", next.(editUserScreen).notice)
	}
	settleScreen(next, env, cmd)
	if n := len(client.deletes()); n != 1 {
		t.Fatalf("%d deletes, want 1", n)
	}
}

func TestEditUserRevokeErrorKeepsTheListAndShowsTheRefusal(t *testing.T) {
	t.Parallel()
	client := newRevokeClient()
	client.err = errors.New("repository administrators cannot modify another repository administrator's grant")
	s, env := revokeFocus(t, client)
	s, _ = editPress(t, s, env, testKeyRunes("x"), testKeyEnter())
	if len(s.grants) != 2 || s.confirm.Active() {
		t.Fatalf("grants=%d confirm=%v after a refusal", len(s.grants), s.confirm.Active())
	}
	if out := renderEdit(s, env); !strings.Contains(out, "cannot modify another repository administrator") {
		t.Fatalf("refusal not shown:\n%s", out)
	}
	if out := renderEdit(s, env); !strings.Contains(out, "▸ team/docs") {
		t.Fatalf("selection lost after the refusal:\n%s", out)
	}
}

func TestEditUserRevokeExpiredSessionRedirectsToLogin(t *testing.T) {
	t.Parallel()
	client := newRevokeClient()
	client.err = NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	s, env := revokeFocus(t, client)
	next, _, _ := s.Update(env, testKeyRunes("x"))
	_, cmd, _ := next.Update(env, testKeyEnter())
	msg := cmd()
	typed, ok := msg.(userScreenMsg)
	if !ok {
		t.Fatalf("result %T is not a userScreenMsg: it would never reach the expiry redirect", msg)
	}
	if reason, expired := typed.expiredReason(); !expired || reason == "" {
		t.Fatal("expired result not reported as expired")
	}
	if typed.screenGen() != s.gen {
		t.Fatal("result carries another generation")
	}
}

func TestEditUserForeignGenerationRevokeResultIsDropped(t *testing.T) {
	t.Parallel()
	client := newRevokeClient()
	s, env := revokeFocus(t, client)
	next, _, _ := s.Update(env, testKeyRunes("x"))
	_, cmd, _ := next.Update(env, testKeyEnter())
	msg := cmd().(userScreenMsg)
	other := newEditUserScreen("u-4", "carol") // a different instance, new gen
	after, _, _ := other.Update(env, msg)
	if len(after.(editUserScreen).grants) != 0 || after.(editUserScreen).notice != "" {
		t.Fatal("a result of another screen instance was applied")
	}
}

// A reload that started before the revoke must not bring the row back.
func TestEditUserStaleReloadCannotResurrectARevokedGrant(t *testing.T) {
	t.Parallel()
	client := newRevokeClient()
	s, env := revokeFocus(t, client)
	stale := editUserLoadCmd(env, s.gen, s.userID)()               // reads both grants
	s, _ = editPress(t, s, env, testKeyRunes("x"), testKeyEnter()) // revoke team/docs
	if len(s.grants) != 1 {
		t.Fatalf("grants = %d after revoke", len(s.grants))
	}
	next, _, _ := s.Update(env, stale)
	if got := next.(editUserScreen).grants; len(got) != 1 || got[0].Repository.String() != "team/api" {
		t.Fatalf("a stale reload resurrected the grant: %v", got)
	}
}

func TestEditUserDialogTargetSurvivesReloadsAndMoves(t *testing.T) {
	t.Parallel()
	for seed := int64(0); seed < 200; seed++ {
		rng := rand.New(rand.NewSource(seed))
		client := newRevokeClient()
		n := []int{1, 2, 5, 100}[rng.Intn(4)]
		var grants []ports.AdminRepoGrant
		for i := 0; i < n; i++ {
			grants = append(grants, ports.AdminRepoGrant{UserID: "u-4", Repository: regixtrydomain.MustParseRepositoryRef(fmt.Sprintf("team/repo-%03d", i)), Role: domainauth.RepoRoleWriter})
		}
		client.grants["u-4"] = grants
		s, env := revokeFocus(t, client)
		for range rng.Intn(120) {
			if rng.Intn(2) == 0 {
				s, _ = editPress(t, s, env, testKeyDown())
			} else {
				s, _ = editPress(t, s, env, testKeyUp())
			}
		}
		want := s.grants[boundedIndex(s.grantCursor, len(s.grants))].Repository.String()
		s, _ = editPress(t, s, env, testKeyRunes("x"))
		if !s.confirm.Active() {
			t.Fatalf("seed %d: no dialog", seed)
		}
		// While the dialog is open the grants are reloaded in a new order.
		shuffled := append([]ports.AdminRepoGrant(nil), grants...)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		next, _, _ := s.Update(env, editUserLoadedMsg{gen: s.gen, userID: s.userID, users: usersTestUsers(), grants: shuffled})
		s = next.(editUserScreen)
		s, _ = editPress(t, s, env, testKeyEnter())
		got := client.deletes()
		if len(got) != 1 || got[0] != [2]string{want, "carol"} {
			t.Fatalf("seed %d: revoked %v, dialog was for %s", seed, got, want)
		}
	}
}

func TestEditUserQuitKeysAreUnchangedWithGrantsFocused(t *testing.T) {
	t.Parallel()
	s, env := revokeFocus(t, newRevokeClient())
	next, _, _ := s.Update(env, testKeyRunes("x"))
	if !next.(editUserScreen).confirm.Active() {
		t.Fatal("no dialog")
	}
}

func TestEditUserGrantsPanelFitsEverySizeWithManyAndLongGrants(t *testing.T) {
	t.Parallel()
	// Repository names are ASCII; the multibyte case is the user name in the dialog.
	const long = "organisation-with-a-long-name-%d/service-repository-with-an-even-longer-name-for-wrapping"
	for _, count := range []int{1, 100} {
		for _, size := range geometrySizes {
			client := newRevokeClient()
			var grants []ports.AdminRepoGrant
			for i := 0; i < count; i++ {
				name := fmt.Sprintf("team/api%d", i)
				if i%2 == 0 {
					name = fmt.Sprintf(long, i)
				}
				grants = append(grants, ports.AdminRepoGrant{UserID: "u-4", Repository: regixtrydomain.MustParseRepositoryRef(name), Role: domainauth.RepoRoleAdmin})
			}
			client.grants["u-4"] = grants
			env := usersTestEnv(client, size.w, size.h)
			s := newEditUserScreen("u-4", "carol")
			next, _ := settleScreen(s, env, s.Init(env))
			s = next.(editUserScreen)
			s.username = "ünïcødé-用户-" + strings.Repeat("x", 30)
			s, _ = editPress(t, s, env, testKeyTab())
			for range count / 2 {
				s, _ = editPress(t, s, env, testKeyDown())
			}
			for _, open := range []bool{false, true} {
				if open {
					s, _ = editPress(t, s, env, testKeyRunes("x"))
				}
				frame := s.View(newAdminTheme(), env)
				for _, body := range []string{frame.Body, frame.Overlay} {
					lines := strings.Split(ansi.Strip(body), "\n")
					if len(lines) > size.h && body == frame.Body {
						t.Errorf("%dx%d count=%d: %d rows", size.w, size.h, count, len(lines))
					}
					for i, line := range lines {
						if w := ansi.StringWidth(line); w > size.w {
							t.Errorf("%dx%d count=%d row %d is %d wide", size.w, size.h, count, i, w)
						}
					}
				}
				if !open && !strings.Contains(ansi.Strip(frame.Body), "▸ organisation") && !strings.Contains(ansi.Strip(frame.Body), "▸ team") {
					t.Errorf("%dx%d count=%d: the selection is not visible", size.w, size.h, count)
				}
			}
		}
	}
}

func TestEditUserRevokeGoldens(t *testing.T) {
	t.Parallel()
	for _, size := range usersViewSizes {
		client := newRevokeClient()
		env := usersTestEnv(client, size.w, size.h)
		s := newEditUserScreen("u-4", "carol")
		next, _ := settleScreen(s, env, s.Init(env))
		s = next.(editUserScreen)
		s, _ = editPress(t, s, env, testKeyTab(), testKeyDown())
		assertUsersGolden(t, fmt.Sprintf("edit_user_grants_focused_%dx%d", size.w, size.h), renderEdit(s, env))
		s, _ = editPress(t, s, env, testKeyRunes("x"))
		frame := s.View(newAdminTheme(), env)
		assertUsersGolden(t, fmt.Sprintf("edit_user_revoke_dialog_%dx%d", size.w, size.h), ansi.Strip(frame.Body)+"\n-- overlay --\n"+ansi.Strip(frame.Overlay))
	}
}
