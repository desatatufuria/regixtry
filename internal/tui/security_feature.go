package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// securityFeature is what the three Security configuration screens share: the
// backend-declared feature page, its actions (enable, disable and the runtime
// ones, with their confirm), the read-only per-repository overrides list and
// the last failure. Every method returns the new value; none mutates its
// receiver.
//
// The field names confirm and err are read by Model
// (migratedScreenCapturesTextInput): do not rename them.
type securityFeature struct {
	feature string
	page    ports.FeaturePage
	loaded  bool
	// err is the failure of the last read or action, shown above the cards.
	err string
	// confirm is the screen's own confirm-before-action prompt.
	confirm confirmPrompt
	// submitting is the progress text shown while a confirmed action runs.
	submitting string
	// note is the outcome of the last successful change ("Configuration
	// saved."); the full-screen shell has no status line of its own.
	note string
	// acting is true from the key that started an action until its result
	// arrives: further action keys are ignored meanwhile.
	acting    bool
	overrides securityOverrides
}

func newSecurityFeature(feature string) securityFeature {
	return securityFeature{feature: feature}
}

// load reads the page. The reads that depend on it follow, one after the other
// (the page, then the screen's own policy, then the overrides), each started by
// the result of the one before.
func (f securityFeature) load(env screenEnv) tea.Cmd {
	return loadFeaturePageCmd(env, f.feature)
}

// loadOverrides reads the feature's per-repository overrides.
func (f securityFeature) loadOverrides(env screenEnv) tea.Cmd {
	return loadFeatureOverridesCmd(env, f.feature)
}

// applyPage stores a loaded page. ok is false when the message belongs to
// another feature.
func (f securityFeature) applyPage(msg adminFeaturePageLoadedMsg) (securityFeature, bool) {
	if msg.feature != f.feature {
		return f, false
	}
	if msg.err != nil {
		f.err = msg.err.Error()
		return f, true
	}
	f.page = msg.page
	f.loaded = true
	f.err = ""
	return f, true
}

// applyOverrides stores a loaded overrides list of this feature.
func (f securityFeature) applyOverrides(env screenEnv, msg featureOverridesLoadedMsg) securityFeature {
	if msg.feature == f.feature {
		f.overrides = f.overrides.apply(env, msg)
	}
	return f
}

// applyActionResult closes the confirm and the in-flight state of an action of
// this feature and reports whether the page must be read again.
func (f securityFeature) applyActionResult(msg adminFeatureActionCompletedMsg) (securityFeature, bool) {
	if msg.feature != f.feature {
		return f, false
	}
	f.confirm = confirmPrompt{}
	f.submitting = ""
	f.acting = false
	if msg.err != nil {
		f.err = msg.err.Error()
		return f, false
	}
	f.err = ""
	f.note = strings.TrimSpace(msg.result.Message)
	return f, true
}

// updateConfirm routes a key to the open confirm prompt.
func (f securityFeature) updateConfirm(env screenEnv, key keyMsg) (securityFeature, tea.Cmd) {
	before := f.confirm
	next, cmd, _ := f.confirm.update(env, key)
	f.confirm = next
	switch {
	case cmd != nil:
		f.submitting = before.submitting
		f.acting = true
	case isEscKey(key) && !f.acting:
		// Esc closes the dialog but never the action already running: its
		// result still arrives and clears the in-flight state.
		f.submitting = ""
	}
	return f, cmd
}

// actionKey runs the enable, disable and runtime actions the page declares.
// handled is false when the key is not an action key of this page.
func (f securityFeature) actionKey(env screenEnv, msg keyMsg) (securityFeature, tea.Cmd, bool) {
	action, ok := featureActionForKey(msg, f.page)
	if !ok {
		return f, nil, false
	}
	if f.acting {
		return f, nil, true
	}
	feature := f.feature
	run := func(env screenEnv) tea.Cmd { return executeFeatureActionCmd(env, feature, action.ID) }
	if strings.TrimSpace(action.ConfirmMessage) != "" {
		submitting := fmt.Sprintf("Submitting %s for %s...", action.ID, feature)
		f.confirm = newConfirmPrompt(adminFirstNonEmpty(action.ConfirmTitle, action.Label), action.ConfirmMessage, strings.ToLower(strings.TrimSpace(action.Label)), submitting, run).
			withTarget(featureDisplayName(feature), "")
		return f, nil, true
	}
	f.acting = true
	f.submitting = fmt.Sprintf("Submitting %s for %s...", action.ID, feature)
	return f, run(env), true
}

// configureFeatureCmd saves one feature's configuration.
func configureFeatureCmd(env screenEnv, name string, input ports.FeatureConfigureInput) tea.Cmd {
	return env.asModel().configureFeatureCmd(name, input)
}

// securityDuration shows a backend duration the short way: "24h0m0s" is
// "24h" and "5m0s" is "5m". Text that is not a duration is returned as is.
func securityDuration(text string) string {
	text = strings.TrimSpace(text)
	if _, err := time.ParseDuration(text); err != nil {
		return text
	}
	if strings.HasSuffix(text, "m0s") {
		text = strings.TrimSuffix(text, "0s")
	}
	if strings.HasSuffix(text, "h0m") {
		text = strings.TrimSuffix(text, "0m")
	}
	return text
}
