package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"regixtry/internal/ports"
)

func renderAdminWorkspace(current screen, session AdminSession, view AdminViewState, knownRepositories []string, status string, now time.Time) string {
	theme := newAdminTheme()
	context, body, help := renderAdminScreen(theme, current, session, view, knownRepositories, now)
	fullBody := body
	if view.ConfirmModal.Active() {
		fullBody = lipgloss.JoinVertical(lipgloss.Left, body, renderAdminModal(theme, view.ConfirmModal))
	}
	return renderConsoleWorkspace("Regixtry Admin", context, fullBody, status, help)
}

func renderAdminScreen(theme adminTheme, current screen, session AdminSession, view AdminViewState, knownRepositories []string, now time.Time) (string, string, string) {
	switch current {
	case screenAdminCreateUser:
		return "Users / Create User", renderAdminCreateUserScreen(theme, view), "Enter: create user | Tab: next field | Space: toggle | Esc: cancel"
	case screenAdminEditUser:
		return fmt.Sprintf("Users / %s / General", selectedAdminUsername(view)), renderAdminEditUserScreen(theme, session, view, now), "g: grants | t: tokens | p: change password | e: enable | x: disable | Esc: back | q: quit"
	case screenAdminChangePassword:
		return fmt.Sprintf("Users / %s / Change Password", selectedAdminUsername(view)), renderAdminChangePasswordScreen(theme, view), "Enter: save password | Esc: cancel"
	case screenAdminEditUserGrants:
		return fmt.Sprintf("Users / %s / Grants", selectedAdminUsername(view)), renderAdminGrantsScreen(theme, view), "n: add grant | e: edit selected grant | x: remove grant | t: tokens | Esc: back | q: quit"
	case screenAdminAddGrant:
		return fmt.Sprintf("Users / %s / Grants / Add Grant", selectedAdminUsername(view)), renderAdminAddGrantScreen(theme, view, knownRepositories), "Up/Down: pick repository | Enter: accept or save | Tab: next field | Space: cycle role | Esc: cancel"
	case screenAdminEditUserTokens:
		return fmt.Sprintf("Users / %s / Tokens", selectedAdminUsername(view)), renderAdminTokensScreen(theme, view), "n: create token | x: revoke token | g: grants | Esc: back | q: quit"
	case screenAdminCreateToken:
		return fmt.Sprintf("Users / %s / Tokens / Create Token", selectedAdminUsername(view)), renderAdminCreateTokenScreen(theme, view), "Enter: create token | Tab: next field | Esc: cancel"
	case screenAdminFeatures:
		return "Features", renderAdminFeaturesScreen(theme, session, view, now), featureActionHelp(view.FeaturePage)
	default:
		return "Users", renderAdminUsersScreen(theme, session, view, now), "/: search | Enter/e: edit user | n: create user | f: features | Esc: back | q: quit"
	}
}

func renderAdminUsersScreen(theme adminTheme, session AdminSession, view AdminViewState, now time.Time) string {
	searchHint := "Press / to edit the search"
	if view.UserSearchActive {
		searchHint = "Typing updates the user list"
	}
	lines := []string{
		theme.subheading.Render("Search"),
		renderTextField(theme, "Username contains", view.UserSearchQuery, view.UserSearchActive),
		theme.muted.Render(searchHint),
		"",
		theme.subheading.Render("Users"),
	}
	filteredUsers := filteredAdminUsers(view)
	if len(filteredUsers) == 0 {
		message := "No admin users available."
		if strings.TrimSpace(view.UserSearchQuery) != "" {
			message = "No users match the current search."
		}
		lines = append(lines, theme.muted.Render(message))
	} else {
		for _, user := range filteredUsers {
			label := formatAdminUserLabel(user)
			if user.ID == view.SelectedUserID {
				label = theme.selected.Render(label)
			}
			lines = append(lines, label)
		}
	}
	lines = append(lines,
		"",
		theme.muted.Render(fmt.Sprintf("Operator: %s", session.Username)),
		theme.muted.Render(fmt.Sprintf("Session remaining: %s", formatRemaining(session.Remaining(now)))),
	)
	return theme.section.Render(strings.Join(lines, "\n"))
}

func renderAdminFeaturesScreen(theme adminTheme, session AdminSession, view AdminViewState, now time.Time) string {
	lines := []string{theme.subheading.Render("Built-in Features")}
	if len(view.Features) == 0 {
		lines = append(lines, theme.muted.Render("No built-in features available."))
	} else {
		selectedIndex := boundedIndex(view.SelectedFeature, len(view.Features))
		for index, feature := range view.Features {
			label := fmt.Sprintf("%s [%s] enabled=%t configured=%t current=%s latest=%s update=%s", feature.Name, feature.Kind, feature.Enabled, feature.Configured, adminFirstNonEmpty(strings.TrimSpace(feature.CurrentVersion), "unknown"), adminFirstNonEmpty(strings.TrimSpace(feature.LatestVersion), "unknown"), adminFirstNonEmpty(strings.TrimSpace(feature.UpdateStatus), "unknown"))
			if index == selectedIndex {
				label = theme.selected.Render(label)
			}
			lines = append(lines, label)
		}
	}

	lines = append(lines, "", theme.subheading.Render("Feature Page"))
	if strings.TrimSpace(view.FeaturePage.Summary.Name) == "" {
		lines = append(lines, theme.muted.Render("Select or refresh a feature to load the backend-declared page."))
	} else {
		for _, field := range view.FeaturePage.Header {
			lines = append(lines, fmt.Sprintf("%s: %s", field.Label, adminFirstNonEmpty(field.Value, "unknown")))
		}
		if len(view.FeaturePage.Sections) == 0 {
			lines = append(lines, theme.muted.Render("No additional feature details."))
		}
		for _, section := range view.FeaturePage.Sections {
			lines = append(lines, "", theme.subheading.Render(section.Title))
			switch section.Kind {
			case "rows":
				for _, row := range section.Rows {
					label := row.Title
					if strings.TrimSpace(row.Status) != "" {
						label += fmt.Sprintf(" [%s]", row.Status)
					}
					if strings.TrimSpace(row.Detail) != "" {
						label += fmt.Sprintf(" — %s", row.Detail)
					}
					lines = append(lines, label)
				}
			default:
				for _, field := range section.Fields {
					lines = append(lines, fmt.Sprintf("%s: %s", field.Label, adminFirstNonEmpty(field.Value, "unknown")))
				}
			}
		}
	}

	lines = append(lines,
		"",
		theme.muted.Render(fmt.Sprintf("Operator: %s", session.Username)),
		theme.muted.Render(fmt.Sprintf("Session remaining: %s", formatRemaining(session.Remaining(now)))),
	)
	return theme.section.Render(strings.Join(lines, "\n"))
}

func renderAdminCreateUserScreen(theme adminTheme, view AdminViewState) string {
	form := view.CreateUserForm
	return theme.section.Render(strings.Join([]string{
		theme.subheading.Render("Create User"),
		renderTextField(theme, "Username", form.Username, form.Focus == adminCreateUserFieldUsername),
		renderSecretField(theme, "Password", form.Password, form.Focus == adminCreateUserFieldPassword),
		renderToggleField(theme, "Create as admin", form.IsAdmin, form.Focus == adminCreateUserFieldIsAdmin),
		renderToggleField(theme, "Enabled", form.Enabled, form.Focus == adminCreateUserFieldEnabled),
	}, "\n"))
}

func renderAdminEditUserScreen(theme adminTheme, session AdminSession, view AdminViewState, now time.Time) string {
	user, ok := selectedAdminUserForView(view)
	if !ok {
		return theme.section.Render(theme.warning.Render("Select a user from Users before opening edit mode."))
	}
	role := "User"
	if user.IsAdmin {
		role = "Admin"
	}
	status := theme.success.Render("Enabled")
	if !user.Enabled {
		status = theme.warning.Render("Disabled")
	}
	return theme.section.Render(strings.Join([]string{
		theme.subheading.Render("Account"),
		fmt.Sprintf("Username: %s", user.Username),
		fmt.Sprintf("Role: %s", role),
		fmt.Sprintf("Status: %s", status),
		fmt.Sprintf("User ID: %s", user.ID),
		"",
		theme.subheading.Render("Context"),
		fmt.Sprintf("Session remaining: %s", formatRemaining(session.Remaining(now))),
		"Grants and tokens are managed from their dedicated screens.",
	}, "\n"))
}

func renderAdminChangePasswordScreen(theme adminTheme, view AdminViewState) string {
	return theme.section.Render(strings.Join([]string{
		theme.subheading.Render("Change Password"),
		fmt.Sprintf("Target: %s", selectedAdminUsername(view)),
		renderSecretField(theme, "New password", view.ResetPasswordForm.NewPassword, true),
	}, "\n"))
}

func renderAdminGrantsScreen(theme adminTheme, view AdminViewState) string {
	if strings.TrimSpace(view.SelectedUserID) == "" {
		return theme.section.Render(theme.warning.Render("Select a user before opening grants."))
	}
	lines := []string{
		theme.subheading.Render("Repository Grants"),
		fmt.Sprintf("User: %s", selectedAdminUsername(view)),
		"",
	}
	if len(view.Grants) == 0 {
		lines = append(lines, theme.muted.Render("No repository grants for the selected user."))
	} else {
		for index, grant := range view.Grants {
			label := fmt.Sprintf("%s | %s", grant.Repository, grant.Role)
			if index == view.SelectedGrant {
				label = theme.selected.Render(label)
			}
			lines = append(lines, label)
		}
	}
	return theme.section.Render(strings.Join(lines, "\n"))
}

func renderAdminAddGrantScreen(theme adminTheme, view AdminViewState, knownRepositories []string) string {
	lines := []string{
		theme.subheading.Render("Grant Details"),
		fmt.Sprintf("User: %s", selectedAdminUsername(view)),
		renderTextField(theme, "Repository", view.GrantForm.Repository, view.GrantForm.Focus == adminGrantFieldRepository),
		renderTextField(theme, "Role", string(view.GrantForm.Role), view.GrantForm.Focus == adminGrantFieldRole),
	}
	suggestions := grantRepositorySuggestions(view.GrantForm, knownRepositories)
	if len(suggestions) == 0 {
		lines = append(lines, theme.muted.Render("No known repositories match the current filter."))
	} else {
		selected := boundedIndex(view.GrantForm.RepositorySuggestion, len(suggestions))
		start := 0
		if selected >= 5 {
			start = selected - 4
		}
		end := start + 5
		if end > len(suggestions) {
			end = len(suggestions)
		}
		lines = append(lines, "", theme.subheading.Render("Known Repositories"))
		for index := start; index < end; index++ {
			repository := suggestions[index]
			label := repository
			if index == selected {
				label = theme.selected.Render(label)
			}
			lines = append(lines, label)
		}
	}
	return theme.section.Render(strings.Join(lines, "\n"))
}

func renderAdminTokensScreen(theme adminTheme, view AdminViewState) string {
	if strings.TrimSpace(view.SelectedUserID) == "" {
		return theme.section.Render(theme.warning.Render("Select a user before opening tokens."))
	}
	lines := []string{
		theme.subheading.Render("Admin Tokens"),
		fmt.Sprintf("User: %s", selectedAdminUsername(view)),
	}
	if strings.TrimSpace(view.RevealedTokenSecret) != "" {
		lines = append(lines,
			"",
			theme.success.Render("One-time secret"),
			fmt.Sprintf("Accessor: %s", view.RevealedTokenAccessor),
			theme.text.Render(view.RevealedTokenSecret),
		)
	}
	lines = append(lines, "")
	if len(view.AdminTokens) == 0 {
		lines = append(lines, theme.muted.Render("No admin tokens for the selected user."))
	} else {
		for index, token := range view.AdminTokens {
			state := "active"
			if token.RevokedAt != nil {
				state = "revoked"
			}
			label := fmt.Sprintf("%s | %s | expires %s", token.Accessor, state, token.ExpiresAt.UTC().Format(time.RFC3339))
			if index == view.SelectedToken {
				label = theme.selected.Render(label)
			}
			lines = append(lines, label)
		}
	}
	return theme.section.Render(strings.Join(lines, "\n"))
}

func renderAdminCreateTokenScreen(theme adminTheme, view AdminViewState) string {
	return theme.section.Render(strings.Join([]string{
		theme.subheading.Render("Create Token"),
		fmt.Sprintf("User: %s", selectedAdminUsername(view)),
		renderTextField(theme, "Name", view.TokenForm.Name, view.TokenForm.Focus == adminTokenFieldName),
		renderTextField(theme, "TTL seconds", view.TokenForm.TTLSeconds, view.TokenForm.Focus == adminTokenFieldTTL),
	}, "\n"))
}

func renderAdminModal(theme adminTheme, modal adminConfirmModal) string {
	return theme.section.Render(strings.Join([]string{
		theme.subheading.Render(modal.Title),
		modal.Message,
		"",
		theme.muted.Render(fmt.Sprintf("Enter: %s | Esc: cancel", modal.ConfirmText)),
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
	case strings.Contains(lower, "loading"), strings.Contains(lower, "refreshing"), strings.Contains(lower, "submitting"):
		style = theme.warning
	}
	return theme.section.Render(strings.Join([]string{theme.subheading.Render("Status"), style.Render(status)}, "\n"))
}

func renderTextField(theme adminTheme, label string, value string, focused bool) string {
	style := theme.input
	if focused {
		style = theme.inputFocus
	}
	if strings.TrimSpace(value) == "" {
		value = ""
	}
	return fmt.Sprintf("%s\n%s", label, style.Render(value))
}

func renderSecretField(theme adminTheme, label string, value string, focused bool) string {
	masked := strings.Repeat("*", len([]rune(value)))
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

func selectedAdminUserForView(view AdminViewState) (ports.AdminUser, bool) {
	for _, user := range view.Users {
		if user.ID == view.SelectedUserID {
			return user, true
		}
	}
	return ports.AdminUser{}, false
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

func formatAdminUserLabel(user ports.AdminUser) string {
	role := "user"
	if user.IsAdmin {
		role = "admin"
	}
	state := "disabled"
	if user.Enabled {
		state = "enabled"
	}
	return fmt.Sprintf("%s [%s, %s]", user.Username, role, state)
}

func adminFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
