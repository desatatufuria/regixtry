package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"regixtry/internal/domain/signing"
)

// trustedKeyList is a navigable, editable list of PEM-encoded trusted
// public keys, shared by the global Signing config screen
// (signingPolicyModal) and any per-repository signing override
// (overrideEditor) -- both need identical add/select/delete behavior, so
// this is the one place that behavior lives.
type trustedKeyList struct {
	keys     []string
	selected int
	// adding tracks an in-progress paste for a new key before Enter commits
	// it; false the rest of the time (list-navigation mode).
	adding bool
	input  string
	// err is a validation error from a rejected add (client-side
	// pre-validation only -- the authoritative validation stays
	// server-side on save, see commitAdd's doc comment).
	err string

	// scopeRepository is the scope the delete-key usage-count call is made
	// against: "" for the global signing policy's key list (every
	// repository), or one repository name for a per-repository override's
	// key list. Fixed at construction.
	scopeRepository string

	// confirm/pendingDeleteIndex back the delete-key confirm flow. This
	// reuses confirmPrompt (internal/tui/confirm.go) purely for its state
	// shape and view() rendering -- THE single confirm primitive this
	// codebase already has. Unlike confirmPrompt's usual callers (which
	// dispatch a real network Cmd from onConfirm and wait for the async
	// result message), removing one entry from an in-memory key list is a
	// synchronous, local mutation with nothing to wait on, so
	// trustedKeyList.update intercepts Enter/Esc itself while
	// confirm.Active() rather than routing through confirmPrompt.update's
	// onConfirm dispatch (onConfirm is set to a harmless no-op so
	// Active()'s nil check still reports true; it is never actually
	// invoked). The confirm is never a hard block: deleting is always
	// possible, the usage count is advisory only (spec requirement).
	confirm            confirmPrompt
	pendingDeleteIndex int
	// usageLoading is true from the moment 'x' fires the key-usage-count
	// request until that response arrives and the confirm prompt actually
	// opens.
	usageLoading bool
}

// newTrustedKeyList constructs a list seeded with keys (a defensive copy),
// scoped to scopeRepository for its delete-key usage-count lookups.
func newTrustedKeyList(scopeRepository string, keys []string) trustedKeyList {
	return trustedKeyList{scopeRepository: scopeRepository, keys: append([]string(nil), keys...)}
}

// SetKeys replaces the list's keys wholesale (used by the prefill flow:
// seeding a never-configured override with the current global policy's
// keys) without disturbing scopeRepository or any in-progress add/delete.
func (l trustedKeyList) SetKeys(keys []string) trustedKeyList {
	l.keys = append([]string(nil), keys...)
	l.selected = boundedIndex(l.selected, len(l.keys))
	return l
}

// Keys returns the list's current keys -- the value a caller persists on
// save.
func (l trustedKeyList) Keys() []string { return l.keys }

// update handles one key while the list has focus. consumed=false lets the
// caller (overrideEditor/signingConfigScreen) fall through to its own
// Tab/Esc/Enter/Space handling for any key this list does not claim --
// Up/Down, 'n' (add, matching this codebase's existing "new" precedent,
// e.g. updateAdminUsersKey), 'x' (delete, matching its existing "remove"
// precedent, e.g. updateAdminGrantsKey), and everything typed while
// adding or confirming a delete.
func (l trustedKeyList) update(env screenEnv, msg tea.KeyMsg) (trustedKeyList, tea.Cmd, bool) {
	if l.usageLoading {
		return l, nil, true // swallow input while the usage-count call is in flight
	}

	if l.confirm.Active() {
		switch {
		case isEscKey(msg):
			l.confirm = confirmPrompt{}
			return l, nil, true
		case isEnterKey(msg):
			l.removeAt(l.pendingDeleteIndex)
			l.confirm = confirmPrompt{}
			return l, nil, true
		}
		return l, nil, true // swallow every other key while confirming, mirroring confirmPrompt.update
	}

	if l.adding {
		switch {
		case isEscKey(msg):
			l.adding = false
			l.input = ""
			l.err = ""
			return l, nil, true
		case isEnterKey(msg):
			// A raw terminal without bracketed-paste support delivers each
			// embedded newline byte of a pasted multi-line PEM as its own
			// tea.KeyEnter event, not accumulated literal '\n' runes. Only
			// treat Enter as a genuine submit once the accumulated input
			// already looks like a complete PEM (both markers present);
			// otherwise this is a mid-paste newline, so append it to input
			// and keep accumulating. commitAdd's own validation
			// (signing.NormalizePublicKeyPEM) still runs unconditionally
			// once Enter IS treated as a submit -- this only changes WHEN
			// that happens, never what counts as valid.
			if !looksLikeCompletePEM(l.input) {
				l.input += "\n"
				l.err = ""
				return l, nil, true
			}
			l.commitAdd()
			return l, nil, true
		case isBackspaceKey(msg):
			l.input = trimLastRune(l.input)
			l.err = ""
			return l, nil, true
		}
		if msg.Type == tea.KeyRunes {
			l.input += string(msg.Runes)
			l.err = ""
			return l, nil, true
		}
		return l, nil, true
	}

	switch {
	case isMoveUpKey(msg):
		l.selected = boundedIndex(l.selected-1, len(l.keys))
		return l, nil, true
	case isMoveDownKey(msg):
		l.selected = boundedIndex(l.selected+1, len(l.keys))
		return l, nil, true
	case isRuneKey(msg, 'n'):
		l.adding = true
		l.input = ""
		l.err = ""
		return l, nil, true
	case isRuneKey(msg, 'x'):
		if len(l.keys) == 0 {
			return l, nil, true
		}
		index := boundedIndex(l.selected, len(l.keys))
		l.pendingDeleteIndex = index
		l.usageLoading = true
		return l, signingKeyUsageCmd(env, l.scopeRepository, l.keys[index]), true
	}
	return l, nil, false
}

// commitAdd validates input via signing.NormalizePublicKeyPEM -- the same
// normalization the backend applies on save -- before appending it. This is
// deliberately only a client-side PRE-validation convenience (catch a typo
// before a round trip); it never replaces the server's own authoritative
// validation at save time (normalizeSigningOverride/
// decodeSigningPolicySettings), which still runs unconditionally.
func (l *trustedKeyList) commitAdd() {
	normalized, err := signing.NormalizePublicKeyPEM(l.input)
	if err != nil {
		l.err = err.Error()
		return
	}
	l.keys = append(l.keys, normalized)
	l.selected = len(l.keys) - 1
	l.adding = false
	l.input = ""
	l.err = ""
}

// looksLikeCompletePEM reports whether input already contains both a PEM
// BEGIN and END marker -- a cheap, deliberately loose check used only to
// distinguish an Enter that ends an in-progress multi-line paste (see the
// l.adding branch of update above) from a genuine submit keystroke. The
// signing package's own decodePEMBlock (internal/domain/signing/keys.go)
// does the authoritative marker parsing at commit time via
// signing.NormalizePublicKeyPEM; pemBlockType there is unexported, so this
// stays a local, best-effort pre-check rather than a second source of
// truth for what a valid PEM block looks like.
func looksLikeCompletePEM(input string) bool {
	return strings.Contains(input, "-----BEGIN") && strings.Contains(input, "-----END")
}

// removeAt deletes the key at index (already bounds-checked by the 'x'
// handler above at request time).
func (l *trustedKeyList) removeAt(index int) {
	if index < 0 || index >= len(l.keys) {
		return
	}
	l.keys = append(l.keys[:index], l.keys[index+1:]...)
	l.selected = boundedIndex(l.selected, len(l.keys))
}

// applyUsageLoaded reflects a signingKeyUsageCmd result: opens the
// delete-confirm prompt with the count folded into its message (design
// requirement: informed, never blocked -- there is no code path here that
// refuses to open the confirm because the count is nonzero). A stale
// response (usageLoading already false, e.g. the list was reset/reseeded
// in the meantime) is discarded.
func (l trustedKeyList) applyUsageLoaded(msg adminSigningKeyUsageLoadedMsg) trustedKeyList {
	if !l.usageLoading {
		return l
	}
	l.usageLoading = false
	if msg.err != nil {
		l.err = msg.err.Error()
		return l
	}
	message := fmt.Sprintf("This key currently verifies %d tagged image(s). Delete it anyway?", msg.count)
	if msg.capped {
		message = fmt.Sprintf("This key currently verifies at least %d tagged image(s) (stopped counting). Delete it anyway?", msg.count)
	}
	l.confirm = newConfirmPrompt("Delete Trusted Key", message, "delete", "", func(screenEnv) tea.Cmd { return nil })
	return l
}

// renderTrustedKeyList renders the list's keys as short fingerprints (via
// signingKeyFingerprints, model.go -- the exact same helper
// renderSigningPolicyKeyList already uses, so a stored key is never
// rendered in this new component either) with a "▸ " cursor on the
// selected row, mirroring renderPeerMenuRows' cursor convention
// (admin_views.go, added earlier this session for the domain menu) adapted
// to a form field rather than a full-screen menu. focused controls whether
// the list itself is drawn with focus styling (theme.inputFocus on its
// heading) when the surrounding form has moved focus elsewhere.
func renderTrustedKeyList(theme adminTheme, l trustedKeyList, focused bool) []string {
	headingStyle := theme.text
	if focused {
		headingStyle = theme.inputFocus
	}
	lines := []string{headingStyle.Render(fmt.Sprintf("Trusted Keys (%d)", len(l.keys)))}

	fingerprints := signingKeyFingerprints(l.keys)
	switch {
	case len(fingerprints) == 0:
		lines = append(lines, theme.muted.Render("No trusted keys configured."))
	default:
		for index, fingerprint := range fingerprints {
			cursor := "  "
			label := theme.text.Render(fmt.Sprintf("Key: %s", fingerprint))
			if focused && index == boundedIndex(l.selected, len(fingerprints)) {
				cursor = "▸ "
				label = theme.selected.Render(fmt.Sprintf("Key: %s", fingerprint))
			}
			lines = append(lines, cursor+label)
		}
	}

	switch {
	case l.usageLoading:
		lines = append(lines, theme.muted.Render("Checking key usage..."))
	case l.adding:
		lines = append(lines, renderTextField(theme, "New Key (PEM)", l.input, true))
	case focused:
		lines = append(lines, theme.muted.Render("n: add key | x: delete selected key"))
	}
	if l.err != "" {
		lines = append(lines, theme.error.Render(l.err))
	}
	return lines
}
