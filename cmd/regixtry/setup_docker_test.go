package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"regixtry/internal/infra/install/compose"
	installlinux "regixtry/internal/infra/install/linux"
)

// fakeComposeRunner is the docker setup mode's fake composeRunner: an
// ordered call-log recorder (tasks.md 8.6) that never touches a real Docker
// daemon, mirroring stubBootstrapRunner's shape for the systemd path.
type fakeComposeRunner struct {
	calls []string

	preflightErr      error
	writeProjectErr   error
	writeProjectFunc  func(compose.ProjectConfig) (compose.Project, error)
	startDatabaseErr  error
	bootstrapAdminErr error
	startRegistryErr  error
	waitReachableErr  error
	saveProvenanceErr error
	downErr           error

	writeProjectArg        compose.ProjectConfig
	bootstrapAdminUsername string
	bootstrapAdminPassword string
	downCalls              int
}

func (f *fakeComposeRunner) Preflight(ctx context.Context) error {
	f.calls = append(f.calls, "Preflight")
	return f.preflightErr
}

func (f *fakeComposeRunner) WriteProject(cfg compose.ProjectConfig) (compose.Project, error) {
	f.calls = append(f.calls, "WriteProject")
	f.writeProjectArg = cfg
	if f.writeProjectErr != nil {
		return compose.Project{}, f.writeProjectErr
	}
	if f.writeProjectFunc != nil {
		return f.writeProjectFunc(cfg)
	}
	return compose.Project{
		Name:            cfg.Name,
		Dir:             cfg.Dir,
		ComposeFilePath: filepath.Join(cfg.Dir, "docker-compose.yml"),
		EnvFilePath:     filepath.Join(cfg.Dir, "regixtry.env"),
		Image:           cfg.Image,
		BundledPostgres: strings.TrimSpace(cfg.ExternalPostgresDSN) == "",
		PublicURL:       cfg.PublicURL,
	}, nil
}

func (f *fakeComposeRunner) StartDatabase(ctx context.Context, p compose.Project) error {
	f.calls = append(f.calls, "StartDatabase")
	return f.startDatabaseErr
}

func (f *fakeComposeRunner) BootstrapAdmin(ctx context.Context, p compose.Project, username, password string) error {
	f.calls = append(f.calls, "BootstrapAdmin")
	f.bootstrapAdminUsername = username
	f.bootstrapAdminPassword = password
	return f.bootstrapAdminErr
}

func (f *fakeComposeRunner) StartRegistry(ctx context.Context, p compose.Project) error {
	f.calls = append(f.calls, "StartRegistry")
	return f.startRegistryErr
}

func (f *fakeComposeRunner) WaitReachable(ctx context.Context, p compose.Project) error {
	f.calls = append(f.calls, "WaitReachable")
	return f.waitReachableErr
}

func (f *fakeComposeRunner) SaveProvenance(p compose.Project) error {
	f.calls = append(f.calls, "SaveProvenance")
	return f.saveProvenanceErr
}

func (f *fakeComposeRunner) Down(ctx context.Context, p compose.Project) error {
	f.calls = append(f.calls, "Down")
	f.downCalls++
	return f.downErr
}

func swapComposeRunner(t *testing.T, runner composeRunner) func() {
	t.Helper()

	previous := newComposeRunner
	newComposeRunner = func() composeRunner {
		return runner
	}

	return func() {
		newComposeRunner = previous
	}
}

// --- 8.1: resolveSetupMode table-driven tests -----------------------------

func TestResolveSetupModeAcceptsDockerModeAndSelection(t *testing.T) {
	t.Run("flag literal docker", func(t *testing.T) {
		mode, selectedInteractively, err := resolveSetupMode("docker", false, nil, nil)
		if err != nil {
			t.Fatalf("resolveSetupMode(docker) error = %v", err)
		}
		if mode != "docker" || selectedInteractively {
			t.Fatalf("resolveSetupMode(docker) = (%q, %v), want (docker, false)", mode, selectedInteractively)
		}
	})

	t.Run("interactive numeric 3", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("3\n"))
		stdout := &bytes.Buffer{}
		mode, selectedInteractively, err := resolveSetupMode("", true, reader, stdout)
		if err != nil {
			t.Fatalf("resolveSetupMode(3) error = %v", err)
		}
		if mode != "docker" || !selectedInteractively {
			t.Fatalf("resolveSetupMode(3) = (%q, %v), want (docker, true)", mode, selectedInteractively)
		}
		if !strings.Contains(stdout.String(), "  3) docker") {
			t.Fatalf("stdout = %q, want docker menu entry", stdout.String())
		}
	})

	t.Run("interactive literal docker", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("docker\n"))
		mode, selectedInteractively, err := resolveSetupMode("", true, reader, io.Discard)
		if err != nil {
			t.Fatalf("resolveSetupMode(docker literal) error = %v", err)
		}
		if mode != "docker" || !selectedInteractively {
			t.Fatalf("resolveSetupMode(docker literal) = (%q, %v), want (docker, true)", mode, selectedInteractively)
		}
	})
}

func TestResolveSetupModeRejectsUnknownMode(t *testing.T) {
	_, _, err := resolveSetupMode("kubernetes", false, nil, nil)
	if err == nil || !strings.Contains(err.Error(), `unsupported setup mode "kubernetes"`) {
		t.Fatalf("resolveSetupMode(kubernetes) error = %v, want unsupported mode error", err)
	}
}

func TestResolveSetupModeNonTTYErrorNamesAllThreeModes(t *testing.T) {
	_, _, err := resolveSetupMode("", false, nil, nil)
	if err == nil {
		t.Fatal("resolveSetupMode(no mode, non-TTY) error = nil, want an error")
	}
	for _, want := range []string{"binary-only", "daemon-sqlite", "docker"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("resolveSetupMode(no mode, non-TTY) error = %q, want it to name %q", err.Error(), want)
		}
	}
}

// TestResolveSetupModeRegressionBinaryOnlyAndDaemonSQLiteUnaffected proves
// resolveSetupMode's existing two modes are byte-for-byte unchanged by the
// docker addition -- the orchestrator's explicit regression-proof
// requirement for this PR's shared dispatch function.
func TestResolveSetupModeRegressionBinaryOnlyAndDaemonSQLiteUnaffected(t *testing.T) {
	t.Run("flag literal binary-only", func(t *testing.T) {
		mode, selectedInteractively, err := resolveSetupMode("binary-only", false, nil, nil)
		if err != nil || mode != "binary-only" || selectedInteractively {
			t.Fatalf("resolveSetupMode(binary-only) = (%q, %v, %v), want (binary-only, false, nil)", mode, selectedInteractively, err)
		}
	})

	t.Run("flag literal daemon-sqlite", func(t *testing.T) {
		mode, selectedInteractively, err := resolveSetupMode("daemon-sqlite", false, nil, nil)
		if err != nil || mode != "daemon-sqlite" || selectedInteractively {
			t.Fatalf("resolveSetupMode(daemon-sqlite) = (%q, %v, %v), want (daemon-sqlite, false, nil)", mode, selectedInteractively, err)
		}
	})

	t.Run("interactive selection unchanged", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("1\n"))
		stdout := &bytes.Buffer{}
		mode, selectedInteractively, err := resolveSetupMode("", true, reader, stdout)
		if err != nil || mode != "binary-only" || !selectedInteractively {
			t.Fatalf("resolveSetupMode(1) = (%q, %v, %v), want (binary-only, true, nil)", mode, selectedInteractively, err)
		}
		if !strings.Contains(stdout.String(), "  1) binary-only") || !strings.Contains(stdout.String(), "  2) daemon-sqlite") {
			t.Fatalf("stdout = %q, want unchanged binary-only/daemon-sqlite menu entries", stdout.String())
		}

		reader2 := bufio.NewReader(strings.NewReader("2\n"))
		mode2, selectedInteractively2, err2 := resolveSetupMode("", true, reader2, io.Discard)
		if err2 != nil || mode2 != "daemon-sqlite" || !selectedInteractively2 {
			t.Fatalf("resolveSetupMode(2) = (%q, %v, %v), want (daemon-sqlite, true, nil)", mode2, selectedInteractively2, err2)
		}
	})

	t.Run("unsupported selection error format unchanged", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("9\n"))
		_, _, err := resolveSetupMode("", true, reader, io.Discard)
		if err == nil || !strings.Contains(err.Error(), `unsupported setup selection "9"`) {
			t.Fatalf("resolveSetupMode(9) error = %v, want unchanged unsupported-selection error text", err)
		}
	})
}

// TestExistingSetupModesEndToEndRegression re-runs runWithIO for
// binary-only/daemon-sqlite end to end (the same paths
// TestRunSetupInteractivePromptSupportsBinaryOnly and
// TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance already cover)
// to make the regression proof explicit for this PR's review, without
// modifying those pre-existing tests.
func TestExistingSetupModesEndToEndRegression(t *testing.T) {
	t.Run("binary-only still prints guidance and never touches composeRunner or bootstrapRunner", func(t *testing.T) {
		restoreTTY := swapInteractiveTTYDetector(t, false)
		defer restoreTTY()

		composeFake := &fakeComposeRunner{}
		restoreCompose := swapComposeRunner(t, composeFake)
		defer restoreCompose()

		stdout := &bytes.Buffer{}
		err := runWithIO(context.Background(), []string{"setup", "-mode", "binary-only"}, strings.NewReader(""), stdout, io.Discard)
		if err != nil {
			t.Fatalf("runWithIO(setup binary-only) error = %v", err)
		}
		if !strings.Contains(stdout.String(), "Binary placement is complete, but setup is not yet complete.") {
			t.Fatalf("stdout = %q, want unchanged binary-only guidance", stdout.String())
		}
		if len(composeFake.calls) != 0 {
			t.Fatalf("composeFake.calls = %v, want binary-only to never call composeRunner", composeFake.calls)
		}
	})

	t.Run("daemon-sqlite still requires public URL and never touches composeRunner", func(t *testing.T) {
		restoreTTY := swapInteractiveTTYDetector(t, false)
		defer restoreTTY()

		composeFake := &fakeComposeRunner{}
		restoreCompose := swapComposeRunner(t, composeFake)
		defer restoreCompose()

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

		err := runWithIO(context.Background(), []string{"setup", "-mode", "daemon-sqlite", "-public-url", "http://127.0.0.1:5000"}, strings.NewReader(""), io.Discard, io.Discard)
		if err != nil {
			t.Fatalf("runWithIO(setup daemon-sqlite) error = %v", err)
		}
		if runner.runCalls != 1 {
			t.Fatalf("runCalls = %d, want 1", runner.runCalls)
		}
		if len(composeFake.calls) != 0 {
			t.Fatalf("composeFake.calls = %v, want daemon-sqlite to never call composeRunner", composeFake.calls)
		}
	})
}

// --- 8.4: promptSetupDockerConfig -----------------------------------------

func TestPromptSetupDockerConfigPromptsBundledVsExternalOnlyWhenDSNAbsent(t *testing.T) {
	t.Run("DSN absent, operator declines external: stays bundled", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("\nn\nadmin\nchange-me-now\n"))
		stdout := &bytes.Buffer{}
		cfg := setupConfig{}
		cfg.Auth.AdminUsername = "admin"

		got, err := promptSetupDockerConfig(reader, stdout, cfg, setupPromptState{}, true)
		if err != nil {
			t.Fatalf("promptSetupDockerConfig() error = %v", err)
		}
		if !strings.Contains(stdout.String(), "Use an external Postgres instance instead of the bundled one") {
			t.Fatalf("stdout = %q, want the bundled-vs-external prompt", stdout.String())
		}
		if strings.TrimSpace(got.Auth.AuthPostgresDSN) != "" {
			t.Fatalf("Auth.AuthPostgresDSN = %q, want empty (bundled)", got.Auth.AuthPostgresDSN)
		}
		if !got.Auth.Enabled {
			t.Fatal("Auth.Enabled = false, want true (docker mode always enables auth)")
		}
	})

	t.Run("DSN absent, operator opts into external: assembles DSN", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("\ny\ndb.example.com\n5432\nregistry\nsecret\ndisable\nadmin\nchange-me-now\n"))
		stdout := &bytes.Buffer{}
		cfg := setupConfig{}

		got, err := promptSetupDockerConfig(reader, stdout, cfg, setupPromptState{}, true)
		if err != nil {
			t.Fatalf("promptSetupDockerConfig() error = %v", err)
		}
		want := "postgres://registry:secret@db.example.com:5432/regixtry_auth?sslmode=disable"
		if got.Auth.AuthPostgresDSN != want {
			t.Fatalf("Auth.AuthPostgresDSN = %q, want %q", got.Auth.AuthPostgresDSN, want)
		}
	})

	t.Run("DSN already provided via flag: prompt is skipped entirely", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("admin\nchange-me-now\n"))
		stdout := &bytes.Buffer{}
		cfg := setupConfig{}
		cfg.Auth.AuthPostgresDSN = "postgres://already:set@host:5432/db?sslmode=disable"
		cfg.PublicURL = "http://127.0.0.1:5000"

		promptState := setupPromptState{authPostgresDSNProvided: true, publicURLProvided: true}
		got, err := promptSetupDockerConfig(reader, stdout, cfg, promptState, true)
		if err != nil {
			t.Fatalf("promptSetupDockerConfig() error = %v", err)
		}
		if strings.Contains(stdout.String(), "Use an external Postgres instance instead of the bundled one") {
			t.Fatalf("stdout = %q, want no bundled-vs-external prompt when DSN already provided", stdout.String())
		}
		if got.Auth.AuthPostgresDSN != cfg.Auth.AuthPostgresDSN {
			t.Fatalf("Auth.AuthPostgresDSN = %q, want unchanged %q", got.Auth.AuthPostgresDSN, cfg.Auth.AuthPostgresDSN)
		}
	})
}

// --- 8.6: orchestration-order test -----------------------------------------

func TestRunSetupDockerOrchestratesComposeRunnerInDesignOrder(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "etc", "regixtry", "bootstrap-state.json")

	fake := &fakeComposeRunner{
		writeProjectFunc: func(cfg compose.ProjectConfig) (compose.Project, error) {
			envPath := filepath.Join(tempDir, "regixtry.env")
			if err := os.WriteFile(envPath, []byte("REGIXTRY_IMAGE=ghcr.io/desatatufuria/regixtry:latest\nREGIXTRY_PORT=5000\nREGIXTRY_POSTGRES_PASSWORD=s3cr3t-generated\nREGIXTRY_AUTH_POSTGRES_DSN=postgres://registry:s3cr3t-generated@postgres:5432/regixtry_auth?sslmode=disable\n"), 0o600); err != nil {
				t.Fatalf("write fake env file: %v", err)
			}
			return compose.Project{
				Name:            cfg.Name,
				Dir:             cfg.Dir,
				ComposeFilePath: filepath.Join(cfg.Dir, "docker-compose.yml"),
				EnvFilePath:     envPath,
				Image:           cfg.Image,
				BundledPostgres: strings.TrimSpace(cfg.ExternalPostgresDSN) == "",
				PublicURL:       cfg.PublicURL,
			}, nil
		},
	}
	restore := swapComposeRunner(t, fake)
	defer restore()

	stdout := &bytes.Buffer{}
	err := runWithIO(
		context.Background(),
		[]string{"setup", "-mode", "docker", "-public-url", "http://127.0.0.1:5000", "-state-path", statePath, "-admin-password", "change-me-now"},
		strings.NewReader(""),
		stdout,
		io.Discard,
	)
	if err != nil {
		t.Fatalf("runWithIO(setup docker) error = %v", err)
	}

	wantOrder := []string{"Preflight", "WriteProject", "StartDatabase", "BootstrapAdmin", "StartRegistry", "WaitReachable", "SaveProvenance"}
	if !reflect.DeepEqual(fake.calls, wantOrder) {
		t.Fatalf("fake.calls = %v, want %v", fake.calls, wantOrder)
	}

	if fake.bootstrapAdminUsername != "admin" {
		t.Fatalf("bootstrapAdminUsername = %q, want %q", fake.bootstrapAdminUsername, "admin")
	}
	if fake.bootstrapAdminPassword != "change-me-now" {
		t.Fatalf("bootstrapAdminPassword = %q, want the admin password", fake.bootstrapAdminPassword)
	}

	if !strings.Contains(fake.writeProjectArg.Dir, filepath.Join("etc", "regixtry", "compose")) {
		t.Fatalf("writeProjectArg.Dir = %q, want it under <state-dir>/compose", fake.writeProjectArg.Dir)
	}
	if strings.TrimSpace(fake.writeProjectArg.ExternalPostgresDSN) != "" {
		t.Fatalf("writeProjectArg.ExternalPostgresDSN = %q, want empty (bundled default)", fake.writeProjectArg.ExternalPostgresDSN)
	}

	if strings.Count(stdout.String(), "s3cr3t-generated") != 1 {
		t.Fatalf("stdout = %q, want the generated password printed exactly once", stdout.String())
	}
	if !strings.Contains(stdout.String(), "docker exec -it <container> regixtry tui -storage-root /var/lib/regixtry") {
		t.Fatalf("stdout = %q, want the docker exec TUI guidance", stdout.String())
	}
	if fake.downCalls != 0 {
		t.Fatalf("downCalls = %d, want 0 on success", fake.downCalls)
	}
}

func TestRunSetupDockerExternalDSNSkipsBundledPasswordDisclosure(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "etc", "regixtry", "bootstrap-state.json")

	fake := &fakeComposeRunner{}
	restore := swapComposeRunner(t, fake)
	defer restore()

	stdout := &bytes.Buffer{}
	err := runWithIO(
		context.Background(),
		[]string{
			"setup", "-mode", "docker",
			"-public-url", "http://127.0.0.1:5000",
			"-state-path", statePath,
			"-auth-postgres-dsn", "postgres://registry:registry@db.example.com:5432/regixtry_auth?sslmode=disable",
			"-admin-password", "change-me-now",
		},
		strings.NewReader(""),
		stdout,
		io.Discard,
	)
	if err != nil {
		t.Fatalf("runWithIO(setup docker external) error = %v", err)
	}

	if strings.TrimSpace(fake.writeProjectArg.ExternalPostgresDSN) == "" {
		t.Fatal("writeProjectArg.ExternalPostgresDSN is empty, want the supplied external DSN")
	}
	if strings.Contains(stdout.String(), "Generated bundled Postgres password") {
		t.Fatalf("stdout = %q, want no bundled password disclosure for an external-DSN project", stdout.String())
	}
}

// --- 8.7: rollback tests ----------------------------------------------------

func TestRunSetupDockerRollsBackOnFailureAfterWriteProject(t *testing.T) {
	tests := []struct {
		name     string
		fake     func() *fakeComposeRunner
		wantCall string
	}{
		{
			name: "StartDatabase failure",
			fake: func() *fakeComposeRunner {
				return &fakeComposeRunner{startDatabaseErr: errors.New("postgres never became ready")}
			},
			wantCall: "StartDatabase",
		},
		{
			name: "BootstrapAdmin failure",
			fake: func() *fakeComposeRunner {
				return &fakeComposeRunner{bootstrapAdminErr: errors.New("bootstrap-admin exited 1")}
			},
			wantCall: "BootstrapAdmin",
		},
		{
			name: "StartRegistry failure",
			fake: func() *fakeComposeRunner {
				return &fakeComposeRunner{startRegistryErr: errors.New("compose up failed")}
			},
			wantCall: "StartRegistry",
		},
		{
			name: "WaitReachable failure",
			fake: func() *fakeComposeRunner {
				return &fakeComposeRunner{waitReachableErr: errors.New("registry never became reachable")}
			},
			wantCall: "WaitReachable",
		},
		{
			name: "SaveProvenance failure",
			fake: func() *fakeComposeRunner {
				return &fakeComposeRunner{saveProvenanceErr: errors.New("disk full")}
			},
			wantCall: "SaveProvenance",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			statePath := filepath.Join(tempDir, "etc", "regixtry", "bootstrap-state.json")

			fake := tc.fake()
			restore := swapComposeRunner(t, fake)
			defer restore()

			err := runWithIO(
				context.Background(),
				[]string{"setup", "-mode", "docker", "-public-url", "http://127.0.0.1:5000", "-state-path", statePath, "-admin-password", "change-me-now"},
				strings.NewReader(""),
				io.Discard,
				io.Discard,
			)
			if err == nil {
				t.Fatal("runWithIO(setup docker) error = nil, want the injected failure to propagate")
			}
			if fake.downCalls != 1 {
				t.Fatalf("downCalls = %d, want exactly 1 rollback call after a %s failure", fake.downCalls, tc.wantCall)
			}
			if fake.calls[len(fake.calls)-1] != "Down" {
				t.Fatalf("last call = %q, want Down to run immediately after the %s failure", fake.calls[len(fake.calls)-1], tc.wantCall)
			}
		})
	}
}

func TestRunSetupDockerPreflightFailureNeverRollsBackOrWritesProject(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "etc", "regixtry", "bootstrap-state.json")

	fake := &fakeComposeRunner{preflightErr: errors.New("docker is not installed or not on PATH")}
	restore := swapComposeRunner(t, fake)
	defer restore()

	err := runWithIO(
		context.Background(),
		[]string{"setup", "-mode", "docker", "-public-url", "http://127.0.0.1:5000", "-state-path", statePath, "-admin-password", "change-me-now"},
		strings.NewReader(""),
		io.Discard,
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "docker is not installed or not on PATH") {
		t.Fatalf("runWithIO(setup docker preflight failure) error = %v, want the truthful preflight message", err)
	}
	if !reflect.DeepEqual(fake.calls, []string{"Preflight"}) {
		t.Fatalf("fake.calls = %v, want only [Preflight] -- nothing written and no rollback attempted", fake.calls)
	}
	if fake.downCalls != 0 {
		t.Fatalf("downCalls = %d, want 0 -- Preflight failure precedes WriteProject", fake.downCalls)
	}
}

func TestRunSetupDockerWriteProjectFailureNeverRollsBack(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "etc", "regixtry", "bootstrap-state.json")

	fake := &fakeComposeRunner{writeProjectErr: errors.New("a compose project already exists")}
	restore := swapComposeRunner(t, fake)
	defer restore()

	err := runWithIO(
		context.Background(),
		[]string{"setup", "-mode", "docker", "-public-url", "http://127.0.0.1:5000", "-state-path", statePath, "-admin-password", "change-me-now"},
		strings.NewReader(""),
		io.Discard,
		io.Discard,
	)
	if err == nil {
		t.Fatal("runWithIO(setup docker WriteProject failure) error = nil, want the injected failure to propagate")
	}
	if !reflect.DeepEqual(fake.calls, []string{"Preflight", "WriteProject"}) {
		t.Fatalf("fake.calls = %v, want [Preflight WriteProject] -- WriteProject failure precedes any rollback-eligible stage", fake.calls)
	}
	if fake.downCalls != 0 {
		t.Fatalf("downCalls = %d, want 0 -- WriteProject failure must not trigger Down", fake.downCalls)
	}
}

// --- non-interactive admin-password requirement, mirroring the
// daemon-sqlite regression already covered by
// TestRunSetupRequiresAdminPasswordWhenAuthEnabledWithoutTTY -------------

func TestRunSetupDockerRequiresAdminPasswordWithoutTTY(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "etc", "regixtry", "bootstrap-state.json")

	fake := &fakeComposeRunner{}
	restore := swapComposeRunner(t, fake)
	defer restore()

	err := runWithIO(
		context.Background(),
		[]string{"setup", "-mode", "docker", "-public-url", "http://127.0.0.1:5000", "-state-path", statePath},
		strings.NewReader(""),
		io.Discard,
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "admin password is required when auth is enabled") {
		t.Fatalf("runWithIO(setup docker, no admin password) error = %v, want missing admin password guidance", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("fake.calls = %v, want composeRunner never invoked before validation fails", fake.calls)
	}
}

// TestSetupDockerImageRefPrefixesMissingV is the RED test for the real bug
// found live during v0.2.1-rc6's docker-mode validation (surfaced only once
// the stderr-swallowing bug in the compose package was independently fixed):
// GoReleaser's `{{ .Version }}` template (used for this binary's own
// -ldflags-injected buildVersion, .goreleaser.yaml) omits the leading "v",
// but every actual GHCR tag this project publishes uses `{{ .Tag }}`, which
// keeps it (confirmed live: "ghcr.io/desatatufuria/regixtry:v0.2.1-rc6" is
// what really exists; "ghcr.io/desatatufuria/regixtry:0.2.1-rc6" -- what
// setupDockerImageRef produced -- does not, and `docker compose run` failed
// with "not found"). setupDockerImageRef must normalize either input shape
// to the one tag format that is ever actually published.
func TestSetupDockerImageRefPrefixesMissingV(t *testing.T) {
	previousVersion := buildVersion
	defer func() { buildVersion = previousVersion }()

	tests := []struct {
		name    string
		version string
		want    string
	}{
		{
			name:    "goreleaser's {{ .Version }} shape, missing v",
			version: "0.2.1-rc6",
			want:    "ghcr.io/desatatufuria/regixtry:v0.2.1-rc6",
		},
		{
			name:    "already v-prefixed, e.g. a manually set buildVersion",
			version: "v0.2.1-rc6",
			want:    "ghcr.io/desatatufuria/regixtry:v0.2.1-rc6",
		},
		{
			name:    "stable release, missing v",
			version: "1.4.0",
			want:    "ghcr.io/desatatufuria/regixtry:v1.4.0",
		},
		{
			name:    "unbuilt dev binary falls back to latest, untouched",
			version: "dev",
			want:    "ghcr.io/desatatufuria/regixtry:latest",
		},
		{
			name:    "empty version falls back to latest, untouched",
			version: "",
			want:    "ghcr.io/desatatufuria/regixtry:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buildVersion = tt.version
			if got := setupDockerImageRef(); got != tt.want {
				t.Fatalf("setupDockerImageRef() with buildVersion=%q = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}
