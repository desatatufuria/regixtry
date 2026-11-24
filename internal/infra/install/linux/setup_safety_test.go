package linux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// setupFixture is a temp-dir fake root plus a Bootstrapper wired to the real
// filesystem inside it. Each fail* field makes one phase of Run fail.
type setupFixture struct {
	root        string
	storageRoot string
	dbPath      string
	contentPath string
	envPath     string
	unitPath    string
	statePath   string
	cfg         BootstrapConfig

	commands []string
	failOn   map[string]bool // keys: write:<base>, mkdir, listen, systemctl daemon-reload, systemctl enable, probe
	probeErr error
}

func newSetupFixture(t *testing.T) *setupFixture {
	t.Helper()
	root := t.TempDir()
	f := &setupFixture{
		root:        root,
		storageRoot: filepath.Join(root, "var", "lib", "regixtry"),
		statePath:   filepath.Join(root, "etc", "regixtry", "bootstrap-state.json"),
		unitPath:    filepath.Join(root, "etc", "systemd", "system", "regixtry.service"),
		failOn:      map[string]bool{},
	}
	f.dbPath = filepath.Join(f.storageRoot, "metadata.db")
	f.contentPath = filepath.Join(f.storageRoot, "content")
	f.envPath = filepath.Join(root, "etc", "regixtry", "regixtry.env")
	f.cfg = BootstrapConfig{
		Mode:        supportedMode,
		PublicURL:   "http://127.0.0.1:5000",
		Addr:        "127.0.0.1:5000",
		StorageRoot: f.storageRoot,
		StatePath:   f.statePath,
		UnitPath:    f.unitPath,
		ServiceName: "regixtry",
	}
	return f
}

func (f *setupFixture) bootstrapper() *Bootstrapper {
	injected := errors.New("injected failure")
	return &Bootstrapper{
		detector: detector{
			goos:     "linux",
			readFile: func(string) ([]byte, error) { return []byte("ID=ubuntu\nVERSION_ID=24.04\n"), nil },
			stat:     func(string) (os.FileInfo, error) { return fakeInfo{name: "systemd"}, nil },
		},
		mkdirAll: os.MkdirAll,
		writeFile: func(path string, body []byte, mode os.FileMode) error {
			if f.failOn["write:"+filepath.Base(path)] {
				return injected
			}
			return os.WriteFile(path, body, mode)
		},
		writeSecret: func(path string, body []byte, mode os.FileMode) error {
			if f.failOn["write:"+filepath.Base(path)] {
				return injected
			}
			return os.WriteFile(path, body, mode)
		},
		readFile:  os.ReadFile,
		removeAll: os.RemoveAll,
		listen: func(string, string) (net.Listener, error) {
			if f.failOn["listen"] {
				return nil, &net.OpError{Op: "listen", Net: "tcp", Err: syscall.EADDRINUSE}
			}
			return stubListener{}, nil
		},
		executablePath: func() (string, error) { return "/usr/local/bin/regixtry", nil },
		runCommand: func(_ context.Context, name string, args ...string) error {
			line := name + " " + strings.Join(args, " ")
			f.commands = append(f.commands, line)
			switch {
			case f.failOn["systemctl daemon-reload"] && strings.Contains(line, "daemon-reload"):
				return injected
			case f.failOn["systemctl enable"] && strings.Contains(line, "enable --now"):
				return injected
			}
			return nil
		},
		probe: func(context.Context, string, bool) (int, error) {
			if f.failOn["probe"] {
				return 503, nil
			}
			return 200, nil
		},
		probeInterval: time.Millisecond,
		probeTimeout:  3 * time.Millisecond,
	}
}

func mustWrite(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestRunRefusesToSetUpOverExistingInstall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		seed     func(t *testing.T, f *setupFixture)
		wantPath func(f *setupFixture) string
	}{
		{
			name:     "non-empty metadata database",
			seed:     func(t *testing.T, f *setupFixture) { mustWrite(t, f.dbPath, []byte("SQLite format 3\x00 precious")) },
			wantPath: func(f *setupFixture) string { return f.dbPath },
		},
		{
			name: "non-empty content store",
			seed: func(t *testing.T, f *setupFixture) {
				mustWrite(t, filepath.Join(f.contentPath, "ab", "blob"), []byte("layer"))
			},
			wantPath: func(f *setupFixture) string { return f.contentPath },
		},
		{
			name:     "existing managed unit",
			seed:     func(t *testing.T, f *setupFixture) { mustWrite(t, f.unitPath, []byte("[Unit]\n")) },
			wantPath: func(f *setupFixture) string { return f.unitPath },
		},
		{
			name:     "existing bootstrap receipt",
			seed:     func(t *testing.T, f *setupFixture) { mustWrite(t, f.statePath, []byte("{}")) },
			wantPath: func(f *setupFixture) string { return f.statePath },
		},
		{
			name: "existing lifecycle provenance",
			seed: func(t *testing.T, f *setupFixture) {
				mustWrite(t, lifecycleProvenancePath(f.statePath), []byte("{}"))
			},
			wantPath: func(f *setupFixture) string { return lifecycleProvenancePath(f.statePath) },
		},
		{
			name:     "existing env file",
			seed:     func(t *testing.T, f *setupFixture) { mustWrite(t, f.envPath, []byte("REGISTRY_X=1\n")) },
			wantPath: func(f *setupFixture) string { return f.envPath },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newSetupFixture(t)
			tt.seed(t, f)
			before := snapshotTree(t, f.root)

			for _, check := range []struct {
				label string
				run   func() error
			}{
				{"Run", func() error { return f.bootstrapper().Run(context.Background(), f.cfg) }},
				{"CheckFreshInstall", func() error { return f.bootstrapper().CheckFreshInstall(f.cfg) }},
			} {
				err := check.run()
				if err == nil {
					t.Fatalf("%s() error = nil, want refusal", check.label)
				}
				if !strings.Contains(err.Error(), tt.wantPath(f)) {
					t.Fatalf("%s() error = %q, want it to name %s", check.label, err, tt.wantPath(f))
				}
				if !strings.Contains(err.Error(), "regixtry upgrade") {
					t.Fatalf("%s() error = %q, want it to point at `regixtry upgrade`", check.label, err)
				}
			}
			if len(f.commands) != 0 {
				t.Fatalf("commands = %v, want none before refusing", f.commands)
			}
			if after := snapshotTree(t, f.root); after != before {
				t.Fatalf("filesystem changed by a refused setup:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

func TestRunAcceptsEmptyPreexistingStorageLayout(t *testing.T) {
	t.Parallel()

	f := newSetupFixture(t)
	if err := os.MkdirAll(f.contentPath, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, f.dbPath, nil)

	if err := f.bootstrapper().Run(context.Background(), f.cfg); err != nil {
		t.Fatalf("Run() error = %v, want an empty storage layout accepted", err)
	}
}

func TestRunRecordsOnlyCreatedPathsInReceipt(t *testing.T) {
	t.Parallel()

	f := newSetupFixture(t)
	if err := f.bootstrapper().Run(context.Background(), f.cfg); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	body, err := os.ReadFile(f.statePath)
	if err != nil {
		t.Fatal(err)
	}
	var receipt BootstrapReceipt
	if err := json.Unmarshal(body, &receipt); err != nil {
		t.Fatal(err)
	}
	want := []string{f.envPath, f.unitPath, f.dbPath, f.contentPath, f.statePath}
	if strings.Join(receipt.CreatedPaths, "|") != strings.Join(want, "|") {
		t.Fatalf("created_paths = %v, want %v", receipt.CreatedPaths, want)
	}
}

// A failed setup must leave anything that existed before it started exactly as
// it was, whichever phase failed, and must remove what it created.
func TestFailedSetupLeavesPreexistingPathsUntouchedAtEveryPhase(t *testing.T) {
	t.Parallel()

	phases := []string{
		"write:regixtry.env",
		"write:regixtry.service",
		"write:metadata.db",
		"write:bootstrap-state.json",
		"listen",
		"systemctl daemon-reload",
		"systemctl enable",
		"probe",
	}
	for _, phase := range phases {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			f := newSetupFixture(t)
			// Pre-existing but empty: the only layout setup may legally run over.
			if err := os.MkdirAll(f.contentPath, 0o755); err != nil {
				t.Fatal(err)
			}
			// The database write is skipped over an existing file, so that phase
			// only fails when the run has to create it.
			seededDB := phase != "write:metadata.db"
			if seededDB {
				mustWrite(t, f.dbPath, nil)
			}
			marker := filepath.Join(f.storageRoot, "operator-notes.txt")
			mustWrite(t, marker, []byte("keep me"))
			f.failOn[phase] = true

			if err := f.bootstrapper().Run(context.Background(), f.cfg); err == nil {
				t.Fatalf("Run() error = nil, want injected %s failure", phase)
			}

			if !exists(f.contentPath) {
				t.Errorf("pre-existing content store %s removed by failed setup", f.contentPath)
			}
			if seededDB && !exists(f.dbPath) {
				t.Errorf("pre-existing database %s removed by failed setup", f.dbPath)
			}
			if !seededDB && exists(f.dbPath) {
				t.Errorf("database %s created by the failed run was left behind", f.dbPath)
			}
			if got, err := os.ReadFile(marker); err != nil || string(got) != "keep me" {
				t.Errorf("marker = %q, %v; want untouched", got, err)
			}
			for _, created := range []string{f.envPath, f.unitPath, f.statePath} {
				if exists(created) {
					t.Errorf("%s created by the failed run was left behind", created)
				}
			}
		})
	}
}

// Fresh storage created by the failed run is removed again (first-time
// behaviour is unchanged).
func TestFailedFirstTimeSetupRemovesWhatItCreated(t *testing.T) {
	t.Parallel()

	for _, phase := range []string{"write:bootstrap-state.json", "systemctl enable", "probe"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			f := newSetupFixture(t)
			f.failOn[phase] = true
			if err := f.bootstrapper().Run(context.Background(), f.cfg); err == nil {
				t.Fatalf("Run() error = nil, want injected failure")
			}
			for _, created := range []string{f.envPath, f.unitPath, f.statePath, f.dbPath, f.contentPath} {
				if exists(created) {
					t.Errorf("%s left behind after a failed first-time setup", created)
				}
			}
		})
	}
}

func TestRollbackWithReceiptRemovesOnlyCreatedPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	db := filepath.Join(root, "metadata.db")
	blob := filepath.Join(root, "content", "ab", "blob")
	env := filepath.Join(root, "regixtry.env")
	mustWrite(t, db, []byte("old database bytes"))
	mustWrite(t, db+"-wal", []byte("old wal"))
	mustWrite(t, blob, []byte("old blob"))
	mustWrite(t, env, []byte("env"))

	b := &Bootstrapper{
		removeAll:  os.RemoveAll,
		runCommand: func(context.Context, string, ...string) error { return nil },
	}
	receipt := BootstrapReceipt{
		ServiceName:  "regixtry",
		Paths:        []string{env, db, filepath.Join(root, "content")},
		CreatedPaths: []string{env},
	}
	if err := b.rollbackWithReceipt(context.Background(), receipt); err != nil {
		t.Fatalf("rollbackWithReceipt() error = %v", err)
	}
	if exists(env) {
		t.Errorf("created path %s not removed", env)
	}
	for path, want := range map[string]string{db: "old database bytes", db + "-wal": "old wal", blob: "old blob"} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q untouched", path, got, err, want)
		}
	}
}

func TestReadReceiptWithoutCreatedPathsKeepsLegacyRollback(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	env := filepath.Join(root, "regixtry.env")
	state := filepath.Join(root, "state.json")
	mustWrite(t, env, []byte("env"))
	mustWrite(t, state, []byte(`{"mode":"daemon-sqlite","service_name":"regixtry","paths":["`+env+`"]}`))
	b := &Bootstrapper{
		readFile:   os.ReadFile,
		removeAll:  os.RemoveAll,
		runCommand: func(context.Context, string, ...string) error { return nil },
	}
	if err := b.Rollback(context.Background(), BootstrapConfig{StatePath: state}); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if exists(env) {
		t.Fatalf("legacy receipt: %s still exists, want every listed path removed as before", env)
	}
}

// The configuration files setup writes must be replaced atomically: a reader
// (or a crash) must never see a truncated file, and the old inode must stay
// intact until rename(2) swaps the new one in.
func TestDefaultWriteFileReplacesAtomicallyAndKeepsIntendedMode(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	dir := t.TempDir()
	path := filepath.Join(dir, "regixtry.service")
	mustWrite(t, path, []byte("old unit"))
	alias := filepath.Join(dir, "alias")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}

	b := NewBootstrapper()
	if err := b.writeFile(path, []byte("new unit"), 0o644); err != nil {
		t.Fatalf("writeFile() error = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "new unit" {
		t.Fatalf("path = %q, want new content", got)
	}
	if got, _ := os.ReadFile(alias); string(got) != "old unit" {
		t.Fatalf("old inode = %q, want it untouched (write must be a rename, not an in-place truncate)", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, want 0644 regardless of umask", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file %s left behind", e.Name())
		}
	}
}

func TestDefaultWriteFileFailureLeavesTargetAndNoTempFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	mustWrite(t, filepath.Join(target, "child"), []byte("x")) // a non-empty directory cannot be replaced by rename
	if err := NewBootstrapper().writeFile(target, []byte("body"), 0o644); err == nil {
		t.Fatal("writeFile() error = nil, want failure")
	}
	if got, _ := os.ReadFile(filepath.Join(target, "child")); !bytes.Equal(got, []byte("x")) {
		t.Fatalf("target changed: %q", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("directory entries = %d, want only the target (temp file leaked)", len(entries))
	}
}

func snapshotTree(t *testing.T, root string) string {
	t.Helper()
	var out strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		out.WriteString(path)
		if info.Mode().IsRegular() {
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			out.WriteString(" " + string(body))
		}
		out.WriteString("\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out.String()
}
