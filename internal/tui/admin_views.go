package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"regixtry/internal/ports"
)

// renderAdminWorkspace renders the base admin workspace exactly once, then
// composites at most one active modal on top of it via a single
// compositeOverlay tail (design.md Decision 1: "One overlay tail, no modal
// budget arithmetic"). The base page always renders at its full, unshrunk
// layout -- exactly as if no modal were open -- and compositeOverlay
// (admin_overlay.go:63-110) already clamps overlayWidth/overlayHeight to the
// canvas, so a modal mathematically cannot grow page height; there is no
// per-modal budget function to keep in sync (the superseded design, still
// used only by the scan history modal below, needed real arithmetic because
// its table page size must be pre-built to match its own budget -- Confirm
// and Trivy render fixed content with no page sizing, so the compositor's
// own clamp is the only bound they need).
func renderAdminWorkspace(current screen, session AdminSession, view AdminViewState, knownRepositories []string, status string, layout consoleLayout, now time.Time) string {
	theme := newAdminTheme()

	context, body, help := renderAdminScreen(theme, current, session, view, knownRepositories, layout, now)
	// statusKindAuto preserves today's substring-classification behavior
	// (design.md Decision 2: 0 of ~11 renderInspectionWorkspace callers are
	// touched by the new kind param, and the admin workspace gets the same
	// treatment — only screenError needs an explicit kind).
	base := renderConsoleWorkspace("Regixtry Admin", context, body, status, help, statusKindAuto)

	var modalView string
	switch {
	case view.ScanHistoryModal.Active():
		// The scan history modal keeps its own nested budget
		// (adminScanHistoryModalRows): its table page size must be
		// pre-built to match this budget, since its content is
		// data-scrollable and cannot simply be clamped after the fact the
		// way compositeOverlay clamps fixed-content modals.
		baseBodyHeight := lipgloss.Height(body)
		modalView = renderAdminScanHistoryModal(theme, view.ScanHistoryModal, view, adminScanHistoryModalRows(layout, baseBodyHeight))
	case view.ConfirmModal.Active():
		modalView = renderAdminModal(theme, view.ConfirmModal)
	case view.TrivyConfigModal.Active():
		modalView = renderTrivyConfigModal(theme, view.TrivyConfigModal)
	case view.ScanPolicyModal.Active():
		modalView = renderScanPolicyModal(theme, view.ScanPolicyModal)
	}
	if modalView == "" {
		return base
	}
	return compositeOverlay(base, modalView, layout.Width, layout.Height)
}

// adminBaseBodyHeight measures the base Feature Page body's ACTUAL rendered
// height the same way renderAdminWorkspace's modal-open branch does
// (lipgloss.Height on renderAdminScreen's own body return), so callers that
// do not already have `body` in scope -- rebuildAdminTables (admin_tables.go)
// runs from Update handlers, before View() ever calls renderAdminScreen --
// can still measure the exact same thing renderAdminWorkspace measures.
// Both call sites feeding adminScanHistoryModalRows must agree on this value
// or the modal's table gets pre-built for one page size while
// renderAdminWorkspace composites the modal into a differently-sized budget.
func adminBaseBodyHeight(current screen, session AdminSession, view AdminViewState, knownRepositories []string, layout consoleLayout, now time.Time) int {
	theme := newAdminTheme()
	_, body, _ := renderAdminScreen(theme, current, session, view, knownRepositories, layout, now)
	return lipgloss.Height(body)
}

// adminScreenHelp returns the help line for an admin screen. Extracted from
// renderAdminScreen so callers can compute the same help text before layout
// is known (model.go's View() needs it to build the consoleLayout that
// renderAdminScreen itself then consumes).
func adminScreenHelp(current screen, view AdminViewState) string {
	switch current {
	case screenAdminCreateUser:
		return "Enter: create user | Tab: next field | Space: toggle | Esc: cancel"
	case screenAdminEditUser:
		return "g: grants | t: tokens | p: change password | e: enable | x: disable | Esc: back | q: quit"
	case screenAdminChangePassword:
		return "Enter: save password | Esc: cancel"
	case screenAdminEditUserGrants:
		return "n: add grant | e: edit selected grant | x: remove grant | t: tokens | Esc: back | q: quit"
	case screenAdminAddGrant:
		return "Up/Down: pick repository | Enter: accept or save | Tab: next field | Space: cycle role | Esc: cancel"
	case screenAdminEditUserTokens:
		return "n: create token | x: revoke token | g: grants | Esc: back | q: quit"
	case screenAdminCreateToken:
		return "Enter: create token | Tab: next field | Esc: cancel"
	case screenAdminFeatures:
		return adminFeatureHelp(view)
	default:
		return "/: search | Enter/e: edit user | n: create user | f: features | Esc: back | q: quit"
	}
}

// renderAdminScreen renders the current admin screen's (context, body, help)
// triple. Only renderAdminUsersScreen/renderAdminFeaturesScreen — the two
// screens whose body can grow with data (user list, Trivy tables) — take
// layout and route their content through renderSection's outer-pane clip
// (design.md decision #4/#6). The remaining screens are short, fixed-size
// forms that do not scale with data and stay on their existing
// theme.section.Render path (Phase 3 precedent: narrow scope to what the
// spec scenarios require).
func renderAdminScreen(theme adminTheme, current screen, session AdminSession, view AdminViewState, knownRepositories []string, layout consoleLayout, now time.Time) (string, string, string) {
	help := adminScreenHelp(current, view)
	switch current {
	case screenAdminCreateUser:
		return "Users / Create User", renderAdminCreateUserScreen(theme, view), help
	case screenAdminEditUser:
		return fmt.Sprintf("Users / %s / General", selectedAdminUsername(view)), renderAdminEditUserScreen(theme, session, view, now), help
	case screenAdminChangePassword:
		return fmt.Sprintf("Users / %s / Change Password", selectedAdminUsername(view)), renderAdminChangePasswordScreen(theme, view), help
	case screenAdminEditUserGrants:
		return fmt.Sprintf("Users / %s / Grants", selectedAdminUsername(view)), renderAdminGrantsScreen(theme, view), help
	case screenAdminAddGrant:
		return fmt.Sprintf("Users / %s / Grants / Add Grant", selectedAdminUsername(view)), renderAdminAddGrantScreen(theme, view, knownRepositories), help
	case screenAdminEditUserTokens:
		return fmt.Sprintf("Users / %s / Tokens", selectedAdminUsername(view)), renderAdminTokensScreen(theme, view), help
	case screenAdminCreateToken:
		return fmt.Sprintf("Users / %s / Tokens / Create Token", selectedAdminUsername(view)), renderAdminCreateTokenScreen(theme, view), help
	case screenAdminFeatures:
		return "Features", renderAdminFeaturesScreen(theme, session, view, layout, now), help
	default:
		return "Users", renderAdminUsersScreen(theme, session, view, layout, now), help
	}
}

func renderAdminUsersScreen(theme adminTheme, session AdminSession, view AdminViewState, layout consoleLayout, now time.Time) string {
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
	return renderSection(theme, strings.Join(lines, "\n"), layout)
}

func renderAdminFeaturesScreen(theme adminTheme, session AdminSession, view AdminViewState, layout consoleLayout, now time.Time) string {
	lines := []string{theme.subheading.Render("Built-in Features")}
	if len(view.Features) == 0 {
		lines = append(lines, theme.muted.Render("No built-in features available."))
	} else {
		lines = append(lines, view.Tables.Features.View())
	}

	lines = append(lines, "", theme.subheading.Render("Feature Page"))
	if strings.TrimSpace(view.FeaturePage.Summary.Name) == "" {
		lines = append(lines, theme.muted.Render("Select or refresh a feature to load the backend-declared page."))
	} else {
		if view.FeaturePage.Summary.Name == trivyFeatureName {
			lines = append(lines, renderTrivyTabs(theme, view))
		}
		lines = append(lines, renderFeaturePageBody(theme, view)...)
	}

	lines = append(lines,
		"",
		theme.muted.Render(fmt.Sprintf("Operator: %s", session.Username)),
		theme.muted.Render(fmt.Sprintf("Session remaining: %s", formatRemaining(session.Remaining(now)))),
	)
	return renderSection(theme, strings.Join(lines, "\n"), layout)
}

func renderFeaturePageBody(theme adminTheme, view AdminViewState) []string {
	if view.FeaturePage.Summary.Name == trivyFeatureName {
		if view.TrivyTab == trivyTabRepositoryAlerts {
			return renderAdminScanSummary(theme, view)
		}
		return renderGenericFeaturePage(theme, view, view.FeaturePage)
	}
	return renderGenericFeaturePage(theme, view, view.FeaturePage)
}

func renderGenericFeaturePage(theme adminTheme, view AdminViewState, page ports.FeaturePage) []string {
	lines := make([]string, 0, len(page.Header)+len(page.Sections)*2)
	for _, field := range page.Header {
		lines = append(lines, fmt.Sprintf("%s: %s", field.Label, adminFirstNonEmpty(field.Value, "unknown")))
	}
	if len(page.Sections) == 0 {
		lines = append(lines, theme.muted.Render("No additional feature details."))
		return lines
	}
	for _, section := range page.Sections {
		lines = append(lines, "", theme.subheading.Render(section.Title))
		switch section.Kind {
		case "rows":
			if tableModel, ok := view.Tables.FeatureRows[section.ID]; ok {
				lines = append(lines, tableModel.View())
				continue
			}
			lines = append(lines, theme.muted.Render("No rows available."))
		default:
			for _, field := range section.Fields {
				lines = append(lines, fmt.Sprintf("%s: %s", field.Label, adminFirstNonEmpty(field.Value, "unknown")))
			}
		}
	}
	return lines
}

func renderTrivyTabs(theme adminTheme, view AdminViewState) string {
	runtimeLabel := "Runtime"
	alertsLabel := "Repository Alerts"
	if view.TrivyTab == trivyTabRuntime {
		runtimeLabel = theme.selected.Render(runtimeLabel)
	} else {
		alertsLabel = theme.selected.Render(alertsLabel)
	}
	return theme.subheading.Render("Tabs") + "\n" + runtimeLabel + " | " + alertsLabel
}

// renderAdminScanSummary renders the Repository Alerts tab as one row per
// repository (spec.md "Repository Alerts Summarized Per Repository With
// Ordering And Freshness"), replacing the old per-scan-run list for this
// screen. Drill-down into a specific run's findings now happens exclusively
// through the scan history modal (renderAdminScanHistoryModal), opened by
// Enter on a summary row.
func renderAdminScanSummary(theme adminTheme, view AdminViewState) []string {
	lines := []string{theme.subheading.Render("Repository Alerts")}
	if len(view.TrivySummaries) == 0 {
		message := "No repository alerts found."
		if !view.TrivyAlertsLoaded {
			message = "Loading repository alerts requires switching into the tab."
		}
		return append(lines, theme.muted.Render(message))
	}
	return append(lines, view.Tables.ScanSummary.View())
}

// adminScanHistoryModalTabBar renders the modal's tab strip as a single
// line, guarded by TestRenderAdminScanHistoryModalChromeLinesAreSingleLine
// (design.md "Modal chrome accounting").
func adminScanHistoryModalTabBar(theme adminTheme, modal adminScanHistoryModal) string {
	if len(modal.Tabs) == 0 {
		return theme.muted.Render("No tabs available.")
	}
	labels := make([]string, 0, len(modal.Tabs))
	for index, tab := range modal.Tabs {
		label := tab.Label
		if index == boundedIndex(modal.ActiveTab, len(modal.Tabs)) {
			label = theme.selected.Render(label)
		}
		labels = append(labels, label)
	}
	return strings.Join(labels, " | ")
}

// adminScanHistoryModalExecutionsColumnStyle is the fixed-width container for
// the modal's left-hand executions rail: a right-only border (no top/bottom,
// so it never adds extra rows) draws the vertical divider between the rail
// and the existing right column, matching theme.section's own border color.
func adminScanHistoryModalExecutionsColumnStyle(theme adminTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(adminScanHistoryModalExecutionsColumnWidth).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(theme.borderColor)
}

// adminScanHistoryModalExecutionsColumn renders the modal's left-hand
// executions rail: a heading followed by a scrollable window
// (adminScanHistoryModalExecutionsWindow) of at most windowSize run rows, so
// the rail never unconditionally renders the full adminScanHistoryWindowLimit
// (50) history. The currently navigated run (modal.Cursor, via boundedIndex)
// is highlighted the same way adminScanHistoryModalTabBar highlights the
// active tab.
func adminScanHistoryModalExecutionsColumn(theme adminTheme, modal adminScanHistoryModal, windowSize int) string {
	heading := theme.subheading.Render("Executions")
	if len(modal.Runs) == 0 {
		return heading
	}

	cursor := boundedIndex(modal.Cursor, len(modal.Runs))
	start, end := adminScanHistoryModalExecutionsWindow(cursor, len(modal.Runs), windowSize)

	lines := []string{heading}
	for index := start; index < end; index++ {
		label := adminScanHistoryModalExecutionRowLabel(index, modal.Runs[index])
		if index == cursor {
			label = theme.selected.Render(label)
		}
		lines = append(lines, label)
	}
	return strings.Join(lines, "\n")
}

// adminScanHistoryModalFooter renders the history-position indicator (spec.md
// "Scan Execution History Navigation": "the TUI SHALL show 2/17, the run's
// date"), guarded to a single row.
func adminScanHistoryModalFooter(modal adminScanHistoryModal) string {
	if len(modal.Runs) == 0 {
		return "Execution 0/0"
	}
	cursor := boundedIndex(modal.Cursor, len(modal.Runs))
	run := modal.Runs[cursor]
	effTime, inProgress := effectiveScanRunTime(run)
	dateLabel := effTime.UTC().Format("2006-01-02 15:04")
	if inProgress {
		dateLabel += " (in progress)"
	}
	return fmt.Sprintf("Execution %d/%d — %s", cursor+1, len(modal.Runs), dateLabel)
}

// adminScanHistoryModalTableBody selects the active tab's table (Findings or
// SecretFindings, reused as-is from view.Tables — design.md interfaces) or a
// tab-appropriate empty state. When tableBudget cannot hold a bordered table
// at all, it returns a single-line substitute instead of ever slicing one
// (design.md "the table is replaced by a single-line substitute, never
// sliced" — the fix for the historical orphaned "Showing x-y of N" bug).
func adminScanHistoryModalTableBody(theme adminTheme, modal adminScanHistoryModal, view AdminViewState, tableBudget int) string {
	if len(modal.Tabs) == 0 {
		return theme.muted.Render("No tabs available.")
	}
	if tableBudget < adminScanHistoryModalMinTableBudget {
		return theme.muted.Render("Terminal too small to show the table.")
	}

	active := modal.Tabs[boundedIndex(modal.ActiveTab, len(modal.Tabs))]
	switch active.Kind {
	case adminScanHistoryTabLeaks:
		if len(modal.Secrets) == 0 {
			return theme.muted.Render("No secret findings recorded for this execution.")
		}
		return view.Tables.SecretFindings.View()
	default:
		if len(modal.Detail.Findings) == 0 {
			return theme.muted.Render("No findings recorded for this execution.")
		}
		return view.Tables.Findings.View()
	}
}

// renderAdminScanHistoryModal composes the scan history modal within its own
// content budget (modalRows, from adminScanHistoryModalRows): title,
// tab bar, the active tab's table (or empty/substitute state), and the
// history-position footer, forming the right column, alongside a left-hand
// executions rail (adminScanHistoryModalExecutionsColumn) joined via
// lipgloss.JoinHorizontal -- which auto-pads the shorter column with blank
// lines, so the two need not be hand-matched in line count. Every block is
// measured, not guessed, and the whole composite is wrapped by
// theme.section.Render directly rather than passed through
// renderSection/fitLines a second time (design.md "No fitLines over a
// composite containing a bordered block" — the bug that produced an orphaned
// "Showing x-y of N" line with no table above it). Each bordered block is
// clipped exactly once, by its own owner: the base screen body is clipped by
// the outer renderSection at baseRows; this modal never needs slicing
// because its table is pre-sized (via adminScanHistoryModalTablePageSize) to
// fit modalRows exactly, or replaced by a one-line substitute when it
// cannot.
func renderAdminScanHistoryModal(theme adminTheme, modal adminScanHistoryModal, view AdminViewState, modalRows int) string {
	title := theme.subheading.Render(fmt.Sprintf("Scan History — %s", adminFirstNonEmpty(modal.Repository, "unknown")))
	tabBar := adminScanHistoryModalTabBar(theme, modal)
	footer := theme.muted.Render(adminScanHistoryModalFooter(modal))
	// Kept deliberately terse (unlike some other screens' longer help lines):
	// this line must never become the modal's own widest rendered line, or
	// it silently drives the executions-side-panel's combined width (measured
	// empirically against TestModelScanHistoryModalRendersWithinViewportAcrossWidths).
	help := theme.help.Render("Tab/Shift+Tab: tabs | Left/Right: history | Up/Down: select | Enter/click: open | Esc: close")

	var header string
	switch {
	case strings.TrimSpace(modal.Error) != "":
		header = theme.error.Render(fmt.Sprintf("Error: %s", modal.Error))
	case modal.Loading:
		header = theme.muted.Render("Loading scan history...")
	}
	measuredHeaderHeight := 0
	if header != "" {
		measuredHeaderHeight = lipgloss.Height(header)
	}

	tableBudget := modalRows - adminScanHistoryModalChromeRows - measuredHeaderHeight
	tableBody := adminScanHistoryModalTableBody(theme, modal, view, tableBudget)

	lines := []string{title, tabBar}
	if header != "" {
		lines = append(lines, header)
	}
	lines = append(lines, tableBody, footer, help)
	rightColumn := strings.Join(lines, "\n")

	// The executions rail's window size is derived from the right column's
	// own measured height (title+tabBar+optional header+tableBody+footer+
	// help), minus one row for the rail's own "Executions" heading, so the
	// two columns stay close in height without hand-matching line counts
	// (lipgloss.JoinHorizontal below pads whichever column ends up shorter).
	executionsWindowSize := lipgloss.Height(rightColumn) - 1
	if executionsWindowSize < 1 {
		executionsWindowSize = 1
	}
	executionsColumn := adminScanHistoryModalExecutionsColumnStyle(theme).
		Render(adminScanHistoryModalExecutionsColumn(theme, modal, executionsWindowSize))

	combined := lipgloss.JoinHorizontal(lipgloss.Top, executionsColumn, rightColumn)

	return theme.section.Render(combined)
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

func renderTrivyConfigModal(theme adminTheme, modal trivyConfigModal) string {
	lines := []string{
		theme.subheading.Render("Edit Trivy Configuration"),
		renderToggleField(theme, "Schedule Enabled", modal.ScheduleEnabled, modal.Focus == trivyConfigFieldScheduleEnabled),
		renderTextField(theme, "Interval", modal.Interval, modal.Focus == trivyConfigFieldInterval),
		renderTextField(theme, "Timeout", modal.Timeout, modal.Focus == trivyConfigFieldTimeout),
		renderTextField(theme, "Registry Reachable URL", modal.RegistryReachableURL, modal.Focus == trivyConfigFieldRegistryReachableURL),
		renderTextField(theme, "Max Concurrency", modal.MaxConcurrency, modal.Focus == trivyConfigFieldMaxConcurrency),
	}
	if strings.TrimSpace(modal.Error) != "" {
		lines = append(lines, "", theme.error.Render(modal.Error))
	}
	lines = append(lines, "", theme.muted.Render("Enter: save | Tab: next field | Space: toggle | Esc: cancel"))
	return theme.section.Render(strings.Join(lines, "\n"))
}

// renderScanPolicyModal renders the vulnerability policy gate's own modal
// (design.md Decision 6): heading + 2 fields x 2 rows + blank/help = 7
// inner rows, +4 rows theme.section chrome = 11 total, 13 with an error
// present. It is a sibling of renderTrivyConfigModal, not an extension —
// the two never share fields or output (spec's "separate surface"
// requirement).
func renderScanPolicyModal(theme adminTheme, modal scanPolicyModal) string {
	lines := []string{
		theme.subheading.Render("Vulnerability Policy"),
		renderToggleField(theme, "Enabled", modal.Enabled, modal.Focus == scanPolicyFieldEnabled),
		renderTextField(theme, "Severity Threshold", scanPolicyThresholdLabel(modal.SeverityThreshold), modal.Focus == scanPolicyFieldThreshold),
	}
	if strings.TrimSpace(modal.Error) != "" {
		lines = append(lines, "", theme.error.Render(modal.Error))
	}
	lines = append(lines, "", theme.muted.Render("Enter: save | Tab: next field | Space: toggle/cycle | Esc: cancel"))
	return theme.section.Render(strings.Join(lines, "\n"))
}

// scanPolicyThresholdLabel renders the severity threshold as the operator-
// facing text the badge and modal both use (design.md Decision 6): CRITICAL
// / CRITICAL+HIGH. An unrecognized value renders as-is rather than
// panicking or hiding state — the admin API already rejects unknown values
// before they can reach here.
func scanPolicyThresholdLabel(threshold string) string {
	switch threshold {
	case ports.ScanPolicyThresholdCriticalHigh:
		return "CRITICAL+HIGH"
	case ports.ScanPolicyThresholdCritical:
		return "CRITICAL"
	default:
		return strings.ToUpper(threshold)
	}
}

func adminFeatureHelp(view AdminViewState) string {
	parts := []string{"Enter/r: refresh page"}
	if view.FeaturePage.Summary.Name == trivyFeatureName {
		parts = append(parts, "Tab: switch tabs")
		if view.TrivyTab == trivyTabRuntime {
			parts = append(parts, "c: configure")
		} else {
			parts = append(parts, "Up/Down: select alert", "Enter: details")
		}
		// p: policy is available on both Trivy tabs, matching the policy
		// badge composed into renderTrivyTabs, which is likewise visible on
		// both (design.md Decision 6).
		parts = append(parts, "p: policy")
	}
	parts = append(parts, strings.Split(featureActionHelp(view.FeaturePage), " | ")[1:]...)
	return strings.Join(parts, " | ")
}

// statusKind is an explicit style selector for renderAdminStatus, carried as
// a parameter along the render path rather than stored on Model (design.md
// Decision 2: m.status has 126 assignment sites, so a {Text,Kind} struct or
// a parallel m.statusKind field would either break all 126 call sites or go
// stale, since those 126 writers would never reset it). statusKindAuto
// preserves today's behavior — classify style from a substring match on the
// status text — for every caller that does not need an explicit kind.
type statusKind int

const (
	statusKindAuto statusKind = iota
	statusKindNeutral
	statusKindSuccess
	statusKindWarning
	statusKindError
)

// classifyStatusText is today's substring-match style selection, moved out
// of renderAdminStatus verbatim so it can serve as statusKindAuto's fallback
// (design.md Decision 2). It survives as the default for the ~126 untyped
// m.status assignment sites, demoted from sole mechanism to fallback.
func classifyStatusText(status string) statusKind {
	lower := strings.ToLower(status)
	switch {
	case strings.Contains(lower, "expired"), strings.Contains(lower, "invalid"), strings.Contains(lower, "error"):
		return statusKindError
	case strings.Contains(lower, "created"), strings.Contains(lower, "enabled"), strings.Contains(lower, "disabled"), strings.Contains(lower, "reset"), strings.Contains(lower, "saved"), strings.Contains(lower, "revoked"):
		return statusKindSuccess
	case strings.Contains(lower, "loading"), strings.Contains(lower, "refreshing"), strings.Contains(lower, "submitting"):
		return statusKindWarning
	default:
		return statusKindNeutral
	}
}

// statusStyle maps an explicit statusKind to its theme style. statusKindAuto
// is not a valid input here — callers must resolve it via classifyStatusText
// first (renderAdminStatus does this).
func statusStyle(theme adminTheme, kind statusKind) lipgloss.Style {
	switch kind {
	case statusKindError:
		return theme.error
	case statusKindSuccess:
		return theme.success
	case statusKindWarning:
		return theme.warning
	default:
		return theme.muted
	}
}

// renderAdminStatus selects its style from an explicit statusKind rather
// than a substring match on status text (design.md Decision 2, spec.md
// "Admin Status And Error Styling Uses An Explicit Status Kind").
// statusKindAuto classifies from the text, preserving today's behavior for
// callers that carry no explicit kind.
//
// Deliberately bare — no theme.section wrap, no "Status" subheading
// (design.md Decision 4, spec.md "Viewport-Bounded Screen Rendering": the
// status line MUST use the same bare, unboxed decoration as the help line
// beneath it). No constant accounts for the status panel's row cost:
// contentBudget (viewport.go) measures this function's real
// lipgloss.Height, so removing the box changes the measured chrome
// automatically -- do NOT decrement sectionChromeRows to "pay for" this;
// that constant is the BODY section's own border and is unrelated (see
// viewport.go's contentBudget doc comment).
func renderAdminStatus(theme adminTheme, status string, kind statusKind) string {
	if kind == statusKindAuto {
		kind = classifyStatusText(status)
	}
	return statusStyle(theme, kind).Render(status)
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
