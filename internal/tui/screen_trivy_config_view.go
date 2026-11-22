package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func (s trivyConfigScreen) View(theme adminTheme, env screenEnv) screenFrame {
	bodyW, bodyH := shellContentSize(env.Layout.Width, env.Layout.Height)

	head := []string{
		securityTitleLine(theme, "Image scanning", "Trivy, vulnerability scanner", bodyW),
		s.noticeLine(theme, bodyW),
	}
	var lines []string
	if !s.loaded {
		lines = append(head, s.tabsLine(theme, "", bodyW))
		lines = append(lines, securityLoadingBody(theme, s.err, bodyW)...)
	} else {
		leftW := max((bodyW-securityPanelGap)*3/5, 1)
		rightW := max(bodyW-securityPanelGap-leftW, 1)
		config, policy := s.configLines(theme, leftW-4), s.policyLines(theme, rightW-4, env)
		columns := func(top int) string {
			return lipgloss.JoinHorizontal(lipgloss.Top,
				securityPanel(theme, "TRIVY CONFIGURATION", leftW, top, config),
				strings.Repeat(" ", securityPanelGap),
				securityPanel(theme, "SCAN POLICY", rightW, top, policy),
			)
		}
		// LAST SWEEP is a card of its own between the two panels and the
		// overrides while the overrides keep their minimum rows; on a short
		// terminal the card gives way to its summary on the tabs row, so the
		// configuration and the overrides stay as they are.
		const headRows = 3
		available := bodyH - headRows
		top := max(len(config), len(policy)) + 3
		card, cardRows := s.sweepCard(theme, env, bodyW)
		if top+cardRows+securityOverridesMinRows <= available {
			lines = append(head, s.tabsLine(theme, "", bodyW), columns(top), card,
				securityOverridesPanel(theme, env, trivyFeatureName, s.overrides, bodyW, max(available-top-cardRows, 1)))
		} else {
			top = securityTopHeight(top, available)
			lines = append(head, s.tabsLine(theme, s.sweepSummary(theme, env), bodyW), columns(top),
				securityOverridesPanel(theme, env, trivyFeatureName, s.overrides, bodyW, max(available-top, 1)))
		}
	}

	frame := screenFrame{
		Context: "Image scanning",
		Full:    true,
		Body:    securityShell(theme, env, "Image scanning", shellNavImageScanning, lines, s.Keys()),
	}
	switch {
	case s.confirm.Active():
		frame.Overlay = s.confirm.view(theme)
	case s.cfg.Active():
		frame.Overlay = s.renderConfigModal(theme, env)
	case s.policyModal.Active():
		frame.Overlay = s.renderPolicyModal(theme, env)
	}
	return frame
}

// noticeLine is the failure, progress or outcome of the last action, else the
// state of the feature.
func (s trivyConfigScreen) noticeLine(theme adminTheme, width int) string {
	if !s.loaded {
		// The body says what went wrong while there is nothing else to show.
		return ""
	}
	if notice := securityNotice(theme, s.err, s.submitting, s.note, width); notice != "" {
		return notice
	}
	return securityStatusLine(theme, s.page, width)
}

// tabsLine marks Config as the open tab; Repositories is the other screen.
// right is muted text at the right end of the row.
func (s trivyConfigScreen) tabsLine(theme adminTheme, right string, width int) string {
	other := lipgloss.NewStyle().Padding(0, 1).Foreground(theme.muted.GetForeground())
	tabs := theme.selected.Render("Config") + " " + other.Render("Repositories")
	if right == "" {
		return tabs
	}
	right = fitText(right, max(width-lipgloss.Width(tabs)-2, 1))
	return tabs + strings.Repeat(" ", max(width-lipgloss.Width(tabs)-lipgloss.Width(right), 1)) + right
}

func (s trivyConfigScreen) configLines(theme adminTheme, width int) []string {
	config := securityPageFields(s.page, "config")
	scheduled := config["Schedule Enabled"] == "true"
	lines := []string{
		securityRow(theme, "Scheduled scans", securityFlag(theme, scheduled, "enabled", "disabled"), width),
		securityRow(theme, "Interval", securityDuration(config["Interval"]), width),
		securityRow(theme, "Timeout", securityDuration(config["Timeout"]), width),
		securityRow(theme, "Max concurrency", config["Max Concurrency"], width),
		securityRow(theme, "Registry reachable URL", config["Registry Reachable URL"], width),
	}
	return append(lines, securityNote(theme, "c edit configuration.", width)...)
}

func (s trivyConfigScreen) policyLines(theme adminTheme, width int, env screenEnv) []string {
	if !s.policyLoaded {
		text := "Loading…"
		if s.policyErr != "" {
			text = "Could not read the policy: " + s.policyErr
		}
		return securityNote(theme, text, width)
	}
	threshold := theme.severityHigh.Render(scanPolicyThresholdLabel(s.policy.SeverityThreshold))
	lines := []string{
		securityRow(theme, "Policy", securityFlag(theme, s.policy.Enabled, "enabled", "disabled"), width),
		securityRow(theme, "Severity threshold", threshold, width),
	}
	lines = append(lines, securityNote(theme, "A scan that reaches the threshold blocks the pull. p edit policy.", width)...)
	if !s.policy.UpdatedAt.IsZero() {
		lines = append(lines, securityNote(theme, "Last saved "+s.policy.UpdatedAt.UTC().Format("2 Jan 2006 15:04 UTC")+".", width)...)
	}
	return lines
}

func (s trivyConfigScreen) renderConfigModal(theme adminTheme, env screenEnv) string {
	inner := securityModalWidth - securityModalChrome
	focus := s.cfg.Focus
	body := []string{securityToggleLine(theme, "Scheduled scans", s.cfg.ScheduleEnabled, focus == trivyConfigFieldScheduleEnabled)}
	focusLine := 0
	field := func(label, value string, at trivyConfigField) {
		if focus == at {
			focusLine = len(body)
		}
		body = append(body, userField(theme, label, value, false, focus == at, inner)...)
	}
	field("Interval", s.cfg.Interval, trivyConfigFieldInterval)
	field("Timeout", s.cfg.Timeout, trivyConfigFieldTimeout)
	field("Registry Reachable URL", s.cfg.RegistryReachableURL, trivyConfigFieldRegistryReachableURL)
	field("Max Concurrency", s.cfg.MaxConcurrency, trivyConfigFieldMaxConcurrency)
	return securityModal(theme, env, securityModalSpec{
		title:     "Trivy configuration",
		subtitle:  "Applies to every repository without an override.",
		body:      body,
		focusLine: focusLine,
		err:       s.cfg.Error,
		saving:    s.saving,
		hint:      "tab next field, space toggles",
		width:     securityModalWidth,
	})
}

func (s trivyConfigScreen) renderPolicyModal(theme adminTheme, env screenEnv) string {
	inner := securityModalWidth - securityModalChrome
	body := []string{securityToggleLine(theme, "Policy enabled", s.policyModal.Enabled, s.policyModal.Focus == scanPolicyFieldEnabled)}
	body = append(body, securityChoiceField(theme, "Severity threshold", scanPolicyThresholdLabel(s.policyModal.SeverityThreshold), s.policyModal.Focus == scanPolicyFieldThreshold, inner)...)
	focusLine := 0
	if s.policyModal.Focus == scanPolicyFieldThreshold {
		focusLine = 1
	}
	return securityModal(theme, env, securityModalSpec{
		title:     "Vulnerability policy",
		subtitle:  "A scan that reaches the threshold blocks the pull. Applies to every repository without an override.",
		body:      body,
		focusLine: focusLine,
		err:       s.policyModal.Error,
		saving:    s.saving,
		hint:      "tab next field, space toggles or cycles",
		width:     securityModalWidth,
	})
}
