package tui

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"regixtry/internal/ports"
)

// The scan screens have their own flag so regenerating their goldens can
// never touch another screen's goldens.
var updateScanRunsGoldens = flag.Bool("update-scan-runs", false, "rewrite the scan runs golden files")

var scanViewSizes = []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}}

func scanTime(offset time.Duration) *time.Time {
	at := fixedTestTime.Add(offset)
	return &at
}

func scanFixtureRun(repo, ref, status string, started, took time.Duration, crit, high int) ports.ScanRun {
	run := ports.ScanRun{
		ID: "run-" + repo, Repository: repo, RequestedRef: ref, Digest: "sha256:" + strings.Repeat("9c41e0d2", 8),
		Status: status, Trigger: "push", StartedAt: scanTime(started), CreatedAt: *scanTime(started),
		Critical: crit, High: high, Medium: 11, Low: 7, TrivyVersion: "0.58.1",
	}
	if status != ports.ScanRunStatusRunning {
		run.FinishedAt = scanTime(started + took)
	}
	return run
}

func scanFixtureClient() *fakeAdminClient {
	api := scanFixtureRun("team/api", "1.8.2", ports.ScanRunStatusCompleted, -time.Hour, 38*time.Second, 2, 5)
	web := scanFixtureRun("team/web", "2.0.0", ports.ScanRunStatusRunning, -10*time.Minute, 0, 0, 0)
	docs := scanFixtureRun("team/docs", "0.3.1", ports.ScanRunStatusFailed, -20*time.Hour, 12*time.Second, 0, 0)
	docs.Error = "registry unreachable"
	older := scanFixtureRun("team/api", "1.8.1", ports.ScanRunStatusCompleted, -30*time.Hour, 41*time.Second, 2, 4)
	older.ID = "run-api-older"
	older.Digest = "sha256:" + strings.Repeat("0a1b2c3d", 8)
	return &fakeAdminClient{
		repositoryScanSummaries: []ports.RepositoryScanSummary{{Run: api, RunCount: 2}, {Run: web, RunCount: 1}, {Run: docs, RunCount: 1}},
		scanRuns:                []ports.ScanRun{api, older},
		scanRunDetails: map[string]ports.ScanRunDetail{
			"run-team/api": {Run: api, Findings: []ports.ScanRunFinding{
				{Severity: "CRITICAL", VulnerabilityID: "CVE-2026-1000", PackageName: "openssl", InstalledVersion: "3.0.0", FixedVersion: "3.0.1", Fixable: true, PrimaryURL: "https://example.test/CVE-2026-1000"},
				{Severity: "HIGH", VulnerabilityID: "CVE-2026-1001", PackageName: "glibc", InstalledVersion: "2.38", FixedVersion: "2.39", Fixable: true},
				{Severity: "LOW", VulnerabilityID: "CVE-2026-1002", PackageName: "ca-certificates", InstalledVersion: "1.0", Fixable: false},
			}},
			"run-api-older": {Run: older, Findings: []ports.ScanRunFinding{{Severity: "HIGH", VulnerabilityID: "CVE-2025-0001", PackageName: "zlib", InstalledVersion: "1.2", FixedVersion: "1.3"}}},
		},
		secretScanFindings: map[string]ports.SecretScanRunDetail{
			"team/api@" + api.Digest: {Findings: []ports.SecretFinding{
				{RuleID: "aws-access-key", Description: "AWS Access Key", BlobDigest: "sha256:" + strings.Repeat("44ab19c0", 8), Path: "src/deploy/prod.env", StartLine: 14, EndLine: 14, Tags: []string{"key", "aws"}},
				{RuleID: "generic-api-key", Description: "Generic API Key", Path: "config/ci.yaml", StartLine: 42, EndLine: 44},
			}},
			"team/docs@" + docs.Digest: {Findings: []ports.SecretFinding{
				{RuleID: "slack-webhook-url", Description: "Slack Webhook URL", Path: "docs/notify.md", StartLine: 23, EndLine: 23, Tags: []string{"slack"}},
			}},
		},
	}
}

// scanRun runs cmd and everything it triggers, flattening batches the way the
// Bubble Tea runtime does.
func scanRun(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			m = scanRun(t, m, sub)
		}
		return m
	}
	updated, next := m.Update(msg)
	return scanRun(t, updated.(Model), next)
}

// scanModelAt opens one of the two list screens at the given size, with the
// screen's own loads already applied.
func scanModelAt(t *testing.T, client AdminClient, id screen, w, h int) Model {
	t.Helper()
	m := newAdminReadyModelWithCatalog(t, []string{"team/api"}, client)
	m.now = func() time.Time { return fixedTestTime }
	m = resizeModel(t, m, w, h)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedTestTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	updated, cmd := m.Update(navigateMsg{To: id})
	return scanRun(t, updated.(Model), cmd)
}

func scanPress(t *testing.T, m Model, keys ...keyMsg) Model {
	t.Helper()
	for _, k := range keys {
		updated, cmd := m.Update(k)
		m = scanRun(t, updated.(Model), cmd)
	}
	return m
}

func scanText(m Model) string { return ansi.Strip(m.View().Content) }

func scanListScreen(t *testing.T, m Model, slot screenSlot) scanRunsScreen {
	t.Helper()
	s, ok := m.adminScreens[slot].(scanRunsScreen)
	if !ok {
		t.Fatalf("slot %d holds %T, want scanRunsScreen", slot, m.adminScreens[slot])
	}
	return s
}

func assertScanGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "scan_runs", name+".golden")
	if *updateScanRunsGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update-scan-runs): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("rendered view differs from %s (run with -update-scan-runs to inspect)\n--- want\n%s\n--- got\n%s", path, strings.TrimRight(string(want), "\n"), strings.TrimRight(got, "\n"))
	}
}

func TestScanRunsShowsTheLatestRunOfEachRepositoryInsideTheShell(t *testing.T) {
	t.Parallel()
	out := scanText(scanModelAt(t, scanFixtureClient(), screenAdminScanRuns, 150, 24))
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[0], "Admin › Operations › Scan runs") || !strings.Contains(lines[0], "signed in as operator") {
		t.Errorf("header = %q", lines[0])
	}
	for _, want := range []string{"REPOSITORY", "STATUS", "STARTED", "TOOK", "CRIT", "HIGH",
		"team/api", "team/web", "team/docs", "completed", "running", "failed", "<1 min",
		"team/api:1.8.2", "completed, trigger push", "sha256:9c41…e0d2", "0.58.1", "Critical", "Medium", "Low",
		"Scan history", "Refresh", "enter", "esc back", "↑↓ move", "r refresh"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	// Only a finished run has counts; the running one shows dashes.
	for _, line := range lines {
		if strings.Contains(line, "team/web") && strings.Contains(line, "running") {
			row := line[strings.Index(line, "running"):]
			if strings.ContainsAny(strings.SplitN(row, "│", 2)[0][len("running"):], "123456789") && strings.Count(strings.SplitN(row, "│", 2)[0], " - ") < 1 {
				t.Errorf("running row shows counts: %q", line)
			}
		}
	}
}

func TestScanRunsArrowsMoveTheSelectionAndThePanelFollows(t *testing.T) {
	t.Parallel()
	m := scanModelAt(t, scanFixtureClient(), screenAdminScanRuns, 150, 24)
	m = scanPress(t, m, testKeyDown())
	if got := scanListScreen(t, m, slotScanRuns).cursor; got != 1 {
		t.Fatalf("cursor = %d, want 1", got)
	}
	if out := scanText(m); !strings.Contains(out, "team/web:2.0.0") || !strings.Contains(out, "running, trigger push") {
		t.Errorf("panel did not follow the selection:\n%s", out)
	}
	m = scanPress(t, m, testKeyDown(), testKeyDown(), testKeyDown())
	if got := scanListScreen(t, m, slotScanRuns).cursor; got != 2 {
		t.Fatalf("cursor = %d after moving past the end, want 2", got)
	}
	m = scanPress(t, m, testKeyUp(), testKeyUp(), testKeyUp())
	if got := scanListScreen(t, m, slotScanRuns).cursor; got != 0 {
		t.Fatalf("cursor = %d after moving past the start, want 0", got)
	}
}

func TestScanRunsFailedRunShowsItsError(t *testing.T) {
	t.Parallel()
	m := scanModelAt(t, scanFixtureClient(), screenAdminScanRuns, 150, 24)
	m = scanPress(t, m, testKeyDown(), testKeyDown())
	if out := scanText(m); !strings.Contains(out, "registry unreachable") {
		t.Errorf("a failed run's error is not shown:\n%s", out)
	}
}

func TestScanRunsLettersAreNotMoves(t *testing.T) {
	t.Parallel()
	m := scanModelAt(t, scanFixtureClient(), screenAdminScanRuns, 150, 24)
	m = scanPress(t, m, testKeyRunes("j"), testKeyRunes("k"), testKeyRunes("q"))
	if got := scanListScreen(t, m, slotScanRuns).cursor; got != 0 {
		t.Fatalf("j/k moved the cursor to %d", got)
	}
	if m.adminScreens[slotScanHistory] != nil {
		t.Fatal("a letter opened the history")
	}
}

func TestScanRunsEscGoesBackToTheOverview(t *testing.T) {
	t.Parallel()
	for _, id := range []screen{screenAdminScanRuns, screenAdminSecretFindings} {
		m := scanPress(t, scanModelAt(t, scanFixtureClient(), id, 150, 24), testKeyEsc())
		if m.screen != screenAdminOverview {
			t.Errorf("%s: esc went to %q, want the Overview", id, m.screen)
		}
	}
}

func TestScanRunsRefreshReadsAgain(t *testing.T) {
	t.Parallel()
	client := scanFixtureClient()
	m := scanModelAt(t, client, screenAdminScanRuns, 150, 24)
	before := client.listRepositoryScanSummariesCalls
	client.repositoryScanSummaries = client.repositoryScanSummaries[:1]
	m = scanPress(t, m, testKeyRunes("r"))
	if got := client.listRepositoryScanSummariesCalls - before; got != 1 {
		t.Fatalf("refresh made %d reads, want 1", got)
	}
	if got := len(scanListScreen(t, m, slotScanRuns).summaries); got != 1 {
		t.Fatalf("rows after refresh = %d, want 1", got)
	}
}

func TestScanRunsIgnoresAReadItDidNotAskFor(t *testing.T) {
	t.Parallel()
	m := scanModelAt(t, scanFixtureClient(), screenAdminScanRuns, 150, 24)
	updated, _ := m.Update(adminRepositoryScanSummariesLoadedMsg{summaries: nil})
	if got := len(scanListScreen(t, updated.(Model), slotScanRuns).summaries); got != 3 {
		t.Fatalf("a foreign read replaced the rows: %d left, want 3", got)
	}
}

func TestScanRunsStates(t *testing.T) {
	t.Parallel()
	t.Run("loading", func(t *testing.T) {
		t.Parallel()
		m := scanModelAt(t, scanFixtureClient(), screenAdminScanRuns, 150, 24)
		m.adminScreens[slotScanRuns] = newScanRunsScreen()
		if out := scanText(m); !strings.Contains(out, "Reading scan history…") {
			t.Errorf("no loading line:\n%s", out)
		}
	})
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		out := scanText(scanModelAt(t, &fakeAdminClient{repositoryScanSummaries: []ports.RepositoryScanSummary{}}, screenAdminScanRuns, 150, 24))
		if !strings.Contains(out, "No repository scan history found.") {
			t.Errorf("no empty line:\n%s", out)
		}
	})
	t.Run("error", func(t *testing.T) {
		t.Parallel()
		out := scanText(scanModelAt(t, &fakeAdminClient{featureErr: errors.New("boom")}, screenAdminScanRuns, 150, 24))
		if !strings.Contains(out, "Could not read scan history: boom") || !strings.Contains(out, "Press r to try again.") {
			t.Errorf("no error line:\n%s", out)
		}
	})
	t.Run("an error then a refresh recovers", func(t *testing.T) {
		t.Parallel()
		client := &fakeAdminClient{featureErr: errors.New("boom")}
		m := scanModelAt(t, client, screenAdminScanRuns, 150, 24)
		client.featureErr = nil
		client.repositoryScanSummaries = scanFixtureClient().repositoryScanSummaries
		m = scanPress(t, m, testKeyRunes("r"))
		if out := scanText(m); !strings.Contains(out, "team/api") || strings.Contains(out, "Could not read") {
			t.Errorf("did not recover:\n%s", out)
		}
	})
}

func TestScanRunsEnterOpensTheHistoryOfTheSelectedRepository(t *testing.T) {
	t.Parallel()
	m := scanModelAt(t, scanFixtureClient(), screenAdminScanRuns, 150, 24)
	m = scanPress(t, m, testKeyEnter())
	hist := scanHistoryScreenOf(t, m)
	if hist.modal.Repository != "team/api" || hist.returnTo != screenAdminScanRuns || hist.modal.ActiveTab != scanHistoryTabIndexVulnerabilities {
		t.Fatalf("history = repo %q returnTo %q tab %d", hist.modal.Repository, hist.returnTo, hist.modal.ActiveTab)
	}
	lines := strings.Split(scanText(m), "\n")
	if !strings.Contains(lines[0], "Admin › Operations › Scan runs › team/api") {
		t.Errorf("breadcrumb = %q", lines[0])
	}
	m = scanPress(t, m, testKeyEsc())
	if m.adminScreens[slotScanHistory] != nil || m.screen != screenAdminScanRuns {
		t.Fatalf("esc left history mounted=%v screen=%q, want back on the list", m.adminScreens[slotScanHistory] != nil, m.screen)
	}
}

func TestScanRunsEnterOnAnEmptyListDoesNothing(t *testing.T) {
	t.Parallel()
	m := scanPress(t, scanModelAt(t, &fakeAdminClient{repositoryScanSummaries: []ports.RepositoryScanSummary{}}, screenAdminScanRuns, 150, 24), testKeyEnter())
	if m.adminScreens[slotScanHistory] != nil {
		t.Fatal("enter opened a history with nothing selected")
	}
}

// ---- Secret findings ----

func TestSecretFindingsListsLeaksAcrossRepositories(t *testing.T) {
	t.Parallel()
	client := scanFixtureClient()
	m := scanModelAt(t, client, screenAdminSecretFindings, 150, 24)
	out := scanText(m)
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[0], "Admin › Operations › Secret findings") {
		t.Errorf("header = %q", lines[0])
	}
	for _, want := range []string{"REPOSITORY", "RULE", "LOCATION", "DESCRIPTION",
		"aws-access-key", "src/deploy/prod.env:14", "AWS Access Key", "generic-api-key", "config/ci.yaml:42-44",
		"slack-webhook-url", "team/docs", "3 findings in 2 repositories", "rotate it",
		"FINDING", "SCAN RUN", "Findings in run", "secret value itself",
		"line 14", "key, aws", "sha256:44ab…19c0", "enter scan history"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	// One read per run with a digest, none for anything else.
	if got := client.getSecretScanFindingsCalls; got != 3 {
		t.Errorf("secret reads = %d, want 3", got)
	}
}

func TestSecretFindingsEnterOpensTheLeaksOfTheSelectedFindingsRepository(t *testing.T) {
	t.Parallel()
	m := scanModelAt(t, scanFixtureClient(), screenAdminSecretFindings, 150, 24)
	m = scanPress(t, m, testKeyDown(), testKeyDown(), testKeyEnter()) // third finding: team/docs
	hist := scanHistoryScreenOf(t, m)
	if hist.modal.Repository != "team/docs" || hist.returnTo != screenAdminSecretFindings || hist.modal.ActiveTab != scanHistoryTabIndexLeaks {
		t.Fatalf("history = repo %q returnTo %q tab %d", hist.modal.Repository, hist.returnTo, hist.modal.ActiveTab)
	}
	if first := strings.Split(scanText(m), "\n")[0]; !strings.Contains(first, "Secret findings › team/docs") {
		t.Errorf("breadcrumb = %q", first)
	}
}

func TestSecretFindingsStates(t *testing.T) {
	t.Parallel()
	t.Run("no findings", func(t *testing.T) {
		t.Parallel()
		client := scanFixtureClient()
		client.secretScanFindings = nil
		out := scanText(scanModelAt(t, client, screenAdminSecretFindings, 150, 24))
		if !strings.Contains(out, "No secret findings in the latest scans.") {
			t.Errorf("no empty line:\n%s", out)
		}
	})
	t.Run("a run without a secret scan is not a failure", func(t *testing.T) {
		t.Parallel()
		client := scanFixtureClient()
		client.secretScanFindingsErr = errors.New("not found")
		out := scanText(scanModelAt(t, client, screenAdminSecretFindings, 150, 24))
		if !strings.Contains(out, "No secret findings in the latest scans.") || strings.Contains(out, "Could not read") {
			t.Errorf("a missing secret scan was reported as an error:\n%s", out)
		}
	})
	t.Run("no repositories", func(t *testing.T) {
		t.Parallel()
		out := scanText(scanModelAt(t, &fakeAdminClient{repositoryScanSummaries: []ports.RepositoryScanSummary{}}, screenAdminSecretFindings, 150, 24))
		if !strings.Contains(out, "No repository scan history found.") {
			t.Errorf("no empty line:\n%s", out)
		}
	})
	t.Run("error", func(t *testing.T) {
		t.Parallel()
		out := scanText(scanModelAt(t, &fakeAdminClient{featureErr: errors.New("boom")}, screenAdminSecretFindings, 150, 24))
		if !strings.Contains(out, "Could not read scan history: boom") {
			t.Errorf("no error line:\n%s", out)
		}
	})
	t.Run("reading", func(t *testing.T) {
		t.Parallel()
		m := scanModelAt(t, scanFixtureClient(), screenAdminSecretFindings, 150, 24)
		s := newSecretFindingsRunsScreen()
		s.loaded, s.summaries = true, scanListScreen(t, m, slotSecretFindings).summaries
		s.requested, s.pending = 3, map[string]bool{"x": true, "y": true}
		m.adminScreens[slotSecretFindings] = s
		out := scanText(m)
		if !strings.Contains(out, "Reading… 1 of 3 scans read") || !strings.Contains(out, "Reading secret findings…") {
			t.Errorf("no progress:\n%s", out)
		}
	})
}

func TestSecretFindingsAnswerForAnotherRunIsDropped(t *testing.T) {
	t.Parallel()
	m := scanModelAt(t, scanFixtureClient(), screenAdminSecretFindings, 150, 24)
	before := len(scanListScreen(t, m, slotSecretFindings).secretRows())
	updated, _ := m.Update(adminSecretScanFindingsLoadedMsg{repository: "team/other", digest: "sha256:zz", findings: []ports.SecretFinding{{RuleID: "x"}}})
	if got := len(scanListScreen(t, updated.(Model), slotSecretFindings).secretRows()); got != before {
		t.Fatalf("rows = %d, want %d: a foreign answer was applied", got, before)
	}
}

// ---- geometry and goldens ----

func scanLongNameClient() *fakeAdminClient {
	client := scanFixtureClient()
	var summaries []ports.RepositoryScanSummary
	secrets := map[string]ports.SecretScanRunDetail{}
	for i := range 40 {
		repo := fmt.Sprintf("organisation/team-%02d/service-with-a-very-long-repository-name-ñandú-%d", i, i)
		run := scanFixtureRun(repo, "release-candidate-with-a-long-reference", ports.ScanRunStatusCompleted, -time.Duration(i+1)*time.Hour, time.Duration(i)*time.Minute, i%4, i%7)
		summaries = append(summaries, ports.RepositoryScanSummary{Run: run, RunCount: 1})
		secrets[repo+"@"+run.Digest] = ports.SecretScanRunDetail{Findings: []ports.SecretFinding{{
			RuleID: "a-rule-with-a-very-long-identifier", Description: strings.Repeat("A long description ", 6), Path: "some/deeply/nested/directory/with/a/long/path/to/the/file.env",
			StartLine: 7, EndLine: 9, Tags: []string{"one", "two"}, BlobDigest: run.Digest,
		}}}
	}
	client.repositoryScanSummaries, client.secretScanFindings = summaries, secrets
	return client
}

func scanAssertFits(t *testing.T, name string, out string, w, h int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) > h {
		t.Errorf("%s: %d rows, want at most %d", name, len(lines), h)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > w {
			t.Errorf("%s: row %d is %d columns wide, want at most %d", name, i, got, w)
		}
	}
}

func TestScanListScreensFitEverySizeInEveryState(t *testing.T) {
	t.Parallel()
	clients := map[string]func() *fakeAdminClient{
		"rows": scanFixtureClient,
		"many": scanLongNameClient,
		"empty": func() *fakeAdminClient {
			return &fakeAdminClient{repositoryScanSummaries: []ports.RepositoryScanSummary{}}
		},
		"error": func() *fakeAdminClient {
			return &fakeAdminClient{featureErr: errors.New(strings.Repeat("very long error ", 20))}
		},
	}
	for _, id := range []screen{screenAdminScanRuns, screenAdminSecretFindings} {
		for name, build := range clients {
			for _, size := range scanViewSizes {
				t.Run(fmt.Sprintf("%s_%s_%dx%d", id, name, size.w, size.h), func(t *testing.T) {
					t.Parallel()
					m := scanModelAt(t, build(), id, size.w, size.h)
					scanAssertFits(t, "list", scanText(m), size.w, size.h)
					// Moving to the last row keeps the selection on screen.
					for range 50 {
						m = scanPress(t, m, testKeyDown())
					}
					out := scanText(m)
					scanAssertFits(t, "list at the end", out, size.w, size.h)
					if name == "many" && !strings.Contains(out, "▸") {
						t.Errorf("the selection left the screen:\n%s", out)
					}
				})
			}
		}
	}
}

func TestScanListScreensViewGoldens(t *testing.T) {
	cases := []struct {
		name   string
		id     screen
		client func() *fakeAdminClient
	}{
		{"scan_runs_rows", screenAdminScanRuns, scanFixtureClient},
		{"scan_runs_empty", screenAdminScanRuns, func() *fakeAdminClient {
			return &fakeAdminClient{repositoryScanSummaries: []ports.RepositoryScanSummary{}}
		}},
		{"scan_runs_error", screenAdminScanRuns, func() *fakeAdminClient { return &fakeAdminClient{featureErr: errors.New("admin API returned 500")} }},
		{"scan_runs_many", screenAdminScanRuns, scanLongNameClient},
		{"secret_findings_rows", screenAdminSecretFindings, scanFixtureClient},
		{"secret_findings_empty", screenAdminSecretFindings, func() *fakeAdminClient {
			return &fakeAdminClient{repositoryScanSummaries: []ports.RepositoryScanSummary{}}
		}},
		{"secret_findings_error", screenAdminSecretFindings, func() *fakeAdminClient { return &fakeAdminClient{featureErr: errors.New("admin API returned 500")} }},
		{"secret_findings_many", screenAdminSecretFindings, scanLongNameClient},
	}
	for _, tc := range cases {
		for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
			name := fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h)
			t.Run(name, func(t *testing.T) {
				assertScanGolden(t, name, scanText(scanModelAt(t, tc.client(), tc.id, size.w, size.h)))
			})
		}
	}
}

func TestScanTookNeverShowsSeconds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		seconds int
		want    string
	}{{0, "<1 min"}, {59, "<1 min"}, {60, "1 min"}, {3599, "59 min"}, {3600, "1 h"}, {7800, "2 h 10 min"}, {90000, "1 d 1 h"}, {86400, "1 d"}} {
		started := fixedTestTime
		finished := started.Add(time.Duration(tc.seconds) * time.Second)
		run := ports.ScanRun{StartedAt: &started, FinishedAt: &finished}
		if got := scanTook(run); got != tc.want {
			t.Errorf("scanTook(%d s) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
	if got := scanTook(ports.ScanRun{}); got != "-" {
		t.Errorf("scanTook of an unfinished run = %q, want -", got)
	}
}
