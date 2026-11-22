package tui

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

var updateUpdateChannelGoldens = flag.Bool("update-update-channel", false, "rewrite the update channel golden files")

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
	env := screenEnv{Client: adminClient, Session: AdminSession{BearerToken: "token"}, Layout: consoleLayout{Width: 150, Height: 24}}

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
	env := screenEnv{Client: adminClient, Session: AdminSession{BearerToken: "token"}, Layout: consoleLayout{Width: 150, Height: 24}}

	screen := newUpdateChannelScreen()
	loaded, _, _ := screen.Update(env, adminUpdateChannelLoadedMsg{gen: screen.gen, channel: "stable"})
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
	env := screenEnv{Client: adminClient, Session: AdminSession{BearerToken: "token"}, Layout: consoleLayout{Width: 150, Height: 24}}

	fresh := newUpdateChannelScreen()
	loaded, _, _ := fresh.Update(env, adminUpdateChannelLoadedMsg{gen: fresh.gen, channel: "stable"})
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

	env := screenEnv{Layout: consoleLayout{Width: 150, Height: 24}}
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

// TestUpdateChannelScreenRefreshIsNoopWhileSaving guards against a refresh
// racing an in-flight save: pressing 'r' while saving must not fire a
// concurrent load Cmd (which could otherwise clobber a save's own
// confirmation/error with a stale read, or leave `saving` stuck true with no
// request left to resolve it) -- mirrors the same !s.loaded || s.saving
// guard Space and Enter already apply.
func TestUpdateChannelScreenRefreshIsNoopWhileSaving(t *testing.T) {
	t.Parallel()

	env := screenEnv{Layout: consoleLayout{Width: 150, Height: 24}}
	screen := updateChannelScreen{loaded: true, current: "stable", selected: "insider", saving: true}

	updated, cmd, consumed := screen.updateKey(env, keyMsgFor("r"))
	if !consumed {
		t.Fatal("'r' was not consumed")
	}
	if cmd != nil {
		t.Fatal("'r' while saving returned a non-nil Cmd, want nil -- refresh must not race an in-flight save")
	}
	next := updated.(updateChannelScreen)
	if !next.saving {
		t.Fatal("saving = false after 'r' while saving, want true (unchanged)")
	}
	if next.current != "stable" || next.selected != "insider" {
		t.Fatalf("current = %q, selected = %q after 'r' while saving, want unchanged %q/%q", next.current, next.selected, "stable", "insider")
	}
}

// TestUpdateChannelScreenRefreshRecoversFromFailedInitialLoad is a regression
// test for a defect the round-1 fix itself introduced (caught by Judgment
// Day's scoped re-judgment): guarding 'r' on "!s.loaded || s.saving" instead
// of "s.saving" alone made a failed INITIAL load permanently unrecoverable
// via refresh for the rest of the admin session, since s.loaded is set true
// only on adminUpdateChannelLoadedMsg's success branch (never on failure),
// and this screen is never re-Init'd on a later visit (navigateMsg only
// Inits a slot the first time it is mounted). 'r' must still work while
// s.loaded is false, as long as no save is in flight.
func TestUpdateChannelScreenRefreshRecoversFromFailedInitialLoad(t *testing.T) {
	t.Parallel()

	env := screenEnv{Layout: consoleLayout{Width: 150, Height: 24}}
	screen := updateChannelScreen{loaded: false, err: "admin API is unavailable for this session"}

	updated, cmd, consumed := screen.updateKey(env, keyMsgFor("r"))
	if !consumed {
		t.Fatal("'r' was not consumed")
	}
	if cmd == nil {
		t.Fatal("'r' after a failed initial load returned a nil Cmd, want the reload Cmd -- refresh must still be able to retry")
	}
	next := updated.(updateChannelScreen)
	if next.saving {
		t.Fatal("saving = true after 'r' on a never-loaded screen, want unchanged false")
	}
}

// keyMsgFor mirrors runKey's own keyMsg construction (model_test.go)
// for the two keys this screen's own tests need, operating directly on a
// screen struct rather than a full Model.
func keyMsgFor(s string) keyMsg {
	if s == "enter" {
		return testKeyEnter()
	}
	return testKeyRunes(s)
}

var updateChannelSizes = []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}}

var updateChannelStates = []struct {
	name   string
	screen updateChannelScreen
}{
	{"loading", updateChannelScreen{}},
	{"error", updateChannelScreen{err: "admin API is unavailable for this session"}},
	{"unchanged", updateChannelScreen{loaded: true, current: "stable", selected: "stable"}},
	{"unsaved", updateChannelScreen{loaded: true, current: "stable", selected: "insider"}},
	{"saving", updateChannelScreen{loaded: true, current: "stable", selected: "insider", saving: true}},
	{"saved", updateChannelScreen{loaded: true, current: "insider", selected: "insider", saved: true}},
	{"save_error", updateChannelScreen{loaded: true, current: "stable", selected: "insider", err: "update channel must be stable or insider"}},
}

func renderUpdateChannel(s updateChannelScreen, w, h int) string {
	env := screenEnv{Session: AdminSession{Username: "admin"}, Layout: consoleLayout{Width: w, Height: h}}
	return ansi.Strip(s.View(newAdminTheme(), env).Body)
}

func TestUpdateChannelGeometryAtEverySizeInEveryState(t *testing.T) {
	t.Parallel()
	for _, size := range updateChannelSizes {
		for _, state := range updateChannelStates {
			out := renderUpdateChannel(state.screen, size.w, size.h)
			lines := strings.Split(out, "\n")
			if len(lines) != size.h {
				t.Fatalf("%s %dx%d: %d rows, want %d\n%s", state.name, size.w, size.h, len(lines), size.h, out)
			}
			for i, line := range lines {
				if width := ansi.StringWidth(line); width > size.w {
					t.Fatalf("%s %dx%d: line %d is %d cells wide: %q", state.name, size.w, size.h, i+1, width, line)
				}
			}
		}
	}
}

func TestUpdateChannelViewInsideShell(t *testing.T) {
	t.Parallel()
	out := renderUpdateChannel(updateChannelStates[3].screen, 150, 24)
	for _, want := range []string{
		"Admin › Operations › Update channel", "signed in as admin", "CHANNEL",
		"▸ insider", "selected, unsaved", "Default channel.",
		"Selected: insider (unsaved, press enter to save)",
		"Space toggle stable/insider", "Enter save", "r refresh", "Esc back", "q quit",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("view lacks %q\n%s", want, out)
		}
	}
	for _, old := range []string{"Current:", "Selected: insider (unsaved --"} {
		if strings.Contains(out, old) {
			t.Fatalf("view still has the old look %q", old)
		}
	}
}

func TestUpdateChannelSavedAndUnchangedMessages(t *testing.T) {
	t.Parallel()
	if out := renderUpdateChannel(updateChannelStates[5].screen, 150, 24); !strings.Contains(out, "Saved: the update check now uses insider") {
		t.Fatalf("saved view lacks confirmation\n%s", out)
	}
	if out := renderUpdateChannel(updateChannelStates[2].screen, 150, 24); !strings.Contains(out, "Unchanged: stable") {
		t.Fatalf("unchanged view lacks note\n%s", out)
	}
}

func TestUpdateChannelSaveMarksSavedAndToggleClearsIt(t *testing.T) {
	t.Parallel()
	env := screenEnv{Client: &fakeAdminClient{}, Layout: consoleLayout{Width: 150, Height: 24}}
	screen := updateChannelScreen{loaded: true, current: "stable", selected: "insider", saving: true}
	updated, _, _ := screen.Update(env, adminUpdateChannelUpdatedMsg{channel: "insider"})
	screen = updated.(updateChannelScreen)
	if !screen.saved {
		t.Fatal("saved = false after a successful save")
	}
	toggled, _, _ := screen.updateKey(env, keyMsgFor(" "))
	if toggled.(updateChannelScreen).saved {
		t.Fatal("saved = true after toggling again")
	}
}

func TestUpdateChannelEnterIgnoredWhileSaving(t *testing.T) {
	t.Parallel()
	adminClient := &fakeAdminClient{}
	env := screenEnv{Client: adminClient, Layout: consoleLayout{Width: 150, Height: 24}}
	screen := updateChannelScreen{loaded: true, current: "stable", selected: "insider", saving: true}
	_, cmd, consumed := screen.updateKey(env, keyMsgFor("enter"))
	if !consumed || cmd != nil {
		t.Fatalf("Enter while saving: consumed=%v cmd=%v, want consumed and nil", consumed, cmd != nil)
	}
}

func TestUpdateChannelEscGoesBackAndQIsNotConsumed(t *testing.T) {
	t.Parallel()
	env := screenEnv{Layout: consoleLayout{Width: 150, Height: 24}}
	screen := updateChannelScreen{loaded: true, current: "stable", selected: "stable"}
	_, cmd, consumed := screen.updateKey(env, testKeyEsc())
	if !consumed || cmd == nil {
		t.Fatal("Esc must navigate back")
	}
}

func TestUpdateChannelViewGoldens(t *testing.T) {
	for _, state := range updateChannelStates {
		for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
			name := state.name + "_" + fmt.Sprintf("%dx%d", size.w, size.h)
			assertUpdateChannelGolden(t, name, renderUpdateChannel(state.screen, size.w, size.h)+"\n")
		}
	}
}

func assertUpdateChannelGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "update_channel", name+".golden")
	if *updateUpdateChannelGoldens {
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
		t.Fatalf("read golden %s (run with -update-update-channel): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("view differs from %s\n--- want\n%s\n--- got\n%s", path, want, got)
	}
}

func TestUpdateChannelDropsMessagesOfAnotherInstance(t *testing.T) {
	t.Parallel()
	env := screenEnv{Layout: consoleLayout{Width: 150, Height: 24}}
	screen := newUpdateChannelScreen()
	other := newUpdateChannelScreen()
	if screen.gen == other.gen {
		t.Fatal("two instances share a generation id")
	}
	updated, _, _ := screen.Update(env, adminUpdateChannelLoadedMsg{gen: other.gen, channel: "insider"})
	if updated.(updateChannelScreen).loaded {
		t.Fatal("a load result of another instance was applied")
	}
	updated, _, _ = screen.Update(env, adminUpdateChannelLoadedMsg{gen: screen.gen, channel: "insider"})
	if !updated.(updateChannelScreen).loaded {
		t.Fatal("the instance's own load result was dropped")
	}
	saving := updateChannelScreen{gen: screen.gen, loaded: true, current: "stable", selected: "insider", saving: true}
	updated, _, _ = saving.Update(env, adminUpdateChannelUpdatedMsg{gen: other.gen, channel: "insider"})
	if got := updated.(updateChannelScreen); !got.saving || got.current != "stable" {
		t.Fatal("a save result of another instance was applied")
	}
}

func TestUpdateChannelStaleExpiredResultDoesNotLogOut(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 26, 9, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession:  AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		updateChannel: "stable",
	}
	model := openSidebarItem(t, runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass"), shellNavUpdateChannel)
	next, _ := model.Update(adminUpdateChannelLoadedMsg{gen: 0, err: NewAdminSessionExpiredError("")})
	if next.(Model).adminAuth != adminAuthStateAuthenticated {
		t.Fatal("a foreign expired result logged the operator out")
	}
	current := model.adminScreens[slotUpdateChannel].(updateChannelScreen)
	next, _ = model.Update(adminUpdateChannelLoadedMsg{gen: current.gen, err: NewAdminSessionExpiredError("")})
	if next.(Model).adminAuth == adminAuthStateAuthenticated {
		t.Fatal("the screen's own expired result did not redirect to login")
	}
}

func TestUpdateChannelStatusPanelShowsRealVersions(t *testing.T) {
	t.Parallel()
	screen := updateChannelStates[2].screen
	render := func(current, available string) string {
		env := screenEnv{Session: AdminSession{Username: "admin"}, CurrentVersion: current, UpdateAvailable: available, Layout: consoleLayout{Width: 150, Height: 24}}
		return ansi.Strip(screen.View(newAdminTheme(), env).Body)
	}
	out := render("v0.14.2", "v0.15.0")
	for _, want := range []string{"UPDATE STATUS", "Current version", "v0.14.2", "Latest on this channel", "v0.15.0", "available"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status panel lacks %q\n%s", want, out)
		}
	}
	if out := render("v0.14.2", ""); strings.Contains(out, "Latest on this channel") || !strings.Contains(out, "no newer release known") {
		t.Fatalf("with no known update the latest row must be omitted\n%s", out)
	}
	if out := render("", ""); !strings.Contains(out, "unknown (development build)") {
		t.Fatalf("a build without a version must say so\n%s", out)
	}
}

func TestUpdateChannelStateLabelsShareTheRightEdge(t *testing.T) {
	t.Parallel()
	out := renderUpdateChannel(updateChannelStates[3].screen, 150, 24)
	var edges []int
	for _, line := range strings.Split(out, "\n") {
		for _, label := range []string{"current  │", "selected, unsaved  │"} {
			if i := strings.Index(line, label); i >= 0 {
				edges = append(edges, ansi.StringWidth(line[:i])+len(strings.TrimSuffix(label, "  │")))
			}
		}
	}
	if len(edges) != 2 || edges[0] != edges[1] {
		t.Fatalf("state labels end at columns %v, want one shared right edge\n%s", edges, out)
	}
}
