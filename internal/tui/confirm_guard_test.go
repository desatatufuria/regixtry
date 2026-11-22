package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"regixtry/internal/ports"
)

// ---- the keys that confirm ----

func TestOnlyAPlainLowercaseYOrEnterConfirms(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  keyMsg
		want bool
	}{
		{"y", keyMsg{Code: 'y', Text: "y"}, true},
		{"enter", testKeyEnter(), true},
		{"Y", keyMsg{Code: 'y', Text: "Y", Mod: tea.ModShift}, false},
		{"Y without a modifier flag", keyMsg{Code: 'Y', Text: "Y"}, false},
		{"alt+y", keyMsg{Code: 'y', Mod: tea.ModAlt}, false},
		{"alt+y with text", keyMsg{Code: 'y', Text: "y", Mod: tea.ModAlt}, false},
		{"ctrl+y", keyMsg{Code: 'y', Mod: tea.ModCtrl}, false},
		{"ctrl+alt+y", keyMsg{Code: 'y', Mod: tea.ModCtrl | tea.ModAlt}, false},
		{"meta+y", keyMsg{Code: 'y', Text: "y", Mod: tea.ModMeta}, false},
		{"y with caps lock on", keyMsg{Code: 'y', Text: "y", Mod: tea.ModCapsLock}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, calls := confirmDialogPrompt("delete")
			_, cmd, consumed := c.update(screenEnv{}, tt.key)
			if !consumed {
				t.Fatal("key not consumed")
			}
			if ran := *calls == 1; ran != tt.want || (cmd != nil) != tt.want {
				t.Fatalf("ran = %v command = %v, want %v", ran, cmd != nil, tt.want)
			}
		})
	}
}

// ---- in flight ----

func TestConfirmPromptIgnoresConfirmKeysOnceInFlight(t *testing.T) {
	t.Parallel()
	enter, y := testKeyEnter(), keyMsg{Code: 'y', Text: "y"}
	for _, keys := range [][]keyMsg{{enter, enter, enter}, {y, y}, {y, enter}, {enter, y}} {
		c, calls := confirmDialogPrompt("delete")
		for _, k := range keys {
			c, _, _ = c.update(screenEnv{}, k)
		}
		if *calls != 1 {
			t.Fatalf("%d keys ran the action %d times, want once", len(keys), *calls)
		}
	}
}

func TestConfirmPromptEscStillClosesWhileInFlight(t *testing.T) {
	t.Parallel()
	c, calls := confirmDialogPrompt("delete")
	c, _, _ = c.update(screenEnv{}, testKeyEnter())
	c, cmd, _ := c.update(screenEnv{}, testKeyEsc())
	if c.Active() || cmd != nil || *calls != 1 {
		t.Fatalf("active = %v command = %v calls = %d, want closed, no command, one call", c.Active(), cmd != nil, *calls)
	}
}

func TestConfirmPromptThatReturnsNoCommandStaysConfirmable(t *testing.T) {
	t.Parallel()
	calls := 0
	c := newConfirmPrompt("T", "M", "delete", "", func(screenEnv) tea.Cmd { calls++; return nil })
	for range 2 {
		c, _, _ = c.update(screenEnv{}, testKeyEnter())
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2: nothing is in flight when no command ran", calls)
	}
}

func TestFeatureActionConfirmsRunOnceOnRapidKeys(t *testing.T) {
	t.Parallel()
	for _, feature := range []string{trivyFeatureName, gitleaksFeatureName, signingFeatureName} {
		t.Run(feature, func(t *testing.T) {
			t.Parallel()
			calls := 0
			c := newConfirmPrompt("Confirm Disable", "Disable?", "disable", "Submitting...", func(screenEnv) tea.Cmd {
				calls++
				return func() tea.Msg { return adminFeatureActionCompletedMsg{feature: feature} }
			})
			for _, k := range []keyMsg{testKeyEnter(), testKeyRunes("y"), testKeyEnter()} {
				c, _, _ = c.update(screenEnv{}, k)
			}
			if calls != 1 {
				t.Fatalf("calls = %d, want once", calls)
			}
		})
	}
}

// ---- login: the longest messages never hide the form ----

func TestLoginLongMessagesNeverHideTheFormButtonsOrFooter(t *testing.T) {
	t.Parallel()
	longError := "Sign in failed: " + strings.Repeat("the admin API refused the credentials, ", 26)
	if len(longError) < 1000 {
		t.Fatalf("worst-case error is only %d characters", len(longError))
	}
	multibyte := strings.Repeat("ünï文字", 220)
	var names []string
	for i := range 6 {
		names = append(names, fmt.Sprintf("organization-%d/%s", i, strings.Repeat("x", 44)))
	}
	note := grantExpiryNote(names, 3)
	cases := map[string]func(m Model) Model{
		"error":           func(m Model) Model { m.status = longError; return m },
		"multibyte error": func(m Model) Model { m.status = multibyte; return m },
		"expiry note": func(m Model) Model {
			m.status, m.expiryNoteShown = note, true
			return m
		},
		"1000 character expiry note": func(m Model) Model {
			m.status, m.expiryNoteShown = longError, true
			return m
		},
	}
	for name, build := range cases {
		for _, size := range loginSizes {
			t.Run(fmt.Sprintf("%s/%dx%d", name, size.w, size.h), func(t *testing.T) {
				t.Parallel()
				m := build(loginScreenModel(t, size.w, size.h))
				m.adminLogin = adminLoginForm{Username: "admin", Password: "pw-secret", Focus: loginFieldPassword}
				view := m.View().Content
				assertGrantNoOverflow(t, name, view, size.w, size.h)
				plain := ansi.Strip(view)
				for _, want := range []string{"Username", "Password", "Sign in", "Back", "tab next field", "enter sign in", "esc back"} {
					if !strings.Contains(plain, want) {
						t.Fatalf("lacks %q:\n%s", want, plain)
					}
				}
				if strings.Contains(plain, "pw-secret") || !strings.Contains(plain, "…") {
					t.Fatalf("password shown or the long message not cut with an ellipsis:\n%s", plain)
				}
			})
		}
	}
}

func TestLoginCapLinesCutsOnARuneBoundary(t *testing.T) {
	t.Parallel()
	original := strings.Repeat("日本語ü ", 200)
	lines := loginCapLines(original, 20)
	if len(lines) != loginMaxMessageLines {
		t.Fatalf("lines = %d, want %d", len(lines), loginMaxMessageLines)
	}
	for _, line := range lines {
		if !utf8.ValidString(line) || strings.ContainsRune(line, '\uFFFD') {
			t.Fatalf("a rune was split: %q", line)
		}
	}
	last := lines[len(lines)-1]
	prefix, cut := strings.CutSuffix(last, "…")
	if !cut {
		t.Fatalf("last line %q does not end in an ellipsis", last)
	}
	// Every kept word comes from the original, in order, and the cut tail is a
	// whole-rune prefix of the original word it was taken from.
	words := strings.Fields(original)
	kept := strings.Fields(strings.Join(lines[:len(lines)-1], " ") + " " + prefix)
	for i, word := range kept {
		if i < len(kept)-1 && word != words[i] {
			t.Fatalf("word %d = %q, want %q", i, word, words[i])
		}
		if i == len(kept)-1 && !strings.HasPrefix(words[i], word) {
			t.Fatalf("truncated tail %q is not a prefix of the original word %q", word, words[i])
		}
	}
}

// ---- feature confirms through the model ----

func TestFeatureConfirmsUnlockAfterAFailedOrSuccessfulCompletion(t *testing.T) {
	t.Parallel()
	features := []struct {
		name string
		nav  shellNavID
		code screen
	}{
		{trivyFeatureName, shellNavImageScanning, screenSecurityTrivy},
		{gitleaksFeatureName, shellNavSecretScanning, screenSecurityGitleaksConfig},
		{signingFeatureName, shellNavSigningPolicy, screenSecuritySigningConfig},
	}
	for _, feature := range features {
		for _, failed := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/failed=%v", feature.name, failed), func(t *testing.T) {
				t.Parallel()
				const confirmMessage = "Confirm disable this feature?"
				client := &fakeAdminClient{
					loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Now().Add(time.Hour)},
					featurePage: ports.FeaturePage{
						Summary: ports.FeatureSummary{Name: feature.name, Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
						Actions: []ports.FeatureAction{{ID: "disable", Label: "Disable", ConfirmTitle: "Confirm Disable", ConfirmMessage: confirmMessage}},
					},
					actionResults: map[string]ports.FeatureActionResult{"disable": {Message: "Feature disabled."}},
				}
				m := runAdminLogin(t, newAdminReadyModel(t, client), "operator", "secret-pass")
				m = openSidebarItem(t, m, feature.nav)
				if m.screen != feature.code {
					t.Fatalf("screen = %q, want %q", m.screen, feature.code)
				}

				// press sends a key and runs the command it returns, without
				// following any command the result message returns.
				press := func(m Model, k keyMsg) (Model, tea.Msg) {
					updated, cmd := m.Update(k)
					if cmd == nil {
						return updated.(Model), nil
					}
					return updated.(Model), cmd()
				}
				deliver := func(m Model, msg tea.Msg) Model {
					updated, _ := m.Update(msg)
					return updated.(Model)
				}
				open := func(m Model) Model {
					m, _ = press(m, testKeyRunes("x"))
					if !strings.Contains(m.View().Content, confirmMessage) {
						t.Fatalf("the confirm did not open:\n%s", m.View().Content)
					}
					return m
				}

				if failed {
					client.featureErr = errors.New("feature backend refused the action")
				}
				m = open(m)
				var result tea.Msg
				for _, k := range []keyMsg{testKeyEnter(), testKeyEnter(), testKeyRunes("y")} {
					var msg tea.Msg
					m, msg = press(m, k)
					if msg != nil {
						result = msg
					}
				}
				if client.executeFeatureActionCalls != 1 {
					t.Fatalf("the action ran %d times, want once", client.executeFeatureActionCalls)
				}
				m = deliver(m, result)
				view := m.View().Content
				if strings.Contains(view, confirmMessage) {
					t.Fatalf("the prompt is still open after the completion:\n%s", view)
				}
				if failed && !strings.Contains(view, "feature backend refused the action") {
					t.Fatalf("the error is not shown:\n%s", view)
				}

				m = open(m)
				_, again := press(m, testKeyEnter())
				if again == nil || client.executeFeatureActionCalls != 2 {
					t.Fatalf("the new prompt did not dispatch (calls = %d)", client.executeFeatureActionCalls)
				}
			})
		}
	}
}
