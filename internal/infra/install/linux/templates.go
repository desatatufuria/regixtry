package linux

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type BootstrapPlan struct {
	Mode                 string
	Addr                 string
	PublicURL            string
	RuntimeTLSMode       string
	TLSCertFile          string
	TLSKeyFile           string
	AuthPostgresDSN      string
	StorageRoot          string
	DatabasePath         string
	ContentPath          string
	StatePath            string
	EnvPath              string
	UnitPath             string
	BinaryPath           string
	ServiceName          string
	TrivyEnabled         bool
	TrivyScheduleEnabled bool
	TrivyInterval        time.Duration
	TrivyTimeout         time.Duration
	TrivyCacheDir        string
	TrivyBinaryPath      string
	TrivyMaxConcurrency  int
}

func RenderEnvFile(plan BootstrapPlan) string {
	lines := []string{
		fmt.Sprintf("REGISTRY_ADDR=%s", quoteEnvValue(plan.Addr)),
		fmt.Sprintf("REGISTRY_PUBLIC_URL=%s", quoteEnvValue(plan.PublicURL)),
		fmt.Sprintf("REGISTRY_STORAGE_ROOT=%s", quoteEnvValue(plan.StorageRoot)),
		fmt.Sprintf("REGISTRY_DATABASE_PATH=%s", quoteEnvValue(plan.DatabasePath)),
		fmt.Sprintf("REGISTRY_SERVICE_NAME=%s", quoteEnvValue(plan.ServiceName)),
		fmt.Sprintf("REGISTRY_TRIVY_ENABLED=%s", quoteEnvValue(strconv.FormatBool(plan.TrivyEnabled))),
		fmt.Sprintf("REGISTRY_TRIVY_SCHEDULE_ENABLED=%s", quoteEnvValue(strconv.FormatBool(plan.TrivyScheduleEnabled))),
		fmt.Sprintf("REGISTRY_TRIVY_INTERVAL=%s", quoteEnvValue(plan.TrivyInterval.String())),
		fmt.Sprintf("REGISTRY_TRIVY_TIMEOUT=%s", quoteEnvValue(plan.TrivyTimeout.String())),
		fmt.Sprintf("REGISTRY_TRIVY_CACHE_DIR=%s", quoteEnvValue(plan.TrivyCacheDir)),
		fmt.Sprintf("REGISTRY_TRIVY_BINARY_PATH=%s", quoteEnvValue(plan.TrivyBinaryPath)),
		fmt.Sprintf("REGISTRY_TRIVY_MAX_CONCURRENCY=%s", quoteEnvValue(strconv.Itoa(plan.TrivyMaxConcurrency))),
	}
	if strings.TrimSpace(plan.AuthPostgresDSN) != "" {
		lines = append(lines, fmt.Sprintf("REGISTRY_AUTH_POSTGRES_DSN=%s", quoteEnvValue(plan.AuthPostgresDSN)))
	}
	if strings.TrimSpace(plan.TLSCertFile) != "" && strings.TrimSpace(plan.TLSKeyFile) != "" {
		lines = append(lines,
			fmt.Sprintf("REGISTRY_TLS_CERT_FILE=%s", quoteEnvValue(plan.TLSCertFile)),
			fmt.Sprintf("REGISTRY_TLS_KEY_FILE=%s", quoteEnvValue(plan.TLSKeyFile)),
		)
	}

	return strings.Join(lines, "\n") + "\n"
}

func RenderSystemdUnit(plan BootstrapPlan) string {
	execStart := fmt.Sprintf("%s serve -addr=${REGISTRY_ADDR} -public-url=${REGISTRY_PUBLIC_URL} -storage-root=${REGISTRY_STORAGE_ROOT} -db=${REGISTRY_DATABASE_PATH} -service=${REGISTRY_SERVICE_NAME}", plan.BinaryPath)
	if strings.TrimSpace(plan.AuthPostgresDSN) != "" {
		execStart += " -auth-postgres-dsn=${REGISTRY_AUTH_POSTGRES_DSN}"
	}
	if strings.TrimSpace(plan.TLSCertFile) != "" && strings.TrimSpace(plan.TLSKeyFile) != "" {
		execStart += " -tls-cert-file=${REGISTRY_TLS_CERT_FILE} -tls-key-file=${REGISTRY_TLS_KEY_FILE}"
	}

	return fmt.Sprintf(`[Unit]
Description=Regixtry service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=%s
ExecStart=%s
Restart=on-failure
RestartSec=5
WorkingDirectory=%s

[Install]
WantedBy=multi-user.target
`, plan.EnvPath, execStart, plan.StorageRoot)
}

func quoteEnvValue(value string) string {
	return strconv.Quote(value)
}
