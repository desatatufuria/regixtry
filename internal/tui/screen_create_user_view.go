package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

// userUsernameRule is the server's username rule (domain/auth usernamePattern
// and normalizeUsername): there is no length limit.
const userUsernameRule = "Username: letters, numbers, dot, dash and underscore; starts and ends with a letter or number; capitals are lowercased"

const (
	userFormRulesWidth = 44
	userFormColumnGap  = 2
	userFormFieldWidth = 40
)

func (s createUserScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH := shellContentSize(width, height)

	lines := []string{
		theme.subheading.Render("New user"),
		theme.muted.Render(fitText("The user signs in with this username and password. Grants come after.", bodyW)),
		"",
	}
	columnsHeight := max(bodyH-len(lines), 3)
	rulesW := min(userFormRulesWidth, max(bodyW*2/5, 20))
	formW := max(bodyW-rulesW-userFormColumnGap, 10)

	columns := lipgloss.JoinHorizontal(lipgloss.Top,
		userPanel(theme, formW, columnsHeight, s.formLines(theme, formW-4)),
		strings.Repeat(" ", userFormColumnGap),
		userPanel(theme, rulesW, columnsHeight, s.rulesLines(theme, rulesW-4)),
	)
	return screenFrame{
		Context: "New user",
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: shellCrumb("Access", "Users", "New user"),
			Operator:   env.Session.Username,
			Active:     shellNavUsers,
			Body:       strings.Join(lines, "\n") + "\n" + columns,
			Hints:      s.Keys().short,
			Width:      width,
			Height:     height,
		}),
	}
}

func (s createUserScreen) formLines(theme adminTheme, width int) []string {
	fieldW := min(userFormFieldWidth, width)
	lines := userField(theme, "Username", s.username, false, s.focus == createUserFieldUsername, fieldW)
	lines = append(lines, userField(theme, "Password", s.password, true, s.focus == createUserFieldPassword, fieldW)...)
	lines = append(lines, s.passwordHint(theme))

	lines = append(lines, "", theme.muted.Render("Role"), s.roleButtons(theme))
	box := "[ ]"
	if s.enabled {
		box = "[x]"
	}
	boxStyle := theme.text
	if s.focus == createUserFieldEnabled {
		boxStyle = theme.accent
	}
	lines = append(lines, "", boxStyle.Render(box)+" Enabled "+theme.muted.Render("(space toggles; a disabled user cannot sign in)"))

	// The short-password message is the counter line above turned red, so it
	// is never told twice.
	if s.err != "" && s.err != userPasswordTooShort {
		lines = append(lines, theme.error.Render(s.err))
	}
	lines = append(lines, "", s.buttons(theme))
	return lines
}

// passwordHint counts the typed characters against the minimum.
func (s createUserScreen) passwordHint(theme adminTheme) string {
	count := utf8.RuneCountInString(s.password)
	switch {
	case s.password == "":
		return theme.muted.Render(fmt.Sprintf("Minimum %d characters.", userPasswordMinLength))
	case validateNewPassword(s.password) == "":
		return theme.success.UnsetBold().Render(fmt.Sprintf("✓ %d characters", count))
	}
	style := theme.warning
	if s.err == userPasswordTooShort {
		style = theme.error
	}
	return style.UnsetBold().Render(fmt.Sprintf("%d characters, minimum %d.", count, userPasswordMinLength))
}

// roleButtons draws the three role buttons on one row: a lit button is a
// chosen role, and the one under the cursor is underlined while the row has
// focus.
func (s createUserScreen) roleButtons(theme adminTheme) string {
	buttons := make([]string, 0, len(createUserRoles)*2)
	for i, role := range createUserRoles {
		style := theme.input.Width(0).Padding(0, 1)
		if s.roleLit(i) {
			style = theme.selected
		}
		if s.focus == createUserFieldRole && i == s.roleCursor {
			style = style.Underline(true)
		}
		label := role
		if s.roleLit(i) {
			label = "✓ " + role
		}
		buttons = append(buttons, style.Render(label), " ")
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, buttons...)
}

func (s createUserScreen) buttons(theme adminTheme) string {
	create := "Create user  enter"
	if s.creating {
		create = "Creating…"
	}
	return theme.pill.Render(create) + "  " + theme.muted.Render("Cancel  esc")
}

func (s createUserScreen) rulesLines(theme adminTheme, width int) []string {
	lines := []string{userSectionTitle(theme, "RULES")}
	rule := func(text string) {
		for i, line := range wrapText(text, max(width-2, 1)) {
			prefix := "  "
			if i == 0 {
				prefix = theme.success.UnsetBold().Render("✓") + " "
			}
			lines = append(lines, prefix+line)
		}
	}
	rule(userUsernameRule)
	rule(fmt.Sprintf("Password: at least %d characters", userPasswordMinLength))
	for _, line := range wrapText("The password is masked while you type and is never shown again, here or after saving.", width) {
		lines = append(lines, theme.muted.Render(line))
	}
	lines = append(lines, "", userSectionTitle(theme, "ROLES"))
	for _, role := range []struct{ name, text string }{
		{"admin", "manages users, robots and grants"},
		{"user", "pulls and pushes where granted"},
		{"read-only", "pulls where granted, never pushes"},
	} {
		for i, line := range wrapText(role.name+" "+role.text, width) {
			if i == 0 {
				line = theme.text.Render(role.name) + theme.muted.Render(strings.TrimPrefix(line, role.name))
			} else {
				line = theme.muted.Render(line)
			}
			lines = append(lines, line)
		}
	}
	return lines
}
