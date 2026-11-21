package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

func (s resetPasswordScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH := shellContentSize(width, height)

	name := adminFirstNonEmpty(s.username, s.userID)
	lines := []string{
		theme.subheading.Render(fitText("Reset password for "+name, bodyW)),
		theme.muted.Render("Enter saves the new password. Esc cancels."),
		"",
	}
	columnsHeight := max(bodyH-len(lines), 3)
	rulesW := min(userFormRulesWidth, max(bodyW*2/5, 20))
	formW := max(bodyW-rulesW-userFormColumnGap, 10)

	columns := lipgloss.JoinHorizontal(lipgloss.Top,
		userPanel(theme, formW, columnsHeight, s.formLines(theme, name, formW-4)),
		strings.Repeat(" ", userFormColumnGap),
		userPanel(theme, rulesW, columnsHeight, s.checkLines(theme, rulesW-4)),
	)
	return screenFrame{
		Context: "Reset password",
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: shellCrumb("Access", "Users", name, "Reset password"),
			Operator:   env.Session.Username,
			Active:     shellNavUsers,
			Body:       strings.Join(lines, "\n") + "\n" + columns,
			Hints:      s.Keys().short,
			Width:      width,
			Height:     height,
		}),
	}
}

func (s resetPasswordScreen) formLines(theme adminTheme, name string, width int) []string {
	fieldW := min(userFormFieldWidth, width)
	lines := userField(theme, "New password", s.password, true, s.focus == resetPasswordFieldNew, fieldW)
	switch {
	case s.err != "":
		lines = append(lines, theme.error.UnsetBold().Render(s.err))
	case validateNewPassword(s.password) == "":
		lines = append(lines, theme.success.UnsetBold().Render(fmt.Sprintf("✓ %d characters", utf8.RuneCountInString(s.password))))
	default:
		lines = append(lines, theme.muted.Render(fmt.Sprintf("Minimum %d characters.", userPasswordMinLength)))
	}

	lines = append(lines, "")
	lines = append(lines, userField(theme, "Confirm new password", s.confirm, true, s.focus == resetPasswordFieldConfirm, fieldW)...)
	switch {
	case s.confirmErr != "":
		lines = append(lines, theme.error.UnsetBold().Render(s.confirmErr))
	case s.mismatch():
		lines = append(lines, theme.error.UnsetBold().Render(userPasswordMismatch))
	}

	lines = append(lines, "")
	for _, line := range wrapText(fmt.Sprintf("Tell %s the new password over a private channel. It is not shown again.", name), width) {
		lines = append(lines, theme.muted.Render(line))
	}
	button := "Set password  enter"
	if s.saving {
		button = "Saving…"
	}
	return append(lines, "", theme.pill.Render(button)+"  "+theme.muted.Render("Cancel  esc"))
}

func (s resetPasswordScreen) checkLines(theme adminTheme, width int) []string {
	mark := func(ok bool) string {
		if ok {
			return theme.success.UnsetBold().Render("✓")
		}
		return theme.error.UnsetBold().Render("·")
	}
	lines := []string{userSectionTitle(theme, "RULE"), fmt.Sprintf("At least %d characters.", userPasswordMinLength)}
	for _, line := range wrapText("Both fields are masked while you type. Pasting is allowed.", width) {
		lines = append(lines, theme.muted.Render(line))
	}
	return append(lines, "", userSectionTitle(theme, "CHECKS"),
		mark(validateNewPassword(s.password) == "")+fmt.Sprintf(" %d characters or more", userPasswordMinLength),
		mark(s.password != "" && s.confirm == s.password)+" Both fields match",
	)
}
