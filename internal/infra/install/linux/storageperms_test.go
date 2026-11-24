package linux

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

const storageUmaskChildEnv = "REGIXTRY_INSTALL_MODES_UMASK"

func ownerOf(t *testing.T, path string) (uint32, uint32) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	return st.Uid, st.Gid
}

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode() & (os.ModePerm | os.ModeSetgid | os.ModeSetuid | os.ModeSticky)
}

func TestTightenOtherAccess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ownerUID, ownerGID := ownerOf(t, dir)

	tests := []struct {
		name       string
		perm       os.FileMode
		isDir      bool
		uid        uint32
		gids       []uint32
		want       tightenResult
		wantMode   os.FileMode
		wantChmods int
	}{
		{name: "world-readable directory owned by the service user", perm: 0o755, isDir: true, uid: ownerUID, want: tightenedOther, wantMode: 0o750, wantChmods: 1},
		{name: "world-readable file owned by the service user", perm: 0o644, uid: ownerUID, want: tightenedOther, wantMode: 0o640, wantChmods: 1},
		{name: "keeps user and group bits and setgid", perm: 0o775 | os.ModeSetgid, isDir: true, uid: ownerUID, want: tightenedOther, wantMode: 0o770 | os.ModeSetgid, wantChmods: 1},
		{name: "service user reaches it through the group", perm: 0o775, isDir: true, uid: ownerUID + 4242, gids: []uint32{ownerGID}, want: tightenedOther, wantMode: 0o770, wantChmods: 1},
		{name: "root always keeps access", perm: 0o755, isDir: true, uid: 0, want: tightenedOther, wantMode: 0o750, wantChmods: 1},
		{name: "already tight tree is untouched", perm: 0o750, isDir: true, uid: ownerUID, want: tightenNotNeeded, wantMode: 0o750},
		{name: "owner-only file is untouched", perm: 0o600, uid: ownerUID, want: tightenNotNeeded, wantMode: 0o600},
		{name: "service user is only other: never lock it out", perm: 0o755, isDir: true, uid: ownerUID + 4242, gids: []uint32{ownerGID + 4242}, want: tightenSkippedServiceIsOther, wantMode: 0o755},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(tt.name, " ", "-"))
			if tt.isDir {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tt.perm); err != nil {
				t.Fatal(err)
			}

			chmods := 0
			got, err := tightenOtherAccess(path, tt.uid, tt.gids, func(p string, m os.FileMode) error {
				chmods++
				return os.Chmod(p, m)
			})
			if err != nil {
				t.Fatalf("tightenOtherAccess() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("result = %v, want %v", got, tt.want)
			}
			if chmods != tt.wantChmods {
				t.Errorf("chmod calls = %d, want %d", chmods, tt.wantChmods)
			}
			if gotMode := modeOf(t, path); gotMode != tt.wantMode {
				t.Errorf("mode = %v, want %v", gotMode, tt.wantMode)
			}
		})
	}

	t.Run("missing path is not an error", func(t *testing.T) {
		got, err := tightenOtherAccess(filepath.Join(dir, "absent"), ownerUID, nil, os.Chmod)
		if err != nil || got != tightenNotNeeded {
			t.Fatalf("tightenOtherAccess(absent) = %v, %v; want %v, nil", got, err, tightenNotNeeded)
		}
	})
}

func newModesBootstrapper(warnings *[]string) *Bootstrapper {
	return &Bootstrapper{
		mkdirAll:  os.MkdirAll,
		writeFile: os.WriteFile,
		chmod:     os.Chmod,
		warnf:     func(format string, args ...any) { *warnings = append(*warnings, format) },
	}
}

func modesPlan(root string) BootstrapPlan {
	return BootstrapPlan{
		Mode:         supportedMode,
		StorageRoot:  filepath.Join(root, "var", "lib", "regixtry"),
		ContentPath:  filepath.Join(root, "var", "lib", "regixtry", "content"),
		DatabasePath: filepath.Join(root, "var", "lib", "regixtry", "metadata.db"),
		EnvPath:      filepath.Join(root, "etc", "regixtry", "regixtry.env"),
		UnitPath:     filepath.Join(root, "etc", "systemd", "system", "regixtry.service"),
		StatePath:    filepath.Join(root, "etc", "regixtry", "bootstrap-state.json"),
		ServiceName:  "regixtry",
	}
}

// TestSetupCreatesStorageWithoutOtherAccess runs under explicit umasks in a
// re-executed child so the process-wide umask never leaks to other tests.
func TestSetupCreatesStorageWithoutOtherAccess(t *testing.T) {
	if raw := os.Getenv(storageUmaskChildEnv); raw != "" {
		mask, err := strconv.ParseUint(raw, 8, 32)
		if err != nil {
			t.Fatalf("bad %s=%q: %v", storageUmaskChildEnv, raw, err)
		}
		syscall.Umask(int(mask))

		root := t.TempDir()
		plan := modesPlan(root)
		var warnings []string
		b := newModesBootstrapper(&warnings)
		if err := b.writeSetupArtifacts(plan, BootstrapReceipt{Mode: supportedMode}); err != nil {
			t.Fatalf("writeSetupArtifacts() error = %v", err)
		}
		want := map[string]os.FileMode{
			plan.StorageRoot:  0o750,
			plan.ContentPath:  0o750,
			plan.DatabasePath: 0o640,
			// The config and unit directories stay traversable: systemd and
			// other tools must still reach them.
			filepath.Dir(plan.EnvPath):  0o755,
			filepath.Dir(plan.UnitPath): 0o755,
		}
		for path, wantMode := range want {
			if got := modeOf(t, path); got != wantMode {
				t.Errorf("mode of %s = %#o, want %#o", path, got, wantMode)
			}
		}
		if len(warnings) != 0 {
			t.Errorf("fresh setup warned: %v", warnings)
		}
		return
	}

	for _, mask := range []string{"022", "0"} {
		t.Run("umask "+mask, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestSetupCreatesStorageWithoutOtherAccess$", "-test.count=1")
			cmd.Env = append(os.Environ(), storageUmaskChildEnv+"="+mask)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("child under umask %s failed: %v\n%s", mask, err, out)
			}
		})
	}
}

// Upgrade over an existing world-accessible tree removes only the "other"
// bits of the three top-level paths and touches nothing below them.
func TestUpgradeTightensOnlyOtherBitsOfTopLevelStorage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	plan := modesPlan(root)
	blobDir := filepath.Join(plan.ContentPath, "blobs", "sha256")
	for _, dir := range []string{plan.StorageRoot, plan.ContentPath, blobDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	blob := filepath.Join(blobDir, "blob")
	for path, mode := range map[string]os.FileMode{plan.DatabasePath: 0o644, blob: 0o444} {
		if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}

	var warnings []string
	b := newModesBootstrapper(&warnings)
	if err := b.writeManagedArtifacts(plan, BootstrapReceipt{Mode: supportedMode}, false); err != nil {
		t.Fatalf("writeManagedArtifacts() error = %v", err)
	}

	for path, want := range map[string]os.FileMode{
		plan.StorageRoot:  0o750,
		plan.ContentPath:  0o750,
		plan.DatabasePath: 0o640,
		blobDir:           0o755, // below the top level: never touched
		blob:              0o444,
	} {
		if got := modeOf(t, path); got != want {
			t.Errorf("mode of %s = %#o, want %#o", path, got, want)
		}
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if got, _ := os.ReadFile(plan.DatabasePath); string(got) != "data" {
		t.Errorf("existing database content changed: %q", got)
	}
}

// Without a chmod hook (tests, fakes) nothing is tightened.
func TestUpgradeWithoutChmodHookLeavesModesAlone(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	plan := modesPlan(root)
	if err := os.MkdirAll(plan.ContentPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(plan.StorageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	b := &Bootstrapper{mkdirAll: os.MkdirAll, writeFile: os.WriteFile}
	if err := b.writeManagedArtifacts(plan, BootstrapReceipt{Mode: supportedMode}, false); err != nil {
		t.Fatalf("writeManagedArtifacts() error = %v", err)
	}
	if got := modeOf(t, plan.StorageRoot); got != 0o755 {
		t.Errorf("mode = %#o, want unchanged 0755", got)
	}
}
