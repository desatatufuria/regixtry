package linux

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDetectHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		goos    string
		osrel   string
		statErr error
		want    HostInfo
		wantErr string
	}{
		{
			name:  "supports ubuntu with systemd",
			goos:  "linux",
			osrel: "ID=ubuntu\nVERSION_ID=24.04\n",
			want:  HostInfo{Distribution: "Ubuntu", VersionID: "24.04"},
		},
		{
			name:    "rejects alpine",
			goos:    "linux",
			osrel:   "ID=alpine\nVERSION_ID=3.20\n",
			wantErr: "Alpine host bootstrap is deferred",
		},
		{
			name:    "rejects missing systemd",
			goos:    "linux",
			osrel:   "ID=debian\nVERSION_ID=12\n",
			statErr: os.ErrNotExist,
			wantErr: "systemd runtime not detected",
		},
	}

	for _, testCase := range tests {
		tt := testCase
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := detector{
				goos: tt.goos,
				readFile: func(string) ([]byte, error) {
					return []byte(tt.osrel), nil
				},
				stat: func(string) (os.FileInfo, error) {
					if tt.statErr != nil {
						return nil, tt.statErr
					}
					return fakeInfo{name: "systemd"}, nil
				},
			}

			got, err := d.Detect()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Detect() error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Detect() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestTemplateRendering(t *testing.T) {
	t.Parallel()

	plan := BootstrapPlan{
		Addr:                "127.0.0.1:5000",
		PublicURL:           "https://regixtry.example.com",
		RuntimeTLSMode:      RuntimeTLSModeDirectTLS,
		TLSCertFile:         "/etc/regixtry/tls/registry.crt",
		TLSKeyFile:          "/etc/regixtry/tls/registry.key",
		AuthPostgresDSN:     "postgres://registry:registry@db.example.com:5432/regixtry_auth?sslmode=disable",
		StorageRoot:         "/var/lib/regixtry",
		DatabasePath:        "/var/lib/regixtry/metadata.db",
		TrivyCacheDir:       "/var/lib/regixtry/trivy-cache",
		TrivyBinaryPath:     "trivy",
		TrivyTimeout:        15 * time.Minute,
		TrivyInterval:       24 * time.Hour,
		TrivyMaxConcurrency: 1,
		EnvPath:             "/etc/regixtry/regixtry.env",
		BinaryPath:          "/usr/local/bin/regixtry",
		ServiceName:         "regixtry",
	}

	env := RenderEnvFile(plan)
	if !strings.Contains(env, `REGISTRY_PUBLIC_URL="https://regixtry.example.com"`) {
		t.Fatalf("env = %q, want quoted public URL", env)
	}
	if !strings.Contains(env, `REGISTRY_TLS_CERT_FILE="/etc/regixtry/tls/registry.crt"`) {
		t.Fatalf("env = %q, want quoted TLS cert path", env)
	}
	if !strings.Contains(env, `REGISTRY_AUTH_POSTGRES_DSN="postgres://registry:registry@db.example.com:5432/regixtry_auth?sslmode=disable"`) {
		t.Fatalf("env = %q, want quoted auth DSN", env)
	}
	for _, unwanted := range []string{
		"REGISTRY_TRIVY_ENABLED",
		"REGISTRY_TRIVY_SCHEDULE_ENABLED",
		"REGISTRY_TRIVY_CACHE_DIR",
		"REGISTRY_TRIVY_TIMEOUT",
		"REGISTRY_TRIVY_INTERVAL",
		"REGISTRY_TRIVY_MAX_CONCURRENCY",
		"REGISTRY_TRIVY_BINARY_PATH",
	} {
		if strings.Contains(env, unwanted) {
			t.Fatalf("env = %q, want base-only env without %s", env, unwanted)
		}
	}

	unit := RenderSystemdUnit(plan)
	if !strings.Contains(unit, "EnvironmentFile=/etc/regixtry/regixtry.env") {
		t.Fatalf("unit = %q, want environment file", unit)
	}
	if !strings.Contains(unit, "/usr/local/bin/regixtry serve") {
		t.Fatalf("unit = %q, want regixtry serve exec start", unit)
	}
	if !strings.Contains(unit, "-auth-postgres-dsn=${REGISTRY_AUTH_POSTGRES_DSN}") {
		t.Fatalf("unit = %q, want auth DSN serve flag", unit)
	}
	if !strings.Contains(unit, "-tls-cert-file=${REGISTRY_TLS_CERT_FILE} -tls-key-file=${REGISTRY_TLS_KEY_FILE}") {
		t.Fatalf("unit = %q, want TLS serve flags", unit)
	}
}

func TestBootstrapReceiptOmitsFeatureOwnedTrivyArtifacts(t *testing.T) {
	t.Parallel()

	receipt := bootstrapReceiptFromPlan(BootstrapPlan{
		Mode:          supportedMode,
		ServiceName:   "regixtry",
		EnvPath:       "/etc/regixtry/regixtry.env",
		UnitPath:      "/etc/systemd/system/regixtry.service",
		DatabasePath:  "/var/lib/regixtry/metadata.db",
		ContentPath:   "/var/lib/regixtry/content",
		TrivyCacheDir: "/var/lib/regixtry/trivy-cache",
		StatePath:     "/etc/regixtry/bootstrap-state.json",
	})

	for _, path := range receipt.Paths {
		if path == "/var/lib/regixtry/trivy-cache" {
			t.Fatalf("receipt paths = %v, want trivy cache excluded from base lifecycle provenance", receipt.Paths)
		}
	}
}

func TestBootstrapRunWritesArtifactsAndRollsBackOnProbeFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	statePath := filepath.Join(root, "etc", "regixtry", "bootstrap-state.json")
	commandCalls := make([]string, 0, 2)

	b := &Bootstrapper{
		detector: detector{
			goos: "linux",
			readFile: func(string) ([]byte, error) {
				return []byte("ID=ubuntu\nVERSION_ID=24.04\n"), nil
			},
			stat: func(string) (os.FileInfo, error) { return fakeInfo{name: "systemd"}, nil },
		},
		mkdirAll:  os.MkdirAll,
		writeFile: os.WriteFile,
		readFile:  os.ReadFile,
		removeAll: os.RemoveAll,
		listen: func(string, string) (net.Listener, error) {
			return stubListener{}, nil
		},
		executablePath: func() (string, error) { return "/usr/local/bin/regixtry", nil },
		runCommand: func(_ context.Context, name string, args ...string) error {
			commandCalls = append(commandCalls, name+" "+strings.Join(args, " "))
			return nil
		},
		probe: func(context.Context, string, bool) (int, error) {
			return 503, nil
		},
		probeInterval: time.Millisecond,
		probeTimeout:  3 * time.Millisecond,
	}

	err := b.Run(context.Background(), BootstrapConfig{
		Mode:        supportedMode,
		PublicURL:   "http://127.0.0.1:5000",
		Addr:        "127.0.0.1:5000",
		StorageRoot: filepath.Join(root, "var", "lib", "regixtry"),
		StatePath:   statePath,
		UnitPath:    filepath.Join(root, "etc", "systemd", "system", "regixtry.service"),
		ServiceName: "regixtry",
	})
	if err == nil || !strings.Contains(err.Error(), "regixtry readiness probe returned 503") {
		t.Fatalf("Run() error = %v, want readiness probe failure", err)
	}

	if len(commandCalls) < 3 {
		t.Fatalf("command calls = %v, want daemon-reload, enable --now, and disable --now", commandCalls)
	}
	if _, statErr := os.Stat(statePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state receipt still exists after rollback, stat error = %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "var", "lib", "regixtry", "content")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("content path still exists after rollback, stat error = %v", statErr)
	}
}

func TestBootstrapRunRejectsUnsupportedMode(t *testing.T) {
	t.Parallel()

	b := &Bootstrapper{}
	err := b.Run(context.Background(), BootstrapConfig{
		Mode:        "postgres",
		PublicURL:   "http://127.0.0.1:5000",
		Addr:        "127.0.0.1:5000",
		StorageRoot: "/var/lib/regixtry",
		StatePath:   "/etc/regixtry/bootstrap-state.json",
		UnitPath:    "/etc/systemd/system/regixtry.service",
		ServiceName: "regixtry",
	})
	if err == nil || !strings.Contains(err.Error(), `unsupported mode "postgres"`) {
		t.Fatalf("Run() error = %v, want unsupported mode error", err)
	}
}

func TestBootstrapRunRejectsUnsupportedHost(t *testing.T) {
	t.Parallel()

	b := &Bootstrapper{
		detector: detector{
			goos: "darwin",
		},
	}
	err := b.Run(context.Background(), BootstrapConfig{
		Mode:        supportedMode,
		PublicURL:   "http://127.0.0.1:5000",
		Addr:        "127.0.0.1:5000",
		StorageRoot: "/var/lib/regixtry",
		StatePath:   "/etc/regixtry/bootstrap-state.json",
		UnitPath:    "/etc/systemd/system/regixtry.service",
		ServiceName: "regixtry",
	})
	if err == nil || !strings.Contains(err.Error(), `unsupported operating system "darwin"`) {
		t.Fatalf("Run() error = %v, want unsupported host error", err)
	}
}

func TestBootstrapRunRollsBackOnEnableFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	statePath := filepath.Join(root, "etc", "regixtry", "bootstrap-state.json")
	commandCalls := make([]string, 0, 3)

	b := &Bootstrapper{
		detector: detector{
			goos: "linux",
			readFile: func(string) ([]byte, error) {
				return []byte("ID=ubuntu\nVERSION_ID=24.04\n"), nil
			},
			stat: func(string) (os.FileInfo, error) { return fakeInfo{name: "systemd"}, nil },
		},
		mkdirAll:  os.MkdirAll,
		writeFile: os.WriteFile,
		readFile:  os.ReadFile,
		removeAll: os.RemoveAll,
		listen: func(string, string) (net.Listener, error) {
			return stubListener{}, nil
		},
		executablePath: func() (string, error) { return "/usr/local/bin/regixtry", nil },
		runCommand: func(_ context.Context, name string, args ...string) error {
			commandCalls = append(commandCalls, name+" "+strings.Join(args, " "))
			if strings.Join(args, " ") == "enable --now regixtry.service" {
				return errors.New("systemd start failed")
			}
			return nil
		},
		probe: func(context.Context, string, bool) (int, error) {
			return 401, nil
		},
		probeInterval: time.Millisecond,
		probeTimeout:  50 * time.Millisecond,
	}

	err := b.Run(context.Background(), BootstrapConfig{
		Mode:        supportedMode,
		PublicURL:   "http://127.0.0.1:5000",
		Addr:        "127.0.0.1:5000",
		StorageRoot: filepath.Join(root, "var", "lib", "regixtry"),
		StatePath:   statePath,
		UnitPath:    filepath.Join(root, "etc", "systemd", "system", "regixtry.service"),
		ServiceName: "regixtry",
	})
	if err == nil || !strings.Contains(err.Error(), "systemctl enable --now regixtry.service") {
		t.Fatalf("Run() error = %v, want enable failure", err)
	}
	if got, want := commandCalls, []string{"systemctl daemon-reload", "systemctl enable --now regixtry.service", "systemctl disable --now regixtry.service"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("command calls = %v, want %v", got, want)
	}
	if _, statErr := os.Stat(statePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state receipt still exists after rollback, stat error = %v", statErr)
	}
}

func TestBootstrapPlanEmitsLifecycleProvenance(t *testing.T) {
	t.Parallel()

	b := &Bootstrapper{
		executablePath: func() (string, error) { return "/usr/local/bin/regixtry", nil },
	}

	_, receipt, provenance, err := b.plan(BootstrapConfig{
		Mode:        supportedMode,
		PublicURL:   "http://127.0.0.1:5000",
		Addr:        "127.0.0.1:5000",
		StorageRoot: "/var/lib/regixtry",
		StatePath:   "/etc/regixtry/bootstrap-state.json",
		UnitPath:    "/etc/systemd/system/regixtry.service",
		ServiceName: "regixtry",
	})
	if err != nil {
		t.Fatalf("plan() error = %v", err)
	}
	if provenance.InstalledBin != "/usr/local/bin/regixtry" {
		t.Fatalf("InstalledBin = %q, want %q", provenance.InstalledBin, "/usr/local/bin/regixtry")
	}
	if provenance.StatePath != "/etc/regixtry/"+lifecycleProvenanceFileName {
		t.Fatalf("StatePath = %q, want %q", provenance.StatePath, "/etc/regixtry/"+lifecycleProvenanceFileName)
	}
	if strings.Join(provenance.ManagedPaths, "|") != strings.Join(receipt.Paths, "|") {
		t.Fatalf("ManagedPaths = %v, want %v", provenance.ManagedPaths, receipt.Paths)
	}
	if provenance.Intent.TrivyCacheDir != "" {
		t.Fatalf("TrivyCacheDir = %q, want omitted feature-owned trivy state", provenance.Intent.TrivyCacheDir)
	}
	if provenance.Intent.TrivyEnabled {
		t.Fatal("TrivyEnabled = true, want omitted feature-owned state")
	}
}

func TestBootstrapRunReportsSuccessAfterActivationAndReadiness(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	statePath := filepath.Join(root, "etc", "regixtry", "bootstrap-state.json")
	storageRoot := filepath.Join(root, "var", "lib", "regixtry")
	unitPath := filepath.Join(root, "etc", "systemd", "system", "regixtry.service")
	commandCalls := make([]string, 0, 2)
	probeCalls := make([]string, 0, 1)
	listenCalls := make([]string, 0, 1)

	b := &Bootstrapper{
		detector: detector{
			goos: "linux",
			readFile: func(string) ([]byte, error) {
				return []byte("ID=ubuntu\nVERSION_ID=24.04\n"), nil
			},
			stat: func(string) (os.FileInfo, error) { return fakeInfo{name: "systemd"}, nil },
		},
		mkdirAll:  os.MkdirAll,
		writeFile: os.WriteFile,
		readFile:  os.ReadFile,
		removeAll: os.RemoveAll,
		listen: func(network string, addr string) (net.Listener, error) {
			listenCalls = append(listenCalls, network+" "+addr)
			return stubListener{}, nil
		},
		executablePath: func() (string, error) { return "/usr/local/bin/regixtry", nil },
		runCommand: func(_ context.Context, name string, args ...string) error {
			commandCalls = append(commandCalls, name+" "+strings.Join(args, " "))
			return nil
		},
		probe: func(_ context.Context, rawURL string, _ bool) (int, error) {
			probeCalls = append(probeCalls, rawURL)
			return 401, nil
		},
		probeInterval: time.Millisecond,
		probeTimeout:  50 * time.Millisecond,
	}

	err := b.Run(context.Background(), BootstrapConfig{
		Mode:        supportedMode,
		PublicURL:   "http://127.0.0.1:5000",
		Addr:        "127.0.0.1:5000",
		StorageRoot: storageRoot,
		StatePath:   statePath,
		UnitPath:    unitPath,
		ServiceName: "regixtry",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got, want := commandCalls, []string{"systemctl daemon-reload", "systemctl enable --now regixtry.service"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("command calls = %v, want %v", got, want)
	}
	if got, want := listenCalls, []string{"tcp 127.0.0.1:5000"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("listen calls = %v, want %v", got, want)
	}
	if len(probeCalls) != 1 || probeCalls[0] != "http://127.0.0.1:5000/v2/" {
		t.Fatalf("probe calls = %v, want [http://127.0.0.1:5000/v2/]", probeCalls)
	}

	for _, path := range []string{statePath, filepath.Join(root, "etc", "regixtry", "regixtry.env"), unitPath, filepath.Join(storageRoot, "metadata.db"), filepath.Join(storageRoot, "content")} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected %s to exist after successful bootstrap, stat error = %v", path, statErr)
		}
	}

	receiptBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile(receipt) error = %v", err)
	}
	if !strings.Contains(string(receiptBytes), `"service_name": "regixtry"`) {
		t.Fatalf("receipt = %q, want service_name", string(receiptBytes))
	}
}

func TestBootstrapRunReverseProxyModeProbesLocalHTTPBackend(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	statePath := filepath.Join(root, "etc", "regixtry", "bootstrap-state.json")
	storageRoot := filepath.Join(root, "var", "lib", "regixtry")
	unitPath := filepath.Join(root, "etc", "systemd", "system", "regixtry.service")
	probeCalls := make([]string, 0, 1)
	insecureProbeFlags := make([]bool, 0, 1)

	b := &Bootstrapper{
		detector: detector{
			goos: "linux",
			readFile: func(string) ([]byte, error) {
				return []byte("ID=ubuntu\nVERSION_ID=24.04\n"), nil
			},
			stat: func(string) (os.FileInfo, error) { return fakeInfo{name: "systemd"}, nil },
		},
		mkdirAll:       os.MkdirAll,
		writeFile:      os.WriteFile,
		readFile:       os.ReadFile,
		removeAll:      os.RemoveAll,
		listen:         func(string, string) (net.Listener, error) { return stubListener{}, nil },
		executablePath: func() (string, error) { return "/usr/local/bin/regixtry", nil },
		runCommand:     func(_ context.Context, _ string, _ ...string) error { return nil },
		probe: func(_ context.Context, rawURL string, insecureTLS bool) (int, error) {
			probeCalls = append(probeCalls, rawURL)
			insecureProbeFlags = append(insecureProbeFlags, insecureTLS)
			return 401, nil
		},
		probeInterval: time.Millisecond,
		probeTimeout:  50 * time.Millisecond,
	}

	err := b.Run(context.Background(), BootstrapConfig{
		Mode:           supportedMode,
		PublicURL:      "https://regixtry.example.com",
		RuntimeTLSMode: RuntimeTLSModeReverseProxy,
		Addr:           "0.0.0.0:5443",
		StorageRoot:    storageRoot,
		StatePath:      statePath,
		UnitPath:       unitPath,
		ServiceName:    "regixtry",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := probeCalls, []string{"http://127.0.0.1:5443/v2/"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("probe calls = %v, want %v", got, want)
	}
	if len(insecureProbeFlags) != 1 || insecureProbeFlags[0] {
		t.Fatalf("insecureProbeFlags = %v, want [false]", insecureProbeFlags)
	}
}

func TestBootstrapRunDirectTLSModeProbesLocalHTTPSBackend(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	statePath := filepath.Join(root, "etc", "regixtry", "bootstrap-state.json")
	storageRoot := filepath.Join(root, "var", "lib", "regixtry")
	unitPath := filepath.Join(root, "etc", "systemd", "system", "regixtry.service")
	probeCalls := make([]string, 0, 1)
	insecureProbeFlags := make([]bool, 0, 1)

	b := &Bootstrapper{
		detector: detector{
			goos: "linux",
			readFile: func(string) ([]byte, error) {
				return []byte("ID=ubuntu\nVERSION_ID=24.04\n"), nil
			},
			stat: func(string) (os.FileInfo, error) { return fakeInfo{name: "systemd"}, nil },
		},
		mkdirAll:       os.MkdirAll,
		writeFile:      os.WriteFile,
		readFile:       os.ReadFile,
		removeAll:      os.RemoveAll,
		listen:         func(string, string) (net.Listener, error) { return stubListener{}, nil },
		executablePath: func() (string, error) { return "/usr/local/bin/regixtry", nil },
		runCommand:     func(_ context.Context, _ string, _ ...string) error { return nil },
		probe: func(_ context.Context, rawURL string, insecureTLS bool) (int, error) {
			probeCalls = append(probeCalls, rawURL)
			insecureProbeFlags = append(insecureProbeFlags, insecureTLS)
			return 401, nil
		},
		probeInterval: time.Millisecond,
		probeTimeout:  50 * time.Millisecond,
	}

	err := b.Run(context.Background(), BootstrapConfig{
		Mode:           supportedMode,
		PublicURL:      "https://regixtry.example.com",
		RuntimeTLSMode: RuntimeTLSModeDirectTLS,
		TLSCertFile:    "/etc/regixtry/tls/registry.crt",
		TLSKeyFile:     "/etc/regixtry/tls/registry.key",
		Addr:           "0.0.0.0:5443",
		StorageRoot:    storageRoot,
		StatePath:      statePath,
		UnitPath:       unitPath,
		ServiceName:    "regixtry",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := probeCalls, []string{"https://127.0.0.1:5443/v2/"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("probe calls = %v, want %v", got, want)
	}
	if len(insecureProbeFlags) != 1 || !insecureProbeFlags[0] {
		t.Fatalf("insecureProbeFlags = %v, want [true]", insecureProbeFlags)
	}
}

func TestBootstrapRunSkipsStartupWhenNoStart(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	statePath := filepath.Join(root, "etc", "regixtry", "bootstrap-state.json")
	storageRoot := filepath.Join(root, "var", "lib", "regixtry")
	unitPath := filepath.Join(root, "etc", "systemd", "system", "regixtry.service")
	var listenCalls, commandCalls, probeCalls int

	b := &Bootstrapper{
		detector: detector{
			goos: "linux",
			readFile: func(string) ([]byte, error) {
				return []byte("ID=ubuntu\nVERSION_ID=24.04\n"), nil
			},
			stat: func(string) (os.FileInfo, error) { return fakeInfo{name: "systemd"}, nil },
		},
		mkdirAll:  os.MkdirAll,
		writeFile: os.WriteFile,
		readFile:  os.ReadFile,
		removeAll: os.RemoveAll,
		listen: func(string, string) (net.Listener, error) {
			listenCalls++
			return stubListener{}, nil
		},
		executablePath: func() (string, error) { return "/usr/local/bin/regixtry", nil },
		runCommand: func(_ context.Context, _ string, _ ...string) error {
			commandCalls++
			return nil
		},
		probe: func(context.Context, string, bool) (int, error) {
			probeCalls++
			return 200, nil
		},
		probeInterval: time.Millisecond,
		probeTimeout:  50 * time.Millisecond,
	}

	err := b.Run(context.Background(), BootstrapConfig{
		Mode:        supportedMode,
		PublicURL:   "http://127.0.0.1:5000",
		Addr:        "127.0.0.1:5000",
		StorageRoot: storageRoot,
		StatePath:   statePath,
		UnitPath:    unitPath,
		ServiceName: "regixtry",
		NoStart:     true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if listenCalls != 0 {
		t.Fatalf("listenCalls = %d, want 0", listenCalls)
	}
	if commandCalls != 0 {
		t.Fatalf("commandCalls = %d, want 0", commandCalls)
	}
	if probeCalls != 0 {
		t.Fatalf("probeCalls = %d, want 0", probeCalls)
	}
	for _, path := range []string{statePath, filepath.Join(root, "etc", "regixtry", "regixtry.env"), unitPath, filepath.Join(storageRoot, "metadata.db"), filepath.Join(storageRoot, "content")} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected %s to exist after no-start bootstrap, stat error = %v", path, statErr)
		}
	}
}

func TestBootstrapRunFailsBeforeServiceStartWhenLocalBindIsOccupied(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	statePath := filepath.Join(root, "etc", "regixtry", "bootstrap-state.json")
	storageRoot := filepath.Join(root, "var", "lib", "regixtry")
	unitPath := filepath.Join(root, "etc", "systemd", "system", "regixtry.service")
	commandCalls := make([]string, 0, 2)
	probeCalls := 0

	b := &Bootstrapper{
		detector: detector{
			goos: "linux",
			readFile: func(string) ([]byte, error) {
				return []byte("ID=ubuntu\nVERSION_ID=24.04\n"), nil
			},
			stat: func(string) (os.FileInfo, error) { return fakeInfo{name: "systemd"}, nil },
		},
		mkdirAll:  os.MkdirAll,
		writeFile: os.WriteFile,
		readFile:  os.ReadFile,
		removeAll: os.RemoveAll,
		listen: func(string, string) (net.Listener, error) {
			return nil, &net.OpError{Op: "listen", Net: "tcp", Err: syscall.EADDRINUSE}
		},
		executablePath: func() (string, error) { return "/usr/local/bin/regixtry", nil },
		runCommand: func(_ context.Context, name string, args ...string) error {
			commandCalls = append(commandCalls, name+" "+strings.Join(args, " "))
			return nil
		},
		probe: func(context.Context, string, bool) (int, error) {
			probeCalls++
			return 200, nil
		},
		probeInterval: time.Millisecond,
		probeTimeout:  50 * time.Millisecond,
	}

	err := b.Run(context.Background(), BootstrapConfig{
		Mode:        supportedMode,
		PublicURL:   "http://127.0.0.1:5000",
		Addr:        "127.0.0.1:5000",
		StorageRoot: storageRoot,
		StatePath:   statePath,
		UnitPath:    unitPath,
		ServiceName: "regixtry",
	})
	if err == nil {
		t.Fatal("Run() error = nil, want occupied local bind failure")
	}
	if !strings.Contains(err.Error(), "configured local bind address 127.0.0.1:5000 is already in use") {
		t.Fatalf("Run() error = %v, want occupied bind summary", err)
	}
	if !strings.Contains(err.Error(), "sudo ss -ltnp 'sport = :5000'") {
		t.Fatalf("Run() error = %v, want ss recovery command", err)
	}
	if !strings.Contains(err.Error(), "sudo systemctl stop regixtry.service") {
		t.Fatalf("Run() error = %v, want systemctl stop recovery command", err)
	}
	if !strings.Contains(err.Error(), "regixtry bootstrap --mode daemon-sqlite --addr 127.0.0.1:5001 --public-url http://127.0.0.1:5001") {
		t.Fatalf("Run() error = %v, want rerun recovery command", err)
	}
	if len(commandCalls) != 0 {
		t.Fatalf("commandCalls = %v, want no systemctl calls before failure", commandCalls)
	}
	if probeCalls != 0 {
		t.Fatalf("probeCalls = %d, want 0", probeCalls)
	}
	if _, statErr := os.Stat(statePath); statErr != nil {
		t.Fatalf("expected state receipt to exist after preflight failure, stat error = %v", statErr)
	}
}

func TestBootstrapRunSkipsLocalPreflightForNonLocalBind(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	statePath := filepath.Join(root, "etc", "regixtry", "bootstrap-state.json")
	storageRoot := filepath.Join(root, "var", "lib", "regixtry")
	unitPath := filepath.Join(root, "etc", "systemd", "system", "regixtry.service")
	listenCalls := 0
	commandCalls := make([]string, 0, 2)

	b := &Bootstrapper{
		detector: detector{
			goos: "linux",
			readFile: func(string) ([]byte, error) {
				return []byte("ID=ubuntu\nVERSION_ID=24.04\n"), nil
			},
			stat: func(string) (os.FileInfo, error) { return fakeInfo{name: "systemd"}, nil },
		},
		mkdirAll:  os.MkdirAll,
		writeFile: os.WriteFile,
		readFile:  os.ReadFile,
		removeAll: os.RemoveAll,
		listen: func(string, string) (net.Listener, error) {
			listenCalls++
			return stubListener{}, nil
		},
		executablePath: func() (string, error) { return "/usr/local/bin/regixtry", nil },
		runCommand: func(_ context.Context, name string, args ...string) error {
			commandCalls = append(commandCalls, name+" "+strings.Join(args, " "))
			return nil
		},
		probe: func(context.Context, string, bool) (int, error) {
			return 401, nil
		},
		probeInterval: time.Millisecond,
		probeTimeout:  50 * time.Millisecond,
	}

	err := b.Run(context.Background(), BootstrapConfig{
		Mode:        supportedMode,
		PublicURL:   "http://regixtry.example.com:5443",
		Addr:        "0.0.0.0:5443",
		StorageRoot: storageRoot,
		StatePath:   statePath,
		UnitPath:    unitPath,
		ServiceName: "regixtry",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if listenCalls != 0 {
		t.Fatalf("listenCalls = %d, want 0 for non-local bind", listenCalls)
	}
	if got, want := commandCalls, []string{"systemctl daemon-reload", "systemctl enable --now regixtry.service"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("command calls = %v, want %v", got, want)
	}
}

func TestBootstrapRollbackRemovesGeneratedArtifacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	statePath := filepath.Join(root, "etc", "regixtry", "bootstrap-state.json")
	servicePath := filepath.Join(root, "regixtry.service")
	envPath := filepath.Join(root, "regixtry.env")
	contentPath := filepath.Join(root, "content")
	if err := os.WriteFile(envPath, []byte("env\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(env) error = %v", err)
	}
	if err := os.MkdirAll(contentPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(content) error = %v", err)
	}
	if err := os.WriteFile(servicePath, []byte("unit\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(unit) error = %v", err)
	}
	receipt := []byte(`{"mode":"daemon-sqlite","service_name":"regixtry","paths":["` + envPath + `","` + servicePath + `","` + contentPath + `","` + statePath + `"]}`)
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(state) error = %v", err)
	}
	if err := os.WriteFile(statePath, receipt, 0o644); err != nil {
		t.Fatalf("WriteFile(receipt) error = %v", err)
	}

	var commands []string
	b := &Bootstrapper{
		readFile:  os.ReadFile,
		removeAll: os.RemoveAll,
		runCommand: func(_ context.Context, name string, args ...string) error {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return nil
		},
	}

	if err := b.Rollback(context.Background(), BootstrapConfig{StatePath: statePath}); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if len(commands) != 1 || !strings.Contains(commands[0], "disable --now regixtry.service") {
		t.Fatalf("commands = %v, want systemctl disable --now", commands)
	}
	if _, err := os.Stat(envPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("env path still exists, stat error = %v", err)
	}
}

func TestBootstrapRollbackReportsDisableFailureAndStillRemovesArtifacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	statePath := filepath.Join(root, "etc", "regixtry", "bootstrap-state.json")
	envPath := filepath.Join(root, "regixtry.env")
	if err := os.WriteFile(envPath, []byte("env\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(env) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(state) error = %v", err)
	}
	receipt := []byte(`{"mode":"daemon-sqlite","service_name":"regixtry","paths":["` + envPath + `","` + statePath + `"]}`)
	if err := os.WriteFile(statePath, receipt, 0o644); err != nil {
		t.Fatalf("WriteFile(receipt) error = %v", err)
	}

	b := &Bootstrapper{
		readFile:  os.ReadFile,
		removeAll: os.RemoveAll,
		runCommand: func(_ context.Context, _ string, _ ...string) error {
			return errors.New("systemd unavailable")
		},
	}

	err := b.Rollback(context.Background(), BootstrapConfig{StatePath: statePath})
	if err == nil || !strings.Contains(err.Error(), "systemctl disable --now regixtry.service") {
		t.Fatalf("Rollback() error = %v, want disable failure", err)
	}
	if _, statErr := os.Stat(envPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("env path still exists after disable failure, stat error = %v", statErr)
	}
}

type fakeInfo struct{ name string }

func (f fakeInfo) Name() string       { return f.name }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() os.FileMode  { return os.ModeDir }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return true }
func (f fakeInfo) Sys() any           { return nil }

type stubListener struct{}

func (stubListener) Accept() (net.Conn, error) { return nil, errors.New("not implemented") }
func (stubListener) Close() error              { return nil }
func (stubListener) Addr() net.Addr            { return &net.TCPAddr{} }
