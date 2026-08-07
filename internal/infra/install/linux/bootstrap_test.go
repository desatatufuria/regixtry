package linux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
		Addr:         "127.0.0.1:5000",
		PublicURL:    "http://127.0.0.1:5000",
		StorageRoot:  "/var/lib/registry",
		DatabasePath: "/var/lib/registry/metadata.db",
		EnvPath:      "/etc/registry/registry.env",
		BinaryPath:   "/usr/local/bin/registry",
		ServiceName:  "registry",
	}

	env := RenderEnvFile(plan)
	if !strings.Contains(env, `REGISTRY_PUBLIC_URL="http://127.0.0.1:5000"`) {
		t.Fatalf("env = %q, want quoted public URL", env)
	}

	unit := RenderSystemdUnit(plan)
	if !strings.Contains(unit, "EnvironmentFile=/etc/registry/registry.env") {
		t.Fatalf("unit = %q, want environment file", unit)
	}
	if !strings.Contains(unit, "/usr/local/bin/registry serve") {
		t.Fatalf("unit = %q, want registry serve exec start", unit)
	}
}

func TestBootstrapRunWritesArtifactsAndRollsBackOnProbeFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	statePath := filepath.Join(root, "etc", "registry", "bootstrap-state.json")
	commandCalls := make([]string, 0, 2)

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
		executablePath: func() (string, error) { return "/usr/local/bin/registry", nil },
		runCommand: func(_ context.Context, name string, args ...string) error {
			commandCalls = append(commandCalls, name+" "+strings.Join(args, " "))
			return nil
		},
		probe: func(context.Context, string) (int, error) {
			return 503, nil
		},
		probeInterval: time.Millisecond,
		probeTimeout:  3 * time.Millisecond,
	}

	err := b.Run(context.Background(), BootstrapConfig{
		Mode:        supportedMode,
		PublicURL:   "http://127.0.0.1:5000",
		Addr:        "127.0.0.1:5000",
		StorageRoot: filepath.Join(root, "var", "lib", "registry"),
		StatePath:   statePath,
		UnitPath:    filepath.Join(root, "etc", "systemd", "system", "registry.service"),
		ServiceName: "registry",
	})
	if err == nil || !strings.Contains(err.Error(), "registry readiness probe returned 503") {
		t.Fatalf("Run() error = %v, want readiness probe failure", err)
	}

	if len(commandCalls) < 3 {
		t.Fatalf("command calls = %v, want daemon-reload, enable --now, and disable --now", commandCalls)
	}
	if _, statErr := os.Stat(statePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state receipt still exists after rollback, stat error = %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "var", "lib", "registry", "content")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("content path still exists after rollback, stat error = %v", statErr)
	}
}

func TestBootstrapRunReportsSuccessAfterActivationAndReadiness(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	statePath := filepath.Join(root, "etc", "registry", "bootstrap-state.json")
	storageRoot := filepath.Join(root, "var", "lib", "registry")
	unitPath := filepath.Join(root, "etc", "systemd", "system", "registry.service")
	commandCalls := make([]string, 0, 2)
	probeCalls := make([]string, 0, 1)

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
		executablePath: func() (string, error) { return "/usr/local/bin/registry", nil },
		runCommand: func(_ context.Context, name string, args ...string) error {
			commandCalls = append(commandCalls, name+" "+strings.Join(args, " "))
			return nil
		},
		probe: func(_ context.Context, rawURL string) (int, error) {
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
		ServiceName: "registry",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got, want := commandCalls, []string{"systemctl daemon-reload", "systemctl enable --now registry.service"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("command calls = %v, want %v", got, want)
	}
	if len(probeCalls) != 1 || probeCalls[0] != "http://127.0.0.1:5000/v2/" {
		t.Fatalf("probe calls = %v, want [http://127.0.0.1:5000/v2/]", probeCalls)
	}

	for _, path := range []string{statePath, filepath.Join(root, "etc", "registry", "registry.env"), unitPath, filepath.Join(storageRoot, "metadata.db"), filepath.Join(storageRoot, "content")} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected %s to exist after successful bootstrap, stat error = %v", path, statErr)
		}
	}

	receiptBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile(receipt) error = %v", err)
	}
	if !strings.Contains(string(receiptBytes), `"service_name": "registry"`) {
		t.Fatalf("receipt = %q, want service_name", string(receiptBytes))
	}
}

func TestBootstrapRollbackRemovesGeneratedArtifacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	statePath := filepath.Join(root, "etc", "registry", "bootstrap-state.json")
	servicePath := filepath.Join(root, "registry.service")
	envPath := filepath.Join(root, "registry.env")
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
	receipt := []byte(`{"mode":"daemon-sqlite","service_name":"registry","paths":["` + envPath + `","` + servicePath + `","` + contentPath + `","` + statePath + `"]}`)
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
	if len(commands) != 1 || !strings.Contains(commands[0], "disable --now registry.service") {
		t.Fatalf("commands = %v, want systemctl disable --now", commands)
	}
	if _, err := os.Stat(envPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("env path still exists, stat error = %v", err)
	}
}

type fakeInfo struct{ name string }

func (f fakeInfo) Name() string       { return f.name }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() os.FileMode  { return os.ModeDir }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return true }
func (f fakeInfo) Sys() any           { return nil }
