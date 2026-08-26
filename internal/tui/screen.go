package tui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// screenKeys is this project's help.KeyMap implementation (design.md
// Decision D). The struct is declared here rather than in admin_keys.go
// (its natural Phase 4 home) because adminScreen's Keys() method below
// needs the type to exist before Phase 4 adds bubbles/help wiring
// (ShortHelp/FullHelp/matches/shortHelpView) — Go requires the type at
// every reference site, not only where it is fully fleshed out.
type screenKeys struct {
	short []key.Binding
	full  [][]key.Binding
}

// screenEnv is everything a migrated sub-model may READ from the outside
// world (design.md Decision A / Interfaces section). It is passed per call,
// never stored on a screen: a cached AdminSession goes stale on re-login or
// expiry, and a cached consoleLayout goes stale on resize.
type screenEnv struct {
	Client            AdminClient
	Session           AdminSession
	Layout            consoleLayout
	KnownRepositories []string
	Now               func() time.Time
}

// asModel builds a throwaway Model carrying only the fields this env's
// AdminClient-backed Cmd builders read (ctx/adminClient/adminSession) — it
// exists so a screenEnv-scoped confirm closure can reuse the Model's
// existing *Cmd methods verbatim instead of duplicating each one as a free
// function. m.ctx is always context.Background() in this codebase (NewModel
// never accepts one), so recreating it here is exactly equivalent to
// reading the live Model's own ctx field.
func (env screenEnv) asModel() Model {
	return Model{ctx: context.Background(), adminClient: env.Client, adminSession: env.Session}
}

// now returns env.Now(), falling back to the real wall clock when Now is
// nil -- a bare screenEnv{} literal (used pervasively by render-only unit
// tests that only care about a screen's Overlay, e.g. admin_views_test.go)
// never sets it, and a screen's View must stay safe to call in that shape.
func (env screenEnv) now() time.Time {
	if env.Now != nil {
		return env.Now()
	}
	return time.Now()
}

// screenFrame is a migrated sub-model's rendered output. There is
// deliberately NO Help field: the router renders the footer from
// s.Keys() via shortHelpView, which is what makes the spec's "help cannot
// drift" requirement structural rather than reviewed (design.md Decision A).
type screenFrame struct {
	Context string
	Body    string
	Overlay string // "" when the screen has no modal/overlay open
}

// adminScreen is the per-screen sub-model contract (design.md
// "Interfaces / Contracts"). Update returns the screen's NEW value — the
// parent replaces its held value, it never writes into a sub-model's fields
// directly (design.md Decision B). consumed reports whether a tea.KeyMsg was
// handled here; false lets the router fall through to the global key
// fallbacks, preserving today's precedence exactly.
type adminScreen interface {
	ID() screen
	Keys() screenKeys
	Init(env screenEnv) tea.Cmd
	Update(env screenEnv, msg tea.Msg) (next adminScreen, cmd tea.Cmd, consumed bool)
	View(theme adminTheme, env screenEnv) screenFrame
}

// screenSlot indexes adminScreenSet. Dense and compile-time bounded.
type screenSlot int

const (
	// slotGitleaksConfig held gitleaks' own global config editor as an
	// overlay through Slice 1/the Phase 12.3 follow-up batch (design.md
	// Decision G). Phase 11 promotes gitleaksConfigScreen to a properly
	// addressable top-level screen (screenSecurityGitleaksConfig), so this
	// slot is now resolved via slotFor like every other top-level screen
	// slot below, rather than read directly as an overlay.
	slotGitleaksConfig screenSlot = iota
	// slotGitleaksRepos/slotSigningRepos hold Gitleaks' and Signing's own
	// dedicated per-repository override list screens (design.md D3, D8) --
	// genuinely new top-level screens, addressed via slotFor.
	slotGitleaksRepos
	slotSigningRepos
	// slotSigningConfig held signingConfigScreen (Phase 12.3) as an overlay;
	// Phase 11 promotes it to a top-level screen
	// (screenSecuritySigningConfig) exactly like slotGitleaksConfig above.
	slotSigningConfig
	// slotSecurityMenu holds securityMenuScreen (Phase 11, design.md
	// Decision I): screenAdminFeatures repurposed as the bare 3-row
	// Security & Compliance domain menu.
	slotSecurityMenu
	// slotTrivyConfig/slotTrivyRepos hold Trivy's own peer screens (Phase
	// 11): trivyConfigScreen (screenSecurityTrivy, Runtime tab equivalent
	// plus config/scan-policy) and trivyReposScreen (screenSecurityTrivyRepos,
	// Repository Alerts tab equivalent). trivyReposScreen embeds its own
	// overrideEditor field directly (mirroring featureOverridesScreen),
	// so the former slotTrivyOverride overlay slot no longer exists.
	slotTrivyConfig
	slotTrivyRepos
	// slotAdminMenu/slotAdminOperations/slotScanRuns/slotSecretFindings hold
	// Phase 18/19's new domain-menu and Operations screens (design.md
	// Decision I, D9): screenAdminMenu (the 4-domain post-login landing
	// screen), screenAdminOperations (2 rows: Scan Runs/Secret Scan
	// Findings), and the two scanRunsScreen-backed repository pickers each
	// row navigates to.
	slotAdminMenu
	slotAdminOperations
	slotScanRuns
	slotSecretFindings
	// slotUpdateChannel holds updateChannelScreen (tui-update-check
	// feature): Operations' third row, the admin-only screen that sets the
	// server-side update channel PUT /admin/v1/update-channel writes to.
	slotUpdateChannel
	// slotScanHistory holds scanHistoryScreen (Phase 19, design.md's State
	// Migration table: "ScanHistoryModal -> scanHistoryScreen"). It is
	// deliberately NOT resolved via slotFor: like slotGitleaksConfig's own
	// Slice 1 precedent, it composites as a floating overlay over whichever
	// screen opened it (m.screen never changes while it is mounted) rather
	// than becoming the active top-level screen itself -- reached from TWO
	// openers (Trivy's Repository Alerts Enter, and Scan Runs/Secret Scan
	// Findings' own repository picker Enter), tracked via its own returnTo
	// field, not a screen id transition.
	slotScanHistory
	numScreenSlots
)

// adminScreenSet is an ARRAY, not a slice or map (design.md Decision B):
// Model is copied by value on every Update, and a reference type here would
// let a discarded copy observe another copy's mutation.
type adminScreenSet [numScreenSlots]adminScreen

// slotFor resolves a screen id to its migrated slot, if any (design.md
// Decision I / the Phase 11 resolved-gap addendum). screenAdminFeatures
// itself is repurposed as securityMenuScreen; Trivy/Gitleaks/Signing each
// gain their own peer screen(s), all addressed here. Every other screen id
// updateAdminKey resolves still falls through to legacyScreenHandlers.
func slotFor(id screen) (screenSlot, bool) {
	switch id {
	case screenAdminFeatures:
		return slotSecurityMenu, true
	case screenSecurityTrivy:
		return slotTrivyConfig, true
	case screenSecurityTrivyRepos:
		return slotTrivyRepos, true
	case screenSecurityGitleaksConfig:
		return slotGitleaksConfig, true
	case screenSecuritySigningConfig:
		return slotSigningConfig, true
	case screenSecurityGitleaksRepos:
		return slotGitleaksRepos, true
	case screenSecuritySigningRepos:
		return slotSigningRepos, true
	case screenAdminMenu:
		return slotAdminMenu, true
	case screenAdminOperations:
		return slotAdminOperations, true
	case screenAdminScanRuns:
		return slotScanRuns, true
	case screenAdminSecretFindings:
		return slotSecretFindings, true
	case screenAdminUpdateChannel:
		return slotUpdateChannel, true
	default:
		return 0, false
	}
}

// navigateMsg asks the router to switch the active top-level screen —
// Slice 2's first real use (design.md screen.go): a migrated screen's Esc
// asks the parent to switch m.screen back without ever writing it directly
// (Decision B: the parent replaces its held value, a child never reaches
// into the parent).
type navigateMsg struct {
	To screen
}

// navigate returns a tea.Cmd that emits navigateMsg{To}; the router owns
// m.screen and applies the transition when it observes the message.
func navigate(to screen) tea.Cmd {
	return func() tea.Msg { return navigateMsg{To: to} }
}

// openScanHistoryMsg asks the parent to mount scanHistoryScreen for one
// repository at slotScanHistory (Phase 19, design.md Data Flow "Secret
// findings, two entries, one sub-model", D9) -- mirrors navigateMsg's own
// "ask the parent" pattern (Decision B: a migrated screen never writes to
// state it does not own). Reached from Trivy's Repository Alerts Enter
// AND from the Scan Runs/Secret Scan Findings repository pickers' own
// Enter; returnTo records which screen opened it so Esc pops back to the
// correct caller (m.screen itself never changes while it is mounted,
// exactly like slotGitleaksConfig's Slice 1 overlay precedent); activeTab
// distinguishes the two D9 access flavors (vulnerabilities-first vs
// leaks-first) without a second screen or sub-model type.
type openScanHistoryMsg struct {
	repository string
	returnTo   screen
	activeTab  int
}

func openScanHistory(repository string, returnTo screen, activeTab int) tea.Cmd {
	return func() tea.Msg {
		return openScanHistoryMsg{repository: repository, returnTo: returnTo, activeTab: activeTab}
	}
}

// returnToInspectionMsg asks the parent to leave the admin panel back to
// Console inspection (Model.returnToInspection): Phase 18 makes
// screenAdminMenu the new admin root, so its own Esc now carries the
// pre-Phase-18 "leave the admin panel" behavior that used to live on
// screenAdminUsers. A migrated screen cannot call
// Model.returnToInspection() directly (Decision B: it mutates several
// Model-level fields beyond m.screen -- adminReturn, Confirm,
// UserSearchActive, revealed token state, adminIntent), so this mirrors
// navigateMsg's own "ask the parent" pattern.
type returnToInspectionMsg struct{}

func returnToInspectionCmd() tea.Cmd {
	return func() tea.Msg { return returnToInspectionMsg{} }
}

// newAdminScreenFor constructs the zero-value screen for id, used by
// navigateMsg's central handler (model.go) to lazily mount a target screen
// the first time the operator navigates to it (Enter from securityMenuScreen,
// Tab between Trivy's peer screens, 'o' into a repository override list,
// Esc back to an already-mounted screen is a no-op here since the slot is
// already occupied). Every migrated top-level screen this change ships is
// listed here; an id with no migrated screen returns nil.
func newAdminScreenFor(id screen) adminScreen {
	switch id {
	case screenAdminFeatures:
		return newSecurityMenuScreen()
	case screenSecurityTrivy:
		return newTrivyConfigScreen()
	case screenSecurityTrivyRepos:
		return newTrivyReposScreen()
	case screenSecurityGitleaksConfig:
		return newGitleaksConfigScreen()
	case screenSecuritySigningConfig:
		return newSigningConfigScreen()
	case screenSecurityGitleaksRepos:
		return newFeatureOverridesScreen(screenSecurityGitleaksRepos, gitleaksFeatureName)
	case screenSecuritySigningRepos:
		return newSigningReposScreen()
	case screenAdminMenu:
		return newAdminMenuScreen()
	case screenAdminOperations:
		return newAdminOperationsScreen()
	case screenAdminScanRuns:
		return newScanRunsScreen()
	case screenAdminSecretFindings:
		return newSecretFindingsRunsScreen()
	case screenAdminUpdateChannel:
		return newUpdateChannelScreen()
	default:
		return nil
	}
}
