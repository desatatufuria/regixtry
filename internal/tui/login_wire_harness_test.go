package tui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	appauth "regixtry/internal/app/auth"
	appregixtry "regixtry/internal/app/regixtry"
	domainauth "regixtry/internal/domain/auth"
	authpostgres "regixtry/internal/infra/auth/postgres"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
	regixtryhttp "regixtry/internal/protocol/http"
)

const loginWirePassword = "fixture-pass-9x"

// loginWireClock is the backoff's clock: tests move it instead of sleeping.
type loginWireClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *loginWireClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *loginWireClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// loginWire is the real stack behind the login security screens: the HTTP
// router with the auth service (limiter, backoff, failure aggregator) over a
// sqlite store, reached through the real HTTPAdminClient.
type loginWire struct {
	t       *testing.T
	server  *httptest.Server
	client  *HTTPAdminClient
	auth    *appauth.Service
	admin   domainauth.Principal
	session AdminSession
	clock   *loginWireClock
}

func newLoginWire(t *testing.T, mode appauth.BackoffMode) *loginWire {
	t.Helper()
	dir := t.TempDir()
	blob, err := fsblob.New(filepath.Join(dir, "c"))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := metadata.New(filepath.Join(dir, "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	store, err := authpostgres.NewWithDriver("sqlite", "file:"+filepath.Join(dir, "a.db")+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	clock := &loginWireClock{now: time.Now()}
	as := appauth.NewService(store)
	as.ConfigureLogin(appauth.LoginPolicy{
		MaxConcurrency: 8, MaxWait: 5 * time.Second,
		Compare: func(_, password []byte) error {
			if string(password) == loginWirePassword {
				return nil
			}
			return errors.New("mismatch")
		},
		Backoff: &appauth.BackoffPolicy{Mode: mode, Threshold: 5, Base: 2 * time.Second, Max: 300 * time.Second, Now: clock.Now},
	})
	agg := appauth.NewLoginFailureAggregator(store, appauth.LoginFailureOptions{})
	as.SetLoginFailureAggregator(agg)

	ac := ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "regixtry"})
	svc := appregixtry.NewService(blob, meta, ac, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	router := regixtryhttp.NewRouter(svc, as, regixtryhttp.WithLoginFailureRecorder(agg))

	boot, err := as.BootstrapAdmin(context.Background(), ports.BootstrapAdminInput{Username: "admin", Password: loginWirePassword})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	client, err := NewHTTPAdminClient(server.URL, &http.Client{Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	w := &loginWire{t: t, server: server, client: client, auth: as, clock: clock,
		admin: domainauth.Principal{UserID: boot.User.ID, Username: "admin", IsAdmin: true}}
	login, err := as.LoginWithPassword(context.Background(), "admin", loginWirePassword, nil)
	if err != nil {
		t.Fatal(err)
	}
	w.session = AdminSession{Username: "admin", BearerToken: login.BearerToken, ExpiresAt: time.Now().Add(time.Hour)}
	return w
}

// user creates an enabled user.
func (w *loginWire) user(name string) {
	w.t.Helper()
	if _, err := w.auth.CreateAdminUser(context.Background(), w.admin, ports.AdminCreateUserInput{Username: name, Password: loginWirePassword, Enabled: true}); err != nil {
		w.t.Fatal(err)
	}
}

// exchange runs the real docker token exchange and returns the HTTP status.
func (w *loginWire) exchange(username, password string) int {
	w.t.Helper()
	req, err := http.NewRequest(http.MethodGet, w.server.URL+"/auth/token?service=regixtry", nil)
	if err != nil {
		w.t.Fatal(err)
	}
	req.SetBasicAuth(username, password)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		w.t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// fail runs n failed password logins for username.
func (w *loginWire) fail(username string, n int) {
	w.t.Helper()
	for range n {
		w.exchange(username, "wrong-password")
		// Each failure past the threshold starts a wait: let it pass so the
		// next attempt is checked, not refused.
		w.clock.advance(10 * time.Minute)
	}
}

// loginWireEnv is a screenEnv for the real client with the admin session.
func (w *loginWire) env(width, height int) screenEnv {
	env := usersTestEnv(w.client, width, height)
	env.Session = w.session
	return env
}
