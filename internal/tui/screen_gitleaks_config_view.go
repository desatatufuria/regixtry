package tui

func (s gitleaksConfigScreen) View(theme adminTheme, env screenEnv) screenFrame {
	bodyW, bodyH := shellContentSize(env.Layout.Width, env.Layout.Height)

	lines := []string{
		securityTitleLine(theme, "Secret scanning", "Gitleaks, leaked-secret scanner", bodyW),
		s.noticeLine(theme, bodyW),
	}
	if !s.loaded {
		lines = append(lines, securityLoadingBody(theme, s.err, bodyW)...)
	} else {
		config := s.configLines(theme, bodyW-4)
		top := securityTopHeight(len(config)+3, bodyH-len(lines))
		lines = append(lines,
			securityPanel(theme, "GITLEAKS CONFIGURATION", bodyW, top, config),
			securityOverridesPanel(theme, env, gitleaksFeatureName, s.overrides, bodyW, max(bodyH-len(lines)-top, 1)))
	}

	frame := screenFrame{
		Context: "Secret scanning",
		Full:    true,
		Body:    securityShell(theme, env, "Secret scanning", shellNavSecretScanning, lines, s.Keys()),
	}
	switch {
	case s.confirm.Active():
		frame.Overlay = s.confirm.view(theme)
	case s.cfg.Active():
		frame.Overlay = s.renderConfigModal(theme, env)
	}
	return frame
}

func (s gitleaksConfigScreen) noticeLine(theme adminTheme, width int) string {
	if !s.loaded {
		// The body says what went wrong while there is nothing else to show.
		return ""
	}
	if notice := securityNotice(theme, s.err, s.submitting, s.note, width); notice != "" {
		return notice
	}
	return securityStatusLine(theme, s.page, width)
}

func (s gitleaksConfigScreen) configLines(theme adminTheme, width int) []string {
	config := securityPageFields(s.page, "config")
	lines := []string{
		securityRow(theme, "Secret scanning", securityFlag(theme, s.page.Summary.Enabled, "enabled", "disabled"), width),
		securityRow(theme, "Timeout", securityDuration(config["Timeout"]), width),
		securityRow(theme, "Max concurrency", config["Max Concurrency"], width),
	}
	return append(lines, securityNote(theme, "c edit configuration.", width)...)
}

func (s gitleaksConfigScreen) renderConfigModal(theme adminTheme, env screenEnv) string {
	inner := securityModalWidth - securityModalChrome
	focus := s.cfg.Focus
	body := []string{securityToggleLine(theme, "Enabled", s.cfg.Enabled, focus == gitleaksConfigFieldEnabled)}
	focusLine := 0
	field := func(label, value string, at gitleaksConfigField) {
		if focus == at {
			focusLine = len(body)
		}
		body = append(body, userField(theme, label, value, false, focus == at, inner)...)
	}
	field("Timeout", s.cfg.Timeout, gitleaksConfigFieldTimeout)
	field("Max Concurrency", s.cfg.MaxConcurrency, gitleaksConfigFieldMaxConcurrency)
	return securityModal(theme, env, securityModalSpec{
		title:     "Gitleaks configuration",
		subtitle:  "Applies to every repository without an override.",
		body:      body,
		focusLine: focusLine,
		err:       s.cfg.Error,
		saving:    s.saving,
		hint:      "tab next field, space toggles",
		width:     securityModalWidth,
	})
}
