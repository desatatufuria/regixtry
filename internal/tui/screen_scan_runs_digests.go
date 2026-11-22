package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// scanRunsDigestWindow is how many digests of one repository the Scan runs
// list reads. Scan results are latest-only: one completed result per digest.
const scanRunsDigestWindow = 100

// scanRunsDigestsLoadedMsg answers the read of one repository's digests. It
// carries the read generation of the screen that asked: a foreign or stale
// answer is dropped.
type scanRunsDigestsLoadedMsg struct {
	gen        int
	repository string
	runs       []ports.ScanRun
	err        error
}

func loadScanRunsDigestsCmd(env screenEnv, gen int, repository string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return scanRunsDigestsLoadedMsg{gen: gen, repository: repository, err: errSecurityUnavailable}
		}
		runs, err := client.ListScanRuns(context.Background(), session, repository, scanRunsDigestWindow)
		return scanRunsDigestsLoadedMsg{gen: gen, repository: repository, runs: runs, err: err}
	}
}

// startDigestReads asks for the digests of every repository the summaries
// name. Until an answer arrives a repository shows its summary run, and one
// that cannot be read keeps showing it.
func (s scanRunsScreen) startDigestReads(env screenEnv) (scanRunsScreen, tea.Cmd) {
	s.reads++
	pending := map[string]bool{}
	var cmds []tea.Cmd
	for _, summary := range s.summaries {
		repository := summary.Run.Repository
		if repository == "" || pending[repository] {
			continue
		}
		pending[repository] = true
		cmds = append(cmds, loadScanRunsDigestsCmd(env, s.reads, repository))
	}
	s.digestPending, s.digests = pending, map[string][]ports.ScanRun{}
	s.rows = s.digestRows()
	return s, tea.Batch(cmds...)
}

func (s scanRunsScreen) applyDigests(msg scanRunsDigestsLoadedMsg) scanRunsScreen {
	if msg.gen != s.reads || !s.digestPending[msg.repository] {
		return s
	}
	pending := make(map[string]bool, len(s.digestPending))
	for k, v := range s.digestPending {
		if k != msg.repository {
			pending[k] = v
		}
	}
	s.digestPending = pending
	if msg.err == nil && len(msg.runs) > 0 {
		digests := make(map[string][]ports.ScanRun, len(s.digests)+1)
		for k, v := range s.digests {
			digests[k] = v
		}
		digests[msg.repository] = msg.runs
		s.digests = digests
	}
	s.rows = s.digestRows()
	return s
}

// digestRows is one row per digest, repositories in the order of the
// summaries. Older servers keep a history per digest: the newest run of each
// digest is the row. A repository whose digests are not read shows its
// summary run.
func (s scanRunsScreen) digestRows() []ports.ScanRun {
	var rows []ports.ScanRun
	for _, summary := range s.summaries {
		repository := summary.Run.Repository
		runs := s.digests[repository]
		if len(runs) == 0 {
			if !s.repositoryListed(rows, repository) {
				rows = append(rows, summary.Run)
			}
			continue
		}
		seen := map[string]bool{}
		for _, run := range runs {
			if run.Repository != repository || (run.Digest != "" && seen[run.Digest]) {
				continue
			}
			seen[run.Digest] = true
			rows = append(rows, run)
		}
	}
	return rows
}

func (s scanRunsScreen) repositoryListed(rows []ports.ScanRun, repository string) bool {
	for _, row := range rows {
		if row.Repository == repository {
			return true
		}
	}
	return false
}

// ---- values ----

// scanShortHash is the digest without its algorithm: "9c41e0…e0d2".
func scanShortHash(digest string) string {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return "—"
	}
	if i := strings.Index(digest, ":"); i >= 0 {
		digest = digest[i+1:]
	}
	if len(digest) <= 11 {
		return digest
	}
	return digest[:6] + "…" + digest[len(digest)-4:]
}

// scanDBText is the age of the vulnerability DB a result used, "3 d ago"; a
// server that does not send it shows a dash.
func scanDBText(run ports.ScanRun, now time.Time) string {
	if run.DBUpdatedAt == nil || run.DBUpdatedAt.IsZero() {
		return "—"
	}
	return scanAge(*run.DBUpdatedAt, now)
}

// scanCheckedText is when a push or sweep last confirmed the result.
func scanCheckedText(run ports.ScanRun, now time.Time) string {
	if run.LastCheckedAt == nil || run.LastCheckedAt.IsZero() {
		return "—"
	}
	return scanAge(*run.LastCheckedAt, now)
}

// scanRowStatus is the status cell: a kept result whose rescan failed reads
// "stale".
func scanRowStatus(run ports.ScanRun) string {
	if run.Stale && run.Status == ports.ScanRunStatusCompleted {
		return "stale"
	}
	return adminFirstNonEmpty(run.Status, "unknown")
}
