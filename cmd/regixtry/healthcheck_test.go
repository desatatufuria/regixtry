package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRunHealthcheckStatusMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		statusCode  int
		wantHealthy bool
	}{
		{name: "200 is healthy", statusCode: http.StatusOK, wantHealthy: true},
		{name: "401 is healthy", statusCode: http.StatusUnauthorized, wantHealthy: true},
		{name: "500 is unhealthy", statusCode: http.StatusInternalServerError, wantHealthy: false},
	}

	for _, testCase := range tests {
		tt := testCase
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			err := runHealthcheck(context.Background(), healthcheckConfig{URL: server.URL, Timeout: time.Second})

			if tt.wantHealthy && err != nil {
				t.Fatalf("runHealthcheck() error = %v, want nil (status %d must be treated healthy)", err, tt.statusCode)
			}
			if !tt.wantHealthy && err == nil {
				t.Fatalf("runHealthcheck() error = nil, want non-nil (status %d must be treated unhealthy)", tt.statusCode)
			}
		})
	}
}

func TestRunHealthcheckConnectionRefusedIsUnhealthy(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("listener.Close() error = %v", err)
	}

	err = runHealthcheck(context.Background(), healthcheckConfig{URL: "http://" + addr + "/v2/", Timeout: time.Second})
	if err == nil {
		t.Fatal("runHealthcheck() error = nil, want non-nil for a refused connection")
	}
}

func TestRunHealthcheckTimeoutIsUnhealthy(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	// Deferred calls run LIFO: close(release) must unblock the handler
	// BEFORE server.Close() blocks waiting for outstanding requests to
	// finish, so it is deferred after (and therefore runs before) Close.
	defer server.Close()
	defer close(release)

	err := runHealthcheck(context.Background(), healthcheckConfig{URL: server.URL, Timeout: 20 * time.Millisecond})
	if err == nil {
		t.Fatal("runHealthcheck() error = nil, want non-nil for a client timeout")
	}
}

func TestParseHealthcheckConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := parseHealthcheckConfig(nil)
	if err != nil {
		t.Fatalf("parseHealthcheckConfig() error = %v", err)
	}

	if got, want := cfg.URL, "http://127.0.0.1:5000/v2/"; got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
	if got, want := cfg.Timeout, 3*time.Second; got != want {
		t.Fatalf("Timeout = %v, want %v", got, want)
	}
}

func TestParseHealthcheckConfigOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := parseHealthcheckConfig([]string{"-url", "http://example.test/v2/", "-timeout", "500ms"})
	if err != nil {
		t.Fatalf("parseHealthcheckConfig() error = %v", err)
	}

	if got, want := cfg.URL, "http://example.test/v2/"; got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
	if got, want := cfg.Timeout, 500*time.Millisecond; got != want {
		t.Fatalf("Timeout = %v, want %v", got, want)
	}
}

func TestRunWithIOHealthcheckWiring(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{name: "200 wired through runWithIO exits healthy", statusCode: http.StatusOK, wantErr: false},
		{name: "500 wired through runWithIO exits unhealthy", statusCode: http.StatusInternalServerError, wantErr: true},
	}

	for _, testCase := range tests {
		tt := testCase
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			err := runWithIO(context.Background(), []string{"healthcheck", "-url", server.URL}, nil, io.Discard, io.Discard)

			if tt.wantErr && err == nil {
				t.Fatal("runWithIO(healthcheck) error = nil, want non-nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("runWithIO(healthcheck) error = %v, want nil", err)
			}
		})
	}
}

func TestRunWithIONoArgsListsHealthcheckSubcommand(t *testing.T) {
	t.Parallel()

	err := runWithIO(context.Background(), nil, nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("runWithIO() error = nil, want non-nil for no args")
	}

	want := "expected subcommand: serve, tui, bootstrap, bootstrap-admin, setup, feature, uninstall, upgrade, or healthcheck"
	if got := err.Error(); got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}
