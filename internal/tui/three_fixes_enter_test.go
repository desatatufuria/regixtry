package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// --- Fix 2: a second Enter does not create twice ---

func enterCmds(t *testing.T, m Model, presses int) (Model, []tea.Cmd) {
	t.Helper()
	var cmds []tea.Cmd
	for i := 0; i < presses; i++ {
		updated, cmd := m.Update(testKeyEnter())
		m = updated.(Model)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return m, cmds
}

type createForm struct {
	name  string
	open  func(t *testing.T, c *ttlRecordingClient) Model
	calls func(c *ttlRecordingClient) int
	fail  func(c *ttlRecordingClient)
}

var createForms = []createForm{
	{
		name:  "token",
		open:  func(t *testing.T, c *ttlRecordingClient) Model { return tokenFormAt(t, c, 150, 24) },
		calls: func(c *ttlRecordingClient) int { return c.createTokenCalls },
		fail:  func(c *ttlRecordingClient) { c.createTokenErr = errors.New("boom") },
	},
	{
		name:  "robot",
		open:  func(t *testing.T, c *ttlRecordingClient) Model { return robotFormAt(t, c, 150, 24) },
		calls: func(c *ttlRecordingClient) int { return c.createRobotCalls },
		fail:  func(c *ttlRecordingClient) { c.createRobotErr = errors.New("boom") },
	},
}

func TestSecondEnterBeforeTheResultCreatesOnce(t *testing.T) {
	t.Parallel()
	for _, f := range createForms {
		t.Run(f.name, func(t *testing.T) {
			c := newTTLClient()
			m, cmds := enterCmds(t, f.open(t, c), 3)
			for _, cmd := range cmds {
				cmd()
			}
			if got := f.calls(c); got != 1 {
				t.Fatalf("create calls after 3 Enters = %d, want 1", got)
			}
			_ = m
		})
	}
}

func TestEnterWorksAgainAfterTheResult(t *testing.T) {
	t.Parallel()
	for _, f := range createForms {
		for _, outcome := range []string{"error", "success"} {
			t.Run(f.name+" "+outcome, func(t *testing.T) {
				c := newTTLClient()
				if outcome == "error" {
					f.fail(c)
				}
				m, cmds := enterCmds(t, f.open(t, c), 1)
				if len(cmds) != 1 {
					t.Fatalf("first Enter returned %d commands", len(cmds))
				}
				updated, _ := m.Update(cmds[0]())
				m = updated.(Model)
				if outcome == "success" {
					// success resets the form; refill it through the same keys
					if f.name == "robot" {
						m = testSendKeys(t, m, testKeyRunes("ci-bot"), testKeyTab(), testKeyRunes("team/app"), testKeyTab(), testKeyTab())
					} else {
						m.screen = screenAdminCreateToken
						m.adminView.SelectedUserID = "u-1"
					}
				}
				_, cmds = enterCmds(t, m, 1)
				if len(cmds) == 0 {
					t.Fatalf("Enter after the %s result issued no command", outcome)
				}
			})
		}
	}
}

func TestEscStillLeavesWhileCreating(t *testing.T) {
	t.Parallel()
	for _, f := range createForms {
		t.Run(f.name, func(t *testing.T) {
			c := newTTLClient()
			m, _ := enterCmds(t, f.open(t, c), 1)
			before := m.screen
			m = pressEsc(t, m)
			if m.screen == before {
				t.Fatalf("Esc did not leave the %s form while creating", f.name)
			}
		})
	}
}
