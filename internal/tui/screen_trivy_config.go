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

// trivyConfigScreen is "Image scanning", the Config tab of Trivy (the approved
// "Image scanning" and "Config modal" boards): the global configuration, the
// scan policy and the per-repository overrides, with the configuration and
// policy editors as modals. Its other tab, Repositories, is trivyReposScreen.
//
// The field names confirm, cfg and policyModal are read by Model
// (migratedScreenCapturesTextInput): do not rename them.
type trivyConfigScreen struct {
	securityFeature
	cfg          trivyConfigModal
	policy       ports.ScanPolicySettings
	policyLoaded bool
	policyErr    string
	policyModal  scanPolicyModal
	// saving is true from the Enter that saved the configuration or the policy
	// until the result arrives: Enter is ignored meanwhile.
	saving bool
}

func newTrivyConfigScreen() trivyConfigScreen {
	return trivyConfigScreen{securityFeature: newSecurityFeature(trivyFeatureName)}
}

func (s trivyConfigScreen) ID() screen { return screenSecurityTrivy }

func (s trivyConfigScreen) Keys() screenKeys {
	if s.cfg.Active() || s.policyModal.Active() {
		return securityModalKeys
	}
	short := []key.Binding{
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch tabs")),
		key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "configure")),
		key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "policy")),
		key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "overrides")),
	}
	short = append(short, featureActionKeyBindings(s.page)...)
	short = append(short,
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	)
	return screenKeys{short: short}
}

func (s trivyConfigScreen) Init(env screenEnv) tea.Cmd { return s.load(env) }

func (s trivyConfigScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	if key, ok := msg.(keyMsg); ok {
		switch {
		case s.confirm.Active():
			f, cmd := s.updateConfirm(env, key)
			s.securityFeature = f
			return s, cmd, true
		case s.cfg.Active():
			return s.updateConfigKey(env, key)
		case s.policyModal.Active():
			return s.updatePolicyKey(env, key)
		}
		return s.updateKey(env, key)
	}
	switch typed := msg.(type) {
	case adminFeaturePageLoadedMsg:
		f, ok := s.applyPage(typed)
		if !ok {
			return s, nil, false
		}
		s.securityFeature = f
		if typed.err != nil {
			return s, nil, false
		}
		return s, loadScanPolicyCmd(env), false
	case featureOverridesLoadedMsg:
		s.securityFeature = s.applyOverrides(env, typed)
		return s, nil, false
	case adminFeatureActionCompletedMsg:
		f, reload := s.applyActionResult(typed)
		s.securityFeature = f
		if reload {
			return s, loadFeaturePageCmd(env, trivyFeatureName), false
		}
		return s, nil, false
	case adminFeatureConfiguredMsg:
		if typed.name != trivyFeatureName || !s.saving {
			return s, nil, false
		}
		s.saving = false
		if typed.err != nil {
			s.showSaveError(typed.err)
			return s, nil, false
		}
		s.cfg = trivyConfigModal{}
		s.err, s.note = "", "Configuration saved."
		return s, loadFeaturePageCmd(env, trivyFeatureName), false
	case adminFeatureRuntimeMutatedMsg:
		if typed.name != trivyFeatureName || typed.err != nil {
			return s, nil, false
		}
		return s, loadFeaturePageCmd(env, trivyFeatureName), false
	case adminScanPolicyLoadedMsg:
		if typed.err != nil {
			s.policyErr = typed.err.Error()
			return s, s.loadOverrides(env), false
		}
		s.policy, s.policyLoaded, s.policyErr = typed.settings, true, ""
		return s, s.loadOverrides(env), false
	case adminScanPolicyUpdatedMsg:
		if !s.saving {
			return s, nil, false
		}
		s.saving = false
		if typed.err != nil {
			s.showSaveError(typed.err)
			return s, nil, false
		}
		s.policy, s.policyLoaded, s.policyErr = typed.settings, true, ""
		s.policyModal = scanPolicyModal{}
		s.err, s.note = "", "Vulnerability policy saved."
		return s, nil, false
	}
	return s, nil, false
}

// showSaveError puts a failed save in the modal that is still open, else on the
// screen.
func (s *trivyConfigScreen) showSaveError(err error) {
	switch {
	case s.cfg.Active():
		s.cfg.Error = err.Error()
	case s.policyModal.Active():
		s.policyModal.Error = err.Error()
	default:
		s.err = err.Error()
	}
}

func (s trivyConfigScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	s.note = ""
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminOverview), true
	case isTabKey(msg), isRuneKey(msg, 'o'):
		return s, navigate(screenSecurityTrivyRepos), true
	case isRuneKey(msg, 'c'):
		modal, ok := trivyConfigModalFromPage(s.page)
		if !ok {
			s.err = "Current Trivy configuration is unavailable."
			return s, nil, true
		}
		s.cfg = modal
		s.err = ""
		return s, nil, true
	case isRuneKey(msg, 'p'):
		if !s.policyLoaded {
			// Saving a policy that never loaded would overwrite the real one
			// with defaults.
			s.err = "The scan policy has not loaded yet. Press r to try again."
			return s, nil, true
		}
		threshold := s.policy.SeverityThreshold
		if strings.TrimSpace(threshold) == "" {
			threshold = ports.ScanPolicyThresholdCritical
		}
		s.policyModal = scanPolicyModal{Open: true, Focus: scanPolicyFieldEnabled, Enabled: s.policy.Enabled, SeverityThreshold: threshold}
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

func (s trivyConfigScreen) updateConfigKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		s.cfg = trivyConfigModal{}
		return s, nil, true
	case isTabKey(msg), isArrowDownKey(msg):
		s.cfg.Focus = nextTrivyConfigField(s.cfg.Focus)
		s.cfg.Error = ""
		return s, nil, true
	case isShiftTabKey(msg), isArrowUpKey(msg):
		s.cfg.Focus = previousTrivyConfigField(s.cfg.Focus)
		s.cfg.Error = ""
		return s, nil, true
	case isEnterKey(msg):
		if s.saving {
			return s, nil, true
		}
		input, err := trivyConfigInputFromModal(s.cfg)
		if err != nil {
			s.cfg.Error = err.Error()
			return s, nil, true
		}
		s.saving = true
		s.cfg.Error = ""
		return s, configureFeatureCmd(env, trivyFeatureName, input), true
	case isBackspaceKey(msg):
		s.editConfigField(func(value string) string { return trimLastRune(value) })
		return s, nil, true
	case isSpaceKey(msg) && s.cfg.Focus == trivyConfigFieldScheduleEnabled:
		s.cfg.ScheduleEnabled = !s.cfg.ScheduleEnabled
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
func (s *trivyConfigScreen) editConfigField(edit func(string) string) {
	switch s.cfg.Focus {
	case trivyConfigFieldInterval:
		s.cfg.Interval = edit(s.cfg.Interval)
	case trivyConfigFieldTimeout:
		s.cfg.Timeout = edit(s.cfg.Timeout)
	case trivyConfigFieldRegistryReachableURL:
		s.cfg.RegistryReachableURL = edit(s.cfg.RegistryReachableURL)
	case trivyConfigFieldMaxConcurrency:
		s.cfg.MaxConcurrency = edit(s.cfg.MaxConcurrency)
	default:
		return
	}
	s.cfg.Error = ""
}

func (s trivyConfigScreen) updatePolicyKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		s.policyModal = scanPolicyModal{}
		return s, nil, true
	case isTabKey(msg), isShiftTabKey(msg), isArrowDownKey(msg), isArrowUpKey(msg):
		s.policyModal.Focus = nextScanPolicyField(s.policyModal.Focus)
		s.policyModal.Error = ""
		return s, nil, true
	case isSpaceKey(msg):
		switch s.policyModal.Focus {
		case scanPolicyFieldEnabled:
			s.policyModal.Enabled = !s.policyModal.Enabled
		case scanPolicyFieldThreshold:
			s.policyModal.SeverityThreshold = nextScanPolicyThreshold(s.policyModal.SeverityThreshold)
		}
		s.policyModal.Error = ""
		return s, nil, true
	case isEnterKey(msg):
		if s.saving {
			return s, nil, true
		}
		s.saving = true
		s.policyModal.Error = ""
		return s, updateScanPolicyCmd(env, ports.ScanPolicySettings{Enabled: s.policyModal.Enabled, SeverityThreshold: s.policyModal.SeverityThreshold}), true
	}
	return s, nil, true
}

// trivyConfigModalFromPage seeds the configuration modal from the loaded page.
func trivyConfigModalFromPage(page ports.FeaturePage) (trivyConfigModal, bool) {
	if page.Summary.Name != trivyFeatureName {
		return trivyConfigModal{}, false
	}
	modal := trivyConfigModal{Open: true, Focus: trivyConfigFieldScheduleEnabled}
	config := securityPageFields(page, "config")
	modal.ScheduleEnabled, _ = strconv.ParseBool(config["Schedule Enabled"])
	modal.Interval = securityDuration(config["Interval"])
	modal.Timeout = securityDuration(config["Timeout"])
	modal.RegistryReachableURL = config["Registry Reachable URL"]
	modal.MaxConcurrency = config["Max Concurrency"]
	if modal.Interval == "" || modal.Timeout == "" || modal.MaxConcurrency == "" {
		return trivyConfigModal{}, false
	}
	return modal, true
}

func nextTrivyConfigField(field trivyConfigField) trivyConfigField {
	if field >= trivyConfigFieldMaxConcurrency {
		return trivyConfigFieldScheduleEnabled
	}
	return field + 1
}

func previousTrivyConfigField(field trivyConfigField) trivyConfigField {
	if field <= trivyConfigFieldScheduleEnabled {
		return trivyConfigFieldMaxConcurrency
	}
	return field - 1
}

// trivyConfigInputFromModal validates the modal's fields into the update the
// backend takes.
func trivyConfigInputFromModal(modal trivyConfigModal) (ports.FeatureConfigureInput, error) {
	interval, err := time.ParseDuration(strings.TrimSpace(modal.Interval))
	if err != nil {
		return ports.FeatureConfigureInput{}, fmt.Errorf("invalid interval: %w", err)
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(modal.Timeout))
	if err != nil {
		return ports.FeatureConfigureInput{}, fmt.Errorf("invalid timeout: %w", err)
	}
	maxConcurrency, err := strconv.Atoi(strings.TrimSpace(modal.MaxConcurrency))
	if err != nil {
		return ports.FeatureConfigureInput{}, fmt.Errorf("invalid max concurrency: %w", err)
	}
	registryURL := strings.TrimSpace(modal.RegistryReachableURL)
	scheduleEnabled := modal.ScheduleEnabled
	return ports.FeatureConfigureInput{
		ScheduleEnabled:      &scheduleEnabled,
		Interval:             &interval,
		Timeout:              &timeout,
		RegistryReachableURL: &registryURL,
		MaxConcurrency:       &maxConcurrency,
	}, nil
}
