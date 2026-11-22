package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// gitleaksConfigScreen is "Secret scanning" (the approved "Secret scanning"
// and "Config modal" boards): the global Gitleaks configuration and the
// per-repository overrides, with the configuration editor as a modal.
//
// The field names confirm and cfg are read by Model
// (migratedScreenCapturesTextInput): do not rename them.
type gitleaksConfigScreen struct {
	securityFeature
	cfg gitleaksConfigModal
	// saving is true from the Enter that saved the configuration until the
	// result arrives: Enter is ignored meanwhile.
	saving bool
}

func newGitleaksConfigScreen() gitleaksConfigScreen {
	return gitleaksConfigScreen{securityFeature: newSecurityFeature(gitleaksFeatureName)}
}

func (s gitleaksConfigScreen) ID() screen { return screenSecurityGitleaksConfig }

func (s gitleaksConfigScreen) Keys() screenKeys {
	if s.cfg.Active() {
		return securityModalKeys
	}
	short := []key.Binding{
		key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "configure")),
		key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "overrides")),
	}
	short = append(short, featureActionKeyBindings(s.page)...)
	short = append(short,
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	)
	return screenKeys{short: short}
}

func (s gitleaksConfigScreen) Init(env screenEnv) tea.Cmd { return s.load(env) }

func (s gitleaksConfigScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	if key, ok := msg.(keyMsg); ok {
		switch {
		case s.confirm.Active():
			f, cmd := s.updateConfirm(env, key)
			s.securityFeature = f
			return s, cmd, true
		case s.cfg.Active():
			return s.updateConfigKey(env, key)
		}
		return s.updateKey(env, key)
	}
	if owned, ok := msg.(securityMsg); ok && owned.securityGen() != s.gen {
		return s, nil, false
	}
	switch typed := msg.(type) {
	case securityPageMsg:
		s.securityFeature = s.applyPage(typed)
		return s, s.loadOverrides(env), false
	case securityOverridesMsg:
		s.securityFeature = s.applyOverrides(env, typed)
		return s, nil, false
	case securityActionMsg:
		f, reload := s.applyActionResult(typed)
		s.securityFeature = f
		if reload {
			return s, s.load(env), false
		}
		return s, nil, false
	case securityConfigureMsg:
		if !s.saving {
			return s, nil, false
		}
		s.saving = false
		if typed.err != nil {
			if s.cfg.Active() {
				s.cfg.Error = typed.err.Error()
			} else {
				s.err = typed.err.Error()
			}
			return s, nil, false
		}
		s.cfg = gitleaksConfigModal{}
		s.err, s.note = "", "Configuration saved."
		return s, s.load(env), false
	}
	return s, nil, false
}

func (s gitleaksConfigScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if isPasteKey(msg) {
		// Pasted text is never a command: its first character must not run an
		// action or open an editor.
		return s, nil, true
	}
	s.note = ""
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminOverview), true
	case isRuneKey(msg, 'o'):
		return s, navigate(screenSecurityGitleaksRepos), true
	case isRuneKey(msg, 'c'):
		modal, ok := gitleaksConfigModalFromPage(s.page)
		if !ok {
			s.err = "Current gitleaks configuration is unavailable."
			return s, nil, true
		}
		s.cfg = modal
		s.err = ""
		return s, nil, true
	case isRuneKey(msg, 'r'), isEnterKey(msg):
		s.err = ""
		return s, s.load(env), true
	}
	f, cmd, _ := s.actionKey(env, msg)
	s.securityFeature = f
	return s, cmd, true
}

func (s gitleaksConfigScreen) updateConfigKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		s.cfg = gitleaksConfigModal{}
		return s, nil, true
	case isTabKey(msg), isArrowDownKey(msg):
		s.cfg.Focus = nextGitleaksConfigField(s.cfg.Focus)
		s.cfg.Error = ""
		return s, nil, true
	case isShiftTabKey(msg), isArrowUpKey(msg):
		s.cfg.Focus = previousGitleaksConfigField(s.cfg.Focus)
		s.cfg.Error = ""
		return s, nil, true
	case isEnterKey(msg):
		if s.saving {
			return s, nil, true
		}
		input, err := gitleaksConfigInputFromModal(s.cfg)
		if err != nil {
			s.cfg.Error = err.Error()
			return s, nil, true
		}
		s.saving = true
		s.cfg.Error = ""
		return s, securityConfigureCmd(env, s.gen, gitleaksFeatureName, input), true
	case isBackspaceKey(msg):
		s.editConfigField(func(value string) string { return trimLastRune(value) })
		return s, nil, true
	case isSpaceKey(msg) && s.cfg.Focus == gitleaksConfigFieldEnabled:
		s.cfg.Enabled = !s.cfg.Enabled
		s.cfg.Error = ""
		return s, nil, true
	case isTextKey(msg):
		text := printableText(keyText(msg))
		s.editConfigField(func(value string) string { return value + text })
		return s, nil, true
	}
	return s, nil, true
}

// editConfigField applies edit to the focused text field; the toggle takes no
// text.
func (s *gitleaksConfigScreen) editConfigField(edit func(string) string) {
	switch s.cfg.Focus {
	case gitleaksConfigFieldTimeout:
		s.cfg.Timeout = edit(s.cfg.Timeout)
	case gitleaksConfigFieldMaxConcurrency:
		s.cfg.MaxConcurrency = edit(s.cfg.MaxConcurrency)
	default:
		return
	}
	s.cfg.Error = ""
}

// gitleaksConfigInputFromModal validates the modal's fields into the update the
// backend takes.
func gitleaksConfigInputFromModal(modal gitleaksConfigModal) (ports.FeatureConfigureInput, error) {
	timeout, err := time.ParseDuration(strings.TrimSpace(modal.Timeout))
	if err != nil {
		return ports.FeatureConfigureInput{}, fmt.Errorf("invalid timeout: %w", err)
	}
	maxConcurrency, err := strconv.Atoi(strings.TrimSpace(modal.MaxConcurrency))
	if err != nil {
		return ports.FeatureConfigureInput{}, fmt.Errorf("invalid max concurrency: %w", err)
	}
	enabled := modal.Enabled
	return ports.FeatureConfigureInput{Enabled: &enabled, Timeout: &timeout, MaxConcurrency: &maxConcurrency}, nil
}

// gitleaksConfigModalFromPage seeds the configuration modal from the loaded
// page: the Enabled flag is the feature's own, the rest is the config section.
func gitleaksConfigModalFromPage(page ports.FeaturePage) (gitleaksConfigModal, bool) {
	if page.Summary.Name != gitleaksFeatureName {
		return gitleaksConfigModal{}, false
	}
	config := securityPageFields(page, "config")
	modal := gitleaksConfigModal{
		Open:           true,
		Focus:          gitleaksConfigFieldEnabled,
		Enabled:        page.Summary.Enabled,
		Timeout:        securityDuration(config["Timeout"]),
		MaxConcurrency: config["Max Concurrency"],
	}
	if modal.Timeout == "" || modal.MaxConcurrency == "" {
		return gitleaksConfigModal{}, false
	}
	return modal, true
}

func nextGitleaksConfigField(field gitleaksConfigField) gitleaksConfigField {
	if field >= gitleaksConfigFieldMaxConcurrency {
		return gitleaksConfigFieldEnabled
	}
	return field + 1
}

func previousGitleaksConfigField(field gitleaksConfigField) gitleaksConfigField {
	if field <= gitleaksConfigFieldEnabled {
		return gitleaksConfigFieldMaxConcurrency
	}
	return field - 1
}
