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

func TestRequireProjectEffectiveDefaultDependsOnTheAdminAPI(t *testing.T) {
	const dsn = "postgres://u:p@h:5432/db"
	tests := []struct {
		name     string
		env      string
		dsnEnv   string
		args     []string
		want     bool
		explicit bool
		wantErr  string
	}{
		{name: "default without DSN is off", want: false},
		{name: "default with DSN flag is on", args: []string{"-auth-postgres-dsn", dsn}, want: true},
		{name: "default with DSN env is on", dsnEnv: dsn, want: true},
		{name: "explicit false without DSN", args: []string{"-require-project=false"}, want: false, explicit: true},
		{name: "explicit false with DSN", args: []string{"-require-project=false", "-auth-postgres-dsn", dsn}, want: false, explicit: true},
		{name: "explicit true with DSN", args: []string{"-require-project=true", "-auth-postgres-dsn", dsn}, want: true, explicit: true},
		{name: "explicit true without DSN fails", args: []string{"-require-project=true"}, wantErr: "project enforcement needs the admin API: configure -auth-postgres-dsn or set -require-project=false"},
		{name: "env true without DSN fails", env: "true", wantErr: "project enforcement needs the admin API"},
		{name: "env true with DSN", env: "true", dsnEnv: dsn, want: true, explicit: true},
		{name: "env false with DSN", env: "false", dsnEnv: dsn, want: false, explicit: true},
		{name: "env false without DSN", env: "false", want: false, explicit: true},
		{name: "flag overrides env false", env: "false", args: []string{"-require-project=true", "-auth-postgres-dsn", dsn}, want: true, explicit: true},
		{name: "flag false overrides env true without DSN", env: "true", args: []string{"-require-project=false"}, want: false, explicit: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("REGISTRY_REQUIRE_PROJECT", tt.env)
			t.Setenv("REGISTRY_AUTH_POSTGRES_DSN", tt.dsnEnv)
			cfg, err := parseServeConfig(tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseServeConfig() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseServeConfig() error = %v", err)
			}
			if cfg.RequireProject != tt.want || cfg.RequireProjectExplicit != tt.explicit {
				t.Fatalf("RequireProject=%v explicit=%v, want %v %v", cfg.RequireProject, cfg.RequireProjectExplicit, tt.want, tt.explicit)
			}
		})
	}
}

func TestProjectEnforcementLogLineStatesTheEffectiveModeAndWhy(t *testing.T) {
	for _, tt := range []struct {
		require, explicit bool
		want              string
	}{
		{true, false, "project enforcement: on (default: admin API available)"},
		{false, false, "project enforcement: off (default: no admin API, create-project is unavailable without -auth-postgres-dsn)"},
		{true, true, "project enforcement: on (explicit)"},
		{false, true, "project enforcement: off (explicit)"},
	} {
		if got := projectEnforcementLogLine(tt.require, tt.explicit); got != tt.want {
			t.Fatalf("line(%v,%v) = %q, want %q", tt.require, tt.explicit, got, tt.want)
		}
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
				AllowAnonymousPull: true, RequireProject: tt.require, RequireProjectExplicit: true,
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
// the parsed flags (anonymous mode), optionally forcing enforcement on the way
// the effective default does when auth is configured.
func startUploadStatus(t *testing.T, env string, forceOn bool, name string) int {
	t.Helper()
	t.Setenv("REGISTRY_REQUIRE_PROJECT", env)
	t.Setenv("REGISTRY_AUTH_POSTGRES_DSN", "")
	root := t.TempDir()
	cfg, err := parseServeConfig([]string{"-storage-root", root, "-db", filepath.Join(root, "registry.db"), "-allow-anonymous-push"})
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}
	if forceOn {
		cfg.RequireProject = true
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

func TestAnonymousDefaultKeepsThePreviousPushBehaviorAndEnforcementRefuses(t *testing.T) {
	// Differential: anonymous default and explicit false are the pre-projects behavior.
	for _, env := range []string{"", "false"} {
		for _, name := range []string{"ghost/app", "flatname"} {
			if got := startUploadStatus(t, env, false, name); got != http.StatusAccepted {
				t.Fatalf("env=%q %s status = %d, want 202 (previous behavior)", env, name, got)
			}
		}
	}
	if got := startUploadStatus(t, "", true, "ghost/app"); got != http.StatusNotFound {
		t.Fatalf("enforced: unknown project status = %d, want 404 NAME_UNKNOWN", got)
	}
	if got := startUploadStatus(t, "", true, "flatname"); got != http.StatusBadRequest {
		t.Fatalf("enforced: new flat name status = %d, want 400 NAME_INVALID", got)
	}
}
