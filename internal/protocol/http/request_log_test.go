package regixtryhttp

import (
	"bufio"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"regixtry/internal/ports"
)

// genuineSuffix is what the router itself appends after the quoted path.
var genuineSuffix = regexp.MustCompile(`" status=\d+ duration=\S+\n$`)

// requestLogLines splits the captured log into records the way an operator's
// grep sees them: one per newline.
func requestLogLines(out string) []string {
	return strings.Split(strings.TrimSuffix(out, "\n"), "\n")
}

func TestRequestLogEscapesClientControlledPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		target  string
		escaped string
	}{
		{"newline forges a login failed line", "/v2/x%0a2026/11/23%2010:00:00%20login%20failed%20endpoint=/auth/token%20ip=10.9.9.9%20reason=%22wrong%20password%22%20account=admin", `\n2026/11/23 10:00:00 login failed endpoint=/auth/token ip=10.9.9.9`},
		{"carriage return", "/v2/x%0dforged", `\rforged`},
		{"terminal escape", "/v2/x%1b[2Jforged", `\x1b[2Jforged`},
		{"percent-encoded bytes in the query stay encoded", "/v2/x?a=%0aregistry%20request%20method=GET", "?a=%0aregistry%20request%20method=GET"},
		{"embedded quote cannot close the value early", "/v2/x%22%20status=200", `\" status=200`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			logs := &syncBuffer{}
			handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}), fakeAuthService{}, WithLogger(log.New(logs, "", 0)))
			defer cleanup()

			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			handler.ServeHTTP(httptest.NewRecorder(), req)

			out := logs.String()
			if lines := requestLogLines(out); len(lines) != 1 {
				t.Fatalf("client bytes created %d log lines, want 1:\n%s", len(lines), out)
			}
			if !strings.HasSuffix(out, "\n") || strings.Count(out, "\n") != 1 {
				t.Fatalf("record must end with exactly one newline: %q", out)
			}
			if strings.ContainsAny(out[:len(out)-1], "\r\x1b") {
				t.Fatalf("raw control byte reached the log: %q", out)
			}
			if !strings.HasPrefix(out, "registry request method=GET path=\"/v2/x") {
				t.Fatalf("line must keep its field order with a quoted path: %q", out)
			}
			if !strings.Contains(out, tc.escaped) {
				t.Fatalf("log %q lacks the escaped form %q", out, tc.escaped)
			}
			if !genuineSuffix.MatchString(out) {
				t.Fatalf("the genuine status and duration fields must close the record: %q", out)
			}
		})
	}
}

// The request below goes through net/http's real request-line parser and the
// real listener, not a hand-built *http.Request, to show a percent-encoded
// newline reaches the router and cannot split the record.
func TestRequestLogOverTheWireCannotBeForged(t *testing.T) {
	t.Parallel()

	logs := &syncBuffer{}
	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}), fakeAuthService{}, WithLogger(log.New(logs, "", 0)))
	defer cleanup()
	server := httptest.NewServer(handler)
	defer server.Close()

	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("GET /v2/x%0aregistry%20request%20method=GET%20path=/pwned%20status=200 HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := http.ReadResponse(bufio.NewReader(conn), nil); err != nil {
		t.Fatal(err)
	}

	// The handler logs after it answers: wait for the record.
	deadline := time.Now().Add(5 * time.Second)
	for logs.String() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	out := logs.String()
	if lines := requestLogLines(out); len(lines) != 1 || !strings.Contains(lines[0], `\nregistry request`) || !genuineSuffix.MatchString(out) {
		t.Fatalf("want one escaped record, got:\n%q", out)
	}
}

func TestRequestLogKeepsPlainPathsReadable(t *testing.T) {
	t.Parallel()

	logs := &syncBuffer{}
	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}), fakeAuthService{}, WithLogger(log.New(logs, "", 0)))
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/v2/team/app/manifests/latest?n=5&last=a%20b", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	if !strings.Contains(out, `method=GET path="/v2/team/app/manifests/latest?n=5&last=a%20b" status=`) {
		t.Fatalf("plain path/query changed shape: %q", out)
	}
}

// recordLoginFailure and recordLoginThrottle take the endpoint as a string. The
// router only passes fixed labels today, but credentialEndpoint falls back to
// the raw request path, so the line must not trust it.
func TestLoginFailureEndpointIsEscaped(t *testing.T) {
	t.Parallel()

	hostile := "/x\nlogin failed endpoint=/auth/token ip=10.9.9.9 reason=\"forged\"\x1b[2J"
	for name, record := range map[string]func(*Router, *http.Request){
		"failure": func(r *Router, req *http.Request) {
			r.recordLoginFailure(req, hostile, "", &ports.LoginThrottledError{})
		},
		"throttle": func(r *Router, req *http.Request) {
			r.recordLoginThrottle(req, hostile, "alice", &ports.LoginThrottledError{Reason: "throttled: account backoff"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			logs := &syncBuffer{}
			handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}), fakeAuthService{}, WithLogger(log.New(logs, "", 0)))
			defer cleanup()

			record(handler, httptest.NewRequest(http.MethodGet, "/v2/", nil))

			out := logs.String()
			if len(requestLogLines(out)) != 1 || strings.ContainsAny(out[:len(out)-1], "\x1b") {
				t.Fatalf("endpoint split or polluted the record: %q", out)
			}
			if !strings.HasPrefix(out, `login failed endpoint="/x\nlogin failed endpoint=/auth/token`) {
				t.Fatalf("endpoint must be quoted: %q", out)
			}
		})
	}
}

func TestLoginFailureKnownEndpointsStayBare(t *testing.T) {
	t.Parallel()

	for _, endpoint := range []string{"/auth/token", "/v2", "/admin/v1"} {
		if got := logEndpoint(endpoint); got != endpoint {
			t.Errorf("logEndpoint(%q) = %q, want it unchanged so greps keep working", endpoint, got)
		}
	}
}
