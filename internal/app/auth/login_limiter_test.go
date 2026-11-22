package auth

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

func TestAutoLoginConcurrencyIsHalfTheCPUsWithAFloorOfTwo(t *testing.T) {
	t.Parallel()

	cases := map[int]int{1: 2, 2: 2, 3: 2, 4: 2, 6: 3, 8: 4, 64: 32}
	for cpus, want := range cases {
		if got := autoLoginConcurrency(cpus); got != want {
			t.Errorf("autoLoginConcurrency(%d) = %d, want %d", cpus, got, want)
		}
	}
	if got := newLoginLimiter(0, 0).capacity(); got != autoLoginConcurrency(runtime.NumCPU()) {
		t.Fatalf("capacity for 0 = %d, want automatic %d", got, autoLoginConcurrency(runtime.NumCPU()))
	}
	if got := newLoginLimiter(7, 0).capacity(); got != 7 {
		t.Fatalf("explicit capacity = %d, want 7", got)
	}
}

func TestLoginLimiterBoundsConcurrentPasswordComparisons(t *testing.T) {
	t.Parallel()

	store, _ := loginFixtureStore(t)
	service := NewService(store)
	var inFlight, peak, calls atomic.Int64
	release := make(chan struct{})
	service.comparePassword = func(h, p []byte) error {
		calls.Add(1)
		n := inFlight.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		<-release
		inFlight.Add(-1)
		return errors.New("mismatch")
	}
	service.ConfigureLogin(LoginPolicy{MaxConcurrency: 2, MaxWait: 40 * time.Millisecond})

	var wg sync.WaitGroup
	var refused, rejected atomic.Int64
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.LoginWithPassword(context.Background(), "alice", "wrong-password", nil)
			var throttled *ports.LoginThrottledError
			switch {
			case errors.As(err, &throttled):
				refused.Add(1)
				if throttled.RetryAfter < time.Second {
					t.Errorf("RetryAfter = %v, want at least 1s", throttled.RetryAfter)
				}
			case domainauth.IsCode(err, domainauth.ErrorCodeInvalidCredentials):
				rejected.Add(1)
			default:
				t.Errorf("unexpected error %v", err)
			}
		}()
	}
	// Keep both slots busy until every other attempt has been refused (the
	// first bcrypt-cost dummy hash can take seconds under -race, so poll
	// instead of sleeping).
	deadline := time.Now().Add(30 * time.Second)
	for refused.Load() < 10 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	wg.Wait()

	if peak.Load() > 2 {
		t.Fatalf("peak concurrent comparisons = %d, want <= 2", peak.Load())
	}
	if calls.Load() != 2 || rejected.Load() != 2 || refused.Load() != 10 {
		t.Fatalf("calls=%d rejected=%d refused=%d, want 2/2/10 (refused attempts never reach bcrypt)", calls.Load(), rejected.Load(), refused.Load())
	}
}

func TestLoginLimiterReleasesSlotsAndWaitsBrieflyForOne(t *testing.T) {
	t.Parallel()

	store, _ := loginFixtureStore(t)
	service := NewService(store)
	var calls atomic.Int64
	service.comparePassword = func(h, p []byte) error {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		return errors.New("mismatch")
	}
	service.ConfigureLogin(LoginPolicy{MaxConcurrency: 1, MaxWait: time.Second})

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.LoginWithPassword(context.Background(), "alice", "wrong-password", nil); !domainauth.IsCode(err, domainauth.ErrorCodeInvalidCredentials) {
				t.Errorf("a waiter within the max wait must be served, got %v", err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 5 {
		t.Fatalf("calls = %d, want all 5 served in turn", calls.Load())
	}
}

func TestLoginLimiterHonoursContextCancellation(t *testing.T) {
	t.Parallel()

	store, _ := loginFixtureStore(t)
	service := NewService(store)
	hold := make(chan struct{})
	service.comparePassword = func(h, p []byte) error { <-hold; return errors.New("mismatch") }
	service.ConfigureLogin(LoginPolicy{MaxConcurrency: 1, MaxWait: time.Minute})
	go func() { _, _ = service.LoginWithPassword(context.Background(), "alice", "wrong-password", nil) }()
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := service.LoginWithPassword(ctx, "alice", "wrong-password", nil); done <- err }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled waiter must not succeed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled waiter kept waiting for a slot")
	}
	close(hold)
}

func TestIsCredentialTokenShaped(t *testing.T) {
	t.Parallel()

	good := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if !ports.IsCredentialTokenShaped(good) {
		t.Fatal("64 lowercase hex must be token shaped")
	}
	for _, bad := range []string{"", good[:63], good + "0", "0123456789ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef", good[:63] + "g", " " + good[:63]} {
		if ports.IsCredentialTokenShaped(bad) {
			t.Errorf("%q must not be token shaped", bad)
		}
	}
}

// loginFixtureStore returns a memory store with the user "alice" whose
// password is "password-for-alice".
func loginFixtureStore(t *testing.T) (*memoryAuthStore, domainauth.User) {
	t.Helper()
	store := newMemoryAuthStore()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	user := domainauth.User{ID: "u1", Username: "alice", PasswordHash: mustHashPassword(t, "password-for-alice"), Enabled: true, CreatedAt: now, UpdatedAt: now}
	store.usersByID[user.ID] = user
	store.usersByUsername[user.Username] = user
	return store, user
}
