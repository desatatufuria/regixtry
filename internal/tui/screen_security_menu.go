package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	bubbletable "github.com/evertras/bubble-table/table"
	"regixtry/internal/ports"
)

// securityMenuScreen is the Security & Compliance domain menu (design.md
// Decision I): screenAdminFeatures repurposed as a bare 3-row peer list
// (Trivy/Gitleaks/Signing). Per the Phase 11 resolved-gap addendum, it holds
// NO page/action state of its own -- Enter on a row navigates into that
// feature's own top-level screen (trivyConfigScreen/
// screenSecurityGitleaksConfig/screenSecuritySigningConfig), which loads its
// own FeaturePage independently.
type securityMenuScreen struct {
	features []ports.FeatureSummary
	selected int
	table    bubbletable.Model
	loaded   bool
	err      string
}

func newSecurityMenuScreen() securityMenuScreen { return securityMenuScreen{} }

func (s securityMenuScreen) ID() screen { return screenAdminFeatures }

func (s securityMenuScreen) Keys() screenKeys { return securityMenuKeys }

// Init loads the Built-in Features list, mirroring the pre-change
// openAdminFeatures opener's own loadAdminFeaturesCmd call.
func (s securityMenuScreen) Init(env screenEnv) tea.Cmd {
	return loadSecurityFeaturesCmd(env)
}

func (s securityMenuScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		return s.updateKey(env, typed)
	case tea.WindowSizeMsg:
		s.rebuildTable(env)
		return s, nil, false
	case adminFeaturesLoadedMsg:
		if typed.err != nil {
			s.err = typed.err.Error()
			return s, nil, false
		}
		s.features = append([]ports.FeatureSummary(nil), typed.features...)
		s.selected = boundedIndex(s.selected, len(s.features))
		s.loaded = true
		s.err = ""
		s.rebuildTable(env)
		return s, nil, false
	}
	return s, nil, false
}

func (s *securityMenuScreen) rebuildTable(env screenEnv) {
	if len(s.features) == 0 {
		return
	}
	primary, _ := tableRoles(env.Layout)
	s.table = buildAdminFeaturesTable(newAdminTheme(), s.features, s.selected, primary)
}

func (s securityMenuScreen) updateKey(env screenEnv, msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminUsers), true
	case isMoveUpKey(msg):
		if len(s.features) == 0 {
			return s, nil, true
		}
		s.selected = boundedIndex(s.selected-1, len(s.features))
		s.rebuildTable(env)
		return s, nil, true
	case isMoveDownKey(msg):
		if len(s.features) == 0 {
			return s, nil, true
		}
		s.selected = boundedIndex(s.selected+1, len(s.features))
		s.rebuildTable(env)
		return s, nil, true
	case isEnterKey(msg):
		return s, s.navigateToSelected(), true
	case isRuneKey(msg, 'r'):
		return s, loadSecurityFeaturesCmd(env), true
	}
	return s, nil, true
}

// navigateToSelected asks the router (design.md screen.go "navigate") to
// switch to the highlighted feature's own top-level screen.
func (s securityMenuScreen) navigateToSelected() tea.Cmd {
	if len(s.features) == 0 {
		return nil
	}
	switch s.features[boundedIndex(s.selected, len(s.features))].Name {
	case trivyFeatureName:
		return navigate(screenSecurityTrivy)
	case gitleaksFeatureName:
		return navigate(screenSecurityGitleaksConfig)
	case signingFeatureName:
		return navigate(screenSecuritySigningConfig)
	}
	return nil
}

func (s securityMenuScreen) View(theme adminTheme, env screenEnv) screenFrame {
	lines := []string{theme.subheading.Render("Built-in Features")}
	switch {
	case s.err != "":
		lines = append(lines, theme.error.Render(s.err))
	case !s.loaded:
		lines = append(lines, theme.muted.Render("Loading built-in features..."))
	case len(s.features) == 0:
		lines = append(lines, theme.muted.Render("No built-in features available."))
	default:
		lines = append(lines, s.table.View())
	}
	lines = append(lines, renderAdminOperatorFooter(theme, env.Session, env.now())...)
	return screenFrame{
		Context: "Security & Compliance",
		Body:    renderSection(theme, strings.Join(lines, "\n"), env.Layout),
	}
}

// loadSecurityFeaturesCmd is the screenEnv-scoped equivalent of Model's own
// loadAdminFeaturesCmd (mirrors screen_gitleaks_config.go's
// configureFeatureCmd wrapper).
func loadSecurityFeaturesCmd(env screenEnv) tea.Cmd {
	return env.asModel().loadAdminFeaturesCmd()
}

var securityMenuKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up"), key.WithHelp("Up", "previous feature")),
	key.NewBinding(key.WithKeys("down"), key.WithHelp("Down", "next feature")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "open feature")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "back")),
	key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
}}
