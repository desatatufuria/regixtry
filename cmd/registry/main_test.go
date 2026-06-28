package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServeStartsAndRespondsToPing(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout := &bytes.Buffer{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(ctx, listener, serveConfig{
			StorageRoot:        t.TempDir(),
			DatabasePath:       filepath.Join(t.TempDir(), "registry.db"),
			AllowAnonymousPull: true,
		}, stdout)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		response, requestErr := http.Get("http://" + listener.Addr().String() + "/v2/")
		if requestErr == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
			}
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

	if !strings.Contains(stdout.String(), "registry serving on") {
		t.Fatalf("stdout = %q, want start message", stdout.String())
	}
}

func TestRunTUIReportsPlaceholder(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	err := runTUI(stdout)
	if err == nil {
		t.Fatal("expected not implemented error")
	}

	if !errors.Is(err, errors.New("tui command is not implemented yet")) && !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("err = %v, want not implemented", err)
	}

	if !strings.Contains(stdout.String(), "Phase 4") {
		t.Fatalf("stdout = %q, want Phase 4 placeholder", stdout.String())
	}
}
