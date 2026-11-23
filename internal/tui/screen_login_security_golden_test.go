package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"regixtry/internal/ports"
)

var updateLoginSecurityGoldens = flag.Bool("update-login-security", false, "rewrite the Login security golden files")

func assertLoginSecurityGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "login_security", name+".golden")
	if *updateLoginSecurityGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update-login-security): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("rendered view differs from %s (run with -update-login-security to inspect)\n--- want\n%s\n--- got\n%s", path, strings.TrimRight(string(want), "\n"), strings.TrimRight(got, "\n"))
	}
}

func TestLoginSecurityViewGoldens(t *testing.T) {
	logMode := func() *fakeAdminClient {
		c := loginScreenClient()
		c.loginAPI.backoff.Mode = loginModeLog
		return c
	}
	cases := []struct {
		name   string
		client func() *fakeAdminClient
		steps  []keyMsg
	}{
		{"enforce", loginScreenClient, nil},
		{"enforce_failures_focused", loginScreenClient, []keyMsg{testKeyTab(), testKeyDown()}},
		{"log_mode", logMode, nil},
		{"off_mode", func() *fakeAdminClient {
			c := loginScreenClient()
			c.loginAPI.backoff.Mode = loginModeOff
			c.loginAPI.backoff.Accounts = nil
			c.loginAPI.summary.AccountsInBackoff = 0
			return c
		}, nil},
		{"empty", func() *fakeAdminClient {
			c := &fakeAdminClient{}
			c.loginAPI.backoff = ports.AdminLoginBackoff{Mode: loginModeEnforce, Threshold: 5, BaseSeconds: 2, MaxSeconds: 300, MaxConcurrency: 8, TrustedProxiesConfigured: true}
			return c
		}, nil},
		{"error", func() *fakeAdminClient {
			c := loginScreenClient()
			c.loginAPI.backoffErr = fmt.Errorf("connection refused")
			return c
		}, nil},
		{"not_allowed", func() *fakeAdminClient {
			c := loginScreenClient()
			c.loginAPI.summaryErr = adminStatusError{status: 403, message: "x", detail: "global administrator required"}
			return c
		}, nil},
		{"confirm_known", loginScreenClient, []keyMsg{testKeyRunes("x")}},
		{"confirm_unknown", loginScreenClient, []keyMsg{testKeyDown(), testKeyRunes("x")}},
		{"reset_done", loginScreenClient, []keyMsg{testKeyRunes("x"), testKeyEnter()}},
		{"filtering", loginScreenClient, []keyMsg{testKeyRunes("/"), testKeyRunes("b"), testKeyRunes("a"), testKeyRunes("d")}},
		{"filtered", loginScreenClient, []keyMsg{testKeyRunes("/"), testKeyRunes("t"), testKeyRunes("h"), testKeyRunes("r"), testKeyEnter()}},
		{"paged", func() *fakeAdminClient {
			c := loginScreenClient()
			for i := range 40 {
				at := fixedTestTime.Add(-time.Duration(30+i) * time.Minute)
				c.loginAPI.failures = append(c.loginAPI.failures, ports.LoginFailureRecord{WindowStart: at, LastSeen: at, Endpoint: "/auth/token",
					Reason: "unknown user", Account: fmt.Sprintf("hash:%08x", i*2654435761%0xffffffff), IP: fmt.Sprintf("2001:db8::%x", i), Count: int64(i + 1)})
			}
			c.loginAPI.pageSize = 10
			return c
		}, []keyMsg{testKeyTab(), testKeyPgDown(), testKeyDown(), testKeyDown()}},
	}
	for _, tc := range cases {
		for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
			name := fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h)
			t.Run(name, func(t *testing.T) {
				m := loginModelAt(t, tc.client(), size.w, size.h)
				for _, k := range tc.steps {
					m = overviewPress(t, m, k)
				}
				assertLoginSecurityGolden(t, name, overviewRender(m))
			})
		}
	}
	t.Run("loading_150x24", func(t *testing.T) {
		m := newAdminReadyModelWithCatalog(t, []string{"team/api"}, loginScreenClient())
		m.now = func() time.Time { return fixedTestTime }
		m = resizeModel(t, m, 150, 24)
		m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedTestTime.Add(24 * time.Hour)}
		m.adminAuth = adminAuthStateAuthenticated
		updated, _ := m.Update(navigateMsg{To: screenAdminLoginSecurity})
		assertLoginSecurityGolden(t, "loading_150x24", overviewRender(updated.(Model)))
	})
}

func TestOverviewLoginRowGoldens(t *testing.T) {
	for name, client := range map[string]func() *fakeAdminClient{
		"overview_login_warning": func() *fakeAdminClient { return loginOverviewClient(42, 3, "enforce") },
		"overview_login_log":     func() *fakeAdminClient { return loginOverviewClient(42, 3, "log") },
		"overview_login_muted":   func() *fakeAdminClient { return loginOverviewClient(1, 1, "off") },
	} {
		for _, size := range overviewViewSizes {
			full := fmt.Sprintf("%s_%dx%d", name, size.w, size.h)
			t.Run(full, func(t *testing.T) {
				m := overviewModelAt(t, client(), size.w, size.h)
				m = overviewPress(t, m, testKeyTab(), testKeyTab(), testKeyTab())
				assertOverviewGolden(t, full, overviewRender(m))
			})
		}
	}
}
