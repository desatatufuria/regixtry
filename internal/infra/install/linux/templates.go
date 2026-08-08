package linux

import (
	"fmt"
	"strconv"
	"strings"
)

type BootstrapPlan struct {
	Mode            string
	Addr            string
	PublicURL       string
	RuntimeTLSMode  string
	TLSCertFile     string
	TLSKeyFile      string
	AuthPostgresDSN string
	StorageRoot     string
	DatabasePath    string
	ContentPath     string
	StatePath       string
	EnvPath         string
	UnitPath        string
	BinaryPath      string
	ServiceName     string
}

func RenderEnvFile(plan BootstrapPlan) string {
	lines := []string{
		fmt.Sprintf("REGISTRY_ADDR=%s", quoteEnvValue(plan.Addr)),
		fmt.Sprintf("REGISTRY_PUBLIC_URL=%s", quoteEnvValue(plan.PublicURL)),
		fmt.Sprintf("REGISTRY_STORAGE_ROOT=%s", quoteEnvValue(plan.StorageRoot)),
		fmt.Sprintf("REGISTRY_DATABASE_PATH=%s", quoteEnvValue(plan.DatabasePath)),
		fmt.Sprintf("REGISTRY_SERVICE_NAME=%s", quoteEnvValue(plan.ServiceName)),
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
