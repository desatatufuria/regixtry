package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	domainauth "registry/internal/domain/auth"
)

type AdminGrantsModel struct {
	User     domainauth.User
	Items    []domainauth.RepoGrant
	Selected int
}

func (m Model) loadAdminGrantsCmd(user domainauth.User) tea.Cmd {
	return func() tea.Msg {
		grants, err := m.authService.ListRepoGrants(m.ctx, m.actor, user.ID)
		return adminGrantsLoadedMsg{user: user, grants: grants, err: err}
	}
}

func (m Model) selectedGrant() (domainauth.RepoGrant, bool) {
	if len(m.adminGrants.Items) == 0 {
		return domainauth.RepoGrant{}, false
	}
	return m.adminGrants.Items[m.adminGrants.Selected], true
}

func (m Model) updateAdminGrantsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace":
		m.screen = screenAdminUsers
		m.status = ""
		return m, nil
	case "n":
		m.form = &adminFormModel{kind: adminFormCreateGrant, title: fmt.Sprintf("Assign grant · %s", m.adminGrants.User.Username), userID: m.adminGrants.User.ID, fields: []adminField{{Label: "Repository"}, {Label: "Role", Value: string(domainauth.RepoRoleReader)}}}
		return m, nil
	case "x":
		grant, ok := m.selectedGrant()
		if !ok {
			return m, nil
		}
		m.status = "Removing repository grant..."
		return m, func() tea.Msg {
			err := m.authService.DeleteRepoGrant(m.ctx, m.actor, m.adminGrants.User.ID, grant.Repository.String())
			if err != nil {
				return adminGrantMutationMsg{err: err}
			}
			grants, loadErr := m.authService.ListRepoGrants(m.ctx, m.actor, m.adminGrants.User.ID)
			return adminGrantMutationMsg{user: m.adminGrants.User, grants: grants, selectedRepository: "", status: fmt.Sprintf("Grant removed from %s.", grant.Repository.String()), err: loadErr}
		}
	}
	return m, nil
}

func submitGrantForm(ctx context.Context, m Model, form adminFormModel) tea.Msg {
	grant, err := m.authService.PutRepoGrant(ctx, m.actor, form.userID, form.fields[0].Value, domainauth.RepoRole(strings.TrimSpace(form.fields[1].Value)))
	if err != nil {
		return adminGrantMutationMsg{err: err}
	}
	grants, loadErr := m.authService.ListRepoGrants(ctx, m.actor, form.userID)
	return adminGrantMutationMsg{user: m.adminGrants.User, grants: grants, selectedRepository: grant.Repository.String(), status: fmt.Sprintf("Grant %s assigned on %s.", grant.Role, grant.Repository.String()), err: loadErr}
}

func selectedGrantIndex(grants []domainauth.RepoGrant, repository string) int {
	if repository == "" {
		return boundedIndex(0, len(grants))
	}
	for index, grant := range grants {
		if grant.Repository.String() == repository {
			return index
		}
	}
	return boundedIndex(0, len(grants))
}

func renderAdminGrants(model AdminGrantsModel) string {
	lines := []string{fmt.Sprintf("Admin · Grants · %s", model.User.Username)}
	if len(model.Items) == 0 {
		return strings.Join(append(lines, "No repository grants are assigned."), "\n")
	}
	for index, grant := range model.Items {
		prefix := "  "
		if index == model.Selected {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s · %s", prefix, grant.Repository.String(), grant.Role))
	}
	return strings.Join(lines, "\n")
}
