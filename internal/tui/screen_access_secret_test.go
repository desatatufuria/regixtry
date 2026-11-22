package tui

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// startRobotCreate fills the Create robot form and presses Enter, returning
// the model and the (not yet delivered) create command.
func startRobotCreate(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	m = overviewPress(t, m, testKeyRunes("r"))
	m = overviewPress(t, m, testKeyRunes("late-bot"), testKeyTab(), testKeyRunes("team/api"), testKeyEnter())
	updated, cmd := m.Update(testKeyEnter())
	if cmd == nil {
		t.Fatal("Enter did not start the creation")
	}
	return updated.(Model), cmd
}

// startTokenCreate opens ci-publisher's tokens, starts issuing and returns the
// pending create command.
func startTokenCreate(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	m = overviewPress(t, openRobotsOf(t, m), testKeyRunes("t"), testKeyRunes("n"))
	updated, cmd := m.Update(testKeyEnter())
	if cmd == nil {
		t.Fatal("Enter did not start issuing")
	}
	return updated.(Model), cmd
}

func TestALateRobotSecretIsShownEvenAfterTheFormWasReopened(t *testing.T) {
	t.Parallel()
	m, _ := accessModel(t, nil)
	m, create := startRobotCreate(t, m)
	m = overviewPress(t, m, testKeyEsc())
	reopened, _ := m.mountCreateRobot()
	m = reopened.(Model)
	updated, _ := m.Update(create())
	m = updated.(Model)
	if m.screen != screenAdminCreateRobot {
		t.Fatalf("the late result left the operator on %q", m.screen)
	}
	mustContain(t, accessModelText(m), "robot-secret-value", "act_robot")
	m = overviewPress(t, m, testKeyEnter())
	mustNotContain(t, fmt.Sprintf("%+v", m), "robot-secret-value")
}

func TestALateTokenSecretIsShownEvenAfterTheTokensScreenWasReopened(t *testing.T) {
	t.Parallel()
	m, _ := accessModel(t, nil)
	m, create := startTokenCreate(t, m)
	m = overviewPress(t, m, testKeyEsc())
	reopened, _ := m.mountTokens(openTokensMsg{principalID: "r-ci", principalName: "ci-publisher", robot: true})
	m = reopened.(Model)
	updated, _ := m.Update(create())
	m = updated.(Model)
	if m.screen != screenAdminEditUserTokens {
		t.Fatalf("the late result left the operator on %q", m.screen)
	}
	view := accessModelText(m)
	mustContain(t, view, "token-secret-value", "Tokens for ci-publisher")
	m = overviewPress(t, m, testKeyEnter())
	mustNotContain(t, fmt.Sprintf("%+v", m), "token-secret-value")
}

func TestAFailedCreateAfterLeavingIsReportedInOneLine(t *testing.T) {
	t.Parallel()
	for _, reopen := range []bool{false, true} {
		for name, start := range map[string]func(*testing.T, Model) (Model, tea.Cmd){"robot": startRobotCreate, "token": startTokenCreate} {
			m, _ := accessModel(t, func(c *accessClient) {
				c.createRobotErr = fmt.Errorf("registry refused")
				c.createTokenErr = fmt.Errorf("registry refused")
			})
			m, create := start(t, m)
			m = overviewPress(t, m, testKeyEsc())
			if reopen {
				next, _ := m.mountRobots("")
				m = next.(Model)
			}
			updated, _ := m.Update(create())
			m = updated.(Model)
			view := accessModelText(m)
			mustContain(t, view, "failed: registry refused")
			if lines := accessLines(view); len(lines) > 24 {
				t.Fatalf("%s reopen=%v: %d rows", name, reopen, len(lines))
			}
			// The notice lasts one key.
			m = overviewPress(t, m, testKeyDown())
			mustNotContain(t, accessModelText(m), "failed: registry refused")
		}
	}
}

func TestTheSecretScreenOwnsTheFirstKeyAfterAYank(t *testing.T) {
	t.Parallel()
	m, client := accessModel(t, nil)
	m, create := startRobotCreate(t, m)
	late := create()
	m = overviewPress(t, m, testKeyEsc())

	// An open disable confirm on the Robots list must not see the next key.
	m = overviewPress(t, m, testKeyRunes("d"))
	if !m.adminScreens[slotRobots].(robotsScreen).confirm.Active() {
		t.Fatal("setup: no confirm open")
	}
	updated, _ := m.Update(late)
	m = updated.(Model)
	if m.screen != screenAdminCreateRobot {
		t.Fatalf("not yanked: %q", m.screen)
	}
	for _, k := range []keyMsg{testKeyRunes("y"), testKeyRunes("d")} {
		if after, cmd := sendKeyCmd(m, k); cmd != nil || after.screen != screenAdminCreateRobot {
			t.Fatalf("key %v acted on the replaced screen (screen=%q cmd=%v)", k, after.screen, cmd != nil)
		}
	}
	m = overviewPress(t, m, testKeyEnter())
	if len(client.disabled) != 0 {
		t.Fatalf("a key reached the replaced confirm: DisableUser calls = %v", client.disabled)
	}

	// The shared confirm and the scan history overlay are dropped too.
	other, _ := accessModel(t, nil)
	other, create = startRobotCreate(t, other)
	late = create()
	other = overviewPress(t, other, testKeyEsc())
	runs := 0
	other.screen = screenAdminUsers
	other.adminView.Confirm = newConfirmPrompt("t", "m", "delete", "", func(screenEnv) tea.Cmd { runs++; return nil })
	other.adminScreens[slotScanHistory] = scanHistoryScreen{}
	updated, _ = other.Update(late)
	other = overviewPress(t, updated.(Model), testKeyEnter())
	if runs != 0 || other.adminView.Confirm.Active() || other.adminScreens[slotScanHistory] != nil {
		t.Fatalf("runs=%d confirm=%v: the replaced screen's prompt survived the yank", runs, other.adminView.Confirm.Active())
	}
}

func TestTwoLateSecretsAreShownOneAfterTheOther(t *testing.T) {
	t.Parallel()
	m, _ := accessModel(t, nil)
	m, createRobot := startRobotCreate(t, m)
	robotMsg := createRobot()
	m = overviewPress(t, m, testKeyEsc())
	m, createToken := startTokenCreate(t, m)
	tokenMsg := createToken()
	m = overviewPress(t, m, testKeyEsc())

	updated, _ := m.Update(robotMsg)
	m = updated.(Model)
	updated, _ = m.Update(tokenMsg)
	m = updated.(Model)
	mustContain(t, accessModelText(m), "robot-secret-value")
	mustNotContain(t, accessModelText(m), "token-secret-value")
	m = overviewPress(t, m, testKeyEnter())
	mustContain(t, accessModelText(m), "token-secret-value")
	mustNotContain(t, accessModelText(m), "robot-secret-value")
	m = overviewPress(t, m, testKeyEnter())
	mustNotContain(t, fmt.Sprintf("%+v", m), "token-secret-value")
	if len(m.queuedSecrets) != 0 {
		t.Fatal("a secret is still queued")
	}
}

func TestRRefreshesTheTokensOnceAndDropsStaleResults(t *testing.T) {
	t.Parallel()
	client, env, s := tokensFixture(true)
	before := len(client.listedTokens)
	first, cmd, _ := s.Update(env, testKeyRunes("r"))
	if cmd == nil || !first.(tokensScreen).loading {
		t.Fatal("r did not start a read")
	}
	if _, second, _ := first.Update(env, testKeyRunes("r")); second != nil {
		t.Fatal("r started a second read while one was in flight")
	}
	done, _ := accessSettle(first, env, cmd)
	if len(client.listedTokens) != before+1 || done.(tokensScreen).loading {
		t.Fatalf("reads=%d loading=%v", len(client.listedTokens)-before, done.(tokensScreen).loading)
	}
	if _, again, _ := done.Update(env, testKeyRunes("r")); again == nil {
		t.Fatal("r does not work again after the result")
	}
	if _, cmd, _ := done.Update(env, keyMsg{Text: "r"}); cmd != nil {
		t.Fatal("a pasted r reloaded")
	}
	mustContain(t, accessView(done, env), "r refresh")
	if reason, ok := (tokensLoadedMsg{err: NewAdminSessionExpiredError("gone")}).expiredReason(); !ok || reason != "gone" {
		t.Fatal("an expired read is not reported")
	}
}

// ---- seeded fuzz of the secret lifecycle ----

// The invariant: a created secret is never retained anywhere in the model
// (fmt "%+v") unless it is on screen or waiting in the queue, and every
// secret the backend created is shown before the run ends.
func TestSecretLifecycleFuzz(t *testing.T) {
	t.Parallel()
	starters := []tea.Msg{openCreateRobotMsg{}, openRobotsMsg{}, openTokensMsg{principalID: "r-ci", principalName: "ci-publisher", robot: true}, openTokensMsg{principalID: "u-1", principalName: "alice"}}
	keys := []keyMsg{testKeyEnter(), testKeyEnter(), testKeyEsc(), testKeyEsc(), testKeyTab(), testKeyDown(), testKeyUp(), testKeyLeft(), testKeyRight(),
		testKeyRunes("n"), testKeyRunes("t"), testKeyRunes("x"), testKeyRunes("y"), testKeyRunes("d"), testKeyRunes("r"), testKeyRunes("a"), testKeyRunes("/")}

	for seed := range 60 {
		rng := rand.New(rand.NewSource(int64(seed)))
		secrets := 0
		client := newAccessClient()
		client.robots = []ports.AdminRobot{{ID: "r-ci", Username: "ci-publisher", Repository: "team/api", Role: domainauth.RepoRoleWriter, Enabled: true}}
		client.tokens["r-ci"] = []ports.AdminToken{accessToken("t", "act_seed", charFixedTime.Add(-day), charFixedTime.Add(40*day))}
		known := map[string]bool{}
		next := func(kind string) string {
			secrets++
			secret := fmt.Sprintf("%s-secret-%d-%d", kind, seed, secrets)
			known[secret] = true
			return secret
		}
		fuzz := &fuzzClient{accessClient: client, next: next, failEvery: 4 + seed%5}
		m := overviewModelAt(t, fuzz, 150, 24)
		var pending []tea.Msg
		run := func(msg tea.Msg) {
			updated, cmd := m.Update(msg)
			m = updated.(Model)
			for cmd != nil {
				out := cmd()
				if _, async := out.(accessMsg); async {
					pending = append(pending, out)
					return
				}
				updated, cmd = m.Update(out)
				m = updated.(Model)
			}
		}
		seen := map[string]bool{}
		for op := 0; op < 60; op++ {
			switch p := rng.Intn(10); {
			case p < 3 && len(pending) > 0:
				j := rng.Intn(len(pending))
				msg := pending[j]
				pending = append(pending[:j], pending[j+1:]...)
				run(msg)
			case p == 3 && !m.secretOnScreen():
				// Synthetic navigation: no key reaches it while a secret is shown.
				run(starters[rng.Intn(len(starters))])
			default:
				k := keys[rng.Intn(len(keys))]
				if rng.Intn(4) == 0 {
					k = testKeyRunes(string("abxyz-"[rng.Intn(6)]))
				}
				run(k)
			}
			if len(known) == 0 {
				continue
			}
			view, dump := accessModelText(m), modelState(m)
			for secret := range known {
				if strings.Contains(view, secret) {
					seen[secret] = true
				}
				if strings.Contains(dump, secret) && !strings.Contains(view, secret) && !queuedHolds(m, secret) {
					t.Fatalf("seed %d op %d: %q is retained in the model but not on screen", seed, op, secret)
				}
			}
		}
		// Deliver what is still in flight and press Done until nothing is left.
		for len(pending) > 0 {
			msg := pending[0]
			pending = pending[1:]
			run(msg)
		}
		for i := 0; i < 8 && (m.secretOnScreen() || len(m.queuedSecrets) > 0); i++ {
			view := accessModelText(m)
			for secret := range known {
				if strings.Contains(view, secret) {
					seen[secret] = true
				}
			}
			run(testKeyEnter())
		}
		for secret := range known {
			if !seen[secret] && fuzz.created[secret] {
				t.Fatalf("seed %d: the created secret %q was never shown", seed, secret)
			}
		}
	}
}

// modelState is everything the model holds that a secret could hide in: the
// mounted screens, the shared view state, the queue and the status lines.
func modelState(m Model) string {
	return fmt.Sprintf("%+v|%+v|%+v|%s|%s", m.adminScreens, m.adminView, m.queuedSecrets, m.status, m.lateNotice)
}

// fuzzClient mints a fresh secret for every successful creation and fails
// some of them.
type fuzzClient struct {
	*accessClient
	next      func(kind string) string
	failEvery int
	calls     int
	created   map[string]bool
}

func (c *fuzzClient) record(secret string) {
	if c.created == nil {
		c.created = map[string]bool{}
	}
	c.created[secret] = true
}

func (c *fuzzClient) CreateRobot(_ context.Context, _ AdminSession, in ports.AdminCreateRobotInput) (ports.AdminCreatedRobot, error) {
	c.calls++
	if c.calls%c.failEvery == 0 {
		return ports.AdminCreatedRobot{}, fmt.Errorf("refused %d", c.calls)
	}
	secret := c.next("robot")
	c.record(secret)
	return ports.AdminCreatedRobot{Robot: ports.AdminRobot{ID: "r-new", Username: in.Name, Repository: in.Repository, Role: in.Role}, Secret: secret, Accessor: "act_robot", ExpiresAt: charFixedTime.Add(day)}, nil
}

func (c *fuzzClient) CreateUserAdminToken(_ context.Context, _ AdminSession, in ports.AdminCreateTokenInput) (ports.AdminCreatedToken, error) {
	c.calls++
	if c.calls%c.failEvery == 0 {
		return ports.AdminCreatedToken{}, fmt.Errorf("refused %d", c.calls)
	}
	secret := c.next("token")
	c.record(secret)
	return ports.AdminCreatedToken{Secret: secret, Accessor: "act_tok", ExpiresAt: charFixedTime.Add(day)}, nil
}

func queuedHolds(m Model, secret string) bool {
	for _, q := range m.queuedSecrets {
		if strings.Contains(fmt.Sprintf("%+v", q), secret) {
			return true
		}
	}
	return false
}
