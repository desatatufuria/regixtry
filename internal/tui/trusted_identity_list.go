package tui

import (
	"fmt"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"regixtry/internal/ports"
)

// identityAddField distinguishes which of a pending identity's two fields
// (SAN regexp, OIDC issuer) is currently receiving input while
// trustedIdentityList is in its adding state -- unlike trustedKeyList, one
// identity entry needs two values, not one pasted PEM blob, so adding
// cycles between them with Tab instead of accumulating a single value.
type identityAddField int

const (
	identityAddFieldRegexp identityAddField = iota
	identityAddFieldIssuer
)

// trustedIdentityList is a navigable, editable list of trusted keyless
// (Fulcio/OIDC) identities -- a certificate SAN regexp paired with a
// required OIDC issuer -- mirroring trustedKeyList's add/select/delete
// shape (trusted_key_list.go) for the signing-keyless-verification
// capability, shared by the global Signing config screen
// (signingPolicyModal) and any per-repository signing override
// (overrideEditor).
//
// Unlike trustedKeyList, there is no delete-usage-count lookup here: an
// identity anchor has no equivalent "how many tagged images currently
// verify against this" advisory endpoint, so 'x' removes the selected entry
// directly, with no confirm prompt.
type trustedIdentityList struct {
	identities []ports.TrustedIdentity
	selected   int

	// adding tracks an in-progress two-field entry before Enter commits it;
	// false the rest of the time (list-navigation mode).
	adding      bool
	addFocus    identityAddField
	inputRegexp string
	inputIssuer string
	// err is a validation error from a rejected add (client-side
	// pre-validation only -- the authoritative validation stays
	// server-side on save, see commitAdd's doc comment).
	err string
}

// newTrustedIdentityList constructs a list seeded with identities (a
// defensive copy).
func newTrustedIdentityList(identities []ports.TrustedIdentity) trustedIdentityList {
	return trustedIdentityList{identities: append([]ports.TrustedIdentity(nil), identities...)}
}

// SetIdentities replaces the list's identities wholesale (used by the same
// prefill flow trustedKeyList.SetKeys backs) without disturbing any
// in-progress add.
func (l trustedIdentityList) SetIdentities(identities []ports.TrustedIdentity) trustedIdentityList {
	l.identities = append([]ports.TrustedIdentity(nil), identities...)
	l.selected = boundedIndex(l.selected, len(l.identities))
	return l
}

// Identities returns the list's current identities -- the value a caller
// persists on save.
func (l trustedIdentityList) Identities() []ports.TrustedIdentity { return l.identities }

// update handles one key while the list has focus, mirroring
// trustedKeyList.update's Up/Down/'n'/'x' claims. Unlike trustedKeyList's
// update, this never dispatches a tea.Cmd (no async usage-count lookup
// exists for identities), so callers that need the (list, cmd, consumed)
// shape trustedKeyList.update returns wrap this with a nil cmd.
func (l trustedIdentityList) update(msg tea.KeyMsg) (trustedIdentityList, bool) {
	if l.adding {
		switch {
		case isEscKey(msg):
			l.adding = false
			l.addFocus = identityAddFieldRegexp
			l.inputRegexp = ""
			l.inputIssuer = ""
			l.err = ""
			return l, true
		case isTabKey(msg):
			if l.addFocus == identityAddFieldRegexp {
				l.addFocus = identityAddFieldIssuer
			} else {
				l.addFocus = identityAddFieldRegexp
			}
			l.err = ""
			return l, true
		case isEnterKey(msg):
			l.commitAdd()
			return l, true
		case isBackspaceKey(msg):
			switch l.addFocus {
			case identityAddFieldRegexp:
				l.inputRegexp = trimLastRune(l.inputRegexp)
			default:
				l.inputIssuer = trimLastRune(l.inputIssuer)
			}
			l.err = ""
			return l, true
		}
		if msg.Type == tea.KeyRunes {
			switch l.addFocus {
			case identityAddFieldRegexp:
				l.inputRegexp += string(msg.Runes)
			default:
				l.inputIssuer += string(msg.Runes)
			}
			l.err = ""
			return l, true
		}
		return l, true
	}

	switch {
	case isMoveUpKey(msg):
		l.selected = boundedIndex(l.selected-1, len(l.identities))
		return l, true
	case isMoveDownKey(msg):
		l.selected = boundedIndex(l.selected+1, len(l.identities))
		return l, true
	case isRuneKey(msg, 'n'):
		l.adding = true
		l.addFocus = identityAddFieldRegexp
		l.inputRegexp = ""
		l.inputIssuer = ""
		l.err = ""
		return l, true
	case isRuneKey(msg, 'x'):
		if len(l.identities) == 0 {
			return l, true
		}
		l.removeAt(boundedIndex(l.selected, len(l.identities)))
		return l, true
	}
	return l, false
}

// commitAdd validates the pending regexp/issuer pair client-side -- the
// same shape of check normalizeSigningOverride/decodeSigningPolicySettings
// (the authoritative, server-side validation) already apply at save time --
// before appending it. This is deliberately only a pre-validation
// convenience, never a replacement for the server's own check.
func (l *trustedIdentityList) commitAdd() {
	regexpValue := strings.TrimSpace(l.inputRegexp)
	issuer := strings.TrimSpace(l.inputIssuer)
	if regexpValue == "" {
		l.err = "certificate_identity_regexp is required"
		return
	}
	if _, err := regexp.Compile(regexpValue); err != nil {
		l.err = "invalid regexp: " + err.Error()
		return
	}
	if issuer == "" {
		l.err = "certificate_oidc_issuer is required"
		return
	}
	l.identities = append(l.identities, ports.TrustedIdentity{CertificateIdentityRegexp: regexpValue, CertificateOIDCIssuer: issuer})
	l.selected = len(l.identities) - 1
	l.adding = false
	l.addFocus = identityAddFieldRegexp
	l.inputRegexp = ""
	l.inputIssuer = ""
	l.err = ""
}

// removeAt deletes the identity at index (already bounds-checked by the
// 'x' handler above at request time).
func (l *trustedIdentityList) removeAt(index int) {
	if index < 0 || index >= len(l.identities) {
		return
	}
	l.identities = append(l.identities[:index], l.identities[index+1:]...)
	l.selected = boundedIndex(l.selected, len(l.identities))
}

// renderTrustedIdentityList renders the list's identities as "SAN regexp
// (issuer)" pairs, mirroring renderTrustedKeyList's cursor/heading
// convention, with "▸ " marking the selected row. Unlike a trusted key, an
// identity's regexp/issuer are not secret, so both are shown in full.
func renderTrustedIdentityList(theme adminTheme, l trustedIdentityList, focused bool) []string {
	headingStyle := theme.text
	if focused {
		headingStyle = theme.inputFocus
	}
	lines := []string{headingStyle.Render(fmt.Sprintf("Trusted Identities (%d)", len(l.identities)))}

	switch {
	case len(l.identities) == 0:
		lines = append(lines, theme.muted.Render("No trusted identities configured."))
	default:
		for index, identity := range l.identities {
			cursor := "  "
			text := fmt.Sprintf("Identity: %s (%s)", identity.CertificateIdentityRegexp, identity.CertificateOIDCIssuer)
			label := theme.text.Render(text)
			if focused && index == boundedIndex(l.selected, len(l.identities)) {
				cursor = "▸ "
				label = theme.selected.Render(text)
			}
			lines = append(lines, cursor+label)
		}
	}

	switch {
	case l.adding:
		lines = append(lines, renderTextField(theme, "New Identity Regexp", l.inputRegexp, l.addFocus == identityAddFieldRegexp))
		lines = append(lines, renderTextField(theme, "New Identity Issuer", l.inputIssuer, l.addFocus == identityAddFieldIssuer))
	case focused:
		lines = append(lines, theme.muted.Render("n: add identity | x: delete selected identity"))
	}
	if l.err != "" {
		lines = append(lines, theme.error.Render(l.err))
	}
	return lines
}
