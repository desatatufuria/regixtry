package regixtryhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	appauth "regixtry/internal/app/auth"
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

type httpClock struct{ now atomic.Int64 }

func newHTTPClock() *httpClock {
	c := &httpClock{}
	c.now.Store(time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC).UnixNano())
	return c
}
func (c *httpClock) Now() time.Time          { return time.Unix(0, c.now.Load()).UTC() }
func (c *httpClock) Advance(d time.Duration) { c.now.Add(int64(d)) }

func backoffRouter(t *testing.T, mode appauth.BackoffMode, clock *httpClock) (*Router, *appauth.Service, domainauth.Principal, func()) {
	t.Helper()
	handler, authService, actor, _, cleanup := newTestRouterWithRealAuthOptions(t, func(s *appauth.Service) {
		s.ConfigureLogin(appauth.LoginPolicy{
			Compare: func(h, p []byte) error {
				if strings.HasPrefix(string(p), "ok-") || string(p) == "password123" {
					return nil
				}
				return errors.New("mismatch")
			},
			Backoff: &appauth.BackoffPolicy{Mode: mode, Threshold: 5, Base: 2 * time.Second, Max: 300 * time.Second, Now: clock.Now},
		})
	})
	return handler, authService, actor, cleanup
}

func TestDefaultModeNeverRefusesALogin(t *testing.T) {
	t.Parallel()
	// No ConfigureLogin: the shipped default is log-only.
	handler, _, _, _, cleanup := newTestRouterWithRealAuthOptions(t, nil)
	defer cleanup()
	for i := 0; i < 12; i++ {
		if rec := tokenExchange(handler, "admin", "wrong-password"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, want 401 in the default (log-only) mode", i, rec.Code)
		}
	}
	if rec := tokenExchange(handler, "admin", "password123"); rec.Code != http.StatusOK {
		t.Fatalf("correct password after failures: %d", rec.Code)
	}
}

func TestEnforcedBackoffReturns429WithRetryAfterAndRecovers(t *testing.T) {
	t.Parallel()
	clock := newHTTPClock()
	handler, authService, actor, cleanup := backoffRouter(t, appauth.BackoffEnforce, clock)
	defer cleanup()
	if _, err := authService.CreateAdminUser(context.Background(), actor, ports.AdminCreateUserInput{Username: "dana", Password: "ok-dana-password", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if rec := tokenExchange(handler, "dana", "bad-password"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("free failure %d: %d", i, rec.Code)
		}
	}
	rec := tokenExchange(handler, "dana", "ok-dana-password")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "2" || !strings.Contains(rec.Body.String(), `"code":"TOOMANYREQUESTS"`) {
		t.Fatalf("in backoff: %d Retry-After=%q body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
	clock.Advance(2 * time.Second)
	if rec := tokenExchange(handler, "dana", "ok-dana-password"); rec.Code != http.StatusOK {
		t.Fatalf("after the window a correct password works: %d %s", rec.Code, rec.Body.String())
	}
	if rec := tokenExchange(handler, "dana", "bad-password"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a success resets the counter: %d", rec.Code)
	}
}

// TestBackoffAntiEnumeration extends the auth_token_oracle_test pattern: before
// and during backoff, existing, unknown, disabled and robot accounts answer
// with the same status, body and headers.
func TestBackoffAntiEnumeration(t *testing.T) {
	t.Parallel()
	clock := newHTTPClock()
	handler, authService, actor, cleanup := backoffRouter(t, appauth.BackoffEnforce, clock)
	defer cleanup()
	ctx := context.Background()
	for _, name := range []string{"active", "gone"} {
		if _, err := authService.CreateAdminUser(ctx, actor, ports.AdminCreateUserInput{Username: name, Password: "ok-password-1", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	users, err := authService.ListAdminUsers(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if u.Username == "gone" {
			if _, err := authService.DisableAdminUser(ctx, actor, u.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	robot, err := authService.CreateAdminRobot(ctx, actor, ports.AdminCreateRobotInput{Name: "bot", Repository: "team/app", Role: domainauth.RepoRoleWriter})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"active", "nobody", "gone", robot.Robot.Username}

	snapshot := func(user string) [3]string {
		rec := tokenExchange(handler, user, "bad-password")
		return [3]string{http.StatusText(rec.Code), rec.Body.String(), rec.Header().Get("WWW-Authenticate") + "|" + rec.Header().Get("Retry-After")}
	}
	// Phase 1: free failures, all identical.
	for i := 0; i < 5; i++ {
		want := snapshot(names[0])
		for _, name := range names[1:] {
			if got := snapshot(name); got != want {
				t.Fatalf("failure %d for %q differs: %v vs %v", i, name, got, want)
			}
		}
	}
	// Phase 2: every one of them is now in backoff, identically.
	want := snapshot(names[0])
	if want[0] != http.StatusText(http.StatusTooManyRequests) {
		t.Fatalf("expected 429 during backoff, got %v", want)
	}
	for _, name := range names[1:] {
		if got := snapshot(name); got != want {
			t.Fatalf("backoff response for %q differs: %v vs %v", name, got, want)
		}
	}
	// Even the correct password gets the same answer while blocked.
	if rec := tokenExchange(handler, "active", "ok-password-1"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("correct password during backoff: %d", rec.Code)
	}
}

func TestRobotTokenExchangeIsNeverGatedAndWrongHexNeverCountsAgainstTheAccount(t *testing.T) {
	t.Parallel()
	clock := newHTTPClock()
	handler, authService, actor, cleanup := backoffRouter(t, appauth.BackoffEnforce, clock)
	defer cleanup()
	ctx := context.Background()
	robot, err := authService.CreateAdminRobot(ctx, actor, ports.AdminCreateRobotInput{Name: "deploy", Repository: "team/app", Role: domainauth.RepoRoleWriter})
	if err != nil {
		t.Fatal(err)
	}
	// An attacker sprays wrong 64-hex secrets and non-hex guesses at the robot.
	for i := 0; i < 40; i++ {
		tokenExchange(handler, robot.Robot.Username, strings.Repeat("ab", 32))
	}
	if rec := tokenExchange(handler, robot.Robot.Username, robot.Secret); rec.Code != http.StatusOK {
		t.Fatalf("wrong 64-hex secrets must not lock the robot out: %d %s", rec.Code, rec.Body.String())
	}
	// Even with the robot's name in backoff from non-token guesses, its token works.
	for i := 0; i < 8; i++ {
		tokenExchange(handler, robot.Robot.Username, "bad-password")
	}
	if rec := tokenExchange(handler, robot.Robot.Username, "bad-password"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("test assumption: expected backoff, got %d", rec.Code)
	}
	if rec := tokenExchange(handler, robot.Robot.Username, robot.Secret); rec.Code != http.StatusOK {
		t.Fatalf("a valid robot token must never be gated: %d %s", rec.Code, rec.Body.String())
	}
}

func TestBackoffIsPerUsernameNotPerIP(t *testing.T) {
	t.Parallel()
	clock := newHTTPClock()
	handler, authService, actor, cleanup := backoffRouter(t, appauth.BackoffEnforce, clock)
	defer cleanup()
	for _, name := range []string{"one", "two"} {
		if _, err := authService.CreateAdminUser(context.Background(), actor, ports.AdminCreateUserInput{Username: name, Password: "ok-password-1", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	do := func(user, pass string) int {
		req := httptest.NewRequest(http.MethodGet, "/auth/token", nil)
		req.RemoteAddr = "198.51.100.1:1"
		req.SetBasicAuth(user, pass)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < 20; i++ {
		do("one", "bad-password")
	}
	if got := do("two", "ok-password-1"); got != http.StatusOK {
		t.Fatalf("another account from the same IP must be unaffected: %d", got)
	}
}
