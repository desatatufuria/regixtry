package linux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const supportedMode = "daemon-sqlite"

type BootstrapConfig struct {
	Mode        string
	PublicURL   string
	StorageRoot string
	StatePath   string
	UnitPath    string
	Addr        string
	ServiceName string
	BinaryPath  string
	Rollback    bool
}

type BootstrapReceipt struct {
	Mode        string   `json:"mode"`
	Paths       []string `json:"paths"`
	ServiceName string   `json:"service_name"`
}

type Bootstrapper struct {
	detector       detector
	mkdirAll       func(string, os.FileMode) error
	writeFile      func(string, []byte, os.FileMode) error
	readFile       func(string) ([]byte, error)
	removeAll      func(string) error
	runCommand     func(context.Context, string, ...string) error
	probe          func(context.Context, string) (int, error)
	executablePath func() (string, error)
	probeInterval  time.Duration
	probeTimeout   time.Duration
}

func NewBootstrapper() *Bootstrapper {
	client := &http.Client{Timeout: 2 * time.Second}

	return &Bootstrapper{
		detector:  newDetector(),
		mkdirAll:  os.MkdirAll,
		writeFile: os.WriteFile,
		readFile:  os.ReadFile,
		removeAll: os.RemoveAll,
		runCommand: func(ctx context.Context, name string, args ...string) error {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Stdout = ioDiscard{}
			cmd.Stderr = ioDiscard{}
			return cmd.Run()
		},
		probe: func(ctx context.Context, rawURL string) (int, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
			if err != nil {
				return 0, err
			}
			resp, err := client.Do(req)
			if err != nil {
				return 0, err
			}
			defer resp.Body.Close()
			return resp.StatusCode, nil
		},
		executablePath: os.Executable,
		probeInterval:  200 * time.Millisecond,
		probeTimeout:   5 * time.Second,
	}
}

func ValidateConfig(cfg BootstrapConfig) error {
	if err := ValidateMode(cfg.Mode); err != nil {
		return err
	}

	if _, err := normalizeAbsoluteHTTPURL(cfg.PublicURL); err != nil {
		return err
	}

	checks := []struct {
		label string
		value string
	}{
		{label: "storage-root", value: cfg.StorageRoot},
		{label: "state-path", value: cfg.StatePath},
		{label: "unit-path", value: cfg.UnitPath},
		{label: "service", value: cfg.ServiceName},
	}

	for _, check := range checks {
		if strings.TrimSpace(check.value) == "" {
			return fmt.Errorf("%s is required", check.label)
		}
		if containsWhitespace(check.value) {
			return fmt.Errorf("%s must not contain whitespace", check.label)
		}
	}

	return nil
}

func ValidateMode(mode string) error {
	trimmed := strings.TrimSpace(mode)
	if trimmed == "" {
		return errors.New("mode is required")
	}
	if trimmed != supportedMode {
		return fmt.Errorf("unsupported mode %q: only %s is supported", trimmed, supportedMode)
	}
	return nil
}

func (b *Bootstrapper) Run(ctx context.Context, cfg BootstrapConfig) error {
	if err := ValidateConfig(cfg); err != nil {
		return err
	}

	if _, err := b.detector.Detect(); err != nil {
		return err
	}

	plan, receipt, err := b.plan(cfg)
	if err != nil {
		return err
	}

	if err := b.writeArtifacts(plan, receipt); err != nil {
		return err
	}

	rollbackErr := func(runErr error) error {
		cleanupErr := b.rollbackWithReceipt(ctx, receipt)
		if cleanupErr != nil {
			return errors.Join(runErr, cleanupErr)
		}
		return runErr
	}

	if err := b.runCommand(ctx, "systemctl", "daemon-reload"); err != nil {
		return rollbackErr(fmt.Errorf("systemctl daemon-reload: %w", err))
	}
	if err := b.runCommand(ctx, "systemctl", "enable", "--now", receipt.ServiceName+".service"); err != nil {
		return rollbackErr(fmt.Errorf("systemctl enable --now %s.service: %w", receipt.ServiceName, err))
	}
	if err := b.waitUntilReachable(ctx, plan.PublicURL); err != nil {
		return rollbackErr(err)
	}

	return nil
}

func (b *Bootstrapper) Rollback(ctx context.Context, cfg BootstrapConfig) error {
	if strings.TrimSpace(cfg.StatePath) == "" {
		return errors.New("state-path is required")
	}

	receipt, err := b.readReceipt(cfg.StatePath)
	if err != nil {
		return err
	}

	return b.rollbackWithReceipt(ctx, receipt)
}

func (b *Bootstrapper) plan(cfg BootstrapConfig) (BootstrapPlan, BootstrapReceipt, error) {
	binaryPath := strings.TrimSpace(cfg.BinaryPath)
	if binaryPath == "" {
		var err error
		binaryPath, err = b.executablePath()
		if err != nil {
			return BootstrapPlan{}, BootstrapReceipt{}, fmt.Errorf("locate registry executable: %w", err)
		}
	}
	if containsWhitespace(binaryPath) {
		return BootstrapPlan{}, BootstrapReceipt{}, errors.New("binary-path must not contain whitespace")
	}

	storageRoot := strings.TrimSpace(cfg.StorageRoot)
	statePath := strings.TrimSpace(cfg.StatePath)
	unitPath := strings.TrimSpace(cfg.UnitPath)
	serviceName := strings.TrimSpace(cfg.ServiceName)
	if unitPath == "" {
		unitPath = filepath.Join("/etc/systemd/system", serviceName+".service")
	}
	plan := BootstrapPlan{
		Mode:         strings.TrimSpace(cfg.Mode),
		Addr:         strings.TrimSpace(cfg.Addr),
		PublicURL:    strings.TrimSpace(cfg.PublicURL),
		StorageRoot:  storageRoot,
		DatabasePath: filepath.Join(storageRoot, "metadata.db"),
		ContentPath:  filepath.Join(storageRoot, "content"),
		StatePath:    statePath,
		EnvPath:      filepath.Join(filepath.Dir(statePath), "registry.env"),
		UnitPath:     unitPath,
		BinaryPath:   binaryPath,
		ServiceName:  serviceName,
	}

	receipt := BootstrapReceipt{
		Mode:        plan.Mode,
		ServiceName: plan.ServiceName,
		Paths: []string{
			plan.EnvPath,
			plan.UnitPath,
			plan.DatabasePath,
			plan.ContentPath,
			plan.StatePath,
		},
	}

	return plan, receipt, nil
}

func (b *Bootstrapper) writeArtifacts(plan BootstrapPlan, receipt BootstrapReceipt) error {
	for _, dir := range []string{filepath.Dir(plan.EnvPath), filepath.Dir(plan.UnitPath), plan.StorageRoot, plan.ContentPath} {
		if err := b.mkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	if err := b.writeFile(plan.EnvPath, []byte(RenderEnvFile(plan)), 0o644); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}
	if err := b.writeFile(plan.UnitPath, []byte(RenderSystemdUnit(plan)), 0o644); err != nil {
		return fmt.Errorf("write service unit: %w", err)
	}
	if err := b.writeFile(plan.DatabasePath, []byte{}, 0o644); err != nil {
		return fmt.Errorf("create SQLite database file: %w", err)
	}
	receiptBytes, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bootstrap receipt: %w", err)
	}
	if err := b.writeFile(plan.StatePath, append(receiptBytes, '\n'), 0o644); err != nil {
		return fmt.Errorf("write bootstrap receipt: %w", err)
	}

	return nil
}

func (b *Bootstrapper) readReceipt(statePath string) (BootstrapReceipt, error) {
	body, err := b.readFile(statePath)
	if err != nil {
		return BootstrapReceipt{}, fmt.Errorf("read bootstrap receipt: %w", err)
	}

	var receipt BootstrapReceipt
	if err := json.Unmarshal(body, &receipt); err != nil {
		return BootstrapReceipt{}, fmt.Errorf("decode bootstrap receipt: %w", err)
	}
	if strings.TrimSpace(receipt.ServiceName) == "" {
		return BootstrapReceipt{}, errors.New("bootstrap receipt is missing service_name")
	}
	return receipt, nil
}

func (b *Bootstrapper) rollbackWithReceipt(ctx context.Context, receipt BootstrapReceipt) error {
	var errs []error
	if err := b.runCommand(ctx, "systemctl", "disable", "--now", receipt.ServiceName+".service"); err != nil {
		errs = append(errs, fmt.Errorf("systemctl disable --now %s.service: %w", receipt.ServiceName, err))
	}

	for i := len(receipt.Paths) - 1; i >= 0; i-- {
		if err := b.removeAll(receipt.Paths[i]); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", receipt.Paths[i], err))
		}
	}

	return errors.Join(errs...)
}

func (b *Bootstrapper) waitUntilReachable(ctx context.Context, publicURL string) error {
	parsed, err := normalizeAbsoluteHTTPURL(publicURL)
	if err != nil {
		return err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v2/"
	parsed.RawPath = ""

	deadline := time.Now().Add(b.probeTimeout)
	for {
		statusCode, probeErr := b.probe(ctx, parsed.String())
		if probeErr == nil && (statusCode == http.StatusOK || statusCode == http.StatusUnauthorized) {
			return nil
		}
		if time.Now().After(deadline) {
			if probeErr != nil {
				return fmt.Errorf("registry readiness probe failed: %w", probeErr)
			}
			return fmt.Errorf("registry readiness probe returned %d, want 200 or 401", statusCode)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(b.probeInterval):
		}
	}
}

func normalizeAbsoluteHTTPURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("public URL is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse public URL: %w", err)
	}
	if !parsed.IsAbs() || strings.TrimSpace(parsed.Host) == "" {
		return nil, errors.New("public URL must be an absolute http(s) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("public URL must use http or https")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return parsed, nil
}

func containsWhitespace(value string) bool {
	for _, r := range value {
		if r == ' ' || r == '\t' || r == '\n' {
			return true
		}
	}
	return false
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
