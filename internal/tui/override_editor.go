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
	overrideFieldIdentities                     // signing only: trusted identity list
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
		return []overrideField{overrideFieldEnabled, overrideFieldPathPrimary, overrideFieldIdentities, overrideFieldUnsignedSelfRead, overrideFieldClear}
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

	// keys is signing's own field at overrideFieldPathPrimary's position --
	// pathPrimary's replacement for the signing feature ONLY (signing-key-
	// management change). gitleaks/trivy still use pathPrimary/
	// pathSecondary exactly as before; this field is simply unused for
	// those two features.
	keys trustedKeyList
	// identities is Keys' sibling for keyless (Fulcio/OIDC) trust anchors
	// (signing-keyless-verification), living at overrideFieldIdentities'
	// own position -- signing only, unused for gitleaks/trivy exactly like
	// keys.
	identities trustedIdentityList
	// pendingGlobalKeys/pendingGlobalKeysLoaded/prefillApplied back the
	// "seed a never-configured signing override with the current global
	// keys" flow. applyLoaded chains a follow-up loadSigningPolicyCmd once
	// the override itself resolves to "no row exists" (a chained Cmd, not
	// tea.Batch -- this codebase's own established multi-load-on-open
	// convention, see applyLoaded's doc comment); the prefill
	// (maybeApplyGlobalPrefill) fires once that follow-up resolves, and
	// only once ever per open editor (prefillApplied).
	pendingGlobalKeys       []string
	pendingGlobalKeysLoaded bool
	prefillApplied          bool
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
		keys:             newTrustedKeyList(repository, nil),
		identities:       newTrustedIdentityList(nil),
	}
}

// Feature returns the immutable feature this editor edits. No key path may
// change it (spec.md "The modal's Feature cannot be changed once open").
func (e overrideEditor) Feature() string { return e.feature }

// Active reports whether the editor is currently open.
func (e overrideEditor) Active() bool { return e.open }

// indexOfOverrideField returns the index of target within fields, or -1 if
// fields does not contain it -- used to snap e.focus to the key list's own
// position once it has actually consumed a key (see update's doc comment).
func indexOfOverrideField(fields []overrideField, target overrideField) int {
	for i, field := range fields {
		if field == target {
			return i
		}
	}
	return -1
}

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
	// The trusted-key list claims Up/Down/'n'/'x' and every key while it is
	// mid-add or mid-delete-confirm; only what it does NOT consume (Tab/
	// Esc/Space/Enter in its own idle navigation state) falls through to
	// this editor's own bindings below -- mirrors
	// signingConfigScreen.updateConfigKey's identical interception.
	//
	// This routing is UNCONDITIONAL for the signing feature (not gated on
	// e.currentField() == overrideFieldPathPrimary): a freshly-opened editor
	// starts focus on overrideFieldEnabled, not the key list's position, so
	// gating on Tab-focus made 'n'/'x'/Up/Down silent no-ops until the
	// operator tabbed to the exact right field first -- the "can't add a
	// key" bug. appendRunes/deleteRune already unconditionally no-op for
	// every rune when feature == signing (see their own doc comments), so
	// there is no other field in the signing form that could ever claim
	// these keys as literal input; routing them here first is safe.
	//
	// Judgment Day fix-round: unconditional routing alone left the visible
	// Tab-focus indicator free to disagree with what actually responded to
	// the keystroke -- pressing 'x' while "Clear override" was highlighted
	// silently acted on the key list instead, with no on-screen sign focus
	// had effectively moved. The moment e.keys.update actually CONSUMES a
	// key, snap e.focus to the key list's own field position too, so the
	// rendered highlight (renderOverrideEditor's
	// e.currentField() == overrideFieldPathPrimary check) is always honest
	// about what is currently receiving input.
	//
	// Identities (signing-keyless-verification) is NOT given the same
	// unconditional priority as Keys: both widgets would otherwise claim
	// the identical Up/Down/'n'/'x' keys ambiguously with no way to tell
	// which list an unfocused keystroke was meant for. Identities only
	// claims them once the operator has actually tabbed onto
	// overrideFieldIdentities -- Keys keeps its established
	// reachable-from-anywhere shortcut (preserving
	// TestOverrideEditorSigningKeyListReachableWithoutTabbingToIt's pinned
	// behavior) since it has no competing sibling field ambiguity.
	if e.feature == signingFeatureName {
		if e.currentField() == overrideFieldIdentities {
			next, consumed := e.identities.update(msg)
			if consumed {
				e.identities = next
				return e, nil, true
			}
		} else {
			next, cmd, consumed := e.keys.update(env, msg)
			if consumed {
				e.keys = next
				if idx := indexOfOverrideField(e.fields, overrideFieldPathPrimary); idx >= 0 {
					e.focus = idx
				}
				return e, cmd, true
			}
		}
	}
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
			input.TrustedPublicKeys = e.keys.Keys()
			input.TrustedIdentities = e.identities.Identities()
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

// deleteRune/appendRunes never touch anything for the signing feature: its
// PathPrimary-position field is e.keys (trustedKeyList), which claims
// Backspace/rune input itself, within its own add flow, before update ever
// reaches these (see update's interception above) -- pathPrimary stays
// unused and untouched for signing.
func (e *overrideEditor) deleteRune() {
	if e.feature == signingFeatureName {
		return
	}
	switch e.currentField() {
	case overrideFieldPathPrimary:
		e.pathPrimary = trimLastRune(e.pathPrimary)
	case overrideFieldPathSecondary:
		e.pathSecondary = trimLastRune(e.pathSecondary)
	}
}

func (e *overrideEditor) appendRunes(value string) {
	if value == "" || e.feature == signingFeatureName {
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
// or that was reopened for a different repository/feature. For a
// never-configured signing override (feature == signing, exists == false),
// it chains a follow-up loadSigningPolicyCmd to fetch the current global
// keys to prefill with -- a chained follow-up Cmd, not tea.Batch, matching
// this codebase's own established multi-load-on-open convention (see
// trivyReposScreen's identical chain from
// adminRepositoryScanSummariesLoadedMsg to loadTrivyRepositoryOverridesListCmd,
// screen_trivy_repos.go).
func (e overrideEditor) applyLoaded(env screenEnv, msg adminRepositoryOverrideLoadedMsg) (overrideEditor, tea.Cmd) {
	if !e.open || msg.repository != e.repository || msg.feature != e.feature {
		return e, nil
	}
	e.loading = false
	if msg.err != nil {
		e.err = msg.err.Error()
		return e, nil
	}
	e.applyOverride(msg.override, msg.exists)
	if e.feature != signingFeatureName {
		return e, nil
	}
	// The global policy load can resolve before this override load does
	// (both are independent async Cmds; a signing policy load may already
	// be in flight from a screen the operator recently visited) -- so
	// applyGlobalPolicyLoaded's own maybeApplyGlobalPrefill call is not
	// enough on its own: its !e.loading guard blocks it while this load is
	// still pending. Re-checking here makes the prefill genuinely
	// order-independent, matching maybeApplyGlobalPrefill's own doc comment,
	// instead of relying on a second, redundant loadSigningPolicyCmd to
	// self-heal the case where the global load won the race.
	e = e.maybeApplyGlobalPrefill()
	if !e.exists && !e.prefillApplied {
		return e, loadSigningPolicyCmd(env)
	}
	return e, nil
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
		if e.feature == signingFeatureName {
			e.keys = newTrustedKeyList(e.repository, nil)
			e.identities = newTrustedIdentityList(nil)
		}
		return
	}
	e.enabled = override.Enabled
	switch e.feature {
	case gitleaksFeatureName:
		e.pathPrimary = override.ConfigPath
	case signingFeatureName:
		e.keys = newTrustedKeyList(e.repository, override.TrustedPublicKeys)
		e.identities = newTrustedIdentityList(override.TrustedIdentities)
		e.unsignedSelfRead = normalizeUnsignedSelfRead(override.UnsignedSelfRead)
	default:
		e.pathPrimary = override.IgnoreFilePath
		e.pathSecondary = override.IgnorePolicyPath
	}
}

// maybeApplyGlobalPrefill seeds a never-configured signing override's key
// list with the current global signing policy's trusted keys (design
// requirement: only a genuinely new, unconfigured override gets this --
// applyOverride above already set e.keys from the STORED override's own
// keys when exists is true, and that must never be overwritten). It is a
// no-op until BOTH the override load and the global policy load have
// resolved (order-independent) and fires at most once per open editor.
func (e overrideEditor) maybeApplyGlobalPrefill() overrideEditor {
	if e.feature != signingFeatureName || e.prefillApplied || e.loading || !e.pendingGlobalKeysLoaded {
		return e
	}
	e.prefillApplied = true
	if !e.exists {
		e.keys = e.keys.SetKeys(e.pendingGlobalKeys)
	}
	return e
}

// applyGlobalPolicyLoaded reflects loadSigningPolicyCmd's result (fired
// alongside loadRepositoryOverrideCmd for the signing feature, see Init)
// into the prefill bookkeeping above. A stale response for an editor the
// operator has since closed, or a non-signing editor (Trivy/gitleaks never
// fire this Cmd, but overrideEditor.Update stays defensive), is discarded.
func (e overrideEditor) applyGlobalPolicyLoaded(msg adminSigningPolicyLoadedMsg) overrideEditor {
	if !e.open || e.feature != signingFeatureName || msg.err != nil {
		return e
	}
	e.pendingGlobalKeys = msg.settings.TrustedPublicKeys
	e.pendingGlobalKeysLoaded = true
	return e.maybeApplyGlobalPrefill()
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
		next, cmd := e.applyLoaded(env, typed)
		return next, cmd, false
	case adminRepositoryOverrideSavedMsg:
		return e.applySaved(typed), nil, false
	case adminSigningPolicyLoadedMsg:
		return e.applyGlobalPolicyLoaded(typed), nil, false
	case adminSigningKeyUsageLoadedMsg:
		e.keys = e.keys.applyUsageLoaded(typed)
		return e, nil, false
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
		lines = append(lines, renderTrustedKeyList(theme, e.keys, e.currentField() == overrideFieldPathPrimary)...)
		lines = append(lines, renderTrustedIdentityList(theme, e.identities, e.currentField() == overrideFieldIdentities)...)
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
