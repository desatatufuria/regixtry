package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"regixtry/internal/ports"
)

// signingConfigScreen is Phase 12.3's sub-model (design.md's State
// Migration table: SigningPolicy/SigningPolicyModal -> signingConfigScreen,
// policy/policyModal). It is the image-signing content-trust gate's own
// global policy editor, moved off AdminViewState.SigningPolicyModal verbatim
// in behavior -- updateSigningPolicyModalKey (model.go) and
// renderSigningPolicyModal (admin_views.go) both move here, mirroring
// gitleaksConfigScreen's Slice 1 precedent exactly. It is mounted into
// Model.adminScreens[slotSigningConfig] only while open; the router clears
// the slot back to nil on close (Esc).
//
// Deviation, disclosed rather than silent: design.md's table lists
// "SigningPolicy" as also migrating to a `policy` field here. That field
// (the LOADED baseline settings, distinct from the modal's own editable
// copy) stays on AdminViewState in this apply batch instead, because it has
// a second, still-legacy reader: renderAdminFeaturesScreen's
// signingPolicyBadge composed onto the Feature Page heading
// (admin_views.go), which is part of screenAdminFeatures' still-unmigrated
// rendering (Phase 11 deviation). Deleting AdminViewState.SigningPolicy here
// would either break that badge's only data source or force a duplicated,
// driftable copy across two owners. baselineKeys below carries exactly the
// one piece of that settings snapshot signingConfigScreen itself needs (the
// raw trusted-key list for its Enter-submit payload, since screenEnv
// deliberately carries no SigningPolicy -- it is per-call READ-only state,
// design.md's Interfaces section), seeded once at construction from the
// same already-loaded AdminViewState.SigningPolicy the pre-change opener
// read, and refreshed by adminSigningPolicyUpdatedMsg on every successful
// save.
type signingConfigScreen struct {
	cfg          signingPolicyModal
	baselineKeys []string // AdminViewState.SigningPolicy.TrustedPublicKeys at open/last-save time
}

// newSigningConfigScreen constructs the screen from an already-seeded
// signingPolicyModal (mirroring the pre-change opener's own construction at
// model.go's 'p' branch exactly) plus the loaded policy's raw trusted-key
// baseline, needed for Enter's save payload.
func newSigningConfigScreen(cfg signingPolicyModal, baselineKeys []string) signingConfigScreen {
	return signingConfigScreen{cfg: cfg, baselineKeys: append([]string(nil), baselineKeys...)}
}

// ID identifies the screen this overlay is nested within (mirrors
// gitleaksConfigScreen.ID's doc comment exactly): signingConfigScreen is not
// addressed by screen identity via slotFor -- it is an overlay on the still-
// legacy screenAdminFeatures, not a top-level screen.
func (s signingConfigScreen) ID() screen { return screenAdminFeatures }

func (s signingConfigScreen) Keys() screenKeys { return signingConfigKeys }

// Init has nothing to load: the modal is always seeded synchronously from
// the already-loaded AdminViewState.SigningPolicy at construction time,
// exactly like the pre-change opener.
func (s signingConfigScreen) Init(env screenEnv) tea.Cmd { return nil }

// Update reproduces updateSigningPolicyModalKey's exact behavior for keys,
// plus adminSigningPolicyUpdatedMsg's modal-refresh half (design.md
// Decision H broadcast arm) -- the central Model.Update handler keeps the
// badge-facing AdminViewState.SigningPolicy assignment and m.status, and
// broadcasts the same message here via routeAdminMsg so this screen can
// refresh its own cfg/baselineKeys, exactly mirroring
// gitleaksConfigScreen's adminFeatureConfiguredMsg handling.
func (s signingConfigScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		return s.updateKey(env, typed)
	case adminSigningPolicyUpdatedMsg:
		if typed.err != nil {
			s.cfg.Error = typed.err.Error()
			return s, nil, false
		}
		// Unlike scanPolicyModal, the modal stays open after a successful
		// save (design.md Decision 11 piece 1's growable key list): the
		// operator can keep adding keys, mirroring the pre-change central
		// handler's own "stays open" precedent exactly.
		s.cfg.Enabled = typed.settings.Enabled
		s.cfg.UnsignedSelfRead = normalizeUnsignedSelfRead(typed.settings.UnsignedSelfRead)
		s.cfg.Fingerprints = signingKeyFingerprints(typed.settings.TrustedPublicKeys)
		s.cfg.AddKey = ""
		s.cfg.Error = ""
		s.baselineKeys = append([]string(nil), typed.settings.TrustedPublicKeys...)
		return s, nil, false
	}
	return s, nil, false
}

func (s signingConfigScreen) updateKey(env screenEnv, msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	keys := s.Keys()
	switch {
	case keys.matches(msg, "Esc"):
		return nil, nil, true
	case keys.matches(msg, "Tab"):
		s.cfg.Focus = nextSigningPolicyField(s.cfg.Focus)
		s.cfg.Error = ""
		return s, nil, true
	case keys.matches(msg, "Space"):
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
	case keys.matches(msg, "Enter"):
		var trustedKeys []string
		if s.cfg.Focus != signingPolicyFieldClearKeys {
			trustedKeys = append(trustedKeys, s.baselineKeys...)
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
// (adminSigningPolicyUpdatedMsg) is reused verbatim (mirrors
// screen_gitleaks_config.go's configureFeatureCmd wrapper).
func updateSigningPolicyCmd(env screenEnv, input ports.SigningPolicySettings) tea.Cmd {
	return env.asModel().updateSigningPolicyCmd(input)
}

// View reproduces renderSigningPolicyModal's exact layout, with the
// pre-change hand-written footer replaced by shortHelpView's
// keymap-generated one.
func (s signingConfigScreen) View(theme adminTheme, env screenEnv) screenFrame {
	return screenFrame{Overlay: renderSigningPolicyModal(theme, s.cfg)}
}

// renderSigningPolicyModal renders the image-signing content-trust gate's
// own modal (design.md Decision 11 piece 1), a sibling of
// renderScanPolicyModal -- NOT an extension of it. Row arithmetic: heading
// (1) + status (1) + 3 fields x 2 rows (6) + key list (1 for empty, else
// min(N,4)+[1 if N>4]) + clear row (1) + blank/help (2, +2 more with an
// error) + 4 rows theme.section chrome. Moved from admin_views.go (Phase
// 12.3); the pre-move hand-written footer is now shortHelpView's
// keymap-generated one (T1.3's byte-identity proof applies to this footer
// exactly the same way it did for gitleaksConfigScreen's).
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
	lines = append(lines, "", theme.muted.Render(shortHelpView(theme, signingConfigKeys)))
	return theme.section.Render(strings.Join(lines, "\n"))
}

// signingPolicyStatusLine answers "is this modal loading, and how many
// trusted keys are currently configured", occupying the modal's own fixed
// Status row regardless of key count (design.md Decision 11's row-budget
// table lists Status as a constant 1-row cost, distinct from the scaling
// key list below it). Moved from admin_views.go (Phase 12.3).
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
// fingerprints, never as raw PEM (renderSigningPolicyModal's own doc
// comment / spec's redaction requirement). Moved from admin_views.go (Phase
// 12.3).
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
// pattern (an action, not an input, so it costs 1 row rather than 2). Moved
// from admin_views.go (Phase 12.3).
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
