package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestUpdateChannelScreenSatisfiesAdminScreenInterface mirrors every other
// migrated screen's own compile-level guard (design.md Decision A):
// updateChannelScreen must satisfy adminScreen value-typed.
func TestUpdateChannelScreenSatisfiesAdminScreenInterface(t *testing.T) {
	t.Parallel()

	var _ adminScreen = updateChannelScreen{}
}

// TestUpdateChannelScreenLoadsCurrentChannelOnInit guards the read path:
// Init's Cmd fetches the server-side channel via AdminClient.GetUpdateChannel
// (not a CLI flag, not client-side Model state -- see checkForUpdateCmd's
// own doc comment for why), and the resulting message seeds both current
// and selected with the loaded value.
func TestUpdateChannelScreenLoadsCurrentChannelOnInit(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{updateChannel: "insider"}
	env := screenEnv{Client: adminClient, Session: AdminSession{BearerToken: "token"}, Layout: consoleLayout{SectionRows: 20}}

	screen := newUpdateChannelScreen()
	cmd := screen.Init(env)
	if cmd == nil {
		t.Fatal("Init() returned a nil Cmd, want the load-channel Cmd")
	}

	updated, _, _ := screen.Update(env, cmd())
	next := updated.(updateChannelScreen)
	if !next.loaded {
		t.Fatal("loaded = false after adminUpdateChannelLoadedMsg, want true")
	}
	if next.current != "insider" || next.selected != "insider" {
		t.Fatalf("current = %q, selected = %q, want both %q", next.current, next.selected, "insider")
	}
	if adminClient.getUpdateChannelCalls != 1 {
		t.Fatalf("GetUpdateChannel call count = %d, want exactly 1", adminClient.getUpdateChannelCalls)
	}

	view := next.View(newAdminTheme(), env).Body
	if !strings.Contains(view, "insider") {
		t.Fatalf("view = %q, want it to mention the loaded channel", view)
	}
}

// TestUpdateChannelScreenSpaceTogglesPendingSelectionThenEnterSaves guards
// the full pick-then-save flow: Space flips the PENDING selection only
// (current stays untouched until a save actually confirms), and Enter
// dispatches AdminClient.SetUpdateChannel with that pending value -- the
// only write path this feature has (PUT /admin/v1/update-channel,
// admin-gated; a bare CLI flag was explicitly rejected during this
// feature's design for not proving the caller was an admin).
func TestUpdateChannelScreenSpaceTogglesPendingSelectionThenEnterSaves(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{updateChannel: "stable"}
	env := screenEnv{Client: adminClient, Session: AdminSession{BearerToken: "token"}, Layout: consoleLayout{SectionRows: 20}}

	screen := newUpdateChannelScreen()
	loaded, _, _ := screen.Update(env, adminUpdateChannelLoadedMsg{channel: "stable"})
	screen = loaded.(updateChannelScreen)

	toggled, cmd, consumed := screen.Update(env, keyMsgFor(" "))
	screen = toggled.(updateChannelScreen)
	if !consumed {
		t.Fatal("Space was not consumed")
	}
	if cmd != nil {
		t.Fatal("Space returned a non-nil Cmd, want nil -- toggling is local state, not a save")
	}
	if screen.selected != "insider" {
		t.Fatalf("selected = %q after Space, want %q", screen.selected, "insider")
	}
	if screen.current != "stable" {
		t.Fatalf("current = %q after Space, want %q (unchanged until saved)", screen.current, "stable")
	}

	saving, saveCmd, consumed := screen.Update(env, keyMsgFor("enter"))
	screen = saving.(updateChannelScreen)
	if !consumed {
		t.Fatal("Enter was not consumed")
	}
	if saveCmd == nil {
		t.Fatal("Enter with a pending change returned a nil Cmd, want the save Cmd")
	}
	if !screen.saving {
		t.Fatal("saving = false after Enter, want true while the save is in flight")
	}

	saveMsg := saveCmd()
	updated, _, _ := screen.Update(env, saveMsg)
	final := updated.(updateChannelScreen)
	if final.saving {
		t.Fatal("saving = true after the save completed, want false")
	}
	if final.current != "insider" || final.selected != "insider" {
		t.Fatalf("current = %q, selected = %q after save, want both %q", final.current, final.selected, "insider")
	}
	if adminClient.updateUpdateChannelCalls != 1 || adminClient.lastUpdateChannelInput != "insider" {
		t.Fatalf("SetUpdateChannel calls = %d, lastInput = %q, want 1 call with %q", adminClient.updateUpdateChannelCalls, adminClient.lastUpdateChannelInput, "insider")
	}
}

// TestUpdateChannelScreenEnterWithNoPendingChangeIsANoop guards against a
// spurious write: pressing Enter without ever toggling the selection must
// not call SetUpdateChannel at all.
func TestUpdateChannelScreenEnterWithNoPendingChangeIsANoop(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{updateChannel: "stable"}
	env := screenEnv{Client: adminClient, Session: AdminSession{BearerToken: "token"}, Layout: consoleLayout{SectionRows: 20}}

	loaded, _, _ := newUpdateChannelScreen().Update(env, adminUpdateChannelLoadedMsg{channel: "stable"})
	screen := loaded.(updateChannelScreen)

	_, cmd, consumed := screen.Update(env, keyMsgFor("enter"))
	if !consumed {
		t.Fatal("Enter was not consumed")
	}
	if cmd != nil {
		t.Fatal("Enter with no pending change returned a non-nil Cmd, want nil")
	}
	if adminClient.updateUpdateChannelCalls != 0 {
		t.Fatalf("SetUpdateChannel call count = %d, want 0 -- nothing was changed", adminClient.updateUpdateChannelCalls)
	}
}

// TestUpdateChannelScreenReflectsSaveError guards the failure path: a save
// error surfaces in View() and leaves saving cleared, mirroring every other
// config screen's own error-display convention (theme.error.Render(s.err)).
func TestUpdateChannelScreenReflectsSaveError(t *testing.T) {
	t.Parallel()

	env := screenEnv{Layout: consoleLayout{SectionRows: 20}}
	screen := updateChannelScreen{loaded: true, current: "stable", selected: "insider", saving: true}

	updated, _, _ := screen.Update(env, adminUpdateChannelUpdatedMsg{err: errors.New("admin session expired")})
	next := updated.(updateChannelScreen)
	if next.saving {
		t.Fatal("saving = true after a failed save, want false")
	}
	if next.current != "stable" {
		t.Fatalf("current = %q after a failed save, want the pre-save value %q unchanged", next.current, "stable")
	}

	view := next.View(newAdminTheme(), env).Body
	if !strings.Contains(view, "admin session expired") {
		t.Fatalf("view = %q, want it to surface the save error", view)
	}
}

// keyMsgFor mirrors runKey's own tea.KeyMsg construction (model_test.go)
// for the two keys this screen's own tests need, operating directly on a
// screen struct rather than a full Model.
func keyMsgFor(s string) tea.KeyMsg {
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}
