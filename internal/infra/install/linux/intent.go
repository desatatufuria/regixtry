package linux

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const bootstrapReceiptFileName = "bootstrap-state.json"

type InstalledIntent struct {
	Mode               string
	Addr               string
	PublicURL          string
	RuntimeTLSMode     string
	TLSCertFile        string
	TLSKeyFile         string
	AuthPostgresDSN    string
	StorageRoot        string
	DatabasePath       string
	ContentPath        string
	BootstrapStatePath string
	EnvPath            string
	UnitPath           string
	BinaryPath         string
	ServiceName        string
	InstalledRef       string
	InstalledVersion   string
}

type MissingIntentError struct {
	Fields []string
}

func (e MissingIntentError) Error() string {
	return fmt.Sprintf("upgrade intent is missing required values: %s", strings.Join(e.Fields, ", "))
}

func ReadManagedEnvFile(path string) (map[string]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read managed runtime env: %w", err)
	}
	return parseManagedEnvFile(body)
}

func parseManagedEnvFile(body []byte) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, rawValue, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		parsedValue, err := parseManagedEnvValue(rawValue)
		if err != nil {
			return nil, fmt.Errorf("parse managed runtime env %q: %w", strings.TrimSpace(key), err)
		}
		values[strings.TrimSpace(key)] = parsedValue
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan managed runtime env: %w", err)
	}
	return values, nil
}

func parseManagedEnvValue(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if unquoted, err := strconv.Unquote(trimmed); err == nil {
		return unquoted, nil
	}
	return trimmed, nil
}

func loadInstalledIntent(provenance LifecycleProvenance, envValues map[string]string) (InstalledIntent, error) {
	intent := InstalledIntent{
		Mode:               provenance.Mode,
		BinaryPath:         firstNonBlank(provenance.InstalledBin, provenance.Intent.BinaryPath),
		ServiceName:        firstNonBlank(provenance.ServiceName, provenance.Intent.ServiceName, envValues["REGISTRY_SERVICE_NAME"]),
		InstalledRef:       strings.TrimSpace(provenance.InstalledRef),
		InstalledVersion:   strings.TrimSpace(provenance.InstalledVersion),
		EnvPath:            firstNonBlank(provenance.Intent.EnvPath, managedPathMatch(provenance.ManagedPaths, func(path string) bool { return filepath.Base(path) == "regixtry.env" }), filepath.Join(filepath.Dir(provenance.StatePath), "regixtry.env")),
		UnitPath:           firstNonBlank(provenance.Intent.UnitPath, managedPathMatch(provenance.ManagedPaths, func(path string) bool { return strings.HasSuffix(path, ".service") })),
		BootstrapStatePath: firstNonBlank(provenance.Intent.BootstrapStatePath, managedPathMatch(provenance.ManagedPaths, func(path string) bool { return filepath.Base(path) == bootstrapReceiptFileName }), filepath.Join(filepath.Dir(provenance.StatePath), bootstrapReceiptFileName)),
		ContentPath:        firstNonBlank(provenance.Intent.ContentPath, managedPathMatch(provenance.ManagedPaths, func(path string) bool { return filepath.Base(path) == "content" })),
	}

	intent.StorageRoot = firstNonBlank(provenance.Intent.StorageRoot, envValues["REGISTRY_STORAGE_ROOT"])
	intent.DatabasePath = firstNonBlank(provenance.Intent.DatabasePath, envValues["REGISTRY_DATABASE_PATH"], managedPathMatch(provenance.ManagedPaths, func(path string) bool { return filepath.Base(path) == "metadata.db" }))
	if intent.StorageRoot == "" && intent.DatabasePath != "" {
		intent.StorageRoot = filepath.Dir(intent.DatabasePath)
	}
	if intent.DatabasePath == "" && intent.StorageRoot != "" {
		intent.DatabasePath = filepath.Join(intent.StorageRoot, "metadata.db")
	}
	if intent.ContentPath == "" && intent.StorageRoot != "" {
		intent.ContentPath = filepath.Join(intent.StorageRoot, "content")
	}

	intent.Addr = firstNonBlank(provenance.Intent.Addr, envValues["REGISTRY_ADDR"])
	intent.PublicURL = firstNonBlank(provenance.Intent.PublicURL, envValues["REGISTRY_PUBLIC_URL"])
	intent.TLSCertFile = firstNonBlank(provenance.Intent.TLSCertFile, envValues["REGISTRY_TLS_CERT_FILE"])
	intent.TLSKeyFile = firstNonBlank(provenance.Intent.TLSKeyFile, envValues["REGISTRY_TLS_KEY_FILE"])
	intent.AuthPostgresDSN = firstNonBlank(provenance.Intent.AuthPostgresDSN, envValues["REGISTRY_AUTH_POSTGRES_DSN"])
	intent.RuntimeTLSMode = firstNonBlank(provenance.Intent.RuntimeTLSMode)
	if intent.RuntimeTLSMode == "" && intent.PublicURL != "" {
		resolvedMode, normalizedPublicURL, normalizedTLSCertFile, normalizedTLSKeyFile, err := ResolveRuntimeTLSMode("", intent.PublicURL, intent.TLSCertFile, intent.TLSKeyFile)
		if err != nil {
			return InstalledIntent{}, err
		}
		intent.RuntimeTLSMode = resolvedMode
		intent.PublicURL = normalizedPublicURL
		intent.TLSCertFile = normalizedTLSCertFile
		intent.TLSKeyFile = normalizedTLSKeyFile
	}

	missing := missingIntentFields(intent)
	if len(missing) > 0 {
		return InstalledIntent{}, MissingIntentError{Fields: missing}
	}

	return intent, nil
}

func buildPlanFromInstalledIntent(intent InstalledIntent) BootstrapPlan {
	return BootstrapPlan{
		Mode:            intent.Mode,
		Addr:            intent.Addr,
		PublicURL:       intent.PublicURL,
		RuntimeTLSMode:  intent.RuntimeTLSMode,
		TLSCertFile:     intent.TLSCertFile,
		TLSKeyFile:      intent.TLSKeyFile,
		AuthPostgresDSN: intent.AuthPostgresDSN,
		StorageRoot:     intent.StorageRoot,
		DatabasePath:    intent.DatabasePath,
		ContentPath:     intent.ContentPath,
		StatePath:       intent.BootstrapStatePath,
		EnvPath:         intent.EnvPath,
		UnitPath:        intent.UnitPath,
		BinaryPath:      intent.BinaryPath,
		ServiceName:     intent.ServiceName,
	}
}

func managedPathMatch(paths []string, match func(string) bool) string {
	for _, raw := range paths {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if match(trimmed) {
			return trimmed
		}
	}
	return ""
}

func missingIntentFields(intent InstalledIntent) []string {
	checks := []struct {
		name  string
		value string
	}{
		{name: "binary_path", value: intent.BinaryPath},
		{name: "service_name", value: intent.ServiceName},
		{name: "storage_root", value: intent.StorageRoot},
		{name: "database_path", value: intent.DatabasePath},
		{name: "env_path", value: intent.EnvPath},
		{name: "unit_path", value: intent.UnitPath},
		{name: "bootstrap_state_path", value: intent.BootstrapStatePath},
		{name: "addr", value: intent.Addr},
		{name: "public_url", value: intent.PublicURL},
	}
	missing := make([]string, 0)
	for _, check := range checks {
		if strings.TrimSpace(check.value) == "" {
			missing = append(missing, check.name)
		}
	}
	return missing
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
