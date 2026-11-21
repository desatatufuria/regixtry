package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// accessNow is the clock every Access screen test runs on.
var accessNow = time.Date(2026, time.November, 21, 9, 30, 0, 0, time.UTC)

// accessClient extends the shared fake so the calls the Robots, Create robot
// and Tokens screens make are recorded exactly.
type accessClient struct {
	*fakeAdminClient
	enabled      []string
	disabled     []string
	tokenInputs  []ports.AdminCreateTokenInput
	revoked      []string
	listedTokens []string
	enableErr    error
	disableErr   error
}

func newAccessClient() *accessClient {
	return &accessClient{fakeAdminClient: &fakeAdminClient{tokens: map[string][]ports.AdminToken{}}}
}

func (c *accessClient) EnableUser(_ context.Context, _ AdminSession, id string) (ports.AdminUser, error) {
	c.enabled = append(c.enabled, id)
	return ports.AdminUser{ID: id}, c.enableErr
}

func (c *accessClient) DisableUser(_ context.Context, _ AdminSession, id string) (ports.AdminUser, error) {
	c.disabled = append(c.disabled, id)
	return ports.AdminUser{ID: id}, c.disableErr
}

func (c *accessClient) ListUserAdminTokens(ctx context.Context, s AdminSession, id string) ([]ports.AdminToken, error) {
	c.listedTokens = append(c.listedTokens, id)
	return c.fakeAdminClient.ListUserAdminTokens(ctx, s, id)
}

func (c *accessClient) CreateUserAdminToken(_ context.Context, _ AdminSession, in ports.AdminCreateTokenInput) (ports.AdminCreatedToken, error) {
	c.tokenInputs = append(c.tokenInputs, in)
	if c.createTokenErr != nil {
		return ports.AdminCreatedToken{}, c.createTokenErr
	}
	return c.createToken, nil
}

func (c *accessClient) RevokeUserAdminToken(_ context.Context, _ AdminSession, _ string, accessor string) error {
	c.revoked = append(c.revoked, accessor)
	return c.revokeTokenErr
}

func accessEnvSized(client AdminClient, w, h int, known ...string) screenEnv {
	return screenEnv{
		Client:            client,
		Session:           AdminSession{Username: "operator", BearerToken: "token"},
		Layout:            consoleLayout{Width: w, Height: h},
		KnownRepositories: known,
		Now:               func() time.Time { return accessNow },
	}
}

func accessEnv(client AdminClient, known ...string) screenEnv {
	return accessEnvSized(client, 150, 24, known...)
}

// accessSettle runs cmd and feeds its message back until no command is left,
// and returns every message it saw.
func accessSettle(s adminScreen, env screenEnv, cmd tea.Cmd) (adminScreen, []tea.Msg) {
	var seen []tea.Msg
	for cmd != nil {
		msg := cmd()
		seen = append(seen, msg)
		s, cmd, _ = s.Update(env, msg)
	}
	return s, seen
}

// accessPress sends keys one by one, settling every command it returns, and
// collects the messages that leave the screen for the parent (navigation).
func accessPress(t *testing.T, s adminScreen, env screenEnv, keys ...keyMsg) (adminScreen, []tea.Msg) {
	t.Helper()
	var all []tea.Msg
	for _, k := range keys {
		next, cmd, _ := s.Update(env, k)
		var seen []tea.Msg
		s, seen = accessSettle(next, env, cmd)
		all = append(all, seen...)
	}
	return s, all
}

func accessType(text string) []keyMsg {
	keys := make([]keyMsg, 0, len(text))
	for _, r := range text {
		keys = append(keys, testKeyRunes(string(r)))
	}
	return keys
}

// accessView renders the screen's frame with ANSI removed.
func accessView(s adminScreen, env screenEnv) string {
	return ansi.Strip(s.View(newAdminTheme(), env).Body)
}

func accessStrip(s string) string { return ansi.Strip(s) }

func lipglossWidth(s string) int { return lipgloss.Width(s) }

func accessLines(rendered string) []string {
	return strings.Split(strings.TrimRight(rendered, "\n"), "\n")
}

// mustContain fails unless every text appears in rendered.
func mustContain(t *testing.T, rendered string, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if !strings.Contains(rendered, text) {
			t.Fatalf("missing %q in:\n%s", text, rendered)
		}
	}
}

func mustNotContain(t *testing.T, rendered string, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if strings.Contains(rendered, text) {
			t.Fatalf("unexpected %q in:\n%s", text, rendered)
		}
	}
}

// sampleRobots is the Robots board's data: one near expiry, one without
// repository access, one healthy, one disabled with an expired token.
func sampleRobots() []ports.AdminRobot {
	return []ports.AdminRobot{
		{ID: "r-ci", Username: "ci-publisher", Repository: "team/api", Role: domainauth.RepoRoleWriter, Enabled: true, CreatedAt: time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)},
		{ID: "r-gh", Username: "gh-actions", Enabled: true},
		{ID: "r-stg", Username: "staging-deploy", Repository: "team/web", Role: domainauth.RepoRoleWriter, Enabled: true},
		{ID: "r-old", Username: "old-deploy", Repository: "team/infra", Role: domainauth.RepoRoleWriter},
	}
}

func accessToken(name, accessor string, created, expires time.Time) ports.AdminToken {
	return ports.AdminToken{ID: "t-" + accessor, Name: name, Accessor: accessor, CreatedAt: created, ExpiresAt: expires}
}

func sampleRobotTokens() map[string][]ports.AdminToken {
	day := 24 * time.Hour
	return map[string][]ports.AdminToken{
		"r-ci": {
			accessToken("staging-deploy", "act_17ab0000003f2a", accessNow.Add(-12*day), accessNow.Add(18*day)),
			accessToken("release-pipeline", "act_9c410000000e0d2", accessNow.Add(-24*day), accessNow.Add(6*day+2*time.Hour)),
		},
		"r-gh":  {accessToken("gh", "act_aaaa0000000bbbb", accessNow.Add(-day), accessNow.Add(89*day))},
		"r-stg": {accessToken("deploy", "act_bbbb0000000cccc", accessNow.Add(-12*day), accessNow.Add(18*day))},
		"r-old": {accessToken("old-deploy", "act_55f00000000b81c", accessNow.Add(-32*day), accessNow.Add(-19*day))},
	}
}

var errBoom = errors.New("boom")
