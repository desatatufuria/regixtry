package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	appauth "regixtry/internal/app/auth"
	appregixtry "regixtry/internal/app/regixtry"
	appscanning "regixtry/internal/app/scanning"
	domainauth "regixtry/internal/domain/auth"
	authpostgres "regixtry/internal/infra/auth/postgres"
	"regixtry/internal/infra/cliprogress"
	installlinux "regixtry/internal/infra/install/linux"
	metadata "regixtry/internal/infra/metadata/sqlite"
	gitleaksinfra "regixtry/internal/infra/scanning/gitleaks"
	trivyinfra "regixtry/internal/infra/scanning/trivy"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
	regixtryhttp "regixtry/internal/protocol/http"
	"regixtry/internal/tui"
)

var openAuthStore = func(dsn string) (ports.AuthStore, error) {
	return authpostgres.New(dsn)
}

type bootstrapRunner interface {
	Run(context.Context, installlinux.BootstrapConfig) error
	Rollback(context.Context, installlinux.BootstrapConfig) error
	Uninstall(context.Context, string) (installlinux.UninstallReport, error)
	Upgrade(context.Context, installlinux.UpgradeConfig) (installlinux.UpgradeResult, error)
	PlanLifecycleProvenance(installlinux.BootstrapConfig) (installlinux.LifecycleProvenance, error)
	SaveLifecycleProvenance(installlinux.LifecycleProvenance) error
}

var newBootstrapRunner = func() bootstrapRunner {
	return installlinux.NewBootstrapper()
}

// newFeatureRuntimeManager builds the managed runtime manager for one
// feature identity. It is a single swappable var (rather than one var per
// feature) so tests that stub it via swapFeatureRuntimeManagerFactory cover
// every registered feature, not just Trivy.
var newFeatureRuntimeManager = func(feature string, cfg appregixtry.FeatureRuntimeManagerConfig) appregixtry.FeatureRuntimeManager {
	switch feature {
	case "gitleaks":
		return gitleaksinfra.NewRuntimeManager(gitleaksinfra.RuntimeManagerConfig{
			StorageRoot: cfg.StorageRoot,
			Store:       cfg.Store,
			Prober:      gitleaksinfra.New(gitleaksinfra.RunnerConfig{}),
		})
	default:
		return trivyinfra.NewRuntimeManager(trivyinfra.RuntimeManagerConfig{
			StorageRoot: cfg.StorageRoot,
			Store:       cfg.Store,
			Prober:      trivyinfra.New(trivyinfra.RunnerConfig{}),
		})
	}
}

var featureBootstrapStatePath = "/etc/regixtry/bootstrap-state.json"

var resolveCurrentExecutable = func() string {
	path, err := os.Executable()
	if err == nil {
		return path
	}
	if len(os.Args) > 0 {
		return os.Args[0]
	}
	return "regixtry"
}

var currentEUID = func() int {
	return os.Geteuid()
}

var isInteractiveTTYPair = func(stdin io.Reader, stdout io.Writer) bool {
	stdinFile, stdinOK := stdin.(*os.File)
	stdoutFile, stdoutOK := stdout.(*os.File)
	if !stdinOK || !stdoutOK {
		return false
	}

	stdinInfo, err := stdinFile.Stat()
	if err != nil || stdinInfo.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	stdoutInfo, err := stdoutFile.Stat()
	if err != nil || stdoutInfo.Mode()&os.ModeCharDevice == 0 {
		return false
	}

	return true
}

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildDate    = "unknown"
)

const (
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 30 * time.Second
	defaultWriteTimeout      = 30 * time.Second
	defaultIdleTimeout       = 120 * time.Second
	defaultShutdownTimeout   = 10 * time.Second
	defaultSetupPublicURL    = "http://127.0.0.1:5000"
	defaultSetupAuthDBName   = "regixtry_auth"
	defaultSetupAuthHost     = "127.0.0.1"
	defaultSetupAuthPort     = "5432"
	defaultSetupAuthUser     = "regixtry"
	defaultSetupAuthSSLMode  = "disable"
)

func releaseMetadata() string {
	return fmt.Sprintf(
		"version=%s commit=%s date=%s",
		defaultBuildValue(buildVersion, "dev"),
		defaultBuildValue(buildCommit, "unknown"),
		defaultBuildValue(buildDate, "unknown"),
	)
}

func defaultBuildValue(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}

	return value
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	return runWithIO(ctx, args, os.Stdin, stdout, stderr)
}

func runWithIO(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
		_, err := fmt.Fprintln(stdout, releaseMetadata())
		return err
	}

	if len(args) == 0 {
		return errors.New("expected subcommand: serve, tui, bootstrap, bootstrap-admin, setup, feature, uninstall, or upgrade")
	}

	switch args[0] {
	case "serve":
		cfg, err := parseServeConfig(args[1:])
		if err != nil {
			return err
		}
		runtimeCfg, err := normalizeRuntimeConfig(cfg)
		if err != nil {
			return err
		}
		cfg.AuthTokenRealmURL = runtimeCfg.tokenRealmURL.String()

		listener, err := net.Listen("tcp", cfg.Address)
		if err != nil {
			return err
		}
		defer listener.Close()

		return serve(ctx, listener, cfg, stdout)
	case "tui":
		cfg, err := parseTUIConfig(args[1:])
		if err != nil {
			return err
		}
		return runTUI(cfg, os.Stdin, stdout)
	case "bootstrap-admin":
		cfg, err := parseBootstrapAdminConfig(args[1:], os.Stdin)
		if err != nil {
			return err
		}
		if cfg.PasswordWarning != "" && stderr != nil {
			_, _ = fmt.Fprintln(stderr, cfg.PasswordWarning)
		}
		return runBootstrapAdmin(ctx, cfg, stdout)
	case "bootstrap":
		cfg, err := parseBootstrapConfig(args[1:])
		if err != nil {
			return err
		}

		runner := newBootstrapRunner()
		if cfg.Rollback {
			return runner.Rollback(ctx, cfg)
		}

		return runner.Run(ctx, cfg)
	case "setup":
		return runSetup(ctx, args[1:], stdin, stdout)
	case "feature":
		return runFeature(ctx, args[1:], stdout)
	case "uninstall":
		return runUninstall(ctx, args[1:], stdout)
	case "upgrade":
		return runUpgrade(ctx, args[1:], stdin, stdout)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

type tuiConfig struct {
	StorageRoot     string
	DatabasePath    string
	Tenant          string
	AuthPostgresDSN string
	APIBaseURL      string
	Snapshot        bool
	// DeleteEnabled gates DeleteManifest for the Tags screen's delete-tag
	// confirm flow, mirroring serveConfig.DeleteEnabled. Without this, the
	// tui subcommand's Service always had deletion disabled with no way to
	// override it, regardless of REGISTRY_DELETE_ENABLED.
	DeleteEnabled bool
	// GCDeleteEnabled mirrors serveConfig.GCDeleteEnabled -- kept distinct
	// from DeleteEnabled for the same reason as serve (design.md decision).
	GCDeleteEnabled bool
}

type serveConfig struct {
	Address              string
	PublicURL            string
	TLSCertFile          string
	TLSKeyFile           string
	StorageRoot          string
	DatabasePath         string
	Tenant               string
	AllowAnonymousPull   bool
	AllowAnonymousPush   bool
	AuthPostgresDSN      string
	AuthTokenRealmURL    string
	Realm                string
	ServiceName          string
	ReadHeaderTimeout    time.Duration
	ReadTimeout          time.Duration
	WriteTimeout         time.Duration
	IdleTimeout          time.Duration
	ShutdownTimeout      time.Duration
	TrivyEnabled         bool
	TrivyScheduleEnabled bool
	TrivyInterval        time.Duration
	TrivyTimeout         time.Duration
	TrivyCacheDir        string
	TrivyBinaryPath      string
	TrivyMaxConcurrency  int
	DeleteEnabled        bool
	// GCDeleteEnabled gates POST /admin/v1/gc/reports/{id}/delete
	// (design.md Decision F / proposal.md D1a). It is DELIBERATELY a
	// separate flag from DeleteEnabled and must never read
	// REGISTRY_DELETE_ENABLED: that flag is metadata-only and never touches
	// blob files.
	GCDeleteEnabled bool
}

type bootstrapAdminConfig struct {
	AuthPostgresDSN string
	Username        string
	Password        string
	PasswordSource  string
	PasswordWarning string
	RotatePassword  bool
}

type BootstrapConfig = installlinux.BootstrapConfig

type setupConfig struct {
	BootstrapConfig
	Auth setupAuthConfig
}

type setupAuthConfig struct {
	Enabled         bool
	AuthPostgresDSN string
	Host            string
	Port            string
	User            string
	Password        string
	SSLMode         string
	AdminUsername   string
	AdminPassword   string
}

type setupPromptState struct {
	addrProvided            bool
	publicURLProvided       bool
	runtimeTLSModeProvided  bool
	tlsCertFileProvided     bool
	tlsKeyFileProvided      bool
	authPostgresDSNProvided bool
	adminUsernameProvided   bool
	adminPasswordProvided   bool
	legacyTrivyProvided     bool
}

type featureConfig struct {
	StorageRoot  string
	DatabasePath string
	PublicURL    string
	Tenant       string
}

type uninstallConfig struct {
	StatePath string
}

type upgradeConfig struct {
	Ref            string
	StatePath      string
	AssumeYes      bool
	CurrentVersion string
}

type runtimeConfig struct {
	publicURL         *url.URL
	tokenRealmURL     *url.URL
	tlsEnabled        bool
	readHeaderTimeout time.Duration
	readTimeout       time.Duration
	writeTimeout      time.Duration
	idleTimeout       time.Duration
	shutdownTimeout   time.Duration
}

func parseServeConfig(args []string) (serveConfig, error) {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var cfg serveConfig
	flags.StringVar(&cfg.Address, "addr", "127.0.0.1:5000", "address to listen on")
	flags.StringVar(&cfg.PublicURL, "public-url", os.Getenv("REGISTRY_PUBLIC_URL"), "canonical public URL advertised to registry clients")
	flags.StringVar(&cfg.TLSCertFile, "tls-cert-file", os.Getenv("REGISTRY_TLS_CERT_FILE"), "path to the TLS certificate PEM file")
	flags.StringVar(&cfg.TLSKeyFile, "tls-key-file", os.Getenv("REGISTRY_TLS_KEY_FILE"), "path to the TLS private key PEM file")
	flags.StringVar(&cfg.StorageRoot, "storage-root", filepath.Join(".", "data"), "root directory for registry storage")
	flags.StringVar(&cfg.DatabasePath, "db", "", "path to the SQLite metadata database")
	flags.StringVar(&cfg.Tenant, "tenant", ports.DefaultTenant, "tenant identifier")
	flags.BoolVar(&cfg.AllowAnonymousPull, "allow-anonymous-pull", false, "allow unauthenticated manifest/blob reads")
	flags.BoolVar(&cfg.AllowAnonymousPush, "allow-anonymous-push", false, "allow unauthenticated blob/manifest writes")
	flags.StringVar(&cfg.AuthPostgresDSN, "auth-postgres-dsn", os.Getenv("REGISTRY_AUTH_POSTGRES_DSN"), "Postgres DSN for auth state")
	flags.StringVar(&cfg.AuthTokenRealmURL, "auth-token-realm", os.Getenv("REGISTRY_AUTH_TOKEN_REALM_URL"), "Bearer token realm URL advertised to registry clients")
	flags.StringVar(&cfg.Realm, "realm", "regixtry", "auth challenge realm")
	flags.StringVar(&cfg.ServiceName, "service", "regixtry", "auth challenge service name")
	flags.DurationVar(&cfg.ReadHeaderTimeout, "read-header-timeout", defaultReadHeaderTimeout, "maximum time to read request headers")
	flags.DurationVar(&cfg.ReadTimeout, "read-timeout", defaultReadTimeout, "maximum time to read the full request")
	flags.DurationVar(&cfg.WriteTimeout, "write-timeout", defaultWriteTimeout, "maximum time to write a response")
	flags.DurationVar(&cfg.IdleTimeout, "idle-timeout", defaultIdleTimeout, "maximum idle keep-alive wait time")
	flags.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", defaultShutdownTimeout, "maximum graceful shutdown wait time")
	flags.BoolVar(&cfg.TrivyEnabled, "trivy-enabled", false, "enable persisted trivy rescans")
	flags.BoolVar(&cfg.TrivyScheduleEnabled, "trivy-schedule-enabled", false, "enable periodic trivy rescans")
	flags.DurationVar(&cfg.TrivyInterval, "trivy-interval", 0, "interval between periodic trivy rescans")
	flags.DurationVar(&cfg.TrivyTimeout, "trivy-timeout", 0, "timeout for each trivy run")
	flags.StringVar(&cfg.TrivyCacheDir, "trivy-cache-dir", "", "shared trivy cache directory")
	flags.StringVar(&cfg.TrivyBinaryPath, "trivy-binary-path", "", "trivy executable path")
	flags.IntVar(&cfg.TrivyMaxConcurrency, "trivy-max-concurrency", 0, "maximum concurrent trivy runs")
	flags.BoolVar(&cfg.DeleteEnabled, "delete-enabled", parseBoolEnv("REGISTRY_DELETE_ENABLED", false), "enable DELETE /v2/<name>/manifests/<reference> (manifest and tag deletion)")
	flags.BoolVar(&cfg.GCDeleteEnabled, "gc-delete-enabled", parseBoolEnv("REGISTRY_GC_DELETE_ENABLED", false), "enable POST /admin/v1/gc/reports/{id}/delete (irreversibly unlinks unreferenced blob files; distinct from -delete-enabled, which is metadata-only)")

	if err := flags.Parse(args); err != nil {
		return serveConfig{}, err
	}

	if cfg.DatabasePath == "" {
		cfg.DatabasePath = filepath.Join(cfg.StorageRoot, "metadata.db")
	}
	if strings.TrimSpace(cfg.TrivyCacheDir) == "" {
		cfg.TrivyCacheDir = filepath.Join(cfg.StorageRoot, "trivy-cache")
	}

	return cfg, nil
}

func normalizeRuntimeConfig(cfg serveConfig) (runtimeConfig, error) {
	publicURL, err := normalizeAbsoluteHTTPURL(cfg.PublicURL, "public URL")
	if err != nil {
		return runtimeConfig{}, err
	}

	tlsCertFile := strings.TrimSpace(cfg.TLSCertFile)
	tlsKeyFile := strings.TrimSpace(cfg.TLSKeyFile)
	hasTLSCert := tlsCertFile != ""
	hasTLSKey := tlsKeyFile != ""
	if hasTLSCert != hasTLSKey {
		return runtimeConfig{}, errors.New("TLS cert file and key file must both be set")
	}

	tlsEnabled := hasTLSCert && hasTLSKey
	switch publicURL.Scheme {
	case "https":
		// HTTPS public URLs can be served directly by regixtry or terminated by a reverse proxy.
	case "http":
		if tlsEnabled {
			return runtimeConfig{}, errors.New("http public URL cannot be combined with TLS cert/key inputs")
		}
	default:
		return runtimeConfig{}, errors.New("public URL must use http or https")
	}

	tokenRealmURL := deriveTokenRealmURL(publicURL)
	if strings.TrimSpace(cfg.AuthTokenRealmURL) != "" {
		configuredRealmURL, err := normalizeAbsoluteHTTPURL(cfg.AuthTokenRealmURL, "auth token realm URL")
		if err != nil {
			return runtimeConfig{}, err
		}
		if configuredRealmURL.String() != tokenRealmURL.String() {
			return runtimeConfig{}, fmt.Errorf("auth token realm URL must match derived public token realm %q", tokenRealmURL.String())
		}
	}

	return runtimeConfig{
		publicURL:         publicURL,
		tokenRealmURL:     tokenRealmURL,
		tlsEnabled:        tlsEnabled,
		readHeaderTimeout: cfg.ReadHeaderTimeout,
		readTimeout:       cfg.ReadTimeout,
		writeTimeout:      cfg.WriteTimeout,
		idleTimeout:       cfg.IdleTimeout,
		shutdownTimeout:   cfg.ShutdownTimeout,
	}, nil
}

func normalizeAbsoluteHTTPURL(raw string, fieldName string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("%s is required", fieldName)
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", fieldName, err)
	}
	if !parsed.IsAbs() || strings.TrimSpace(parsed.Host) == "" {
		return nil, fmt.Errorf("%s must be an absolute http(s) URL", fieldName)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%s must use http or https", fieldName)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("%s must not include query or fragment components", fieldName)
	}

	parsed.Path = normalizeURLPath(parsed.Path)
	parsed.RawPath = ""
	return parsed, nil
}

func deriveTokenRealmURL(publicURL *url.URL) *url.URL {
	realmURL := *publicURL
	realmURL.Path = tokenRealmPath(publicURL.Path)
	realmURL.RawPath = ""
	return &realmURL
}

func tokenRealmPath(basePath string) string {
	normalizedPath := normalizeURLPath(basePath)
	if normalizedPath == "" {
		return "/auth/token"
	}
	return normalizedPath + "/auth/token"
}

func normalizeURLPath(rawPath string) string {
	if strings.TrimSpace(rawPath) == "" || rawPath == "/" {
		return ""
	}
	normalized := path.Clean(rawPath)
	if normalized == "." || normalized == "/" {
		return ""
	}
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	return strings.TrimRight(normalized, "/")
}

func parseTUIConfig(args []string) (tuiConfig, error) {
	return parseTUIConfigWithBootstrapStatePath(args, "/etc/regixtry/bootstrap-state.json")
}

func parseTUIConfigWithBootstrapStatePath(args []string, bootstrapStatePath string) (tuiConfig, error) {
	defaultCfg, err := defaultTUIConfigWithBootstrapStatePath(bootstrapStatePath)
	if err != nil {
		return tuiConfig{}, err
	}

	flags := flag.NewFlagSet("tui", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	cfg := defaultCfg
	flags.StringVar(&cfg.StorageRoot, "storage-root", defaultCfg.StorageRoot, "root directory for registry storage")
	flags.StringVar(&cfg.DatabasePath, "db", defaultCfg.DatabasePath, "path to the SQLite metadata database")
	flags.StringVar(&cfg.Tenant, "tenant", ports.DefaultTenant, "tenant identifier")
	flags.StringVar(&cfg.AuthPostgresDSN, "auth-postgres-dsn", defaultCfg.AuthPostgresDSN, "Postgres DSN for auth state")
	flags.StringVar(&cfg.APIBaseURL, "api-base-url", defaultCfg.APIBaseURL, "base URL for authenticated admin API")
	flags.BoolVar(&cfg.Snapshot, "snapshot", false, "render the first inspection view and exit")
	flags.BoolVar(&cfg.DeleteEnabled, "delete-enabled", parseBoolEnv("REGISTRY_DELETE_ENABLED", false), "enable DELETE /v2/<name>/manifests/<reference> (manifest and tag deletion)")
	flags.BoolVar(&cfg.GCDeleteEnabled, "gc-delete-enabled", parseBoolEnv("REGISTRY_GC_DELETE_ENABLED", false), "enable POST /admin/v1/gc/reports/{id}/delete (irreversibly unlinks unreferenced blob files; distinct from -delete-enabled, which is metadata-only)")

	if err := flags.Parse(args); err != nil {
		return tuiConfig{}, err
	}

	visited := map[string]bool{}
	flags.Visit(func(f *flag.Flag) {
		visited[f.Name] = true
	})

	if !visited["db"] && (visited["storage-root"] || strings.TrimSpace(cfg.DatabasePath) == "") {
		cfg.DatabasePath = filepath.Join(cfg.StorageRoot, "metadata.db")
	}
	cfg.APIBaseURL = strings.TrimSpace(cfg.APIBaseURL)
	if cfg.APIBaseURL != "" {
		parsed, err := url.Parse(cfg.APIBaseURL)
		if err != nil {
			return tuiConfig{}, fmt.Errorf("parse admin API base URL: %w", err)
		}
		if !parsed.IsAbs() || strings.TrimSpace(parsed.Host) == "" {
			return tuiConfig{}, errors.New("admin API base URL must be an absolute http(s) URL")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return tuiConfig{}, errors.New("admin API base URL must use http or https")
		}
		cfg.APIBaseURL = strings.TrimRight(parsed.String(), "/")
	}

	return cfg, nil
}

func defaultTUIConfig() (tuiConfig, error) {
	return defaultTUIConfigWithBootstrapStatePath("/etc/regixtry/bootstrap-state.json")
}

func defaultTUIConfigWithBootstrapStatePath(bootstrapStatePath string) (tuiConfig, error) {
	cfg := tuiConfig{
		StorageRoot:     filepath.Join(".", "data"),
		AuthPostgresDSN: os.Getenv("REGISTRY_AUTH_POSTGRES_DSN"),
		APIBaseURL:      os.Getenv("REGISTRY_API_BASE_URL"),
	}

	installedCfg, ok, err := loadSetupManagedTUIConfig(bootstrapStatePath)
	if err != nil {
		return tuiConfig{}, err
	}
	if ok {
		cfg.StorageRoot = installedCfg.StorageRoot
		cfg.DatabasePath = installedCfg.DatabasePath
		cfg.AuthPostgresDSN = installedCfg.AuthPostgresDSN
		if strings.TrimSpace(cfg.APIBaseURL) == "" {
			cfg.APIBaseURL = installedCfg.APIBaseURL
		}
	}

	return cfg, nil
}

func loadSetupManagedTUIConfig(bootstrapStatePath string) (tuiConfig, bool, error) {
	provenancePath := installlinux.LifecycleProvenancePath(bootstrapStatePath)
	if _, err := os.Stat(provenancePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return tuiConfig{}, false, nil
		}
		return tuiConfig{}, false, fmt.Errorf("stat setup-managed runtime config: %w", err)
	}

	envPath := filepath.Join(filepath.Dir(provenancePath), "regixtry.env")
	envValues, err := readSetupManagedEnvFile(envPath)
	if err != nil {
		return tuiConfig{}, false, err
	}

	storageRoot := strings.TrimSpace(envValues["REGISTRY_STORAGE_ROOT"])
	if storageRoot == "" {
		return tuiConfig{}, false, nil
	}

	databasePath := strings.TrimSpace(envValues["REGISTRY_DATABASE_PATH"])
	if databasePath == "" {
		databasePath = filepath.Join(storageRoot, "metadata.db")
	}

	return tuiConfig{
		StorageRoot:     storageRoot,
		DatabasePath:    databasePath,
		AuthPostgresDSN: strings.TrimSpace(envValues["REGISTRY_AUTH_POSTGRES_DSN"]),
		APIBaseURL:      strings.TrimSpace(envValues["REGISTRY_PUBLIC_URL"]),
	}, true, nil
}

func readSetupManagedEnvFile(path string) (map[string]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read setup-managed runtime env: %w", err)
	}

	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, rawValue, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		parsedValue, err := parseSetupManagedEnvValue(rawValue)
		if err != nil {
			return nil, fmt.Errorf("parse setup-managed runtime env %q: %w", strings.TrimSpace(key), err)
		}
		values[strings.TrimSpace(key)] = parsedValue
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan setup-managed runtime env: %w", err)
	}

	return values, nil
}

func parseSetupManagedEnvValue(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if unquoted, err := strconv.Unquote(trimmed); err == nil {
		return unquoted, nil
	}
	return trimmed, nil
}

func parseBootstrapAdminConfig(args []string, stdin io.Reader) (bootstrapAdminConfig, error) {
	flags := flag.NewFlagSet("bootstrap-admin", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var cfg bootstrapAdminConfig
	var passwordFromStdin bool
	flags.StringVar(&cfg.AuthPostgresDSN, "auth-postgres-dsn", os.Getenv("REGISTRY_AUTH_POSTGRES_DSN"), "Postgres DSN for auth state")
	flags.StringVar(&cfg.Username, "username", "admin", "username for the global admin bootstrap account")
	flags.StringVar(&cfg.Password, "password", "", "password for the global admin bootstrap account")
	flags.BoolVar(&passwordFromStdin, "password-stdin", false, "read the bootstrap admin password from stdin")
	flags.BoolVar(&cfg.RotatePassword, "rotate-password", false, "rotate the password when the admin already exists")

	if err := flags.Parse(args); err != nil {
		return bootstrapAdminConfig{}, err
	}

	if cfg.AuthPostgresDSN == "" {
		return bootstrapAdminConfig{}, errors.New("auth Postgres DSN is required")
	}
	if strings.TrimSpace(cfg.Password) != "" {
		cfg.PasswordWarning = "warning: -password is discouraged; prefer -password-stdin to avoid exposing secrets in argv"
		cfg.PasswordSource = "argv"
	}
	if passwordFromStdin {
		secret, err := readSecretFromReader(stdin)
		if err != nil {
			return bootstrapAdminConfig{}, err
		}
		cfg.Password = secret
		cfg.PasswordSource = "stdin"
	}

	if strings.TrimSpace(cfg.Password) == "" {
		return bootstrapAdminConfig{}, errors.New("bootstrap admin password is required")
	}

	return cfg, nil
}

func parseBootstrapConfig(args []string) (BootstrapConfig, error) {
	flags := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var cfg BootstrapConfig
	flags.StringVar(&cfg.Mode, "mode", "", "bootstrap mode to apply")
	flags.StringVar(&cfg.PublicURL, "public-url", os.Getenv("REGISTRY_PUBLIC_URL"), "canonical public URL advertised to registry clients")
	flags.StringVar(&cfg.RuntimeTLSMode, "runtime-tls-mode", "", "runtime TLS mode (local-http, reverse-proxy, or direct-tls)")
	flags.StringVar(&cfg.TLSCertFile, "tls-cert-file", os.Getenv("REGISTRY_TLS_CERT_FILE"), "path to the TLS certificate PEM file for direct-tls mode")
	flags.StringVar(&cfg.TLSKeyFile, "tls-key-file", os.Getenv("REGISTRY_TLS_KEY_FILE"), "path to the TLS private key PEM file for direct-tls mode")
	flags.StringVar(&cfg.Addr, "addr", "127.0.0.1:5000", "address to listen on")
	flags.StringVar(&cfg.StorageRoot, "storage-root", "/var/lib/regixtry", "root directory for registry runtime state")
	flags.StringVar(&cfg.StatePath, "state-path", "/etc/regixtry/bootstrap-state.json", "path to the bootstrap receipt file")
	flags.StringVar(&cfg.UnitPath, "unit-path", "/etc/systemd/system/regixtry.service", "path to the generated systemd unit")
	flags.StringVar(&cfg.ServiceName, "service", "regixtry", "systemd service name")
	flags.BoolVar(&cfg.TrivyEnabled, "trivy-enabled", false, "enable persisted trivy rescans")
	flags.BoolVar(&cfg.TrivyScheduleEnabled, "trivy-schedule-enabled", false, "enable periodic trivy rescans")
	flags.DurationVar(&cfg.TrivyInterval, "trivy-interval", 0, "interval between periodic trivy rescans")
	flags.DurationVar(&cfg.TrivyTimeout, "trivy-timeout", 0, "timeout for each trivy run")
	flags.StringVar(&cfg.TrivyCacheDir, "trivy-cache-dir", "", "shared trivy cache directory")
	flags.StringVar(&cfg.TrivyBinaryPath, "trivy-binary-path", "", "trivy executable path")
	flags.IntVar(&cfg.TrivyMaxConcurrency, "trivy-max-concurrency", 0, "maximum concurrent trivy runs")
	flags.BoolVar(&cfg.DeleteEnabled, "delete-enabled", false, "enable DELETE /v2/<name>/manifests/<reference> (manifest and tag deletion)")
	flags.BoolVar(&cfg.GCDeleteEnabled, "gc-delete-enabled", false, "enable POST /admin/v1/gc/reports/{id}/delete (irreversibly unlinks unreferenced blob files; distinct from -delete-enabled, which is metadata-only)")
	flags.BoolVar(&cfg.NoStart, "no-start", false, "generate bootstrap artifacts without starting the service")
	flags.BoolVar(&cfg.Rollback, "rollback", false, "remove generated bootstrap artifacts and stop the service")

	if err := flags.Parse(args); err != nil {
		return BootstrapConfig{}, err
	}

	if err := installlinux.ValidateConfig(cfg); err != nil {
		return BootstrapConfig{}, err
	}
	runtimeTLSMode, normalizedPublicURL, normalizedTLSCertFile, normalizedTLSKeyFile, err := installlinux.ResolveRuntimeTLSMode(cfg.RuntimeTLSMode, cfg.PublicURL, cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return BootstrapConfig{}, err
	}
	cfg.RuntimeTLSMode = runtimeTLSMode
	cfg.PublicURL = normalizedPublicURL
	cfg.TLSCertFile = normalizedTLSCertFile
	cfg.TLSKeyFile = normalizedTLSKeyFile

	return cfg, nil
}

func parseSetupConfig(args []string) (BootstrapConfig, error) {
	cfg, _, err := parseSetupConfigWithPromptState(args)
	if err != nil {
		return BootstrapConfig{}, err
	}

	return cfg.BootstrapConfig, nil
}

func parseSetupConfigWithPromptState(args []string) (setupConfig, setupPromptState, error) {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	defaultPublicURL := os.Getenv("REGISTRY_PUBLIC_URL")
	defaultAuthPostgresDSN := os.Getenv("REGISTRY_AUTH_POSTGRES_DSN")
	cfg := setupConfig{}
	flags.StringVar(&cfg.Mode, "mode", "", "setup mode to apply (daemon-sqlite or binary-only)")
	flags.StringVar(&cfg.PublicURL, "public-url", defaultPublicURL, "canonical public URL advertised to registry clients")
	flags.StringVar(&cfg.RuntimeTLSMode, "runtime-tls-mode", "", "runtime TLS mode (local-http, reverse-proxy, or direct-tls)")
	flags.StringVar(&cfg.TLSCertFile, "tls-cert-file", os.Getenv("REGISTRY_TLS_CERT_FILE"), "path to the TLS certificate PEM file for direct-tls mode")
	flags.StringVar(&cfg.TLSKeyFile, "tls-key-file", os.Getenv("REGISTRY_TLS_KEY_FILE"), "path to the TLS private key PEM file for direct-tls mode")
	flags.StringVar(&cfg.Addr, "addr", "127.0.0.1:5000", "address to listen on")
	flags.StringVar(&cfg.StorageRoot, "storage-root", "/var/lib/regixtry", "root directory for registry runtime state")
	flags.StringVar(&cfg.StatePath, "state-path", "/etc/regixtry/bootstrap-state.json", "path to the bootstrap receipt file")
	flags.StringVar(&cfg.UnitPath, "unit-path", "/etc/systemd/system/regixtry.service", "path to the generated systemd unit")
	flags.StringVar(&cfg.ServiceName, "service", "regixtry", "systemd service name")
	flags.BoolVar(&cfg.TrivyEnabled, "trivy-enabled", parseBoolEnv("REGISTRY_TRIVY_ENABLED", false), "enable persisted trivy rescans")
	flags.BoolVar(&cfg.TrivyScheduleEnabled, "trivy-schedule-enabled", parseBoolEnv("REGISTRY_TRIVY_SCHEDULE_ENABLED", false), "enable periodic trivy rescans")
	flags.DurationVar(&cfg.TrivyInterval, "trivy-interval", parseDurationEnv("REGISTRY_TRIVY_INTERVAL", 24*time.Hour), "interval between periodic trivy rescans")
	flags.DurationVar(&cfg.TrivyTimeout, "trivy-timeout", parseDurationEnv("REGISTRY_TRIVY_TIMEOUT", 15*time.Minute), "timeout for each trivy run")
	flags.StringVar(&cfg.TrivyCacheDir, "trivy-cache-dir", os.Getenv("REGISTRY_TRIVY_CACHE_DIR"), "shared trivy cache directory")
	flags.StringVar(&cfg.TrivyBinaryPath, "trivy-binary-path", firstNonEmpty(os.Getenv("REGISTRY_TRIVY_BINARY_PATH"), "trivy"), "trivy executable path")
	flags.IntVar(&cfg.TrivyMaxConcurrency, "trivy-max-concurrency", parseIntEnv("REGISTRY_TRIVY_MAX_CONCURRENCY", 1), "maximum concurrent trivy runs")
	flags.BoolVar(&cfg.DeleteEnabled, "delete-enabled", parseBoolEnv("REGISTRY_DELETE_ENABLED", false), "enable DELETE /v2/<name>/manifests/<reference> (manifest and tag deletion)")
	flags.BoolVar(&cfg.GCDeleteEnabled, "gc-delete-enabled", parseBoolEnv("REGISTRY_GC_DELETE_ENABLED", false), "enable POST /admin/v1/gc/reports/{id}/delete (irreversibly unlinks unreferenced blob files; distinct from -delete-enabled, which is metadata-only)")
	flags.BoolVar(&cfg.NoStart, "no-start", false, "generate setup artifacts without starting the service")
	flags.StringVar(&cfg.Auth.AuthPostgresDSN, "auth-postgres-dsn", defaultAuthPostgresDSN, "Postgres DSN for auth state")
	flags.StringVar(&cfg.Auth.AdminUsername, "admin-username", "admin", "username for the setup bootstrap admin account")
	flags.StringVar(&cfg.Auth.AdminPassword, "admin-password", "", "password for the setup bootstrap admin account")

	if err := flags.Parse(args); err != nil {
		return setupConfig{}, setupPromptState{}, err
	}

	cfg.Auth.Enabled = strings.TrimSpace(cfg.Auth.AuthPostgresDSN) != ""
	cfg.Auth.Host = defaultSetupAuthHost
	cfg.Auth.Port = defaultSetupAuthPort
	cfg.Auth.User = defaultSetupAuthUser
	cfg.Auth.SSLMode = defaultSetupAuthSSLMode
	cfg.Auth.AdminUsername = strings.TrimSpace(cfg.Auth.AdminUsername)
	cfg.Auth.AdminPassword = strings.TrimSpace(cfg.Auth.AdminPassword)
	cfg.Auth.AuthPostgresDSN = strings.TrimSpace(cfg.Auth.AuthPostgresDSN)
	cfg.BootstrapConfig.AuthPostgresDSN = cfg.Auth.AuthPostgresDSN
	if strings.TrimSpace(cfg.TrivyCacheDir) == "" {
		cfg.TrivyCacheDir = filepath.Join(cfg.StorageRoot, "trivy-cache")
	}

	promptState := setupPromptState{
		publicURLProvided:       strings.TrimSpace(defaultPublicURL) != "",
		authPostgresDSNProvided: strings.TrimSpace(defaultAuthPostgresDSN) != "",
	}
	flags.Visit(func(flag *flag.Flag) {
		switch flag.Name {
		case "addr":
			promptState.addrProvided = true
		case "public-url":
			promptState.publicURLProvided = true
		case "runtime-tls-mode":
			promptState.runtimeTLSModeProvided = true
		case "tls-cert-file":
			promptState.tlsCertFileProvided = true
		case "tls-key-file":
			promptState.tlsKeyFileProvided = true
		case "auth-postgres-dsn":
			promptState.authPostgresDSNProvided = true
		case "admin-username":
			promptState.adminUsernameProvided = true
		case "admin-password":
			promptState.adminPasswordProvided = true
		case "trivy-enabled", "trivy-schedule-enabled", "trivy-interval", "trivy-timeout", "trivy-cache-dir", "trivy-binary-path", "trivy-max-concurrency":
			promptState.legacyTrivyProvided = true
		}
	})

	return cfg, promptState, nil
}

func parseUninstallConfig(args []string) (uninstallConfig, error) {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	defaultStatePath := installlinux.LifecycleProvenancePath("/etc/regixtry/bootstrap-state.json")
	var cfg uninstallConfig
	flags.StringVar(&cfg.StatePath, "state-path", defaultStatePath, "path to the lifecycle provenance file")

	if err := flags.Parse(args); err != nil {
		return uninstallConfig{}, err
	}

	cfg.StatePath = strings.TrimSpace(cfg.StatePath)
	if cfg.StatePath == "" {
		return uninstallConfig{}, errors.New("state-path is required")
	}

	return cfg, nil
}

func parseUpgradeConfig(args []string) (upgradeConfig, error) {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	defaultStatePath := installlinux.LifecycleProvenancePath("/etc/regixtry/bootstrap-state.json")
	var cfg upgradeConfig
	flags.StringVar(&cfg.Ref, "ref", "", "target release tag; defaults to the latest compatible release")
	flags.StringVar(&cfg.StatePath, "state-path", defaultStatePath, "path to the lifecycle provenance file")
	flags.BoolVar(&cfg.AssumeYes, "yes", false, "accept risky upgrade confirmations without prompting")

	if err := flags.Parse(args); err != nil {
		return upgradeConfig{}, err
	}

	cfg.Ref = strings.TrimSpace(cfg.Ref)
	cfg.StatePath = strings.TrimSpace(cfg.StatePath)
	if cfg.StatePath == "" {
		return upgradeConfig{}, errors.New("state-path is required")
	}
	return cfg, nil
}

func parseFeatureConfig(args []string) (featureConfig, error) {
	defaultCfg, err := defaultFeatureConfigWithBootstrapStatePath(featureBootstrapStatePath)
	if err != nil {
		return featureConfig{}, err
	}

	flags := flag.NewFlagSet("feature", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	cfg := defaultCfg
	flags.StringVar(&cfg.StorageRoot, "storage-root", defaultCfg.StorageRoot, "root directory for registry runtime state")
	flags.StringVar(&cfg.DatabasePath, "db", defaultCfg.DatabasePath, "path to the SQLite metadata database")
	flags.StringVar(&cfg.PublicURL, "public-url", defaultCfg.PublicURL, "canonical public URL advertised to registry clients")
	flags.StringVar(&cfg.Tenant, "tenant", ports.DefaultTenant, "tenant identifier")

	if err := flags.Parse(args); err != nil {
		return featureConfig{}, err
	}
	finalizeFeatureConfigFlags(flags, &cfg)
	return cfg, nil
}

func defaultFeatureConfigWithBootstrapStatePath(bootstrapStatePath string) (featureConfig, error) {
	cfg := featureConfig{
		StorageRoot: filepath.Join(".", "data"),
		PublicURL:   os.Getenv("REGISTRY_PUBLIC_URL"),
	}

	installedCfg, ok, err := loadSetupManagedTUIConfig(bootstrapStatePath)
	if err != nil {
		return featureConfig{}, err
	}
	if ok {
		cfg.StorageRoot = installedCfg.StorageRoot
		cfg.DatabasePath = installedCfg.DatabasePath
		if strings.TrimSpace(cfg.PublicURL) == "" {
			cfg.PublicURL = installedCfg.APIBaseURL
		}
	}

	return cfg, nil
}

func finalizeFeatureConfigFlags(flags *flag.FlagSet, cfg *featureConfig) {
	visited := map[string]bool{}
	flags.Visit(func(f *flag.Flag) {
		visited[f.Name] = true
	})
	if !visited["db"] && (visited["storage-root"] || strings.TrimSpace(cfg.DatabasePath) == "") {
		cfg.DatabasePath = filepath.Join(cfg.StorageRoot, "metadata.db")
	}
}

func runFeature(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("expected feature action: list, show, status, install, upgrade, rollback, enable, disable, or configure")
	}

	switch args[0] {
	case "list":
		cfg, err := parseFeatureConfig(args[1:])
		if err != nil {
			return err
		}
		service, cleanup, err := openFeatureService(cfg)
		if err != nil {
			return err
		}
		defer cleanup()
		features, err := service.ListFeatures(ctx)
		if err != nil {
			return err
		}
		return writeFeatureTable(stdout, features)
	case "show", "status", "install", "upgrade", "rollback", "enable", "disable":
		if len(args) < 2 {
			return fmt.Errorf("feature %s requires a feature name", args[0])
		}
		name := args[1]
		if err := appregixtry.ValidateFeatureName(name); err != nil {
			return err
		}
		flags := flag.NewFlagSet("feature "+args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		var (
			cfg     featureConfig
			version string
		)
		defaultCfg, err := defaultFeatureConfigWithBootstrapStatePath(featureBootstrapStatePath)
		if err != nil {
			return err
		}
		cfg = defaultCfg
		flags.StringVar(&cfg.StorageRoot, "storage-root", defaultCfg.StorageRoot, "root directory for registry runtime state")
		flags.StringVar(&cfg.DatabasePath, "db", defaultCfg.DatabasePath, "path to the SQLite metadata database")
		flags.StringVar(&cfg.PublicURL, "public-url", defaultCfg.PublicURL, "canonical public URL advertised to registry clients")
		flags.StringVar(&cfg.Tenant, "tenant", ports.DefaultTenant, "tenant identifier")
		flags.StringVar(&version, "version", "", "managed trivy runtime version")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		finalizeFeatureConfigFlags(flags, &cfg)
		service, cleanup, err := openFeatureService(cfg)
		if err != nil {
			return err
		}
		defer cleanup()

		switch args[0] {
		case "show":
			details, err := service.GetFeature(ctx, name)
			if err != nil {
				return err
			}
			return writeFeatureDetails(stdout, details, false)
		case "status":
			details, err := service.GetFeatureStatus(ctx, name)
			if err != nil {
				return err
			}
			return writeFeatureDetails(stdout, details, true)
		case "install":
			state, err := service.InstallFeatureRuntimeWithProgress(ctx, name, version, featureProgressWriter(stdout))
			if err != nil {
				_, _ = fmt.Fprintf(stdout, "Failed managed runtime install for %s: %v\n", name, err)
				return err
			}
			_, err = fmt.Fprintf(stdout, "Installed managed runtime for %s at %s\n", name, state.ActiveVersion)
			return err
		case "upgrade":
			state, err := service.UpgradeFeatureRuntimeWithProgress(ctx, name, version, featureProgressWriter(stdout))
			if err != nil {
				_, _ = fmt.Fprintf(stdout, "Failed managed runtime upgrade for %s: %v\n", name, err)
				return err
			}
			_, err = fmt.Fprintf(stdout, "Upgraded managed runtime for %s to %s\n", name, state.ActiveVersion)
			return err
		case "rollback":
			state, err := service.RollbackFeatureRuntime(ctx, name)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "Rolled back managed runtime for %s to %s\n", name, state.ActiveVersion)
			return err
		case "enable":
			details, err := service.SetFeatureEnabled(ctx, name, true)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "Enabled feature %s\n", details.Name)
			return err
		case "disable":
			details, err := service.SetFeatureEnabled(ctx, name, false)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "Disabled feature %s\n", details.Name)
			return err
		}
	case "configure":
		if len(args) < 2 {
			return errors.New("feature configure requires a feature name")
		}
		name := args[1]
		if err := appregixtry.ValidateFeatureName(name); err != nil {
			return err
		}
		flags := flag.NewFlagSet("feature configure", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		var (
			cfg                   featureConfig
			enabled               bool
			scheduleEnabled       bool
			interval              time.Duration
			timeout               time.Duration
			serviceURL            string
			registryReachableURL  string
			authToken             string
			tlsCACertPath         string
			tlsInsecureSkipVerify bool
			maxConcurrency        int
		)
		defaultCfg, err := defaultFeatureConfigWithBootstrapStatePath(featureBootstrapStatePath)
		if err != nil {
			return err
		}
		cfg = defaultCfg
		flags.StringVar(&cfg.StorageRoot, "storage-root", defaultCfg.StorageRoot, "root directory for registry runtime state")
		flags.StringVar(&cfg.DatabasePath, "db", defaultCfg.DatabasePath, "path to the SQLite metadata database")
		flags.StringVar(&cfg.Tenant, "tenant", ports.DefaultTenant, "tenant identifier")
		flags.BoolVar(&enabled, "enabled", false, "enable the feature")
		flags.BoolVar(&scheduleEnabled, "schedule-enabled", false, "enable scheduled execution")
		flags.DurationVar(&interval, "interval", 0, "feature interval")
		flags.DurationVar(&timeout, "timeout", 0, "feature timeout")
		flags.StringVar(&serviceURL, "service-url", "", "feature service URL")
		flags.StringVar(&registryReachableURL, "registry-reachable-url", "", "scanner-facing registry URL")
		flags.StringVar(&authToken, "auth-token", "", "feature bearer token")
		flags.StringVar(&tlsCACertPath, "tls-ca-cert-path", "", "feature TLS CA certificate path")
		flags.BoolVar(&tlsInsecureSkipVerify, "tls-insecure-skip-verify", false, "skip feature TLS verification")
		flags.IntVar(&maxConcurrency, "max-concurrency", 0, "feature max concurrency")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		finalizeFeatureConfigFlags(flags, &cfg)
		input := ports.FeatureConfigureInput{}
		flags.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "enabled":
				input.Enabled = &enabled
			case "schedule-enabled":
				input.ScheduleEnabled = &scheduleEnabled
			case "interval":
				input.Interval = &interval
			case "timeout":
				input.Timeout = &timeout
			case "service-url":
				input.ServiceURL = &serviceURL
			case "registry-reachable-url":
				input.RegistryReachableURL = &registryReachableURL
			case "auth-token":
				input.AuthToken = &authToken
			case "tls-ca-cert-path":
				input.TLSCACertPath = &tlsCACertPath
			case "tls-insecure-skip-verify":
				input.TLSInsecureSkipVerify = &tlsInsecureSkipVerify
			case "max-concurrency":
				input.MaxConcurrency = &maxConcurrency
			}
		})
		service, cleanup, err := openFeatureService(cfg)
		if err != nil {
			return err
		}
		defer cleanup()
		details, err := service.ConfigureFeature(ctx, name, input)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "Configured feature %s\n", details.Name)
		return err
	}

	return fmt.Errorf("unsupported feature action %q", args[0])
}

func openFeatureService(cfg featureConfig) (*appregixtry.Service, func(), error) {
	if err := os.MkdirAll(cfg.StorageRoot, 0o755); err != nil {
		return nil, nil, err
	}
	store, err := metadata.New(cfg.DatabasePath)
	if err != nil {
		return nil, nil, err
	}
	service := appregixtry.NewService(nil, store, ports.NewConfigurableAccessController(ports.AccessConfig{}), ports.NewSingleTenantResolver(cfg.Tenant), ports.NewInlineJobRunner())
	service.SetScanHost(cfg.PublicURL)
	service.SetScanRunner(trivyinfra.New(trivyinfra.RunnerConfig{}))
	service.SetFeatureRuntimeManager("trivy", newFeatureRuntimeManager("trivy", appregixtry.FeatureRuntimeManagerConfig{StorageRoot: cfg.StorageRoot, Store: store, ScanRunner: trivyinfra.New(trivyinfra.RunnerConfig{})}))
	service.SetSecretScanRunner(gitleaksinfra.New(gitleaksinfra.RunnerConfig{}))
	service.SetFeatureRuntimeManager("gitleaks", newFeatureRuntimeManager("gitleaks", appregixtry.FeatureRuntimeManagerConfig{StorageRoot: cfg.StorageRoot, Store: store}))
	return service, func() { _ = store.Close() }, nil
}

func writeFeatureDetails(stdout io.Writer, details ports.FeatureDetails, includeRuntime bool) error {
	lines := []string{
		fmt.Sprintf("Name: %s", details.Name),
		fmt.Sprintf("Kind: %s", details.Kind),
		fmt.Sprintf("Enabled: %t", details.Enabled),
		fmt.Sprintf("Configured: %t", details.Configured),
		fmt.Sprintf("Schedule Enabled: %t", details.ScheduleEnabled),
		fmt.Sprintf("Interval: %s", details.Interval),
		fmt.Sprintf("Timeout: %s", details.Timeout),
		fmt.Sprintf("Service URL: %s", details.ServiceURL),
		fmt.Sprintf("Registry Reachable URL: %s", details.RegistryReachableURL),
		fmt.Sprintf("TLS CA Cert Path: %s", details.TLSCACertPath),
		fmt.Sprintf("TLS Insecure Skip Verify: %t", details.TLSInsecureSkipVerify),
		fmt.Sprintf("Max Concurrency: %d", details.MaxConcurrency),
	}
	if includeRuntime {
		lines = append(lines,
			fmt.Sprintf("Runtime Status: %s", firstNonEmpty(details.Runtime.Status, details.Runtime.Health, "unknown")),
			fmt.Sprintf("Runtime Health: %s", firstNonEmpty(details.Runtime.Health, "unknown")),
			fmt.Sprintf("Runtime Version: %s", firstNonEmpty(details.Runtime.Version, "unknown")),
			fmt.Sprintf("Runtime Latest Version: %s", firstNonEmpty(details.Runtime.LatestVersion, "unknown")),
			fmt.Sprintf("Runtime Update Status: %s", firstNonEmpty(details.Runtime.UpdateStatus, "unknown")),
		)
		if details.Runtime.RollbackAvailable {
			lines = append(lines, "Runtime Rollback Available: true")
		}
		if strings.TrimSpace(details.Runtime.Detail) != "" {
			lines = append(lines, fmt.Sprintf("Runtime Detail: %s", details.Runtime.Detail))
		}
	}
	_, err := fmt.Fprintln(stdout, strings.Join(lines, "\n"))
	return err
}

func writeFeatureTable(stdout io.Writer, features []ports.FeatureSummary) error {
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "NAME\tKIND\tENABLED\tCONFIGURED\tCURRENT\tLATEST\tUPDATE"); err != nil {
		return err
	}
	for _, feature := range features {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%t\t%t\t%s\t%s\t%s\n",
			feature.Name,
			feature.Kind,
			feature.Enabled,
			feature.Configured,
			firstNonEmpty(feature.CurrentVersion, "unknown"),
			firstNonEmpty(feature.LatestVersion, "unknown"),
			firstNonEmpty(feature.UpdateStatus, "unknown"),
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func featureProgressWriter(stdout io.Writer) func(ports.FeatureRuntimeProgress) {
	return func(progress ports.FeatureRuntimeProgress) {
		if stdout == nil {
			return
		}
		_, _ = fmt.Fprintf(stdout, "[%s] %s\n", firstNonEmpty(strings.TrimSpace(progress.Stage), "unknown"), firstNonEmpty(strings.TrimSpace(progress.Detail), "working"))
	}
}

func runSetup(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	cfg, promptState, err := parseSetupConfigWithPromptState(args)
	if err != nil {
		return err
	}
	if stdin == nil {
		stdin = strings.NewReader("")
	}

	interactive := isInteractiveTTYPair(stdin, stdout)
	reader := bufio.NewReader(stdin)

	mode, selectedInteractively, err := resolveSetupMode(cfg.Mode, interactive, reader, stdout)
	if err != nil {
		return err
	}

	switch mode {
	case "binary-only":
		return printBinaryOnlyGuidance(stdout)
	case "daemon-sqlite":
		cfg.Mode = mode
		if interactive {
			cfg, err = promptSetupDaemonConfig(reader, stdout, cfg, promptState, selectedInteractively)
			if err != nil {
				return err
			}
		}
		if err := validateSetupAuthConfig(cfg.Auth); err != nil {
			return err
		}
		cfg.BootstrapConfig.AuthPostgresDSN = cfg.Auth.AuthPostgresDSN
		if err := installlinux.ValidateConfig(cfg.BootstrapConfig); err != nil {
			return err
		}
		cfg.RuntimeTLSMode, cfg.PublicURL, cfg.TLSCertFile, cfg.TLSKeyFile, err = installlinux.ResolveRuntimeTLSMode(cfg.RuntimeTLSMode, cfg.PublicURL, cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			return err
		}

		authOutcome, err := bootstrapSetupAuth(ctx, cfg.Auth)
		if err != nil {
			return err
		}

		runner := newBootstrapRunner()
		if err := runner.Run(ctx, cfg.BootstrapConfig); err != nil {
			return upgradeLifecyclePermissionError(err, "daemon-sqlite setup", formatSetupCommand(cfg.BootstrapConfig, false))
		}

		legacyImported := false
		if promptState.legacyTrivyProvided {
			legacyImported, err = importLegacySetupTrivyFlags(ctx, cfg.BootstrapConfig)
			if err != nil {
				return rollbackSetupFailure(ctx, runner, cfg.BootstrapConfig, fmt.Errorf("import legacy trivy setup flags: %w", err))
			}
		}

		provenance, err := runner.PlanLifecycleProvenance(cfg.BootstrapConfig)
		if err != nil {
			return rollbackSetupFailure(ctx, runner, cfg.BootstrapConfig, fmt.Errorf("plan lifecycle provenance: %w", err))
		}
		if err := runner.SaveLifecycleProvenance(provenance); err != nil {
			return upgradeLifecyclePermissionError(
				rollbackSetupFailure(ctx, runner, cfg.BootstrapConfig, fmt.Errorf("write lifecycle provenance: %w", err)),
				"daemon-sqlite setup",
				formatSetupCommand(cfg.BootstrapConfig, false),
			)
		}

		if stdout != nil {
			_, _ = fmt.Fprintf(stdout, "Setup complete: regixtry is installed, %s.service is running, and %s is reachable.\n", cfg.ServiceName, cfg.PublicURL)
			_, _ = fmt.Fprintf(stdout, "Lifecycle provenance recorded at %s\n", provenance.StatePath)
			if legacyImported {
				_, _ = fmt.Fprintln(stdout, "Legacy Trivy setup flags were imported into feature state. Use `regixtry feature ...` to manage Trivy going forward; service_url and registry_reachable_url are still required.")
			}
			printSetupAuthGuidance(stdout, cfg, authOutcome)
		}
		return nil
	default:
		return fmt.Errorf("unsupported setup mode %q", mode)
	}
}

func promptSetupDaemonConfig(reader *bufio.Reader, stdout io.Writer, cfg setupConfig, promptState setupPromptState, selectedInteractively bool) (setupConfig, error) {
	if !promptState.addrProvided {
		value, err := promptSetupValue(reader, stdout, "Listen address", cfg.Addr)
		if err != nil {
			return setupConfig{}, err
		}
		cfg.Addr = value
	}
	if !promptState.runtimeTLSModeProvided {
		value, err := promptSetupRuntimeTLSMode(reader, stdout, defaultSetupRuntimeTLSMode(cfg.BootstrapConfig))
		if err != nil {
			return setupConfig{}, err
		}
		cfg.RuntimeTLSMode = value
	}

	if !promptState.publicURLProvided {
		defaultPublicURL := cfg.PublicURL
		if strings.TrimSpace(defaultPublicURL) == "" {
			defaultPublicURL = defaultSetupPublicURLForMode(cfg.Addr, cfg.RuntimeTLSMode)
		}
		value, err := promptSetupValue(reader, stdout, "Public URL", defaultPublicURL)
		if err != nil {
			return setupConfig{}, err
		}
		cfg.PublicURL = value
	}
	if strings.TrimSpace(cfg.RuntimeTLSMode) == installlinux.RuntimeTLSModeDirectTLS {
		if !promptState.tlsCertFileProvided {
			value, err := promptSetupValue(reader, stdout, "TLS cert file", cfg.TLSCertFile)
			if err != nil {
				return setupConfig{}, err
			}
			cfg.TLSCertFile = value
		}
		if !promptState.tlsKeyFileProvided {
			value, err := promptSetupValue(reader, stdout, "TLS key file", cfg.TLSKeyFile)
			if err != nil {
				return setupConfig{}, err
			}
			cfg.TLSKeyFile = value
		}
	}

	enableAuth, err := promptSetupBool(reader, stdout, "Enable auth", cfg.Auth.Enabled)
	if err != nil {
		return setupConfig{}, err
	}
	cfg.Auth.Enabled = enableAuth
	if !cfg.Auth.Enabled {
		cfg.Auth.AuthPostgresDSN = ""
		cfg.Auth.AdminPassword = ""
		cfg.BootstrapConfig.AuthPostgresDSN = ""
		return cfg, nil
	}
	if !promptState.authPostgresDSNProvided {
		authDSN, err := promptSetupAuthPostgresDSN(reader, stdout, cfg.Auth)
		if err != nil {
			return setupConfig{}, err
		}
		cfg.Auth.AuthPostgresDSN = authDSN
	}
	if !promptState.adminUsernameProvided {
		value, err := promptSetupValue(reader, stdout, "Admin username", firstNonEmpty(cfg.Auth.AdminUsername, "admin"))
		if err != nil {
			return setupConfig{}, err
		}
		cfg.Auth.AdminUsername = value
	}
	if !promptState.adminPasswordProvided {
		value, err := promptSetupValue(reader, stdout, "Admin password", "")
		if err != nil {
			return setupConfig{}, err
		}
		cfg.Auth.AdminPassword = value
	}
	cfg.BootstrapConfig.AuthPostgresDSN = strings.TrimSpace(cfg.Auth.AuthPostgresDSN)

	return cfg, nil
}

func promptSetupAuthPostgresDSN(reader *bufio.Reader, stdout io.Writer, cfg setupAuthConfig) (string, error) {
	host, err := promptSetupValue(reader, stdout, "Auth Postgres host", firstNonEmpty(cfg.Host, defaultSetupAuthHost))
	if err != nil {
		return "", err
	}
	port, err := promptSetupValue(reader, stdout, "Auth Postgres port", firstNonEmpty(cfg.Port, defaultSetupAuthPort))
	if err != nil {
		return "", err
	}
	user, err := promptSetupValue(reader, stdout, "Auth Postgres user", firstNonEmpty(cfg.User, defaultSetupAuthUser))
	if err != nil {
		return "", err
	}
	password, err := promptSetupValue(reader, stdout, "Auth Postgres password", cfg.Password)
	if err != nil {
		return "", err
	}
	sslMode, err := promptSetupValue(reader, stdout, "Auth Postgres ssl mode", firstNonEmpty(cfg.SSLMode, defaultSetupAuthSSLMode))
	if err != nil {
		return "", err
	}
	return buildSetupAuthPostgresDSN(host, port, user, password, sslMode)
}

func buildSetupAuthPostgresDSN(host string, port string, user string, password string, sslMode string) (string, error) {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	user = strings.TrimSpace(user)
	sslMode = strings.TrimSpace(sslMode)
	if host == "" {
		return "", errors.New("auth Postgres host is required when auth is enabled")
	}
	if port == "" {
		return "", errors.New("auth Postgres port is required when auth is enabled")
	}
	if user == "" {
		return "", errors.New("auth Postgres user is required when auth is enabled")
	}
	if sslMode == "" {
		return "", errors.New("auth Postgres ssl mode is required when auth is enabled")
	}
	assembled := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   net.JoinHostPort(host, port),
		Path:   defaultSetupAuthDBName,
	}
	query := assembled.Query()
	query.Set("sslmode", sslMode)
	assembled.RawQuery = query.Encode()
	return assembled.String(), nil
}

func importLegacySetupTrivyFlags(ctx context.Context, cfg installlinux.BootstrapConfig) (bool, error) {
	service, cleanup, err := openFeatureService(featureConfig{
		StorageRoot:  cfg.StorageRoot,
		DatabasePath: filepath.Join(cfg.StorageRoot, "metadata.db"),
		Tenant:       ports.DefaultTenant,
	})
	if err != nil {
		return false, err
	}
	defer cleanup()
	details, err := service.ImportLegacyFeatureConfigIfMissing(ctx, "trivy", ports.FeatureConfigureInput{
		Enabled:         boolPointer(cfg.TrivyEnabled),
		ScheduleEnabled: boolPointer(cfg.TrivyScheduleEnabled),
		Interval:        durationPointer(cfg.TrivyInterval),
		Timeout:         durationPointer(cfg.TrivyTimeout),
		MaxConcurrency:  intPointer(cfg.TrivyMaxConcurrency),
	})
	if err != nil {
		return false, err
	}
	return details.Configured, nil
}

func boolPointer(value bool) *bool { return &value }

func durationPointer(value time.Duration) *time.Duration { return &value }

func stringPointer(value string) *string { return &value }

func intPointer(value int) *int { return &value }

func runUninstall(ctx context.Context, args []string, stdout io.Writer) error {
	cfg, err := parseUninstallConfig(args)
	if err != nil {
		return err
	}

	runner := newBootstrapRunner()
	report, uninstallErr := runner.Uninstall(ctx, cfg.StatePath)
	if stdout != nil {
		if _, err := fmt.Fprintln(stdout, report.Format()); err != nil {
			return err
		}
	}

	return upgradeLifecyclePermissionError(uninstallErr, "daemon-sqlite uninstall", formatUninstallCommand(cfg))
}

func runUpgrade(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	cfg, err := parseUpgradeConfig(args)
	if err != nil {
		return err
	}
	if stdin == nil {
		stdin = strings.NewReader("")
	}

	runner := newBootstrapRunner()
	interactive := isInteractiveTTYPair(stdin, stdout)
	progress := newUpgradeProgressWriter(stdout, interactive)
	reader := bufio.NewReader(stdin)
	result, upgradeErr := runner.Upgrade(ctx, installlinux.UpgradeConfig{
		Ref:            cfg.Ref,
		ProvenancePath: cfg.StatePath,
		AssumeYes:      cfg.AssumeYes,
		Preflight: func(preflight installlinux.UpgradePreflight) error {
			return renderUpgradePreflight(stdout, preflight)
		},
		Confirm: func(preflight installlinux.UpgradePreflight) error {
			return confirmUpgradePreflight(reader, stdout, interactive, cfg.AssumeYes, preflight)
		},
		Progress: progress.Advance,
	})
	if stdout != nil && upgradeErr == nil {
		if result.UpToDate {
			return nil
		}
		progress.Finish(result)
	}
	if upgradeErr != nil {
		progress.Fail()
	}
	return upgradeLifecyclePermissionError(upgradeErr, "daemon-sqlite upgrade", formatUpgradeCommand(cfg))
}

func renderUpgradePreflight(stdout io.Writer, preflight installlinux.UpgradePreflight) error {
	if stdout != nil {
		_, _ = fmt.Fprintf(stdout, "Installed version: %s\n", formatUpgradeSummaryIdentity(preflight.InstalledRef, preflight.InstalledVersion))
		_, _ = fmt.Fprintf(stdout, "Available version: %s\n", formatUpgradeSummaryIdentity(preflight.TargetRef, preflight.TargetVersion))
	}
	if preflight.UpToDate {
		if stdout != nil {
			_, _ = fmt.Fprintln(stdout, "regixtry is already up to date.")
		}
		return nil
	}
	return nil
}

func confirmUpgradePreflight(reader *bufio.Reader, stdout io.Writer, interactive bool, assumeYes bool, preflight installlinux.UpgradePreflight) error {
	if preflight.UpToDate {
		return nil
	}
	if assumeYes || !interactive {
		return nil
	}
	return promptUpgradeConfirmation(reader, stdout)
}

func promptUpgradeConfirmation(reader *bufio.Reader, stdout io.Writer) error {
	if reader == nil {
		return errors.New("upgrade confirmation requires input")
	}
	if stdout != nil {
		_, _ = fmt.Fprint(stdout, "Proceed with upgrade [y/N]: ")
	}
	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read upgrade confirmation: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes":
		return nil
	case "", "n", "no":
		return errors.New("upgrade cancelled")
	default:
		return fmt.Errorf("unsupported upgrade confirmation %q", strings.TrimSpace(value))
	}
}

// upgradeStageOrder lists the normal (non-rollback) lifecycle stages, in
// order, driving the cliprogress.Checklist shown for `regixtry upgrade`.
// "rollback" is deliberately excluded: it is a rare, failure-only stage and
// showing it upfront as a pending checklist entry during a normal upgrade
// would be confusing, so it is rendered as a standalone announcement line
// instead (see upgradeProgressWriter.printRollback).
var upgradeStageOrder = []string{"resolve", "download", "verify", "stop", "swap", "restart", "health-check"}

type upgradeProgressWriter struct {
	out         io.Writer
	interactive bool
	stageKnown  map[string]bool
	checklist   *cliprogress.Checklist
	bar         *cliprogress.Bar
	barActive   bool
	activeKey   string
}

func newUpgradeProgressWriter(out io.Writer, interactive bool) *upgradeProgressWriter {
	steps := make([]cliprogress.Step, 0, len(upgradeStageOrder))
	stageKnown := make(map[string]bool, len(upgradeStageOrder))
	for _, key := range upgradeStageOrder {
		steps = append(steps, cliprogress.Step{Key: key, Label: upgradeStageLabel(key)})
		stageKnown[key] = true
	}
	return &upgradeProgressWriter{
		out:         out,
		interactive: interactive,
		stageKnown:  stageKnown,
		checklist:   cliprogress.NewChecklist(out, interactive, steps),
		bar:         cliprogress.NewBar(out, interactive),
	}
}

func (w *upgradeProgressWriter) Advance(event installlinux.UpgradeProgress) {
	if w == nil || w.out == nil {
		return
	}
	if event.Stage == "rollback" {
		w.closeBar()
		w.printRollback(event)
		return
	}
	if !w.stageKnown[event.Stage] {
		return
	}
	if event.Stage == "download" && event.TotalBytes > 0 {
		// The bar draws its own line directly below the checklist's fixed
		// block; the checklist itself is not touched again until the bar is
		// closed, so its cursor-up redraw math stays valid.
		w.activeKey = "download"
		w.barActive = true
		w.bar.Update(event.BytesRead, event.TotalBytes, upgradeStageLabel("download"))
		return
	}
	w.closeBar()
	w.activeKey = event.Stage
	w.checklist.Activate(event.Stage, event.Detail)
}

// closeBar clears the bar's transient line (if any) so the checklist's next
// redraw resumes from the exact row it left off at.
func (w *upgradeProgressWriter) closeBar() {
	if !w.barActive {
		return
	}
	w.barActive = false
	if w.interactive {
		_, _ = fmt.Fprint(w.out, "\r\x1b[K")
	}
}

func (w *upgradeProgressWriter) printRollback(event installlinux.UpgradeProgress) {
	label := upgradeStageLabel(event.Stage)
	detail := strings.TrimSpace(event.Detail)
	line := label
	if detail != "" {
		line = label + ": " + detail
	}
	if w.interactive {
		_, _ = fmt.Fprintf(w.out, "  ! %s\n", line)
		return
	}
	_, _ = fmt.Fprintln(w.out, line)
}

func (w *upgradeProgressWriter) Finish(result installlinux.UpgradeResult) {
	if w == nil || w.out == nil {
		return
	}
	w.closeBar()
	if w.activeKey != "" {
		w.checklist.Complete(w.activeKey)
	}
	if w.interactive {
		_, _ = fmt.Fprintln(w.out)
	}
	_, _ = fmt.Fprintf(w.out, "Upgrade complete: %s -> %s. Lifecycle provenance: %s\n", formatUpgradeSummaryIdentity(result.FromRef, result.FromVersion), formatUpgradeSummaryIdentity(result.TargetRef, result.ToVersion), result.ProvenancePath)
}

func (w *upgradeProgressWriter) Fail() {
	if w == nil || w.out == nil || !w.interactive {
		return
	}
	w.closeBar()
	if w.activeKey != "" {
		w.checklist.Fail(w.activeKey)
	}
	_, _ = fmt.Fprintln(w.out)
}

func upgradeStageLabel(stage string) string {
	switch stage {
	case "resolve":
		return "Resolve"
	case "download":
		return "Download"
	case "verify":
		return "Verify"
	case "stop":
		return "Stop"
	case "swap":
		return "Swap"
	case "restart":
		return "Restart"
	case "health-check":
		return "Health check"
	case "rollback":
		return "Rollback"
	default:
		return stage
	}
}

func formatUpgradeSummaryIdentity(ref string, version string) string {
	trimmedRef := strings.TrimSpace(ref)
	trimmedVersion := strings.TrimSpace(version)
	if trimmedRef != "" && trimmedVersion != "" {
		return fmt.Sprintf("%s (%s)", trimmedRef, trimmedVersion)
	}
	return firstNonEmpty(trimmedRef, trimmedVersion, "unknown version")
}

func maxDuration(a time.Duration, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func parseBoolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseDurationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseIntEnv(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func resolveSetupMode(rawMode string, interactive bool, reader *bufio.Reader, stdout io.Writer) (string, bool, error) {
	mode := strings.TrimSpace(rawMode)
	if mode != "" {
		switch mode {
		case "daemon-sqlite", "binary-only":
			return mode, false, nil
		default:
			return "", false, fmt.Errorf("unsupported setup mode %q", mode)
		}
	}

	if !interactive {
		return "", false, errors.New("setup mode is required without a TTY; rerun with --mode binary-only or --mode daemon-sqlite")
	}

	if stdout != nil {
		_, _ = fmt.Fprintln(stdout, "Select setup mode:")
		_, _ = fmt.Fprintln(stdout, "  1) binary-only")
		_, _ = fmt.Fprintln(stdout, "  2) daemon-sqlite")
		_, _ = fmt.Fprint(stdout, "Choice: ")
	}

	selection, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", false, fmt.Errorf("read setup mode selection: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(selection)) {
	case "1", "binary-only":
		return "binary-only", true, nil
	case "2", "daemon-sqlite":
		return "daemon-sqlite", true, nil
	default:
		return "", false, fmt.Errorf("unsupported setup selection %q", strings.TrimSpace(selection))
	}
}

func promptSetupValue(reader *bufio.Reader, stdout io.Writer, label string, defaultValue string) (string, error) {
	if stdout != nil {
		if strings.TrimSpace(defaultValue) != "" {
			_, _ = fmt.Fprintf(stdout, "%s [%s]: ", label, defaultValue)
		} else {
			_, _ = fmt.Fprintf(stdout, "%s: ", label)
		}
	}

	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read %s: %w", strings.ToLower(label), err)
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return strings.TrimSpace(defaultValue), nil
	}

	return trimmed, nil
}

func promptSetupBool(reader *bufio.Reader, stdout io.Writer, label string, defaultValue bool) (bool, error) {
	defaultHint := "y/N"
	if defaultValue {
		defaultHint = "Y/n"
	}
	if stdout != nil {
		_, _ = fmt.Fprintf(stdout, "%s [%s]: ", label, defaultHint)
	}

	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read %s: %w", strings.ToLower(label), err)
	}

	trimmed := strings.ToLower(strings.TrimSpace(value))
	switch trimmed {
	case "":
		return defaultValue, nil
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, fmt.Errorf("unsupported %s selection %q", strings.ToLower(label), strings.TrimSpace(value))
	}
}

func promptSetupRuntimeTLSMode(reader *bufio.Reader, stdout io.Writer, defaultMode string) (string, error) {
	if stdout != nil {
		_, _ = fmt.Fprintln(stdout, "Select runtime TLS mode:")
		_, _ = fmt.Fprintln(stdout, "  1) local HTTP test")
		_, _ = fmt.Fprintln(stdout, "  2) reverse proxy terminates TLS and forwards HTTP internally")
		_, _ = fmt.Fprintln(stdout, "  3) regixtry serves TLS directly")
		if label := runtimeTLSModeLabel(defaultMode); label != "" {
			_, _ = fmt.Fprintf(stdout, "Choice [%s]: ", label)
		} else {
			_, _ = fmt.Fprint(stdout, "Choice: ")
		}
	}

	selection, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read runtime TLS mode selection: %w", err)
	}

	trimmed := strings.ToLower(strings.TrimSpace(selection))
	if trimmed == "" {
		trimmed = strings.TrimSpace(defaultMode)
	}

	switch trimmed {
	case "1", installlinux.RuntimeTLSModeLocalHTTP:
		return installlinux.RuntimeTLSModeLocalHTTP, nil
	case "2", installlinux.RuntimeTLSModeReverseProxy:
		return installlinux.RuntimeTLSModeReverseProxy, nil
	case "3", installlinux.RuntimeTLSModeDirectTLS:
		return installlinux.RuntimeTLSModeDirectTLS, nil
	default:
		return "", fmt.Errorf("unsupported runtime TLS mode selection %q", strings.TrimSpace(selection))
	}
}

func defaultSetupRuntimeTLSMode(cfg BootstrapConfig) string {
	mode, _, _, _, err := installlinux.ResolveRuntimeTLSMode(cfg.RuntimeTLSMode, firstNonEmpty(cfg.PublicURL, defaultSetupPublicURL), cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return installlinux.RuntimeTLSModeLocalHTTP
	}
	return mode
}

func defaultSetupPublicURLForMode(addr string, runtimeTLSMode string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return defaultSetupPublicURL
	}
	host = strings.TrimSpace(host)
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	scheme := "http"
	if strings.TrimSpace(runtimeTLSMode) != installlinux.RuntimeTLSModeLocalHTTP {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: net.JoinHostPort(host, port)}).String()
}

func runtimeTLSModeLabel(mode string) string {
	switch strings.TrimSpace(mode) {
	case installlinux.RuntimeTLSModeLocalHTTP:
		return "1"
	case installlinux.RuntimeTLSModeReverseProxy:
		return "2"
	case installlinux.RuntimeTLSModeDirectTLS:
		return "3"
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func printBinaryOnlyGuidance(stdout io.Writer) error {
	if stdout == nil {
		return nil
	}

	_, err := fmt.Fprintln(stdout, "Binary placement is complete, but setup is not yet complete.")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "To finish phase-1 setup on a supported Linux + systemd host, run: %s\n", formatSetupCommand(BootstrapConfig{Mode: "daemon-sqlite", PublicURL: "<url>"}, true))
	return err
}

func lifecycleExecutablePath() string {
	raw := strings.TrimSpace(resolveCurrentExecutable())
	if raw == "" {
		return "regixtry"
	}
	if filepath.IsAbs(raw) {
		return raw
	}
	abs, err := filepath.Abs(raw)
	if err == nil {
		return abs
	}
	return raw
}

func formatSetupCommand(cfg BootstrapConfig, placeholderOnly bool) string {
	args := []string{"setup", "--mode", "daemon-sqlite"}
	publicURL := strings.TrimSpace(cfg.PublicURL)
	if placeholderOnly {
		publicURL = "<url>"
	}
	if publicURL != "" {
		args = append(args, "--public-url", publicURL)
	}
	if !placeholderOnly {
		if strings.TrimSpace(cfg.RuntimeTLSMode) != "" {
			args = append(args, "--runtime-tls-mode", cfg.RuntimeTLSMode)
		}
		if strings.TrimSpace(cfg.AuthPostgresDSN) != "" {
			args = append(args, "--auth-postgres-dsn", cfg.AuthPostgresDSN)
		}
		args = append(args,
			"--addr", cfg.Addr,
			"--storage-root", cfg.StorageRoot,
			"--state-path", cfg.StatePath,
			"--unit-path", cfg.UnitPath,
			"--service", cfg.ServiceName,
		)
		if strings.TrimSpace(cfg.TLSCertFile) != "" {
			args = append(args, "--tls-cert-file", cfg.TLSCertFile)
		}
		if strings.TrimSpace(cfg.TLSKeyFile) != "" {
			args = append(args, "--tls-key-file", cfg.TLSKeyFile)
		}
		if cfg.NoStart {
			args = append(args, "--no-start")
		}
	}
	return formatPrivilegedLifecycleCommand(args...)
}

func formatUninstallCommand(cfg uninstallConfig) string {
	return formatPrivilegedLifecycleCommand("uninstall", "--state-path", cfg.StatePath)
}

func formatUpgradeCommand(cfg upgradeConfig) string {
	args := []string{"upgrade", "--state-path", cfg.StatePath}
	if cfg.Ref != "" {
		args = append(args, "--ref", cfg.Ref)
	}
	if cfg.AssumeYes {
		args = append(args, "--yes")
	}
	return formatPrivilegedLifecycleCommand(args...)
}

func formatPrivilegedLifecycleCommand(args ...string) string {
	parts := make([]string, 0, len(args)+2)
	if currentEUID() != 0 {
		parts = append(parts, "sudo")
	}
	parts = append(parts, lifecycleExecutablePath())
	parts = append(parts, args...)
	for i, part := range parts {
		parts[i] = shellQuote(part)
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\n\"'\\$`!&|;()<>*?[]{}") {
		return strconv.Quote(value)
	}
	return value
}

func upgradeLifecyclePermissionError(err error, action string, rerunCommand string) error {
	if err == nil || !isPermissionDenied(err) {
		return err
	}
	return fmt.Errorf("%s hit a permission-denied failure on privileged lifecycle paths: %w\nRerun with:\n  %s", action, err, rerunCommand)
}

func isPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "permission denied") || strings.Contains(message, "operation not permitted")
}

func rollbackSetupFailure(ctx context.Context, runner bootstrapRunner, cfg BootstrapConfig, runErr error) error {
	rollbackErr := runner.Rollback(ctx, BootstrapConfig{StatePath: cfg.StatePath})
	if rollbackErr != nil {
		return errors.Join(runErr, fmt.Errorf("rollback failed: %w", rollbackErr))
	}
	return runErr
}

func readSecretFromReader(reader io.Reader) (string, error) {
	if reader == nil {
		return "", errors.New("bootstrap admin password stdin reader is required")
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("read bootstrap admin password from stdin: %w", err)
	}
	secret := strings.TrimSpace(string(body))
	if secret == "" {
		return "", errors.New("bootstrap admin password is required")
	}
	return secret, nil
}

func serve(ctx context.Context, listener net.Listener, cfg serveConfig, stdout io.Writer) error {
	handler, cleanup, err := newHandler(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	server := newHTTPServer(cfg, handler)
	errCh := make(chan error, 1)
	go func() {
		err := serveWithRuntimeMode(server, listener, cfg)
		if err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	if stdout != nil {
		fmt.Fprintf(stdout, "regixtry serving on %s\n", listener.Addr().String())
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), boundedDuration(cfg.ShutdownTimeout, defaultShutdownTimeout))
		defer cancel()
		shutdownErr := server.Shutdown(shutdownCtx)
		if errors.Is(shutdownErr, context.DeadlineExceeded) {
			shutdownErr = nil
			if closeErr := server.Close(); closeErr != nil && !errors.Is(closeErr, stdhttp.ErrServerClosed) {
				return closeErr
			}
		}
		if shutdownErr != nil {
			return shutdownErr
		}
		return <-errCh
	case err := <-errCh:
		return err
	}
}

func newHTTPServer(cfg serveConfig, handler stdhttp.Handler) *stdhttp.Server {
	return &stdhttp.Server{
		Handler:           handler,
		ReadHeaderTimeout: boundedDuration(cfg.ReadHeaderTimeout, defaultReadHeaderTimeout),
		ReadTimeout:       boundedDuration(cfg.ReadTimeout, defaultReadTimeout),
		WriteTimeout:      boundedDuration(cfg.WriteTimeout, defaultWriteTimeout),
		IdleTimeout:       boundedDuration(cfg.IdleTimeout, defaultIdleTimeout),
	}
}

func serveWithRuntimeMode(server *stdhttp.Server, listener net.Listener, cfg serveConfig) error {
	if strings.TrimSpace(cfg.TLSCertFile) != "" && strings.TrimSpace(cfg.TLSKeyFile) != "" {
		return server.ServeTLS(listener, cfg.TLSCertFile, cfg.TLSKeyFile)
	}
	return server.Serve(listener)
}

func boundedDuration(value time.Duration, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func newHandler(cfg serveConfig) (stdhttp.Handler, func(), error) {
	var authStore ports.AuthStore
	var authService ports.AuthService
	bearerRealm := cfg.Realm
	if trimmed := strings.TrimSpace(cfg.AuthTokenRealmURL); trimmed != "" {
		bearerRealm = trimmed
	}

	accessController := ports.NewConfigurableAccessController(ports.AccessConfig{
		AllowAnonymousPull: cfg.AllowAnonymousPull,
		AllowAnonymousPush: cfg.AllowAnonymousPush,
		Realm:              bearerRealm,
		Service:            cfg.ServiceName,
	})

	if cfg.AuthPostgresDSN != "" {
		var err error
		authStore, err = openAuthStore(cfg.AuthPostgresDSN)
		if err != nil {
			return nil, nil, err
		}

		authService = appauth.NewService(authStore)
		err = authService.EnsureBootstrapAdmin(context.Background())
		if err != nil {
			_ = authStore.Close()
			return nil, nil, err
		}

		accessController = ports.NewPrincipalAccessController(ports.Challenge{Realm: bearerRealm, Service: cfg.ServiceName})
	}

	if err := os.MkdirAll(cfg.StorageRoot, 0o755); err != nil {
		return nil, nil, err
	}

	blobStore, err := fsblob.New(filepath.Join(cfg.StorageRoot, "content"))
	if err != nil {
		return nil, nil, err
	}

	metadataStore, err := metadata.New(cfg.DatabasePath)
	if err != nil {
		return nil, nil, err
	}

	service := appregixtry.NewService(
		blobStore,
		metadataStore,
		accessController,
		ports.NewSingleTenantResolver(cfg.Tenant),
		ports.NewInlineJobRunner(),
	)
	service.SetScanHost(cfg.PublicURL)
	service.SetScanRunner(trivyinfra.New(trivyinfra.RunnerConfig{}))
	service.SetFeatureRuntimeManager("trivy", newFeatureRuntimeManager("trivy", appregixtry.FeatureRuntimeManagerConfig{StorageRoot: cfg.StorageRoot, Store: metadataStore, ScanRunner: trivyinfra.New(trivyinfra.RunnerConfig{})}))
	service.SetSecretScanRunner(gitleaksinfra.New(gitleaksinfra.RunnerConfig{Blobs: blobStore}))
	service.SetFeatureRuntimeManager("gitleaks", newFeatureRuntimeManager("gitleaks", appregixtry.FeatureRuntimeManagerConfig{StorageRoot: cfg.StorageRoot, Store: metadataStore}))
	service.SetDeleteEnabled(cfg.DeleteEnabled)
	service.SetGCDeleteEnabled(cfg.GCDeleteEnabled)
	trivyMaxConcurrency := cfg.TrivyMaxConcurrency
	if trivyMaxConcurrency <= 0 {
		trivyMaxConcurrency = 1
	}
	trivyTimeout := cfg.TrivyTimeout
	if trivyTimeout <= 0 {
		trivyTimeout = 15 * time.Minute
	}
	trivyInterval := cfg.TrivyInterval
	if trivyInterval <= 0 {
		trivyInterval = 24 * time.Hour
	}
	settings, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{
		Enabled:         cfg.TrivyEnabled,
		ScheduleEnabled: cfg.TrivyScheduleEnabled,
		Interval:        trivyInterval,
		Timeout:         trivyTimeout,
		MaxConcurrency:  trivyMaxConcurrency,
	})
	if err != nil {
		_ = metadataStore.Close()
		if authStore != nil {
			_ = authStore.Close()
		}
		return nil, nil, err
	}
	schedulerCtx, cancelScheduler := context.WithCancel(context.Background())
	scheduler := appscanning.NewScheduler(metadataStore, service, cfg.Tenant, cfg.ServiceName, settings.Interval, maxDuration(settings.Interval/2, time.Second))
	var schedulerDone sync.WaitGroup
	schedulerDone.Add(1)
	go func() {
		defer schedulerDone.Done()
		_ = scheduler.Run(schedulerCtx)
	}()

	return regixtryhttp.NewRouter(service, authService), func() {
		// Wait for the scheduler goroutine to actually stop before closing the
		// stores it reads/writes: cancelling schedulerCtx only takes effect
		// between ticks (scheduler.Run checks ctx.Done() after each tick
		// returns), so an in-flight tick can still touch storageRoot/the
		// database after this cleanup would otherwise have returned -- racing
		// callers such as t.TempDir()'s RemoveAll cleanup in tests.
		cancelScheduler()
		schedulerDone.Wait()
		_ = metadataStore.Close()
		if authStore != nil {
			_ = authStore.Close()
		}
	}, nil
}

func runTUI(cfg tuiConfig, stdin io.Reader, stdout io.Writer) error {
	if err := os.MkdirAll(cfg.StorageRoot, 0o755); err != nil {
		return err
	}

	blobStore, err := fsblob.New(filepath.Join(cfg.StorageRoot, "content"))
	if err != nil {
		return err
	}

	metadataStore, err := metadata.New(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer metadataStore.Close()

	var (
		authStore ports.AuthStore
		modelOpts []tui.Option
	)
	if cfg.AuthPostgresDSN != "" {
		authStore, err = openAuthStore(cfg.AuthPostgresDSN)
		if err != nil {
			return err
		}
		defer authStore.Close()

		authService := appauth.NewService(authStore)
		if err := authService.EnsureBootstrapAdmin(context.Background()); err != nil {
			return err
		}
	}
	if cfg.APIBaseURL != "" {
		adminClient, err := tui.NewHTTPAdminClient(cfg.APIBaseURL, nil)
		if err != nil {
			return err
		}
		modelOpts = append(modelOpts, tui.WithAdminClient(adminClient))
	}
	if tuiRequiresStartupLogin(cfg) {
		modelOpts = append(modelOpts, tui.WithStartupLogin())
	}

	service := appregixtry.NewService(
		blobStore,
		metadataStore,
		localOperatorAccessController{},
		ports.NewSingleTenantResolver(cfg.Tenant),
		ports.NewInlineJobRunner(),
	)
	service.SetFeatureRuntimeManager("trivy", newFeatureRuntimeManager("trivy", appregixtry.FeatureRuntimeManagerConfig{StorageRoot: cfg.StorageRoot, Store: metadataStore, ScanRunner: trivyinfra.New(trivyinfra.RunnerConfig{})}))
	service.SetSecretScanRunner(gitleaksinfra.New(gitleaksinfra.RunnerConfig{Blobs: blobStore}))
	service.SetFeatureRuntimeManager("gitleaks", newFeatureRuntimeManager("gitleaks", appregixtry.FeatureRuntimeManagerConfig{StorageRoot: cfg.StorageRoot, Store: metadataStore}))
	service.SetDeleteEnabled(cfg.DeleteEnabled)
	service.SetGCDeleteEnabled(cfg.GCDeleteEnabled)
	if authStore != nil {
		service.SetUsernameResolver(authStoreUsernameResolver{store: authStore})
	}

	if cfg.Snapshot {
		model := tui.NewModel(service, modelOpts...)
		updated := tea.Model(model)
		if cmd := model.Init(); cmd != nil {
			updated, _ = model.Update(cmd())
		}
		if stdout != nil {
			_, _ = io.WriteString(stdout, updated.(tui.Model).View())
		}
		return nil
	}

	program := tea.NewProgram(
		tui.NewModel(service, modelOpts...),
		tea.WithInput(stdin),
		tea.WithOutput(stdout),
		tea.WithAltScreen(),
	)

	_, err = program.Run()
	return err
}

func tuiRequiresStartupLogin(cfg tuiConfig) bool {
	return strings.TrimSpace(cfg.AuthPostgresDSN) != "" && strings.TrimSpace(cfg.APIBaseURL) != ""
}

func runBootstrapAdmin(ctx context.Context, cfg bootstrapAdminConfig, stdout io.Writer) error {
	_, err := bootstrapAdmin(ctx, cfg, stdout)
	if err != nil {
		return err
	}

	return nil
}

type setupAuthBootstrapOutcome struct {
	Enabled              bool
	Username             string
	Created              bool
	AlreadyConfigured    bool
	RequiresExistingAuth bool
}

func validateSetupAuthConfig(cfg setupAuthConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if strings.TrimSpace(cfg.AuthPostgresDSN) == "" {
		return errors.New("auth Postgres DSN is required when auth is enabled")
	}
	if strings.TrimSpace(cfg.AdminUsername) == "" {
		return errors.New("admin username is required when auth is enabled")
	}
	if strings.TrimSpace(cfg.AdminPassword) == "" {
		return errors.New("admin password is required when auth is enabled")
	}
	return nil
}

func bootstrapSetupAuth(ctx context.Context, cfg setupAuthConfig) (setupAuthBootstrapOutcome, error) {
	if !cfg.Enabled {
		return setupAuthBootstrapOutcome{}, nil
	}

	result, err := bootstrapAdmin(ctx, bootstrapAdminConfig{
		AuthPostgresDSN: strings.TrimSpace(cfg.AuthPostgresDSN),
		Username:        strings.TrimSpace(cfg.AdminUsername),
		Password:        strings.TrimSpace(cfg.AdminPassword),
	}, nil)
	if err == nil {
		return setupAuthBootstrapOutcome{
			Enabled:           true,
			Username:          result.User.Username,
			Created:           result.Created,
			AlreadyConfigured: !result.Created && !result.PasswordRotated,
		}, nil
	}
	if domainauth.IsCode(err, domainauth.ErrorCodeConflict) {
		return setupAuthBootstrapOutcome{
			Enabled:              true,
			Username:             strings.TrimSpace(cfg.AdminUsername),
			RequiresExistingAuth: true,
		}, nil
	}
	return setupAuthBootstrapOutcome{}, err
}

func printSetupAuthGuidance(stdout io.Writer, cfg setupConfig, outcome setupAuthBootstrapOutcome) {
	if stdout == nil || !outcome.Enabled {
		return
	}

	registryHost := dockerLoginHost(cfg.PublicURL)
	if registryHost == "" {
		return
	}

	switch {
	case outcome.Created:
		_, _ = fmt.Fprintf(stdout, "Auth bootstrap complete: created global admin %q.\n", outcome.Username)
		_, _ = fmt.Fprintf(stdout, "Next: printf '%%s\\n' '<admin-password>' | docker login %s -u %s --password-stdin\n", registryHost, shellQuote(outcome.Username))
	case outcome.RequiresExistingAuth:
		_, _ = fmt.Fprintln(stdout, "Auth bootstrap skipped: a global admin already exists in the auth store.")
		_, _ = fmt.Fprintf(stdout, "Next: docker login %s with the existing global admin credentials.\n", registryHost)
	default:
		_, _ = fmt.Fprintf(stdout, "Auth bootstrap confirmed: global admin %q is already configured.\n", outcome.Username)
		_, _ = fmt.Fprintf(stdout, "Next: printf '%%s\\n' '<admin-password>' | docker login %s -u %s --password-stdin\n", registryHost, shellQuote(outcome.Username))
	}

	if strings.HasPrefix(strings.TrimSpace(cfg.PublicURL), "http://") {
		_, _ = fmt.Fprintln(stdout, "Docker may require this registry to be configured as insecure before login, push, or pull over HTTP.")
	}
}

func dockerLoginHost(publicURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Host)
}

func bootstrapAdmin(ctx context.Context, cfg bootstrapAdminConfig, stdout io.Writer) (ports.BootstrapAdminResult, error) {
	authStore, err := openAuthStore(cfg.AuthPostgresDSN)
	if err != nil {
		return ports.BootstrapAdminResult{}, err
	}
	defer authStore.Close()

	authService := appauth.NewService(authStore)
	result, err := authService.BootstrapAdmin(ctx, ports.BootstrapAdminInput{
		Username:       cfg.Username,
		Password:       cfg.Password,
		RotatePassword: cfg.RotatePassword,
	})
	if err != nil {
		return ports.BootstrapAdminResult{}, err
	}

	if stdout != nil {
		switch {
		case result.Created:
			_, _ = fmt.Fprintf(stdout, "bootstrapped global admin %q\n", result.User.Username)
		case result.PasswordRotated:
			_, _ = fmt.Fprintf(stdout, "rotated global admin password for %q\n", result.User.Username)
		default:
			_, _ = fmt.Fprintf(stdout, "global admin %q already configured\n", result.User.Username)
		}
	}

	return result, nil
}

type localOperatorAccessController struct{}

func (localOperatorAccessController) Authorize(context.Context, ports.Action) error {
	return nil
}

func (localOperatorAccessController) Challenge(ports.Action) ports.Challenge {
	return ports.Challenge{Scheme: "Bearer", Realm: "regixtry", Service: "regixtry"}
}

// authStoreUsernameResolver adapts ports.AuthStore to
// appregixtry.UsernameResolver (console-tags-pushed-by change), translating
// AuthStore.GetUserByID's typed "not found" error into that interface's own
// contract -- ("", nil), not an error -- so a deleted/unknown user never
// fails the whole TagDetails call. Any other error (a genuine auth store
// failure) propagates unchanged.
type authStoreUsernameResolver struct {
	store ports.AuthStore
}

func (r authStoreUsernameResolver) ResolveUsername(ctx context.Context, userID string) (string, error) {
	user, err := r.store.GetUserByID(ctx, userID)
	if err != nil {
		if domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
			return "", nil
		}
		return "", err
	}
	return user.Username, nil
}
