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

// overridePolicyMsg is the global signing policy, read to seed the keys of a
// signing override that does not exist yet.
type overridePolicyMsg struct {
	securityResult
	editor   uint64
	settings ports.SigningPolicySettings
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

func loadOverridePolicyCmd(env screenEnv, gen, editor uint64) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return overridePolicyMsg{securityResult: securityResult{gen: gen, err: errSecurityUnavailable}, editor: editor}
		}
		settings, err := client.GetSigningPolicy(context.Background(), session)
		return overridePolicyMsg{securityResult: securityResult{gen: gen, err: err}, editor: editor, settings: settings}
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
