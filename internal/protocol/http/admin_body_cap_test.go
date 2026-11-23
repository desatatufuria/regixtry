package regixtryhttp

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	capBytes       = 1048576 // the documented 1 MiB admin body limit
	capTooLargeMsg = `{"error":"request body too large; the limit is 1 MiB"}`
)

// adminBodyRoutes is every admin route that decodes a request body.
var adminBodyRoutes = []struct {
	name, method, path, payload string
}{
	{"projects create", http.MethodPost, "/admin/v1/projects", `{"name":"p1"}`},
	{"project member put", http.MethodPut, "/admin/v1/projects/p1/members/bob", `{"role":"member"}`},
	{"scan settings", http.MethodPut, "/admin/v1/scan-settings", `{}`},
	{"scan policy", http.MethodPut, "/admin/v1/scan-policy", `{}`},
	{"signing policy", http.MethodPut, "/admin/v1/signing-policy", `{}`},
	{"update channel", http.MethodPut, "/admin/v1/update-channel", `{}`},
	{"scan run", http.MethodPost, "/admin/v1/scan-runs", `{"repository":"a/b","reference":"latest"}`},
	{"feature config", http.MethodPut, "/admin/v1/features/trivy/config", `{}`},
	{"feature runtime install", http.MethodPost, "/admin/v1/features/trivy:install", `{"version":"1"}`},
	{"repository override", http.MethodPut, "/admin/v1/features/trivy/repository-overrides/team", `{}`},
	{"users create", http.MethodPost, "/admin/v1/users", `{"username":"u","password":"x"}`},
	{"robots create", http.MethodPost, "/admin/v1/robots", `{"name":"r"}`},
	{"user password reset", http.MethodPost, "/admin/v1/users/u1:reset-password", `{}`},
	{"user grant put", http.MethodPut, "/admin/v1/users/u1/grants/team", `{}`},
	{"user admin token", http.MethodPost, "/admin/v1/users/u1/admin-tokens", `{}`},
	{"repository grant put", http.MethodPut, "/admin/v1/repositories/team/grants/bob", `{}`},
}

// padded left-pads payload with spaces (valid JSON whitespace) to size bytes.
func padded(payload string, size int) string {
	return strings.Repeat(" ", size-len(payload)) + payload
}

// countingReader yields prefix then endless spaces and counts bytes served.
type countingReader struct {
	prefix string
	limit  int64 // total bytes it may serve; <0 = endless
	read   atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	served := c.read.Load()
	if c.limit >= 0 && served >= c.limit {
		return 0, io.EOF
	}
	n := len(p)
	if c.limit >= 0 && int64(n) > c.limit-served {
		n = int(c.limit - served)
	}
	for i := 0; i < n; i++ {
		p[i] = ' '
	}
	c.read.Add(int64(n))
	return n, nil
}

func (e *projectsHTTP) send(method, path, token string, body io.Reader, contentLength int64) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, body)
	req.ContentLength = contentLength
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func TestAdminBodyCapEveryRoute(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, false)

	for _, route := range adminBodyRoutes {
		t.Run(route.name, func(t *testing.T) {
			env := env
			exact := env.do(route.method, route.path, env.adminToken, padded(route.payload, capBytes))
			if exact.Code == http.StatusRequestEntityTooLarge {
				t.Fatalf("exactly-cap body refused: %d %s", exact.Code, exact.Body.String())
			}

			over := env.do(route.method, route.path, env.adminToken, padded(route.payload, capBytes+1))
			if over.Code != http.StatusRequestEntityTooLarge || strings.TrimSpace(over.Body.String()) != capTooLargeMsg {
				t.Fatalf("cap+1 = %d %q, want 413 %s", over.Code, over.Body.String(), capTooLargeMsg)
			}
			if got := over.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
				t.Fatalf("Content-Type = %q", got)
			}

			// Content-Length over the cap is refused without reading the body.
			cr := &countingReader{limit: 100 << 20}
			byLength := env.send(route.method, route.path, env.adminToken, cr, capBytes+1)
			if byLength.Code != http.StatusRequestEntityTooLarge || cr.read.Load() != 0 {
				t.Fatalf("declared oversize = %d after reading %d bytes, want 413 and 0", byLength.Code, cr.read.Load())
			}

			// A 100 MB chunked body (no Content-Length) stops near the cap.
			stream := &countingReader{limit: 100 << 20}
			streamed := env.send(route.method, route.path, env.adminToken, stream, -1)
			if streamed.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("streamed = %d %s, want 413", streamed.Code, streamed.Body.String())
			}
			if got := stream.read.Load(); got > capBytes+64<<10 {
				t.Fatalf("server read %d bytes of a streamed body, want at most cap+64KiB", got)
			}
		})
	}
}

// Trailing data past the cap (after a complete object) is also bounded.
func TestAdminBodyCapTrailingDataBeyondCap(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, false)
	body := `{"name":"p1"}` + strings.Repeat(" ", capBytes)
	rec := env.do(http.MethodPost, "/admin/v1/projects", env.adminToken, body)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d %s, want 413", rec.Code, rec.Body.String())
	}
}

func TestAdminBodyCapUnauthorizedCallersStayBounded(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, false)

	for _, tc := range []struct{ name, token string }{{"no credentials", ""}, {"invalid token", "nope"}} {
		t.Run(tc.name, func(t *testing.T) {
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			stream := &countingReader{limit: 50 << 20}
			rec := env.send(http.MethodPut, "/admin/v1/projects/p1/members/bob", tc.token, stream, -1)
			runtime.ReadMemStats(&after)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d %s, want 401 unchanged", rec.Code, rec.Body.String())
			}
			if got := stream.read.Load(); got > capBytes+64<<10 {
				t.Fatalf("read %d bytes for an unauthenticated caller", got)
			}
			if delta := int64(after.TotalAlloc) - int64(before.TotalAlloc); delta > 16<<20 {
				t.Fatalf("allocated %d bytes handling a 50 MB unauthenticated body", delta)
			}
		})
	}
}

// Malformed JSON under the cap keeps its pre-cap status and message.
func TestAdminBodyCapLeavesMalformedBodiesUnchanged(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, false)
	cases := map[string]string{
		"not json":      `{nope`,
		"unknown field": `{"zzz":1}`,
		"two objects":   `{} {}`,
		"empty":         ``,
		"wrong type":    `{"name":5}`,
	}
	want := map[string]string{
		"not json":      `request body must be valid JSON`,
		"unknown field": `request body must be valid JSON`,
		"two objects":   `request body must contain a single JSON object`,
		"empty":         `request body is required`,
		"wrong type":    `name has an invalid type`,
	}
	for name, body := range cases {
		rec := env.do(http.MethodPost, "/admin/v1/projects", env.adminToken, body)
		if rec.Code != http.StatusUnprocessableEntity || !bytes.Contains(rec.Body.Bytes(), []byte(want[name])) {
			t.Errorf("%s: %d %s, want 422 containing %q", name, rec.Code, rec.Body.String(), want[name])
		}
	}
}

func TestAdminBodyCapSecurityBackoffReset(t *testing.T) {
	t.Parallel()
	env := newSecHTTP(t, "enforce")
	path := "/admin/v1/security/login-backoff/reset"
	payload := `{"account":"nobody"}`

	if rec := env.do(http.MethodPost, path, env.adminToken, padded(payload, capBytes)); rec.Code == http.StatusRequestEntityTooLarge {
		t.Fatalf("exactly-cap body refused: %s", rec.Body.String())
	}
	rec := env.do(http.MethodPost, path, env.adminToken, padded(payload, capBytes+1))
	if rec.Code != http.StatusRequestEntityTooLarge || strings.TrimSpace(rec.Body.String()) != capTooLargeMsg {
		t.Fatalf("cap+1 = %d %q, want 413", rec.Code, rec.Body.String())
	}
}

// Over a real connection a chunked 100 MB upload is answered 413 and the
// server stops consuming it.
func TestAdminBodyCapOverRealChunkedConnection(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, false)
	srv := httptest.NewServer(env.handler)
	defer srv.Close()

	stream := &countingReader{limit: 100 << 20}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/v1/projects", stream)
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = -1 // chunked
	req.Header.Set("Authorization", "Bearer "+env.adminToken)
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413", resp.StatusCode)
		}
	} // a reset after the 413 is also an acceptable way for the server to stop
	if got := stream.read.Load(); got >= 100<<20 {
		t.Fatalf("client uploaded the whole 100 MB (%d bytes): the server never stopped reading", got)
	}
}
