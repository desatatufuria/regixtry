package tui

import (
	"sort"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Every key message in these tests is built only by the
// testKey* helpers below, so a Bubble Tea major upgrade edits just these bodies.

func testKeyRunes(text string) keyMsg { return keyMsg{Code: firstRune(text), Text: text} }
func testKeySpace() keyMsg            { return keyMsg{Code: tea.KeySpace, Text: " "} }
func testKeyEnter() keyMsg            { return keyMsg{Code: tea.KeyEnter} }
func testKeyEsc() keyMsg              { return keyMsg{Code: tea.KeyEscape} }
func testKeyBackspace() keyMsg        { return keyMsg{Code: tea.KeyBackspace} }
func testKeyTab() keyMsg              { return keyMsg{Code: tea.KeyTab} }
func testKeyShiftTab() keyMsg         { return keyMsg{Code: tea.KeyTab, Mod: tea.ModShift} }
func testKeyUp() keyMsg               { return keyMsg{Code: tea.KeyUp} }
func testKeyDown() keyMsg             { return keyMsg{Code: tea.KeyDown} }
func testKeyLeft() keyMsg             { return keyMsg{Code: tea.KeyLeft} }
func testKeyRight() keyMsg            { return keyMsg{Code: tea.KeyRight} }
func testKeyPgUp() keyMsg             { return keyMsg{Code: tea.KeyPgUp} }
func testKeyPgDown() keyMsg           { return keyMsg{Code: tea.KeyPgDown} }
func testKeyHome() keyMsg             { return keyMsg{Code: tea.KeyHome} }
func testKeyEnd() keyMsg              { return keyMsg{Code: tea.KeyEnd} }
func testKeyAlt(text string) keyMsg {
	return keyMsg{Code: firstRune(text), Text: "", Mod: tea.ModAlt}
}

func testKeyCtrl(letter byte) keyMsg {
	return keyMsg{Code: rune(letter), Mod: tea.ModCtrl}
}

func firstRune(text string) rune {
	for _, r := range text {
		return r
	}
	return 0
}

// testSendKey feeds one key through Update and drops the returned command.
func testSendKey(t *testing.T, m Model, msg keyMsg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	return updated.(Model)
}

func testSendKeys(t *testing.T, m Model, msgs ...keyMsg) Model {
	t.Helper()
	for _, msg := range msgs {
		m = testSendKey(t, m, msg)
	}
	return m
}

func TestKeyHelpers(t *testing.T) {
	t.Parallel()

	helpers := map[string]func(keyMsg) bool{
		"isMoveUpKey":    isMoveUpKey,
		"isMoveDownKey":  isMoveDownKey,
		"isMoveLeftKey":  isMoveLeftKey,
		"isMoveRightKey": isMoveRightKey,
		"isEnterKey":     isEnterKey,
		"isTabKey":       isTabKey,
		"isShiftTabKey":  isShiftTabKey,
		"isEscKey":       isEscKey,
		"isBackspaceKey": isBackspaceKey,
		"isBackKey":      isBackKey,
		"isPgUpKey":      isPgUpKey,
		"isPgDnKey":      isPgDnKey,
		"isHomeKey":      isHomeKey,
		"isEndKey":       isEndKey,
	}

	tests := []struct {
		name string
		msg  keyMsg
		want []string // helpers that must return true; every other helper must return false
	}{
		{"enter", testKeyEnter(), []string{"isEnterKey"}},
		{"tab", testKeyTab(), []string{"isTabKey"}},
		{"shift+tab", testKeyShiftTab(), []string{"isShiftTabKey"}},
		{"esc", testKeyEsc(), []string{"isEscKey", "isBackKey"}},
		{"backspace", testKeyBackspace(), []string{"isBackspaceKey", "isBackKey"}},
		{"up arrow", testKeyUp(), []string{"isMoveUpKey"}},
		{"down arrow", testKeyDown(), []string{"isMoveDownKey"}},
		{"left arrow", testKeyLeft(), []string{"isMoveLeftKey"}},
		{"right arrow", testKeyRight(), []string{"isMoveRightKey"}},
		{"pgup", testKeyPgUp(), []string{"isPgUpKey"}},
		{"pgdown", testKeyPgDown(), []string{"isPgDnKey"}},
		{"home", testKeyHome(), []string{"isHomeKey"}},
		{"end", testKeyEnd(), []string{"isEndKey"}},
		{"k rune is vim up", testKeyRunes("k"), []string{"isMoveUpKey"}},
		{"upper K is vim up", testKeyRunes("K"), []string{"isMoveUpKey"}},
		{"j rune is vim down", testKeyRunes("j"), []string{"isMoveDownKey"}},
		{"upper J is vim down", testKeyRunes("J"), []string{"isMoveDownKey"}},
		{"alt+k still vim up", testKeyAlt("k"), []string{"isMoveUpKey"}},
		{"h has no left alias", testKeyRunes("h"), nil},
		{"l has no right alias", testKeyRunes("l"), nil},
		{"plain letter", testKeyRunes("a"), nil},
		{"space", testKeySpace(), nil},
		{"multi-rune kk", testKeyRunes("kk"), nil},
		{"multi-rune with space", testKeyRunes("a b"), nil},
		{"empty runes", testKeyRunes(""), nil},
		{"ctrl+c", testKeyCtrl('c'), nil},
		{"ctrl+k is not vim up", testKeyCtrl('k'), nil},
		{"ctrl+j is not vim down", testKeyCtrl('j'), nil},
		// A v1 paste stringifies as "[text]", so pasted "enter" is not Enter.
		{"pasted text spelling a key name", keyMsg{Text: "enter"}, nil},
	}

	names := make([]string, 0, len(helpers))
	for name := range helpers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wantSet := map[string]bool{}
			for _, name := range tt.want {
				wantSet[name] = true
			}
			for _, name := range names {
				if got := helpers[name](tt.msg); got != wantSet[name] {
					t.Errorf("%s(%q) = %v, want %v", name, tt.msg.String(), got, wantSet[name])
				}
			}
		})
	}
}

func TestIsRuneKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		msg        keyMsg
		candidates []rune
		want       bool
	}{
		{"match", testKeyRunes("g"), []rune{'g'}, true},
		{"match is case-insensitive on the message", testKeyRunes("G"), []rune{'g'}, true},
		{"match is case-insensitive on the candidate", testKeyRunes("g"), []rune{'G'}, true},
		{"any of several candidates", testKeyRunes("x"), []rune{'a', 'x'}, true},
		{"no candidate matches", testKeyRunes("z"), []rune{'a', 'x'}, false},
		{"no candidates", testKeyRunes("a"), nil, false},
		{"space candidate", testKeySpace(), []rune{' '}, true},
		{"space is not a letter", testKeySpace(), []rune{'a'}, false},
		{"multi-rune input", testKeyRunes("gg"), []rune{'g'}, false},
		{"empty runes", testKeyRunes(""), []rune{'g'}, false},
		{"alt modifier is ignored", testKeyAlt("g"), []rune{'g'}, true},
		{"ctrl combo has no rune", testKeyCtrl('g'), []rune{'g'}, false},
		{"enter has no rune", testKeyEnter(), []rune{'g'}, false},
		{"digit", testKeyRunes("7"), []rune{'7'}, true},
		{"non-ascii", testKeyRunes("ñ"), []rune{'Ñ'}, true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := isRuneKey(tt.msg, tt.candidates...); got != tt.want {
				t.Fatalf("isRuneKey(%q, %q) = %v, want %v", tt.msg.String(), string(tt.candidates), got, tt.want)
			}
		})
	}
}

func TestCreateUserFormPasswordEntry(t *testing.T) {
	t.Parallel()

	m := fixedAdminModel(t, 160, 40)
	mountCreateUser(&m, createUserFieldUsername)

	m = testSendKey(t, m, testKeyRunes("alice"))
	if got, want := createUserOf(m).username, "alice"; got != want {
		t.Fatalf("Username = %q, want %q", got, want)
	}
	m = testSendKey(t, m, testKeyTab())
	m = testSendKeys(t, m, testKeyRunes("s3cr"), testKeySpace(), testKeyRunes("et"))
	if got, want := createUserOf(m).password, "s3cr et"; got != want {
		t.Fatalf("Password = %q, want %q (a real space is typed into the field)", got, want)
	}
	m = testSendKey(t, m, testKeyBackspace())
	if got, want := createUserOf(m).password, "s3cr e"; got != want {
		t.Fatalf("Password after backspace = %q, want %q", got, want)
	}

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "alice") {
		t.Fatalf("view does not show the username")
	}
	if !strings.Contains(view, "••••••") {
		t.Fatalf("view does not show a 6-char mask for the password:\n%s", view)
	}
	for _, leak := range []string{"s3cr", "s3c", "3cr"} {
		if strings.Contains(view, leak) {
			t.Fatalf("view leaks password fragment %q:\n%s", leak, view)
		}
	}

	m = testSendKey(t, m, testKeyTab())
	m = testSendKeys(t, m, testKeyLeft(), testKeySpace())
	if got := createUserOf(m); !got.readOnly || got.admin {
		t.Fatalf("left then space on the role row: admin=%v readOnly=%v, want only read-only", got.admin, got.readOnly)
	}
}

func TestResetPasswordFormEntry(t *testing.T) {
	t.Parallel()

	m := fixedAdminModel(t, 160, 40)
	mountResetPassword(&m)

	m = testSendKeys(t, m, testKeyRunes("pa"), testKeySpace(), testKeyRunes("ss"))
	if got, want := resetPasswordOf(m).password, "pa ss"; got != want {
		t.Fatalf("NewPassword = %q, want %q (this field types spaces)", got, want)
	}
	m = testSendKey(t, m, testKeyBackspace())
	if got, want := resetPasswordOf(m).password, "pa s"; got != want {
		t.Fatalf("NewPassword after backspace = %q, want %q", got, want)
	}
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "pa s") {
		t.Fatalf("view leaks the new password:\n%s", view)
	}
}
