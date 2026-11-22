package tui

import (
	"regexp"
	"strings"
	"testing"
	"unicode"

	appregixtry "regixtry/internal/app/regixtry"
	"regixtry/internal/ports"
)

// hostile carries every class of terminal control an untrusted pusher can put
// into a registry-controlled string.
const hostile = "X\x1b[2J\x1b]0;pwned\x07\x1b]52;c;aGk=\x07\x1b]8;;http://evil\x07L\x1b]8;;\x07\rOVER\b\b\u009b2J\x00\u202eRTL\u200bZW\x7f"

var ownSGR = regexp.MustCompile("\x1b\\[[0-9;:]*m")

// assertNoTerminalControls fails when frame holds anything that would act on a
// terminal beyond the renderer's own SGR styling.
func assertNoTerminalControls(t *testing.T, where, frame string) {
	t.Helper()
	text := ownSGR.ReplaceAllString(frame, "")
	for _, r := range text {
		if r == '\n' {
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			t.Fatalf("%s: frame carries control rune %U", where, r)
		}
	}
	for _, injected := range []string{"\x1b[31m", "\x1b[8m"} {
		if strings.Contains(frame, injected) && strings.Contains(hostile, injected) {
			t.Fatalf("%s: injected styling survived", where)
		}
	}
}

func hostileScanClient() *fakeAdminClient {
	run := scanFixtureRun(hostile+"/api", hostile, ports.ScanRunStatusCompleted, -3600e9, 38e9, 2, 5)
	run.Error = hostile
	return &fakeAdminClient{
		users:                   []ports.AdminUser{{ID: "u-1", Username: hostile, Enabled: true}},
		robots:                  []ports.AdminRobot{{ID: "r-1", Username: hostile, Repository: hostile, Enabled: true}},
		repositoryScanSummaries: []ports.RepositoryScanSummary{{Run: run, RunCount: 1}},
		scanRuns:                []ports.ScanRun{run},
		scanRunDetails: map[string]ports.ScanRunDetail{"run-" + hostile + "/api": {Run: run, Findings: []ports.ScanRunFinding{
			{Severity: "CRITICAL", VulnerabilityID: hostile, PackageName: hostile, InstalledVersion: hostile, FixedVersion: hostile, PrimaryURL: hostile},
		}}},
		secretScanFindings: map[string]ports.SecretScanRunDetail{
			hostile + "/api@" + run.Digest: {Findings: []ports.SecretFinding{
				{RuleID: hostile, Description: hostile, Path: hostile, StartLine: 1, EndLine: 1, Tags: []string{hostile}},
			}},
		},
	}
}

func TestEveryScreenNeutralisesRegistryControlledText(t *testing.T) {
	screens := []screen{
		screenAdminOverview, screenAdminScanRuns, screenAdminSecretFindings,
		screenAdminUsers, screenAdminRobots,
	}
	for _, id := range screens {
		t.Run(string(id), func(t *testing.T) {
			m := scanModelAt(t, hostileScanClient(), id, 150, 30)
			frame := m.View().Content
			if !strings.Contains(frame, "pwned") && !strings.Contains(frame, "OVER") && id != screenAdminSecretFindings {
				t.Logf("%s: hostile text not visible in this frame (still checked)", id)
			}
			assertNoTerminalControls(t, string(id), frame)
			// Drill into the first row where the screen supports it.
			m = scanPress(t, m, testKeyEnter())
			assertNoTerminalControls(t, string(id)+" after enter", m.View().Content)
		})
	}
}

func TestRepositoriesScreenNeutralisesHostileNames(t *testing.T) {
	m := newRepositoriesReadyModel(t, &fakeQueryService{
		repositorySummaries: []appregixtry.RepositorySummary{{Name: hostile + "/img"}},
	})
	m = resizeModel(t, m, 150, 30)
	assertNoTerminalControls(t, "repositories", m.View().Content)
}
