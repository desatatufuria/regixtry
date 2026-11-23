package regixtryhttp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appauth "regixtry/internal/app/auth"
	domainauth "regixtry/internal/domain/auth"
	authpostgres "regixtry/internal/infra/auth/postgres"
	"regixtry/internal/ports"
)

const secPassword = "fixture-pass-9x"

// secHTTP is a router wired like cmd/regixtry for login security: a real auth
// service and aggregator over a SQLite-backed store, backoff in enforce mode on
// a fake clock, and a recording logger.
type secHTTP struct {
	t          *testing.T
	handler    *Router
	auth       *appauth.Service
	store      *authpostgres.Store
	agg        *appauth.LoginFailureAggregator
	clock      *httpClock
	logs       *syncBuffer
	admin      domainauth.Principal
	adminToken string
}

func newSecHTTP(t *testing.T, mode appauth.BackoffMode, options ...RouterOption) *secHTTP {
	t.Helper()
	store, err := authpostgres.NewWithDriver("sqlite", "file:"+filepath.Join(t.TempDir(), "auth.db")+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	clock := newHTTPClock()
	logs := &syncBuffer{}
	service := appauth.NewService(store)
	service.ConfigureLogin(appauth.LoginPolicy{
		MaxConcurrency: 8, MaxWait: 5 * time.Second, Logger: log.New(logs, "", 0),
		Compare: func(_, password []byte) error {
			if string(password) == secPassword {
				return nil
			}
			return errors.New("mismatch")
		},
		Backoff: &appauth.BackoffPolicy{Mode: mode, Threshold: 5, Base: 2 * time.Second, Max: 300 * time.Second, Now: clock.Now},
	})
	agg := appauth.NewLoginFailureAggregator(store, appauth.LoginFailureOptions{})
	service.SetLoginFailureAggregator(agg)

	blobStore, metadataStore, cleanup := newTestStores(t)
	accessController := ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "regixtry"})
	options = append([]RouterOption{WithLogger(log.New(logs, "", 0)), WithLoginFailureRecorder(agg)}, options...)
	router := newRouterWithStores(blobStore, metadataStore, accessController, service, options...)
	t.Cleanup(drainingCleanup(router, cleanup))

	bootstrap, err := service.BootstrapAdmin(context.Background(), ports.BootstrapAdminInput{Username: "admin", Password: secPassword})
	if err != nil {
		t.Fatal(err)
	}
	login, err := service.LoginWithPassword(context.Background(), "admin", secPassword, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &secHTTP{t: t, handler: router, auth: service, store: store, agg: agg, clock: clock, logs: logs,
		admin:      domainauth.Principal{UserID: bootstrap.User.ID, Username: "admin", IsAdmin: true},
		adminToken: login.BearerToken}
}

func (e *secHTTP) do(method, path, token, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

// exchange runs the docker token exchange from a given client address.
func (e *secHTTP) exchange(user, secret, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/auth/token?service=regixtry", nil)
	req.RemoteAddr = ip + ":40000"
	req.SetBasicAuth(user, secret)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func (e *secHTTP) newUser(name string, mutate ...func(*ports.AdminCreateUserInput)) string {
	e.t.Helper()
	input := ports.AdminCreateUserInput{Username: name, Password: secPassword, Enabled: true}
	for _, m := range mutate {
		m(&input)
	}
	if _, err := e.auth.CreateAdminUser(context.Background(), e.admin, input); err != nil {
		e.t.Fatal(err)
	}
	login, err := e.auth.LoginWithPassword(context.Background(), name, secPassword, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	return login.BearerToken
}

// tokens returns one bearer token per kind of non-global-admin principal.
func (e *secHTTP) refusedPrincipals() map[string]string {
	e.t.Helper()
	ctx := context.Background()
	out := map[string]string{}
	out["standard user"] = e.newUser("plain")
	out["read-only user"] = e.newUser("viewer", func(i *ports.AdminCreateUserInput) { i.IsReadOnly = true })
	e.newUser("repoadmin")
	if _, err := e.auth.PutAdminRepositoryGrant(ctx, e.admin, ports.AdminPutRepositoryGrantInput{Username: "repoadmin", Repository: "team/app", Role: domainauth.RepoRoleAdmin}); err != nil {
		e.t.Fatal(err)
	}
	login, err := e.auth.LoginWithPassword(ctx, "repoadmin", secPassword, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	out["repository admin"] = login.BearerToken
	robot, err := e.auth.CreateAdminRobot(ctx, e.admin, ports.AdminCreateRobotInput{Name: "ci-bot", Repository: "team/app", Role: domainauth.RepoRoleReader, TTL: time.Hour})
	if err != nil {
		e.t.Fatal(err)
	}
	robotLogin, err := e.auth.LoginWithPreissuedToken(ctx, "ci-bot", robot.Secret, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	out["robot"] = robotLogin.BearerToken
	scopes, _ := domainauth.ParseScopes([]string{"repository:team/app:pull"})
	scoped, err := e.auth.LoginWithPassword(ctx, "admin", secPassword, scopes)
	if err != nil {
		e.t.Fatal(err)
	}
	out["scope-limited admin token"] = scoped.BearerToken
	return out
}

type failurePage struct {
	Failures []ports.LoginFailureRecord `json:"failures"`
	Next     string                     `json:"next"`
}

func (e *secHTTP) failures(query string) failurePage {
	e.t.Helper()
	rec := e.do(http.MethodGet, "/admin/v1/security/login-failures"+query, e.adminToken, "")
	if rec.Code != http.StatusOK {
		e.t.Fatalf("login-failures%s status = %d body=%s", query, rec.Code, rec.Body.String())
	}
	var page failurePage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		e.t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	return page
}

func TestSecurityEndpointsRefuseEveryoneButAGlobalAdmin(t *testing.T) {
	t.Parallel()
	e := newSecHTTP(t, appauth.BackoffLog)
	principals := e.refusedPrincipals()
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/admin/v1/security/login-failures", ""},
		{http.MethodGet, "/admin/v1/security/login-summary", ""},
		{http.MethodGet, "/admin/v1/security/login-backoff", ""},
		{http.MethodPost, "/admin/v1/security/login-backoff/reset", `{"account":"plain"}`},
		{http.MethodPost, "/admin/v1/security/login-backoff/reset", `{"account":"nobody-here"}`},
	}
	for _, route := range routes {
		route := route
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			if rec := e.do(route.method, route.path, "", route.body); rec.Code != http.StatusUnauthorized {
				t.Fatalf("anonymous = %d, want 401 (%s)", rec.Code, rec.Body.String())
			}
			bodies := map[string]bool{}
			for name, token := range principals {
				rec := e.do(route.method, route.path, token, route.body)
				if rec.Code != http.StatusForbidden {
					t.Fatalf("%s = %d, want 403 (%s)", name, rec.Code, rec.Body.String())
				}
				bodies[rec.Body.String()] = true
			}
			if len(bodies) > 2 { // the scope-limited admin has its own, equally uniform, message
				t.Fatalf("403 bodies differ across principals: %v", bodies)
			}
		})
	}
	for _, path := range []string{"/admin/v1/security/login-failures", "/admin/v1/security/login-summary", "/admin/v1/security/login-backoff"} {
		if rec := e.do(http.MethodGet, path, e.adminToken, ""); rec.Code != http.StatusOK {
			t.Fatalf("global admin %s = %d (%s)", path, rec.Code, rec.Body.String())
		}
	}
}

func TestSecurityEndpointsRejectWrongMethodsAndUnknownSubroutes(t *testing.T) {
	t.Parallel()
	e := newSecHTTP(t, appauth.BackoffLog)
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/admin/v1/security/login-failures", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/admin/v1/security/login-summary", http.StatusMethodNotAllowed},
		{http.MethodPut, "/admin/v1/security/login-backoff", http.StatusMethodNotAllowed},
		{http.MethodGet, "/admin/v1/security/nope", http.StatusNotFound},
		{http.MethodGet, "/admin/v1/security", http.StatusNotFound},
	} {
		if rec := e.do(tc.method, tc.path, e.adminToken, ""); rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d (%s)", tc.method, tc.path, rec.Code, tc.want, rec.Body.String())
		}
	}
}

func TestLoginFailuresListAggregatedRowsAndNeverTheRawUnknownName(t *testing.T) {
	t.Parallel()
	e := newSecHTTP(t, appauth.BackoffLog)
	e.newUser("alice")

	for i := 0; i < 3; i++ {
		e.exchange("Alice", "wrong-password", "203.0.113.7")
	}
	e.exchange("alice", "wrong-password", "198.51.100.9")
	for i := 0; i < 2; i++ {
		e.exchange("Mallory-Raw-Name", "wrong-password", "203.0.113.7")
	}
	// A bearer failure on /v2 contributes once, with no account.
	e.do(http.MethodGet, "/v2/", "not-a-real-token", "")

	page := e.failures("")
	if page.Next != "" {
		t.Fatalf("next = %q, want empty on a single page", page.Next)
	}
	type row struct{ endpoint, reason, account, ip string }
	got := map[row]int64{}
	for _, f := range page.Failures {
		got[row{f.Endpoint, f.Reason, f.Account, f.IP}] += f.Count
		if f.WindowStart.IsZero() || f.FirstSeen.IsZero() || f.LastSeen.Before(f.FirstSeen) {
			t.Fatalf("bad times in %+v", f)
		}
	}
	hash := "hash:" + sha8("mallory-raw-name")
	want := map[row]int64{
		{"/auth/token", "wrong password or unknown admin credential token", "alice", "203.0.113.7"}:  3,
		{"/auth/token", "wrong password or unknown admin credential token", "alice", "198.51.100.9"}: 1,
		{"/auth/token", "unknown user", hash, "203.0.113.7"}:                                         2,
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("row %+v = %d, want %d (all: %+v)", k, got[k], n, got)
		}
	}
	var bearerCount int64
	for k, n := range got {
		if k.endpoint == "/v2" {
			if k.account != "" {
				t.Errorf("bearer failure carries account %q", k.account)
			}
			bearerCount += n
		}
	}
	if bearerCount != 1 {
		t.Errorf("bearer failures = %d, want exactly 1", bearerCount)
	}

	// The raw unknown username appears nowhere: not in any response, any log
	// line, or any stored row.
	rec := e.do(http.MethodGet, "/admin/v1/security/login-failures", e.adminToken, "")
	if strings.Contains(strings.ToLower(rec.Body.String()), "mallory") {
		t.Fatalf("response leaks the raw name: %s", rec.Body.String())
	}
	if strings.Contains(strings.ToLower(e.logs.String()), "mallory") {
		t.Fatalf("log leaks the raw name:\n%s", e.logs.String())
	}
	rows, err := e.store.ListLoginFailures(context.Background(), ports.LoginFailureFilter{}, nil, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.Account+r.Endpoint+r.Reason+r.IP), "mallory") {
			t.Fatalf("stored row leaks the raw name: %+v", r)
		}
	}
}

func TestLoginFailuresListNormalizesTheAccountOfAnExistingUser(t *testing.T) {
	t.Parallel()
	e := newSecHTTP(t, appauth.BackoffLog)
	e.newUser("alice")
	e.exchange(" ALICE ", "wrong-password", "203.0.113.7")
	if page := e.failures("?account=Alice"); len(page.Failures) != 1 || page.Failures[0].Account != "alice" {
		t.Fatalf("filter by ' Alice ' style name = %+v, want the alice row", page.Failures)
	}
}

func TestLoginFailuresFiltersLimitAndPagination(t *testing.T) {
	t.Parallel()
	e := newSecHTTP(t, appauth.BackoffLog)
	for _, name := range []string{"u1", "u2", "u3"} {
		e.newUser(name)
	}
	now := time.Now().UTC()
	var records []ports.LoginFailureRecord
	for w := 0; w < 4; w++ {
		at := now.Add(-time.Duration(w) * 10 * time.Minute)
		for _, name := range []string{"u1", "u2", "u3"} {
			records = append(records, ports.LoginFailureRecord{WindowStart: at.Truncate(appauth.LoginFailureWindow), Endpoint: "/auth/token", Reason: "wrong password", Account: name, IP: "203.0.113.7", Count: 1, FirstSeen: at, LastSeen: at})
		}
	}
	if err := e.store.AddLoginFailures(context.Background(), records); err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	next, pages := "", 0
	for {
		query := "?limit=5"
		if next != "" {
			query += "&cursor=" + next
		}
		page := e.failures(query)
		pages++
		for _, f := range page.Failures {
			key := f.WindowStart.String() + f.Account
			if seen[key] {
				t.Fatalf("row repeated across pages: %s", key)
			}
			seen[key] = true
		}
		if page.Next == "" {
			break
		}
		next = page.Next
		if pages > 10 {
			t.Fatal("pagination does not terminate")
		}
	}
	if len(seen) != 12 || pages != 3 {
		t.Fatalf("rows = %d pages = %d, want 12 rows over 3 pages", len(seen), pages)
	}

	if page := e.failures("?account=u2"); len(page.Failures) != 4 {
		t.Errorf("account filter rows = %d, want 4", len(page.Failures))
	}
	if page := e.failures("?ip=198.51.100.9"); len(page.Failures) != 0 {
		t.Errorf("ip filter rows = %d, want 0", len(page.Failures))
	}
	if page := e.failures("?reason=unknown+user"); len(page.Failures) != 0 {
		t.Errorf("reason filter rows = %d, want 0", len(page.Failures))
	}
	from := now.Add(-15 * time.Minute).Format(time.RFC3339)
	if page := e.failures("?from=" + from); len(page.Failures) != 6 {
		t.Errorf("from filter rows = %d, want 6 (the two newest windows)", len(page.Failures))
	}
	to := now.Add(-25 * time.Minute).Format(time.RFC3339)
	if page := e.failures("?to=" + to); len(page.Failures) != 3 {
		t.Errorf("to filter rows = %d, want 3 (the oldest window)", len(page.Failures))
	}
	// Default page size 50, newest first.
	page := e.failures("")
	for i := 1; i < len(page.Failures); i++ {
		if page.Failures[i].WindowStart.After(page.Failures[i-1].WindowStart) {
			t.Fatalf("not newest first at %d", i)
		}
	}
}

func TestLoginFailuresRejectBadQueriesAndTamperedCursors(t *testing.T) {
	t.Parallel()
	e := newSecHTTP(t, appauth.BackoffLog)
	enc := func(raw string) string { return base64.RawURLEncoding.EncodeToString([]byte(raw)) }
	for name, query := range map[string]string{
		"limit zero":         "?limit=0",
		"limit too big":      "?limit=201",
		"limit text":         "?limit=ten",
		"from not a time":    "?from=yesterday",
		"to not a time":      "?to=2026-13-45",
		"from after to":      "?from=2026-03-02T00:00:00Z&to=2026-03-01T00:00:00Z",
		"cursor not base64":  "?cursor=%25%25%25",
		"cursor NUL":         "?cursor=" + enc("[\"2026-03-01T12:00:00Z\",\"a\",\"b\",\"c\\u0000d\",\"e\"]"),
		"cursor raw NUL":     "?cursor=" + enc("a\x00b"),
		"cursor bad UTF-8":   "?cursor=" + enc("\xff\xfe\xfd"),
		"cursor not JSON":    "?cursor=" + enc("hello"),
		"cursor wrong arity": "?cursor=" + enc(`["2026-03-01T12:00:00Z","a"]`),
		"cursor bad time":    "?cursor=" + enc(`["noon","a","b","c","d"]`),
		"cursor too long":    "?cursor=" + enc(strings.Repeat("x", 4096)),
		"account NUL":        "?account=a%00b",
	} {
		rec := e.do(http.MethodGet, "/admin/v1/security/login-failures"+query, e.adminToken, "")
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422 (%s)", name, rec.Code, rec.Body.String())
		}
	}
}

func TestLoginSummaryCountsTheLast15MinutesAndAccountsInBackoff(t *testing.T) {
	t.Parallel()
	e := newSecHTTP(t, appauth.BackoffEnforce)
	e.newUser("alice")
	e.newUser("bob")
	for i := 0; i < 5; i++ {
		e.exchange("alice", "wrong-password", "203.0.113.7")
	}
	e.exchange("bob", "wrong-password", "198.51.100.9")
	e.exchange("ghost", "wrong-password", "198.51.100.9")
	if rec := e.exchange("alice", secPassword, "203.0.113.7"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("alice in backoff = %d, want 429", rec.Code)
	}
	// distinct_accounts is 4: alice, bob, the ghost hash and the hash alice's
	// throttled refusal is keyed by (a refusal never looks the account up).
	// Old rows do not count.
	old := time.Now().UTC().Add(-2 * time.Hour)
	if err := e.store.AddLoginFailures(context.Background(), []ports.LoginFailureRecord{{WindowStart: old.Truncate(appauth.LoginFailureWindow), Endpoint: "/auth/token", Reason: "wrong password", Account: "carol", IP: "192.0.2.50", Count: 40, FirstSeen: old, LastSeen: old}}); err != nil {
		t.Fatal(err)
	}

	rec := e.do(http.MethodGet, "/admin/v1/security/login-summary", e.adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	want := map[string]any{"window_minutes": 15.0, "failures": 8.0, "distinct_ips": 2.0, "distinct_accounts": 4.0, "accounts_in_backoff": 1.0}
	if len(got) != len(want) {
		t.Fatalf("summary keys = %v, want exactly %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("summary[%s] = %v, want %v (all: %v)", k, got[k], v, got)
		}
	}
}

func TestLoginBackoffShowsPolicyAndAccountsWithHashesForUnknownNames(t *testing.T) {
	t.Parallel()
	trusted, err := ParseTrustedProxies("192.168.1.245")
	if err != nil {
		t.Fatal(err)
	}
	e := newSecHTTP(t, appauth.BackoffEnforce, WithTrustedProxies(trusted))
	e.newUser("alice")
	for i := 0; i < 6; i++ {
		e.exchange("alice", "wrong-password", "203.0.113.7")
	}
	for i := 0; i < 5; i++ {
		e.exchange("Ghost-Raw", "wrong-password", "203.0.113.7")
	}
	e.exchange("bob-below-threshold", "wrong-password", "203.0.113.7")

	rec := e.do(http.MethodGet, "/admin/v1/security/login-backoff", e.adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "ghost-raw") {
		t.Fatalf("response leaks an unknown username: %s", rec.Body.String())
	}
	var got struct {
		Mode                     string `json:"mode"`
		Threshold                int    `json:"threshold"`
		BaseSeconds              int    `json:"base_seconds"`
		MaxSeconds               int    `json:"max_seconds"`
		MaxConcurrency           int    `json:"max_concurrency"`
		TrustedProxiesConfigured bool   `json:"trusted_proxies_configured"`
		Accounts                 []struct {
			Account          string `json:"account"`
			Failures         int    `json:"failures"`
			RemainingSeconds int    `json:"remaining_seconds"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Mode != "enforce" || got.Threshold != 5 || got.BaseSeconds != 2 || got.MaxSeconds != 300 || got.MaxConcurrency != 8 || !got.TrustedProxiesConfigured {
		t.Fatalf("policy = %+v", got)
	}
	byAccount := map[string]int{}
	for _, a := range got.Accounts {
		byAccount[a.Account] = a.Failures
		if a.RemainingSeconds < 1 {
			t.Errorf("%s remaining = %d, want at least 1", a.Account, a.RemainingSeconds)
		}
	}
	if len(got.Accounts) != 2 || byAccount["alice"] != 5 || byAccount["hash:"+sha8("ghost-raw")] != 5 {
		t.Fatalf("accounts = %+v, want alice and the ghost hash only (bob is below the threshold)", got.Accounts)
	}
	// The hash shown here is the very one the failures listing and the log use.
	if !strings.Contains(e.logs.String(), "account_hash="+sha8("ghost-raw")) {
		t.Fatalf("log does not carry the same hash:\n%s", e.logs.String())
	}

	clean := newSecHTTP(t, appauth.BackoffLog)
	body := clean.do(http.MethodGet, "/admin/v1/security/login-backoff", clean.adminToken, "").Body.String()
	if !strings.Contains(body, `"trusted_proxies_configured":false`) || !strings.Contains(body, `"accounts":[]`) || !strings.Contains(body, `"mode":"log"`) {
		t.Fatalf("default body = %s", body)
	}
}

func TestLoginBackoffAccountsExpireWithTheirWindow(t *testing.T) {
	t.Parallel()
	e := newSecHTTP(t, appauth.BackoffEnforce)
	e.newUser("alice")
	for i := 0; i < 5; i++ {
		e.exchange("alice", "wrong-password", "203.0.113.7")
	}
	count := func() int {
		var got struct {
			Accounts []struct{} `json:"accounts"`
		}
		_ = json.Unmarshal(e.do(http.MethodGet, "/admin/v1/security/login-backoff", e.adminToken, "").Body.Bytes(), &got)
		return len(got.Accounts)
	}
	if count() != 1 {
		t.Fatal("alice should be waiting")
	}
	e.clock.Advance(3 * time.Second)
	if count() != 0 {
		t.Fatal("a finished wait must not be listed as in backoff")
	}
}

func sha8(normalized string) string {
	return accountHashForTest(normalized)
}

func TestLoginFailuresTextFiltersDefaultToTheLast24HoursAndRangesAreCapped(t *testing.T) {
	t.Parallel()
	e := newSecHTTP(t, appauth.BackoffLog)
	now := time.Now().UTC()
	rec := func(at time.Time, account string) ports.LoginFailureRecord {
		return ports.LoginFailureRecord{WindowStart: at.Truncate(appauth.LoginFailureWindow), Endpoint: "/auth/token", Reason: "wrong password", Account: account, IP: "203.0.113.7", Count: 1, FirstSeen: at, LastSeen: at}
	}
	if err := e.store.AddLoginFailures(context.Background(), []ports.LoginFailureRecord{
		rec(now.Add(-time.Hour), "recent"), rec(now.Add(-72*time.Hour), "old"),
	}); err != nil {
		t.Fatal(err)
	}
	if page := e.failures(""); len(page.Failures) != 2 {
		t.Fatalf("unfiltered rows = %d, want 2", len(page.Failures))
	}
	for _, q := range []string{"?account=old", "?ip=203.0.113.7", "?reason=wrong+password"} {
		page := e.failures(q)
		for _, f := range page.Failures {
			if f.Account == "old" {
				t.Fatalf("%s returned a row older than the default 24 h range", q)
			}
		}
	}
	if page := e.failures("?account=old&from=" + now.Add(-96*time.Hour).Format(time.RFC3339)); len(page.Failures) != 1 {
		t.Fatalf("explicit from rows = %d, want the old row", len(page.Failures))
	}
	for name, q := range map[string]string{
		"from 31 days ago":     "?from=" + now.Add(-31*24*time.Hour).Format(time.RFC3339),
		"explicit 60 day span": "?from=" + now.Add(-70*24*time.Hour).Format(time.RFC3339) + "&to=" + now.Add(-10*24*time.Hour).Format(time.RFC3339),
	} {
		if rec := e.do(http.MethodGet, "/admin/v1/security/login-failures"+q, e.adminToken, ""); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d, want 422 (%s)", name, rec.Code, rec.Body.String())
		}
	}
	if rec := e.do(http.MethodGet, "/admin/v1/security/login-failures?from="+now.Add(-29*24*time.Hour).Format(time.RFC3339), e.adminToken, ""); rec.Code != http.StatusOK {
		t.Errorf("29 day range = %d, want 200", rec.Code)
	}
}
