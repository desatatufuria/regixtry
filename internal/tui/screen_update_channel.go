package tui

import (
	"context"
	"errors"
	"sync/atomic"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
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
	// gen identifies this instance. Every async message it produces carries
	// it and the model and Update drop a message with another gen, so a late
	// result can never land on (or log out from) a different instance.
	gen    uint64
	loaded bool
	// current is the value actually persisted server-side, as of the last
	// load/save. selected is the operator's pending pick -- they start
	// equal on load/save and diverge only while an unsaved toggle is
	// pending, exactly like scanPolicyModal's own Enabled field before a
	// PUT confirms it.
	current  string
	selected string
	saving   bool
	// saved is true from a successful save until the next toggle or reload,
	// so the screen can confirm what was persisted.
	saved bool
	err   string
}

// updateChannelGenerations hands out the unique id of each instance.
var updateChannelGenerations atomic.Uint64

func newUpdateChannelScreen() updateChannelScreen {
	return updateChannelScreen{gen: updateChannelGenerations.Add(1)}
}

// adminUpdateChannelLoadedMsg and adminUpdateChannelUpdatedMsg carry the
// result of the load and save commands, tagged with the issuing instance.
type adminUpdateChannelLoadedMsg struct {
	gen     uint64
	channel string
	err     error
}

type adminUpdateChannelUpdatedMsg struct {
	gen     uint64
	channel string
	err     error
}

func (m adminUpdateChannelLoadedMsg) expiredReason() (string, bool)  { return expiredReasonOf(m.err) }
func (m adminUpdateChannelUpdatedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

// updateChannelMsgGen returns the screen generation of an update-channel
// async message.
func updateChannelMsgGen(msg tea.Msg) (uint64, bool) {
	switch typed := msg.(type) {
	case adminUpdateChannelLoadedMsg:
		return typed.gen, true
	case adminUpdateChannelUpdatedMsg:
		return typed.gen, true
	}
	return 0, false
}

var errUpdateChannelUnavailable = errors.New("admin API is unavailable for this session")

func loadUpdateChannelCmd(env screenEnv, gen uint64) tea.Cmd {
	return func() tea.Msg {
		if env.Client == nil {
			return adminUpdateChannelLoadedMsg{gen: gen, err: errUpdateChannelUnavailable}
		}
		channel, err := env.Client.GetUpdateChannel(context.Background(), env.Session)
		return adminUpdateChannelLoadedMsg{gen: gen, channel: channel, err: err}
	}
}

func updateUpdateChannelCmd(env screenEnv, gen uint64, channel string) tea.Cmd {
	return func() tea.Msg {
		if env.Client == nil {
			return adminUpdateChannelUpdatedMsg{gen: gen, err: errUpdateChannelUnavailable}
		}
		persisted, err := env.Client.SetUpdateChannel(context.Background(), env.Session, channel)
		return adminUpdateChannelUpdatedMsg{gen: gen, channel: persisted, err: err}
	}
}

func (s updateChannelScreen) ID() screen { return screenAdminUpdateChannel }

func (s updateChannelScreen) Keys() screenKeys { return updateChannelKeys }

func (s updateChannelScreen) Init(env screenEnv) tea.Cmd {
	return loadUpdateChannelCmd(env, s.gen)
}

func (s updateChannelScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case adminUpdateChannelLoadedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		if typed.err != nil {
			s.err = typed.err.Error()
			return s, nil, false
		}
		channel := adminFirstNonEmpty(typed.channel, string(release.ChannelStable))
		s.current = channel
		s.selected = channel
		s.loaded = true
		s.saved = false
		s.err = ""
		return s, nil, false
	case adminUpdateChannelUpdatedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		s.saving = false
		if typed.err != nil {
			s.err = typed.err.Error()
			return s, nil, false
		}
		channel := adminFirstNonEmpty(typed.channel, s.selected)
		s.current = channel
		s.selected = channel
		s.saved = true
		s.err = ""
		return s, nil, false
	}
	return s, nil, false
}

func (s updateChannelScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminOverview), true
	case isSpaceKey(msg):
		if !s.loaded || s.saving {
			return s, nil, true
		}
		s.selected = otherUpdateChannel(s.selected)
		s.saved = false
		s.err = ""
		return s, nil, true
	case isEnterKey(msg):
		if !s.loaded || s.saving || s.selected == "" || s.selected == s.current {
			return s, nil, true
		}
		s.saving = true
		s.saved = false
		s.err = ""
		return s, updateUpdateChannelCmd(env, s.gen, s.selected), true
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
		return s, loadUpdateChannelCmd(env, s.gen), true
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

var updateChannelKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "toggle stable/insider")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
}}
