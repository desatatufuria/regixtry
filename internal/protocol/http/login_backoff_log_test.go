package regixtryhttp

import (
	"context"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	appauth "regixtry/internal/app/auth"
	"regixtry/internal/ports"
)

// An enforced backoff refusal writes exactly ONE "login failed" line (the
// service stays silent in enforce mode, the router writes it with the client
// IP) and contributes exactly once to the aggregate, never twice.
func TestEnforcedBackoff429WritesExactlyOneLoginFailedLineAndOneContribution(t *testing.T) {
	t.Parallel()
	logs := &syncBuffer{}
	recorder := &recordingFailures{}
	clock := newHTTPClock()
	handler, authService, actor, _, cleanup := newTestRouterWithRealAuthOptions(t, func(s *appauth.Service) {
		s.ConfigureLogin(appauth.LoginPolicy{
			Logger:  log.New(logs, "", 0),
			Compare: (&countingCompare{}).compare,
			Backoff: &appauth.BackoffPolicy{Mode: appauth.BackoffEnforce, Threshold: 5, Base: 2 * time.Second, Max: 300 * time.Second, Now: clock.Now},
		})
	}, WithLogger(log.New(logs, "", 0)), WithLoginFailureRecorder(recorder))
	defer cleanup()
	if _, err := authService.CreateAdminUser(context.Background(), actor, ports.AdminCreateUserInput{Username: "dana", Password: "ok-dana-password", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if code := tokenExchange(handler, "dana", "bad-password").Code; code != http.StatusUnauthorized {
			t.Fatalf("free failure %d = %d", i, code)
		}
	}
	linesBefore := len(failedLoginLines(logs.String()))
	contributionsBefore := len(recorder.all())

	rec := tokenExchange(handler, "dana", "ok-dana-password")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("in backoff = %d, want 429", rec.Code)
	}

	lines := failedLoginLines(logs.String())
	if len(lines) != linesBefore+1 {
		t.Fatalf("login failed lines for the 429 = %d, want exactly 1:\n%s", len(lines)-linesBefore, strings.Join(lines, "\n"))
	}
	line := lines[len(lines)-1]
	for _, want := range []string{"login failed", "endpoint=/auth/token", "ip=192.0.2.1", `reason="throttled: account backoff"`, "account_hash="} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q lacks %q", line, want)
		}
	}
	if strings.Contains(line, "account=dana") || strings.Contains(strings.ToLower(line), "dana") {
		t.Fatalf("a throttled refusal must not name the account: %q", line)
	}
	if got := len(recorder.all()) - contributionsBefore; got != 1 {
		t.Fatalf("aggregator contributions for the 429 = %d, want exactly 1", got)
	}
	// The service writes no second line of its own in enforce mode.
	if strings.Contains(logs.String(), "would have blocked") {
		t.Fatalf("enforce mode must not log the log-mode notice:\n%s", logs.String())
	}
}
