package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
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
		withConsequence(fmt.Sprintf("%s loses %s access to %s. It applies from the next request; no token is revoked. %s keeps access to every other repository.", username, label, repo, username))
}

// revokeProjectRoleConfirm is the dialog before a project role is removed.
// The consequence is worded from how access is evaluated: a project role
// covers every repository whose first path segment is the project and is read
// on each request, so the loss applies from the next request. Nothing else is
// revoked: tokens and exact repository grants stay.
func revokeProjectRoleConfirm(project string, member ports.AdminProjectMember, onConfirm func(screenEnv) tea.Cmd) confirmPrompt {
	label := projectRoleLabel(member.Role)
	short := strings.TrimPrefix(label, "Project ")
	since := ""
	if !member.CreatedAt.IsZero() {
		since = " since " + member.CreatedAt.Format("2006-01-02")
	}
	return newConfirmPrompt(
		fmt.Sprintf("Revoke %s?", label),
		fmt.Sprintf("%s loses %s access to every repository in %s, unless an exact repository grant also gives it. It applies from the next request; no token is revoked.", member.Username, strings.ToUpper(short[:1])+short[1:], project),
		"revoke",
		fmt.Sprintf("Revoking %s on %s...", member.Username, project),
		onConfirm,
	).withTarget(fmt.Sprintf("%s on project %s", member.Username, project), label+since).
		withButtonLabel("Revoke").
		withConsequence("Grant a project role again to restore it.")
}
