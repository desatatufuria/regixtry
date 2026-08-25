package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"regixtry/internal/ports"
)

// gitleaksConfigScreen is Slice 1's proof sub-model (design.md Decision G):
// gitleaks' own global config editor, moved off
// AdminViewState.GitleaksConfigModal verbatim in behavior --
// updateGitleaksConfigModalKey (model.go) and renderGitleaksConfigModal
// (admin_views.go) both move here. It is mounted into
// Model.adminScreens[slotGitleaksConfig] only while open; the router clears
// the slot back to nil on close (Esc or a successful submit).
type gitleaksConfigScreen struct {
	cfg gitleaksConfigModal
}

// newGitleaksConfigScreen constructs the screen from an already-seeded
// gitleaksConfigModal (gitleaksConfigModalFromPage's return value) --
// mirrors the pre-change opener's own construction exactly.
func newGitleaksConfigScreen(cfg gitleaksConfigModal) gitleaksConfigScreen {
	return gitleaksConfigScreen{cfg: cfg}
}

// ID identifies the screen this overlay is nested within (design.md
// Interfaces section). gitleaksConfigScreen is not addressed by screen
// identity via slotFor (it is an overlay, not a top-level screen — see
// slotFor's doc comment); ID() exists for adminScreen conformance and for
// any future caller that needs to know which base screen this overlay sits
// on top of.
func (s gitleaksConfigScreen) ID() screen { return screenAdminFeatures }

func (s gitleaksConfigScreen) Keys() screenKeys { return gitleaksConfigKeys }

// Init has nothing to load: the modal is always seeded synchronously from
// the already-loaded FeaturePage at construction time (gitleaksConfigModalFromPage),
// exactly like the pre-change opener.
func (s gitleaksConfigScreen) Init(env screenEnv) tea.Cmd { return nil }

// Update reproduces updateGitleaksConfigModalKey's exact behavior. Returning
// nil (as an adminScreen) signals "closed" — the router replaces the slot
// with it, which naturally un-mounts this overlay.
func (s gitleaksConfigScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		return s.updateKey(env, typed)
	case adminFeatureConfiguredMsg:
		// Broadcast arm (design.md Decision H): only react to a result for
		// gitleaks specifically -- a Trivy configure completing must not
		// touch this screen if it somehow stayed mounted.
		if typed.name != gitleaksFeatureName {
			return s, nil, false
		}
		if typed.err != nil {
			s.cfg.Error = typed.err.Error()
			return s, nil, false
		}
		return nil, nil, false
	}
	return s, nil, false
}

func (s gitleaksConfigScreen) updateKey(env screenEnv, msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	keys := s.Keys()
	switch {
	case keys.matches(msg, "Esc"):
		return nil, nil, true
	case keys.matches(msg, "Tab"):
		s.cfg.Focus = nextGitleaksConfigField(s.cfg.Focus)
		s.cfg.Error = ""
		return s, nil, true
	case keys.matches(msg, "Space"):
		if s.cfg.Focus == gitleaksConfigFieldEnabled {
			s.cfg.Enabled = !s.cfg.Enabled
			s.cfg.Error = ""
		}
		return s, nil, true
	case isBackspaceKey(msg):
		s.deleteRune()
		s.cfg.Error = ""
		return s, nil, true
	case keys.matches(msg, "Enter"):
		input, err := s.inputFromModal()
		if err != nil {
			s.cfg.Error = err.Error()
			return s, nil, true
		}
		return s, configureFeatureCmd(env, gitleaksFeatureName, input), true
	}
	if msg.Type == tea.KeyRunes {
		s.appendRunes(string(msg.Runes))
		s.cfg.Error = ""
		return s, nil, true
	}
	return s, nil, true
}

// deleteRune/appendRunes mirror model.go's retired
// deleteGitleaksConfigModalRune/appendGitleaksConfigModalRunes exactly,
// operating on this screen's own cfg instead of a Model field.
func (s *gitleaksConfigScreen) deleteRune() {
	switch s.cfg.Focus {
	case gitleaksConfigFieldTimeout:
		s.cfg.Timeout = trimLastRune(s.cfg.Timeout)
	case gitleaksConfigFieldMaxConcurrency:
		s.cfg.MaxConcurrency = trimLastRune(s.cfg.MaxConcurrency)
	}
}

func (s *gitleaksConfigScreen) appendRunes(value string) {
	if value == "" {
		return
	}
	switch s.cfg.Focus {
	case gitleaksConfigFieldTimeout:
		s.cfg.Timeout += value
	case gitleaksConfigFieldMaxConcurrency:
		s.cfg.MaxConcurrency += value
	}
}

// configureFeatureCmd is the screenEnv-scoped equivalent of Model's own
// configureFeatureCmd method, needed because a sub-model has no Model to
// call a method on -- built from env.asModel() so the exact same
// AdminClient.ConfigureFeature call/message shape (adminFeatureConfiguredMsg)
// is reused verbatim.
func configureFeatureCmd(env screenEnv, name string, input ports.FeatureConfigureInput) tea.Cmd {
	return env.asModel().configureFeatureCmd(name, input)
}

// inputFromModal mirrors model.go's retired gitleaksConfigInputFromModal.
func (s gitleaksConfigScreen) inputFromModal() (ports.FeatureConfigureInput, error) {
	timeout, err := time.ParseDuration(strings.TrimSpace(s.cfg.Timeout))
	if err != nil {
		return ports.FeatureConfigureInput{}, fmt.Errorf("invalid timeout: %w", err)
	}
	maxConcurrency, err := strconv.Atoi(strings.TrimSpace(s.cfg.MaxConcurrency))
	if err != nil {
		return ports.FeatureConfigureInput{}, fmt.Errorf("invalid max concurrency: %w", err)
	}
	enabled := s.cfg.Enabled
	return ports.FeatureConfigureInput{
		Enabled:        &enabled,
		Timeout:        &timeout,
		MaxConcurrency: &maxConcurrency,
	}, nil
}

// View reproduces renderGitleaksConfigModal's exact layout, with the
// pre-change hand-written footer replaced by shortHelpView's
// keymap-generated one -- T1.3 proves the two are byte-identical.
func (s gitleaksConfigScreen) View(theme adminTheme, env screenEnv) screenFrame {
	lines := []string{
		theme.subheading.Render("Edit Gitleaks Configuration"),
		renderToggleField(theme, "Enabled", s.cfg.Enabled, s.cfg.Focus == gitleaksConfigFieldEnabled),
		renderTextField(theme, "Timeout", s.cfg.Timeout, s.cfg.Focus == gitleaksConfigFieldTimeout),
		renderTextField(theme, "Max Concurrency", s.cfg.MaxConcurrency, s.cfg.Focus == gitleaksConfigFieldMaxConcurrency),
	}
	if strings.TrimSpace(s.cfg.Error) != "" {
		lines = append(lines, "", theme.error.Render(s.cfg.Error))
	}
	lines = append(lines, "", theme.muted.Render(shortHelpView(theme, s.Keys())))
	return screenFrame{Overlay: theme.section.Render(strings.Join(lines, "\n"))}
}
