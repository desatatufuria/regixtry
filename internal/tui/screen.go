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
	// slotGitleaksConfig is Slice 1's proof screen (design.md Decision G):
	// gitleaks' own global config editor, migrated off
	// AdminViewState.GitleaksConfigModal.
	slotGitleaksConfig screenSlot = iota
	// slotTrivyOverride holds the uniform overrideEditor (design.md
	// Decision F) while open for Trivy's own Repository Alerts row -- an
	// overlay on screenAdminFeatures, mounted/unmounted exactly like
	// slotGitleaksConfig, not addressed via slotFor.
	slotTrivyOverride
	// slotGitleaksRepos/slotSigningRepos hold Gitleaks' and Signing's own
	// dedicated per-repository override list screens (design.md D3, D8) --
	// genuinely new top-level screens, addressed via slotFor.
	slotGitleaksRepos
	slotSigningRepos
	numScreenSlots
)

// adminScreenSet is an ARRAY, not a slice or map (design.md Decision B):
// Model is copied by value on every Update, and a reference type here would
// let a discarded copy observe another copy's mutation.
type adminScreenSet [numScreenSlots]adminScreen

// slotFor resolves a screen id to its migrated slot, if any. Slice 2
// migrates exactly two top-level screens (design.md D3, D8): Gitleaks' and
// Signing's own dedicated repository override list screens. Trivy's own
// override editor and Gitleaks' config editor stay mounted as overlays on
// the still-legacy screenAdminFeatures (slotTrivyOverride/
// slotGitleaksConfig), not addressed here — every other screen id
// updateAdminKey resolves still falls through to legacyScreenHandlers.
func slotFor(id screen) (screenSlot, bool) {
	switch id {
	case screenSecurityGitleaksRepos:
		return slotGitleaksRepos, true
	case screenSecuritySigningRepos:
		return slotSigningRepos, true
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
