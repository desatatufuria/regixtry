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
		s.cfg.Keys = s.cfg.Keys.SetKeys(typed.settings.TrustedPublicKeys)
		s.cfg.Error = ""
		return s, nil, false
	case adminSigningKeyUsageLoadedMsg:
		s.cfg.Keys = s.cfg.Keys.applyUsageLoaded(typed)
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
			Keys:             newTrustedKeyList("", s.policy.TrustedPublicKeys),
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
	// The Keys field (trustedKeyList) claims Up/Down/'n'/'x' and every key
	// while it is mid-add or mid-delete-confirm; only what it does NOT
	// consume (Tab/Esc/Space/Enter in its own idle navigation state) falls
	// through to this modal's own bindings below.
	//
	// This routing is UNCONDITIONAL (not gated on
	// s.cfg.Focus == signingPolicyFieldAddKey): the modal opens with
	// Focus == signingPolicyFieldEnabled (updateKey's 'p' branch), not the
	// key list's position, so gating on Tab-focus made 'n'/'x'/Up/Down
	// silent no-ops until the operator tabbed to the exact right field
	// first -- the "can't add a key" bug. Enabled/UnsignedSelfRead are
	// Space-toggled/cycled, never rune-typed, so there is no other field in
	// this modal that could ever claim these keys as literal input; routing
	// them here first is safe.
	//
	// Judgment Day fix-round: unconditional routing alone left the visible
	// Tab-focus indicator free to disagree with what actually responded to
	// the keystroke -- pressing 'n' while "Unsigned Self-Read" was
	// highlighted silently acted on the key list instead, with no on-screen
	// sign focus had effectively moved. The moment s.cfg.Keys.update
	// actually CONSUMES a key, snap s.cfg.Focus to signingPolicyFieldAddKey
	// too, so the rendered highlight (renderSigningPolicyModal's
	// modal.Focus == signingPolicyFieldAddKey check) is always honest about
	// what is currently receiving input.
	next, cmd, consumed := s.cfg.Keys.update(env, msg)
	if consumed {
		s.cfg.Keys = next
		s.cfg.Focus = signingPolicyFieldAddKey
		return s, cmd, true
	}
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
	case isEnterKey(msg):
		var trustedKeys []string
		if s.cfg.Focus != signingPolicyFieldClearKeys {
			trustedKeys = s.cfg.Keys.Keys()
		}
		input := ports.SigningPolicySettings{
			Enabled:           s.cfg.Enabled,
			TrustedPublicKeys: trustedKeys,
			UnsignedSelfRead:  normalizeUnsignedSelfRead(s.cfg.UnsignedSelfRead),
		}
		return s, updateSigningPolicyCmd(env, input), true
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
	}
	lines = append(lines, renderTrustedKeyList(theme, modal.Keys, modal.Focus == signingPolicyFieldAddKey)...)
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
	return fmt.Sprintf("%d trusted key(s) configured", len(modal.Keys.Keys()))
}

// renderSigningPolicyClearKeysRow renders the modal's ClearKeys action as a
// single-row line, mirroring renderRepositoryOverrideClearRow's action-row
// pattern (an action, not an input, so it costs 1 row rather than 2). This
// stays alongside trustedKeyList's own per-key 'x' delete as a bulk
// convenience -- "clear every key in the list" -- not a replacement for it.
func renderSigningPolicyClearKeysRow(theme adminTheme, modal signingPolicyModal) string {
	label := "Clear all trusted keys"
	if len(modal.Keys.Keys()) == 0 {
		label = "No trusted keys to clear"
	}
	style := theme.muted
	if modal.Focus == signingPolicyFieldClearKeys {
		style = theme.inputFocus
	}
	return style.Render(label)
}
