package tui

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"regixtry/internal/ports"
)

// ---- r refreshes ----

func TestUsersRReloadsUsersAndExpiringKeepingTheSelection(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	s, env := usersOpen(t, client, "u-4")
	users, expiring := client.listUsersCalls, client.listExpiringCalls
	s, _ = usersPress(t, s, env, testKeyRunes("r"))
	if client.listUsersCalls != users+1 || client.listExpiringCalls != expiring+1 {
		t.Fatalf("r reads: users %d->%d expiring %d->%d, want one more each", users, client.listUsersCalls, expiring, client.listExpiringCalls)
	}
	if user, _ := s.current(); user.ID != "u-4" || s.refreshing || s.notice != "" {
		t.Fatalf("after r: on %q refreshing=%v notice=%q", user.ID, s.refreshing, s.notice)
	}
	// The user was removed meanwhile: the cursor falls back to the first row.
	client.users = client.users[:3]
	s, _ = usersPress(t, s, env, testKeyRunes("r"))
	if user, ok := s.current(); !ok || user.ID != "u-1" {
		t.Fatalf("after the user vanished the cursor is on %q", user.ID)
	}
}

func TestUsersRIsTypedInTheSearchAndIgnoredWhileLoading(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	s, env := usersOpen(t, client, "")
	s, _ = usersPress(t, s, env, testKeyRunes("/"), testKeyRunes("r"))
	if s.query != "r" || client.listUsersCalls != 1 {
		t.Fatalf("r in the search: query=%q reads=%d", s.query, client.listUsersCalls)
	}
	// Not loaded yet: no command.
	fresh := newUsersScreen("")
	if _, cmd, _ := fresh.Update(env, testKeyRunes("r")); cmd != nil {
		t.Fatal("r before the first load finished started another load")
	}
	// A refresh in flight: a second r does nothing.
	loaded := usersAt(client, "")
	next, cmd, _ := loaded.Update(env, testKeyRunes("r"))
	if cmd == nil {
		t.Fatal("r did not reload")
	}
	if _, again, _ := next.(usersScreen).Update(env, testKeyRunes("r")); again != nil {
		t.Fatal("r while a refresh is in flight started another")
	}
	if !strings.Contains(renderUsers(loaded, env), "r") || len(usersKeys.short) != 11 {
		t.Fatal("the hints lost r refresh")
	}
}

func TestEditUserRReloadsAndIsIgnoredWhileLoading(t *testing.T) {
	t.Parallel()
	client := editTestClient()
	s, env := editOpen(t, client, "u-4", "carol")
	users := client.listUsersCalls
	s, _ = editPress(t, s, env, testKeyRunes("r"))
	if client.listUsersCalls != users+1 || client.listGrantsCalls != 2 || client.listTokensCalls != 2 || !s.found {
		t.Fatalf("reads users=%d grants=%d tokens=%d found=%v", client.listUsersCalls, client.listGrantsCalls, client.listTokensCalls, s.found)
	}
	next, cmd, _ := s.Update(env, testKeyRunes("r"))
	if cmd == nil {
		t.Fatal("r did not reload")
	}
	if _, again, _ := next.(editUserScreen).Update(env, testKeyRunes("r")); again != nil {
		t.Fatal("r while loading started another load")
	}
	if _, cmd, _ := newEditUserScreen("u-4", "carol").Update(env, testKeyRunes("r")); cmd != nil {
		t.Fatal("r before the first load finished started another")
	}
	found := false
	for _, k := range s.Keys().short {
		found = found || k.Help().Key == "r"
	}
	if !found {
		t.Fatal("the profile footer lacks r refresh")
	}
}

// ---- passwords never outlive their form ----

func TestEscFromTheFormsLeavesNoPasswordInAnySlot(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, editTestClient(), 150, 24)
	m = modelPress(t, m, testKeyRunes("n"), testKeyRunes("grace"), testKeyTab(), testKeyRunes("typed-secret-1"))
	m = modelPress(t, m, testKeyEsc())
	assertNoPasswordInSlots(t, m, "typed-secret-1")
	if m.screen != screenAdminUsers {
		t.Fatalf("Esc went to %q", m.screen)
	}
	m = modelPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyRunes("p"), testKeyRunes("typed-secret-2"), testKeyTab(), testKeyRunes("typed-secret-2"))
	m = modelPress(t, m, testKeyEsc())
	assertNoPasswordInSlots(t, m, "typed-secret-2")
	if m.screen != screenAdminEditUser || m.adminScreens[slotChangePassword] != nil {
		t.Fatalf("screen=%q reset slot=%T", m.screen, m.adminScreens[slotChangePassword])
	}
}

func TestOpeningAnotherScreenDropsTheFormsAndTheirPasswords(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, editTestClient(), 150, 24)
	m = modelPress(t, m, testKeyRunes("n"), testKeyTab(), testKeyRunes("typed-secret-3"))
	updated, _ := m.Update(navigateMsg{To: screenAdminOverview})
	m = updated.(Model)
	assertNoPasswordInSlots(t, m, "typed-secret-3")
	if m.adminScreens[slotCreateUser] != nil {
		t.Fatal("the New user form is still mounted after navigating away")
	}
	m = usersModelAt(t, editTestClient(), 150, 24)
	m = modelPress(t, m, testKeyRunes("p"), testKeyRunes("typed-secret-4"))
	updated, _ = m.Update(openUserScreenMsg{to: screenAdminUsers})
	assertNoPasswordInSlots(t, updated.(Model), "typed-secret-4")
	updated, _ = m.Update(openUserScreenMsg{to: screenAdminCreateUser})
	if resetPasswordOf(updated.(Model)).password != "" || updated.(Model).adminScreens[slotChangePassword] != nil {
		t.Fatal("the reset form survived another form opening")
	}
	// The next open is a fresh form.
	m = modelPress(t, usersModelAt(t, editTestClient(), 150, 24), testKeyRunes("p"))
	if resetPasswordOf(m).password != "" {
		t.Fatal("a new reset form is not empty")
	}
}

func TestEscReturnsAClearedFormValue(t *testing.T) {
	t.Parallel()
	env := createEnv(usersTestClient())
	create := newCreateUserScreen()
	create.username, create.password = "grace", "typed-secret"
	next, _, _ := create.Update(env, testKeyEsc())
	if c := next.(createUserScreen); c.username != "" || c.password != "" {
		t.Fatalf("Esc kept %q / %q", c.username, c.password)
	}
	reset := resetFilled("typed-secret", "typed-secret")
	next, _, _ = reset.Update(env, testKeyEsc())
	if r := next.(resetPasswordScreen); r.password != "" || r.confirm != "" {
		t.Fatalf("Esc kept %q / %q", r.password, r.confirm)
	}
}

// assertNoPasswordInSlots fails when the secret appears anywhere in the
// screens the model holds, or in its status.
func assertNoPasswordInSlots(t *testing.T, m Model, secret string) {
	t.Helper()
	if strings.Contains(m.status, secret) {
		t.Fatalf("status holds the password: %q", m.status)
	}
	for slot, s := range m.adminScreens {
		if s != nil && strings.Contains(fmt.Sprintf("%+v", s), secret) {
			t.Fatalf("slot %d (%T) still holds the password", slot, s)
		}
	}
}

// ---- the operator cannot disable their own account ----

func ownAccountClient() *fakeAdminClient {
	client := usersTestClient()
	client.users = append(client.users, ports.AdminUser{ID: "u-op", Username: "operator", IsAdmin: true, Enabled: true})
	return client
}

func TestUsersCannotDisableTheOperatorsOwnAccount(t *testing.T) {
	t.Parallel()
	client := ownAccountClient()
	s, env := usersOpen(t, client, "u-op")
	s, _ = usersPress(t, s, env, testKeyRunes("d"))
	if s.confirm.Active() || s.notice != "You cannot disable your own account" || s.noticeTone != userNoticeError {
		t.Fatalf("confirm=%v notice=%q", s.confirm.Active(), s.notice)
	}
	if out := renderUsers(s, env); !strings.Contains(out, "You cannot disable your own account") {
		t.Fatalf("the reason is not on screen:\n%s", out)
	}
	if client.disableCalls != 0 {
		t.Fatal("DisableUser was called")
	}
	// Another user is unaffected, and the name match ignores case.
	other, _ := usersOpen(t, client, "u-2")
	other, _ = usersPress(t, other, env, testKeyRunes("d"))
	if !other.confirm.Active() {
		t.Fatal("d on another user did not open the confirm")
	}
	env.Session.Username = "OPERATOR"
	s, _ = usersPress(t, usersAt(client, "u-op"), env, testKeyRunes("d"))
	if s.confirm.Active() {
		t.Fatal("the guard is case sensitive")
	}
}

func TestUsersEnablingTheOperatorsOwnAccountIsNotBlocked(t *testing.T) {
	t.Parallel()
	client := ownAccountClient()
	client.users[len(client.users)-1].Enabled = false
	s, env := usersOpen(t, client, "u-op")
	s, _ = usersPress(t, s, env, testKeyRunes("d"))
	if !s.confirm.Active() {
		t.Fatal("enabling the operator's own (disabled) account was blocked")
	}
}

func TestProfileCannotDisableTheOperatorsOwnAccount(t *testing.T) {
	t.Parallel()
	client := ownAccountClient()
	for _, key := range []keyMsg{testKeyRunes("d"), testKeySpace()} {
		s, env := editOpen(t, client, "u-op", "operator")
		s, _ = editPress(t, s, env, key)
		if s.confirm.Active() || s.notice != "You cannot disable your own account" {
			t.Fatalf("key %q: confirm=%v notice=%q", keyText(key), s.confirm.Active(), s.notice)
		}
	}
	if client.disableCalls != 0 {
		t.Fatal("DisableUser was called")
	}
}

// ---- seeded fuzz: every action hits the highlighted user ----

// targetRecorder records the user ids the screens act on.
type targetRecorder struct {
	*fakeAdminClient
	disabled, enabled, reset []string
}

func (c *targetRecorder) DisableUser(ctx context.Context, s AdminSession, id string) (ports.AdminUser, error) {
	c.disabled = append(c.disabled, id)
	return c.fakeAdminClient.DisableUser(ctx, s, id)
}

func (c *targetRecorder) EnableUser(ctx context.Context, s AdminSession, id string) (ports.AdminUser, error) {
	c.enabled = append(c.enabled, id)
	return c.fakeAdminClient.EnableUser(ctx, s, id)
}

func (c *targetRecorder) ResetPassword(ctx context.Context, s AdminSession, in ports.AdminResetPasswordInput) error {
	c.reset = append(c.reset, in.UserID)
	return c.fakeAdminClient.ResetPassword(ctx, s, in)
}

var fuzzUsersKeys = func() []keyMsg {
	keys := []keyMsg{testKeyDown(), testKeyUp(), testKeyEnter(), testKeyEsc(), testKeyBackspace(), testKeyTab(), testKeySpace()}
	for _, r := range "jkgtpdnr/yxqaoz" {
		keys = append(keys, testKeyRunes(string(r)))
	}
	return keys
}()

func TestFuzzUsersListActionsAlwaysTargetTheHighlightedUser(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(20261121))
	for seq := range 300 {
		rec := &targetRecorder{fakeAdminClient: ownAccountClient()}
		env := usersTestEnv(rec, 150, 24)
		ids := []string{"", "u-1", "u-2", "u-3", "u-4", "u-5", "u-op"}
		s := usersAt(rec, ids[rng.Intn(len(ids))])
		var wantDisable, wantEnable []string
		var promptFor string // the user the open confirm was raised for
		for step := range 12 + rng.Intn(20) {
			key := fuzzUsersKeys[rng.Intn(len(fuzzUsersKeys))]
			if rng.Intn(3) == 0 {
				key = []keyMsg{testKeyRunes("d"), testKeyEnter(), testKeyDown(), testKeyRunes("j")}[rng.Intn(4)]
			}
			before, hadUser := s.current()
			hadConfirm := s.confirm.Active()
			wasSearching := s.searching
			next, cmd, _ := s.Update(env, key)
			s = next.(usersScreen)
			if !hadConfirm && s.confirm.Active() {
				promptFor = before.ID
				if before.Username == "operator" && before.Enabled {
					t.Fatalf("seq %d step %d: the confirm opened for the operator's own account", seq, step)
				}
			}
			for cmd != nil {
				msg := cmd()
				switch typed := msg.(type) {
				case openUserScreenMsg:
					if typed.to != screenAdminCreateUser && (!hadUser || typed.userID != before.ID || typed.username != before.Username) {
						t.Fatalf("seq %d step %d: open %+v does not target the highlighted %q", seq, step, typed, before.ID)
					}
					if wasSearching {
						t.Fatalf("seq %d step %d: an action fired inside the search", seq, step)
					}
				case openGrantAccessMsg:
					if !hadUser || typed.preselectID != before.ID || typed.returnTo != screenAdminUsers {
						t.Fatalf("seq %d step %d: grant %+v does not target %q", seq, step, typed, before.ID)
					}
				case navigateMsg:
				case userEnabledMsg:
					if typed.enabled {
						wantEnable = append(wantEnable, typed.userID)
					} else {
						wantDisable = append(wantDisable, typed.userID)
					}
					if typed.userID != promptFor {
						t.Fatalf("seq %d step %d: toggle for %q but the prompt was raised for %q", seq, step, typed.userID, promptFor)
					}
				}
				if _, ok := msg.(userScreenMsg); !ok {
					break
				}
				next, cmd, _ = s.Update(env, msg)
				s = next.(usersScreen)
			}
		}
		for _, id := range rec.disabled {
			if id == "u-op" {
				t.Fatalf("seq %d: DisableUser was called for the operator's own account", seq)
			}
		}
		if len(rec.disabled) != len(wantDisable) || len(rec.enabled) != len(wantEnable) {
			t.Fatalf("seq %d: calls disable=%v enable=%v, want %v / %v", seq, rec.disabled, rec.enabled, wantDisable, wantEnable)
		}
	}
}

func TestFuzzProfileActionsAlwaysTargetTheOpenUser(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(7))
	for seq := range 300 {
		rec := &targetRecorder{fakeAdminClient: ownAccountClient()}
		env := usersTestEnv(rec, 150, 24)
		id, name := "u-4", "carol"
		if seq%3 == 0 {
			id, name = "u-op", "operator"
		}
		s, _ := editOpen(t, rec, id, name)
		for step := range 10 + rng.Intn(15) {
			key := fuzzUsersKeys[rng.Intn(len(fuzzUsersKeys))]
			next, cmd, _ := s.Update(env, key)
			s = next.(editUserScreen)
			for cmd != nil {
				msg := cmd()
				switch typed := msg.(type) {
				case openUserScreenMsg:
					if typed.to != screenAdminUsers && typed.userID != id {
						t.Fatalf("seq %d step %d: open %+v is not for %q", seq, step, typed, id)
					}
				case openGrantAccessMsg:
					if typed.preselectID != id {
						t.Fatalf("seq %d step %d: grant %+v is not for %q", seq, step, typed, id)
					}
				}
				if _, ok := msg.(userScreenMsg); !ok {
					break
				}
				next, cmd, _ = s.Update(env, msg)
				s = next.(editUserScreen)
			}
		}
		for _, got := range append(append([]string{}, rec.disabled...), rec.enabled...) {
			if got != id {
				t.Fatalf("seq %d: a toggle hit %q, the open profile is %q", seq, got, id)
			}
		}
		if id == "u-op" && len(rec.disabled) != 0 {
			t.Fatalf("seq %d: the operator disabled their own account from the profile", seq)
		}
	}
}
