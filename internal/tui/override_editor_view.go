package tui

import (
	"fmt"
)

// renderOverrideEditor draws the editor as a modal in the style of the approved
// "Config modal" board: the title, what it applies to, the fields, the buttons
// and the key hint.
func renderOverrideEditor(theme adminTheme, env screenEnv, e overrideEditor) string {
	width := securityModalWidth
	if e.feature == signingFeatureName {
		width = securitySigningModalWidth
	}
	inner := width - securityModalChrome

	var body []string
	focusLine := 0
	formShown := e.loaded && (e.exists || e.prefillApplied)
	if formShown {
		body, focusLine = e.formLines(theme, inner)
	}
	hint := "tab next field, space toggles"
	if e.feature == signingFeatureName {
		hint = "tab next field, space toggles or cycles, n adds and x deletes in a list"
	}
	err := e.err
	if !e.loaded && !e.loading && err == "" {
		err = "The override could not be read."
	}
	if !formShown {
		hint = "esc closes. Open the editor again to retry."
	}
	return securityModal(theme, env, securityModalSpec{
		title:     featureDisplayName(e.feature) + " override",
		subtitle:  fmt.Sprintf("%s. %s.", e.repository, overrideStatusLine(e)),
		body:      body,
		focusLine: focusLine,
		err:       err,
		note:      e.note,
		saving:    e.busy,
		noSave:    !formShown,
		hint:      hint,
		width:     width,
	})
}

// formLines is the body of the loaded form and the line that must stay visible
// when the terminal is too short for all of it.
func (e overrideEditor) formLines(theme adminTheme, inner int) ([]string, int) {
	var body []string
	focusLine := 0
	at := func(field overrideField) {
		if e.currentField() == field {
			focusLine = len(body)
		}
	}
	focused := func(field overrideField) bool { return e.currentField() == field }

	at(overrideFieldEnabled)
	switch e.feature {
	case signingFeatureName:
		body = append(body, securityToggleLine(theme, "Pull-time gate enforced", e.enabled, focused(overrideFieldEnabled)))
		at(overrideFieldPathPrimary)
		if focused(overrideFieldPathPrimary) {
			focusLine = len(body) + trustedKeyFocusRow(e.keys)
		}
		body = append(body, renderTrustedKeyList(theme, e.keys, focused(overrideFieldPathPrimary))...)
		at(overrideFieldIdentities)
		if focused(overrideFieldIdentities) {
			focusLine = len(body) + trustedIdentityFocusRow(e.identities)
		}
		body = append(body, renderTrustedIdentityList(theme, e.identities, focused(overrideFieldIdentities))...)
		at(overrideFieldUnsignedSelfRead)
		body = append(body, securityChoiceField(theme, "Unsigned self-read (space cycles)", normalizeUnsignedSelfRead(e.unsignedSelfRead), focused(overrideFieldUnsignedSelfRead), inner)...)
	case gitleaksFeatureName:
		body = append(body, securityToggleLine(theme, "Scanning enabled", e.enabled, focused(overrideFieldEnabled)))
		at(overrideFieldPathPrimary)
		body = append(body, userField(theme, "Config path", e.pathPrimary, false, focused(overrideFieldPathPrimary), inner)...)
	default:
		body = append(body, securityToggleLine(theme, "Scanning enabled", e.enabled, focused(overrideFieldEnabled)))
		at(overrideFieldPathPrimary)
		body = append(body, userField(theme, "Ignore file path", e.pathPrimary, false, focused(overrideFieldPathPrimary), inner)...)
		at(overrideFieldPathSecondary)
		body = append(body, userField(theme, "Ignore policy path", e.pathSecondary, false, focused(overrideFieldPathSecondary), inner)...)
	}

	at(overrideFieldClear)
	label := "Clear override, use the global settings"
	if !e.exists {
		label = "No override to clear"
	}
	style := theme.muted
	if focused(overrideFieldClear) {
		style = theme.inputFocus.Width(0)
	}
	body = append(body, style.Render(label))
	return body, focusLine
}
