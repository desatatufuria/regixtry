package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRequireProjectDefaultsToOnAndHonorsExplicitOptOut(t *testing.T) {
	tests := []struct {
		name string
		env  string
		args []string
		want bool
	}{
		{name: "default is on", want: true},
		{name: "env false disables", env: "false", want: false},
		{name: "env true keeps it on", env: "true", want: true},
		{name: "flag false disables", args: []string{"-require-project=false"}, want: false},
		{name: "flag overrides env", env: "false", args: []string{"-require-project=true"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("REGISTRY_REQUIRE_PROJECT", tt.env)
			cfg, err := parseServeConfig(tt.args)
			if err != nil {
				t.Fatalf("parseServeConfig() error = %v", err)
			}
			if cfg.RequireProject != tt.want {
				t.Fatalf("RequireProject = %v, want %v", cfg.RequireProject, tt.want)
			}
		})
	}
}

func TestProjectEnforcementLogLineStatesTheEffectiveMode(t *testing.T) {
	on := projectEnforcementLogLine(true)
	if !strings.HasPrefix(on, "project enforcement: on") || !strings.Contains(on, "admin API") || !strings.Contains(on, "TUI") {
		t.Fatalf("on line = %q", on)
	}
	if off := projectEnforcementLogLine(false); !strings.HasPrefix(off, "project enforcement: off") {
		t.Fatalf("off line = %q", off)
	}
}

func TestServePrintsTheProjectEnforcementModeBeforeServing(t *testing.T) {
	for _, tt := range []struct {
		require bool
		want    string
	}{{true, "project enforcement: on"}, {false, "project enforcement: off"}} {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Listen() error = %v", err)
		}
		stdout := &bytes.Buffer{}
		ctx, cancel := context.WithCancel(context.Background())
		root := t.TempDir()
		errCh := make(chan error, 1)
		go func() {
			errCh <- serve(ctx, listener, serveConfig{
				StorageRoot: root, DatabasePath: filepath.Join(root, "registry.db"),
				AllowAnonymousPull: true, RequireProject: tt.require,
			}, stdout)
		}()
		deadline := time.Now().Add(5 * time.Second)
		for {
			response, requestErr := http.Get("http://" + listener.Addr().String() + "/v2/")
			if requestErr == nil {
				response.Body.Close()
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
		out := stdout.String()
		i, j := strings.Index(out, tt.want), strings.Index(out, "regixtry serving on")
		if i < 0 || j < 0 || i > j {
			t.Fatalf("stdout = %q, want %q before the serving line", out, tt.want)
		}
	}
}

// startUploadStatus pushes through the real handler built by newHandler from
// the parsed flags, so the default itself is what is exercised.
func startUploadStatus(t *testing.T, env string, name string) int {
	t.Helper()
	t.Setenv("REGISTRY_REQUIRE_PROJECT", env)
	root := t.TempDir()
	cfg, err := parseServeConfig([]string{"-storage-root", root, "-db", filepath.Join(root, "registry.db"), "-allow-anonymous-push"})
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}
	handler, cleanup, err := newHandler(cfg)
	if err != nil {
		t.Fatalf("newHandler() error = %v", err)
	}
	defer cleanup()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v2/"+name+"/blobs/uploads/", nil))
	return recorder.Code
}

func TestDefaultEnforcementRefusesAnUnknownProjectAndOptOutKeepsTheLegacyPath(t *testing.T) {
	if got := startUploadStatus(t, "", "ghost/app"); got != http.StatusNotFound {
		t.Fatalf("default: unknown project status = %d, want 404 NAME_UNKNOWN", got)
	}
	if got := startUploadStatus(t, "", "flatname"); got != http.StatusBadRequest {
		t.Fatalf("default: new flat name status = %d, want 400 NAME_INVALID", got)
	}
	for _, name := range []string{"ghost/app", "flatname"} {
		if got := startUploadStatus(t, "false", name); got != http.StatusAccepted {
			t.Fatalf("opt-out: %s status = %d, want 202 (previous behavior)", name, got)
		}
	}
}
