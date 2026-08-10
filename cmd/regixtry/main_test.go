package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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
		tc := testCase
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := defaultBuildValue(tc.value, tc.fallback); got != tc.want {
				t.Fatalf("defaultBuildValue(%q, %q) = %q, want %q", tc.value, tc.fallback, got, tc.want)
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

func TestRunVersionPrintsReleaseMetadata(t *testing.T) {
	previousVersion := buildVersion
	previousCommit := buildCommit
	previousDate := buildDate
	t.Cleanup(func() {
		buildVersion = previousVersion
		buildCommit = previousCommit
		buildDate = previousDate
	})

	buildVersion = "1.2.3"
	buildCommit = "abc1234"
	buildDate = "2026-08-09T12:00:00Z"

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "flag",
			args: []string{"--version"},
		},
		{
			name: "subcommand",
			args: []string{"version"},
		},
	}

	for _, testCase := range tests {
		tt := testCase
		t.Run(tt.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}

			if err := run(context.Background(), tt.args, stdout, stderr); err != nil {
				t.Fatalf("run(%v) error = %v", tt.args, err)
			}

			if got, want := stdout.String(), "version=1.2.3 commit=abc1234 date=2026-08-09T12:00:00Z\n"; got != want {
				t.Fatalf("stdout = %q, want %q", got, want)
			}

			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
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
			name: "direct TLS derives token realm from https public URL",
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
			name: "reverse proxy mode allows https public URL without local TLS files",
			cfg: serveConfig{
				PublicURL:         "https://regixtry.example.com",
				ReadHeaderTimeout: defaultReadHeaderTimeout,
				ShutdownTimeout:   defaultShutdownTimeout,
			},
			wantPublicURL:  "https://regixtry.example.com",
			wantTokenRealm: "https://regixtry.example.com/auth/token",
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
			name: "https public URL still rejects incomplete TLS pair",
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

func TestParseTUIConfigAutoDetectsSetupManagedRuntime(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	bootstrapStatePath := filepath.Join(tempDir, "etc", "regixtry", "bootstrap-state.json")
	lifecyclePath := installlinux.LifecycleProvenancePath(bootstrapStatePath)
	envPath := filepath.Join(filepath.Dir(lifecyclePath), "regixtry.env")

	for _, path := range []string{lifecyclePath, envPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}
	if err := os.WriteFile(lifecyclePath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(lifecyclePath) error = %v", err)
	}
	if err := os.WriteFile(envPath, []byte(strings.Join([]string{
		`REGISTRY_STORAGE_ROOT="/var/lib/regixtry"`,
		`REGISTRY_DATABASE_PATH="/var/lib/regixtry/metadata.db"`,
		`REGISTRY_AUTH_POSTGRES_DSN="postgres://regixtry:secret@127.0.0.1:5432/regixtry_auth?sslmode=disable"`,
		`REGISTRY_PUBLIC_URL="https://registry.example.com/admin/"`,
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(envPath) error = %v", err)
	}

	cfg, err := parseTUIConfigWithBootstrapStatePath(nil, bootstrapStatePath)
	if err != nil {
		t.Fatalf("parseTUIConfig() error = %v", err)
	}

	if cfg.StorageRoot != "/var/lib/regixtry" {
		t.Fatalf("StorageRoot = %q, want %q", cfg.StorageRoot, "/var/lib/regixtry")
	}
	if cfg.DatabasePath != "/var/lib/regixtry/metadata.db" {
		t.Fatalf("DatabasePath = %q, want %q", cfg.DatabasePath, "/var/lib/regixtry/metadata.db")
	}
	if cfg.AuthPostgresDSN != "postgres://regixtry:secret@127.0.0.1:5432/regixtry_auth?sslmode=disable" {
		t.Fatalf("AuthPostgresDSN = %q, want installed DSN", cfg.AuthPostgresDSN)
	}
	if cfg.APIBaseURL != "https://registry.example.com/admin" {
		t.Fatalf("APIBaseURL = %q, want %q", cfg.APIBaseURL, "https://registry.example.com/admin")
	}
}

func TestParseTUIConfigExplicitFlagsOverrideSetupManagedRuntime(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	bootstrapStatePath := filepath.Join(tempDir, "etc", "regixtry", "bootstrap-state.json")
	lifecyclePath := installlinux.LifecycleProvenancePath(bootstrapStatePath)
	envPath := filepath.Join(filepath.Dir(lifecyclePath), "regixtry.env")

	for _, path := range []string{lifecyclePath, envPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}
	if err := os.WriteFile(lifecyclePath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(lifecyclePath) error = %v", err)
	}
	if err := os.WriteFile(envPath, []byte(strings.Join([]string{
		`REGISTRY_STORAGE_ROOT="/var/lib/regixtry"`,
		`REGISTRY_DATABASE_PATH="/var/lib/regixtry/metadata.db"`,
		`REGISTRY_AUTH_POSTGRES_DSN="postgres://regixtry:secret@127.0.0.1:5432/regixtry_auth?sslmode=disable"`,
		`REGISTRY_PUBLIC_URL="https://registry.example.com/managed/"`,
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(envPath) error = %v", err)
	}

	cfg, err := parseTUIConfigWithBootstrapStatePath([]string{
		"-storage-root", "/tmp/override-root",
		"-auth-postgres-dsn", "postgres://override",
		"-api-base-url", "https://override.example.com/api/",
	}, bootstrapStatePath)
	if err != nil {
		t.Fatalf("parseTUIConfig() error = %v", err)
	}

	if cfg.StorageRoot != "/tmp/override-root" {
		t.Fatalf("StorageRoot = %q, want %q", cfg.StorageRoot, "/tmp/override-root")
	}
	if cfg.DatabasePath != "/tmp/override-root/metadata.db" {
		t.Fatalf("DatabasePath = %q, want storage-root-derived default", cfg.DatabasePath)
	}
	if cfg.AuthPostgresDSN != "postgres://override" {
		t.Fatalf("AuthPostgresDSN = %q, want %q", cfg.AuthPostgresDSN, "postgres://override")
	}
	if cfg.APIBaseURL != "https://override.example.com/api" {
		t.Fatalf("APIBaseURL = %q, want %q", cfg.APIBaseURL, "https://override.example.com/api")
	}

	cfg, err = parseTUIConfigWithBootstrapStatePath([]string{"-db", "/tmp/override.db"}, bootstrapStatePath)
	if err != nil {
		t.Fatalf("parseTUIConfig() with explicit db error = %v", err)
	}
	if cfg.DatabasePath != "/tmp/override.db" {
		t.Fatalf("DatabasePath = %q, want %q", cfg.DatabasePath, "/tmp/override.db")
	}
}

func TestParseTUIConfigEnvAPIBaseURLOverridesSetupManagedRuntime(t *testing.T) {
	t.Setenv("REGISTRY_API_BASE_URL", "https://env.example.com/base/")

	tempDir := t.TempDir()
	bootstrapStatePath := filepath.Join(tempDir, "etc", "regixtry", "bootstrap-state.json")
	lifecyclePath := installlinux.LifecycleProvenancePath(bootstrapStatePath)
	envPath := filepath.Join(filepath.Dir(lifecyclePath), "regixtry.env")

	for _, path := range []string{lifecyclePath, envPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}
	if err := os.WriteFile(lifecyclePath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(lifecyclePath) error = %v", err)
	}
	if err := os.WriteFile(envPath, []byte(strings.Join([]string{
		`REGISTRY_STORAGE_ROOT="/var/lib/regixtry"`,
		`REGISTRY_PUBLIC_URL="https://managed.example.com/admin/"`,
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(envPath) error = %v", err)
	}

	cfg, err := parseTUIConfigWithBootstrapStatePath(nil, bootstrapStatePath)
	if err != nil {
		t.Fatalf("parseTUIConfig() error = %v", err)
	}

	if cfg.APIBaseURL != "https://env.example.com/base" {
		t.Fatalf("APIBaseURL = %q, want %q", cfg.APIBaseURL, "https://env.example.com/base")
	}
}

func TestParseFeatureConfigAutoDetectsManagedRuntime(t *testing.T) {
	t.Setenv("REGISTRY_PUBLIC_URL", "")
	bootstrapStatePath := writeManagedFeatureBootstrapState(t, "/var/lib/regixtry", "/var/lib/regixtry/metadata.db", "https://registry.example.com")
	restore := swapFeatureBootstrapStatePath(t, bootstrapStatePath)
	defer restore()

	cfg, err := parseFeatureConfig(nil)
	if err != nil {
		t.Fatalf("parseFeatureConfig() error = %v", err)
	}
	if cfg.StorageRoot != "/var/lib/regixtry" {
		t.Fatalf("StorageRoot = %q, want %q", cfg.StorageRoot, "/var/lib/regixtry")
	}
	if cfg.DatabasePath != "/var/lib/regixtry/metadata.db" {
		t.Fatalf("DatabasePath = %q, want %q", cfg.DatabasePath, "/var/lib/regixtry/metadata.db")
	}
	if cfg.PublicURL != "https://registry.example.com" {
		t.Fatalf("PublicURL = %q, want %q", cfg.PublicURL, "https://registry.example.com")
	}
}

func TestParseFeatureConfigExplicitFlagsOverrideManagedRuntime(t *testing.T) {
	t.Parallel()

	bootstrapStatePath := writeManagedFeatureBootstrapState(t, "/var/lib/regixtry", "/var/lib/regixtry/metadata.db", "https://registry.example.com")
	restore := swapFeatureBootstrapStatePath(t, bootstrapStatePath)
	defer restore()

	cfg, err := parseFeatureConfig([]string{"-storage-root", "/tmp/override-root", "-public-url", "https://override.example.com"})
	if err != nil {
		t.Fatalf("parseFeatureConfig() error = %v", err)
	}
	if cfg.StorageRoot != "/tmp/override-root" {
		t.Fatalf("StorageRoot = %q, want %q", cfg.StorageRoot, "/tmp/override-root")
	}
	if cfg.DatabasePath != "/tmp/override-root/metadata.db" {
		t.Fatalf("DatabasePath = %q, want storage-root-derived default", cfg.DatabasePath)
	}
	if cfg.PublicURL != "https://override.example.com" {
		t.Fatalf("PublicURL = %q, want %q", cfg.PublicURL, "https://override.example.com")
	}

	cfg, err = parseFeatureConfig([]string{"-db", "/tmp/override.db"})
	if err != nil {
		t.Fatalf("parseFeatureConfig() explicit db error = %v", err)
	}
	if cfg.DatabasePath != "/tmp/override.db" {
		t.Fatalf("DatabasePath = %q, want %q", cfg.DatabasePath, "/tmp/override.db")
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

func TestParseSetupConfigPublicURLPrefersFlagOverEnv(t *testing.T) {
	t.Setenv("REGISTRY_PUBLIC_URL", "https://env.example.com")

	cfg, err := parseSetupConfig([]string{"-public-url", "https://flag.example.com"})
	if err != nil {
		t.Fatalf("parseSetupConfig() error = %v", err)
	}
	if cfg.PublicURL != "https://flag.example.com" {
		t.Fatalf("PublicURL = %q, want explicit flag value", cfg.PublicURL)
	}
}

func TestParseSetupConfigUsesEnvPublicURLWhenFlagMissing(t *testing.T) {
	t.Setenv("REGISTRY_PUBLIC_URL", "https://env.example.com")

	cfg, err := parseSetupConfig(nil)
	if err != nil {
		t.Fatalf("parseSetupConfig() error = %v", err)
	}
	if cfg.PublicURL != "https://env.example.com" {
		t.Fatalf("PublicURL = %q, want environment value", cfg.PublicURL)
	}
}

func TestBuildSetupAuthPostgresDSNUsesDefaultDatabaseName(t *testing.T) {
	t.Parallel()

	dsn, err := buildSetupAuthPostgresDSN("db.example.com", "5432", "registry", "secret", "require")
	if err != nil {
		t.Fatalf("buildSetupAuthPostgresDSN() error = %v", err)
	}
	if dsn != "postgres://registry:secret@db.example.com:5432/regixtry_auth?sslmode=require" {
		t.Fatalf("buildSetupAuthPostgresDSN() = %q, want default auth database name in DSN", dsn)
	}
}

func TestBuildSetupAuthPostgresDSNRequiresHostPortUserAndSSLMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		host    string
		port    string
		user    string
		sslMode string
		wantErr string
	}{
		{name: "missing host", port: "5432", user: "registry", sslMode: "disable", wantErr: "auth Postgres host is required"},
		{name: "missing port", host: "db.example.com", user: "registry", sslMode: "disable", wantErr: "auth Postgres port is required"},
		{name: "missing user", host: "db.example.com", port: "5432", sslMode: "disable", wantErr: "auth Postgres user is required"},
		{name: "missing ssl mode", host: "db.example.com", port: "5432", user: "registry", wantErr: "auth Postgres ssl mode is required"},
	}

	for _, testCase := range tests {
		tt := testCase
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := buildSetupAuthPostgresDSN(tt.host, tt.port, tt.user, "secret", tt.sslMode)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("buildSetupAuthPostgresDSN() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestRunSetupRequiresAdminPasswordWhenAuthEnabledWithoutTTY(t *testing.T) {
	restoreTTY := swapInteractiveTTYDetector(t, false)
	defer restoreTTY()

	err := runWithIO(
		context.Background(),
		[]string{"setup", "-mode", "daemon-sqlite", "-public-url", "https://regixtry.example.com", "-runtime-tls-mode", "reverse-proxy", "-auth-postgres-dsn", "postgres://registry:registry@db.example.com:5432/regixtry_auth?sslmode=disable"},
		strings.NewReader(""),
		io.Discard,
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "admin password is required when auth is enabled") {
		t.Fatalf("runWithIO(setup auth non-interactive) error = %v, want missing admin password guidance", err)
	}
}

func TestRunSetupRequiresModeWithoutTTY(t *testing.T) {
	restore := swapInteractiveTTYDetector(t, false)
	defer restore()

	err := runWithIO(context.Background(), []string{"setup"}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "setup mode is required without a TTY") {
		t.Fatalf("runWithIO(setup) error = %v, want explicit non-TTY mode guidance", err)
	}
}

func TestRunSetupInteractivePromptSupportsBinaryOnly(t *testing.T) {
	restoreExec := swapCurrentExecutablePath(t, "/home/test/.local/bin/regixtry")
	defer restoreExec()
	restoreEUID := swapCurrentEUID(t, 1000)
	defer restoreEUID()

	restoreTTY := swapInteractiveTTYDetector(t, true)
	defer restoreTTY()

	stdout := &bytes.Buffer{}
	err := runWithIO(context.Background(), []string{"setup"}, strings.NewReader("1\n"), stdout, io.Discard)
	if err != nil {
		t.Fatalf("runWithIO(setup prompt) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Select setup mode:") {
		t.Fatalf("stdout = %q, want setup mode prompt", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Binary placement is complete, but setup is not yet complete.") {
		t.Fatalf("stdout = %q, want binary-only guidance", stdout.String())
	}
	if !strings.Contains(stdout.String(), "sudo /home/test/.local/bin/regixtry setup --mode daemon-sqlite --public-url \"<url>\"") {
		t.Fatalf("stdout = %q, want sudo-safe daemon setup guidance", stdout.String())
	}
}

func TestRunSetupInteractivePromptCollectsAddrAndPublicURLForDaemonSQLite(t *testing.T) {
	runner := &stubBootstrapRunner{
		plannedProvenance: installlinux.LifecycleProvenance{
			Version:      1,
			Mode:         "daemon-sqlite",
			InstalledBin: "/usr/local/bin/regixtry",
			ServiceName:  "regixtry",
			StatePath:    "/etc/regixtry/regixtry-lifecycle-state.json",
		},
	}
	restoreRunner := swapBootstrapRunner(t, runner)
	defer restoreRunner()

	restoreTTY := swapInteractiveTTYDetector(t, true)
	defer restoreTTY()

	stdout := &bytes.Buffer{}
	err := runWithIO(context.Background(), []string{"setup"}, strings.NewReader("2\n0.0.0.0:5443\n2\nhttps://regixtry.example.com\n"), stdout, io.Discard)
	if err != nil {
		t.Fatalf("runWithIO(setup prompt) error = %v", err)
	}
	if runner.lastConfig.Addr != "0.0.0.0:5443" {
		t.Fatalf("lastConfig.Addr = %q, want prompted listen address", runner.lastConfig.Addr)
	}
	if runner.lastConfig.PublicURL != "https://regixtry.example.com" {
		t.Fatalf("lastConfig.PublicURL = %q, want prompted public URL", runner.lastConfig.PublicURL)
	}
	if runner.lastConfig.RuntimeTLSMode != installlinux.RuntimeTLSModeReverseProxy {
		t.Fatalf("lastConfig.RuntimeTLSMode = %q, want reverse-proxy", runner.lastConfig.RuntimeTLSMode)
	}
	if !strings.Contains(stdout.String(), "Listen address [127.0.0.1:5000]: ") {
		t.Fatalf("stdout = %q, want listen address prompt with default", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Select runtime TLS mode:") {
		t.Fatalf("stdout = %q, want runtime TLS mode prompt", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Public URL [https://127.0.0.1:5443]: ") {
		t.Fatalf("stdout = %q, want public URL prompt with https default derived from selected mode", stdout.String())
	}
}

func TestRunSetupInteractivePromptDefaultsAddrAndPublicURLForDaemonSQLite(t *testing.T) {
	runner := &stubBootstrapRunner{
		plannedProvenance: installlinux.LifecycleProvenance{
			Version:      1,
			Mode:         "daemon-sqlite",
			InstalledBin: "/usr/local/bin/regixtry",
			ServiceName:  "regixtry",
			StatePath:    "/etc/regixtry/regixtry-lifecycle-state.json",
		},
	}
	restoreRunner := swapBootstrapRunner(t, runner)
	defer restoreRunner()

	restoreTTY := swapInteractiveTTYDetector(t, true)
	defer restoreTTY()

	stdout := &bytes.Buffer{}
	err := runWithIO(context.Background(), []string{"setup"}, strings.NewReader("2\n\n\n\n"), stdout, io.Discard)
	if err != nil {
		t.Fatalf("runWithIO(setup prompt) error = %v", err)
	}
	if runner.lastConfig.Addr != "127.0.0.1:5000" {
		t.Fatalf("lastConfig.Addr = %q, want default listen address %q", runner.lastConfig.Addr, "127.0.0.1:5000")
	}
	if runner.lastConfig.PublicURL != defaultSetupPublicURL {
		t.Fatalf("lastConfig.PublicURL = %q, want default public URL %q", runner.lastConfig.PublicURL, defaultSetupPublicURL)
	}
	if !strings.Contains(stdout.String(), "Listen address [127.0.0.1:5000]: ") {
		t.Fatalf("stdout = %q, want default listen address prompt", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Select runtime TLS mode:") {
		t.Fatalf("stdout = %q, want runtime TLS mode prompt", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Public URL ["+defaultSetupPublicURL+"]: ") {
		t.Fatalf("stdout = %q, want default public URL prompt", stdout.String())
	}
}

func TestRunSetupInteractivePromptCollectsCertAndKeyOnlyForDirectTLS(t *testing.T) {
	runner := &stubBootstrapRunner{
		plannedProvenance: installlinux.LifecycleProvenance{
			Version:      1,
			Mode:         "daemon-sqlite",
			InstalledBin: "/usr/local/bin/regixtry",
			ServiceName:  "regixtry",
			StatePath:    "/etc/regixtry/regixtry-lifecycle-state.json",
		},
	}
	restoreRunner := swapBootstrapRunner(t, runner)
	defer restoreRunner()

	restoreTTY := swapInteractiveTTYDetector(t, true)
	defer restoreTTY()

	stdout := &bytes.Buffer{}
	err := runWithIO(context.Background(), []string{"setup"}, strings.NewReader("2\n0.0.0.0:5443\n3\nhttps://regixtry.example.com\n/tmp/registry.crt\n/tmp/registry.key\n"), stdout, io.Discard)
	if err != nil {
		t.Fatalf("runWithIO(setup prompt) error = %v", err)
	}
	if runner.lastConfig.RuntimeTLSMode != installlinux.RuntimeTLSModeDirectTLS {
		t.Fatalf("lastConfig.RuntimeTLSMode = %q, want direct-tls", runner.lastConfig.RuntimeTLSMode)
	}
	if runner.lastConfig.TLSCertFile != "/tmp/registry.crt" {
		t.Fatalf("lastConfig.TLSCertFile = %q, want prompted cert path", runner.lastConfig.TLSCertFile)
	}
	if runner.lastConfig.TLSKeyFile != "/tmp/registry.key" {
		t.Fatalf("lastConfig.TLSKeyFile = %q, want prompted key path", runner.lastConfig.TLSKeyFile)
	}
	if !strings.Contains(stdout.String(), "TLS cert file:") {
		t.Fatalf("stdout = %q, want cert prompt", stdout.String())
	}
	if !strings.Contains(stdout.String(), "TLS key file:") {
		t.Fatalf("stdout = %q, want key prompt", stdout.String())
	}
}

func TestRunSetupInteractivePromptValidatesPromptedPublicURL(t *testing.T) {
	restoreTTY := swapInteractiveTTYDetector(t, true)
	defer restoreTTY()

	err := runWithIO(context.Background(), []string{"setup"}, strings.NewReader("2\n0.0.0.0:5443\n2\nnot-a-url\n"), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "public URL must be an absolute http(s) URL") {
		t.Fatalf("runWithIO(setup prompt) error = %v, want prompted public URL validation failure", err)
	}
}

func TestRunSetupInteractivePromptKeepsExplicitFlagValues(t *testing.T) {
	runner := &stubBootstrapRunner{
		plannedProvenance: installlinux.LifecycleProvenance{
			Version:      1,
			Mode:         "daemon-sqlite",
			InstalledBin: "/usr/local/bin/regixtry",
			ServiceName:  "regixtry",
			StatePath:    "/etc/regixtry/regixtry-lifecycle-state.json",
		},
	}
	restoreRunner := swapBootstrapRunner(t, runner)
	defer restoreRunner()

	restoreTTY := swapInteractiveTTYDetector(t, true)
	defer restoreTTY()

	stdout := &bytes.Buffer{}
	err := runWithIO(
		context.Background(),
		[]string{"setup", "-mode", "daemon-sqlite", "-addr", "0.0.0.0:5443", "-public-url", "https://flag.example.com", "-runtime-tls-mode", "reverse-proxy"},
		strings.NewReader(""),
		stdout,
		io.Discard,
	)
	if err != nil {
		t.Fatalf("runWithIO(setup explicit flags) error = %v", err)
	}
	if runner.lastConfig.Addr != "0.0.0.0:5443" {
		t.Fatalf("lastConfig.Addr = %q, want explicit flag value", runner.lastConfig.Addr)
	}
	if runner.lastConfig.PublicURL != "https://flag.example.com" {
		t.Fatalf("lastConfig.PublicURL = %q, want explicit flag value", runner.lastConfig.PublicURL)
	}
	if strings.Contains(stdout.String(), "Listen address [") || strings.Contains(stdout.String(), "Public URL [") || strings.Contains(stdout.String(), "Select runtime TLS mode:") {
		t.Fatalf("stdout = %q, want explicit values to skip interactive prompts", stdout.String())
	}
	if runner.runCalls != 1 {
		t.Fatalf("runCalls = %d, want 1", runner.runCalls)
	}
}

func TestRunSetupInteractivePromptAppliesPromptedAddrBeforePortConflict(t *testing.T) {
	runner := &stubBootstrapRunner{
		runHook: func(cfg installlinux.BootstrapConfig) error {
			if cfg.Addr != "0.0.0.0:5443" {
				t.Fatalf("cfg.Addr = %q, want prompted address", cfg.Addr)
			}
			if cfg.PublicURL != "https://regixtry.example.com" {
				t.Fatalf("cfg.PublicURL = %q, want prompted public URL", cfg.PublicURL)
			}
			return errors.New("configured local bind address 0.0.0.0:5443 is already in use\nRecover with:\n  sudo ss -ltnp 'sport = :5443'\n  sudo systemctl stop regixtry.service\n  regixtry bootstrap --mode daemon-sqlite --addr 0.0.0.0:5444 --public-url https://regixtry.example.com --storage-root /var/lib/regixtry --state-path /etc/regixtry/bootstrap-state.json --unit-path /etc/systemd/system/regixtry.service --service regixtry")
		},
	}
	restoreRunner := swapBootstrapRunner(t, runner)
	defer restoreRunner()

	restoreTTY := swapInteractiveTTYDetector(t, true)
	defer restoreTTY()

	err := runWithIO(context.Background(), []string{"setup"}, strings.NewReader("2\n0.0.0.0:5443\n2\nhttps://regixtry.example.com\n"), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--addr 0.0.0.0:5444 --public-url https://regixtry.example.com") {
		t.Fatalf("runWithIO(setup prompt) error = %v, want prompted endpoints in port-conflict guidance", err)
	}
}

func TestRunSetupDaemonSQLiteWithoutPublicURLFailsWithoutTTY(t *testing.T) {
	restoreTTY := swapInteractiveTTYDetector(t, false)
	defer restoreTTY()

	err := runWithIO(context.Background(), []string{"setup", "-mode", "daemon-sqlite"}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "public URL is required") {
		t.Fatalf("runWithIO(setup daemon-sqlite) error = %v, want explicit public URL requirement", err)
	}
}

func TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance(t *testing.T) {
	runner := &stubBootstrapRunner{
		plannedProvenance: installlinux.LifecycleProvenance{
			Version:      1,
			Mode:         "daemon-sqlite",
			InstalledBin: "/usr/local/bin/regixtry",
			ServiceName:  "registry-custom",
			StatePath:    "/etc/regixtry/regixtry-lifecycle-state.json",
			ManagedPaths: []string{"/etc/regixtry/bootstrap-state.json"},
		},
	}
	restore := swapBootstrapRunner(t, runner)
	defer restore()

	stdout := &bytes.Buffer{}
	args := []string{
		"setup",
		"-mode", "daemon-sqlite",
		"-public-url", "https://regixtry.example.com",
		"-runtime-tls-mode", "reverse-proxy",
		"-addr", "0.0.0.0:5443",
		"-storage-root", "/var/lib/regixtry-data",
		"-state-path", "/etc/regixtry/bootstrap-state.json",
		"-unit-path", "/etc/systemd/system/registry-custom.service",
		"-service", "registry-custom",
	}

	if err := runWithIO(context.Background(), args, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(setup) error = %v", err)
	}
	if runner.runCalls != 1 {
		t.Fatalf("runCalls = %d, want 1", runner.runCalls)
	}
	if runner.saveProvenanceCalls != 1 {
		t.Fatalf("saveProvenanceCalls = %d, want 1", runner.saveProvenanceCalls)
	}
	if runner.lastConfig.Mode != "daemon-sqlite" {
		t.Fatalf("lastConfig.Mode = %q, want daemon-sqlite", runner.lastConfig.Mode)
	}
	if runner.lastConfig.RuntimeTLSMode != installlinux.RuntimeTLSModeReverseProxy {
		t.Fatalf("lastConfig.RuntimeTLSMode = %q, want reverse-proxy", runner.lastConfig.RuntimeTLSMode)
	}
	if runner.savedProvenance.StatePath != "/etc/regixtry/regixtry-lifecycle-state.json" {
		t.Fatalf("saved provenance path = %q, want lifecycle provenance path", runner.savedProvenance.StatePath)
	}
	if !strings.Contains(stdout.String(), "Setup complete:") {
		t.Fatalf("stdout = %q, want setup success message", stdout.String())
	}
}

func TestRunSetupImportsLegacyTrivyFlagsIntoFeatureState(t *testing.T) {
	root := t.TempDir()
	storageRoot := filepath.Join(root, "var", "lib", "regixtry")
	provenancePath := filepath.Join(root, "etc", "regixtry", "regixtry-lifecycle-state.json")
	if err := os.MkdirAll(filepath.Dir(provenancePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	runner := &stubBootstrapRunner{
		plannedProvenance: installlinux.LifecycleProvenance{
			Version:      2,
			Mode:         "daemon-sqlite",
			InstalledBin: "/usr/local/bin/regixtry",
			ServiceName:  "registry-custom",
			StatePath:    provenancePath,
			ManagedPaths: []string{filepath.Join(root, "etc", "regixtry", "bootstrap-state.json")},
		},
		saveProvenanceHook: func(provenance installlinux.LifecycleProvenance) error {
			return installlinux.NewBootstrapper().SaveLifecycleProvenance(provenance)
		},
	}
	restore := swapBootstrapRunner(t, runner)
	defer restore()

	stdout := &bytes.Buffer{}
	args := []string{
		"setup",
		"-mode", "daemon-sqlite",
		"-public-url", "https://regixtry.example.com",
		"-runtime-tls-mode", "reverse-proxy",
		"-addr", "0.0.0.0:5443",
		"-storage-root", storageRoot,
		"-state-path", filepath.Join(root, "etc", "regixtry", "bootstrap-state.json"),
		"-unit-path", filepath.Join(root, "etc", "systemd", "system", "registry-custom.service"),
		"-service", "registry-custom",
		"-trivy-enabled",
		"-trivy-schedule-enabled",
		"-trivy-interval", "3h",
		"-trivy-timeout", "17m",
		"-trivy-cache-dir", filepath.Join(root, "var", "cache", "trivy-custom"),
		"-trivy-binary-path", "/usr/local/bin/trivy-custom",
		"-trivy-max-concurrency", "4",
	}

	if err := runWithIO(context.Background(), args, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(setup) error = %v", err)
	}

	store, err := metadata.New(filepath.Join(storageRoot, "metadata.db"))
	if err != nil {
		t.Fatalf("metadata.New() error = %v", err)
	}
	defer store.Close()
	settings, err := store.GetScanSettings(context.Background(), ports.DefaultTenant)
	if err != nil {
		t.Fatalf("GetScanSettings() error = %v", err)
	}
	if !settings.Enabled || !settings.ScheduleEnabled || settings.Interval != 3*time.Hour || settings.Timeout != 17*time.Minute || settings.ServiceURL != "" || settings.RegistryReachableURL != "" || settings.MaxConcurrency != 4 {
		t.Fatalf("settings = %#v, want legacy setup knobs imported without binary-backed runtime", settings)
	}
	if !strings.Contains(stdout.String(), "Legacy Trivy setup flags were imported into feature state.") {
		t.Fatalf("stdout = %q, want legacy import guidance", stdout.String())
	}
	if !strings.Contains(stdout.String(), "service_url and registry_reachable_url are still required") {
		t.Fatalf("stdout = %q, want service runtime follow-up guidance", stdout.String())
	}
	if runner.savedProvenance.Intent.TrivyBinaryPath != "" || runner.savedProvenance.Intent.TrivyCacheDir != "" || runner.savedProvenance.Intent.TrivyInterval != "" {
		t.Fatalf("saved provenance intent = %#v, want base-only lifecycle provenance", runner.savedProvenance.Intent)
	}
}

func TestRunFeatureCommandsManageBuiltInTrivyState(t *testing.T) {
	root := t.TempDir()
	storageRoot := filepath.Join(root, "data")
	databasePath := filepath.Join(storageRoot, "metadata.db")
	if err := os.MkdirAll(storageRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	store, err := metadata.New(databasePath)
	if err != nil {
		t.Fatalf("metadata.New() error = %v", err)
	}
	defer store.Close()

	stdout := &bytes.Buffer{}
	if err := runWithIO(context.Background(), []string{"feature", "list", "-storage-root", storageRoot, "-db", databasePath}, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(feature list) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "trivy") {
		t.Fatalf("stdout = %q, want trivy feature inventory", stdout.String())
	}

	stdout.Reset()
	if err := runWithIO(context.Background(), []string{"feature", "configure", "trivy", "-storage-root", storageRoot, "-db", databasePath, "-enabled", "-schedule-enabled", "-interval", "6h", "-timeout", "10m", "-registry-reachable-url", "https://registry.internal:5443", "-max-concurrency", "2"}, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(feature configure) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Configured feature trivy") {
		t.Fatalf("stdout = %q, want configure confirmation", stdout.String())
	}

	stdout.Reset()
	if err := runWithIO(context.Background(), []string{"feature", "status", "trivy", "-storage-root", storageRoot, "-db", databasePath}, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(feature status) error = %v", err)
	}
	for _, want := range []string{"Name: trivy", "Enabled: true", "Schedule Enabled: true", "Registry Reachable URL: https://registry.internal:5443", "Runtime Status: uninstalled", "Runtime Health: uninstalled"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}

	stdout.Reset()
	if err := runWithIO(context.Background(), []string{"feature", "configure", "trivy", "-storage-root", storageRoot, "-db", databasePath, "-enabled", "-schedule-enabled", "-interval", "6h", "-timeout", "10m", "-max-concurrency", "2"}, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(feature configure fallback) error = %v", err)
	}

	stdout.Reset()
	if err := runWithIO(context.Background(), []string{"feature", "status", "trivy", "-storage-root", storageRoot, "-db", databasePath, "-public-url", "https://registry.example.com"}, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(feature status with public-url fallback) error = %v", err)
	}
	for _, want := range []string{"Runtime Status: uninstalled", "Runtime Health: uninstalled"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q after public-url fallback", stdout.String(), want)
		}
	}

	stdout.Reset()
	if err := runWithIO(context.Background(), []string{"feature", "disable", "trivy", "-storage-root", storageRoot, "-db", databasePath}, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(feature disable) error = %v", err)
	}
	settings, err := store.GetScanSettings(context.Background(), ports.DefaultTenant)
	if err != nil {
		t.Fatalf("GetScanSettings() error = %v", err)
	}
	if settings.Enabled {
		t.Fatalf("settings.Enabled = %v, want false after feature disable", settings.Enabled)
	}
}

func TestFeatureRuntimeLifecycleCommandsUseManagedRuntimeActions(t *testing.T) {
	root := t.TempDir()
	storageRoot := filepath.Join(root, "data")
	databasePath := filepath.Join(storageRoot, "metadata.db")
	if err := os.MkdirAll(storageRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	stub := &stubFeatureRuntimeManager{
		installState:  ports.TrivyRuntimeState{Status: ports.TrivyRuntimeStatusReady, ActiveVersion: "0.57.1", PreviousVersion: "", ActiveBinaryPath: filepath.Join(storageRoot, "features", "trivy", "bin", "active", "trivy"), CacheDir: filepath.Join(storageRoot, "features", "trivy", "trivy-cache"), ReceiptPath: filepath.Join(storageRoot, "features", "trivy", "receipts", "0.57.1.json")},
		upgradeState:  ports.TrivyRuntimeState{Status: ports.TrivyRuntimeStatusReady, ActiveVersion: "0.58.0", PreviousVersion: "0.57.1", ActiveBinaryPath: filepath.Join(storageRoot, "features", "trivy", "bin", "active", "trivy"), CacheDir: filepath.Join(storageRoot, "features", "trivy", "trivy-cache"), ReceiptPath: filepath.Join(storageRoot, "features", "trivy", "receipts", "0.58.0.json")},
		rollbackState: ports.TrivyRuntimeState{Status: ports.TrivyRuntimeStatusReady, ActiveVersion: "0.57.1", PreviousVersion: "0.58.0", ActiveBinaryPath: filepath.Join(storageRoot, "features", "trivy", "bin", "active", "trivy"), CacheDir: filepath.Join(storageRoot, "features", "trivy", "trivy-cache"), ReceiptPath: filepath.Join(storageRoot, "features", "trivy", "receipts", "0.57.1.json")},
		statusState:   ports.TrivyRuntimeState{Status: ports.TrivyRuntimeStatusMigrationRequired, MigrationHint: `legacy binary_path "/tmp/README.sh" requires managed reinstall and will never be executed`},
	}
	restoreRuntimeManager := swapFeatureRuntimeManagerFactory(t, func(appregixtry.FeatureRuntimeManagerConfig) appregixtry.FeatureRuntimeManager {
		return stub
	})
	defer restoreRuntimeManager()

	stdout := &bytes.Buffer{}
	if err := runWithIO(context.Background(), []string{"feature", "install", "trivy", "-storage-root", storageRoot, "-db", databasePath, "-version", "0.57.1"}, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(feature install) error = %v", err)
	}
	if stub.installCalls != 1 || !strings.Contains(stdout.String(), "Installed managed runtime for trivy at 0.57.1") {
		t.Fatalf("install stub/stdout = %#v / %q, want managed install evidence", stub, stdout.String())
	}

	stdout.Reset()
	if err := runWithIO(context.Background(), []string{"feature", "upgrade", "trivy", "-storage-root", storageRoot, "-db", databasePath, "-version", "0.58.0"}, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(feature upgrade) error = %v", err)
	}
	if stub.upgradeCalls != 1 || !strings.Contains(stdout.String(), "Upgraded managed runtime for trivy to 0.58.0") {
		t.Fatalf("upgrade stub/stdout = %#v / %q, want managed upgrade evidence", stub, stdout.String())
	}

	stdout.Reset()
	if err := runWithIO(context.Background(), []string{"feature", "rollback", "trivy", "-storage-root", storageRoot, "-db", databasePath}, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(feature rollback) error = %v", err)
	}
	if stub.rollbackCalls != 1 || !strings.Contains(stdout.String(), "Rolled back managed runtime for trivy to 0.57.1") {
		t.Fatalf("rollback stub/stdout = %#v / %q, want managed rollback evidence", stub, stdout.String())
	}

	stdout.Reset()
	if err := runWithIO(context.Background(), []string{"feature", "status", "trivy", "-storage-root", storageRoot, "-db", databasePath}, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(feature status) error = %v", err)
	}
	for _, want := range []string{"Runtime Status: migration-required", "Runtime Detail: legacy binary_path \"/tmp/README.sh\" requires managed reinstall and will never be executed"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestFeatureRuntimeLifecycleCommandsAutoDetectManagedRuntimePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	storageRoot := filepath.Join(root, "managed")
	databasePath := filepath.Join(storageRoot, "metadata.db")
	if err := os.MkdirAll(storageRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	store, err := metadata.New(databasePath)
	if err != nil {
		t.Fatalf("metadata.New() error = %v", err)
	}
	defer store.Close()
	stub := &stubFeatureRuntimeManager{
		statusState:   ports.TrivyRuntimeState{Status: ports.TrivyRuntimeStatusMigrationRequired, MigrationHint: `legacy binary_path "/tmp/README.sh" requires managed reinstall and will never be executed`},
		installState:  ports.TrivyRuntimeState{Status: ports.TrivyRuntimeStatusReady, ActiveVersion: "0.57.1"},
		upgradeState:  ports.TrivyRuntimeState{Status: ports.TrivyRuntimeStatusReady, ActiveVersion: "0.58.0"},
		rollbackState: ports.TrivyRuntimeState{Status: ports.TrivyRuntimeStatusReady, ActiveVersion: "0.57.1"},
	}
	restoreRuntimeManager := swapFeatureRuntimeManagerFactory(t, func(appregixtry.FeatureRuntimeManagerConfig) appregixtry.FeatureRuntimeManager {
		return stub
	})
	defer restoreRuntimeManager()
	bootstrapStatePath := writeManagedFeatureBootstrapState(t, storageRoot, databasePath, "https://registry.example.com")
	restoreBootstrapPath := swapFeatureBootstrapStatePath(t, bootstrapStatePath)
	defer restoreBootstrapPath()

	stdout := &bytes.Buffer{}
	if err := runWithIO(context.Background(), []string{"feature", "status", "trivy"}, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(feature status) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Runtime Status: migration-required") {
		t.Fatalf("stdout = %q, want managed runtime status using autodetected paths", stdout.String())
	}

	stdout.Reset()
	if err := runWithIO(context.Background(), []string{"feature", "install", "trivy", "-version", "0.57.1"}, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(feature install) error = %v", err)
	}
	if stub.installCalls != 1 {
		t.Fatalf("installCalls = %d, want 1", stub.installCalls)
	}

	stdout.Reset()
	if err := runWithIO(context.Background(), []string{"feature", "upgrade", "trivy", "-version", "0.58.0"}, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(feature upgrade) error = %v", err)
	}
	if stub.upgradeCalls != 1 {
		t.Fatalf("upgradeCalls = %d, want 1", stub.upgradeCalls)
	}

	stdout.Reset()
	if err := runWithIO(context.Background(), []string{"feature", "rollback", "trivy"}, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(feature rollback) error = %v", err)
	}
	if stub.rollbackCalls != 1 {
		t.Fatalf("rollbackCalls = %d, want 1", stub.rollbackCalls)
	}
}

func TestRunFeatureRejectsUnknownBuiltInName(t *testing.T) {
	t.Chdir(t.TempDir())

	stdout := &bytes.Buffer{}
	err := runWithIO(context.Background(), []string{"feature", "show", "future-plugin"}, strings.NewReader(""), stdout, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unsupported feature") {
		t.Fatalf("runWithIO(feature show unknown) error = %v, want unsupported feature rejection", err)
	}
	if _, statErr := os.Stat(filepath.Join("data", "metadata.db")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("data/metadata.db stat error = %v, want not exists", statErr)
	}
}

func TestRunSetupPersistsCustomTrivyFlagsInLifecycleProvenance(t *testing.T) {
	root := t.TempDir()
	storageRoot := filepath.Join(root, "var", "lib", "regixtry-data")
	provenancePath := filepath.Join(root, "etc", "regixtry", "regixtry-lifecycle-state.json")
	if err := os.MkdirAll(filepath.Dir(provenancePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	runner := &stubBootstrapRunner{
		planProvenanceHook: func(cfg installlinux.BootstrapConfig) (installlinux.LifecycleProvenance, error) {
			return installlinux.LifecycleProvenance{
				Version:      2,
				Mode:         cfg.Mode,
				InstalledBin: "/usr/local/bin/regixtry",
				ServiceName:  cfg.ServiceName,
				StatePath:    provenancePath,
				ManagedPaths: []string{cfg.StatePath, cfg.UnitPath, filepath.Join(cfg.StorageRoot, "metadata.db"), filepath.Join(cfg.StorageRoot, "content")},
				Intent:       installlinux.LifecycleIntent{Addr: cfg.Addr, PublicURL: cfg.PublicURL, RuntimeTLSMode: cfg.RuntimeTLSMode, StorageRoot: cfg.StorageRoot, BootstrapStatePath: cfg.StatePath, UnitPath: cfg.UnitPath, ServiceName: cfg.ServiceName},
			}, nil
		},
		saveProvenanceHook: func(provenance installlinux.LifecycleProvenance) error {
			return installlinux.NewBootstrapper().SaveLifecycleProvenance(provenance)
		},
	}
	restore := swapBootstrapRunner(t, runner)
	defer restore()

	stdout := &bytes.Buffer{}
	args := []string{
		"setup",
		"-mode", "daemon-sqlite",
		"-public-url", "https://regixtry.example.com",
		"-runtime-tls-mode", "reverse-proxy",
		"-addr", "0.0.0.0:5443",
		"-storage-root", storageRoot,
		"-state-path", filepath.Join(root, "etc", "regixtry", "bootstrap-state.json"),
		"-unit-path", filepath.Join(root, "etc", "systemd", "system", "registry-custom.service"),
		"-service", "registry-custom",
		"-trivy-enabled",
		"-trivy-schedule-enabled",
		"-trivy-interval", "3h",
		"-trivy-timeout", "17m",
		"-trivy-cache-dir", filepath.Join(root, "var", "cache", "trivy-custom"),
		"-trivy-binary-path", "/usr/local/bin/trivy-custom",
		"-trivy-max-concurrency", "4",
	}

	if err := runWithIO(context.Background(), args, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(setup) error = %v", err)
	}
	if !runner.lastConfig.TrivyEnabled || !runner.lastConfig.TrivyScheduleEnabled {
		t.Fatalf("lastConfig = %#v, want custom trivy enablement passed through setup", runner.lastConfig)
	}
	if runner.lastConfig.TrivyInterval != 3*time.Hour || runner.lastConfig.TrivyTimeout != 17*time.Minute {
		t.Fatalf("lastConfig = %#v, want explicit trivy durations passed through setup", runner.lastConfig)
	}
	if runner.lastConfig.TrivyCacheDir != filepath.Join(root, "var", "cache", "trivy-custom") || runner.lastConfig.TrivyBinaryPath != "/usr/local/bin/trivy-custom" || runner.lastConfig.TrivyMaxConcurrency != 4 {
		t.Fatalf("lastConfig = %#v, want explicit trivy cache/binary/concurrency passed through setup", runner.lastConfig)
	}

	body, err := os.ReadFile(provenancePath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", provenancePath, err)
	}
	var persisted installlinux.LifecycleProvenance
	if err := json.Unmarshal(body, &persisted); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if persisted.Intent.TrivyEnabled || persisted.Intent.TrivyScheduleEnabled || persisted.Intent.TrivyInterval != "" || persisted.Intent.TrivyTimeout != "" || persisted.Intent.TrivyCacheDir != "" || persisted.Intent.TrivyBinaryPath != "" || persisted.Intent.TrivyMaxConcurrency != 0 {
		t.Fatalf("persisted intent = %#v, want base-only lifecycle provenance without trivy fields", persisted.Intent)
	}
	if !strings.Contains(stdout.String(), "Lifecycle provenance recorded at "+provenancePath) {
		t.Fatalf("stdout = %q, want lifecycle provenance path", stdout.String())
	}
}

func TestRunSetupInteractivePromptBootstrapsAuthAndPrintsDockerLoginGuidance(t *testing.T) {
	restoreAuth := swapAuthStoreOpener(t)
	defer restoreAuth()

	runner := &stubBootstrapRunner{
		plannedProvenance: installlinux.LifecycleProvenance{
			Version:      1,
			Mode:         "daemon-sqlite",
			InstalledBin: "/usr/local/bin/regixtry",
			ServiceName:  "regixtry",
			StatePath:    "/etc/regixtry/regixtry-lifecycle-state.json",
		},
	}
	restoreRunner := swapBootstrapRunner(t, runner)
	defer restoreRunner()

	restoreTTY := swapInteractiveTTYDetector(t, true)
	defer restoreTTY()

	authDBURL, err := url.Parse("postgres://bootstrap:change-me@127.0.0.1:5432/" + defaultSetupAuthDBName + "?sslmode=disable")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	stdout := &bytes.Buffer{}
	err = runWithIO(
		context.Background(),
		[]string{"setup"},
		strings.NewReader("2\n\n\n\ny\ndb.example.com\n5432\nregistry\nregistry-secret\ndisable\nbootstrap-admin\nchange-me-now\n"),
		stdout,
		io.Discard,
	)
	if err != nil {
		t.Fatalf("runWithIO(setup auth prompt) error = %v", err)
	}
	if runner.lastConfig.AuthPostgresDSN != "postgres://registry:registry-secret@db.example.com:5432/regixtry_auth?sslmode=disable" {
		t.Fatalf("lastConfig.AuthPostgresDSN = %q, want assembled auth DSN", runner.lastConfig.AuthPostgresDSN)
	}
	if !strings.Contains(stdout.String(), `Auth bootstrap complete: created global admin "bootstrap-admin".`) {
		t.Fatalf("stdout = %q, want auth bootstrap success message", stdout.String())
	}
	if !strings.Contains(stdout.String(), "docker login 127.0.0.1:5000 -u bootstrap-admin --password-stdin") {
		t.Fatalf("stdout = %q, want docker login guidance", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Enable auth [y/N]: ") {
		t.Fatalf("stdout = %q, want enable auth prompt", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Auth Postgres host [127.0.0.1]: ") || !strings.Contains(stdout.String(), "Auth Postgres port [5432]: ") || !strings.Contains(stdout.String(), "Auth Postgres user [regixtry]: ") || !strings.Contains(stdout.String(), "Auth Postgres password: ") || !strings.Contains(stdout.String(), "Auth Postgres ssl mode [disable]: ") {
		t.Fatalf("stdout = %q, want structured auth prompts", stdout.String())
	}

	handler, cleanup, err := newHandler(serveConfig{
		StorageRoot:     t.TempDir(),
		DatabasePath:    filepath.Join(t.TempDir(), "registry.db"),
		AuthPostgresDSN: authDBURL.String(),
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
}

func TestRunSetupSkipsBootstrapFailureWhenGlobalAdminAlreadyExists(t *testing.T) {
	restoreAuth := swapAuthStoreOpener(t)
	defer restoreAuth()

	authDB := filepath.Join(t.TempDir(), "auth.db")
	if err := run(context.Background(), []string{"bootstrap-admin", "-auth-postgres-dsn", authDB, "-username", "existing-admin", "-password", "change-me-now"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("run(bootstrap-admin) error = %v", err)
	}

	runner := &stubBootstrapRunner{
		plannedProvenance: installlinux.LifecycleProvenance{
			Version:      1,
			Mode:         "daemon-sqlite",
			InstalledBin: "/usr/local/bin/regixtry",
			ServiceName:  "regixtry",
			StatePath:    "/etc/regixtry/regixtry-lifecycle-state.json",
		},
	}
	restoreRunner := swapBootstrapRunner(t, runner)
	defer restoreRunner()

	stdout := &bytes.Buffer{}
	err := runWithIO(
		context.Background(),
		[]string{
			"setup",
			"-mode", "daemon-sqlite",
			"-public-url", "https://regixtry.example.com",
			"-runtime-tls-mode", "reverse-proxy",
			"-auth-postgres-dsn", authDB,
			"-admin-username", "different-admin",
			"-admin-password", "change-me-now",
		},
		strings.NewReader(""),
		stdout,
		io.Discard,
	)
	if err != nil {
		t.Fatalf("runWithIO(setup existing admin) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Auth bootstrap skipped: a global admin already exists in the auth store.") {
		t.Fatalf("stdout = %q, want existing-admin guidance", stdout.String())
	}
	if runner.runCalls != 1 {
		t.Fatalf("runCalls = %d, want 1", runner.runCalls)
	}
}

func TestRunSetupRollsBackWhenProvenanceSaveFails(t *testing.T) {
	runner := &stubBootstrapRunner{
		plannedProvenance: installlinux.LifecycleProvenance{
			Version:      1,
			Mode:         "daemon-sqlite",
			InstalledBin: "/usr/local/bin/regixtry",
			ServiceName:  "regixtry",
			StatePath:    "/etc/regixtry/regixtry-lifecycle-state.json",
		},
		saveProvenanceErr: errors.New("disk full"),
	}
	restore := swapBootstrapRunner(t, runner)
	defer restore()

	err := runWithIO(context.Background(), []string{"setup", "-mode", "daemon-sqlite", "-public-url", "http://127.0.0.1:5000"}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "write lifecycle provenance") {
		t.Fatalf("runWithIO(setup) error = %v, want provenance write failure", err)
	}
	if runner.rollbackCalls != 1 {
		t.Fatalf("rollbackCalls = %d, want 1", runner.rollbackCalls)
	}
}

func TestRunSetupPermissionDeniedReturnsSudoSafeRerunGuidance(t *testing.T) {
	runner := &stubBootstrapRunner{runErr: fmt.Errorf("mkdir /etc/regixtry: %w", os.ErrPermission)}
	restoreRunner := swapBootstrapRunner(t, runner)
	defer restoreRunner()
	restoreExec := swapCurrentExecutablePath(t, "/home/test/.local/bin/regixtry")
	defer restoreExec()
	restoreEUID := swapCurrentEUID(t, 1000)
	defer restoreEUID()

	err := runWithIO(context.Background(), []string{"setup", "-mode", "daemon-sqlite", "-public-url", "https://regixtry.example.com"}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("runWithIO(setup) error = nil, want permission guidance")
	}
	if !strings.Contains(err.Error(), "permission-denied failure") {
		t.Fatalf("runWithIO(setup) error = %v, want permission guidance", err)
	}
	if !strings.Contains(err.Error(), "sudo /home/test/.local/bin/regixtry setup --mode daemon-sqlite --public-url https://regixtry.example.com --runtime-tls-mode reverse-proxy --addr 127.0.0.1:5000 --storage-root /var/lib/regixtry --state-path /etc/regixtry/bootstrap-state.json --unit-path /etc/systemd/system/regixtry.service --service regixtry") {
		t.Fatalf("runWithIO(setup) error = %v, want sudo-safe rerun command", err)
	}
	if runner.rollbackCalls != 0 {
		t.Fatalf("rollbackCalls = %d, want 0", runner.rollbackCalls)
	}
}

func TestRunUninstallUsesProvenanceStatePathAndPrintsReport(t *testing.T) {
	runner := &stubBootstrapRunner{
		uninstallReport: installlinux.UninstallReport{
			Mode:        "daemon-sqlite",
			ServiceName: "regixtry",
			Service:     installlinux.CleanupItem{Path: "regixtry.service", Status: installlinux.CleanupStatusRemoved, Detail: "systemd service disabled and stopped"},
			Items:       []installlinux.CleanupItem{{Path: "/usr/local/bin/regixtry", Status: installlinux.CleanupStatusRemoved, Detail: "path removed"}},
		},
	}
	restore := swapBootstrapRunner(t, runner)
	defer restore()

	stdout := &bytes.Buffer{}
	if err := runWithIO(context.Background(), []string{"uninstall", "-state-path", "/tmp/regixtry-lifecycle-state.json"}, strings.NewReader(""), stdout, io.Discard); err != nil {
		t.Fatalf("runWithIO(uninstall) error = %v", err)
	}
	if runner.uninstallCalls != 1 {
		t.Fatalf("uninstallCalls = %d, want 1", runner.uninstallCalls)
	}
	if runner.uninstallPath != "/tmp/regixtry-lifecycle-state.json" {
		t.Fatalf("uninstallPath = %q, want provenance state path", runner.uninstallPath)
	}
	if !strings.Contains(stdout.String(), "Uninstall report:") {
		t.Fatalf("stdout = %q, want uninstall report", stdout.String())
	}
}

func TestRunUninstallPermissionDeniedReturnsSudoSafeRerunGuidance(t *testing.T) {
	runner := &stubBootstrapRunner{uninstallErr: fmt.Errorf("remove /etc/regixtry/regixtry-lifecycle-state.json: %w", os.ErrPermission)}
	restoreRunner := swapBootstrapRunner(t, runner)
	defer restoreRunner()
	restoreExec := swapCurrentExecutablePath(t, "/home/test/.local/bin/regixtry")
	defer restoreExec()
	restoreEUID := swapCurrentEUID(t, 1000)
	defer restoreEUID()

	err := runWithIO(context.Background(), []string{"uninstall", "-state-path", "/etc/regixtry/regixtry-lifecycle-state.json"}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("runWithIO(uninstall) error = nil, want permission guidance")
	}
	if !strings.Contains(err.Error(), "permission-denied failure") {
		t.Fatalf("runWithIO(uninstall) error = %v, want permission guidance", err)
	}
	if !strings.Contains(err.Error(), "sudo /home/test/.local/bin/regixtry uninstall --state-path /etc/regixtry/regixtry-lifecycle-state.json") {
		t.Fatalf("runWithIO(uninstall) error = %v, want sudo-safe rerun command", err)
	}
}

func TestRunUpgradePassesConfigToGoOwnedLifecycleRunner(t *testing.T) {
	runner := &stubBootstrapRunner{upgradeResult: installlinux.UpgradeResult{FromRef: "v1.2.2", FromVersion: "1.2.2", TargetRef: "v1.2.3", ToVersion: "1.2.3", ProvenancePath: "/etc/regixtry/regixtry-lifecycle-state.json"}}
	restore := swapBootstrapRunner(t, runner)
	defer restore()

	stdout := &bytes.Buffer{}
	err := runWithIO(context.Background(), []string{"upgrade", "--ref", "v1.2.3", "--yes", "--state-path", "/tmp/lifecycle.json"}, strings.NewReader(""), stdout, io.Discard)
	if err != nil {
		t.Fatalf("runWithIO(upgrade) error = %v", err)
	}
	if runner.upgradeCalls != 1 {
		t.Fatalf("upgradeCalls = %d, want 1", runner.upgradeCalls)
	}
	if runner.upgradeConfig.Ref != "v1.2.3" || runner.upgradeConfig.ProvenancePath != "/tmp/lifecycle.json" || !runner.upgradeConfig.AssumeYes {
		t.Fatalf("upgradeConfig = %#v, want parsed upgrade config", runner.upgradeConfig)
	}
	for _, want := range []string{
		"Installed version: v1.2.2 (1.2.2)",
		"Available version: v1.2.3 (1.2.3)",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if !strings.Contains(stdout.String(), "Upgrade complete: v1.2.2 (1.2.2) -> v1.2.3 (1.2.3)") {
		t.Fatalf("stdout = %q, want upgrade success message with from/to", stdout.String())
	}
}

func TestRunUpgradeRendersProgressStagesWithoutTTY(t *testing.T) {
	runner := &stubBootstrapRunner{
		upgradeResult: installlinux.UpgradeResult{FromRef: "v1.2.2", FromVersion: "1.2.2", TargetRef: "v1.2.3", ToVersion: "1.2.3", ProvenancePath: "/etc/regixtry/regixtry-lifecycle-state.json"},
		upgradeHook: func(cfg installlinux.UpgradeConfig) {
			for _, event := range []installlinux.UpgradeProgress{
				{Stage: "resolve", Detail: "Upgrading from v1.2.2 (1.2.2) to v1.2.3 (1.2.3)"},
				{Stage: "download", Detail: "Downloading regixtry_1.2.3_linux_amd64.tar.gz"},
				{Stage: "verify", Detail: "Verifying regixtry_1.2.3_linux_amd64.tar.gz"},
				{Stage: "stop", Detail: "Stopping regixtry.service"},
				{Stage: "swap", Detail: "Swapping installed binary at /usr/local/bin/regixtry"},
				{Stage: "restart", Detail: "Restarting regixtry.service"},
				{Stage: "health-check", Detail: "Waiting for regixtry health check"},
			} {
				if cfg.Progress != nil {
					cfg.Progress(event)
				}
			}
		},
	}
	restore := swapBootstrapRunner(t, runner)
	defer restore()
	restoreTTY := swapInteractiveTTYDetector(t, false)
	defer restoreTTY()

	stdout := &bytes.Buffer{}
	err := runWithIO(context.Background(), []string{"upgrade"}, strings.NewReader(""), stdout, io.Discard)
	if err != nil {
		t.Fatalf("runWithIO(upgrade) error = %v", err)
	}
	for _, want := range []string{
		"[1/7] Resolve: Upgrading from v1.2.2 (1.2.2) to v1.2.3 (1.2.3)",
		"[2/7] Download:",
		"[3/7] Verify:",
		"[4/7] Stop:",
		"[5/7] Swap:",
		"[6/7] Restart:",
		"[7/7] Health check:",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunUpgradeRejectsUnmanagedTargetTruthfully(t *testing.T) {
	runner := &stubBootstrapRunner{upgradeErr: errors.New("lifecycle provenance is missing service_name")}
	restore := swapBootstrapRunner(t, runner)
	defer restore()

	err := runWithIO(context.Background(), []string{"upgrade"}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "lifecycle provenance is missing service_name") {
		t.Fatalf("runWithIO(upgrade) error = %v, want unmanaged upgrade failure", err)
	}
}

func TestRunUpgradeAlreadyUpToDateSkipsPromptAndCompletionOutput(t *testing.T) {
	runner := &stubBootstrapRunner{upgradeResult: installlinux.UpgradeResult{FromRef: "v1.2.3", FromVersion: "1.2.3", TargetRef: "v1.2.3", ToVersion: "1.2.3", ProvenancePath: "/etc/regixtry/regixtry-lifecycle-state.json", UpToDate: true}}
	restoreRunner := swapBootstrapRunner(t, runner)
	defer restoreRunner()
	restoreTTY := swapInteractiveTTYDetector(t, false)
	defer restoreTTY()

	stdout := &bytes.Buffer{}
	err := runWithIO(context.Background(), []string{"upgrade"}, strings.NewReader(""), stdout, io.Discard)
	if err != nil {
		t.Fatalf("runWithIO(upgrade) error = %v", err)
	}
	for _, unwanted := range []string{"Proceed with upgrade", "Upgrade complete:"} {
		if strings.Contains(stdout.String(), unwanted) {
			t.Fatalf("stdout = %q, want no %q output", stdout.String(), unwanted)
		}
	}
	for _, unwanted := range []string{"[#", "[1/7]", "Resolve:"} {
		if strings.Contains(stdout.String(), unwanted) {
			t.Fatalf("stdout = %q, want no progress output containing %q", stdout.String(), unwanted)
		}
	}
	for _, want := range []string{
		"Installed version: v1.2.3 (1.2.3)",
		"Available version: v1.2.3 (1.2.3)",
		"regixtry is already up to date.",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if runner.upgradeCalls != 1 {
		t.Fatalf("upgradeCalls = %d, want 1", runner.upgradeCalls)
	}
	if runner.upgradeConfig.AssumeYes {
		t.Fatalf("upgradeConfig.AssumeYes = true, want false")
	}
	if runner.upgradeConfig.Preflight == nil {
		t.Fatal("upgradeConfig.Preflight = nil, want preflight callback")
	}
	if runner.upgradeConfig.Progress == nil {
		t.Fatal("upgradeConfig.Progress = nil, want progress callback")
	}
}

func TestRunUpgradeRendersProgressOnlyAfterConfirmation(t *testing.T) {
	runner := &stubBootstrapRunner{
		upgradeResult: installlinux.UpgradeResult{FromRef: "v1.2.2", FromVersion: "1.2.2", TargetRef: "v1.2.3", ToVersion: "1.2.3", ProvenancePath: "/etc/regixtry/regixtry-lifecycle-state.json"},
		upgradeHook: func(cfg installlinux.UpgradeConfig) {
			cfg.Progress(installlinux.UpgradeProgress{Stage: "resolve", Detail: "Resolving target release"})
		},
	}
	restoreRunner := swapBootstrapRunner(t, runner)
	defer restoreRunner()
	restoreTTY := swapInteractiveTTYDetector(t, true)
	defer restoreTTY()

	stdout := &bytes.Buffer{}
	err := runWithIO(context.Background(), []string{"upgrade"}, strings.NewReader("y\n"), stdout, io.Discard)
	if err != nil {
		t.Fatalf("runWithIO(upgrade) error = %v", err)
	}
	out := stdout.String()
	installedIndex := strings.Index(out, "Installed version: v1.2.2 (1.2.2)")
	availableIndex := strings.Index(out, "Available version: v1.2.3 (1.2.3)")
	promptIndex := strings.Index(out, "Proceed with upgrade [y/N]: ")
	progressIndex := strings.Index(out, "[#------] 1/7 Resolve: Resolving target release")
	if installedIndex == -1 || availableIndex == -1 || promptIndex == -1 || progressIndex == -1 {
		t.Fatalf("stdout = %q, want preflight, prompt, and progress output", out)
	}
	if !(installedIndex < availableIndex && availableIndex < promptIndex && promptIndex < progressIndex) {
		t.Fatalf("stdout = %q, want preflight and prompt before progress", out)
	}
}

func TestRunUpgradePromptsForConfirmationWhenInteractive(t *testing.T) {
	runner := &stubBootstrapRunner{upgradeResult: installlinux.UpgradeResult{FromRef: "v1.2.2", FromVersion: "1.2.2", TargetRef: "v1.2.3", ToVersion: "1.2.3", ProvenancePath: "/etc/regixtry/regixtry-lifecycle-state.json"}}
	restoreRunner := swapBootstrapRunner(t, runner)
	defer restoreRunner()
	restoreTTY := swapInteractiveTTYDetector(t, true)
	defer restoreTTY()

	stdout := &bytes.Buffer{}
	err := runWithIO(context.Background(), []string{"upgrade"}, strings.NewReader("y\n"), stdout, io.Discard)
	if err != nil {
		t.Fatalf("runWithIO(upgrade) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Proceed with upgrade [y/N]:") {
		t.Fatalf("stdout = %q, want confirmation prompt", stdout.String())
	}
}

func TestRunUpgradeDeclinesConfirmationInteractively(t *testing.T) {
	runner := &stubBootstrapRunner{
		upgradeResult: installlinux.UpgradeResult{FromRef: "v1.2.2", FromVersion: "1.2.2", TargetRef: "v1.2.3", ToVersion: "1.2.3", ProvenancePath: "/etc/regixtry/regixtry-lifecycle-state.json"},
		upgradeHook: func(installlinux.UpgradeConfig) {
			t.Fatal("upgradeHook should not run after declined confirmation")
		},
	}
	restoreRunner := swapBootstrapRunner(t, runner)
	defer restoreRunner()
	restoreTTY := swapInteractiveTTYDetector(t, true)
	defer restoreTTY()

	stdout := &bytes.Buffer{}
	err := runWithIO(context.Background(), []string{"upgrade"}, strings.NewReader("n\n"), stdout, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "upgrade cancelled") {
		t.Fatalf("runWithIO(upgrade) error = %v, want cancellation", err)
	}
	if !strings.Contains(stdout.String(), "Proceed with upgrade [y/N]:") {
		t.Fatalf("stdout = %q, want confirmation prompt", stdout.String())
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
		Mode:           "daemon-sqlite",
		PublicURL:      "https://regixtry.example.com",
		RuntimeTLSMode: installlinux.RuntimeTLSModeReverseProxy,
		Addr:           "0.0.0.0:5443",
		StorageRoot:    "/var/lib/regixtry-data",
		StatePath:      "/etc/regixtry/bootstrap-state.json",
		UnitPath:       "/etc/systemd/system/registry-custom.service",
		ServiceName:    "registry-custom",
		NoStart:        true,
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
	if !strings.Contains(view, "Enter: open tags | Tab: admin | q: quit") {
		t.Fatalf("stdout = %q, want unified help footer", view)
	}
}

func TestTUIRequiresStartupLoginOnlyWhenAuthAndAPIAreConfigured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  tuiConfig
		want bool
	}{
		{name: "local only", cfg: tuiConfig{}, want: false},
		{name: "auth only", cfg: tuiConfig{AuthPostgresDSN: "postgres://auth"}, want: false},
		{name: "api only", cfg: tuiConfig{APIBaseURL: "https://registry.example.com/admin"}, want: false},
		{name: "both configured", cfg: tuiConfig{AuthPostgresDSN: "postgres://auth", APIBaseURL: "https://registry.example.com/admin"}, want: true},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tuiRequiresStartupLogin(tc.cfg); got != tc.want {
				t.Fatalf("tuiRequiresStartupLogin(%+v) = %v, want %v", tc.cfg, got, tc.want)
			}
		})
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

func TestRunTUISnapshotRequiresLoginWhenAuthAndAPIAreConfigured(t *testing.T) {
	restore := swapAuthStoreOpener(t)
	defer restore()

	authDB := filepath.Join(t.TempDir(), "auth.db")
	if err := run(context.Background(), []string{"bootstrap-admin", "-auth-postgres-dsn", authDB, "-username", "admin", "-password", "change-me-now"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("run(bootstrap-admin) error = %v", err)
	}

	storageRoot := t.TempDir()
	databasePath := filepath.Join(storageRoot, "registry.db")
	seedRegixtryState(t, storageRoot, databasePath)
	apiServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("snapshot login screen should not hit the admin API before login")
	}))
	defer apiServer.Close()

	stdout := &bytes.Buffer{}
	err := runTUI(tuiConfig{StorageRoot: storageRoot, DatabasePath: databasePath, Tenant: "tenant-a", AuthPostgresDSN: authDB, APIBaseURL: apiServer.URL, Snapshot: true}, strings.NewReader("q"), stdout)
	if err != nil {
		t.Fatalf("runTUI() error = %v", err)
	}

	view := stdout.String()
	if !strings.Contains(view, "Operator Login") || !strings.Contains(view, "Enter: sign in | Tab: switch field | Esc: back | q: quit") {
		t.Fatalf("stdout = %q, want login-first snapshot", view)
	}
	if strings.Contains(view, "library/alpine") {
		t.Fatalf("stdout = %q, want catalog hidden until login", view)
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
	sqliteRoot := t.TempDir()
	openAuthStore = func(dsn string) (ports.AuthStore, error) {
		if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
			parsed, err := url.Parse(dsn)
			if err != nil {
				return nil, err
			}
			dbName := strings.TrimPrefix(strings.TrimSpace(parsed.Path), "/")
			if dbName == "" {
				dbName = defaultSetupAuthDBName
			}
			dsn = filepath.Join(sqliteRoot, dbName+".db")
		}
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

func swapFeatureRuntimeManagerFactory(t *testing.T, factory func(appregixtry.FeatureRuntimeManagerConfig) appregixtry.FeatureRuntimeManager) func() {
	t.Helper()

	previous := newFeatureRuntimeManager
	newFeatureRuntimeManager = factory

	return func() {
		newFeatureRuntimeManager = previous
	}
}

func swapFeatureBootstrapStatePath(t *testing.T, path string) func() {
	t.Helper()

	previous := featureBootstrapStatePath
	featureBootstrapStatePath = path

	return func() {
		featureBootstrapStatePath = previous
	}
}

func swapCurrentExecutablePath(t *testing.T, path string) func() {
	t.Helper()

	previous := resolveCurrentExecutable
	resolveCurrentExecutable = func() string {
		return path
	}

	return func() {
		resolveCurrentExecutable = previous
	}
}

func swapCurrentEUID(t *testing.T, euid int) func() {
	t.Helper()

	previous := currentEUID
	currentEUID = func() int {
		return euid
	}

	return func() {
		currentEUID = previous
	}
}

func swapInteractiveTTYDetector(t *testing.T, value bool) func() {
	t.Helper()

	previous := isInteractiveTTYPair
	isInteractiveTTYPair = func(io.Reader, io.Writer) bool {
		return value
	}

	return func() {
		isInteractiveTTYPair = previous
	}
}

func writeManagedFeatureBootstrapState(t *testing.T, storageRoot string, databasePath string, publicURL string) string {
	t.Helper()

	root := t.TempDir()
	bootstrapStatePath := filepath.Join(root, "etc", "regixtry", "bootstrap-state.json")
	lifecyclePath := installlinux.LifecycleProvenancePath(bootstrapStatePath)
	envPath := filepath.Join(filepath.Dir(lifecyclePath), "regixtry.env")
	for _, path := range []string{lifecyclePath, envPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}
	if err := os.WriteFile(lifecyclePath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(lifecyclePath) error = %v", err)
	}
	if err := os.WriteFile(envPath, []byte(strings.Join([]string{
		fmt.Sprintf(`REGISTRY_STORAGE_ROOT=%q`, storageRoot),
		fmt.Sprintf(`REGISTRY_DATABASE_PATH=%q`, databasePath),
		fmt.Sprintf(`REGISTRY_PUBLIC_URL=%q`, publicURL),
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(envPath) error = %v", err)
	}
	return bootstrapStatePath
}

type stubBootstrapRunner struct {
	runErr              error
	runHook             func(installlinux.BootstrapConfig) error
	rollbackErr         error
	uninstallErr        error
	upgradeErr          error
	upgradeHook         func(installlinux.UpgradeConfig)
	planProvenanceHook  func(installlinux.BootstrapConfig) (installlinux.LifecycleProvenance, error)
	planProvenanceErr   error
	saveProvenanceErr   error
	saveProvenanceHook  func(installlinux.LifecycleProvenance) error
	lastConfig          installlinux.BootstrapConfig
	upgradeConfig       installlinux.UpgradeConfig
	plannedProvenance   installlinux.LifecycleProvenance
	savedProvenance     installlinux.LifecycleProvenance
	uninstallReport     installlinux.UninstallReport
	upgradeResult       installlinux.UpgradeResult
	uninstallPath       string
	runCalls            int
	rollbackCalls       int
	uninstallCalls      int
	upgradeCalls        int
	saveProvenanceCalls int
}

type stubFeatureRuntimeManager struct {
	installState  ports.TrivyRuntimeState
	upgradeState  ports.TrivyRuntimeState
	rollbackState ports.TrivyRuntimeState
	statusState   ports.TrivyRuntimeState
	err           error
	installCalls  int
	upgradeCalls  int
	rollbackCalls int
	statusCalls   int
}

func (s *stubFeatureRuntimeManager) Install(context.Context, string) (ports.TrivyRuntimeState, error) {
	s.installCalls++
	return s.installState, s.err
}

func (s *stubFeatureRuntimeManager) Upgrade(context.Context, string) (ports.TrivyRuntimeState, error) {
	s.upgradeCalls++
	return s.upgradeState, s.err
}

func (s *stubFeatureRuntimeManager) Rollback(context.Context) (ports.TrivyRuntimeState, error) {
	s.rollbackCalls++
	return s.rollbackState, s.err
}

func (s *stubFeatureRuntimeManager) Status(context.Context) (ports.TrivyRuntimeState, error) {
	s.statusCalls++
	return s.statusState, s.err
}

func (s *stubBootstrapRunner) Run(_ context.Context, cfg installlinux.BootstrapConfig) error {
	s.lastConfig = cfg
	s.runCalls++
	if s.runHook != nil {
		return s.runHook(cfg)
	}
	return s.runErr
}

func (s *stubBootstrapRunner) Rollback(_ context.Context, cfg installlinux.BootstrapConfig) error {
	s.lastConfig = cfg
	s.rollbackCalls++
	return s.rollbackErr
}

func (s *stubBootstrapRunner) Uninstall(_ context.Context, provenancePath string) (installlinux.UninstallReport, error) {
	s.uninstallPath = provenancePath
	s.uninstallCalls++
	return s.uninstallReport, s.uninstallErr
}

func (s *stubBootstrapRunner) Upgrade(_ context.Context, cfg installlinux.UpgradeConfig) (installlinux.UpgradeResult, error) {
	s.upgradeConfig = cfg
	s.upgradeCalls++
	if cfg.Preflight != nil {
		if err := cfg.Preflight(installlinux.UpgradePreflight{
			InstalledRef:     s.upgradeResult.FromRef,
			InstalledVersion: s.upgradeResult.FromVersion,
			TargetRef:        s.upgradeResult.TargetRef,
			TargetVersion:    s.upgradeResult.ToVersion,
			UpToDate:         s.upgradeResult.UpToDate,
		}); err != nil {
			return installlinux.UpgradeResult{}, err
		}
	}
	if cfg.Confirm != nil {
		if err := cfg.Confirm(installlinux.UpgradePreflight{
			InstalledRef:     s.upgradeResult.FromRef,
			InstalledVersion: s.upgradeResult.FromVersion,
			TargetRef:        s.upgradeResult.TargetRef,
			TargetVersion:    s.upgradeResult.ToVersion,
			UpToDate:         s.upgradeResult.UpToDate,
		}); err != nil {
			return installlinux.UpgradeResult{}, err
		}
	}
	if s.upgradeHook != nil {
		s.upgradeHook(cfg)
	}
	return s.upgradeResult, s.upgradeErr
}

func (s *stubBootstrapRunner) PlanLifecycleProvenance(cfg installlinux.BootstrapConfig) (installlinux.LifecycleProvenance, error) {
	s.lastConfig = cfg
	if s.planProvenanceHook != nil {
		return s.planProvenanceHook(cfg)
	}
	if s.planProvenanceErr != nil {
		return installlinux.LifecycleProvenance{}, s.planProvenanceErr
	}
	return s.plannedProvenance, nil
}

func (s *stubBootstrapRunner) SaveLifecycleProvenance(provenance installlinux.LifecycleProvenance) error {
	s.savedProvenance = provenance
	s.saveProvenanceCalls++
	if s.saveProvenanceHook != nil {
		return s.saveProvenanceHook(provenance)
	}
	return s.saveProvenanceErr
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
