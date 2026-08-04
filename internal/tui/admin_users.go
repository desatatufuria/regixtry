package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	domainauth "registry/internal/domain/auth"
	"registry/internal/ports"
)

type Option func(*Model)

func WithAuthAdministration(authService ports.AuthService, actor domainauth.Principal) Option {
	return func(m *Model) {
		m.authService = authService
		m.actor = actor
	}
}

type AdminUsersModel struct {
	Items    []domainauth.User
	Selected int
}

type adminFormKind string

const (
	adminFormCreateUser    adminFormKind = "create-user"
	adminFormUpdateUser    adminFormKind = "update-user"
	adminFormResetPassword adminFormKind = "reset-password"
	adminFormCreateGrant   adminFormKind = "create-grant"
	adminFormCreateToken   adminFormKind = "create-token"
)

type adminField struct {
	Label  string
	Value  string
	Secret bool
}

type adminFormModel struct {
	kind     adminFormKind
	title    string
	userID   string
	fields   []adminField
	selected int
}

func (m Model) updateAdminKey(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	if m.authService == nil {
		return false, m, nil
	}

	switch m.screen {
	case screenRepositories:
		if msg.String() == "a" {
			m.screen = screenLoading
			m.loadingText = "Loading admin users..."
			return true, m, m.loadAdminUsersCmd("")
		}
	case screenAdminUsers:
		switch msg.String() {
		case "esc", "backspace":
			m.screen = screenRepositories
			m.status = ""
			return true, m, nil
		case "r":
			m.screen = screenLoading
			m.loadingText = "Refreshing admin users..."
			return true, m, m.loadAdminUsersCmd("")
		case "n":
			m.form = &adminFormModel{kind: adminFormCreateUser, title: "Create user", fields: []adminField{{Label: "Username"}, {Label: "Password", Secret: true}, {Label: "Admin (yes/no)", Value: "no"}}}
			return true, m, nil
		case "e":
			user, ok := m.selectedAdminUser()
			if !ok {
				return true, m, nil
			}
			adminValue := "no"
			if user.IsAdmin {
				adminValue = "yes"
			}
			m.form = &adminFormModel{kind: adminFormUpdateUser, title: fmt.Sprintf("Edit user · %s", user.Username), userID: user.ID, fields: []adminField{{Label: "Username", Value: user.Username}, {Label: "Admin (yes/no)", Value: adminValue}}}
			return true, m, nil
		case "!":
			user, ok := m.selectedAdminUser()
			if !ok {
				return true, m, nil
			}
			m.status = "Updating user state..."
			return true, m, m.toggleAdminUserCmd(user)
		case "p":
			user, ok := m.selectedAdminUser()
			if !ok {
				return true, m, nil
			}
			m.form = &adminFormModel{kind: adminFormResetPassword, title: fmt.Sprintf("Reset password · %s", user.Username), userID: user.ID, fields: []adminField{{Label: "New password", Secret: true}}}
			return true, m, nil
		case "g":
			user, ok := m.selectedAdminUser()
			if !ok {
				return true, m, nil
			}
			m.screen = screenLoading
			m.loadingText = fmt.Sprintf("Loading grants for %s...", user.Username)
			return true, m, m.loadAdminGrantsCmd(user)
		case "o":
			user, ok := m.selectedAdminUser()
			if !ok {
				return true, m, nil
			}
			m.screen = screenLoading
			m.loadingText = fmt.Sprintf("Loading tokens for %s...", user.Username)
			return true, m, m.loadAdminTokensCmd(user)
		case "x":
			user, ok := m.selectedAdminUser()
			if !ok {
				return true, m, nil
			}
			m.status = "Deleting user..."
			return true, m, m.deleteAdminUserCmd(user)
		}
	case screenAdminGrants:
		switch msg.String() {
		case "esc", "backspace", "n", "x":
			updated, cmd := m.updateAdminGrantsKey(msg)
			return true, updated, cmd
		}
	case screenAdminTokens:
		switch msg.String() {
		case "esc", "backspace", "n", "x":
			updated, cmd := m.updateAdminTokensKey(msg)
			return true, updated, cmd
		}
	}

	return false, m, nil
}

func (m Model) loadAdminUsersCmd(selectedUserID string) tea.Cmd {
	return func() tea.Msg {
		users, err := m.authService.ListUsers(m.ctx, m.actor)
		if err != nil {
			return adminUserMutationMsg{err: err}
		}
		if selectedUserID == "" {
			return adminUsersLoadedMsg{users: users}
		}
		return adminUserMutationMsg{users: users, selectedUserID: selectedUserID}
	}
}

func (m Model) selectedAdminUser() (domainauth.User, bool) {
	if len(m.adminUsers.Items) == 0 {
		return domainauth.User{}, false
	}
	return m.adminUsers.Items[m.adminUsers.Selected], true
}

func (m Model) toggleAdminUserCmd(user domainauth.User) tea.Cmd {
	return func() tea.Msg {
		updated, err := m.authService.SetUserEnabled(m.ctx, m.actor, user.ID, !user.Enabled)
		if err != nil {
			return adminUserMutationMsg{err: err}
		}
		users, err := m.authService.ListUsers(m.ctx, m.actor)
		if err != nil {
			return adminUserMutationMsg{err: err}
		}
		state := "disabled"
		if updated.Enabled {
			state = "enabled"
		}
		return adminUserMutationMsg{users: users, selectedUserID: updated.ID, status: fmt.Sprintf("User %s %s.", updated.Username, state)}
	}
}

func (m Model) deleteAdminUserCmd(user domainauth.User) tea.Cmd {
	return func() tea.Msg {
		if err := m.authService.DeleteUser(m.ctx, m.actor, user.ID); err != nil {
			return adminUserMutationMsg{err: err}
		}
		users, err := m.authService.ListUsers(m.ctx, m.actor)
		if err != nil {
			return adminUserMutationMsg{err: err}
		}
		return adminUserMutationMsg{users: users, status: fmt.Sprintf("User %s deleted.", user.Username)}
	}
}

func submitUserForm(ctx context.Context, m Model, form adminFormModel) tea.Msg {
	switch form.kind {
	case adminFormCreateUser:
		user, err := m.authService.CreateUser(ctx, m.actor, ports.CreateUserInput{Username: form.fields[0].Value, Password: form.fields[1].Value, IsAdmin: parseBoolInput(form.fields[2].Value), Enabled: true})
		if err != nil {
			return adminUserMutationMsg{err: err}
		}
		users, err := m.authService.ListUsers(ctx, m.actor)
		if err != nil {
			return adminUserMutationMsg{err: err}
		}
		return adminUserMutationMsg{users: users, selectedUserID: user.ID, status: fmt.Sprintf("User %s created.", user.Username)}
	case adminFormUpdateUser:
		user, err := m.authService.UpdateUser(ctx, m.actor, ports.UpdateUserInput{UserID: form.userID, Username: form.fields[0].Value, IsAdmin: parseBoolInput(form.fields[1].Value)})
		if err != nil {
			return adminUserMutationMsg{err: err}
		}
		users, err := m.authService.ListUsers(ctx, m.actor)
		if err != nil {
			return adminUserMutationMsg{err: err}
		}
		return adminUserMutationMsg{users: users, selectedUserID: user.ID, status: fmt.Sprintf("User %s updated.", user.Username)}
	case adminFormResetPassword:
		if err := m.authService.ResetPassword(ctx, m.actor, form.userID, form.fields[0].Value); err != nil {
			return adminUserMutationMsg{err: err}
		}
		users, err := m.authService.ListUsers(ctx, m.actor)
		if err != nil {
			return adminUserMutationMsg{err: err}
		}
		selectedUserID := form.userID
		username := selectedUserID
		for _, user := range users {
			if user.ID == selectedUserID {
				username = user.Username
				break
			}
		}
		return adminUserMutationMsg{users: users, selectedUserID: selectedUserID, status: fmt.Sprintf("Password reset for %s.", username)}
	default:
		return adminUserMutationMsg{err: domainauth.NewValidationError("unsupported admin form")}
	}
}

func selectedUserIndex(users []domainauth.User, userID string) int {
	if userID == "" {
		return boundedIndex(0, len(users))
	}
	for index, user := range users {
		if user.ID == userID {
			return index
		}
	}
	return boundedIndex(0, len(users))
}

func renderAdminUsers(model AdminUsersModel) string {
	lines := []string{"Admin · Users"}
	if len(model.Items) == 0 {
		return strings.Join(append(lines, "No local users are configured."), "\n")
	}
	for index, user := range model.Items {
		prefix := "  "
		if index == model.Selected {
			prefix = "> "
		}
		role := "user"
		if user.IsAdmin {
			role = "admin"
		}
		state := "enabled"
		if !user.Enabled {
			state = "disabled"
		}
		lines = append(lines, fmt.Sprintf("%s%s · %s · %s", prefix, user.Username, role, state))
	}
	return strings.Join(lines, "\n")
}

func renderAdminForm(form adminFormModel) string {
	lines := []string{form.title, "Tab: next field · Enter: submit · Esc: cancel"}
	for index, field := range form.fields {
		prefix := "  "
		if index == form.selected {
			prefix = "> "
		}
		value := field.Value
		if field.Secret {
			value = strings.Repeat("*", len(field.Value))
		}
		lines = append(lines, fmt.Sprintf("%s%s: %s", prefix, field.Label, value))
	}
	return strings.Join(lines, "\n")
}
