package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// signingConfigScreen is "Signing policy" (the approved "Signing policy"
// board): whether unsigned images may be pulled, the trusted public keys and
// keyless identities, and the per-repository overrides, with the policy editor
// as a modal.
//
// The field names confirm and cfg are read by Model
// (migratedScreenCapturesTextInput): do not rename them.
type signingConfigScreen struct {
	securityFeature
	cfg          signingPolicyModal
	policy       ports.SigningPolicySettings
	policyLoaded bool
	policyErr    string
	// saving is true from the Enter that saved the policy until the result
	// arrives: Enter is ignored meanwhile.
	saving bool
	// clearArmed is set by the Enter that asked to clear every trusted key: the
	// next Enter confirms it, any other key cancels it.
	clearArmed bool
}

func newSigningConfigScreen() signingConfigScreen {
	return signingConfigScreen{securityFeature: newSecurityFeature(signingFeatureName)}
}

func (s signingConfigScreen) ID() screen { return screenSecuritySigningConfig }

func (s signingConfigScreen) Keys() screenKeys {
	if s.cfg.Active() {
		return securityModalKeys
	}
	short := []key.Binding{
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

func (s signingConfigScreen) Init(env screenEnv) tea.Cmd { return s.load(env) }

func (s signingConfigScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
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
		if typed.err != nil {
			return s, nil, false
		}
		return s, securityLoadSigningPolicyCmd(env, s.gen), false
	case securityOverridesMsg:
		s.securityFeature = s.applyOverrides(env, typed)
		return s, nil, false
	case securitySigningPolicyLoadedMsg:
		if typed.err != nil {
			s.policyErr = typed.err.Error()
			return s, s.loadOverrides(env), false
		}
		s.policy, s.policyLoaded, s.policyErr = typed.settings, true, ""
		return s, s.loadOverrides(env), false
	case securitySigningPolicySavedMsg:
		if !s.saving {
			return s, nil, false
		}
		s.saving = false
		if typed.err != nil {
			// The modal keeps everything the operator typed.
			if s.cfg.Active() {
				s.cfg.Error = typed.err.Error()
			} else {
				s.err = typed.err.Error()
			}
			return s, nil, false
		}
		// Unlike the scan policy modal, this one stays open after a save: the
		// operator can keep adding keys and identities.
		s.policy, s.policyLoaded, s.policyErr = typed.settings, true, ""
		s.cfg.Enabled = typed.settings.Enabled
		s.cfg.UnsignedSelfRead = normalizeUnsignedSelfRead(typed.settings.UnsignedSelfRead)
		s.cfg.Keys = s.cfg.Keys.SetKeys(typed.settings.TrustedPublicKeys)
		s.cfg.Identities = s.cfg.Identities.SetIdentities(typed.settings.TrustedIdentities)
		s.cfg.Error = ""
		s.err, s.note = "", "Signing policy saved."
		return s, nil, false
	case securityKeyUsageMsg:
		s.cfg.Keys = s.cfg.Keys.applyUsageLoaded(adminSigningKeyUsageLoadedMsg{count: typed.count, capped: typed.capped, err: typed.err})
		return s, nil, false
	case securityActionMsg:
		f, reload := s.applyActionResult(typed)
		s.securityFeature = f
		if reload {
			return s, s.load(env), false
		}
		return s, nil, false
	}
	return s, nil, false
}

func (s signingConfigScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	s.note = ""
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminOverview), true
	case isRuneKey(msg, 'o'):
		return s, navigate(screenSecuritySigningRepos), true
	case isRuneKey(msg, 'p'):
		if !s.policyLoaded {
			// Saving a policy that never loaded would wipe the real trusted
			// keys and identities.
			s.err = "The signing policy has not loaded yet. Press r to try again."
			return s, nil, true
		}
		s.cfg = signingPolicyModal{
			Open:             true,
			Focus:            signingPolicyFieldEnabled,
			Enabled:          s.policy.Enabled,
			UnsignedSelfRead: normalizeUnsignedSelfRead(s.policy.UnsignedSelfRead),
			Keys:             newTrustedKeyList("", s.policy.TrustedPublicKeys),
			Identities:       newTrustedIdentityList(s.policy.TrustedIdentities),
		}
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

// updateConfigKey handles one key of the policy modal.
//
// The trusted-key list claims Up, Down, j, k, n and x, and every key while it
// is adding a key or confirming a delete, whichever field has focus: none of
// the modal's other fields takes typed text, so there is no clash. When it
// consumes a key the focus snaps onto it, so the highlight always shows what
// is receiving input. The identity list claims the same keys only once Tab
// has put focus on it: two lists cannot both own them unannounced.
func (s signingConfigScreen) updateConfigKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if isEscKey(msg) && !s.cfg.Keys.adding && !s.cfg.Keys.confirm.Active() && !s.cfg.Keys.usageLoading && !s.cfg.Identities.adding {
		s.cfg = signingPolicyModal{}
		s.clearArmed = false
		return s, nil, true
	}
	if s.saving {
		return s, nil, true
	}
	s.note = ""
	armed := s.clearArmed
	s.clearArmed = false
	if s.cfg.Focus == signingPolicyFieldIdentities {
		next, consumed := s.cfg.Identities.update(msg)
		if consumed {
			s.cfg.Identities = next
			return s, nil, true
		}
	} else {
		next, cmd, consumed := s.cfg.Keys.update(env, msg)
		if consumed {
			if next.usageLoading && !s.cfg.Keys.usageLoading {
				// The usage lookup is this screen's own, stamped with its
				// generation, so a late answer cannot reach another screen.
				cmd = securityKeyUsageCmd(env, s.gen, next.keys[next.pendingDeleteIndex])
			}
			s.cfg.Keys = next
			s.cfg.Focus = signingPolicyFieldAddKey
			return s, cmd, true
		}
	}
	switch {
	case isTabKey(msg), isArrowDownKey(msg):
		s.cfg.Focus = nextSigningPolicyField(s.cfg.Focus)
		s.cfg.Error = ""
	case isShiftTabKey(msg), isArrowUpKey(msg):
		s.cfg.Focus = previousSigningPolicyField(s.cfg.Focus)
		s.cfg.Error = ""
	case isSpaceKey(msg):
		switch s.cfg.Focus {
		case signingPolicyFieldEnabled:
			s.cfg.Enabled = !s.cfg.Enabled
		case signingPolicyFieldUnsignedSelfRead:
			s.cfg.UnsignedSelfRead = nextUnsignedSelfReadValue(s.cfg.UnsignedSelfRead)
		}
		s.cfg.Error = ""
	case isEnterKey(msg):
		var trustedKeys []string
		if s.cfg.Focus == signingPolicyFieldClearKeys {
			if len(s.cfg.Keys.Keys()) > 0 && !armed {
				// Removing every trusted key makes every pull unverifiable:
				// ask before sending it.
				s.clearArmed = true
				return s, nil, true
			}
		} else {
			trustedKeys = s.cfg.Keys.Keys()
		}
		s.saving = true
		s.cfg.Error = ""
		return s, securitySaveSigningPolicyCmd(env, s.gen, ports.SigningPolicySettings{
			Enabled:           s.cfg.Enabled,
			TrustedPublicKeys: trustedKeys,
			TrustedIdentities: s.cfg.Identities.Identities(),
			UnsignedSelfRead:  normalizeUnsignedSelfRead(s.cfg.UnsignedSelfRead),
		}), true
	}
	return s, nil, true
}

func previousSigningPolicyField(field signingPolicyField) signingPolicyField {
	if field <= signingPolicyFieldEnabled {
		return signingPolicyFieldClearKeys
	}
	return field - 1
}
