package trivy

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"regixtry/internal/ports"
)

func TestRunnerProbesServiceHealthAndVersionAndExecutesScan(t *testing.T) {
	t.Parallel()

	var authHeader string
	var scanAuthHeader string
	var scanPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			authHeader = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		case "/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Version":"0.57.1"}`))
		case "/scan":
			scanAuthHeader = r.Header.Get("Authorization")
			scanPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Metadata":{"DBUpdatedAt":"2026-08-10T12:00:00Z"},"Results":[{"Vulnerabilities":[{"Severity":"CRITICAL"},{"Severity":"HIGH"},{"Severity":"LOW"}]}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	runner := New(RunnerConfig{})
	settings := ports.ScanSettings{ServiceURL: server.URL, AuthToken: "secret-token", Timeout: time.Minute, RegistryReachableURL: "https://registry.internal:5443"}
	runtime, err := runner.Probe(context.Background(), settings)
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if runtime.Health != "ready" || runtime.Version != "0.57.1" || runtime.Mode != string(ports.FeatureKindExternalService) {
		t.Fatalf("runtime = %#v, want ready external_service with version", runtime)
	}
	if authHeader != "Bearer secret-token" {
		t.Fatalf("health authorization = %q, want bearer token", authHeader)
	}

	result, err := runner.Run(context.Background(), "registry.internal:5443/library/alpine@sha256:abc", settings)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Critical != 1 || result.High != 1 || result.Low != 1 {
		t.Fatalf("result = %#v, want severity counts", result)
	}
	if result.TrivyVersion != "0.57.1" {
		t.Fatalf("result.TrivyVersion = %q, want 0.57.1", result.TrivyVersion)
	}
	if scanAuthHeader != "Bearer secret-token" || scanPath != "/scan" {
		t.Fatalf("scan auth/path = %q %q, want bearer token and /scan", scanAuthHeader, scanPath)
	}
}

func TestRunnerSupportsTLSCAAndInsecureModes(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Version":"0.58.0"}`))
		case "/scan":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Results":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	certFile := writeServerCACert(t, server.Certificate())
	runner := New(RunnerConfig{})
	for _, settings := range []ports.ScanSettings{
		{ServiceURL: server.URL, TLSCACertPath: certFile, Timeout: time.Minute, RegistryReachableURL: "https://registry.internal"},
		{ServiceURL: server.URL, TLSInsecureSkipVerify: true, Timeout: time.Minute, RegistryReachableURL: "https://registry.internal"},
	} {
		if _, err := runner.Probe(context.Background(), settings); err != nil {
			t.Fatalf("Probe() error = %v, want TLS config accepted", err)
		}
	}
}

func TestRunnerRejectsHealthVersionScanAndCancellationFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		run     func(*Runner, string) error
		want    string
	}{
		{
			name: "health failure",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/healthz" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.WriteHeader(http.StatusOK)
			},
			run: func(runner *Runner, serviceURL string) error {
				_, err := runner.Probe(context.Background(), ports.ScanSettings{ServiceURL: serviceURL, Timeout: time.Minute, RegistryReachableURL: "https://registry.internal"})
				return err
			},
			want: "healthz returned 503",
		},
		{
			name: "version failure",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/healthz":
					w.WriteHeader(http.StatusOK)
				case "/version":
					w.WriteHeader(http.StatusBadGateway)
				default:
					w.WriteHeader(http.StatusOK)
				}
			},
			run: func(runner *Runner, serviceURL string) error {
				_, err := runner.Probe(context.Background(), ports.ScanSettings{ServiceURL: serviceURL, Timeout: time.Minute, RegistryReachableURL: "https://registry.internal"})
				return err
			},
			want: "version returned 502",
		},
		{
			name: "malformed scan json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/scan":
					_, _ = w.Write([]byte("{"))
				case "/version":
					_, _ = w.Write([]byte(`{"Version":"0.58.0"}`))
				default:
					w.WriteHeader(http.StatusOK)
				}
			},
			run: func(runner *Runner, serviceURL string) error {
				_, err := runner.Run(context.Background(), "registry.internal/library/alpine@sha256:abc", ports.ScanSettings{ServiceURL: serviceURL, Timeout: time.Minute, RegistryReachableURL: "https://registry.internal"})
				return err
			},
			want: "decode trivy output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			runner := New(RunnerConfig{})
			if err := tt.run(runner, server.URL); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if _, err := New(RunnerConfig{}).Run(canceledCtx, "registry.internal/library/alpine@sha256:abc", ports.ScanSettings{ServiceURL: server.URL, Timeout: time.Minute, RegistryReachableURL: "https://registry.internal"}); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
}

func writeServerCACert(t *testing.T, cert *x509.Certificate) string {
	t.Helper()
	path := t.TempDir() + "/trivy-server-ca.pem"
	pemBody := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	if err := os.WriteFile(path, pemBody, 0o600); err != nil {
		t.Fatalf("WriteFile(ca) error = %v", err)
	}
	return path
}
