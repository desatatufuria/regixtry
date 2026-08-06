package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	appauth "registry/internal/app/auth"
	appregistry "registry/internal/app/registry"
	authpostgres "registry/internal/infra/auth/postgres"
	metadata "registry/internal/infra/metadata/sqlite"
	"registry/internal/infra/storage/fsblob"
	"registry/internal/ports"
	registryhttp "registry/internal/protocol/http"
	"registry/internal/tui"
)

var openAuthStore = func(dsn string) (ports.AuthStore, error) {
	return authpostgres.New(dsn)
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("expected subcommand: serve, tui, or bootstrap-admin")
	}

	switch args[0] {
	case "serve":
		cfg, err := parseServeConfig(args[1:])
		if err != nil {
			return err
		}

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
		cfg, err := parseBootstrapAdminConfig(args[1:])
		if err != nil {
			return err
		}
		return runBootstrapAdmin(ctx, cfg, stdout)
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
}

type serveConfig struct {
	Address            string
	StorageRoot        string
	DatabasePath       string
	Tenant             string
	AllowAnonymousPull bool
	AllowAnonymousPush bool
	AuthPostgresDSN    string
	AuthTokenRealmURL  string
	Realm              string
	ServiceName        string
}

type bootstrapAdminConfig struct {
	AuthPostgresDSN string
	Username        string
	Password        string
	RotatePassword  bool
}

func parseServeConfig(args []string) (serveConfig, error) {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var cfg serveConfig
	flags.StringVar(&cfg.Address, "addr", "127.0.0.1:5000", "address to listen on")
	flags.StringVar(&cfg.StorageRoot, "storage-root", filepath.Join(".", "data"), "root directory for registry storage")
	flags.StringVar(&cfg.DatabasePath, "db", "", "path to the SQLite metadata database")
	flags.StringVar(&cfg.Tenant, "tenant", ports.DefaultTenant, "tenant identifier")
	flags.BoolVar(&cfg.AllowAnonymousPull, "allow-anonymous-pull", false, "allow unauthenticated manifest/blob reads")
	flags.BoolVar(&cfg.AllowAnonymousPush, "allow-anonymous-push", false, "allow unauthenticated blob/manifest writes")
	flags.StringVar(&cfg.AuthPostgresDSN, "auth-postgres-dsn", os.Getenv("REGISTRY_AUTH_POSTGRES_DSN"), "Postgres DSN for auth state")
	flags.StringVar(&cfg.AuthTokenRealmURL, "auth-token-realm", os.Getenv("REGISTRY_AUTH_TOKEN_REALM_URL"), "Bearer token realm URL advertised to registry clients")
	flags.StringVar(&cfg.Realm, "realm", "registry", "auth challenge realm")
	flags.StringVar(&cfg.ServiceName, "service", "registry", "auth challenge service name")

	if err := flags.Parse(args); err != nil {
		return serveConfig{}, err
	}

	if cfg.DatabasePath == "" {
		cfg.DatabasePath = filepath.Join(cfg.StorageRoot, "metadata.db")
	}

	return cfg, nil
}

func parseTUIConfig(args []string) (tuiConfig, error) {
	flags := flag.NewFlagSet("tui", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var cfg tuiConfig
	flags.StringVar(&cfg.StorageRoot, "storage-root", filepath.Join(".", "data"), "root directory for registry storage")
	flags.StringVar(&cfg.DatabasePath, "db", "", "path to the SQLite metadata database")
	flags.StringVar(&cfg.Tenant, "tenant", ports.DefaultTenant, "tenant identifier")
	flags.StringVar(&cfg.AuthPostgresDSN, "auth-postgres-dsn", os.Getenv("REGISTRY_AUTH_POSTGRES_DSN"), "Postgres DSN for auth state")
	flags.StringVar(&cfg.APIBaseURL, "api-base-url", os.Getenv("REGISTRY_API_BASE_URL"), "base URL for authenticated admin API")
	flags.BoolVar(&cfg.Snapshot, "snapshot", false, "render the first inspection view and exit")

	if err := flags.Parse(args); err != nil {
		return tuiConfig{}, err
	}

	if cfg.DatabasePath == "" {
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

func parseBootstrapAdminConfig(args []string) (bootstrapAdminConfig, error) {
	flags := flag.NewFlagSet("bootstrap-admin", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var cfg bootstrapAdminConfig
	flags.StringVar(&cfg.AuthPostgresDSN, "auth-postgres-dsn", os.Getenv("REGISTRY_AUTH_POSTGRES_DSN"), "Postgres DSN for auth state")
	flags.StringVar(&cfg.Username, "username", "admin", "username for the global admin bootstrap account")
	flags.StringVar(&cfg.Password, "password", "", "password for the global admin bootstrap account")
	flags.BoolVar(&cfg.RotatePassword, "rotate-password", false, "rotate the password when the admin already exists")

	if err := flags.Parse(args); err != nil {
		return bootstrapAdminConfig{}, err
	}

	if cfg.AuthPostgresDSN == "" {
		return bootstrapAdminConfig{}, errors.New("auth Postgres DSN is required")
	}

	if cfg.Password == "" {
		return bootstrapAdminConfig{}, errors.New("bootstrap admin password is required")
	}

	return cfg, nil
}

func serve(ctx context.Context, listener net.Listener, cfg serveConfig, stdout io.Writer) error {
	handler, cleanup, err := newHandler(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	server := &stdhttp.Server{Handler: handler}
	errCh := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	if stdout != nil {
		fmt.Fprintf(stdout, "registry serving on %s\n", listener.Addr().String())
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return <-errCh
	case err := <-errCh:
		return err
	}
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

	service := appregistry.NewService(
		blobStore,
		metadataStore,
		accessController,
		ports.NewSingleTenantResolver(cfg.Tenant),
		ports.NewInlineJobRunner(),
	)

	return registryhttp.NewRouter(service, authService), func() {
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

	service := appregistry.NewService(
		blobStore,
		metadataStore,
		localOperatorAccessController{},
		ports.NewSingleTenantResolver(cfg.Tenant),
		ports.NewInlineJobRunner(),
	)

	if cfg.Snapshot {
		model := tui.NewModel(service, modelOpts...)
		msg := model.Init()()
		updated, _ := model.Update(msg)
		if stdout != nil {
			_, _ = io.WriteString(stdout, updated.(tui.Model).View())
		}
		return nil
	}

	program := tea.NewProgram(
		tui.NewModel(service, modelOpts...),
		tea.WithInput(stdin),
		tea.WithOutput(stdout),
	)

	_, err = program.Run()
	return err
}

func runBootstrapAdmin(ctx context.Context, cfg bootstrapAdminConfig, stdout io.Writer) error {
	authStore, err := openAuthStore(cfg.AuthPostgresDSN)
	if err != nil {
		return err
	}
	defer authStore.Close()

	authService := appauth.NewService(authStore)
	result, err := authService.BootstrapAdmin(ctx, ports.BootstrapAdminInput{
		Username:       cfg.Username,
		Password:       cfg.Password,
		RotatePassword: cfg.RotatePassword,
	})
	if err != nil {
		return err
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

	return nil
}

type localOperatorAccessController struct{}

func (localOperatorAccessController) Authorize(context.Context, ports.Action) error {
	return nil
}

func (localOperatorAccessController) Challenge(ports.Action) ports.Challenge {
	return ports.Challenge{Scheme: "Bearer", Realm: "registry", Service: "registry"}
}
