package linux

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	supportedMode              = "daemon-sqlite"
	RuntimeTLSModeLocalHTTP    = "local-http"
	RuntimeTLSModeReverseProxy = "reverse-proxy"
	RuntimeTLSModeDirectTLS    = "direct-tls"
)

type BootstrapConfig struct {
	Mode                       string
	PublicURL                  string
	RuntimeTLSMode             string
	TLSCertFile                string
	TLSKeyFile                 string
	AuthPostgresDSN            string
	StorageRoot                string
	StatePath                  string
	UnitPath                   string
	Addr                       string
	ServiceName                string
	BinaryPath                 string
	TrivyEnabled               bool
	TrivyScheduleEnabled       bool
	TrivyInterval              time.Duration
	TrivyTimeout               time.Duration
	TrivyServiceURL            string
	TrivyRegistryReachableURL  string
	TrivyAuthToken             string
	TrivyTLSCACertPath         string
	TrivyTLSInsecureSkipVerify bool
	TrivyCacheDir              string
	TrivyBinaryPath            string
	TrivyMaxConcurrency        int
	NoStart                    bool
	Rollback                   bool
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
	stat           func(string) (os.FileInfo, error)
	rename         func(string, string) error
	removeAll      func(string) error
	listen         func(string, string) (net.Listener, error)
	runCommand     func(context.Context, string, ...string) error
	probe          func(context.Context, string, bool) (int, error)
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
		stat:      os.Stat,
		rename:    os.Rename,
		removeAll: os.RemoveAll,
		listen:    net.Listen,
		runCommand: func(ctx context.Context, name string, args ...string) error {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Stdout = ioDiscard{}
			cmd.Stderr = ioDiscard{}
			return cmd.Run()
		},
		probe: func(ctx context.Context, rawURL string, insecureTLS bool) (int, error) {
			clientToUse := client
			if insecureTLS {
				clientToUse = &http.Client{
					Timeout: client.Timeout,
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
					},
				}
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
			if err != nil {
				return 0, err
			}
			resp, err := clientToUse.Do(req)
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

	if _, _, _, _, err := ResolveRuntimeTLSMode(cfg.RuntimeTLSMode, cfg.PublicURL, cfg.TLSCertFile, cfg.TLSKeyFile); err != nil {
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
	if cfg.TrivyTimeout < 0 {
		return errors.New("trivy-timeout must be zero or greater")
	}
	if cfg.TrivyInterval < 0 {
		return errors.New("trivy-interval must be zero or greater")
	}
	if cfg.TrivyMaxConcurrency < 0 {
		return errors.New("trivy-max-concurrency must be zero or greater")
	}

	return nil
}

func ResolveRuntimeTLSMode(mode string, publicURL string, tlsCertFile string, tlsKeyFile string) (resolvedMode string, normalizedPublicURL string, normalizedTLSCertFile string, normalizedTLSKeyFile string, err error) {
	parsedPublicURL, err := normalizeAbsoluteHTTPURL(publicURL)
	if err != nil {
		return "", "", "", "", err
	}

	normalizedTLSCertFile = strings.TrimSpace(tlsCertFile)
	normalizedTLSKeyFile = strings.TrimSpace(tlsKeyFile)
	hasTLSCert := normalizedTLSCertFile != ""
	hasTLSKey := normalizedTLSKeyFile != ""
	if hasTLSCert != hasTLSKey {
		return "", "", "", "", errors.New("TLS cert file and key file must both be set")
	}

	resolvedMode = strings.TrimSpace(mode)
	if resolvedMode == "" {
		switch {
		case hasTLSCert:
			resolvedMode = RuntimeTLSModeDirectTLS
		case parsedPublicURL.Scheme == "https":
			resolvedMode = RuntimeTLSModeReverseProxy
		default:
			resolvedMode = RuntimeTLSModeLocalHTTP
		}
	}

	switch resolvedMode {
	case RuntimeTLSModeLocalHTTP:
		if parsedPublicURL.Scheme != "http" {
			return "", "", "", "", errors.New("local-http runtime TLS mode requires an http public URL")
		}
		if hasTLSCert {
			return "", "", "", "", errors.New("local-http runtime TLS mode cannot be combined with TLS cert/key inputs")
		}
	case RuntimeTLSModeReverseProxy:
		if parsedPublicURL.Scheme != "https" {
			return "", "", "", "", errors.New("reverse-proxy runtime TLS mode requires an https public URL")
		}
		if hasTLSCert {
			return "", "", "", "", errors.New("reverse-proxy runtime TLS mode cannot be combined with TLS cert/key inputs")
		}
	case RuntimeTLSModeDirectTLS:
		if parsedPublicURL.Scheme != "https" {
			return "", "", "", "", errors.New("direct-tls runtime TLS mode requires an https public URL")
		}
		if !hasTLSCert {
			return "", "", "", "", errors.New("direct-tls runtime TLS mode requires TLS cert/key inputs")
		}
	default:
		return "", "", "", "", fmt.Errorf("unsupported runtime TLS mode %q", resolvedMode)
	}

	return resolvedMode, parsedPublicURL.String(), normalizedTLSCertFile, normalizedTLSKeyFile, nil
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

	plan, receipt, _, err := b.plan(cfg)
	if err != nil {
		return err
	}

	if err := b.writeSetupArtifacts(plan, receipt); err != nil {
		return err
	}

	if cfg.NoStart {
		return nil
	}

	if err := b.preflightLocalBind(cfg); err != nil {
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
	if err := b.waitUntilReachable(ctx, plan); err != nil {
		return rollbackErr(err)
	}

	return nil
}

func (b *Bootstrapper) preflightLocalBind(cfg BootstrapConfig) error {
	if !isConfiguredLocalBind(cfg.Addr) {
		return nil
	}

	listener, err := b.listenTCP(strings.TrimSpace(cfg.Addr))
	if err == nil {
		return listener.Close()
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		return occupiedLocalBindError{cfg: cfg}
	}

	return fmt.Errorf("preflight local bind %s: %w", strings.TrimSpace(cfg.Addr), err)
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

func (b *Bootstrapper) Uninstall(ctx context.Context, provenancePath string) (UninstallReport, error) {
	trimmedPath := strings.TrimSpace(provenancePath)
	if trimmedPath == "" {
		return UninstallReport{}, errors.New("provenance path is required")
	}

	provenance, err := b.readLifecycleProvenance(trimmedPath)
	if err != nil {
		return UninstallReport{}, err
	}

	return b.uninstallWithProvenance(ctx, provenance)
}

func (b *Bootstrapper) plan(cfg BootstrapConfig) (BootstrapPlan, BootstrapReceipt, LifecycleProvenance, error) {
	binaryPath := strings.TrimSpace(cfg.BinaryPath)
	if binaryPath == "" {
		var err error
		binaryPath, err = b.executablePath()
		if err != nil {
			return BootstrapPlan{}, BootstrapReceipt{}, LifecycleProvenance{}, fmt.Errorf("locate regixtry executable: %w", err)
		}
	}
	if containsWhitespace(binaryPath) {
		return BootstrapPlan{}, BootstrapReceipt{}, LifecycleProvenance{}, errors.New("binary-path must not contain whitespace")
	}

	storageRoot := strings.TrimSpace(cfg.StorageRoot)
	statePath := strings.TrimSpace(cfg.StatePath)
	unitPath := strings.TrimSpace(cfg.UnitPath)
	serviceName := strings.TrimSpace(cfg.ServiceName)
	authPostgresDSN := strings.TrimSpace(cfg.AuthPostgresDSN)
	runtimeTLSMode, publicURL, tlsCertFile, tlsKeyFile, err := ResolveRuntimeTLSMode(cfg.RuntimeTLSMode, cfg.PublicURL, cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return BootstrapPlan{}, BootstrapReceipt{}, LifecycleProvenance{}, err
	}
	if unitPath == "" {
		unitPath = filepath.Join("/etc/systemd/system", serviceName+".service")
	}
	plan := BootstrapPlan{
		Mode:                 strings.TrimSpace(cfg.Mode),
		Addr:                 strings.TrimSpace(cfg.Addr),
		PublicURL:            publicURL,
		RuntimeTLSMode:       runtimeTLSMode,
		TLSCertFile:          tlsCertFile,
		TLSKeyFile:           tlsKeyFile,
		AuthPostgresDSN:      authPostgresDSN,
		StorageRoot:          storageRoot,
		DatabasePath:         filepath.Join(storageRoot, "metadata.db"),
		ContentPath:          filepath.Join(storageRoot, "content"),
		StatePath:            statePath,
		EnvPath:              filepath.Join(filepath.Dir(statePath), "regixtry.env"),
		UnitPath:             unitPath,
		BinaryPath:           binaryPath,
		ServiceName:          serviceName,
		TrivyEnabled:         cfg.TrivyEnabled,
		TrivyScheduleEnabled: cfg.TrivyScheduleEnabled,
		TrivyInterval:        defaultTrivyInterval(cfg.TrivyInterval),
		TrivyTimeout:         defaultTrivyTimeout(cfg.TrivyTimeout),
		TrivyCacheDir:        defaultTrivyCacheDir(strings.TrimSpace(cfg.TrivyCacheDir), storageRoot),
		TrivyBinaryPath:      defaultTrivyBinaryPath(strings.TrimSpace(cfg.TrivyBinaryPath)),
		TrivyMaxConcurrency:  defaultTrivyMaxConcurrency(cfg.TrivyMaxConcurrency),
	}

	receipt := bootstrapReceiptFromPlan(plan)
	provenance := lifecycleProvenanceFromPlan(plan, receipt)

	return plan, receipt, provenance, nil
}

func bootstrapReceiptFromPlan(plan BootstrapPlan) BootstrapReceipt {
	return BootstrapReceipt{
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
}

func (b *Bootstrapper) writeSetupArtifacts(plan BootstrapPlan, receipt BootstrapReceipt) error {
	return b.writeManagedArtifacts(plan, receipt, true)
}

func (b *Bootstrapper) writeManagedArtifacts(plan BootstrapPlan, receipt BootstrapReceipt, createDatabase bool) error {
	for _, dir := range []string{filepath.Dir(plan.EnvPath), filepath.Dir(plan.UnitPath), plan.StorageRoot, plan.ContentPath} {
		if strings.TrimSpace(dir) == "" {
			continue
		}
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
	if createDatabase {
		if err := b.writeFile(plan.DatabasePath, []byte{}, 0o644); err != nil {
			return fmt.Errorf("create SQLite database file: %w", err)
		}
	}

	return b.writeBootstrapReceipt(plan.StatePath, receipt)
}

func (b *Bootstrapper) writeBootstrapReceipt(statePath string, receipt BootstrapReceipt) error {
	receiptBytes, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bootstrap receipt: %w", err)
	}
	if err := b.writeFile(statePath, append(receiptBytes, '\n'), 0o644); err != nil {
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
		for _, path := range sqliteCleanupPaths(receipt.Paths[i]) {
			if err := b.removeAll(path); err != nil {
				errs = append(errs, fmt.Errorf("remove %s: %w", path, err))
			}
		}
	}

	return errors.Join(errs...)
}

func (b *Bootstrapper) uninstallWithProvenance(ctx context.Context, provenance LifecycleProvenance) (UninstallReport, error) {
	report := UninstallReport{
		Mode:        provenance.Mode,
		ServiceName: provenance.ServiceName,
	}

	var errs []error
	serviceUnit := strings.TrimSpace(provenance.ServiceName)
	if serviceUnit != "" {
		serviceUnit += ".service"
	}
	if err := b.runCommand(ctx, "systemctl", "disable", "--now", serviceUnit); err != nil {
		report.Service = CleanupItem{Path: serviceUnit, Status: CleanupStatusFailed, Detail: err.Error()}
		errs = append(errs, fmt.Errorf("systemctl disable --now %s: %w", serviceUnit, err))
	} else {
		report.Service = CleanupItem{Path: serviceUnit, Status: CleanupStatusRemoved, Detail: "systemd service disabled and stopped"}
	}

	for _, target := range uninstallCleanupTargets(provenance) {
		item, err := b.cleanupPath(target)
		report.Items = append(report.Items, item)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return report, errors.Join(errs...)
}

func (b *Bootstrapper) cleanupPath(target string) (CleanupItem, error) {
	trimmedTarget := strings.TrimSpace(target)
	if trimmedTarget == "" {
		return CleanupItem{Status: CleanupStatusSkipped, Detail: "path is empty"}, nil
	}

	paths := sqliteCleanupPaths(trimmedTarget)
	hadExisting := false
	for _, path := range paths {
		exists, err := b.pathExists(path)
		if err != nil {
			return CleanupItem{Path: trimmedTarget, Status: CleanupStatusFailed, Detail: err.Error()}, fmt.Errorf("stat %s: %w", path, err)
		}
		if !exists {
			continue
		}
		hadExisting = true
		if err := b.removeAll(path); err != nil {
			return CleanupItem{Path: trimmedTarget, Status: CleanupStatusFailed, Detail: err.Error()}, fmt.Errorf("remove %s: %w", path, err)
		}
	}
	if !hadExisting {
		return CleanupItem{Path: trimmedTarget, Status: CleanupStatusMissing, Detail: "path already absent"}, nil
	}

	return CleanupItem{Path: trimmedTarget, Status: CleanupStatusRemoved, Detail: "path removed"}, nil
}

func sqliteCleanupPaths(target string) []string {
	trimmedTarget := strings.TrimSpace(target)
	if trimmedTarget == "" {
		return nil
	}
	if filepath.Ext(trimmedTarget) != ".db" {
		return []string{trimmedTarget}
	}
	return []string{trimmedTarget + "-wal", trimmedTarget + "-shm", trimmedTarget}
}

func (b *Bootstrapper) pathExists(target string) (bool, error) {
	if b.stat != nil {
		if _, err := b.stat(target); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}

	if _, err := os.Stat(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (b *Bootstrapper) waitUntilReachable(ctx context.Context, plan BootstrapPlan) error {
	probeURL, insecureTLS, err := readinessProbeURL(plan)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(b.probeTimeout)
	for {
		statusCode, probeErr := b.probe(ctx, probeURL, insecureTLS)
		if probeErr == nil && (statusCode == http.StatusOK || statusCode == http.StatusUnauthorized) {
			return nil
		}
		if time.Now().After(deadline) {
			if probeErr != nil {
				return fmt.Errorf("regixtry readiness probe failed: %w", probeErr)
			}
			return fmt.Errorf("regixtry readiness probe returned %d, want 200 or 401", statusCode)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(b.probeInterval):
		}
	}
}

func readinessProbeURL(plan BootstrapPlan) (string, bool, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(plan.Addr))
	if err != nil {
		return "", false, fmt.Errorf("parse listen address for readiness probe: %w", err)
	}

	host = strings.TrimSpace(host)
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}

	probeURL := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, port),
		Path:   "/v2/",
	}
	if plan.RuntimeTLSMode == RuntimeTLSModeDirectTLS {
		probeURL.Scheme = "https"
		return probeURL.String(), true, nil
	}

	return probeURL.String(), false, nil
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

func isConfiguredLocalBind(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return false
	}

	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func (b *Bootstrapper) listenTCP(addr string) (net.Listener, error) {
	if b.listen != nil {
		return b.listen("tcp", addr)
	}
	return net.Listen("tcp", addr)
}

type occupiedLocalBindError struct {
	cfg BootstrapConfig
}

func (e occupiedLocalBindError) Error() string {
	port := bindPort(e.cfg.Addr)
	suggestedAddr, suggestedPublicURL := suggestedRecoveryEndpoints(e.cfg.Addr, e.cfg.PublicURL)

	return strings.Join([]string{
		fmt.Sprintf("configured local bind address %s is already in use", strings.TrimSpace(e.cfg.Addr)),
		"Recover with:",
		fmt.Sprintf("  sudo ss -ltnp 'sport = :%s'", port),
		fmt.Sprintf("  sudo systemctl stop %s.service", strings.TrimSpace(e.cfg.ServiceName)),
		fmt.Sprintf("  regixtry bootstrap --mode %s --addr %s --public-url %s --storage-root %s --state-path %s --unit-path %s --service %s", strings.TrimSpace(e.cfg.Mode), suggestedAddr, suggestedPublicURL, strings.TrimSpace(e.cfg.StorageRoot), strings.TrimSpace(e.cfg.StatePath), strings.TrimSpace(e.cfg.UnitPath), strings.TrimSpace(e.cfg.ServiceName)),
	}, "\n")
}

func bindPort(addr string) string {
	_, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil || strings.TrimSpace(port) == "" {
		return "<port>"
	}
	return port
}

func suggestedRecoveryEndpoints(addr string, publicURL string) (string, string) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return "<new-addr>", "<matching-public-url>"
	}

	nextPort, convErr := strconv.Atoi(port)
	if convErr != nil || nextPort <= 0 || nextPort >= 65535 {
		return "<new-addr>", "<matching-public-url>"
	}

	suggestedAddr := net.JoinHostPort(host, strconv.Itoa(nextPort+1))
	parsedURL, parseErr := normalizeAbsoluteHTTPURL(publicURL)
	if parseErr != nil {
		return suggestedAddr, "<matching-public-url>"
	}
	parsedURL.Host = suggestedAddr

	return suggestedAddr, parsedURL.String()
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

func defaultTrivyInterval(value time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return 24 * time.Hour
}

func defaultTrivyTimeout(value time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return 15 * time.Minute
}

func defaultTrivyMaxConcurrency(value int) int {
	if value > 0 {
		return value
	}
	return 1
}

func defaultTrivyCacheDir(value string, storageRoot string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return filepath.Join(strings.TrimSpace(storageRoot), "trivy-cache")
}

func defaultTrivyBinaryPath(value string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return "trivy"
}
