package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"regixtry/internal/ports"
)

// ---- New user ----

func createEnv(client AdminClient) screenEnv { return usersTestEnv(client, 150, 24) }

func createPress(t *testing.T, s createUserScreen, env screenEnv, keys ...keyMsg) (createUserScreen, []tea.Msg) {
	t.Helper()
	var all []tea.Msg
	for _, k := range keys {
		next, cmd, _ := s.Update(env, k)
		settled, out := settleScreen(next, env, cmd)
		s = settled.(createUserScreen)
		all = append(all, out...)
	}
	return s, all
}

func renderCreate(s createUserScreen, env screenEnv) string {
	return ansi.Strip(s.View(newAdminTheme(), env).Body)
}

func TestCreateUserTypesEveryLetterIncludingJKAndQ(t *testing.T) {
	t.Parallel()
	env := createEnv(usersTestClient())
	s, out := createPress(t, newCreateUserScreen(), env, typeKeys("jkqQ")...)
	if s.username != "jkqQ" || len(out) != 0 {
		t.Fatalf("username = %q out=%v", s.username, out)
	}
	s, _ = createPress(t, s, env, testKeyTab())
	s, _ = createPress(t, s, env, typeKeys("pjkq")...)
	if s.password != "pjkq" || s.username != "jkqQ" {
		t.Fatalf("password = %q username = %q", s.password, s.username)
	}
}

func TestCreateUserBackspacePasteAndTabCycle(t *testing.T) {
	t.Parallel()
	env := createEnv(usersTestClient())
	s, _ := createPress(t, newCreateUserScreen(), env, keyMsg{Text: "alice\n"})
	if s.username != "alice" {
		t.Fatalf("pasted username = %q, want the newline dropped", s.username)
	}
	s, _ = createPress(t, s, env, testKeyBackspace(), testKeyBackspace())
	if s.username != "ali" {
		t.Fatalf("after backspaces = %q", s.username)
	}
	want := []createUserField{createUserFieldPassword, createUserFieldRole, createUserFieldEnabled, createUserFieldUsername}
	for _, field := range want {
		s, _ = createPress(t, s, env, testKeyTab())
		if s.focus != field {
			t.Fatalf("tab went to %v, want %v", s.focus, field)
		}
	}
	s, _ = createPress(t, s, env, testKeyShiftTab())
	if s.focus != createUserFieldEnabled {
		t.Fatalf("shift+tab went to %v, want Enabled", s.focus)
	}
}

func TestCreateUserRoleAndEnabledToggles(t *testing.T) {
	t.Parallel()
	env := createEnv(usersTestClient())
	s := newCreateUserScreen()
	if s.admin || s.readOnly || !s.enabled || createUserRoles[s.roleCursor] != "user" {
		t.Fatalf("defaults: admin=%v readOnly=%v enabled=%v cursor=%d, want a plain enabled user", s.admin, s.readOnly, s.enabled, s.roleCursor)
	}
	s.focus = createUserFieldRole
	s, _ = createPress(t, s, env, testKeyLeft(), testKeySpace()) // read-only
	s, _ = createPress(t, s, env, testKeyLeft(), testKeySpace()) // admin, independent of read-only
	if !s.admin || !s.readOnly {
		t.Fatalf("admin=%v readOnly=%v, want both flags set independently", s.admin, s.readOnly)
	}
	if got := renderCreate(s, env); strings.Count(got, "admin") < 1 {
		t.Fatalf("role buttons missing:\n%s", got)
	}
	s, _ = createPress(t, s, env, testKeyRight(), testKeyRight(), testKeySpace()) // "user" clears both
	if s.admin || s.readOnly {
		t.Fatalf("the user button did not clear the flags: admin=%v readOnly=%v", s.admin, s.readOnly)
	}
	s, _ = createPress(t, s, env, testKeyRight(), testKeyRight(), testKeyRight())
	if s.roleCursor != 2 {
		t.Fatalf("the cursor ran past the last button: %d", s.roleCursor)
	}
	s.focus = createUserFieldEnabled
	s, _ = createPress(t, s, env, testKeySpace())
	if s.enabled {
		t.Fatal("space on Enabled did not turn it off")
	}
	// Space on the username row types a space; on Role/Enabled it never types.
	s.focus = createUserFieldRole
	s, _ = createPress(t, s, env, testKeyRunes("x"))
	if s.username != "" || s.password != "" {
		t.Fatalf("a letter on the role row was typed: %q %q", s.username, s.password)
	}
}

func TestCreateUserValidatesOnTheClientWithoutCallingTheServer(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	env := createEnv(client)
	tests := []struct {
		name      string
		username  string
		password  string
		wantError string
		wantFocus createUserField
	}{
		{"no username", "", "longenough", "username is required", createUserFieldUsername},
		{"no password", "carol", "", "password is required", createUserFieldPassword},
		{"short password", "carol", "short", "password must be at least 8 characters", createUserFieldPassword},
		{"spaces do not count", "carol", "  abc   ", "password must be at least 8 characters", createUserFieldPassword},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newCreateUserScreen()
			s.username, s.password = tt.username, tt.password
			s, _ = createPress(t, s, env, testKeyEnter())
			if s.err != tt.wantError || s.focus != tt.wantFocus || s.creating {
				t.Fatalf("err=%q focus=%v creating=%v", s.err, s.focus, s.creating)
			}
			if client.createUserCalls != 0 {
				t.Fatalf("CreateUser was called %d times", client.createUserCalls)
			}
			if out := renderCreate(s, env); !strings.Contains(out, tt.wantError) {
				t.Fatalf("the error is not on screen:\n%s", out)
			}
		})
	}
}

func TestCreateUserSendsTheFormOnceAndShowsTheServerError(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	client.createUserErr = errors.New("user already exists")
	env := createEnv(client)
	s := newCreateUserScreen()
	s.username, s.password, s.admin, s.enabled = " carol ", "hunter2hunter", true, false

	next, cmd, _ := s.Update(env, testKeyEnter())
	s = next.(createUserScreen)
	if !s.creating {
		t.Fatal("Enter did not start the create")
	}
	// Enter again, and Enter from another field, while it is in flight.
	for _, focus := range []createUserField{createUserFieldPassword, createUserFieldRole} {
		s.focus = focus
		if _, again, _ := s.Update(env, testKeyEnter()); again != nil {
			t.Fatalf("Enter on %v while in flight dispatched again", focus)
		}
	}
	// Esc still works in flight.
	if _, out := createPress(t, s, env, testKeyEsc()); len(out) != 1 {
		t.Fatalf("Esc in flight made %d requests", len(out))
	}
	settled, _ := settleScreen(s, env, cmd)
	s = settled.(createUserScreen)
	if client.createUserCalls != 1 {
		t.Fatalf("CreateUser called %d times, want 1", client.createUserCalls)
	}
	if got := client.lastCreateUserInput; got.Username != "carol" || got.Password != "hunter2hunter" || !got.IsAdmin || got.IsReadOnly || got.Enabled {
		t.Fatalf("input = %+v", got)
	}
	if s.creating || s.err != "user already exists" {
		t.Fatalf("creating=%v err=%q, want the error shown and the form open again", s.creating, s.err)
	}
	view := renderCreate(s, env)
	if !strings.Contains(view, "user already exists") || strings.Contains(view, "hunter2hunter") {
		t.Fatalf("view:\n%s", view)
	}
	// Typing clears the error, and Enter works again.
	s.focus = createUserFieldPassword
	s, _ = createPress(t, s, env, testKeyRunes("2"))
	if s.err != "" {
		t.Fatalf("typing left the error %q", s.err)
	}
}

func TestCreateUserSuccessLeavesTheFormAndTheUserListIsToldWhoWasCreated(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	client.createUser = ports.AdminUser{ID: "u-9", Username: "grace", Enabled: true}
	env := createEnv(client)
	s := newCreateUserScreen()
	s.username, s.password = "grace", "hunter2hunter"
	s, out := createPress(t, s, env, testKeyEnter())
	if s.password != "" {
		t.Fatalf("the password stayed in the screen after success: %q", s.password)
	}
	if len(out) != 1 {
		t.Fatalf("requests = %v", out)
	}
	open := out[0].(openUserScreenMsg)
	if open.to != screenAdminUsers || open.userID != "u-9" || open.notice != `User "grace" created.` || open.noticeTone != userNoticeSuccess {
		t.Fatalf("open = %+v", open)
	}
	if strings.Contains(open.notice, "hunter2hunter") {
		t.Fatal("the notice contains the password")
	}
}

func TestCreateUserEscCancelsAndForeignResultsAreDropped(t *testing.T) {
	t.Parallel()
	env := createEnv(usersTestClient())
	s := newCreateUserScreen()
	_, out := createPress(t, s, env, testKeyEsc())
	if len(out) != 1 || out[0].(openUserScreenMsg).to != screenAdminUsers {
		t.Fatalf("Esc requests = %v", out)
	}
	s.creating = true
	next, cmd, _ := s.Update(env, userCreatedMsg{gen: s.gen + 1, err: errors.New("other")})
	if !next.(createUserScreen).creating || cmd != nil || next.(createUserScreen).err != "" {
		t.Fatal("a foreign result changed the form")
	}
}

func TestCreateUserExpiredSessionIsReportedForRelogin(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	client.createUserErr = NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	env := createEnv(client)
	s := newCreateUserScreen()
	s.username, s.password = "grace", "hunter2hunter"
	_, cmd, _ := s.Update(env, testKeyEnter())
	msg, ok := cmd().(userScreenMsg)
	if !ok {
		t.Fatal("the create result is not a userScreenMsg")
	}
	if reason, expired := msg.expiredReason(); !expired || reason != AdminSessionExpiredReasonExpired {
		t.Fatalf("expiredReason = %q, %v", reason, expired)
	}
}

func TestCreateUserPasswordIsNeverRenderedInClear(t *testing.T) {
	t.Parallel()
	env := createEnv(usersTestClient())
	s := newCreateUserScreen()
	s.focus = createUserFieldPassword
	s, _ = createPress(t, s, env, keyMsg{Text: "s3cr3t-Pa55"})
	view := renderCreate(s, env)
	for _, leak := range []string{"s3cr3t", "Pa55", "s3c"} {
		if strings.Contains(view, leak) {
			t.Fatalf("view leaks %q:\n%s", leak, view)
		}
	}
	if !strings.Contains(view, strings.Repeat("•", 11)) || !strings.Contains(view, "✓ 11 characters") {
		t.Fatalf("no mask or count:\n%s", view)
	}
}

func TestCreateUserViewHasTheBoardsParts(t *testing.T) {
	t.Parallel()
	env := createEnv(usersTestClient())
	out := renderCreate(newCreateUserScreen(), env)
	for _, want := range []string{"Admin › Access › Users › New user", "The user signs in with this username and password. Grants come after.",
		"Username", "Password", "Minimum 8 characters.", "Role", "admin", "read-only", "user", "[x] Enabled", "space toggles", "Create user  enter", "Cancel  esc",
		"RULES", "Password: at least 8 characters", "never shown again", "ROLES", "manages users, robots", "pulls and pushes", "read-only pulls where granted",
		"tab", "next field", "role", "toggle", "create", "cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	for _, banned := range []string{"is available", "Sample data", "3 to 32"} {
		if strings.Contains(out, banned) {
			t.Errorf("view shows %q, which nothing backs", banned)
		}
	}
}

func TestCreateUserViewFitsEverySizeInEveryState(t *testing.T) {
	t.Parallel()
	cases := map[string]func() createUserScreen{
		"empty": newCreateUserScreen,
		"long": func() createUserScreen {
			s := newCreateUserScreen()
			s.username, s.password = strings.Repeat("ünï-", 40), strings.Repeat("pässwörd", 30)
			s.err = strings.Repeat("a very long server error ", 10)
			return s
		},
		"creating": func() createUserScreen { s := newCreateUserScreen(); s.creating = true; return s },
		"toggles": func() createUserScreen {
			s := newCreateUserScreen()
			s.admin, s.readOnly, s.enabled = true, true, false
			s.focus = createUserFieldRole
			return s
		},
	}
	for name, build := range cases {
		for _, size := range geometrySizes {
			t.Run(fmt.Sprintf("%s_%dx%d", name, size.w, size.h), func(t *testing.T) {
				t.Parallel()
				assertFitsTerminal(t, renderCreate(build(), usersTestEnv(usersTestClient(), size.w, size.h)), size.w, size.h)
			})
		}
	}
}

func TestCreateUserViewGoldens(t *testing.T) {
	cases := []struct {
		name string
		make func() createUserScreen
	}{
		{"create_user_empty", newCreateUserScreen},
		{"create_user_filled", func() createUserScreen {
			s := newCreateUserScreen()
			s.username, s.password, s.focus = "grace", "hunter2hunter", createUserFieldPassword
			s.readOnly, s.roleCursor = true, 1
			return s
		}},
		{"create_user_short_password", func() createUserScreen {
			s := newCreateUserScreen()
			s.username, s.password, s.focus, s.err = "grace", "hunter", createUserFieldPassword, userPasswordTooShort
			return s
		}},
	}
	for _, tc := range cases {
		for _, size := range usersViewSizes {
			name := fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h)
			t.Run(name, func(t *testing.T) {
				assertUsersGolden(t, name, renderCreate(tc.make(), usersTestEnv(usersTestClient(), size.w, size.h)))
			})
		}
	}
}

// ---- Reset password ----

func resetPress(t *testing.T, s resetPasswordScreen, env screenEnv, keys ...keyMsg) (resetPasswordScreen, []tea.Msg) {
	t.Helper()
	var all []tea.Msg
	for _, k := range keys {
		next, cmd, _ := s.Update(env, k)
		settled, out := settleScreen(next, env, cmd)
		s = settled.(resetPasswordScreen)
		all = append(all, out...)
	}
	return s, all
}

func renderReset(s resetPasswordScreen, env screenEnv) string {
	return ansi.Strip(s.View(newAdminTheme(), env).Body)
}

func resetFilled(password, confirm string) resetPasswordScreen {
	s := newResetPasswordScreen("u-4", "carol")
	s.password, s.confirm = password, confirm
	return s
}

func TestResetPasswordTypesEveryCharacterIncludingJKQAndSpace(t *testing.T) {
	t.Parallel()
	env := createEnv(usersTestClient())
	s, out := resetPress(t, newResetPasswordScreen("u-4", "carol"), env, typeKeys("jkq")...)
	s, _ = resetPress(t, s, env, testKeySpace(), testKeyRunes("Q"))
	if s.password != "jkq Q" || len(out) != 0 {
		t.Fatalf("password = %q out=%v", s.password, out)
	}
	s, _ = resetPress(t, s, env, testKeyTab())
	s, _ = resetPress(t, s, env, typeKeys("jkq")...)
	if s.confirm != "jkq" || s.password != "jkq Q" {
		t.Fatalf("confirm = %q password = %q", s.confirm, s.password)
	}
	s, _ = resetPress(t, s, env, testKeyBackspace(), keyMsg{Text: "x\n"})
	if s.confirm != "jkx" {
		t.Fatalf("confirm after backspace and paste = %q", s.confirm)
	}
	s, _ = resetPress(t, s, env, testKeyTab())
	if s.focus != resetPasswordFieldNew {
		t.Fatalf("tab did not return to the first field: %v", s.focus)
	}
}

func TestResetPasswordMismatchIsShownOnTheConfirmationFieldAndBlocksSaving(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	env := createEnv(client)
	s := resetFilled("correct-horse-battery", "correct-horse-batter")
	if out := renderReset(s, env); !strings.Contains(out, userPasswordMismatch) || !strings.Contains(out, "✓ 21 characters") {
		t.Fatalf("live mismatch or count missing:\n%s", out)
	}
	s, out := resetPress(t, s, env, testKeyEnter())
	if client.resetPasswordCalls != 0 || len(out) != 0 || s.saving {
		t.Fatalf("a mismatch was sent: calls=%d out=%v saving=%v", client.resetPasswordCalls, out, s.saving)
	}
	if s.confirmErr != userPasswordMismatch || s.focus != resetPasswordFieldConfirm {
		t.Fatalf("confirmErr=%q focus=%v, want the error on the confirmation field", s.confirmErr, s.focus)
	}
	view := renderReset(s, env)
	lines := strings.Split(view, "\n")
	confirmAt, errAt := -1, -1
	for i, line := range lines {
		if strings.Contains(line, "Confirm new password") {
			confirmAt = i
		}
		if strings.Contains(line, userPasswordMismatch) && confirmAt >= 0 {
			errAt = i
			break
		}
	}
	if confirmAt < 0 || errAt < confirmAt || errAt-confirmAt > 3 {
		t.Fatalf("the mismatch error is not under the confirmation field (field row %d, error row %d):\n%s", confirmAt, errAt, view)
	}
	if !strings.Contains(view, "· Both fields match") && !strings.Contains(view, "Both fields match") {
		t.Fatalf("no checks list:\n%s", view)
	}
}

func TestResetPasswordShortPasswordUsesTheServersMessageAndDoesNotCall(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	env := createEnv(client)
	for _, short := range []string{"", "short", "   abc   "} {
		s, _ := resetPress(t, resetFilled(short, short), env, testKeyEnter())
		if s.err != "password must be at least 8 characters" || s.focus != resetPasswordFieldNew || client.resetPasswordCalls != 0 {
			t.Fatalf("password %q: err=%q focus=%v calls=%d", short, s.err, s.focus, client.resetPasswordCalls)
		}
		if !strings.Contains(renderReset(s, env), "password must be at least 8 characters") {
			t.Fatalf("the message is not on screen for %q", short)
		}
	}
}

func TestResetPasswordSavesOnceAndGoesBackToTheProfile(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	env := createEnv(client)
	s := resetFilled("correct-horse-battery", "correct-horse-battery")
	next, cmd, _ := s.Update(env, testKeyEnter())
	s = next.(resetPasswordScreen)
	if !s.saving {
		t.Fatal("Enter did not start the save")
	}
	if _, again, _ := s.Update(env, testKeyEnter()); again != nil {
		t.Fatal("Enter in flight dispatched again")
	}
	if _, out := resetPress(t, s, env, testKeyEsc()); len(out) != 1 {
		t.Fatal("Esc does not work while saving")
	}
	settled, out := settleScreen(s, env, cmd)
	s = settled.(resetPasswordScreen)
	if client.resetPasswordCalls != 1 || client.lastResetPasswordInput.UserID != "u-4" || client.lastResetPasswordInput.NewPassword != "correct-horse-battery" {
		t.Fatalf("calls=%d input=%+v", client.resetPasswordCalls, client.lastResetPasswordInput)
	}
	if s.password != "" || s.confirm != "" {
		t.Fatalf("the passwords stayed in the screen after success: %q %q", s.password, s.confirm)
	}
	if len(out) != 1 {
		t.Fatalf("requests = %v", out)
	}
	open := out[0].(openUserScreenMsg)
	if open.to != screenAdminEditUser || open.userID != "u-4" || open.username != "carol" || open.notice != `Password reset for "carol".` || open.noticeTone != userNoticeSuccess {
		t.Fatalf("open = %+v", open)
	}
}

func TestResetPasswordServerErrorKeepsTheFormAndNeverEchoesThePassword(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	client.resetPasswordErr = errors.New("password is too common")
	env := createEnv(client)
	s, out := resetPress(t, resetFilled("correct-horse-battery", "correct-horse-battery"), env, testKeyEnter())
	if len(out) != 0 || s.saving || s.err != "password is too common" {
		t.Fatalf("out=%v saving=%v err=%q", out, s.saving, s.err)
	}
	view := renderReset(s, env)
	if !strings.Contains(view, "password is too common") || strings.Contains(view, "correct-horse") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestResetPasswordEscCancelsToTheProfile(t *testing.T) {
	t.Parallel()
	_, out := resetPress(t, resetFilled("abc", "abc"), createEnv(usersTestClient()), testKeyEsc())
	if len(out) != 1 {
		t.Fatalf("requests = %v", out)
	}
	if open := out[0].(openUserScreenMsg); open.to != screenAdminEditUser || open.userID != "u-4" {
		t.Fatalf("open = %+v", open)
	}
}

func TestResetPasswordForeignResultAndExpiry(t *testing.T) {
	t.Parallel()
	env := createEnv(usersTestClient())
	s := resetFilled("correct-horse-battery", "correct-horse-battery")
	s.saving = true
	next, cmd, _ := s.Update(env, userPasswordResetMsg{gen: s.gen + 1, err: errors.New("other")})
	if !next.(resetPasswordScreen).saving || cmd != nil {
		t.Fatal("a foreign result changed the form")
	}
	client := usersTestClient()
	client.resetPasswordErr = NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	_, cmd, _ = resetFilled("correct-horse-battery", "correct-horse-battery").Update(createEnv(client), testKeyEnter())
	msg, ok := cmd().(userScreenMsg)
	if !ok {
		t.Fatal("the reset result is not a userScreenMsg")
	}
	if reason, expired := msg.expiredReason(); !expired || reason != AdminSessionExpiredReasonExpired {
		t.Fatalf("expiredReason = %q, %v", reason, expired)
	}
}

func TestResetPasswordNeverRendersEitherPasswordInClear(t *testing.T) {
	t.Parallel()
	env := createEnv(usersTestClient())
	s, _ := resetPress(t, newResetPasswordScreen("u-4", "carol"), env, keyMsg{Text: "s3cr3t-Pa55"})
	s, _ = resetPress(t, s, env, testKeyTab())
	s, _ = resetPress(t, s, env, keyMsg{Text: "s3cr3t-Pa5"})
	view := renderReset(s, env)
	for _, leak := range []string{"s3cr3t", "Pa55", "Pa5"} {
		if strings.Contains(view, leak) {
			t.Fatalf("view leaks %q:\n%s", leak, view)
		}
	}
}

func TestResetPasswordViewHasTheBoardsParts(t *testing.T) {
	t.Parallel()
	env := createEnv(usersTestClient())
	out := renderReset(resetFilled("correct-horse-battery", "correct-horse-batter"), env)
	for _, want := range []string{"Admin › Access › Users › carol › Reset password", "Reset password for carol", "Enter saves the new password. Esc cancels.",
		"New password", "Confirm new password", "Tell carol the new password over a private channel", "shown again.", "Set password  enter", "Cancel  esc",
		"RULE", "At least 8 characters.", "Both fields are masked while you type.", "Pasting is allowed.", "CHECKS", "8 characters or more", "Both fields match",
		"tab", "next field", "set password", "cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
}

func TestResetPasswordViewFitsEverySizeInEveryState(t *testing.T) {
	t.Parallel()
	cases := map[string]func() resetPasswordScreen{
		"empty":    func() resetPasswordScreen { return newResetPasswordScreen("u-4", "carol") },
		"mismatch": func() resetPasswordScreen { return resetFilled("correct-horse-battery", "correct-horse-batter") },
		"long": func() resetPasswordScreen {
			s := newResetPasswordScreen("u-4", strings.Repeat("ünï-", 30))
			s.password, s.confirm, s.err = strings.Repeat("pässwörd", 30), "x", strings.Repeat("a very long server error ", 10)
			return s
		},
		"saving": func() resetPasswordScreen {
			s := resetFilled("correct-horse-battery", "correct-horse-battery")
			s.saving = true
			return s
		},
	}
	for name, build := range cases {
		for _, size := range geometrySizes {
			t.Run(fmt.Sprintf("%s_%dx%d", name, size.w, size.h), func(t *testing.T) {
				t.Parallel()
				assertFitsTerminal(t, renderReset(build(), usersTestEnv(usersTestClient(), size.w, size.h)), size.w, size.h)
			})
		}
	}
}

func TestResetPasswordViewGoldens(t *testing.T) {
	cases := []struct {
		name string
		make func() resetPasswordScreen
	}{
		{"reset_password_empty", func() resetPasswordScreen { return newResetPasswordScreen("u-4", "carol") }},
		{"reset_password_mismatch", func() resetPasswordScreen {
			s := resetFilled("correct-horse-battery", "correct-horse-batter")
			s.focus = resetPasswordFieldConfirm
			return s
		}},
		{"reset_password_server_error", func() resetPasswordScreen {
			s := resetFilled("correct-horse-battery", "correct-horse-battery")
			s.err = "password is too common"
			return s
		}},
	}
	for _, tc := range cases {
		for _, size := range usersViewSizes {
			name := fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h)
			t.Run(name, func(t *testing.T) {
				assertUsersGolden(t, name, renderReset(tc.make(), usersTestEnv(usersTestClient(), size.w, size.h)))
			})
		}
	}
}
