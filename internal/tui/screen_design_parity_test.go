package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

func TestGrantRepoPreviewSaysWhatTheChosenRoleWouldDo(t *testing.T) {
	t.Parallel()
	alice := grantPrincipal{ID: "u-1", Name: "alice"}
	grants := []ports.AdminRepoGrant{
		{Repository: regixtrydomain.MustParseRepositoryRef("team/docs"), Role: domainauth.RepoRoleReader},
		{Repository: regixtrydomain.MustParseRepositoryRef("team/api"), Role: domainauth.RepoRoleWriter},
	}
	s := grantAccessScreen{role: domainauth.RepoRoleWriter, grants: map[string]grantsEntry{"u-1": {loaded: true, items: grants}}}
	for repo, want := range map[string]string{
		"team/docs":  "alice is Reader, will become Writer",
		"team/api":   "alice is Writer",
		"team/infra": "no access yet",
	} {
		if got := s.repoPreview(alice, repo); got != want {
			t.Errorf("repoPreview(%s) = %q, want %q", repo, got, want)
		}
	}
	for name, entry := range map[string]grantsEntry{
		"not loaded": {items: grants},
		"errored":    {loaded: true, err: "boom"},
		"missing":    {},
	} {
		s.grants = map[string]grantsEntry{"u-1": entry}
		if got := s.repoPreview(alice, "team/infra"); got != "" {
			t.Errorf("%s: repoPreview = %q, want nothing claimed", name, got)
		}
	}
}

func TestGrantScreenFollowsTheBoardLabelsButtonsAndFooter(t *testing.T) {
	t.Parallel()
	rendered := grantRender(grantStateFiltered(t, 150, 24))
	for _, want := range []string{
		"1 Who", "2 Which repositories", "3 Role and review",
		"Search users and robots", "Filter, or type a new name",
		"Grant access", "enter", "Cancel", "esc",
		"tab next step   ↑↓ move   space toggle repo   ctrl+a all shown   1 2 3 pick role   enter grant   esc cancel",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("missing %q in:\n%s", want, rendered)
		}
	}
}

func TestUsersToggleDialogsFollowTheSharedPattern(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		id, title, consequence string
	}{
		{"u-4", "Disable user?", "Grants are kept. Enable carol again to restore access."},
		{"u-5", "Enable user?", "dave can sign in again."},
	} {
		s, env := usersOpen(t, usersTestClient(), tt.id)
		s, _ = usersPress(t, s, env, testKeyRunes("d"))
		overlay := ansi.Strip(s.View(newAdminTheme(), env).Overlay)
		for _, want := range []string{tt.title, tt.consequence, "Confirm", "y / enter", "Cancel", "esc"} {
			if !strings.Contains(overlay, want) {
				t.Fatalf("%s: missing %q in:\n%s", tt.id, want, overlay)
			}
		}
		if strings.Count(overlay, "created 3 Mar 2026") != 1 {
			t.Fatalf("%s: the user card must appear once:\n%s", tt.id, overlay)
		}
		if strings.Contains(overlay, "tokens restored") || strings.Contains(overlay, "active tokens") {
			t.Fatalf("%s: claims something the data does not prove:\n%s", tt.id, overlay)
		}
		base := ansi.Strip(s.View(newAdminTheme(), env).Body)
		if strings.Contains(base, "USERNAME") || strings.Contains(base, "ACTIONS") {
			t.Fatalf("%s: the dialog must sit on a blank panel, got table or detail under it:\n%s", tt.id, base)
		}
		if got := shortHelpView(newAdminTheme(), s.Keys()); !strings.Contains(got, "y / enter") || !strings.Contains(got, "cancel") {
			t.Fatalf("footer = %q", got)
		}
	}
}

func TestEditUserDangerButtonIsBorderedAndPinnedToTheBottom(t *testing.T) {
	t.Parallel()
	for _, size := range usersViewSizes {
		client := editTestClient()
		env := usersTestEnv(client, size.w, size.h)
		s := newEditUserScreen("u-4", "carol")
		next, _ := settleScreen(s, env, s.Init(env))
		lines := strings.Split(ansi.Strip(next.(editUserScreen).View(newAdminTheme(), env).Body), "\n")
		row := -1
		for i, line := range lines {
			if strings.Contains(line, "Disable user  d") {
				row = i
			}
		}
		if row < 1 || !strings.Contains(lines[row-1], "╭") || !strings.Contains(lines[row+1], "╰") {
			t.Fatalf("%dx%d: the button is not bordered (row %d)", size.w, size.h, row)
		}
		// The panel closes right under the button and the footer rule follows.
		if !strings.Contains(lines[row+2], "╰") || !strings.HasPrefix(lines[row+3], "──") {
			t.Fatalf("%dx%d: the danger panel is not pinned to the bottom", size.w, size.h)
		}
	}
}

func TestCreateUserUsernameRuleStatesTheServersRule(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"letters, numbers, dot, dash and underscore", "starts and ends with a letter or number"} {
		if !strings.Contains(userUsernameRule, want) {
			t.Fatalf("rule %q lacks %q", userUsernameRule, want)
		}
	}
	if strings.Contains(userUsernameRule, "3 to 32") {
		t.Fatal("the server has no length rule for usernames")
	}
	out := ansi.Strip(renderCreate(newCreateUserScreen(), usersTestEnv(usersTestClient(), 200, 40)))
	if !strings.Contains(out, "✓ Username: letters, numbers, dot, dash") {
		t.Fatalf("the rule is not on screen:\n%s", out)
	}
}
