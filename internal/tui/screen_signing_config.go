package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	bubbletable "github.com/evertras/bubble-table/table"
	"regixtry/internal/ports"
)

// signingConfigScreen was Phase 12.3's sub-model, mounted as an overlay on
// the still-legacy screenAdminFeatures. Phase 11 (the resolved-gap
// addendum) promotes it to a properly addressable top-level screen
// (screenSecuritySigningConfig), mirroring gitleaksConfigScreen exactly:
// it now independently owns its own `page` (loaded via loadFeaturePageCmd
// scoped to signingFeatureName) and the enable/disable/install/upgrade/
// rollback action dispatch.
//
// policy is now the full migrated AdminViewState.SigningPolicy (design.md's
// State Migration table, closing Phase 12.3's own disclosed deviation): the
// signing badge's only reader, renderAdminFeaturesScreen's Feature Page
// heading, no longer exists (Phase 11 deletes it -- securityMenuScreen is a
// bare peer list with no per-feature badge), so the second-reader conflict
// that justified keeping SigningPolicy on AdminViewState in the Phase 12.3
// follow-up batch no longer applies.
type signingConfigScreen struct {
	page   ports.FeaturePage
	loaded bool
	rows   map[string]bubbletable.Model
	cfg    signingPolicyModal
	policy ports.SigningPolicySettings
	// confirm is this screen's own confirm-before-destructive-action prompt
	// (mirrors trivyConfigScreen.confirm's doc comment exactly).
	confirm confirmPrompt
	// submitting is the transient "Submitting X for Y..." status text shown
	// while confirm's dispatched command is in flight, surfaced through this
	// screen's own View (Body) the instant Enter dispatches onConfirm --
	// mirrors trivyConfigScreen.submitting exactly (design.md Decision B).
	submitting string
	err        string
}

// newSigningConfigScreen constructs an unloaded screen; Init issues the
// FeaturePage load. Phase 11 deviation from Phase 12.3's own constructor
// (which took an already-seeded signingPolicyModal + baseline keys): now
// top-level and long-lived, it loads its own page/policy independently
// instead of being pre-seeded by its opener (mirrors
// newGitleaksConfigScreen's identical Phase 11 change).
func newSigningConfigScreen() signingConfigScreen { return signingConfigScreen{} }

func (s signingConfigScreen) ID() screen { return screenSecuritySigningConfig }

func (s signingConfigScreen) Keys() screenKeys {
	if s.cfg.Active() {
		return signingConfigModalKeys
	}
	short := []key.Binding{
		key.NewBinding(key.WithKeys("enter", "r"), key.WithHelp("Enter/r", "refresh page")),
		key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "policy")),
	}
	short = append(short, featureActionKeyBindings(s.page)...)
	short = append(short,
		key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "repository overrides")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "back")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	)
	return screenKeys{short: short}
}

func (s signingConfigScreen) Init(env screenEnv) tea.Cmd {
	return loadFeaturePageCmd(env, signingFeatureName)
}

func (s signingConfigScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
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
		}
		return s.updateKey(env, key)
	}
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		s.rebuildRows(env)
		return s, nil, false
	case adminFeaturePageLoadedMsg:
		if typed.feature != signingFeatureName {
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
		return s, loadSigningPolicyCmd(env), false
	case adminSigningPolicyLoadedMsg:
		if typed.err != nil {
			return s, nil, false
		}
		s.policy = typed.settings
		return s, nil, false
	case adminSigningPolicyUpdatedMsg:
		if typed.err != nil {
			s.cfg.Error = typed.err.Error()
			return s, nil, false
		}
		// Unlike scanPolicyModal, the modal stays open after a successful
		// save (design.md Decision 11 piece 1's growable key list): the
		// operator can keep adding keys.
		s.policy = typed.settings
		s.cfg.Enabled = typed.settings.Enabled
		s.cfg.UnsignedSelfRead = normalizeUnsignedSelfRead(typed.settings.UnsignedSelfRead)
		s.cfg.Fingerprints = signingKeyFingerprints(typed.settings.TrustedPublicKeys)
		s.cfg.AddKey = ""
		s.cfg.Error = ""
		return s, nil, false
	case adminFeatureActionCompletedMsg:
		if typed.feature != signingFeatureName {
			return s, nil, false
		}
		s.confirm = confirmPrompt{}
		s.submitting = ""
		if typed.err != nil {
			s.err = typed.err.Error()
			return s, nil, false
		}
		s.err = ""
		return s, loadFeaturePageCmd(env, signingFeatureName), false
	case adminFeatureConfiguredMsg:
		if typed.name != signingFeatureName {
			return s, nil, false
		}
		if typed.err != nil {
			s.err = typed.err.Error()
			return s, nil, false
		}
		return s, loadFeaturePageCmd(env, signingFeatureName), false
	case adminFeatureRuntimeMutatedMsg:
		if typed.name != signingFeatureName || typed.err != nil {
			return s, nil, false
		}
		return s, loadFeaturePageCmd(env, signingFeatureName), false
	}
	return s, nil, false
}

func (s *signingConfigScreen) rebuildRows(env screenEnv) {
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

func (s signingConfigScreen) updateKey(env screenEnv, msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminFeatures), true
	case isRuneKey(msg, 'p'):
		s.cfg = signingPolicyModal{
			Open:             true,
			Focus:            signingPolicyFieldEnabled,
			Enabled:          s.policy.Enabled,
			UnsignedSelfRead: normalizeUnsignedSelfRead(s.policy.UnsignedSelfRead),
			Fingerprints:     signingKeyFingerprints(s.policy.TrustedPublicKeys),
		}
		return s, nil, true
	case isRuneKey(msg, 'o'):
		// Same fix as gitleaks, for signing (spec.md "Signing
		// Repository-Scoped Override Entry Point").
		return s, navigate(screenSecuritySigningRepos), true
	case isEnterKey(msg), isRuneKey(msg, 'r'):
		return s, loadFeaturePageCmd(env, signingFeatureName), true
	}
	if action, ok := featureActionForKey(msg, s.page); ok {
		return s.dispatchAction(env, action)
	}
	return s, nil, true
}

// dispatchAction mirrors trivyConfigScreen.dispatchAction exactly, scoped
// to signingFeatureName.
func (s signingConfigScreen) dispatchAction(env screenEnv, action ports.FeatureAction) (adminScreen, tea.Cmd, bool) {
	if strings.TrimSpace(action.ConfirmMessage) != "" {
		var (
			submitting string
			onConfirm  func(screenEnv) tea.Cmd
		)
		if action.ID == "enable" || action.ID == "disable" {
			actionID := action.ID
			submitting = fmt.Sprintf("Submitting %s for %s...", actionID, signingFeatureName)
			onConfirm = func(env screenEnv) tea.Cmd { return executeFeatureActionCmd(env, signingFeatureName, actionID) }
		}
		s.confirm = newConfirmPrompt(adminFirstNonEmpty(action.ConfirmTitle, action.Label), action.ConfirmMessage, strings.ToLower(strings.TrimSpace(action.Label)), submitting, onConfirm)
		return s, nil, true
	}
	return s, executeFeatureActionCmd(env, signingFeatureName, action.ID), true
}

func (s signingConfigScreen) updateConfigKey(env screenEnv, msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		s.cfg = signingPolicyModal{}
		return s, nil, true
	case isTabKey(msg):
		s.cfg.Focus = nextSigningPolicyField(s.cfg.Focus)
		s.cfg.Error = ""
		return s, nil, true
	case isRuneKey(msg, ' '):
		switch s.cfg.Focus {
		case signingPolicyFieldEnabled:
			s.cfg.Enabled = !s.cfg.Enabled
		case signingPolicyFieldUnsignedSelfRead:
			s.cfg.UnsignedSelfRead = nextUnsignedSelfReadValue(s.cfg.UnsignedSelfRead)
		}
		s.cfg.Error = ""
		return s, nil, true
	case isBackspaceKey(msg):
		if s.cfg.Focus == signingPolicyFieldAddKey {
			s.cfg.AddKey = trimLastRune(s.cfg.AddKey)
		}
		s.cfg.Error = ""
		return s, nil, true
	case isEnterKey(msg):
		var trustedKeys []string
		if s.cfg.Focus != signingPolicyFieldClearKeys {
			trustedKeys = append(trustedKeys, s.policy.TrustedPublicKeys...)
			if strings.TrimSpace(s.cfg.AddKey) != "" {
				trustedKeys = append(trustedKeys, s.cfg.AddKey)
			}
		}
		input := ports.SigningPolicySettings{
			Enabled:           s.cfg.Enabled,
			TrustedPublicKeys: trustedKeys,
			UnsignedSelfRead:  normalizeUnsignedSelfRead(s.cfg.UnsignedSelfRead),
		}
		return s, updateSigningPolicyCmd(env, input), true
	}
	if msg.Type == tea.KeyRunes && s.cfg.Focus == signingPolicyFieldAddKey {
		s.cfg.AddKey += string(msg.Runes)
		s.cfg.Error = ""
		return s, nil, true
	}
	return s, nil, true
}

// updateSigningPolicyCmd is the screenEnv-scoped equivalent of Model's own
// updateSigningPolicyCmd method, needed because a sub-model has no Model to
// call a method on -- built from env.asModel() so the exact same
// AdminClient.UpdateSigningPolicy call/message shape
// (adminSigningPolicyUpdatedMsg) is reused verbatim.
func updateSigningPolicyCmd(env screenEnv, input ports.SigningPolicySettings) tea.Cmd {
	return env.asModel().updateSigningPolicyCmd(input)
}

// View reproduces renderSigningPolicyModal's exact layout as this screen's
// Overlay when the modal is open, with the base Feature Page body rendered
// underneath (mirrors gitleaksConfigScreen.View exactly).
func (s signingConfigScreen) View(theme adminTheme, env screenEnv) screenFrame {
	// The signing badge is composed onto this heading line at zero row
	// cost -- there is no tab strip on the signing page the way
	// renderTrivyTabs hosts the scan policy badge, so the heading itself is
	// the zero-row host (design.md Decision 11 piece 1).
	heading := theme.subheading.Render("Feature Page") + "  " + signingPolicyBadge(theme, s.policy)
	lines := []string{heading}
	if s.err != "" {
		lines = append(lines, "", theme.error.Render(s.err))
	}
	if s.submitting != "" {
		lines = append(lines, "", theme.muted.Render(s.submitting))
	}
	if !s.loaded {
		lines = append(lines, theme.muted.Render("Select or refresh a feature to load the backend-declared page."))
	} else {
		lines = append(lines, renderGenericFeaturePage(theme, s.page, s.rows)...)
	}
	lines = append(lines, renderAdminOperatorFooter(theme, env.Session, env.now())...)
	frame := screenFrame{
		Context: "Security & Compliance / Signing",
		Body:    renderSection(theme, strings.Join(lines, "\n"), env.Layout),
	}
	switch {
	case s.confirm.Active():
		frame.Overlay = s.confirm.view(theme)
	case s.cfg.Active():
		frame.Overlay = renderSigningPolicyModal(theme, s.cfg)
	}
	return frame
}

// renderSigningPolicyModal renders the image-signing content-trust gate's
// own modal (design.md Decision 11 piece 1), a sibling of
// renderScanPolicyModal -- NOT an extension of it. Row arithmetic: heading
// (1) + status (1) + 3 fields x 2 rows (6) + key list (1 for empty, else
// min(N,4)+[1 if N>4]) + clear row (1) + blank/help (2, +2 more with an
// error) + 4 rows theme.section chrome.
func renderSigningPolicyModal(theme adminTheme, modal signingPolicyModal) string {
	lines := []string{
		theme.subheading.Render("Signing Policy"),
		theme.muted.Render(signingPolicyStatusLine(modal)),
		renderToggleField(theme, "Enabled", modal.Enabled, modal.Focus == signingPolicyFieldEnabled),
		renderTextField(theme, "Unsigned Self-Read", normalizeUnsignedSelfRead(modal.UnsignedSelfRead), modal.Focus == signingPolicyFieldUnsignedSelfRead),
		renderTextField(theme, "Trusted Key (PEM)", modal.AddKey, modal.Focus == signingPolicyFieldAddKey),
	}
	lines = append(lines, renderSigningPolicyKeyList(theme, modal.Fingerprints)...)
	lines = append(lines, renderSigningPolicyClearKeysRow(theme, modal))
	if strings.TrimSpace(modal.Error) != "" {
		lines = append(lines, "", theme.error.Render(modal.Error))
	}
	lines = append(lines, "", theme.muted.Render(shortHelpView(theme, signingConfigModalKeys)))
	return theme.section.Render(strings.Join(lines, "\n"))
}

// signingPolicyStatusLine answers "is this modal loading, and how many
// trusted keys are currently configured", occupying the modal's own fixed
// Status row regardless of key count (design.md Decision 11's row-budget
// table lists Status as a constant 1-row cost, distinct from the scaling
// key list below it).
func signingPolicyStatusLine(modal signingPolicyModal) string {
	if modal.Loading {
		return "Loading…"
	}
	return fmt.Sprintf("%d trusted key(s) configured", len(modal.Fingerprints))
}

// renderSigningPolicyKeyList renders at most 4 fingerprint rows plus one
// "+N more" row when there are more than 4, or a single empty-state row when
// there are none (design.md Decision 11's row-budget table: Key list = 1 /
// N / min(N,4)+1). Stored keys are shown only as truncated SHA-256/12
// fingerprints, never as raw PEM.
func renderSigningPolicyKeyList(theme adminTheme, fingerprints []string) []string {
	if len(fingerprints) == 0 {
		return []string{theme.muted.Render("No trusted keys configured.")}
	}
	shown := fingerprints
	more := 0
	if len(shown) > 4 {
		more = len(shown) - 4
		shown = shown[:4]
	}
	lines := make([]string, 0, len(shown)+1)
	for _, fingerprint := range shown {
		lines = append(lines, theme.text.Render(fmt.Sprintf("Key: %s", fingerprint)))
	}
	if more > 0 {
		lines = append(lines, theme.muted.Render(fmt.Sprintf("+%d more", more)))
	}
	return lines
}

// renderSigningPolicyClearKeysRow renders the modal's ClearKeys action as a
// single-row line, mirroring renderRepositoryOverrideClearRow's action-row
// pattern (an action, not an input, so it costs 1 row rather than 2).
func renderSigningPolicyClearKeysRow(theme adminTheme, modal signingPolicyModal) string {
	label := "Clear all trusted keys"
	if len(modal.Fingerprints) == 0 {
		label = "No trusted keys to clear"
	}
	style := theme.muted
	if modal.Focus == signingPolicyFieldClearKeys {
		style = theme.inputFocus
	}
	return style.Render(label)
}
