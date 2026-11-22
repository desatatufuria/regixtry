package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// featureOverrideRow is one repository row of an override screen: the catalog
// plus every stored override.
type featureOverrideRow struct {
	Repository  string
	HasOverride bool
	Detail      ports.RepositoryOverrideDetails
}

// featureOverridesScreen backs BOTH screenSecurityGitleaksRepos ("Secret
// scanning" repositories) and screenSecuritySigningRepos ("Signing policy"
// repositories): the repositories of one feature and which of them override
// it, with the override editor on the selected one (the "Repositories" half of
// the approved "Secret scanning" and "Signing policy" boards).
//
// The feature is fixed at construction, as in overrideEditor, so neither
// screen can be navigated into the other's feature.
//
// The field name editor (inside overridesList) is read by Model
// (migratedScreenCapturesTextInput): do not rename it.
type featureOverridesScreen struct {
	id screen
	overridesList
}

func newFeatureOverridesScreen(id screen, feature string) featureOverridesScreen {
	list := newOverridesList(feature)
	list.reading = true
	return featureOverridesScreen{id: id, overridesList: list}
}

func (s featureOverridesScreen) ID() screen { return s.id }

func (s featureOverridesScreen) Keys() screenKeys { return featureOverridesKeys }

func (s featureOverridesScreen) Init(env screenEnv) tea.Cmd {
	return securityLoadOverridesCmd(env, s.gen, s.feature)
}

func (s featureOverridesScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	if key, ok := msg.(keyMsg); ok {
		if s.editor.Active() {
			list, cmd := s.updateEditorKey(env, key)
			s.overridesList = list
			return s, cmd, true
		}
		return s.updateKey(env, key)
	}
	if owned, ok := msg.(securityMsg); ok && owned.securityGen() != s.gen {
		return s, nil, false
	}
	if typed, ok := msg.(securityOverridesMsg); ok {
		s.overridesList = s.applyOverrides(env, typed)
		return s, nil, false
	}
	list, cmd, _ := s.applyEditorMsg(env, msg)
	s.overridesList = list
	return s, cmd, false
}

func (s featureOverridesScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if isPasteKey(msg) {
		// Pasted text is never a command: its first character must not open
		// an editor or reload the list.
		return s, nil, true
	}
	if list, handled := s.moveKey(msg); handled {
		s.overridesList = list
		return s, nil, true
	}
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminOverview), true
	case isRuneKey(msg, 'o'):
		list, cmd := s.openEditor(env)
		s.overridesList = list
		return s, cmd, true
	case isRuneKey(msg, 'r'):
		if s.reading {
			return s, nil, true
		}
		s.reading, s.note = true, ""
		return s, securityLoadOverridesCmd(env, s.gen, s.feature), true
	}
	return s, nil, true
}

func (s featureOverridesScreen) View(theme adminTheme, env screenEnv) screenFrame {
	v := overridesView{
		title: featureNavLabel(s.feature),
		keys:  s.Keys(),
	}
	v.crumb = []string{featureNavGroup, v.title, "Repositories"}
	if s.feature == signingFeatureName {
		v.subtitle = "image signature verification"
		v.nav = shellNavSigningPolicy
		v.foot = "An override replaces the global policy for that repository. o edits the override of the selected repository."
	} else {
		v.subtitle = "Gitleaks, leaked-secret scanner"
		v.nav = shellNavSecretScanning
		v.foot = "o edits the override of the selected repository. An override replaces the global settings for it."
	}
	return s.frame(theme, env, v)
}

// featureDisplayName maps a feature identity to its display label.
func featureDisplayName(feature string) string {
	switch feature {
	case trivyFeatureName:
		return "Trivy"
	case gitleaksFeatureName:
		return "Gitleaks"
	case signingFeatureName:
		return "Signing"
	default:
		return feature
	}
}

// mergeFeatureOverrideRows is the repository list of one feature: the catalog,
// then any repository that only has a stored override.
func mergeFeatureOverrideRows(knownRepositories []string, overrides []ports.RepositoryOverrideDetails) []featureOverrideRow {
	return orderedOverrideRows(nil, knownRepositories, overrides)
}

var featureOverridesKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "override")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}}

// featureOverrideRowStatus and featureOverridesLoadedMsg are what the old
// override tables and Model's old broadcast still name; the next commit deletes
// them with those.
func featureOverrideRowStatus(row featureOverrideRow) string {
	if row.HasOverride {
		return "override active"
	}
	return "inheriting global settings"
}

type featureOverridesLoadedMsg struct {
	feature   string
	overrides []ports.RepositoryOverrideDetails
	err       error
}
