package tui

import (
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// overrideField identifies which of overrideEditor's fields has focus. There is
// deliberately NO Feature entry: the feature is fixed when the editor is
// created and no key changes it.
type overrideField int

const (
	overrideFieldEnabled          overrideField = iota
	overrideFieldPathPrimary                    // trivy: ignore file | gitleaks: config | signing: trusted keys
	overrideFieldPathSecondary                  // trivy only
	overrideFieldIdentities                     // signing only: trusted identities
	overrideFieldUnsignedSelfRead               // signing only
	overrideFieldClear                          // action row, not an input
)

// overrideFieldsForFeature is the ordered field set of one feature's override,
// built once when the editor opens.
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

// overrideEditor edits ONE repository's override of ONE feature, as a modal on
// the repository list that opened it. Its repository and feature are set when
// it is created and read only through the fields below: no key and no result
// changes them, so a save can only ever go to the repository it was opened for.
//
// Nothing can be saved until the stored override has been read: saving an
// editor that never loaded would replace the real override with an empty one.
type overrideEditor struct {
	open bool
	// gen is the generation of the screen that opened the editor and id this
	// opening of it: every result it asks for carries both.
	gen, id    uint64
	repository string
	feature    string
	fields     []overrideField
	focus      int

	// loaded is true once the stored override was read; loading while that read
	// is in flight.
	loaded  bool
	loading bool
	exists  bool
	// busy is true from the Enter that saved or cleared until the result
	// arrives: Enter is ignored meanwhile.
	busy bool

	enabled       bool
	pathPrimary   string
	pathSecondary string
	// unsignedSelfRead is the mode shown; unsignedSelfReadStored the wire value
	// the stored override carried ("" and "off" both mean off), sent back
	// unchanged while the mode is unchanged.
	unsignedSelfRead       string
	unsignedSelfReadStored string
	keys                   trustedKeyList
	identities             trustedIdentityList

	// pendingGlobalKeys, pendingGlobalKeysLoaded and prefillApplied seed a
	// signing override that does not exist yet with the current global keys:
	// once, and only after both reads resolved, in either order.
	pendingGlobalKeys       []string
	pendingGlobalKeysLoaded bool
	prefillApplied          bool

	// confirm asks before the override is cleared.
	confirm confirmPrompt
	err     string
	note    string
}

// newOverrideEditor opens an editor for feature+repository on the screen with
// generation gen; Init returns its read.
func newOverrideEditor(gen uint64, feature, repository string) overrideEditor {
	return overrideEditor{
		open:             true,
		gen:              gen,
		id:               overrideEditorIDs.Add(1),
		repository:       repository,
		feature:          feature,
		fields:           overrideFieldsForFeature(feature),
		loading:          true,
		unsignedSelfRead: "off",
		keys:             newTrustedKeyList(repository, nil),
		identities:       newTrustedIdentityList(nil),
	}
}

// Feature returns the feature this editor edits.
func (e overrideEditor) Feature() string { return e.feature }

// Active reports whether the editor is open. While it is, the screen under it
// gets every key.
func (e overrideEditor) Active() bool { return e.open }

func (e overrideEditor) Init(env screenEnv) tea.Cmd {
	return loadOverrideCmd(env, e.gen, e.id, e.repository, e.feature)
}

// keysHint is the footer of the screen under the editor.
func (e overrideEditor) keysHint() screenKeys { return securityModalKeys }

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

func indexOfOverrideField(fields []overrideField, target overrideField) int {
	for i, field := range fields {
		if field == target {
			return i
		}
	}
	return -1
}

// ready reports whether the stored override has been read and nothing is in
// flight: the only state in which the form takes input.
func (e overrideEditor) ready() bool { return e.loaded && !e.busy }

// update handles one key while the editor is open.
func (e overrideEditor) update(env screenEnv, msg keyMsg) (overrideEditor, tea.Cmd, bool) {
	if e.confirm.Active() {
		return e.updateConfirm(env, msg)
	}
	if isEscKey(msg) && !e.keysCaptureEsc() {
		return overrideEditor{}, nil, true
	}
	if !e.ready() {
		// Reading, saving or clearing: nothing but Esc does anything.
		return e, nil, true
	}
	if isPasteKey(msg) {
		return e.updatePaste(env, msg)
	}
	if e.feature == signingFeatureName {
		if next, cmd, consumed := e.updateLists(env, msg); consumed {
			return next, cmd, true
		}
	}
	switch {
	case isEscKey(msg):
		return overrideEditor{}, nil, true
	case isTabKey(msg), isArrowDownKey(msg) && e.feature != signingFeatureName:
		e.focus = (e.focus + 1) % len(e.fields)
		e.err, e.note = "", ""
		return e, nil, true
	case isShiftTabKey(msg), isArrowUpKey(msg) && e.feature != signingFeatureName:
		e.focus = (e.focus + len(e.fields) - 1) % len(e.fields)
		e.err, e.note = "", ""
		return e, nil, true
	case isBackspaceKey(msg):
		e.deleteRune()
		e.err, e.note = "", ""
		return e, nil, true
	case isEnterKey(msg):
		return e.enter(env)
	case isSpaceKey(msg):
		switch e.currentField() {
		case overrideFieldEnabled:
			e.enabled = !e.enabled
		case overrideFieldUnsignedSelfRead:
			e.unsignedSelfRead = nextUnsignedSelfReadValue(e.unsignedSelfRead)
		case overrideFieldPathPrimary, overrideFieldPathSecondary:
			e.appendText(" ")
		}
		e.err, e.note = "", ""
		return e, nil, true
	}
	if isTextKey(msg) {
		e.appendText(printableText(keyText(msg)))
		e.err, e.note = "", ""
	}
	return e, nil, true
}

// keysCaptureEsc is true while a list inside the editor has its own use of Esc
// (cancel an add, stop waiting for a usage count, keep a key).
func (e overrideEditor) keysCaptureEsc() bool {
	if e.feature != signingFeatureName || !e.ready() {
		return false
	}
	return e.keys.adding || e.keys.usageLoading || e.keys.confirm.Active() || e.identities.adding
}

// updateLists gives the signing key and identity lists the keys they claim.
//
// The key list claims them whichever field has focus (nothing else in the
// signing form takes typed text), and the focus then snaps onto it so the
// highlight shows what is receiving input. The identity list claims the same
// keys only once Tab has put focus on it: two lists cannot both own them
// unannounced.
func (e overrideEditor) updateLists(env screenEnv, msg keyMsg) (overrideEditor, tea.Cmd, bool) {
	if e.currentField() == overrideFieldIdentities {
		next, consumed := e.identities.update(msg)
		if consumed {
			e.identities = next
			e.err, e.note = "", ""
			return e, nil, true
		}
		return e, nil, false
	}
	next, cmd, consumed := e.keys.update(env, msg)
	if !consumed {
		return e, nil, false
	}
	if next.usageLoading && !e.keys.usageLoading {
		// The usage lookup is this editor's own, stamped with its generation
		// and id, so a late answer cannot reach another editor or screen.
		cmd = overrideKeyUsageCmd(env, e.gen, e.id, e.repository, next.keys[next.pendingDeleteIndex])
	}
	e.keys = next
	if idx := indexOfOverrideField(e.fields, overrideFieldPathPrimary); idx >= 0 {
		e.focus = idx
	}
	e.err, e.note = "", ""
	return e, cmd, true
}

// updatePaste takes pasted text into the field that is typing and nowhere
// else: pasted text is never a command, so its first character must not toggle
// a switch, add or delete a key, or ask to clear the override.
func (e overrideEditor) updatePaste(env screenEnv, msg keyMsg) (overrideEditor, tea.Cmd, bool) {
	if e.feature == signingFeatureName {
		switch {
		case e.keys.adding:
			next, cmd, _ := e.keys.update(env, msg)
			e.keys = next
			return e, cmd, true
		case e.identities.adding && e.currentField() == overrideFieldIdentities:
			next, _ := e.identities.update(msg)
			e.identities = next
		}
		return e, nil, true
	}
	switch e.currentField() {
	case overrideFieldPathPrimary, overrideFieldPathSecondary:
		e.appendText(printableText(keyText(msg)))
		e.err, e.note = "", ""
	}
	return e, nil, true
}

func (e overrideEditor) updateConfirm(env screenEnv, msg keyMsg) (overrideEditor, tea.Cmd, bool) {
	next, cmd, _ := e.confirm.update(env, msg)
	e.confirm = next
	if cmd != nil {
		e.busy = true
	}
	return e, cmd, true
}

// enter saves, or on the Clear row asks to clear.
func (e overrideEditor) enter(env screenEnv) (overrideEditor, tea.Cmd, bool) {
	e.err, e.note = "", ""
	if e.currentField() == overrideFieldClear {
		if !e.exists {
			e.err = "Already inheriting the global settings."
			return e, nil, true
		}
		gen, id, repository, feature := e.gen, e.id, e.repository, e.feature
		e.confirm = newConfirmPrompt(
			"Clear override",
			"Remove this repository's override of the "+featureDisplayName(feature)+" settings. It goes back to the global settings.",
			"remove", "Clearing the override...",
			func(env screenEnv) tea.Cmd { return clearOverrideCmd(env, gen, id, repository, feature) },
		).withTarget(repository, featureDisplayName(feature)+" override").
			withConsequence("Only this repository changes. Other repositories keep their overrides.").
			withButtonLabel("Clear override")
		return e, nil, true
	}
	e.busy = true
	return e, saveOverrideCmd(env, e.gen, e.id, e.repository, e.feature, e.input()), true
}

// input is the whole override the form shows. A PUT replaces the stored
// override in full, so every field of the feature is sent, edited or not.
func (e overrideEditor) input() ports.RepositoryOverrideDetails {
	input := ports.RepositoryOverrideDetails{Enabled: e.enabled}
	switch e.feature {
	case gitleaksFeatureName:
		input.ConfigPath = e.pathPrimary
	case signingFeatureName:
		input.TrustedPublicKeys = e.keys.Keys()
		input.TrustedIdentities = e.identities.Identities()
		input.UnsignedSelfRead = e.unsignedSelfReadWire()
	default:
		input.IgnoreFilePath = e.pathPrimary
		input.IgnorePolicyPath = e.pathSecondary
	}
	return input
}

// unsignedSelfReadWire is the mode as the API stores it: the stored value itself
// while the mode was not changed, so "" is not rewritten as "off".
func (e overrideEditor) unsignedSelfReadWire() string {
	mode := normalizeUnsignedSelfRead(e.unsignedSelfRead)
	if normalizeUnsignedSelfRead(e.unsignedSelfReadStored) == mode {
		return e.unsignedSelfReadStored
	}
	return mode
}

// appendText and deleteRune edit the focused path field. The signing form has
// none: its trusted lists take their own typing.
func (e *overrideEditor) appendText(value string) {
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

// ---- results ----

// mine reports whether a result was asked for by this very opening of the
// editor, for this repository and feature.
func (e overrideEditor) mine(editor uint64, repository, feature string) bool {
	return e.open && editor == e.id && repository == e.repository && feature == e.feature
}

// applyLoaded takes the stored override the editor asked for. For a signing
// override that does not exist yet it then reads the global policy, to seed the
// keys from.
func (e overrideEditor) applyLoaded(env screenEnv, msg overrideLoadedMsg) (overrideEditor, tea.Cmd) {
	if !e.mine(msg.editor, msg.repository, msg.feature) {
		return e, nil
	}
	e.loading = false
	if msg.err != nil {
		e.err = msg.err.Error()
		return e, nil
	}
	e.loaded = true
	e.applyOverride(msg.override, msg.exists)
	if e.feature != signingFeatureName {
		return e, nil
	}
	e = e.maybeApplyGlobalPrefill()
	if !e.exists && !e.prefillApplied {
		return e, loadOverridePolicyCmd(env, e.gen, e.id)
	}
	return e, nil
}

// applySaved takes the result of a save or a clear. The editor stays open so
// the operator sees the reflected state, and keeps everything typed after a
// failed one.
func (e overrideEditor) applySaved(msg overrideSavedMsg) overrideEditor {
	if !e.mine(msg.editor, msg.repository, msg.feature) {
		return e
	}
	e.busy = false
	e.confirm = confirmPrompt{}
	if msg.err != nil {
		e.err = msg.err.Error()
		return e
	}
	e.err = ""
	e.applyOverride(msg.override, msg.exists)
	if msg.exists {
		e.note = "Override saved."
	} else {
		e.note = "Override cleared."
	}
	return e
}

func (e *overrideEditor) applyOverride(override ports.RepositoryOverrideDetails, exists bool) {
	e.exists = exists
	if !exists {
		e.enabled = false
		e.pathPrimary = ""
		e.pathSecondary = ""
		e.unsignedSelfRead = "off"
		e.unsignedSelfReadStored = ""
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
		e.unsignedSelfReadStored = override.UnsignedSelfRead
	default:
		e.pathPrimary = override.IgnoreFilePath
		e.pathSecondary = override.IgnorePolicyPath
	}
}

// applyPolicy takes the global signing policy read for the prefill.
func (e overrideEditor) applyPolicy(msg overridePolicyMsg) overrideEditor {
	if !e.open || msg.editor != e.id || e.feature != signingFeatureName || msg.err != nil {
		return e
	}
	e.pendingGlobalKeys = msg.settings.TrustedPublicKeys
	e.pendingGlobalKeysLoaded = true
	return e.maybeApplyGlobalPrefill()
}

// maybeApplyGlobalPrefill seeds a signing override that does not exist yet with
// the global trusted keys. It waits for both reads, runs once per opening, and
// never touches the keys of an override that exists.
func (e overrideEditor) maybeApplyGlobalPrefill() overrideEditor {
	if e.feature != signingFeatureName || e.prefillApplied || !e.loaded || !e.pendingGlobalKeysLoaded {
		return e
	}
	e.prefillApplied = true
	if !e.exists {
		e.keys = e.keys.SetKeys(e.pendingGlobalKeys)
	}
	return e
}

// applyKeyUsage takes the answer of a key's usage count.
func (e overrideEditor) applyKeyUsage(msg overrideKeyUsageMsg) overrideEditor {
	if !e.open || msg.editor != e.id || e.feature != signingFeatureName {
		return e
	}
	e.keys = e.keys.applyUsageLoaded(adminSigningKeyUsageLoadedMsg{key: msg.key, count: msg.count, capped: msg.capped, err: msg.err})
	return e
}

func overrideStatusLine(e overrideEditor) string {
	switch {
	case e.loading:
		return "Reading the override…"
	case !e.loaded:
		return "The override could not be read."
	case e.exists:
		return "override active"
	}
	return "inheriting the global settings"
}
