package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)} }
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type backoffHarness struct {
	service *Service
	clock   *fakeClock
	logs    *bytes.Buffer
	calls   int
}

// newBackoffHarness builds a service over the "alice" fixture whose bcrypt
// comparison is replaced by a counter that accepts only "right-password".
func newBackoffHarness(t *testing.T, mode BackoffMode) *backoffHarness {
	t.Helper()
	store, _ := loginFixtureStore(t)
	h := &backoffHarness{clock: newFakeClock(), logs: &bytes.Buffer{}}
	h.service = NewService(store)
	h.service.ConfigureLogin(LoginPolicy{
		Logger: log.New(h.logs, "", 0),
		Compare: func(hash, password []byte) error {
			h.calls++
			if string(password) == "right-password" {
				return nil
			}
			return errors.New("mismatch")
		},
		Backoff: &BackoffPolicy{Mode: mode, Threshold: 5, Base: 2 * time.Second, Max: 300 * time.Second, Now: h.clock.Now},
	})
	return h
}

func (h *backoffHarness) login(user, pass string) error {
	_, err := h.service.LoginWithPassword(context.Background(), user, pass, nil)
	return err
}

func retryAfter(t *testing.T, err error) time.Duration {
	t.Helper()
	var throttled *ports.LoginThrottledError
	if !errors.As(err, &throttled) {
		t.Fatalf("err = %v, want a *ports.LoginThrottledError", err)
	}
	return throttled.RetryAfter
}

func retryAfterOrZero(err error) time.Duration {
	var throttled *ports.LoginThrottledError
	if errors.As(err, &throttled) {
		return throttled.RetryAfter
	}
	return 0
}

func TestBackoffScheduleAfterThreshold(t *testing.T) {
	t.Parallel()
	h := newBackoffHarness(t, BackoffEnforce)

	for i := 1; i <= 5; i++ {
		err := h.login("alice", "nope")
		if !domainauth.IsCode(err, domainauth.ErrorCodeInvalidCredentials) {
			t.Fatalf("failure %d: err = %v, want invalid credentials", i, err)
		}
	}
	// After the 5th consecutive failure the next attempt waits base (2s),
	// then 4s, 8s, ... capped at 300s.
	wants := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, 64 * time.Second, 128 * time.Second, 256 * time.Second, 300 * time.Second, 300 * time.Second}
	for i, want := range wants {
		err := h.login("alice", "right-password") // never verified while blocked
		if got := retryAfter(t, err); got != want {
			t.Fatalf("step %d: Retry-After = %v, want %v", i, got, want)
		}
		h.clock.Advance(want - time.Second)
		if got := retryAfter(t, h.login("alice", "right-password")); got != time.Second {
			t.Fatalf("step %d: 1s before the window ends, Retry-After = %v, want 1s", i, got)
		}
		h.clock.Advance(time.Second)
		if err := h.login("alice", "nope"); !domainauth.IsCode(err, domainauth.ErrorCodeInvalidCredentials) {
			t.Fatalf("step %d: after the window the attempt is verified (401), got %v", i, err)
		}
	}
}

func TestBackoffNeverVerifiesThePasswordWhileBlocked(t *testing.T) {
	t.Parallel()
	h := newBackoffHarness(t, BackoffEnforce)
	for i := 0; i < 5; i++ {
		_ = h.login("alice", "nope")
	}
	before := h.calls
	for i := 0; i < 20; i++ {
		_ = h.login("alice", "right-password")
	}
	if h.calls != before {
		t.Fatalf("bcrypt ran %d times while the account was in backoff, want 0", h.calls-before)
	}
}

func TestBackoffResetsOnSuccessAndAfterIdle(t *testing.T) {
	t.Parallel()
	h := newBackoffHarness(t, BackoffEnforce)

	for i := 0; i < 4; i++ {
		_ = h.login("alice", "nope")
	}
	if err := h.login("alice", "right-password"); err != nil {
		t.Fatalf("login = %v, want success below the threshold", err)
	}
	for i := 0; i < 4; i++ { // would be the 8th failure if success had not reset
		if err := h.login("alice", "nope"); !domainauth.IsCode(err, domainauth.ErrorCodeInvalidCredentials) {
			t.Fatalf("after a success the counter restarts, got %v", err)
		}
	}

	_ = h.login("alice", "nope") // 5th: blocked next
	if retryAfterOrZero(h.login("alice", "nope")) == 0 {
		t.Fatal("expected backoff after 5 failures")
	}
	h.clock.Advance(15 * time.Minute)
	if err := h.login("alice", "right-password"); err != nil {
		t.Fatalf("after 15 minutes without failures the state is forgotten, got %v", err)
	}
}

func TestBackoffKeyIsTheNormalizedUsername(t *testing.T) {
	t.Parallel()
	h := newBackoffHarness(t, BackoffEnforce)
	for _, name := range []string{"alice", "ALICE", " Alice ", "aLiCe", "alice"} {
		_ = h.login(name, "nope")
	}
	if retryAfterOrZero(h.login("ALICE ", "nope")) == 0 {
		t.Fatal("case and surrounding spaces must share one bucket")
	}
}

func TestBackoffIsUniformForUnknownAndExistingUsers(t *testing.T) {
	t.Parallel()
	h := newBackoffHarness(t, BackoffEnforce)
	for _, name := range []string{"alice", "ghost"} {
		for i := 0; i < 5; i++ {
			if err := h.login(name, "nope"); !domainauth.IsCode(err, domainauth.ErrorCodeInvalidCredentials) {
				t.Fatalf("%s failure %d: %v", name, i, err)
			}
		}
		if got := retryAfter(t, h.login(name, "nope")); got != 2*time.Second {
			t.Fatalf("%s: Retry-After = %v, want 2s", name, got)
		}
	}
}

func TestTokenShapedSecretsNeverCountOrAreGated(t *testing.T) {
	t.Parallel()
	h := newBackoffHarness(t, BackoffEnforce)
	wrongToken := strings.Repeat("ab", 32)
	for i := 0; i < 50; i++ {
		if err := h.login("alice", wrongToken); !domainauth.IsCode(err, domainauth.ErrorCodeInvalidCredentials) {
			t.Fatalf("wrong 64-hex #%d: %v", i, err)
		}
	}
	if err := h.login("alice", "right-password"); err != nil {
		t.Fatalf("an attacker's wrong 64-hex secrets must not put the account in backoff: %v", err)
	}
	// And while the account IS in backoff, a token-shaped attempt is not gated.
	for i := 0; i < 5; i++ {
		_ = h.login("alice", "nope")
	}
	if err := h.login("alice", wrongToken); !domainauth.IsCode(err, domainauth.ErrorCodeInvalidCredentials) {
		t.Fatalf("token-shaped attempt during backoff: %v", err)
	}
}

func TestLogOnlyModeLogsWouldHaveBlockedButNeverBlocks(t *testing.T) {
	t.Parallel()
	h := newBackoffHarness(t, BackoffLog)
	for i := 0; i < 5; i++ {
		_ = h.login("alice", "nope")
		_ = h.login("Mallory\ninjected", "nope")
	}
	if err := h.login("alice", "nope"); !domainauth.IsCode(err, domainauth.ErrorCodeInvalidCredentials) {
		t.Fatalf("log-only mode must keep the uniform 401, got %v", err)
	}
	if err := h.login("alice", "right-password"); err != nil {
		t.Fatalf("log-only mode must not block a correct password: %v", err)
	}
	_ = h.login("Mallory\ninjected", "nope")

	out := h.logs.String()
	if !strings.Contains(out, "would have blocked") {
		t.Fatalf("missing 'would have blocked' log:\n%s", out)
	}
	if !strings.Contains(out, "account=alice") {
		t.Errorf("an existing account is logged by normalized name:\n%s", out)
	}
	if strings.Contains(out, "mallory") || strings.Contains(strings.ToLower(out), "injected") {
		t.Errorf("an unknown username must be logged as a hash only:\n%s", out)
	}
	if !strings.Contains(out, "account_hash=") {
		t.Errorf("missing account_hash for the unknown username:\n%s", out)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(line, "login backoff") {
			t.Errorf("unexpected line (log injection?): %q", line)
		}
	}
}

func TestBackoffDisabledByThresholdZeroOrModeOff(t *testing.T) {
	t.Parallel()
	for name, policy := range map[string]*BackoffPolicy{
		"threshold 0": {Mode: BackoffEnforce, Threshold: 0},
		"mode off":    {Mode: BackoffOff, Threshold: 5},
	} {
		t.Run(name, func(t *testing.T) {
			h := newBackoffHarness(t, BackoffEnforce)
			policy.Now = h.clock.Now
			h.service.ConfigureLogin(LoginPolicy{Compare: h.service.comparePassword, Backoff: policy})
			for i := 0; i < 50; i++ {
				if err := h.login("alice", "nope"); !domainauth.IsCode(err, domainauth.ErrorCodeInvalidCredentials) {
					t.Fatalf("attempt %d: %v", i, err)
				}
			}
			if err := h.login("alice", "right-password"); err != nil {
				t.Fatalf("disabled backoff must never block: %v", err)
			}
		})
	}
}

func TestBackoffStateIsBoundedByAnLRU(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	b := newLoginBackoff(BackoffPolicy{Mode: BackoffEnforce, Threshold: 1, Base: time.Second, Max: time.Minute, MaxKeys: 100, Now: clock.Now})
	for i := 0; i < 10_000; i++ {
		b.fail(fmt.Sprintf("user-%d", i))
	}
	if got := b.size(); got != 100 {
		t.Fatalf("tracked keys = %d, want exactly the 100 most recent", got)
	}
	if b.check("user-0") != 0 {
		t.Fatal("the oldest key must have been evicted")
	}
	if b.check("user-9999") == 0 {
		t.Fatal("the newest key must still be tracked")
	}
	if got := newLoginBackoff(BackoffPolicy{Mode: BackoffEnforce}).maxKeys(); got != 10_000 {
		t.Fatalf("default LRU size = %d, want 10000", got)
	}
}

func TestBackoffIsRaceFree(t *testing.T) {
	t.Parallel()
	b := newLoginBackoff(BackoffPolicy{Mode: BackoffEnforce, Threshold: 3, Base: time.Millisecond, Max: 10 * time.Millisecond, MaxKeys: 50})
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				key := fmt.Sprintf("u%d", (g+i)%80)
				b.check(key)
				b.fail(key)
				if i%7 == 0 {
					b.success(key)
				}
			}
		}(g)
	}
	wg.Wait()
	if b.size() > 50 {
		t.Fatalf("size = %d > 50", b.size())
	}
}
