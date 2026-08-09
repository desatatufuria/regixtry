package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"regixtry/internal/ports"
)

func renderAdminWorkspace(session AdminSession, view AdminViewState, status string, now time.Time) string {
	theme := newAdminTheme()
	sidebar := renderAdminSidebar(theme, session, view, now)
	main := renderAdminMainPanel(theme, session, view, now)
	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", main)
	if view.ConfirmModal.Active() {
		body = lipgloss.JoinVertical(lipgloss.Left, body, "", renderAdminModal(theme, view.ConfirmModal))
	}
	if strings.TrimSpace(status) != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, body, "", renderAdminStatus(theme, status))
	}
	return theme.app.Render(body)
}

func renderAdminSidebar(theme adminTheme, session AdminSession, view AdminViewState, now time.Time) string {
	lines := []string{theme.heading.Render("Admin Workspace")}
	if strings.TrimSpace(session.Username) != "" {
		lines = append(lines, theme.text.Render(fmt.Sprintf("Operator: %s", session.Username)))
	}
	if !session.ExpiresAt.IsZero() {
		lines = append(lines, theme.muted.Render(fmt.Sprintf("Session expires in %s", formatRemaining(session.Remaining(now)))))
	}
	lines = append(lines, "", theme.heading.Render("Navigation"))
	for _, item := range []struct {
		panel adminPanel
		label string
	}{
		{adminPanelUsers, "Users"},
		{adminPanelGrants, "Grants"},
		{adminPanelTokens, "Tokens"},
	} {
		label := item.label
		if view.SelectedPanel == item.panel {
			label = theme.selected.Render(label)
		}
		lines = append(lines, label)
	}
	lines = append(lines, "", theme.heading.Render("Selected User"))
	if view.SelectedUserID == "" {
		lines = append(lines, theme.muted.Render("No user selected"))
	} else {
		lines = append(lines, theme.badge.Render(view.SelectedUsername))
	}
	lines = append(lines, "", theme.heading.Render("Users"))
	if len(view.Users) == 0 {
		lines = append(lines, theme.muted.Render("No admin users available."))
	} else {
		for index, user := range view.Users {
			role := "user"
			if user.IsAdmin {
				role = "admin"
			}
			state := "disabled"
			if user.Enabled {
				state = "enabled"
			}
			label := fmt.Sprintf("%s [%s, %s]", user.Username, role, state)
			if index == view.SelectedUser {
				label = theme.selected.Render(label)
			}
			lines = append(lines, label)
		}
	}
	lines = append(lines, "", theme.muted.Render("u/g/t: switch panel · j/k: move user · l: logout · q: quit"))
	return theme.sidebar.Render(strings.Join(lines, "\n"))
}

func renderAdminMainPanel(theme adminTheme, session AdminSession, view AdminViewState, now time.Time) string {
	var title string
	var content string
	switch view.SelectedPanel {
	case adminPanelGrants:
		title = fmt.Sprintf("Repository Grants · %s", selectedAdminUsername(view))
		content = renderAdminGrantsPanel(theme, view)
	case adminPanelTokens:
		title = fmt.Sprintf("Admin Tokens · %s", selectedAdminUsername(view))
		content = renderAdminTokensPanel(theme, view)
	default:
		title = "Users"
		content = renderAdminUsersPanel(theme, session, view, now)
	}
	return theme.panel.Render(lipgloss.JoinVertical(lipgloss.Left, theme.heading.Render(title), "", content))
}

func renderAdminUsersPanel(theme adminTheme, session AdminSession, view AdminViewState, now time.Time) string {
	sections := []string{
		theme.section.Render(strings.Join([]string{
			theme.accent.Render("Workspace"),
			fmt.Sprintf("Selected user: %s", selectedAdminUsername(view)),
			fmt.Sprintf("Session remaining: %s", formatRemaining(session.Remaining(now))),
			"Enter: open grants · c: create user · p: reset password · e/d: enable or disable",
		}, "\n")),
		renderCreateUserSection(theme, view),
		renderResetPasswordSection(theme, view),
	}
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func renderCreateUserSection(theme adminTheme, view AdminViewState) string {
	form := view.CreateUserForm
	lines := []string{
		theme.accent.Render("Create User"),
		renderTextField(theme, "Username", form.Username, view.ActiveForm == adminFormCreateUser && form.Focus == adminCreateUserFieldUsername),
		renderSecretField(theme, "Password", form.Password, view.ActiveForm == adminFormCreateUser && form.Focus == adminCreateUserFieldPassword),
		renderToggleField(theme, "Create as admin", form.IsAdmin, view.ActiveForm == adminFormCreateUser && form.Focus == adminCreateUserFieldIsAdmin),
		renderToggleField(theme, "Enabled", form.Enabled, view.ActiveForm == adminFormCreateUser && form.Focus == adminCreateUserFieldEnabled),
		theme.muted.Render("c: edit · tab: next field · space: toggle · enter: submit · esc: cancel"),
	}
	return theme.section.Render(strings.Join(lines, "\n"))
}

func renderResetPasswordSection(theme adminTheme, view AdminViewState) string {
	if view.SelectedUserID == "" {
		return theme.section.Render(strings.Join([]string{
			theme.accent.Render("Reset Password"),
			theme.warning.Render("Select a user before resetting a password."),
		}, "\n"))
	}
	form := view.ResetPasswordForm
	return theme.section.Render(strings.Join([]string{
		theme.accent.Render("Reset Password"),
		fmt.Sprintf("Target: %s", view.SelectedUsername),
		renderSecretField(theme, "New password", form.NewPassword, view.ActiveForm == adminFormResetPassword && form.Focus == adminResetPasswordFieldPassword),
		theme.muted.Render("p: edit · enter: submit · esc: cancel"),
	}, "\n"))
}

func renderAdminGrantsPanel(theme adminTheme, view AdminViewState) string {
	if view.SelectedUserID == "" {
		return theme.section.Render(strings.Join([]string{
			theme.warning.Render("Select a user to manage grants."),
			theme.muted.Render("Grant mutations stay blocked until a user is selected."),
		}, "\n"))
	}
	items := []string{theme.accent.Render("Grant Form")}
	items = append(items,
		renderTextField(theme, "Repository", view.GrantForm.Repository, view.ActiveForm == adminFormGrant && view.GrantForm.Focus == adminGrantFieldRepository),
		renderTextField(theme, "Role", string(view.GrantForm.Role), view.ActiveForm == adminFormGrant && view.GrantForm.Focus == adminGrantFieldRole),
		theme.muted.Render("a: edit · tab: next field · space: cycle role · enter: save grant · x: remove selected grant"),
		"",
		theme.accent.Render("Current Grants"),
	)
	if len(view.Grants) == 0 {
		items = append(items, theme.muted.Render("No repository grants for the selected user."))
	} else {
		for index, grant := range view.Grants {
			line := fmt.Sprintf("- %s · %s", grant.Repository, grant.Role)
			if index == view.SelectedGrant {
				line = theme.selected.Render(line)
			}
			items = append(items, line)
		}
	}
	return theme.section.Render(strings.Join(items, "\n"))
}

func renderAdminTokensPanel(theme adminTheme, view AdminViewState) string {
	if view.SelectedUserID == "" {
		return theme.section.Render(strings.Join([]string{
			theme.warning.Render("Select a user to manage admin tokens."),
			theme.muted.Render("Token mutations stay blocked until a user is selected."),
		}, "\n"))
	}
	items := []string{theme.accent.Render("Create Admin Token")}
	items = append(items,
		renderTextField(theme, "Name", view.TokenForm.Name, view.ActiveForm == adminFormToken && view.TokenForm.Focus == adminTokenFieldName),
		renderTextField(theme, "TTL seconds", view.TokenForm.TTLSeconds, view.ActiveForm == adminFormToken && view.TokenForm.Focus == adminTokenFieldTTL),
		theme.muted.Render("n: edit · tab: next field · enter: create token · x: revoke selected token"),
	)
	if strings.TrimSpace(view.RevealedTokenSecret) != "" {
		items = append(items, "", theme.success.Render("One-time secret"), fmt.Sprintf("Accessor: %s", view.RevealedTokenAccessor), view.RevealedTokenSecret)
	}
	items = append(items, "", theme.accent.Render("Current Tokens"))
	if len(view.AdminTokens) == 0 {
		items = append(items, theme.muted.Render("No admin tokens for the selected user."))
	} else {
		for index, token := range view.AdminTokens {
			state := "active"
			if token.RevokedAt != nil {
				state = "revoked"
			}
			line := fmt.Sprintf("- %s · %s · expires %s", token.Accessor, state, token.ExpiresAt.UTC().Format(time.RFC3339))
			if index == view.SelectedToken {
				line = theme.selected.Render(line)
			}
			items = append(items, line)
		}
	}
	return theme.section.Render(strings.Join(items, "\n"))
}

func renderAdminModal(theme adminTheme, modal adminConfirmModal) string {
	return theme.modal.Render(strings.Join([]string{
		theme.heading.Render(modal.Title),
		modal.Message,
		"",
		theme.muted.Render(fmt.Sprintf("Enter: %s · n: cancel · esc: cancel", modal.ConfirmText)),
	}, "\n"))
}

func renderAdminStatus(theme adminTheme, status string) string {
	style := theme.muted
	lower := strings.ToLower(status)
	switch {
	case strings.Contains(lower, "expired"), strings.Contains(lower, "invalid"), strings.Contains(lower, "error"):
		style = theme.error
	case strings.Contains(lower, "created"), strings.Contains(lower, "enabled"), strings.Contains(lower, "disabled"), strings.Contains(lower, "reset"), strings.Contains(lower, "saved"), strings.Contains(lower, "revoked"):
		style = theme.success
	case strings.Contains(lower, "loading"), strings.Contains(lower, "refreshing"):
		style = theme.warning
	}
	return theme.section.Render(strings.Join([]string{theme.heading.Render("Status"), style.Render(status)}, "\n"))
}

func renderTextField(theme adminTheme, label string, value string, focused bool) string {
	style := theme.input
	if focused {
		style = theme.inputFocus
	}
	if strings.TrimSpace(value) == "" {
		value = " "
	}
	return fmt.Sprintf("%s\n%s", label, style.Render(value))
}

func renderSecretField(theme adminTheme, label string, value string, focused bool) string {
	masked := strings.Repeat("*", len([]rune(value)))
	if masked == "" {
		masked = " "
	}
	style := theme.input
	if focused {
		style = theme.inputFocus
	}
	return fmt.Sprintf("%s\n%s", label, style.Render(masked))
}

func renderToggleField(theme adminTheme, label string, value bool, focused bool) string {
	current := "off"
	if value {
		current = "on"
	}
	style := theme.input
	if focused {
		style = theme.inputFocus
	}
	return fmt.Sprintf("%s\n%s", label, style.Render(current))
}

func selectedAdminUsername(view AdminViewState) string {
	if strings.TrimSpace(view.SelectedUsername) != "" {
		return view.SelectedUsername
	}
	return "No selection"
}

func selectedGrantForView(view AdminViewState) (ports.AdminRepoGrant, bool) {
	if len(view.Grants) == 0 {
		return ports.AdminRepoGrant{}, false
	}
	index := boundedIndex(view.SelectedGrant, len(view.Grants))
	return view.Grants[index], true
}

func selectedTokenForView(view AdminViewState) (ports.AdminToken, bool) {
	if len(view.AdminTokens) == 0 {
		return ports.AdminToken{}, false
	}
	index := boundedIndex(view.SelectedToken, len(view.AdminTokens))
	return view.AdminTokens[index], true
}
