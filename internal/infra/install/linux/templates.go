package linux

import (
	"fmt"
	"strconv"
	"strings"
)

type BootstrapPlan struct {
	Mode         string
	Addr         string
	PublicURL    string
	StorageRoot  string
	DatabasePath string
	ContentPath  string
	StatePath    string
	EnvPath      string
	UnitPath     string
	BinaryPath   string
	ServiceName  string
}

func RenderEnvFile(plan BootstrapPlan) string {
	lines := []string{
		fmt.Sprintf("REGISTRY_ADDR=%s", quoteEnvValue(plan.Addr)),
		fmt.Sprintf("REGISTRY_PUBLIC_URL=%s", quoteEnvValue(plan.PublicURL)),
		fmt.Sprintf("REGISTRY_STORAGE_ROOT=%s", quoteEnvValue(plan.StorageRoot)),
		fmt.Sprintf("REGISTRY_DATABASE_PATH=%s", quoteEnvValue(plan.DatabasePath)),
		fmt.Sprintf("REGISTRY_SERVICE_NAME=%s", quoteEnvValue(plan.ServiceName)),
	}

	return strings.Join(lines, "\n") + "\n"
}

func RenderSystemdUnit(plan BootstrapPlan) string {
	return fmt.Sprintf(`[Unit]
Description=Regixtry service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=%s
ExecStart=%s serve -addr=${REGISTRY_ADDR} -public-url=${REGISTRY_PUBLIC_URL} -storage-root=${REGISTRY_STORAGE_ROOT} -db=${REGISTRY_DATABASE_PATH} -service=${REGISTRY_SERVICE_NAME}
Restart=on-failure
RestartSec=5
WorkingDirectory=%s

[Install]
WantedBy=multi-user.target
`, plan.EnvPath, plan.BinaryPath, plan.StorageRoot)
}

func quoteEnvValue(value string) string {
	return strconv.Quote(value)
}
