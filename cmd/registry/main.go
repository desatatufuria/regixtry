package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	appregistry "registry/internal/app/registry"
	metadata "registry/internal/infra/metadata/sqlite"
	"registry/internal/infra/storage/fsblob"
	"registry/internal/ports"
	registryhttp "registry/internal/protocol/http"
	"registry/internal/tui"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("expected subcommand: serve or tui")
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
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

type tuiConfig struct {
	StorageRoot  string
	DatabasePath string
	Tenant       string
	Snapshot     bool
}

type serveConfig struct {
	Address            string
	StorageRoot        string
	DatabasePath       string
	Tenant             string
	AllowAnonymousPull bool
	AllowAnonymousPush bool
	Realm              string
	ServiceName        string
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
	flags.BoolVar(&cfg.Snapshot, "snapshot", false, "render the first inspection view and exit")

	if err := flags.Parse(args); err != nil {
		return tuiConfig{}, err
	}

	if cfg.DatabasePath == "" {
		cfg.DatabasePath = filepath.Join(cfg.StorageRoot, "metadata.db")
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
		ports.NewConfigurableAccessController(ports.AccessConfig{
			AllowAnonymousPull: cfg.AllowAnonymousPull,
			AllowAnonymousPush: cfg.AllowAnonymousPush,
			Realm:              cfg.Realm,
			Service:            cfg.ServiceName,
		}),
		ports.NewSingleTenantResolver(cfg.Tenant),
		ports.NewInlineJobRunner(),
	)

	return registryhttp.NewRouter(service), func() {
		_ = metadataStore.Close()
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

	service := appregistry.NewService(
		blobStore,
		metadataStore,
		localOperatorAccessController{},
		ports.NewSingleTenantResolver(cfg.Tenant),
		ports.NewInlineJobRunner(),
	)

	if cfg.Snapshot {
		model := tui.NewModel(service)
		msg := model.Init()()
		updated, _ := model.Update(msg)
		if stdout != nil {
			_, _ = io.WriteString(stdout, updated.(tui.Model).View())
		}
		return nil
	}

	program := tea.NewProgram(
		tui.NewModel(service),
		tea.WithInput(stdin),
		tea.WithOutput(stdout),
	)

	_, err = program.Run()
	return err
}

type localOperatorAccessController struct{}

func (localOperatorAccessController) Authorize(context.Context, ports.Action) error {
	return nil
}

func (localOperatorAccessController) Challenge() ports.Challenge {
	return ports.Challenge{Scheme: "Bearer", Realm: "registry", Service: "registry"}
}
