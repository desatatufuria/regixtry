package regixtryhttp

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"regixtry/internal/ports"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func failedLoginLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "login failed") {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestAuthTokenFailureLogsClientIPAndReason(t *testing.T) {
	t.Parallel()

	logs := &syncBuffer{}
	trusted, err := ParseTrustedProxies("192.168.1.245")
	if err != nil {
		t.Fatal(err)
	}
	handler, _, _, _, cleanup := newTestRouterWithRealAuthOptions(t, nil, WithLogger(log.New(logs, "", 0)), WithTrustedProxies(trusted))
	defer cleanup()

	attempt := func(remote, xff, user, pass string) {
		req := httptest.NewRequest(http.MethodGet, "/auth/token", nil)
		req.RemoteAddr = remote
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		req.SetBasicAuth(user, pass)
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	attempt("192.168.1.245:4000", "6.6.6.6, 198.51.100.4", "admin", "Sup3r-secret-wrong")
	attempt("203.0.113.7:4000", "6.6.6.6", "nobody", "another-secret-wrong")

	lines := failedLoginLines(logs.String())
	if len(lines) != 2 {
		t.Fatalf("want 2 failure lines, got %d in:\n%s", len(lines), logs.String())
	}
	for _, want := range []string{"ip=198.51.100.4", `reason="wrong password or unknown admin credential token"`, "endpoint=/auth/token", "account=admin"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("line 1 %q lacks %q", lines[0], want)
		}
	}
	if !strings.Contains(lines[1], "ip=203.0.113.7") || strings.Contains(lines[1], "6.6.6.6") {
		t.Errorf("forged XFF from an untrusted peer must be ignored: %q", lines[1])
	}
	if !strings.Contains(lines[1], `reason="unknown user"`) || strings.Contains(lines[1], "nobody") || !strings.Contains(lines[1], "account_hash=") {
		t.Errorf("unknown usernames are logged as a short hash only: %q", lines[1])
	}
	for _, secret := range []string{"Sup3r-secret-wrong", "another-secret-wrong"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("a password reached the log: %s", logs.String())
		}
	}

	counts := handler.LoginFailureCounts()
	if counts["wrong password or unknown admin credential token"] != 1 || counts["unknown user"] != 1 {
		t.Fatalf("counter = %v", counts)
	}
}

func TestUsernameInLogIsBoundedAndEscaped(t *testing.T) {
	t.Parallel()

	logs := &syncBuffer{}
	handler, _, _, _, cleanup := newTestRouterWithRealAuthOptions(t, nil, WithLogger(log.New(logs, "", 0)))
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/auth/token", nil)
	req.SetBasicAuth("admin\nlogin failed ip=1.1.1.1"+strings.Repeat("x", 500), "wrong-password-x")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	lines := failedLoginLines(logs.String())
	if len(lines) != 1 {
		t.Fatalf("log injection created extra lines:\n%s", logs.String())
	}
	if len(lines[0]) > 400 {
		t.Fatalf("log line is unbounded (%d bytes)", len(lines[0]))
	}
}

func TestBearerFailuresAreLoggedAndCounted(t *testing.T) {
	t.Parallel()

	logs := &syncBuffer{}
	handler, _, _, _, cleanup := newTestRouterWithRealAuthOptions(t, nil, WithLogger(log.New(logs, "", 0)))
	defer cleanup()

	for _, path := range []string{"/v2/", "/admin/v1/users", "/v2/team/app/manifests/latest"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "203.0.113.50:1234"
		req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 64))
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	lines := failedLoginLines(logs.String())
	if len(lines) != 3 {
		t.Fatalf("want 3 bearer failure lines, got %d:\n%s", len(lines), logs.String())
	}
	for _, line := range lines {
		if !strings.Contains(line, "ip=203.0.113.50") || !strings.Contains(line, "reason=") || strings.Contains(line, strings.Repeat("a", 64)) {
			t.Errorf("bad bearer failure line: %q", line)
		}
	}
	if !strings.Contains(lines[1], "endpoint=/admin/v1") || !strings.Contains(lines[0], "endpoint=/v2") {
		t.Errorf("endpoint labels wrong: %v", lines)
	}
	total := uint64(0)
	for _, n := range handler.LoginFailureCounts() {
		total += n
	}
	if total != 3 {
		t.Fatalf("counter total = %d, want 3", total)
	}
}

func TestSuccessfulLoginIsNotCountedAsFailure(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, _, cleanup := newTestRouterWithRealAuthOptions(t, nil)
	defer cleanup()
	if _, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{Username: "ok", Password: "a-fine-password-1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/token", nil)
	req.SetBasicAuth("ok", "a-fine-password-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || len(handler.LoginFailureCounts()) != 0 {
		t.Fatalf("status=%d counts=%v", rec.Code, handler.LoginFailureCounts())
	}
}
