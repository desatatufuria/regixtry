package trivy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"regixtry/internal/ports"
)

type RunnerConfig struct {
	Client *http.Client
}

type Runner struct {
	client *http.Client
}

func New(cfg RunnerConfig) *Runner {
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Runner{client: client}
}

func (r *Runner) Probe(ctx context.Context, settings ports.ScanSettings) (ports.FeatureRuntime, error) {
	if r == nil {
		return ports.FeatureRuntime{}, fmt.Errorf("trivy runner is not configured")
	}
	client, err := r.clientForSettings(settings)
	if err != nil {
		return ports.FeatureRuntime{}, err
	}
	if err := r.expectStatus(ctx, client, settings, http.MethodGet, "/healthz", http.StatusOK); err != nil {
		return ports.FeatureRuntime{Mode: string(ports.FeatureKindExternalService), Health: "degraded", Detail: err.Error()}, err
	}
	body, err := r.do(ctx, client, settings, http.MethodGet, "/version", nil)
	if err != nil {
		return ports.FeatureRuntime{Mode: string(ports.FeatureKindExternalService), Health: "degraded", Detail: err.Error()}, err
	}
	defer body.Close()
	var payload struct {
		Version string `json:"Version"`
	}
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return ports.FeatureRuntime{Mode: string(ports.FeatureKindExternalService), Health: "degraded", Detail: fmt.Sprintf("decode version response: %v", err)}, fmt.Errorf("decode version response: %w", err)
	}
	version := strings.TrimSpace(payload.Version)
	if version == "" {
		return ports.FeatureRuntime{Mode: string(ports.FeatureKindExternalService), Health: "degraded", Detail: "version response did not include Version"}, fmt.Errorf("version response did not include Version")
	}
	return ports.FeatureRuntime{Mode: string(ports.FeatureKindExternalService), Health: "ready", Version: version, Detail: "service reachable"}, nil
}

func (r *Runner) Run(ctx context.Context, imageRef string, settings ports.ScanSettings) (ports.ScanResult, error) {
	if r == nil {
		return ports.ScanResult{}, fmt.Errorf("trivy runner is not configured")
	}
	timeout := settings.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client, err := r.clientForSettings(settings)
	if err != nil {
		return ports.ScanResult{}, err
	}
	runtime, err := r.Probe(runCtx, settings)
	if err != nil {
		return ports.ScanResult{}, err
	}
	payload := map[string]string{"target": strings.TrimSpace(imageRef)}
	bodyReader, err := encodeJSON(payload)
	if err != nil {
		return ports.ScanResult{}, err
	}
	body, err := r.do(runCtx, client, settings, http.MethodPost, "/scan", bodyReader)
	if err != nil {
		return ports.ScanResult{}, err
	}
	defer body.Close()
	var payloadResult struct {
		Metadata struct {
			DBUpdatedAt *time.Time `json:"DBUpdatedAt"`
		} `json:"Metadata"`
		Results []struct {
			Vulnerabilities []struct {
				Severity string `json:"Severity"`
			} `json:"Vulnerabilities"`
		} `json:"Results"`
	}
	if err := json.NewDecoder(body).Decode(&payloadResult); err != nil {
		return ports.ScanResult{}, fmt.Errorf("decode trivy output: %w", err)
	}
	result := ports.ScanResult{TrivyVersion: runtime.Version, DBUpdatedAt: payloadResult.Metadata.DBUpdatedAt}
	for _, section := range payloadResult.Results {
		for _, vulnerability := range section.Vulnerabilities {
			switch strings.ToUpper(strings.TrimSpace(vulnerability.Severity)) {
			case "CRITICAL":
				result.Critical++
			case "HIGH":
				result.High++
			case "MEDIUM":
				result.Medium++
			case "LOW":
				result.Low++
			}
		}
	}
	return result, nil
}

func (r *Runner) expectStatus(ctx context.Context, client *http.Client, settings ports.ScanSettings, method string, path string, want int) error {
	body, err := r.do(ctx, client, settings, method, path, nil)
	if err != nil {
		return err
	}
	defer body.Close()
	return nil
}

func (r *Runner) do(ctx context.Context, client *http.Client, settings ports.ScanSettings, method string, path string, body io.Reader) (io.ReadCloser, error) {
	serviceURL := strings.TrimRight(strings.TrimSpace(settings.ServiceURL), "/")
	if serviceURL == "" {
		return nil, fmt.Errorf("service_url is required")
	}
	req, err := http.NewRequestWithContext(ctx, method, serviceURL+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token := strings.TrimSpace(settings.AuthToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		endpoint := strings.TrimPrefix(path, "/")
		return nil, fmt.Errorf("%s returned %d", endpoint, resp.StatusCode)
	}
	return resp.Body, nil
}

func (r *Runner) clientForSettings(settings ports.ScanSettings) (*http.Client, error) {
	base := r.client
	if base == nil {
		base = &http.Client{Timeout: 30 * time.Second}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if settings.TLSInsecureSkipVerify || strings.TrimSpace(settings.TLSCACertPath) != "" {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: settings.TLSInsecureSkipVerify}
	}
	if path := strings.TrimSpace(settings.TLSCACertPath); path != "" {
		pool, err := loadCertPool(path)
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig.RootCAs = pool
	}
	return &http.Client{Timeout: base.Timeout, Transport: transport}, nil
}

func loadCertPool(path string) (*x509.CertPool, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read TLS CA cert: %w", err)
	}
	pool := x509.NewCertPool()
	for len(body) > 0 {
		block, rest := pem.Decode(body)
		body = rest
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("parse TLS CA cert: %w", err)
			}
			pool.AddCert(cert)
		}
	}
	return pool, nil
}

func encodeJSON(value any) (io.Reader, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return strings.NewReader(string(payload)), nil
}
