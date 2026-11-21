package tui

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"regixtry/internal/ports"
)

// The lifetime goldens have their own flag so regenerating them can never
// touch any other golden (those use -update and -update-grant-screen).
var updateTokenLifetimeGoldens = flag.Bool("update-token-lifetime", false, "rewrite the token lifetime golden files")

// ttlRecordingClient records the input of every token creation. Robot
// creation is already recorded by fakeAdminClient (lastCreateRobotInput).
type ttlRecordingClient struct {
	*fakeAdminClient
	tokenInputs []ports.AdminCreateTokenInput
}

func (c *ttlRecordingClient) CreateUserAdminToken(ctx context.Context, s AdminSession, in ports.AdminCreateTokenInput) (ports.AdminCreatedToken, error) {
	c.tokenInputs = append(c.tokenInputs, in)
	return c.fakeAdminClient.CreateUserAdminToken(ctx, s, in)
}

func newTTLClient() *ttlRecordingClient {
	return &ttlRecordingClient{fakeAdminClient: &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: charFixedTime.Add(24 * time.Hour)},
	}}
}

func lifetimeModel(t *testing.T, client *ttlRecordingClient, w, h int) Model {
	t.Helper()
	m := newAdminReadyModelWithCatalog(t, []string{"team/app", "team/demo"}, client)
	m.now = func() time.Time { return charFixedTime }
	m = charResize(t, m, w, h)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: charFixedTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	m.adminView.SelectedUserID = "u-1"
	m.adminView.SelectedUsername = "alice"
	return m
}

func tokenFormAt(t *testing.T, client *ttlRecordingClient, w, h int) Model {
	m := lifetimeModel(t, client, w, h)
	m.screen = screenAdminCreateToken
	return m
}

// robotFormAt returns the robot form with Name and Repository filled through
// real keys and the focus on the Lifetime field (Role was skipped with Tab).
func robotFormAt(t *testing.T, client *ttlRecordingClient, w, h int) Model {
	m := lifetimeModel(t, client, w, h)
	m.screen = screenAdminCreateRobot
	m = testSendKeys(t, m, testKeyRunes("ci-bot"), testKeyTab(), testKeyRunes("team/app"), testKeyTab(), testKeyTab())
	if m.adminView.CreateRobotForm.Focus != adminCreateRobotFieldLifetime {
		t.Fatalf("robot focus = %v, want Lifetime", m.adminView.CreateRobotForm.Focus)
	}
	return m
}

// pressEnter feeds Enter and runs the resulting command, if any.
func pressEnter(t *testing.T, m Model) (Model, bool) {
	t.Helper()
	updated, cmd := m.Update(testKeyEnter())
	m = updated.(Model)
	if cmd == nil {
		return m, false
	}
	return runCmd(t, m, cmd), true
}

// toCustom moves the selection from the default (30 days) to Custom.
func toCustom() []keyMsg {
	return []keyMsg{testKeyRight(), testKeyRight(), testKeyRight(), testKeyRight()}
}

func withCustom(typed ...string) []keyMsg {
	keys := toCustom()
	for _, text := range typed {
		keys = append(keys, testKeyRunes(text))
	}
	return keys
}

const lifetimeInvalidMessage = "Enter a whole number followed by h, d or y, up to 2 years (for example 12h, 30d, 2y)"

var lifetimeSubmitCases = []struct {
	name string
	keys []keyMsg
	want time.Duration
}{
	{"default is 30 days", nil, 30 * day},
	{"Left selects 7 days", []keyMsg{testKeyLeft()}, 7 * day},
	{"Left then Right is 30 days again", []keyMsg{testKeyLeft(), testKeyRight()}, 30 * day},
	{"Right selects 90 days", []keyMsg{testKeyRight()}, 90 * day},
	{"two Right select 1 year (365 days)", []keyMsg{testKeyRight(), testKeyRight()}, 365 * day},
	{"three Right select 2 years (730 days)", []keyMsg{testKeyRight(), testKeyRight(), testKeyRight()}, 730 * day},
	{"Custom 12h", withCustom("1", "2", "h"), 12 * time.Hour},
	{"Custom 36h", withCustom("3", "6", "h"), 36 * time.Hour},
	{"Custom 45 means days", withCustom("4", "5"), 45 * day},
	{"Custom 30d", withCustom("3", "0", "d"), 30 * day},
	{"Custom 2y", withCustom("2", "y"), 730 * day},
	{"pasted 12h", append(toCustom(), keyMsg{Text: "12h"}), 12 * time.Hour},
	{"pasted 2y", append(toCustom(), keyMsg{Text: "2y"}), 730 * day},
	{"pasted 45", append(toCustom(), keyMsg{Text: "45"}), 45 * day},
	{"pasted padded 30d", append(toCustom(), keyMsg{Text: " 30d "}), 30 * day},
	{"Custom 17520h is the ceiling", withCustom("1", "7", "5", "2", "0", "h"), 730 * day},
}

var lifetimeRejectCases = []struct {
	name string
	keys []keyMsg
}{
	{"Custom empty", toCustom()},
	{"Custom 0", withCustom("0")},
	{"Custom 0h", withCustom("0", "h")},
	{"Custom 731d", withCustom("7", "3", "1", "d")},
	{"Custom 3y", withCustom("3", "y")},
	{"Custom 17521h", withCustom("1", "7", "5", "2", "1", "h")},
	{"Custom huge", append(toCustom(), keyMsg{Text: "99999999999999999999"})},
	{"Custom emptied again", append(withCustom("5"), testKeyBackspace())},
	{"Custom unit only", withCustom("h")},
	{"pasted 1 day is ignored", append(toCustom(), keyMsg{Text: "1 day"})},
	{"pasted 2 days is ignored", append(toCustom(), keyMsg{Text: "2 days"})},
	{"pasted 30 days is ignored", append(toCustom(), keyMsg{Text: "30 days"})},
	{"pasted 12 hours is ignored", append(toCustom(), keyMsg{Text: "12 hours"})},
	{"pasted 1d2h is ignored", append(toCustom(), keyMsg{Text: "1d2h"})},
}

func TestCreateTokenFormSendsTheChosenLifetime(t *testing.T) {
	t.Parallel()
	for _, tt := range lifetimeSubmitCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := newTTLClient()
			m := tokenFormAt(t, client, 150, 24)
			m = testSendKeys(t, m, testKeyTab())
			m = testSendKeys(t, m, tt.keys...)
			if _, sent := pressEnter(t, m); !sent {
				t.Fatalf("Enter sent nothing")
			}
			if len(client.tokenInputs) != 1 {
				t.Fatalf("token creations = %d, want 1", len(client.tokenInputs))
			}
			if got := client.tokenInputs[0].TTL; got != tt.want {
				t.Fatalf("TTL sent = %v (%d s), want %v (%d s)", got, int64(got/time.Second), tt.want, int64(tt.want/time.Second))
			}
		})
	}
}

func TestCreateTokenFormSubmitsFromTheNameFieldToo(t *testing.T) {
	t.Parallel()
	client := newTTLClient()
	m := tokenFormAt(t, client, 150, 24)
	m = testSendKeys(t, m, testKeyRunes("ci"), testKeyTab(), testKeyLeft(), testKeyTab())
	if _, sent := pressEnter(t, m); !sent || len(client.tokenInputs) != 1 {
		t.Fatalf("Enter on Name sent %d creations", len(client.tokenInputs))
	}
	if got := client.tokenInputs[0]; got.TTL != 7*day || got.Name != "ci" || got.UserID != "u-1" {
		t.Fatalf("input = %#v, want 7 days for ci/u-1", got)
	}
}

func TestCreateTokenFormRejectsAnInvalidCustomLifetime(t *testing.T) {
	t.Parallel()
	for _, tt := range lifetimeRejectCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := newTTLClient()
			m := tokenFormAt(t, client, 150, 24)
			m = testSendKeys(t, m, testKeyTab())
			m = testSendKeys(t, m, tt.keys...)
			m, sent := pressEnter(t, m)
			if sent || len(client.tokenInputs) != 0 {
				t.Fatalf("an invalid lifetime sent a creation (cmd=%v, calls=%d)", sent, len(client.tokenInputs))
			}
			if m.screen != screenAdminCreateToken {
				t.Fatalf("screen = %q, want the form to stay open", m.screen)
			}
			if got := ansi.Strip(m.View().Content); !strings.Contains(got, lifetimeInvalidMessage) {
				t.Fatalf("view lacks the validation message:\n%s", got)
			}
		})
	}
}

func TestCreateRobotFormSendsTheChosenLifetime(t *testing.T) {
	t.Parallel()
	for _, tt := range lifetimeSubmitCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := newTTLClient()
			m := robotFormAt(t, client, 150, 24)
			m = testSendKeys(t, m, tt.keys...)
			if _, sent := pressEnter(t, m); !sent {
				t.Fatalf("Enter sent nothing")
			}
			if client.createRobotCalls != 1 {
				t.Fatalf("robot creations = %d, want 1", client.createRobotCalls)
			}
			in := client.lastCreateRobotInput
			if in.TTL != tt.want || in.Name != "ci-bot" || in.Repository != "team/app" {
				t.Fatalf("input = %#v, want TTL %v for ci-bot on team/app", in, tt.want)
			}
		})
	}
}

func TestCreateRobotFormRejectsAnInvalidCustomLifetime(t *testing.T) {
	t.Parallel()
	for _, tt := range lifetimeRejectCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := newTTLClient()
			m := robotFormAt(t, client, 150, 24)
			m = testSendKeys(t, m, tt.keys...)
			m, sent := pressEnter(t, m)
			if sent || client.createRobotCalls != 0 {
				t.Fatalf("an invalid lifetime sent a creation (cmd=%v, calls=%d)", sent, client.createRobotCalls)
			}
			if m.screen != screenAdminCreateRobot {
				t.Fatalf("screen = %q, want the form to stay open", m.screen)
			}
			if got := ansi.Strip(m.View().Content); !strings.Contains(got, lifetimeInvalidMessage) {
				t.Fatalf("view lacks the validation message:\n%s", got)
			}
		})
	}
}

func TestLifetimeFieldOrder(t *testing.T) {
	t.Parallel()
	m := tokenFormAt(t, newTTLClient(), 150, 24)
	if m.adminView.TokenForm.Focus != adminTokenFieldName {
		t.Fatalf("initial token focus = %v, want Name", m.adminView.TokenForm.Focus)
	}
	m = testSendKey(t, m, testKeyTab())
	if m.adminView.TokenForm.Focus != adminTokenFieldLifetime {
		t.Fatalf("token focus after Tab = %v, want Lifetime", m.adminView.TokenForm.Focus)
	}
	m = testSendKey(t, m, testKeyTab())
	if m.adminView.TokenForm.Focus != adminTokenFieldName {
		t.Fatalf("token focus after second Tab = %v, want Name", m.adminView.TokenForm.Focus)
	}
	order := []adminCreateRobotField{adminCreateRobotFieldName, adminCreateRobotFieldRepository, adminCreateRobotFieldRole, adminCreateRobotFieldLifetime, adminCreateRobotFieldName}
	for i := 0; i < len(order)-1; i++ {
		if got := nextCreateRobotField(order[i]); got != order[i+1] {
			t.Fatalf("nextCreateRobotField(%v) = %v, want %v", order[i], got, order[i+1])
		}
	}
}

func TestLifetimeFieldKeepsEscAndStrayKeysHarmless(t *testing.T) {
	t.Parallel()
	for _, form := range []string{"token", "robot"} {
		t.Run(form, func(t *testing.T) {
			t.Parallel()
			client := newTTLClient()
			var m Model
			focus := func(m Model) any { return m.adminView.TokenForm.Focus }
			if form == "token" {
				m = testSendKey(t, tokenFormAt(t, client, 150, 24), testKeyTab())
			} else {
				m = robotFormAt(t, client, 150, 24)
				focus = func(m Model) any { return m.adminView.CreateRobotForm.Focus }
			}
			before := focus(m)
			for _, k := range []keyMsg{testKeyRunes("j"), testKeyRunes("k"), testKeyRunes("7"), testKeyRunes("x"), testKeySpace(), testKeyUp(), testKeyDown(), testKeyBackspace()} {
				m = testSendKey(t, m, k)
				if focus(m) != before {
					t.Fatalf("key %#v moved the focus", k)
				}
			}
			if d, ok := lifetimeOf(m, form).duration(lifetimeCeiling()); !ok || d != 30*day {
				t.Fatalf("stray keys changed the lifetime: %v, %v", d, ok)
			}
			m = testSendKey(t, m, testKeyEsc())
			if form == "token" && m.screen != screenAdminEditUserTokens || form == "robot" && m.screen != screenAdminRobots {
				t.Fatalf("Esc went to %q", m.screen)
			}
		})
	}
}

func lifetimeOf(m Model, form string) tokenLifetime {
	if form == "token" {
		return m.adminView.TokenForm.Lifetime
	}
	return m.adminView.CreateRobotForm.Lifetime
}

func TestLifetimeCustomKeysInTheForm(t *testing.T) {
	t.Parallel()
	m := testSendKey(t, tokenFormAt(t, newTTLClient(), 150, 24), testKeyTab())
	m = testSendKeys(t, m, toCustom()...)
	m = testSendKeys(t, m, testKeyRunes("j"), testKeyRunes("k"), testKeyRunes("a"), testKeyRunes("1"), testKeyRunes("2"))
	got := func() string { return m.adminView.TokenForm.Lifetime.Custom }
	if got() != "12" {
		t.Fatalf("input = %q, want 12", got())
	}
	if !m.adminView.TokenForm.Lifetime.isCustom(lifetimeCeiling()) || m.adminView.TokenForm.Focus != adminTokenFieldLifetime {
		t.Fatalf("j/k moved the selection or the focus")
	}
	m = testSendKeys(t, m, testKeyRunes("h"), testKeyRunes("Y"))
	if got() != "12y" {
		t.Fatalf("input after h then Y = %q, want 12y (replaced, lowercase)", got())
	}
	m = testSendKey(t, m, testKeyBackspace())
	m = testSendKey(t, m, testKeyBackspace())
	if got() != "1" {
		t.Fatalf("input after two Backspace = %q, want 1", got())
	}
	m = testSendKey(t, m, keyMsg{Text: "4x2d"}) // bracketed paste that is not a lifetime: ignored
	if got() != "1" {
		t.Fatalf("input after an invalid paste = %q, want 1", got())
	}
	m = testSendKey(t, m, keyMsg{Text: " 30d "}) // a valid paste replaces the input
	if got() != "30d" {
		t.Fatalf("input after a valid paste = %q, want 30d", got())
	}
}

func TestNameAndRepositoryStillTakeLettersAndPastes(t *testing.T) {
	t.Parallel()
	m := testSendKeys(t, tokenFormAt(t, newTTLClient(), 150, 24), testKeyRunes("j"), testKeyRunes("k"), testKeyRunes("x"), testKeyBackspace(), keyMsg{Text: "enter"})
	if got := m.adminView.TokenForm.Name; got != "jkenter" {
		t.Fatalf("token name = %q, want jkenter", got)
	}
	if m.adminView.TokenForm.Focus != adminTokenFieldName {
		t.Fatalf("a letter moved the token focus")
	}
	r := lifetimeModel(t, newTTLClient(), 150, 24)
	r.screen = screenAdminCreateRobot
	r = testSendKeys(t, r, testKeyRunes("jk"), testKeyRunes("x"), testKeyBackspace(), keyMsg{Text: "enter"})
	if got := r.adminView.CreateRobotForm.Name; got != "jkenter" {
		t.Fatalf("robot name = %q, want jkenter", got)
	}
}

func TestLifetimePreviewReflectsTheModelClock(t *testing.T) {
	t.Parallel()
	m := tokenFormAt(t, newTTLClient(), 150, 24)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Lifetime", "7 days", "30 days", "90 days", "1 year", "2 years", "Custom", "Expires Mon 13 Apr 2026 09:30 UTC (in 30 days)", "A leaked token stays valid until it expires or you revoke it."} {
		if !strings.Contains(view, want) {
			t.Fatalf("default view lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "TTL seconds") {
		t.Fatalf("the raw TTL field is still rendered:\n%s", view)
	}
	m.now = func() time.Time { return time.Date(2028, time.February, 25, 12, 0, 0, 0, time.UTC) }
	m = testSendKeys(t, m, testKeyTab(), testKeyLeft())
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Expires Fri 3 Mar 2028 12:00 UTC (in 7 days)") {
		t.Fatalf("preview does not follow the clock and the selection:\n%s", view)
	}
}

// Rendering: goldens and no-overflow at the two supported sizes.

var lifetimeViewSizes = []struct{ w, h int }{{150, 24}, {200, 40}}

type lifetimeViewState struct {
	name string
	keys []keyMsg
}

var lifetimeViewStates = []lifetimeViewState{
	{"default", nil},
	{"custom_valid_hours", withCustom("1", "2", "h")},
	{"custom_valid_years", withCustom("2", "y")},
	{"custom_invalid", withCustom("3", "y")},
}

func assertLifetimeGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "token_lifetime", name+".golden")
	if *updateTokenLifetimeGoldens {
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
		t.Fatalf("read golden %s (run with -update-token-lifetime): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("rendered view differs from %s (run with -update-token-lifetime to inspect)\n--- want\n%s\n--- got\n%s", path, strings.TrimRight(string(want), "\n"), strings.TrimRight(got, "\n"))
	}
}

func assertLifetimeNoOverflow(t *testing.T, label, rendered string, w, h int) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	if len(lines) > h {
		t.Fatalf("%s: %d rows, terminal has %d\n%s", label, len(lines), h, rendered)
	}
	for i, line := range lines {
		if width := ansi.StringWidth(line); width > w {
			t.Fatalf("%s: line %d is %d cells wide, terminal has %d: %q", label, i+1, width, w, line)
		}
	}
}

func TestLifetimeFormViews(t *testing.T) {
	for _, form := range []string{"token", "robot"} {
		for _, size := range lifetimeViewSizes {
			for _, state := range lifetimeViewStates {
				name := form + "_" + state.name + "_" + sizeLabel(size.w, size.h)
				t.Run(name, func(t *testing.T) {
					var m Model
					if form == "token" {
						m = testSendKeys(t, tokenFormAt(t, newTTLClient(), size.w, size.h), testKeyRunes("ci"), testKeySpace(), testKeyRunes("bot"), testKeyTab())
					} else {
						m = robotFormAt(t, newTTLClient(), size.w, size.h)
					}
					m = testSendKeys(t, m, state.keys...)
					rendered := ansi.Strip(m.View().Content)
					assertLifetimeNoOverflow(t, name, rendered, size.w, size.h)
					assertLifetimeGolden(t, name, rendered)
				})
			}
		}
	}
}

func sizeLabel(w, h int) string { return fmt.Sprintf("%dx%d", w, h) }

// The Create Robot form is the tallest one: with a full suggestion list and
// Custom selected (the longest lifetime block) it must still fit 24 rows.
func TestLifetimeRobotFormWithFullSuggestionListFits(t *testing.T) {
	for _, size := range lifetimeViewSizes {
		t.Run(sizeLabel(size.w, size.h), func(t *testing.T) {
			client := newTTLClient()
			m := newAdminReadyModelWithCatalog(t, []string{"a/1", "a/2", "a/3", "a/4", "a/5", "a/6", "a/7"}, client)
			m.now = func() time.Time { return charFixedTime }
			m = charResize(t, m, size.w, size.h)
			m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: charFixedTime.Add(24 * time.Hour)}
			m.adminAuth = adminAuthStateAuthenticated
			m.screen = screenAdminCreateRobot
			m = testSendKeys(t, m, testKeyRunes("x"), testKeyTab(), testKeyTab(), testKeyTab())
			m = testSendKeys(t, m, withCustom("3", "y")...)
			rendered := ansi.Strip(m.View().Content)
			name := "robot_full_suggestions_custom_invalid_" + sizeLabel(size.w, size.h)
			assertLifetimeNoOverflow(t, name, rendered, size.w, size.h)
			assertLifetimeGolden(t, name, rendered)
		})
	}
}
