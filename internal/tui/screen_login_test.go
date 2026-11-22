package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The login and confirm screens have their own flag so regenerating their
// goldens can never touch another screen's goldens.
var updateLoginConfirmGoldens = flag.Bool("update-login-confirm", false, "rewrite the login and confirm golden files")

var loginSizes = []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}}

func assertLoginConfirmGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "login_confirm", name+".golden")
	if *updateLoginConfirmGoldens {
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
		t.Fatalf("read golden %s (run with -update-login-confirm): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("view differs from %s (run with -update-login-confirm to inspect)\n--- want\n%s\n--- got\n%s", path, strings.TrimRight(string(want), "\n"), strings.TrimRight(got, "\n"))
	}
}

func loginScreenModel(t *testing.T, w, h int) Model {
	t.Helper()
	m := newAdminReadyModel(t, &fakeAdminClient{})
	m = resizeModel(t, m, w, h)
	m.screen = screenAdminLogin
	m.adminLogin = adminLoginForm{Focus: loginFieldUsername}
	return m
}

// ---- behavior kept from the old screen ----

func TestLoginTypesEveryLetterInBothFieldsAndTabArrowsToggleFocus(t *testing.T) {
	t.Parallel()
	m := typeRunes(t, loginScreenModel(t, 150, 24), "jkq")
	if m.adminLogin.Username != "jkq" || m.screen != screenAdminLogin {
		t.Fatalf("username = %q screen = %q, want jkq typed on the login screen", m.adminLogin.Username, m.screen)
	}
	for _, toggle := range []keyMsg{testKeyTab(), testKeyDown(), testKeyUp()} {
		before := m.adminLogin.Focus
		m = testSendKey(t, m, toggle)
		if m.adminLogin.Focus == before {
			t.Fatalf("%v did not toggle focus", toggle)
		}
	}
	m = typeRunes(t, m, "qjk")
	if m.adminLogin.Password != "qjk" {
		t.Fatalf("password = %q, want qjk", m.adminLogin.Password)
	}
}

func TestLoginShiftTabAndBackspaceAndPaste(t *testing.T) {
	t.Parallel()
	m := loginScreenModel(t, 150, 24)
	m = testSendKey(t, m, testKeyShiftTab())
	if m.adminLogin.Focus != loginFieldUsername {
		t.Fatalf("shift+tab moved focus to %v, want it unchanged as before", m.adminLogin.Focus)
	}
	m = testSendKey(t, m, keyMsg{Code: 'a', Text: "alice smith"})
	if m.adminLogin.Username != "alice smith" {
		t.Fatalf("pasted username = %q", m.adminLogin.Username)
	}
	m = testSendKey(t, m, testKeyBackspace())
	if m.adminLogin.Username != "alice smit" {
		t.Fatalf("after backspace username = %q", m.adminLogin.Username)
	}
}

func TestLoginCtrlCQuitsAndEscGoesBack(t *testing.T) {
	t.Parallel()
	m := loginScreenModel(t, 150, 24)
	_, cmd := m.Update(keyMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c command = %T, want tea.QuitMsg", cmd())
	}
	back := testSendKey(t, m, testKeyEsc())
	if back.screen == screenAdminLogin {
		t.Fatal("esc stayed on the login screen")
	}
}

func TestLoginEnterNeedsBothFieldsAndNeverPutsThePasswordInStatus(t *testing.T) {
	t.Parallel()
	m := typeRunes(t, loginScreenModel(t, 150, 24), "admin")
	m = testSendKey(t, m, testKeyEnter())
	if m.status != "Username and password are required." || m.screen != screenAdminLogin {
		t.Fatalf("status = %q screen = %q", m.status, m.screen)
	}
	m = testSendKey(t, m, testKeyTab())
	m = typeRunes(t, m, "s3cret-pass")
	m = testSendKey(t, m, testKeyEnter())
	if m.screen != screenAdminAuthenticating {
		t.Fatalf("screen = %q, want the authenticating screen", m.screen)
	}
	if strings.Contains(m.status, "s3cret-pass") || strings.Contains(m.loadingText, "s3cret-pass") {
		t.Fatalf("password leaked: status %q loading %q", m.status, m.loadingText)
	}
	if view := m.View().Content; strings.Contains(view, "s3cret-pass") {
		t.Fatalf("password rendered while authenticating:\n%s", view)
	}
}

// ---- the new look ----

func TestLoginViewShowsTheBoardWithoutASidebar(t *testing.T) {
	t.Parallel()
	m := typeRunes(t, loginScreenModel(t, 150, 24), "admin")
	m = testSendKey(t, m, testKeyTab())
	m = typeRunes(t, m, "correct-horse")
	plain := ansi.Strip(m.View().Content)
	for _, want := range []string{"regixtry › Sign in", "not signed in", "operator sign in", "Username", "admin", "Password", "*************", "Sign in", "enter", "Back", "esc", "tab next field", "enter sign in", "esc back"} {
		if !strings.Contains(plain, want) {
			t.Errorf("login view lacks %q:\n%s", want, plain)
		}
	}
	for _, banned := range []string{"correct-horse", "Overview", "ACCESS", "Operator Login", "Enter: sign in"} {
		if strings.Contains(plain, banned) {
			t.Errorf("login view still shows %q:\n%s", banned, plain)
		}
	}
}

func loginStates() []struct {
	name  string
	build func(t *testing.T, w, h int) Model
} {
	return []struct {
		name  string
		build func(t *testing.T, w, h int) Model
	}{
		{"empty", func(t *testing.T, w, h int) Model { return loginScreenModel(t, w, h) }},
		{"typed", func(t *testing.T, w, h int) Model {
			m := typeRunes(t, loginScreenModel(t, w, h), "admin")
			m = testSendKey(t, m, testKeyTab())
			return typeRunes(t, m, "correct-horse")
		}},
		{"long values", func(t *testing.T, w, h int) Model {
			m := loginScreenModel(t, w, h)
			m.adminLogin = adminLoginForm{Username: strings.Repeat("ünï", 60), Password: strings.Repeat("p", 200), Focus: loginFieldPassword}
			return m
		}},
		{"error status", func(t *testing.T, w, h int) Model {
			m := loginScreenModel(t, w, h)
			m.status = "Sign in failed: " + strings.Repeat("the admin API refused the credentials ", 4)
			return m
		}},
		{"expiry note", func(t *testing.T, w, h int) Model {
			m := grantWorstCaseExpiredModel(t, w, h)
			return m
		}},
		{"authenticating", func(t *testing.T, w, h int) Model {
			m := loginScreenModel(t, w, h)
			m.screen = screenAdminAuthenticating
			m.loadingText = "Signing in as " + strings.Repeat("operator", 12) + "..."
			return m
		}},
	}
}

func TestLoginViewFitsEverySizeInEveryState(t *testing.T) {
	t.Parallel()
	for _, state := range loginStates() {
		for _, size := range loginSizes {
			t.Run(fmt.Sprintf("%s/%dx%d", state.name, size.w, size.h), func(t *testing.T) {
				t.Parallel()
				view := state.build(t, size.w, size.h).View().Content
				assertGrantNoOverflow(t, state.name, view, size.w, size.h)
				if strings.Contains(ansi.Strip(view), strings.Repeat("p", 20)) {
					t.Fatalf("a password is shown in clear:\n%s", view)
				}
			})
		}
	}
}

func TestLoginViewGoldens(t *testing.T) {
	t.Parallel()
	for _, state := range loginStates() {
		for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
			name := fmt.Sprintf("login_%s_%dx%d", strings.ReplaceAll(state.name, " ", "_"), size.w, size.h)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assertLoginConfirmGolden(t, name, ansi.Strip(state.build(t, size.w, size.h).View().Content))
			})
		}
	}
}

// flattenBoxed is a rendered view as one line of words: colors and box-drawing
// characters removed, so text the panel wrapped across rows reads as written.
func flattenBoxed(view string) string {
	plain := strings.NewReplacer("│", " ", "╭", " ", "╮", " ", "╰", " ", "╯", " ", "─", " ").Replace(ansi.Strip(view))
	return strings.Join(strings.Fields(plain), " ")
}

func TestLoginShowsTheWholeExpiryNoteWrappedInsideThePanel(t *testing.T) {
	t.Parallel()
	for _, size := range loginSizes {
		m := grantWorstCaseExpiredModel(t, size.w, size.h)
		if !strings.Contains(flattenBoxed(m.View().Content), m.status) {
			t.Fatalf("%dx%d: the note %q is not shown whole:\n%s", size.w, size.h, m.status, ansi.Strip(m.View().Content))
		}
	}
}
