package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"regixtry/internal/ports"
)

// loginCell finds where text starts in the rendered screen, below the header.
func loginCell(m Model, text string) (x, y int, ok bool) {
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	for row := shellHeaderRows; row < len(lines)-shellFooterRows; row++ {
		col := shellSidebarWidth
		if i := strings.Index(ansi.Cut(lines[row], col, 1<<20), text); i >= 0 {
			return col + lipgloss.Width(ansi.Cut(lines[row], col, 1<<20)[:i]), row, true
		}
	}
	return 0, 0, false
}

func loginMouseClient(accounts, failures int) *fakeAdminClient {
	client := loginScreenClient()
	client.loginAPI.backoff.Accounts, client.loginAPI.failures = nil, nil
	for i := range accounts {
		client.loginAPI.backoff.Accounts = append(client.loginAPI.backoff.Accounts, ports.AdminBackoffAccount{Account: fmt.Sprintf("wait-%d", i), Failures: 5 + i, RemainingSeconds: 60 * (i + 1)})
	}
	for i := range failures {
		at := fixedTestTime.Add(-time.Duration(i) * time.Minute)
		client.loginAPI.failures = append(client.loginAPI.failures, ports.LoginFailureRecord{WindowStart: at, LastSeen: at, Endpoint: "/auth/token",
			Reason: "unknown user", Account: fmt.Sprintf("fail-%d", i), IP: fmt.Sprintf("10.0.0.%d", i), Count: int64(i + 1)})
	}
	client.loginAPI.summary.AccountsInBackoff = accounts
	return client
}

func TestLoginSecurityClickSelectsAndFocusesAtEverySize(t *testing.T) {
	t.Parallel()
	for _, size := range geometrySizes {
		t.Run(sizeLabel(size.w, size.h), func(t *testing.T) {
			t.Parallel()
			client := loginMouseClient(3, 8)
			m := loginModelAt(t, client, size.w, size.h)
			// A click on a failures row focuses that panel and selects the row.
			x, y, ok := loginCell(m, "fail-2")
			if !ok {
				t.Skipf("fail-2 is not on screen at %dx%d", size.w, size.h)
			}
			m = sendMouse(t, m, mouseClickAt(x, y))
			if s := loginScreenOf(t, m); s.focus != loginPanelFailures || s.selFail != 2 {
				t.Fatalf("after clicking fail-2: focus=%v selFail=%d", s.focus, s.selFail)
			}
			// A click on a waiting row focuses the waiting panel and selects it.
			x, y, ok = loginCell(m, "wait-1")
			if !ok {
				t.Fatalf("wait-1 is not on screen at %dx%d", size.w, size.h)
			}
			m = sendMouse(t, m, mouseClickAt(x, y))
			if s := loginScreenOf(t, m); s.focus != loginPanelWaiting || s.selWaiting != 1 || s.selFail != 2 {
				t.Fatalf("after clicking wait-1: focus=%v selWaiting=%d selFail=%d", s.focus, s.selWaiting, s.selFail)
			}
			// Clicking the selected row again does nothing: no dialog, no call.
			m = sendMouse(t, m, mouseClickAt(x, y))
			m = sendMouse(t, m, mouseClickAt(x, y))
			if out := ansi.Strip(m.View().Content); strings.Contains(out, "Reset backoff?") || len(client.loginAPI.resetCalls) != 0 {
				t.Fatalf("a click opened the dialog or reset something: calls=%v", client.loginAPI.resetCalls)
			}
		})
	}
}

func TestLoginSecurityClickOnAHeaderOrBorderOnlyFocuses(t *testing.T) {
	t.Parallel()
	client := loginMouseClient(3, 8)
	m := loginModelAt(t, client, 200, 40)
	x, y, ok := loginCell(m, "LAST SEEN")
	if !ok {
		t.Fatal("no failures header")
	}
	m = sendMouse(t, m, mouseClickAt(x, y))
	if s := loginScreenOf(t, m); s.focus != loginPanelFailures || s.selFail != 0 || s.selWaiting != 0 {
		t.Fatalf("header click: focus=%v selFail=%d selWaiting=%d", s.focus, s.selFail, s.selWaiting)
	}
	x, y, ok = loginCell(m, "FAILS")
	if !ok {
		t.Fatal("no waiting header")
	}
	m = sendMouse(t, m, mouseClickAt(x, y))
	if s := loginScreenOf(t, m); s.focus != loginPanelWaiting || s.selWaiting != 0 {
		t.Fatalf("waiting header click: focus=%v selWaiting=%d", s.focus, s.selWaiting)
	}
}

func TestLoginSecurityWheelMovesThePanelUnderThePointer(t *testing.T) {
	t.Parallel()
	m := loginModelAt(t, loginMouseClient(3, 8), 200, 40)
	x, y, _ := loginCell(m, "fail-0")
	for range 3 {
		m = sendMouse(t, m, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	}
	if s := loginScreenOf(t, m); s.focus != loginPanelFailures || s.selFail != 3 || s.selWaiting != 0 {
		t.Fatalf("wheel down over failures: focus=%v selFail=%d selWaiting=%d", s.focus, s.selFail, s.selWaiting)
	}
	m = sendMouse(t, m, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})
	if s := loginScreenOf(t, m); s.selFail != 2 {
		t.Fatalf("wheel up: selFail=%d", s.selFail)
	}
	wx, wy, _ := loginCell(m, "wait-0")
	m = sendMouse(t, m, tea.MouseWheelMsg{X: wx, Y: wy, Button: tea.MouseWheelDown})
	if s := loginScreenOf(t, m); s.focus != loginPanelWaiting || s.selWaiting != 1 || s.selFail != 2 {
		t.Fatalf("wheel over waiting: focus=%v selWaiting=%d selFail=%d", s.focus, s.selWaiting, s.selFail)
	}
}

func TestLoginSecurityMouseNeverResetsAndIsBlockedByTheDialog(t *testing.T) {
	t.Parallel()
	client := loginMouseClient(3, 8)
	m := loginModelAt(t, client, 200, 40)
	m, _ = loginSend(t, m, testKeyRunes("x"))
	before := loginScreenOf(t, m)
	x, y, _ := loginCell(m, "wait-2")
	// Every cell of the screen: nothing moves, nothing is confirmed.
	for cx := 0; cx < 200; cx += 7 {
		for cy := 0; cy < 40; cy += 3 {
			m = sendMouse(t, m, mouseClickAt(cx, cy))
			m = sendMouse(t, m, tea.MouseWheelMsg{X: cx, Y: cy, Button: tea.MouseWheelDown})
		}
	}
	m = sendMouse(t, m, mouseClickAt(x, y))
	s := loginScreenOf(t, m)
	if !s.confirm.Active() || s.selWaiting != before.selWaiting || len(client.loginAPI.resetCalls) != 0 {
		t.Fatalf("the mouse acted while the dialog was open: active=%v sel=%d calls=%v", s.confirm.Active(), s.selWaiting, client.loginAPI.resetCalls)
	}
	// The sidebar is blocked too while the dialog is in front.
	sx, sy := 3, shellHeaderRows+1+2
	m = sendMouse(t, m, mouseClickAt(sx, sy))
	if m.screen != screenAdminLoginSecurity {
		t.Fatalf("the sidebar opened %q while a dialog was open", m.screen)
	}
}

func TestLoginSecuritySidebarClickOpensTheScreenFromAnotherScreen(t *testing.T) {
	t.Parallel()
	m := overviewModelAt(t, loginMouseClient(1, 1), 200, 40)
	y := -1
	for i, line := range shellSidebarLines() {
		if line.IsItem && line.Item.ID == shellNavLoginSecurity {
			y = shellHeaderRows + 1 + i
		}
	}
	if y < 0 {
		t.Fatal("no sidebar row for Login security")
	}
	m = sendMouse(t, m, mouseClickAt(5, y))
	if m.screen != screenAdminLoginSecurity {
		t.Fatalf("click opened %q", m.screen)
	}
}
