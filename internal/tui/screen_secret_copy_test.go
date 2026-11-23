package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var updateAccessSecretGoldens = flag.Bool("update-access-secret", false, "rewrite the reveal view golden files")

// clipboardText reports the text a command asks the terminal to copy, and
// whether the message is the Bubble Tea v2 clipboard-set message at all. The
// message type is unexported by the library, so it is inspected by name.
func clipboardText(msg tea.Msg) (string, bool) {
	if msg == nil {
		return "", false
	}
	v := reflect.ValueOf(msg)
	if v.Type().String() != "tea.setClipboardMsg" || v.Kind() != reflect.String {
		return "", false
	}
	return v.String(), true
}

// secretStates are the two screens that show a one-time secret, in their
// secret state, and what each one shows next to it.
var secretStates = []struct {
	name  string
	build func(t *testing.T, w, h int) (adminScreen, screenEnv)
}{
	{"tokens", tokensStateAt(nil, testKeyRunes("n"), testKeyEnter())},
	{"robot", createStateAt(fiveRepos(), append(append(accessType("release-bot"), testKeyTab(), testKeyTab()), testKeyEnter(), testKeyEnter())...)},
}

func TestCCopiesTheWholeSecretThroughTheClipboardCommand(t *testing.T) {
	t.Parallel()
	for _, st := range secretStates {
		t.Run(st.name, func(t *testing.T) {
			s, env := st.build(t, 150, 24)
			next, cmd, consumed := s.Update(env, testKeyRunes("c"))
			if cmd == nil || !consumed {
				t.Fatal("c did not return a command")
			}
			text, ok := clipboardText(cmd())
			if !ok || text != accessSecret {
				t.Fatalf("the command did not carry exactly the secret: ok=%v text=%q", ok, text)
			}
			view := accessView(next, env)
			mustContain(t, view, "Copied.", "OSC 52", "press v")
			mustNotContain(t, accessView(s, env), "Copied.")
			if _, cmd, _ := s.Update(env, keyMsg{Text: "c"}); cmd != nil {
				t.Fatal("a pasted c copied the secret")
			}
		})
	}
}

func TestCAndVAreOfferedInTheSecretFooter(t *testing.T) {
	t.Parallel()
	for _, st := range secretStates {
		s, env := st.build(t, 150, 24)
		mustContain(t, accessView(s, env), "c copy", "v show alone", "enter done")
	}
}

func TestCDoesNothingOnceTheSecretIsGone(t *testing.T) {
	t.Parallel()
	for _, st := range secretStates {
		t.Run(st.name, func(t *testing.T) {
			s, env := st.build(t, 150, 24)
			for _, k := range []keyMsg{testKeyEnter(), testKeyEsc()} {
				after, _, _ := s.Update(env, k)
				// Esc and Enter leave the secret screen; the next screen may be
				// the same value (tokens) or a fresh form (robot).
				next, cmd, _ := after.Update(env, testKeyRunes("c"))
				if text, ok := clipboardText(msgOf(cmd)); ok {
					t.Fatalf("c copied %q after Done", text)
				}
				mustNotContain(t, fmt.Sprintf("%+v", next), accessSecret)
			}
		})
	}
}

func msgOf(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestCTypesCInsideATextInput(t *testing.T) {
	t.Parallel()
	_, env, tokens := tokensFixture(true)
	next, cmd, _ := tokens.Update(env, testKeyRunes("n"))
	typed, cmd, _ := next.Update(env, testKeyRunes("c"))
	if _, ok := clipboardText(msgOf(cmd)); ok || typed.(tokensScreen).name != "c" {
		t.Fatalf("c in the name field: name=%q", typed.(tokensScreen).name)
	}
	form, _ := accessPress(t, newCreateRobotScreen(), env, testKeyRunes("c"))
	if form.(createRobotScreen).name != "c" {
		t.Fatalf("c in the robot name field typed %q", form.(createRobotScreen).name)
	}
}

func TestVTogglesTheBareRevealView(t *testing.T) {
	t.Parallel()
	for _, st := range secretStates {
		t.Run(st.name, func(t *testing.T) {
			s, env := st.build(t, 150, 24)
			normal := accessView(s, env)
			mustContain(t, normal, "regixtry admin")

			shown, cmd, consumed := s.Update(env, testKeyRunes("v"))
			if !consumed || cmd != nil {
				t.Fatalf("v consumed=%v cmd=%v", consumed, cmd != nil)
			}
			view := accessView(shown, env)
			mustNotContain(t, view, "regixtry admin", "Overview", "│", "╭", "ACCESS")
			mustContain(t, view, revealHint, accessSecret)

			back, _, _ := shown.Update(env, testKeyRunes("v"))
			if accessView(back, env) != normal {
				t.Fatal("v twice did not return to the normal secret screen")
			}
			// Mouse never reaches a copy: only keys do.
			if _, cmd, _ := shown.Update(env, keyMsg{Text: "v"}); cmd != nil {
				t.Fatal("a pasted v did something")
			}
		})
	}
}

func TestRevealViewCopiesAndEscEnterAreDone(t *testing.T) {
	t.Parallel()
	for _, st := range secretStates {
		t.Run(st.name, func(t *testing.T) {
			s, env := st.build(t, 150, 24)
			shown, _, _ := s.Update(env, testKeyRunes("v"))
			next, cmd, _ := shown.Update(env, testKeyRunes("c"))
			if text, ok := clipboardText(msgOf(cmd)); !ok || text != accessSecret {
				t.Fatalf("c in the reveal view: %q %v", text, ok)
			}
			mustContain(t, accessView(next, env), "Copied.")
			for _, k := range []keyMsg{testKeyEnter(), testKeyEsc()} {
				done, _, _ := shown.Update(env, k)
				mustNotContain(t, fmt.Sprintf("%+v", done), accessSecret)
				mustNotContain(t, accessView(done, env), accessSecret, revealHint)
			}
		})
	}
}

// secretOnlyRows are the rows of a rendered frame made of nothing but the
// secret: what a triple-click copies.
func secretOnlyRows(frame, secret string) []string {
	var rows []string
	for _, line := range strings.Split(frame, "\n") {
		trimmed := strings.TrimRight(line, " ")
		if trimmed != "" && strings.Contains(secret, trimmed) && !strings.HasPrefix(line, " ") {
			rows = append(rows, trimmed)
		}
	}
	return rows
}

func TestRevealViewGeometryAndTheSecretAloneOnItsRow(t *testing.T) {
	t.Parallel()
	for _, st := range secretStates {
		for _, size := range accessSizes {
			t.Run(st.name+"_"+sizeLabel(size.w, size.h), func(t *testing.T) {
				s, env := st.build(t, size.w, size.h)
				shown, _, _ := s.Update(env, testKeyRunes("v"))
				frame := s.View(newAdminTheme(), env)
				if !frame.Full {
					t.Fatal("the secret screen is not a full frame")
				}
				revealed := shown.View(newAdminTheme(), env)
				if !revealed.Full {
					t.Fatal("the reveal view is not a full frame")
				}
				plain := ansi.Strip(revealed.Body)
				lines := strings.Split(plain, "\n")
				if len(lines) != size.h {
					t.Fatalf("%d rows, want %d", len(lines), size.h)
				}
				for _, line := range lines {
					if w := lipglossWidth(line); w > size.w {
						t.Fatalf("line %d wide in %d: %q", w, size.w, line)
					}
				}
				if got := strings.Join(secretOnlyRows(plain, accessSecret), ""); got != accessSecret {
					t.Fatalf("the secret rows join to %q", got)
				}
				rows := secretOnlyRows(plain, accessSecret)
				if len(rows) != 1 || !strings.HasPrefix(plain[strings.Index(plain, accessSecret)-1:], "\n"+accessSecret+"\n") {
					t.Fatalf("the secret is not alone on one row starting at column 0:\n%s", plain)
				}
				if !strings.HasPrefix(lines[len(lines)-1], revealHint) {
					t.Fatalf("the hint is not the last row: %q", lines[len(lines)-1])
				}
				mustContain(t, plain, "act_")
				for _, line := range lines {
					if strings.Contains(line, "act_") && strings.Contains(line, accessSecret[:12]) {
						t.Fatalf("the accessor shares a row with the secret: %q", line)
					}
				}
				for _, bad := range []string{"…", "­", "\x1b"} {
					mustNotContain(t, plain, bad)
				}
			})
		}
	}
}

func TestNormalSecretScreenKeepsTheSecretWholeAndOnOneRowWhenItFits(t *testing.T) {
	t.Parallel()
	for _, st := range secretStates {
		for _, size := range accessSizes {
			t.Run(st.name+"_"+sizeLabel(size.w, size.h), func(t *testing.T) {
				s, env := st.build(t, size.w, size.h)
				plain := accessView(s, env)
				mustContain(t, plain, accessSecret)
				lines := strings.Split(plain, "\n")
				if len(lines) > size.h {
					t.Fatalf("%d rows in %d", len(lines), size.h)
				}
				for _, line := range lines {
					if lipglossWidth(line) > size.w {
						t.Fatalf("line too wide: %q", line)
					}
				}
			})
		}
	}
}

func TestRevealRowsSplitsOnlyWhenTheTerminalIsNarrower(t *testing.T) {
	t.Parallel()
	secret := accessSecret
	for _, width := range []int{40, 80, 78, 200} {
		rows := revealRows(secret, width)
		if strings.Join(rows, "") != secret {
			t.Fatalf("width %d lost characters: %q", width, rows)
		}
		for i, row := range rows {
			if i < len(rows)-1 && len(row) != width {
				t.Fatalf("width %d: row %d is %d wide, want exactly the terminal width", width, i, len(row))
			}
			if len(row) > width {
				t.Fatalf("width %d: row too wide", width)
			}
		}
		if width >= len(secret) && len(rows) != 1 {
			t.Fatalf("width %d: %d rows for a secret that fits", width, len(rows))
		}
	}
}

func TestRevealViewOfANarrowTerminalNamesTheWholeCopy(t *testing.T) {
	t.Parallel()
	out := ansi.Strip(renderReveal(newAdminTheme(), revealSpec{Title: "Token ci", Secret: accessSecret, Facts: []string{"Accessor  act_x"}, Copied: false, Width: 40, Height: 12}))
	lines := strings.Split(out, "\n")
	if len(lines) != 12 {
		t.Fatalf("%d rows", len(lines))
	}
	mustContain(t, out, "c copies it whole")
	if got := strings.Join(secretOnlyRows(out, accessSecret), ""); got != accessSecret {
		t.Fatalf("narrow secret rows join to %q", got)
	}
	for _, line := range lines {
		if lipglossWidth(line) > 40 {
			t.Fatalf("line wider than 40: %q", line)
		}
	}
}

func TestRevealViewWorksWithTheUpdateBannerAndTheTooSmallNotice(t *testing.T) {
	t.Parallel()
	m, _ := accessModel(t, nil)
	m = resizeModel(t, m, 150, 24)
	m.currentVersion = "v1.2.3"
	m, create := startTokenCreate(t, m)
	updated, _ := m.Update(create())
	m = updated.(Model)
	updated, cmd := m.Update(testKeyRunes("c"))
	if text, ok := clipboardText(msgOf(cmd)); !ok || text != "token-secret-value" {
		t.Fatalf("c on the model: %q %v", text, ok)
	}
	m = updated.(Model)
	m = overviewPress(t, m, testKeyRunes("v"))
	lines := strings.Split(accessModelText(m), "\n")
	if len(lines) != 24 || !strings.Contains(lines[23], "v1.2.3") {
		t.Fatalf("%d rows, last %q", len(lines), lines[len(lines)-1])
	}
	mustContain(t, accessModelText(m), "token-secret-value", revealHint)
	mustNotContain(t, accessModelText(m), "regixtry admin")

	small := resizeModel(t, m, 100, 20)
	mustNotContain(t, accessModelText(small), "token-secret-value")
	back := resizeModel(t, small, 150, 24)
	mustContain(t, accessModelText(back), "token-secret-value")

	m = overviewPress(t, back, testKeyEsc())
	mustNotContain(t, fmt.Sprintf("%+v", m), "token-secret-value")
}

func TestQStillDoesNotQuitWhileASecretIsShown(t *testing.T) {
	t.Parallel()
	m, _ := accessModel(t, nil)
	m, create := startTokenCreate(t, m)
	updated, _ := m.Update(create())
	m = updated.(Model)
	for _, press := range [][]keyMsg{nil, {testKeyRunes("v")}} {
		m = overviewPress(t, m, press...)
		_, cmd := m.Update(testKeyRunes("q"))
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Fatal("q quit while a secret was shown")
			}
		}
	}
}

func TestRevealViewGoldens(t *testing.T) {
	for _, st := range secretStates {
		for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
			file := "reveal_" + st.name + "_" + sizeLabel(size.w, size.h)
			t.Run(file, func(t *testing.T) {
				s, env := st.build(t, size.w, size.h)
				shown, _, _ := s.Update(env, testKeyRunes("v"))
				got := accessView(shown, env)
				path := filepath.Join("testdata", "access_secret", file+".golden")
				if *updateAccessSecretGoldens {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read golden (run with -update-access-secret): %v", err)
				}
				if string(want) != got {
					t.Fatalf("view differs from %s\n--- want\n%s\n--- got\n%s", path, want, got)
				}
			})
		}
	}
}

func TestTheMouseNeverCopiesTheSecret(t *testing.T) {
	t.Parallel()
	m, _ := accessModel(t, nil)
	m, create := startTokenCreate(t, m)
	updated, _ := m.Update(create())
	m = updated.(Model)
	for _, reveal := range []bool{false, true} {
		if reveal {
			m = overviewPress(t, m, testKeyRunes("v"))
		}
		for y := 0; y < 24; y += 2 {
			for x := 0; x < 150; x += 9 {
				for _, msg := range []tea.Msg{mouseClickAt(x, y), tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown}} {
					next, cmd := m.Update(msg)
					if _, ok := clipboardText(msgOf(cmd)); ok {
						t.Fatalf("the mouse at %d,%d copied", x, y)
					}
					m = next.(Model)
				}
			}
		}
		mustContain(t, accessModelText(m), "token-secret-value")
	}
}
