package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"regixtry/internal/infra/release"
)

// updateChannelScreen is Operations' third row (tui-update-check feature):
// the ONLY way to change the server-side update channel that gates every
// operator's pre-login banner check (GET /update-channel). A bare CLI flag
// was explicitly rejected during this feature's design -- it never proved
// the caller was an admin -- so this screen, reachable only through an
// authenticated admin session, is deliberately the sole write path
// (PUT /admin/v1/update-channel, admin-gated like every other admin/v1
// route). Deliberately no modal/overlay: unlike Trivy's multi-field config,
// this is a single two-value setting, so the picker lives directly on the
// screen body.
type updateChannelScreen struct {
	loaded bool
	// current is the value actually persisted server-side, as of the last
	// load/save. selected is the operator's pending pick -- they start
	// equal on load/save and diverge only while an unsaved toggle is
	// pending, exactly like scanPolicyModal's own Enabled field before a
	// PUT confirms it.
	current  string
	selected string
	saving   bool
	err      string
}

func newUpdateChannelScreen() updateChannelScreen { return updateChannelScreen{} }

func (s updateChannelScreen) ID() screen { return screenAdminUpdateChannel }

func (s updateChannelScreen) Keys() screenKeys { return updateChannelKeys }

func (s updateChannelScreen) Init(env screenEnv) tea.Cmd {
	return loadUpdateChannelCmd(env)
}

func (s updateChannelScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		return s.updateKey(env, typed)
	case adminUpdateChannelLoadedMsg:
		if typed.err != nil {
			s.err = typed.err.Error()
			return s, nil, false
		}
		channel := adminFirstNonEmpty(typed.channel, string(release.ChannelStable))
		s.current = channel
		s.selected = channel
		s.loaded = true
		s.err = ""
		return s, nil, false
	case adminUpdateChannelUpdatedMsg:
		s.saving = false
		if typed.err != nil {
			s.err = typed.err.Error()
			return s, nil, false
		}
		channel := adminFirstNonEmpty(typed.channel, s.selected)
		s.current = channel
		s.selected = channel
		s.err = ""
		return s, nil, false
	}
	return s, nil, false
}

func (s updateChannelScreen) updateKey(env screenEnv, msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminOperations), true
	case isRuneKey(msg, ' '):
		if !s.loaded || s.saving {
			return s, nil, true
		}
		s.selected = otherUpdateChannel(s.selected)
		s.err = ""
		return s, nil, true
	case isEnterKey(msg):
		if !s.loaded || s.saving || s.selected == "" || s.selected == s.current {
			return s, nil, true
		}
		s.saving = true
		s.err = ""
		return s, updateUpdateChannelCmd(env, s.selected), true
	case isRuneKey(msg, 'r'):
		// Only s.saving is guarded here, deliberately NOT !s.loaded: unlike
		// Space/Enter (which need a loaded value to toggle or compare
		// against), a refresh is exactly how the operator recovers from a
		// failed initial load -- s.loaded is set true only on
		// adminUpdateChannelLoadedMsg's success branch, and this screen is
		// never re-Init'd on a later visit (navigateMsg only Inits a slot
		// the first time it's mounted), so guarding on !s.loaded here would
		// make a failed first load permanently unrecoverable via 'r' for
		// the rest of the admin session.
		if s.saving {
			return s, nil, true
		}
		return s, loadUpdateChannelCmd(env), true
	}
	return s, nil, true
}

// otherUpdateChannel flips between the two valid channel values -- never a
// third state, mirroring nextScanPolicyThreshold's own closed-cycle shape
// for a fixed enum.
func otherUpdateChannel(current string) string {
	if current == string(release.ChannelInsider) {
		return string(release.ChannelStable)
	}
	return string(release.ChannelInsider)
}

func (s updateChannelScreen) View(theme adminTheme, env screenEnv) screenFrame {
	lines := []string{theme.subheading.Render("Update Channel")}
	switch {
	case s.err != "":
		lines = append(lines, theme.error.Render(s.err))
	case !s.loaded:
		lines = append(lines, theme.muted.Render("Loading update channel..."))
	default:
		lines = append(lines, fmt.Sprintf("Current: %s", s.current))
		selectedLine := fmt.Sprintf("Selected: %s", s.selected)
		if s.selected != s.current {
			selectedLine += " (unsaved -- press Enter to save)"
		}
		lines = append(lines, selectedLine)
		if s.saving {
			lines = append(lines, theme.muted.Render("Saving..."))
		}
	}
	lines = append(lines, renderAdminOperatorFooter(theme, env.Session, env.now())...)
	return screenFrame{
		Context: "Operations / Update Channel",
		Body:    renderSection(theme, strings.Join(lines, "\n"), env.Layout),
	}
}

var updateChannelKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys(" "), key.WithHelp("Space", "toggle stable/insider")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "save")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "back")),
	key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
}}
