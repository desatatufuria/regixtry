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
// per-modal budget function to keep in sync. The scan history is not a
// modal: it is a whole screen that replaces the base (see below).
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
		// scanHistoryScreen is never slotFor(current)-addressed (screen.go's
		// slotScanHistory doc comment): it draws the whole shell over
		// whichever screen opened it, so it replaces the base entirely.
		if scan, ok := adminScreens[slotScanHistory].(scanHistoryScreen); ok {
			return scan.viewFull(theme, env, status).Body
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
	case screenRepoAdminGrants:
		return "n: add grant | e: edit selected grant | x: remove grant | Esc: back | q: quit"
	case screenRepoAdminAddGrant:
		return "Enter: save | Tab: next field | Space: cycle role | Esc: cancel"
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
	case screenRepoAdminGrants:
		return fmt.Sprintf("Repositories / %s / Grants", view.RepoAdminRepository), renderRepoAdminGrantsScreen(theme, view), help
	case screenRepoAdminAddGrant:
		return fmt.Sprintf("Repositories / %s / Grants / Add Grant", view.RepoAdminRepository), renderRepoAdminAddGrantScreen(theme, view), help
	default:
		return adminContext("Access"), "", help
	}
}

// renderAdminOperatorFooter is the trailing "Operator: %s" / "Session
// remaining: %s" pair the Users screen renders (see
// renderAdminUsersScreen below). The migrated Security
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

func selectedRepoAdminGrantForView(view AdminViewState) (ports.AdminRepositoryGrant, bool) {
	if len(view.RepoAdminGrants) == 0 {
		return ports.AdminRepositoryGrant{}, false
	}
	index := boundedIndex(view.SelectedRepoAdminGrant, len(view.RepoAdminGrants))
	return view.RepoAdminGrants[index], true
}

func adminFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
