package tui

import (
	"maps"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// scanRunsSummaryLimit is how many repositories' latest scan the two lists
// ask for; the server returns them severity-ordered.
const scanRunsSummaryLimit = 25

// scanRunsScreen backs two sidebar items (tui-v2-redesign, the "Scan runs"
// and "Secret findings" boards) over the same read, the latest Trivy scan of
// each repository (ListRepositoryScanSummaries):
//
//   - Scan runs lists those runs.
//   - Secret findings lists the leaked secrets of those runs: one
//     GetSecretScanFindings per run, answered through
//     adminSecretScanFindingsLoadedMsg.
//
// Enter opens the scan history of the selected repository on the tab the
// screen stands for. The two are told apart by id, set once at construction.
type scanRunsScreen struct {
	id screen

	// awaiting is set while a summaries read asked for by this screen is
	// outstanding: the same message also answers the Trivy repositories
	// screen, and a read nobody here asked for is ignored.
	awaiting  bool
	loaded    bool
	err       string
	summaries []ports.RepositoryScanSummary
	cursor    int

	// Secret findings only. The maps are replaced as a whole, never mutated,
	// so copies of the screen share them safely.
	requested int
	pending   map[string]bool
	secrets   map[string][]ports.SecretFinding
	failed    int
}

func newScanRunsScreen() scanRunsScreen {
	return scanRunsScreen{id: screenAdminScanRuns, awaiting: true}
}

func newSecretFindingsRunsScreen() scanRunsScreen {
	return scanRunsScreen{id: screenAdminSecretFindings, awaiting: true}
}

func (s scanRunsScreen) ID() screen { return s.id }

func (s scanRunsScreen) Keys() screenKeys { return scanRunsKeys }

func (s scanRunsScreen) Init(env screenEnv) tea.Cmd {
	return loadScanRunsScreenSummariesCmd(env, scanRunsSummaryLimit)
}

func (s scanRunsScreen) isSecretFindings() bool { return s.id == screenAdminSecretFindings }

// historyTab is the scan history tab Enter opens.
func (s scanRunsScreen) historyTab() int {
	if s.isSecretFindings() {
		return scanHistoryTabIndexLeaks
	}
	return scanHistoryTabIndexVulnerabilities
}

func (s scanRunsScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case adminRepositoryScanSummariesLoadedMsg:
		return s.applySummaries(env, typed)
	case adminSecretScanFindingsLoadedMsg:
		return s.applySecrets(typed), nil, false
	}
	return s, nil, false
}

func (s scanRunsScreen) applySummaries(env screenEnv, msg adminRepositoryScanSummariesLoadedMsg) (adminScreen, tea.Cmd, bool) {
	if !s.awaiting {
		return s, nil, false
	}
	s.awaiting = false
	s.loaded = true
	s.cursor = 0
	s.requested, s.failed = 0, 0
	s.pending, s.secrets = nil, nil
	if msg.err != nil {
		s.err = msg.err.Error()
		s.summaries = nil
		return s, nil, false
	}
	s.err = ""
	s.summaries = msg.summaries
	if !s.isSecretFindings() {
		return s, nil, false
	}
	pending := map[string]bool{}
	var cmds []tea.Cmd
	for _, summary := range s.summaries {
		run := summary.Run
		if run.Digest == "" || pending[secretKey(run.Repository, run.Digest)] {
			continue
		}
		pending[secretKey(run.Repository, run.Digest)] = true
		cmds = append(cmds, loadSecretScanFindingsCmd(env, run.Repository, run.Digest))
	}
	s.requested, s.pending, s.secrets = len(pending), pending, map[string][]ports.SecretFinding{}
	return s, tea.Batch(cmds...), false
}

func (s scanRunsScreen) applySecrets(msg adminSecretScanFindingsLoadedMsg) scanRunsScreen {
	k := secretKey(msg.repository, msg.digest)
	if !s.pending[k] {
		return s
	}
	pending := maps.Clone(s.pending)
	delete(pending, k)
	s.pending = pending
	if msg.err != nil {
		// A run without a secret scan answers with an error: no findings,
		// not a failure of the screen.
		s.failed++
		return s
	}
	secrets := maps.Clone(s.secrets)
	secrets[k] = msg.findings
	s.secrets = secrets
	return s
}

func secretKey(repository, digest string) string { return repository + "\x00" + digest }

// secretRow is one leaked secret with the run it was found in.
type secretRow struct {
	run     ports.ScanRun
	finding ports.SecretFinding
	// inRun is how many secrets that run found.
	inRun int
}

// secretRows lists every finding read so far, in the order of the runs.
func (s scanRunsScreen) secretRows() []secretRow {
	var rows []secretRow
	for _, summary := range s.summaries {
		run := summary.Run
		findings := s.secrets[secretKey(run.Repository, run.Digest)]
		for _, finding := range findings {
			rows = append(rows, secretRow{run: run, finding: finding, inRun: len(findings)})
		}
	}
	return rows
}

// rowCount is how many rows the list shows.
func (s scanRunsScreen) rowCount() int {
	if s.isSecretFindings() {
		return len(s.secretRows())
	}
	return len(s.summaries)
}

// selectedRepository is the repository of the row the cursor is on.
func (s scanRunsScreen) selectedRepository() (string, bool) {
	if s.isSecretFindings() {
		rows := s.secretRows()
		if len(rows) == 0 {
			return "", false
		}
		return rows[boundedIndex(s.cursor, len(rows))].run.Repository, true
	}
	if len(s.summaries) == 0 {
		return "", false
	}
	return s.summaries[boundedIndex(s.cursor, len(s.summaries))].Run.Repository, true
}

func (s scanRunsScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminOverview), true
	case isArrowUpKey(msg):
		s.cursor = boundedIndex(s.cursor-1, s.rowCount())
	case isArrowDownKey(msg):
		s.cursor = boundedIndex(s.cursor+1, s.rowCount())
	case isEnterKey(msg):
		repository, ok := s.selectedRepository()
		if !ok {
			return s, nil, true
		}
		return s, openScanHistory(repository, s.id, s.historyTab()), true
	case isRuneKey(msg, 'r'):
		s.awaiting, s.loaded, s.err = true, false, ""
		return s, loadScanRunsScreenSummariesCmd(env, scanRunsSummaryLimit), true
	}
	return s, nil, true
}

// loadScanRunsScreenSummariesCmd is the screenEnv-scoped equivalent of
// Model's own loadAdminRepositoryScanSummariesCmd.
func loadScanRunsScreenSummariesCmd(env screenEnv, limit int) tea.Cmd {
	return env.asModel().loadAdminRepositoryScanSummariesCmd(limit)
}

var scanRunsKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "scan history")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	quitKey(),
}}
