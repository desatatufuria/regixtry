package tui

import (
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

// The project screens have their own golden flag so regenerating them can
// never touch another screen's goldens.
var updateProjectsGoldens = flag.Bool("update-projects", false, "rewrite the project screens' golden files")

// projectsClient serves the Projects board's sample.
func projectsClient() *fakeAdminClient {
	return &fakeAdminClient{projectsAPI: fakeProjectsAPI{projects: projectsBoardSample()}}
}

// projectsAt builds a ready, signed-in Model of the given size.
func projectsAt(t *testing.T, client AdminClient, w, h int, repos ...string) Model {
	t.Helper()
	m := newAdminReadyModelWithCatalog(t, repos, client)
	m.now = func() time.Time { return fixedTestTime }
	m = resizeModel(t, m, w, h)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedTestTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	return m
}

// projectsModelAt mounts the Projects list (its first read delivered).
func projectsModelAt(t *testing.T, client AdminClient, w, h int, repos ...string) Model {
	t.Helper()
	m := projectsAt(t, client, w, h, repos...)
	updated, cmd := m.Update(navigateMsg{To: screenAdminProjects})
	return repoAccessRun(t, updated.(Model), cmd)
}

func projectsPress(t *testing.T, m Model, keys ...keyMsg) Model {
	t.Helper()
	return repoAccessPress(t, m, keys...)
}

func projectsScreenOf(t *testing.T, m Model) adminProjectsScreen {
	t.Helper()
	s, ok := m.adminScreens[slotProjects].(adminProjectsScreen)
	if !ok {
		t.Fatalf("slot holds %T, want adminProjectsScreen", m.adminScreens[slotProjects])
	}
	return s
}

func projectsRender(m Model) string { return ansi.Strip(m.View().Content) }

func projectNames(s adminProjectsScreen) []string {
	var names []string
	for _, p := range s.sorted() {
		names = append(names, p.Name)
	}
	return names
}

// flattenView turns a rendered screen into one line of words: box borders and
// the line breaks of wrapped text no longer matter when a long sentence is
// looked for.
func flattenView(out string) string {
	lines := strings.Split(out, "\n")
	for i := 1; i < len(lines)-1; i++ {
		// The sidebar sits on the same rows as the content: leave it out.
		if runes := []rune(lines[i]); len(runes) > shellSidebarWidth+1 {
			lines[i] = string(runes[shellSidebarWidth+1:])
		}
	}
	out = strings.Join(lines, "\n")
	return strings.Join(strings.Fields(strings.NewReplacer("│", " ", "╭", " ", "╮", " ", "╰", " ", "╯", " ", "─", " ").Replace(out)), " ")
}

func requireContains(t *testing.T, out string, texts ...string) {
	t.Helper()
	flat := flattenView(out)
	for _, text := range texts {
		if !strings.Contains(flat, strings.Join(strings.Fields(text), " ")) {
			t.Errorf("view lacks %q\n%s", text, out)
		}
	}
}

func requireAbsent(t *testing.T, out string, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if strings.Contains(out, text) {
			t.Errorf("view must not contain %q\n%s", text, out)
		}
	}
}

func TestProjectsListShowsTheBoard(t *testing.T) {
	t.Parallel()
	client := projectsClient()
	m := projectsModelAt(t, client, 150, 24)
	if m.screen != screenAdminProjects {
		t.Fatalf("screen = %q", m.screen)
	}
	if got := strings.Join(projectNames(projectsScreenOf(t, m)), ","); got != "gosse,metalab,odabank,sandbox" {
		t.Fatalf("rows = %s", got)
	}
	if client.projectsAPI.listCalls != 1 {
		t.Fatalf("project reads = %d, want 1", client.projectsAPI.listCalls)
	}
	out := projectsRender(m)
	requireContains(t, out, "Admin › Access › Projects", "Projects", "4 projects, sorted by name",
		"PROJECT ▲", "REPOSITORIES", "TAGS", "LAST PUSH", "▸ gosse", "metalab", "odabank", "sandbox", "never", "19 Nov",
		"A project is the first path segment of a repository name. enter opens its members.",
		"d deletes a project only when it has no repositories.",
		"↑↓ move", "enter open", "n new project", "d delete project", "s sort", "esc back", "q quit")
	requireAbsent(t, out, "MEMBERS")
	if !strings.Contains(out, "Projects") || !strings.Contains(out, "Repository access") {
		t.Errorf("the sidebar must list Projects after Repository access:\n%s", out)
	}
}

func TestProjectsSortCyclesAndKeepsTheSelection(t *testing.T) {
	t.Parallel()
	m := projectsModelAt(t, projectsClient(), 150, 24)
	m = projectsPress(t, m, testKeyDown(), testKeyDown()) // odabank
	cases := []struct {
		label string
		order string
		right string
		arrow string
	}{
		{"last push", "metalab,odabank,gosse,sandbox", "sorted by last push", "LAST PUSH ▼"},
		{"repositories", "metalab,odabank,gosse,sandbox", "sorted by repositories", "REPOSITORIES ▼"},
		{"name", "gosse,metalab,odabank,sandbox", "sorted by name", "PROJECT ▲"},
	}
	for _, tc := range cases {
		m = projectsPress(t, m, testKeyRunes("s"))
		s := projectsScreenOf(t, m)
		if got := strings.Join(projectNames(s), ","); got != tc.order {
			t.Errorf("sort %s: order = %s, want %s", tc.label, got, tc.order)
		}
		if s.selectedName != "odabank" {
			t.Errorf("sort %s: selection = %q, want odabank kept", tc.label, s.selectedName)
		}
		requireContains(t, projectsRender(m), tc.right, tc.arrow)
	}
}

func TestProjectsLoadingErrorForbiddenAndEmptyStates(t *testing.T) {
	t.Parallel()
	t.Run("loading", func(t *testing.T) {
		t.Parallel()
		m := projectsAt(t, projectsClient(), 150, 24)
		updated, _ := m.Update(navigateMsg{To: screenAdminProjects})
		out := projectsRender(updated.(Model))
		requireContains(t, out, "Loading projects…", "esc back")
		requireAbsent(t, out, "enter open", "d delete project")
	})
	t.Run("error with retry keeps working", func(t *testing.T) {
		t.Parallel()
		client := projectsClient()
		client.projectsAPI.listErr = fmt.Errorf("connection refused")
		m := projectsModelAt(t, client, 150, 24)
		out := projectsRender(m)
		requireContains(t, out, "Could not load projects: connection refused.", "Press r to retry.", "r retry")
		client.projectsAPI.listErr = nil
		m = projectsPress(t, m, testKeyRunes("r"))
		requireContains(t, projectsRender(m), "odabank", "4 projects")
		if client.projectsAPI.listCalls != 2 {
			t.Fatalf("reads = %d, want the first and one retry", client.projectsAPI.listCalls)
		}
	})
	t.Run("a failed reload keeps the list", func(t *testing.T) {
		t.Parallel()
		client := projectsClient()
		m := projectsModelAt(t, client, 150, 24)
		s := projectsScreenOf(t, m)
		next, _, _ := s.Update(screenEnv{}, adminProjectsLoadedMsg{gen: s.gen, seq: projectLoadSeq.Add(1), err: fmt.Errorf("boom")})
		m.adminScreens[slotProjects] = next
		out := projectsRender(m)
		requireContains(t, out, "odabank", "Could not load projects: boom. Press r to retry.", "r retry")
	})
	t.Run("not allowed", func(t *testing.T) {
		t.Parallel()
		client := projectsClient()
		client.projectsAPI.listErr = adminStatusError{status: 403, message: "read admin resource: forbidden", detail: "forbidden"}
		m := projectsModelAt(t, client, 150, 24)
		out := projectsRender(m)
		requireContains(t, out, "Only global administrators manage projects.", "esc back")
		requireAbsent(t, out, "n new project", "d delete project")
		m = projectsPress(t, m, testKeyRunes("n"))
		if projectsScreenOf(t, m).dialog.open {
			t.Fatal("n opened the dialog for a session that may not create projects")
		}
	})
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		m := projectsModelAt(t, &fakeAdminClient{}, 150, 24)
		out := projectsRender(m)
		requireContains(t, out, "0 projects", "No projects yet",
			"A project must exist before anyone can push to it. Press n to create the first one, for example odabank.",
			"Repositories pushed as odabank/api then belong to project odabank.",
			"n new project", "esc back", "q quit")
		requireAbsent(t, out, "enter open", "d delete project", "s sort")
		m = projectsPress(t, m, testKeyRunes("d"), testKeyRunes("s"), testKeyEnter())
		if s := projectsScreenOf(t, m); s.confirm.Active() || s.refusal.active() || m.screen != screenAdminProjects {
			t.Fatal("d, s or enter did something on an empty list")
		}
	})
}

func projectCreateKeys(name string) []keyMsg {
	keys := []keyMsg{testKeyRunes("n")}
	for _, r := range name {
		keys = append(keys, testKeyRunes(string(r)))
	}
	return keys
}

func TestProjectsCreateDialogValidatesLive(t *testing.T) {
	t.Parallel()
	cases := []struct {
		typed   string
		message string
		valid   bool
	}{
		{"", "Enter a project name.", false},
		{strings.Repeat("a", 65), "At most 64 characters (65 now).", false},
		{"oda/bank", "A project name has no slash. Repositories go inside the project.", false},
		{"Odabank", "Use lowercase letters, digits and . _ - only; start and end with a letter or digit.", false},
		{"blobs", "blobs is reserved by the registry API. Pick another name.", false},
		{"odabank", "✓ Valid name.", true},
	}
	for _, tc := range cases {
		t.Run(tc.typed, func(t *testing.T) {
			t.Parallel()
			client := projectsClient()
			m := projectsModelAt(t, client, 150, 40)
			m = projectsPress(t, m, projectCreateKeys(tc.typed)...)
			out := projectsRender(m)
			requireContains(t, out, "New project", "Project name", tc.message, "Create project", "Cancel", "esc")
			if tc.valid {
				requireContains(t, out, "new project, 0 repositories", "Creates project odabank. Repositories pushed as odabank/... belong to it.")
			} else {
				requireContains(t, out, "(no valid name yet)", "type a lowercase name", "Nothing is created until the name is valid.")
			}
			m = projectsPress(t, m, testKeyEnter())
			if created := len(client.projectsAPI.createCalls) == 1; created != tc.valid {
				t.Fatalf("Enter created = %v (%v), want %v", created, client.projectsAPI.createCalls, tc.valid)
			}
		})
	}
}

func TestProjectsCreateSendsTheNameOnceAndSelectsIt(t *testing.T) {
	t.Parallel()
	client := projectsClient()
	m := projectsModelAt(t, client, 150, 24)
	m = projectsPress(t, m, projectCreateKeys("zeta")...)
	// Two Enters before the result arrives: the second is ignored.
	updated, cmd := m.Update(testKeyEnter())
	m = updated.(Model)
	updated2, cmd2 := m.Update(testKeyEnter())
	m = updated2.(Model)
	if cmd2 != nil {
		t.Fatal("a second Enter while the create is in flight dispatched a command")
	}
	m = repoAccessRun(t, m, cmd)
	if fmt.Sprint(client.projectsAPI.createCalls) != "[zeta]" {
		t.Fatalf("create calls = %v, want exactly one for zeta", client.projectsAPI.createCalls)
	}
	s := projectsScreenOf(t, m)
	if s.dialog.open || s.selectedName != "zeta" || s.saving {
		t.Fatalf("dialog open=%v selected=%q saving=%v, want a closed dialog with zeta selected", s.dialog.open, s.selectedName, s.saving)
	}
	requireContains(t, projectsRender(m), "Created project zeta.", "5 projects", "▸ zeta")
	if client.projectsAPI.listCalls != 2 {
		t.Fatalf("project reads = %d, want the open and one reload", client.projectsAPI.listCalls)
	}
}

func TestProjectsCreateErrorsKeepTheDialogOpen(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"exists", adminStatusError{status: 409, message: `mutate admin resource: project "odabank" already exists`, detail: `project "odabank" already exists`}, "Project odabank already exists."},
		{"failure", fmt.Errorf("connection reset"), "Could not create project: connection reset."},
		{"unprocessable", adminStatusError{status: 422, message: "mutate admin resource: bad", detail: "bad name."}, "Could not create project: bad name."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := projectsClient()
			client.projectsAPI.createErr = tc.err
			m := projectsModelAt(t, client, 150, 40)
			reads := client.projectsAPI.listCalls
			m = projectsPress(t, m, projectCreateKeys("odabank")...)
			m = projectsPress(t, m, testKeyEnter())
			s := projectsScreenOf(t, m)
			if !s.dialog.open || s.saving || s.dialog.name != "odabank" {
				t.Fatalf("dialog open=%v saving=%v name=%q, want it open with the name kept", s.dialog.open, s.saving, s.dialog.name)
			}
			requireContains(t, projectsRender(m), tc.want)
			if client.projectsAPI.listCalls != reads {
				t.Fatal("a failed create reloaded the list")
			}
			// Typing clears the stale error and Esc closes without another call.
			m = projectsPress(t, m, testKeyBackspace(), testKeyEsc())
			if projectsScreenOf(t, m).dialog.open || len(client.projectsAPI.createCalls) != 1 {
				t.Fatal("Esc did not close the dialog, or a call was repeated")
			}
		})
	}
}

func TestProjectsEscClosesTheCreateDialogWithoutACall(t *testing.T) {
	t.Parallel()
	client := projectsClient()
	m := projectsModelAt(t, client, 150, 24)
	m = projectsPress(t, m, projectCreateKeys("odabank")...)
	m = projectsPress(t, m, testKeyEsc())
	if projectsScreenOf(t, m).dialog.open || len(client.projectsAPI.createCalls) != 0 || m.screen != screenAdminProjects {
		t.Fatalf("open=%v calls=%v screen=%s", projectsScreenOf(t, m).dialog.open, client.projectsAPI.createCalls, m.screen)
	}
}

func TestProjectsTypedLettersInTheDialogAreNotCommands(t *testing.T) {
	t.Parallel()
	client := projectsClient()
	m := projectsModelAt(t, client, 150, 24)
	// q, d, s and n are text while the dialog is open.
	m = projectsPress(t, m, projectCreateKeys("qdsn")...)
	s := projectsScreenOf(t, m)
	if !s.dialog.open || s.dialog.name != "qdsn" || m.screen != screenAdminProjects {
		t.Fatalf("dialog open=%v name=%q screen=%s", s.dialog.open, s.dialog.name, m.screen)
	}
}

func TestProjectsDeleteEmptyProjectConfirmsThenDeletesExactlyThat(t *testing.T) {
	t.Parallel()
	client := projectsClient()
	m := projectsModelAt(t, client, 150, 40)
	m = projectsPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyRunes("d")) // sandbox
	out := projectsRender(m)
	requireContains(t, out, "Delete project sandbox?",
		"The project is removed together with its member grants. The project must be empty.",
		"sandbox", "0 repositories, 0 tags",
		"Removes the project and all its member grants. The project must be empty. Cannot be undone.",
		"Delete", "y / enter", "Cancel", "esc")
	if len(client.projectsAPI.deleteCalls) != 0 {
		t.Fatal("d deleted without confirmation")
	}
	updated, cmd := m.Update(testKeyEnter())
	m = updated.(Model)
	if _, cmd2 := m.Update(testKeyRunes("y")); cmd2 != nil {
		t.Fatal("a second confirm key while the delete is in flight dispatched a command")
	}
	m = repoAccessRun(t, m, cmd)
	if fmt.Sprint(client.projectsAPI.deleteCalls) != "[sandbox]" {
		t.Fatalf("delete calls = %v", client.projectsAPI.deleteCalls)
	}
	s := projectsScreenOf(t, m)
	if got := strings.Join(projectNames(s), ","); got != "gosse,metalab,odabank" || s.confirm.Active() || s.deleting {
		t.Fatalf("after delete: rows %s confirm=%v deleting=%v", got, s.confirm.Active(), s.deleting)
	}
	requireContains(t, projectsRender(m), "Deleted project sandbox.")
}

func TestProjectsDeleteEscCancelsWithoutACall(t *testing.T) {
	t.Parallel()
	client := projectsClient()
	m := projectsModelAt(t, client, 150, 24)
	m = projectsPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyRunes("d"), testKeyEsc())
	if projectsScreenOf(t, m).confirm.Active() || len(client.projectsAPI.deleteCalls) != 0 {
		t.Fatal("Esc did not cancel, or a delete was sent")
	}
}

func TestProjectsDeleteNonEmptyProjectIsRefusedWithoutACall(t *testing.T) {
	t.Parallel()
	client := projectsClient()
	m := projectsModelAt(t, client, 150, 40)
	m = projectsPress(t, m, testKeyDown(), testKeyDown(), testKeyRunes("d")) // odabank
	out := projectsRender(m)
	requireContains(t, out, "Cannot delete project", `project "odabank" still has 3 repositories; delete or finish them first`,
		"3 repositories, 27 tags", "Delete repositories one by one in Console › Projects › odabank. Nothing was changed.", "Close", "esc / enter")
	requireAbsent(t, out, "y / enter")
	m = projectsPress(t, m, testKeyRunes("y")) // y must not delete anything
	if len(client.projectsAPI.deleteCalls) != 0 {
		t.Fatalf("a refusal sent a delete: %v", client.projectsAPI.deleteCalls)
	}
	for _, closeKey := range []keyMsg{testKeyEnter(), testKeyEsc()} {
		m2 := projectsPress(t, m, closeKey)
		if projectsScreenOf(t, m2).refusal.active() || m2.screen != screenAdminProjects {
			t.Fatalf("%v did not close the refusal", closeKey)
		}
	}
}

func TestProjectsDeleteOutcomesFromTheServer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		err        error
		wantText   []string
		wantRows   string
		wantReload bool
	}{
		{"deletion disabled", adminStatusError{status: 422, message: "mutate admin resource: project deletion is not enabled", detail: "project deletion is not enabled"},
			[]string{"Deletion is disabled", "This server does not allow deleting projects.", "Start the server with -delete-enabled to allow it. Nothing was changed."}, "gosse,metalab,odabank,sandbox", false},
		{"race, no longer empty", adminStatusError{status: 409, message: "x", detail: "x"},
			[]string{"Project sandbox is no longer empty. Nothing was deleted."}, "gosse,metalab,odabank,sandbox", true},
		{"already gone", adminNotFoundError{message: "x", detail: "project not found"},
			[]string{"Project sandbox no longer exists. The list was refreshed."}, "gosse,metalab,odabank,sandbox", true},
		{"other failure keeps the list", fmt.Errorf("connection reset"),
			[]string{"Could not delete project: connection reset."}, "gosse,metalab,odabank,sandbox", false},
		{"grants not purged", adminStatusError{status: 503, message: "x", detail: `project "sandbox" was deleted but its grants could not be purged; retry the delete`},
			[]string{`Could not delete project: project "sandbox" was deleted but its grants could not be purged; retry the delete.`}, "gosse,metalab,odabank,sandbox", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := projectsClient()
			m := projectsModelAt(t, client, 150, 40)
			m = projectsPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyRunes("d"))
			reads := client.projectsAPI.listCalls
			client.projectsAPI.deleteErr = tc.err
			m = projectsPress(t, m, testKeyEnter())
			s := projectsScreenOf(t, m)
			if s.confirm.Active() || s.deleting {
				t.Fatalf("confirm=%v deleting=%v after the result", s.confirm.Active(), s.deleting)
			}
			requireContains(t, projectsRender(m), tc.wantText...)
			if (client.projectsAPI.listCalls > reads) != tc.wantReload {
				t.Fatalf("reload = %v, want %v", client.projectsAPI.listCalls > reads, tc.wantReload)
			}
			if got := strings.Join(projectNames(s), ","); got != tc.wantRows {
				t.Fatalf("rows = %s, want %s", got, tc.wantRows)
			}
		})
	}
}

func TestProjectsStaleReloadCannotBringADeletedProjectBack(t *testing.T) {
	t.Parallel()
	client := projectsClient()
	m := projectsModelAt(t, client, 150, 24)
	s := projectsScreenOf(t, m)
	stale := adminProjectsLoadedMsg{gen: s.gen, seq: projectLoadSeq.Add(1), projects: projectsBoardSample()}
	m = projectsPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyRunes("d"), testKeyEnter())
	updated, _ := m.Update(stale) // read started before the delete lands after it
	m = updated.(Model)
	if got := strings.Join(projectNames(projectsScreenOf(t, m)), ","); got != "gosse,metalab,odabank" {
		t.Fatalf("rows = %s: a stale read brought sandbox back", got)
	}
}

// TestProjectsConfirmTargetIsTheProjectThatWasOpened reorders and replaces the
// list (reloads, new sort) while the dialog is open: Enter still deletes the
// project the dialog names.
func TestProjectsConfirmTargetIsTheProjectThatWasOpened(t *testing.T) {
	t.Parallel()
	for seed := range 25 {
		client := projectsClient()
		client.projectsAPI.projects = append(client.projectsAPI.projects, ports.ProjectSummary{Name: "empty-a"}, ports.ProjectSummary{Name: "empty-b"})
		m := projectsModelAt(t, client, 150, 40)
		target := []string{"sandbox", "empty-a", "empty-b"}[seed%3]
		for i := 0; projectsScreenOf(t, m).selectedName != target && i < 10; i++ {
			m = projectsPress(t, m, testKeyDown())
		}
		m = projectsPress(t, m, testKeyRunes("d"))
		s := projectsScreenOf(t, m)
		if !s.confirm.Active() || s.confirm.target != target {
			t.Fatalf("seed %d: confirm target = %q, want %q", seed, s.confirm.target, target)
		}
		// A reload that reorders and drops rows lands while the dialog is open.
		reordered := []ports.ProjectSummary{{Name: "zzz-new"}, {Name: "aaa-new"}}
		if seed%2 == 0 {
			reordered = append(reordered, projectsBoardSample()...)
		}
		updated, _ := m.Update(adminProjectsLoadedMsg{gen: s.gen, seq: projectLoadSeq.Add(1), projects: reordered})
		m = updated.(Model)
		m = projectsPress(t, m, testKeyEnter())
		if got := fmt.Sprint(client.projectsAPI.deleteCalls); got != "["+target+"]" {
			t.Fatalf("seed %d: delete calls = %s, want [%s] (what the dialog named)", seed, got, target)
		}
	}
}

func TestProjectsExpiredSessionRedirectsToLogin(t *testing.T) {
	t.Parallel()
	expired := NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	t.Run("load", func(t *testing.T) {
		t.Parallel()
		client := projectsClient()
		client.projectsAPI.listErr = expired
		m := projectsModelAt(t, client, 150, 24)
		if m.screen != screenAdminLogin {
			t.Fatalf("screen = %q, want the login", m.screen)
		}
	})
	t.Run("create", func(t *testing.T) {
		t.Parallel()
		client := projectsClient()
		m := projectsModelAt(t, client, 150, 24)
		client.projectsAPI.createErr = expired
		m = projectsPress(t, m, projectCreateKeys("zeta")...)
		m = projectsPress(t, m, testKeyEnter())
		if m.screen != screenAdminLogin {
			t.Fatalf("screen = %q, want the login", m.screen)
		}
	})
	t.Run("delete", func(t *testing.T) {
		t.Parallel()
		client := projectsClient()
		m := projectsModelAt(t, client, 150, 24)
		client.projectsAPI.deleteErr = expired
		m = projectsPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyRunes("d"), testKeyEnter())
		if m.screen != screenAdminLogin {
			t.Fatalf("screen = %q, want the login", m.screen)
		}
	})
}

func TestProjectsMessagesOfAnotherInstanceAreDropped(t *testing.T) {
	t.Parallel()
	client := projectsClient()
	m := projectsModelAt(t, client, 150, 24)
	s := projectsScreenOf(t, m)
	expired := NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	for _, msg := range []tea.Msg{
		adminProjectsLoadedMsg{gen: s.gen + 1, seq: projectLoadSeq.Add(1), err: expired},
		adminProjectCreatedMsg{gen: s.gen + 1, name: "zeta", err: expired},
		adminProjectDeletedMsg{gen: s.gen + 1, name: "sandbox", err: expired},
		adminProjectsLoadedMsg{gen: s.gen + 1, seq: projectLoadSeq.Add(1), projects: nil},
	} {
		updated, _ := m.Update(msg)
		m = updated.(Model)
		if m.screen != screenAdminProjects || len(projectsScreenOf(t, m).projects) != 4 {
			t.Fatalf("%T of another instance changed the screen (%s, %d rows)", msg, m.screen, len(projectsScreenOf(t, m).projects))
		}
	}
}

func TestProjectsEnterOpensMembersAndEscGoesBack(t *testing.T) {
	t.Parallel()
	m := projectsModelAt(t, projectsClient(), 150, 24)
	m = projectsPress(t, m, testKeyDown(), testKeyDown())
	s := projectsScreenOf(t, m)
	_, cmd, _ := s.Update(screenEnv{}, testKeyEnter())
	if open, ok := cmd().(openProjectMembersMsg); !ok || open.project != "odabank" {
		t.Fatalf("Enter produced %#v, want openProjectMembersMsg for odabank", cmd())
	}
	m = projectsPress(t, m, testKeyEsc())
	if m.screen != screenAdminOverview {
		t.Fatalf("Esc went to %q, want the Overview", m.screen)
	}
}

func assertProjectsGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "projects", name+".golden")
	if *updateProjectsGoldens {
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
		t.Fatalf("read golden %s (run with -update-projects): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("rendered view differs from %s (run with -update-projects to inspect)\n--- want\n%s\n--- got\n%s", path, strings.TrimRight(string(want), "\n"), strings.TrimRight(got, "\n"))
	}
}
