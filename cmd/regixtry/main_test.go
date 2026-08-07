package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appregixtry "regixtry/internal/app/regixtry"
	authpostgres "regixtry/internal/infra/auth/postgres"
	installlinux "regixtry/internal/infra/install/linux"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
)

func TestDefaultBuildValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    string
		fallback string
		want     string
	}{
		{
			name:     "keeps non-empty value",
			value:    "v1.2.3",
			fallback: "dev",
			want:     "v1.2.3",
		},
		{
			name:     "falls back for empty string",
			value:    "",
			fallback: "dev",
			want:     "dev",
		},
		{
			name:     "falls back for whitespace",
			value:    "   ",
			fallback: "unknown",
			want:     "unknown",
		},
	}

	for _, testCase := range tests {
		tt := testCase
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := defaultBuildValue(tt.value, tt.fallback); got != tt.want {
				t.Fatalf("defaultBuildValue(%q, %q) = %q, want %q", tt.value, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestReleaseMetadataUsesDefaultsAndOverrides(t *testing.T) {
	previousVersion := buildVersion
	previousCommit := buildCommit
	previousDate := buildDate
	t.Cleanup(func() {
		buildVersion = previousVersion
		buildCommit = previousCommit
		buildDate = previousDate
	})

	tests := []struct {
		name    string
		version string
		commit  string
		date    string
		want    string
	}{
		{
			name:    "uses defaults when values are blank",
			version: "",
			commit:  " ",
			date:    "",
			want:    "version=dev commit=unknown date=unknown",
		},
		{
			name:    "uses overridden release metadata",
			version: "1.2.3",
			commit:  "abc1234",
			date:    "2026-08-06T23:30:00Z",
			want:    "version=1.2.3 commit=abc1234 date=2026-08-06T23:30:00Z",
		},
	}

	for _, testCase := range tests {
		tt := testCase
		t.Run(tt.name, func(t *testing.T) {
			buildVersion = tt.version
			buildCommit = tt.commit
			buildDate = tt.date

			if got := releaseMetadata(); got != tt.want {
				t.Fatalf("releaseMetadata() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseServeConfigAnonymousAccessDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := parseServeConfig(nil)
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}

	if cfg.AllowAnonymousPull {
		t.Fatal("AllowAnonymousPull = true, want false")
	}

	if cfg.AllowAnonymousPush {
		t.Fatal("AllowAnonymousPush = true, want false")
	}
}

func TestParseServeConfigAllowsAnonymousPushFlag(t *testing.T) {
	t.Parallel()

	cfg, err := parseServeConfig([]string{"-allow-anonymous-push", "-allow-anonymous-pull"})
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}

	if !cfg.AllowAnonymousPull {
		t.Fatal("AllowAnonymousPull = false, want true")
	}

	if !cfg.AllowAnonymousPush {
		t.Fatal("AllowAnonymousPush = false, want true")
	}
}

func TestParseServeConfigParsesAuthPostgresDSN(t *testing.T) {
	t.Parallel()

	cfg, err := parseServeConfig([]string{"-auth-postgres-dsn", "postgres://auth"})
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}

	if cfg.AuthPostgresDSN != "postgres://auth" {
		t.Fatalf("AuthPostgresDSN = %q, want %q", cfg.AuthPostgresDSN, "postgres://auth")
	}
}

func TestParseServeConfigParsesAuthTokenRealmURL(t *testing.T) {
	t.Parallel()

	cfg, err := parseServeConfig([]string{"-auth-token-realm", "http://127.0.0.1:5000/auth/token"})
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}

	if cfg.AuthTokenRealmURL != "http://127.0.0.1:5000/auth/token" {
		t.Fatalf("AuthTokenRealmURL = %q, want %q", cfg.AuthTokenRealmURL, "http://127.0.0.1:5000/auth/token")
	}
}

func TestParseServeConfigParsesPublicURLAndTLSInputs(t *testing.T) {
	t.Parallel()

	cfg, err := parseServeConfig([]string{
		"-public-url", "https://regixtry.example.com",
		"-tls-cert-file", "/tmp/registry.crt",
		"-tls-key-file", "/tmp/registry.key",
	})
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}

	if cfg.PublicURL != "https://regixtry.example.com" {
		t.Fatalf("PublicURL = %q, want %q", cfg.PublicURL, "https://regixtry.example.com")
	}
	if cfg.TLSCertFile != "/tmp/registry.crt" {
		t.Fatalf("TLSCertFile = %q, want %q", cfg.TLSCertFile, "/tmp/registry.crt")
	}
	if cfg.TLSKeyFile != "/tmp/registry.key" {
		t.Fatalf("TLSKeyFile = %q, want %q", cfg.TLSKeyFile, "/tmp/registry.key")
	}
	if cfg.ReadHeaderTimeout != defaultReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", cfg.ReadHeaderTimeout, defaultReadHeaderTimeout)
	}
	if cfg.ShutdownTimeout != defaultShutdownTimeout {
		t.Fatalf("ShutdownTimeout = %s, want %s", cfg.ShutdownTimeout, defaultShutdownTimeout)
	}
}

func TestNormalizeRuntimeConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		cfg            serveConfig
		wantPublicURL  string
		wantTokenRealm string
		wantTLSEnabled bool
		wantErr        string
	}{
		{
			name: "https public URL requires TLS pair and derives token realm",
			cfg: serveConfig{
				PublicURL:         "https://regixtry.example.com/edge/",
				TLSCertFile:       "/tmp/registry.crt",
				TLSKeyFile:        "/tmp/registry.key",
				ReadHeaderTimeout: defaultReadHeaderTimeout,
				ShutdownTimeout:   defaultShutdownTimeout,
			},
			wantPublicURL:  "https://regixtry.example.com/edge",
			wantTokenRealm: "https://regixtry.example.com/edge/auth/token",
			wantTLSEnabled: true,
		},
		{
			name: "explicit local http mode stays http without TLS",
			cfg: serveConfig{
				PublicURL:         "http://127.0.0.1:5000",
				ReadHeaderTimeout: defaultReadHeaderTimeout,
				ShutdownTimeout:   defaultShutdownTimeout,
			},
			wantPublicURL:  "http://127.0.0.1:5000",
			wantTokenRealm: "http://127.0.0.1:5000/auth/token",
		},
		{
			name: "configured auth realm must match derived token realm",
			cfg: serveConfig{
				PublicURL:         "https://regixtry.example.com",
				TLSCertFile:       "/tmp/registry.crt",
				TLSKeyFile:        "/tmp/registry.key",
				AuthTokenRealmURL: "https://regixtry.example.com/custom/token",
				ReadHeaderTimeout: defaultReadHeaderTimeout,
				ShutdownTimeout:   defaultShutdownTimeout,
			},
			wantErr: "auth token realm URL must match derived public token realm",
		},
		{
			name: "https public URL rejects incomplete TLS pair",
			cfg: serveConfig{
				PublicURL:         "https://regixtry.example.com",
				TLSCertFile:       "/tmp/registry.crt",
				ReadHeaderTimeout: defaultReadHeaderTimeout,
				ShutdownTimeout:   defaultShutdownTimeout,
			},
			wantErr: "TLS cert file and key file must both be set",
		},
		{
			name: "http public URL forbids TLS inputs",
			cfg: serveConfig{
				PublicURL:         "http://127.0.0.1:5000",
				TLSCertFile:       "/tmp/registry.crt",
				TLSKeyFile:        "/tmp/registry.key",
				ReadHeaderTimeout: defaultReadHeaderTimeout,
				ShutdownTimeout:   defaultShutdownTimeout,
			},
			wantErr: "http public URL cannot be combined with TLS cert/key inputs",
		},
		{
			name: "public URL is required",
			cfg: serveConfig{
				ReadHeaderTimeout: defaultReadHeaderTimeout,
				ShutdownTimeout:   defaultShutdownTimeout,
			},
			wantErr: "public URL is required",
		},
	}

	for _, testCase := range tests {
		tt := testCase
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeRuntimeConfig(tt.cfg)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("normalizeRuntimeConfig() error = nil, want %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("normalizeRuntimeConfig() error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeRuntimeConfig() error = %v", err)
			}
			if got.publicURL.String() != tt.wantPublicURL {
				t.Fatalf("publicURL = %q, want %q", got.publicURL.String(), tt.wantPublicURL)
			}
			if got.tokenRealmURL.String() != tt.wantTokenRealm {
				t.Fatalf("tokenRealmURL = %q, want %q", got.tokenRealmURL.String(), tt.wantTokenRealm)
			}
			if got.tlsEnabled != tt.wantTLSEnabled {
				t.Fatalf("tlsEnabled = %t, want %t", got.tlsEnabled, tt.wantTLSEnabled)
			}
		})
	}
}

func TestParseTUIConfigParsesAdminAPIBaseURL(t *testing.T) {
	t.Parallel()

	cfg, err := parseTUIConfig([]string{"-api-base-url", "http://127.0.0.1:5000/"})
	if err != nil {
		t.Fatalf("parseTUIConfig() error = %v", err)
	}

	if cfg.APIBaseURL != "http://127.0.0.1:5000" {
		t.Fatalf("APIBaseURL = %q, want %q", cfg.APIBaseURL, "http://127.0.0.1:5000")
	}
}

func TestParseTUIConfigRejectsRelativeAdminAPIBaseURL(t *testing.T) {
	t.Parallel()

	_, err := parseTUIConfig([]string{"-api-base-url", "/admin"})
	if err == nil {
		t.Fatal("parseTUIConfig() error = nil, want invalid admin API base URL")
	}
	if !strings.Contains(err.Error(), "absolute http(s) URL") {
		t.Fatalf("parseTUIConfig() error = %v, want absolute URL validation", err)
	}
}

func TestParseBootstrapAdminConfigPrefersStdinSecretAndWarnsOnLegacyArgv(t *testing.T) {
	t.Parallel()

	cfg, err := parseBootstrapAdminConfig(
		[]string{"-auth-postgres-dsn", "postgres://auth", "-password", "legacy-secret", "-password-stdin"},
		strings.NewReader("safer-secret\n"),
	)
	if err != nil {
		t.Fatalf("parseBootstrapAdminConfig() error = %v", err)
	}

	if cfg.Password != "safer-secret" {
		t.Fatalf("Password = %q, want %q", cfg.Password, "safer-secret")
	}
	if cfg.PasswordSource != "stdin" {
		t.Fatalf("PasswordSource = %q, want %q", cfg.PasswordSource, "stdin")
	}
	if !strings.Contains(cfg.PasswordWarning, "-password is discouraged") {
		t.Fatalf("PasswordWarning = %q, want discouraged argv guidance", cfg.PasswordWarning)
	}
}

func TestParseBootstrapAdminConfigWarnsWhenUsingLegacyPasswordFlag(t *testing.T) {
	t.Parallel()

	cfg, err := parseBootstrapAdminConfig(
		[]string{"-auth-postgres-dsn", "postgres://auth", "-password", "legacy-secret"},
		strings.NewReader(""),
	)
	if err != nil {
		t.Fatalf("parseBootstrapAdminConfig() error = %v", err)
	}

	if cfg.PasswordSource != "argv" {
		t.Fatalf("PasswordSource = %q, want %q", cfg.PasswordSource, "argv")
	}
	if !strings.Contains(cfg.PasswordWarning, "-password is discouraged") {
		t.Fatalf("PasswordWarning = %q, want discouraged argv guidance", cfg.PasswordWarning)
	}
}

func TestBootstrapParseConfigRejectsUnsupportedModeAndWhitespacePaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "rejects unsupported mode",
			args:    []string{"-mode", "containers", "-public-url", "http://127.0.0.1:5000"},
			wantErr: `unsupported mode "containers"`,
		},
		{
			name:    "rejects whitespace storage root",
			args:    []string{"-mode", "daemon-sqlite", "-public-url", "http://127.0.0.1:5000", "-storage-root", "/tmp/registry data"},
			wantErr: "storage-root must not contain whitespace",
		},
	}

	for _, testCase := range tests {
		tt := testCase
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseBootstrapConfig(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("parseBootstrapConfig() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestBootstrapParseConfigSupportsNoStart(t *testing.T) {
	t.Parallel()

	cfg, err := parseBootstrapConfig([]string{"-mode", "daemon-sqlite", "-public-url", "http://127.0.0.1:5000", "-no-start"})
	if err != nil {
		t.Fatalf("parseBootstrapConfig() error = %v", err)
	}
	if !cfg.NoStart {
		t.Fatal("NoStart = false, want true")
	}
}

func TestRunBootstrapPropagatesHostAndRuntimeFailures(t *testing.T) {
	tests := []struct {
		name    string
		runErr  error
		wantErr string
	}{
		{
			name:    "unsupported os release",
			runErr:  errors.New(`unsupported Linux distribution "fedora"`),
			wantErr: `unsupported Linux distribution "fedora"`,
		},
		{
			name:    "missing systemd",
			runErr:  errors.New("systemd runtime not detected at /run/systemd/system"),
			wantErr: "systemd runtime not detected",
		},
		{
			name:    "systemctl enable failure",
			runErr:  errors.New("systemctl enable --now regixtry.service: exit status 1"),
			wantErr: "systemctl enable --now regixtry.service",
		},
		{
			name:    "probe failure",
			runErr:  errors.New("registry readiness probe failed: connect: connection refused"),
			wantErr: "registry readiness probe failed",
		},
		{
			name:    "occupied local bind recovery guidance",
			runErr:  errors.New("configured local bind address 127.0.0.1:5000 is already in use\nRecover with:\n  sudo ss -ltnp 'sport = :5000'\n  sudo systemctl stop regixtry.service"),
			wantErr: "sudo systemctl stop regixtry.service",
		},
	}

	for _, testCase := range tests {
		tt := testCase
		t.Run(tt.name, func(t *testing.T) {
			runner := &stubBootstrapRunner{runErr: tt.runErr}
			restore := swapBootstrapRunner(t, runner)
			defer restore()

			err := run(context.Background(), []string{"bootstrap", "-mode", "daemon-sqlite", "-public-url", "http://127.0.0.1:5000"}, io.Discard, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("run(bootstrap) error = %v, want substring %q", err, tt.wantErr)
			}
			if runner.runCalls != 1 {
				t.Fatalf("runCalls = %d, want 1", runner.runCalls)
			}
			if runner.rollbackCalls != 0 {
				t.Fatalf("rollbackCalls = %d, want 0", runner.rollbackCalls)
			}
		})
	}
}

func TestRunBootstrapPassesParsedConfigToRunner(t *testing.T) {
	runner := &stubBootstrapRunner{}
	restore := swapBootstrapRunner(t, runner)
	defer restore()

	args := []string{
		"bootstrap",
		"-mode", "daemon-sqlite",
		"-public-url", "https://regixtry.example.com",
		"-addr", "0.0.0.0:5443",
		"-storage-root", "/var/lib/regixtry-data",
		"-state-path", "/etc/regixtry/bootstrap-state.json",
		"-unit-path", "/etc/systemd/system/registry-custom.service",
		"-service", "registry-custom",
		"-no-start",
	}

	if err := run(context.Background(), args, io.Discard, io.Discard); err != nil {
		t.Fatalf("run(bootstrap) error = %v", err)
	}

	if runner.runCalls != 1 {
		t.Fatalf("runCalls = %d, want 1", runner.runCalls)
	}
	if runner.rollbackCalls != 0 {
		t.Fatalf("rollbackCalls = %d, want 0", runner.rollbackCalls)
	}

	want := installlinux.BootstrapConfig{
		Mode:        "daemon-sqlite",
		PublicURL:   "https://regixtry.example.com",
		Addr:        "0.0.0.0:5443",
		StorageRoot: "/var/lib/regixtry-data",
		StatePath:   "/etc/regixtry/bootstrap-state.json",
		UnitPath:    "/etc/systemd/system/registry-custom.service",
		ServiceName: "registry-custom",
		NoStart:     true,
	}
	if runner.lastConfig != want {
		t.Fatalf("lastConfig = %#v, want %#v", runner.lastConfig, want)
	}
}

func TestRunBootstrapRollbackUsesRunnerRollback(t *testing.T) {
	runner := &stubBootstrapRunner{}
	restore := swapBootstrapRunner(t, runner)
	defer restore()

	err := run(
		context.Background(),
		[]string{"bootstrap", "-mode", "daemon-sqlite", "-public-url", "http://127.0.0.1:5000", "-rollback", "-state-path", "/etc/regixtry/bootstrap-state.json"},
		io.Discard,
		io.Discard,
	)
	if err != nil {
		t.Fatalf("run(bootstrap rollback) error = %v", err)
	}
	if runner.runCalls != 0 {
		t.Fatalf("runCalls = %d, want 0", runner.runCalls)
	}
	if runner.rollbackCalls != 1 {
		t.Fatalf("rollbackCalls = %d, want 1", runner.rollbackCalls)
	}
	if !runner.lastConfig.Rollback {
		t.Fatal("lastConfig.Rollback = false, want true")
	}
	if runner.lastConfig.StatePath != "/etc/regixtry/bootstrap-state.json" {
		t.Fatalf("StatePath = %q, want %q", runner.lastConfig.StatePath, "/etc/regixtry/bootstrap-state.json")
	}
}

func TestRunBootstrapAdminPrintsLegacyPasswordWarningToStderr(t *testing.T) {
	restore := swapAuthStoreOpener(t)
	defer restore()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	args := []string{"bootstrap-admin", "-auth-postgres-dsn", filepath.Join(t.TempDir(), "auth.db"), "-username", "admin", "-password", "legacy-secret"}

	if err := run(context.Background(), args, stdout, stderr); err != nil {
		t.Fatalf("run(bootstrap-admin) error = %v", err)
	}
	if !strings.Contains(stderr.String(), "-password is discouraged") {
		t.Fatalf("stderr = %q, want discouraged argv guidance", stderr.String())
	}
	if !strings.Contains(stdout.String(), "bootstrapped global admin") {
		t.Fatalf("stdout = %q, want bootstrap success message", stdout.String())
	}
}

func TestServeStartsAndRespondsToPing(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout := &bytes.Buffer{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(ctx, listener, serveConfig{
			StorageRoot:        t.TempDir(),
			DatabasePath:       filepath.Join(t.TempDir(), "registry.db"),
			AllowAnonymousPull: true,
		}, stdout)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		response, requestErr := http.Get("http://" + listener.Addr().String() + "/v2/")
		if requestErr == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
			}
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("server did not become ready: %v", requestErr)
		}

		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("serve() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "regixtry serving on") {
		t.Fatalf("stdout = %q, want start message", stdout.String())
	}
}

func TestServeStartsAndRespondsToPingOverTLS(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeTestTLSCertificate(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout := &bytes.Buffer{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(ctx, listener, serveConfig{
			StorageRoot:        t.TempDir(),
			DatabasePath:       filepath.Join(t.TempDir(), "registry.db"),
			AllowAnonymousPull: true,
			TLSCertFile:        certFile,
			TLSKeyFile:         keyFile,
		}, stdout)
	}()

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		response, requestErr := httpClient.Get("https://" + listener.Addr().String() + "/v2/")
		if requestErr == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
			}
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("TLS server did not become ready: %v", requestErr)
		}

		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("serve() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "regixtry serving on") {
		t.Fatalf("stdout = %q, want start message", stdout.String())
	}
}

func TestServeShutdownDeadlineStopsWaitingOnActiveUpload(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- serve(ctx, listener, serveConfig{
			StorageRoot:        t.TempDir(),
			DatabasePath:       filepath.Join(t.TempDir(), "registry.db"),
			AllowAnonymousPush: true,
			ShutdownTimeout:    50 * time.Millisecond,
		}, io.Discard)
	}()

	uploadLocation := waitForUploadLocation(t, listener.Addr().String())

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	if _, err := io.WriteString(conn, "PATCH "+uploadLocation+" HTTP/1.1\r\nHost: "+listener.Addr().String()+"\r\nContent-Type: application/octet-stream\r\nContent-Length: 1048576\r\n\r\npartial-upload-body"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-serveErrCh:
		if err != nil {
			t.Fatalf("serve() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve() did not stop waiting after shutdown deadline")
	}
}

func TestNewHTTPServerAppliesBoundedTimeouts(t *testing.T) {
	t.Parallel()

	t.Run("uses configured finite bounds", func(t *testing.T) {
		t.Parallel()

		server := newHTTPServer(serveConfig{
			ReadHeaderTimeout: 7 * time.Second,
			ReadTimeout:       8 * time.Second,
			WriteTimeout:      9 * time.Second,
			IdleTimeout:       10 * time.Second,
		}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

		if server.ReadHeaderTimeout != 7*time.Second {
			t.Fatalf("ReadHeaderTimeout = %s, want %s", server.ReadHeaderTimeout, 7*time.Second)
		}
		if server.ReadTimeout != 8*time.Second {
			t.Fatalf("ReadTimeout = %s, want %s", server.ReadTimeout, 8*time.Second)
		}
		if server.WriteTimeout != 9*time.Second {
			t.Fatalf("WriteTimeout = %s, want %s", server.WriteTimeout, 9*time.Second)
		}
		if server.IdleTimeout != 10*time.Second {
			t.Fatalf("IdleTimeout = %s, want %s", server.IdleTimeout, 10*time.Second)
		}
	})

	t.Run("falls back to hardened defaults when config is zero", func(t *testing.T) {
		t.Parallel()

		server := newHTTPServer(serveConfig{}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

		if server.ReadHeaderTimeout != defaultReadHeaderTimeout {
			t.Fatalf("ReadHeaderTimeout = %s, want %s", server.ReadHeaderTimeout, defaultReadHeaderTimeout)
		}
		if server.ReadTimeout != defaultReadTimeout {
			t.Fatalf("ReadTimeout = %s, want %s", server.ReadTimeout, defaultReadTimeout)
		}
		if server.WriteTimeout != defaultWriteTimeout {
			t.Fatalf("WriteTimeout = %s, want %s", server.WriteTimeout, defaultWriteTimeout)
		}
		if server.IdleTimeout != defaultIdleTimeout {
			t.Fatalf("IdleTimeout = %s, want %s", server.IdleTimeout, defaultIdleTimeout)
		}
	})
}

func TestNewHandlerAnonymousPushWiring(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		allowAnonymousPush bool
		wantStatus         int
	}{
		{
			name:       "anonymous push disabled stays unauthorized",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:               "anonymous push enabled allows upload start",
			allowAnonymousPush: true,
			wantStatus:         http.StatusAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			storageRoot := t.TempDir()
			handler, cleanup, err := newHandler(serveConfig{
				StorageRoot:        storageRoot,
				DatabasePath:       filepath.Join(storageRoot, "registry.db"),
				AllowAnonymousPush: tt.allowAnonymousPush,
			})
			if err != nil {
				t.Fatalf("newHandler() error = %v", err)
			}
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusAccepted {
				if got := recorder.Header().Get("Location"); !strings.HasPrefix(got, "/v2/library/alpine/blobs/uploads/") {
					t.Fatalf("Location = %q, want upload location", got)
				}
			}
		})
	}
}

func TestNewHandlerFailsFastWhenAuthEnabledWithoutAdmin(t *testing.T) {
	restore := swapAuthStoreOpener(t)
	defer restore()

	storageRoot := t.TempDir()
	_, cleanup, err := newHandler(serveConfig{
		StorageRoot:     storageRoot,
		DatabasePath:    filepath.Join(storageRoot, "registry.db"),
		AuthPostgresDSN: filepath.Join(t.TempDir(), "auth.db"),
	})
	if err == nil {
		cleanup()
		t.Fatal("newHandler() error = nil, want bootstrap admin failure")
	}
	if !strings.Contains(err.Error(), "bootstrap-admin") {
		t.Fatalf("newHandler() error = %v, want bootstrap-admin guidance", err)
	}
}

func TestRunBootstrapAdminIsIdempotent(t *testing.T) {
	restore := swapAuthStoreOpener(t)
	defer restore()

	authDB := filepath.Join(t.TempDir(), "auth.db")
	stdout := &bytes.Buffer{}
	args := []string{"bootstrap-admin", "-auth-postgres-dsn", authDB, "-username", "admin", "-password", "change-me-now"}
	if err := run(context.Background(), args, stdout, io.Discard); err != nil {
		t.Fatalf("run(first bootstrap) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "bootstrapped global admin") {
		t.Fatalf("stdout = %q, want bootstrap message", stdout.String())
	}

	stdout.Reset()
	if err := run(context.Background(), args, stdout, io.Discard); err != nil {
		t.Fatalf("run(second bootstrap) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "already configured") {
		t.Fatalf("stdout = %q, want idempotent message", stdout.String())
	}

	storageRoot := t.TempDir()
	handler, cleanup, err := newHandler(serveConfig{
		StorageRoot:        storageRoot,
		DatabasePath:       filepath.Join(storageRoot, "registry.db"),
		AllowAnonymousPull: true,
		AuthPostgresDSN:    authDB,
	})
	if err != nil {
		t.Fatalf("newHandler() error = %v", err)
	}
	defer cleanup()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v2/", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if challenge := recorder.Header().Get("WWW-Authenticate"); !strings.Contains(challenge, "Bearer") {
		t.Fatalf("WWW-Authenticate = %q, want Bearer challenge", challenge)
	}
}

func TestNewHandlerUsesConfiguredAuthTokenRealmInChallenge(t *testing.T) {
	restore := swapAuthStoreOpener(t)
	defer restore()

	authDB := filepath.Join(t.TempDir(), "auth.db")
	if err := run(context.Background(), []string{"bootstrap-admin", "-auth-postgres-dsn", authDB, "-username", "admin", "-password", "change-me-now"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("run(bootstrap-admin) error = %v", err)
	}

	storageRoot := t.TempDir()
	handler, cleanup, err := newHandler(serveConfig{
		StorageRoot:       storageRoot,
		DatabasePath:      filepath.Join(storageRoot, "registry.db"),
		AuthPostgresDSN:   authDB,
		AuthTokenRealmURL: "http://127.0.0.1:5000/auth/token",
	})
	if err != nil {
		t.Fatalf("newHandler() error = %v", err)
	}
	defer cleanup()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v2/team/app/tags/list", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	challenge := recorder.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, `realm="http://127.0.0.1:5000/auth/token"`) {
		t.Fatalf("WWW-Authenticate = %q, want configured token realm URL", challenge)
	}
}

func TestRunTUIRendersRepositorySnapshot(t *testing.T) {
	t.Parallel()

	storageRoot := t.TempDir()
	databasePath := filepath.Join(storageRoot, "registry.db")
	seedRegixtryState(t, storageRoot, databasePath)

	stdout := &bytes.Buffer{}
	err := runTUI(tuiConfig{StorageRoot: storageRoot, DatabasePath: databasePath, Tenant: "tenant-a", Snapshot: true}, strings.NewReader("q"), stdout)
	if err != nil {
		t.Fatalf("runTUI() error = %v", err)
	}

	view := stdout.String()
	if !strings.Contains(view, "Regixtry Console") || !strings.Contains(view, "library/alpine") {
		t.Fatalf("stdout = %q, want rendered repository view", view)
	}
}

func TestRunTUIDisablesAuthAdminShortcutWhenAuthIsEnabled(t *testing.T) {
	restore := swapAuthStoreOpener(t)
	defer restore()

	authDB := filepath.Join(t.TempDir(), "auth.db")
	if err := run(context.Background(), []string{"bootstrap-admin", "-auth-postgres-dsn", authDB, "-username", "admin", "-password", "change-me-now"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("run(bootstrap-admin) error = %v", err)
	}

	storageRoot := t.TempDir()
	databasePath := filepath.Join(storageRoot, "registry.db")
	seedRegixtryState(t, storageRoot, databasePath)

	stdout := &bytes.Buffer{}
	err := runTUI(tuiConfig{StorageRoot: storageRoot, DatabasePath: databasePath, Tenant: "tenant-a", AuthPostgresDSN: authDB, Snapshot: true}, strings.NewReader("q"), stdout)
	if err != nil {
		t.Fatalf("runTUI() error = %v", err)
	}

	view := stdout.String()
	if strings.Contains(view, "a: admin") {
		t.Fatalf("stdout = %q, want auth admin shortcut removed", view)
	}
	if strings.Contains(view, "Auth-backed admin actions are disabled in the local TUI until a real operator login flow exists.") {
		t.Fatalf("stdout = %q, want local admin shortcut notice removed", view)
	}
	if !strings.Contains(view, "library/alpine") {
		t.Fatalf("stdout = %q, want repository snapshot to stay available", view)
	}
}

func seedRegixtryState(t *testing.T, storageRoot string, databasePath string) {
	t.Helper()

	blobStore, err := fsblob.New(filepath.Join(storageRoot, "content"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}

	metadataStore, err := metadata.New(databasePath)
	if err != nil {
		t.Fatalf("sqlite.New() error = %v", err)
	}
	defer metadataStore.Close()

	service := appregixtry.NewService(blobStore, metadataStore, localOperatorAccessController{}, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	upload, err := service.BeginUpload(context.Background(), "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if _, err := service.AppendUpload(context.Background(), "library/alpine", upload.ID, strings.NewReader("layer-one")); err != nil {
		t.Fatalf("AppendUpload() error = %v", err)
	}
	digest := "sha256:0139c1c77468f75e6763a4612262743bd47a36b26cb2863d662756b3377bb029"
	if _, err := service.CompleteUpload(context.Background(), "library/alpine", upload.ID, digest, nil); err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	manifest := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + digest + `","size":9}]}`)
	if _, err := service.PublishManifest(context.Background(), "library/alpine", "latest", "application/vnd.oci.image.manifest.v1+json", manifest); err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}
}

func swapAuthStoreOpener(t *testing.T) func() {
	t.Helper()

	previous := openAuthStore
	openAuthStore = func(dsn string) (ports.AuthStore, error) {
		return authpostgres.NewWithDriver("sqlite", dsn)
	}

	return func() {
		openAuthStore = previous
	}
}

func swapBootstrapRunner(t *testing.T, runner bootstrapRunner) func() {
	t.Helper()

	previous := newBootstrapRunner
	newBootstrapRunner = func() bootstrapRunner {
		return runner
	}

	return func() {
		newBootstrapRunner = previous
	}
}

type stubBootstrapRunner struct {
	runErr        error
	rollbackErr   error
	lastConfig    installlinux.BootstrapConfig
	runCalls      int
	rollbackCalls int
}

func (s *stubBootstrapRunner) Run(_ context.Context, cfg installlinux.BootstrapConfig) error {
	s.lastConfig = cfg
	s.runCalls++
	return s.runErr
}

func (s *stubBootstrapRunner) Rollback(_ context.Context, cfg installlinux.BootstrapConfig) error {
	s.lastConfig = cfg
	s.rollbackCalls++
	return s.rollbackErr
}

func writeTestTLSCertificate(t *testing.T) (string, string) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}

	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	certFile := filepath.Join(t.TempDir(), "registry.crt")
	keyFile := filepath.Join(t.TempDir(), "registry.key")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("WriteFile(cert) error = %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("WriteFile(key) error = %v", err)
	}

	return certFile, keyFile
}

func waitForUploadLocation(t *testing.T, address string) string {
	t.Helper()

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(2 * time.Second)
	for {
		request, err := http.NewRequest(http.MethodPost, "http://"+address+"/v2/library/alpine/blobs/uploads/", nil)
		if err != nil {
			t.Fatalf("NewRequest() error = %v", err)
		}
		response, err := client.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusAccepted {
				location := response.Header.Get("Location")
				if location == "" {
					t.Fatal("Location header = empty, want upload location")
				}
				return location
			}
		}

		if time.Now().After(deadline) {
			t.Fatalf("upload start did not become ready: %v", err)
		}

		time.Sleep(20 * time.Millisecond)
	}
}
