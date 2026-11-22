package tui

import (
	"context"
	"errors"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// Every async result of the three Security configuration screens carries the
// generation id of the screen instance that asked for it. Model.Update drops a
// result whose generation no mounted screen owns BEFORE it looks at an expired
// session, so a late result can neither land on a newer screen nor log the
// operator out; each screen drops a foreign generation again for itself.

// securityGenerations hands out the unique id of each Security screen instance.
var securityGenerations atomic.Uint64

var errSecurityUnavailable = errors.New("admin API is unavailable for this session")

// securityMsg is implemented by every result below.
type securityMsg interface {
	securityGen() uint64
	expiredReason() (string, bool)
}

// securityResult is the envelope every result shares.
type securityResult struct {
	gen uint64
	err error
}

func (r securityResult) securityGen() uint64           { return r.gen }
func (r securityResult) expiredReason() (string, bool) { return expiredReasonOf(r.err) }

type securityPageMsg struct {
	securityResult
	page ports.FeaturePage
}

type securityOverridesMsg struct {
	securityResult
	overrides []ports.RepositoryOverrideDetails
}

type securityActionMsg struct {
	securityResult
	result ports.FeatureActionResult
}

type securityConfigureMsg struct{ securityResult }

type securityScanPolicyLoadedMsg struct {
	securityResult
	settings ports.ScanPolicySettings
}

type securityScanPolicySavedMsg struct {
	securityResult
	settings ports.ScanPolicySettings
}

type securitySigningPolicyLoadedMsg struct {
	securityResult
	settings ports.SigningPolicySettings
}

type securitySigningPolicySavedMsg struct {
	securityResult
	settings ports.SigningPolicySettings
}

type securityKeyUsageMsg struct {
	securityResult
	key    string
	count  int
	capped bool
}

func securityLoadPageCmd(env screenEnv, gen uint64, feature string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return securityPageMsg{securityResult: securityResult{gen: gen, err: errSecurityUnavailable}}
		}
		page, err := client.GetFeaturePage(context.Background(), session, feature)
		return securityPageMsg{securityResult: securityResult{gen: gen, err: err}, page: page}
	}
}

func securityLoadOverridesCmd(env screenEnv, gen uint64, feature string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return securityOverridesMsg{securityResult: securityResult{gen: gen, err: errSecurityUnavailable}}
		}
		overrides, err := client.ListRepositoryOverrides(context.Background(), session, feature)
		return securityOverridesMsg{securityResult: securityResult{gen: gen, err: err}, overrides: overrides}
	}
}

func securityActionCmd(env screenEnv, gen uint64, feature, actionID string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return securityActionMsg{securityResult: securityResult{gen: gen, err: errSecurityUnavailable}}
		}
		result, err := client.ExecuteFeatureAction(context.Background(), session, feature, actionID)
		return securityActionMsg{securityResult: securityResult{gen: gen, err: err}, result: result}
	}
}

func securityConfigureCmd(env screenEnv, gen uint64, feature string, input ports.FeatureConfigureInput) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return securityConfigureMsg{securityResult{gen: gen, err: errSecurityUnavailable}}
		}
		_, err := client.ConfigureFeature(context.Background(), session, feature, input)
		return securityConfigureMsg{securityResult{gen: gen, err: err}}
	}
}

func securityLoadScanPolicyCmd(env screenEnv, gen uint64) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return securityScanPolicyLoadedMsg{securityResult: securityResult{gen: gen, err: errSecurityUnavailable}}
		}
		settings, err := client.GetScanPolicy(context.Background(), session)
		return securityScanPolicyLoadedMsg{securityResult: securityResult{gen: gen, err: err}, settings: settings}
	}
}

func securitySaveScanPolicyCmd(env screenEnv, gen uint64, input ports.ScanPolicySettings) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return securityScanPolicySavedMsg{securityResult: securityResult{gen: gen, err: errSecurityUnavailable}}
		}
		settings, err := client.UpdateScanPolicy(context.Background(), session, input)
		return securityScanPolicySavedMsg{securityResult: securityResult{gen: gen, err: err}, settings: settings}
	}
}

func securityLoadSigningPolicyCmd(env screenEnv, gen uint64) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return securitySigningPolicyLoadedMsg{securityResult: securityResult{gen: gen, err: errSecurityUnavailable}}
		}
		settings, err := client.GetSigningPolicy(context.Background(), session)
		return securitySigningPolicyLoadedMsg{securityResult: securityResult{gen: gen, err: err}, settings: settings}
	}
}

func securitySaveSigningPolicyCmd(env screenEnv, gen uint64, input ports.SigningPolicySettings) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return securitySigningPolicySavedMsg{securityResult: securityResult{gen: gen, err: errSecurityUnavailable}}
		}
		settings, err := client.UpdateSigningPolicy(context.Background(), session, input)
		return securitySigningPolicySavedMsg{securityResult: securityResult{gen: gen, err: err}, settings: settings}
	}
}

func securityKeyUsageCmd(env screenEnv, gen uint64, keyPEM string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return securityKeyUsageMsg{securityResult: securityResult{gen: gen, err: errSecurityUnavailable}, key: keyPEM}
		}
		count, capped, err := client.CountSigningKeyUsage(context.Background(), session, "", keyPEM)
		return securityKeyUsageMsg{securityResult: securityResult{gen: gen, err: err}, key: keyPEM, count: count, capped: capped}
	}
}

// securityScreenGen returns the generation of the mounted Security screen in
// slot, if there is one.
func securityScreenGen(screens adminScreenSet, slot screenSlot) (uint64, bool) {
	switch s := screens[slot].(type) {
	case trivyConfigScreen:
		return s.gen, true
	case gitleaksConfigScreen:
		return s.gen, true
	case signingConfigScreen:
		return s.gen, true
	}
	return 0, false
}

// securityScreenOwns reports whether a mounted Security screen has generation
// gen, i.e. whether a result carrying it still has a screen to land on.
func securityScreenOwns(screens adminScreenSet, gen uint64) bool {
	for _, slot := range []screenSlot{slotTrivyConfig, slotGitleaksConfig, slotSigningConfig} {
		if current, ok := securityScreenGen(screens, slot); ok && current == gen {
			return true
		}
	}
	return false
}
