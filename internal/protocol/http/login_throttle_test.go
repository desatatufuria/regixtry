package regixtryhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appauth "regixtry/internal/app/auth"
	"regixtry/internal/ports"
)

func tokenExchange(handler http.Handler, user, secret string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/auth/token?scope=repository:team/app:pull", nil)
	req.SetBasicAuth(user, secret)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// countingCompare is a cheap stand-in for bcrypt that treats "ok-*" passwords
// as correct and counts calls and peak concurrency.
type countingCompare struct {
	calls, inFlight, peak atomic.Int64
	delay                 time.Duration
}

func (c *countingCompare) compare(hash, password []byte) error {
	c.calls.Add(1)
	n := c.inFlight.Add(1)
	for {
		old := c.peak.Load()
		if n <= old || c.peak.CompareAndSwap(old, n) {
			break
		}
	}
	if c.delay > 0 {
		time.Sleep(c.delay)
	}
	c.inFlight.Add(-1)
	// "fixture-pass-9x" is the fixture admin the router helper logs in as.
	if strings.HasPrefix(string(password), "ok-") || string(password) == "fixture-pass-9x" {
		return nil
	}
	return errors.New("mismatch")
}

func TestBcryptFloodIsBoundedAndV2StaysResponsive(t *testing.T) {
	t.Parallel()

	cmp := &countingCompare{delay: 30 * time.Millisecond}
	handler, authService, adminActor, _, cleanup := newTestRouterWithRealAuthOptions(t, func(s *appauth.Service) {
		s.ConfigureLogin(appauth.LoginPolicy{MaxConcurrency: 2, MaxWait: 20 * time.Millisecond, Compare: cmp.compare})
	})
	defer cleanup()
	if _, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{Username: "victim", Password: "a-fine-password-1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	const attackers = 80
	var wg sync.WaitGroup
	var unauthorized, throttled atomic.Int64
	for i := 0; i < attackers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, server.URL+"/auth/token", nil)
			req.SetBasicAuth(fmt.Sprintf("victim-%d", i%7), "bad-password")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("request: %v", err)
				return
			}
			defer resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusUnauthorized:
				unauthorized.Add(1)
			case http.StatusTooManyRequests:
				throttled.Add(1)
				if resp.Header.Get("Retry-After") != "1" {
					t.Errorf("Retry-After = %q, want 1", resp.Header.Get("Retry-After"))
				}
			default:
				t.Errorf("status = %d", resp.StatusCode)
			}
		}(i)
	}

	// While the flood runs, the registry API must keep answering quickly.
	start := time.Now()
	resp, err := http.Get(server.URL + "/v2/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if took := time.Since(start); took > time.Second {
		t.Errorf("/v2/ took %v during a login flood", took)
	}
	wg.Wait()

	if cmp.peak.Load() > 2 {
		t.Fatalf("peak concurrent bcrypt comparisons = %d, want <= 2", cmp.peak.Load())
	}
	if cmp.calls.Load() >= attackers || throttled.Load() == 0 {
		t.Fatalf("bcrypt executions = %d of %d attempts, throttled = %d: the flood was not bounded", cmp.calls.Load(), attackers, throttled.Load())
	}
	if unauthorized.Load()+throttled.Load() != attackers {
		t.Fatalf("401=%d 429=%d, want %d total", unauthorized.Load(), throttled.Load(), attackers)
	}
}

func TestLimiterRefusalBodyIsUniformAndNotAnInvalidCredential(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	started := make(chan struct{}, 1)
	handler, authService, adminActor, _, cleanup := newTestRouterWithRealAuthOptions(t, func(s *appauth.Service) {
		s.ConfigureLogin(appauth.LoginPolicy{MaxConcurrency: 1, MaxWait: 10 * time.Millisecond, Compare: func(h, p []byte) error {
			if string(p) == "fixture-pass-9x" {
				return nil
			}
			select {
			case started <- struct{}{}:
			default:
			}
			<-block
			return errors.New("mismatch")
		}})
	})
	defer cleanup()
	if _, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{Username: "bob", Password: "a-fine-password-1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	go tokenExchange(handler, "bob", "bad-password")
	<-started

	for _, user := range []string{"bob", "nobody"} {
		rec := tokenExchange(handler, user, "another-bad-password")
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "1" {
			t.Fatalf("%s: status=%d Retry-After=%q", user, rec.Code, rec.Header().Get("Retry-After"))
		}
		var body struct {
			Errors []struct{ Code, Message string }
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Errors) != 1 || body.Errors[0].Code != "TOOMANYREQUESTS" {
			t.Fatalf("%s: body = %s", user, rec.Body.String())
		}
		if rec.Header().Get("WWW-Authenticate") != "" {
			t.Fatalf("a 429 must not carry a challenge: %q", rec.Header().Get("WWW-Authenticate"))
		}
	}
	close(block)
}

func TestCredentialTokenFastPathSkipsBcryptWithTheSameResponse(t *testing.T) {
	t.Parallel()

	cmp := &countingCompare{}
	handler, authService, adminActor, _, cleanup := newTestRouterWithRealAuthOptions(t, func(s *appauth.Service) {
		s.ConfigureLogin(appauth.LoginPolicy{Compare: cmp.compare})
	})
	defer cleanup()
	ctx := context.Background()
	user, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: "ci", Password: "a-fine-password-1", Enabled: true, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	cred, err := authService.CreateAdminUserToken(ctx, adminActor, ports.AdminCreateTokenInput{UserID: user.ID, Name: "k", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if !ports.IsCredentialTokenShaped(cred.Secret) {
		t.Fatalf("test assumption broken: credential secret %q is not 64-hex", cred.Secret)
	}

	before := cmp.calls.Load()
	rec := tokenExchange(handler, "ci", cred.Secret)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"expires_in":900`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if cmp.calls.Load() != before {
		t.Fatalf("a valid credential token paid %d bcrypt comparison(s), want 0", cmp.calls.Load()-before)
	}

	// A wrong 64-hex secret keeps the exact baseline failure: same status and
	// body as a wrong password, one comparison.
	wrongPassword := tokenExchange(handler, "ci", "bad-password")
	wrongToken := tokenExchange(handler, "ci", strings.Repeat("ab", 32))
	if wrongToken.Code != wrongPassword.Code || wrongToken.Body.String() != wrongPassword.Body.String() ||
		wrongToken.Header().Get("WWW-Authenticate") != wrongPassword.Header().Get("WWW-Authenticate") {
		t.Fatalf("wrong token differs from wrong password:\n%d %q\n%d %q", wrongToken.Code, wrongToken.Body.String(), wrongPassword.Code, wrongPassword.Body.String())
	}
}

func TestHexPasswordStillWorksAfterTheFastPathMisses(t *testing.T) {
	t.Parallel()

	hexPassword := strings.Repeat("c0", 32)
	handler, authService, adminActor, _, cleanup := newTestRouterWithRealAuthOptions(t, func(s *appauth.Service) {
		s.ConfigureLogin(appauth.LoginPolicy{Compare: func(h, p []byte) error {
			if string(p) == hexPassword || string(p) == "fixture-pass-9x" {
				return nil
			}
			return errors.New("mismatch")
		}})
	})
	defer cleanup()
	if _, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{Username: "hexer", Password: hexPassword, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if rec := tokenExchange(handler, "hexer", hexPassword); rec.Code != http.StatusOK {
		t.Fatalf("a 64-hex password must keep working: %d %s", rec.Code, rec.Body.String())
	}
}

func TestLegitCITrafficIsNeverRefusedAfterFailuresOfOtherAccountsAndTheSameIP(t *testing.T) {
	t.Parallel()

	cmp := &countingCompare{}
	handler, authService, adminActor, _, cleanup := newTestRouterWithRealAuthOptions(t, func(s *appauth.Service) {
		s.ConfigureLogin(appauth.LoginPolicy{Compare: cmp.compare})
	})
	defer cleanup()
	ctx := context.Background()
	ci, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: "ci", Password: "ok-ci-password-1", Enabled: true, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	cred, err := authService.CreateAdminUserToken(ctx, adminActor, ports.AdminCreateTokenInput{UserID: ci.ID, Name: "k", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: "other", Password: "a-fine-password-1", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	do := func(user, secret string) int {
		req := httptest.NewRequest(http.MethodGet, "/auth/token", nil)
		req.RemoteAddr = "203.0.113.9:5555"
		req.SetBasicAuth(user, secret)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < 100; i++ {
		if got := do("other", "bad-password"); got != http.StatusUnauthorized {
			t.Fatalf("failure %d: status %d", i, got)
		}
	}
	tokenBefore := cmp.calls.Load()
	for i := 0; i < 1000; i++ {
		secret, wantCompare := cred.Secret, false
		if i%2 == 1 {
			secret, wantCompare = "ok-ci-password-1", true
		}
		before := cmp.calls.Load()
		if got := do("ci", secret); got != http.StatusOK {
			t.Fatalf("legit exchange %d refused: %d", i, got)
		}
		if paid := cmp.calls.Load() != before; paid != wantCompare {
			t.Fatalf("exchange %d: bcrypt paid = %v, want %v", i, paid, wantCompare)
		}
	}
	if cmp.calls.Load()-tokenBefore != 500 {
		t.Fatalf("1000 exchanges (500 by token) cost %d comparisons, want 500", cmp.calls.Load()-tokenBefore)
	}
}
