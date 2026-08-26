package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	bubbletable "github.com/evertras/bubble-table/table"
	"regixtry/internal/ports"
)

// trivyConfigScreen is Trivy's Runtime tab, promoted to its own top-level
// screen (Phase 11, design.md Decision I: "Tab becomes a screen switch").
// Per the resolved-gap addendum (design.md, State Migration section), it
// independently owns its own `page` (loaded via loadFeaturePageCmd scoped to
// trivyFeatureName) and the enable/disable/install/upgrade/rollback action
// dispatch (featureActionForKey/featureActionKeyBindings), which used to
// live in the shared updateAdminFeaturesKey/adminFeatureHelp -- both now
// deleted.
type trivyConfigScreen struct {
	page        ports.FeaturePage
	loaded      bool
	rows        map[string]bubbletable.Model
	cfg         trivyConfigModal
	policy      ports.ScanPolicySettings
	policyModal scanPolicyModal
	// confirm is this screen's own confirm-before-destructive-action prompt
	// (design.md Decision E), embedded locally rather than read through the
	// shared AdminViewState.Confirm field: a migrated screen cannot write to
	// AdminViewState directly (Decision B), so each screen that opens a
	// confirm owns one, mirroring featureOverridesScreen's own
	// self-contained editor field.
	confirm confirmPrompt
	// submitting is the transient "Submitting X for Y..." status text shown
	// while confirm's dispatched command is in flight, surfaced through this
	// screen's own View (Body) the instant Enter dispatches onConfirm --
	// mirroring the legacy AdminViewState.Confirm path's
	// `m.status = before.submitting` (model.go's updateAdminKey), which this
	// migrated screen cannot reach into per design.md Decision B.
	submitting string
	err        string
}

func newTrivyConfigScreen() trivyConfigScreen { return trivyConfigScreen{} }

func (s trivyConfigScreen) ID() screen { return screenSecurityTrivy }

func (s trivyConfigScreen) Keys() screenKeys {
	short := []key.Binding{
		key.NewBinding(key.WithKeys("enter", "r"), key.WithHelp("Enter/r", "refresh page")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "switch tabs")),
		key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "configure")),
		key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "policy")),
	}
	short = append(short, featureActionKeyBindings(s.page)...)
	short = append(short,
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "back")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	)
	return screenKeys{short: short}
}

func (s trivyConfigScreen) Init(env screenEnv) tea.Cmd {
	return loadFeaturePageCmd(env, trivyFeatureName)
}

func (s trivyConfigScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch {
		case s.confirm.Active():
			before := s.confirm
			next, cmd, consumed := s.confirm.update(env, key)
			s.confirm = next
			switch {
			case cmd != nil:
				s.submitting = before.submitting
			case isEscKey(key):
				s.submitting = ""
			}
			return s, cmd, consumed
		case s.cfg.Active():
			return s.updateConfigKey(env, key)
		case s.policyModal.Active():
			return s.updatePolicyKey(env, key)
		}
		return s.updateKey(env, key)
	}
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		s.rebuildRows(env)
		return s, nil, false
	case adminFeaturePageLoadedMsg:
		if typed.feature != trivyFeatureName {
			return s, nil, false
		}
		if typed.err != nil {
			s.err = typed.err.Error()
			return s, nil, false
		}
		s.page = typed.page
		s.loaded = true
		s.err = ""
		s.rebuildRows(env)
		return s, loadScanPolicyCmd(env), false
	case adminFeatureActionCompletedMsg:
		if typed.feature != trivyFeatureName {
			return s, nil, false
		}
		s.confirm = confirmPrompt{}
		s.submitting = ""
		if typed.err != nil {
			s.err = typed.err.Error()
			return s, nil, false
		}
		s.err = ""
		return s, loadFeaturePageCmd(env, trivyFeatureName), false
	case adminFeatureConfiguredMsg:
		if typed.name != trivyFeatureName {
			return s, nil, false
		}
		if typed.err != nil {
			s.cfg.Error = typed.err.Error()
			return s, nil, false
		}
		s.cfg = trivyConfigModal{}
		return s, loadFeaturePageCmd(env, trivyFeatureName), false
	case adminFeatureRuntimeMutatedMsg:
		if typed.name != trivyFeatureName || typed.err != nil {
			return s, nil, false
		}
		return s, loadFeaturePageCmd(env, trivyFeatureName), false
	case adminScanPolicyLoadedMsg:
		if typed.err != nil {
			return s, nil, false
		}
		s.policy = typed.settings
		return s, nil, false
	case adminScanPolicyUpdatedMsg:
		if typed.err != nil {
			s.policyModal.Error = typed.err.Error()
			return s, nil, false
		}
		s.policy = typed.settings
		s.policyModal = scanPolicyModal{}
		return s, nil, false
	}
	return s, nil, false
}

func (s *trivyConfigScreen) rebuildRows(env screenEnv) {
	_, compact := tableRoles(env.Layout)
	rows := make(map[string]bubbletable.Model)
	for _, section := range s.page.Sections {
		if section.Kind != "rows" {
			continue
		}
		rows[section.ID] = buildAdminFeatureRowsTable(section, compact)
	}
	s.rows = rows
}

func (s trivyConfigScreen) updateKey(env screenEnv, msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminFeatures), true
	case isTabKey(msg):
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
		threshold := s.policy.SeverityThreshold
		if strings.TrimSpace(threshold) == "" {
			threshold = ports.ScanPolicyThresholdCritical
		}
		s.policyModal = scanPolicyModal{Open: true, Focus: scanPolicyFieldEnabled, Enabled: s.policy.Enabled, SeverityThreshold: threshold}
		return s, nil, true
	case isEnterKey(msg), isRuneKey(msg, 'r'):
		return s, loadFeaturePageCmd(env, trivyFeatureName), true
	}
	if action, ok := featureActionForKey(msg, s.page); ok {
		return s.dispatchAction(env, action)
	}
	return s, nil, true
}

// dispatchAction mirrors updateAdminFeaturesKey's own post-switch action
// dispatch exactly (model.go, now deleted): a declared ConfirmMessage opens
// this screen's own confirm prompt; otherwise the action runs immediately.
func (s trivyConfigScreen) dispatchAction(env screenEnv, action ports.FeatureAction) (adminScreen, tea.Cmd, bool) {
	if strings.TrimSpace(action.ConfirmMessage) != "" {
		var (
			submitting string
			onConfirm  func(screenEnv) tea.Cmd
		)
		if action.ID == "enable" || action.ID == "disable" {
			actionID := action.ID
			submitting = fmt.Sprintf("Submitting %s for %s...", actionID, trivyFeatureName)
			onConfirm = func(env screenEnv) tea.Cmd { return executeFeatureActionCmd(env, trivyFeatureName, actionID) }
		}
		s.confirm = newConfirmPrompt(adminFirstNonEmpty(action.ConfirmTitle, action.Label), action.ConfirmMessage, strings.ToLower(strings.TrimSpace(action.Label)), submitting, onConfirm)
		return s, nil, true
	}
	return s, executeFeatureActionCmd(env, trivyFeatureName, action.ID), true
}

func (s trivyConfigScreen) updateConfigKey(env screenEnv, msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		s.cfg = trivyConfigModal{}
		return s, nil, true
	case isTabKey(msg):
		s.cfg.Focus = nextTrivyConfigField(s.cfg.Focus)
		s.cfg.Error = ""
		return s, nil, true
	case isRuneKey(msg, ' '):
		if s.cfg.Focus == trivyConfigFieldScheduleEnabled {
			s.cfg.ScheduleEnabled = !s.cfg.ScheduleEnabled
			s.cfg.Error = ""
		}
		return s, nil, true
	case isBackspaceKey(msg):
		s.deleteConfigRune()
		s.cfg.Error = ""
		return s, nil, true
	case isEnterKey(msg):
		input, err := trivyConfigInputFromModal(s.cfg)
		if err != nil {
			s.cfg.Error = err.Error()
			return s, nil, true
		}
		return s, configureFeatureCmd(env, trivyFeatureName, input), true
	}
	if msg.Type == tea.KeyRunes {
		s.appendConfigRunes(string(msg.Runes))
		s.cfg.Error = ""
		return s, nil, true
	}
	return s, nil, true
}

func (s *trivyConfigScreen) deleteConfigRune() {
	switch s.cfg.Focus {
	case trivyConfigFieldInterval:
		s.cfg.Interval = trimLastRune(s.cfg.Interval)
	case trivyConfigFieldTimeout:
		s.cfg.Timeout = trimLastRune(s.cfg.Timeout)
	case trivyConfigFieldRegistryReachableURL:
		s.cfg.RegistryReachableURL = trimLastRune(s.cfg.RegistryReachableURL)
	case trivyConfigFieldMaxConcurrency:
		s.cfg.MaxConcurrency = trimLastRune(s.cfg.MaxConcurrency)
	}
}

func (s *trivyConfigScreen) appendConfigRunes(value string) {
	if value == "" {
		return
	}
	switch s.cfg.Focus {
	case trivyConfigFieldInterval:
		s.cfg.Interval += value
	case trivyConfigFieldTimeout:
		s.cfg.Timeout += value
	case trivyConfigFieldRegistryReachableURL:
		s.cfg.RegistryReachableURL += value
	case trivyConfigFieldMaxConcurrency:
		s.cfg.MaxConcurrency += value
	}
}

func (s trivyConfigScreen) updatePolicyKey(env screenEnv, msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		s.policyModal = scanPolicyModal{}
		return s, nil, true
	case isTabKey(msg):
		s.policyModal.Focus = nextScanPolicyField(s.policyModal.Focus)
		s.policyModal.Error = ""
		return s, nil, true
	case isRuneKey(msg, ' '):
		switch s.policyModal.Focus {
		case scanPolicyFieldEnabled:
			s.policyModal.Enabled = !s.policyModal.Enabled
		case scanPolicyFieldThreshold:
			s.policyModal.SeverityThreshold = nextScanPolicyThreshold(s.policyModal.SeverityThreshold)
		}
		s.policyModal.Error = ""
		return s, nil, true
	case isEnterKey(msg):
		return s, updateScanPolicyCmd(env, ports.ScanPolicySettings{Enabled: s.policyModal.Enabled, SeverityThreshold: s.policyModal.SeverityThreshold}), true
	}
	return s, nil, true
}

func (s trivyConfigScreen) View(theme adminTheme, env screenEnv) screenFrame {
	lines := []string{renderTrivyTabs(theme, screenSecurityTrivy, s.policy)}
	if s.err != "" {
		lines = append(lines, "", theme.error.Render(s.err))
	}
	if s.submitting != "" {
		lines = append(lines, "", theme.muted.Render(s.submitting))
	}
	lines = append(lines, "", theme.subheading.Render("Feature Page"))
	if !s.loaded {
		lines = append(lines, theme.muted.Render("Select or refresh a feature to load the backend-declared page."))
	} else {
		lines = append(lines, renderGenericFeaturePage(theme, s.page, s.rows)...)
	}
	lines = append(lines, renderAdminOperatorFooter(theme, env.Session, env.now())...)
	frame := screenFrame{
		Context: "Security & Compliance / Trivy",
		Body:    renderSection(theme, strings.Join(lines, "\n"), env.Layout),
	}
	switch {
	case s.confirm.Active():
		frame.Overlay = s.confirm.view(theme)
	case s.cfg.Active():
		frame.Overlay = renderTrivyConfigModal(theme, s.cfg)
	case s.policyModal.Active():
		frame.Overlay = renderScanPolicyModal(theme, s.policyModal)
	}
	return frame
}

// trivyConfigModalFromPage mirrors the retired Model method of the same
// name exactly, operating on a plain ports.FeaturePage instead of reading
// m.adminView.FeaturePage.
func trivyConfigModalFromPage(page ports.FeaturePage) (trivyConfigModal, bool) {
	if page.Summary.Name != trivyFeatureName {
		return trivyConfigModal{}, false
	}
	modal := trivyConfigModal{Open: true, Focus: trivyConfigFieldScheduleEnabled}
	for _, section := range page.Sections {
		if section.ID != "config" {
			continue
		}
		for _, field := range section.Fields {
			switch field.Label {
			case "Schedule Enabled":
				modal.ScheduleEnabled, _ = strconv.ParseBool(strings.TrimSpace(field.Value))
			case "Interval":
				modal.Interval = strings.TrimSpace(field.Value)
			case "Timeout":
				modal.Timeout = strings.TrimSpace(field.Value)
			case "Registry Reachable URL":
				modal.RegistryReachableURL = strings.TrimSpace(field.Value)
			case "Max Concurrency":
				modal.MaxConcurrency = strings.TrimSpace(field.Value)
			}
		}
	}
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

// trivyConfigInputFromModal mirrors the retired Model method of the same
// name exactly, operating on a plain trivyConfigModal value.
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
