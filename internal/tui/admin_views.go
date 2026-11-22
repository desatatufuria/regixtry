package tui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
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
func renderAdminWorkspace(current screen, session AdminSession, knownRepositories []string, status string, layout consoleLayout, now time.Time, adminScreens adminScreenSet) string {
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
		context, help = adminContext("Access"), "Esc: back | q: quit"
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

func adminFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
