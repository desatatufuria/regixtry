package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

var fixedTestTime = time.Date(2026, time.March, 14, 9, 30, 0, 0, time.UTC)

// fixedAdminModel is a logged-in admin Model with a fixed catalog, clock and
// viewport; screens are then selected by setting model state directly.
func fixedAdminModel(t *testing.T, width, height int) Model {
	t.Helper()
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedTestTime.Add(24 * time.Hour)},
	}
	model := newAdminReadyModelWithCatalog(t, []string{"library/alpine", "team/demo", "team/backend", "ops/console"}, adminClient)
	model.now = func() time.Time { return fixedTestTime }
	updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	model = updated.(Model)
	model.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedTestTime.Add(24 * time.Hour)}
	return model
}

func resizeModel(t *testing.T, m Model, width, height int) Model {
	t.Helper()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(Model)
}
