package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

var updateRobotsTokensGoldens = flag.Bool("update-robots-tokens", false, "rewrite the Robots, Create robot and Tokens golden files")

var accessSizes = []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}}

const accessSecret = "rgx_0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-_0123456789"

// accessState builds one state of one screen at one size.
type accessState struct {
	name  string
	build func(t *testing.T, w, h int) (adminScreen, screenEnv)
	// secret, when set, must be whole in the rendering.
	secret string
	// contains must all appear.
	contains []string
}

func robotsStateAt(mutate func(*accessClient), keys ...keyMsg) func(*testing.T, int, int) (adminScreen, screenEnv) {
	return func(t *testing.T, w, h int) (adminScreen, screenEnv) {
		client, _ := robotsFixture()
		if mutate != nil {
			mutate(client)
		}
		env := accessEnvSized(client, w, h)
		var s adminScreen = openRobotsScreen(t, client, env)
		s, _ = accessPress(t, s, env, keys...)
		return s, env
	}
}

func manyRobots(client *accessClient) {
	client.robots, client.tokens = nil, map[string][]ports.AdminToken{}
	for i := range 60 {
		id := fmt.Sprintf("r-%02d", i)
		client.robots = append(client.robots, ports.AdminRobot{
			ID: id, Username: fmt.Sprintf("robot-%02d-%s", i, strings.Repeat("ñ", 40)),
			Repository: "team/" + strings.Repeat("repo", 12), Role: domainauth.RepoRoleAdmin, Enabled: i%3 != 0,
		})
		client.tokens[id] = []ports.AdminToken{accessToken("t", "act_"+id, accessNow.Add(-day), accessNow.Add(accessDays(i+1)))}
	}
}

func accessDays(n int) time.Duration { return time.Duration(n) * day }

func downs(n int) []keyMsg {
	keys := make([]keyMsg, n)
	for i := range keys {
		keys[i] = testKeyDown()
	}
	return keys
}

func createStateAt(known []string, keys ...keyMsg) func(*testing.T, int, int) (adminScreen, screenEnv) {
	return func(t *testing.T, w, h int) (adminScreen, screenEnv) {
		client := newAccessClient()
		client.createRobot = ports.AdminCreatedRobot{
			Robot:  ports.AdminRobot{ID: "r-7", Username: "release-bot", Repository: "team/app-1", Role: domainauth.RepoRoleWriter},
			Secret: accessSecret, Accessor: "act_c07e1b52d9a4", ExpiresAt: accessNow.Add(30 * day),
		}
		env := accessEnvSized(client, w, h, known...)
		s, _ := accessPress(t, newCreateRobotScreen(), env, keys...)
		return s, env
	}
}

func tokensStateAt(mutate func(*accessClient), keys ...keyMsg) func(*testing.T, int, int) (adminScreen, screenEnv) {
	return func(t *testing.T, w, h int) (adminScreen, screenEnv) {
		client, _, _ := tokensFixture(true)
		client.createToken = ports.AdminCreatedToken{
			Token: ports.AdminToken{Name: "ci", Accessor: "act_new0000000001"}, Secret: accessSecret,
			Accessor: "act_new0000000001", ExpiresAt: accessNow.Add(30 * day),
		}
		if mutate != nil {
			mutate(client)
		}
		env := accessEnvSized(client, w, h)
		s := newTokensScreen("r-ci", "ci-publisher", true, screenAdminRobots)
		settled, _ := accessSettle(s, env, s.Init(env))
		settled, _ = accessPress(t, settled, env, keys...)
		return settled, env
	}
}

func fiveRepos() []string {
	var repos []string
	for i := range 7 {
		repos = append(repos, fmt.Sprintf("team/app-%d", i+1))
	}
	return repos
}

func toCustomLifetime(from ...keyMsg) []keyMsg {
	keys := append([]keyMsg{}, from...)
	for range 5 {
		keys = append(keys, testKeyRight())
	}
	return keys
}

var accessStates = []accessState{
	{name: "robots_list", build: robotsStateAt(nil), contains: []string{"ci-publisher", "6 days"}},
	{name: "robots_without_access_selected", build: robotsStateAt(nil, testKeyDown()), contains: []string{"Grant it a repository with g."}},
	{name: "robots_old_selected", build: robotsStateAt(nil, testKeyDown(), testKeyDown(), testKeyDown()), contains: []string{"Enable robot"}},
	{name: "robots_many_rows", build: robotsStateAt(manyRobots, downs(40)...), contains: []string{"robot-"}},
	{name: "robots_empty", build: robotsStateAt(func(c *accessClient) { c.robots = nil }), contains: []string{"No robots yet"}},
	{name: "robots_error", build: robotsStateAt(func(c *accessClient) { c.listRobotsErr = errBoom }), contains: []string{"Could not load robots: boom"}},
	{name: "robots_failed_action", build: robotsStateAt(func(c *accessClient) { c.disableErr = errBoom }, testKeyRunes("d"), testKeyEnter()), contains: []string{"boom"}},
	{name: "robots_loading", build: func(t *testing.T, w, h int) (adminScreen, screenEnv) {
		return newRobotsScreen(), accessEnvSized(newAccessClient(), w, h)
	}, contains: []string{"Loading robots..."}},

	{name: "create_form_default", build: createStateAt(nil), contains: []string{"New robot", "Lifetime"}},
	{name: "create_form_five_suggestions", build: createStateAt(fiveRepos(), append(accessType("ci-bot"), testKeyTab())...), contains: []string{"team/app-1", "+2 more"}},
	{name: "create_form_suggestions_filtered", build: createStateAt(fiveRepos(), append(append(accessType("ci-bot"), testKeyTab()), accessType("team/app")...)...), contains: []string{"team/app-1", "+3 more"}},
	{name: "create_form_custom_empty", build: createStateAt(fiveRepos(), append(toCustomLifetime(testKeyTab(), testKeyTab(), testKeyTab()), []keyMsg{}...)...), contains: []string{"Custom"}},
	{name: "create_form_custom_valid", build: createStateAt(fiveRepos(), append(toCustomLifetime(testKeyTab(), testKeyTab(), testKeyTab()), accessType("12h")...)...), contains: []string{"in 12 hours"}},
	{name: "create_form_custom_invalid", build: createStateAt(fiveRepos(), append(toCustomLifetime(testKeyTab(), testKeyTab(), testKeyTab()), accessType("3y")...)...), contains: []string{lifetimeInvalidText()[:30]}},
	{name: "create_form_error", build: createStateAt(fiveRepos(), testKeyEnter()), contains: []string{"Name and repository are required."}},
	{name: "create_form_long_multibyte", build: createStateAt(fiveRepos(), accessType(strings.Repeat("ñandú-", 20))...), contains: []string{"Name"}},
	{name: "create_done_secret", build: createStateAt(fiveRepos(), append(append(accessType("release-bot"), testKeyTab()), testKeyEnter(), testKeyEnter())...), secret: accessSecret, contains: []string{"✓ Robot release-bot created"}},

	{name: "tokens_list", build: tokensStateAt(nil), contains: []string{"6 days left", "expired"}},
	{name: "tokens_loading", build: func(t *testing.T, w, h int) (adminScreen, screenEnv) {
		return newTokensScreen("u-1", "alice", false, screenAdminUsers), accessEnvSized(newAccessClient(), w, h)
	}, contains: []string{"Loading tokens..."}},
	{name: "tokens_empty", build: tokensStateAt(func(c *accessClient) { c.tokens["r-ci"] = nil }), contains: []string{"No tokens yet"}},
	{name: "tokens_error", build: tokensStateAt(func(c *accessClient) { c.tokensErr = errBoom }), contains: []string{"Could not load tokens: boom"}},
	{name: "tokens_many", build: tokensStateAt(manyTokens, downs(40)...), contains: []string{"token-"}},
	{name: "tokens_confirm_revoke", build: tokensStateAt(nil, testKeyRunes("x")), contains: []string{"release-pipeline"}},
	{name: "tokens_issue_name_focus", build: tokensStateAt(nil, append([]keyMsg{testKeyRunes("n")}, accessType("release-pipeline-2")...)...), contains: []string{"Issue a new token"}},
	{name: "tokens_issue_custom_invalid_error", build: tokensStateAt(func(c *accessClient) { c.createTokenErr = fmt.Errorf("token limit reached for this user") }, append(toCustomLifetime(testKeyRunes("n"), testKeyTab()), append(accessType("3y"), testKeyBackspace(), testKeyBackspace(), testKeyRunes("2"), testKeyRunes("h"), testKeyEnter())...)...), contains: []string{"token limit reached"}},
	{name: "tokens_issue_custom_invalid", build: tokensStateAt(nil, append(toCustomLifetime(testKeyRunes("n"), testKeyTab()), accessType("3y")...)...), contains: []string{"Enter a whole number"}},
	{name: "tokens_done_secret", build: tokensStateAt(nil, testKeyRunes("n"), testKeyEnter()), secret: accessSecret, contains: []string{"✓ Token ci created"}},
	{name: "tokens_long_multibyte_name", build: tokensStateAt(func(c *accessClient) {
		c.tokens["r-ci"][0].Name = strings.Repeat("ñandú-", 30)
	}), contains: []string{"ñandú"}},
}

func TestAccessScreensNeverExceedTheTerminalInAnyState(t *testing.T) {
	t.Parallel()
	for _, st := range accessStates {
		for _, size := range accessSizes {
			t.Run(st.name+"_"+sizeLabel(size.w, size.h), func(t *testing.T) {
				t.Parallel()
				s, env := st.build(t, size.w, size.h)
				rendered := accessView(s, env)
				lines := accessLines(rendered)
				if len(lines) != size.h {
					t.Fatalf("%d rows, want exactly %d", len(lines), size.h)
				}
				for i, line := range lines {
					if w := lipglossWidth(line); w > size.w {
						t.Fatalf("row %d is %d cells wide, terminal has %d: %q", i, w, size.w, line)
					}
				}
				mustContain(t, rendered, st.contains...)
				if st.secret != "" && !strings.Contains(panelText(rendered), st.secret) {
					t.Fatalf("the one-time secret is not whole:\n%s", rendered)
				}
			})
		}
	}
}

// panelText rejoins the text of the rightmost column: the secret may wrap
// across rows there, and nothing may be cut.
func panelText(rendered string) string {
	var b strings.Builder
	for _, line := range accessLines(rendered) {
		line = strings.TrimRight(line, " ")
		if i := strings.LastIndex(line, "│"); i > 0 {
			if j := strings.LastIndex(line[:i], "│"); j >= 0 {
				b.WriteString(strings.TrimSpace(line[j+len("│") : i]))
				continue
			}
		}
		b.WriteString(strings.TrimSpace(line))
	}
	return strings.ReplaceAll(b.String(), " ", "")
}

func TestAccessScreensSelectionStaysVisibleInLongLists(t *testing.T) {
	t.Parallel()
	for _, size := range accessSizes {
		t.Run(sizeLabel(size.w, size.h), func(t *testing.T) {
			t.Parallel()
			client, _ := robotsFixture()
			manyRobots(client)
			env := accessEnvSized(client, size.w, size.h)
			var s adminScreen = openRobotsScreen(t, client, env)
			for i := range 59 {
				s, _ = accessPress(t, s, env, testKeyDown())
				if i%7 == 0 || i == 58 {
					want := fmt.Sprintf("robot-%02d", s.(robotsScreen).selected)
					if view := accessView(s, env); !strings.Contains(view, "▸ "+want) {
						t.Fatalf("selection %s not visible after %d moves:\n%s", want, i+1, view)
					}
				}
			}
			ts, tenv := tokensStateAt(manyTokens)(t, size.w, size.h)
			for i := range 29 {
				ts, _ = accessPress(t, ts, tenv, testKeyDown())
				want := fmt.Sprintf("▸ token-%02d", ts.(tokensScreen).selected)
				if view := accessView(ts, tenv); !strings.Contains(view, want) {
					t.Fatalf("token selection %q not visible after %d moves:\n%s", want, i+1, view)
				}
			}
		})
	}
}

func manyTokens(client *accessClient) {
	var tokens []ports.AdminToken
	for i := range 30 {
		tokens = append(tokens, accessToken(fmt.Sprintf("token-%02d", i), fmt.Sprintf("act_%016d", i), accessNow.Add(-day), accessNow.Add(accessDays(i+1))))
	}
	client.tokens["r-ci"] = tokens
}

func TestAccessScreensGoldens(t *testing.T) {
	for _, name := range []string{
		"robots_list", "robots_without_access_selected", "robots_empty", "robots_error",
		"create_form_five_suggestions", "create_form_custom_invalid", "create_form_error", "create_done_secret",
		"tokens_list", "tokens_issue_name_focus", "tokens_issue_custom_invalid_error", "tokens_done_secret", "tokens_confirm_revoke",
	} {
		var st accessState
		for _, candidate := range accessStates {
			if candidate.name == name {
				st = candidate
			}
		}
		for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
			file := name + "_" + sizeLabel(size.w, size.h)
			t.Run(file, func(t *testing.T) {
				s, env := st.build(t, size.w, size.h)
				got := accessView(s, env)
				path := filepath.Join("testdata", "robots_tokens", file+".golden")
				if *updateRobotsTokensGoldens {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read golden (run with -update-robots-tokens): %v", err)
				}
				if string(want) != got {
					t.Fatalf("view differs from %s\n--- want\n%s\n--- got\n%s", path, want, got)
				}
			})
		}
	}
}
