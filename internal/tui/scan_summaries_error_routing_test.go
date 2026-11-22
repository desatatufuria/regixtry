package tui

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// summariesProbe records the scan summary reads that reach a mounted screen.
type summariesProbe struct {
	got []adminRepositoryScanSummariesLoadedMsg
}

func (p summariesProbe) ID() screen                             { return screenAdminScanRuns }
func (p summariesProbe) Keys() screenKeys                       { return screenKeys{} }
func (p summariesProbe) Init(screenEnv) tea.Cmd                 { return nil }
func (p summariesProbe) View(adminTheme, screenEnv) screenFrame { return screenFrame{} }
func (p summariesProbe) Update(_ screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	if loaded, ok := msg.(adminRepositoryScanSummariesLoadedMsg); ok {
		p.got = append(p.got, loaded)
	}
	return p, nil, false
}

func TestScanSummaryLoadErrorReachesTheScreensAndTheStatus(t *testing.T) {
	t.Parallel()
	m := newAdminReadyModel(t, &fakeAdminClient{})
	m.adminAuth = adminAuthStateAuthenticated
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: charFixedTime.Add(time.Hour)}
	m.now = func() time.Time { return charFixedTime }
	m.adminScreens[slotScanRuns] = summariesProbe{}

	updated, _ := m.Update(adminRepositoryScanSummariesLoadedMsg{err: errors.New("boom")})
	got := updated.(Model)

	probe := got.adminScreens[slotScanRuns].(summariesProbe)
	if len(probe.got) != 1 || probe.got[0].err == nil || probe.got[0].err.Error() != "boom" {
		t.Fatalf("screen received %v, want the failed read", probe.got)
	}
	if got.status != "boom" {
		t.Fatalf("status = %q, want the error kept as before", got.status)
	}
}

func TestScanSummaryLoadExpiredSessionStillLogsOut(t *testing.T) {
	t.Parallel()
	m := newAdminReadyModel(t, &fakeAdminClient{})
	m.adminAuth = adminAuthStateAuthenticated
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: charFixedTime.Add(time.Hour)}
	m.adminScreens[slotScanRuns] = summariesProbe{}

	updated, _ := m.Update(adminRepositoryScanSummariesLoadedMsg{err: NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)})
	if got := updated.(Model); got.adminAuth == adminAuthStateAuthenticated {
		t.Fatal("an expired session did not log the operator out")
	}
}
