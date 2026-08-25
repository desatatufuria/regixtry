package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"regixtry/internal/ports"
)

// overrideField identifies which of overrideEditor's fields has focus
// (design.md Decision F). Unlike the retired repositoryOverrideField enum,
// there is deliberately NO Feature entry here: Feature is fixed at
// construction (newOverrideEditor) and unrepresentable as a focusable field,
// not merely unreachable by a deleted cycle key.
type overrideField int

const (
	overrideFieldEnabled          overrideField = iota
	overrideFieldPathPrimary                    // trivy: ignore file | gitleaks: config | signing: trusted key
	overrideFieldPathSecondary                  // trivy only
	overrideFieldUnsignedSelfRead               // signing only
	overrideFieldClear                          // action row, not an input
)

// overrideFieldsForFeature returns the ordered, per-feature field set built
// ONCE at construction (design.md Decision F) -- the concrete replacement
// for the retired nextRepositoryOverrideField's runtime feature-conditional
// skipping.
func overrideFieldsForFeature(feature string) []overrideField {
	switch feature {
	case gitleaksFeatureName:
		return []overrideField{overrideFieldEnabled, overrideFieldPathPrimary, overrideFieldClear}
	case signingFeatureName:
		return []overrideField{overrideFieldEnabled, overrideFieldPathPrimary, overrideFieldUnsignedSelfRead, overrideFieldClear}
	default: // trivy
		return []overrideField{overrideFieldEnabled, overrideFieldPathPrimary, overrideFieldPathSecondary, overrideFieldClear}
	}
}

// overrideEditor replaces repositoryOverrideModal (design.md Decision F).
// feature is unexported, set only by newOverrideEditor, and read only
// through Feature(). There is no setter and no key path that writes it: the
// spec's "no key changes which feature the modal edits" is a property of the
// type, not of a code review.
type overrideEditor struct {
	open             bool
	repository       string
	feature          string
	fields           []overrideField
	focus            int
	exists           bool
	enabled          bool
	pathPrimary      string
	pathSecondary    string
	unsignedSelfRead string
	loading          bool
	err              string
}

// newOverrideEditor constructs an open editor bound to feature+repository,
// with its own load already in flight (Init returns the load Cmd).
func newOverrideEditor(feature, repository string) overrideEditor {
	return overrideEditor{
		open:             true,
		repository:       repository,
		feature:          feature,
		fields:           overrideFieldsForFeature(feature),
		loading:          true,
		unsignedSelfRead: "off",
	}
}

// Feature returns the immutable feature this editor edits. No key path may
// change it (spec.md "The modal's Feature cannot be changed once open").
func (e overrideEditor) Feature() string { return e.feature }

// Active reports whether the editor is currently open.
func (e overrideEditor) Active() bool { return e.open }

func (e overrideEditor) currentField() overrideField {
	if len(e.fields) == 0 {
		return overrideFieldClear
	}
	idx := e.focus
	if idx < 0 || idx >= len(e.fields) {
		idx = 0
	}
	return e.fields[idx]
}

// update handles one key while the editor is open, mirroring the retired
// updateRepositoryOverrideModalKey's exact behavior minus the Feature
// cycle: Esc clears, Tab cycles the per-feature field set, Space toggles
// Enabled/cycles UnsignedSelfRead, runes append to the focused path field,
// Enter means save on every focus except Clear, where it means clear.
func (e overrideEditor) update(env screenEnv, msg tea.KeyMsg) (overrideEditor, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return overrideEditor{}, nil, true
	case isTabKey(msg):
		if len(e.fields) > 0 {
			e.focus = (e.focus + 1) % len(e.fields)
		}
		e.err = ""
		return e, nil, true
	case isRuneKey(msg, ' '):
		switch e.currentField() {
		case overrideFieldEnabled:
			e.enabled = !e.enabled
		case overrideFieldUnsignedSelfRead:
			e.unsignedSelfRead = nextUnsignedSelfReadValue(e.unsignedSelfRead)
		}
		e.err = ""
		return e, nil, true
	case isBackspaceKey(msg):
		e.deleteRune()
		e.err = ""
		return e, nil, true
	case isEnterKey(msg):
		if e.currentField() == overrideFieldClear {
			if !e.exists {
				e.err = "Already inheriting global settings."
				return e, nil, true
			}
			return e, clearRepositoryOverrideCmd(env, e.repository, e.feature), true
		}
		input := ports.RepositoryOverrideDetails{Enabled: e.enabled}
		switch e.feature {
		case gitleaksFeatureName:
			input.ConfigPath = e.pathPrimary
		case signingFeatureName:
			if strings.TrimSpace(e.pathPrimary) != "" {
				input.TrustedPublicKeys = []string{e.pathPrimary}
			}
			input.UnsignedSelfRead = normalizeUnsignedSelfRead(e.unsignedSelfRead)
		default:
			input.IgnoreFilePath = e.pathPrimary
			input.IgnorePolicyPath = e.pathSecondary
		}
		return e, saveRepositoryOverrideCmd(env, e.repository, e.feature, input), true
	}
	if msg.Type == tea.KeyRunes {
		e.appendRunes(string(msg.Runes))
		e.err = ""
		return e, nil, true
	}
	return e, nil, true
}

func (e *overrideEditor) deleteRune() {
	switch e.currentField() {
	case overrideFieldPathPrimary:
		e.pathPrimary = trimLastRune(e.pathPrimary)
	case overrideFieldPathSecondary:
		e.pathSecondary = trimLastRune(e.pathSecondary)
	}
}

func (e *overrideEditor) appendRunes(value string) {
	if value == "" {
		return
	}
	switch e.currentField() {
	case overrideFieldPathPrimary:
		e.pathPrimary += value
	case overrideFieldPathSecondary:
		e.pathSecondary += value
	}
}

// applyLoaded reflects a loadRepositoryOverrideCmd result onto the editor,
// discarding a stale response for an editor the operator has since closed
// or that was reopened for a different repository/feature.
func (e overrideEditor) applyLoaded(msg adminRepositoryOverrideLoadedMsg) overrideEditor {
	if !e.open || msg.repository != e.repository || msg.feature != e.feature {
		return e
	}
	e.loading = false
	if msg.err != nil {
		e.err = msg.err.Error()
		return e
	}
	e.applyOverride(msg.override, msg.exists)
	return e
}

// applySaved reflects a save/clear result back onto the editor (spec.md
// "both actions MUST round-trip through the admin API and be reflected back
// in the modal"). The editor stays open after a successful save/clear so
// the operator can see the reflected state immediately.
func (e overrideEditor) applySaved(msg adminRepositoryOverrideSavedMsg) overrideEditor {
	if !e.open || msg.repository != e.repository || msg.feature != e.feature {
		return e
	}
	if msg.err != nil {
		e.err = msg.err.Error()
		return e
	}
	e.err = ""
	e.applyOverride(msg.override, msg.exists)
	return e
}

func (e *overrideEditor) applyOverride(override ports.RepositoryOverrideDetails, exists bool) {
	e.exists = exists
	if !exists {
		e.enabled = false
		e.pathPrimary = ""
		e.pathSecondary = ""
		e.unsignedSelfRead = "off"
		return
	}
	e.enabled = override.Enabled
	switch e.feature {
	case gitleaksFeatureName:
		e.pathPrimary = override.ConfigPath
	case signingFeatureName:
		e.pathPrimary = firstRepositoryOverrideTrustedKey(override.TrustedPublicKeys)
		e.unsignedSelfRead = normalizeUnsignedSelfRead(override.UnsignedSelfRead)
	default:
		e.pathPrimary = override.IgnoreFilePath
		e.pathSecondary = override.IgnorePolicyPath
	}
}

// firstRepositoryOverrideTrustedKey returns the first stored trusted key, or
// "" when none are stored -- overrideEditor's PathPrimary field edits at
// most one key per repository override (moved verbatim from model.go).
func firstRepositoryOverrideTrustedKey(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

// loadRepositoryOverrideCmd/saveRepositoryOverrideCmd/clearRepositoryOverrideCmd
// are the screenEnv-scoped equivalents of Model's own identically-named
// methods (model.go), needed because a sub-model has no Model to call a
// method on -- built from env.asModel() so the exact same AdminClient calls
// and message shapes (adminRepositoryOverrideLoadedMsg/SavedMsg) are reused
// verbatim (mirrors screen_gitleaks_config.go's configureFeatureCmd wrapper).
func loadRepositoryOverrideCmd(env screenEnv, repository string, feature string) tea.Cmd {
	return env.asModel().loadRepositoryOverrideCmd(repository, feature)
}

func saveRepositoryOverrideCmd(env screenEnv, repository string, feature string, input ports.RepositoryOverrideDetails) tea.Cmd {
	return env.asModel().saveRepositoryOverrideCmd(repository, feature, input)
}

func clearRepositoryOverrideCmd(env screenEnv, repository string, feature string) tea.Cmd {
	return env.asModel().clearRepositoryOverrideCmd(repository, feature)
}

// overrideEditorKeys is the editor's key.Map (design.md Decision D),
// replacing the retired hand-written "Enter: save/clear | Tab: next field |
// Space: toggle/cycle | Esc: cancel" footer -- "cycle" drops out along with
// the deleted Feature cycle.
var overrideEditorKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "save/clear")),
	key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "next field")),
	key.NewBinding(key.WithKeys(" "), key.WithHelp("Space", "toggle")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "cancel")),
}}

// overrideEditor also satisfies adminScreen directly (design.md Decision A),
// so Trivy's own override entry point (updateAdminFeaturesKey's 'o' branch)
// can mount it as an overlay on screenAdminFeatures exactly the way
// gitleaksConfigScreen mounts at slotGitleaksConfig -- one uniform mechanism
// for all three features (D2: "the override action is selection-driven,
// uniformly").

func (e overrideEditor) ID() screen { return screenAdminFeatures }

func (e overrideEditor) Keys() screenKeys { return overrideEditorKeys }

func (e overrideEditor) Init(env screenEnv) tea.Cmd {
	return loadRepositoryOverrideCmd(env, e.repository, e.feature)
}

// Update returns nil (the "closed" signal, mirroring gitleaksConfigScreen)
// once Esc has cleared the editor.
func (e overrideEditor) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		next, cmd, consumed := e.update(env, typed)
		if !next.open {
			return nil, cmd, consumed
		}
		return next, cmd, consumed
	case adminRepositoryOverrideLoadedMsg:
		return e.applyLoaded(typed), nil, false
	case adminRepositoryOverrideSavedMsg:
		return e.applySaved(typed), nil, false
	}
	return e, nil, false
}

func (e overrideEditor) View(theme adminTheme, env screenEnv) screenFrame {
	return screenFrame{Overlay: renderOverrideEditor(theme, e)}
}

// renderOverrideEditor reproduces renderRepositoryOverrideModal's layout
// minus the retired Feature row, with the keymap-generated footer.
func renderOverrideEditor(theme adminTheme, e overrideEditor) string {
	lines := []string{
		theme.subheading.Render("Repository Override — " + e.repository),
		theme.muted.Render(overrideStatusLine(e)),
		renderToggleField(theme, "Enabled", e.enabled, e.currentField() == overrideFieldEnabled),
	}
	switch e.feature {
	case gitleaksFeatureName:
		lines = append(lines, renderTextField(theme, "Config Path", e.pathPrimary, e.currentField() == overrideFieldPathPrimary))
	case signingFeatureName:
		lines = append(lines, renderTextField(theme, "Trusted Key (PEM)", e.pathPrimary, e.currentField() == overrideFieldPathPrimary))
		lines = append(lines, renderTextField(theme, "Unsigned Self-Read", normalizeUnsignedSelfRead(e.unsignedSelfRead), e.currentField() == overrideFieldUnsignedSelfRead))
	default:
		lines = append(lines, renderTextField(theme, "Ignore File Path", e.pathPrimary, e.currentField() == overrideFieldPathPrimary))
		lines = append(lines, renderTextField(theme, "Ignore Policy Path", e.pathSecondary, e.currentField() == overrideFieldPathSecondary))
	}
	lines = append(lines, renderOverrideClearRow(theme, e))
	if strings.TrimSpace(e.err) != "" {
		lines = append(lines, "", theme.error.Render(e.err))
	}
	lines = append(lines, "", theme.muted.Render(shortHelpView(theme, overrideEditorKeys)))
	return theme.section.Render(strings.Join(lines, "\n"))
}

func overrideStatusLine(e overrideEditor) string {
	if e.loading {
		return "Loading…"
	}
	if e.exists {
		return "override active"
	}
	return "inheriting global settings"
}

func renderOverrideClearRow(theme adminTheme, e overrideEditor) string {
	label := "Clear override -> use global settings"
	if !e.exists {
		label = "Already inheriting global settings"
	}
	style := theme.muted
	if e.currentField() == overrideFieldClear {
		style = theme.inputFocus
	}
	return style.Render(label)
}
