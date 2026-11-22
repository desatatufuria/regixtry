package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	domainauth "regixtry/internal/domain/auth"
)

// revokeGrantConfirm is the dialog every screen shows before removing one
// repository grant: Repository access, Delegate and Edit user build the same
// text, so the operator reads the same warning wherever they revoke.
//
// The consequence is worded from how access is evaluated: a request is
// authorized from the grants read from the store when it arrives (the token
// scope only caps what a token may ask for), so the loss applies from the
// next request. Nothing is revoked besides the grant: the user's tokens and
// their access to other repositories are untouched.
func revokeGrantConfirm(repo, username string, role domainauth.RepoRole, onConfirm func(screenEnv) tea.Cmd) confirmPrompt {
	label := grantRoleLabel(role)
	return newConfirmPrompt(
		"Revoke access",
		fmt.Sprintf("Revoke %s's %s access to %s?", username, label, repo),
		"revoke",
		fmt.Sprintf("Revoking %s on %s...", username, repo),
		onConfirm,
	).withTarget(fmt.Sprintf("%s on %s", username, repo), label).
		withConsequence(fmt.Sprintf("%s loses %s access to %s. It applies from the next request; no token is revoked.", username, label, repo))
}
