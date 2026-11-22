package tui

import (
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// scanHistoryTabIndexVulnerabilities/scanHistoryTabIndexLeaks index into
// newAdminScanHistoryTabs()'s fixed [Vulnerabilities, Leaks] ordering --
// used by openScanHistory's activeTab parameter: Trivy's drill-down and Scan
// runs open on Vulnerabilities, Secret findings on Leaks.
const (
	scanHistoryTabIndexVulnerabilities = 0
	scanHistoryTabIndexLeaks           = 1
)

// scanHistoryScreen is the scan history of one repository: its runs, newest
// first, one at a time with that run's findings (Vulnerabilities) and leaked
// secrets (Leaks). Left and Right move between runs, Tab between the two
// lists, Up and Down inside the list, Enter opens a vulnerability's advisory.
//
// It is deliberately NOT resolved via slotFor/m.screen (screen.go's
// slotScanHistory doc comment): m.screen never changes while it is mounted
// and it draws the whole shell over whichever screen opened it (returnTo),
// reached two ways: Trivy's Repository Alerts Enter, and the Scan runs and
// Secret findings lists' own Enter. Because it is never routed through
// slotFor, its Update is invoked from a dedicated top-of-function precedence
// check in updateAdminKey and, for non-key messages, through the ordinary
// routeAdminMsg broadcast every occupied slot already receives.
type scanHistoryScreen struct {
	returnTo screen
	modal    adminScanHistoryModal
}

// ID has no meaningful identity of its own -- this screen is never resolved
// via slotFor, so it always defers to the screen it was opened from (returnTo).
func (s scanHistoryScreen) ID() screen { return s.returnTo }

func (s scanHistoryScreen) Init(env screenEnv) tea.Cmd {
	return loadScanHistoryCmd(env, s.modal.Repository)
}

func (s scanHistoryScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case adminScanHistoryLoadedMsg:
		if typed.repository != s.modal.Repository {
			return s, nil, false
		}
		if typed.err != nil {
			s.modal.Error = typed.err.Error()
			s.modal.Loading = false
			return s, nil, false
		}
		s.modal.Runs = typed.runs
		s.modal.Cursor = 0
		s.modal.Error = ""
		if len(typed.runs) == 0 {
			s.modal.Loading = false
			return s, nil, false
		}
		return s, loadScanRunDetailCmd(env, typed.runs[0].ID), false
	case adminScanRunDetailLoadedMsg:
		if typed.err != nil {
			// The error carries no run: it belongs to the run being read
			// only while nothing of it has arrived.
			if s.modal.Detail.Run.ID == "" && len(s.modal.Runs) > 0 {
				s.modal.Error = typed.err.Error()
				s.modal.Loading = false
			}
			return s, nil, false
		}
		if !adminScanHistoryDetailMatchesCursor(s.modal, typed.detail) {
			return s, nil, false
		}
		s.modal.Detail = typed.detail
		s.modal.Secrets = nil
		s.modal.Error = ""
		return s, loadSecretScanFindingsCmd(env, typed.detail.Run.Repository, typed.detail.Run.Digest), false
	case adminSecretScanFindingsLoadedMsg:
		if !adminScanHistorySecretsMatchCursor(s.modal, typed.repository, typed.digest) {
			return s, nil, false
		}
		// Informational only: a run without a secret scan is an empty Leaks
		// list, not an error.
		s.modal.Secrets = nil
		if typed.err == nil {
			s.modal.Secrets = typed.findings
		}
		s.modal.Loading = false
		return s, nil, false
	}
	return s, nil, false
}

func (s scanHistoryScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isShiftTabKey(msg):
		s.modal.ActiveTab = cycleIndex(s.modal.ActiveTab-1, len(s.modal.Tabs))
		s.modal.FindingCursor = 0
		return s, nil, true
	case isTabKey(msg):
		s.modal.ActiveTab = cycleIndex(s.modal.ActiveTab+1, len(s.modal.Tabs))
		s.modal.FindingCursor = 0
		return s, nil, true
	case isMoveLeftKey(msg):
		return s.page(env, -1)
	case isMoveRightKey(msg):
		return s.page(env, 1)
	case isMoveUpKey(msg):
		return s.moveFindingCursor(-1)
	case isMoveDownKey(msg):
		return s.moveFindingCursor(1)
	case isEnterKey(msg):
		return s, s.openSelectedFindingLink(), true
	}
	return s, nil, true
}

// page moves between runs: clamped, never wrapping.
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
	return s, loadScanRunDetailCmd(env, run.ID), true
}

// moveFindingCursor moves inside the active tab's list: clamped, never
// wrapping.
func (s scanHistoryScreen) moveFindingCursor(delta int) (adminScreen, tea.Cmd, bool) {
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
	return s, nil, true
}

// openSelectedFindingLink opens the selected vulnerability's advisory. The
// exec stays inside the returned tea.Cmd, never in the key handler.
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

// loadScanHistoryCmd/loadScanRunDetailCmd/loadSecretScanFindingsCmd are the
// screenEnv-scoped equivalents of Model's own loadAdminScanHistoryCmd/
// loadAdminScanRunDetailCmd/loadAdminSecretScanFindingsCmd.
func loadScanHistoryCmd(env screenEnv, repository string) tea.Cmd {
	return env.asModel().loadAdminScanHistoryCmd(repository)
}

func loadScanRunDetailCmd(env screenEnv, runID string) tea.Cmd {
	return env.asModel().loadAdminScanRunDetailCmd(runID)
}

func loadSecretScanFindingsCmd(env screenEnv, repository string, digest string) tea.Cmd {
	return env.asModel().loadAdminSecretScanFindingsCmd(repository, digest)
}
