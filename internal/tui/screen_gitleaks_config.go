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

// gitleaksConfigScreen was Slice 1's proof sub-model (design.md Decision G),
// mounted as an overlay on the still-legacy screenAdminFeatures through the
// Phase 12.3 follow-up batch. Phase 11 (the resolved-gap addendum) promotes
// it to a properly addressable top-level screen
// (screenSecurityGitleaksConfig): it now independently owns its own `page`
// (loaded via loadFeaturePageCmd scoped to gitleaksFeatureName) and the
// enable/disable/install/upgrade/rollback action dispatch
// (featureActionForKey/featureActionKeyBindings), mirroring
// trivyConfigScreen exactly at gitleaks' narrower scope (no tabs, no scan
// policy).
type gitleaksConfigScreen struct {
	page   ports.FeaturePage
	loaded bool
	rows   map[string]bubbletable.Model
	cfg    gitleaksConfigModal
	// confirm is this screen's own confirm-before-destructive-action prompt
	// (mirrors trivyConfigScreen.confirm's doc comment exactly).
	confirm confirmPrompt
	err     string
}

// newGitleaksConfigScreen constructs an unloaded screen; Init issues the
// FeaturePage load. Phase 11 deviation from Slice 1's own constructor
// (which took an already-seeded gitleaksConfigModal): now that this screen
// is top-level and long-lived rather than an overlay opened fresh from an
// already-loaded page, it loads its own page independently instead of being
// pre-seeded by its opener.
func newGitleaksConfigScreen() gitleaksConfigScreen { return gitleaksConfigScreen{} }

func (s gitleaksConfigScreen) ID() screen { return screenSecurityGitleaksConfig }

func (s gitleaksConfigScreen) Keys() screenKeys {
	if s.cfg.Active() {
		return gitleaksConfigModalKeys
	}
	short := []key.Binding{
		key.NewBinding(key.WithKeys("enter", "r"), key.WithHelp("Enter/r", "refresh page")),
		key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "configure")),
	}
	short = append(short, featureActionKeyBindings(s.page)...)
	short = append(short,
		key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "repository overrides")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "back")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	)
	return screenKeys{short: short}
}

func (s gitleaksConfigScreen) Init(env screenEnv) tea.Cmd {
	return loadFeaturePageCmd(env, gitleaksFeatureName)
}

func (s gitleaksConfigScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch {
		case s.confirm.Active():
			next, cmd, consumed := s.confirm.update(env, key)
			s.confirm = next
			return s, cmd, consumed
		case s.cfg.Active():
			return s.updateConfigKey(env, key)
		}
		return s.updateKey(env, key)
	}
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		s.rebuildRows(env)
		return s, nil, false
	case adminFeaturePageLoadedMsg:
		if typed.feature != gitleaksFeatureName {
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
		return s, nil, false
	case adminFeatureActionCompletedMsg:
		if typed.feature != gitleaksFeatureName {
			return s, nil, false
		}
		s.confirm = confirmPrompt{}
		if typed.err != nil {
			s.err = typed.err.Error()
			return s, nil, false
		}
		s.err = ""
		return s, loadFeaturePageCmd(env, gitleaksFeatureName), false
	case adminFeatureConfiguredMsg:
		if typed.name != gitleaksFeatureName {
			return s, nil, false
		}
		if typed.err != nil {
			s.cfg.Error = typed.err.Error()
			return s, nil, false
		}
		s.cfg = gitleaksConfigModal{}
		return s, loadFeaturePageCmd(env, gitleaksFeatureName), false
	case adminFeatureRuntimeMutatedMsg:
		if typed.name != gitleaksFeatureName || typed.err != nil {
			return s, nil, false
		}
		return s, loadFeaturePageCmd(env, gitleaksFeatureName), false
	}
	return s, nil, false
}

func (s *gitleaksConfigScreen) rebuildRows(env screenEnv) {
	_, compact := tableRoles(env.Layout)
	rows := make(map[string]bubbletable.Model)
	for _, section := range s.page.Sections {
		if section.Kind != "rows" {
			continue
		}
		rows[section.ID] = buildAdminFeatureRowsTable(newAdminTheme(), section, compact)
	}
	s.rows = rows
}

func (s gitleaksConfigScreen) updateKey(env screenEnv, msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminFeatures), true
	case isRuneKey(msg, 's'):
		modal, ok := gitleaksConfigModalFromPage(s.page)
		if !ok {
			s.err = "Current gitleaks configuration is unavailable."
			return s, nil, true
		}
		s.cfg = modal
		s.err = ""
		return s, nil, true
	case isRuneKey(msg, 'o'):
		// The actual fix for the originally reported defect (proposal
		// Intent): gitleaks' own discoverable override entry point,
		// reachable without ever entering Trivy's screen (spec.md
		// "Gitleaks Repository-Scoped Override Entry Point").
		return s, navigate(screenSecurityGitleaksRepos), true
	case isEnterKey(msg), isRuneKey(msg, 'r'):
		return s, loadFeaturePageCmd(env, gitleaksFeatureName), true
	}
	if action, ok := featureActionForKey(msg, s.page); ok {
		return s.dispatchAction(env, action)
	}
	return s, nil, true
}

// dispatchAction mirrors trivyConfigScreen.dispatchAction exactly, scoped
// to gitleaksFeatureName.
func (s gitleaksConfigScreen) dispatchAction(env screenEnv, action ports.FeatureAction) (adminScreen, tea.Cmd, bool) {
	if strings.TrimSpace(action.ConfirmMessage) != "" {
		var (
			submitting string
			onConfirm  func(screenEnv) tea.Cmd
		)
		if action.ID == "enable" || action.ID == "disable" {
			actionID := action.ID
			submitting = fmt.Sprintf("Submitting %s for %s...", actionID, gitleaksFeatureName)
			onConfirm = func(env screenEnv) tea.Cmd { return executeFeatureActionCmd(env, gitleaksFeatureName, actionID) }
		}
		s.confirm = newConfirmPrompt(adminFirstNonEmpty(action.ConfirmTitle, action.Label), action.ConfirmMessage, strings.ToLower(strings.TrimSpace(action.Label)), submitting, onConfirm)
		return s, nil, true
	}
	return s, executeFeatureActionCmd(env, gitleaksFeatureName, action.ID), true
}

func (s gitleaksConfigScreen) updateConfigKey(env screenEnv, msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		s.cfg = gitleaksConfigModal{}
		return s, nil, true
	case isTabKey(msg):
		s.cfg.Focus = nextGitleaksConfigField(s.cfg.Focus)
		s.cfg.Error = ""
		return s, nil, true
	case isRuneKey(msg, ' '):
		if s.cfg.Focus == gitleaksConfigFieldEnabled {
			s.cfg.Enabled = !s.cfg.Enabled
			s.cfg.Error = ""
		}
		return s, nil, true
	case isBackspaceKey(msg):
		s.deleteRune()
		s.cfg.Error = ""
		return s, nil, true
	case isEnterKey(msg):
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

// gitleaksConfigModalFromPage mirrors the retired Model method of the same
// name exactly, operating on a plain ports.FeaturePage.
func gitleaksConfigModalFromPage(page ports.FeaturePage) (gitleaksConfigModal, bool) {
	if page.Summary.Name != gitleaksFeatureName {
		return gitleaksConfigModal{}, false
	}
	modal := gitleaksConfigModal{Open: true, Focus: gitleaksConfigFieldEnabled}
	for _, field := range page.Header {
		if field.Label == "Enabled" {
			modal.Enabled, _ = strconv.ParseBool(strings.TrimSpace(field.Value))
		}
	}
	for _, section := range page.Sections {
		if section.ID != "config" {
			continue
		}
		for _, field := range section.Fields {
			switch field.Label {
			case "Timeout":
				modal.Timeout = strings.TrimSpace(field.Value)
			case "Max Concurrency":
				modal.MaxConcurrency = strings.TrimSpace(field.Value)
			}
		}
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

// View renders this screen's base Feature Page body, plus the config modal
// as its Overlay when open -- reproduces renderGitleaksConfigModal's exact
// layout (T1.3's byte-identity proof), with the pre-change hand-written
// footer replaced by shortHelpView's keymap-generated one.
func (s gitleaksConfigScreen) View(theme adminTheme, env screenEnv) screenFrame {
	lines := []string{theme.subheading.Render("Feature Page")}
	if s.err != "" {
		lines = append(lines, "", theme.error.Render(s.err))
	}
	if !s.loaded {
		lines = append(lines, theme.muted.Render("Select or refresh a feature to load the backend-declared page."))
	} else {
		lines = append(lines, renderGenericFeaturePage(theme, s.page, s.rows)...)
	}
	lines = append(lines, renderAdminOperatorFooter(theme, env.Session, env.now())...)
	frame := screenFrame{
		Context: "Security & Compliance / Gitleaks",
		Body:    renderSection(theme, strings.Join(lines, "\n"), env.Layout),
	}
	switch {
	case s.confirm.Active():
		frame.Overlay = s.confirm.view(theme)
	case s.cfg.Active():
		frame.Overlay = s.renderConfigModal(theme)
	}
	return frame
}

func (s gitleaksConfigScreen) renderConfigModal(theme adminTheme) string {
	lines := []string{
		theme.subheading.Render("Edit Gitleaks Configuration"),
		renderToggleField(theme, "Enabled", s.cfg.Enabled, s.cfg.Focus == gitleaksConfigFieldEnabled),
		renderTextField(theme, "Timeout", s.cfg.Timeout, s.cfg.Focus == gitleaksConfigFieldTimeout),
		renderTextField(theme, "Max Concurrency", s.cfg.MaxConcurrency, s.cfg.Focus == gitleaksConfigFieldMaxConcurrency),
	}
	if strings.TrimSpace(s.cfg.Error) != "" {
		lines = append(lines, "", theme.error.Render(s.cfg.Error))
	}
	lines = append(lines, "", theme.muted.Render(shortHelpView(theme, gitleaksConfigModalKeys)))
	return theme.section.Render(strings.Join(lines, "\n"))
}
