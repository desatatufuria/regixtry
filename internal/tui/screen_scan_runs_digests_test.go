package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"regixtry/internal/ports"
)

// scanDigestClient is one repository with n digests: one completed, one
// stale, one skipped and the rest plain results; DB and check times are set on
// all but the last, so the rows also show the older-server dash.
func scanDigestClient(n int) *fakeAdminClient {
	var runs []ports.ScanRun
	for i := range n {
		run := scanFixtureRun("team/api", fmt.Sprintf("v%d", i), ports.ScanRunStatusCompleted, -time.Duration(i+1)*time.Hour, time.Minute, i%3, i%5)
		run.ID = fmt.Sprintf("run-%03d", i)
		run.Digest = fmt.Sprintf("sha256:%064x", i+1)
		run.DBUpdatedAt = scanTime(-time.Duration(i+1) * 24 * time.Hour)
		run.LastCheckedAt = scanTime(-time.Duration(i+1) * time.Hour)
		switch {
		case i == 1:
			run.Stale, run.StaleReason = true, "trivy exited with status 1: failed to download the vulnerability database "+strings.Repeat("because the mirror is unreachable ", 3)
		case i == 2:
			run.Status, run.Error = ports.ScanRunStatusSkipped, "push scan dropped: the queue is full"
		case i == n-1:
			run.DBUpdatedAt, run.LastCheckedAt = nil, nil
		}
		runs = append(runs, run)
	}
	return &fakeAdminClient{
		repositoryScanSummaries: []ports.RepositoryScanSummary{{Run: runs[0], RunCount: n}},
		scanRuns:                runs,
	}
}

func TestScanRunsShowsStaleSkippedAndMissingFields(t *testing.T) {
	t.Parallel()
	m := scanModelAt(t, scanDigestClient(5), screenAdminScanRuns, 160, 30)
	out := scanText(m)
	for _, want := range []string{"stale", "skipped", "completed", "1 d ago", "—"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	m = scanPress(t, m, testKeyDown())
	if out := scanText(m); !strings.Contains(out, "Rescan failed: trivy exited with") {
		t.Errorf("the stale reason is not in the panel:\n%s", out)
	}
	m = scanPress(t, m, testKeyDown())
	out = scanText(m)
	if !strings.Contains(out, "Reason") || !strings.Contains(out, "push scan dropped") {
		t.Errorf("a skipped row does not say why:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "skipped") && strings.ContainsAny(line[strings.Index(line, "skipped"):strings.Index(line, "skipped")+9], "123456789") {
			t.Errorf("a skipped row shows counts: %q", line)
		}
	}
	// The last digest comes from a server that sends neither field.
	m = scanPress(t, m, testKeyDown(), testKeyDown())
	if out := scanText(m); strings.Count(out, "—") < 3 {
		t.Errorf("missing fields do not degrade to a dash:\n%s", out)
	}
}

func TestScanRunsAgeNeverShowsSeconds(t *testing.T) {
	t.Parallel()
	now := fixedTestTime
	for _, tc := range []struct {
		ago  time.Duration
		want string
	}{{30 * time.Second, "<1 h ago"}, {59 * time.Minute, "<1 h ago"}, {5 * time.Hour, "5 h ago"}, {3 * 24 * time.Hour, "3 d ago"}, {800 * 24 * time.Hour, "2 y ago"}} {
		at := now.Add(-tc.ago)
		run := ports.ScanRun{DBUpdatedAt: &at, LastCheckedAt: &at}
		if got := scanDBText(run, now); got != tc.want {
			t.Errorf("scanDBText(%v) = %q, want %q", tc.ago, got, tc.want)
		}
		if got := scanCheckedText(run, now); got != tc.want {
			t.Errorf("scanCheckedText(%v) = %q, want %q", tc.ago, got, tc.want)
		}
	}
	if scanDBText(ports.ScanRun{}, now) != "—" || scanCheckedText(ports.ScanRun{}, now) != "—" {
		t.Error("missing times must show a dash")
	}
}

func TestScanRunsOneRowPerDigestEvenWithAnOlderServersHistory(t *testing.T) {
	t.Parallel()
	client := scanDigestClient(3)
	dup := client.scanRuns[0]
	dup.ID = "older-run-of-the-same-digest"
	client.scanRuns = append(client.scanRuns, dup)
	s := scanListScreen(t, scanModelAt(t, client, screenAdminScanRuns, 150, 24), slotScanRuns)
	if len(s.rows) != 3 {
		t.Fatalf("rows = %d, want 3 (the older run of a digest is not a row)", len(s.rows))
	}
	if s.rows[0].ID != "run-000" {
		t.Fatalf("first row = %q, want the newest run of the digest", s.rows[0].ID)
	}
}

func TestScanRunsDigestReadsAreGuardedAndFailOpenToTheSummaryRow(t *testing.T) {
	t.Parallel()
	summary := scanFixtureRun("team/api", "1.8.2", ports.ScanRunStatusCompleted, -time.Hour, time.Minute, 1, 1)
	loaded := adminRepositoryScanSummariesLoadedMsg{summaries: []ports.RepositoryScanSummary{{Run: summary, RunCount: 1}}}
	next, cmd, _ := newScanRunsScreen().applySummaries(screenEnv{}, loaded)
	s := next.(scanRunsScreen)
	if cmd == nil || len(s.rows) != 1 || len(s.digestPending) != 1 {
		t.Fatalf("after the summaries: cmd %v, rows %d, pending %d; want the summary row and one read", cmd != nil, len(s.rows), len(s.digestPending))
	}
	other := summary
	other.Digest = "sha256:other"
	// A foreign generation and a foreign repository are dropped.
	if got := s.applyDigests(scanRunsDigestsLoadedMsg{gen: s.reads + 1, repository: "team/api", runs: []ports.ScanRun{other, summary}}); len(got.rows) != 1 || len(got.digestPending) != 1 {
		t.Fatal("a stale-generation answer was applied")
	}
	if got := s.applyDigests(scanRunsDigestsLoadedMsg{gen: s.reads, repository: "team/other", runs: []ports.ScanRun{other}}); len(got.rows) != 1 {
		t.Fatal("an answer for a repository nobody asked for was applied")
	}
	// A failed read keeps the summary row and ends the wait.
	failed := s.applyDigests(scanRunsDigestsLoadedMsg{gen: s.reads, repository: "team/api", err: errors.New("boom")})
	if len(failed.rows) != 1 || failed.rows[0].ID != summary.ID || len(failed.digestPending) != 0 {
		t.Fatalf("a failed read: rows %d pending %d, want the summary row and nothing pending", len(failed.rows), len(failed.digestPending))
	}
	// A good answer replaces the summary row with the digests.
	good := s.applyDigests(scanRunsDigestsLoadedMsg{gen: s.reads, repository: "team/api", runs: []ports.ScanRun{summary, other}})
	if len(good.rows) != 2 {
		t.Fatalf("rows = %d, want the 2 digests", len(good.rows))
	}
}

func TestScanRunsDigestListFitsEverySize(t *testing.T) {
	t.Parallel()
	for _, n := range []int{5, 200} {
		for _, size := range scanViewSizes {
			t.Run(fmt.Sprintf("%d_digests_%dx%d", n, size.w, size.h), func(t *testing.T) {
				t.Parallel()
				m := scanModelAt(t, scanDigestClient(n), screenAdminScanRuns, size.w, size.h)
				scanAssertFits(t, "list", scanText(m), size.w, size.h)
				for range 3 {
					m = scanPress(t, m, testKeyDown())
					scanAssertFits(t, "stale and skipped selected", scanText(m), size.w, size.h)
				}
				for range n + 10 {
					m = scanPress(t, m, testKeyDown())
				}
				out := scanText(m)
				scanAssertFits(t, "last digest", out, size.w, size.h)
				if !strings.Contains(out, "▸") {
					t.Errorf("the selection left the screen:\n%s", out)
				}
				if got := scanListScreen(t, m, slotScanRuns).cursor; got != n-1 {
					t.Errorf("cursor = %d, want %d", got, n-1)
				}
			})
		}
	}
}

func TestScanRunsDigestGoldens(t *testing.T) {
	for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
		name := fmt.Sprintf("scan_runs_digests_%dx%d", size.w, size.h)
		t.Run(name, func(t *testing.T) {
			m := scanModelAt(t, scanDigestClient(8), screenAdminScanRuns, size.w, size.h)
			m = scanPress(t, m, testKeyDown())
			assertScanGolden(t, name, scanText(m))
		})
	}
}
