package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"regixtry/internal/ports"
)

// setup must refuse an existing install before anything else happens: no auth
// bootstrap, no bootstrap Run, no rollback (which would touch the paths).
func TestRunSetupRefusesExistingInstallBeforeAnySideEffect(t *testing.T) {
	runner := &stubBootstrapRunner{freshInstallErr: errors.New("refusing to set up over an existing install: /var/lib/regixtry/metadata.db contains data; run `regixtry upgrade`")}
	defer swapBootstrapRunner(t, runner)()

	authOpened := 0
	previous := openAuthStore
	openAuthStore = func(string) (ports.AuthStore, error) {
		authOpened++
		return nil, errors.New("auth store must not be opened")
	}
	defer func() { openAuthStore = previous }()

	err := runWithIO(context.Background(), []string{
		"setup", "-mode", "daemon-sqlite", "-public-url", "http://127.0.0.1:5000",
		"-auth-postgres-dsn", "postgres://u:p@127.0.0.1:1/db", "-admin-password", "change-me-now",
	}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "regixtry upgrade") {
		t.Fatalf("runWithIO(setup) error = %v, want the refusal pointing at `regixtry upgrade`", err)
	}
	if runner.freshInstallCalls != 1 {
		t.Fatalf("freshInstallCalls = %d, want 1", runner.freshInstallCalls)
	}
	if authOpened != 0 {
		t.Fatalf("auth store opened %d times, want 0 before the refusal", authOpened)
	}
	if runner.runCalls != 0 || runner.rollbackCalls != 0 || runner.saveProvenanceCalls != 0 {
		t.Fatalf("runCalls=%d rollbackCalls=%d saveProvenanceCalls=%d, want none", runner.runCalls, runner.rollbackCalls, runner.saveProvenanceCalls)
	}
}

// End to end with the real bootstrapper against a temp-dir fake root: an
// accidental `setup` over a populated install changes nothing.
func TestRunSetupWithRealBootstrapperLeavesExistingInstallByteIdentical(t *testing.T) {
	root := t.TempDir()
	storage := filepath.Join(root, "var", "lib", "regixtry")
	db := filepath.Join(storage, "metadata.db")
	blob := filepath.Join(storage, "content", "ab", "blob")
	for path, body := range map[string]string{db: "SQLite format 3 data", blob: "layer bytes"} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state := filepath.Join(root, "etc", "regixtry", "bootstrap-state.json")

	for _, noStart := range []bool{false, true} {
		args := []string{
			"setup", "-mode", "daemon-sqlite", "-public-url", "http://127.0.0.1:5000",
			"-storage-root", storage, "-state-path", state,
			"-unit-path", filepath.Join(root, "etc", "systemd", "system", "regixtry.service"),
		}
		if noStart {
			args = append(args, "-no-start")
		}
		err := runWithIO(context.Background(), args, strings.NewReader(""), io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), db) || !strings.Contains(err.Error(), "regixtry upgrade") {
			t.Fatalf("setup (no-start=%v) error = %v, want a refusal naming %s and `regixtry upgrade`", noStart, err, db)
		}
		for path, want := range map[string]string{db: "SQLite format 3 data", blob: "layer bytes"} {
			if got, readErr := os.ReadFile(path); readErr != nil || string(got) != want {
				t.Fatalf("%s = %q, %v; want %q untouched", path, got, readErr, want)
			}
		}
		if _, statErr := os.Stat(state); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("receipt written by a refused setup, stat error = %v", statErr)
		}
	}
}
