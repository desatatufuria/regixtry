package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"regixtry/internal/ports"
)

// historyModelAt opens the scan history of team/api from the Scan runs list.
func historyModelAt(t *testing.T, client AdminClient, w, h int) Model {
	t.Helper()
	return scanPress(t, scanModelAt(t, client, screenAdminScanRuns, w, h), testKeyEnter())
}

func TestScanHistoryShowsTheRunAndItsFindings(t *testing.T) {
	t.Parallel()
	out := scanText(historyModelAt(t, scanFixtureClient(), 150, 24))
	for _, want := range []string{"Admin › Operations › Scan runs › team/api", "Run 1 of 2", "Vulnerabilities (3)", "Leaks (2)",
		"SEVERITY", "FINDING", "PACKAGE", "INSTALLED", "FIXED", "CRITICAL", "CVE-2026-1000", "openssl", "3.0.0", "3.0.1", "n/a",
		"team/api:1.8.2", "sha256:9c41…e0d2", "enter open advisory", "←→ run", "esc back", "enter opens the advisory"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q", want)
		}
	}
}

func TestScanHistoryLeaksTabListsSecretsWithoutTheirValues(t *testing.T) {
	t.Parallel()
	m := scanPress(t, historyModelAt(t, scanFixtureClient(), 150, 24), testKeyTab())
	out := scanText(m)
	for _, want := range []string{"RULE", "LOCATION", "DESCRIPTION", "aws-access-key", "src/deploy/prod.env:14", "AWS Access Key (key",
		"generic-api-key", "config/ci.yaml:42-44", "secret value itself is never shown."} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	if strings.Contains(out, "enter open advisory") {
		t.Error("the Leaks tab offers an advisory")
	}
}

func TestScanHistoryTabsCycleBothWays(t *testing.T) {
	t.Parallel()
	m := historyModelAt(t, scanFixtureClient(), 150, 24)
	tab := func() int { return scanHistoryScreenOf(t, m).modal.ActiveTab }
	m = scanPress(t, m, testKeyTab())
	if tab() != 1 {
		t.Fatalf("tab = %d, want 1", tab())
	}
	m = scanPress(t, m, testKeyTab())
	if tab() != 0 {
		t.Fatalf("tab = %d after wrapping forward, want 0", tab())
	}
	m = scanPress(t, m, testKeyShiftTab())
	if tab() != 1 {
		t.Fatalf("tab = %d after wrapping back, want 1", tab())
	}
}

func TestScanHistoryOpensOnLeaksFromSecretFindings(t *testing.T) {
	t.Parallel()
	m := scanPress(t, scanModelAt(t, scanFixtureClient(), screenAdminSecretFindings, 150, 24), testKeyEnter())
	out := scanText(m)
	if !strings.Contains(out, "src/deploy/prod.env:14") || !strings.Contains(out, "DESCRIPTION") {
		t.Errorf("history did not open on Leaks:\n%s", out)
	}
}

func TestScanHistoryFindingCursorIsBoundedAndResets(t *testing.T) {
	t.Parallel()
	m := historyModelAt(t, scanFixtureClient(), 150, 24)
	cursor := func() int { return scanHistoryScreenOf(t, m).modal.FindingCursor }
	m = scanPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown())
	if cursor() != 2 {
		t.Fatalf("cursor = %d, want 2 (bounded by 3 findings)", cursor())
	}
	m = scanPress(t, m, testKeyUp(), testKeyUp(), testKeyUp())
	if cursor() != 0 {
		t.Fatalf("cursor = %d, want 0", cursor())
	}
	m = scanPress(t, m, testKeyDown(), testKeyTab())
	if cursor() != 0 {
		t.Fatalf("cursor = %d after a tab switch, want 0", cursor())
	}
	m = scanPress(t, m, testKeyShiftTab(), testKeyDown(), testKeyRight())
	if cursor() != 0 {
		t.Fatalf("cursor = %d after paging, want 0", cursor())
	}
}

func TestScanHistoryPagesBetweenRunsAndNeverMixesTheirData(t *testing.T) {
	t.Parallel()
	client := scanFixtureClient()
	m := historyModelAt(t, client, 150, 24)
	m = scanPress(t, m, testKeyRight())
	out := scanText(m)
	if !strings.Contains(out, "Run 2 of 2") || !strings.Contains(out, "CVE-2025-0001") || strings.Contains(out, "CVE-2026-1000") {
		t.Errorf("second run not shown on its own:\n%s", out)
	}
	if strings.Contains(out, "team/api:1.8.2") {
		t.Errorf("first run's panel stayed on screen:\n%s", out)
	}
	// Right at the end stays put; left comes back.
	m = scanPress(t, m, testKeyRight())
	if got := scanHistoryScreenOf(t, m).modal.Cursor; got != 1 {
		t.Fatalf("cursor = %d past the last run, want 1", got)
	}
	m = scanPress(t, m, testKeyLeft(), testKeyLeft())
	if got := scanHistoryScreenOf(t, m).modal.Cursor; got != 0 {
		t.Fatalf("cursor = %d before the first run, want 0", got)
	}
	// The secret findings read for each run uses that run's own digest.
	if want := "team/api@" + scanFixtureRun("team/api", "1.8.2", ports.ScanRunStatusCompleted, 0, 0, 0, 0).Digest; client.lastSecretScanFindingsQuery != want {
		t.Errorf("last secret read = %q, want %q", client.lastSecretScanFindingsQuery, want)
	}
}

func TestScanHistoryDropsAnAnswerForARunItMovedAwayFrom(t *testing.T) {
	t.Parallel()
	m := historyModelAt(t, scanFixtureClient(), 150, 24)
	m = scanPress(t, m, testKeyRight())
	stale := scanFixtureClient().scanRunDetails["run-team/api"]
	updated, _ := m.Update(adminScanRunDetailLoadedMsg{detail: stale})
	hist := scanHistoryScreenOf(t, updated.(Model))
	if hist.modal.Detail.Run.ID != "run-api-older" {
		t.Fatalf("detail run = %q, want the run on screen", hist.modal.Detail.Run.ID)
	}
	updated, _ = updated.(Model).Update(adminSecretScanFindingsLoadedMsg{repository: "team/api", digest: "sha256:other", findings: []ports.SecretFinding{{RuleID: "x"}}})
	if got := len(scanHistoryScreenOf(t, updated.(Model)).modal.Secrets); got != 0 {
		t.Fatalf("secrets = %d, want a foreign answer dropped", got)
	}
}

func TestScanHistoryEnterOpensTheSelectedAdvisory(t *testing.T) {
	original := openURLInBrowser
	defer func() { openURLInBrowser = original }()
	var opened []string
	openURLInBrowser = func(rawURL string) error { opened = append(opened, rawURL); return nil }

	m := historyModelAt(t, scanFixtureClient(), 150, 24)
	m = scanPress(t, m, testKeyEnter())
	m = scanPress(t, m, testKeyDown(), testKeyEnter())
	want := []string{"https://example.test/CVE-2026-1000", "https://nvd.nist.gov/vuln/detail/CVE-2026-1001"}
	if fmt.Sprint(opened) != fmt.Sprint(want) {
		t.Fatalf("opened %v, want %v", opened, want)
	}
	// Leaks have no advisory.
	opened = nil
	m = scanPress(t, m, testKeyTab(), testKeyEnter())
	if len(opened) != 0 {
		t.Fatalf("Enter on Leaks opened %v", opened)
	}
}

func TestScanHistoryShowsACopyableLinkWhenTheBrowserFails(t *testing.T) {
	original := openURLInBrowser
	defer func() { openURLInBrowser = original }()
	openURLInBrowser = func(string) error { return errors.New("no opener") }

	m := scanPress(t, historyModelAt(t, scanFixtureClient(), 150, 24), testKeyEnter())
	if out := scanText(m); !strings.Contains(out, "copy this link") {
		t.Errorf("no copyable link after a failed open:\n%s", out)
	}
}

func TestScanHistoryStates(t *testing.T) {
	t.Parallel()
	t.Run("no runs", func(t *testing.T) {
		t.Parallel()
		client := scanFixtureClient()
		client.scanRuns = nil
		out := scanText(historyModelAt(t, client, 150, 24))
		if !strings.Contains(out, "No scans recorded for this repository.") || !strings.Contains(out, "No runs") {
			t.Errorf("no empty state:\n%s", out)
		}
	})
	t.Run("run without findings", func(t *testing.T) {
		t.Parallel()
		client := scanFixtureClient()
		d := client.scanRunDetails["run-team/api"]
		d.Findings = nil
		client.scanRunDetails["run-team/api"] = d
		client.secretScanFindings = nil
		m := historyModelAt(t, client, 150, 24)
		if out := scanText(m); !strings.Contains(out, "No findings recorded for this run.") {
			t.Errorf("no empty vulnerabilities state:\n%s", out)
		}
		if out := scanText(scanPress(t, m, testKeyTab())); !strings.Contains(out, "No secret findings recorded for this run.") {
			t.Errorf("no empty leaks state:\n%s", out)
		}
	})
	t.Run("loading", func(t *testing.T) {
		t.Parallel()
		m := scanModelAt(t, scanFixtureClient(), screenAdminScanRuns, 150, 24)
		updated, _ := m.Update(openScanHistoryMsg{repository: "team/api", returnTo: screenAdminScanRuns})
		out := scanText(updated.(Model))
		if !strings.Contains(out, "Reading scan history…") || !strings.Contains(out, "Reading…") {
			t.Errorf("no loading state:\n%s", out)
		}
	})
	t.Run("error", func(t *testing.T) {
		t.Parallel()
		m := scanModelAt(t, scanFixtureClient(), screenAdminScanRuns, 150, 24)
		updated, _ := m.Update(openScanHistoryMsg{repository: "team/api", returnTo: screenAdminScanRuns})
		updated, _ = updated.(Model).Update(adminScanHistoryLoadedMsg{repository: "team/api", err: errors.New("boom")})
		out := scanText(updated.(Model))
		if !strings.Contains(out, "Could not read scan history: boom") || !strings.Contains(out, "Unavailable") {
			t.Errorf("no error state:\n%s", out)
		}
		if hist := scanHistoryScreenOf(t, updated.(Model)); hist.modal.Loading {
			t.Error("still loading after an error")
		}
	})
	t.Run("in progress run", func(t *testing.T) {
		t.Parallel()
		client := scanFixtureClient()
		runs := client.scanRuns
		running := scanFixtureRun("team/api", "2.0.0", ports.ScanRunStatusRunning, -time.Minute, 0, 0, 0)
		running.FinishedAt = nil
		client.scanRuns = append([]ports.ScanRun{running}, runs...)
		client.scanRunDetails[running.ID] = ports.ScanRunDetail{Run: running}
		out := scanText(historyModelAt(t, client, 150, 24))
		if !strings.Contains(out, "(in progress)") {
			t.Errorf("an unfinished run is not marked:\n%s", out)
		}
	})
}

func TestScanHistoryBreadcrumbByOpener(t *testing.T) {
	t.Parallel()
	m := scanModelAt(t, scanFixtureClient(), screenAdminScanRuns, 150, 24)
	updated, cmd := m.Update(openScanHistoryMsg{repository: "team/api", returnTo: screenSecurityTrivyRepos, activeTab: scanHistoryTabIndexVulnerabilities})
	m = scanRun(t, updated.(Model), cmd)
	first := strings.Split(scanText(m), "\n")[0]
	if !strings.Contains(first, "Admin › Security › Image scanning › team/api") {
		t.Errorf("breadcrumb = %q", first)
	}
	if !strings.Contains(scanText(m), "▸ Image scanning") {
		t.Errorf("sidebar does not highlight Image scanning")
	}
}

func TestScanHistoryFitsEverySizeInEveryState(t *testing.T) {
	t.Parallel()
	many := scanFixtureClient()
	var findings []ports.ScanRunFinding
	var secrets []ports.SecretFinding
	for i := range 60 {
		findings = append(findings, ports.ScanRunFinding{Severity: "HIGH", VulnerabilityID: fmt.Sprintf("CVE-2026-%04d-with-a-long-identifier", i),
			PackageName: "a-package-with-a-very-long-name-ñandú", InstalledVersion: "1.2.3-long-version-string", FixedVersion: "9.9.9-long-version-string"})
		secrets = append(secrets, ports.SecretFinding{RuleID: "a-rule-with-a-very-long-identifier", Path: strings.Repeat("deep/", 12) + "file.env", StartLine: i + 1, EndLine: i + 3,
			Description: strings.Repeat("long description ", 8), Tags: []string{"a", "b"}})
	}
	d := many.scanRunDetails["run-team/api"]
	d.Findings = findings
	many.scanRunDetails["run-team/api"] = d
	api := d.Run
	many.secretScanFindings = map[string]ports.SecretScanRunDetail{"team/api@" + api.Digest: {Findings: secrets}}

	for _, tab := range []string{"vulnerabilities", "leaks"} {
		for _, size := range scanViewSizes {
			t.Run(fmt.Sprintf("%s_%dx%d", tab, size.w, size.h), func(t *testing.T) {
				t.Parallel()
				m := historyModelAt(t, many, size.w, size.h)
				if tab == "leaks" {
					m = scanPress(t, m, testKeyTab())
				}
				scanAssertFits(t, tab, scanText(m), size.w, size.h)
				for range 70 {
					m = scanPress(t, m, testKeyDown())
				}
				out := scanText(m)
				scanAssertFits(t, tab+" at the end", out, size.w, size.h)
				if !strings.Contains(out, "▸") {
					t.Errorf("the selection left the screen:\n%s", out)
				}
			})
		}
	}
	t.Run("empty and error", func(t *testing.T) {
		t.Parallel()
		for _, size := range scanViewSizes {
			client := scanFixtureClient()
			client.scanRuns = nil
			scanAssertFits(t, "empty", scanText(historyModelAt(t, client, size.w, size.h)), size.w, size.h)
			m := scanModelAt(t, scanFixtureClient(), screenAdminScanRuns, size.w, size.h)
			updated, _ := m.Update(openScanHistoryMsg{repository: "team/api", returnTo: screenAdminScanRuns})
			updated, _ = updated.(Model).Update(adminScanHistoryLoadedMsg{repository: "team/api", err: errors.New(strings.Repeat("long error ", 30))})
			scanAssertFits(t, "error", scanText(updated.(Model)), size.w, size.h)
		}
	})
}

func TestScanHistoryViewGoldens(t *testing.T) {
	for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
		for _, tc := range []struct {
			name string
			keys []keyMsg
		}{
			{"scan_history_vulnerabilities", nil},
			{"scan_history_leaks", []keyMsg{testKeyTab()}},
			{"scan_history_second_run", []keyMsg{testKeyRight(), testKeyDown()}},
		} {
			name := fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h)
			t.Run(name, func(t *testing.T) {
				m := scanPress(t, historyModelAt(t, scanFixtureClient(), size.w, size.h), tc.keys...)
				assertScanGolden(t, name, scanText(m))
			})
		}
	}
}
