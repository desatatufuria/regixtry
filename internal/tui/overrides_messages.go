package tui

import (
	"context"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// The per-repository override screens (Trivy, Gitleaks and Signing) and their
// editor ask the admin API through the commands below. Every result carries the
// generation of the screen instance that asked (the securityResult envelope,
// so Model.Update drops a result no mounted screen owns before it can log the
// operator out) and, for the editor, the id of the editor that was open when it
// asked: a result for an editor that has since been closed or reopened for
// another repository is dropped.

// overrideEditorIDs hands out the id of each opened editor.
var overrideEditorIDs atomic.Uint64

// overrideLoadedMsg is one repository's stored override as the editor read it.
type overrideLoadedMsg struct {
	securityResult
	editor     uint64
	repository string
	feature    string
	override   ports.RepositoryOverrideDetails
	exists     bool
}

// overrideSavedMsg is the result of a save (exists true) or a clear (false).
type overrideSavedMsg struct {
	securityResult
	editor     uint64
	repository string
	feature    string
	override   ports.RepositoryOverrideDetails
	exists     bool
}

// overrideGlobalMsg is the global setting a new override starts from: whether
// the feature is enabled and, for signing, the trusted keys of the policy.
type overrideGlobalMsg struct {
	securityResult
	editor  uint64
	enabled bool
	keys    []string
	// identities is the number of keyless identities of the signing policy.
	identities int
}

// overrideListMsg is the stored overrides of the feature. seq is the number of
// saves and clears the screen had seen when the read started: an answer that
// started before a later one would show the old state.
type overrideListMsg struct {
	securityResult
	seq       uint64
	overrides []ports.RepositoryOverrideDetails
}

// overrideKeyUsageMsg answers the "how many images does this key verify"
// advisory asked before a trusted key is removed from an override.
type overrideKeyUsageMsg struct {
	securityResult
	editor uint64
	key    string
	count  int
	capped bool
}

// overrideSummariesMsg is the latest scan of each repository (Trivy only).
type overrideSummariesMsg struct {
	securityResult
	summaries []ports.RepositoryScanSummary
}

func loadOverrideCmd(env screenEnv, gen, editor uint64, repository, feature string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return overrideLoadedMsg{securityResult: securityResult{gen: gen, err: errSecurityUnavailable}, editor: editor, repository: repository, feature: feature}
		}
		override, exists, err := client.GetRepositoryOverride(context.Background(), session, repository, feature)
		return overrideLoadedMsg{securityResult: securityResult{gen: gen, err: err}, editor: editor, repository: repository, feature: feature, override: override, exists: exists}
	}
}

func saveOverrideCmd(env screenEnv, gen, editor uint64, repository, feature string, input ports.RepositoryOverrideDetails) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return overrideSavedMsg{securityResult: securityResult{gen: gen, err: errSecurityUnavailable}, editor: editor, repository: repository, feature: feature}
		}
		override, err := client.SetRepositoryOverride(context.Background(), session, repository, feature, input)
		return overrideSavedMsg{securityResult: securityResult{gen: gen, err: err}, editor: editor, repository: repository, feature: feature, override: override, exists: true}
	}
}

func clearOverrideCmd(env screenEnv, gen, editor uint64, repository, feature string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return overrideSavedMsg{securityResult: securityResult{gen: gen, err: errSecurityUnavailable}, editor: editor, repository: repository, feature: feature}
		}
		err := client.ClearRepositoryOverride(context.Background(), session, repository, feature)
		return overrideSavedMsg{securityResult: securityResult{gen: gen, err: err}, editor: editor, repository: repository, feature: feature}
	}
}

// loadOverrideGlobalCmd reads what a new override starts from.
func loadOverrideGlobalCmd(env screenEnv, gen, editor uint64, feature string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		fail := func(err error) tea.Msg {
			return overrideGlobalMsg{securityResult: securityResult{gen: gen, err: err}, editor: editor}
		}
		if client == nil {
			return fail(errSecurityUnavailable)
		}
		if feature == signingFeatureName {
			settings, err := client.GetSigningPolicy(context.Background(), session)
			if err != nil {
				return fail(err)
			}
			return overrideGlobalMsg{securityResult: securityResult{gen: gen}, editor: editor, enabled: settings.Enabled, keys: settings.TrustedPublicKeys, identities: len(settings.TrustedIdentities)}
		}
		page, err := client.GetFeaturePage(context.Background(), session, feature)
		if err != nil {
			return fail(err)
		}
		return overrideGlobalMsg{securityResult: securityResult{gen: gen}, editor: editor, enabled: page.Summary.Enabled}
	}
}

// loadOverrideListCmd reads the stored overrides of one feature.
func loadOverrideListCmd(env screenEnv, gen, seq uint64, feature string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return overrideListMsg{securityResult: securityResult{gen: gen, err: errSecurityUnavailable}, seq: seq}
		}
		overrides, err := client.ListRepositoryOverrides(context.Background(), session, feature)
		return overrideListMsg{securityResult: securityResult{gen: gen, err: err}, seq: seq, overrides: overrides}
	}
}

// overrideKeyUsageCmd counts the images a key verifies inside one repository.
func overrideKeyUsageCmd(env screenEnv, gen, editor uint64, repository, keyPEM string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return overrideKeyUsageMsg{securityResult: securityResult{gen: gen, err: errSecurityUnavailable}, editor: editor, key: keyPEM}
		}
		count, capped, err := client.CountSigningKeyUsage(context.Background(), session, repository, keyPEM)
		return overrideKeyUsageMsg{securityResult: securityResult{gen: gen, err: err}, editor: editor, key: keyPEM, count: count, capped: capped}
	}
}

func loadOverrideSummariesCmd(env screenEnv, gen uint64, limit int) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return overrideSummariesMsg{securityResult: securityResult{gen: gen, err: errSecurityUnavailable}}
		}
		summaries, err := client.ListRepositoryScanSummaries(context.Background(), session, limit)
		return overrideSummariesMsg{securityResult: securityResult{gen: gen, err: err}, summaries: summaries}
	}
}
