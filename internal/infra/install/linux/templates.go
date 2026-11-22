package linux

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type BootstrapPlan struct {
	Mode                       string
	Addr                       string
	PublicURL                  string
	RuntimeTLSMode             string
	TLSCertFile                string
	TLSKeyFile                 string
	AuthPostgresDSN            string
	StorageRoot                string
	DatabasePath               string
	ContentPath                string
	StatePath                  string
	EnvPath                    string
	UnitPath                   string
	BinaryPath                 string
	ServiceName                string
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
	DeleteEnabled              bool
	GCDeleteEnabled            bool
}

type envEntry struct{ key, value string }

// envEntries lists every KEY=value pair written to the managed env file.
func envEntries(plan BootstrapPlan) []envEntry {
	entries := []envEntry{
		{"REGISTRY_ADDR", plan.Addr},
		{"REGISTRY_PUBLIC_URL", plan.PublicURL},
		{"REGISTRY_STORAGE_ROOT", plan.StorageRoot},
		{"REGISTRY_DATABASE_PATH", plan.DatabasePath},
		{"REGISTRY_SERVICE_NAME", plan.ServiceName},
	}
	if strings.TrimSpace(plan.AuthPostgresDSN) != "" {
		entries = append(entries, envEntry{"REGISTRY_AUTH_POSTGRES_DSN", plan.AuthPostgresDSN})
	}
	if strings.TrimSpace(plan.TLSCertFile) != "" && strings.TrimSpace(plan.TLSKeyFile) != "" {
		entries = append(entries,
			envEntry{"REGISTRY_TLS_CERT_FILE", plan.TLSCertFile},
			envEntry{"REGISTRY_TLS_KEY_FILE", plan.TLSKeyFile},
		)
	}
	if plan.DeleteEnabled {
		entries = append(entries, envEntry{"REGISTRY_DELETE_ENABLED", "true"})
	}
	if plan.GCDeleteEnabled {
		entries = append(entries, envEntry{"REGISTRY_GC_DELETE_ENABLED", "true"})
	}
	return entries
}

// ValidateEnvValues rejects any env value systemd would mangle: a C0 control
// character (newline, CR, tab, NUL, ...) or DEL cannot be represented in a
// double-quoted EnvironmentFile value. The value itself is never echoed, since
// it may be a DSN.
func ValidateEnvValues(plan BootstrapPlan) error {
	for _, entry := range envEntries(plan) {
		for _, r := range entry.value {
			if r < 0x20 || r == 0x7f {
				return fmt.Errorf("%s contains a control character (U+%04X) that cannot be written to the env file", entry.key, r)
			}
		}
	}
	return nil
}

func RenderEnvFile(plan BootstrapPlan) string {
	lines := make([]string, 0, 10)
	for _, entry := range envEntries(plan) {
		lines = append(lines, entry.key+"="+quoteEnvValue(entry.value))
	}
	return strings.Join(lines, "\n") + "\n"
}

func RenderSystemdUnit(plan BootstrapPlan) string {
	execStart := fmt.Sprintf("%s serve -addr=${REGISTRY_ADDR} -public-url=${REGISTRY_PUBLIC_URL} -storage-root=${REGISTRY_STORAGE_ROOT} -db=${REGISTRY_DATABASE_PATH} -service=${REGISTRY_SERVICE_NAME}", plan.BinaryPath)
	// The Postgres DSN carries a password: it reaches the daemon only through
	// the 0600 EnvironmentFile (serve reads REGISTRY_AUTH_POSTGRES_DSN), never
	// as an ExecStart argument visible in the unit file, systemctl show or ps.
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

// quoteEnvValue renders a systemd EnvironmentFile double-quoted value. Inside
// double quotes systemd treats only backslash, double quote, dollar and
// backtick specially, so exactly those are backslash-escaped; everything else
// (single quote, space, #, %, unicode) is literal. Control characters are
// rejected earlier by ValidateEnvValues.
func quoteEnvValue(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\', '"', '$', '`':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// ParseManagedEnvValue reads a value written by quoteEnvValue, or by older
// installers that used Go-quoted strings.
func ParseManagedEnvValue(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if unquoted, err := strconv.Unquote(trimmed); err == nil {
		return unquoted, nil
	}
	if len(trimmed) >= 2 && trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
		inner := trimmed[1 : len(trimmed)-1]
		var b strings.Builder
		for i := 0; i < len(inner); i++ {
			if inner[i] == '\\' && i+1 < len(inner) {
				switch inner[i+1] {
				case '\\', '"', '$', '`':
					i++
				}
			}
			b.WriteByte(inner[i])
		}
		return b.String(), nil
	}
	return trimmed, nil
}
