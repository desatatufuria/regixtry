package regixtryhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	appauth "regixtry/internal/app/auth"
	"regixtry/internal/ports"
)

type recordingFailures struct {
	mu   sync.Mutex
	seen []ports.LoginFailureObservation
}

func (r *recordingFailures) RecordLoginFailure(o ports.LoginFailureObservation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, o)
}

func (r *recordingFailures) all() []ports.LoginFailureObservation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ports.LoginFailureObservation(nil), r.seen...)
}

func TestEveryFailurePathContributesExactlyOnceToTheRecorder(t *testing.T) {
	t.Parallel()
	recorder := &recordingFailures{}
	clock := newHTTPClock()
	handler, authService, actor, _, cleanup := newTestRouterWithRealAuthOptions(t, func(s *appauth.Service) {
		s.ConfigureLogin(appauth.LoginPolicy{
			Compare: (&countingCompare{}).compare,
			Backoff: &appauth.BackoffPolicy{Mode: appauth.BackoffEnforce, Threshold: 2, Base: 2 * time.Second, Max: 300 * time.Second, Now: clock.Now},
		})
	}, WithLoginFailureRecorder(recorder))
	defer cleanup()
	if _, err := authService.CreateAdminUser(context.Background(), actor, ports.AdminCreateUserInput{Username: "dana", Password: "ok-dana-password", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	step := func(name string, wantStatus int, run func() int, want ports.LoginFailureObservation) {
		before := len(recorder.all())
		if got := run(); got != wantStatus {
			t.Fatalf("%s: status = %d, want %d", name, got, wantStatus)
		}
		seen := recorder.all()
		if len(seen) != before+1 {
			t.Fatalf("%s: contributions = %d, want exactly 1", name, len(seen)-before)
		}
		got := seen[len(seen)-1]
		if got.Endpoint != want.Endpoint || got.Account != want.Account || got.Reason != want.Reason || got.IP != "192.0.2.1" || got.At.IsZero() {
			t.Fatalf("%s: observation = %+v, want %+v", name, got, want)
		}
	}
	exchange := func(user, pass string) func() int {
		return func() int { return tokenExchange(handler, user, pass).Code }
	}
	step("unknown user", http.StatusUnauthorized, exchange("Ghost-Name", "bad"), ports.LoginFailureObservation{Endpoint: "/auth/token", Account: "hash:" + accountHashForTest("ghost-name"), Reason: "unknown user"})
	step("wrong password", http.StatusUnauthorized, exchange("Dana", "bad"), ports.LoginFailureObservation{Endpoint: "/auth/token", Account: "dana", Reason: "wrong password or unknown admin credential token"})
	step("second wrong password", http.StatusUnauthorized, exchange("dana", "bad"), ports.LoginFailureObservation{Endpoint: "/auth/token", Account: "dana", Reason: "wrong password or unknown admin credential token"})
	step("enforced backoff", http.StatusTooManyRequests, exchange("dana", "ok-dana-password"), ports.LoginFailureObservation{Endpoint: "/auth/token", Account: "hash:" + accountHashForTest("dana"), Reason: "throttled: account backoff"})
	bearer := func(path string) func() int {
		return func() int {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer not-a-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			return rec.Code
		}
	}
	step("bearer on /v2", http.StatusUnauthorized, bearer("/v2/"), ports.LoginFailureObservation{Endpoint: "/v2", Reason: "invalid credentials"})
	step("bearer on /admin/v1", http.StatusUnauthorized, bearer("/admin/v1/users"), ports.LoginFailureObservation{Endpoint: "/admin/v1", Reason: "invalid credentials"})

	// Successful logins and malformed requests contribute nothing.
	before := len(recorder.all())
	clock.Advance(time.Hour)
	if code := tokenExchange(handler, "dana", "ok-dana-password").Code; code != http.StatusOK {
		t.Fatalf("success status = %d", code)
	}
	if len(recorder.all()) != before {
		t.Fatal("a successful login must not be recorded")
	}
}

func TestRouterWithoutARecorderBehavesAsBefore(t *testing.T) {
	t.Parallel()
	handler, _, _, _, cleanup := newTestRouterWithRealAuthOptions(t, nil)
	defer cleanup()
	if code := tokenExchange(handler, "admin", "wrong").Code; code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", code)
	}
}
