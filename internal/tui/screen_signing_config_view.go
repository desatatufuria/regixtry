package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// signingKeyType is the one kind of key the policy accepts (ports.
// SigningPolicySettings.TrustedPublicKeys: canonical PEM, ECDSA P-256).
const signingKeyType = "ECDSA P-256"

func (s signingConfigScreen) View(theme adminTheme, env screenEnv) screenFrame {
	bodyW, bodyH := shellContentSize(env.Layout.Width, env.Layout.Height)

	lines := []string{
		securityTitleLine(theme, "Signing policy", "image signature verification", bodyW),
		s.noticeLine(theme, bodyW),
	}
	if !s.loaded {
		lines = append(lines, securityLoadingBody(theme, s.err, bodyW)...)
	} else {
		leftW := max((bodyW-securityPanelGap)*2/5, 1)
		rightW := max(bodyW-securityPanelGap-leftW, 1)
		policy := s.policyLines(theme, leftW-4)
		natural := 4 + len(s.policy.TrustedPublicKeys) + len(s.policy.TrustedIdentities) + 4
		top := securityTopHeight(max(natural, len(policy)+3), bodyH-len(lines))
		columns := lipgloss.JoinHorizontal(lipgloss.Top,
			securityPanel(theme, "SIGNING POLICY", leftW, top, policy),
			strings.Repeat(" ", securityPanelGap),
			userPanel(theme, rightW, top, s.trustLines(theme, rightW-4, top-2)),
		)
		lines = append(lines, columns,
			securityOverridesPanel(theme, env, signingFeatureName, s.overrides, bodyW, max(bodyH-len(lines)-top, 1)))
	}

	frame := screenFrame{
		Context: "Signing policy",
		Full:    true,
		Body:    securityShell(theme, env, "Signing policy", shellNavSigningPolicy, lines, s.Keys()),
	}
	switch {
	case s.confirm.Active():
		frame.Overlay = s.confirm.view(theme)
	case s.cfg.Active():
		frame.Overlay = s.renderPolicyModal(theme, env)
	}
	return frame
}

func (s signingConfigScreen) noticeLine(theme adminTheme, width int) string {
	if !s.loaded {
		// The body says what went wrong while there is nothing else to show.
		return ""
	}
	if notice := securityNotice(theme, s.err, s.submitting, s.note, width); notice != "" {
		return notice
	}
	return securityStatusLine(theme, s.page, width)
}

func (s signingConfigScreen) policyLines(theme adminTheme, width int) []string {
	if !s.policyLoaded {
		text := "Loading…"
		if s.policyErr != "" {
			text = "Could not read the policy: " + s.policyErr
		}
		return securityNote(theme, text, width)
	}
	lines := []string{
		securityRow(theme, "Pull-time gate", securityFlag(theme, s.policy.Enabled, "enforced", "not enforced"), width),
		securityRow(theme, "Unsigned self-read", normalizeUnsignedSelfRead(s.policy.UnsignedSelfRead), width),
	}
	lines = append(lines, securityNote(theme, "Enforced: unsigned images cannot be pulled. self-read: off, pusher (whoever pushed that digest) or repo_push (anyone who can push). p edit policy.", width)...)
	if !s.policy.UpdatedAt.IsZero() {
		lines = append(lines, securityNote(theme, "Last saved "+s.policy.UpdatedAt.UTC().Format("2 Jan 2006 15:04 UTC")+".", width)...)
	}
	return lines
}

// trustLines lists the trusted keys and the keyless identities in at most rows
// lines: both lists get room, the longer one loses rows first.
func (s signingConfigScreen) trustLines(theme adminTheme, width, rows int) []string {
	if !s.policyLoaded {
		return []string{userSectionTitle(theme, "TRUSTED PUBLIC KEYS"), theme.muted.Render("Loading…")}
	}
	fingerprints := signingKeyFingerprints(s.policy.TrustedPublicKeys)
	identities := s.policy.TrustedIdentities

	// Fixed rows: two titles, two headers and the closing note.
	note := securityNote(theme, "Any one matching key or identity is enough.", width)
	room := max(rows-4-len(note), 2)
	keyRows, idRows := splitTrustRows(max(len(fingerprints), 1), max(len(identities), 1), room)

	typeW := len(signingKeyType)
	keyW := max(width-typeW-securityColGap, 1)
	lines := []string{
		userSectionTitle(theme, "TRUSTED PUBLIC KEYS"),
		theme.muted.Bold(true).Render(padText("FINGERPRINT", keyW) + strings.Repeat(" ", securityColGap) + "TYPE"),
	}
	lines = append(lines, trustRows(theme, len(fingerprints), keyRows, "No trusted keys.", func(i int) string {
		return padText(fingerprints[i], keyW) + strings.Repeat(" ", securityColGap) + signingKeyType
	})...)

	issuerW := min(44, max(width/2, 1))
	sanW := max(width-issuerW-securityColGap, 1)
	lines = append(lines,
		userSectionTitle(theme, "TRUSTED IDENTITIES (KEYLESS)"),
		theme.muted.Bold(true).Render(padText("IDENTITY (SAN REGEXP)", sanW)+strings.Repeat(" ", securityColGap)+"OIDC ISSUER"),
	)
	lines = append(lines, trustRows(theme, len(identities), idRows, "No trusted identities.", func(i int) string {
		return padText(identities[i].CertificateIdentityRegexp, sanW) + strings.Repeat(" ", securityColGap) + identities[i].CertificateOIDCIssuer
	})...)
	return append(lines, note...)
}

// splitTrustRows shares room rows between two lists that want a and b rows.
func splitTrustRows(a, b, room int) (int, int) {
	if a+b <= room {
		return a, b
	}
	half := room / 2
	switch {
	case a <= half:
		return a, room - a
	case b <= room-half:
		return room - b, b
	}
	return room - half, half
}

// trustRows draws up to rows lines of a list of count entries; when entries do
// not fit the last line says how many are hidden.
func trustRows(theme adminTheme, count, rows int, empty string, entry func(int) string) []string {
	if count == 0 {
		return []string{theme.muted.Render(empty)}
	}
	shown := min(count, rows)
	if count > rows {
		shown = max(rows-1, 0)
	}
	lines := make([]string, 0, rows)
	for i := range shown {
		lines = append(lines, theme.text.Render(entry(i)))
	}
	if count > shown {
		lines = append(lines, theme.muted.Render(fmt.Sprintf("+%d more, p opens the policy", count-shown)))
	}
	return lines
}

func (s signingConfigScreen) renderPolicyModal(theme adminTheme, env screenEnv) string {
	inner := securitySigningModalWidth - securityModalChrome
	cfg := s.cfg
	var body []string
	focusLine := 0
	at := func(field signingPolicyField) {
		if cfg.Focus == field {
			focusLine = len(body)
		}
	}

	at(signingPolicyFieldEnabled)
	body = append(body, securityToggleLine(theme, "Pull-time gate enforced", cfg.Enabled, cfg.Focus == signingPolicyFieldEnabled))
	at(signingPolicyFieldUnsignedSelfRead)
	body = append(body, securityChoiceField(theme, "Unsigned self-read (space cycles)", normalizeUnsignedSelfRead(cfg.UnsignedSelfRead), cfg.Focus == signingPolicyFieldUnsignedSelfRead, inner)...)

	at(signingPolicyFieldAddKey)
	if cfg.Focus == signingPolicyFieldAddKey {
		focusLine = len(body) + trustedKeyFocusRow(cfg.Keys)
	}
	body = append(body, renderTrustedKeyList(theme, cfg.Keys, cfg.Focus == signingPolicyFieldAddKey)...)

	at(signingPolicyFieldIdentities)
	if cfg.Focus == signingPolicyFieldIdentities {
		focusLine = len(body) + trustedIdentityFocusRow(cfg.Identities)
	}
	body = append(body, renderTrustedIdentityList(theme, cfg.Identities, cfg.Focus == signingPolicyFieldIdentities)...)

	at(signingPolicyFieldClearKeys)
	clear := "Clear all trusted keys"
	if len(cfg.Keys.Keys()) == 0 {
		clear = "No trusted keys to clear"
	}
	clearStyle := theme.muted
	if cfg.Focus == signingPolicyFieldClearKeys {
		clearStyle = theme.inputFocus.Width(0)
	}
	body = append(body, clearStyle.Render(clear))

	return securityModal(theme, env, securityModalSpec{
		title:     "Signing policy",
		subtitle:  fmt.Sprintf("Applies to every repository without an override. %s, %s configured.", securityCount(len(cfg.Keys.Keys()), "key", "keys"), securityCount(len(cfg.Identities.Identities()), "identity", "identities")),
		body:      body,
		focusLine: focusLine,
		err:       cfg.Error,
		note:      s.note,
		saving:    s.saving,
		hint:      "tab next field, space toggles, n adds and x deletes in a list",
		width:     securitySigningModalWidth,
	})
}
