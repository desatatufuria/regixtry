package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

func editTestClient() *fakeAdminClient {
	client := usersTestClient()
	client.grants = map[string][]ports.AdminRepoGrant{
		"u-4": {
			{UserID: "u-4", Repository: regixtrydomain.MustParseRepositoryRef("team/docs"), Role: domainauth.RepoRoleReader},
			{UserID: "u-4", Repository: regixtrydomain.MustParseRepositoryRef("team/api"), Role: domainauth.RepoRoleWriter},
		},
	}
	client.tokens = map[string][]ports.AdminToken{
		"u-4": {
			{ID: "t-1", UserID: "u-4", Name: "laptop", Accessor: "acc-1", CreatedAt: usersTestNow.Add(-26 * 24 * time.Hour), ExpiresAt: usersTestNow.Add(4 * 24 * time.Hour)},
			{ID: "t-2", UserID: "u-4", Name: "ci-local", Accessor: "acc-2", CreatedAt: usersTestNow.Add(-30 * 24 * time.Hour), ExpiresAt: usersTestNow.Add(61 * 24 * time.Hour)},
			{ID: "t-3", UserID: "u-4", Name: "old-laptop", Accessor: "acc-3", CreatedAt: usersTestNow.Add(-90 * 24 * time.Hour), ExpiresAt: usersTestNow.Add(-24 * time.Hour)},
		},
	}
	return client
}

func editOpen(t *testing.T, client AdminClient, id, name string) (editUserScreen, screenEnv) {
	t.Helper()
	env := usersTestEnv(client, 150, 24)
	s := newEditUserScreen(id, name)
	next, _ := settleScreen(s, env, s.Init(env))
	return next.(editUserScreen), env
}

func editPress(t *testing.T, s editUserScreen, env screenEnv, keys ...keyMsg) (editUserScreen, []tea.Msg) {
	t.Helper()
	var all []tea.Msg
	for _, k := range keys {
		next, cmd, _ := s.Update(env, k)
		settled, out := settleScreen(next, env, cmd)
		s = settled.(editUserScreen)
		all = append(all, out...)
	}
	return s, all
}

func renderEdit(s editUserScreen, env screenEnv) string {
	return ansi.Strip(s.View(newAdminTheme(), env).Body)
}

func TestEditUserLoadsTheUserItsGrantsAndItsTokens(t *testing.T) {
	t.Parallel()
	client := editTestClient()
	s, _ := editOpen(t, client, "u-4", "carol")
	if !s.found || s.user.Username != "carol" || len(s.grants) != 2 || len(s.tokens) != 3 {
		t.Fatalf("found=%v user=%q grants=%d tokens=%d", s.found, s.user.Username, len(s.grants), len(s.tokens))
	}
	if client.listGrantsCalls != 1 || client.listTokensCalls != 1 {
		t.Fatalf("grants=%d tokens=%d calls, want one each", client.listGrantsCalls, client.listTokensCalls)
	}
}

func TestEditUserViewHasTheBoardsPartsWithoutInventedData(t *testing.T) {
	t.Parallel()
	s, env := editOpen(t, editTestClient(), "u-4", "carol")
	out := renderEdit(s, env)
	for _, want := range []string{"Admin › Access › Users › carol", "user, created 3 Mar 2026", "PROFILE", "Username", "Role", "Enabled", "[x] enabled",
		"PASSWORD", "Set a new password for carol", "p reset",
		"PROJECT ACCESS AND REPOSITORY GRANTS", "g grant access", "team/docs", "Reader", "team/api", "Writer", "0 project roles, 2 repository grants.",
		"TOKENS", "t manage", "laptop", "4 days left", "ci-local", "61 days left", "old-laptop", "expired",
		"DANGER AREA", "Disable carol.", "Disable user  d", "space", "enable/disable", "password", "grant access", "tokens", "disable", "back"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	for _, banned := range []string{"Sample data", "last sign-in", "seconds"} {
		if strings.Contains(out, banned) {
			t.Errorf("view shows %q", banned)
		}
	}
}

func TestEditUserPasswordRuleLineAppearsOnTallTerminals(t *testing.T) {
	t.Parallel()
	s, _ := editOpen(t, editTestClient(), "u-4", "carol")
	small := renderEdit(s, usersTestEnv(editTestClient(), 150, 24))
	tall := renderEdit(s, usersTestEnv(editTestClient(), 200, 40))
	if strings.Contains(small, "You type it twice.") || !strings.Contains(tall, "At least 8 characters. You type it twice.") {
		t.Errorf("rule line: small=%v tall=%v, want only on the tall terminal", strings.Contains(small, "You type it twice."), strings.Contains(tall, "You type it twice."))
	}
}

func TestEditUserKeysOpenTheirScreens(t *testing.T) {
	t.Parallel()
	s, env := editOpen(t, editTestClient(), "u-4", "carol")
	_, out := editPress(t, s, env, testKeyRunes("g"))
	if g := out[0].(openGrantAccessMsg); g.preselectID != "u-4" || g.returnTo != screenAdminEditUser {
		t.Fatalf("g opened %+v", g)
	}
	_, out = editPress(t, s, env, testKeyRunes("t"))
	if o := out[0].(openUserScreenMsg); o.to != screenAdminEditUserTokens || o.userID != "u-4" || o.username != "carol" {
		t.Fatalf("t opened %+v", o)
	}
	_, out = editPress(t, s, env, testKeyRunes("p"))
	if o := out[0].(openUserScreenMsg); o.to != screenAdminChangePassword || o.userID != "u-4" || o.username != "carol" {
		t.Fatalf("p opened %+v", o)
	}
	_, out = editPress(t, s, env, testKeyEsc())
	if o := out[0].(openUserScreenMsg); o.to != screenAdminUsers || o.userID != "u-4" {
		t.Fatalf("esc opened %+v, want the list with carol selected", o)
	}
}

func TestEditUserDisableAndSpaceBothAskBeforeActing(t *testing.T) {
	t.Parallel()
	for _, key := range []keyMsg{testKeyRunes("d"), testKeySpace()} {
		client := editTestClient()
		client.disableUser = ports.AdminUser{ID: "u-4", Username: "carol"}
		s, env := editOpen(t, client, "u-4", "carol")
		s, _ = editPress(t, s, env, key)
		if !s.confirm.Active() || client.disableCalls != 0 {
			t.Fatalf("key %q: confirm=%v calls=%d, want a prompt and no call", keyText(key), s.confirm.Active(), client.disableCalls)
		}
		if got := ansi.Strip(s.View(newAdminTheme(), env).Overlay); !strings.Contains(got, "Disable user?") {
			t.Fatalf("overlay = %q", got)
		}
		// Two Enters before the result: one call.
		next, cmd, _ := s.Update(env, testKeyEnter())
		s = next.(editUserScreen)
		if _, again, _ := s.Update(env, testKeyEnter()); again != nil {
			t.Fatal("a second Enter dispatched again while in flight")
		}
		settled, _ := settleScreen(s, env, cmd)
		s = settled.(editUserScreen)
		if client.disableCalls != 1 || s.confirm.Active() || !strings.Contains(s.notice, `User "carol" disabled.`) {
			t.Fatalf("calls=%d confirm=%v notice=%q", client.disableCalls, s.confirm.Active(), s.notice)
		}
	}
}

func TestEditUserOfADisabledUserOffersEnable(t *testing.T) {
	t.Parallel()
	client := editTestClient()
	s, env := editOpen(t, client, "u-5", "dave")
	out := renderEdit(s, env)
	if !strings.Contains(out, "Enable user  d") || !strings.Contains(out, "[ ] disabled") {
		t.Errorf("a disabled user is not offered Enable:\n%s", out)
	}
	_, _ = editPress(t, s, env, testKeyRunes("d"), testKeyEnter())
	if client.enableCalls != 1 || client.disableCalls != 0 {
		t.Fatalf("enable=%d disable=%d", client.enableCalls, client.disableCalls)
	}
}

func TestEditUserDisableFailureIsShown(t *testing.T) {
	t.Parallel()
	client := editTestClient()
	client.disableErr = errors.New("cannot disable the last admin")
	s, env := editOpen(t, client, "u-1", "admin")
	s, _ = editPress(t, s, env, testKeyRunes("d"), testKeyEnter())
	if s.confirm.Active() || s.noticeTone != userNoticeError || !strings.Contains(renderEdit(s, env), "cannot disable the last admin") {
		t.Fatalf("confirm=%v notice=%q", s.confirm.Active(), s.notice)
	}
}

func TestEditUserUnknownUserAndLoadErrors(t *testing.T) {
	t.Parallel()
	s, env := editOpen(t, editTestClient(), "u-404", "ghost")
	if out := renderEdit(s, env); !strings.Contains(out, "This user was not found.") {
		t.Errorf("no not-found message:\n%s", out)
	}
	// Esc still leaves, and d does nothing without a loaded user.
	s, out := editPress(t, s, env, testKeyRunes("d"))
	if s.confirm.Active() || len(out) != 0 {
		t.Fatalf("d on a missing user: confirm=%v out=%v", s.confirm.Active(), out)
	}
	if _, out = editPress(t, s, env, testKeyEsc()); len(out) != 1 {
		t.Fatalf("esc made %d requests", len(out))
	}

	client := editTestClient()
	client.usersErr = errors.New("users exploded")
	client.grantsErr = errors.New("grants exploded")
	client.tokensErr = errors.New("tokens exploded")
	s, env = editOpen(t, client, "u-4", "carol")
	out2 := renderEdit(s, env)
	for _, want := range []string{"Could not load the user: users exploded"} {
		if !strings.Contains(out2, want) {
			t.Errorf("lacks %q:\n%s", want, out2)
		}
	}
	client.usersErr = nil
	s, env = editOpen(t, client, "u-4", "carol")
	out2 = renderEdit(s, env)
	for _, want := range []string{"Could not load access: grants exploded", "Could not load tokens: tokens exploded"} {
		if !strings.Contains(out2, want) {
			t.Errorf("lacks %q:\n%s", want, out2)
		}
	}
}

func TestEditUserExpiredSessionAndForeignResults(t *testing.T) {
	t.Parallel()
	client := editTestClient()
	client.grantsErr = NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	env := usersTestEnv(client, 150, 24)
	s := newEditUserScreen("u-4", "carol")
	msg, ok := s.Init(env)().(userScreenMsg)
	if !ok {
		t.Fatal("the load result is not a userScreenMsg")
	}
	if reason, expired := msg.expiredReason(); !expired || reason != AdminSessionExpiredReasonExpired {
		t.Fatalf("expiredReason = %q, %v", reason, expired)
	}
	loaded, _ := editOpen(t, editTestClient(), "u-4", "carol")
	next, _, _ := loaded.Update(env, editUserLoadedMsg{gen: loaded.gen + 1, userID: "u-4"})
	if len(next.(editUserScreen).tokens) != 3 {
		t.Fatal("a foreign result replaced the screen's data")
	}
}

func TestEditUserLongListsAndNamesFitEverySize(t *testing.T) {
	t.Parallel()
	client := editTestClient()
	client.users = append(client.users, ports.AdminUser{ID: "u-long", Username: strings.Repeat("ünï-long-", 10), IsAdmin: true, IsReadOnly: true, Enabled: true, CreatedAt: usersTestNow})
	var grants []ports.AdminRepoGrant
	var tokens []ports.AdminToken
	for i := range 40 {
		grants = append(grants, ports.AdminRepoGrant{UserID: "u-long", Repository: regixtrydomain.MustParseRepositoryRef(fmt.Sprintf("team/repo-%02d-with-a-quite-long-name-%s", i, strings.Repeat("x", 30))), Role: domainauth.RepoRoleAdmin})
		tokens = append(tokens, ports.AdminToken{ID: fmt.Sprint(i), Name: strings.Repeat("tok-", 20), CreatedAt: usersTestNow.Add(-time.Hour), ExpiresAt: usersTestNow.Add(time.Duration(i+1) * 24 * time.Hour)})
	}
	client.grants = map[string][]ports.AdminRepoGrant{"u-long": grants}
	client.tokens = map[string][]ports.AdminToken{"u-long": tokens}
	failing := editTestClient()
	failing.usersErr = errors.New(strings.Repeat("long error ", 30))
	empty := editTestClient()
	cases := map[string]func() editUserScreen{
		"full":    func() editUserScreen { s, _ := editOpen(t, client, "u-long", "x"); return s },
		"normal":  func() editUserScreen { s, _ := editOpen(t, editTestClient(), "u-4", "carol"); return s },
		"empty":   func() editUserScreen { s, _ := editOpen(t, empty, "u-2", "alice"); return s },
		"error":   func() editUserScreen { s, _ := editOpen(t, failing, "u-4", "carol"); return s },
		"loading": func() editUserScreen { return newEditUserScreen("u-4", "carol") },
		"confirm": func() editUserScreen {
			s, env := editOpen(t, editTestClient(), "u-4", "carol")
			s, _ = editPress(t, s, env, testKeyRunes("d"))
			return s
		},
		"notice": func() editUserScreen {
			s, _ := editOpen(t, editTestClient(), "u-4", "carol")
			return s.withNotice(userNoticeSuccess, `Password reset for "carol".`)
		},
	}
	for name, build := range cases {
		for _, size := range geometrySizes {
			t.Run(fmt.Sprintf("%s_%dx%d", name, size.w, size.h), func(t *testing.T) {
				t.Parallel()
				env := usersTestEnv(client, size.w, size.h)
				frame := build().View(newAdminTheme(), env)
				assertFitsTerminal(t, ansi.Strip(frame.Body), size.w, size.h)
				if frame.Overlay != "" {
					assertFitsTerminal(t, ansi.Strip(compositeOverlay(frame.Body, frame.Overlay, size.w, size.h)), size.w, size.h)
				}
			})
		}
	}
}

func TestEditUserViewGoldens(t *testing.T) {
	cases := []struct {
		name string
		make func() editUserScreen
	}{
		{"edit_user", func() editUserScreen { s, _ := editOpen(t, editTestClient(), "u-4", "carol"); return s }},
		{"edit_user_disabled", func() editUserScreen { s, _ := editOpen(t, editTestClient(), "u-5", "dave"); return s }},
		{"edit_user_notice", func() editUserScreen {
			s, _ := editOpen(t, editTestClient(), "u-4", "carol")
			return s.withNotice(userNoticeSuccess, `Password reset for "carol".`)
		}},
		{"edit_user_not_found", func() editUserScreen { s, _ := editOpen(t, editTestClient(), "u-404", "ghost"); return s }},
	}
	for _, tc := range cases {
		for _, size := range usersViewSizes {
			name := fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h)
			t.Run(name, func(t *testing.T) {
				env := usersTestEnv(editTestClient(), size.w, size.h)
				assertUsersGolden(t, name, ansi.Strip(tc.make().View(newAdminTheme(), env).Body))
			})
		}
	}
}
