package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	bubbletable "github.com/evertras/bubble-table/table"
	"regixtry/internal/ports"
)

// scanHistoryTabIndexVulnerabilities/scanHistoryTabIndexLeaks index into
// newAdminScanHistoryTabs()'s fixed [Vulnerabilities, Leaks] ordering --
// used by openScanHistory's activeTab parameter to distinguish D9's two
// access flavors (Trivy's drill-down and Scan Runs default to
// Vulnerabilities-first; Secret Scan Findings defaults to Leaks-first).
const (
	scanHistoryTabIndexVulnerabilities = 0
	scanHistoryTabIndexLeaks           = 1
)

// scanHistoryScreen is the migrated Repository Alerts drill-down / Secret
// Scan Findings sub-model (Phase 19, design.md's State Migration table:
// "ScanHistoryModal -> scanHistoryScreen (Operations, Slice 3)"). It reuses
// adminScanHistoryModal's exact pre-existing shape as its own embedded
// field, so every already-tested modal-shaped helper this codebase has
// (adminScanHistoryModalTabBar, adminScanHistoryModalExecutionsColumn,
// adminScanHistoryModalFooter, adminScanHistoryDetailMatchesCursor,
// adminScanHistorySecretsMatchCursor, sortScanRunsChronologically) keeps its
// exact signature -- only the two callers that used to read
// AdminViewState.Tables (adminScanHistoryModalTableBody,
// renderAdminScanHistoryModal) change to take this screen's own
// findings/secretFindings tables directly (design.md Decision B: a migrated
// screen owns its own tables, never AdminViewState.Tables).
//
// It is deliberately NOT resolved via slotFor/m.screen (screen.go's
// slotScanHistory doc comment): m.screen never changes while it is mounted,
// mirroring slotGitleaksConfig's own Slice 1 overlay precedent -- it
// composites as a floating overlay on top of whichever screen opened it
// (returnTo), reached two ways (D9): Trivy's Repository Alerts Enter, and
// the Scan Runs/Secret Scan Findings repository pickers' own Enter. Because
// it is never routed through slotFor, its Update is instead invoked from a
// dedicated top-of-function precedence check in updateAdminKey (mirroring
// AdminViewState.Confirm's own existing precedence check) and, for non-key
// messages, through the ordinary routeAdminMsg broadcast every occupied
// slot already receives.
type scanHistoryScreen struct {
	returnTo       screen
	modal          adminScanHistoryModal
	findings       bubbletable.Model
	secretFindings bubbletable.Model
}

// ID has no meaningful identity of its own -- this screen is never resolved
// via slotFor, so it always defers to whichever screen it is floating over.
func (s scanHistoryScreen) ID() screen { return s.returnTo }

func (s scanHistoryScreen) Keys() screenKeys { return scanHistoryKeys }

func (s scanHistoryScreen) Init(env screenEnv) tea.Cmd {
	return loadScanHistoryCmd(env, s.modal.Repository)
}

func (s scanHistoryScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		return s.updateKey(env, typed)
	case tea.WindowSizeMsg:
		s.rebuildTables(env)
		return s, nil, false
	case adminScanHistoryLoadedMsg:
		if typed.err != nil || typed.repository != s.modal.Repository {
			return s, nil, false
		}
		s.modal.Runs = typed.runs
		s.modal.Cursor = 0
		s.modal.Error = ""
		if len(typed.runs) == 0 {
			s.modal.Loading = false
			s.rebuildTables(env)
			return s, nil, false
		}
		return s, loadScanRunDetailCmd(env, typed.runs[0].ID), false
	case adminScanRunDetailLoadedMsg:
		if typed.err != nil || !adminScanHistoryDetailMatchesCursor(s.modal, typed.detail) {
			return s, nil, false
		}
		s.modal.Detail = typed.detail
		s.modal.Secrets = nil
		s.modal.Error = ""
		s.rebuildTables(env)
		return s, loadSecretScanFindingsCmd(env, typed.detail.Run.Repository, typed.detail.Run.Digest), false
	case adminSecretScanFindingsLoadedMsg:
		if !adminScanHistorySecretsMatchCursor(s.modal, typed.repository, typed.digest) {
			return s, nil, false
		}
		if typed.err != nil {
			s.modal.Secrets = nil
			s.modal.Loading = false
			s.rebuildTables(env)
			return s, nil, false
		}
		s.modal.Secrets = typed.findings
		s.modal.Loading = false
		s.rebuildTables(env)
		return s, nil, false
	}
	return s, nil, false
}

func (s *scanHistoryScreen) rebuildTables(env screenEnv) {
	primary, _ := tableRoles(env.Layout)
	s.findings = buildAdminFindingsTable(s.modal.Detail.Findings, s.modal.FindingCursor, primary)
	s.secretFindings = buildAdminSecretFindingsTable(s.modal.Secrets, s.modal.FindingCursor, primary)
}

func (s scanHistoryScreen) updateKey(env screenEnv, msg tea.KeyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isShiftTabKey(msg):
		s.modal.ActiveTab = cycleIndex(s.modal.ActiveTab-1, len(s.modal.Tabs))
		s.modal.FindingCursor = 0
		s.rebuildTables(env)
		return s, nil, true
	case isTabKey(msg):
		s.modal.ActiveTab = cycleIndex(s.modal.ActiveTab+1, len(s.modal.Tabs))
		s.modal.FindingCursor = 0
		s.rebuildTables(env)
		return s, nil, true
	case isMoveLeftKey(msg):
		return s.page(env, -1)
	case isMoveRightKey(msg):
		return s.page(env, 1)
	case isMoveUpKey(msg):
		return s.moveFindingCursor(env, -1)
	case isMoveDownKey(msg):
		return s.moveFindingCursor(env, 1)
	case isEnterKey(msg):
		return s, s.openSelectedFindingLink(), true
	}
	return s, nil, true
}

// page mirrors the retired Model.pageAdminScanHistory exactly: clamped
// (boundedIndex, never wrapping) navigation between executions.
func (s scanHistoryScreen) page(env screenEnv, delta int) (adminScreen, tea.Cmd, bool) {
	if len(s.modal.Runs) == 0 {
		return s, nil, true
	}
	newCursor := boundedIndex(s.modal.Cursor+delta, len(s.modal.Runs))
	if newCursor == s.modal.Cursor {
		return s, nil, true
	}
	s.modal.Cursor = newCursor
	s.modal.Detail = ports.ScanRunDetail{}
	run := s.modal.Runs[newCursor]
	s.modal.Secrets = nil
	s.modal.Loading = true
	s.modal.Error = ""
	s.modal.FindingCursor = 0
	s.rebuildTables(env)
	return s, loadScanRunDetailCmd(env, run.ID), true
}

// moveFindingCursor mirrors the retired Model.moveAdminFindingCursor
// exactly: bounded (never wrapping) within the currently active tab's own
// list length.
func (s scanHistoryScreen) moveFindingCursor(env screenEnv, delta int) (adminScreen, tea.Cmd, bool) {
	if len(s.modal.Tabs) == 0 {
		return s, nil, true
	}
	active := s.modal.Tabs[boundedIndex(s.modal.ActiveTab, len(s.modal.Tabs))]
	length := len(s.modal.Detail.Findings)
	if active.Kind == adminScanHistoryTabLeaks {
		length = len(s.modal.Secrets)
	}
	if length == 0 {
		return s, nil, true
	}
	newCursor := boundedIndex(s.modal.FindingCursor+delta, length)
	if newCursor == s.modal.FindingCursor {
		return s, nil, true
	}
	s.modal.FindingCursor = newCursor
	s.rebuildTables(env)
	return s, nil, true
}

// openSelectedFindingLink mirrors the retired
// Model.openSelectedAdminFindingLink exactly (T3.3's threat-matrix row: the
// exec stays inside the returned tea.Cmd, never here in the key handler).
func (s scanHistoryScreen) openSelectedFindingLink() tea.Cmd {
	if len(s.modal.Tabs) == 0 {
		return nil
	}
	active := s.modal.Tabs[boundedIndex(s.modal.ActiveTab, len(s.modal.Tabs))]
	if active.Kind != adminScanHistoryTabVulnerabilities {
		return nil
	}
	findings := s.modal.Detail.Findings
	if len(findings) == 0 {
		return nil
	}
	finding := findings[boundedIndex(s.modal.FindingCursor, len(findings))]
	link := adminFindingLink(finding)
	if link == "" {
		return nil
	}
	return openAdminURLCmd(link)
}

func (s scanHistoryScreen) View(theme adminTheme, env screenEnv) screenFrame {
	// Never composited directly through renderAdminWorkspace's ordinary
	// slotFor branch (it is not slotFor-addressed); renderAdminWorkspace
	// reads s.modal/s.findings/s.secretFindings straight from
	// adminScreens[slotScanHistory] and calls renderAdminScanHistoryModal
	// itself for the overlay content. This method exists only to satisfy
	// adminScreen for routeAdminMsg's uniform broadcast loop.
	return screenFrame{}
}

// loadScanHistoryCmd/loadScanRunDetailCmd/loadSecretScanFindingsCmd are the
// screenEnv-scoped equivalents of Model's own loadAdminScanHistoryCmd/
// loadAdminScanRunDetailCmd/loadAdminSecretScanFindingsCmd (mirrors
// screen_trivy_repos.go's own *Cmd wrapper precedent).
func loadScanHistoryCmd(env screenEnv, repository string) tea.Cmd {
	return env.asModel().loadAdminScanHistoryCmd(repository)
}

func loadScanRunDetailCmd(env screenEnv, runID string) tea.Cmd {
	return env.asModel().loadAdminScanRunDetailCmd(runID)
}

func loadSecretScanFindingsCmd(env screenEnv, repository string, digest string) tea.Cmd {
	return env.asModel().loadAdminSecretScanFindingsCmd(repository, digest)
}

// scanHistoryKeys documents this screen's bindings (design.md Decision A);
// the rendered footer for this specific overlay predates the keymap-derived
// footer convention (admin_views.go:450's hand-written help line, unchanged
// by this migration -- pre-existing debt, not a new regression) and is not
// generated from this value, unlike every other migrated screen's footer.
var scanHistoryKeys = screenKeys{short: nil}
