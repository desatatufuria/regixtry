package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	appregixtry "regixtry/internal/app/regixtry"
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

var updateCharacterization = flag.Bool("update", false, "rewrite the characterization golden files")

var charFixedTime = time.Date(2026, time.March, 14, 9, 30, 0, 0, time.UTC)

// charAdminModel is a logged-in admin Model with a fixed catalog, clock and
// viewport; screens are then selected by setting model state directly.
func charAdminModel(t *testing.T, width, height int) Model {
	t.Helper()
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: charFixedTime.Add(24 * time.Hour)},
	}
	model := newAdminReadyModelWithCatalog(t, []string{"library/alpine", "team/demo", "team/backend", "ops/console"}, adminClient)
	model.now = func() time.Time { return charFixedTime }
	updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	model = updated.(Model)
	model.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: charFixedTime.Add(24 * time.Hour)}
	model.adminView.Users = []ports.AdminUser{
		{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true, CreatedAt: charFixedTime},
		{ID: "u-2", Username: "bob", Enabled: true, CreatedAt: charFixedTime},
		{ID: "u-3", Username: "carol", IsReadOnly: true, CreatedAt: charFixedTime},
	}
	model.adminView.SelectedUserID = "u-1"
	model.adminView.SelectedUsername = "alice"
	return model
}

func charConsoleService() *fakeQueryService {
	return &fakeQueryService{
		repositorySummaries: []appregixtry.RepositorySummary{
			{Name: "team/app", TagCount: 2, LastPushed: charFixedTime},
			{Name: "team/other", TagCount: 5, LastPushed: charFixedTime.Add(-48 * time.Hour)},
			{Name: "smoke/keyless-live-verify", TagCount: 1, LastPushed: charFixedTime.Add(-72 * time.Hour)},
			{Name: "govault-api", TagCount: 4, LastPushed: charFixedTime.Add(-96 * time.Hour)},
		},
		tagDetails: map[string][]appregixtry.TagDetails{
			"team/app": {
				{Name: "latest", CreatedAt: charFixedTime, SignatureState: appregixtry.SignatureStatusUnsigned, PushedBy: "alice"},
				{Name: "v1.2.0", CreatedAt: charFixedTime.Add(-24 * time.Hour), SignatureState: appregixtry.SignatureStatusUnsigned, PushedBy: "bob"},
			},
		},
	}
}

func charResize(t *testing.T, m Model, width, height int) Model {
	t.Helper()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(Model)
}

var characterizationScreens = []struct {
	name  string
	build func(t *testing.T, width, height int) Model
}{
	{"console_repositories", func(t *testing.T, w, h int) Model {
		return charResize(t, newRepositoriesReadyModel(t, charConsoleService()), w, h)
	}},
	{"console_tags", func(t *testing.T, w, h int) Model {
		return charResize(t, newTagsReadyModel(t, charConsoleService()), w, h)
	}},
	{"console_projects", func(t *testing.T, w, h int) Model {
		m := newRepositoriesReadyModel(t, charConsoleService())
		m = testSendKey(t, m, testKeyRunes("p"))
		if m.screen != screenProjects {
			t.Fatalf("screen = %q, want %q", m.screen, screenProjects)
		}
		return charResize(t, m, w, h)
	}},
	{"admin_users", func(t *testing.T, w, h int) Model {
		m := charAdminModel(t, w, h)
		m.screen = screenAdminUsers
		return m
	}},
	{"admin_create_user", func(t *testing.T, w, h int) Model {
		m := charAdminModel(t, w, h)
		m.screen = screenAdminCreateUser
		m.adminView.CreateUserForm.Username = "dave"
		m.adminView.CreateUserForm.Password = "hunter2"
		return m
	}},
	{"admin_create_token_form", func(t *testing.T, w, h int) Model {
		m := charAdminModel(t, w, h)
		m.screen = screenAdminCreateToken
		m.adminView.TokenForm = adminTokenForm{Name: "ci bot", TTLSeconds: "3600"}
		return m
	}},
	{"admin_grant_form_suggestions", func(t *testing.T, w, h int) Model {
		m := charAdminModel(t, w, h)
		m.screen = screenAdminAddGrant
		m.adminView.GrantForm = adminGrantForm{Repository: "team", Role: domainauth.RepoRoleWriter}
		return m
	}},
	{"admin_tokens_list", func(t *testing.T, w, h int) Model {
		m := charAdminModel(t, w, h)
		m.screen = screenAdminEditUserTokens
		m.adminView.AdminTokens = []ports.AdminToken{
			{ID: "t-1", UserID: "u-1", Name: "ci bot", Accessor: "acc-1111", CreatedAt: charFixedTime, ExpiresAt: charFixedTime.Add(30 * 24 * time.Hour)},
			{ID: "t-2", UserID: "u-1", Name: "laptop", Accessor: "acc-2222", CreatedAt: charFixedTime, ExpiresAt: charFixedTime.Add(7 * 24 * time.Hour)},
		}
		return m
	}},
	{"admin_confirm_prompt", func(t *testing.T, w, h int) Model {
		m := charAdminModel(t, w, h)
		m.screen = screenAdminUsers
		m.adminView.Confirm = newConfirmPrompt("Enable User", "Enable alice?", "enable", "", func(screenEnv) tea.Cmd { return nil })
		return m
	}},
	{"overlay_trivy_config_modal", func(t *testing.T, w, h int) Model {
		m := charAdminModel(t, w, h)
		m.screen = screenSecurityTrivy
		m.adminScreens[slotTrivyConfig] = trivyConfigScreen{
			loaded: true,
			cfg: trivyConfigModal{
				Open: true, ScheduleEnabled: true, Interval: "1h", Timeout: "30s",
				RegistryReachableURL: "https://registry.example.com", MaxConcurrency: "4",
			},
		}
		return m
	}},
	{"overlay_gitleaks_config_modal", func(t *testing.T, w, h int) Model {
		m := charAdminModel(t, w, h)
		m.screen = screenSecurityGitleaksConfig
		m.adminScreens[slotGitleaksConfig] = gitleaksConfigScreen{
			loaded: true,
			cfg:    gitleaksConfigModal{Open: true, Enabled: true, Timeout: "45s", MaxConcurrency: "2"},
		}
		return m
	}},
}

func TestRenderCharacterization(t *testing.T) {
	// The TUI refuses to render below 150x24, so the requested 80x24 and
	// 120x40 pin the too-small notice; real screens are pinned at 150x24 and 200x40.
	for _, size := range []struct{ width, height int }{{80, 24}, {120, 40}} {
		size := size
		name := fmt.Sprintf("too_small_%dx%d", size.width, size.height)
		t.Run(name, func(t *testing.T) {
			model := characterizationScreens[0].build(t, size.width, size.height)
			assertCharacterizationGolden(t, name, ansi.Strip(model.View()))
		})
	}

	sizes := []struct{ width, height int }{{150, 24}, {200, 40}}
	for _, screen := range characterizationScreens {
		for _, size := range sizes {
			screen, size := screen, size
			name := fmt.Sprintf("%s_%dx%d", screen.name, size.width, size.height)
			t.Run(name, func(t *testing.T) {
				model := screen.build(t, size.width, size.height)
				got := ansi.Strip(model.View())
				assertCharacterizationGolden(t, name, got)
			})
		}
	}
}

func assertCharacterizationGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "characterization", name+".golden")
	if *updateCharacterization {
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
		t.Fatalf("read golden %s (run with -update): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("rendered view differs from %s (run with -update to inspect)\n--- want\n%s\n--- got\n%s", path, strings.TrimRight(string(want), "\n"), strings.TrimRight(got, "\n"))
	}
}
