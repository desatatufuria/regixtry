package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	bubbletable "github.com/evertras/bubble-table/table"
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
func renderAdminWorkspace(current screen, session AdminSession, view AdminViewState, knownRepositories []string, status string, layout consoleLayout, now time.Time, adminScreens adminScreenSet) string {
	theme := newAdminTheme()
	env := screenEnv{Session: session, Layout: layout, KnownRepositories: knownRepositories, Now: func() time.Time { return now }}

	// Migrated top-level screen (design.md Decision I / Data Flow): the
	// screen's own View() supplies context/body/help/overlay. Every other
	// screen id still resolves through the renderAdminScreen path
	// unchanged (design.md D5's adapter boundary).
	var context, body, help, screenOverlay string
	var fullScreen bool
	if slot, ok := slotFor(current); ok && adminScreens[slot] != nil {
		frame := adminScreens[slot].View(theme, env)
		context, body = frame.Context, frame.Body
		help = shortHelpView(theme, adminScreens[slot].Keys())
		screenOverlay = frame.Overlay
		fullScreen = frame.Full
	} else {
		context, body, help = renderAdminScreen(theme, current, session, view, knownRepositories, layout, now)
	}
	// statusKindAuto preserves today's substring-classification behavior
	// (design.md Decision 2: 0 of ~11 renderInspectionWorkspace callers are
	// touched by the new kind param, and the admin workspace gets the same
	// treatment — only screenError needs an explicit kind).
	base := renderConsoleWorkspace("Regixtry Admin", context, body, status, help, statusKindAuto)
	if fullScreen {
		// The screen drew everything itself (renderShell): no workspace
		// chrome around it.
		base = body
	}

	var modalView string
	switch {
	case adminScreens[slotScanHistory] != nil:
		// scanHistoryScreen (Phase 19) is never slotFor(current)-addressed
		// (screen.go's slotScanHistory doc comment): current/body above
		// already resolve to whichever screen opened it (m.screen never
		// changes while it is mounted), so this composites on top exactly
		// like the pre-migration ScanHistoryModal.Active() branch did. The
		// scan history overlay keeps its own nested budget
		// (adminScanHistoryModalRows): its table page size must be
		// pre-built to match this budget, since its content is
		// data-scrollable and cannot simply be clamped after the fact the
		// way compositeOverlay clamps fixed-content modals.
		if scan, ok := adminScreens[slotScanHistory].(scanHistoryScreen); ok {
			baseBodyHeight := lipgloss.Height(body)
			modalView = renderAdminScanHistoryModal(theme, scan.modal, scan.findings, scan.secretFindings, adminScanHistoryModalRows(layout, baseBodyHeight))
		}
	case view.Confirm.Active():
		modalView = view.Confirm.view(theme)
	case screenOverlay != "":
		// A migrated top-level screen's own overlay (design.md Decision A):
		// Trivy's/Gitleaks'/Signing's own config/scan-policy modals
		// (Phase 11, now that those screens are top-level and addressed via
		// slotFor at the top of this function), and Gitleaks'/Signing's
		// embedded overrideEditor while browsing their own repository list
		// screen -- every migrated screen's own overlay reaches the
		// workspace through the exact same frame.Overlay field.
		modalView = screenOverlay
	}
	if modalView == "" {
		return base
	}
	return compositeOverlay(base, modalView, layout.Width, layout.Height)
}

// adminScreenHelp returns the help line for an admin screen. Extracted from
// renderAdminScreen so callers can compute the same help text before layout
// is known (model.go's View() needs it to build the consoleLayout that
// renderAdminScreen itself then consumes).
func adminScreenHelp(current screen, view AdminViewState) string {
	switch current {
	case screenAdminEditUserTokens:
		return "n: create token | x: revoke token | g: grants | Esc: back | q: quit"
	case screenAdminCreateToken:
		return "Enter: create token | Tab: next field | Esc: cancel"
	case screenRepoAdminGrants:
		return "n: add grant | e: edit selected grant | x: remove grant | Esc: back | q: quit"
	case screenRepoAdminAddGrant:
		return "Enter: save | Tab: next field | Space: cycle role | Esc: cancel"
	case screenAdminRobots:
		return "n: create robot | e: enable | x: disable | t: tokens | g: grant access | r: refresh | Esc: back | q: quit"
	case screenAdminCreateRobot:
		return "Enter: create robot | Tab: next field | Space: cycle role | Esc: cancel"
	default:
		return "Esc: back | q: quit"
	}
}

// renderAdminScreen renders the current admin screen's (context, body, help)
// triple. Only renderAdminUsersScreen -- the screen whose body can grow with
// data (the user list) -- takes layout and routes its content through
// renderSection's outer-pane clip
// (design.md decision #4/#6). The remaining screens are short, fixed-size
// forms that do not scale with data and stay on their existing
// theme.section.Render path (Phase 3 precedent: narrow scope to what the
// spec scenarios require).
func renderAdminScreen(theme adminTheme, current screen, session AdminSession, view AdminViewState, knownRepositories []string, layout consoleLayout, now time.Time) (string, string, string) {
	help := adminScreenHelp(current, view)
	switch current {
	case screenAdminEditUserTokens:
		return adminContext("Access", "Users", selectedAdminUsername(view), "Tokens"), renderAdminTokensScreen(theme, view), help
	case screenAdminCreateToken:
		return adminContext("Access", "Users", selectedAdminUsername(view), "Tokens", "Create token"), renderAdminCreateTokenScreen(theme, view, now), help
	case screenRepoAdminGrants:
		return fmt.Sprintf("Repositories / %s / Grants", view.RepoAdminRepository), renderRepoAdminGrantsScreen(theme, view), help
	case screenRepoAdminAddGrant:
		return fmt.Sprintf("Repositories / %s / Grants / Add Grant", view.RepoAdminRepository), renderRepoAdminAddGrantScreen(theme, view), help
	case screenAdminRobots:
		return adminContext("Access", "Robots"), renderAdminRobotsScreen(theme, session, view, layout, now), help
	case screenAdminCreateRobot:
		return adminContext("Access", "Robots", "Create robot"), renderAdminCreateRobotScreenWithin(theme, view, knownRepositories, now, layout.SectionRows), help
	default:
		return adminContext("Access"), "", help
	}
}

// renderAdminOperatorFooter is the trailing "Operator: %s" / "Session
// remaining: %s" pair the Robots screen renders (see
// renderAdminRobotsScreen below). The migrated Security
// screens (trivyConfigScreen, trivyReposScreen, gitleaksConfigScreen,
// signingConfigScreen) share it via this helper so the operator never loses
// their session countdown while navigating the Security screens.
func renderAdminOperatorFooter(theme adminTheme, session AdminSession, now time.Time) []string {
	return []string{
		"",
		theme.muted.Render(fmt.Sprintf("Operator: %s", session.Username)),
		theme.muted.Render(fmt.Sprintf("Session remaining: %s", formatRemaining(session.Remaining(now)))),
	}
}

// renderFeaturePageBody moved onto trivyConfigScreen/gitleaksConfigScreen/
// signingConfigScreen's own View methods (Phase 11, design.md Decision I).

// renderGenericFeaturePage renders a backend-declared ports.FeaturePage's
// generic body: header fields, then each section (a "rows" section renders
// the caller's already-built bubbletable.Model for that section, keyed by
// ID; every other kind renders its fields inline). Phase 11 deviation from
// the pre-change signature: takes rows map[string]bubbletable.Model
// directly instead of a full AdminViewState, since Tables.FeatureRows moved
// onto each of trivyConfigScreen/gitleaksConfigScreen/signingConfigScreen's
// own `rows` field (design.md Decision D4: no shared cross-screen state).
func renderGenericFeaturePage(theme adminTheme, page ports.FeaturePage, rows map[string]bubbletable.Model) []string {
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
			if tableModel, ok := rows[section.ID]; ok {
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

// renderTrivyTabs composes the persistent policy status badge onto its
// existing tab line (design.md Decision 6), which returns exactly 2 rows
// and keeps returning 2 — the badge costs 0 rows because it is appended to
// the same line, not placed on a line of its own. contentBudget/fitLines/
// SectionRows need no change (spec's explicit "no arithmetic change"
// scope note).
//
// Phase 11 deviation from the pre-change signature: "Tab becomes a screen
// switch" (design.md Decision I) means Runtime/Repository Alerts are now
// two screens (screenSecurityTrivy/screenSecurityTrivyRepos) rather than
// one AdminViewState.TrivyTab field, so this takes the ACTIVE screen id
// directly. Called from both trivyConfigScreen and trivyReposScreen's own
// View (design's "retained as a header on both") -- each passes its own
// read-only copy of ScanPolicy (Decision D4: no shared cross-screen state;
// trivyReposScreen's copy is display-only, it owns no policyModal).
func renderTrivyTabs(theme adminTheme, active screen, policy ports.ScanPolicySettings) string {
	runtimeLabel := "Runtime"
	alertsLabel := "Repository Alerts"
	if active == screenSecurityTrivy {
		runtimeLabel = theme.selected.Render(runtimeLabel)
	} else {
		alertsLabel = theme.selected.Render(alertsLabel)
	}
	return theme.subheading.Render("Tabs") + "\n" + runtimeLabel + " | " + alertsLabel + "  " + scanPolicyBadge(theme, policy)
}

// scanPolicyBadge renders the persistent, text-only policy status badge
// (spec.md "Persistent Policy Status Badge" — text, not an icon or glyph).
func scanPolicyBadge(theme adminTheme, policy ports.ScanPolicySettings) string {
	if !policy.Enabled {
		return theme.muted.Render("Policy: OFF")
	}
	return theme.selected.Render(fmt.Sprintf("Policy: ON (%s)", scanPolicyThresholdLabel(policy.SeverityThreshold)))
}

// renderAdminScanSummary renders the Repository Alerts tab as one row per
// repository (spec.md "Repository Alerts Summarized Per Repository With
// Ordering And Freshness"). Drill-down into a specific run's findings
// happens exclusively through the scan history modal
// (renderAdminScanHistoryModal), opened by Enter on a summary row.
//
// Phase 11 deviation from the pre-change signature: takes the already-loaded
// summaries/loaded flag/table directly instead of a full AdminViewState,
// mirroring renderGenericFeaturePage's own signature change now that these
// live on trivyReposScreen's own fields.
func renderAdminScanSummary(theme adminTheme, summaries []repositorySummary, loaded bool, table bubbletable.Model) []string {
	lines := []string{theme.subheading.Render("Repository Alerts")}
	if len(summaries) == 0 {
		message := "No repository alerts found."
		if !loaded {
			message = "Loading repository alerts..."
		}
		return append(lines, theme.muted.Render(message))
	}
	return append(lines, table.View())
}

// renderFeatureOverridesTable mirrors renderAdminScanSummary's shape for
// featureOverridesScreen (screen_gitleaks_repos.go): a bubble-table body in
// place of the old plain "Repository — status" line list. loaded/empty
// precedence matches the screen's own pre-existing behavior exactly (a
// not-yet-loaded screen reports "Loading...", never the empty state, even
// though rows is naturally empty in both cases).
func renderFeatureOverridesTable(theme adminTheme, feature string, rows []featureOverrideRow, loaded bool, table bubbletable.Model) []string {
	lines := []string{theme.subheading.Render(featureDisplayName(feature) + " — Repository Overrides")}
	switch {
	case !loaded:
		return append(lines, theme.muted.Render("Loading repository overrides..."))
	case len(rows) == 0:
		return append(lines, theme.muted.Render("No repositories available to override."))
	default:
		return append(lines, table.View())
	}
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

// adminScanHistoryModalTableBody selects the active tab's table (findings or
// secretFindings, owned by scanHistoryScreen itself since Phase 19 -- design.md
// Decision B) or a tab-appropriate empty state. When tableBudget cannot hold
// a bordered table at all, it returns a single-line substitute instead of
// ever slicing one (design.md "the table is replaced by a single-line
// substitute, never sliced" — the fix for the historical orphaned
// "Showing x-y of N" bug).
func adminScanHistoryModalTableBody(theme adminTheme, modal adminScanHistoryModal, findings, secretFindings bubbletable.Model, tableBudget int) string {
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
		return secretFindings.View()
	default:
		if len(modal.Detail.Findings) == 0 {
			return theme.muted.Render("No findings recorded for this execution.")
		}
		return findings.View()
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
func renderAdminScanHistoryModal(theme adminTheme, modal adminScanHistoryModal, findings, secretFindings bubbletable.Model, modalRows int) string {
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
	tableBody := adminScanHistoryModalTableBody(theme, modal, findings, secretFindings, tableBudget)

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

// renderRepoAdminGrantsScreen renders the repo-admin delegate's own
// repository's grants (design.md Decision 7 / spec.md "Delegate sees only
// their own repositories' grants"). It is keyed by
// RepoAdminRepository, not SelectedUserID.
func renderRepoAdminGrantsScreen(theme adminTheme, view AdminViewState) string {
	if strings.TrimSpace(view.RepoAdminRepository) == "" {
		return theme.section.Render(theme.warning.Render("Select a repository before opening grants."))
	}
	lines := []string{
		theme.subheading.Render("Repository Grants"),
		fmt.Sprintf("Repository: %s", view.RepoAdminRepository),
		"",
	}
	if len(view.RepoAdminGrants) == 0 {
		lines = append(lines, theme.muted.Render("No grants for this repository."))
	} else {
		for index, grant := range view.RepoAdminGrants {
			label := fmt.Sprintf("%s | %s", grant.Username, grant.Role)
			if index == view.SelectedRepoAdminGrant {
				label = theme.selected.Render(label)
			}
			lines = append(lines, label)
		}
	}
	return theme.section.Render(strings.Join(lines, "\n"))
}

// renderRepoAdminAddGrantScreen renders the delegate's username+role form.
// It never offers a repository field (RepoAdminRepository is fixed context,
// not editable here) and the Role value it displays can never be
// domainauth.RepoRoleAdmin -- that guarantee lives in nextDelegateGrantRole,
// the only function allowed to change this field (spec.md "Delegate cannot
// select repo-admin in the grant role picker").
func renderRepoAdminAddGrantScreen(theme adminTheme, view AdminViewState) string {
	return theme.section.Render(strings.Join([]string{
		theme.subheading.Render("Grant Details"),
		fmt.Sprintf("Repository: %s", view.RepoAdminRepository),
		renderTextField(theme, "Username", view.RepoAdminGrantForm.Username, view.RepoAdminGrantForm.Focus == adminRepoGrantFieldUsername),
		renderTextField(theme, "Role", string(view.RepoAdminGrantForm.Role), view.RepoAdminGrantForm.Focus == adminRepoGrantFieldRole),
	}, "\n"))
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

// renderAdminRobotsScreen renders screenAdminRobots (design.md Decision 7),
// mirroring renderAdminUsersScreen's list shape: one line per robot, the
// selected row highlighted, an Operator/session-remaining footer. Keyed by
// repository/role/enabled state instead of admin/read-only flags, since a
// robot's identity is its single repository grant, not a global role.
func renderAdminRobotsScreen(theme adminTheme, session AdminSession, view AdminViewState, layout consoleLayout, now time.Time) string {
	lines := []string{theme.subheading.Render("Robots")}
	if len(view.Robots) == 0 {
		lines = append(lines, theme.muted.Render("No robot accounts available."))
	} else {
		for index, robot := range view.Robots {
			label := formatAdminRobotLabel(robot)
			if index == view.SelectedRobot {
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

// formatAdminRobotLabel renders a robot as "name [repository, role, state]".
func formatAdminRobotLabel(robot ports.AdminRobot) string {
	state := "disabled"
	if robot.Enabled {
		state = "enabled"
	}
	return fmt.Sprintf("%s [%s, %s, %s]", robot.Username, robot.Repository, robot.Role, state)
}

// renderAdminCreateRobotScreen renders screenAdminCreateRobot (design.md
// Decision 7). Immediately after a successful creation, RevealedTokenSecret/
// Accessor are populated (the exact fields renderAdminTokensScreen already
// reveals once for human admin tokens) and rendered here exactly once, above
// the (now-cleared) form -- mirroring renderAdminTokensScreen's own
// conditional block so this reveal follows the one already-audited pattern
// instead of introducing a new one.
// renderAdminCreateRobotScreen renders a windowed, highlighted
// repository-suggestion list under its Repository field.
func renderAdminCreateRobotScreen(theme adminTheme, view AdminViewState, knownRepositories []string, now time.Time) string {
	return renderAdminCreateRobotScreenWithin(theme, view, knownRepositories, now, 0)
}

// renderAdminCreateRobotScreenWithin renders the Create Robot form inside a
// budget of `rows` body rows (consoleLayout.SectionRows; 0 means unbounded).
// The one-time secret, the Lifetime rows and the form fields are never cut:
// only the suggestion list shrinks (selected row first, then a muted "+N more"
// when rows allow), and the blank separators are dropped when even that is not
// enough.
func renderAdminCreateRobotScreenWithin(theme adminTheme, view AdminViewState, knownRepositories []string, now time.Time, rows int) string {
	suggestions := robotRepositorySuggestions(view.CreateRobotForm, knownRepositories)
	hasSecret := strings.TrimSpace(view.RevealedTokenSecret) != ""

	build := func(compactSecret bool) []string {
		lines := []string{theme.subheading.Render("Create Robot")}
		if hasSecret {
			if !compactSecret {
				lines = append(lines, "")
			}
			lines = append(lines,
				theme.success.Render("One-time secret"),
				fmt.Sprintf("Accessor: %s", view.RevealedTokenAccessor),
				theme.text.Render(view.RevealedTokenSecret),
			)
			if !compactSecret {
				lines = append(lines, "")
			}
		}
		return append(lines,
			renderTextField(theme, "Name", view.CreateRobotForm.Name, view.CreateRobotForm.Focus == adminCreateRobotFieldName),
			renderTextField(theme, "Repository", view.CreateRobotForm.Repository, view.CreateRobotForm.Focus == adminCreateRobotFieldRepository),
			renderTextField(theme, "Role", string(view.CreateRobotForm.Role), view.CreateRobotForm.Focus == adminCreateRobotFieldRole),
			renderLifetimeField(theme, view.CreateRobotForm.Lifetime, now, view.CreateRobotForm.Focus == adminCreateRobotFieldLifetime),
		)
	}
	height := func(lines []string) int { return strings.Count(strings.Join(lines, "\n"), "\n") + 1 }

	lines := build(false)
	avail := 0 // rows left for the suggestions; 0 with rows == 0 means unbounded
	if rows > 0 {
		avail = rows - height(lines)
		// Needed for one suggestion under its header: blank + header + row.
		if avail < 3 && hasSecret && len(suggestions) > 0 {
			lines = build(true)
			avail = rows - height(lines)
		}
	}

	if len(suggestions) == 0 {
		if rows == 0 || avail >= 1 {
			lines = append(lines, theme.muted.Render("No known repositories match the current filter."))
		}
		return theme.section.Render(strings.Join(lines, "\n"))
	}

	window := 5
	if window > len(suggestions) {
		window = len(suggestions)
	}
	withBlank, withHeader := true, true
	moreLine := false
	if rows > 0 {
		switch {
		case avail >= 3:
			if room := avail - 2; room < window {
				// Rows cut the list: keep one row for "+N more" when at least
				// two suggestions still fit next to it.
				window = room
				if room >= 3 {
					window = room - 1
					moreLine = true
				}
			}
		case avail == 2:
			withBlank, window = false, 1
		default:
			withBlank, withHeader, window = false, false, 1
		}
	}
	selected := boundedIndex(view.CreateRobotForm.RepositorySuggestion, len(suggestions))
	start := 0
	if selected >= window {
		start = selected - window + 1
	}
	end := start + window
	if end > len(suggestions) {
		end = len(suggestions)
	}
	if withBlank {
		lines = append(lines, "")
	}
	if withHeader {
		lines = append(lines, theme.subheading.Render("Known Repositories"))
	}
	for index := start; index < end; index++ {
		label := suggestions[index]
		if index == selected {
			label = theme.selected.Render(label)
		}
		lines = append(lines, label)
	}
	if moreLine {
		lines = append(lines, theme.muted.Render(fmt.Sprintf("+%d more", len(suggestions)-window)))
	}
	return theme.section.Render(strings.Join(lines, "\n"))
}

func renderAdminCreateTokenScreen(theme adminTheme, view AdminViewState, now time.Time) string {
	return theme.section.Render(strings.Join([]string{
		theme.subheading.Render("Create Token"),
		fmt.Sprintf("User: %s", selectedAdminUsername(view)),
		renderTextField(theme, "Name", view.TokenForm.Name, view.TokenForm.Focus == adminTokenFieldName),
		renderLifetimeField(theme, view.TokenForm.Lifetime, now, view.TokenForm.Focus == adminTokenFieldLifetime),
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

// renderGitleaksConfigModal moved to gitleaksConfigScreen.View
// (screen_gitleaks_config.go, design.md Decision G).

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

// signingPolicyBadge renders the image-signing content-trust gate's
// persistent, text-only policy status badge, mirroring scanPolicyBadge's
// exact shape (design.md Decision 11 piece 1) -- text, not an icon or
// glyph.
func signingPolicyBadge(theme adminTheme, policy ports.SigningPolicySettings) string {
	if !policy.Enabled {
		return theme.muted.Render("Signing: OFF")
	}
	return theme.selected.Render(fmt.Sprintf("Signing: REQUIRED (%d keys, %d identities)", len(policy.TrustedPublicKeys), len(policy.TrustedIdentities)))
}

// renderSigningPolicyModal/signingPolicyStatusLine/renderSigningPolicyKeyList/
// renderSigningPolicyClearKeysRow moved to screen_signing_config.go (Phase
// 12.3, design.md's State Migration table): signingConfigScreen.View calls
// renderSigningPolicyModal directly, with the pre-move hand-written footer
// replaced by shortHelpView's keymap-generated one.

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

// renderRepositoryOverrideModal/repositoryOverrideStatusLine/
// renderRepositoryOverrideClearRow were retired by tui-menu-architecture
// (design.md Decision F). The uniform per-repository override editor now
// renders through renderOverrideEditor/overrideStatusLine/
// renderOverrideClearRow (override_editor.go), with no Feature row.

// Each of trivyConfigScreen/gitleaksConfigScreen/signingConfigScreen composes
// its own Keys() directly (screen_trivy_config.go, screen_gitleaks_config.go,
// screen_signing_config.go), rendered through shortHelpView so help can never
// drift from what a key actually does (design.md Decision A).

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

func selectedRepoAdminGrantForView(view AdminViewState) (ports.AdminRepositoryGrant, bool) {
	if len(view.RepoAdminGrants) == 0 {
		return ports.AdminRepositoryGrant{}, false
	}
	index := boundedIndex(view.SelectedRepoAdminGrant, len(view.RepoAdminGrants))
	return view.RepoAdminGrants[index], true
}

func selectedTokenForView(view AdminViewState) (ports.AdminToken, bool) {
	if len(view.AdminTokens) == 0 {
		return ports.AdminToken{}, false
	}
	index := boundedIndex(view.SelectedToken, len(view.AdminTokens))
	return view.AdminTokens[index], true
}

func adminFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
