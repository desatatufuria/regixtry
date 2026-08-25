package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"regixtry/internal/domain/signing"
)

func trustedKeyListTestKey(runes string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(runes)}
}

const trustedKeyListTestPEM1 = "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAErSUZU4IUnh+IonVC8RB3NSN1lQLe\nxnF8AMhBjtuWIOnJjBgfpVuckOjRgdcXwUr/GtUnkVEeSGvmLC4F3sr/Cw==\n-----END PUBLIC KEY-----\n"
const trustedKeyListTestPEM2 = "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE05HiE6xnaIc5rwUh4Ss78iRLVzix\nGWu5bNe7ig1IsxcuyxdTcDnhqJPPfjnownBfbM8b9jZLMopp6MmPX3JDLw==\n-----END PUBLIC KEY-----\n"

func TestTrustedKeyListUpDownNavigationIsBounded(t *testing.T) {
	t.Parallel()

	list := newTrustedKeyList("", []string{trustedKeyListTestPEM1, trustedKeyListTestPEM2})
	env := screenEnv{}

	next, cmd, consumed := list.update(env, tea.KeyMsg{Type: tea.KeyUp})
	if !consumed || cmd != nil {
		t.Fatalf("Up: consumed = %v, cmd = %v, want consumed=true, cmd=nil", consumed, cmd)
	}
	if next.selected != 0 {
		t.Fatalf("selected = %d, want 0 (bounded at zero)", next.selected)
	}

	next, _, _ = next.update(env, tea.KeyMsg{Type: tea.KeyDown})
	if next.selected != 1 {
		t.Fatalf("selected = %d, want 1", next.selected)
	}
	next, _, _ = next.update(env, tea.KeyMsg{Type: tea.KeyDown})
	if next.selected != 1 {
		t.Fatalf("selected = %d, want 1 (bounded at len-1)", next.selected)
	}
}

func TestTrustedKeyListUnhandledKeyIsNotConsumed(t *testing.T) {
	t.Parallel()

	list := newTrustedKeyList("", nil)
	next, cmd, consumed := list.update(screenEnv{}, tea.KeyMsg{Type: tea.KeyTab})
	if consumed || cmd != nil {
		t.Fatalf("Tab: consumed = %v, cmd = %v, want consumed=false, cmd=nil (falls through to the host screen)", consumed, cmd)
	}
	if next.adding {
		t.Fatal("Tab must not enter adding mode")
	}
}

func TestTrustedKeyListAddFlowCommitsAValidKey(t *testing.T) {
	t.Parallel()

	list := newTrustedKeyList("", nil)
	env := screenEnv{}

	list, _, consumed := list.update(env, trustedKeyListTestKey("n"))
	if !consumed || !list.adding {
		t.Fatalf("'n': consumed = %v, adding = %v, want consumed=true, adding=true", consumed, list.adding)
	}

	for _, r := range trustedKeyListTestPEM1 {
		list, _, _ = list.update(env, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	list, cmd, consumed := list.update(env, tea.KeyMsg{Type: tea.KeyEnter})
	if !consumed || cmd != nil {
		t.Fatalf("Enter (commit add): consumed = %v, cmd = %v, want consumed=true, cmd=nil", consumed, cmd)
	}
	if list.adding {
		t.Fatal("adding = true after a successful commit, want false")
	}
	if len(list.keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(list.keys))
	}
	if list.err != "" {
		t.Fatalf("err = %q, want empty after a successful commit", list.err)
	}
}

// TestTrustedKeyListAddFlowEnterWithoutMarkersDoesNotCommit covers the
// paste-in-progress case: input with neither a BEGIN nor an END marker yet
// (e.g. an operator who has typed/pasted only a fragment so far) does not
// look like a complete PEM, so Enter must be treated as part of the input
// (appending a newline) rather than a premature submit -- no validation
// attempt, no error, still adding. This replaces the pre-fix expectation
// that any non-PEM input rejects immediately on Enter: now that only
// applies once both markers are present (see
// TestTrustedKeyListAddFlowRejectsGenuinelyMalformedCompletePEM below).
func TestTrustedKeyListAddFlowEnterWithoutMarkersDoesNotCommit(t *testing.T) {
	t.Parallel()

	list := newTrustedKeyList("", nil)
	env := screenEnv{}

	list, _, _ = list.update(env, trustedKeyListTestKey("n"))
	list, _, _ = list.update(env, trustedKeyListTestKey("not a pem"))
	list, cmd, consumed := list.update(env, tea.KeyMsg{Type: tea.KeyEnter})

	if !consumed || cmd != nil {
		t.Fatalf("Enter (no markers yet): consumed = %v, cmd = %v, want consumed=true, cmd=nil", consumed, cmd)
	}
	if !list.adding {
		t.Fatal("adding = false after Enter with no PEM markers yet, want true (still accumulating)")
	}
	if list.err != "" {
		t.Fatalf("err = %q, want empty (no commit attempted without both markers)", list.err)
	}
	if list.input != "not a pem\n" {
		t.Fatalf("input = %q, want %q (Enter appended as a newline)", list.input, "not a pem\n")
	}
	if len(list.keys) != 0 {
		t.Fatalf("len(keys) = %d, want 0", len(list.keys))
	}
}

// TestTrustedKeyListAddFlowRejectsGenuinelyMalformedCompletePEM is the
// updated regression for a real validation failure (constraint: "a
// genuinely malformed key must still surface commitAdd's existing
// validation error correctly"): once input already contains BOTH a BEGIN
// and an END marker, Enter is treated as a genuine submit and a bad body
// still fails signing.NormalizePublicKeyPEM, staying in adding mode with
// the error surfaced -- exactly as before this change for input that looks
// complete.
func TestTrustedKeyListAddFlowRejectsGenuinelyMalformedCompletePEM(t *testing.T) {
	t.Parallel()

	list := newTrustedKeyList("", nil)
	env := screenEnv{}

	list, _, _ = list.update(env, trustedKeyListTestKey("n"))
	list, _, _ = list.update(env, trustedKeyListTestKey("-----BEGIN PUBLIC KEY----- not valid base64 -----END PUBLIC KEY-----"))
	list, _, consumed := list.update(env, tea.KeyMsg{Type: tea.KeyEnter})

	if !consumed {
		t.Fatal("Enter (rejected commit): consumed = false, want true")
	}
	if !list.adding {
		t.Fatal("adding = false after a rejected commit, want true (stays in adding mode)")
	}
	if list.err == "" {
		t.Fatal("err = \"\", want a validation error surfaced")
	}
	if len(list.keys) != 0 {
		t.Fatalf("len(keys) = %d, want 0 (nothing appended on a rejected commit)", len(list.keys))
	}
}

// trustedKeyListTestTypeLine drives a sequence of tea.KeyRunes events for
// each rune in line, mirroring how a real terminal delivers typed/pasted
// text one rune-batch at a time.
func trustedKeyListTestTypeLine(t *testing.T, env screenEnv, list trustedKeyList, line string) trustedKeyList {
	t.Helper()
	for _, r := range line {
		var consumed bool
		list, _, consumed = list.update(env, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if !consumed {
			t.Fatalf("rune %q not consumed while adding", r)
		}
	}
	return list
}

// TestTrustedKeyListMultilinePasteSurvivesEmbeddedNewlineEnterEvents is the
// RED test for the "premature commit on the first embedded newline" bug:
// a raw terminal without bracketed-paste support delivers each newline
// byte inside a pasted multi-line PEM as its own tea.KeyEnter event. Before
// the fix, the very first such Enter (right after "-----BEGIN PUBLIC
// KEY-----") would call commitAdd prematurely and fail validation. After
// the fix, Enter is only treated as a real submit once the accumulated
// input already contains both markers, so the paste survives intact and
// the key is added successfully once the final Enter arrives.
func TestTrustedKeyListMultilinePasteSurvivesEmbeddedNewlineEnterEvents(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	list := newTrustedKeyList("", nil)
	list, _, _ = list.update(env, trustedKeyListTestKey("n"))

	pemLines := strings.Split(strings.TrimRight(trustedKeyListTestPEM1, "\n"), "\n")
	for i, line := range pemLines {
		list = trustedKeyListTestTypeLine(t, env, list, line)
		next, cmd, consumed := list.update(env, tea.KeyMsg{Type: tea.KeyEnter})
		if !consumed || cmd != nil {
			t.Fatalf("line %d Enter: consumed = %v, cmd = %v, want consumed=true, cmd=nil", i, consumed, cmd)
		}
		list = next
		if i < len(pemLines)-1 {
			if !list.adding {
				t.Fatalf("line %d: adding = false, want true (paste not yet complete)", i)
			}
			if list.err != "" {
				t.Fatalf("line %d: err = %q, want empty (no premature commit attempt)", i, list.err)
			}
		}
	}

	if list.adding {
		t.Fatal("adding = true after the final Enter, want false (paste committed)")
	}
	if list.err != "" {
		t.Fatalf("err = %q, want empty after a successful commit", list.err)
	}
	if len(list.keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(list.keys))
	}
	wantNormalized, err := signing.NormalizePublicKeyPEM(trustedKeyListTestPEM1)
	if err != nil {
		t.Fatalf("test setup: NormalizePublicKeyPEM(trustedKeyListTestPEM1) failed: %v", err)
	}
	if list.keys[0] != wantNormalized {
		t.Fatalf("keys[0] = %q, want %q", list.keys[0], wantNormalized)
	}
}

// TestTrustedKeyListSingleLinePasteStillCommitsOnFirstEnter is the
// regression test: an operator who already stripped newlines before
// pasting (both markers on one line) must still commit immediately on the
// very first Enter, exactly as before this change, since
// looksLikeCompletePEM is already true at that point.
func TestTrustedKeyListSingleLinePasteStillCommitsOnFirstEnter(t *testing.T) {
	t.Parallel()

	env := screenEnv{}
	list := newTrustedKeyList("", nil)
	list, _, _ = list.update(env, trustedKeyListTestKey("n"))

	singleLine := strings.Join(strings.Fields(trustedKeyListTestPEM1), " ")
	list = trustedKeyListTestTypeLine(t, env, list, singleLine)

	list, cmd, consumed := list.update(env, tea.KeyMsg{Type: tea.KeyEnter})
	if !consumed || cmd != nil {
		t.Fatalf("Enter (single-line commit): consumed = %v, cmd = %v, want consumed=true, cmd=nil", consumed, cmd)
	}
	if list.adding {
		t.Fatal("adding = true after the first Enter on a single-line paste, want false (committed immediately)")
	}
	if list.err != "" {
		t.Fatalf("err = %q, want empty after a successful commit", list.err)
	}
	if len(list.keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(list.keys))
	}
}

func TestTrustedKeyListEscWhileAddingCancelsWithoutAdding(t *testing.T) {
	t.Parallel()

	list := newTrustedKeyList("", nil)
	env := screenEnv{}

	list, _, _ = list.update(env, trustedKeyListTestKey("n"))
	list, _, _ = list.update(env, trustedKeyListTestKey("partial input"))
	list, cmd, consumed := list.update(env, tea.KeyMsg{Type: tea.KeyEsc})

	if !consumed || cmd != nil {
		t.Fatalf("Esc (cancel add): consumed = %v, cmd = %v, want consumed=true, cmd=nil", consumed, cmd)
	}
	if list.adding {
		t.Fatal("adding = true after Esc, want false")
	}
	if list.input != "" {
		t.Fatalf("input = %q after Esc, want empty", list.input)
	}
	if len(list.keys) != 0 {
		t.Fatalf("len(keys) = %d, want 0", len(list.keys))
	}
}

func TestTrustedKeyListDeleteWithNoKeysIsNoOp(t *testing.T) {
	t.Parallel()

	list := newTrustedKeyList("", nil)
	next, cmd, consumed := list.update(screenEnv{}, trustedKeyListTestKey("x"))
	if !consumed || cmd != nil {
		t.Fatalf("'x' on empty list: consumed = %v, cmd = %v, want consumed=true, cmd=nil (no-op)", consumed, cmd)
	}
	if next.usageLoading {
		t.Fatal("usageLoading = true on an empty list, want false")
	}
}

func TestTrustedKeyListDeleteFiresUsageCmdAndOpensConfirmOnResponse(t *testing.T) {
	t.Parallel()

	list := newTrustedKeyList("team/api", []string{trustedKeyListTestPEM1})
	env := screenEnv{}

	list, cmd, consumed := list.update(env, trustedKeyListTestKey("x"))
	if !consumed || cmd == nil {
		t.Fatalf("'x': consumed = %v, cmd = %v, want consumed=true, cmd != nil", consumed, cmd)
	}
	if !list.usageLoading {
		t.Fatal("usageLoading = false immediately after 'x', want true")
	}
	if list.confirm.Active() {
		t.Fatal("confirm active before the usage response arrives")
	}

	list = list.applyUsageLoaded(adminSigningKeyUsageLoadedMsg{count: 3, capped: false})
	if list.usageLoading {
		t.Fatal("usageLoading = true after the response, want false")
	}
	if !list.confirm.Active() {
		t.Fatal("confirm not active after the usage response arrives")
	}
}

// TestTrustedKeyListDeleteNeverBlocksOnAHighCount is THE design requirement
// under test: no count, however high, ever prevents the confirm from
// opening -- deletion is only ever informed, never blocked.
func TestTrustedKeyListDeleteNeverBlocksOnAHighCount(t *testing.T) {
	t.Parallel()

	list := newTrustedKeyList("", []string{trustedKeyListTestPEM1})
	list, _, _ = list.update(screenEnv{}, trustedKeyListTestKey("x"))
	list = list.applyUsageLoaded(adminSigningKeyUsageLoadedMsg{count: 9999, capped: true})

	if !list.confirm.Active() {
		t.Fatal("confirm not active for a high, capped usage count -- deletion must never be blocked, only informed")
	}
}

func TestTrustedKeyListConfirmEnterRemovesExactlyTheSelectedKey(t *testing.T) {
	t.Parallel()

	list := newTrustedKeyList("", []string{trustedKeyListTestPEM1, trustedKeyListTestPEM2})
	env := screenEnv{}

	list.selected = 0
	list, _, _ = list.update(env, trustedKeyListTestKey("x"))
	list = list.applyUsageLoaded(adminSigningKeyUsageLoadedMsg{count: 0, capped: false})

	list, cmd, consumed := list.update(env, tea.KeyMsg{Type: tea.KeyEnter})
	if !consumed || cmd != nil {
		t.Fatalf("Enter (confirm delete): consumed = %v, cmd = %v, want consumed=true, cmd=nil", consumed, cmd)
	}
	if list.confirm.Active() {
		t.Fatal("confirm still active after Enter, want closed")
	}
	if len(list.keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1 (exactly one entry removed)", len(list.keys))
	}
	if list.keys[0] != trustedKeyListTestPEM2 {
		t.Fatal("the wrong key was removed")
	}
}

func TestTrustedKeyListConfirmEscCancelsWithoutRemoving(t *testing.T) {
	t.Parallel()

	list := newTrustedKeyList("", []string{trustedKeyListTestPEM1})
	env := screenEnv{}

	list, _, _ = list.update(env, trustedKeyListTestKey("x"))
	list = list.applyUsageLoaded(adminSigningKeyUsageLoadedMsg{count: 0, capped: false})

	list, _, consumed := list.update(env, tea.KeyMsg{Type: tea.KeyEsc})
	if !consumed {
		t.Fatal("Esc (cancel delete): consumed = false, want true")
	}
	if list.confirm.Active() {
		t.Fatal("confirm still active after Esc, want closed")
	}
	if len(list.keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1 (nothing removed on cancel)", len(list.keys))
	}
}

func TestTrustedKeyListSetKeysReplacesWholesale(t *testing.T) {
	t.Parallel()

	list := newTrustedKeyList("", []string{trustedKeyListTestPEM1})
	list = list.SetKeys([]string{trustedKeyListTestPEM2})
	if len(list.Keys()) != 1 || list.Keys()[0] != trustedKeyListTestPEM2 {
		t.Fatalf("Keys() = %#v, want [%q]", list.Keys(), trustedKeyListTestPEM2)
	}
}
